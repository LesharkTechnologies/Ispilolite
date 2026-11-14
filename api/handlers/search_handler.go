package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/search"
)

// SearchHandler exposes the HTTP search endpoints used by the main API router.
// It wraps the production search service (Elasticsearch fast path + Postgres
// fallback + circuit breaker).
type SearchHandler struct {
	svc *search.Service
}

// NewSearchHandler creates a new search handler backed by a configured search
// service. If svc is nil, every request returns an empty degraded result.
func NewSearchHandler(svc *search.Service) *SearchHandler {
	return &SearchHandler{svc: svc}
}

// SearchLocations handles GET /api/v1/search/locations.
func (h *SearchHandler) SearchLocations(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		h.respondDegraded(w)
		return
	}
	p := dto.ParseSearchParams(r)
	h.respondSearch(w, h.svc.SearchLocations(r.Context(), p))
}

// SearchISPs handles GET /api/v1/search/isps.
func (h *SearchHandler) SearchISPs(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		h.respondDegraded(w)
		return
	}
	p := dto.ParseSearchParams(r)
	h.respondSearch(w, h.svc.SearchISPs(r.Context(), p))
}

// SearchTechnicians handles GET /api/v1/search/technicians.
func (h *SearchHandler) SearchTechnicians(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		h.respondDegraded(w)
		return
	}
	p := dto.ParseSearchParams(r)
	h.respondSearch(w, h.svc.SearchTechnicians(r.Context(), p))
}

// Suggest handles GET /api/v1/search/suggest?q=mac&domain=all. domain may be
// location, isp, technician, or all (the default).
func (h *SearchHandler) Suggest(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		h.respondDegraded(w)
		return
	}
	prefix := strings.TrimSpace(r.URL.Query().Get("q"))
	if prefix == "" {
		respondWithErrorRaw(w, http.StatusBadRequest, "q is required")
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if size <= 0 || size > 25 {
		size = 8
	}
	domain := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	if domain == "" {
		domain = "all"
	}
	var suggestions []dto.Suggestion
	var source string
	if domain == "all" {
		suggestions, source = h.svc.SuggestAcross(r.Context(), prefix, size)
	} else {
		switch domain {
		case "location", "locations", "place", "places", "isp", "isps", "tech", "technician", "technicians":
		default:
			respondWithErrorRaw(w, http.StatusBadRequest, "domain must be all, location, isp, or technician")
			return
		}
		suggestions, source = h.svc.Suggest(r.Context(), domain, prefix, size)
	}

	result := dto.SearchResult{Items: []interface{}{}, Suggestions: suggestions,
		Meta: dto.SearchMeta{Source: source, Total: len(suggestions), Query: prefix, Fallback: source != "elasticsearch"}}
	// When autocomplete has no location prefix hit, reuse fuzzy location search
	// to return a spelling correction.
	if len(suggestions) == 0 && (domain == "all" || strings.HasPrefix(domain, "loc") || strings.HasPrefix(domain, "place")) {
		request := *r
		query := request.URL.Query()
		query.Set("q", prefix)
		request.URL.RawQuery = query.Encode()
		locationResult := h.svc.SearchLocations(request.Context(), dto.ParseSearchParams(&request))
		result.DidYouMean = locationResult.DidYouMean
	}
	h.respondSearch(w, result)
}

// Health handles GET /api/v1/search/health. It reports the circuit-breaker
// state per endpoint and the latest backend health (Elasticsearch + Postgres).
func (h *SearchHandler) Health(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(dto.APIResponse{
			Success: false,
			Message: "search service unavailable",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	health := h.svc.Health(ctx)
	breakerStates := h.svc.BreakerStates()

	// Determine overall status: degraded if any backend is down.
	overall := "healthy"
	for _, bs := range health {
		if !bs.Up {
			overall = "degraded"
			break
		}
	}

	type healthResponse struct {
		Overall  string                           `json:"overall"`
		Breakers map[string]string                `json:"breakers"`
		Backends map[string]*search.BackendStatus `json:"backends"`
		Fallback bool                             `json:"fallback"`
		Strategy string                           `json:"strategy"`
	}

	resp := healthResponse{
		Overall:  overall,
		Breakers: breakerStates,
		Backends: health,
		Fallback: overall == "degraded",
		Strategy: "es_first",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dto.APIResponse{
		Success: true,
		Data:    resp,
	})
}

func (h *SearchHandler) respondSearch(w http.ResponseWriter, res dto.SearchResult) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dto.APIResponse{
		Success: true,
		Data:    res,
	})
}

func (h *SearchHandler) respondDegraded(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(dto.APIResponse{
		Success: false,
		Message: "search service unavailable",
	})
}
