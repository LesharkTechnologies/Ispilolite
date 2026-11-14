package postgres

import (
	"context"
	"database/sql"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type ReferralRepository struct{ db *sql.DB }

func NewReferralRepository() (*ReferralRepository, error) {
	db, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	return &ReferralRepository{db: db}, nil
}
func (r *ReferralRepository) Summary(ctx context.Context, userID string) (string, float64, []*models.Referral, error) {
	var code string
	var balance float64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(referral_code,''),credit_balance FROM users WHERE id=$1`, userID).Scan(&code, &balance); err != nil {
		return "", 0, nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT r.id,r.referrer_id,r.referred_id,u.name,r.code,r.status,r.reward_amount,r.qualified_at,r.created_at FROM referrals r JOIN users u ON u.id=r.referred_id WHERE r.referrer_id=$1 ORDER BY r.created_at DESC LIMIT 100`, userID)
	if err != nil {
		return "", 0, nil, err
	}
	defer rows.Close()
	items := make([]*models.Referral, 0)
	for rows.Next() {
		item := &models.Referral{}
		var qualified sql.NullTime
		if err := rows.Scan(&item.ID, &item.ReferrerID, &item.ReferredID, &item.ReferredName, &item.Code, &item.Status, &item.RewardAmount, &qualified, &item.CreatedAt); err != nil {
			return "", 0, nil, err
		}
		if qualified.Valid {
			v := qualified.Time
			item.QualifiedAt = &v
		}
		items = append(items, item)
	}
	return code, balance, items, rows.Err()
}
