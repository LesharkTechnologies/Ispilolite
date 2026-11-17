package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ispilolite/internal/models"
	"ispilolite/internal/utils"
	"ispilolite/pkg/database"
)

type JobPairingRepository struct {
	reader *sql.DB
	writer *sql.DB
}

func NewJobPairingRepository() (*JobPairingRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &JobPairingRepository{reader: reader, writer: writer}, nil
}

func NewJobPairingRepositoryWithDB(reader, writer *sql.DB) *JobPairingRepository {
	return &JobPairingRepository{reader: reader, writer: writer}
}

func (r *JobPairingRepository) CreatePairingSession(ctx context.Context, session *models.JobPairingSession) error {
	if r.writer == nil {
		return errors.New("database writer is not configured")
	}
	_, err := r.writer.ExecContext(ctx, `
		INSERT INTO job_pairing_sessions (
			id, job_id, technician_id, customer_id, amount, currency, nonce,
			verification_code, signature, qr_payload, deep_link, status,
			expires_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`, session.ID, session.JobID, session.TechnicianID, session.CustomerID, session.Amount, session.Currency,
		session.Nonce, session.VerificationCode, session.Signature, session.QRPayload, session.DeepLink,
		session.Status, session.ExpiresAt, session.CreatedAt, session.UpdatedAt)
	return err
}

