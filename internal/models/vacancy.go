package models

import "time"

type ISPVacancy struct {
	ID          string     `json:"id"`
	ISPID       string     `json:"isp_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	ServiceType string     `json:"service_type"`
	Slots       int        `json:"slots"`
	DeadlineAt  *time.Time `json:"deadline_at,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
