package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"ispilolite/internal/models"
)

var (
	ErrInvalid   = errors.New("invalid payment request")
	ErrForbidden = errors.New("payment access denied")
	ErrNotFound  = errors.New("payment or escrow not found")
	ErrProvider  = errors.New("payment provider unavailable")
)

type InitiateRequest struct {
	Reference   string
	Phone       string
	Amount      float64
	Currency    string
	Description string
}

type InitiateResult struct {
	ExternalID string
	Status     string
	Raw        map[string]interface{}
}

type Provider interface {
	Name() string
	Initiate(context.Context, InitiateRequest) (*InitiateResult, error)
	ParseWebhook(http.Header, []byte) (*models.PaymentProviderEvent, error)
}

func verifyHMAC(secret string, payload []byte, signature string) bool {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(signature) == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	want := mac.Sum(nil)
	got, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(signature), "sha256="))
	return err == nil && hmac.Equal(want, got)
}
