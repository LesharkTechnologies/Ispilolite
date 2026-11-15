package postgres

import (
	"context"
	"database/sql"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type ProfileRepository struct{ db *sql.DB }

func NewProfileRepository() (*ProfileRepository, error) {
	db, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &ProfileRepository{db: db}, nil
}

const publicProfileSelect = `SELECT u.id,u.name,u.role,
	COALESCE(NULLIF(u.avatar_url,''),NULLIF(i.logo_url,''),''),
	u.profile_slug,u.profile_path,
	(SELECT count(*) FROM user_follows f JOIN users v ON v.id=f.follower_id AND v.status='active' AND v.role IN ('customer','isp','technician') WHERE f.followee_id=u.id),
	(SELECT count(*) FROM user_follows f JOIN users v ON v.id=f.followee_id AND v.status='active' AND v.role IN ('customer','isp','technician') WHERE f.follower_id=u.id),
	(SELECT count(*) FROM user_favorites f WHERE f.target_id=u.id),
	(SELECT count(*) FROM user_reactions r WHERE r.target_type='user_profile' AND r.target_id=u.id AND r.reaction_type='like')
	FROM users u LEFT JOIN isps i ON i.id=u.id
	WHERE u.status='active' AND u.role IN ('customer','isp','technician') AND `

func scanPublicProfile(row *sql.Row) (*models.PublicProfile, error) {
	profile := &models.PublicProfile{}
	err := row.Scan(&profile.ID, &profile.Name, &profile.Role, &profile.AvatarURL,
		&profile.ProfileSlug, &profile.ProfilePath, &profile.FollowerCount, &profile.FollowingCount, &profile.FavoriteCount, &profile.LikeCount)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (r *ProfileRepository) BySlug(ctx context.Context, slug string) (*models.PublicProfile, error) {
	return scanPublicProfile(r.db.QueryRowContext(ctx, publicProfileSelect+`u.profile_slug=$1`, slug))
}

func (r *ProfileRepository) ByUserID(ctx context.Context, userID string) (*models.PublicProfile, error) {
	return scanPublicProfile(r.db.QueryRowContext(ctx, publicProfileSelect+`u.id=$1`, userID))
}
