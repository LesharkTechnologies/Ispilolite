package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"ispilolite/api/dto"
	"ispilolite/internal/middleware"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
)

type PromotionHandler struct{ repo *postgres.PromotionRepository }

func NewPromotionHandler() (*PromotionHandler, error) {
	repo, err := postgres.NewPromotionRepository()
	if err != nil {
		return nil, err
	}
	return &PromotionHandler{repo: repo}, nil
}

var promotionPlacements = map[string]bool{
	"directory": true,
	"search":    true,
	"home":      true,
	"profile":   true,
	"category":  true,
	"map":       true,
}

func (h *PromotionHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, userIDFromContext(r.Context()), userRoleFromContext(r.Context()))
}

func (h *PromotionHandler) list(w http.ResponseWriter, r *http.Request, actorID, role string) {
	limit := intQuery(r.URL.Query().Get("limit"))
	offset := intQuery(r.URL.Query().Get("offset"))
	items, total, err := h.repo.List(r.Context(), actorID, role, strings.TrimSpace(r.URL.Query().Get("status")), limit, offset)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{
		"items": items, "total": total, "limit": limit, "offset": offset,
	}})
}

func (h *PromotionHandler) GetMine(w http.ResponseWriter, r *http.Request) {
	item, err := h.authorized(r)
	if err != nil {
		promotionError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: item})
}

func (h *PromotionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ListingType     string  `json:"listing_type"`
		ListingID       string  `json:"listing_id"`
		DestinationType string  `json:"destination_type"`
		DestinationURL  string  `json:"destination_url"`
		Placement       string  `json:"placement"`
		StartsAt        string  `json:"starts_at"`
		ExpiresAt       string  `json:"expires_at"`
		DurationDays    int     `json:"duration_days"`
		Amount          float64 `json:"amount"`
		Currency        string  `json:"currency"`
	}
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid promotion")
		return
	}
	actorID := userIDFromContext(r.Context())
	role := userRoleFromContext(r.Context())
	req.ListingType = strings.TrimSpace(req.ListingType)
	req.ListingID = strings.TrimSpace(req.ListingID)
	if _, err := uuid.Parse(req.ListingID); err != nil || !validPromotionListingType(req.ListingType) {
		respondWithError(w, http.StatusBadRequest, "listing_type and listing_id are required")
		return
	}
	listing, err := h.repo.ResolveListing(r.Context(), actorID, role, req.ListingType, req.ListingID)
	if err != nil {
		promotionError(w, err)
		return
	}
	now := time.Now().UTC()
	startsAt, expiresAt, err := promotionSchedule(now, req.StartsAt, req.ExpiresAt, req.DurationDays)
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	destinationType, destinationURL, err := promotionDestination(req.DestinationType, req.DestinationURL, listing.InternalPath)
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	placement := strings.TrimSpace(req.Placement)
	if placement == "" {
		placement = "directory"
	}
	if !promotionPlacements[placement] {
		respondWithError(w, http.StatusBadRequest, "invalid placement")
		return
	}
	if math.IsNaN(req.Amount) || math.IsInf(req.Amount, 0) || req.Amount <= 0 || req.Amount > 1_000_000_000 {
		respondWithError(w, http.StatusBadRequest, "amount must be greater than zero")
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "KES"
	}
	if len(currency) != 3 {
		respondWithError(w, http.StatusBadRequest, "currency must be a 3-letter code")
		return
	}
	item := &models.Promotion{
		OwnerID:         actorID,
		ListingType:     listing.Type,
		ListingID:       listing.ID,
		ListingName:     listing.Name,
		DestinationType: destinationType,
		DestinationURL:  destinationURL,
		Placement:       placement,
		StartsAt:        startsAt,
		ExpiresAt:       expiresAt,
		Amount:          req.Amount,
		Currency:        currency,
	}
	payment := &models.PromotionPayment{Amount: req.Amount, Currency: currency}
	if err := h.repo.Create(r.Context(), item, payment); err != nil {
		respondWithInternalError(w, err)
		return
	}
	created, err := h.repo.Get(r.Context(), item.ID, actorID, role)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: created})
}

