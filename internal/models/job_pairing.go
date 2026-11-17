package models

import "time"

// JobPairingSession represents a cryptographically secured hand-shake
// session between a technician and a customer to confirm job completion
// and release escrow funds via dynamic QR scanning.
type JobPairingSession struct {
	ID               string     `json:"id" db:"id"`
	JobID            string     `json:"job_id" db:"job_id"`
	TechnicianID     string     `json:"technician_id" db:"technician_id"`
	CustomerID       string     `json:"customer_id" db:"customer_id"`
	Amount           float64    `json:"amount" db:"amount"`
	Currency         string     `json:"currency" db:"currency"`
	Nonce            string     `json:"nonce" db:"nonce"`
	VerificationCode string     `json:"verification_code" db:"verification_code"`
	Signature        string     `json:"signature" db:"signature"`
	QRPayload        string     `json:"qr_payload" db:"qr_payload"`
	DeepLink         string     `json:"deep_link" db:"deep_link"`
	Status           string     `json:"status" db:"status"` // pending, confirmed, expired, cancelled
	ExpiresAt        time.Time  `json:"expires_at" db:"expires_at"`
	ConfirmedAt      *time.Time `json:"confirmed_at,omitempty" db:"confirmed_at"`
	ConfirmedBy      string     `json:"confirmed_by,omitempty" db:"confirmed_by"`
	Notes            string     `json:"notes,omitempty" db:"notes"`
	Rating           *int       `json:"rating,omitempty" db:"rating"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}
