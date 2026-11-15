package models

import "time"

type ServiceabilityRecord struct {
	ID            string    `json:"id"`
	ISPID         string    `json:"isp_id"`
	ISPName       string    `json:"isp_name,omitempty"`
	LocationID    string    `json:"location_id,omitempty"`
	County        string    `json:"county"`
	Town          string    `json:"town"`
	Estate        string    `json:"estate"`
	Building      string    `json:"building"`
	Technology    string    `json:"technology"`
	Status        string    `json:"status"`
	EstimatedDays int       `json:"estimated_days"`
	Reason        string    `json:"reason,omitempty"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ServiceabilityWaitlist struct {
	ID          string    `json:"id"`
	CustomerID  string    `json:"customer_id,omitempty"`
	ISPID       string    `json:"isp_id,omitempty"`
	LocationID  string    `json:"location_id,omitempty"`
	County      string    `json:"county"`
	Town        string    `json:"town"`
	Estate      string    `json:"estate"`
	Building    string    `json:"building"`
	ServiceType string    `json:"service_type"`
	CreatedAt   time.Time `json:"created_at"`
}

type InstallationProof struct {
	ID                  string          `json:"id"`
	RequestID           string          `json:"request_id"`
	TechnicianID        string          `json:"technician_id"`
	CheckInAt           time.Time       `json:"check_in_at"`
	CheckOutAt          *time.Time      `json:"check_out_at,omitempty"`
	Latitude            *float64        `json:"latitude,omitempty"`
	Longitude           *float64        `json:"longitude,omitempty"`
	PhotoURLs           []string        `json:"photo_urls"`
	Checklist           map[string]bool `json:"checklist"`
	DownloadMbps        *float64        `json:"download_mbps,omitempty"`
	UploadMbps          *float64        `json:"upload_mbps,omitempty"`
	LatencyMs           *float64        `json:"latency_ms,omitempty"`
	PacketLossPercent   *float64        `json:"packet_loss_percent,omitempty"`
	DeviceSerial        string          `json:"device_serial"`
	Notes               string          `json:"notes,omitempty"`
	Status              string          `json:"status"`
	CustomerConfirmedAt *time.Time      `json:"customer_confirmed_at,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}
