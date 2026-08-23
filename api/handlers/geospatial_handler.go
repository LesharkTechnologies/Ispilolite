package handlers

import (
	"net/http"
	"strconv"
	"strings"

	geo "ispilolite/internal/services/geospatial"
)

type GeospatialHandler struct{ service *geo.GeospatialService }

func NewGeospatialHandler(service *geo.GeospatialService) *GeospatialHandler {
	return &GeospatialHandler{service: service}
}
func (h *GeospatialHandler) NearbyISPs(w http.ResponseWriter, r *http.Request) {
	lat, lon, radius, limit := geoParams(r)
	v, err := h.service.FindNearbyISPs(r.Context(), lat, lon, radius, limit)
	if err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 200, map[string]interface{}{"success": true, "data": v})
}
func (h *GeospatialHandler) NearbyTechnicians(w http.ResponseWriter, r *http.Request) {
	lat, lon, radius, _ := geoParams(r)
	v, err := h.service.FindNearbyTechnicians(r.Context(), lat, lon, radius, strings.Split(r.URL.Query().Get("skills"), ","))
	if err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 200, map[string]interface{}{"success": true, "data": v})
}
func (h *GeospatialHandler) Coverage(w http.ResponseWriter, r *http.Request) {
	lat, lon, _, _ := geoParams(r)
	v, err := h.service.CheckCoverage(r.Context(), lat, lon)
	if err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 200, map[string]interface{}{"success": true, "data": v})
}
func (h *GeospatialHandler) Locations(w http.ResponseWriter, r *http.Request) {
	v, err := h.service.SearchLocations(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("type"), intQuery(r.URL.Query().Get("limit")))
	if err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 200, map[string]interface{}{"success": true, "data": v})
}
func geoParams(r *http.Request) (float64, float64, float64, int) {
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lon, _ := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	radius, _ := strconv.ParseFloat(r.URL.Query().Get("radius_km"), 64)
	return lat, lon, radius, intQuery(r.URL.Query().Get("limit"))
}
