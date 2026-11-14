package postgres

import (
	"context"
	"database/sql"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

// FollowRepository reads from the writer so a follow is visible immediately
// on the next request, even in deployments with lagging read replicas.
type FollowRepository struct{ db *sql.DB }

func NewFollowRepository() (*FollowRepository, error) {
	db, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &FollowRepository{db: db}, nil
}

func (r *FollowRepository) Active(ctx context.Context, userID string) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active' AND role IN ('customer','isp','technician'))`, userID).Scan(&active)
	return active, err
}

// Follow returns true when a new relationship was created, false if it
// already exists. Both accounts must be active; no role is excluded.
func (r *FollowRepository) Follow(ctx context.Context, followerID, followeeID string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `INSERT INTO user_follows (follower_id,followee_id)
		SELECT follower.id,followee.id FROM users follower CROSS JOIN users followee
		WHERE follower.id=$1 AND follower.status='active' AND follower.role IN ('customer','isp','technician')
		AND followee.id=$2 AND followee.status='active' AND followee.role IN ('customer','isp','technician')
		ON CONFLICT (follower_id,followee_id) DO NOTHING`, followerID, followeeID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// Unfollow is idempotent; it returns whether an existing relationship was removed.
func (r *FollowRepository) Unfollow(ctx context.Context, followerID, followeeID string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM user_follows WHERE follower_id=$1 AND followee_id=$2`, followerID, followeeID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (r *FollowRepository) Summary(ctx context.Context, userID, viewerID string) (*models.FollowSummary, error) {
	s := &models.FollowSummary{UserID: userID}
	err := r.db.QueryRowContext(ctx, `SELECT
		(SELECT profile_path FROM users WHERE id=$1),
		(SELECT count(*) FROM user_follows f JOIN users u ON u.id=f.follower_id AND u.status='active' AND u.role IN ('customer','isp','technician') WHERE f.followee_id=$1),
		(SELECT count(*) FROM user_follows f JOIN users u ON u.id=f.followee_id AND u.status='active' AND u.role IN ('customer','isp','technician') WHERE f.follower_id=$1),
		EXISTS(SELECT 1 FROM user_follows WHERE follower_id=$2 AND followee_id=$1)
	`, userID, viewerID).Scan(&s.ProfilePath, &s.FollowerCount, &s.FollowingCount, &s.IsFollowing)
	return s, err
}

func (r *FollowRepository) Followers(ctx context.Context, userID string, limit, offset int) (*models.FollowPage, error) {
	return r.list(ctx, userID, limit, offset, true)
}

func (r *FollowRepository) Following(ctx context.Context, userID string, limit, offset int) (*models.FollowPage, error) {
	return r.list(ctx, userID, limit, offset, false)
}

func (r *FollowRepository) list(ctx context.Context, userID string, limit, offset int, followers bool) (*models.FollowPage, error) {
	page := &models.FollowPage{Items: []*models.FollowProfile{}, Limit: limit, Offset: offset}
	field, joined := "f.followee_id", "f.follower_id"
	if !followers {
		field, joined = "f.follower_id", "f.followee_id"
	}
	// SQL fragments above are fixed literals, not caller-controlled input.
	query := `SELECT u.id,u.name,u.role,COALESCE(u.avatar_url,''),u.profile_path,f.created_at
		FROM user_follows f JOIN users u ON u.id=` + joined + ` AND u.status='active' AND u.role IN ('customer','isp','technician')
		WHERE ` + field + `=$1 ORDER BY f.created_at DESC,u.id DESC LIMIT $2 OFFSET $3`
	rows, err := r.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		profile := &models.FollowProfile{}
		if err := rows.Scan(&profile.ID, &profile.Name, &profile.Role, &profile.AvatarURL, &profile.ProfilePath, &profile.FollowedAt); err != nil {
			return nil, err
		}
		page.Items = append(page.Items, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	countSQL := `SELECT count(*) FROM user_follows f JOIN users u ON u.id=` + joined + ` AND u.status='active' AND u.role IN ('customer','isp','technician') WHERE ` + field + `=$1`
	if err := r.db.QueryRowContext(ctx, countSQL, userID).Scan(&page.Total); err != nil {
		return nil, err
	}
	return page, nil
}
