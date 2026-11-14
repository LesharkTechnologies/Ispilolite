package idempotency

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/go-redis/redis/v8"
)

func request(ctx context.Context, user, key, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/my/jobs", strings.NewReader(body))
	r.Header.Set("Idempotency-Key", key)
	return r.WithContext(context.WithValue(ctx, "userID", user))
}

func TestGuardedReplayAndScope(t *testing.T) {
	server := miniredis.RunT(t)
	store := &GuardedStore{redis: goredis.NewClient(&goredis.Options{Addr: server.Addr()})}
	defer store.redis.Close()
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	serve := func(user, key, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request(context.Background(), user, key, body))
		return w
	}
	first := serve("a", "key", `{"a":1}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first request: %d %s", first.Code, first.Body.String())
	}
	replayed := serve("a", "key", `{"a":1}`)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" || replayed.Body.String() != first.Body.String() || calls.Load() != 1 {
		t.Fatalf("replay failed: %d %s, handler calls=%d", replayed.Code, replayed.Body.String(), calls.Load())
	}
	if conflict := serve("a", "key", `{"a":2}`); conflict.Code != http.StatusConflict || calls.Load() != 1 {
		t.Fatalf("key reused with a different body: %d, handler calls=%d", conflict.Code, calls.Load())
	}
	if otherUser := serve("b", "key", `{"a":1}`); otherUser.Code != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("different user collided: %d, handler calls=%d", otherUser.Code, calls.Load())
	}
}

func TestGuardedConcurrentRequestRunsOnce(t *testing.T) {
	server := miniredis.RunT(t)
	store := &GuardedStore{redis: goredis.NewClient(&goredis.Options{Addr: server.Addr()})}
	defer store.redis.Close()
	entered := make(chan struct{})
	proceed := make(chan struct{})
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		close(entered)
		<-proceed
		w.WriteHeader(http.StatusCreated)
	}))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request(context.Background(), "a", "key", `{}`))
		done <- w
	}()
	<-entered
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request(context.Background(), "a", "key", `{}`))
	if w.Code != http.StatusConflict || calls.Load() != 1 {
		t.Fatalf("overlapping request ran handler again: %d, calls=%d", w.Code, calls.Load())
	}
	close(proceed)
	if first := <-done; first.Code != http.StatusCreated {
		t.Fatalf("first request: %d", first.Code)
	}
}
