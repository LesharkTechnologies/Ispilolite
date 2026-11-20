package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/validation"
	"time"
)

type BillingHandler struct{ repo *postgres.BillingRepository }

func NewBillingHandler() (*BillingHandler, error) {
	repo, err := postgres.NewBillingRepository()
	if err != nil {
		return nil, err
	}
	return &BillingHandler{repo: repo}, nil
}

func (h *BillingHandler) List(w http.ResponseWriter, r *http.Request) {
	items, total, err := h.repo.List(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")), strings.TrimSpace(r.URL.Query().Get("service")), intQuery(r.URL.Query().Get("limit")), intQuery(r.URL.Query().Get("offset")))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"items": items, "total": total}})
}

func (h *BillingHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	item, err := h.repo.Get(r.Context(), id)
	if err != nil {
		item, err = h.repo.GetBySlug(r.Context(), id)
	}
	if err != nil {
		respondWithError(w, http.StatusNotFound, "billing system not found")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: item})
}

func (h *BillingHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string `json:"name"`
		LogoURL         string `json:"logo_url"`
		Description     string `json:"description"`
		OwnerCompany    string `json:"owner_company"`
		OwnerPhone      string `json:"owner_phone"`
		SupportPhone    string `json:"support_phone"`
		SupportWhatsApp string `json:"support_whatsapp"`
		SupportEmail    string `json:"support_email"`
		SupportHours    string `json:"support_hours"`
		WebsiteURL      string `json:"website_url"`
		LoginURL        string `json:"login_url"`
		ReferralURL     string `json:"referral_url"`
	}
	if decodeJSON(w, r, &req) != nil || strings.TrimSpace(req.Name) == "" {
		respondWithError(w, http.StatusBadRequest, "name is required")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(req.Name))
	slug = strings.ReplaceAll(slug, " ", "-")
	item := &models.BillingSystem{Name: strings.TrimSpace(req.Name), Slug: slug, LogoURL: strings.TrimSpace(req.LogoURL), Description: strings.TrimSpace(req.Description), OwnerCompany: strings.TrimSpace(req.OwnerCompany), OwnerPhone: strings.TrimSpace(req.OwnerPhone), SupportPhone: strings.TrimSpace(req.SupportPhone), SupportWhatsApp: strings.TrimSpace(req.SupportWhatsApp), SupportEmail: strings.TrimSpace(req.SupportEmail), SupportHours: strings.TrimSpace(req.SupportHours), WebsiteURL: strings.TrimSpace(req.WebsiteURL), LoginURL: strings.TrimSpace(req.LoginURL), ReferralURL: strings.TrimSpace(req.ReferralURL)}
	if item.LogoURL != "" {
		urls, err := validation.MediaURLs([]string{item.LogoURL}, 1)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		item.LogoURL = urls[0]
	}
	if err := h.repo.Create(r.Context(), item, userIDFromContext(r.Context())); err != nil {
		respondWithError(w, http.StatusBadRequest, "failed to create billing system")
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: item})
}

func (h *BillingHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	var req models.BillingSystem
	if id == "" || decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid billing system")
		return
	}
	req.ID = id
	if strings.TrimSpace(req.LogoURL) != "" {
		urls, err := validation.MediaURLs([]string{req.LogoURL}, 1)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.LogoURL = urls[0]
	}
	if err := h.repo.Update(r.Context(), &req, userIDFromContext(r.Context())); err != nil {
		respondWithError(w, http.StatusForbidden, "update not allowed")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "billing system updated"})
}

func (h *BillingHandler) Archive(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	if err := h.repo.Archive(r.Context(), id, userIDFromContext(r.Context())); err != nil {
		respondWithError(w, http.StatusForbidden, "archive not allowed")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "billing system archived"})
}

