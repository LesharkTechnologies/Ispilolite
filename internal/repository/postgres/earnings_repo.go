package postgres

import (
	"context"
	"database/sql"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
	"time"
)

type EarningsRepository struct{ db *sql.DB }

func NewEarningsRepository() (*EarningsRepository, error) {
	db, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	return &EarningsRepository{db: db}, nil
}
func (r *EarningsRepository) Dashboard(ctx context.Context, technicianID string, from time.Time, bucket, period string) (*models.EarningsDashboard, error) {
	result := &models.EarningsDashboard{Currency: "KES", Period: period, Series: make([]*models.EarningsPoint, 0), TopISPs: make([]*models.EarningsPartner, 0)}
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT sum(l.amount) FROM payment_ledger l JOIN escrows e ON e.id=l.escrow_id WHERE e.payee_id=$1 AND l.entry_type='released' AND l.created_at>=$2),0),COALESCE((SELECT sum(l.amount) FROM payment_ledger l JOIN escrows e ON e.id=l.escrow_id WHERE e.payee_id=$1 AND l.entry_type='release_requested' AND l.created_at>=$2),0),(SELECT count(*) FROM service_requests WHERE assigned_technician_id=$1 AND status='completed' AND updated_at>=$2),COALESCE((SELECT rating FROM users WHERE id=$1),0)`, technicianID, from).Scan(&result.SettledEarnings, &result.PendingRelease, &result.CompletedJobs, &result.AverageRating)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT date_trunc($3,l.created_at) period_start,sum(l.amount),count(DISTINCT e.request_id) FROM payment_ledger l JOIN escrows e ON e.id=l.escrow_id WHERE e.payee_id=$1 AND l.entry_type='released' AND l.created_at>=$2 GROUP BY 1 ORDER BY 1`, technicianID, from, bucket)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		item := &models.EarningsPoint{}
		if err := rows.Scan(&item.PeriodStart, &item.Amount, &item.Jobs); err != nil {
			rows.Close()
			return nil, err
		}
		result.Series = append(result.Series, item)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	partners, err := r.db.QueryContext(ctx, `SELECT COALESCE(j.assigned_isp_id::text,''),COALESCE(u.name,'Independent'),sum(l.amount),count(DISTINCT j.id) FROM payment_ledger l JOIN escrows e ON e.id=l.escrow_id JOIN service_requests j ON j.id=e.request_id LEFT JOIN users u ON u.id=j.assigned_isp_id WHERE e.payee_id=$1 AND l.entry_type='released' AND l.created_at>=$2 GROUP BY j.assigned_isp_id,u.name ORDER BY sum(l.amount) DESC LIMIT 5`, technicianID, from)
	if err != nil {
		return nil, err
	}
	defer partners.Close()
	for partners.Next() {
		item := &models.EarningsPartner{}
		if err := partners.Scan(&item.ISPID, &item.ISPName, &item.Amount, &item.Jobs); err != nil {
			return nil, err
		}
		result.TopISPs = append(result.TopISPs, item)
	}
	return result, partners.Err()
}
