package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"ispilolite/api/dto"
	"ispilolite/internal/repository/postgres"
	socialsvc "ispilolite/internal/services/social"
)

type SocialHandler struct{ service *socialsvc.Service }

func NewSocialHandler() (*SocialHandler, error) {
	repo, err := postgres.NewSocialRepository()
	if err != nil {
		return nil, err
	}
	return &SocialHandler{service: socialsvc.NewService(repo)}, nil
}

func socialTarget(path, prefix string) (string, string) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, prefix), "/"), "/")
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
func favoriteTarget(path string) (string, string) {
	if strings.HasPrefix(path, "/api/v1/users/favorites/") {
		return socialTarget(path, "/api/v1/users/favorites/")
	}
	return socialTarget(path, "/api/v1/favorites/")
}

func (h *SocialHandler) Favorite(w http.ResponseWriter, r *http.Request) {
	targetType, targetID := favoriteTarget(r.URL.Path)
	created, err := h.service.Favorite(r.Context(), userIDFromContext(r.Context()), targetType, targetID)
	if err != nil {
		socialError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondWithJSON(w, status, dto.APIResponse{Success: true, Data: map[string]interface{}{"target_type": targetType, "target_id": targetID, "is_favorite": true}})
}
func (h *SocialHandler) Unfavorite(w http.ResponseWriter, r *http.Request) {
	targetType, targetID := favoriteTarget(r.URL.Path)
	if err := h.service.Unfavorite(r.Context(), userIDFromContext(r.Context()), targetType, targetID); err != nil {
		socialError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"target_type": targetType, "target_id": targetID, "is_favorite": false}})
}
func (h *SocialHandler) Favorites(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, total, limit, offset, err := h.service.Favorites(r.Context(), userIDFromContext(r.Context()), r.URL.Query().Get("type"), limit, offset)
	if err != nil {
		socialError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"items": items, "total": total, "limit": limit, "offset": offset}})
}
func reactionInput(w http.ResponseWriter, r *http.Request) (string, error) {
	var input struct {
		Reaction string `json:"reaction"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Reaction) == "" {
		input.Reaction = "like"
	}
	return input.Reaction, nil
}
func (h *SocialHandler) React(w http.ResponseWriter, r *http.Request) {
	targetType, targetID := socialTarget(r.URL.Path, "/api/v1/reactions/")
	value, err := reactionInput(w, r)
	if err != nil {
		respondWithError(w, 400, "invalid reaction")
		return
	}
	created, err := h.service.React(r.Context(), userIDFromContext(r.Context()), targetType, targetID, value)
	if err != nil {
		socialError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	summary, err := h.service.Reactions(r.Context(), userIDFromContext(r.Context()), targetType, targetID)
	if err != nil {
		socialError(w, err)
		return
	}
	respondWithJSON(w, status, dto.APIResponse{Success: true, Data: summary})
}
func (h *SocialHandler) Unreact(w http.ResponseWriter, r *http.Request) {
	targetType, targetID := socialTarget(r.URL.Path, "/api/v1/reactions/")
	value := r.URL.Query().Get("reaction")
	if value == "" {
		value = "like"
	}
	if err := h.service.Unreact(r.Context(), userIDFromContext(r.Context()), targetType, targetID, value); err != nil {
		socialError(w, err)
		return
	}
	summary, err := h.service.Reactions(r.Context(), userIDFromContext(r.Context()), targetType, targetID)
	if err != nil {
		socialError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: summary})
}
func (h *SocialHandler) Reactions(w http.ResponseWriter, r *http.Request) {
	targetType, targetID := socialTarget(r.URL.Path, "/api/v1/reactions/")
	summary, err := h.service.Reactions(r.Context(), userIDFromContext(r.Context()), targetType, targetID)
	if err != nil {
		socialError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: summary})
}
func (h *SocialHandler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Platform string `json:"platform"`
		Token    string `json:"token"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithError(w, 400, "invalid device token")
		return
	}
	item, err := h.service.RegisterDevice(r.Context(), userIDFromContext(r.Context()), input.Platform, input.Token)
	if err != nil {
		socialError(w, err)
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: item})
}
func (h *SocialHandler) RemoveDevice(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		token = pathParam(r.URL.Path, "/api/v1/my/devices/")
	}
	if token == "" {
		var input struct {
			Token string `json:"token"`
		}
		if decodeJSON(w, r, &input) != nil || strings.TrimSpace(input.Token) == "" {
			respondWithError(w, 400, "invalid device token")
			return
		}
		token = strings.TrimSpace(input.Token)
	}
	if err := h.service.RemoveDevice(r.Context(), userIDFromContext(r.Context()), token); err != nil {
		socialError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "device unregistered"})
}
func socialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, socialsvc.ErrInvalid):
		respondWithError(w, 400, err.Error())
	case errors.Is(err, sql.ErrNoRows):
		respondWithError(w, 404, "target not found")
	default:
		respondWithInternalError(w, err)
	}
}
