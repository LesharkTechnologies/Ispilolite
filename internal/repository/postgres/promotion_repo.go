package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

var (
	ErrPromotionNotFound  = errors.New("promotion not found")
	ErrPromotionForbidden = errors.New("promotion access denied")
	ErrPromotionConflict  = errors.New("promotion state conflict")
)

type PromotionRepository struct {
	reader *sql.DB
	writer *sql.DB
}

func NewPromotionRepository() (*PromotionRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &PromotionRepository{reader: reader, writer: writer}, nil
}

const promotionEffectiveStatusSQL = `CASE
	WHEN p.status='cancelled' THEN 'cancelled'
	WHEN p.status='suspended' THEN 'suspended'
	WHEN p.status='pending_approval' THEN 'pending_approval'
	WHEN p.status='pending_payment' THEN 'pending_payment'
	WHEN p.status='rejected' THEN 'rejected'
	WHEN p.expires_at <= now() THEN 'expired'
	WHEN p.starts_at > now() THEN 'scheduled'
	ELSE 'active'
END`

const promotionColumnsSQL = `p.id,p.owner_id,p.listing_type,p.listing_id,p.listing_name,
	p.destination_type,p.destination_url,p.placement,` + promotionEffectiveStatusSQL + `,p.status,
	p.starts_at,p.expires_at,p.amount,p.currency,
	COALESCE((SELECT pp.status FROM promotion_payments pp WHERE pp.promotion_id=p.id ORDER BY pp.created_at DESC LIMIT 1),''),
	(SELECT count(*) FROM promotion_events pe WHERE pe.promotion_id=p.id AND pe.event_type='impression'),
	(SELECT count(DISTINCT pe.viewer_key) FROM promotion_events pe WHERE pe.promotion_id=p.id AND pe.event_type='impression'),
	(SELECT count(*) FROM promotion_events pe WHERE pe.promotion_id=p.id AND pe.event_type='click'),
	(SELECT count(DISTINCT pe.viewer_key) FROM promotion_events pe WHERE pe.promotion_id=p.id AND pe.event_type='click'),
	(SELECT count(*) FROM promotion_enquiries en WHERE en.promotion_id=p.id),
	COALESCE(p.renewed_from_id::text,''),p.approved_at,p.rejected_at,p.rejection_reason,
	p.cancelled_at,p.suspended_at,p.created_at,p.updated_at`

func promotionAccessSQL(roleArg, actorArg string) string {
	return fmt.Sprintf(`(%[1]s='admin' OR p.owner_id=%[2]s OR (p.listing_type='billing_system' AND EXISTS (
		SELECT 1 FROM billing_system_managers m WHERE m.billing_system_id=p.listing_id AND m.user_id=%[2]s)))`, roleArg, actorArg)
}

// ResolveListing verifies that actorID can promote the requested listing and
// returns the current display name and internal path.
func (r *PromotionRepository) ResolveListing(ctx context.Context, actorID, role, listingType, listingID string) (*models.PromotionListing, error) {
	admin := role == "admin"
	var name, internalPath string
	var err error
	switch listingType {
	case models.PromotionListingBillingSystem:
		err = r.reader.QueryRowContext(ctx, `SELECT b.name,'/billing-systems/'||b.slug
			FROM billing_systems b
			WHERE b.id=$1 AND b.status='active'
			AND ($2::boolean OR b.owner_id=$3 OR EXISTS(SELECT 1 FROM billing_system_managers m WHERE m.billing_system_id=b.id AND m.user_id=$3))`, listingID, admin, actorID).Scan(&name, &internalPath)
	case models.PromotionListingISP:
		err = r.reader.QueryRowContext(ctx, `SELECT u.name,u.profile_path
			FROM users u JOIN isps i ON i.id=u.id
			WHERE u.id=$1 AND u.role='isp' AND u.status='active' AND i.is_active
			AND ($2::boolean OR u.id=$3)`, listingID, admin, actorID).Scan(&name, &internalPath)
	case models.PromotionListingTechnician:
		err = r.reader.QueryRowContext(ctx, `SELECT u.name,u.profile_path
			FROM users u JOIN technicians t ON t.user_id=u.id
			WHERE u.id=$1 AND u.role='technician' AND u.status='active'
			AND ($2::boolean OR u.id=$3)`, listingID, admin, actorID).Scan(&name, &internalPath)
	default:
		return nil, ErrPromotionForbidden
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPromotionForbidden
	}
	if err != nil {
		return nil, err
	}
	return &models.PromotionListing{Type: listingType, ID: listingID, Name: name, InternalPath: internalPath}, nil
}

