package handlers

import (
	"ispilolite/api/dto"
	referralsvc "ispilolite/internal/services/referral"
	"net/http"
)

type ReferralHandler struct{ service *referralsvc.Service }

func NewReferralHandler(service *referralsvc.Service) *ReferralHandler {
	return &ReferralHandler{service: service}
}
func (h *ReferralHandler) Summary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.service.Summary(r.Context(), userIDFromContext(r.Context()))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: summary})
}
