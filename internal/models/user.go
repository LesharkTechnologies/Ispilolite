package models

import "time"

type Coordinates struct {
	Lat float64 `json:"lat" db:"latitude"`
	Lng float64 `json:"lng" db:"longitude"`
}

// Role constants for platform users.
const (
	RoleCustomer   = "customer"
	RoleTechnician = "technician"
	RoleISP        = "isp"
)

// User is the base account record shared across roles.
type User struct {
	ID                  string       `json:"id"`
	Phone               string       `json:"phone"`
	Username            string       `json:"username,omitempty"`
	Name                string       `json:"name"`
	Email               string       `json:"email"`
	Role                string       `json:"role"`
	PasswordHash        string       `json:"-"`
	IsVerified          bool         `json:"is_verified"`
	Rating              float64      `json:"rating"`
	TotalRatings        int          `json:"total_ratings"`
	Joined              time.Time    `json:"joined"`
	Location            *Coordinates `json:"location,omitempty"`
	Town                string       `json:"town,omitempty"`
	County              string       `json:"county,omitempty"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
	ISP_ID              string       `json:"isp_id,omitempty"`
	Status              string       `json:"status"`
	AvatarURL           string       `json:"avatar_url" db:"avatar_url"`
	CompanyID           string       `json:"company_id,omitempty" db:"company_id"`
	CompanyTechnicianID string       `json:"company_technician_id,omitempty" db:"company_technician_id"`
	CompanyStartedAt    *time.Time   `json:"company_started_at,omitempty" db:"company_started_at"`
	CompanyEndedAt      *time.Time   `json:"company_ended_at,omitempty" db:"company_ended_at"`
	ReferralCode        string       `json:"referral_code,omitempty" db:"referral_code"`
	ReferredByCode      string       `json:"-" db:"-"`
	CreditBalance       float64      `json:"credit_balance" db:"credit_balance"`
	Locale              string       `json:"locale" db:"locale"`
}

type Referral struct {
	ID           string     `json:"id"`
	ReferrerID   string     `json:"referrer_id"`
	ReferredID   string     `json:"referred_id"`
	ReferredName string     `json:"referred_name,omitempty"`
	Code         string     `json:"code"`
	Status       string     `json:"status"`
	RewardAmount float64    `json:"reward_amount"`
	QualifiedAt  *time.Time `json:"qualified_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}
