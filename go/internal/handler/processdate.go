package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"arteci-go/internal/dateparser"
	"arteci-go/internal/observability"
	"arteci-go/internal/storage"
)

var numWorkers = func() int {
	n := runtime.NumCPU()
	if n > 8 {
		return 8
	}
	return n
}()

const (
	batchSize    = 100_000
	previewMax   = 100
	scanBufSize  = 4 * 1024 * 1024
	writeBufSize = 4 * 1024 * 1024
)

type processRequest struct {
	Bucket      string   `json:"bucket"`
	File        string   `json:"file"`
	DateColumns []string `json:"date_columns"`
	DateFormats []string `json:"date_formats"`
}

type batchJob struct {
	id      int
	lines   []string
	colIdxs []int
	hints   []dateparser.Hint
}

type batchResult struct {
	id            int
	lines         []string
	failed        int
	failedSamples map[int][]string // colIdx → up to 5 raw values that failed
}

func ProcessDate(mc *storage.MinioClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req processRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if len(req.DateColumns) == 0 {
			jsonError(w, "date_columns must not be empty", http.StatusBadRequest)
			return
		}
		if len(req.DateColumns) != len(req.DateFormats) {
			jsonError(w, fmt.Sprintf("date_columns (%d items) and date_formats (%d items) must have the same length",
				len(req.DateColumns), len(req.DateFormats)), http.StatusBadRequest)
			return
		}

		hints := make([]dateparser.Hint, len(req.DateFormats))
		for i, f := range req.DateFormats {
			hints[i] = dateparser.Hint(f)
		}

		ext := strings.ToLower(filepath.Ext(req.File))
		isExcel := ext == ".xlsx" || ext == ".xls"
		fileType := "csv"
		spanName := "ProcessDateService.processCsv"
		if isExcel {
			fileType = "excel"
			spanName = "ProcessDateService.processExcel"
		}

		ctx, span := observability.Tracer.Start(ctx, spanName,
			trace.WithAttributes(
				attribute.String("minio.bucket", req.Bucket),
				attribute.String("minio.file", req.File),
				attribute.String("date.columns", strings.Join(req.DateColumns, ",")),
			),
		)

		start := time.Now()
		observability.ProcessLog.Info(ctx, "Starting date normalization", observability.Attrs{
			"method": spanName, "bucket": req.Bucket, "file": req.File,
			"columns": strings.Join(req.DateColumns, ","),
			"formats": strings.Join(req.DateFormats, ","),
			"file_type": fileType,
		})

		var preview []map[string]string
		var totalRows, failedRows int64
		var err error
		if isExcel {
			preview, totalRows, failedRows, err = processExcel(ctx, mc, req.Bucket, req.File, req.DateColumns, hints, start)
		} else {
			preview, totalRows, failedRows, err = processCSV(ctx, mc, req.Bucket, req.File, req.DateColumns, hints, start)
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			durationMs := time.Since(start).Milliseconds()
			observability.ProcessLog.Error(ctx, "Date normalization failed", observability.Attrs{
				"method": spanName, "bucket": req.Bucket, "file": req.File,
				"error": err.Error(), "duration_ms": durationMs,
			})
			msg := err.Error()
			switch {
			case strings.Contains(msg, "NoSuchKey") || strings.Contains(msg, "not found"):
				jsonError(w, msg, http.StatusNotFound)
			case strings.Contains(msg, "Column") || strings.Contains(msg, "column"):
				jsonError(w, msg, http.StatusUnprocessableEntity)
			default:
				jsonError(w, "internal error: "+msg, http.StatusInternalServerError)
			}
			return
		}

		durationMs := time.Since(start).Milliseconds()
		rowsPerSec := int64(0)
		if durationMs > 0 {
			rowsPerSec = totalRows * 1000 / durationMs
		}

		span.SetAttributes(
			attribute.Int64("total_rows", totalRows),
			attribute.Int64("date.rows_failed", failedRows),
			attribute.Int("preview.count", len(preview)),
		)
		span.SetStatus(codes.Ok, "")
		span.End()

		observability.ProcessRowsTotal.Add(ctx, totalRows, metric.WithAttributes(attribute.String("file_type", fileType)))
		observability.ProcessRowsFailed.Add(ctx, failedRows, metric.WithAttributes(attribute.String("file_type", fileType)))
		observability.ProcessDuration.Record(ctx, durationMs, metric.WithAttributes(attribute.String("file_type", fileType)))

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		observability.ProcessLog.Info(ctx, "Date normalization completed", observability.Attrs{
			"method": spanName, "bucket": req.Bucket, "file": req.File,
			"total_rows": totalRows, "rows_failed": failedRows,
			"duration_ms": durationMs, "rows_per_sec": rowsPerSec,
			"alloc_mb": int64(mem.Alloc) / 1024 / 1024,
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"rows":       preview,
			"total_rows": totalRows,
		})
	}
}

