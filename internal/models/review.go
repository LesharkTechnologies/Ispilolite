package models

import "time"

type Review struct {
	ID             string    `json:"id" db:"id"`
	TargetID       string    `json:"target_id" db:"target_id"`
	TargetType     string    `json:"target_type" db:"target_type"`
	CompanyID      string    `json:"company_id,omitempty" db:"company_id"`
	EngagementID   string    `json:"engagement_id,omitempty" db:"engagement_id"`
	JobRequestID   string    `json:"job_request_id,omitempty" db:"request_id"`
	UserID         string    `json:"user_id" db:"user_id"`
	Rating         int       `json:"rating" db:"rating"`
	Comment        string    `json:"comment" db:"comment"`
	Status         string    `json:"status" db:"status"`
	ModerationNote string    `json:"moderation_note,omitempty" db:"moderation_note"`
	PhotoURLs      []string  `json:"photo_urls,omitempty" db:"-"`
	IsVerified     bool      `json:"is_verified" db:"is_verified"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}
type ReviewReport struct {
	ID         string    `json:"id"`
	ReviewID   string    `json:"review_id"`
	ReporterID string    `json:"reporter_id"`
	Reason     string    `json:"reason"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}
