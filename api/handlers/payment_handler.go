package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	paymentsvc "ispilolite/internal/services/payment"
)

type PaymentHandler struct{ service *paymentsvc.Service }

func NewPaymentHandler(service *paymentsvc.Service) *PaymentHandler {
	return &PaymentHandler{service: service}
}

func (h *PaymentHandler) Initiate(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	var input struct {
		Provider       string                    `json:"provider"`
		Phone          string                    `json:"phone"`
		Amount         float64                   `json:"amount"`
		Milestones     []*models.EscrowMilestone `json:"milestones,omitempty"`
		IdempotencyKey string                    `json:"idempotency_key,omitempty"`
	}
	if jobID == "" || decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, 400, "invalid payment request")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		key = input.IdempotencyKey
	}
	intent, err := h.service.Initiate(r.Context(), jobID, userIDFromContext(r.Context()), input.Provider, input.Phone, key, input.Amount, input.Milestones)
	if err != nil {
		recordAudit(r, "payment.initiate", "CREATE", "escrow_payment", jobID, fmt.Sprintf("Payment initiation failed for job %s: %s", jobID, err.Error()), false, map[string]interface{}{"job_id": jobID, "provider": input.Provider, "amount": input.Amount})
		respondWithErrorRaw(w, 400, err.Error())
		return
	}
	recordAudit(r, "payment.initiate", "CREATE", "escrow_payment", jobID, fmt.Sprintf("Client initiated %s payment of %.2f for job %s", input.Provider, input.Amount, jobID), true, map[string]interface{}{"job_id": jobID, "provider": input.Provider, "amount": input.Amount})
	respondWithJSON(w, 202, dto.APIResponse{Success: true, Data: intent})
}

func (h *PaymentHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	provider := pathParam(r.URL.Path, "/api/v1/payments/webhooks/")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		respondWithErrorRaw(w, 400, "invalid webhook")
		return
	}
	headers := r.Header.Clone()
	// Some payment rails cannot set custom callback headers. A high-entropy
	// callback token may therefore be supplied in the configured callback URL.
	if headers.Get("X-Webhook-Token") == "" {
		headers.Set("X-Webhook-Token", r.URL.Query().Get("token"))
	}
	escrow, err := h.service.HandleWebhook(r.Context(), provider, headers, body)
	if err != nil {
		recordAudit(r, "payment.webhook", "SYSTEM", "payment_provider", provider, "Payment webhook rejected for provider "+provider, false, map[string]interface{}{"provider": provider})
		respondWithErrorRaw(w, 400, "webhook rejected")
		return
	}
	recordAudit(r, "payment.webhook", "SYSTEM", "payment_provider", provider, "Payment webhook processed successfully for provider "+provider, true, map[string]interface{}{"provider": provider})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: escrow})
}

func (h *PaymentHandler) Settlement(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		respondWithErrorRaw(w, 400, "invalid settlement")
		return
	}
	headers := r.Header.Clone()
	if headers.Get("X-Webhook-Token") == "" {
		headers.Set("X-Webhook-Token", r.URL.Query().Get("token"))
	}
	if err := h.service.ConfirmSettlement(r.Context(), headers, body); err != nil {
		recordAudit(r, "payment.settlement", "SYSTEM", "settlement", "", "Payment settlement confirmation rejected", false, nil)
		respondWithErrorRaw(w, 400, "settlement rejected")
		return
	}
	recordAudit(r, "payment.settlement", "SYSTEM", "settlement", "", "Payment settlement confirmed successfully", true, nil)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "settlement recorded"})
}

func (h *PaymentHandler) GetEscrow(w http.ResponseWriter, r *http.Request) {
	jobID := pathParam(r.URL.Path, "/api/v1/jobs/")
	item, err := h.service.Escrow(r.Context(), jobID, userIDFromContext(r.Context()))
	if err != nil {
		respondWithErrorRaw(w, 404, err.Error())
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: item})
}

func (h *PaymentHandler) Release(w http.ResponseWriter, r *http.Request) {
	escrowID := pathParam(r.URL.Path, "/api/v1/escrows/")
	var input struct {
		MilestoneID string `json:"milestone_id,omitempty"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithErrorRaw(w, 400, "invalid release request")
		return
	}
	item, err := h.service.Release(r.Context(), escrowID, userIDFromContext(r.Context()), input.MilestoneID)
	if err != nil {
		recordAudit(r, "payment.release", "UPDATE", "escrow_payment", escrowID, "Escrow release failed: "+err.Error(), false, map[string]interface{}{"escrow_id": escrowID, "milestone_id": input.MilestoneID})
		respondWithErrorRaw(w, 409, err.Error())
		return
	}
	recordAudit(r, "payment.release", "UPDATE", "escrow_payment", escrowID, "Client released escrow payment milestone "+input.MilestoneID, true, map[string]interface{}{"escrow_id": escrowID, "milestone_id": input.MilestoneID})
	respondWithJSON(w, 202, dto.APIResponse{Success: true, Message: "release requested", Data: item})
}
