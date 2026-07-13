package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

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

	observability.ProcessLog.Info(ctx, "Excel file loaded — starting fast ZIP/XML processing", observability.Attrs{
		"method": "processExcel", "bucket": bucket, "file": file,
		"size_bytes": int64(len(data)),
	})

	pr, pw := io.Pipe()
	uploadErr := make(chan error, 1)
	go func() {
		uploadErr <- mc.PutObject(ctx, bucket, file, pr, excelContentType)
	}()

	preview, totalRows, totalFailed, err := fastXLSX(data, dateColumns, hints, previewMax, pw)
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
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read excel: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
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
