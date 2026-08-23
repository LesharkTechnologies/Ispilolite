// Command core is the modular-monolith API entrypoint.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"ispilolite/api/routes"
	"ispilolite/internal/middleware"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/repository/redis"
	authsvc "ispilolite/internal/services/auth"
	"ispilolite/pkg/database"
	"ispilolite/pkg/monitoring"
	"ispilolite/pkg/telemetry"
	"ispilolite/pkg/worklife"
)

func main() {
	ctx := context.Background()
	shutdownTelemetry, err := telemetry.Setup(ctx, "ispilolite-core")
	if err != nil {
		log.Fatalf("telemetry startup failed: %v", err)
	}
	defer shutdownTelemetry(ctx)
	configPath := env("CONFIG_PATH", "config/config.yaml")
	if err := database.InitDB(configPath); err != nil {
		log.Fatalf("database startup failed: %v", err)
	}
	if err := database.InitRedis(configPath); err != nil {
		log.Fatalf("redis startup failed: %v", err)
	}
	defer database.CloseDB()
	defer database.CloseRedis()
	middleware.InitAuth(env("JWT_SECRET", "ispilolite-dev-secret"), env("JWT_ISSUER", "ispilolite"))
	revocations := authsvc.NewAuthService(postgres.NewUserRepo(), redis.NewCacheRepo())
	middleware.SetRevocationChecker(revocations.IsTokenRevoked)
	collectorStop := make(chan struct{})
	defer close(collectorStop)
	monitoring.StartDBCollector(collectorStop, database.GetWriter(), database.GetReader())
	worklife.StartDeadlineScheduler(ctx, database.GetWriter(), log.Default())
	srv := &http.Server{Addr: ":" + strconv.Itoa(envInt("PORT", 8001)), Handler: routes.SetupRouter(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("core server failed: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Printf("core shutdown failed: %v", err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
