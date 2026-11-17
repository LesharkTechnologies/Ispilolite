package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultPairingSecret is a fallback key used for local dev/testing when no env secret is set.
var DefaultPairingSecret = []byte("ispilolite-qr-pairing-hmac-sha256-secret-key-32bytes!")

// GetPairingSecret loads the signing secret from environment, falling back safely.
func GetPairingSecret() []byte {
	if s := os.Getenv("PAIRING_SECRET"); strings.TrimSpace(s) != "" {
		return []byte(strings.TrimSpace(s))
	}
	if s := os.Getenv("JWT_SECRET"); strings.TrimSpace(s) != "" {
		return []byte(strings.TrimSpace(s))
	}
	if s := os.Getenv("PAYMENT_WEBHOOK_SECRET"); strings.TrimSpace(s) != "" {
		return []byte(strings.TrimSpace(s))
	}
	return DefaultPairingSecret
}

// CanonicalPairingPayload builds the deterministic string representation of the pairing parameters.
func CanonicalPairingPayload(jobID, customerID, technicianID string, amount float64, currency, nonce string, expiresAt int64) string {
	return fmt.Sprintf("job_id=%s|customer_id=%s|technician_id=%s|amount=%.2f|currency=%s|nonce=%s|expires_at=%d",
		strings.TrimSpace(jobID),
		strings.TrimSpace(customerID),
		strings.TrimSpace(technicianID),
		amount,
		strings.ToUpper(strings.TrimSpace(currency)),
		strings.TrimSpace(nonce),
		expiresAt,
	)
}

// SignJobPairing creates an HMAC-SHA256 signature for pairing verification.
func SignJobPairing(secret []byte, jobID, customerID, technicianID string, amount float64, currency, nonce string, expiresAt int64) string {
	if len(secret) == 0 {
		secret = GetPairingSecret()
	}
	payload := CanonicalPairingPayload(jobID, customerID, technicianID, amount, currency, nonce, expiresAt)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyJobPairing checks if the HMAC-SHA256 signature matches and whether it is unexpired.
func VerifyJobPairing(secret []byte, jobID, customerID, technicianID string, amount float64, currency, nonce string, expiresAt int64, signature string) bool {
	if len(secret) == 0 {
		secret = GetPairingSecret()
	}
	if strings.TrimSpace(signature) == "" || strings.TrimSpace(nonce) == "" {
		return false
	}
	// Check if already expired
	if time.Now().UTC().Unix() > expiresAt {
		return false
	}

	expected := SignJobPairing(secret, jobID, customerID, technicianID, amount, currency, nonce, expiresAt)
	expectedClean := strings.TrimPrefix(strings.TrimSpace(expected), "sha256=")
	gotClean := strings.TrimPrefix(strings.TrimSpace(signature), "sha256=")

	expectedBytes, err1 := hex.DecodeString(expectedClean)
	gotBytes, err2 := hex.DecodeString(gotClean)
	if err1 != nil || err2 != nil {
		return false
	}
	return hmac.Equal(expectedBytes, gotBytes)
}

// GenerateVerificationCode generates a random 6-digit decimal fallback code for manual entry.
func GenerateVerificationCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "123456"
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// FormatDeepLink formats the custom URI scheme for app pairing (e.g., ispilolite://jobs/{id}/pair?...).
func FormatDeepLink(jobID, nonce, techID, customerID, code, signature string, amount float64, currency string, expiresAt int64) string {
	v := url.Values{}
	v.Set("pairing_id", nonce)
	v.Set("tech_id", techID)
	v.Set("customer_id", customerID)
	v.Set("amount", fmt.Sprintf("%.2f", amount))
	v.Set("currency", currency)
	if code != "" {
		v.Set("code", code)
	}
	v.Set("exp", strconv.FormatInt(expiresAt, 10))
	v.Set("sig", signature)
	return fmt.Sprintf("ispilolite://jobs/%s/pair?%s", jobID, v.Encode())
}

// FormatQRPayloadJSON formats the JSON representation of the pairing QR payload.
func FormatQRPayloadJSON(jobID, nonce, techID, customerID, code, signature string, amount float64, currency string, expiresAt int64) string {
	m := map[string]interface{}{
		"app":               "ispilolite",
		"action":            "job_pairing",
		"job_id":            jobID,
		"pairing_id":        nonce,
		"technician_id":     techID,
		"customer_id":       customerID,
		"amount":            amount,
		"currency":          currency,
		"verification_code": code,
		"expires_at":        expiresAt,
		"signature":         signature,
	}
	bytes, _ := json.Marshal(m)
	return string(bytes)
}
