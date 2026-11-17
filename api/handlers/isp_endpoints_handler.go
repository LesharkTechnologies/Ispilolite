package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	dto "ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/monitoring"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/repository/redis"
	authsvc "ispilolite/internal/services/auth"
	"ispilolite/internal/services/coverage"
	"ispilolite/internal/services/installation"
	"ispilolite/internal/services/isp"
	"ispilolite/internal/services/jobrequest"
	"ispilolite/internal/services/user"
	"ispilolite/internal/utils"
	"ispilolite/pkg/notifications"
)

// ISPEndpointsHandler handles ISP-specific requests.
type ISPEndpointsHandler struct {
	ispService          *isp.ISPService
	installationService *installation.InstallationService
	userService         *user.UserService
	authService         *authsvc.AuthService
	coverageService     *coverage.Service
	jobRequestService   *jobrequest.Service
	companyRepository   *postgres.CompanyRepository
}

// NewISPEndpointsHandler creates a new ISPEndpointsHandler.
func NewISPEndpointsHandler() (*ISPEndpointsHandler, error) {
	ispRepo, err := postgres.NewISPRepo()
	if err != nil {
		return nil, err
	}
	installationRepo, err := postgres.NewInstallationRepo()
	if err != nil {
		return nil, err
	}
	userRepo, err := postgres.NewUserRepo()
	if err != nil {
		return nil, err
	}
	cacheRepo, err := redis.NewCacheRepo()
	if err != nil {
		return nil, err
	}
	coverageRepo, err := postgres.NewCoverageRepository()
	if err != nil {
		return nil, err
	}
	notificationRepo, err := postgres.NewNotificationRepository()
	if err != nil {
		return nil, err
	}
	jobRepo, err := postgres.NewJobRequestRepo()
	if err != nil {
		return nil, err
	}
	companyRepo, err := postgres.NewCompanyRepository()
	if err != nil {
		return nil, err
	}

	return &ISPEndpointsHandler{
		ispService:          isp.NewISPService(ispRepo),
		installationService: installation.NewInstallationService(installationRepo),
		userService:         user.NewUserService(userRepo),
		authService:         authsvc.NewAuthService(userRepo, cacheRepo),
		coverageService:     coverage.NewService(coverageRepo, notificationRepo),
		jobRequestService:   jobrequest.NewService(jobRepo),
		companyRepository:   companyRepo,
	}, nil
}

func (h *ISPEndpointsHandler) LinkTechnician(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/technicians/")
	id = strings.TrimSuffix(id, "/link")
	if id == "" {
		respondWithError(w, 400, "technician id is required")
		return
	}
	number, err := h.companyRepository.LinkTechnician(userIDFromContext(r.Context()), id)
	if err != nil {
		respondWithError(w, 409, "technician could not be linked")
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: map[string]string{"company_technician_id": number}})
}
func (h *ISPEndpointsHandler) ListCompanyTechnicians(w http.ResponseWriter, r *http.Request) {
	items, err := h.companyRepository.ListCompanyTechnicians(userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, 500, "failed to list company technicians")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
func (h *ISPEndpointsHandler) EndTechnicianLink(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/technicians/")
	if id == "" {
		id = pathParam(r.URL.Path, "/api/v1/technicians/")
	}
	id = strings.TrimSuffix(id, "/end")
	id = strings.TrimSuffix(id, "/link")
	if id == "" {
		respondWithError(w, 400, "technician id is required")
		return
	}
	if err := h.companyRepository.EndTechnicianLink(userIDFromContext(r.Context()), id, time.Now().UTC()); err != nil {
		respondWithError(w, 404, "company technician link not found")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "technician link ended"})
}

func (h *ISPEndpointsHandler) CreateTechnicianWorkRequest(w http.ResponseWriter, r *http.Request) {
	var req dto.JobRequestRequest
	if decodeJSON(w, r, &req) != nil || strings.TrimSpace(req.TargetTechnicianID) == "" || strings.TrimSpace(req.ServiceType) == "" {
		respondWithError(w, 400, "target_technician_id and service_type are required")
		return
	}
	job, err := h.jobRequestService.Create(userIDFromContext(r.Context()), &models.JobRequest{RequesterRole: "isp", RequestType: "technician_service", Mode: "direct", TechnicianID: req.TargetTechnicianID, ServiceType: req.ServiceType, Description: req.Description, County: req.County, Town: req.Town, Village: req.Village, Budget: req.Budget})
	if err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: job})
}

