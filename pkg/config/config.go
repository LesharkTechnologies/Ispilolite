package config

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
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

	// Elasticsearch configuration. When ESEnabled is false the search service
	// runs Postgres-only (no fast path, no breaker).
	ESEnabled          bool
	ESAddresses        []string
	ESUsername         string
	ESPassword         string
	ESAPIKey           string
	ESEnableSniff      bool
	ESMaxRetries       int
	ESRequestTimeout   time.Duration
	ESFallbackTimeout  time.Duration
	ESBreakerThreshold int
	ESBreakerCooldown  time.Duration
	// FallbackStrategy selects how ES and Postgres combine:
	//   "es_first"   - try ES, fall back to Postgres on failure (default)
	//   "pg_first"   - try Postgres, fall back to ES
	//   "es_only"    - ES only, error if unavailable
	//   "pg_only"    - Postgres only, ignore ES
	ESFallbackStrategy string
}

// IsProduction reports whether the process is running in production mode.
func (c Config) IsProduction() bool {
	return strings.EqualFold(strings.TrimSpace(c.Environment), "production")
}

// Load reads and validates the startup configuration. It returns a single,
// human-readable error listing every missing or malformed required value so a
// misconfigured deployment fails immediately with an actionable message rather
// than a nil dereference or an opaque driver error later on.
func Load() (Config, error) {
	c := Config{
		Environment:      getenv("ENVIRONMENT", "development"),
		Port:             getint("PORT", 8001),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		DatabaseReadURL:  os.Getenv("DATABASE_READ_URL"),
		RedisURL:         os.Getenv("REDIS_URL"),
		JWTSecret:        os.Getenv("JWT_SECRET"),
		JWTIssuer:        getenv("JWT_ISSUER", "ispilolite"),
		TalksasaToken:    os.Getenv("TALKSASA_API_TOKEN"),
		TalksasaSenderID: os.Getenv("TALKSASA_SENDER_ID"),
		AdminAlertPhones: os.Getenv("ADMIN_ALERT_PHONES"),
		AuditArchiveRoot: getenv("AUDIT_ARCHIVE_ROOT", "storage/audit"),

		// Elasticsearch
		ESEnabled:          parseBoolEnv("ES_ENABLED", true),
		ESAddresses:        splitAddrs(os.Getenv("ES_ADDRESSES")),
		ESUsername:         os.Getenv("ES_USERNAME"),
		ESPassword:         os.Getenv("ES_PASSWORD"),
		ESAPIKey:           os.Getenv("ES_API_KEY"),
		ESEnableSniff:      parseBoolEnv("ES_ENABLE_SNIFF", false),
		ESMaxRetries:       getint("ES_MAX_RETRIES", 2),
		ESRequestTimeout:   getduration("ES_REQUEST_TIMEOUT", 800*time.Millisecond),
		ESFallbackTimeout:  getduration("ES_FALLBACK_TIMEOUT", 2*time.Second),
		ESBreakerThreshold: getint("ES_BREAKER_THRESHOLD", 5),
		ESBreakerCooldown:  getduration("ES_BREAKER_COOLDOWN", 15*time.Second),
		ESFallbackStrategy: getenv("ES_FALLBACK_STRATEGY", "es_first"),
	}

	if value := strings.TrimSpace(os.Getenv("JWT_ACCESS_TTL")); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return c, fmt.Errorf("JWT_ACCESS_TTL must be a valid duration (for example 15m): %w", err)
		}
		c.AccessTTL = d
	} else {
		c.AccessTTL = 15 * time.Minute
	}
	if value := strings.TrimSpace(os.Getenv("JWT_REFRESH_TTL")); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return c, fmt.Errorf("JWT_REFRESH_TTL must be a valid duration (for example 168h): %w", err)
		}
		c.RefreshTTL = d
	} else {
		c.RefreshTTL = 7 * 24 * time.Hour
	}

	var missing []string

	if c.JWTSecret == "" || (c.IsProduction() && placeholder(c.JWTSecret)) {
		if c.IsProduction() {
			missing = append(missing, "JWT_SECRET is required in production")
		} else {
			// Development convenience only. Production never reaches here.
			c.JWTSecret = "ispilolite-dev-secret"
			log.Printf("WARN: JWT_SECRET is not set; using the insecure development fallback. Set JWT_SECRET before deploying.")
		}
	}

	if c.IsProduction() {
		if secret := strings.TrimSpace(os.Getenv("PAYMENT_WEBHOOK_SECRET")); secret == "" || placeholder(secret) {
			missing = append(missing, "PAYMENT_WEBHOOK_SECRET must be a non-placeholder secret in production")
		}
		if (c.DatabaseURL == "" || placeholder(c.DatabaseURL)) && os.Getenv("DB_URL") == "" && os.Getenv("DB_WRITER_HOST") == "" {
			missing = append(missing, "a database source is required in production: set DATABASE_URL (or DB_URL / DB_WRITER_HOST)")
		}
		if c.RedisURL == "" || placeholder(c.RedisURL) {
			missing = append(missing, "REDIS_URL is required in production")
		}
		if c.ESEnabled && len(c.ESAddresses) > 0 && placeholder(c.ESAPIKey) {
			missing = append(missing, "ES_API_KEY contains a placeholder; replace it or leave it empty for an unsecured local cluster")
		}
		// A plaintext database connection is only acceptable when the database
		// is not reachable over the network. This keeps the documented
		// single-VPS topology (Postgres on 127.0.0.1) working while rejecting
		// accidental remote plaintext connections.
		seen := map[string]bool{}
		for _, target := range databaseTargets(c) {
			if target.host == "" || seen[target.host] {
				continue
			}
			seen[target.host] = true
			if strings.EqualFold(target.sslmode, "disable") && !isLoopbackHost(target.host) {
				missing = append(missing, fmt.Sprintf(
					"sslmode=disable is only allowed for a loopback database in production (configured host %q); use sslmode=require or verify-full",
					target.host,
				))
			}
		}
	}

	if len(missing) > 0 {
		return c, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(missing, "\n  - "))
	}
	return c, nil
}

