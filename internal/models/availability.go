package models

import "time"

type AvailabilityRule struct {
	ID           string    `json:"id"`
	TechnicianID string    `json:"technician_id"`
	Weekday      int       `json:"weekday"`    // 0=Sunday ... 6=Saturday
	StartTime    string    `json:"start_time"` // HH:MM
	EndTime      string    `json:"end_time"`   // HH:MM
	Timezone     string    `json:"timezone"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
}

type AvailabilityException struct {
	ID           string    `json:"id"`
	TechnicianID string    `json:"technician_id"`
	Date         string    `json:"date"` // YYYY-MM-DD in technician timezone
	Available    bool      `json:"available"`
	StartTime    string    `json:"start_time,omitempty"`
	EndTime      string    `json:"end_time,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type BookableSlot struct {
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	Timezone     string    `json:"timezone"`
	TechnicianID string    `json:"technician_id"`
}
