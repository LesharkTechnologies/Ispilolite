package models

// PublicProfile deliberately excludes phone, email, employment details and
// other private fields. Only active customers, technicians and ISPs resolve.
type PublicProfile struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DisplayName    string `json:"display_name,omitempty"`
	VerifiedName   string `json:"verified_name,omitempty"`
	IdentityLabel  string `json:"identity_label,omitempty"`
	IdentityStatus string `json:"identity_status,omitempty"`
	Role           string `json:"role"`
	AvatarURL      string `json:"avatar_url"`
	ProfileSlug    string `json:"profile_slug"`
	ProfilePath    string `json:"profile_path"`
	ProfileURL     string `json:"profile_url"`
	FollowerCount  int    `json:"follower_count"`
	FollowingCount int    `json:"following_count"`
	FavoriteCount  int    `json:"favorite_count"`
	LikeCount      int    `json:"like_count"`
}
