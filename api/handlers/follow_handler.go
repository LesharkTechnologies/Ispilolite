package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"ispilolite/api/dto"
	"ispilolite/internal/repository/postgres"
	followsvc "ispilolite/internal/services/follow"
	appnotifications "ispilolite/pkg/notifications"
	"ispilolite/pkg/profilelink"
)

type FollowHandler struct {
	service  *followsvc.Service
	notifier *appnotifications.Service
}

func NewFollowHandler(links *profilelink.Builder) (*FollowHandler, error) {
	repo, err := postgres.NewFollowRepository()
	if err != nil {
		return nil, err
	}
	notificationRepo, err := postgres.NewNotificationRepositoryAdapter()
	if err != nil {
		return nil, err
	}
	return &FollowHandler{service: followsvc.NewService(repo, links), notifier: appnotifications.NewService(notificationRepo, nil)}, nil
}

func followTarget(r *http.Request) string {
	return pathParam(r.URL.Path, "/api/v1/users/")
}

func (h *FollowHandler) Follow(w http.ResponseWriter, r *http.Request) {
	target := followTarget(r)
	created, err := h.service.Follow(r.Context(), userIDFromContext(r.Context()), target)
	if err != nil {
		followError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		if h.notifier != nil {
			_ = h.notifier.Notify(r.Context(), target, "user.followed", map[string]interface{}{"follower_id": userIDFromContext(r.Context())})
		}
	}
	summary, err := h.service.Summary(r.Context(), target, userIDFromContext(r.Context()))
	if err != nil {
		followError(w, err)
		return
	}
	respondWithJSON(w, status, dto.APIResponse{Success: true, Data: summary})
}

func (h *FollowHandler) Unfollow(w http.ResponseWriter, r *http.Request) {
	target := followTarget(r)
	if err := h.service.Unfollow(r.Context(), userIDFromContext(r.Context()), target); err != nil {
		followError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"user_id": target, "is_following": false}})
}

func (h *FollowHandler) Summary(w http.ResponseWriter, r *http.Request) {
	target := followTarget(r)
	if target == "" {
		target = userIDFromContext(r.Context())
	}
	summary, err := h.service.Summary(r.Context(), target, userIDFromContext(r.Context()))
	if err != nil {
		followError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: summary})
}

func followPageParams(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return limit, offset
}

func (h *FollowHandler) Followers(w http.ResponseWriter, r *http.Request) {
	target := followTarget(r)
	if target == "" {
		target = userIDFromContext(r.Context())
	}
	limit, offset := followPageParams(r)
	page, err := h.service.Followers(r.Context(), target, limit, offset)
	if err != nil {
		followError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: page})
}

func (h *FollowHandler) Following(w http.ResponseWriter, r *http.Request) {
	target := followTarget(r)
	if target == "" {
		target = userIDFromContext(r.Context())
	}
	limit, offset := followPageParams(r)
	page, err := h.service.Following(r.Context(), target, limit, offset)
	if err != nil {
		followError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: page})
}

func followError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, followsvc.ErrInvalidUser), errors.Is(err, followsvc.ErrSelfFollow):
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, followsvc.ErrNotFound):
		respondWithErrorRaw(w, http.StatusNotFound, err.Error())
	default:
		respondWithInternalError(w, err)
	}
}
