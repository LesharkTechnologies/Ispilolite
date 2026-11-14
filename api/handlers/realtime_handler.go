package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	realtimesvc "ispilolite/internal/services/realtime"
	"ispilolite/pkg/notifications"
)

// RealtimeHandler exposes job-scoped status streams and technician tracking.
// Media is not accepted here; all photo uploads remain client-to-Cloudinary.
type RealtimeHandler struct {
	service *realtimesvc.Service
}

func NewRealtimeHandler(service *realtimesvc.Service) *RealtimeHandler {
	return &RealtimeHandler{service: service}
}

// UpdateLocation handles POST /api/v1/jobs/:id/location. The authenticated
// technician normally calls this every 30 seconds while travelling.
func (h *RealtimeHandler) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	var input models.TechnicianLocationUpdate
	if jobID == "" || decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "valid job id and location are required")
		return
	}
	input.JobRequestID = jobID // path is authoritative
	location, err := h.service.UpdateLocation(r.Context(), userIDFromContext(r.Context()), input)
	if err != nil {
		switch {
		case errors.Is(err, realtimesvc.ErrInvalidLocation):
			respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, realtimesvc.ErrForbidden):
			respondWithErrorRaw(w, http.StatusForbidden, err.Error())
		default:
			respondWithInternalError(w, err)
		}
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: location})
}

// GetLocation handles GET /api/v1/jobs/:id/location. Only a participant in the
// job can see the current technician position.
func (h *RealtimeHandler) GetLocation(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	location, err := h.service.Location(r.Context(), jobID, userIDFromContext(r.Context()))
	if err != nil {
		if errors.Is(err, realtimesvc.ErrForbidden) {
			respondWithErrorRaw(w, http.StatusNotFound, "live location is unavailable")
			return
		}
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: location})
}

// Events handles GET /api/v1/jobs/:id/events for reconnect/replay.
func (h *RealtimeHandler) Events(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.service.Events(r.Context(), jobID, userIDFromContext(r.Context()), limit)
	if err != nil {
		if errors.Is(err, realtimesvc.ErrForbidden) {
			respondWithErrorRaw(w, http.StatusForbidden, err.Error())
			return
		}
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

// JobStream handles GET /api/v1/jobs/:id/stream using Server-Sent Events.
// Authorization is checked before subscribing to the in-memory topic.
func (h *RealtimeHandler) JobStream(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	job, err := h.service.CanAccess(r.Context(), jobID, userIDFromContext(r.Context()))
	if err != nil {
		if errors.Is(err, realtimesvc.ErrForbidden) {
			respondWithErrorRaw(w, http.StatusForbidden, err.Error())
			return
		}
		respondWithInternalError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondWithErrorRaw(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	updates, unsubscribe := notifications.Default.SubscribeTopic("job:" + jobID)
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	initial, _ := json.Marshal(map[string]interface{}{"job_request_id": job.ID, "status": job.Status, "updated_at": job.UpdatedAt})
	_, _ = w.Write([]byte("event: job_status\ndata: " + string(initial) + "\n\n"))
	flusher.Flush()

	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": keep-alive\n\n"))
			flusher.Flush()
		case payload, open := <-updates:
			if !open {
				return
			}
			_, _ = w.Write([]byte("event: job_update\ndata: " + string(payload) + "\n\n"))
			flusher.Flush()
		}
	}
}