func (r *PromotionRepository) Create(ctx context.Context, promotion *models.Promotion, payment *models.PromotionPayment) error {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if promotion.ID == "" {
		promotion.ID = uuid.NewString()
	}
	promotion.Status = models.PromotionPendingApproval
	promotion.CreatedAt = now
	promotion.UpdatedAt = now
	_, err = tx.ExecContext(ctx, `INSERT INTO promotions(id,owner_id,listing_type,listing_id,listing_name,destination_type,destination_url,placement,status,starts_at,expires_at,amount,currency,renewed_from_id,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,'')::uuid,$15,$15)`,
		promotion.ID, promotion.OwnerID, promotion.ListingType, promotion.ListingID, promotion.ListingName,
		promotion.DestinationType, promotion.DestinationURL, promotion.Placement, promotion.Status,
		promotion.StartsAt, promotion.ExpiresAt, promotion.Amount, promotion.Currency, promotion.RenewedFromID, promotion.CreatedAt)
	if err != nil {
		return err
	}
	if payment != nil {
		payment.ID = uuid.NewString()
		payment.PromotionID = promotion.ID
		payment.Status = "pending"
		payment.CreatedAt = now
		payment.UpdatedAt = now
		_, err = tx.ExecContext(ctx, `INSERT INTO promotion_payments(id,promotion_id,amount,currency,status,provider,provider_reference,receipt,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`, payment.ID, payment.PromotionID, payment.Amount, payment.Currency, payment.Status, payment.Provider, payment.ProviderReference, payment.Receipt, payment.CreatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanPromotion(row interface{ Scan(...interface{}) error }) (*models.Promotion, error) {
	item := &models.Promotion{}
	var approved, rejected, cancelled, suspended sql.NullTime
	err := row.Scan(&item.ID, &item.OwnerID, &item.ListingType, &item.ListingID, &item.ListingName,
		&item.DestinationType, &item.DestinationURL, &item.Placement, &item.Status, &item.StoredStatus,
		&item.StartsAt, &item.ExpiresAt, &item.Amount, &item.Currency, &item.PaymentStatus,
		&item.Impressions, &item.UniqueViewers, &item.Clicks, &item.UniqueClickers, &item.EnquiryCount,
		&item.RenewedFromID, &approved, &rejected, &item.RejectionReason,
		&cancelled, &suspended, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if approved.Valid {
		value := approved.Time
		item.ApprovedAt = &value
	}
	if rejected.Valid {
		value := rejected.Time
		item.RejectedAt = &value
	}
	if cancelled.Valid {
		value := cancelled.Time
		item.CancelledAt = &value
	}
	if suspended.Valid {
		value := suspended.Time
		item.SuspendedAt = &value
	}
	if item.Impressions > 0 {
		item.ClickThroughRate = float64(item.Clicks) / float64(item.Impressions) * 100
	}
	item.TimeRemainingSeconds, item.TimeRemaining = models.PromotionTimeRemaining(item.Status, item.StartsAt, item.ExpiresAt, time.Now().UTC())
	return item, nil
}

func (r *PromotionRepository) Get(ctx context.Context, promotionID, actorID, role string) (*models.Promotion, error) {
	item, err := scanPromotion(r.reader.QueryRowContext(ctx, `SELECT `+promotionColumnsSQL+` FROM promotions p WHERE p.id=$1 AND `+promotionAccessSQL("$2", "$3"), promotionID, role, actorID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPromotionNotFound
	}
	return item, err
}

func (r *PromotionRepository) List(ctx context.Context, actorID, role, status string, limit, offset int) ([]*models.Promotion, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	where := `FROM promotions p WHERE ` + promotionAccessSQL("$2", "$1") + ` AND ($3='' OR (` + promotionEffectiveStatusSQL + `)=$3)`
	var total int
	if err := r.reader.QueryRowContext(ctx, `SELECT count(*) `+where, actorID, role, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.reader.QueryContext(ctx, `SELECT `+promotionColumnsSQL+` `+where+` ORDER BY p.created_at DESC LIMIT $4 OFFSET $5`, actorID, role, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*models.Promotion, 0)
	for rows.Next() {
		item, err := scanPromotion(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *PromotionRepository) Cancel(ctx context.Context, promotionID, actorID, role string) error {
	result, err := r.writer.ExecContext(ctx, `UPDATE promotions p SET status='cancelled',cancelled_at=now(),updated_at=now()
		WHERE p.id=$1 AND p.status<>'cancelled' AND `+promotionAccessSQL("$2", "$3"), promotionID, role, actorID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrPromotionConflict
	}
	return nil
}

func (r *PromotionRepository) ListPayments(ctx context.Context, promotionID string) ([]*models.PromotionPayment, error) {
	rows, err := r.reader.QueryContext(ctx, `SELECT id,promotion_id,amount,currency,status,provider,provider_reference,receipt,
		COALESCE(confirmed_by::text,''),paid_at,created_at,updated_at
		FROM promotion_payments WHERE promotion_id=$1 ORDER BY created_at DESC`, promotionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.PromotionPayment, 0)
	for rows.Next() {
		item := &models.PromotionPayment{}
		var paid sql.NullTime
		if err := rows.Scan(&item.ID, &item.PromotionID, &item.Amount, &item.Currency, &item.Status,
			&item.Provider, &item.ProviderReference, &item.Receipt, &item.ConfirmedBy, &paid,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if paid.Valid {
			value := paid.Time
			item.PaidAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ConfirmPayment records an externally verified payment. It deliberately does
// not call a provider: no money is moved by this method, so it is only exposed
// to admins reconciling a payment reference.
func (r *PromotionRepository) ConfirmPayment(ctx context.Context, promotionID, paymentID, adminID, provider, reference, receipt string) error {
	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var paymentStatus, promotionStatus string
	var expiresAt time.Time
	var approvedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT pp.status,p.status,p.expires_at,p.approved_at FROM promotion_payments pp
		JOIN promotions p ON p.id=pp.promotion_id
		WHERE pp.id=$1 AND pp.promotion_id=$2 FOR UPDATE`, paymentID, promotionID).Scan(&paymentStatus, &promotionStatus, &expiresAt, &approvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPromotionNotFound
	}
	if err != nil {
		return err
	}
	if paymentStatus != "pending" && paymentStatus != "failed" {
		return ErrPromotionConflict
	}
	if promotionStatus == models.PromotionCancelled || promotionStatus == models.PromotionSuspended || promotionStatus == models.PromotionRejected || !expiresAt.After(time.Now().UTC()) {
		return ErrPromotionConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE promotion_payments SET status='paid',provider=$1,provider_reference=$2,receipt=$3,confirmed_by=$4,paid_at=now(),updated_at=now() WHERE id=$5`, provider, reference, receipt, adminID, paymentID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE promotions SET status=CASE
			WHEN approved_at IS NULL THEN 'pending_approval'
			WHEN starts_at > now() THEN 'scheduled'
			ELSE 'active' END,updated_at=now() WHERE id=$1`, promotionID); err != nil {
		return err
	}
	return tx.Commit()
}

// Approve records the admin content review. An approved promotion still waits
// for payment when no paid/waived payment exists; the customer side only serves
// active or started scheduled promotions.
func (r *PromotionRepository) Approve(ctx context.Context, promotionID, adminID string) error {
	result, err := r.writer.ExecContext(ctx, `UPDATE promotions p SET approved_by=$2,approved_at=now(),
		rejected_by=NULL,rejected_at=NULL,rejection_reason='',
		status=CASE
			WHEN EXISTS(SELECT 1 FROM promotion_payments pp WHERE pp.promotion_id=p.id AND pp.status IN ('paid','waived'))
				THEN CASE WHEN p.starts_at > now() THEN 'scheduled' ELSE 'active' END
			ELSE 'pending_payment' END,
		updated_at=now()
		WHERE p.id=$1 AND p.approved_at IS NULL
		AND p.status NOT IN ('cancelled','suspended','rejected','expired')
		AND p.expires_at > now()`, promotionID, adminID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrPromotionConflict
	}
	return nil
}

// Reject closes a submitted promotion without making it public. The review
// reason is visible to the owner and admins.
func (r *PromotionRepository) Reject(ctx context.Context, promotionID, adminID, reason string) error {
	result, err := r.writer.ExecContext(ctx, `UPDATE promotions SET status='rejected',rejected_by=$2,rejected_at=now(),
		rejection_reason=$3,updated_at=now()
		WHERE id=$1 AND approved_at IS NULL AND status='pending_approval'`, promotionID, adminID, reason)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrPromotionConflict
	}
	return nil
}

func (r *PromotionRepository) SetStatus(ctx context.Context, promotionID, status string) error {
	var result sql.Result
	var err error
	switch status {
	case models.PromotionSuspended:
		result, err = r.writer.ExecContext(ctx, `UPDATE promotions SET status='suspended',suspended_at=now(),updated_at=now() WHERE id=$1 AND status<>'cancelled'`, promotionID)
	case models.PromotionCancelled:
		result, err = r.writer.ExecContext(ctx, `UPDATE promotions SET status='cancelled',cancelled_at=now(),updated_at=now() WHERE id=$1`, promotionID)
	case models.PromotionActive:
		result, err = r.writer.ExecContext(ctx, `UPDATE promotions p SET status=CASE WHEN p.starts_at > now() THEN 'scheduled' WHEN p.expires_at <= now() THEN 'expired' ELSE 'active' END,suspended_at=NULL,updated_at=now()
			WHERE p.id=$1 AND p.status<>'cancelled' AND p.approved_at IS NOT NULL
			AND EXISTS(SELECT 1 FROM promotion_payments pp WHERE pp.promotion_id=p.id AND pp.status IN ('paid','waived'))`, promotionID)
	default:
		return ErrPromotionConflict
	}
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrPromotionConflict
	}
	return nil
}

func (r *PromotionRepository) RecordEvent(ctx context.Context, promotionID, eventType, viewerKey, viewerRole, county, placement string, bucket int64) (bool, error) {
	result, err := r.writer.ExecContext(ctx, `INSERT INTO promotion_events(promotion_id,event_type,viewer_key,viewer_role,viewer_county,placement,dedup_bucket,occurred_at)
		SELECT p.id,$2,$3,$4,$5,$6,$7,now() FROM promotions p
		WHERE p.id=$1 AND p.status IN ('active','scheduled') AND p.starts_at<=now() AND p.expires_at>now()
		ON CONFLICT(promotion_id,event_type,viewer_key,placement,dedup_bucket) DO NOTHING`,
		promotionID, eventType, viewerKey, viewerRole, county, placement, bucket)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (r *PromotionRepository) Active(ctx context.Context, placement string, limit int) ([]*models.PromotionPublic, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := r.reader.QueryContext(ctx, `SELECT id,listing_type,listing_id,listing_name,destination_url,placement,starts_at,expires_at
		FROM promotions WHERE status IN ('active','scheduled') AND starts_at<=now() AND expires_at>now()
		AND ($1='' OR placement=$1) ORDER BY created_at DESC LIMIT $2`, placement, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.PromotionPublic, 0)
	for rows.Next() {
		item := &models.PromotionPublic{}
		if err := rows.Scan(&item.ID, &item.ListingType, &item.ListingID, &item.ListingName, &item.DestinationURL, &item.Placement, &item.StartsAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PromotionRepository) Insights(ctx context.Context, promotionID string, from, to time.Time, placement string) (*models.PromotionInsights, error) {
	out := &models.PromotionInsights{
		PromotionID: promotionID,
		From:        from.Format("2006-01-02"),
		To:          to.Add(-time.Second).Format("2006-01-02"),
		Roles:       []models.PromotionRoleInsight{},
		Locations:   []models.PromotionLocationInsight{},
		Placements:  []models.PromotionPlacementInsight{},
		Daily:       []models.PromotionDailyInsight{},
	}
	filter := `promotion_id=$1 AND occurred_at >= $2 AND occurred_at < $3 AND ($4='' OR placement=$4)`
	err := r.reader.QueryRowContext(ctx, `SELECT
		COALESCE(count(*) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(DISTINCT viewer_key) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(*) FILTER (WHERE event_type='click'),0),
		COALESCE(count(DISTINCT viewer_key) FILTER (WHERE event_type='click'),0)
		FROM promotion_events WHERE `+filter, promotionID, from, to, placement).Scan(
		&out.Totals.Impressions, &out.Totals.UniqueViewers, &out.Totals.Clicks, &out.Totals.UniqueClickers)
	if err != nil {
		return nil, err
	}
	out.Totals.ClickThroughRate = clickThroughRate(out.Totals.Clicks, out.Totals.Impressions)

	roleRows, err := r.reader.QueryContext(ctx, `SELECT
		CASE WHEN viewer_role IN ('technician','isp') THEN viewer_role ELSE 'other' END,
		COALESCE(count(*) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(DISTINCT viewer_key) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(*) FILTER (WHERE event_type='click'),0)
		FROM promotion_events WHERE `+filter+` GROUP BY 1 ORDER BY 1`, promotionID, from, to, placement)
	if err != nil {
		return nil, err
	}
	for roleRows.Next() {
		item := models.PromotionRoleInsight{}
		if err := roleRows.Scan(&item.Role, &item.Impressions, &item.UniqueViewers, &item.Clicks); err != nil {
			roleRows.Close()
			return nil, err
		}
		out.Roles = append(out.Roles, item)
	}
	if err := roleRows.Close(); err != nil {
		return nil, err
	}

	locationRows, err := r.reader.QueryContext(ctx, `SELECT COALESCE(NULLIF(viewer_county,''),'Unknown'),
		COALESCE(count(*) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(DISTINCT viewer_key) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(*) FILTER (WHERE event_type='click'),0)
		FROM promotion_events WHERE `+filter+` GROUP BY 1 ORDER BY 2 DESC LIMIT 50`, promotionID, from, to, placement)
	if err != nil {
		return nil, err
	}
	for locationRows.Next() {
		item := models.PromotionLocationInsight{}
		if err := locationRows.Scan(&item.County, &item.Impressions, &item.UniqueViewers, &item.Clicks); err != nil {
			locationRows.Close()
			return nil, err
		}
		item.ClickThroughRate = clickThroughRate(item.Clicks, item.Impressions)
		out.Locations = append(out.Locations, item)
	}
	if err := locationRows.Close(); err != nil {
		return nil, err
	}

	placementRows, err := r.reader.QueryContext(ctx, `SELECT placement,
		COALESCE(count(*) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(DISTINCT viewer_key) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(*) FILTER (WHERE event_type='click'),0),
		COALESCE(count(DISTINCT viewer_key) FILTER (WHERE event_type='click'),0)
		FROM promotion_events WHERE `+filter+` GROUP BY 1 ORDER BY 2 DESC`, promotionID, from, to, placement)
	if err != nil {
		return nil, err
	}
	for placementRows.Next() {
		item := models.PromotionPlacementInsight{}
		if err := placementRows.Scan(&item.Placement, &item.Impressions, &item.UniqueViewers, &item.Clicks, &item.UniqueClickers); err != nil {
			placementRows.Close()
			return nil, err
		}
		item.ClickThroughRate = clickThroughRate(item.Clicks, item.Impressions)
		out.Placements = append(out.Placements, item)
	}
	if err := placementRows.Close(); err != nil {
		return nil, err
	}

	dailyRows, err := r.reader.QueryContext(ctx, `SELECT to_char(occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD'),
		COALESCE(count(*) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(DISTINCT viewer_key) FILTER (WHERE event_type='impression'),0),
		COALESCE(count(*) FILTER (WHERE event_type='click'),0),
		COALESCE(count(DISTINCT viewer_key) FILTER (WHERE event_type='click'),0)
		FROM promotion_events WHERE `+filter+` GROUP BY 1 ORDER BY 1`, promotionID, from, to, placement)
	if err != nil {
		return nil, err
	}
	for dailyRows.Next() {
		item := models.PromotionDailyInsight{}
		if err := dailyRows.Scan(&item.Date, &item.Impressions, &item.UniqueViewers, &item.Clicks, &item.UniqueClickers); err != nil {
			dailyRows.Close()
			return nil, err
		}
		item.ClickThroughRate = clickThroughRate(item.Clicks, item.Impressions)
		out.Daily = append(out.Daily, item)
	}
	if err := dailyRows.Err(); err != nil {
		dailyRows.Close()
		return nil, err
	}
	return out, dailyRows.Close()
}

func clickThroughRate(clicks, impressions int64) float64 {
	if impressions <= 0 {
		return 0
	}
	return float64(clicks) / float64(impressions) * 100
}

// ViewerCounty returns only the broad county used for aggregate audience
// insights. Callers must never expose a viewer's exact location or identity.
func (r *PromotionRepository) ViewerCounty(ctx context.Context, userID string) (string, error) {
	var county string
	err := r.reader.QueryRowContext(ctx, `SELECT COALESCE(county,'') FROM users WHERE id=$1`, userID).Scan(&county)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return county, err
}

func (r *PromotionRepository) CreateEnquiry(ctx context.Context, promotionID, userID, name, phone, email, message string) (*models.PromotionEnquiry, error) {
	item := &models.PromotionEnquiry{ID: uuid.NewString(), PromotionID: promotionID, UserID: userID}
	now := time.Now().UTC()
	item.CreatedAt, item.UpdatedAt = now, now
	item.Consent = true
	item.Status = "new"
	var recent bool
	if err := r.reader.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM promotion_enquiries WHERE promotion_id=$1 AND user_id=$2 AND created_at > now()-interval '1 day')`, promotionID, userID).Scan(&recent); err != nil {
		return nil, err
	}
	if recent {
		return nil, ErrPromotionConflict
	}
	err := r.writer.QueryRowContext(ctx, `INSERT INTO promotion_enquiries(id,promotion_id,user_id,name,phone,email,message,consent,status,created_at,updated_at)
		SELECT $1,$2,u.id,COALESCE(NULLIF($3,''),u.name),COALESCE(NULLIF($4,''),u.phone),COALESCE(NULLIF($5,''),u.email),$6,true,'new',$7,$7
		FROM users u WHERE u.id=$2 AND u.status='active'
		AND EXISTS(SELECT 1 FROM promotions p WHERE p.id=$8)
		RETURNING name,phone,email`, item.ID, userID, name, phone, email, message, now, promotionID).Scan(&item.Name, &item.Phone, &item.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPromotionForbidden
	}
	if err != nil {
		return nil, err
	}
	item.Message = message
	return item, nil
}

func (r *PromotionRepository) ListEnquiries(ctx context.Context, promotionID string, limit, offset int) ([]*models.PromotionEnquiry, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := r.reader.QueryRowContext(ctx, `SELECT count(*) FROM promotion_enquiries WHERE promotion_id=$1`, promotionID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.reader.QueryContext(ctx, `SELECT id,promotion_id,user_id,name,phone,email,message,consent,status,created_at,updated_at
		FROM promotion_enquiries WHERE promotion_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, promotionID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*models.PromotionEnquiry, 0)
	for rows.Next() {
		item := &models.PromotionEnquiry{}
		if err := rows.Scan(&item.ID, &item.PromotionID, &item.UserID, &item.Name, &item.Phone, &item.Email, &item.Message, &item.Consent, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