func (h *ISPEndpointsHandler) AssignTechnician(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSuffix(pathParam(r.URL.Path, "/api/v1/my/requests/"), "/assign-technician")
	var req struct {
		TechnicianID string `json:"technician_id"`
	}
	if requestID == "" || decodeJSON(w, r, &req) != nil || req.TechnicianID == "" {
		respondWithError(w, 400, "technician_id is required")
		return
	}
	job, err := h.jobRequestService.AssignTechnician(userIDFromContext(r.Context()), requestID, req.TechnicianID)
	if err != nil {
		respondWithError(w, 409, "technician assignment failed")
		return
	}
	monitoring.RecordFor(r, req.TechnicianID, monitoring.EventJobClaim, "job", requestID,
		map[string]interface{}{"assigned_by": userIDFromContext(r.Context())})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: job})
}

func (h *ISPEndpointsHandler) GetCoverageAreas(w http.ResponseWriter, r *http.Request) {
	areas, err := h.coverageService.List(userIDFromContext(r.Context()), r.URL.Query().Get("county"))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to list coverage areas")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"areas": areas}})
}

func (h *ISPEndpointsHandler) AddCoverageArea(w http.ResponseWriter, r *http.Request) {
	var req dto.CoverageRequest
	if decodeJSON(w, r, &req) != nil || strings.TrimSpace(req.LocationID) == "" {
		respondWithError(w, http.StatusBadRequest, "location_id is required")
		return
	}
	if err := h.coverageService.Add(userIDFromContext(r.Context()), req.LocationID); err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to add coverage area")
		return
	}
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Message: "coverage area added"})
}

func (h *ISPEndpointsHandler) DeleteCoverageArea(w http.ResponseWriter, r *http.Request) {
	locationID := pathParam(r.URL.Path, "/api/v1/my/coverage/areas/")
	if locationID == "" {
		locationID = pathParam(r.URL.Path, "/api/v1/coverage/areas/")
	}
	if locationID == "" {
		locationID = pathParam(r.URL.Path, "/api/v1/my/coverage/")
	}
	if locationID == "" {
		locationID = strings.TrimSpace(r.URL.Query().Get("location_id"))
	}
	if locationID == "" {
		respondWithError(w, http.StatusBadRequest, "location id is required")
		return
	}
	if err := h.coverageService.Remove(userIDFromContext(r.Context()), locationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "coverage area not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to remove coverage area")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "coverage area removed"})
}

func (h *ISPEndpointsHandler) GetCoverageRecommendations(w http.ResponseWriter, r *http.Request) {
	places, err := h.coverageService.Recommendations(userIDFromContext(r.Context()), r.URL.Query().Get("county"), 10)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to create coverage recommendations")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"recommendations": places}})
}

func (h *ISPEndpointsHandler) GetNotifications(w http.ResponseWriter, r *http.Request) {
	items, err := h.coverageService.Notifications(userIDFromContext(r.Context()), r.URL.Query().Get("unread") == "true", 20)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

func (h *ISPEndpointsHandler) ReadNotification(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/notifications/")
	if id == "" {
		respondWithError(w, http.StatusBadRequest, "notification id is required")
		return
	}
	if err := h.coverageService.MarkRead(userIDFromContext(r.Context()), id); err != nil {
		respondWithError(w, http.StatusNotFound, "notification not found")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "notification marked as read"})
}

func (h *ISPEndpointsHandler) StreamNotifications(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondWithError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	updates, unsubscribe := notifications.Default.Subscribe(userIDFromContext(r.Context()))
	defer unsubscribe()
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload, ok := <-updates:
			if !ok {
				return
			}
			_, _ = w.Write([]byte("event: notification\ndata: " + string(payload) + "\n\n"))
			flusher.Flush()
		}
	}
}

// GetProfile returns the ISP's profile.
func (h *ISPEndpointsHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	ispID := userIDFromContext(r.Context())
	isp, err := h.ispService.GetISPByID(ispID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "ISP not found")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: isp})
}

