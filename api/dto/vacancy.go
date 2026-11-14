package dto

type VacancyRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ServiceType string `json:"service_type"`
	Slots       int    `json:"slots"`
	DeadlineAt  string `json:"deadline_at,omitempty"`
}

type VacancyApplicationRequest struct {
	Message string `json:"message"`
}
