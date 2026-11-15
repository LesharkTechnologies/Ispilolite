package models

import (
	"testing"
	"time"
)

func TestEffectivePromotionStatus(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		stored   string
		startsAt time.Time
		expires  time.Time
		want     string
	}{
		{"active", PromotionActive, now.Add(-time.Hour), now.Add(time.Hour), PromotionActive},
		{"scheduled", PromotionScheduled, now.Add(time.Hour), now.Add(2 * time.Hour), PromotionScheduled},
		{"expired", PromotionActive, now.Add(-2 * time.Hour), now.Add(-time.Hour), PromotionExpired},
		{"pending approval", PromotionPendingApproval, now, now.Add(time.Hour), PromotionPendingApproval},
		{"pending payment", PromotionPendingPayment, now, now.Add(time.Hour), PromotionPendingPayment},
		{"rejected", PromotionRejected, now, now.Add(time.Hour), PromotionRejected},
		{"suspended", PromotionSuspended, now.Add(-time.Hour), now.Add(time.Hour), PromotionSuspended},
		{"cancelled", PromotionCancelled, now.Add(-time.Hour), now.Add(time.Hour), PromotionCancelled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectivePromotionStatus(tc.stored, tc.startsAt, tc.expires, now); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPromotionTimeRemaining(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	if _, label := PromotionTimeRemaining(PromotionActive, now.Add(-time.Hour), now.Add(72*time.Hour), now); label != "3 days remaining" {
		t.Fatalf("active label = %q", label)
	}
	if _, label := PromotionTimeRemaining(PromotionExpired, now.Add(-4*24*time.Hour), now.Add(-2*24*time.Hour), now); label != "Expired 2 days ago" {
		t.Fatalf("expired label = %q", label)
	}
	if _, label := PromotionTimeRemaining(PromotionScheduled, now.Add(2*time.Hour), now.Add(3*time.Hour), now); label != "Starts in 2 hours" {
		t.Fatalf("scheduled label = %q", label)
	}
	if _, label := PromotionTimeRemaining(PromotionPendingPayment, now, now.Add(time.Hour), now); label != "Awaiting payment" {
		t.Fatalf("pending label = %q", label)
	}
	if _, label := PromotionTimeRemaining(PromotionPendingApproval, now, now.Add(time.Hour), now); label != "Awaiting admin approval" {
		t.Fatalf("approval label = %q", label)
	}
	if _, label := PromotionTimeRemaining(PromotionRejected, now, now.Add(time.Hour), now); label != "Rejected" {
		t.Fatalf("rejected label = %q", label)
	}
}
