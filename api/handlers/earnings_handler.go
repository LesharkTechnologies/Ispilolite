package handlers

import (
	"ispilolite/api/dto"
	earningssvc "ispilolite/internal/services/earnings"
	"net/http"
)

type EarningsHandler struct{ service *earningssvc.Service }

func NewEarningsHandler(service *earningssvc.Service) *EarningsHandler {
	return &EarningsHandler{service: service}
}
func (h *EarningsHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.Dashboard(r.Context(), userIDFromContext(r.Context()), r.URL.Query().Get("period"))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: item})
}
