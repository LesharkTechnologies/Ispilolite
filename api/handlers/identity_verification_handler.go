package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/validation"
)

type identityVerificationStore interface {
	CreateVerification(ctx context.Context, item *models.IdentityVerification) error
	GetVerificationByID(ctx context.Context, id string) (*models.IdentityVerification, error)
	GetLatestVerificationByUserID(ctx context.Context, userID string) (*models.IdentityVerification, error)
	ListVerifications(ctx context.Context, status, badge string, limit, offset int) ([]*models.IdentityVerification, int, error)
	ApproveVerification(ctx context.Context, id, adminID, verifiedName, badge, notes string) (*models.IdentityVerification, error)
	RejectVerification(ctx context.Context, id, adminID, reason, notes string) (*models.IdentityVerification, error)
}

type IdentityVerificationHandler struct {
	store identityVerificationStore
	users repository.UserRepository
}

func NewIdentityVerificationHandler() (*IdentityVerificationHandler, error) {
	repo, err := postgres.NewIdentityVerificationRepository()
	if err != nil {
		return nil, err
	}
	userRepo, err := postgres.NewUserRepo()
	if err != nil {
		return nil, err
	}
	return &IdentityVerificationHandler{
		store: repo,
		users: userRepo,
	}, nil
}

func newIdentityVerificationHandlerWithDeps(store identityVerificationStore, users repository.UserRepository) *IdentityVerificationHandler {
	return &IdentityVerificationHandler{
		store: store,
		users: users,
	}
}

// Submit allows a technician or user to upload their ID photo linked to their verified phone number.
func (h *IdentityVerificationHandler) Submit(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	if userID == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetUserByID(userID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "user account not found")
		return
	}

	// The technician must have verified their account with a phone number first.
	if !user.IsVerified || strings.TrimSpace(user.Phone) == "" {
		respondWithError(w, http.StatusBadRequest, "account phone number must be verified before submitting identity verification")
		return
	}

	var req models.SubmitVerificationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid verification payload")
		return
	}

	photoURL := strings.TrimSpace(req.IDPhotoURL)
	if photoURL == "" {
		respondWithError(w, http.StatusBadRequest, "id_photo_url is required")
		return
	}
	if len(photoURL) > 2048 {
		respondWithError(w, http.StatusBadRequest, "id_photo_url is too long")
		return
	}
	documentURLs := []string{photoURL}
	if back := strings.TrimSpace(req.IDPhotoBackURL); back != "" {
		documentURLs = append(documentURLs, back)
	}
	documentURLs, err = validation.MediaURLs(documentURLs, 2)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	photoURL = documentURLs[0]
	backURL := ""
	if len(documentURLs) > 1 {
		backURL = documentURLs[1]
	}

	docType := strings.ToLower(strings.TrimSpace(req.DocumentType))
	if docType == "" {
		docType = "national_id"
	}
	switch docType {
	case "national_id", "passport", "business_permit", "driving_license", "other":
	default:
		respondWithError(w, http.StatusBadRequest, "document_type must be national_id, passport, business_permit, driving_license, or other")
		return
	}

	item := &models.IdentityVerification{
		ID:                uuid.NewString(),
		UserID:            user.ID,
		Phone:             user.Phone, // Linked directly to the verified account's phone number!
		IDPhotoURL:        photoURL,
		IDPhotoBackURL:    backURL,
		IDNumber:          strings.TrimSpace(req.IDNumber),
		DocumentType:      docType,
		Status:            models.VerificationStatusPending,
		VerificationBadge: models.VerificationBadgeUnverified,
		Notes:             strings.TrimSpace(req.Notes),
		SubmittedAt:       time.Now().UTC(),
	}

	if err := h.store.CreateVerification(r.Context(), item); err != nil {
		respondWithInternalError(w, err)
		return
	}

	item.BadgeLabel = models.FormatVerificationBadge(item.VerificationBadge)

	recordAudit(r, "identity_verification.submit", "CREATE", "identity_verification", item.ID,
		fmt.Sprintf("User submitted identity verification linked to phone %s", user.Phone), true,
		map[string]interface{}{
			"verification_id": item.ID,
			"user_id":         user.ID,
			"phone":           user.Phone,
			"document_type":   docType,
		})

	respondWithJSON(w, http.StatusCreated, dto.APIResponse{
		Success: true,
		Message: "identity verification submitted successfully",
		Data:    item,
	})
}

