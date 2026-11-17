package models

import (
	"testing"
)

func TestNormalizeVerificationBadge(t *testing.T) {
	tests := []struct {
		input       string
		expected    string
		expectError bool
	}{
		{"id_verified", VerificationBadgeIDVerified, false},
		{"ID verified", VerificationBadgeIDVerified, false},
		{"ID Verified", VerificationBadgeIDVerified, false},
		{"id", VerificationBadgeIDVerified, false},
		{"ID", VerificationBadgeIDVerified, false},
		{"business_verified", VerificationBadgeBusinessVerified, false},
		{"business verified", VerificationBadgeBusinessVerified, false},
		{"Business Verified", VerificationBadgeBusinessVerified, false},
		{"business", VerificationBadgeBusinessVerified, false},
		{"both", VerificationBadgeBoth, false},
		{"Both", VerificationBadgeBoth, false},
		{"id_and_business", VerificationBadgeBoth, false},
		{"rejected", VerificationBadgeRejected, false},
		{"unverified", VerificationBadgeUnverified, false},
		{"invalid_status", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NormalizeVerificationBadge(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for input %q: %v", tt.input, err)
				}
				if got != tt.expected {
					t.Fatalf("expected %q, got %q", tt.expected, got)
				}
			}
		})
	}
}

func TestFormatVerificationBadge(t *testing.T) {
	if FormatVerificationBadge(VerificationBadgeIDVerified) != "ID verified" {
		t.Errorf("expected 'ID verified', got %q", FormatVerificationBadge(VerificationBadgeIDVerified))
	}
	if FormatVerificationBadge(VerificationBadgeBusinessVerified) != "business verified" {
		t.Errorf("expected 'business verified', got %q", FormatVerificationBadge(VerificationBadgeBusinessVerified))
	}
	if FormatVerificationBadge(VerificationBadgeBoth) != "both" {
		t.Errorf("expected 'both', got %q", FormatVerificationBadge(VerificationBadgeBoth))
	}
	if FormatVerificationBadge(VerificationBadgeRejected) != "rejected" {
		t.Errorf("expected 'rejected', got %q", FormatVerificationBadge(VerificationBadgeRejected))
	}
	if FormatVerificationBadge(VerificationStatusPending) != "pending" {
		t.Errorf("expected 'pending', got %q", FormatVerificationBadge(VerificationStatusPending))
	}
	if FormatVerificationBadge(VerificationBadgeUnverified) != "unverified" {
		t.Errorf("expected 'unverified', got %q", FormatVerificationBadge(VerificationBadgeUnverified))
	}
}
