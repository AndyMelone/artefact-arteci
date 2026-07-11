package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"arteci-go/internal/observability"
)

type MinioClient struct {
	client *minio.Client
	Bucket string
}

func NewMinioClient() (*MinioClient, error) {
	endpoint := getenv("MINIO_ENDPOINT", "localhost")
	port := getenv("MINIO_PORT", "9000")
	portInt, _ := strconv.Atoi(port)
	useSSL := os.Getenv("MINIO_USE_SSL") == "true"
	accessKey := getenv("MINIO_ACCESS_KEY", "minioadmin")
	secretKey := getenv("MINIO_SECRET_KEY", "minioadmin")

	c, err := minio.New(fmt.Sprintf("%s:%d", endpoint, portInt), &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio connect: %w", err)
	}
	return &MinioClient{
		client: c,
		Bucket: getenv("MINIO_BUCKET", "raw"),
	}, nil
}

func (m *MinioClient) GetObject(ctx context.Context, bucket, file string) (io.ReadCloser, error) {
	ctx, span := observability.Tracer.Start(ctx, "Minio.getObject",
		trace.WithAttributes(
			attribute.String("minio.bucket", bucket),
			attribute.String("minio.file", file),
		),
	)
	defer span.End()

	observability.MinioLog.Info(ctx, "Fetching file from MinIO bucket", observability.Attrs{
		"method": "getObject", "bucket": bucket, "file": file,
	})
	t := time.Now()

	obj, err := m.client.GetObject(ctx, bucket, file, minio.GetObjectOptions{})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		observability.MinioLog.Error(ctx, "Failed to fetch file from MinIO", observability.Attrs{
			"method": "getObject", "bucket": bucket, "file": file, "error": err.Error(),
		})
		return nil, err
	}
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if isNotFound(err) {
			observability.MinioLog.Warn(ctx, "File not found in MinIO bucket", observability.Attrs{
				"method": "getObject", "bucket": bucket, "file": file,
			})
			return nil, fmt.Errorf("NoSuchKey: File '%s' not found in bucket '%s'", file, bucket)
		}
		observability.MinioLog.Error(ctx, "Failed to fetch file from MinIO", observability.Attrs{
			"method": "getObject", "bucket": bucket, "file": file, "error": err.Error(),
		})
		return nil, err
	}

	durationMs := time.Since(t).Milliseconds()
	observability.MinioGetDuration.Record(ctx, durationMs, metric.WithAttributes(
		attribute.String("bucket", bucket),
	))
	observability.MinioLog.Info(ctx, "File fetched from MinIO successfully", observability.Attrs{
		"method": "getObject", "bucket": bucket, "file": file, "duration_ms": durationMs,
	})
	span.SetStatus(codes.Ok, "")
	return obj, nil
}

func (m *MinioClient) PutObject(ctx context.Context, bucket, file string, r io.Reader) error {
	ctx, span := observability.Tracer.Start(ctx, "Minio.putObject",
		trace.WithAttributes(
			attribute.String("minio.bucket", bucket),
			attribute.String("minio.file", file),
		),
	)
	defer span.End()

	observability.MinioLog.Info(ctx, "Writing normalized file back to MinIO (in-place)", observability.Attrs{
		"method": "putObject", "bucket": bucket, "file": file,
	})
	t := time.Now()

	_, err := m.client.PutObject(ctx, bucket, file, r, -1, minio.PutObjectOptions{
		ContentType:      "text/csv",
		DisableMultipart: false,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		observability.MinioLog.Error(ctx, "Failed to write file to MinIO", observability.Attrs{
			"method": "putObject", "bucket": bucket, "file": file, "error": err.Error(),
		})
		return err
	}

	durationMs := time.Since(t).Milliseconds()
	span.SetAttributes(attribute.Int64("minio.duration_ms", durationMs))
	observability.MinioPutDuration.Record(ctx, durationMs, metric.WithAttributes(
		attribute.String("bucket", bucket),
	))
	observability.MinioLog.Info(ctx, "File written to MinIO successfully", observability.Attrs{
		"method": "putObject", "bucket": bucket, "file": file, "duration_ms": durationMs,
	})
	span.SetStatus(codes.Ok, "")
	return nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "NoSuchKey") || strings.Contains(s, "key does not exist")
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
