package handler

import (
	"bufio"
	"encoding/json"
	"net/http"
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
		defer obj.Close()

		scanner := bufio.NewScanner(obj)
		if !scanner.Scan() {
			span.SetStatus(codes.Error, "file is empty")
			jsonError(w, "file is empty", http.StatusUnprocessableEntity)
			return
		}
		columns := strings.Split(scanner.Text(), ";")

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
	w.Write([]byte(`{"message":"` + msg + `"}`))
}
