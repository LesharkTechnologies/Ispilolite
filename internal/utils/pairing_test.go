package utils

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSignAndVerifyJobPairing(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-long-1234567")
	jobID := "job-12345"
	custID := "cust-67890"
	techID := "tech-11223"
	amount := 2500.00
	currency := "KES"
	nonce := "nonce-998877"
	expiresAt := time.Now().UTC().Add(10 * time.Minute).Unix()

	sig := SignJobPairing(secret, jobID, custID, techID, amount, currency, nonce, expiresAt)
	if !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("expected signature prefix 'sha256=', got: %s", sig)
	}

	// 1. Valid verification
	valid := VerifyJobPairing(secret, jobID, custID, techID, amount, currency, nonce, expiresAt, sig)
	if !valid {
		t.Fatalf("expected valid signature to verify successfully")
	}

	// 2. Tampered amount
	if VerifyJobPairing(secret, jobID, custID, techID, 500.00, currency, nonce, expiresAt, sig) {
		t.Fatalf("expected verification to fail for tampered amount")
	}

	// 3. Tampered technician ID
	if VerifyJobPairing(secret, jobID, custID, "other-tech", amount, currency, nonce, expiresAt, sig) {
		t.Fatalf("expected verification to fail for wrong technician")
	}

	// 4. Tampered customer ID
	if VerifyJobPairing(secret, jobID, "other-cust", techID, amount, currency, nonce, expiresAt, sig) {
		t.Fatalf("expected verification to fail for wrong customer")
	}

	// 5. Expired timestamp
	expiredAt := time.Now().UTC().Add(-5 * time.Minute).Unix()
	expiredSig := SignJobPairing(secret, jobID, custID, techID, amount, currency, nonce, expiredAt)
	if VerifyJobPairing(secret, jobID, custID, techID, amount, currency, nonce, expiredAt, expiredSig) {
		t.Fatalf("expected verification to fail for expired signature")
	}

	// 6. Wrong secret
	wrongSecret := []byte("wrong-secret-key-00000000000000000")
	if VerifyJobPairing(wrongSecret, jobID, custID, techID, amount, currency, nonce, expiresAt, sig) {
		t.Fatalf("expected verification to fail for wrong secret")
	}
}

func TestGenerateVerificationCode(t *testing.T) {
	code := GenerateVerificationCode()
	if len(code) != 6 {
		t.Fatalf("expected 6-digit code, got length %d: %s", len(code), code)
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Fatalf("expected only digits in code, got: %s", code)
		}
	}
}

func TestFormatDeepLinkAndQRPayload(t *testing.T) {
	jobID := "job-100"
	nonce := "nonce-200"
	techID := "tech-300"
	custID := "cust-400"
	code := "543210"
	sig := "sha256=abcdef123456"
	amount := 3500.50
	currency := "KES"
	expiresAt := time.Now().UTC().Add(10 * time.Minute).Unix()

	link := FormatDeepLink(jobID, nonce, techID, custID, code, sig, amount, currency, expiresAt)
	if !strings.HasPrefix(link, "ispilolite://jobs/job-100/pair?") {
		t.Fatalf("unexpected deep link format: %s", link)
	}
	if !strings.Contains(link, "pairing_id=nonce-200") || !strings.Contains(link, "sig=sha256%3Dabcdef123456") {
		t.Fatalf("deep link missing parameters: %s", link)
	}

	qrJSON := FormatQRPayloadJSON(jobID, nonce, techID, custID, code, sig, amount, currency, expiresAt)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(qrJSON), &parsed); err != nil {
		t.Fatalf("failed to parse qr json: %v", err)
	}
	if parsed["action"] != "job_pairing" || parsed["pairing_id"] != "nonce-200" || parsed["amount"] != 3500.50 {
		t.Fatalf("qr json parsed data mismatch: %+v", parsed)
	}
}
