package search

import (
	"sync"
	"time"
)

// breakerState is the classic three-state circuit breaker.
type breakerState int

const (
	stateClosed   breakerState = iota // healthy: requests flow to ES
	stateOpen                         // tripped: skip ES, go straight to fallback
	stateHalfOpen                     // probing: allow one trial request
)

// CircuitBreakerConfig tunes a single circuit breaker.
type CircuitBreakerConfig struct {
	// Threshold consecutive failures before the breaker opens.
	Threshold int
	// Cooldown how long the breaker stays open before probing.
	Cooldown time.Duration
	// MaxHalfOpenRequests how many trial requests to allow in half-open state.
	MaxHalfOpenRequests int
	// FailureRateWindow how long to track failure rate for adaptive tripping.
	FailureRateWindow time.Duration
	// FailureRateThreshold trips the breaker when the failure rate exceeds
	// this ratio (0.0 to 1.0) within FailureRateWindow. 0 disables adaptive.
	FailureRateThreshold float64
	// MinimumRequests prevents one early failure from tripping the adaptive
	// rate breaker before enough traffic has been observed.
	MinimumRequests int
}

// DefaultCircuitBreakerConfig returns production-sane defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Threshold:            5,
		Cooldown:             15 * time.Second,
		MaxHalfOpenRequests:  1,
		FailureRateWindow:    1 * time.Minute,
		FailureRateThreshold: 0.5, // 50% failure rate trips adaptively
		MinimumRequests:      10,
	}
}

// circuitBreaker guards a single backend path. After `threshold` consecutive
// failures (or an adaptive failure-rate trip) it opens for `cooldown`, during
// which every call is routed straight to the fallback without touching ES.
// After the cooldown it allows a limited number of trial requests (half-open).
type circuitBreaker struct {
	mu        sync.Mutex
	state     breakerState
	failures  int
	threshold int
	cooldown  time.Duration
	openedAt  time.Time
	now       func() time.Time // injectable clock for tests

	// Half-open tracking.
	halfOpenAllowed int
	halfOpenUsed    int

	// Adaptive failure-rate tracking.
	failureEvents   []failureEvent // ring of recent outcomes
	window          time.Duration
	rateThreshold   float64
	minimumRequests int
}

// failureEvent records one call outcome with its timestamp.
type failureEvent struct {
	at time.Time
	ok bool
}

func newCircuitBreaker(cfg CircuitBreakerConfig) *circuitBreaker {
	if cfg.Threshold <= 0 {
		cfg.Threshold = 5
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 15 * time.Second
	}
	if cfg.MaxHalfOpenRequests <= 0 {
		cfg.MaxHalfOpenRequests = 1
	}
	if cfg.FailureRateWindow <= 0 {
		cfg.FailureRateWindow = time.Minute
	}
	if cfg.MinimumRequests <= 0 {
		cfg.MinimumRequests = 10
	}
	return &circuitBreaker{
		state:           stateClosed,
		threshold:       cfg.Threshold,
		cooldown:        cfg.Cooldown,
		now:             time.Now,
		halfOpenAllowed: cfg.MaxHalfOpenRequests,
		window:          cfg.FailureRateWindow,
		rateThreshold:   cfg.FailureRateThreshold,
		minimumRequests: cfg.MinimumRequests,
	}
}

// Allow reports whether a request may attempt the ES path right now.
func (cb *circuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := cb.now()
	cb.expireEvents(now)

	switch cb.state {
	case stateClosed:
		return true
	case stateOpen:
		if now.Sub(cb.openedAt) >= cb.cooldown {
			cb.state = stateHalfOpen
			cb.halfOpenUsed = 1
			return true // trial
		}
		return false
	case stateHalfOpen:
		// Allow a limited number of trial requests concurrently.
		if cb.halfOpenUsed < cb.halfOpenAllowed {
			cb.halfOpenUsed++
			return true
		}
		return false
	default:
		return true
	}
}

