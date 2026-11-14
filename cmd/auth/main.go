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
	shutdownTelemetry, err := telemetry.Setup(context.Background(), "ispilolite-api")
	if err != nil {
		log.Fatalf("telemetry startup failed: %v", err)
	}
	defer shutdownTelemetry(context.Background())

	// Validate configuration up front. Load .env first so values placed there
	// (not only real environment variables) are visible to the validator. In
	// production this fails fast when a required secret (e.g. JWT_SECRET) is
	// missing, instead of starting with a known-insecure development fallback.
	database.LoadDotEnv(".env")
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	configPath := getenv("CONFIG_PATH", "config/config.yaml")
	if err := database.InitDB(configPath); err != nil {
		log.Fatalf("database startup check failed: %v", err)
	}
	if err := database.InitRedis(configPath); err != nil {
		log.Fatalf("redis startup check failed: %v", err)
	}
	if !database.Ready() {
		log.Fatal("database and redis must be initialized before serving")
	}

	userRepo, err := postgres.NewUserRepo()
	if err != nil {
		log.Fatalf("user repository: %v", err)
	}
	cacheRepo, err := redis.NewCacheRepo()
	if err != nil {
		log.Fatalf("cache repository: %v", err)
	}
	middleware.InitAuth(cfg.JWTSecret, cfg.JWTIssuer)

	revocationService := authsvc.NewAuthService(userRepo, cacheRepo)
	middleware.SetRevocationChecker(revocationService.IsTokenRevoked)

	handler, matcher, err := routes.SetupRouter()
	if err != nil {
		log.Fatalf("router startup failed: %v", err)
	}

	collectorStop := make(chan struct{})
	monitoring.StartDBCollector(collectorStop, database.GetWriter(), database.GetReader())
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	bootstrap.StartBackgroundWorkers(workerCtx, matcher, database.GetWriter(), log.Default())

	queueCtx, cancelQueue := context.WithCancel(context.Background())
	var queueService *queue.Service
	if rabbitURL := os.Getenv("RABBITMQ_URL"); rabbitURL != "" {
		var err error
		queueService, err = queue.New(queue.Config{URL: rabbitURL, ConsumerName: getenv("RABBITMQ_CONSUMER_NAME", "ispilolite-api"), Prefetch: getenvInt("RABBITMQ_PREFETCH", 20)})
		if err != nil {
			if getenv("RABBITMQ_REQUIRED", "false") == "true" {
				log.Fatalf("rabbitmq startup failed: %v", err)
			}
			log.Printf("rabbitmq unavailable; continuing without async events: %v", err)
		} else {
			queue.SetDefault(queueService)
			queueService.StartDepthCollector(queueCtx)
			log.Printf("rabbitmq connected")
		}
	}

	// SSE streams can remain open for hours; a server-wide WriteTimeout would
	// close them after 30s. The reverse proxy should enforce client limits.
	srv := &http.Server{Addr: ":" + strconv.Itoa(cfg.Port), Handler: middleware.WriteDeadline(30 * time.Second)(handler), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		log.Printf("ispilolite api listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	close(collectorStop)
	cancelQueue()
	cancelWorkers()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	if queueService != nil {
		if err := queueService.Close(); err != nil {
			log.Printf("rabbitmq shutdown failed: %v", err)
		}
	}
	if err := database.CloseRedis(); err != nil {
		log.Printf("redis shutdown failed: %v", err)
	}
	if err := database.CloseDB(); err != nil {
		log.Printf("database shutdown failed: %v", err)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return value
	}
	return fallback
}
