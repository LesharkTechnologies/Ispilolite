package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type BillingRepository struct{ db *sql.DB }

func NewBillingRepository() (*BillingRepository, error) {
	db, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &BillingRepository{db: db}, nil
}

func (r *BillingRepository) Create(ctx context.Context, item *models.BillingSystem, ownerID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	item.ID = uuid.NewString()
	item.OwnerID = ownerID
	item.Status = "active"
	item.CreatedAt = now
	item.UpdatedAt = now
	if _, err = tx.ExecContext(ctx, `INSERT INTO billing_systems(id,owner_id,name,slug,logo_url,description,owner_company,owner_phone,support_phone,support_whatsapp,support_email,support_hours,website_url,login_url,referral_url,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, item.ID, ownerID, item.Name, item.Slug, item.LogoURL, item.Description, item.OwnerCompany, item.OwnerPhone, item.SupportPhone, item.SupportWhatsApp, item.SupportEmail, item.SupportHours, item.WebsiteURL, item.LoginURL, item.ReferralURL, item.Status, item.CreatedAt, item.UpdatedAt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO billing_system_managers(billing_system_id,user_id,role) VALUES($1,$2,'owner')`, item.ID, ownerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *BillingRepository) SlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM billing_systems WHERE slug=$1)`, slug).Scan(&exists)
	return exists, err
}

func (r *BillingRepository) Get(ctx context.Context, id string) (*models.BillingSystem, error) {
	return r.load(ctx, `id=$1`, id)
}

func (r *BillingRepository) GetBySlug(ctx context.Context, slug string) (*models.BillingSystem, error) {
	return r.load(ctx, `slug=$1`, slug)
}

func (r *BillingRepository) load(ctx context.Context, where string, arg interface{}) (*models.BillingSystem, error) {
	item := &models.BillingSystem{}
	var pricingUpdated sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT id,owner_id,name,slug,logo_url,description,owner_company,owner_phone,support_phone,support_whatsapp,support_email,support_hours,website_url,login_url,referral_url,pricing_updated_at,status,domain_verified,business_verified,created_at,updated_at,
		(SELECT count(*) FROM billing_system_reviews WHERE billing_system_id=billing_systems.id),
		(SELECT COALESCE(avg((reliability+ease_of_use+support+features+value_for_money)/5.0),0) FROM billing_system_reviews WHERE billing_system_id=billing_systems.id),
		(SELECT count(*) FROM user_follows WHERE followee_id=billing_systems.id)
		FROM billing_systems WHERE `+where, arg).Scan(&item.ID, &item.OwnerID, &item.Name, &item.Slug, &item.LogoURL, &item.Description, &item.OwnerCompany, &item.OwnerPhone, &item.SupportPhone, &item.SupportWhatsApp, &item.SupportEmail, &item.SupportHours, &item.WebsiteURL, &item.LoginURL, &item.ReferralURL, &pricingUpdated, &item.Status, &item.DomainVerified, &item.BusinessVerified, &item.CreatedAt, &item.UpdatedAt, &item.ReviewCount, &item.AverageRating, &item.FollowerCount)
	if err != nil {
		return nil, err
	}
	if pricingUpdated.Valid {
		value := pricingUpdated.Time
		item.PricingUpdatedAt = &value
	}
	item.Pricing, err = r.Pricing(ctx, item.ID)
	return item, err
}

