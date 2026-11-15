package social

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"ispilolite/internal/models"
)

var ErrInvalid = errors.New("invalid social action")

type Repository interface {
	AddFavorite(context.Context, string, string, string) (bool, error)
	RemoveFavorite(context.Context, string, string, string) error
	Favorites(context.Context, string, string, int, int) ([]*models.FavoriteProfile, int, error)
	React(context.Context, string, string, string, string) (bool, error)
	Unreact(context.Context, string, string, string, string) error
	ReactionSummary(context.Context, string, string, string) (*models.ReactionSummary, error)
	RegisterDevice(context.Context, string, string, string) (*models.DeviceToken, error)
	RemoveDevice(context.Context, string, string) error
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func normalize(value string) string  { return strings.ToLower(strings.TrimSpace(value)) }
func validID(value string) bool      { _, err := uuid.Parse(value); return err == nil }
func favoriteType(value string) bool { return value == "isp" || value == "technician" }
func reactionType(value string) bool {
	return value == "user_profile" || value == "technician_post" || value == "review"
}
func reaction(value string) bool { return value == "like" || value == "helpful" }

func (s *Service) Favorite(ctx context.Context, userID, targetType, targetID string) (bool, error) {
	targetType = normalize(targetType)
	if !favoriteType(targetType) || !validID(targetID) || targetID == userID {
		return false, ErrInvalid
	}
	return s.repo.AddFavorite(ctx, userID, targetType, targetID)
}
func (s *Service) Unfavorite(ctx context.Context, userID, targetType, targetID string) error {
	targetType = normalize(targetType)
	if !favoriteType(targetType) || !validID(targetID) {
		return ErrInvalid
	}
	return s.repo.RemoveFavorite(ctx, userID, targetType, targetID)
}
func (s *Service) Favorites(ctx context.Context, userID, targetType string, limit, offset int) ([]*models.FavoriteProfile, int, int, int, error) {
	targetType = normalize(targetType)
	if targetType != "" && !favoriteType(targetType) {
		return nil, 0, 0, 0, ErrInvalid
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	items, total, err := s.repo.Favorites(ctx, userID, targetType, limit, offset)
	return items, total, limit, offset, err
}
func (s *Service) React(ctx context.Context, userID, targetType, targetID, value string) (bool, error) {
	targetType, value = normalize(targetType), normalize(value)
	if !reactionType(targetType) || !reaction(value) || !validID(targetID) {
		return false, ErrInvalid
	}
	return s.repo.React(ctx, userID, targetType, targetID, value)
}
func (s *Service) Unreact(ctx context.Context, userID, targetType, targetID, value string) error {
	targetType, value = normalize(targetType), normalize(value)
	if !reactionType(targetType) || !reaction(value) || !validID(targetID) {
		return ErrInvalid
	}
	return s.repo.Unreact(ctx, userID, targetType, targetID, value)
}
func (s *Service) Reactions(ctx context.Context, userID, targetType, targetID string) (*models.ReactionSummary, error) {
	targetType = normalize(targetType)
	if !reactionType(targetType) || !validID(targetID) {
		return nil, ErrInvalid
	}
	return s.repo.ReactionSummary(ctx, userID, targetType, targetID)
}
func (s *Service) RegisterDevice(ctx context.Context, userID, platform, token string) (*models.DeviceToken, error) {
	platform, token = normalize(platform), strings.TrimSpace(token)
	if !map[string]bool{"android": true, "ios": true, "web": true}[platform] || len(token) < 16 || len(token) > 4096 {
		return nil, ErrInvalid
	}
	return s.repo.RegisterDevice(ctx, userID, platform, token)
}
func (s *Service) RemoveDevice(ctx context.Context, userID, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalid
	}
	return s.repo.RemoveDevice(ctx, userID, token)
}
