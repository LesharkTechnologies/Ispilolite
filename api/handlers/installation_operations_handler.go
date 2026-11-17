package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
)

type speedStatsReader interface {
	GetCountyMonthlyStats(context.Context, string, string, int, int) (*models.CountyMonthlySpeedStats, error)
}

type InstallationOperationsHandler struct {
	repo      *postgres.InstallationOperationsRepository
	speedRepo speedStatsReader
}

func NewInstallationOperationsHandler() (*InstallationOperationsHandler, error) {
	repo, err := postgres.NewInstallationOperationsRepository()
	if err != nil {
		return nil, err
	}
	speedRepo, err := postgres.NewSpeedTestRepository()
	if err != nil {
		return nil, err
	}
	return &InstallationOperationsHandler{repo: repo, speedRepo: speedRepo}, nil
}

func (h *InstallationOperationsHandler) CheckServiceability(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	county := strings.TrimSpace(q.Get("county"))
	town := strings.TrimSpace(q.Get("town"))
	items, err := h.repo.CheckServiceability(q.Get("location_id"), county, town, q.Get("estate"), q.Get("building"))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}

	data := map[string]interface{}{
		"items": items,
		"count": len(items),
	}

	lookupCounty := county
	lookupTown := town
	if lookupCounty == "" && len(items) > 0 {
		lookupCounty = items[0].County
		if lookupTown == "" {
			lookupTown = items[0].Town
		}
	}

	if lookupCounty != "" && h.speedRepo != nil {
		if stats, err := h.speedRepo.GetCountyMonthlyStats(r.Context(), lookupCounty, lookupTown, 0, 0); err == nil && stats != nil {
			data["speed_recommendation"] = stats
		}
	}

	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: data})
}

func (h *InstallationOperationsHandler) UpsertServiceability(w http.ResponseWriter, r *http.Request) {
	var item models.ServiceabilityRecord
	if decodeJSON(w, r, &item) != nil || strings.TrimSpace(item.County) == "" {
		respondWithError(w, http.StatusBadRequest, "county and a valid serviceability status are required")
		return
	}
	if strings.TrimSpace(item.Status) == "" {
		item.Status = "serviceable"
	}
	if !validServiceabilityStatus(item.Status) {
		respondWithError(w, http.StatusBadRequest, "invalid serviceability status")
		return
	}
	if item.LocationID != "" {
		if _, err := uuid.Parse(item.LocationID); err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid location_id")
			return
		}
	}
	item.ISPID = userIDFromContext(r.Context())
	if err := h.repo.UpsertServiceability(&item); err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: item})
}

func (h *InstallationOperationsHandler) ListMyServiceability(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListServiceability(userIDFromContext(r.Context()))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

func (h *InstallationOperationsHandler) DeleteServiceability(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/serviceability/")
	if err := h.repo.DeleteServiceability(id, userIDFromContext(r.Context())); err != nil {
		respondWithError(w, http.StatusNotFound, "serviceability record not found")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "serviceability record removed"})
}

func (h *InstallationOperationsHandler) AddWaitlist(w http.ResponseWriter, r *http.Request) {
	var item models.ServiceabilityWaitlist
	if decodeJSON(w, r, &item) != nil || strings.TrimSpace(item.County) == "" {
		respondWithError(w, http.StatusBadRequest, "county is required")
		return
	}
	item.CustomerID = userIDFromContext(r.Context())
	if err := h.repo.AddWaitlist(&item); err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: item})
}

func (h *InstallationOperationsHandler) ListWaitlist(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListWaitlist(userIDFromContext(r.Context()))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

func (h *InstallationOperationsHandler) SaveProof(w http.ResponseWriter, r *http.Request) {
	requestID := pathParam(r.URL.Path, "/api/v1/jobs/")
	var item models.InstallationProof
	if decodeJSON(w, r, &item) != nil || requestID == "" {
		respondWithError(w, http.StatusBadRequest, "invalid installation proof")
		return
	}
	item.ID = uuid.NewString()
	item.RequestID = requestID
	item.TechnicianID = userIDFromContext(r.Context())
	if item.CheckInAt.IsZero() {
		item.CheckInAt = time.Now().UTC()
	}
	if len(item.PhotoURLs) == 0 || strings.TrimSpace(item.DeviceSerial) == "" || len(item.Checklist) == 0 {
		respondWithError(w, http.StatusBadRequest, "photo_urls, device_serial and checklist are required")
		return
	}
	if err := h.repo.SaveProof(&item); err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusForbidden, "job is not assigned to this technician")
			return
		}
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: item})
}

func (h *InstallationOperationsHandler) GetProof(w http.ResponseWriter, r *http.Request) {
	requestID := pathParam(r.URL.Path, "/api/v1/jobs/")
	item, err := h.repo.GetProof(requestID, userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, http.StatusNotFound, "installation proof not found")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: item})
}

func (h *InstallationOperationsHandler) ConfirmProof(w http.ResponseWriter, r *http.Request) {
	requestID := pathParam(r.URL.Path, "/api/v1/jobs/")
	if err := h.repo.ConfirmProof(requestID, userIDFromContext(r.Context())); err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusConflict, "installation proof is not available")
			return
		}
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "installation confirmed"})
}

func validServiceabilityStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "serviceable", "likely_serviceable", "survey_required", "not_serviceable":
		return true
	default:
		return false
	}
}
