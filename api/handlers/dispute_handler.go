package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"ispilolite/api/dto"
	disputesvc "ispilolite/internal/services/dispute"
)

type DisputeHandler struct{ service *disputesvc.Service }

func NewDisputeHandler(service *disputesvc.Service) *DisputeHandler {
	return &DisputeHandler{service: service}
}
func (h *DisputeHandler) Create(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	var input struct {
		Reason       string   `json:"reason"`
		Description  string   `json:"description"`
		EvidenceURLs []string `json:"evidence_urls,omitempty"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, 400, "invalid dispute")
		return
	}
	item, err := h.service.Create(r.Context(), jobID, userIDFromContext(r.Context()), input.Reason, input.Description, input.EvidenceURLs)
	if err != nil {
		respondWithErrorRaw(w, 400, err.Error())
		return
	}
	recordAudit(r, "dispute.create", "CREATE", "dispute", item.ID, "User opened dispute for job "+jobID+": "+input.Reason, true, map[string]interface{}{"job_id": jobID, "dispute_id": item.ID, "reason": input.Reason})
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: item})
}
func (h *DisputeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/disputes/")
	item, err := h.service.Get(r.Context(), id, userIDFromContext(r.Context()), userRoleFromContext(r.Context()))
	if err != nil {
		respondWithErrorRaw(w, 403, err.Error())
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: item})
}
func (h *DisputeHandler) Comment(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/disputes/")
	var input struct {
		Body         string   `json:"body"`
		EvidenceURLs []string `json:"evidence_urls,omitempty"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, 400, "invalid comment")
		return
	}
	item, err := h.service.Comment(r.Context(), id, userIDFromContext(r.Context()), input.Body, input.EvidenceURLs)
	if err != nil {
		respondWithErrorRaw(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: item})
}
func (h *DisputeHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.service.List(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
func (h *DisputeHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/disputes/")
	var input struct {
		Resolution string  `json:"resolution"`
		Amount     float64 `json:"amount,omitempty"`
		Note       string  `json:"note,omitempty"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, 400, "invalid resolution")
		return
	}
	if err := h.service.Resolve(r.Context(), id, userIDFromContext(r.Context()), input.Resolution, input.Note, input.Amount); err != nil {
		respondWithErrorRaw(w, 400, err.Error())
		return
	}
	recordAudit(r, "dispute.resolve", "UPDATE", "dispute", id, fmt.Sprintf("Admin resolved dispute %s with resolution: %s", id, input.Resolution), true, map[string]interface{}{"dispute_id": id, "resolution": input.Resolution, "amount": input.Amount})
	respondWithJSON(w, 202, dto.APIResponse{Success: true, Message: "settlement queued"})
}

func (h *DisputeHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/disputes/")
	if id == "" {
		id = pathParam(r.URL.Path, "/api/v1/admin/disputes/")
	}
	id = strings.TrimSuffix(id, "/withdraw")
	id = strings.TrimSuffix(id, "/cancel")
	if id == "" {
		respondWithErrorRaw(w, http.StatusBadRequest, "dispute id is required")
		return
	}
	err := h.service.Withdraw(r.Context(), id, userIDFromContext(r.Context()), userRoleFromContext(r.Context()))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, disputesvc.ErrForbidden) {
			respondWithErrorRaw(w, http.StatusNotFound, "dispute not found or cannot be withdrawn")
			return
		}
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "dispute withdrawn successfully"})
}
