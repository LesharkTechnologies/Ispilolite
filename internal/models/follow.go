package models

import "time"

// FollowProfile exposes only public account fields, never phone or email.
type FollowProfile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Role        string    `json:"role"`
	AvatarURL   string    `json:"avatar_url"`
	ProfilePath string    `json:"profile_path"`
	ProfileURL  string    `json:"profile_url"`
	FollowedAt  time.Time `json:"followed_at"`
}

type FollowPage struct {
	Items  []*FollowProfile `json:"items"`
	Total  int              `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

type FollowSummary struct {
	UserID         string `json:"user_id"`
	FollowerCount  int    `json:"follower_count"`
	FollowingCount int    `json:"following_count"`
	IsFollowing    bool   `json:"is_following"`
	ProfilePath    string `json:"profile_path"`
	ProfileURL     string `json:"profile_url"`
}
