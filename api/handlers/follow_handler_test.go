package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	followsvc "ispilolite/internal/services/follow"
)

type followRepoStub struct{ active map[string]bool }

func (s *followRepoStub) Active(_ context.Context, id string) (bool, error)    { return s.active[id], nil }
func (*followRepoStub) Follow(context.Context, string, string) (bool, error)   { return true, nil }
func (*followRepoStub) Unfollow(context.Context, string, string) (bool, error) { return true, nil }
func (*followRepoStub) Summary(_ context.Context, id, _ string) (*models.FollowSummary, error) {
	return &models.FollowSummary{UserID: id, FollowerCount: 1, IsFollowing: true}, nil
}
func (*followRepoStub) Followers(_ context.Context, _ string, limit, offset int) (*models.FollowPage, error) {
	return &models.FollowPage{Items: []*models.FollowProfile{{ID: uuid.NewString(), Name: "Sample", Role: "technician", AvatarURL: "", FollowedAt: time.Now()}}, Total: 1, Limit: limit, Offset: offset}, nil
}
func (s *followRepoStub) Following(ctx context.Context, id string, limit, offset int) (*models.FollowPage, error) {
	return s.Followers(ctx, id, limit, offset)
}

func TestFollowEndpointResponseAndPrivateFields(t *testing.T) {
	viewer, target := uuid.NewString(), uuid.NewString()
	h := &FollowHandler{service: followsvc.NewService(&followRepoStub{active: map[string]bool{viewer: true, target: true}})}
	invoke := func(method, path string, fn http.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r = r.WithContext(context.WithValue(r.Context(), "userID", viewer))
		w := httptest.NewRecorder()
		fn(w, r)
		return w
	}
	w := invoke(http.MethodPost, "/api/v1/users/"+target+"/follow", h.Follow)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"is_following":true`) {
		t.Fatalf("follow result: %d %s", w.Code, w.Body.String())
	}
	w = invoke(http.MethodGet, "/api/v1/users/"+target+"/followers", h.Followers)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":1`) || strings.Contains(w.Body.String(), `"phone"`) || strings.Contains(w.Body.String(), `"email"`) {
		t.Fatalf("followers leaked private fields or missing pagination: %d %s", w.Code, w.Body.String())
	}
	w = invoke(http.MethodPost, "/api/v1/users/"+viewer+"/follow", h.Follow)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("self follow status: %d %s", w.Code, w.Body.String())
	}
}
