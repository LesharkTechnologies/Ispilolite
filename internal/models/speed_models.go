package models

import "time"

type SpeedTest struct {
	ID                   string    `json:"id"`
	TestSessionID        string    `json:"test_session_id"`
	UserID               string    `json:"user_id,omitempty"`
	ISPID                string    `json:"isp_id,omitempty"`
	ISPName              string    `json:"isp_name,omitempty"`
	ISPSelectionSource   string    `json:"isp_selection_source"`
	County               string    `json:"county"`
	Town                 string    `json:"town"`
	DownloadMbps         float64   `json:"download_mbps"`
	UploadMbps           float64   `json:"upload_mbps"`
	LatencyMs            float64   `json:"latency_ms"`
	JitterMs             *float64  `json:"jitter_ms,omitempty"`
	PacketLossPercent    *float64  `json:"packet_loss_percent,omitempty"`
	ISPPlanSpeedMbps     *float64  `json:"isp_plan_speed_mbps,omitempty"`
	IncludeInPublicStats bool      `json:"include_in_public_stats"`
	Status               string    `json:"status,omitempty"`
	RejectionReason      string    `json:"rejection_reason,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
}

type SpeedTestISPOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	County   string `json:"county,omitempty"`
	Selected bool   `json:"selected,omitempty"`
}

type SpeedTestStats struct {
	ISPID             string  `json:"isp_id"`
	ISPName           string  `json:"isp_name"`
	County            string  `json:"county,omitempty"`
	Town              string  `json:"town,omitempty"`
	AcceptedCount     int     `json:"accepted_count"`
	PendingCount      int     `json:"pending_count"`
	SuspiciousCount   int     `json:"suspicious_count"`
	AverageDownload   float64 `json:"average_download_mbps"`
	AverageUpload     float64 `json:"average_upload_mbps"`
	AverageLatency    float64 `json:"average_latency_ms"`
	AverageJitter     float64 `json:"average_jitter_ms"`
	AveragePacketLoss float64 `json:"average_packet_loss_percent"`
}

type SpeedTestModerationItem struct {
	SpeedTest
	IP     string `json:"ip_address"`
	UserID string `json:"user_id,omitempty"`
}

type CountyMonthlySpeedStats struct {
	ID                    string    `json:"id"`
	Year                  int       `json:"year"`
	Month                 int       `json:"month"`
	County                string    `json:"county"`
	Town                  string    `json:"town,omitempty"`
	ISPID                 string    `json:"isp_id,omitempty"`
	ISPName               string    `json:"isp_name,omitempty"`
	SampleCount           int       `json:"sample_count"`
	MinDownloadMbps       float64   `json:"min_download_mbps"`
	MaxDownloadMbps       float64   `json:"max_download_mbps"`
	AvgDownloadMbps       float64   `json:"avg_download_mbps"`
	MinUploadMbps         float64   `json:"min_upload_mbps"`
	MaxUploadMbps         float64   `json:"max_upload_mbps"`
	AvgUploadMbps         float64   `json:"avg_upload_mbps"`
	AvgLatencyMs          float64   `json:"avg_latency_ms"`
	RecommendationSummary string    `json:"recommendation_summary,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type CountySpeedBenchmark struct {
	County          string  `json:"county"`
	SampleCount     int     `json:"sample_count"`
	MinDownloadMbps float64 `json:"min_download_mbps"`
	MaxDownloadMbps float64 `json:"max_download_mbps"`
	AvgDownloadMbps float64 `json:"avg_download_mbps"`
	AvgUploadMbps   float64 `json:"avg_upload_mbps"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
}
