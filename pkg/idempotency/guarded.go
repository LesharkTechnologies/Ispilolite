package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	goredis "github.com/go-redis/redis/v8"
	"ispilolite/pkg/database"
)

// GuardedStore provides atomic, per-user request replay. Unlike Middleware
// in idempotency.go, concurrent requests cannot execute the handler twice.
type GuardedStore struct{ redis *goredis.Client }

func NewGuardedStore() (*GuardedStore, error) {
	client, err := database.MustRedis()
	if err != nil {
		return nil, err
	}
	return &GuardedStore{redis: client}, nil
}

type guardedRecord struct {
	Fingerprint string `json:"fingerprint"`
	Complete    bool   `json:"complete"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
}

var replaceIfOwned = goredis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
return 1`)
var deleteIfOwned = goredis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('DEL', KEYS[1])
return 1`)

// Middleware requires a key for opt-in replay; requests without a key retain
// their existing behavior. Install it INSIDE auth middleware, after userID is
// set in the context, never on an unauthenticated route.
func (s *GuardedStore) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		userID, _ := r.Context().Value("userID").(string)
		if userID == "" || len(key) > 128 || s == nil || s.redis == nil {
			http.Error(w, "invalid idempotency key or user", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
		if err != nil || len(body) > 1<<20 {
			http.Error(w, "request body exceeds limit", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		fingerprint := hashParts(r.Method, r.URL.RequestURI(), string(body))
		cacheKey := "idempotency:safe:" + hashParts(userID, r.Method, r.URL.RequestURI(), key)
		pending, _ := json.Marshal(guardedRecord{Fingerprint: fingerprint})
		claimed, err := s.redis.SetNX(r.Context(), cacheKey, pending, 2*time.Minute).Result()
		if err != nil {
			http.Error(w, "idempotency storage unavailable", http.StatusServiceUnavailable)
			return
		}
		if !claimed {
			cached, err := s.redis.Get(r.Context(), cacheKey).Bytes()
			if err != nil {
				http.Error(w, "idempotency storage unavailable; retry", http.StatusServiceUnavailable)
				return
			}
			var rec guardedRecord
			if json.Unmarshal(cached, &rec) != nil || rec.Fingerprint != fingerprint {
				http.Error(w, "idempotency key already used with a different request", http.StatusConflict)
				return
			}
			if !rec.Complete {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "request already in progress", http.StatusConflict)
				return
			}
			w.Header().Set("Idempotency-Replayed", "true")
			w.Header().Set("Content-Type", rec.ContentType)
			w.WriteHeader(rec.Status)
			_, _ = w.Write(rec.Body)
			return
		}

		// Hold the response until the result has been recorded. A retry cannot
		// execute between a successful handler return and the cache write.
		buffer := &bufferedResponse{header: make(http.Header), status: http.StatusOK}
		next.ServeHTTP(buffer, r)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if buffer.status >= 200 && buffer.status < 300 {
			complete, _ := json.Marshal(guardedRecord{Fingerprint: fingerprint, Complete: true, Status: buffer.status, ContentType: buffer.header.Get("Content-Type"), Body: buffer.body.Bytes()})
			ok, err := replaceIfOwned.Run(ctx, s.redis, []string{cacheKey}, string(pending), string(complete), int64((24*time.Hour)/time.Millisecond)).Int()
			if err != nil || ok != 1 {
				log.Printf("idempotency response could not be persisted for %s: %v", cacheKey, err)
				http.Error(w, "request completed but replay record unavailable; check before retrying", http.StatusServiceUnavailable)
				return
			}
		} else {
			_ = deleteIfOwned.Run(ctx, s.redis, []string{cacheKey}, string(pending)).Err()
		}
		for name, values := range buffer.header {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.WriteHeader(buffer.status)
		_, _ = w.Write(buffer.body.Bytes())
	})
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header         { return b.header }
func (b *bufferedResponse) WriteHeader(status int)      { b.status = status }
func (b *bufferedResponse) Write(p []byte) (int, error) { return b.body.Write(p) }

func hashParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = io.WriteString(h, part)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
