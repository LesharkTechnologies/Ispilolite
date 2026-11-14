package models

import "time"

type ChatMessage struct {
	ID             string     `json:"id" db:"id"`
	JobRequestID   string     `json:"job_request_id" db:"request_id"`
	SenderID       string     `json:"sender_id" db:"sender_id"`
	SenderName     string     `json:"sender_name,omitempty" db:"-"`
	SenderRole     string     `json:"sender_role,omitempty" db:"-"`
	Body           string     `json:"body" db:"body"`
	AttachmentURLs []string   `json:"attachment_urls,omitempty" db:"-"`
	ReadAt         *time.Time `json:"read_at,omitempty" db:"-"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	EditedAt       *time.Time `json:"edited_at,omitempty" db:"edited_at"`
}
