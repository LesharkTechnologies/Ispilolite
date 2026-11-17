package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/lib/pq"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

// MaxFlagsPerReporterPerDay bounds report volume per reporter. The unique
// partial index already prevents duplicate open reports about one account.
const MaxFlagsPerReporterPerDay = 5

var (
	ErrFlagDuplicate = errors.New("an open report about this user already exists")
	ErrFlagLimit     = errors.New("too many reports submitted; try again tomorrow")
	ErrFlagNotFound  = errors.New("report not found")
	ErrFlagForbidden = errors.New("report cannot be changed")
	ErrFlagClosed    = errors.New("report has already been reviewed")
)

type FlagRepository struct{ reader, writer *sql.DB }

func NewFlagRepository() (*FlagRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &FlagRepository{reader: reader, writer: writer}, nil
}

// CreateFlag stores a report and applies the per-reporter daily quota. A
// foreign-key failure means the reported account does not exist.
func (r *FlagRepository) CreateFlag(ctx context.Context, item *models.CustomerFlag) error {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var recent int
	if err = tx.QueryRowContext(ctx,
		`SELECT count(*) FROM customer_flags WHERE reporter_id=$1 AND created_at > now() - interval '24 hours'`,
		item.ReporterID).Scan(&recent); err != nil {
		return err
	}
	if recent >= MaxFlagsPerReporterPerDay {
		return ErrFlagLimit
	}

	images, err := json.Marshal(item.ImageURLs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO customer_flags
			(id, reporter_id, reported_user_id, reported_phone, category, description,
			 image_urls, status, account_action, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'none',$9,$9)`,
		item.ID, item.ReporterID, item.ReportedUser, item.ReportedPhone,
		item.Category, item.Description, images, models.FlagPending, item.CreatedAt)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				return ErrFlagDuplicate
			case "23503":
				return ErrFlagNotFound
			}
		}
		return err
	}
	return tx.Commit()
}

// ListFlagsByReporter returns the caller's own reports, newest first. Admin-only
// fields about the reported account are stripped so a reporter never sees the
// other user's phone number or status.
func (r *FlagRepository) ListFlagsByReporter(ctx context.Context, reporterID string, limit int) ([]*models.CustomerFlag, error) {
	rows, err := r.reader.QueryContext(ctx, flagSelect+` WHERE f.reporter_id=$1 ORDER BY f.created_at DESC LIMIT $2`, reporterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanFlags(rows)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		item.ReportedUserName, item.ReportedUserPhone, item.ReportedUserStatus = "", "", ""
	}
	return items, nil
}

// ListFlags returns the admin queue, optionally filtered by status.
func (r *FlagRepository) ListFlags(ctx context.Context, status string, limit int) ([]*models.CustomerFlag, error) {
	rows, err := r.reader.QueryContext(ctx, flagSelect+`
		WHERE ($1='' OR f.status=$1)
		ORDER BY f.created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFlags(rows)
}

// GetFlag returns a single report with admin-only reporter context.
func (r *FlagRepository) GetFlag(ctx context.Context, id string) (*models.CustomerFlag, error) {
	item := &models.CustomerFlag{}
	if err := scanFlagRow(r.reader.QueryRowContext(ctx, flagSelect+` WHERE f.id=$1`, id), item); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFlagNotFound
		}
		return nil, err
	}
	return item, nil
}

// CancelFlag lets a reporter withdraw their own unreviewed report.
func (r *FlagRepository) CancelFlag(ctx context.Context, id, reporterID string) error {
	result, err := r.writer.ExecContext(ctx, `
		UPDATE customer_flags SET status='cancelled', updated_at=now()
		WHERE id=$1 AND reporter_id=$2 AND status IN ('pending','in_progress')`, id, reporterID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrFlagForbidden
	}
	return nil
}

// ReviewFlag applies an admin decision atomically: the report status, the
// feedback, and (when approved) the account action. Suspending sets the user
// status to 'suspended', which blocks both password and OTP login and, because
// phones are unique, prevents re-registration with the same number.
func (r *FlagRepository) ReviewFlag(ctx context.Context, id, adminID, status, feedback, action string) error {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var current string
	var reported sql.NullString
	if err = tx.QueryRowContext(ctx,
		`SELECT status, reported_user_id FROM customer_flags WHERE id=$1 FOR UPDATE`, id).
		Scan(&current, &reported); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFlagNotFound
		}
		return err
	}
	if current == models.FlagApproved || current == models.FlagCancelled {
		return ErrFlagClosed
	}

	if status == models.FlagApproved && reported.Valid && action != models.FlagActionNone {
		if action == models.FlagActionSuspend {
			if _, err = tx.ExecContext(ctx,
				`UPDATE users SET status='suspended', updated_at=now() WHERE id=$1`, reported.String); err != nil {
				return err
			}
		}
		// Suspended accounts stay on the monitoring list for the audit trail.
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO user_monitoring (user_id, flag_id, reason, added_by, active, created_at, updated_at)
			VALUES ($1,$2,$3,$4,true,now(),now())
			ON CONFLICT (user_id) DO UPDATE SET
				flag_id=EXCLUDED.flag_id, reason=EXCLUDED.reason,
				added_by=EXCLUDED.added_by, active=true, released_at=NULL, updated_at=now()`,
			reported.String, id, feedback, adminID); err != nil {
			return err
		}
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE customer_flags
		SET status=$2, admin_feedback=$3, reviewed_by=$4, reviewed_at=now(),
		    account_action=$5, updated_at=now()
		WHERE id=$1`, id, status, feedback, adminID, action); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMonitored returns users on the monitoring list.