func (r *BillingRepository) List(ctx context.Context, query, serviceType string, limit, offset int) ([]*models.BillingSystem, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,owner_id,name,slug,logo_url,description,owner_company,owner_phone,support_phone,support_whatsapp,support_email,support_hours,website_url,login_url,referral_url,pricing_updated_at,status,domain_verified,business_verified,created_at,updated_at,
		(SELECT count(*) FROM billing_system_reviews WHERE billing_system_id=billing_systems.id),
		(SELECT COALESCE(avg((reliability+ease_of_use+support+features+value_for_money)/5.0),0) FROM billing_system_reviews WHERE billing_system_id=billing_systems.id),
		(SELECT count(*) FROM user_follows WHERE followee_id=billing_systems.id),
		count(*) OVER()
		FROM billing_systems WHERE status='active' AND ($1='' OR name ILIKE '%'||$1||'%' OR description ILIKE '%'||$1||'%')
		AND ($2='' OR EXISTS(SELECT 1 FROM billing_system_pricing WHERE billing_system_id=billing_systems.id AND service_type=$2 AND available=true))
		ORDER BY average_rating DESC NULLS LAST,name LIMIT $3 OFFSET $4`, query, serviceType, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*models.BillingSystem, 0)
	total := 0
	for rows.Next() {
		item := &models.BillingSystem{}
		var pricingUpdated sql.NullTime
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Name, &item.Slug, &item.LogoURL, &item.Description, &item.OwnerCompany, &item.OwnerPhone, &item.SupportPhone, &item.SupportWhatsApp, &item.SupportEmail, &item.SupportHours, &item.WebsiteURL, &item.LoginURL, &item.ReferralURL, &pricingUpdated, &item.Status, &item.DomainVerified, &item.BusinessVerified, &item.CreatedAt, &item.UpdatedAt, &item.ReviewCount, &item.AverageRating, &item.FollowerCount, &total); err != nil {
			return nil, 0, err
		}
		if pricingUpdated.Valid {
			value := pricingUpdated.Time
			item.PricingUpdatedAt = &value
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *BillingRepository) Update(ctx context.Context, item *models.BillingSystem, actorID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE billing_systems SET name=$1,logo_url=$2,description=$3,owner_company=$4,owner_phone=$5,support_phone=$6,support_whatsapp=$7,support_email=$8,support_hours=$9,website_url=$10,login_url=$11,referral_url=$12,updated_at=now()
		WHERE id=$13 AND EXISTS(SELECT 1 FROM billing_system_managers WHERE billing_system_id=$13 AND user_id=$14)`, item.Name, item.LogoURL, item.Description, item.OwnerCompany, item.OwnerPhone, item.SupportPhone, item.SupportWhatsApp, item.SupportEmail, item.SupportHours, item.WebsiteURL, item.LoginURL, item.ReferralURL, item.ID, actorID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *BillingRepository) Archive(ctx context.Context, id, actorID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE billing_systems SET status='archived',updated_at=now() WHERE id=$1 AND EXISTS(SELECT 1 FROM billing_system_managers WHERE billing_system_id=$1 AND user_id=$2)`, id, actorID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *BillingRepository) SetVerification(ctx context.Context, id string, domainVerified, businessVerified bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE billing_systems SET domain_verified=$2,business_verified=$3,updated_at=now() WHERE id=$1`, id, domainVerified, businessVerified)
	return err
}

