package models

import "time"

type BillingSystem struct {
	ID               string          `json:"id"`
	OwnerID          string          `json:"owner_id"`
	Name             string          `json:"name"`
	Slug             string          `json:"slug"`
	LogoURL          string          `json:"logo_url"`
	Description      string          `json:"description"`
	OwnerCompany     string          `json:"owner_company"`
	OwnerPhone       string          `json:"owner_phone"`
	SupportPhone     string          `json:"support_phone"`
	SupportWhatsApp  string          `json:"support_whatsapp"`
	SupportEmail     string          `json:"support_email"`
	SupportHours     string          `json:"support_hours"`
	WebsiteURL       string          `json:"website_url"`
	LoginURL         string          `json:"login_url"`
	ReferralURL      string          `json:"referral_url"`
	PricingUpdatedAt *time.Time      `json:"pricing_updated_at,omitempty"`
	Status           string          `json:"status"`
	DomainVerified   bool            `json:"domain_verified"`
	BusinessVerified bool            `json:"business_verified"`
	ReviewCount      int             `json:"review_count"`
	AverageRating    float64         `json:"average_rating"`
	FollowerCount    int             `json:"follower_count"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	Pricing          []*BillingPrice `json:"pricing,omitempty"`
}

type BillingPrice struct {
	ID                    string     `json:"id"`
	BillingSystemID       string     `json:"billing_system_id"`
	ServiceType           string     `json:"service_type"`
	Currency              string     `json:"currency"`
	BillingPeriod         string     `json:"billing_period"`
	Amount                *float64   `json:"amount,omitempty"`
	SetupFee              float64    `json:"setup_fee"`
	SubscriberLimit       *int       `json:"subscriber_limit,omitempty"`
	ExtraSubscriberCost   *float64   `json:"extra_subscriber_cost,omitempty"`
	TransactionFeePercent *float64   `json:"transaction_fee_percent,omitempty"`
	Notes                 string     `json:"notes"`
	Available             bool       `json:"available"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type BillingReview struct {
	ID              string     `json:"id"`
	BillingSystemID string     `json:"billing_system_id"`
	ReviewerID      string     `json:"reviewer_id"`
	ReviewerName    string     `json:"reviewer_name,omitempty"`
	ReviewerRole    string     `json:"reviewer_role,omitempty"`
	Reliability     int        `json:"reliability"`
	EaseOfUse       int        `json:"ease_of_use"`
	Support         int        `json:"support"`
	Features        int        `json:"features"`
	ValueForMoney   int        `json:"value_for_money"`
	AverageScore    float64    `json:"average_score"`
	Comment         string     `json:"comment"`
	UseVerified     bool       `json:"use_verified"`
	OwnerReply      string     `json:"owner_reply,omitempty"`
	OwnerRepliedAt  *time.Time `json:"owner_replied_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type BillingFlag struct {
	ID              string    `json:"id"`
	BillingSystemID string    `json:"billing_system_id"`
	ReporterID      string    `json:"reporter_id"`
	Reason          string    `json:"reason"`
	Details         string    `json:"details"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

type BillingClaim struct {
	ID              string     `json:"id"`
	BillingSystemID string     `json:"billing_system_id"`
	ClaimantID      string     `json:"claimant_id"`
	Evidence        string     `json:"evidence"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
}