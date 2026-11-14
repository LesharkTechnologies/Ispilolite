package monitoring

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsMiddlewarePreservesFlusher(t *testing.T) {
	handler := Middleware("api", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("metrics middleware hid http.Flusher")
			return
		}
		_, _ = w.Write([]byte("data: hello\n\n"))
		flusher.Flush()
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/123/stream", nil))
	if !w.Flushed || w.Body.String() != "data: hello\n\n" {
		t.Fatalf("stream was not flushed: %+v", w)
	}
}