func (h *BillingHandler) Pricing(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	items, err := h.repo.Pricing(r.Context(), id)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

func (h *BillingHandler) UpsertPricing(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	var input struct {
		Prices []*models.BillingPrice `json:"prices"`
	}
	if decodeJSON(w, r, &input) != nil || len(input.Prices) == 0 {
		respondWithError(w, http.StatusBadRequest, "prices are required")
		return
	}
	if err := h.repo.UpsertPricing(r.Context(), id, userIDFromContext(r.Context()), input.Prices, timeNow()); err != nil {
		respondWithError(w, http.StatusForbidden, "pricing update not allowed")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "pricing updated"})
}

func (h *BillingHandler) Reviews(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, total, err := h.repo.Reviews(r.Context(), id, limit, offset)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"items": items, "total": total}})
}

func (h *BillingHandler) CreateReview(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	var req models.BillingReview
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid review")
		return
	}
	req.BillingSystemID = id
	req.ReviewerID = userIDFromContext(r.Context())
	if err := h.repo.CreateReview(r.Context(), &req); err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: req})
}

func (h *BillingHandler) ReplyReview(w http.ResponseWriter, r *http.Request) {
	systemID := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	reviewID := pathParam(r.URL.Path, "/api/v1/billing-reviews/")
	var input struct {
		Reply string `json:"reply"`
	}
	if reviewID == "" || decodeJSON(w, r, &input) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid reply")
		return
	}
	if err := h.repo.ReplyReview(r.Context(), systemID, userIDFromContext(r.Context()), reviewID, strings.TrimSpace(input.Reply)); err != nil {
		respondWithError(w, http.StatusForbidden, "reply not allowed")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "reply saved"})
}

func (h *BillingHandler) Flag(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	var req models.BillingFlag
	if decodeJSON(w, r, &req) != nil || strings.TrimSpace(req.Reason) == "" {
		respondWithError(w, http.StatusBadRequest, "reason is required")
		return
	}
	req.BillingSystemID = id
	req.ReporterID = userIDFromContext(r.Context())
	if err := h.repo.Flag(r.Context(), &req); err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: req})
}

func (h *BillingHandler) Claim(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	var req models.BillingClaim
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid claim")
		return
	}
	req.BillingSystemID = id
	req.ClaimantID = userIDFromContext(r.Context())
	if err := h.repo.Claim(r.Context(), &req); err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: req})
}

func (h *BillingHandler) Verify(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/billing-systems/")
	var req struct {
		DomainVerified   bool `json:"domain_verified"`
		BusinessVerified bool `json:"business_verified"`
	}
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid verification")
		return
	}
	if err := h.repo.SetVerification(r.Context(), id, req.DomainVerified, req.BusinessVerified); err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "verification updated"})
}

func (h *BillingHandler) AdminFlags(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.repo.ListFlags(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

func (h *BillingHandler) ResolveFlag(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/billing-flags/")
	var req struct {
		Status string `json:"status"`
	}
	if decodeJSON(w, r, &req) != nil || (req.Status != "resolved" && req.Status != "dismissed") {
		respondWithError(w, http.StatusBadRequest, "status must be resolved or dismissed")
		return
	}
	if err := h.repo.ResolveFlag(r.Context(), id, userIDFromContext(r.Context()), req.Status); err != nil {
		respondWithError(w, http.StatusNotFound, "flag not found")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "flag resolved"})
}

func (h *BillingHandler) AdminClaims(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.repo.ListClaims(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

func (h *BillingHandler) ResolveClaim(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/billing-claims/")
	var req struct {
		Status string `json:"status"`
	}
	if decodeJSON(w, r, &req) != nil || (req.Status != "approved" && req.Status != "rejected") {
		respondWithError(w, http.StatusBadRequest, "status must be approved or rejected")
		return
	}
	if err := h.repo.ResolveClaim(r.Context(), id, userIDFromContext(r.Context()), req.Status); err != nil {
		respondWithError(w, http.StatusNotFound, "claim not found")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "claim resolved"})
}

func timeNow() time.Time { return time.Now().UTC() }
