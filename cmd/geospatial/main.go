// Command geospatial exposes the maintained geospatial service runtime health
// endpoint. Repository-backed nearby/coverage routes are tracked in rem.md.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ispilolite/pkg/monitoring"
	"ispilolite/pkg/telemetry"
)

func main() {
	shutdownTelemetry, err := telemetry.Setup(context.Background(), "ispilolite-geospatial")
	if err != nil {
		log.Fatalf("telemetry startup failed: %v", err)
	}
	defer shutdownTelemetry(context.Background())
	mux := http.NewServeMux()
	mux.Handle("/metrics", monitoring.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: env("GEOSPATIAL_ADDR", ":8083"), Handler: monitoring.Middleware("geospatial", mux), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("geospatial server failed: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
