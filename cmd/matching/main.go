// Command matching exposes the matching service runtime health endpoint.
// Business matching is implemented in internal/services/matching and awaits
// the repository/matches-table adapter documented in rem.md.
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
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/services/matching"
)

func main() {
	shutdownTelemetry, err := telemetry.Setup(context.Background(), "ispilolite-matching")
	if err != nil {
		log.Fatalf("telemetry startup failed: %v", err)
	}
	defer shutdownTelemetry(context.Background())
	service := matching.NewMatchingService(postgres.NewMatchingRepository(), nil)
	service.StartExpiryWorker(context.Background(), time.Hour, 24*time.Hour)
	mux := http.NewServeMux()
	mux.Handle("/metrics", monitoring.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: env("MATCHING_ADDR", ":8082"), Handler: monitoring.Middleware("matching", mux), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("matching server failed: %v", err)
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
