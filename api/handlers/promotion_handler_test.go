package handlers

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestPromotionSchedule(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	starts, expires, err := promotionSchedule(now, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !starts.Equal(now) || !expires.Equal(now.AddDate(0, 0, 30)) {
		t.Fatalf("default schedule = %v %v", starts, expires)
	}
	if _, _, err := promotionSchedule(now, "", now.Add(-time.Hour).Format(time.RFC3339), 30); err == nil {
		t.Fatal("past expiry should be rejected")
	}
	if _, _, err := promotionSchedule(now, "not-a-time", "", 30); err == nil {
		t.Fatal("invalid start should be rejected")
	}
}

func TestPromotionDestination(t *testing.T) {
	kind, value, err := promotionDestination("", "", "/u/amina-2026-abc")
	if err != nil || kind != "internal" || value != "/u/amina-2026-abc" {
		t.Fatalf("internal default: %q %q %v", kind, value, err)
	}
	if _, _, err := promotionDestination("internal", "https://example.com", "/u/x"); err == nil {
		t.Fatal("external URL accepted as internal")
	}
	if _, _, err := promotionDestination("external", "javascript:alert(1)", "/u/x"); err == nil {
		t.Fatal("javascript URL accepted")
	}
	if kind, value, err := promotionDestination("external", "https://example.com/login?ref=1", "/u/x"); err != nil || kind != "external" || value == "" {
		t.Fatalf("external destination: %q %q %v", kind, value, err)
	}
}

func TestPromotionDateRange(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/my/promotions/abc/insights?from=2026-11-01&to=2026-11-15", nil)
	from, to, err := promotionDateRange(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := from.Format("2006-01-02"); got != "2026-11-01" {
		t.Fatalf("from = %s", got)
	}
	if got := to.Format("2006-01-02"); got != "2026-11-16" {
		t.Fatalf("to = %s", got)
	}
	bad := httptest.NewRequest("GET", "/api/v1/my/promotions/abc/insights?from=2026-11-16&to=2026-11-15", nil)
	if _, _, err := promotionDateRange(bad); err == nil {
		t.Fatal("reversed range accepted")
	}
}

func TestBotDetectionAndViewerKeys(t *testing.T) {
	if !isBotUserAgent("Googlebot/2.1") {
		t.Fatal("Googlebot not detected")
	}
	if isBotUserAgent("Mozilla/5.0 (Linux; Android 14; CUBOT X70) AppleWebKit/537.36") {
		t.Fatal("CUBOT device was mistaken for a bot")
	}
	t.Setenv("PROMOTION_VIEWER_SALT", "test-salt")
	first := promotionViewerKey("user-1", "1.2.3.4:55", "agent")
	second := promotionViewerKey("user-1", "5.6.7.8:55", "other-agent")
	if first != second {
		t.Fatal("signed-in viewer key should be based on the account")
	}
	anonymousA := promotionViewerKey("", "1.2.3.4:55", "agent")
	anonymousB := promotionViewerKey("", "1.2.3.4:55", "agent")
	if anonymousA != anonymousB {
		t.Fatal("anonymous viewer key should be stable")
	}
	if anonymousA == first {
		t.Fatal("anonymous and signed-in keys must not collide")
	}
}

func TestPathParamBeforeLast(t *testing.T) {
	got := pathParamBeforeLast("/api/v1/admin/promotions/promo-1/payments/payment-1/confirm")
	if got != "payment-1" {
		t.Fatalf("payment id = %q", got)
	}
}
