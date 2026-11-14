package handlers

import (
	"ispilolite/api/dto"
	warrantysvc "ispilolite/internal/services/warranty"
	"net/http"
	"strconv"
)

type WarrantyHandler struct{ service *warrantysvc.Service }

func NewWarrantyHandler(service *warrantysvc.Service) *WarrantyHandler {
	return &WarrantyHandler{service: service}
}
func (h *WarrantyHandler) Create(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	var input struct {
		Description string   `json:"description"`
		PhotoURLs   []string `json:"photo_urls,omitempty"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, 400, "invalid warranty claim")
		return
	}
	claim, err := h.service.Create(r.Context(), jobID, userIDFromContext(r.Context()), input.Description, input.PhotoURLs)
	if err != nil {
		respondWithErrorRaw(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: claim})
}
func (h *WarrantyHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.service.List(r.Context(), userIDFromContext(r.Context()), userRoleFromContext(r.Context()), limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
func (h *WarrantyHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/warranty-claims/")
	var input struct {
		Status string `json:"status"`
		Note   string `json:"note,omitempty"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, 400, "invalid resolution")
		return
	}
	if err := h.service.Resolve(r.Context(), id, userIDFromContext(r.Context()), input.Status, input.Note); err != nil {
		respondWithErrorRaw(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "warranty claim updated"})
}
