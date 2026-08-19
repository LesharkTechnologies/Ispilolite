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

func NewMatchingRepository() *MatchingRepository { return &MatchingRepository{db: database.GetWriter()} }

func (r *MatchingRepository) CreateRequest(ctx context.Context, req *matching.Request) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO service_requests(id,customer_id,request_type,mode,location_id,service_type,description,budget,status,is_available,created_at,updated_at) VALUES($1,$2,'internet','broadcast',NULLIF($3,'')::uuid,$4,$5,$6,$7,true,$8,$8)`, req.ID, req.CustomerID, req.LocationID, req.ServiceType, req.Description, req.Budget, req.Status, req.CreatedAt)
	return err
}

func (r *MatchingRepository) GetRequest(ctx context.Context, id string) (*matching.Request, error) {
	request := &matching.Request{}
	var preferred sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT id,customer_id,COALESCE(location_id::text,''),service_type,description,status,budget,preferred_date,created_at,updated_at FROM service_requests WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&request.ID,&request.CustomerID,&request.LocationID,&request.ServiceType,&request.Description,&request.Status,&request.Budget,&preferred,&request.CreatedAt,&request.UpdatedAt)
	if preferred.Valid { request.PreferredDate=&preferred.Time }
	return request, err
}

func (r *MatchingRepository) ListRequests(context.Context,string,int,int)([]*matching.Request,int,error){return nil,0,fmt.Errorf("matching request listing is not implemented")}

func (r *MatchingRepository) FindMatches(ctx context.Context, req *matching.Request, opts matching.MatchOptions) ([]matching.Match,error) {
	if opts.RadiusKM<=0 { opts.RadiusKM=10 }; if opts.RadiusKM>100 {opts.RadiusKM=100}
	rows,err:=r.db.QueryContext(ctx,`SELECT u.id,u.role,u.name,COALESCE(u.rating,0),0::numeric FROM users u WHERE u.status='active' AND u.role IN ('isp','technician') AND u.id<>$1 AND ($2='' OR u.role=$2) ORDER BY u.rating DESC LIMIT 10`,req.CustomerID, "")
	if err!=nil{return nil,err};defer rows.Close();out:=[]matching.Match{};for rows.Next(){var m matching.Match;var role string;if err:=rows.Scan(&m.ProviderID,&role,&m.Name,&m.Rating,&m.DistanceKM);err!=nil{return nil,err};if role=="technician" && opts.AvailabilityRequired { /* availability is role-specific and filtered by provider status upstream */ };m.MatchScore=0.3+(m.Rating/5)*0.7;if m.MatchScore<opts.MinRating/5 {continue};out=append(out,m)};return out,rows.Err()
}

func (r *MatchingRepository) CreateQuotation(context.Context,*matching.Quotation)error{return fmt.Errorf("matching quotation persistence uses quotation service")}
func (r *MatchingRepository) GetQuotation(context.Context,string)(*matching.Quotation,error){return nil,fmt.Errorf("matching quotation persistence uses quotation service")}
func (r *MatchingRepository) AcceptQuotation(context.Context,*matching.Quotation)(*matching.Job,error){return nil,fmt.Errorf("matching quotation persistence uses quotation service")}

func (r *MatchingRepository) ExpireStale(ctx context.Context, olderThan time.Time) error { _,err:=r.db.ExecContext(ctx,`UPDATE matches SET status='expired' WHERE status='notified' AND notified_at<$1`,olderThan);return err }
