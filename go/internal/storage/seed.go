package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/minio/minio-go/v7"

	"arteci-go/internal/observability"
)

func (m *MinioClient) EnsureBucket(ctx context.Context) error {
	exists, err := m.client.BucketExists(ctx, m.Bucket)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := m.client.MakeBucket(ctx, m.Bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("create bucket %s: %w", m.Bucket, err)
		}
		observability.MinioLog.Info(ctx, "Bucket created", observability.Attrs{"bucket": m.Bucket})
	}
	return nil
}

func (m *MinioClient) SeedBucket(ctx context.Context, searchDirs []string, files []string) {
	for _, f := range files {
		if _, err := m.client.StatObject(ctx, m.Bucket, f, minio.StatObjectOptions{}); err == nil {
			observability.MinioLog.Info(ctx, "Seed: file already present, skipped", observability.Attrs{
				"file": f, "bucket": m.Bucket,
			})
			continue
		}

		uploaded := false
		for _, dir := range searchDirs {
			path := filepath.Join(dir, f)
			fh, err := os.Open(path)
			if err != nil {
				continue
			}
			fi, err := fh.Stat()
			if err != nil {
				fh.Close()
				continue
			}
			_, putErr := m.client.PutObject(ctx, m.Bucket, f, fh, fi.Size(), minio.PutObjectOptions{
				ContentType: "text/csv",
			})
			fh.Close()
			if putErr == nil {
				observability.MinioLog.Info(ctx, "Seed: file uploaded to bucket", observability.Attrs{
					"file": f, "source": dir, "bucket": m.Bucket,
				})
				uploaded = true
				break
			}
		}
		if !uploaded {
			observability.MinioLog.Info(ctx, "Seed: file not found in any search dir, skipped", observability.Attrs{
				"file": f, "bucket": m.Bucket,
			})
		}
	}
}
