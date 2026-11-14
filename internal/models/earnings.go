package models

import "time"

type EarningsPoint struct {
	PeriodStart time.Time `json:"period_start"`
	Amount      float64   `json:"amount"`
	Jobs        int       `json:"jobs"`
}
type EarningsPartner struct {
	ISPID   string  `json:"isp_id"`
	ISPName string  `json:"isp_name"`
	Amount  float64 `json:"amount"`
	Jobs    int     `json:"jobs"`
}
type EarningsDashboard struct {
	Currency        string             `json:"currency"`
	Period          string             `json:"period"`
	SettledEarnings float64            `json:"settled_earnings"`
	PendingRelease  float64            `json:"pending_release"`
	CompletedJobs   int                `json:"completed_jobs"`
	AverageRating   float64            `json:"average_rating"`
	Series          []*EarningsPoint   `json:"series"`
	TopISPs         []*EarningsPartner `json:"top_isps"`
}
