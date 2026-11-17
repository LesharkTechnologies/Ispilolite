package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/middleware"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
)

type speedTestReader interface {
	ISPOptions(string, string) ([]*models.SpeedTestISPOption, error)
	Create(*models.SpeedTest, string) error
	ListMine(string, int) ([]*models.SpeedTest, error)
	GetByID(context.Context, string, string) (*models.SpeedTest, error)
	Delete(context.Context, string, string) error
	Stats(string, string, string) (*models.SpeedTestStats, error)
	GetCountyMonthlyStats(context.Context, string, string, int, int) (*models.CountyMonthlySpeedStats, error)
	ListCountyBenchmarks(context.Context, int, int) ([]*models.CountySpeedBenchmark, error)
	ListTownStats(context.Context, string, int, int) ([]*models.CountyMonthlySpeedStats, error)
	AggregateMonthlySpeedStats(context.Context, int, int) error
}

type speedTestAdminReader interface {
	ListModeration(string, int) ([]*models.SpeedTestModerationItem, error)
	Moderate(string, string, string) error
}

type SpeedTestHandler struct{ repo speedTestReader }

func NewSpeedTestHandler() (*SpeedTestHandler, error) {
	repo, err := postgres.NewSpeedTestRepository()
	if err != nil {
		return nil, err
	}
	return &SpeedTestHandler{repo: repo}, nil
}

func newSpeedTestHandler(repo speedTestReader) *SpeedTestHandler {
	return &SpeedTestHandler{repo: repo}
}

// ISPOptions is intentionally public. A user can choose a town before running
// a test and receive registered ISP suggestions without logging in.
func (h *SpeedTestHandler) ISPOptions(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ISPOptions(r.URL.Query().Get("county"), r.URL.Query().Get("town"))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"items": items}})
}

func (h *SpeedTestHandler) Create(w http.ResponseWriter, r *http.Request) {
	var item models.SpeedTest
	if decodeJSON(w, r, &item) != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateSpeedTest(&item); err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	if item.ISPID != "" {
		item.ISPSelectionSource = "registered"
	} else if strings.TrimSpace(item.ISPName) != "" {
		item.ISPSelectionSource = "user_entered"
	} else {
		item.ISPSelectionSource = "none"
	}
	item.UserID = userIDFromContext(r.Context())
	// This is deliberately resolved from the trusted proxy-aware address, not
	// from a client field or an untrusted X-Forwarded-For header.
	ip := middleware.ClientIP(r)
	if strings.TrimSpace(ip) == "" {
		respondWithErrorRaw(w, http.StatusBadRequest, "client IP could not be determined")
		return
	}
	if err := h.repo.Create(&item, ip); err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	// The response keeps the result private to the submitting user. ISP and
	// public reporting endpoints use aggregates and never expose this IP.
	item.Status = "pending"
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: item})
}

func (h *SpeedTestHandler) Mine(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	items, err := h.repo.ListMine(userIDFromContext(r.Context()), limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"items": items, "count": len(items)}})
}

func (h *SpeedTestHandler) ISPStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.repo.Stats(userIDFromContext(r.Context()), r.URL.Query().Get("county"), r.URL.Query().Get("town"))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: stats})
}

func (h *SpeedTestHandler) AdminQueue(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.repo.(speedTestAdminReader)
	if !ok {
		respondWithInternalError(w, nil)
		return
	}
	items, err := repo.ListModeration(r.URL.Query().Get("status"), 50)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"items": items, "count": len(items)}})
}

func (h *SpeedTestHandler) AdminModerate(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.repo.(speedTestAdminReader)
	if !ok {
		respondWithInternalError(w, nil)
		return
	}
	var request struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if decodeJSON(w, r, &request) != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	status := strings.ToLower(strings.TrimSpace(request.Status))
	if err := repo.Moderate(pathParam(r.URL.Path, "/api/v1/admin/speed-tests/"), status, request.Reason); err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]string{"status": status}})
}

