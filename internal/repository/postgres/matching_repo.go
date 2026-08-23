package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"ispilolite/internal/services/matching"
	"ispilolite/pkg/database"
)

type MatchingRepository struct{ db *sql.DB }

func NewMatchingRepository() *MatchingRepository {
	return &MatchingRepository{db: database.GetWriter()}
}

func (r *MatchingRepository) CreateRequest(ctx context.Context, req *matching.Request) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO service_requests(id,customer_id,request_type,mode,location_id,service_type,description,budget,status,is_available,created_at,updated_at) VALUES($1,$2,'internet','broadcast',NULLIF($3,'')::uuid,$4,$5,$6,$7,true,$8,$8)`, req.ID, req.CustomerID, req.LocationID, req.ServiceType, req.Description, req.Budget, req.Status, req.CreatedAt)
	return err
}

func (r *MatchingRepository) GetRequest(ctx context.Context, id string) (*matching.Request, error) {
	request := &matching.Request{}
	var preferred sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT id,customer_id,COALESCE(location_id::text,''),service_type,description,status,budget,preferred_date,created_at,updated_at FROM service_requests WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&request.ID, &request.CustomerID, &request.LocationID, &request.ServiceType, &request.Description, &request.Status, &request.Budget, &preferred, &request.CreatedAt, &request.UpdatedAt)
	if preferred.Valid {
		request.PreferredDate = &preferred.Time
	}
	return request, err
}

func (r *MatchingRepository) ListRequests(context.Context, string, int, int) ([]*matching.Request, int, error) {
	return nil, 0, fmt.Errorf("matching request listing is not implemented")
}

func (r *MatchingRepository) FindMatches(ctx context.Context, req *matching.Request, opts matching.MatchOptions) ([]matching.Match, error) {
	if opts.RadiusKM <= 0 {
		opts.RadiusKM = 10
	}
	if opts.RadiusKM > 100 {
		opts.RadiusKM = 100
	}
	rows, err := r.db.QueryContext(ctx, `WITH weights AS (SELECT distance_weight,rating_weight,activity_weight,coverage_weight,availability_weight FROM matching_weights WHERE id=true), request_location AS (SELECT l.latitude,l.longitude,l.county FROM locations l WHERE l.id=NULLIF($2,'')::uuid), candidates AS (SELECT u.id,u.role,u.name,COALESCE(u.rating,0) rating,CASE WHEN rl.latitude IS NOT NULL AND u.latitude IS NOT NULL THEN 6371*2*asin(sqrt(power(sin(radians(u.latitude-rl.latitude)/2),2)+cos(radians(rl.latitude))*cos(radians(u.latitude))*power(sin(radians(u.longitude-rl.longitude)/2),2))) ELSE 0 END distance_km,CASE WHEN EXISTS(SELECT 1 FROM isp_coverage_locations c WHERE c.isp_id=u.id AND c.location_id=NULLIF($2,'')::uuid) THEN 1 ELSE 0 END exact_coverage,CASE WHEN u.role='technician' THEN COALESCE(tp.is_available,false)::int ELSE 1 END available,CASE WHEN EXISTS(SELECT 1 FROM service_requests sr WHERE (sr.assigned_isp_id=u.id OR sr.assigned_technician_id=u.id) AND sr.status='completed' AND sr.updated_at>now()-interval '90 days') THEN 1 ELSE .5 END activity FROM users u CROSS JOIN request_location rl LEFT JOIN technician_profiles tp ON tp.technician_id=u.id WHERE u.status='active' AND u.role IN ('isp','technician') AND u.id<>$1), ranked AS (SELECT c.*,GREATEST(0,LEAST(1,w.distance_weight*(1-LEAST(c.distance_km/$3,1))+w.rating_weight*(c.rating/5)+w.activity_weight*c.activity+w.coverage_weight*c.exact_coverage+w.availability_weight*c.available)) score FROM candidates c CROSS JOIN weights w WHERE ($4=false OR c.available=1) AND ($5=0 OR c.rating >= $5) AND c.distance_km <= $3) SELECT id,role,name,rating,distance_km,score FROM ranked ORDER BY score DESC LIMIT 10`, req.CustomerID, req.LocationID, opts.RadiusKM, opts.AvailabilityRequired, opts.MinRating)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []matching.Match{}
	for rows.Next() {
		var m matching.Match
		if err := rows.Scan(&m.ProviderID, &m.ProviderType, &m.Name, &m.Rating, &m.DistanceKM, &m.MatchScore); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, m := range out {
		if _, err = tx.ExecContext(ctx, `INSERT INTO matches(id,request_id,provider_id,provider_type,rating,distance_km,price_estimate,match_score,status,notified_at) VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7,'notified',now()) ON CONFLICT(request_id,provider_id) DO UPDATE SET rating=EXCLUDED.rating,distance_km=EXCLUDED.distance_km,price_estimate=EXCLUDED.price_estimate,match_score=EXCLUDED.match_score,status='notified',notified_at=now()`, req.ID, m.ProviderID, m.ProviderType, m.Rating, m.DistanceKM, m.PriceEstimate, m.MatchScore); err != nil {
			return nil, err
		}
	}
	if len(out) > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE service_requests SET status='open',updated_at=now() WHERE id=$1`, req.ID)
	}
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func (r *MatchingRepository) CreateQuotation(context.Context, *matching.Quotation) error {
	return fmt.Errorf("matching quotation persistence uses quotation service")
}
func (r *MatchingRepository) GetQuotation(context.Context, string) (*matching.Quotation, error) {
	return nil, fmt.Errorf("matching quotation persistence uses quotation service")
}
func (r *MatchingRepository) AcceptQuotation(context.Context, *matching.Quotation) (*matching.Job, error) {
	return nil, fmt.Errorf("matching quotation persistence uses quotation service")
}

func (r *MatchingRepository) ExpireStale(ctx context.Context, olderThan time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE matches SET status='expired' WHERE status='notified' AND notified_at<$1`, olderThan)
	return err
}

func (r *MatchingRepository) Respond(ctx context.Context, matchID, providerID string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE matches SET status='seen',seen_at=now(),responded_at=now() WHERE id=$1 AND provider_id=$2 AND status='notified'`, matchID, providerID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *MatchingRepository) Decline(ctx context.Context, matchID, providerID string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE matches SET status='declined',responded_at=now() WHERE id=$1 AND provider_id=$2 AND status IN ('notified','seen')`, matchID, providerID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
