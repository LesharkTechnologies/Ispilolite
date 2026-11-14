package handlers

import (
	"ispilolite/api/dto"
	disputesvc "ispilolite/internal/services/dispute"
	"net/http"
	"strconv"
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
	respondWithJSON(w, 202, dto.APIResponse{Success: true, Message: "settlement queued"})
}
