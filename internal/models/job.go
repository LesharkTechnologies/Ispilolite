package models

import "time"

type Installation struct {
	ID            string     `json:"id" db:"id"`
	LocationID    string     `json:"location_id" db:"location_id"`
	ServiceType   string     `json:"service_type" db:"service_type"`
	Description   string     `json:"description" db:"description"`
	PreferredDate *time.Time `json:"preferred_date,omitempty" db:"preferred_date"`
	Budget        float64    `json:"budget" db:"budget"`
	Status        string     `json:"status" db:"status"`
	ClientID      string     `json:"client_id" db:"client_id"`
	IspID         string     `json:"isp_id,omitempty" db:"isp_id"`
	TechnicianID  string     `json:"technician_id,omitempty" db:"technician_id"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

type JobRequest struct {
	ID                   string            `json:"id" db:"id"`
	CustomerID           string            `json:"customer_id" db:"customer_id"`
	RequesterRole        string            `json:"requester_role" db:"requester_role"`
	RequestType          string            `json:"request_type" db:"request_type"`
	Mode                 string            `json:"mode" db:"mode"`
	TargetISPID          string            `json:"target_isp_id,omitempty" db:"target_isp_id"`
	TechnicianID         string            `json:"target_technician_id,omitempty" db:"target_technician_id"`
	AssignedISPID        string            `json:"assigned_isp_id,omitempty" db:"assigned_isp_id"`
	AssignedTechnicianID string            `json:"assigned_technician_id,omitempty" db:"assigned_technician_id"`
	LocationID           string            `json:"location_id,omitempty" db:"location_id"`
	County               string            `json:"county" db:"county"`
	Town                 string            `json:"town" db:"town"`
	Village              string            `json:"village" db:"village"`
	ServiceType          string            `json:"service_type" db:"service_type"`
	SpeedMbps            int               `json:"speed_mbps,omitempty" db:"speed_mbps"`
	Description          string            `json:"description" db:"description"`
	Budget               float64           `json:"budget" db:"budget"`
	PreferredDate        *time.Time        `json:"preferred_date,omitempty" db:"preferred_date"`
	DeadlineAt           *time.Time        `json:"deadline_at,omitempty" db:"deadline_at"`
	TerminatedAt         *time.Time        `json:"terminated_at,omitempty" db:"terminated_at"`
	TerminationReason    string            `json:"termination_reason,omitempty" db:"termination_reason"`
	Status               string            `json:"status" db:"status"`
	IsAvailable          bool              `json:"is_available" db:"is_available"`
	Applications         []*JobApplication `json:"applications,omitempty" db:"-"`
	CreatedAt            time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at" db:"updated_at"`
	DeletedAt            *time.Time        `json:"deleted_at,omitempty" db:"deleted_at"`
	WarrantyDays         int               `json:"warranty_days" db:"warranty_days"`
	WarrantyExpiresAt    *time.Time        `json:"warranty_expires_at,omitempty" db:"warranty_expires_at"`
	ResponseDueAt        *time.Time        `json:"response_due_at,omitempty" db:"response_due_at"`
	QuoteDueAt           *time.Time        `json:"quote_due_at,omitempty" db:"quote_due_at"`
	InstallationDueAt    *time.Time        `json:"installation_due_at,omitempty" db:"installation_due_at"`
	ViewedAt             *time.Time        `json:"viewed_at,omitempty" db:"viewed_at"`
	AcceptedAt           *time.Time        `json:"accepted_at,omitempty" db:"accepted_at"`
	ScheduledAt          *time.Time        `json:"scheduled_at,omitempty" db:"scheduled_at"`
	CompletedAt          *time.Time        `json:"completed_at,omitempty" db:"completed_at"`
	LossReason           string            `json:"loss_reason,omitempty" db:"loss_reason"`
}

type WarrantyClaim struct {
	ID                   string    `json:"id"`
	OriginalRequestID    string    `json:"original_request_id"`
	ReplacementRequestID string    `json:"replacement_request_id,omitempty"`
	CustomerID           string    `json:"customer_id"`
	Description          string    `json:"description"`
	PhotoURLs            []string  `json:"photo_urls,omitempty"`
	Status               string    `json:"status"`
	ResolutionNote       string    `json:"resolution_note,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type JobApplication struct {
	ID            string    `json:"id" db:"id"`
	RequestID     string    `json:"request_id" db:"request_id"`
	ApplicantID   string    `json:"applicant_id" db:"applicant_id"`
	ApplicantRole string    `json:"applicant_role" db:"applicant_role"`
	Message       string    `json:"message" db:"message"`
	ProposedPrice float64   `json:"proposed_price" db:"proposed_price"`
	Status        string    `json:"status" db:"status"`
	ApplicantName string    `json:"applicant_name,omitempty" db:"applicant_name"`
	Rating        float64   `json:"rating,omitempty" db:"rating"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}
