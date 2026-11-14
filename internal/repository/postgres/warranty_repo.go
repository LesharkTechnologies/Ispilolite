package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"ispilolite/internal/models"
	"ispilolite/internal/utils"
	"ispilolite/pkg/database"
)

type WarrantyRepository struct{ reader, writer *sql.DB }

func NewWarrantyRepository() (*WarrantyRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &WarrantyRepository{reader: reader, writer: writer}, nil
}

func (r *WarrantyRepository) CreateClaim(ctx context.Context, claim *models.WarrantyClaim) (*models.JobRequest, error) {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	original, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobSelectColumns+` FROM service_requests WHERE id=$1 AND customer_id=$2 AND status='completed' AND warranty_expires_at>now() FOR UPDATE`, claim.OriginalRequestID, claim.CustomerID))
	if err != nil {
		return nil, err
	}
	photos, _ := json.Marshal(claim.PhotoURLs)
	_, err = tx.ExecContext(ctx, `INSERT INTO warranty_claims(id,original_request_id,customer_id,description,photo_urls,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'open',$6,$6)`, claim.ID, claim.OriginalRequestID, claim.CustomerID, claim.Description, photos, claim.CreatedAt)
	if err != nil {
		return nil, err
	}
	replacement := &models.JobRequest{ID: utils.GenerateID(), CustomerID: claim.CustomerID, RequesterRole: "customer", RequestType: original.RequestType, Mode: "broadcast", LocationID: original.LocationID, County: original.County, Town: original.Town, Village: original.Village, ServiceType: original.ServiceType, SpeedMbps: original.SpeedMbps, Description: "Warranty recurrence: " + claim.Description, Budget: 0, Status: "open", IsAvailable: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	_, err = tx.ExecContext(ctx, `INSERT INTO service_requests(id,customer_id,requester_role,request_type,mode,location_id,county,town,village,service_type,speed_mbps,description,budget,status,is_available,created_at,updated_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7,$8,$9,$10,NULLIF($11,0),$12,0,'open',true,$13,$13)`, replacement.ID, replacement.CustomerID, replacement.RequesterRole, replacement.RequestType, replacement.Mode, replacement.LocationID, replacement.County, replacement.Town, replacement.Village, replacement.ServiceType, replacement.SpeedMbps, replacement.Description, replacement.CreatedAt)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE warranty_claims SET replacement_request_id=$1,status='rematched',updated_at=now() WHERE id=$2`, replacement.ID, claim.ID)
	if err != nil {
		return nil, err
	}
	claim.ReplacementRequestID, claim.Status = replacement.ID, "rematched"
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return replacement, nil
}

func (r *WarrantyRepository) List(ctx context.Context, userID, role string, limit int) ([]*models.WarrantyClaim, error) {
	where := `w.customer_id=$1`
	if role == "isp" {
		where = `(j.target_isp_id=$1 OR j.assigned_isp_id=$1)`
	} else if role == "technician" {
		where = `(j.target_technician_id=$1 OR j.assigned_technician_id=$1)`
	}
	rows, err := r.reader.QueryContext(ctx, `SELECT w.id,w.original_request_id,COALESCE(w.replacement_request_id::text,''),w.customer_id,w.description,w.photo_urls,w.status,w.resolution_note,w.created_at,w.updated_at FROM warranty_claims w JOIN service_requests j ON j.id=w.original_request_id WHERE `+where+` ORDER BY w.created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.WarrantyClaim, 0)
	for rows.Next() {
		item := &models.WarrantyClaim{}
		var photos []byte
		if err := rows.Scan(&item.ID, &item.OriginalRequestID, &item.ReplacementRequestID, &item.CustomerID, &item.Description, &photos, &item.Status, &item.ResolutionNote, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(photos, &item.PhotoURLs)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *WarrantyRepository) Resolve(ctx context.Context, claimID, actorID, status, note string) error {
	result, err := r.writer.ExecContext(ctx, `UPDATE warranty_claims w SET status=$1,resolution_note=$2,updated_at=now() FROM service_requests j WHERE w.id=$3 AND j.id=w.original_request_id AND (j.target_isp_id=$4 OR j.assigned_isp_id=$4 OR j.target_technician_id=$4 OR j.assigned_technician_id=$4) AND w.status IN('rematched','in_progress')`, status, note, claimID, actorID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

var _ = errors.Is
