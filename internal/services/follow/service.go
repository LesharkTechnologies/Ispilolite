// Package follow implements social follows for all user roles. ISP employment
// is a separate relationship in isp_technicians and is not changed here.
package follow

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/profilelink"
)

var (
	ErrInvalidUser = errors.New("invalid user ID")
	ErrSelfFollow  = errors.New("you cannot follow yourself")
	ErrNotFound    = errors.New("active user not found")
)

type Repository interface {
	Active(context.Context, string) (bool, error)
	Follow(context.Context, string, string) (bool, error)
	Unfollow(context.Context, string, string) (bool, error)
	Summary(context.Context, string, string) (*models.FollowSummary, error)
	Followers(context.Context, string, int, int) (*models.FollowPage, error)
	Following(context.Context, string, int, int) (*models.FollowPage, error)
}

type Service struct {
	repo  Repository
	links *profilelink.Builder
}

func NewService(repo Repository, links ...*profilelink.Builder) *Service {
	s := &Service{repo: repo}
	if len(links) > 0 {
		s.links = links[0]
	}
	return s
}

func validateID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrInvalidUser
	}
	return nil
}

func (s *Service) active(ctx context.Context, id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	active, err := s.repo.Active(ctx, id)
	if err != nil {
		return err
	}
	if !active {
		return ErrNotFound
	}
	return nil
}

func (s *Service) Follow(ctx context.Context, followerID, followeeID string) (bool, error) {
	followerID, followeeID = strings.TrimSpace(followerID), strings.TrimSpace(followeeID)
	if err := validateID(followerID); err != nil {
		return false, err
	}
	if err := validateID(followeeID); err != nil {
		return false, err
	}
	from, _ := uuid.Parse(followerID)
	to, _ := uuid.Parse(followeeID)
	if from == to {
		return false, ErrSelfFollow
	}
	if err := s.active(ctx, followerID); err != nil {
		return false, err
	}
	if err := s.active(ctx, followeeID); err != nil {
		return false, err
	}
	created, err := s.repo.Follow(ctx, followerID, followeeID)
	if err != nil || created {
		return created, err
	}
	// If the followee was deactivated between the read and INSERT, do not
	// accidentally report a duplicate as a success.
	if err := s.active(ctx, followerID); err != nil {
		return false, err
	}
	if err := s.active(ctx, followeeID); err != nil {
		return false, err
	}
	return false, nil
}

func (s *Service) Unfollow(ctx context.Context, followerID, followeeID string) error {
	if err := validateID(followerID); err != nil {
		return err
	}
	if err := validateID(followeeID); err != nil {
		return err
	}
	// Removing an old edge is allowed even if the followed account has since
	// been disabled. A second removal is an idempotent success.
	_, err := s.repo.Unfollow(ctx, followerID, followeeID)
	return err
}

func (s *Service) Summary(ctx context.Context, userID, viewerID string) (*models.FollowSummary, error) {
	if err := s.active(ctx, userID); err != nil {
		return nil, err
	}
	summary, err := s.repo.Summary(ctx, userID, viewerID)
	if err == nil {
		summary.ProfileURL = s.links.URL(summary.ProfilePath)
	}
	return summary, err
}

func pageSize(limit, offset int) (int, int) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 || offset > 10000 {
		offset = 0
	}
	return limit, offset
}

func (s *Service) Followers(ctx context.Context, userID string, limit, offset int) (*models.FollowPage, error) {
	if err := s.active(ctx, userID); err != nil {
		return nil, err
	}
	limit, offset = pageSize(limit, offset)
	page, err := s.repo.Followers(ctx, userID, limit, offset)
	if err == nil {
		s.addURLs(page)
	}
	return page, err
}

func (s *Service) Following(ctx context.Context, userID string, limit, offset int) (*models.FollowPage, error) {
	if err := s.active(ctx, userID); err != nil {
		return nil, err
	}
	limit, offset = pageSize(limit, offset)
	page, err := s.repo.Following(ctx, userID, limit, offset)
	if err == nil {
		s.addURLs(page)
	}
	return page, err
}

func (s *Service) addURLs(page *models.FollowPage) {
	for _, item := range page.Items {
		item.ProfileURL = s.links.URL(item.ProfilePath)
	}
}
