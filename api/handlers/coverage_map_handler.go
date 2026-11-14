package handlers

import (
	"ispilolite/api/dto"
	"ispilolite/internal/models"
	coveragemapsvc "ispilolite/internal/services/coveragemap"
	"net/http"
	"strconv"
)

type CoverageMapHandler struct{ service *coveragemapsvc.Service }

func NewCoverageMapHandler(service *coveragemapsvc.Service) *CoverageMapHandler {
	return &CoverageMapHandler{service: service}
}
func (h *CoverageMapHandler) Create(w http.ResponseWriter, r *http.Request) {
	item := &models.CoveragePolygon{}
	if decodeJSON(w, r, item) != nil {
		respondWithErrorRaw(w, 400, "invalid GeoJSON polygon")
		return
	}
	created, err := h.service.Create(r.Context(), userIDFromContext(r.Context()), item)
	if err != nil {
		respondWithErrorRaw(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: created})
}
func (h *CoverageMapHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var lat, lon *float64
	if q.Get("lat") != "" && q.Get("lon") != "" {
		a, aErr := strconv.ParseFloat(q.Get("lat"), 64)
		o, oErr := strconv.ParseFloat(q.Get("lon"), 64)
		if aErr != nil || oErr != nil {
			respondWithErrorRaw(w, 400, "lat and lon must be numbers")
			return
		}
		lat, lon = &a, &o
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	items, err := h.service.FeatureCollection(r.Context(), q.Get("isp_id"), q.Get("county"), lat, lon, limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}
