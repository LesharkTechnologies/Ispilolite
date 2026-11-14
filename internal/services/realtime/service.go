package realtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	goredis "github.com/go-redis/redis/v8"

	"ispilolite/internal/models"
	"ispilolite/internal/utils"
	appnotifications "ispilolite/pkg/notifications"
)

var (
	ErrInvalidLocation = errors.New("invalid live location")
	ErrForbidden       = errors.New("job not found or access denied")
)

type Repository interface {
	JobForParticipant(context.Context, string, string) (*models.JobRequest, error)
	UpsertLocation(context.Context, string, *models.TechnicianLiveLocation) (*models.JobRequest, error)
	LocationForParticipant(context.Context, string, string) (*models.TechnicianLiveLocation, error)
	Destination(context.Context, string) (float64, float64, bool, error)
	EventsForParticipant(context.Context, string, string, int) ([]*models.JobEvent, error)
}

type Service struct {
	repo     Repository
	redis    *goredis.Client
	notifier *appnotifications.Service
	hub      *appnotifications.Hub
}

func NewService(repo Repository, redisClient *goredis.Client, notifier *appnotifications.Service, hub *appnotifications.Hub) *Service {
	if hub == nil {
		hub = appnotifications.Default
	}
	return &Service{repo: repo, redis: redisClient, notifier: notifier, hub: hub}
}

func (s *Service) CanAccess(ctx context.Context, jobID, userID string) (*models.JobRequest, error) {
	job, err := s.repo.JobForParticipant(ctx, strings.TrimSpace(jobID), strings.TrimSpace(userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	return job, err
}

func (s *Service) UpdateLocation(ctx context.Context, technicianID string, in models.TechnicianLocationUpdate) (*models.TechnicianLiveLocation, error) {
	if strings.TrimSpace(in.JobRequestID) == "" || in.Lat < -90 || in.Lat > 90 || in.Lon < -180 || in.Lon > 180 || in.AccuracyM < 0 || in.SpeedKPH < 0 || in.Heading < 0 || in.Heading > 360 {
		return nil, ErrInvalidLocation
	}
	item := &models.TechnicianLiveLocation{TechnicianID: technicianID, JobRequestID: in.JobRequestID,
		Lat: in.Lat, Lon: in.Lon, AccuracyM: in.AccuracyM, Heading: in.Heading,
		SpeedKPH: in.SpeedKPH, UpdatedAt: time.Now().UTC(), IsOnline: true}
	job, err := s.repo.UpsertLocation(ctx, technicianID, item)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	if err != nil {
		return nil, err
	}

	lat, lon, hasDestination, err := s.repo.Destination(ctx, in.JobRequestID)
	if err != nil {
		return nil, err
	}
	if hasDestination {
		item.DistanceKM = haversineKM(in.Lat, in.Lon, lat, lon)
		speed := in.SpeedKPH
		if speed < 5 {
			speed = 30 // conservative urban default when GPS speed is absent
		}
		item.ETASeconds = int((item.DistanceKM / speed) * 3600)
	}

	payload := map[string]interface{}{"technician_id": technicianID, "job_request_id": in.JobRequestID,
		"lat": in.Lat, "lon": in.Lon, "accuracy_m": in.AccuracyM, "heading": in.Heading,
		"speed_kph": in.SpeedKPH, "distance_km": item.DistanceKM, "eta_seconds": item.ETASeconds,
		"updated_at": item.UpdatedAt}
	s.hub.PublishTopic("job:"+in.JobRequestID, "technician_location", payload)

	// Send the five-minute toast once per approach, with a Redis guard that also
	// works across API replicas.
	if item.ETASeconds > 0 && item.ETASeconds <= 300 && s.notifier != nil {
		shouldNotify := true
		if s.redis != nil {
			key := "job:" + in.JobRequestID + ":eta5-notified"
			set, setErr := s.redis.SetNX(ctx, key, "1", 6*time.Hour).Result()
			shouldNotify = setErr == nil && set
		}
		if shouldNotify {
			_ = s.notifier.SendInApp(job.CustomerID, "technician_arriving", "Technician arriving soon",
				"Your technician is about five minutes away.", payload)
		}
	}
	return item, nil
}

func (s *Service) Location(ctx context.Context, jobID, userID string) (*models.TechnicianLiveLocation, error) {
	item, err := s.repo.LocationForParticipant(ctx, jobID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	return item, err
}

func (s *Service) Events(ctx context.Context, jobID, userID string, limit int) ([]*models.JobEvent, error) {
	items, err := s.repo.EventsForParticipant(ctx, jobID, userID, limit)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	return items, err
}

// PublishJobEvent satisfies jobrequest.EventPublisher. It stores an immutable
// event, sends durable per-user notifications, and publishes one job-scoped SSE
// event. Failures are best-effort so a notification outage never rolls back a
// successfully committed job transition.
func (s *Service) PublishJobEvent(ctx context.Context, job *models.JobRequest, eventType, actorID string) {
	if job == nil {
		return
	}
	data := map[string]interface{}{"job_request_id": job.ID, "status": job.Status,
		"customer_id": job.CustomerID, "assigned_technician_id": job.AssignedTechnicianID,
		"assigned_isp_id": job.AssignedISPID, "actor_id": actorID}
	event := &models.JobEvent{ID: utils.GenerateID(), JobRequestID: job.ID, Type: eventType,
		Status: job.Status, ActorID: actorID, Data: data, CreatedAt: time.Now().UTC()}
	s.hub.PublishTopic("job:"+job.ID, eventType, event)

	if s.notifier == nil {
		return
	}
	title, message := jobEventCopy(job.Status)
	seen := map[string]bool{}
	for _, userID := range []string{job.CustomerID, job.TargetISPID, job.TechnicianID, job.AssignedISPID, job.AssignedTechnicianID} {
		if userID == "" || userID == actorID || seen[userID] {
			continue
		}
		seen[userID] = true
		_ = s.notifier.SendInApp(userID, "job_status", title, message, data)
	}
}

func jobEventCopy(status string) (string, string) {
	switch status {
	case "assigned":
		return "Provider assigned", "A provider has been assigned to your job."
	case "accepted":
		return "Job accepted", "Your job has been accepted."
	case "in_progress":
		return "Work started", "Work on your job is now in progress."
	case "completed":
		return "Job completed", "Your job has been marked complete."
	case "cancelled":
		return "Job cancelled", "Your job has been cancelled."
	default:
		return "Job updated", "Your job status changed to " + status + "."
	}
}

func haversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371
	toRad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*toRad, (lon2-lon1)*toRad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*toRad)*math.Cos(lat2*toRad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// MarshalLocation is used by Redis-backed callers and tests without exposing
// transport details from the service.
func MarshalLocation(item *models.TechnicianLiveLocation) []byte {
	body, _ := json.Marshal(item)
	return body
}
