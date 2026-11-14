package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"ispilolite/internal/services/matching"
)

type MatchingHandler struct{ service *matching.MatchingService }

func NewMatchingHandler(service *matching.MatchingService) *MatchingHandler {
	return &MatchingHandler{service: service}
}

func (h *MatchingHandler) Respond(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(pathParam(r.URL.Path, "/api/v1/matches/"), "/respond")
	if err := h.service.Respond(r.Context(), id, userIDFromContext(r.Context())); err != nil {
		respondWithError(w, http.StatusConflict, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "match acknowledged"})
}

func (h *MatchingHandler) Decline(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(pathParam(r.URL.Path, "/api/v1/matches/"), "/decline")
	if err := h.service.Decline(r.Context(), id, userIDFromContext(r.Context())); err != nil {
		respondWithError(w, http.StatusConflict, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "match declined"})
}
func (h *MatchingHandler) Match(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/matches/request/")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}
	items, err := h.service.MatchProviders(r.Context(), id, matching.MatchOptions{RadiusKM: 10})
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	if len(items) > limit {
		items = items[:limit]
	}
	respondWithJSON(w, 200, map[string]interface{}{"success": true, "data": items})
}