func (r *FlagRepository) ListMonitored(ctx context.Context, activeOnly bool, limit int) ([]*models.MonitoredUser, error) {
	rows, err := r.reader.QueryContext(ctx, `
		SELECT m.user_id, u.name, u.phone, u.status, m.reason, m.flag_id,
		       m.active, m.added_by, m.created_at, m.released_at
		FROM user_monitoring m JOIN users u ON u.id=m.user_id
		WHERE (NOT $1 OR m.active)
		ORDER BY m.created_at DESC LIMIT $2`, activeOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.MonitoredUser, 0)
	for rows.Next() {
		item := &models.MonitoredUser{}
		if err := rows.Scan(&item.UserID, &item.Name, &item.Phone, &item.Status,
			&item.Reason, &item.FlagID, &item.Active, &item.AddedBy,
			&item.CreatedAt, &item.ReleasedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ReleaseMonitoring removes a user from the active monitoring list. It does
// not reinstate a suspended account; that is a separate human decision.
func (r *FlagRepository) ReleaseMonitoring(ctx context.Context, userID, adminID string) error {
	result, err := r.writer.ExecContext(ctx, `
		UPDATE user_monitoring SET active=false, released_at=now(), updated_at=now()
		WHERE user_id=$1 AND active`, userID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrFlagNotFound
	}
	return nil
}

// RecordActivity stores one watched action, but only when the user is on the
// active monitoring list. Non-monitored users pay a single EXISTS probe.
func (r *FlagRepository) RecordActivity(ctx context.Context, userID, event, subjectType, subjectID string, metadata map[string]interface{}, ip string) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = r.writer.ExecContext(ctx, `
		INSERT INTO monitored_activity (user_id, event, subject_type, subject_id, metadata, ip_address)
		SELECT $1,$2,$3,$4,$5,NULLIF($6,'')::inet
		WHERE EXISTS (SELECT 1 FROM user_monitoring WHERE user_id=$1 AND active)`,
		userID, event, subjectType, subjectID, encoded, ip)
	return err
}

// ListActivity returns the monitored activity trail for one user.
func (r *FlagRepository) ListActivity(ctx context.Context, userID string, limit int) ([]*models.MonitoredActivity, error) {
	rows, err := r.reader.QueryContext(ctx, `
		SELECT id, user_id, event, subject_type, subject_id, metadata,
		       COALESCE(host(ip_address),''), created_at
		FROM monitored_activity WHERE user_id=$1
		ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.MonitoredActivity, 0)
	for rows.Next() {
		item := &models.MonitoredActivity{}
		var raw []byte
		if err := rows.Scan(&item.ID, &item.UserID, &item.Event, &item.SubjectType,
			&item.SubjectID, &raw, &item.IPAddress, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &item.Metadata)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const flagSelect = `
	SELECT f.id, f.reporter_id, f.reported_user_id, f.reported_phone, f.category,
	       f.description, f.image_urls, f.status, f.admin_feedback, f.reviewed_by,
	       f.reviewed_at, f.account_action, f.created_at, f.updated_at,
	       COALESCE(u.name,''), COALESCE(u.phone,''), COALESCE(u.status,'')
	FROM customer_flags f LEFT JOIN users u ON u.id=f.reported_user_id`

func scanFlags(rows *sql.Rows) ([]*models.CustomerFlag, error) {
	items := make([]*models.CustomerFlag, 0)
	for rows.Next() {
		item := &models.CustomerFlag{}
		if err := scanFlagRow(rows, item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type rowScanner interface{ Scan(...interface{}) error }

func scanFlagRow(row rowScanner, item *models.CustomerFlag) error {
	var reported, reviewedBy sql.NullString
	var reviewedAt sql.NullTime
	var images []byte
	if err := row.Scan(&item.ID, &item.ReporterID, &reported, &item.ReportedPhone,
		&item.Category, &item.Description, &images, &item.Status, &item.AdminFeedback,
		&reviewedBy, &reviewedAt, &item.AccountAction, &item.CreatedAt, &item.UpdatedAt,
		&item.ReportedUserName, &item.ReportedUserPhone, &item.ReportedUserStatus); err != nil {
		return err
	}
	if reported.Valid {
		item.ReportedUser = &reported.String
	}
	if reviewedBy.Valid {
		item.ReviewedBy = &reviewedBy.String
	}
	if reviewedAt.Valid {
		item.ReviewedAt = &reviewedAt.Time
	}
	item.ImageURLs = []string{}
	if len(images) > 0 {
		_ = json.Unmarshal(images, &item.ImageURLs)
	}
	return nil
}
