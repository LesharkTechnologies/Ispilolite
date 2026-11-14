package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"gopkg.in/yaml.v2"
)

// RedisConfig holds the configuration for a redis connection.
type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type RedisRootConfig struct {
	Database struct {
		Redis RedisConfig `yaml:"redis"`
	} `yaml:"database"`
}

var (
	// RedisClient is the connection to the redis.
	RedisClient *redis.Client
)

// InitRedis initializes the redis client. A full connection string in REDIS_URL
// (e.g. redis://:pass@host:6379/0) takes precedence; otherwise the host/port/db
// are read from the config file with ${VAR} placeholders expanded from the
// environment, mirroring InitDB.
func InitRedis(configPath string) error {
	loadDotEnv(".env")

	options := &redis.Options{}
	if url := os.Getenv("REDIS_URL"); url != "" {
		parsed, err := redis.ParseURL(url)
		if err != nil {
			return fmt.Errorf("failed to parse REDIS_URL: %w", err)
		}
		options = parsed
	} else {
		configFile, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}

		configFile = []byte(os.ExpandEnv(string(configFile)))
		var config RedisRootConfig
		if err := yaml.Unmarshal(configFile, &config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}

		options.Addr = fmt.Sprintf("%s:%d", config.Database.Redis.Host, config.Database.Redis.Port)
		options.Password = config.Database.Redis.Password
		options.DB = config.Database.Redis.DB
	}

	RedisClient = redis.NewClient(options)

	if err := RedisClient.Ping(context.Background()).Err(); err != nil {
		_ = RedisClient.Close()
		RedisClient = nil
		return fmt.Errorf("failed to connect to redis: %w", err)
	}
	return nil
}

// GetRedis returns the redis client.
func GetRedis() *redis.Client {
	return RedisClient
}

// MustRedis returns the redis client or an error explaining that it has not
// been initialized, so callers fail fast instead of dereferencing nil.
func MustRedis() (*redis.Client, error) {
	if RedisClient == nil {
		return nil, errors.New("redis is not initialized: call database.InitRedis before constructing repositories")
	}
	return RedisClient, nil
}

// CloseRedis releases Redis resources during graceful shutdown.
func CloseRedis() error {
	if RedisClient == nil {
		return nil
	}
	err := RedisClient.Close()
	RedisClient = nil
	return err
}

// RedisClientWrapper wraps *redis.Client with the minimal interface needed by
// the idempotency package, keeping that package independent of go-redis types.
type RedisClientWrapper struct {
	client *redis.Client
}

// NewRedisClientWrapper wraps a redis.Client for idempotency storage.
func NewRedisClientWrapper(c *redis.Client) *RedisClientWrapper {
	return &RedisClientWrapper{client: c}
}

// Get returns the value for key, or ("", redis.Nil) when the key does not exist.
func (w *RedisClientWrapper) Get(ctx context.Context, key string) (string, error) {
	return w.client.Get(ctx, key).Result()
}

// Set stores value with the given expiration. Zero expiration means no TTL.
func (w *RedisClientWrapper) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return w.client.Set(ctx, key, value, expiration).Err()
}