// UpdateProfile updates the ISP's profile.
func (h *ISPEndpointsHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req dto.ISPProfileRequest
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Name) == "" || req.AvgPrice < 0 || req.AvgResponseTime < 0 {
		respondWithError(w, http.StatusBadRequest, "invalid isp profile")
		return
	}
	ispID := userIDFromContext(r.Context())
	if err := h.ispService.UpdateISP(&models.ISP{ID: ispID, Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description), LogoURL: strings.TrimSpace(req.LogoURL), CustomerCareNumber: strings.TrimSpace(req.CustomerCareNumber), AvgResponseTime: req.AvgResponseTime, AvgPrice: req.AvgPrice}); err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to update isp profile")
		return
	}
	if account, err := h.userService.GetUserByID(ispID); err == nil {
		account.Name = strings.TrimSpace(req.Name)
		if strings.TrimSpace(req.LogoURL) != "" {
			account.AvatarURL = strings.TrimSpace(req.LogoURL)
		}
		account.UpdatedAt = time.Now().UTC()
		if err := h.userService.UpdateUser(account); err != nil {
			respondWithInternalError(w, err)
			return
		}
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "isp profile updated"})
}

// GetInstallations returns a list of the ISP's installations.
func (h *ISPEndpointsHandler) GetInstallations(w http.ResponseWriter, r *http.Request) {
	ispID := userIDFromContext(r.Context())
	installations, err := h.installationService.GetInstallationsByISPID(ispID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get installations")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: installations})
}

func (h *ISPEndpointsHandler) GetJobs(w http.ResponseWriter, r *http.Request) {
	items, err := h.jobRequestService.ListForISP(userIDFromContext(r.Context()), strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: items})
}

func (h *ISPEndpointsHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/isp-jobs/")
	if id == "" {
		id = pathParam(r.URL.Path, "/api/v1/my/jobs/")
	}
	item, err := h.jobRequestService.GetForParticipant(id, userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, http.StatusNotFound, "job not found")
		return
	}
	// ISP staff opening a client's job details is a watched "client view".
	monitoring.Record(r, monitoring.EventClientView, "job", id, nil)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: item})
}

// UpdateInstallation updates the status of an installation.
func (h *ISPEndpointsHandler) UpdateInstallation(w http.ResponseWriter, r *http.Request) {
	installationID := pathParam(r.URL.Path, "/api/v1/installations/")
	var req dto.InstallationStatusRequest
	if installationID == "" || decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid installation update")
		return
	}
	valid := map[string]bool{"accepted": true, "assigned": true, "scheduled": true, "in_progress": true, "completed": true, "cancelled": true, "rejected": true}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if !valid[req.Status] {
		respondWithError(w, http.StatusBadRequest, "invalid installation status")
		return
	}
	if req.Status == "rejected" {
		req.Status = "cancelled"
	}
	if req.Status == "assigned" && strings.TrimSpace(req.TechnicianID) == "" {
		respondWithError(w, http.StatusBadRequest, "technician_id is required when assigning")
		return
	}
	ispID := userIDFromContext(r.Context())
	if req.TechnicianID != "" {
		technician, err := h.userService.GetUserByID(req.TechnicianID)
		if err != nil || technician.Role != "technician" || technician.ISP_ID != ispID {
			respondWithError(w, http.StatusBadRequest, "technician does not belong to this isp")
			return
		}
	}
	installation, err := h.installationService.GetInstallationByID(installationID)
	if err != nil || installation.IspID != ispID {
		respondWithError(w, http.StatusNotFound, "installation not found")
		return
	}
	installation.Status, installation.TechnicianID, installation.UpdatedAt = req.Status, req.TechnicianID, time.Now().UTC()
	if err := h.installationService.UpdateInstallation(installation); err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to update installation")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: installation})
}

// GetTechnicians returns a list of the ISP's technicians.
func (h *ISPEndpointsHandler) GetTechnicians(w http.ResponseWriter, r *http.Request) {
	ispID := userIDFromContext(r.Context())
	technicians, err := h.userService.GetTechniciansByISPID(ispID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get technicians")
		return
	}
	respondWithJSON(w, http.StatusOK, technicians)
}

