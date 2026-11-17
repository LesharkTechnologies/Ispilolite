package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

var (
	ErrVerificationNotFound = errors.New("identity verification not found")
	ErrVerificationClosed   = errors.New("identity verification has already been reviewed")
)

type IdentityVerificationRepository struct {
	reader *sql.DB
	writer *sql.DB
}

func NewIdentityVerificationRepository() (*IdentityVerificationRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &IdentityVerificationRepository{reader: reader, writer: writer}, nil
}

// CreateVerification inserts a new verification record and sets user/technician status to pending.
func (r *IdentityVerificationRepository) CreateVerification(ctx context.Context, item *models.IdentityVerification) error {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	if item.SubmittedAt.IsZero() {
		item.SubmittedAt = now
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now

	if item.DocumentType == "" {
		item.DocumentType = "national_id"
	}
	if item.Status == "" {
		item.Status = models.VerificationStatusPending
	}
	if item.VerificationBadge == "" {
		item.VerificationBadge = models.VerificationBadgeUnverified
	}

	query := `
		INSERT INTO identity_verifications (
			id, user_id, phone, id_photo_url, id_photo_back_url, id_number,
			document_type, status, verification_badge, verified_name, rejection_reason,
			notes, submitted_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $13, $14, $15
		)
	`
	_, err = tx.ExecContext(ctx, query,
		item.ID, item.UserID, item.Phone, item.IDPhotoURL, item.IDPhotoBackURL, item.IDNumber,
		item.DocumentType, item.Status, item.VerificationBadge, item.VerifiedName, item.RejectionReason,
		item.Notes, item.SubmittedAt, item.CreatedAt, item.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert identity_verification: %w", err)
	}

	// Update user and related role profiles to 'pending'
	_, _ = tx.ExecContext(ctx, `UPDATE users SET identity_status='pending', updated_at=now() WHERE id=$1`, item.UserID)
	_, _ = tx.ExecContext(ctx, `UPDATE technicians SET identity_status='pending', updated_at=now() WHERE id=$1 OR user_id=$1`, item.UserID)
	_, _ = tx.ExecContext(ctx, `UPDATE technician_profiles SET identity_status='pending', updated_at=now() WHERE technician_id=$1`, item.UserID)
	_, _ = tx.ExecContext(ctx, `UPDATE isps SET identity_status='pending', updated_at=now() WHERE id=$1`, item.UserID)

	return tx.Commit()
}

// GetVerificationByID returns a single verification by ID with joined user details.
func (r *IdentityVerificationRepository) GetVerificationByID(ctx context.Context, id string) (*models.IdentityVerification, error) {
	query := `
		SELECT 
			v.id, v.user_id, v.phone, v.id_photo_url, v.id_photo_back_url, v.id_number,
			v.document_type, v.status, v.verification_badge, v.verified_name, v.rejection_reason,
			v.notes, v.submitted_at, COALESCE(v.reviewed_by::text, ''), v.reviewed_at,
			v.created_at, v.updated_at,
			COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.role, '')
		FROM identity_verifications v
		LEFT JOIN users u ON u.id = v.user_id
		WHERE v.id = $1
	`
	v := &models.IdentityVerification{}
	err := r.reader.QueryRowContext(ctx, query, id).Scan(
		&v.ID, &v.UserID, &v.Phone, &v.IDPhotoURL, &v.IDPhotoBackURL, &v.IDNumber,
		&v.DocumentType, &v.Status, &v.VerificationBadge, &v.VerifiedName, &v.RejectionReason,
		&v.Notes, &v.SubmittedAt, &v.ReviewedBy, &v.ReviewedAt,
		&v.CreatedAt, &v.UpdatedAt,
		&v.UserName, &v.UserEmail, &v.UserRole,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVerificationNotFound
	}
	if err != nil {
		return nil, err
	}
	v.BadgeLabel = models.FormatVerificationBadge(v.VerificationBadge)
	return v, nil
}

// GetLatestVerificationByUserID returns the most recently submitted verification for a user.
func (r *IdentityVerificationRepository) GetLatestVerificationByUserID(ctx context.Context, userID string) (*models.IdentityVerification, error) {
	query := `
		SELECT 
			v.id, v.user_id, v.phone, v.id_photo_url, v.id_photo_back_url, v.id_number,
			v.document_type, v.status, v.verification_badge, v.verified_name, v.rejection_reason,
			v.notes, v.submitted_at, COALESCE(v.reviewed_by::text, ''), v.reviewed_at,
			v.created_at, v.updated_at,
			COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.role, '')
		FROM identity_verifications v
		LEFT JOIN users u ON u.id = v.user_id
		WHERE v.user_id = $1
		ORDER BY v.submitted_at DESC
		LIMIT 1
	`
	v := &models.IdentityVerification{}
	err := r.reader.QueryRowContext(ctx, query, userID).Scan(
		&v.ID, &v.UserID, &v.Phone, &v.IDPhotoURL, &v.IDPhotoBackURL, &v.IDNumber,
		&v.DocumentType, &v.Status, &v.VerificationBadge, &v.VerifiedName, &v.RejectionReason,
		&v.Notes, &v.SubmittedAt, &v.ReviewedBy, &v.ReviewedAt,
		&v.CreatedAt, &v.UpdatedAt,
		&v.UserName, &v.UserEmail, &v.UserRole,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVerificationNotFound
	}
	if err != nil {
		return nil, err
	}
	v.BadgeLabel = models.FormatVerificationBadge(v.VerificationBadge)
	return v, nil
}

// ListVerifications returns paginated verifications matching status or badge filters.
func (r *IdentityVerificationRepository) ListVerifications(ctx context.Context, status, badge string, limit, offset int) ([]*models.IdentityVerification, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	whereClauses := []string{"1=1"}
	args := []interface{}{}
	argIdx := 1

	if strings.TrimSpace(status) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("v.status = $%d", argIdx))
		args = append(args, strings.ToLower(strings.TrimSpace(status)))
		argIdx++
	}
	if strings.TrimSpace(badge) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("v.verification_badge = $%d", argIdx))
		args = append(args, strings.ToLower(strings.TrimSpace(badge)))
		argIdx++
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	countQuery := fmt.Sprintf(`SELECT count(*) FROM identity_verifications v WHERE %s`, whereSQL)
	var total int
	if err := r.reader.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listQuery := fmt.Sprintf(`
		SELECT 
			v.id, v.user_id, v.phone, v.id_photo_url, v.id_photo_back_url, v.id_number,
			v.document_type, v.status, v.verification_badge, v.verified_name, v.rejection_reason,
			v.notes, v.submitted_at, COALESCE(v.reviewed_by::text, ''), v.reviewed_at,
			v.created_at, v.updated_at,
			COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.role, '')
		FROM identity_verifications v
		LEFT JOIN users u ON u.id = v.user_id
		WHERE %s
		ORDER BY v.submitted_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	args = append(args, limit, offset)
	rows, err := r.reader.QueryContext(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []*models.IdentityVerification{}
	for rows.Next() {
		v := &models.IdentityVerification{}
		if err := rows.Scan(
			&v.ID, &v.UserID, &v.Phone, &v.IDPhotoURL, &v.IDPhotoBackURL, &v.IDNumber,
			&v.DocumentType, &v.Status, &v.VerificationBadge, &v.VerifiedName, &v.RejectionReason,
			&v.Notes, &v.SubmittedAt, &v.ReviewedBy, &v.ReviewedAt,
			&v.CreatedAt, &v.UpdatedAt,
			&v.UserName, &v.UserEmail, &v.UserRole,
		); err != nil {
			return nil, 0, err
		}
		v.BadgeLabel = models.FormatVerificationBadge(v.VerificationBadge)
		items = append(items, v)
	}

	return items, total, rows.Err()
}

// ApproveVerification marks verification approved, assigns the badge, and updates technician/user names and statuses.
func (r *IdentityVerificationRepository) ApproveVerification(ctx context.Context, id, adminID, verifiedName, badge, notes string) (*models.IdentityVerification, error) {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM identity_verifications WHERE id = $1 FOR UPDATE`, id).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVerificationNotFound
	}
	if err != nil {
		return nil, err
	}

	updateQuery := `
		UPDATE identity_verifications SET
			status = 'approved',
			verification_badge = $1,
			verified_name = $2,
			notes = CASE WHEN $3 = '' THEN notes ELSE $3 END,
			reviewed_by = NULLIF($4, '')::uuid,
			reviewed_at = now(),
			updated_at = now()
		WHERE id = $5
	`
	if _, err := tx.ExecContext(ctx, updateQuery, badge, verifiedName, notes, adminID, id); err != nil {
		return nil, fmt.Errorf("update identity_verification: %w", err)
	}

	// Update users table: name, verified_name, identity_status
	userUpdateQuery := `
		UPDATE users SET
			name = $1,
			verified_name = $1,
			identity_status = $2,
			updated_at = now()
		WHERE id = $3
	`
	if _, err := tx.ExecContext(ctx, userUpdateQuery, verifiedName, badge, userID); err != nil {
		return nil, fmt.Errorf("update user verification: %w", err)
	}

	// Update technicians table: name, verified_name, identity_status
	techUpdateQuery := `
		UPDATE technicians SET
			name = $1,
			verified_name = $1,
			identity_status = $2,
			updated_at = now()
		WHERE id = $3 OR user_id = $3
	`
	if _, err := tx.ExecContext(ctx, techUpdateQuery, verifiedName, badge, userID); err != nil {
		return nil, fmt.Errorf("update technician verification: %w", err)
	}

	// Update technician_profiles table: verified_name, identity_status
	techProfileQuery := `
		UPDATE technician_profiles SET
			verified_name = $1,
			identity_status = $2,
			updated_at = now()
		WHERE technician_id = $3
	`
	if _, err := tx.ExecContext(ctx, techProfileQuery, verifiedName, badge, userID); err != nil {
		return nil, fmt.Errorf("update technician_profile verification: %w", err)
	}

	// Update isps table if present or if business/both
	ispUpdateQuery := `
		UPDATE isps SET
			verified_name = $1,
			identity_status = $2,
			updated_at = now()
		WHERE id = $3
	`
	if _, err := tx.ExecContext(ctx, ispUpdateQuery, verifiedName, badge, userID); err != nil {
		return nil, fmt.Errorf("update isp verification: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return r.GetVerificationByID(ctx, id)
}

// RejectVerification marks verification as rejected with a reason.
func (r *IdentityVerificationRepository) RejectVerification(ctx context.Context, id, adminID, reason, notes string) (*models.IdentityVerification, error) {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM identity_verifications WHERE id = $1 FOR UPDATE`, id).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVerificationNotFound
	}
	if err != nil {
		return nil, err
	}

	updateQuery := `
		UPDATE identity_verifications SET
			status = 'rejected',
			verification_badge = 'rejected',
			rejection_reason = $1,
			notes = CASE WHEN $2 = '' THEN notes ELSE $2 END,
			reviewed_by = NULLIF($3, '')::uuid,
			reviewed_at = now(),
			updated_at = now()
		WHERE id = $4
	`
	if _, err := tx.ExecContext(ctx, updateQuery, reason, notes, adminID, id); err != nil {
		return nil, fmt.Errorf("update identity_verification: %w", err)
	}

	_, _ = tx.ExecContext(ctx, `UPDATE users SET identity_status='rejected', updated_at=now() WHERE id=$1`, userID)
	_, _ = tx.ExecContext(ctx, `UPDATE technicians SET identity_status='rejected', updated_at=now() WHERE id=$1 OR user_id=$1`, userID)
	_, _ = tx.ExecContext(ctx, `UPDATE technician_profiles SET identity_status='rejected', updated_at=now() WHERE technician_id=$1`, userID)
	_, _ = tx.ExecContext(ctx, `UPDATE isps SET identity_status='rejected', updated_at=now() WHERE id=$1`, userID)

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return r.GetVerificationByID(ctx, id)
}
