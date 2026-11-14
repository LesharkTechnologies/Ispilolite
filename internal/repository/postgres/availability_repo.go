package postgres

import (
	"context"
	"database/sql"
	"time"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type AvailabilityRepository struct {
	reader *sql.DB
	writer *sql.DB
}

func NewAvailabilityRepository() (*AvailabilityRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &AvailabilityRepository{reader: reader, writer: writer}, nil
}

func (r *AvailabilityRepository) ReplaceRules(ctx context.Context, technicianID string, rules []*models.AvailabilityRule) error {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var isTechnician bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='technician' AND status='active')`, technicianID).Scan(&isTechnician); err != nil || !isTechnician {
		if err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM technician_availability_rules WHERE technician_id=$1`, technicianID); err != nil {
		return err
	}
	for _, rule := range rules {
		_, err := tx.ExecContext(ctx, `INSERT INTO technician_availability_rules
			(id,technician_id,weekday,start_time,end_time,timezone,active,created_at,updated_at)
			VALUES($1,$2,$3,$4::time,$5::time,$6,$7,$8,$8)`, rule.ID, technicianID, rule.Weekday,
			rule.StartTime, rule.EndTime, rule.Timezone, rule.Active, rule.CreatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *AvailabilityRepository) Rules(ctx context.Context, technicianID string) ([]*models.AvailabilityRule, error) {
	rows, err := r.reader.QueryContext(ctx, `SELECT id,technician_id,weekday,to_char(start_time,'HH24:MI'),
		to_char(end_time,'HH24:MI'),timezone,active,created_at FROM technician_availability_rules
		WHERE technician_id=$1 AND active=true ORDER BY weekday,start_time`, technicianID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.AvailabilityRule, 0)
	for rows.Next() {
		item := &models.AvailabilityRule{}
		if err := rows.Scan(&item.ID, &item.TechnicianID, &item.Weekday, &item.StartTime, &item.EndTime,
			&item.Timezone, &item.Active, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *AvailabilityRepository) UpsertException(ctx context.Context, item *models.AvailabilityException) error {
	_, err := r.writer.ExecContext(ctx, `INSERT INTO technician_availability_exceptions
		(id,technician_id,exception_date,available,start_time,end_time,reason,created_at)
		VALUES($1,$2,$3::date,$4,NULLIF($5,'')::time,NULLIF($6,'')::time,$7,$8)
		ON CONFLICT(technician_id,exception_date) DO UPDATE SET available=EXCLUDED.available,
		start_time=EXCLUDED.start_time,end_time=EXCLUDED.end_time,reason=EXCLUDED.reason`,
		item.ID, item.TechnicianID, item.Date, item.Available, item.StartTime, item.EndTime, item.Reason, item.CreatedAt)
	return err
}

func (r *AvailabilityRepository) Exceptions(ctx context.Context, technicianID string, from, to time.Time) ([]*models.AvailabilityException, error) {
	rows, err := r.reader.QueryContext(ctx, `SELECT id,technician_id,to_char(exception_date,'YYYY-MM-DD'),available,
		COALESCE(to_char(start_time,'HH24:MI'),''),COALESCE(to_char(end_time,'HH24:MI'),''),reason,created_at
		FROM technician_availability_exceptions WHERE technician_id=$1 AND exception_date BETWEEN $2::date AND $3::date`,
		technicianID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.AvailabilityException, 0)
	for rows.Next() {
		item := &models.AvailabilityException{}
		if err := rows.Scan(&item.ID, &item.TechnicianID, &item.Date, &item.Available, &item.StartTime,
			&item.EndTime, &item.Reason, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *AvailabilityRepository) BusyTimes(ctx context.Context, technicianID string, from, to time.Time) ([]time.Time, error) {
	rows, err := r.reader.QueryContext(ctx, `SELECT preferred_date FROM service_requests WHERE preferred_date IS NOT NULL
		AND preferred_date>=$2 AND preferred_date<$3 AND deleted_at IS NULL
		AND status NOT IN ('rejected','completed','cancelled','deleted')
		AND (target_technician_id=$1 OR assigned_technician_id=$1) ORDER BY preferred_date`, technicianID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]time.Time, 0)
	for rows.Next() {
		var value time.Time
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}
