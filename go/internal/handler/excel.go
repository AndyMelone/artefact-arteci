package handler

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"arteci-go/internal/dateparser"
	"arteci-go/internal/observability"
	"arteci-go/internal/storage"
)

const excelContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// spillToTempFile copies src to a temp file and returns it as an io.ReaderAt.
// archive/zip needs random access (the central directory sits at the end of
// the file), and a MinIO GetObject stream isn't seekable — so unlike the CSV
// path, XLSX can't be processed with O(1) memory. Spilling to local disk
// instead of buffering the whole file in RAM keeps memory flat regardless of
// file size; the caller must call the returned cleanup func.
func spillToTempFile(src io.ReadCloser) (f *os.File, size int64, cleanup func(), err error) {
	defer src.Close()
	tmp, err := os.CreateTemp("", "arteci-xlsx-*")
	if err != nil {
		return nil, 0, nil, fmt.Errorf("create temp file: %w", err)
	}
	cleanup = func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}
	n, err := io.Copy(tmp, src)
	if err != nil {
		cleanup()
		return nil, 0, nil, fmt.Errorf("spill to temp file: %w", err)
	}
	return tmp, n, cleanup, nil
}

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
	tmp, size, cleanup, err := spillToTempFile(src)
	if err != nil {
		return nil, 0, 0, err
	}
	defer cleanup()

	observability.ProcessLog.Info(ctx, "Excel file spilled to disk — starting fast ZIP/XML processing", observability.Attrs{
		"method": "processExcel", "bucket": bucket, "file": file,
		"size_bytes": size,
	})

	pr, pw := io.Pipe()
	uploadErr := make(chan error, 1)
	go func() {
		uploadErr <- mc.PutObject(ctx, bucket, file, pr, excelContentType)
	}()

	preview, totalRows, totalFailed, err := fastXLSX(tmp, size, dateColumns, hints, previewMax, pw)
	if err != nil {
		pw.CloseWithError(err)
		<-uploadErr
		return nil, 0, 0, err
	}
	pw.Close()

	if err := <-uploadErr; err != nil {
		return nil, 0, 0, fmt.Errorf("minio upload: %w", err)
	}

	observability.ProcessLog.Info(ctx, "Excel processed and uploaded", observability.Attrs{
		"method": "processExcel", "bucket": bucket, "file": file,
		"total_rows": totalRows, "rows_failed": totalFailed,
		"elapsed_ms": time.Since(start).Milliseconds(),
	})

	return preview, totalRows, totalFailed, nil
}

func columnsFromExcel(obj io.ReadCloser) ([]string, error) {
	tmp, size, cleanup, err := spillToTempFile(obj)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return nil, fmt.Errorf("xlsx: open zip: %w", err)
	}

	var ss []string
	var sheetFile interface{ Open() (io.ReadCloser, error) }

	for _, f := range zr.File {
		switch f.Name {
		case "xl/sharedStrings.xml":
			rc, e := f.Open()
			if e == nil {
				ss, _ = xlsxParseSST(rc)
				rc.Close()
			}
		case "xl/worksheets/sheet1.xml":
			sheetFile = f
		}
	}

	if sheetFile == nil {
		return nil, fmt.Errorf("xlsx: sheet not found")
	}

	rc, err := sheetFile.Open()
	if err != nil {
		return nil, fmt.Errorf("xlsx: open sheet: %w", err)
	}
	defer rc.Close()

	headers, err := xlsxReadFirstRow(rc, ss)
	if err != nil {
		return nil, fmt.Errorf("xlsx: %w", err)
	}

	// Strip BOM from first header if present
	if len(headers) > 0 {
		headers[0] = strings.TrimPrefix(headers[0], "\uFEFF")
	}
	return headers, nil
}
