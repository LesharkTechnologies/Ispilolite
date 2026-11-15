package models

import "time"

type FavoriteProfile struct {
	ID         string    `json:"id"`
	TargetType string    `json:"target_type"`
	Name       string    `json:"name"`
	AvatarURL  string    `json:"avatar_url"`
	Rating     float64   `json:"rating"`
	AddedAt    time.Time `json:"added_at"`
}

type ReactionSummary struct {
	TargetType  string         `json:"target_type"`
	TargetID    string         `json:"target_id"`
	Counts      map[string]int `json:"counts"`
	MyReactions []string       `json:"my_reactions"`
}

type DeviceToken struct {
	ID         string    `json:"id"`
	UserID     string    `json:"-"`
	Platform   string    `json:"platform"`
	Token      string    `json:"token,omitempty"`
	Enabled    bool      `json:"enabled"`
	LastSeenAt time.Time `json:"last_seen_at"`
}
