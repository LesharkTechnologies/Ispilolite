// Package middleware provides HTTP middleware for the Ispilo Lite API.
package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

// CORS returns a middleware that handles Cross-Origin Resource Sharing.
//
// In production, allowedOrigins should be your Flutter web domain(s), not "*".
// When allowedOrigins is empty, CORS is effectively disabled (no headers set).
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				allowed := false
				if len(allowedOrigins) == 0 {
					// Open for development/all origins when not explicitly constrained
					allowed = true
				} else {
					for _, o := range allowedOrigins {
						if o == "*" || o == origin || strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") {
							allowed = true
							break
						}
					}
				}

				if allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Idempotency-Key, X-Request-Id, Origin, X-Requested-With")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Max-Age", "86400")
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders returns middleware that sets defensive HTTP headers.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'self'")
			next.ServeHTTP(w, r)
		})
	}
}

// MetricsGuard protects the Prometheus scrape endpoint.
//
// The /metrics endpoint exposes process, database, queue, and business
// internals, so it must not be world-readable in production. Behaviour:
//
//   - token set: require "Authorization: Bearer <token>" or "X-Metrics-Token".
//   - token empty, production: return 404 so the endpoint is not discoverable.
//   - token empty, non-production: allow (local development and tests).
//
// This is defence in depth: deployments should still bind the endpoint to the
// monitoring network or put it behind an Nginx allowlist.
func MetricsGuard(token, environment string) func(http.Handler) http.Handler {
	production := strings.EqualFold(strings.TrimSpace(environment), "production")
	token = strings.TrimSpace(token)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				if production {
					http.NotFound(w, r)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			provided := strings.TrimSpace(r.Header.Get("X-Metrics-Token"))
			if provided == "" {
				if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
					provided = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
				}
			}
			// Constant-time comparison avoids leaking the token through timing.
			if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// StrictTransportSecurity returns middleware that sets HSTS. Only use behind TLS.
func StrictTransportSecurity(maxAge time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Strict-Transport-Security", "max-age="+formatDuration(maxAge)+"; includeSubDomains; preload")
			next.ServeHTTP(w, r)
		})
	}
}

func formatDuration(d time.Duration) string {
	return formatInt64(int64(d.Seconds()))
}

func formatInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
