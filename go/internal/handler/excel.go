package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"go.opentelemetry.io/otel/attribute"

	"arteci-go/internal/dateparser"
	"arteci-go/internal/observability"
	"arteci-go/internal/storage"
)

const excelContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func processExcel(
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
	data, err := io.ReadAll(src)
	src.Close()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read excel: %w", err)
	}

	observability.ProcessLog.Info(ctx, "Excel file loaded into memory", observability.Attrs{
		"method": "processExcel", "bucket": bucket, "file": file,
		"size_bytes": int64(len(data)),
	})

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("parse excel: %w", err)
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)

	// Read header row to build column index
	headerRow, err := f.GetRows(sheetName)
	if err != nil || len(headerRow) == 0 {
		return nil, 0, 0, fmt.Errorf("excel: empty or unreadable sheet '%s'", sheetName)
	}
	headers := headerRow[0]
	headerIdx := make(map[string]int, len(headers))
	for i, h := range headers {
		headerIdx[strings.TrimSpace(h)] = i
	}

	colIdxs := make([]int, len(dateColumns))
	for i, col := range dateColumns {
		idx, ok := headerIdx[col]
		if !ok {
			return nil, 0, 0, fmt.Errorf("Column '%s' not found in file. Available: %s",
				col, strings.Join(headers, ", "))
		}
		colIdxs[i] = idx
	}

	observability.ProcessLog.Info(ctx, "Excel header parsed — date columns validated", observability.Attrs{
		"method":         "processExcel",
		"bucket":         bucket,
		"file":           file,
		"sheet":          sheetName,
		"col_count":      len(headers),
		"date_col_count": len(dateColumns),
		"date_columns":   strings.Join(dateColumns, ","),
	})

	rowIter, err := f.Rows(sheetName)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("excel: cannot iterate rows: %w", err)
	}

	preview := make([]map[string]string, 0, previewMax)
	var totalRows, totalFailed int64
	colCache := make(map[int]string)
	excelRowNum := 0 

	for rowIter.Next() {
		row, err := rowIter.Columns()
		if err != nil {
			rowIter.Close()
			return nil, totalRows, totalFailed, fmt.Errorf("excel: read row %d: %w", excelRowNum+1, err)
		}
		excelRowNum++
		if excelRowNum == 1 {
			continue
		}
		totalRows++

		for ci, colIdx := range colIdxs {
			if colIdx >= len(row) {
				continue
			}
			raw := row[colIdx]
			res := dateparser.Normalize(raw, hints[ci], colCache[colIdx])
			if res.MatchedFormat != "" {
				colCache[colIdx] = res.MatchedFormat
			}
			if !res.WasParsed && strings.TrimSpace(raw) != "" {
				totalFailed++
			}
			cellRef, _ := excelize.CoordinatesToCellName(colIdx+1, excelRowNum)
			if setErr := f.SetCellStr(sheetName, cellRef, res.Normalized); setErr != nil {
				observability.ProcessLog.Warn(ctx, "Excel: failed to set cell", observability.Attrs{
					"cell": cellRef, "error": setErr.Error(),
				})
			}
			row[colIdx] = res.Normalized
		}

		if int(totalRows) <= previewMax {
			rec := make(map[string]string, len(headers))
			for i, h := range headers {
				if i < len(row) {
					rec[h] = row[i]
				} else {
					rec[h] = ""
				}
			}
			preview = append(preview, rec)
		}

		if totalRows%100_000 == 0 {
			observability.ProcessLog.Info(ctx, "Excel normalization progress", observability.Attrs{
				"method": "processExcel", "total_rows": totalRows,
				"rows_failed": totalFailed, "elapsed_ms": time.Since(start).Milliseconds(),
			})
		}
	}
	rowIter.Close()

	observability.ProcessLog.Info(ctx, "Excel normalization done, writing back to MinIO", observability.Attrs{
		"method": "processExcel", "bucket": bucket, "file": file,
		"total_rows": totalRows, "rows_failed": totalFailed,
	})
	_ = attribute.Int64("excel.total_rows", totalRows) // referenced by OTel span in caller

	pr, pw := io.Pipe()
	uploadErr := make(chan error, 1)
	go func() {
		uploadErr <- mc.PutObject(ctx, bucket, file, pr, excelContentType)
	}()

	observability.ProcessLog.Info(ctx, "Excel upload goroutine started — writing workbook to pipe", observability.Attrs{
		"method": "processExcel", "bucket": bucket, "file": file,
	})

	if err := f.Write(pw); err != nil {
		pw.CloseWithError(err)
		<-uploadErr
		return nil, totalRows, totalFailed, fmt.Errorf("excel write: %w", err)
	}
	pw.Close()

	if err := <-uploadErr; err != nil {
		return nil, totalRows, totalFailed, fmt.Errorf("minio upload: %w", err)
	}

	observability.ProcessLog.Info(ctx, "Excel uploaded to MinIO — file updated in place", observability.Attrs{
		"method": "processExcel", "bucket": bucket, "file": file,
		"total_rows": totalRows, "rows_failed": totalFailed,
	})

	return preview, totalRows, totalFailed, nil
}

func columnsFromExcel(obj io.ReadCloser) ([]string, error) {
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read excel: %w", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse excel: %w", err)
	}
	defer f.Close()

	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("empty or unreadable sheet")
	}
	return rows[0], nil
}
