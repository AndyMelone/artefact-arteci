package storage

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"

	"arteci-go/internal/observability"
)

// driveIDsRaw is the single source of truth for Google Drive file IDs,
// shared with docker/scripts/minio-init.sh and k8s/minio/minio-init-job.yaml
// (both read the same file, synced/mounted from this same path) — add
// missing IDs there when files are shared, not as a fourth hardcoded copy.
//
//go:embed drive-ids.env
var driveIDsRaw string

var driveFileIDs = parseDriveIDs(driveIDsRaw)

func parseDriveIDs(raw string) map[string]string {
	ids := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, id, ok := strings.Cut(line, "=")
		if ok {
			ids[name] = id
		}
	}
	return ids
}

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
	} else {
		observability.MinioLog.Info(ctx, "Bucket already exists  ready", observability.Attrs{"bucket": m.Bucket})
	}
	return nil
}

func (m *MinioClient) SeedBucket(ctx context.Context, searchDirs []string, files []string) {
	observability.MinioLog.Info(ctx, "Seed: starting bucket seed check", observability.Attrs{
		"bucket":     m.Bucket,
		"file_count": len(files),
	})
	for _, f := range files {
		observability.MinioLog.Info(ctx, "Seed: checking file presence", observability.Attrs{
			"file": f, "bucket": m.Bucket,
		})
		if _, err := m.client.StatObject(ctx, m.Bucket, f, minio.StatObjectOptions{}); err == nil {
			observability.MinioLog.Info(ctx, "Seed: file already present, skipped", observability.Attrs{
				"file": f, "bucket": m.Bucket,
			})
			continue
		}

		if m.seedFromLocal(ctx, f, searchDirs) {
			continue
		}
		if m.seedFromDrive(ctx, f) {
			continue
		}
		observability.MinioLog.Warn(ctx, "Seed: file not found in any source — upload manually if needed", observability.Attrs{
			"file": f, "bucket": m.Bucket,
		})
	}
}

func (m *MinioClient) seedFromLocal(ctx context.Context, f string, searchDirs []string) bool {
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
		_, putErr := m.client.PutObject(ctx, m.Bucket, f, fh, fi.Size(), minio.PutObjectOptions{ContentType: "text/csv"})
		fh.Close()
		if putErr == nil {
			observability.MinioLog.Info(ctx, "Seed: file uploaded from local", observability.Attrs{
				"file": f, "source": dir, "bucket": m.Bucket,
			})
			return true
		}
	}
	return false
}

func (m *MinioClient) seedFromDrive(ctx context.Context, f string) bool {
	id, ok := driveFileIDs[f]
	if !ok {
		return false
	}
	url := "https://drive.usercontent.google.com/download?id=" + id + "&export=download&confirm=t"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		observability.MinioLog.Info(ctx, "Seed: Google Drive download failed", observability.Attrs{
			"file": f, "error": err.Error(),
		})
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		observability.MinioLog.Info(ctx, "Seed: Google Drive returned non-200", observability.Attrs{
			"file": f, "status": resp.StatusCode,
		})
		return false
	}
	_, putErr := m.client.PutObject(ctx, m.Bucket, f, resp.Body, resp.ContentLength, minio.PutObjectOptions{ContentType: "text/csv"})
	if putErr != nil {
		observability.MinioLog.Info(ctx, "Seed: Google Drive upload to MinIO failed", observability.Attrs{
			"file": f, "error": putErr.Error(),
		})
		return false
	}
	observability.MinioLog.Info(ctx, "Seed: file downloaded from Google Drive", observability.Attrs{
		"file": f, "bucket": m.Bucket,
	})
	return true
}
