package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ispilolite/internal/models"
	"ispilolite/internal/utils"
	"ispilolite/pkg/database"
)

type PaymentRepository struct {
	reader *sql.DB
	writer *sql.DB
}

func NewPaymentRepository() (*PaymentRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &PaymentRepository{reader: reader, writer: writer}, nil
}

const paymentColumns = `id,request_id,payer_id,payee_id,provider,external_id,phone,currency,amount,status,idempotency_key,failure_reason,metadata,created_at,updated_at`

func scanPayment(row interface{ Scan(...interface{}) error }) (*models.PaymentIntent, error) {
	item := &models.PaymentIntent{}
	var metadata []byte
	err := row.Scan(&item.ID, &item.JobRequestID, &item.PayerID, &item.PayeeID, &item.Provider, &item.ExternalID, &item.Phone, &item.Currency, &item.Amount, &item.Status, &item.IdempotencyKey, &item.FailureReason, &metadata, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	return item, err
}

func (r *PaymentRepository) CreateIntent(ctx context.Context, item *models.PaymentIntent) (*models.PaymentIntent, bool, error) {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if existing, getErr := scanPayment(tx.QueryRowContext(ctx, `SELECT `+paymentColumns+` FROM payment_intents WHERE payer_id=$1 AND idempotency_key=$2 FOR UPDATE`, item.PayerID, item.IdempotencyKey)); getErr == nil {
		return existing, false, nil
	} else if !errors.Is(getErr, sql.ErrNoRows) {
		return nil, false, getErr
	}
	var payeeID string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(assigned_isp_id,assigned_technician_id,target_isp_id,target_technician_id)::text FROM service_requests WHERE id=$1 AND customer_id=$2 AND deleted_at IS NULL AND status NOT IN ('completed','cancelled','deleted')`, item.JobRequestID, item.PayerID).Scan(&payeeID)
	if err != nil {
		return nil, false, err
	}
	item.PayeeID = payeeID
	metadata, _ := json.Marshal(item.Metadata)
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_intents(id,request_id,payer_id,payee_id,provider,phone,currency,amount,status,idempotency_key,metadata,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'created',$9,$10,$11,$11)`, item.ID, item.JobRequestID, item.PayerID, item.PayeeID, item.Provider, item.Phone, item.Currency, item.Amount, item.IdempotencyKey, metadata, item.CreatedAt)
	if err != nil {
		return nil, false, err
	}
	for _, milestone := range item.Milestones {
		_, err = tx.ExecContext(ctx, `INSERT INTO escrow_milestones(id,payment_id,title,amount,sequence,status,created_at) VALUES($1,$2,$3,$4,$5,'pending',$6)`, milestone.ID, item.ID, milestone.Title, milestone.Amount, milestone.Sequence, item.CreatedAt)
		if err != nil {
			return nil, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, true, nil
}

func (r *PaymentRepository) SetProviderReference(ctx context.Context, id, externalID, status string, metadata map[string]interface{}) error {
	raw, _ := json.Marshal(metadata)
	result, err := r.writer.ExecContext(ctx, `UPDATE payment_intents SET external_id=$1,status=$2,metadata=metadata||$3::jsonb,updated_at=now() WHERE id=$4`, externalID, status, raw, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *PaymentRepository) ApplyProviderEvent(ctx context.Context, provider string, event *models.PaymentProviderEvent, raw []byte) (*models.Escrow, bool, error) {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	webhookID := utils.GenerateID()
	result, err := tx.ExecContext(ctx, `INSERT INTO payment_webhook_events(id,provider,external_event_id,external_payment_id,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(provider,external_event_id) DO NOTHING`, webhookID, provider, event.EventID, event.ExternalID, string(raw))
	if err != nil {
		return nil, false, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil, false, nil
	}
	intent, err := scanPayment(tx.QueryRowContext(ctx, `SELECT `+paymentColumns+` FROM payment_intents WHERE provider=$1 AND external_id=$2 FOR UPDATE`, provider, event.ExternalID))
	if err != nil {
		return nil, false, err
	}
	if intent.Status == "succeeded" {
		return nil, false, tx.Commit()
	}
	if event.Status != "succeeded" {
		_, err = tx.ExecContext(ctx, `UPDATE payment_intents SET status='failed',failure_reason=$1,updated_at=now() WHERE id=$2`, event.FailureReason, intent.ID)
		if err != nil {
			return nil, false, err
		}
		return nil, false, tx.Commit()
	}
	if event.Amount > 0 && event.Amount != intent.Amount {
		return nil, false, fmt.Errorf("provider amount does not match payment intent")
	}
	_, err = tx.ExecContext(ctx, `UPDATE payment_intents SET status='succeeded',updated_at=now() WHERE id=$1`, intent.ID)
	if err != nil {
		return nil, false, err
	}
	escrow := &models.Escrow{ID: utils.GenerateID(), PaymentID: intent.ID, JobRequestID: intent.JobRequestID, PayerID: intent.PayerID, PayeeID: intent.PayeeID, Currency: intent.Currency, Amount: intent.Amount, Status: "held", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	escrow.HeldAt = &escrow.CreatedAt
	_, err = tx.ExecContext(ctx, `INSERT INTO escrows(id,payment_id,request_id,payer_id,payee_id,currency,amount,status,held_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'held',$8,$8,$8) ON CONFLICT(payment_id) DO NOTHING`, escrow.ID, escrow.PaymentID, escrow.JobRequestID, escrow.PayerID, escrow.PayeeID, escrow.Currency, escrow.Amount, escrow.CreatedAt)
	if err != nil {
		return nil, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE escrow_milestones SET escrow_id=$1,status='funded' WHERE payment_id=$2`, escrow.ID, intent.ID)
	if err != nil {
		return nil, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_ledger(id,escrow_id,entry_type,amount,currency,reference) VALUES($1,$2,'hold',$3,$4,$5)`, utils.GenerateID(), escrow.ID, escrow.Amount, escrow.Currency, event.Receipt)
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return escrow, true, nil
}

func (r *PaymentRepository) EscrowForParticipant(ctx context.Context, jobID, userID string) (*models.Escrow, error) {
	item := &models.Escrow{}
	var held, released, refunded sql.NullTime
	err := r.reader.QueryRowContext(ctx, `SELECT e.id,e.payment_id,e.request_id,e.payer_id,e.payee_id,e.currency,e.amount,e.status,e.held_at,e.released_at,e.refunded_at,e.created_at,e.updated_at FROM escrows e JOIN service_requests j ON j.id=e.request_id WHERE e.request_id=$1 AND (j.customer_id=$2 OR j.target_isp_id=$2 OR j.target_technician_id=$2 OR j.assigned_isp_id=$2 OR j.assigned_technician_id=$2) ORDER BY e.created_at DESC LIMIT 1`, jobID, userID).Scan(&item.ID, &item.PaymentID, &item.JobRequestID, &item.PayerID, &item.PayeeID, &item.Currency, &item.Amount, &item.Status, &held, &released, &refunded, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if held.Valid {
		v := held.Time
		item.HeldAt = &v
	}
	if released.Valid {
		v := released.Time
		item.ReleasedAt = &v
	}
	if refunded.Valid {
		v := refunded.Time
		item.RefundedAt = &v
	}
	rows, err := r.reader.QueryContext(ctx, `SELECT id,payment_id,COALESCE(escrow_id::text,''),title,amount,sequence,status,approved_at,released_at,created_at FROM escrow_milestones WHERE payment_id=$1 ORDER BY sequence`, item.PaymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m := &models.EscrowMilestone{}
		var approved, releasedAt sql.NullTime
		if err := rows.Scan(&m.ID, &m.PaymentID, &m.EscrowID, &m.Title, &m.Amount, &m.Sequence, &m.Status, &approved, &releasedAt, &m.CreatedAt); err != nil {
			return nil, err
		}
		if approved.Valid {
			v := approved.Time
			m.ApprovedAt = &v
		}
		if releasedAt.Valid {
			v := releasedAt.Time
			m.ReleasedAt = &v
		}
		item.Milestones = append(item.Milestones, m)
	}
	return item, rows.Err()
}

func (r *PaymentRepository) RequestRelease(ctx context.Context, escrowID, payerID, milestoneID string) (*models.EscrowMilestone, error) {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var jobID, currency, status string
	if err = tx.QueryRowContext(ctx, `SELECT e.request_id,e.currency,e.status FROM escrows e JOIN service_requests j ON j.id=e.request_id WHERE e.id=$1 AND e.payer_id=$2 AND j.status='completed' FOR UPDATE`, escrowID, payerID).Scan(&jobID, &currency, &status); err != nil {
		return nil, err
	}
	if status != "held" {
		return nil, fmt.Errorf("escrow is not held")
	}
	if milestoneID == "" {
		var funded int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM escrow_milestones WHERE escrow_id=$1 AND status='funded'`, escrowID).Scan(&funded); err != nil {
			return nil, err
		}
		if funded > 1 {
			return nil, fmt.Errorf("milestone_id is required when escrow has multiple funded milestones")
		}
	}
	query := `UPDATE escrow_milestones SET status='release_pending',approved_at=now() WHERE escrow_id=$1 AND status='funded'`
	args := []interface{}{escrowID}
	if milestoneID != "" {
		query += ` AND id=$2`
		args = append(args, milestoneID)
	}
	query += ` RETURNING id,payment_id,escrow_id,title,amount,sequence,status,approved_at,released_at,created_at`
	m := &models.EscrowMilestone{}
	var approved, released sql.NullTime
	if err = tx.QueryRowContext(ctx, query, args...).Scan(&m.ID, &m.PaymentID, &m.EscrowID, &m.Title, &m.Amount, &m.Sequence, &m.Status, &approved, &released, &m.CreatedAt); err != nil {
		return nil, err
	}
	if approved.Valid {
		v := approved.Time
		m.ApprovedAt = &v
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_ledger(id,escrow_id,milestone_id,entry_type,amount,currency,reference) VALUES($1,$2,$3,'release_requested',$4,$5,$6)`, utils.GenerateID(), escrowID, m.ID, m.Amount, currency, "job:"+jobID)
	if err != nil {
		return nil, err
	}
	var remaining int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM escrow_milestones WHERE escrow_id=$1 AND status='funded'`, escrowID).Scan(&remaining); err != nil {
		return nil, err
	}
	if remaining == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE escrows SET status='release_pending',updated_at=now() WHERE id=$1`, escrowID)
		if err != nil {
			return nil, err
		}
	}
	return m, tx.Commit()
}

func (r *PaymentRepository) ConfirmSettlement(ctx context.Context, escrowID, milestoneID, action, reference string, amount float64) error {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currency, status string
	var escrowAmount float64
	if err = tx.QueryRowContext(ctx, `SELECT currency,status,amount FROM escrows WHERE id=$1 FOR UPDATE`, escrowID).Scan(&currency, &status, &escrowAmount); err != nil {
		return err
	}
	if action == "refunded" {
		if status != "refund_pending" && status != "disputed" {
			return fmt.Errorf("escrow is not awaiting refund")
		}
		if amount <= 0 {
			amount = escrowAmount
		}
		if amount > escrowAmount {
			return fmt.Errorf("refund exceeds escrow")
		}
		_, err = tx.ExecContext(ctx, `UPDATE escrow_milestones SET status='refunded' WHERE escrow_id=$1 AND status<>'released'`, escrowID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE escrows SET status='refunded',refunded_at=now(),updated_at=now() WHERE id=$1`, escrowID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO payment_ledger(id,escrow_id,entry_type,amount,currency,reference) VALUES($1,$2,'refunded',$3,$4,$5)`, utils.GenerateID(), escrowID, amount, currency, reference)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if action != "released" {
		return fmt.Errorf("unknown settlement action")
	}
	if milestoneID == "" {
		var pending int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM escrow_milestones WHERE escrow_id=$1 AND status='release_pending'`, escrowID).Scan(&pending); err != nil {
			return err
		}
		if pending > 1 {
			return fmt.Errorf("milestone_id is required for multi-milestone settlement")
		}
	}
	query := `UPDATE escrow_milestones SET status='released',released_at=now() WHERE escrow_id=$1 AND status='release_pending'`
	args := []interface{}{escrowID}
	if milestoneID != "" {
		query += ` AND id=$2`
		args = append(args, milestoneID)
	}
	query += ` RETURNING id,amount`
	var settledID string
	var settledAmount float64
	if err = tx.QueryRowContext(ctx, query, args...).Scan(&settledID, &settledAmount); err != nil {
		return err
	}
	if amount > 0 && amount != settledAmount {
		return fmt.Errorf("settlement amount mismatch")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_ledger(id,escrow_id,milestone_id,entry_type,amount,currency,reference) VALUES($1,$2,$3,'released',$4,$5,$6)`, utils.GenerateID(), escrowID, settledID, settledAmount, currency, reference)
	if err != nil {
		return err
	}
	var remaining int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM escrow_milestones WHERE escrow_id=$1 AND status<>'released'`, escrowID).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE escrows SET status='released',released_at=now(),updated_at=now() WHERE id=$1`, escrowID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
