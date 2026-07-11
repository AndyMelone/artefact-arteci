package observability

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	HTTPRequestsTotal   metric.Int64Counter
	HTTPRequestDuration metric.Int64Histogram

	ProcessRowsTotal metric.Int64Counter
	ProcessRowsFailed metric.Int64Counter
	ProcessDuration  metric.Int64Histogram

	MinioGetDuration metric.Int64Histogram
	MinioPutDuration metric.Int64Histogram
)

// InitMetrics creates all metric instruments — must be called after Init().
func InitMetrics() {
	m := otel.GetMeterProvider().Meter("arteci-api")

	HTTPRequestsTotal, _ = m.Int64Counter("http.requests.total",
		metric.WithDescription("Total HTTP requests"))
	HTTPRequestDuration, _ = m.Int64Histogram("http.request.duration_ms",
		metric.WithDescription("HTTP request duration in milliseconds"),
		metric.WithUnit("ms"))

	ProcessRowsTotal, _ = m.Int64Counter("processdate.rows.processed_total",
		metric.WithDescription("Total rows processed across all files"))
	ProcessRowsFailed, _ = m.Int64Counter("processdate.rows.failed_total",
		metric.WithDescription("Total rows where date parsing failed"))
	ProcessDuration, _ = m.Int64Histogram("processdate.duration_ms",
		metric.WithDescription("File processing duration in milliseconds"),
		metric.WithUnit("ms"))

	MinioGetDuration, _ = m.Int64Histogram("minio.get.duration_ms",
		metric.WithDescription("MinIO getObject latency in milliseconds"),
		metric.WithUnit("ms"))
	MinioPutDuration, _ = m.Int64Histogram("minio.put.duration_ms",
		metric.WithDescription("MinIO putObject latency in milliseconds"),
		metric.WithUnit("ms"))
}
