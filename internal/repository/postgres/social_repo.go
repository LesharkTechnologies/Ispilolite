package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type SocialRepository struct{ db *sql.DB }

func NewSocialRepository() (*SocialRepository, error) {
	db, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &SocialRepository{db: db}, nil
}

func (r *SocialRepository) AddFavorite(ctx context.Context, userID, targetType, targetID string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `INSERT INTO user_favorites(user_id,target_type,target_id)
		SELECT $1,$2,u.id FROM users u WHERE u.id=$3 AND u.role=$2 AND u.status='active' AND u.id<>$1
		ON CONFLICT DO NOTHING`, userID, targetType, targetID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		var exists bool
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_favorites WHERE user_id=$1 AND target_type=$2 AND target_id=$3)`, userID, targetType, targetID).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return false, sql.ErrNoRows
		}
	}
	return n > 0, nil
}

func (r *SocialRepository) RemoveFavorite(ctx context.Context, userID, targetType, targetID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_favorites WHERE user_id=$1 AND target_type=$2 AND target_id=$3`, userID, targetType, targetID)
	return err
}

func (r *SocialRepository) Favorites(ctx context.Context, userID, targetType string, limit, offset int) ([]*models.FavoriteProfile, int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT f.target_id,f.target_type,u.name,COALESCE(u.avatar_url,''),u.rating,f.created_at,count(*) OVER()
		FROM user_favorites f JOIN users u ON u.id=f.target_id AND u.status='active'
		WHERE f.user_id=$1 AND ($2='' OR f.target_type=$2) ORDER BY f.created_at DESC LIMIT $3 OFFSET $4`, userID, targetType, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*models.FavoriteProfile, 0)
	total := 0
	for rows.Next() {
		item := &models.FavoriteProfile{}
		if err := rows.Scan(&item.ID, &item.TargetType, &item.Name, &item.AvatarURL, &item.Rating, &item.AddedAt, &total); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *SocialRepository) validReactionTarget(ctx context.Context, targetType, targetID string) (bool, error) {
	var query string
	switch targetType {
	case "user_profile":
		query = `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active' AND role IN ('customer','isp','technician'))`
	case "technician_post":
		query = `SELECT EXISTS(SELECT 1 FROM technician_posts WHERE id=$1 AND status='published')`
	case "review":
		query = `SELECT EXISTS(SELECT 1 FROM reviews WHERE id=$1 AND status='approved')`
	default:
		return false, nil
	}
	var valid bool
	err := r.db.QueryRowContext(ctx, query, targetID).Scan(&valid)
	return valid, err
}

func (r *SocialRepository) React(ctx context.Context, userID, targetType, targetID, reaction string) (bool, error) {
	valid, err := r.validReactionTarget(ctx, targetType, targetID)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, sql.ErrNoRows
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO user_reactions(user_id,target_type,target_id,reaction_type) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, userID, targetType, targetID, reaction)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (r *SocialRepository) Unreact(ctx context.Context, userID, targetType, targetID, reaction string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_reactions WHERE user_id=$1 AND target_type=$2 AND target_id=$3 AND reaction_type=$4`, userID, targetType, targetID, reaction)
	return err
}

func (r *SocialRepository) ReactionSummary(ctx context.Context, userID, targetType, targetID string) (*models.ReactionSummary, error) {
	valid, err := r.validReactionTarget(ctx, targetType, targetID)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, sql.ErrNoRows
	}
	summary := &models.ReactionSummary{TargetType: targetType, TargetID: targetID, Counts: map[string]int{}, MyReactions: []string{}}
	rows, err := r.db.QueryContext(ctx, `SELECT reaction_type,count(*),bool_or(user_id=$3) FROM user_reactions WHERE target_type=$1 AND target_id=$2 GROUP BY reaction_type`, targetType, targetID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var reaction string
		var count int
		var mine bool
		if err := rows.Scan(&reaction, &count, &mine); err != nil {
			return nil, err
		}
		summary.Counts[reaction] = count
		if mine {
			summary.MyReactions = append(summary.MyReactions, reaction)
		}
	}
	return summary, rows.Err()
}

func (r *SocialRepository) RegisterDevice(ctx context.Context, userID, platform, token string) (*models.DeviceToken, error) {
	item := &models.DeviceToken{ID: uuid.NewString(), UserID: userID, Platform: platform, Token: token, Enabled: true, LastSeenAt: time.Now().UTC()}
	result, err := r.db.ExecContext(ctx, `INSERT INTO user_device_tokens(id,user_id,platform,token,enabled,last_seen_at) VALUES($1,$2,$3,$4,true,$5)
		ON CONFLICT(token) DO UPDATE SET platform=EXCLUDED.platform,enabled=true,updated_at=now(),last_seen_at=EXCLUDED.last_seen_at WHERE user_device_tokens.user_id=EXCLUDED.user_id`, item.ID, userID, platform, token, item.LastSeenAt)
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, errors.New("device token belongs to another account")
	}
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM user_device_tokens WHERE token=$1 AND user_id=$2`, token, userID).Scan(&item.ID); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *SocialRepository) RemoveDevice(ctx context.Context, userID, token string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE user_device_tokens SET enabled=false,updated_at=now() WHERE user_id=$1 AND token=$2`, userID, token)
	return err
}
