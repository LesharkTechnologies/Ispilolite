package models

import "time"

// ISPCard is an e-commerce product-like card model for ISPs.
type ISPCard struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	DisplayName      string  `json:"display_name,omitempty"`
	PrimaryName      string  `json:"primary_name"`
	IconURL          string  `json:"icon_url"`
	LogoURL          string  `json:"logo_url"`
	Rating           float64 `json:"rating"`
	ReviewCount      int     `json:"review_count"`
	County           string  `json:"county"`
	SubCounty        string  `json:"sub_county"`
	Town             string  `json:"town,omitempty"`
	StartingPrice    float64 `json:"starting_price"`
	TechniciansCount int     `json:"technicians_count"`
	IdentityStatus   string  `json:"identity_status"`
	BadgeLabel       string  `json:"badge_label"`
	ProfilePath      string  `json:"profile_path"`
	IsActive         bool    `json:"is_active"`
}

// TechnicianCard is an e-commerce card model for Technicians.
type TechnicianCard struct {
	ID             string   `json:"id"`
	UserID         string   `json:"user_id"`
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name,omitempty"`
	PrimaryName    string   `json:"primary_name"`
	VerifiedName   string   `json:"verified_name,omitempty"`
	IdentityLabel  string   `json:"identity_label"`
	IdentityStatus string   `json:"identity_status"`
	BadgeLabel     string   `json:"badge_label"`
	IconURL        string   `json:"icon_url"`
	Rating         float64  `json:"rating"`
	JobsDone       int      `json:"jobs_done"`
	PricePerHour   float64  `json:"price_per_hour"`
	Skills         []string `json:"skills"`
	County         string   `json:"county"`
	Town           string   `json:"town"`
	IsAvailable    bool     `json:"is_available"`
	ProfilePath    string   `json:"profile_path"`
	ISPID          string   `json:"isp_id,omitempty"`
	ISPName        string   `json:"isp_name,omitempty"`
}

// CustomerWithJobsCard is an e-commerce card model for Customers with active/posted jobs.
type CustomerWithJobsCard struct {
	CustomerID       string     `json:"customer_id"`
	Name             string     `json:"name"`
	DisplayName      string     `json:"display_name,omitempty"`
	PrimaryName      string     `json:"primary_name"`
	VerifiedName     string     `json:"verified_name,omitempty"`
	IdentityLabel    string     `json:"identity_label"`
	IdentityStatus   string     `json:"identity_status"`
	BadgeLabel       string     `json:"badge_label"`
	IconURL          string     `json:"icon_url"`
	County           string     `json:"county"`
	Town             string     `json:"town"`
	ActiveJobsCount  int        `json:"active_jobs_count"`
	LatestJobTitle   string     `json:"latest_job_title"`
	LatestJobService string     `json:"latest_job_service"`
	LatestJobBudget  float64    `json:"latest_job_budget"`
	LatestJobDate    *time.Time `json:"latest_job_date,omitempty"`
	ISPID            string     `json:"isp_id,omitempty"`
	ISPName          string     `json:"isp_name,omitempty"`
	Jobs             []*JobCard `json:"jobs,omitempty"`
}

// JobCard is an e-commerce card model for service jobs.
type JobCard struct {
	ID                  string     `json:"id"`
	Title               string     `json:"title"`
	ServiceType         string     `json:"service_type"`
	IconType            string     `json:"icon_type"`
	Description         string     `json:"description"`
	Budget              float64    `json:"budget"`
	Currency            string     `json:"currency"`
	County              string     `json:"county"`
	Town                string     `json:"town"`
	Status              string     `json:"status"`
	CustomerID          string     `json:"customer_id"`
	CustomerName        string     `json:"customer_name"`
	CustomerDisplayName string     `json:"customer_display_name,omitempty"`
	CustomerPrimaryName string     `json:"customer_primary_name"`
	CustomerIdentity    string     `json:"customer_identity"`
	CustomerAvatarURL   string     `json:"customer_avatar_url"`
	CustomerBadge       string     `json:"customer_badge"`
	TargetISPID         string     `json:"target_isp_id,omitempty"`
	TargetISPName       string     `json:"target_isp_name,omitempty"`
	ApplicationsCount   int        `json:"applications_count"`
	PreferredDate       *time.Time `json:"preferred_date,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

// MarketplaceCardsOverview combines all 4 card categories into a single response.
type MarketplaceCardsOverview struct {
	ISPs              []*ISPCard              `json:"isps"`
	Technicians       []*TechnicianCard       `json:"technicians"`
	CustomersWithJobs []*CustomerWithJobsCard `json:"customers_with_jobs"`
	Jobs              []*JobCard              `json:"jobs"`
	TotalISPs         int                     `json:"total_isps"`
	TotalTechnicians  int                     `json:"total_technicians"`
	TotalCustomers    int                     `json:"total_customers"`
	TotalJobs         int                     `json:"total_jobs"`
}