func (h *SpeedTestHandler) CountyStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	county := strings.TrimSpace(q.Get("county"))
	town := strings.TrimSpace(q.Get("town"))
	year, _ := strconv.Atoi(q.Get("year"))
	month, _ := strconv.Atoi(q.Get("month"))

	stats, err := h.repo.GetCountyMonthlyStats(r.Context(), county, town, year, month)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}

	data := map[string]any{"stats": stats}
	if q.Get("include_towns") == "true" && county != "" {
		if towns, err := h.repo.ListTownStats(r.Context(), county, year, month); err == nil {
			data["towns"] = towns
		}
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: data})
}

func (h *SpeedTestHandler) CountyBenchmarks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	year, _ := strconv.Atoi(q.Get("year"))
	month, _ := strconv.Atoi(q.Get("month"))

	benchmarks, err := h.repo.ListCountyBenchmarks(r.Context(), year, month)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"benchmarks": benchmarks, "count": len(benchmarks)}})
}

func (h *SpeedTestHandler) TownStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	county := strings.TrimSpace(q.Get("county"))
	if county == "" {
		respondWithErrorRaw(w, http.StatusBadRequest, "county is required")
		return
	}
	year, _ := strconv.Atoi(q.Get("year"))
	month, _ := strconv.Atoi(q.Get("month"))

	towns, err := h.repo.ListTownStats(r.Context(), county, year, month)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"towns": towns, "count": len(towns)}})
}

func (h *SpeedTestHandler) GetSpeedTest(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/speed-tests/")
	if id == "" {
		id = pathParam(r.URL.Path, "/api/v1/speed-tests/")
	}
	if id == "" {
		respondWithErrorRaw(w, http.StatusBadRequest, "speed test id is required")
		return
	}
	userID := userIDFromContext(r.Context())
	item, err := h.repo.GetByID(r.Context(), id, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "speed test not found")
			return
		}
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: item})
}

func (h *SpeedTestHandler) DeleteSpeedTest(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/speed-tests/")
	if id == "" {
		id = pathParam(r.URL.Path, "/api/v1/speed-tests/")
	}
	if id == "" {
		respondWithErrorRaw(w, http.StatusBadRequest, "speed test id is required")
		return
	}
	userID := userIDFromContext(r.Context())
	if err := h.repo.Delete(r.Context(), id, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "speed test not found")
			return
		}
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "speed test deleted"})
}

func (h *SpeedTestHandler) AdminAggregate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Year  int `json:"year"`
		Month int `json:"month"`
	}
	_ = decodeJSON(w, r, &body)
	now := time.Now().UTC()
	if body.Year <= 0 {
		body.Year = now.Year()
	}
	if body.Month <= 0 || body.Month > 12 {
		body.Month = int(now.Month())
	}
	if err := h.repo.AggregateMonthlySpeedStats(r.Context(), body.Year, body.Month); err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: fmt.Sprintf("monthly speed statistics aggregated for %s %d", time.Month(body.Month).String(), body.Year)})
}

func validateSpeedTest(item *models.SpeedTest) error {
	if item.DownloadMbps < 0 || item.DownloadMbps > 100000 {
		return errSpeed("download_mbps must be between 0 and 100000")
	}
	if item.UploadMbps < 0 || item.UploadMbps > 100000 {
		return errSpeed("upload_mbps must be between 0 and 100000")
	}
	if item.LatencyMs < 0 || item.LatencyMs > 60000 {
		return errSpeed("latency_ms must be between 0 and 60000")
	}
	if item.JitterMs != nil && (*item.JitterMs < 0 || *item.JitterMs > 60000) {
		return errSpeed("jitter_ms must be between 0 and 60000")
	}
	if item.PacketLossPercent != nil && (*item.PacketLossPercent < 0 || *item.PacketLossPercent > 100) {
		return errSpeed("packet_loss_percent must be between 0 and 100")
	}
	if item.ISPPlanSpeedMbps != nil && (*item.ISPPlanSpeedMbps < 0 || *item.ISPPlanSpeedMbps > 100000) {
		return errSpeed("isp_plan_speed_mbps must be between 0 and 100000")
	}
	if strings.TrimSpace(item.County) == "" {
		return errSpeed("county is required")
	}
	if len(item.County) > 120 || len(item.Town) > 120 || len(item.ISPName) > 160 {
		return errSpeed("place and ISP names are too long")
	}
	return nil
}

type speedValidationError string

func (e speedValidationError) Error() string { return string(e) }
func errSpeed(value string) error            { return speedValidationError(value) }
