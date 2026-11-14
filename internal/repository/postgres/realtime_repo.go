package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

// RealtimeRepository persists job event history and the last technician
// location. Authorization is enforced inside every read/write query.
type RealtimeRepository struct {
	reader *sql.DB
	writer *sql.DB
}

func NewRealtimeRepository() (*RealtimeRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &RealtimeRepository{reader: reader, writer: writer}, nil
}

// JobForParticipant returns the job only when userID is a customer, target, or
// assigned provider on it.
func (r *RealtimeRepository) JobForParticipant(ctx context.Context, jobID, userID string) (*models.JobRequest, error) {
	const query = `SELECT ` + jobSelectColumns + ` FROM service_requests
		WHERE id=$1 AND deleted_at IS NULL AND
		(customer_id=$2 OR target_isp_id=$2 OR target_technician_id=$2 OR assigned_isp_id=$2 OR assigned_technician_id=$2)`
	job, err := scanJob(r.reader.QueryRowContext(ctx, query, jobID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	return job, err
}

// UpsertLocation accepts positions only from the technician attached to an
// active job. It returns the job so the service can notify its customer.
func (r *RealtimeRepository) UpsertLocation(ctx context.Context, technicianID string, location *models.TechnicianLiveLocation) (*models.JobRequest, error) {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	const allowed = `SELECT ` + jobSelectColumns + ` FROM service_requests
		WHERE id=$1 AND deleted_at IS NULL
		AND (assigned_technician_id=$2 OR target_technician_id=$2)
		AND status IN ('assigned','accepted','in_progress') FOR UPDATE`
	job, err := scanJob(tx.QueryRowContext(ctx, allowed, location.JobRequestID, technicianID))
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO technician_live_locations
		(technician_id,request_id,latitude,longitude,accuracy_m,heading,speed_kph,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT(technician_id) DO UPDATE SET request_id=EXCLUDED.request_id,
		latitude=EXCLUDED.latitude,longitude=EXCLUDED.longitude,accuracy_m=EXCLUDED.accuracy_m,
		heading=EXCLUDED.heading,speed_kph=EXCLUDED.speed_kph,updated_at=EXCLUDED.updated_at`,
		technicianID, location.JobRequestID, location.Lat, location.Lon, location.AccuracyM,
		location.Heading, location.SpeedKPH, location.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

func (r *RealtimeRepository) LocationForParticipant(ctx context.Context, jobID, userID string) (*models.TechnicianLiveLocation, error) {
	const query = `SELECT l.technician_id,l.request_id,l.latitude,l.longitude,l.accuracy_m,l.heading,l.speed_kph,l.updated_at
		FROM technician_live_locations l JOIN service_requests j ON j.id=l.request_id
		WHERE l.request_id=$1 AND j.deleted_at IS NULL AND
		(j.customer_id=$2 OR j.target_isp_id=$2 OR j.target_technician_id=$2 OR j.assigned_isp_id=$2 OR j.assigned_technician_id=$2)`
	item := &models.TechnicianLiveLocation{}
	err := r.reader.QueryRowContext(ctx, query, jobID, userID).Scan(&item.TechnicianID, &item.JobRequestID,
		&item.Lat, &item.Lon, &item.AccuracyM, &item.Heading, &item.SpeedKPH, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.IsOnline = time.Since(item.UpdatedAt) <= 2*time.Minute
	return item, nil
}

func (r *RealtimeRepository) Destination(ctx context.Context, jobID string) (lat, lon float64, ok bool, err error) {
	err = r.reader.QueryRowContext(ctx, `SELECT l.latitude,l.longitude FROM service_requests j
		JOIN locations l ON l.id=j.location_id WHERE j.id=$1 AND l.latitude IS NOT NULL AND l.longitude IS NOT NULL`, jobID).Scan(&lat, &lon)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	return lat, lon, err == nil, err
}

func (r *RealtimeRepository) AddEvent(ctx context.Context, event *models.JobEvent) error {
	data, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}
	_, err = r.writer.ExecContext(ctx, `INSERT INTO service_request_events
		(id,request_id,event_type,status,actor_id,data,created_at)
		VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7)`, event.ID, event.JobRequestID,
		event.Type, event.Status, event.ActorID, data, event.CreatedAt)
	return err
}

func (r *RealtimeRepository) EventsForParticipant(ctx context.Context, jobID, userID string, limit int) ([]*models.JobEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.reader.QueryContext(ctx, `SELECT e.id,e.request_id,e.event_type,e.status,
		COALESCE(e.actor_id::text,''),e.data,e.created_at FROM service_request_events e
		JOIN service_requests j ON j.id=e.request_id WHERE e.request_id=$1 AND
		(j.customer_id=$2 OR j.target_isp_id=$2 OR j.target_technician_id=$2 OR j.assigned_isp_id=$2 OR j.assigned_technician_id=$2)
		ORDER BY e.created_at DESC LIMIT $3`, jobID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.JobEvent, 0)
	for rows.Next() {
		item := &models.JobEvent{}
		var raw []byte
		if err := rows.Scan(&item.ID, &item.JobRequestID, &item.Type, &item.Status, &item.ActorID, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Data); err != nil {
			return nil, fmt.Errorf("decode job event: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
