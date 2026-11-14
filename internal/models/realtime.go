package models

import "time"

// JobEvent is an immutable service-request lifecycle event. Events are stored
// for reconnect/replay while SSE carries the same payload in real time.
type JobEvent struct {
	ID           string                 `json:"id" db:"id"`
	JobRequestID string                 `json:"job_request_id" db:"request_id"`
	Type         string                 `json:"type" db:"event_type"`
	Status       string                 `json:"status,omitempty" db:"status"`
	ActorID      string                 `json:"actor_id,omitempty" db:"actor_id"`
	Data         map[string]interface{} `json:"data,omitempty" db:"-"`
	CreatedAt    time.Time              `json:"created_at" db:"created_at"`
}

// TechnicianLiveLocation is the most recent position shared for one active
// job. It is job-scoped deliberately: unrelated users cannot track a
// technician by knowing their user ID.
type TechnicianLiveLocation struct {
	TechnicianID string    `json:"technician_id" db:"technician_id"`
	JobRequestID string    `json:"job_request_id" db:"request_id"`
	Lat          float64   `json:"lat" db:"latitude"`
	Lon          float64   `json:"lon" db:"longitude"`
	AccuracyM    float64   `json:"accuracy_m,omitempty" db:"accuracy_m"`
	Heading      float64   `json:"heading,omitempty" db:"heading"`
	SpeedKPH     float64   `json:"speed_kph,omitempty" db:"speed_kph"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
	IsOnline     bool      `json:"is_online" db:"-"`
	DistanceKM   float64   `json:"distance_km,omitempty" db:"-"`
	ETASeconds   int       `json:"eta_seconds,omitempty" db:"-"`
}

// TechnicianLocationUpdate is sent by the technician app, normally every 30
// seconds while travelling to an assigned job.
type TechnicianLocationUpdate struct {
	JobRequestID string  `json:"job_request_id"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	AccuracyM    float64 `json:"accuracy_m,omitempty"`
	Heading      float64 `json:"heading,omitempty"`
	SpeedKPH     float64 `json:"speed_kph,omitempty"`
}
