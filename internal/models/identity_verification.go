package models

import (
	"errors"
	"strings"
	"time"
)

const (
	VerificationStatusPending  = "pending"
	VerificationStatusApproved = "approved"
	VerificationStatusRejected = "rejected"
	VerificationStatusInReview = "in_review"

	VerificationBadgeUnverified       = "unverified"
	VerificationBadgeIDVerified       = "id_verified"
	VerificationBadgeBusinessVerified = "business_verified"
	VerificationBadgeBoth             = "both"
	VerificationBadgeRejected         = "rejected"
)

var (
	ErrInvalidVerificationBadge = errors.New("invalid verification status: must be 'id_verified', 'business_verified', or 'both'")
	ErrVerifiedNameRequired     = errors.New("verified name is required when verifying identity")
	ErrRejectionReasonRequired  = errors.New("rejection reason is required when rejecting verification")
)

// NormalizeVerificationBadge normalizes human and API inputs into valid badge constants.
func NormalizeVerificationBadge(input string) (string, error) {
	norm := strings.ToLower(strings.TrimSpace(input))
	norm = strings.ReplaceAll(norm, "-", "_")
	norm = strings.ReplaceAll(norm, " ", "_")

	switch norm {
	case "id_verified", "id", "id_verify", "verified_id":
		return VerificationBadgeIDVerified, nil
	case "business_verified", "business", "biz_verified", "business_verify":
		return VerificationBadgeBusinessVerified, nil
	case "both", "id_and_business", "id_and_business_verified", "all":
		return VerificationBadgeBoth, nil
	case "rejected":
		return VerificationBadgeRejected, nil
	case "unverified":
		return VerificationBadgeUnverified, nil
	default:
		return "", ErrInvalidVerificationBadge
	}
}

// FormatVerificationBadge returns the display label for a verification badge.
func FormatVerificationBadge(badge string) string {
	switch badge {
	case VerificationBadgeIDVerified:
		return "ID verified"
	case VerificationBadgeBusinessVerified:
		return "business verified"
	case VerificationBadgeBoth:
		return "both"
	case VerificationBadgeRejected:
		return "rejected"
	case VerificationStatusPending:
		return "pending"
	default:
		return "unverified"
	}
}

// IdentityVerification represents a submitted ID photo & verification record.
type IdentityVerification struct {
	ID                string     `json:"id" db:"id"`
	UserID            string     `json:"user_id" db:"user_id"`
	Phone             string     `json:"phone" db:"phone"`
	IDPhotoURL        string     `json:"id_photo_url" db:"id_photo_url"`
	IDPhotoBackURL    string     `json:"id_photo_back_url,omitempty" db:"id_photo_back_url"`
	IDNumber          string     `json:"id_number,omitempty" db:"id_number"`
	DocumentType      string     `json:"document_type" db:"document_type"`
	Status            string     `json:"status" db:"status"`
	VerificationBadge string     `json:"verification_badge" db:"verification_badge"`
	BadgeLabel        string     `json:"badge_label,omitempty" db:"-"`
	VerifiedName      string     `json:"verified_name,omitempty" db:"verified_name"`
	RejectionReason   string     `json:"rejection_reason,omitempty" db:"rejection_reason"`
	Notes             string     `json:"notes,omitempty" db:"notes"`
	SubmittedAt       time.Time  `json:"submitted_at" db:"submitted_at"`
	ReviewedBy        string     `json:"reviewed_by,omitempty" db:"reviewed_by"`
	ReviewedAt        *time.Time `json:"reviewed_at,omitempty" db:"reviewed_at"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`

	// Joined fields for administrative views
	UserName  string `json:"user_name,omitempty" db:"user_name"`
	UserEmail string `json:"user_email,omitempty" db:"user_email"`
	UserRole  string `json:"user_role,omitempty" db:"user_role"`
}

// SubmitVerificationRequest is the payload sent by a technician or user.
type SubmitVerificationRequest struct {
	IDPhotoURL     string `json:"id_photo_url"`
	IDPhotoBackURL string `json:"id_photo_back_url,omitempty"`
	IDNumber       string `json:"id_number,omitempty"`
	DocumentType   string `json:"document_type,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

// AdminReviewVerificationRequest is the payload sent by the admin to approve or reject.
type AdminReviewVerificationRequest struct {
	Status            string `json:"status,omitempty"`
	VerificationBadge string `json:"verification_badge,omitempty"`
	VerifiedName      string `json:"verified_name,omitempty"`
	RejectionReason   string `json:"rejection_reason,omitempty"`
	Notes             string `json:"notes,omitempty"`
}