func processCSV(
	ctx context.Context,
	mc *storage.MinioClient,
	bucket, file string,
	dateColumns []string,
	hints []dateparser.Hint,
	start time.Time,
) ([]map[string]string, int64, int64, error) {

	src, err := mc.GetObject(ctx, bucket, file)
	if err != nil {
		return nil, 0, 0, err
	}

	pr, pw := io.Pipe()
	uploadErr := make(chan error, 1)
	go func() {
		uploadErr <- mc.PutObject(ctx, bucket, file, pr, "text/csv")
	}()

	observability.ProcessLog.Info(ctx, "Upload stream goroutine started — pipe open", observability.Attrs{
		"method": "processCsv", "bucket": bucket, "file": file,
	})

	bw := bufio.NewWriterSize(pw, writeBufSize)
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, scanBufSize), scanBufSize)

	if !scanner.Scan() {
		src.Close()
		pw.CloseWithError(fmt.Errorf("empty file"))
		<-uploadErr
		return nil, 0, 0, fmt.Errorf("empty file")
	}
	headerLine := scanner.Text()
	headers := strings.Split(headerLine, ";")
	headerIdx := make(map[string]int, len(headers))
	for i, h := range headers {
		headerIdx[h] = i
	}

	colIdxs := make([]int, len(dateColumns))
	for i, col := range dateColumns {
		idx, ok := headerIdx[col]
		if !ok {
			src.Close()
			pw.CloseWithError(fmt.Errorf("column not found"))
			<-uploadErr
			return nil, 0, 0, fmt.Errorf("Column '%s' not found in file. Available: %s", col, strings.Join(headers, ", "))
		}
		colIdxs[i] = idx
	}
	bw.WriteString(headerLine + "\n")

	observability.ProcessLog.Info(ctx, "CSV header parsed — date columns validated", observability.Attrs{
		"method":         "processCsv",
		"bucket":         bucket,
		"file":           file,
		"col_count":      len(headers),
		"date_col_count": len(dateColumns),
		"date_columns":   strings.Join(dateColumns, ","),
	})

	jobCh := make(chan batchJob, numWorkers)
	resultCh := make(chan batchResult, numWorkers*2)
	var wg sync.WaitGroup

	observability.ProcessLog.Info(ctx, "Worker pool started — streaming normalization in progress", observability.Attrs{
		"method":      "processCsv",
		"num_workers": numWorkers,
		"batch_size":  batchSize,
	})

	for range numWorkers {
		go func() {
			colCache := make(map[int]string)
			for job := range jobCh {
				out := make([]string, len(job.lines))
				failed := 0
				failedSamples := make(map[int][]string)
				for r, line := range job.lines {
					parts := strings.Split(line, ";")
					for ci, idx := range job.colIdxs {
						if idx >= len(parts) {
							continue
						}
						raw := parts[idx]
						res := dateparser.Normalize(raw, job.hints[ci], colCache[idx])
						if res.MatchedFormat != "" {
							colCache[idx] = res.MatchedFormat
						}
						if res.WasParsed && len(res.Normalized) >= 10 {
							yearStr := res.Normalized[6:10]
							if y, err := strconv.Atoi(yearStr); err == nil && (y < 1900 || y > 2100) {
								if len(failedSamples[idx]) < 5 {
									failedSamples[idx] = append(failedSamples[idx], raw+" [year="+yearStr+"]")
								}
								res = dateparser.Result{Normalized: raw}
								failed++
							}
						}
						if !res.WasParsed && strings.TrimSpace(raw) != "" {
							failed++
							if len(failedSamples[idx]) < 5 {
								failedSamples[idx] = append(failedSamples[idx], raw)
							}
						}
						parts[idx] = res.Normalized
					}
					out[r] = strings.Join(parts, ";")
				}
				resultCh <- batchResult{id: job.id, lines: out, failed: failed, failedSamples: failedSamples}
				wg.Done()
			}
		}()
	}

	go func() {
		defer src.Close()
		batchID := 0
		batchCount := 0
		batch := make([]string, 0, batchSize)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			batch = append(batch, line)
			if len(batch) == batchSize {
				wg.Add(1)
				jobCh <- batchJob{id: batchID, lines: batch, colIdxs: colIdxs, hints: hints}
				batch = make([]string, 0, batchSize)
				batchID++
				batchCount++
			}
		}
		if len(batch) > 0 {
			wg.Add(1)
			jobCh <- batchJob{id: batchID, lines: batch, colIdxs: colIdxs, hints: hints}
			batchCount++
		}
		close(jobCh)
		observability.ProcessLog.Info(ctx, "All batches dispatched to worker pool", observability.Attrs{
			"method":      "processCsv",
			"batch_count": batchCount,
			"num_workers": numWorkers,
		})
		wg.Wait()
		close(resultCh)
	}()

	allFailedSamples := make(map[int][]string) 
	pending := make(map[int]batchResult)
	nextWrite := 0
	var totalRows, totalFailed int64
	preview := make([]map[string]string, 0, previewMax)

	for res := range resultCh {
		for idx, samples := range res.failedSamples {
			for _, s := range samples {
				if len(allFailedSamples[idx]) < 5 {
					allFailedSamples[idx] = append(allFailedSamples[idx], s)
				}
			}
		}
		pending[res.id] = res
		for {
			r, ok := pending[nextWrite]
			if !ok {
				break
			}
			delete(pending, nextWrite)
			nextWrite++
			totalFailed += int64(r.failed)

			for _, line := range r.lines {
				totalRows++
				if totalRows%1_000_000 == 0 {
					var mem runtime.MemStats
					runtime.ReadMemStats(&mem)
					observability.ProcessLog.Info(ctx, "CSV normalization progress", observability.Attrs{
						"method": "processCsv", "total_rows": totalRows,
						"rows_failed": totalFailed, "elapsed_ms": time.Since(start).Milliseconds(),
						"alloc_mb": int64(mem.Alloc) / 1024 / 1024,
					})
				}
				if len(preview) < previewMax {
					parts := strings.Split(line, ";")
					rec := make(map[string]string, len(headers))
					for i, h := range headers {
						if i < len(parts) {
							rec[h] = parts[i]
						} else {
							rec[h] = ""
						}
					}
					preview = append(preview, rec)
				}
				bw.WriteString(line + "\n")
			}
		}
	}

	bw.Flush()
	pw.Close()

	observability.ProcessLog.Info(ctx, "Write buffer flushed — waiting for MinIO upload confirmation", observability.Attrs{
		"method":     "processCsv",
		"bucket":     bucket,
		"file":       file,
		"total_rows": totalRows,
	})

	if err := <-uploadErr; err != nil {
		return nil, totalRows, totalFailed, fmt.Errorf("minio upload: %w", err)
	}

	observability.ProcessLog.Info(ctx, "MinIO write confirmed — file updated in place", observability.Attrs{
		"method":      "processCsv",
		"bucket":      bucket,
		"file":        file,
		"total_rows":  totalRows,
		"rows_failed": totalFailed,
	})

	for i, idx := range colIdxs {
		if samples, ok := allFailedSamples[idx]; ok {
			observability.ProcessLog.Warn(ctx, "Unparsed date values detected — verify format hints", observability.Attrs{
				"method":  "processCsv",
				"column":  dateColumns[i],
				"samples": strings.Join(samples, " | "),
			})
		}
	}

	return preview, totalRows, totalFailed, nil
}