// GetStatus returns the current identity verification record and badge for the authenticated user.
func (h *IdentityVerificationHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	if userID == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetUserByID(userID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	item, err := h.store.GetLatestVerificationByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, postgres.ErrVerificationNotFound) {
			status := user.IdentityStatus
			if status == "" {
				status = models.VerificationBadgeUnverified
			}
			respondWithJSON(w, http.StatusOK, dto.APIResponse{
				Success: true,
				Data: map[string]interface{}{
					"user_id":            user.ID,
					"phone":              user.Phone,
					"is_phone_verified":  user.IsVerified,
					"identity_status":    status,
					"verification_badge": status,
					"badge_label":        models.FormatVerificationBadge(status),
					"verified_name":      user.VerifiedName,
				},
			})
			return
		}
		respondWithInternalError(w, err)
		return
	}

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data:    item,
	})
}

// AdminList returns all identity verification submissions with filtering and pagination.
func (h *IdentityVerificationHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	badge := strings.TrimSpace(r.URL.Query().Get("badge"))

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 && val <= 100 {
			limit = val
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}

	items, total, err := h.store.ListVerifications(r.Context(), status, badge, limit, offset)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"items":  items,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// AdminGet returns a single identity verification by ID.
func (h *IdentityVerificationHandler) AdminGet(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/identity-verifications/")
	if id == "" {
		respondWithError(w, http.StatusBadRequest, "verification id is required")
		return
	}

	item, err := h.store.GetVerificationByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, postgres.ErrVerificationNotFound) {
			respondWithError(w, http.StatusNotFound, "identity verification not found")
			return
		}
		respondWithInternalError(w, err)
		return
	}

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data:    item,
	})
}

// AdminApprove approves an identity verification, assigning verified name and badge:
// 'id_verified', 'business_verified', or 'both'.
func (h *IdentityVerificationHandler) AdminApprove(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/identity-verifications/")
	if id == "" {
		respondWithError(w, http.StatusBadRequest, "verification id is required")
		return
	}

	var req models.AdminReviewVerificationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid review payload")
		return
	}

	verifiedName := strings.TrimSpace(req.VerifiedName)
	if len(verifiedName) < 2 {
		respondWithError(w, http.StatusBadRequest, "verified_name is required when verifying identity")
		return
	}

	badgeInput := strings.TrimSpace(req.VerificationBadge)
	if badgeInput == "" {
		badgeInput = strings.TrimSpace(req.Status)
	}
	if badgeInput == "" || badgeInput == "approved" {
		badgeInput = models.VerificationBadgeIDVerified
	}

	badge, err := models.NormalizeVerificationBadge(badgeInput)
	if err != nil || (badge != models.VerificationBadgeIDVerified && badge != models.VerificationBadgeBusinessVerified && badge != models.VerificationBadgeBoth) {
		respondWithError(w, http.StatusBadRequest, "status or verification_badge must be 'id_verified', 'business_verified', or 'both'")
		return
	}

	adminID := userIDFromContext(r.Context())
	item, err := h.store.ApproveVerification(r.Context(), id, adminID, verifiedName, badge, strings.TrimSpace(req.Notes))
	if err != nil {
		if errors.Is(err, postgres.ErrVerificationNotFound) {
			respondWithError(w, http.StatusNotFound, "identity verification not found")
			return
		}
		respondWithInternalError(w, err)
		return
	}

	recordAudit(r, "identity_verification.approve", "UPDATE", "identity_verification", id,
		fmt.Sprintf("Admin approved verification for user %s: verified_name=%s, badge=%s", item.UserID, verifiedName, badge), true,
		map[string]interface{}{
			"verification_id": id,
			"user_id":         item.UserID,
			"verified_name":   verifiedName,
			"badge":           badge,
			"status":          models.VerificationStatusApproved,
		})

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Message: fmt.Sprintf("identity verification approved as %s", models.FormatVerificationBadge(badge)),
		Data:    item,
	})
}

// AdminReject rejects an identity verification with a reason.
func (h *IdentityVerificationHandler) AdminReject(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/identity-verifications/")
	if id == "" {
		respondWithError(w, http.StatusBadRequest, "verification id is required")
		return
	}

	var req models.AdminReviewVerificationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid review payload")
		return
	}

	reason := strings.TrimSpace(req.RejectionReason)
	if len(reason) < 3 {
		respondWithError(w, http.StatusBadRequest, "rejection_reason is required")
		return
	}

	adminID := userIDFromContext(r.Context())
	item, err := h.store.RejectVerification(r.Context(), id, adminID, reason, strings.TrimSpace(req.Notes))
	if err != nil {
		if errors.Is(err, postgres.ErrVerificationNotFound) {
			respondWithError(w, http.StatusNotFound, "identity verification not found")
			return
		}
		respondWithInternalError(w, err)
		return
	}

	recordAudit(r, "identity_verification.reject", "UPDATE", "identity_verification", id,
		fmt.Sprintf("Admin rejected verification for user %s: reason=%s", item.UserID, reason), true,
		map[string]interface{}{
			"verification_id": id,
			"user_id":         item.UserID,
			"reason":          reason,
			"status":          models.VerificationStatusRejected,
		})

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Message: "identity verification rejected",
		Data:    item,
	})
}
