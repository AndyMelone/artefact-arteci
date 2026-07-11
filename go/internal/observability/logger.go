package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	otelglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/trace"
)

var (
	_pid      = os.Getpid()
	_hostname string
	_appName  string

	MinioLog   *Logger
	ColumnsLog *Logger
	ProcessLog *Logger
	HTTPLog    *Logger
)

func init() {
	h, _ := os.Hostname()
	_hostname = h
	_appName = getEnvOr("OTEL_SERVICE_NAME", "arteci-api-go")
}

func InitLoggers() {
	base := &Logger{}
	MinioLog = base.Child(Attrs{"service": "MinioService"})
	ColumnsLog = base.Child(Attrs{"service": "ColumnsService"})
	ProcessLog = base.Child(Attrs{"service": "ProcessDateService"})
	HTTPLog = base.Child(Attrs{"service": "OtelTraceInterceptor"})
}

type Attrs map[string]any

type Logger struct {
	base Attrs
}

func (l *Logger) Child(extra Attrs) *Logger {
	merged := make(Attrs, len(l.base)+len(extra))
	for k, v := range l.base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return &Logger{base: merged}
}

func (l *Logger) Info(ctx context.Context, message string, attrs Attrs) {
	l.emit(ctx, 30, otellog.SeverityInfo, "INFO", message, attrs)
}

func (l *Logger) Warn(ctx context.Context, message string, attrs Attrs) {
	l.emit(ctx, 40, otellog.SeverityWarn, "WARN", message, attrs)
}

func (l *Logger) Error(ctx context.Context, message string, attrs Attrs) {
	l.emit(ctx, 50, otellog.SeverityError, "ERROR", message, attrs)
}

func (l *Logger) emit(ctx context.Context, levelInt int, severity otellog.Severity, levelText, message string, attrs Attrs) {
	body := make(map[string]any, 12+len(l.base)+len(attrs))
	body["level"] = levelInt
	body["time"] = time.Now().UnixMilli()
	body["pid"] = _pid
	body["hostname"] = _hostname
	body["app"] = _appName
	body["message"] = message
	if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() {
		body["trace_id"] = sc.TraceID().String()
		body["span_id"] = sc.SpanID().String()
	}
	for k, v := range l.base {
		body[k] = v
	}
	for k, v := range attrs {
		body[k] = v
	}

	b, _ := json.Marshal(body)
	fmt.Fprintln(os.Stdout, string(b))

	// Emit OTel log record
	lg := otelglobal.GetLoggerProvider().Logger("arteci-api")
	var rec otellog.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverity(severity)
	rec.SetSeverityText(levelText)
	rec.SetBody(otellog.StringValue(string(b)))
	for k, v := range body {
		rec.AddAttributes(kvToOtel(k, v))
	}
	lg.Emit(ctx, rec)
}

func kvToOtel(k string, v any) otellog.KeyValue {
	switch val := v.(type) {
	case string:
		return otellog.String(k, val)
	case int:
		return otellog.Int64(k, int64(val))
	case int64:
		return otellog.Int64(k, val)
	case float64:
		return otellog.Float64(k, val)
	case bool:
		return otellog.Bool(k, val)
	default:
		return otellog.String(k, fmt.Sprintf("%v", val))
	}
}