// Success records a successful ES call, potentially closing the breaker.
func (cb *circuitBreaker) Success() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := cb.now()
	cb.recordEvent(now, true)
	if cb.state == stateHalfOpen {
		if cb.halfOpenUsed > 0 {
			cb.halfOpenUsed--
		}
		cb.failures = 0
		cb.state = stateClosed
		return
	}
	cb.failures = 0
}

// Failure records a failed ES call, tripping the breaker at the threshold or
// when the adaptive failure-rate threshold is exceeded.
func (cb *circuitBreaker) Failure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := cb.now()
	cb.recordEvent(now, false)
	if cb.state == stateHalfOpen {
		if cb.halfOpenUsed > 0 {
			cb.halfOpenUsed--
		}
		cb.trip()
		return
	}
	cb.failures++
	if cb.failures >= cb.threshold {
		cb.trip()
		return
	}
	// Adaptive: trip if the recent failure rate exceeds the threshold.
	if cb.rateThreshold > 0 && len(cb.failureEvents) >= cb.minimumRequests && cb.failureRate() >= cb.rateThreshold {
		cb.trip()
	}
}

func (cb *circuitBreaker) trip() {
	cb.state = stateOpen
	cb.openedAt = cb.now()
	cb.failures = 0
}

// State returns a human-readable state for metrics/logging.
func (cb *circuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case stateOpen:
		return "open"
	case stateHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}

// FailureRate returns the observed failure rate (0.0 to 1.0) over the tracking
// window. Callers must not hold the mutex.
func (cb *circuitBreaker) FailureRate() float64 {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failureRate()
}

// FailureCount returns the consecutive failure count.
func (cb *circuitBreaker) FailureCount() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}

// recordEvent appends an outcome and trims events outside the window.
func (cb *circuitBreaker) recordEvent(at time.Time, ok bool) {
	cb.failureEvents = append(cb.failureEvents, failureEvent{at: at, ok: ok})
	cb.expireEvents(at)
}

// expireEvents removes events older than the window from the front of the slice.
func (cb *circuitBreaker) expireEvents(now time.Time) {
	cutoff := now.Add(-cb.window)
	i := 0
	for _, e := range cb.failureEvents {
		if e.at.After(cutoff) {
			cb.failureEvents[i] = e
			i++
		}
	}
	cb.failureEvents = cb.failureEvents[:i]
}

// failureRate computes the failure rate over the tracked window. Must be
// called with the mutex held.
func (cb *circuitBreaker) failureRate() float64 {
	if len(cb.failureEvents) == 0 {
		return 0
	}
	fails := 0
	for _, e := range cb.failureEvents {
		if !e.ok {
			fails++
		}
	}
	return float64(fails) / float64(len(cb.failureEvents))
}

// breakerRegistry holds one named breaker per search endpoint so a degraded
// ISP search doesn't take down technician or location search.
type breakerRegistry struct {
	mu    sync.RWMutex
	items map[string]*circuitBreaker
	cfg   CircuitBreakerConfig
}

func newBreakerRegistry(cfg CircuitBreakerConfig) *breakerRegistry {
	return &breakerRegistry{items: make(map[string]*circuitBreaker), cfg: cfg}
}

// Get returns the breaker for the named endpoint, creating it on first use.
func (br *breakerRegistry) Get(endpoint string) *circuitBreaker {
	br.mu.RLock()
	cb, ok := br.items[endpoint]
	br.mu.RUnlock()
	if ok {
		return cb
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	// Double-check after acquiring the write lock.
	if cb, ok := br.items[endpoint]; ok {
		return cb
	}
	cb = newCircuitBreaker(br.cfg)
	br.items[endpoint] = cb
	return cb
}

// States returns a snapshot of all breaker states.
func (br *breakerRegistry) States() map[string]string {
	br.mu.RLock()
	defer br.mu.RUnlock()
	out := make(map[string]string, len(br.items))
	for k, v := range br.items {
		out[k] = v.State()
	}
	return out
}
