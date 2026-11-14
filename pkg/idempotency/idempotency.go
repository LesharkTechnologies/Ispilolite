// Package idempotency provides idempotency-key support for state-changing
// endpoints. Clients send an Idempotency-Key header; the server records the
// response and replays it on subsequent requests with the same key within the
// TTL window. This prevents duplicate charges, job creations, or quotation
// finalizations when a client retries after a network timeout.
//
// Keys are stored in Redis with a TTL. The response body is stored as-is so
// replays are byte-for-byte identical.
package idempotency

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"ispilolite/pkg/database"
)

const defaultTTL = 24 * time.Hour

// Store holds idempotency records in Redis.
type Store struct {
	client *database.RedisClientWrapper
	ttl    time.Duration
}

// RedisStore is an alias for Store for explicit naming.
type RedisStore = Store

// NewStore creates a new idempotency store backed by Redis.
func NewStore() (*Store, error) {
	client, err := database.MustRedis()
	if err != nil {
		return nil, err
	}
	return &Store{
		client: database.NewRedisClientWrapper(client),
		ttl:    defaultTTL,
	}, nil
}

// Middleware returns an http.Handler that enforces idempotency for the
// wrapped handler. The key is read from the Idempotency-Key header. Missing
// keys pass through unchecked (callers can require them in handlers).
func (s *Store) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}

		cacheKey := "idempotency:" + key

		// Check for a previously recorded response.
		val, err := s.client.Get(context.Background(), cacheKey)
		if err == nil && val != "" {
			var rec record
			if json.Unmarshal([]byte(val), &rec) == nil {
				w.Header().Set("Idempotency-Replayed", "true")
				w.Header().Set("Content-Type", rec.ContentType)
				w.WriteHeader(rec.StatusCode)
				_, _ = w.Write(rec.Body)
				return
			}
		}

		// Capture the response from the downstream handler.
		cap := &responseCapture{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(cap, r)

		// Only cache successful, idempotent-safe responses (2xx).
		if cap.statusCode >= 200 && cap.statusCode < 300 && cap.body != nil {
			rec := record{
				StatusCode:  cap.statusCode,
				Body:        cap.body,
				ContentType: cap.Header().Get("Content-Type"),
			}
			data, _ := json.Marshal(rec)
			_ = s.client.Set(context.Background(), cacheKey, data, s.ttl)
		}
	})
}

type responseCapture struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.statusCode = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	rc.body = append(rc.body, b...)
	return rc.ResponseWriter.Write(b)
}

type record struct {
	StatusCode  int    `json:"status_code"`
	Body        []byte `json:"body"`
	ContentType string `json:"content_type"`
}

// RequireIdempotencyKey returns middleware that rejects requests without an
// Idempotency-Key header. Use on state-changing endpoints that must not be
// retried blindly.
func RequireIdempotencyKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "idempotency key is required",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
