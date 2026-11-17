package models

import "time"

// Customer flag statuses.
const (
	FlagPending    = "pending"
	FlagInProgress = "in_progress"
	FlagApproved   = "approved"
	FlagCancelled  = "cancelled"
)

// Account actions an approved flag may trigger.
const (
	FlagActionNone    = "none"
	FlagActionMonitor = "monitor"
	FlagActionSuspend = "suspend"
)

// FlagCategories lists the report categories accepted by the API.
var FlagCategories = []string{
	"fraud",
	"harassment",
	"impersonation",
	"spam",
	"payment_scam",
	"fake_listing",
	"abusive_content",
	"other",
}

// ValidFlagCategory reports whether category is on the accepted list.
func ValidFlagCategory(category string) bool {
	for _, candidate := range FlagCategories {
		if candidate == category {
			return true
		}
	}
	return false
}

// ValidFlagStatus reports whether status is an accepted review state.
func ValidFlagStatus(status string) bool {
	switch status {
	case FlagPending, FlagInProgress, FlagApproved, FlagCancelled:
		return true
	}
	return false
}

// ValidFlagAction reports whether action is an accepted account action.
func ValidFlagAction(action string) bool {
	switch action {
	case FlagActionNone, FlagActionMonitor, FlagActionSuspend:
		return true
	}
	return false
}

// CustomerFlag is a trust-and-safety report filed by a customer.
type CustomerFlag struct {
	ID            string     `json:"id"`
	ReporterID    string     `json:"reporter_id"`
	ReportedUser  *string    `json:"reported_user_id,omitempty"`
	ReportedPhone string     `json:"reported_phone,omitempty"`
	Category      string     `json:"category"`
	Description   string     `json:"description"`
	ImageURLs     []string   `json:"image_urls"`
	Status        string     `json:"status"`
	AdminFeedback string     `json:"admin_feedback,omitempty"`
	ReviewedBy    *string    `json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
	AccountAction string     `json:"account_action"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// Admin-only context about the reported account.
	ReportedUserName   string `json:"reported_user_name,omitempty"`
	ReportedUserPhone  string `json:"reported_user_phone,omitempty"`
	ReportedUserStatus string `json:"reported_user_status,omitempty"`
}

// MonitoredUser is a user on the verification/monitoring list.
type MonitoredUser struct {
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Phone      string     `json:"phone"`
	Status     string     `json:"status"`
	Reason     string     `json:"reason"`
	FlagID     *string    `json:"flag_id,omitempty"`
	Active     bool       `json:"active"`
	AddedBy    *string    `json:"added_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
}

// MonitoredActivity is one watched action performed by a monitored user.
type MonitoredActivity struct {
	ID          int64                  `json:"id"`
	UserID      string                 `json:"user_id"`
	Event       string                 `json:"event"`
	SubjectType string                 `json:"subject_type"`
	SubjectID   string                 `json:"subject_id"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	IPAddress   string                 `json:"ip_address,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}
