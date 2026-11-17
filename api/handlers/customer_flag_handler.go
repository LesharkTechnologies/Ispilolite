package handlers

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/utils"
	"ispilolite/internal/validation"
)

// flagStore is the persistence surface used by the handler so validation can
// be unit-tested without a database.
type flagStore interface {
	CreateFlag(context.Context, *models.CustomerFlag) error
	ListFlagsByReporter(context.Context, string, int) ([]*models.CustomerFlag, error)
	CancelFlag(context.Context, string, string) error
	ListFlags(context.Context, string, int) ([]*models.CustomerFlag, error)
	ReviewFlag(context.Context, string, string, string, string, string) error
	ListMonitored(context.Context, bool, int) ([]*models.MonitoredUser, error)
	ReleaseMonitoring(context.Context, string, string) error
	ListActivity(context.Context, string, int) ([]*models.MonitoredActivity, error)
}

type CustomerFlagHandler struct{ store flagStore }

func NewCustomerFlagHandler() (*CustomerFlagHandler, error) {
	repo, err := postgres.NewFlagRepository()
	if err != nil {
		return nil, err
	}
	return &CustomerFlagHandler{store: repo}, nil
}

func newCustomerFlagHandlerWithStore(store flagStore) *CustomerFlagHandler {
	return &CustomerFlagHandler{store: store}
}

// reportedPhonePattern accepts local or international digits after spaces and
// dashes are stripped. Numbers are never used to auto-create an account.
var reportedPhonePattern = regexp.MustCompile(`^\+?[0-9]{7,15}$`)

