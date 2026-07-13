package handler

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"arteci-go/internal/observability"
	"arteci-go/internal/storage"
)

func Columns(mc *storage.MinioClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		bucket := r.URL.Query().Get("bucket")
		file := r.URL.Query().Get("file")
		if bucket == "" || file == "" {
			jsonError(w, "bucket and file query params are required", http.StatusBadRequest)
			return
		}

		ctx, span := observability.Tracer.Start(ctx, "ColumnsService.getColumns",
			trace.WithAttributes(
				attribute.String("minio.bucket", bucket),
				attribute.String("minio.file", file),
			),
		)
		defer span.End()

		observability.ColumnsLog.Info(ctx, "Reading column headers from file", observability.Attrs{
			"method": "getColumns", "bucket": bucket, "file": file,
		})

		obj, err := mc.GetObject(ctx, bucket, file)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			observability.ColumnsLog.Error(ctx, "Failed to read column headers from file", observability.Attrs{
				"method": "getColumns", "bucket": bucket, "file": file, "error": err.Error(),
			})
			jsonError(w, "file not found: "+err.Error(), http.StatusNotFound)
			return
		}

		var columns []string
		ext := strings.ToLower(filepath.Ext(file))
		if ext == ".xlsx" || ext == ".xls" {
			columns, err = columnsFromExcel(obj)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				jsonError(w, "failed to read excel: "+err.Error(), http.StatusUnprocessableEntity)
				return
			}
		} else {
			defer obj.Close()
			br := bufio.NewReader(obj)
			if bom, _ := br.Peek(3); bytes.Equal(bom, []byte{0xEF, 0xBB, 0xBF}) {
				br.Discard(3)
			}
			sample, _ := br.Peek(4096)
			cr := csv.NewReader(br)
			cr.Comma = detectDelimiter(sample)
			cr.LazyQuotes = true
			cr.FieldsPerRecord = -1
			columns, err = cr.Read()
			if err != nil {
				span.SetStatus(codes.Error, "file is empty or unreadable")
				jsonError(w, "file is empty or unreadable", http.StatusUnprocessableEntity)
				return
			}
		}

		span.SetAttributes(attribute.Int("columns.count", len(columns)))
		span.SetStatus(codes.Ok, "")
		observability.ColumnsLog.Info(ctx, "Column headers read successfully", observability.Attrs{
			"method": "getColumns", "bucket": bucket, "file": file,
			"count": len(columns), "columns": strings.Join(columns, ","),
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(columns)
	}
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	b, _ := json.Marshal(map[string]string{"message": msg})
	w.Write(b)
}
