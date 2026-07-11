package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"arteci-go/internal/handler"
	"arteci-go/internal/observability"
	"arteci-go/internal/storage"
)

func main() {
	_ = godotenv.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Initialize otel
	shutdown := observability.Init(ctx)
	observability.InitMetrics()
	observability.InitLoggers()

	mc, err := storage.NewMinioClient()
	if err != nil {
		log.Fatalf("minio init failed: %v", err)
	}
	log.Printf("MinIO connected — bucket=%s", mc.Bucket)

	if err := mc.EnsureBucket(ctx); err != nil {
		log.Printf("[warn] bucket ensure: %v", err)
	}
	mc.SeedBucket(ctx,
		[]string{"ressources", "fixtures", "../ressources", "../fixtures"},
		[]string{"lst_of_users_anon_1.csv", "lst_of_users_anon_2.csv", "lst_of_users_anon_3.csv"},
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /columns", handler.Columns(mc))
	mux.HandleFunc("POST /processDate", handler.ProcessDate(mc))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: otelMiddleware(mux),
	}

	log.Printf("Go API listening on :%s", port)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	shutdown(shutCtx)
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func otelMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		spanName := r.Method + " " + r.URL.Path
		start := time.Now()

		ctx, span := observability.Tracer.Start(ctx, spanName)
		sc := span.SpanContext()
		traceID := sc.TraceID().String()
		spanID := sc.SpanID().String()

		observability.HTTPLog.Info(ctx, "Incoming HTTP request", observability.Attrs{
			"method": r.Method, "path": r.URL.Path, "traceId": traceID, "spanId": spanID,
		})

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(ctx))

		durationMs := time.Since(start).Milliseconds()
		status := "ok"
		if rw.statusCode >= 500 {
			status = "error"
			span.SetStatus(codes.Error, http.StatusText(rw.statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.SetAttributes(attribute.Int64("http.duration_ms", durationMs))
		span.End()

		attrs := metric.WithAttributes(
			attribute.String("method", r.Method),
			attribute.String("path", r.URL.Path),
			attribute.String("status", status),
		)
		observability.HTTPRequestDuration.Record(ctx, durationMs, attrs)
		observability.HTTPRequestsTotal.Add(ctx, 1, attrs)

		if status == "error" {
			observability.HTTPLog.Error(ctx, "HTTP request failed with error", observability.Attrs{
				"method": r.Method, "path": r.URL.Path,
				"duration_ms": durationMs, "traceId": traceID, "spanId": spanID,
			})
		} else {
			observability.HTTPLog.Info(ctx, "HTTP request completed successfully", observability.Attrs{
				"method": r.Method, "path": r.URL.Path,
				"duration_ms": durationMs, "traceId": traceID,
			})
		}

	})
}