// Renew intentionally creates a new pending-approval promotion for the same
// listing. The original row, its statistics and its payment history remain
// untouched, which keeps expired campaigns auditable.
func (h *PromotionHandler) Renew(w http.ResponseWriter, r *http.Request) {
	previous, err := h.authorized(r)
	if err != nil {
		promotionError(w, err)
		return
	}
	if previous.Status == models.PromotionPendingApproval || previous.Status == models.PromotionPendingPayment || previous.Status == models.PromotionCancelled || previous.Status == models.PromotionSuspended || previous.Status == models.PromotionRejected {
		respondWithError(w, http.StatusConflict, "promotion cannot be renewed in its current state")
		return
	}
	var req struct {
		DurationDays int     `json:"duration_days"`
		Amount       float64 `json:"amount"`
		Currency     string  `json:"currency"`
	}
	if err := decodeJSON(w, r, &req); err != nil && !errors.Is(err, io.EOF) {
		respondWithError(w, http.StatusBadRequest, "invalid renewal")
		return
	}
	now := time.Now().UTC()
	duration := req.DurationDays
	if duration <= 0 {
		duration = int(previous.ExpiresAt.Sub(previous.StartsAt).Hours() / 24)
	}
	renewalStart := now
	if previous.ExpiresAt.After(renewalStart) {
		renewalStart = previous.ExpiresAt
	}
	startsAt, expiresAt, err := promotionSchedule(now, renewalStart.Format(time.RFC3339), "", duration)
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	amount := req.Amount
	if amount == 0 {
		amount = previous.Amount
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 || amount > 1_000_000_000 {
		respondWithError(w, http.StatusBadRequest, "amount must be greater than zero")
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = previous.Currency
	}
	if len(currency) != 3 {
		respondWithError(w, http.StatusBadRequest, "currency must be a 3-letter code")
		return
	}
	item := &models.Promotion{
		OwnerID:         userIDFromContext(r.Context()),
		ListingType:     previous.ListingType,
		ListingID:       previous.ListingID,
		ListingName:     previous.ListingName,
		DestinationType: previous.DestinationType,
		DestinationURL:  previous.DestinationURL,
		Placement:       previous.Placement,
		StartsAt:        startsAt,
		ExpiresAt:       expiresAt,
		Amount:          amount,
		Currency:        currency,
		RenewedFromID:   previous.ID,
	}
	payment := &models.PromotionPayment{Amount: amount, Currency: currency}
	if err := h.repo.Create(r.Context(), item, payment); err != nil {
		respondWithInternalError(w, err)
		return
	}
	created, err := h.repo.Get(r.Context(), item.ID, userIDFromContext(r.Context()), userRoleFromContext(r.Context()))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: created})
}

func (h *PromotionHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	item, err := h.authorized(r)
	if err != nil {
		promotionError(w, err)
		return
	}
	if item.Status == models.PromotionExpired {
		respondWithError(w, http.StatusConflict, "expired promotion cannot be cancelled")
		return
	}
	if err := h.repo.Cancel(r.Context(), item.ID, userIDFromContext(r.Context()), userRoleFromContext(r.Context())); err != nil {
		promotionError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "promotion cancelled"})
}

func (h *PromotionHandler) Insights(w http.ResponseWriter, r *http.Request) {
	item, err := h.authorized(r)
	if err != nil {
		promotionError(w, err)
		return
	}
	from, to, err := promotionDateRange(r)
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	placement := strings.TrimSpace(r.URL.Query().Get("placement"))
	if placement != "" && !promotionPlacements[placement] {
		respondWithError(w, http.StatusBadRequest, "invalid placement")
		return
	}
	insights, err := h.repo.Insights(r.Context(), item.ID, from, to, placement)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: insights})
}