// AddTechnician adds a new technician to the ISP.
func (h *ISPEndpointsHandler) AddTechnician(w http.ResponseWriter, r *http.Request) {
	var req dto.TechnicianInviteRequest
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Phone) == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Username) == "" || len(req.Password) < 8 {
		respondWithError(w, http.StatusBadRequest, "phone, name, username, and an 8-character password are required")
		return
	}
	technician, err := h.authService.CreateUser(dto.RegisterRequest{Phone: req.Phone, Name: req.Name, Email: req.Email, Role: "technician", Username: req.Username, Password: req.Password})
	if err != nil {
		if errors.Is(err, authsvc.ErrUserAlreadyExists) {
			respondWithError(w, http.StatusConflict, err.Error())
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to create technician")
		return
	}
	companyID, err := h.companyRepository.LinkTechnician(userIDFromContext(r.Context()), technician.ID)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	_, expiresIn, err := h.authService.IssueOTP(technician.ID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to send technician verification code")
		return
	}
	recordAudit(r, "isp.technician_add", "CREATE", "technician", technician.ID, "ISP added technician "+technician.Username, true, map[string]interface{}{"technician_id": technician.ID, "username": technician.Username})
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Message: "technician registered; phone verification required", Data: map[string]any{"technician_id": technician.ID, "company_technician_id": companyID, "expires_in": expiresIn}})
}

// RemoveTechnician removes a technician from the ISP.
func (h *ISPEndpointsHandler) RemoveTechnician(w http.ResponseWriter, r *http.Request) {
	technicianID := pathParam(r.URL.Path, "/api/v1/technicians/")
	if technicianID == "" {
		technicianID = pathParam(r.URL.Path, "/api/v1/my/technicians/")
	}
	technicianID = strings.TrimSuffix(technicianID, "/link")
	if technicianID == "" {
		respondWithError(w, http.StatusBadRequest, "technician id is required")
		return
	}
	ispID := userIDFromContext(r.Context())
	technician, err := h.userService.GetUserByID(technicianID)
	if err != nil || technician.Role != "technician" || technician.ISP_ID != ispID {
		respondWithError(w, http.StatusNotFound, "technician not found")
		return
	}
	if err := h.companyRepository.EndTechnicianLink(ispID, technicianID, time.Now().UTC()); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			respondWithInternalError(w, err)
			return
		}
		// Legacy technicians created before company links were recorded may
		// only have users.isp_id. Clear that old association as well.
		technician.ISP_ID, technician.UpdatedAt = "", time.Now().UTC()
		if err := h.userService.UpdateUser(technician); err != nil {
			respondWithInternalError(w, err)
			return
		}
	}
	recordAudit(r, "isp.technician_remove", "DELETE", "technician", technicianID, "ISP removed technician "+technicianID, true, map[string]interface{}{"technician_id": technicianID})
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "technician removed"})
}

// SetTechnicianStatus allows an ISP to suspend, deactivate, or reactivate a technician.
// When suspended/inactive, the technician's sessions are revoked and their account is blocked from using the system.
func (h *ISPEndpointsHandler) SetTechnicianStatus(w http.ResponseWriter, r *http.Request) {
	ispID := userIDFromContext(r.Context())
	if ispID == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	technicianID := pathParam(r.URL.Path, "/api/v1/my/technicians/")
	technicianID = strings.TrimSuffix(technicianID, "/status")
	technicianID = strings.TrimSuffix(technicianID, "/suspend")
	technicianID = strings.TrimSuffix(technicianID, "/activate")
	if technicianID == "" {
		respondWithError(w, http.StatusBadRequest, "technician id is required")
		return
	}

	var req dto.UpdateTechnicianStatusRequest
	if strings.HasSuffix(r.URL.Path, "/suspend") {
		req.Status = "suspended"
	} else if strings.HasSuffix(r.URL.Path, "/activate") {
		req.Status = "active"
	} else {
		if decodeJSON(w, r, &req) != nil {
			respondWithError(w, http.StatusBadRequest, "invalid request payload")
			return
		}
	}

	status := strings.ToLower(strings.TrimSpace(req.Status))
	switch status {
	case "active", "suspended", "inactive":
	default:
		respondWithError(w, http.StatusBadRequest, "status must be 'active', 'suspended', or 'inactive'")
		return
	}

	if err := h.userService.SetISPTechnicianStatus(ispID, technicianID, status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "technician not found or not assigned to this ISP")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to update technician status: "+err.Error())
		return
	}

	recordAudit(r, "isp.technician_status_update", "UPDATE", "technician", technicianID,
		fmt.Sprintf("ISP %s updated technician %s status to %s", ispID, technicianID, status), true,
		map[string]interface{}{
			"isp_id":        ispID,
			"technician_id": technicianID,
			"new_status":    status,
		})

	msg := fmt.Sprintf("technician status updated to %s", status)
	if status == "suspended" || status == "inactive" {
		msg = fmt.Sprintf("technician %s has been %s and blocked from system access", technicianID, status)
	}

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Message: msg,
		Data: map[string]interface{}{
			"technician_id": technicianID,
			"status":        status,
		},
	})
}