func placeholder(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "<") || strings.Contains(value, "your-") || strings.Contains(value, "change-me")
}

// dbTarget is a database endpoint as it will actually be used at runtime.
type dbTarget struct {
	host    string
	sslmode string
}

// databaseTargets mirrors the connection precedence in pkg/database.InitDB so
// production validation inspects the writer and reader that will really be
// dialled, including full DATABASE_URL values that override config.yaml.
func databaseTargets(c Config) []dbTarget {
	writerDSN := firstNonEmpty(c.DatabaseURL, os.Getenv("DB_URL"))
	var targets []dbTarget
	if writerDSN != "" {
		targets = append(targets, parseDatabaseTarget(writerDSN))
	} else {
		targets = append(targets, dbTarget{
			host:    os.Getenv("DB_WRITER_HOST"),
			sslmode: os.Getenv("DB_SSL_MODE"),
		})
	}

	readerDSN := firstNonEmpty(c.DatabaseReadURL, os.Getenv("DB_READ_URL"))
	switch {
	case readerDSN != "":
		targets = append(targets, parseDatabaseTarget(readerDSN))
	case writerDSN != "":
		// InitDB points the reader at the writer when only one URL is set.
		targets = append(targets, targets[0])
	default:
		targets = append(targets, dbTarget{
			host:    os.Getenv("DB_READER_HOST"),
			sslmode: os.Getenv("DB_SSL_MODE"),
		})
	}
	return targets
}

// parseDatabaseTarget extracts the host and sslmode from either a URL DSN or a
// libpq keyword/value DSN.
func parseDatabaseTarget(dsn string) dbTarget {
	if strings.Contains(dsn, "://") {
		if u, err := url.Parse(dsn); err == nil {
			return dbTarget{host: u.Hostname(), sslmode: u.Query().Get("sslmode")}
		}
	}

	var target dbTarget
	for _, field := range strings.Fields(dsn) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `'"`)
		switch strings.ToLower(key) {
		case "host":
			target.host = value
		case "sslmode":
			target.sslmode = value
		}
	}
	return target
}

// isLoopbackHost reports whether the host is local-only: a Unix socket path,
// localhost, or a loopback IP.
func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	if strings.HasPrefix(h, "/") {
		return true
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

func getduration(k string, d time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil {
			return parsed
		}
	}
	return d
}

func parseBoolEnv(k string, d bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(k))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return d
	}
}

// splitAddrs turns a comma-separated list of node URLs into a slice, trimming
// blanks so "a, b ," yields ["a","b"]. Returns nil when the input is empty.
func splitAddrs(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
