package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Environment      string
	Port             int
	DatabaseURL      string
	DatabaseReadURL  string
	RedisURL         string
	JWTSecret        string
	JWTIssuer        string
	AccessTTL        time.Duration
	RefreshTTL       time.Duration
	TalksasaToken    string
	TalksasaSenderID string
	AdminAlertPhones string
	AuditArchiveRoot string
}

func Load() (Config, error) {
	c := Config{Environment: getenv("ENVIRONMENT", "development"), Port: getint("PORT", 8001), DatabaseURL: os.Getenv("DATABASE_URL"), DatabaseReadURL: os.Getenv("DATABASE_READ_URL"), RedisURL: os.Getenv("REDIS_URL"), JWTSecret: os.Getenv("JWT_SECRET"), JWTIssuer: getenv("JWT_ISSUER", "ispilolite"), TalksasaToken: os.Getenv("TALKSASA_API_TOKEN"), TalksasaSenderID: os.Getenv("TALKSASA_SENDER_ID"), AdminAlertPhones: os.Getenv("ADMIN_ALERT_PHONES"), AuditArchiveRoot: getenv("AUDIT_ARCHIVE_ROOT", "storage/audit")}
	if value := os.Getenv("JWT_ACCESS_TTL"); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return c, fmt.Errorf("JWT_ACCESS_TTL: %w", err)
		}
		c.AccessTTL = d
	} else {
		c.AccessTTL = 15 * time.Minute
	}
	if value := os.Getenv("JWT_REFRESH_TTL"); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return c, fmt.Errorf("JWT_REFRESH_TTL: %w", err)
		}
		c.RefreshTTL = d
	} else {
		c.RefreshTTL = 7 * 24 * time.Hour
	}
	if c.Environment == "production" && c.JWTSecret == "" {
		return c, fmt.Errorf("JWT_SECRET is required in production")
	}
	return c, nil
}
func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func getint(k string, d int) int {
	v, e := strconv.Atoi(os.Getenv(k))
	if e != nil || v <= 0 {
		return d
	}
	return v
}
