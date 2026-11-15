package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"ispilolite/internal/models"
	"ispilolite/internal/utils"
	"ispilolite/pkg/database"
	"time"
)

type DisputeRepository struct{ reader, writer *sql.DB }

func NewDisputeRepository() (*DisputeRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &DisputeRepository{reader: reader, writer: writer}, nil
}
func (r *DisputeRepository) Create(ctx context.Context, item *models.Dispute) error {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var escrowID string
	err = tx.QueryRowContext(ctx, `SELECT e.id FROM escrows e JOIN service_requests j ON j.id=e.request_id WHERE e.request_id=$1 AND e.status IN('held','release_pending') AND (j.customer_id=$2 OR j.target_isp_id=$2 OR j.target_technician_id=$2 OR j.assigned_isp_id=$2 OR j.assigned_technician_id=$2) FOR UPDATE`, item.JobRequestID, item.OpenedBy).Scan(&escrowID)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(item.EvidenceURLs)
	_, err = tx.ExecContext(ctx, `INSERT INTO disputes(id,request_id,escrow_id,opened_by,reason,description,evidence_urls,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'open',$8,$8)`, item.ID, item.JobRequestID, escrowID, item.OpenedBy, item.Reason, item.Description, raw, item.CreatedAt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE escrows SET status='disputed',updated_at=now() WHERE id=$1`, escrowID)
	if err != nil {
		return err
	}
	item.EscrowID = escrowID
	return tx.Commit()
}
func scanDispute(row interface{ Scan(...interface{}) error }) (*models.Dispute, error) {
	item := &models.Dispute{}
	var raw []byte
	var resolved sql.NullTime
	err := row.Scan(&item.ID, &item.JobRequestID, &item.EscrowID, &item.OpenedBy, &item.Reason, &item.Description, &raw, &item.Status, &item.Resolution, &item.ResolutionAmount, &item.ResolutionNote, &item.ResolvedBy, &resolved, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		_ = json.Unmarshal(raw, &item.EvidenceURLs)
		if resolved.Valid {
			v := resolved.Time
			item.ResolvedAt = &v
		}
	}
	return item, err
}

const disputeColumns = `d.id,d.request_id,COALESCE(d.escrow_id::text,''),d.opened_by,d.reason,d.description,d.evidence_urls,d.status,d.resolution,d.resolution_amount,d.resolution_note,COALESCE(d.resolved_by::text,''),d.resolved_at,d.created_at,d.updated_at`

func (r *DisputeRepository) Get(ctx context.Context, id, userID, role string) (*models.Dispute, error) {
	where := `d.id=$1 AND ($3='admin' OR j.customer_id=$2 OR j.target_isp_id=$2 OR j.target_technician_id=$2 OR j.assigned_isp_id=$2 OR j.assigned_technician_id=$2)`
	return scanDispute(r.reader.QueryRowContext(ctx, `SELECT `+disputeColumns+` FROM disputes d JOIN service_requests j ON j.id=d.request_id WHERE `+where, id, userID, role))
}
func (r *DisputeRepository) List(ctx context.Context, status string, limit int) ([]*models.Dispute, error) {
	rows, err := r.reader.QueryContext(ctx, `SELECT `+disputeColumns+` FROM disputes d WHERE ($1='' OR d.status=$1) ORDER BY d.created_at LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.Dispute, 0)
	for rows.Next() {
		item, err := scanDispute(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *DisputeRepository) AddComment(ctx context.Context, item *models.DisputeComment) error {
	raw, _ := json.Marshal(item.EvidenceURLs)
	result, err := r.writer.ExecContext(ctx, `INSERT INTO dispute_comments(id,dispute_id,author_id,body,evidence_urls,created_at) SELECT $1,d.id,$3,$4,$5,$6 FROM disputes d JOIN service_requests j ON j.id=d.request_id WHERE d.id=$2 AND d.status IN('open','under_review') AND (j.customer_id=$3 OR j.target_isp_id=$3 OR j.target_technician_id=$3 OR j.assigned_isp_id=$3 OR j.assigned_technician_id=$3)`, item.ID, item.DisputeID, item.AuthorID, item.Body, raw, item.CreatedAt)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *DisputeRepository) Resolve(ctx context.Context, id, adminID, resolution, note string, amount float64) error {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var escrowID, currency string
	var escrowAmount float64
	err = tx.QueryRowContext(ctx, `SELECT e.id,e.amount,e.currency FROM disputes d JOIN escrows e ON e.id=d.escrow_id WHERE d.id=$1 AND d.status IN('open','under_review') FOR UPDATE`, id).Scan(&escrowID, &escrowAmount, &currency)
	if err != nil {
		return err
	}
	if amount <= 0 {
		amount = escrowAmount
	}
	if amount > escrowAmount {
		return errors.New("resolution amount exceeds escrow")
	}
	escrowStatus, entry := "release_pending", "release_requested"
	if resolution == "refund" || resolution == "partial_refund" {
		escrowStatus, entry = "refund_pending", "refund_requested"
	}
	_, err = tx.ExecContext(ctx, `UPDATE disputes SET status='resolved',resolution=$1,resolution_amount=$2,resolution_note=$3,resolved_by=$4,resolved_at=now(),updated_at=now() WHERE id=$5`, resolution, amount, note, adminID, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE escrows SET status=$1,updated_at=now() WHERE id=$2`, escrowStatus, escrowID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_ledger(id,escrow_id,entry_type,amount,currency,reference) VALUES($1,$2,$3,$4,$5,$6)`, utils.GenerateID(), escrowID, entry, amount, currency, "dispute:"+id)
	if err == nil {
		action := "release"
		if resolution == "refund" || resolution == "partial_refund" {
			action = "refund"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO payment_settlement_outbox(id,escrow_id,action,amount,currency) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, utils.GenerateID(), escrowID, action, amount, currency)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

var _ = time.Now
