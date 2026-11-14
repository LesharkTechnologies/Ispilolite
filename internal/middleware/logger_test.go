package middleware

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestLoggerPreservesFlusher(t *testing.T) {
	handler := RequestLogger(log.New(io.Discard, "", 0))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("request logger hid http.Flusher")
			return
		}
		_, _ = w.Write([]byte("data: hello\n\n"))
		flusher.Flush()
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if !w.Flushed || w.Body.String() != "data: hello\n\n" {
		t.Fatalf("stream was not flushed: %+v", w)
	}
}