// Create files a trust-and-safety report about another user or a phone number.
func (h *CustomerFlagHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ReportedUserID string   `json:"reported_user_id"`
		ReportedPhone  string   `json:"reported_phone"`
		Category       string   `json:"category"`
		Description    string   `json:"description"`
		ImageURLs      []string `json:"image_urls"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid report payload")
		return
	}

	reporter := userIDFromContext(r.Context())
	reportedID := strings.TrimSpace(input.ReportedUserID)
	phone := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(input.ReportedPhone))
	category := strings.ToLower(strings.TrimSpace(input.Category))
	description := strings.TrimSpace(input.Description)

	if len(description) < 10 || len(description) > 4000 {
		respondWithError(w, http.StatusBadRequest, "description must be between 10 and 4000 characters")
		return
	}
	if !models.ValidFlagCategory(category) {
		respondWithError(w, http.StatusBadRequest, "unsupported report category")
		return
	}
	if reportedID == "" && phone == "" {
		respondWithError(w, http.StatusBadRequest, "reported_user_id or reported_phone is required")
		return
	}
	if reportedID != "" && reportedID == reporter {
		respondWithError(w, http.StatusBadRequest, "you cannot report yourself")
		return
	}
	if phone != "" && !reportedPhonePattern.MatchString(phone) {
		respondWithError(w, http.StatusBadRequest, "reported_phone must be a valid phone number")
		return
	}
	images, err := validation.CloudinaryURLs(input.ImageURLs, 3)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now().UTC()
	item := &models.CustomerFlag{
		ID:            utils.GenerateID(),
		ReporterID:    reporter,
		ReportedPhone: phone,
		Category:      category,
		Description:   description,
		ImageURLs:     images,
		Status:        models.FlagPending,
		AccountAction: models.FlagActionNone,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if reportedID != "" {
		item.ReportedUser = &reportedID
	}

	if err := h.store.CreateFlag(r.Context(), item); err != nil {
		switch {
		case errors.Is(err, postgres.ErrFlagDuplicate):
			respondWithError(w, http.StatusConflict, err.Error())
		case errors.Is(err, postgres.ErrFlagLimit):
			respondWithError(w, http.StatusTooManyRequests, err.Error())
		case errors.Is(err, postgres.ErrFlagNotFound):
			respondWithError(w, http.StatusBadRequest, "reported account not found")
		default:
			respondWithInternalError(w, err)
		}
		return
	}
	// Audit deliberately omits the description and image URLs: reports may
	// contain personal data that must not be copied into the audit trail.
	recordAudit(r, "flag.create", "CREATE", "customer_flag", item.ID, "Customer report filed: "+category, true,
		map[string]interface{}{"flag_id": item.ID, "category": category})
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: item})
}

// ListMine returns the caller's own reports and their review status.
func (h *CustomerFlagHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListFlagsByReporter(r.Context(), userIDFromContext(r.Context()), flagLimit(r))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

// Cancel lets a reporter withdraw their own unreviewed report.
func (h *CustomerFlagHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(pathParam(r.URL.Path, "/api/v1/flags/"), "/cancel")
	if id == "" {
		respondWithError(w, http.StatusBadRequest, "report id is required")
		return
	}
	if err := h.store.CancelFlag(r.Context(), id, userIDFromContext(r.Context())); err != nil {
		if errors.Is(err, postgres.ErrFlagForbidden) || errors.Is(err, postgres.ErrFlagNotFound) {
			respondWithError(w, http.StatusNotFound, "report not found or already reviewed")
			return
		}
		respondWithInternalError(w, err)
		return
	}
	recordAudit(r, "flag.cancel", "UPDATE", "customer_flag", id, "Reporter withdrew report "+id, true, nil)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "report withdrawn"})
}

// AdminList returns the moderation queue.
func (h *CustomerFlagHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !models.ValidFlagStatus(status) {
		respondWithError(w, http.StatusBadRequest, "invalid status filter")
		return
	}
	items, err := h.store.ListFlags(r.Context(), status, flagLimit(r))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

// AdminReview moves a report through pending -> in_progress/approved/cancelled
// and applies the approved account action.
func (h *CustomerFlagHandler) AdminReview(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/admin/flags/")
	if id == "" {
		respondWithError(w, http.StatusBadRequest, "report id is required")
		return
	}
	var input struct {
		Status        string `json:"status"`
		AdminFeedback string `json:"admin_feedback"`
		AccountAction string `json:"account_action"`
	}
	if decodeJSON(w, r, &input) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid review payload")
		return
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	action := strings.ToLower(strings.TrimSpace(input.AccountAction))
	if action == "" {
		action = models.FlagActionNone
	}
	feedback := strings.TrimSpace(input.AdminFeedback)

	if status != models.FlagInProgress && status != models.FlagApproved && status != models.FlagCancelled {
		respondWithError(w, http.StatusBadRequest, "status must be in_progress, approved or cancelled")
		return
	}
	if !models.ValidFlagAction(action) {
		respondWithError(w, http.StatusBadRequest, "account_action must be none, monitor or suspend")
		return
	}
	if status != models.FlagApproved && action != models.FlagActionNone {
		respondWithError(w, http.StatusBadRequest, "account_action is only allowed when approving a report")
		return
	}
	if status != models.FlagInProgress && len(feedback) < 3 {
		respondWithError(w, http.StatusBadRequest, "admin_feedback is required when approving or cancelling")
		return
	}
	if len(feedback) > 2000 {
		respondWithError(w, http.StatusBadRequest, "admin_feedback is too long")
		return
	}

	adminID := userIDFromContext(r.Context())
	if err := h.store.ReviewFlag(r.Context(), id, adminID, status, feedback, action); err != nil {
		switch {
		case errors.Is(err, postgres.ErrFlagNotFound):
			respondWithError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, postgres.ErrFlagClosed):
			respondWithError(w, http.StatusConflict, err.Error())
		default:
			respondWithInternalError(w, err)
		}
		return
	}
	recordAudit(r, "flag.review", "UPDATE", "customer_flag", id,
		"Admin reviewed report: "+status+" ("+action+")", true,
		map[string]interface{}{"flag_id": id, "status": status, "account_action": action})
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "report reviewed"})
}

// AdminMonitoring lists users on the verification/monitoring list.
func (h *CustomerFlagHandler) AdminMonitoring(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") != "false"
	items, err := h.store.ListMonitored(r.Context(), activeOnly, flagLimit(r))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

// AdminActivity returns the watched-activity trail for one monitored user.
func (h *CustomerFlagHandler) AdminActivity(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSuffix(pathParam(r.URL.Path, "/api/v1/admin/monitoring/"), "/activity")
	if userID == "" {
		respondWithError(w, http.StatusBadRequest, "user id is required")
		return
	}
	items, err := h.store.ListActivity(r.Context(), userID, flagLimit(r))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

// AdminReleaseMonitoring stops watching a user. Reinstating a suspended
// account is a separate decision and is not performed here.
func (h *CustomerFlagHandler) AdminReleaseMonitoring(w http.ResponseWriter, r *http.Request) {
	userID := pathParam(r.URL.Path, "/api/v1/admin/monitoring/")
	if userID == "" {
		respondWithError(w, http.StatusBadRequest, "user id is required")
		return
	}
	if err := h.store.ReleaseMonitoring(r.Context(), userID, userIDFromContext(r.Context())); err != nil {
		if errors.Is(err, postgres.ErrFlagNotFound) {
			respondWithError(w, http.StatusNotFound, "user is not actively monitored")
			return
		}
		respondWithInternalError(w, err)
		return
	}
	recordAudit(r, "monitoring.release", "UPDATE", "user", userID, "Removed user from monitoring list", true,
		map[string]interface{}{"user_id": userID})
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "monitoring released"})
}

// flagLimit parses the limit query parameter with a safe default and cap.
func flagLimit(r *http.Request) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
}
