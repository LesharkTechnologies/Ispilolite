package search

import (
	"context"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

// BackendStatus describes the health of a single search backend.
type BackendStatus struct {
	Name      string      `json:"name"`       // "elasticsearch" | "postgres"
	Up        bool        `json:"up"`         // reachable right now
	LatencyMS int64       `json:"latency_ms"` // last successful ping latency
	Error     string      `json:"error,omitempty"`
	LastCheck time.Time   `json:"last_check"`
	Details   interface{} `json:"details,omitempty"` // backend-specific details
}

// HealthChecker polls backend health and caches the latest status.
type HealthChecker struct {
	mu       sync.RWMutex
	backends map[string]*BackendStatus
	es       *elasticsearch.Client
}

// NewHealthChecker creates a checker that pings the ES client (and, if
// supplied, a postgres ping function) on demand.
func NewHealthChecker(es *elasticsearch.Client) *HealthChecker {
	return &HealthChecker{
		backends: make(map[string]*BackendStatus),
		es:       es,
	}
}

// CheckES pings Elasticsearch and fetches cluster health (if reachable).
func (hc *HealthChecker) CheckES(ctx context.Context) BackendStatus {
	start := time.Now()
	bs := BackendStatus{Name: "elasticsearch", LastCheck: start}
	if hc.es == nil {
		bs.Up = false
		bs.Error = "elasticsearch client not configured"
		hc.set("elasticsearch", bs)
		return bs
	}
	// go-elasticsearch v8: Ping takes variadic options and returns (*Response, error).
	response, err := hc.es.Ping(hc.es.Ping.WithContext(ctx))
	if err != nil {
		bs.Up = false
		bs.Error = err.Error()
		bs.LatencyMS = int64(time.Since(start).Milliseconds())
		hc.set("elasticsearch", bs)
		return bs
	}
	defer response.Body.Close()
	if response.IsError() {
		bs.Up = false
		bs.Error = response.Status()
		bs.LatencyMS = int64(time.Since(start).Milliseconds())
		hc.set("elasticsearch", bs)
		return bs
	}
	bs.Up = true
	bs.LatencyMS = int64(time.Since(start).Milliseconds())
	bs.Details = map[string]string{"note": "ping ok"}
	hc.set("elasticsearch", bs)
	return bs
}

// CheckPostgres runs a supplied ping function and records Postgres health.
func (hc *HealthChecker) CheckPostgres(ctx context.Context, ping func(ctx context.Context) error) BackendStatus {
	start := time.Now()
	bs := BackendStatus{Name: "postgres", LastCheck: start}
	if ping == nil {
		bs.Up = false
		bs.Error = "postgres ping function not configured"
		hc.set("postgres", bs)
		return bs
	}
	if err := ping(ctx); err != nil {
		bs.Up = false
		bs.Error = err.Error()
		bs.LatencyMS = int64(time.Since(start).Milliseconds())
		hc.set("postgres", bs)
		return bs
	}
	bs.Up = true
	bs.LatencyMS = int64(time.Since(start).Milliseconds())
	hc.set("postgres", bs)
	return bs
}

// Status returns a snapshot of all known backend statuses.
func (hc *HealthChecker) Status() map[string]*BackendStatus {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	out := make(map[string]*BackendStatus, len(hc.backends))
	for k, v := range hc.backends {
		cp := *v
		out[k] = &cp
	}
	return out
}

// Overall reports whether any backend is healthy and lists degraded ones.
func (hc *HealthChecker) Overall() (anyUp bool, degraded []string) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	for _, v := range hc.backends {
		if v.Up {
			anyUp = true
		} else {
			degraded = append(degraded, v.Name)
		}
	}
	return
}

func (hc *HealthChecker) set(name string, bs BackendStatus) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.backends[name] = &bs
}

var _ interface {
	CheckES(context.Context) BackendStatus
} = (*HealthChecker)(nil)