func (h *PromotionHandler) Payments(w http.ResponseWriter, r *http.Request) {
	item, err := h.authorized(r)
	if err != nil {
		promotionError(w, err)
		return
	}
	items, err := h.repo.ListPayments(r.Context(), item.ID)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"items": items, "total": len(items)}})
}

func (h *PromotionHandler) Enquiries(w http.ResponseWriter, r *http.Request) {
	item, err := h.authorized(r)
	if err != nil {
		promotionError(w, err)
		return
	}
	limit, offset := intQuery(r.URL.Query().Get("limit")), intQuery(r.URL.Query().Get("offset"))
	items, total, err := h.repo.ListEnquiries(r.Context(), item.ID, limit, offset)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"items": items, "total": total, "limit": limit, "offset": offset}})
}

func (h *PromotionHandler) CreateEnquiry(w http.ResponseWriter, r *http.Request) {
	promotionID := pathParam(r.URL.Path, "/api/v1/promotions/")
	if _, err := uuid.Parse(promotionID); err != nil {
		respondWithError(w, http.StatusNotFound, "promotion not found")
		return
	}
	var req struct {
		Name    string `json:"name"`
		Phone   string `json:"phone"`
		Email   string `json:"email"`
		Message string `json:"message"`
		Consent bool   `json:"consent"`
	}
	if decodeJSON(w, r, &req) != nil || !req.Consent {
		respondWithError(w, http.StatusBadRequest, "explicit consent is required")
		return
	}
	if len(strings.TrimSpace(req.Message)) > 2000 || len(req.Name) > 120 || len(req.Phone) > 40 || len(req.Email) > 200 {
		respondWithError(w, http.StatusBadRequest, "enquiry is too long")
		return
	}
	item, err := h.repo.CreateEnquiry(r.Context(), promotionID, userIDFromContext(r.Context()), strings.TrimSpace(req.Name), strings.TrimSpace(req.Phone), strings.TrimSpace(req.Email), strings.TrimSpace(req.Message))
	if err != nil {
		promotionError(w, err)
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: item})
}

func (h *PromotionHandler) Active(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.Active(r.Context(), strings.TrimSpace(r.URL.Query().Get("placement")), intQuery(r.URL.Query().Get("limit")))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"items": items}})
}

func (h *PromotionHandler) Track(w http.ResponseWriter, r *http.Request) {
	promotionID := pathParam(r.URL.Path, "/api/v1/promotions/")
	if _, err := uuid.Parse(promotionID); err != nil {
		respondWithError(w, http.StatusNotFound, "promotion not found")
		return
	}
	var req struct {
		Type      string `json:"type"`
		Placement string `json:"placement"`
	}
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid event")
		return
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Type != "impression" && req.Type != "click" {
		respondWithError(w, http.StatusBadRequest, "type must be impression or click")
		return
	}
	req.Placement = strings.TrimSpace(req.Placement)
	if req.Placement == "" {
		req.Placement = "directory"
	}
	if !promotionPlacements[req.Placement] {
		respondWithError(w, http.StatusBadRequest, "invalid placement")
		return
	}
	userAgent := r.UserAgent()
	if isBotUserAgent(userAgent) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	userID := userIDFromContext(r.Context())
	role := userRoleFromContext(r.Context())
	if role != "technician" && role != "isp" && role != "customer" {
		role = "visitor"
	}
	county := ""
	if userID != "" {
		county, _ = h.repo.ViewerCounty(r.Context(), userID)
	}
	viewerKey := promotionViewerKey(userID, middleware.ClientIP(r), userAgent)
	now := time.Now().UTC()
	bucketSeconds := int64(300)
	if req.Type == "click" {
		bucketSeconds = 60
	}
	recorded, err := h.repo.RecordEvent(r.Context(), promotionID, req.Type, viewerKey, role, county, req.Placement, now.Unix()/bucketSeconds)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusAccepted, dto.APIResponse{Success: true, Data: map[string]interface{}{"recorded": recorded}})
}

