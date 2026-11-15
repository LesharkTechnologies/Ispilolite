package models

import (
	"fmt"
	"time"
)

// Promotion listing types.
const (
	PromotionListingBillingSystem = "billing_system"
	PromotionListingISP           = "isp"
	PromotionListingTechnician    = "technician"
)

// Promotion statuses returned to clients. stored status is evaluated against
// starts_at/expires_at so an active promotion becomes expired without a worker.
const (
	PromotionPendingApproval = "pending_approval"
	PromotionPendingPayment  = "pending_payment"
	PromotionScheduled       = "scheduled"
	PromotionActive          = "active"
	PromotionExpired         = "expired"
	PromotionCancelled       = "cancelled"
	PromotionSuspended       = "suspended"
	PromotionRejected        = "rejected"
)

// Promotion is a sponsored listing owned by a user. The listing_id is
// polymorphic; see migration 0037 for the mapping.
type Promotion struct {
	ID                   string     `json:"id"`
	OwnerID              string     `json:"owner_id"`
	ListingType          string     `json:"listing_type"`
	ListingID            string     `json:"listing_id"`
	ListingName          string     `json:"listing_name"`
	DestinationType      string     `json:"destination_type"`
	DestinationURL       string     `json:"destination_url"`
	Placement            string     `json:"placement"`
	Status               string     `json:"status"`
	StoredStatus         string     `json:"-"`
	StartsAt             time.Time  `json:"starts_at"`
	ExpiresAt            time.Time  `json:"expires_at"`
	TimeRemainingSeconds int64      `json:"time_remaining_seconds"`
	TimeRemaining        string     `json:"time_remaining"`
	Amount               float64    `json:"amount"`
	Currency             string     `json:"currency"`
	PaymentStatus        string     `json:"payment_status,omitempty"`
	Impressions          int64      `json:"impressions"`
	UniqueViewers        int64      `json:"unique_viewers"`
	Clicks               int64      `json:"clicks"`
	UniqueClickers       int64      `json:"unique_clickers"`
	ClickThroughRate     float64    `json:"click_through_rate"`
	EnquiryCount         int64      `json:"enquiry_count"`
	RenewedFromID        string     `json:"renewed_from_id,omitempty"`
	ApprovedAt           *time.Time `json:"approved_at,omitempty"`
	RejectedAt           *time.Time `json:"rejected_at,omitempty"`
	RejectionReason      string     `json:"rejection_reason,omitempty"`
	CancelledAt          *time.Time `json:"cancelled_at,omitempty"`
	SuspendedAt          *time.Time `json:"suspended_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// PromotionListing is the resolved listing metadata used to create a
// promotion. It is never persisted by itself.
type PromotionListing struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	InternalPath string `json:"internal_path"`
}

type PromotionPayment struct {
	ID                string     `json:"id"`
	PromotionID       string     `json:"promotion_id"`
	Amount            float64    `json:"amount"`
	Currency          string     `json:"currency"`
	Status            string     `json:"status"`
	Provider          string     `json:"provider,omitempty"`
	ProviderReference string     `json:"provider_reference,omitempty"`
	Receipt           string     `json:"receipt,omitempty"`
	ConfirmedBy       string     `json:"confirmed_by,omitempty"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type PromotionPublic struct {
	ID             string    `json:"id"`
	ListingType    string    `json:"listing_type"`
	ListingID      string    `json:"listing_id"`
	ListingName    string    `json:"listing_name"`
	DestinationURL string    `json:"destination_url"`
	Placement      string    `json:"placement"`
	StartsAt       time.Time `json:"starts_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type PromotionTotals struct {
	Impressions      int64   `json:"impressions"`
	UniqueViewers    int64   `json:"unique_viewers"`
	Clicks           int64   `json:"clicks"`
	UniqueClickers   int64   `json:"unique_clickers"`
	ClickThroughRate float64 `json:"click_through_rate"`
}

type PromotionRoleInsight struct {
	Role          string `json:"role"`
	Impressions   int64  `json:"impressions"`
	UniqueViewers int64  `json:"unique_viewers"`
	Clicks        int64  `json:"clicks"`
}

type PromotionLocationInsight struct {
	County           string  `json:"county"`
	Impressions      int64   `json:"impressions"`
	UniqueViewers    int64   `json:"unique_viewers"`
	Clicks           int64   `json:"clicks"`
	ClickThroughRate float64 `json:"click_through_rate"`
}

type PromotionPlacementInsight struct {
	Placement        string  `json:"placement"`
	Impressions      int64   `json:"impressions"`
	UniqueViewers    int64   `json:"unique_viewers"`
	Clicks           int64   `json:"clicks"`
	UniqueClickers   int64   `json:"unique_clickers"`
	ClickThroughRate float64 `json:"click_through_rate"`
}

type PromotionDailyInsight struct {
	Date             string  `json:"date"`
	Impressions      int64   `json:"impressions"`
	UniqueViewers    int64   `json:"unique_viewers"`
	Clicks           int64   `json:"clicks"`
	UniqueClickers   int64   `json:"unique_clickers"`
	ClickThroughRate float64 `json:"click_through_rate"`
}

type PromotionInsights struct {
	PromotionID string                      `json:"promotion_id"`
	From        string                      `json:"from"`
	To          string                      `json:"to"`
	Totals      PromotionTotals             `json:"totals"`
	Roles       []PromotionRoleInsight      `json:"roles"`
	Locations   []PromotionLocationInsight  `json:"locations"`
	Placements  []PromotionPlacementInsight `json:"placements"`
	Daily       []PromotionDailyInsight     `json:"daily"`
}

type PromotionEnquiry struct {
	ID          string    `json:"id"`
	PromotionID string    `json:"promotion_id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Phone       string    `json:"phone,omitempty"`
	Email       string    `json:"email,omitempty"`
	Message     string    `json:"message"`
	Consent     bool      `json:"consent"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// EffectivePromotionStatus derives the status shown to clients. Expiry and
// scheduling are evaluated against server time so no background worker is
// required. Explicit moderation states always win.
func EffectivePromotionStatus(stored string, startsAt, expiresAt, now time.Time) string {
	switch stored {
	case PromotionCancelled, PromotionSuspended, PromotionRejected:
		return stored
	}
	if !expiresAt.After(now) {
		return PromotionExpired
	}
	switch stored {
	case PromotionPendingApproval, PromotionPendingPayment:
		return stored
	}
	if startsAt.After(now) {
		return PromotionScheduled
	}
	return PromotionActive
}

// PromotionTimeRemaining returns a signed number of seconds and a short,
// human-readable label. Expired promotions keep a negative remaining value.
func PromotionTimeRemaining(status string, startsAt, expiresAt, now time.Time) (int64, string) {
	switch status {
	case PromotionPendingApproval:
		return 0, "Awaiting admin approval"
	case PromotionPendingPayment:
		return 0, "Awaiting payment"
	case PromotionRejected:
		return 0, "Rejected"
	case PromotionCancelled:
		return 0, "Cancelled"
	case PromotionSuspended:
		return 0, "Suspended"
	case PromotionExpired:
		delta := now.Sub(expiresAt)
		return int64(-delta / time.Second), "Expired " + promotionDuration(delta) + " ago"
	case PromotionScheduled:
		delta := startsAt.Sub(now)
		return int64(delta / time.Second), "Starts in " + promotionDuration(delta)
	default:
		delta := expiresAt.Sub(now)
		if delta <= 0 {
			return int64(delta / time.Second), "Expired " + promotionDuration(-delta) + " ago"
		}
		return int64(delta / time.Second), promotionDuration(delta) + " remaining"
	}
}

func promotionDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	days := int64(d / (24 * time.Hour))
	hours := int64(d / time.Hour)
	minutes := int64(d / time.Minute)
	switch {
	case days >= 1:
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	case hours >= 1:
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	case minutes >= 1:
		if minutes == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	default:
		return "less than a minute"
	}
}
