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
	"ispilolite/internal/bootstrap"
	"ispilolite/internal/middleware"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/repository/redis"
	authsvc "ispilolite/internal/services/auth"
	"ispilolite/pkg/config"
	"ispilolite/pkg/database"
	"ispilolite/pkg/monitoring"
	"ispilolite/pkg/queue"
	"ispilolite/pkg/telemetry"
)

func main() {
	ctx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	shutdownTelemetry, err := telemetry.Setup(ctx, "ispilolite-core")
	if err != nil {
		log.Fatalf("telemetry startup failed: %v", err)
	}
	defer shutdownTelemetry(ctx)

	database.LoadDotEnv(".env")
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	configPath := env("CONFIG_PATH", "config/config.yaml")
	if err := database.InitDB(configPath); err != nil {
		log.Fatalf("database startup failed: %v", err)
	}
	if err := database.InitRedis(configPath); err != nil {
		log.Fatalf("redis startup failed: %v", err)
	}
	defer database.CloseDB()
	defer database.CloseRedis()
	if !database.Ready() {
		log.Fatal("database and redis must be initialized before serving")
	}

	middleware.InitAuth(cfg.JWTSecret, cfg.JWTIssuer)
	userRepo, err := postgres.NewUserRepo()
	if err != nil {
		log.Fatalf("user repository: %v", err)
	}
	cacheRepo, err := redis.NewCacheRepo()
	if err != nil {
		log.Fatalf("cache repository: %v", err)
	}
	revocations := authsvc.NewAuthService(userRepo, cacheRepo)
	middleware.SetRevocationChecker(revocations.IsTokenRevoked)

	handler, matcher, err := routes.SetupRouter()
	if err != nil {
		log.Fatalf("router startup failed: %v", err)
	}

	collectorStop := make(chan struct{})
	defer close(collectorStop)
	monitoring.StartDBCollector(collectorStop, database.GetWriter(), database.GetReader())
	bootstrap.StartBackgroundWorkers(ctx, matcher, database.GetWriter(), log.Default())
	queueCtx, cancelQueue := context.WithCancel(ctx)
	defer cancelQueue()
	var queueService *queue.Service
	if url := os.Getenv("RABBITMQ_URL"); url != "" {
		queueService, err = queue.New(queue.Config{URL: url, ConsumerName: env("RABBITMQ_CONSUMER_NAME", "ispilolite-core")})
		if err != nil {
			if env("RABBITMQ_REQUIRED", "false") == "true" {
				log.Fatalf("rabbitmq startup failed: %v", err)
			}
			log.Printf("rabbitmq unavailable; continuing without async events: %v", err)
		} else {
			queue.SetDefault(queueService)
			queueService.StartDepthCollector(queueCtx)
		}
	} else if env("RABBITMQ_REQUIRED", "false") == "true" {
		log.Fatal("RABBITMQ_REQUIRED=true but RABBITMQ_URL is empty")
	}

	srv := &http.Server{Addr: ":" + strconv.Itoa(cfg.Port), Handler: middleware.WriteDeadline(30 * time.Second)(handler), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("core server failed: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	cancelWorkers()
	cancelQueue()
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Printf("core shutdown failed: %v", err)
	}
	if queueService != nil {
		if err := queueService.Close(); err != nil {
			log.Printf("rabbitmq shutdown failed: %v", err)
		}
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
