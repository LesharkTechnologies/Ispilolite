package models

// PublicCoveragePage is the safe, public view of an area landing page. It is
// shared by the JSON endpoint and the server-rendered SEO page. No customer,
// technician or waitlist identities are included.
type PublicCoveragePage struct {
	PageType     string               `json:"page_type"`
	County       string               `json:"county"`
	Town         string               `json:"town,omitempty"`
	Estate       string               `json:"estate,omitempty"`
	Title        string               `json:"title"`
	Description  string               `json:"description"`
	CanonicalURL string               `json:"canonical_url"`
	ImageURL     string               `json:"image_url,omitempty"`
	ISPCount     int                  `json:"isp_count"`
	PackageCount int                  `json:"package_count"`
	FromPrice    float64              `json:"from_price"`
	ISPs         []*PublicCoverageISP `json:"isps"`
}

// PublicCoverageISP is an ISP that serves the requested area. Serviceability
// reflects the ISP's own published coverage record when one exists.
type PublicCoverageISP struct {
	ID                   string                   `json:"id"`
	Name                 string                   `json:"name"`
	Description          string                   `json:"description"`
	LogoURL              string                   `json:"logo_url,omitempty"`
	ProfilePath          string                   `json:"profile_path,omitempty"`
	ProfileURL           string                   `json:"profile_url,omitempty"`
	Rating               float64                  `json:"rating"`
	ReviewCount          int                      `json:"review_count"`
	County               string                   `json:"county,omitempty"`
	Town                 string                   `json:"town,omitempty"`
	Estate               string                   `json:"estate,omitempty"`
	Serviceability       string                   `json:"serviceability,omitempty"`
	EstimatedDays        int                      `json:"estimated_days,omitempty"`
	TechniciansAvailable int                      `json:"technicians_available"`
	FromPrice            float64                  `json:"from_price"`
	Packages             []*PublicCoveragePackage `json:"packages"`
}

// PublicCoveragePackage is intentionally narrower than models.ISPPackage so
// public responses cannot accidentally expose internal catalogue metadata.
type PublicCoveragePackage struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Category     string  `json:"category"`
	SpeedValue   float64 `json:"speed_value"`
	SpeedUnit    string  `json:"speed_unit"`
	Speed        string  `json:"speed,omitempty"`
	Price        float64 `json:"price"`
	BillingCycle string  `json:"billing_cycle"`
	Description  string  `json:"description,omitempty"`
}
