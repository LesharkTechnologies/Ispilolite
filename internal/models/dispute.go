package models

import "time"

type Dispute struct {
	ID               string     `json:"id"`
	JobRequestID     string     `json:"job_request_id"`
	EscrowID         string     `json:"escrow_id,omitempty"`
	OpenedBy         string     `json:"opened_by"`
	Reason           string     `json:"reason"`
	Description      string     `json:"description"`
	EvidenceURLs     []string   `json:"evidence_urls,omitempty"`
	Status           string     `json:"status"`
	Resolution       string     `json:"resolution,omitempty"`
	ResolutionAmount float64    `json:"resolution_amount,omitempty"`
	ResolutionNote   string     `json:"resolution_note,omitempty"`
	ResolvedBy       string     `json:"resolved_by,omitempty"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}
type DisputeComment struct {
	ID           string    `json:"id"`
	DisputeID    string    `json:"dispute_id"`
	AuthorID     string    `json:"author_id"`
	Body         string    `json:"body"`
	EvidenceURLs []string  `json:"evidence_urls,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
