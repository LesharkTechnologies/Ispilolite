package models

import (
	"errors"
	"strings"
	"time"
)

type Coordinates struct {
	Lat float64 `json:"lat" db:"latitude"`
	Lng float64 `json:"lng" db:"longitude"`
}

// Role constants for platform users.
const (
	RoleCustomer   = "customer"
	RoleTechnician = "technician"
	RoleISP        = "isp"
	RoleBoth       = "both"
	RoleAdmin      = "admin"
)

// NormalizeRole maps friendly role aliases ('c', 't', 'i', 't&i', 'both') to official role constants.
func NormalizeRole(input string) (string, error) {
	norm := strings.ToLower(strings.TrimSpace(input))
	norm = strings.ReplaceAll(norm, "-", "_")
	norm = strings.ReplaceAll(norm, " ", "_")
	switch norm {
	case "c", "client", "customer":
		return RoleCustomer, nil
	case "t", "tech", "technician":
		return RoleTechnician, nil
	case "i", "isp":
		return RoleISP, nil
	case "both", "t&i", "t_and_i", "isp_and_tech", "isp_and_technician", "all":
		return RoleBoth, nil
	case "admin":
		return RoleAdmin, nil
	default:
		return "", errors.New("invalid account type: must be 'customer', 'technician', 'isp', or 'both'")
	}
}

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
	IdentityStatus      string       `json:"identity_status" db:"identity_status"`
	VerifiedName        string       `json:"verified_name,omitempty" db:"verified_name"`
	DisplayName         string       `json:"display_name,omitempty" db:"display_name"`
	RoleChangedAt       *time.Time   `json:"role_changed_at,omitempty" db:"role_changed_at"`
	SecondaryRole       string       `json:"secondary_role,omitempty" db:"secondary_role"`
	DeletedAt           *time.Time   `json:"deleted_at,omitempty" db:"deleted_at"`
	RetentionUntil      *time.Time   `json:"retention_until,omitempty" db:"retention_until"`
}

// EffectiveDisplayName returns the user's custom display name (e.g. "Leshark")
// if set, falling back to their full name (e.g. "Collins Muthomi").
func (u *User) EffectiveDisplayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Name
}

// IdentityLabel formats the identity display string (e.g. "identity [Collins Muthomi]").
func (u *User) IdentityLabel() string {
	legal := u.VerifiedName
	if legal == "" {
		legal = u.Name
	}
	if legal == "" {
		return ""
	}
	return "identity [" + legal + "]"
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
