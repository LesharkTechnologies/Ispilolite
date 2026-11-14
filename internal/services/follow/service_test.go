package follow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"ispilolite/internal/models"
)

type fakeRepo struct {
	active map[string]bool
	edges  map[string]map[string]bool
}

func (r *fakeRepo) Active(_ context.Context, id string) (bool, error) { return r.active[id], nil }
func (r *fakeRepo) Follow(_ context.Context, from, to string) (bool, error) {
	if r.edges[from] == nil {
		r.edges[from] = make(map[string]bool)
	}
	if r.edges[from][to] {
		return false, nil
	}
	r.edges[from][to] = true
	return true, nil
}
func (r *fakeRepo) Unfollow(_ context.Context, from, to string) (bool, error) {
	was := r.edges[from][to]
	delete(r.edges[from], to)
	return was, nil
}
func (r *fakeRepo) Summary(_ context.Context, id, viewer string) (*models.FollowSummary, error) {
	s := &models.FollowSummary{UserID: id, FollowingCount: len(r.edges[id]), IsFollowing: r.edges[viewer][id]}
	for _, following := range r.edges {
		if following[id] {
			s.FollowerCount++
		}
	}
	return s, nil
}
func (r *fakeRepo) Followers(_ context.Context, _ string, limit, offset int) (*models.FollowPage, error) {
	return &models.FollowPage{Limit: limit, Offset: offset}, nil
}
func (r *fakeRepo) Following(_ context.Context, _ string, limit, offset int) (*models.FollowPage, error) {
	return &models.FollowPage{Limit: limit, Offset: offset}, nil
}

func TestFollowsCrossRolesWithoutCompanyMembership(t *testing.T) {
	customer, technician, isp := uuid.NewString(), uuid.NewString(), uuid.NewString()
	r := &fakeRepo{
		active: map[string]bool{customer: true, technician: true, isp: true},
		edges:  make(map[string]map[string]bool),
	}
	s := NewService(r)
	for _, pair := range [][2]string{{customer, technician}, {technician, isp}, {isp, customer}} {
		created, err := s.Follow(context.Background(), pair[0], pair[1])
		if err != nil || !created {
			t.Fatalf("cross-role follow failed: %v %v", created, err)
		}
	}
	if created, err := s.Follow(context.Background(), customer, technician); err != nil || created {
		t.Fatalf("duplicate follow should be an idempotent success: %v %v", created, err)
	}
	profile, err := s.Summary(context.Background(), technician, customer)
	if err != nil || profile.FollowerCount != 1 || !profile.IsFollowing {
		t.Fatalf("summary incorrect: %+v %v", profile, err)
	}
	if err := s.Unfollow(context.Background(), customer, technician); err != nil {
		t.Fatal(err)
	}
	if err := s.Unfollow(context.Background(), customer, technician); err != nil {
		t.Fatalf("unfollow must be idempotent: %v", err)
	}
}

func TestFollowValidation(t *testing.T) {
	viewer := uuid.NewString()
	missing := uuid.NewString()
	s := NewService(&fakeRepo{active: map[string]bool{viewer: true}, edges: make(map[string]map[string]bool)})
	if _, err := s.Follow(context.Background(), viewer, viewer); !errors.Is(err, ErrSelfFollow) {
		t.Fatalf("self-follow: %v", err)
	}
	if _, err := s.Follow(context.Background(), viewer, strings.ToUpper(viewer)); !errors.Is(err, ErrSelfFollow) {
		t.Fatalf("case-variant self-follow: %v", err)
	}
	if _, err := s.Follow(context.Background(), viewer, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive user: %v", err)
	}
	if _, err := s.Follow(context.Background(), viewer, "bad-id"); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("invalid ID: %v", err)
	}
	if _, err := s.Summary(context.Background(), missing, viewer); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive profile: %v", err)
	}
	page, err := s.Followers(context.Background(), viewer, 500, -1)
	if err != nil || page.Limit != 20 || page.Offset != 0 {
		t.Fatalf("pagination not bounded: %+v %v", page, err)
	}
}