// ChangeTechnicianAccountType allows an ISP to change a technician's user type.
func (h *ISPEndpointsHandler) ChangeTechnicianAccountType(w http.ResponseWriter, r *http.Request) {
	ispID := userIDFromContext(r.Context())
	if ispID == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	technicianID := pathParam(r.URL.Path, "/api/v1/my/technicians/")
	technicianID = strings.TrimSuffix(technicianID, "/account-type")
	technicianID = strings.TrimSuffix(technicianID, "/role")
	if technicianID == "" {
		respondWithError(w, http.StatusBadRequest, "technician id is required")
		return
	}

	var req dto.ChangeAccountTypeRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	targetRole, err := models.NormalizeRole(req.DesiredRole())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	if targetRole == models.RoleAdmin {
		respondWithError(w, http.StatusForbidden, "cannot change technician role to admin")
		return
	}

	// Verify technician belongs to this ISP
	tech, err := h.userService.GetUserByID(technicianID)
	if err != nil || (tech.ISP_ID != ispID && tech.Role != "technician") {
		respondWithError(w, http.StatusNotFound, "technician not found or not associated with this ISP")
		return
	}

	// ISP change bypasses the 24h user cooldown
	if err := h.userService.ChangeAccountType(technicianID, targetRole, true); err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to update technician account type: "+err.Error())
		return
	}

	recordAudit(r, "isp.technician_role_update", "UPDATE", "technician", technicianID,
		fmt.Sprintf("ISP %s updated technician %s account type to %s", ispID, technicianID, targetRole), true,
		map[string]interface{}{
			"isp_id":        ispID,
			"technician_id": technicianID,
			"new_role":      targetRole,
		})

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Message: fmt.Sprintf("technician account type updated to %s", targetRole),
		Data: map[string]interface{}{
			"technician_id": technicianID,
			"role":          targetRole,
		},
	})
}

// CreatePackage creates a new internet package.
func (h *ISPEndpointsHandler) CreatePackage(w http.ResponseWriter, r *http.Request) {
	var req dto.ISPPackageRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid package")
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	pkg := &models.ISPPackage{ID: utils.GenerateID(), ISP_ID: userIDFromContext(r.Context()), Name: req.Name, Category: req.Category, SpeedValue: req.SpeedValue, SpeedUnitID: req.SpeedUnitID, BasePrice: req.BasePrice, BillingCycle: req.BillingCycle, CapacityType: req.CapacityType, CapacityValue: req.CapacityValue, CapacityUnitID: req.CapacityUnitID, IsActive: active, Description: strings.TrimSpace(req.Description), Tier: req.Tier, OveragePrice: req.OveragePrice}
	if err := h.ispService.CreatePackage(pkg); err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	recordAudit(r, "package.create", "CREATE", "isp_package", pkg.ID, "ISP created internet package "+pkg.Name, true, map[string]interface{}{"package_id": pkg.ID, "name": pkg.Name})
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: pkg})
}

