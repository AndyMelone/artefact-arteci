package observability

import (
	"context"
	"crypto/tls"
	"log"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
)

var Tracer trace.Tracer

func Init(ctx context.Context) func(context.Context) {
	rawEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if rawEndpoint == "" {
		log.Println("[otel] OTEL_EXPORTER_OTLP_ENDPOINT not set — observability disabled")
		Tracer = otel.GetTracerProvider().Tracer("arteci-api")
		return func(context.Context) {}
	}

	endpoint := cleanEndpoint(rawEndpoint)
	name := getEnvOr("OTEL_SERVICE_NAME", "arteci-api-go")

	res, _ := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(name),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
		resource.WithTelemetrySDK(),
	)

	tp := initTracer(ctx, endpoint, res)
	mp := initMeter(ctx, endpoint, res)
	lp := initLogProvider(ctx, endpoint, res)

	Tracer = otel.GetTracerProvider().Tracer("arteci-api")

	return func(ctx context.Context) {
		if tp != nil {
			_ = tp.Shutdown(ctx)
		}
		if mp != nil {
			_ = mp.Shutdown(ctx)
		}
		if lp != nil {
			_ = lp.Shutdown(ctx)
		}
	}
}

func initTracer(ctx context.Context, endpoint string, res *resource.Resource) *sdktrace.TracerProvider {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
	opts = append(opts, tlsOpts(otlptracegrpc.WithInsecure(), otlptracegrpc.WithTLSCredentials, otlptracegrpc.WithHeaders)...)
	exp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		log.Printf("[otel] trace exporter init: %v (traces disabled)", err)
		return nil
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return tp
}

func initMeter(ctx context.Context, endpoint string, res *resource.Resource) *sdkmetric.MeterProvider {
	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(endpoint)}
	opts = append(opts, tlsOpts(otlpmetricgrpc.WithInsecure(), otlpmetricgrpc.WithTLSCredentials, otlpmetricgrpc.WithHeaders)...)
	exp, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		log.Printf("[otel] metric exporter init: %v (metrics disabled)", err)
		return nil
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp,
			sdkmetric.WithInterval(15*time.Second),
		)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
	return mp
}

func initLogProvider(ctx context.Context, endpoint string, res *resource.Resource) *sdklog.LoggerProvider {
	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(endpoint)}
	opts = append(opts, tlsOpts(otlploggrpc.WithInsecure(), otlploggrpc.WithTLSCredentials, otlploggrpc.WithHeaders)...)
	exp, err := otlploggrpc.New(ctx, opts...)
	if err != nil {
		log.Printf("[otel] log exporter init: %v (otel logs disabled)", err)
		return nil
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
		sdklog.WithResource(res),
	)
	otelglobal.SetLoggerProvider(lp)
	return lp
}

// tlsOpts returns TLS + auth header options when OTEL_EXPORTER_OTLP_HEADERS is set,
// falling back to insecure for local/self-hosted collectors.
func tlsOpts[T any](insecure T, withTLS func(credentials.TransportCredentials) T, withHeaders func(map[string]string) T) []T {
	headers := parseOTLPHeaders()
	if len(headers) == 0 {
		return []T{insecure}
	}
	return []T{
		withTLS(credentials.NewTLS(&tls.Config{})),
		withHeaders(headers),
	}
}

func parseOTLPHeaders() map[string]string {
	raw := os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")
	if raw == "" {
		if key := os.Getenv("SIGNOZ_INGESTION_KEY"); key != "" {
			return map[string]string{"signoz-ingestion-key": key}
		}
		return nil
	}
	headers := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if ok {
			headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return headers
}

func cleanEndpoint(e string) string {
	e = strings.TrimPrefix(e, "http://")
	e = strings.TrimPrefix(e, "https://")
	return e
}

func getEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