func (h *PromotionHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, userIDFromContext(r.Context()), "admin")
}

func (h *PromotionHandler) AdminGet(w http.ResponseWriter, r *http.Request) {
	h.GetMine(w, r)
}

func (h *PromotionHandler) AdminApprove(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/promotions/")
	if _, err := uuid.Parse(id); err != nil {
		respondWithError(w, http.StatusNotFound, "promotion not found")
		return
	}
	if err := h.repo.Approve(r.Context(), id, userIDFromContext(r.Context())); err != nil {
		promotionError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "promotion approved"})
}

func (h *PromotionHandler) AdminReject(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/promotions/")
	if _, err := uuid.Parse(id); err != nil {
		respondWithError(w, http.StatusNotFound, "promotion not found")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "a rejection reason is required")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" || len(req.Reason) > 1000 {
		respondWithError(w, http.StatusBadRequest, "a rejection reason is required")
		return
	}
	if err := h.repo.Reject(r.Context(), id, userIDFromContext(r.Context()), req.Reason); err != nil {
		promotionError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "promotion rejected"})
}

func (h *PromotionHandler) AdminSetStatus(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/promotions/")
	var req struct {
		Status string `json:"status"`
	}
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid status")
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	if req.Status != models.PromotionActive && req.Status != models.PromotionSuspended && req.Status != models.PromotionCancelled {
		respondWithError(w, http.StatusBadRequest, "status must be active, suspended or cancelled")
		return
	}
	if err := h.repo.SetStatus(r.Context(), id, req.Status); err != nil {
		promotionError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "promotion status updated"})
}

func (h *PromotionHandler) AdminConfirmPayment(w http.ResponseWriter, r *http.Request) {
	promotionID := pathParam(r.URL.Path, "/api/v1/admin/promotions/")
	paymentID := pathParamBeforeLast(r.URL.Path)
	if _, err := uuid.Parse(promotionID); err != nil {
		respondWithError(w, http.StatusNotFound, "promotion not found")
		return
	}
	if _, err := uuid.Parse(paymentID); err != nil {
		respondWithError(w, http.StatusNotFound, "payment not found")
		return
	}
	var req struct {
		Provider  string `json:"provider"`
		Reference string `json:"reference"`
		Receipt   string `json:"receipt"`
	}
	if decodeJSON(w, r, &req) != nil || strings.TrimSpace(req.Reference) == "" {
		respondWithError(w, http.StatusBadRequest, "payment reference is required")
		return
	}
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "manual"
	}
	if err := h.repo.ConfirmPayment(r.Context(), promotionID, paymentID, userIDFromContext(r.Context()), provider, strings.TrimSpace(req.Reference), strings.TrimSpace(req.Receipt)); err != nil {
		promotionError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "payment confirmed"})
}

func (h *PromotionHandler) authorized(r *http.Request) (*models.Promotion, error) {
	id := pathParam(r.URL.Path, "/api/v1/my/promotions/")
	if id == "" {
		id = pathParam(r.URL.Path, "/api/v1/admin/promotions/")
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, postgres.ErrPromotionNotFound
	}
	return h.repo.Get(r.Context(), id, userIDFromContext(r.Context()), userRoleFromContext(r.Context()))
}

func promotionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrPromotionNotFound):
		respondWithError(w, http.StatusNotFound, "promotion not found")
	case errors.Is(err, postgres.ErrPromotionForbidden):
		respondWithError(w, http.StatusForbidden, "promotion access denied")
	case errors.Is(err, postgres.ErrPromotionConflict):
		respondWithError(w, http.StatusConflict, "promotion state conflict")
	default:
		respondWithInternalError(w, err)
	}
}

func validPromotionListingType(value string) bool {
	return value == models.PromotionListingBillingSystem || value == models.PromotionListingISP || value == models.PromotionListingTechnician
}

