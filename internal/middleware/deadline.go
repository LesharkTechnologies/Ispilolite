package middleware

import (
	"net/http"
	"strings"
	"time"
)

// WriteDeadline gives ordinary responses a bounded write window while
// permitting authenticated /stream endpoints to remain open for SSE. Apply
// this outside Gin, before any ResponseWriter wrappers are installed.
func WriteDeadline(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if timeout > 0 && !(r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/stream")) {
				_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout))
			}
			next.ServeHTTP(w, r)
		})
	}
}
