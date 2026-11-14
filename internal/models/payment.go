package models

import "time"

type PaymentIntent struct {
	ID             string                 `json:"id"`
	JobRequestID   string                 `json:"job_request_id"`
	PayerID        string                 `json:"payer_id"`
	PayeeID        string                 `json:"payee_id"`
	Provider       string                 `json:"provider"`
	ExternalID     string                 `json:"external_id,omitempty"`
	Phone          string                 `json:"phone"`
	Currency       string                 `json:"currency"`
	Amount         float64                `json:"amount"`
	Status         string                 `json:"status"`
	IdempotencyKey string                 `json:"-"`
	FailureReason  string                 `json:"failure_reason,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	Milestones     []*EscrowMilestone     `json:"milestones,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type Escrow struct {
	ID           string             `json:"id"`
	PaymentID    string             `json:"payment_id"`
	JobRequestID string             `json:"job_request_id"`
	PayerID      string             `json:"payer_id"`
	PayeeID      string             `json:"payee_id"`
	Currency     string             `json:"currency"`
	Amount       float64            `json:"amount"`
	Status       string             `json:"status"`
	HeldAt       *time.Time         `json:"held_at,omitempty"`
	ReleasedAt   *time.Time         `json:"released_at,omitempty"`
	RefundedAt   *time.Time         `json:"refunded_at,omitempty"`
	Milestones   []*EscrowMilestone `json:"milestones,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type EscrowMilestone struct {
	ID         string     `json:"id"`
	PaymentID  string     `json:"payment_id"`
	EscrowID   string     `json:"escrow_id,omitempty"`
	Title      string     `json:"title"`
	Amount     float64    `json:"amount"`
	Sequence   int        `json:"sequence"`
	Status     string     `json:"status"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type PaymentProviderEvent struct {
	EventID       string                 `json:"event_id"`
	ExternalID    string                 `json:"external_id"`
	Status        string                 `json:"status"`
	Amount        float64                `json:"amount,omitempty"`
	Receipt       string                 `json:"receipt,omitempty"`
	FailureReason string                 `json:"failure_reason,omitempty"`
	Raw           map[string]interface{} `json:"-"`
}