// UpdatePackage updates an existing internet package.
func (h *ISPEndpointsHandler) UpdatePackage(w http.ResponseWriter, r *http.Request) {
	packageID := pathParam(r.URL.Path, "/api/v1/packages/")
	var req dto.ISPPackageRequest
	if packageID == "" || decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid package")
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	pkg := &models.ISPPackage{ID: packageID, ISP_ID: userIDFromContext(r.Context()), Name: req.Name, Category: req.Category, SpeedValue: req.SpeedValue, SpeedUnitID: req.SpeedUnitID, BasePrice: req.BasePrice, BillingCycle: req.BillingCycle, CapacityType: req.CapacityType, CapacityValue: req.CapacityValue, CapacityUnitID: req.CapacityUnitID, IsActive: active, Description: strings.TrimSpace(req.Description), Tier: req.Tier, OveragePrice: req.OveragePrice}
	if err := h.ispService.UpdatePackage(pkg); err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusNotFound, "package not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to update package")
		return
	}
	recordAudit(r, "package.update", "UPDATE", "isp_package", pkg.ID, "ISP updated internet package "+pkg.Name, true, map[string]interface{}{"package_id": pkg.ID, "name": pkg.Name})
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: pkg})
}

func (h *ISPEndpointsHandler) SetPackageCountyPrice(w http.ResponseWriter, r *http.Request) {
	packageID := pathParam(r.URL.Path, "/api/v1/packages/")
	packageID = strings.TrimSuffix(packageID, "/prices")
	var req dto.ISPPackagePriceRequest
	if packageID == "" || decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid county price")
		return
	}
	if err := h.ispService.SetPackageCountyPrice(packageID, userIDFromContext(r.Context()), req.County, req.Price); err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, 404, "package not found")
			return
		}
		respondWithError(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "county price updated"})
}
func (h *ISPEndpointsHandler) ArchivePackage(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/packages/")
	if id == "" {
		id = pathParam(r.URL.Path, "/api/v1/my/packages/")
	}
	id = strings.TrimSuffix(id, "/archive")
	if id == "" {
		respondWithError(w, 400, "package id is required")
		return
	}
	if err := h.ispService.ArchivePackage(id, userIDFromContext(r.Context())); err != nil {
		respondWithError(w, 404, "package not found")
		return
	}
	recordAudit(r, "package.archive", "UPDATE", "isp_package", id, "ISP archived package "+id, true, map[string]interface{}{"package_id": id})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "package archived"})
}
func (h *ISPEndpointsHandler) DeletePackage(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/packages/")
	if id == "" {
		id = pathParam(r.URL.Path, "/api/v1/my/packages/")
	}
	if id == "" {
		respondWithError(w, 400, "package id is required")
		return
	}
	if err := h.ispService.DeletePackage(id, userIDFromContext(r.Context())); err != nil {
		respondWithError(w, 409, err.Error())
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "package deleted"})
}
func (h *ISPEndpointsHandler) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.ispService.ListSubscriptions(userIDFromContext(r.Context()), userRoleFromContext(r.Context()), r.URL.Query().Get("status"), limit)
	if err != nil {
		respondWithError(w, 500, "failed to list subscriptions")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
func (h *ISPEndpointsHandler) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/subscriptions/")
	var req dto.PackageSubscriptionStatusRequest
	if id == "" || decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid subscription update")
		return
	}
	var ends *time.Time
	if req.EndsAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.EndsAt)
		if err != nil {
			respondWithError(w, 400, "ends_at must be RFC3339")
			return
		}
		ends = &parsed
	}
	if err := h.ispService.UpdateSubscription(id, userIDFromContext(r.Context()), req.Status, ends); err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "subscription updated"})
}

func (h *ISPEndpointsHandler) RecordSubscriptionUsage(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/subscriptions/")
	var req dto.PackageUsageRequest
	if id == "" || decodeJSON(w, r, &req) != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid usage event")
		return
	}
	var measuredAt time.Time
	if req.MeasuredAt != "" {
		var err error
		measuredAt, err = time.Parse(time.RFC3339, req.MeasuredAt)
		if err != nil {
			respondWithErrorRaw(w, http.StatusBadRequest, "measured_at must be RFC3339")
			return
		}
	}
	subscription, err := h.ispService.RecordUsage(id, userIDFromContext(r.Context()), req.ExternalRef, req.UsageValue, measuredAt)
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: subscription})
}