func (r *BillingRepository) Pricing(ctx context.Context, id string) ([]*models.BillingPrice, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,billing_system_id,service_type,currency,billing_period,amount,setup_fee,subscriber_limit,extra_subscriber_cost,transaction_fee_percent,notes,available,updated_at FROM billing_system_pricing WHERE billing_system_id=$1 ORDER BY service_type`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.BillingPrice, 0)
	for rows.Next() {
		item := &models.BillingPrice{}
		var amount, extra, fee sql.NullFloat64
		var limit sql.NullInt64
		if err := rows.Scan(&item.ID, &item.BillingSystemID, &item.ServiceType, &item.Currency, &item.BillingPeriod, &amount, &item.SetupFee, &limit, &extra, &fee, &item.Notes, &item.Available, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if amount.Valid {
			value := amount.Float64
			item.Amount = &value
		}
		if extra.Valid {
			value := extra.Float64
			item.ExtraSubscriberCost = &value
		}
		if fee.Valid {
			value := fee.Float64
			item.TransactionFeePercent = &value
		}
		if limit.Valid {
			value := int(limit.Int64)
			item.SubscriberLimit = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *BillingRepository) UpsertPricing(ctx context.Context, systemID, actorID string, prices []*models.BillingPrice, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var allowed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM billing_system_managers WHERE billing_system_id=$1 AND user_id=$2)`, systemID, actorID).Scan(&allowed); err != nil || !allowed {
		return sql.ErrNoRows
	}
	for _, price := range prices {
		if price.ServiceType == "" {
			continue
		}
		id := price.ID
		if id == "" {
			id = uuid.NewString()
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO billing_system_pricing(id,billing_system_id,service_type,currency,billing_period,amount,setup_fee,subscriber_limit,extra_subscriber_cost,transaction_fee_percent,notes,available,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT(billing_system_id,service_type) DO UPDATE SET currency=EXCLUDED.currency,billing_period=EXCLUDED.billing_period,amount=EXCLUDED.amount,setup_fee=EXCLUDED.setup_fee,subscriber_limit=EXCLUDED.subscriber_limit,extra_subscriber_cost=EXCLUDED.extra_subscriber_cost,transaction_fee_percent=EXCLUDED.transaction_fee_percent,notes=EXCLUDED.notes,available=EXCLUDED.available,updated_at=EXCLUDED.updated_at`, id, systemID, price.ServiceType, price.Currency, price.BillingPeriod, price.Amount, price.SetupFee, price.SubscriberLimit, price.ExtraSubscriberCost, price.TransactionFeePercent, price.Notes, price.Available, now)
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE billing_systems SET pricing_updated_at=$1,updated_at=$1 WHERE id=$2`, now, systemID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *BillingRepository) CreateReview(ctx context.Context, review *models.BillingReview) error {
	if review.Reliability < 1 || review.Reliability > 5 || review.EaseOfUse < 1 || review.EaseOfUse > 5 || review.Support < 1 || review.Support > 5 || review.Features < 1 || review.Features > 5 || review.ValueForMoney < 1 || review.ValueForMoney > 5 {
		return errors.New("ratings must be between 1 and 5")
	}
	review.ID = uuid.NewString()
	review.CreatedAt = time.Now().UTC()
	review.UpdatedAt = review.CreatedAt
	review.AverageScore = float64(review.Reliability+review.EaseOfUse+review.Support+review.Features+review.ValueForMoney) / 5.0
	_, err := r.db.ExecContext(ctx, `INSERT INTO billing_system_reviews(id,billing_system_id,reviewer_id,reliability,ease_of_use,support,features,value_for_money,comment,use_verified,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(billing_system_id,reviewer_id) DO UPDATE SET reliability=EXCLUDED.reliability,ease_of_use=EXCLUDED.ease_of_use,support=EXCLUDED.support,features=EXCLUDED.features,value_for_money=EXCLUDED.value_for_money,comment=EXCLUDED.comment,use_verified=EXCLUDED.use_verified,updated_at=EXCLUDED.updated_at`, review.ID, review.BillingSystemID, review.ReviewerID, review.Reliability, review.EaseOfUse, review.Support, review.Features, review.ValueForMoney, review.Comment, review.UseVerified, review.CreatedAt, review.UpdatedAt)
	if err != nil {
		return err
	}
	return nil
}

func (r *BillingRepository) Reviews(ctx context.Context, id string, limit, offset int) ([]*models.BillingReview, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.db.QueryContext(ctx, `SELECT r.id,r.billing_system_id,r.reviewer_id,u.name,u.role,r.reliability,r.ease_of_use,r.support,r.features,r.value_for_money,r.comment,r.use_verified,r.owner_reply,r.owner_replied_at,r.created_at,r.updated_at,count(*) OVER()
		FROM billing_system_reviews r JOIN users u ON u.id=r.reviewer_id
		WHERE r.billing_system_id=$1 ORDER BY r.created_at DESC LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*models.BillingReview, 0)
	total := 0
	for rows.Next() {
		item := &models.BillingReview{}
		var replied sql.NullTime
		if err := rows.Scan(&item.ID, &item.BillingSystemID, &item.ReviewerID, &item.ReviewerName, &item.ReviewerRole, &item.Reliability, &item.EaseOfUse, &item.Support, &item.Features, &item.ValueForMoney, &item.Comment, &item.UseVerified, &item.OwnerReply, &replied, &item.CreatedAt, &item.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		if replied.Valid {
			value := replied.Time
			item.OwnerRepliedAt = &value
		}
		item.AverageScore = float64(item.Reliability+item.EaseOfUse+item.Support+item.Features+item.ValueForMoney) / 5.0
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *BillingRepository) ReplyReview(ctx context.Context, systemID, ownerID, reviewID, reply string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE billing_system_reviews SET owner_reply=$1,owner_replied_at=now(),updated_at=now() WHERE id=$2 AND billing_system_id=$3 AND EXISTS(SELECT 1 FROM billing_system_managers WHERE billing_system_id=$3 AND user_id=$4)`, reply, reviewID, systemID, ownerID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *BillingRepository) Flag(ctx context.Context, flag *models.BillingFlag) error {
	flag.ID = uuid.NewString()
	flag.Status = "open"
	flag.CreatedAt = time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `INSERT INTO billing_system_flags(id,billing_system_id,reporter_id,reason,details,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(billing_system_id,reporter_id) DO UPDATE SET reason=EXCLUDED.reason,details=EXCLUDED.details,status='open',created_at=EXCLUDED.created_at`, flag.ID, flag.BillingSystemID, flag.ReporterID, flag.Reason, flag.Details, flag.Status, flag.CreatedAt)
	return err
}

func (r *BillingRepository) Claim(ctx context.Context, claim *models.BillingClaim) error {
	claim.ID = uuid.NewString()
	claim.Status = "pending"
	claim.CreatedAt = time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `INSERT INTO billing_system_claims(id,billing_system_id,claimant_id,evidence,status,created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(billing_system_id,claimant_id) DO UPDATE SET evidence=EXCLUDED.evidence,status='pending',created_at=EXCLUDED.created_at`, claim.ID, claim.BillingSystemID, claim.ClaimantID, claim.Evidence, claim.Status, claim.CreatedAt)
	return err
}

func (r *BillingRepository) Manager(ctx context.Context, systemID, userID string) (bool, error) {
	var manager bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM billing_system_managers WHERE billing_system_id=$1 AND user_id=$2)`, systemID, userID).Scan(&manager)
	return manager, err
}

func (r *BillingRepository) ResolveFlag(ctx context.Context, flagID, adminID, status string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE billing_system_flags SET status=$1 WHERE id=$2 AND status='open'`, status, flagID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *BillingRepository) ResolveClaim(ctx context.Context, claimID, adminID, status string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE billing_system_claims SET status=$1,resolved_at=now() WHERE id=$2 AND status='pending'`, status, claimID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *BillingRepository) ListFlags(ctx context.Context, status string, limit int) ([]*models.BillingFlag, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,billing_system_id,reporter_id,reason,details,status,created_at FROM billing_system_flags WHERE ($1='' OR status=$1) ORDER BY created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.BillingFlag, 0)
	for rows.Next() {
		item := &models.BillingFlag{}
		if err := rows.Scan(&item.ID, &item.BillingSystemID, &item.ReporterID, &item.Reason, &item.Details, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *BillingRepository) ListClaims(ctx context.Context, status string, limit int) ([]*models.BillingFlag, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,billing_system_id,claimant_id,evidence,status,created_at FROM billing_system_claims WHERE ($1='' OR status=$1) ORDER BY created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.BillingFlag, 0)
	for rows.Next() {
		item := &models.BillingFlag{}
		var claimant string
		if err := rows.Scan(&item.ID, &item.BillingSystemID, &claimant, &item.Reason, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Details = claimant
		items = append(items, item)
	}
	return items, rows.Err()
}

var _ = sort.Strings
var _ = json.Marshal
var _ = errors.New
var _ = fmt.Sprintf