func promotionSchedule(now time.Time, startsRaw, expiresRaw string, durationDays int) (time.Time, time.Time, error) {
	startsAt := now
	if strings.TrimSpace(startsRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(startsRaw))
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("starts_at must be an RFC3339 timestamp")
		}
		startsAt = parsed.UTC()
	}
	var expiresAt time.Time
	if strings.TrimSpace(expiresRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresRaw))
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("expires_at must be an RFC3339 timestamp")
		}
		expiresAt = parsed.UTC()
	} else {
		if durationDays <= 0 {
			durationDays = 30
		}
		if durationDays > 730 {
			return time.Time{}, time.Time{}, errors.New("duration_days must be between 1 and 730")
		}
		expiresAt = startsAt.AddDate(0, 0, durationDays)
	}
	if !expiresAt.After(startsAt) {
		return time.Time{}, time.Time{}, errors.New("expires_at must be after starts_at")
	}
	if !expiresAt.After(now) {
		return time.Time{}, time.Time{}, errors.New("expires_at must be in the future")
	}
	return startsAt, expiresAt, nil
}

func promotionDestination(destinationType, raw, internalPath string) (string, string, error) {
	destinationType = strings.ToLower(strings.TrimSpace(destinationType))
	if destinationType == "" {
		destinationType = "internal"
	}
	raw = strings.TrimSpace(raw)
	switch destinationType {
	case "internal":
		if raw == "" {
			raw = internalPath
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") || strings.ContainsAny(raw, "\\\r\n") || len(raw) > 512 {
			return "", "", errors.New("destination_url must be an internal path beginning with /")
		}
		return destinationType, raw, nil
	case "external":
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n") {
			return "", "", errors.New("destination_url must be an http(s) URL")
		}
		return destinationType, raw, nil
	default:
		return "", "", errors.New("destination_type must be internal or external")
	}
}

func promotionDateRange(r *http.Request) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := today.AddDate(0, 0, -29)
	toInclusive := today
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("from must be YYYY-MM-DD")
		}
		from = parsed.UTC()
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("to must be YYYY-MM-DD")
		}
		toInclusive = parsed.UTC()
	}
	if toInclusive.Before(from) {
		return time.Time{}, time.Time{}, errors.New("to must not be before from")
	}
	to := toInclusive.AddDate(0, 0, 1)
	if to.Sub(from) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("date range cannot exceed 366 days")
	}
	return from, to, nil
}

func promotionViewerKey(userID, remoteAddr, userAgent string) string {
	salt := os.Getenv("PROMOTION_VIEWER_SALT")
	if salt == "" {
		salt = os.Getenv("JWT_SECRET")
	}
	if salt == "" {
		salt = "ispilolite-promotions-development"
	}
	identity := "anon|" + remoteAddr + "|" + userAgent
	if userID != "" {
		identity = "user|" + userID
	}
	sum := sha256.Sum256([]byte(salt + "|" + identity))
	return hex.EncodeToString(sum[:])
}

func isBotUserAgent(userAgent string) bool {
	lower := strings.ToLower(userAgent)
	if strings.TrimSpace(lower) == "" {
		return true
	}
	for _, marker := range []string{"googlebot", "bingbot", "duckduckbot", "baiduspider", "yandexbot", "facebookexternalhit", "twitterbot", "linkedinbot", "telegrambot", "applebot", "semrushbot", "ahrefsbot", "mj12bot", "dotbot", "petalbot", "bytespider", "crawler", "spider", "slurp", "headlesschrome", "phantomjs", "selenium", "puppeteer", "playwright", "curl/", "wget/", "python-requests", "scrapy"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func pathParamBeforeLast(path string) string {
	trimmed := strings.TrimRight(path, "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx <= 0 {
		return ""
	}
	trimmed = trimmed[:idx]
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return trimmed[idx+1:]
	}
	return ""
}
