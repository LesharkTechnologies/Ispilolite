package search

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// SearchMetrics tracks per-endpoint search latency, fallback rates, and
// breaker state transitions. It wraps the existing package-level
// monitoring.SearchRequests counter with finer-grained histograms.
type SearchMetrics struct {
	mu sync.Mutex

	// Prometheus histograms (registered once via init).
	latency       *prometheus.HistogramVec
	fallbackTotal *prometheus.CounterVec
	breakerState  *prometheus.GaugeVec
	queriesTotal  *prometheus.CounterVec
	zeroResults   *prometheus.CounterVec

	// In-memory rolling counters for the health endpoint.
	latencyByEndpoint   map[string][]float64
	fallbacksByEndpoint map[string]int
	requestsByEndpoint  map[string]int
	maxLatencySamples   int
}

var (
	searchMetrics     *SearchMetrics
	searchMetricsOnce sync.Once
)

// SearchMetricsSingleton returns the global metrics instance.
func SearchMetricsSingleton() *SearchMetrics {
	searchMetricsOnce.Do(func() {
		searchMetrics = newSearchMetrics()
	})
	return searchMetrics
}

func newSearchMetrics() *SearchMetrics {
	latency := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ispilo_search_latency_seconds",
			Help:    "Search request latency by backend and endpoint.",
			Buckets: []float64{.005, .01, .025, .05, .1, .2, .5, 1, 2, 5},
		},
		[]string{"backend", "endpoint"},
	)
	fallbackTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ispilo_search_fallbacks_total",
			Help: "Number of search requests that fell back from ES to Postgres.",
		},
		[]string{"endpoint", "reason"},
	)
	breakerState := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ispilo_search_breaker_state",
			Help: "Circuit breaker state per endpoint (0=closed,1=half_open,2=open).",
		},
		[]string{"endpoint"},
	)
	queriesTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ispilo_search_queries_total",
			Help: "Total search queries by endpoint and source.",
		},
		[]string{"endpoint", "source"},
	)
	zeroResults := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ispilo_search_zero_results_total",
			Help: "Searches that returned zero results, by endpoint and source.",
		},
		[]string{"endpoint", "source"},
	)

	prometheus.MustRegister(latency, fallbackTotal, breakerState, queriesTotal, zeroResults)

	return &SearchMetrics{
		latency:             latency,
		fallbackTotal:       fallbackTotal,
		breakerState:        breakerState,
		queriesTotal:        queriesTotal,
		zeroResults:         zeroResults,
		latencyByEndpoint:   make(map[string][]float64),
		fallbacksByEndpoint: make(map[string]int),
		requestsByEndpoint:  make(map[string]int),
		maxLatencySamples:   100,
	}
}

// ObserveLatency records a search latency sample.
func (m *SearchMetrics) ObserveLatency(backend, endpoint string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latency.WithLabelValues(backend, endpoint).Observe(d.Seconds())
	samples := m.latencyByEndpoint[endpoint]
	samples = append(samples, float64(d.Milliseconds()))
	if len(samples) > m.maxLatencySamples {
		samples = samples[1:]
	}
	m.latencyByEndpoint[endpoint] = samples
	m.requestsByEndpoint[endpoint]++
}

// ObserveFallback records that a request fell back from ES to Postgres.
func (m *SearchMetrics) ObserveFallback(endpoint, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallbackTotal.WithLabelValues(endpoint, reason).Inc()
	m.fallbacksByEndpoint[endpoint]++
}

// ObserveBreakerState records the current breaker state for an endpoint.
// state is "closed", "half_open", or "open".
func (m *SearchMetrics) ObserveBreakerState(endpoint, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var g float64
	switch state {
	case "closed":
		g = 0
	case "half_open":
		g = 1
	case "open":
		g = 2
	}
	m.breakerState.WithLabelValues(endpoint).Set(g)
}

// ObserveQuery records a query by endpoint and source.
func (m *SearchMetrics) ObserveQuery(endpoint, source string, zero bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queriesTotal.WithLabelValues(endpoint, source).Inc()
	if zero {
		m.zeroResults.WithLabelValues(endpoint, source).Inc()
	}
}

// Snapshot returns a summary of in-memory metrics for the health endpoint.
func (m *SearchMetrics) Snapshot() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]interface{}, len(m.latencyByEndpoint))
	for ep, samples := range m.latencyByEndpoint {
		if len(samples) == 0 {
			continue
		}
		var sum float64
		var max float64
		for _, s := range samples {
			sum += s
			if s > max {
				max = s
			}
		}
		avg := sum / float64(len(samples))
		out[ep] = map[string]interface{}{
			"requests":       m.requestsByEndpoint[ep],
			"fallbacks":      m.fallbacksByEndpoint[ep],
			"avg_latency_ms": avg,
			"max_latency_ms": max,
			"samples":        len(samples),
		}
	}
	return out
}

// Package-level helpers using the global metrics.

func ObserveLatency(backend, endpoint string, d time.Duration) {
	SearchMetricsSingleton().ObserveLatency(backend, endpoint, d)
}

func ObserveFallback(endpoint, reason string) {
	SearchMetricsSingleton().ObserveFallback(endpoint, reason)
}

func ObserveBreakerState(endpoint, state string) {
	SearchMetricsSingleton().ObserveBreakerState(endpoint, state)
}

func ObserveQuery(endpoint, source string, zero bool) {
	SearchMetricsSingleton().ObserveQuery(endpoint, source, zero)
}

func Snapshot() map[string]interface{} {
	return SearchMetricsSingleton().Snapshot()
}