func (r *JobPairingRepository) GetPairingSession(ctx context.Context, jobID, key string) (*models.JobPairingSession, error) {
	if r.reader == nil {
		return nil, errors.New("database reader is not configured")
	}
	s := &models.JobPairingSession{}
	var confirmedAt sql.NullTime
	var confirmedBy, notes sql.NullString
	var rating sql.NullInt32

	err := r.reader.QueryRowContext(ctx, `
		SELECT id, job_id, technician_id, customer_id, amount, currency, nonce,
		       verification_code, signature, qr_payload, deep_link, status,
		       expires_at, confirmed_at, confirmed_by, notes, rating, created_at, updated_at
		FROM job_pairing_sessions
		WHERE job_id = $1 AND (nonce = $2 OR id::text = $2 OR verification_code = $2)
		ORDER BY created_at DESC
		LIMIT 1
	`, jobID, key).Scan(
		&s.ID, &s.JobID, &s.TechnicianID, &s.CustomerID, &s.Amount, &s.Currency, &s.Nonce,
		&s.VerificationCode, &s.Signature, &s.QRPayload, &s.DeepLink, &s.Status,
		&s.ExpiresAt, &confirmedAt, &confirmedBy, &notes, &rating, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if confirmedAt.Valid {
		v := confirmedAt.Time
		s.ConfirmedAt = &v
	}
	if confirmedBy.Valid {
		s.ConfirmedBy = confirmedBy.String
	}
	if notes.Valid {
		s.Notes = notes.String
	}
	if rating.Valid {
		rVal := int(rating.Int32)
		s.Rating = &rVal
	}
	// Check expiry in place
	if s.Status == "pending" && time.Now().UTC().After(s.ExpiresAt) {
		s.Status = "expired"
		if r.writer != nil {
			_, _ = r.writer.ExecContext(ctx, `UPDATE job_pairing_sessions SET status = 'expired', updated_at = now() WHERE id = $1`, s.ID)
		}
	}
	return s, nil
}

func (r *JobPairingRepository) GetActivePairingSession(ctx context.Context, jobID string) (*models.JobPairingSession, error) {
	if r.reader == nil {
		return nil, errors.New("database reader is not configured")
	}
	s := &models.JobPairingSession{}
	var confirmedAt sql.NullTime
	var confirmedBy, notes sql.NullString
	var rating sql.NullInt32

	err := r.reader.QueryRowContext(ctx, `
		SELECT id, job_id, technician_id, customer_id, amount, currency, nonce,
		       verification_code, signature, qr_payload, deep_link, status,
		       expires_at, confirmed_at, confirmed_by, notes, rating, created_at, updated_at
		FROM job_pairing_sessions
		WHERE job_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, jobID).Scan(
		&s.ID, &s.JobID, &s.TechnicianID, &s.CustomerID, &s.Amount, &s.Currency, &s.Nonce,
		&s.VerificationCode, &s.Signature, &s.QRPayload, &s.DeepLink, &s.Status,
		&s.ExpiresAt, &confirmedAt, &confirmedBy, &notes, &rating, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if confirmedAt.Valid {
		v := confirmedAt.Time
		s.ConfirmedAt = &v
	}
	if confirmedBy.Valid {
		s.ConfirmedBy = confirmedBy.String
	}
	if notes.Valid {
		s.Notes = notes.String
	}
	if rating.Valid {
		rVal := int(rating.Int32)
		s.Rating = &rVal
	}
	if s.Status == "pending" && time.Now().UTC().After(s.ExpiresAt) {
		s.Status = "expired"
		if r.writer != nil {
			_, _ = r.writer.ExecContext(ctx, `UPDATE job_pairing_sessions SET status = 'expired', updated_at = now() WHERE id = $1`, s.ID)
		}
	}
	return s, nil
}

func (r *JobPairingRepository) ConfirmPairingSession(ctx context.Context, jobID, sessionID, customerID, notes string, rating int) (*models.JobPairingSession, error) {
	if r.writer == nil {
		return nil, errors.New("database writer is not configured")
	}
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	s := &models.JobPairingSession{}
	var confirmedAt sql.NullTime
	var confirmedBy, storedNotes sql.NullString
	var storedRating sql.NullInt32

	err = tx.QueryRowContext(ctx, `
		SELECT id, job_id, technician_id, customer_id, amount, currency, nonce,
		       verification_code, signature, qr_payload, deep_link, status,
		       expires_at, confirmed_at, confirmed_by, notes, rating, created_at, updated_at
		FROM job_pairing_sessions
		WHERE id = $1 AND job_id = $2
		FOR UPDATE
	`, sessionID, jobID).Scan(
		&s.ID, &s.JobID, &s.TechnicianID, &s.CustomerID, &s.Amount, &s.Currency, &s.Nonce,
		&s.VerificationCode, &s.Signature, &s.QRPayload, &s.DeepLink, &s.Status,
		&s.ExpiresAt, &confirmedAt, &confirmedBy, &storedNotes, &storedRating, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if s.Status == "confirmed" {
		return nil, errors.New("pairing session already confirmed")
	}
	if s.Status == "expired" || time.Now().UTC().After(s.ExpiresAt) {
		_, _ = tx.ExecContext(ctx, `UPDATE job_pairing_sessions SET status = 'expired', updated_at = now() WHERE id = $1`, s.ID)
		return nil, errors.New("pairing session has expired")
	}

	now := time.Now().UTC()
	var ratingVal interface{} = nil
	if rating >= 1 && rating <= 5 {
		ratingVal = rating
		s.Rating = &rating
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE job_pairing_sessions
		SET status = 'confirmed', confirmed_at = $1, confirmed_by = $2, notes = $3, rating = $4, updated_at = $1
		WHERE id = $5
	`, now, customerID, notes, ratingVal, s.ID)
	if err != nil {
		return nil, err
	}
	s.Status = "confirmed"
	s.ConfirmedAt = &now
	s.ConfirmedBy = customerID
	s.Notes = notes

	// Ensure installation_proofs record is approved
	_, err = tx.ExecContext(ctx, `
		INSERT INTO installation_proofs(
			id, request_id, technician_id, check_in_at, check_out_at, status, customer_confirmed_at, customer_confirmed_by, notes, updated_at
		) VALUES ($1, $2, $3, $4, $4, 'approved', $4, $5, $6, $4)
		ON CONFLICT (request_id) DO UPDATE SET
			customer_confirmed_at = EXCLUDED.customer_confirmed_at,
			customer_confirmed_by = EXCLUDED.customer_confirmed_by,
			status = 'approved',
			updated_at = EXCLUDED.updated_at
	`, utils.GenerateID(), jobID, s.TechnicianID, now, customerID, notes)
	if err != nil {
		return nil, err
	}

	// Move service_requests status to completed
	_, err = tx.ExecContext(ctx, `
		UPDATE service_requests
		SET status = 'completed', completed_at = COALESCE(completed_at, $1), is_available = false, updated_at = $1
		WHERE id = $2
	`, now, jobID)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s, nil
}
