package search

import (
	"testing"
	"time"
)

func TestBreakerHonorsConsecutiveThreshold(t *testing.T) {
	cb := newCircuitBreaker(CircuitBreakerConfig{Threshold: 3, Cooldown: time.Second, FailureRateThreshold: .5, MinimumRequests: 10})
	cb.Failure()
	cb.Failure()
	if cb.State() != "closed" {
		t.Fatalf("breaker opened before threshold: %s", cb.State())
	}
	cb.Failure()
	if cb.State() != "open" {
		t.Fatalf("expected open breaker, got %s", cb.State())
	}
}

func TestBreakerAllowsOnlyConfiguredHalfOpenTrials(t *testing.T) {
	now := time.Unix(100, 0)
	cb := newCircuitBreaker(CircuitBreakerConfig{Threshold: 1, Cooldown: time.Second, MaxHalfOpenRequests: 1})
	cb.now = func() time.Time { return now }
	cb.Failure()
	now = now.Add(2 * time.Second)
	if !cb.Allow() {
		t.Fatal("expected one half-open trial")
	}
	if cb.Allow() {
		t.Fatal("second half-open trial should be blocked")
	}
	cb.Success()
	if cb.State() != "closed" {
		t.Fatalf("successful probe should close breaker, got %s", cb.State())
	}
}
