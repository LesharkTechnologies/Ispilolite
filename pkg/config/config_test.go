package config

import (
	"strings"
	"testing"
)

func setProductionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("JWT_SECRET", "test-secret-value")
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "ci-payment-webhook-secret")
	t.Setenv("DATABASE_URL", "postgres://user:pass@127.0.0.1:5432/ispilolite?sslmode=disable")
	t.Setenv("DATABASE_READ_URL", "")
	t.Setenv("DB_URL", "")
	t.Setenv("DB_READ_URL", "")
	t.Setenv("DB_WRITER_HOST", "")
	t.Setenv("DB_READER_HOST", "")
	t.Setenv("DB_SSL_MODE", "")
	t.Setenv("REDIS_URL", "redis://127.0.0.1:6379/0")
	t.Setenv("ES_ENABLED", "false")
	t.Setenv("ES_ADDRESSES", "")
}

func TestLoadAllowsPlaintextLoopbackInProduction(t *testing.T) {
	setProductionEnv(t)
	if _, err := Load(); err != nil {
		t.Fatalf("loopback database rejected in production: %v", err)
	}
}

func TestLoadRejectsMissingPaymentWebhookSecretInProduction(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PAYMENT_WEBHOOK_SECRET") {
		t.Fatalf("missing payment webhook secret should block production startup: %v", err)
	}
}

func TestLoadRejectsPlaintextRemoteInProduction(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("DATABASE_URL", "postgres://user:pass@db.example.com:5432/ispilolite?sslmode=disable")

	_, err := Load()
	if err == nil {
		t.Fatal("remote plaintext database accepted in production")
	}
	if !strings.Contains(err.Error(), "sslmode=disable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAllowsPlaintextRemoteInDevelopment(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DATABASE_URL", "postgres://user:pass@db.example.com:5432/ispilolite?sslmode=disable")

	if _, err := Load(); err != nil {
		t.Fatalf("development startup rejected: %v", err)
	}
}

func TestLoadRejectsPlaintextRemoteKeywordDSN(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_READ_URL", "")
	t.Setenv("DB_WRITER_HOST", "10.0.0.5")
	t.Setenv("DB_SSL_MODE", "disable")

	_, err := Load()
	if err == nil {
		t.Fatal("remote plaintext keyword DSN accepted in production")
	}
	if !strings.Contains(err.Error(), "10.0.0.5") {
		t.Fatalf("error does not identify the host: %v", err)
	}
}

func TestParseDatabaseTarget(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		host    string
		sslmode string
	}{
		{
			name:    "url dsn",
			dsn:     "postgres://user:pass@127.0.0.1:5432/db?sslmode=require",
			host:    "127.0.0.1",
			sslmode: "require",
		},
		{
			name:    "url dsn without sslmode",
			dsn:     "postgres://user:pass@db.example.com:5432/db",
			host:    "db.example.com",
			sslmode: "",
		},
		{
			name:    "keyword dsn",
			dsn:     "host=db.example.com port=5432 user=u sslmode=disable",
			host:    "db.example.com",
			sslmode: "disable",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDatabaseTarget(tc.dsn)
			if got.host != tc.host || got.sslmode != tc.sslmode {
				t.Fatalf("got %+v, want host=%q sslmode=%q", got, tc.host, tc.sslmode)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	loopback := []string{"127.0.0.1", "127.0.1.1", "::1", "[::1]", "localhost", "LOCALHOST", "/var/run/postgresql"}
	for _, host := range loopback {
		if !isLoopbackHost(host) {
			t.Errorf("%q should be treated as loopback", host)
		}
	}

	remote := []string{"", "10.0.0.5", "db.example.com", "203.0.113.9"}
	for _, host := range remote {
		if isLoopbackHost(host) {
			t.Errorf("%q should not be treated as loopback", host)
		}
	}
}
