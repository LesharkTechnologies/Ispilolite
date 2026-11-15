package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	availabilitysvc "ispilolite/internal/services/availability"
)

type AvailabilityHandler struct{ service *availabilitysvc.Service }

func NewAvailabilityHandler(service *availabilitysvc.Service) *AvailabilityHandler {
	return &AvailabilityHandler{service: service}
}

func (h *AvailabilityHandler) Replace(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Rules []*models.AvailabilityRule `json:"rules"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid schedule")
		return
	}
	rules, err := h.service.Replace(r.Context(), userIDFromContext(r.Context()), input.Rules)
	if err != nil {
		if errors.Is(err, availabilitysvc.ErrInvalid) {
			respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		} else {
			respondWithInternalError(w, err)
		}
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: rules})
}

func (h *AvailabilityHandler) AddException(w http.ResponseWriter, r *http.Request) {
	input := &models.AvailabilityException{}
	if decodeJSON(w, r, input) != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid exception")
		return
	}
	item, err := h.service.AddException(r.Context(), userIDFromContext(r.Context()), input)
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: item})
}

func (h *AvailabilityHandler) DeleteException(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.URL.Query().Get("date"))
	if target == "" {
		target = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	if target == "" {
		target = pathParam(r.URL.Path, "/api/v1/my/availability/exceptions/")
	}
	if target == "" {
		var input struct {
			Date string `json:"date"`
			ID   string `json:"id"`
		}
		if decodeJSON(w, r, &input) == nil {
			if input.Date != "" {
				target = input.Date
			} else {
				target = input.ID
			}
		}
	}
	if target == "" {
		respondWithErrorRaw(w, http.StatusBadRequest, "date or exception id is required")
		return
	}
	if err := h.service.DeleteException(r.Context(), userIDFromContext(r.Context()), target); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondWithErrorRaw(w, http.StatusNotFound, "exception not found")
			return
		}
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "exception deleted"})
}

func (h *AvailabilityHandler) Slots(w http.ResponseWriter, r *http.Request) {
	technicianID := pathParam(r.URL.Path, "/api/v1/technicians/")
	var from, to time.Time
	var err error
	if raw := r.URL.Query().Get("from"); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			respondWithErrorRaw(w, http.StatusBadRequest, "from must be RFC3339")
			return
		}
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			respondWithErrorRaw(w, http.StatusBadRequest, "to must be RFC3339")
			return
		}
	}
	duration, _ := strconv.Atoi(r.URL.Query().Get("duration_minutes"))
	slots, err := h.service.Slots(r.Context(), technicianID, from, to, duration)
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: slots})
}
