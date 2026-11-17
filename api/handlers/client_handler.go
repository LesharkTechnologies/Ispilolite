package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"ispilolite/api/dto"
	"ispilolite/internal/middleware"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	redisrepo "ispilolite/internal/repository/redis"
	"ispilolite/internal/services/installation"
	"ispilolite/internal/services/isp"
	"ispilolite/internal/services/jobrequest"
	"ispilolite/internal/services/user"
	"ispilolite/pkg/database"
)

type ClientHandler struct {
	db            *sql.DB
	jobs          *jobrequest.Service
	users         *user.UserService
	installations *installation.InstallationService
	packages      *isp.ISPService
}

func NewClientHandler() (*ClientHandler, error) {
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	repo, err := postgres.NewUserRepo()
	if err != nil {
		return nil, err
	}
	jobRepo, err := postgres.NewJobRequestRepo()
	if err != nil {
		return nil, err
	}
	installationRepo, err := postgres.NewInstallationRepo()
	if err != nil {
		return nil, err
	}
	ispRepo, err := postgres.NewISPRepo()
	if err != nil {
		return nil, err
	}
	redisClient, err := database.MustRedis()
	if err != nil {
		return nil, err
	}
	cachedJobs := redisrepo.NewCachedJobRepository(jobRepo, redisClient)
	return &ClientHandler{
		db:            writer,
		jobs:          jobrequest.NewService(cachedJobs),
		users:         user.NewUserService(repo),
		installations: installation.NewInstallationService(installationRepo),
		packages:      isp.NewISPService(ispRepo),
	}, nil
}

func (h *ClientHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	u, err := h.users.GetUserByID(userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, 404, "user not found")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: u})
}
func (h *ClientHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req dto.UpdateProfileRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid request payload")
		return
	}
	u, err := h.users.GetUserByID(userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, 404, "user not found")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		u.Name = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(req.DisplayName) != "" {
		u.DisplayName = strings.TrimSpace(req.DisplayName)
	}
	if strings.TrimSpace(req.Email) != "" {
		u.Email = strings.TrimSpace(req.Email)
	}
	if strings.TrimSpace(req.AvatarURL) != "" {
		u.AvatarURL = strings.TrimSpace(req.AvatarURL)
	}
	if req.Location != nil {
		u.Location = &models.Coordinates{Lat: req.Location.Lat, Lng: req.Location.Lng}
	}
	if strings.EqualFold(req.Locale, "en") || strings.EqualFold(req.Locale, "sw") {
		u.Locale = strings.ToLower(req.Locale)
	}
	if err := h.users.UpdateUser(u); err != nil {
		respondWithError(w, 500, "failed to update profile")
		return
	}
	recordAudit(r, "user.profile_update", "UPDATE", "user", u.ID, "User updated personal profile details", true, nil)
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: u})
}

// UpdateDisplayName provides a dedicated endpoint to update a user's public display name (e.g. "Leshark")
// while maintaining their legal/verified identity (e.g. "identity [Collins Muthomi]").
func (h *ClientHandler) UpdateDisplayName(w http.ResponseWriter, r *http.Request) {
	var req dto.UpdateDisplayNameRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if len(displayName) < 2 || len(displayName) > 60 {
		respondWithError(w, http.StatusBadRequest, "display_name must be between 2 and 60 characters")
		return
	}
	uid := userIDFromContext(r.Context())
	if uid == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := h.users.SetDisplayName(uid, displayName); err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to update display name")
		return
	}
	u, err := h.users.GetUserByID(uid)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to reload user profile")
		return
	}
	recordAudit(r, "user.display_name_update", "UPDATE", "user", uid,
		fmt.Sprintf("User updated display name to %q", displayName), true,
		map[string]interface{}{"display_name": displayName, "identity_label": u.IdentityLabel()})

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Message: "display name updated successfully",
		Data: map[string]interface{}{
			"id":              u.ID,
			"display_name":    u.DisplayName,
			"name":            u.Name,
			"verified_name":   u.VerifiedName,
			"identity_label":  u.IdentityLabel(),
			"identity_status": u.IdentityStatus,
		},
	})
}

// ChangeAccountType allows users to switch their account type (Customer, Technician, ISP, Both).
// A strict 24-hour cooldown is enforced once changed unless an admin reviews/overrides.
// On success, returns updated profile and a fresh JWT token matching the new role.
func (h *ClientHandler) ChangeAccountType(w http.ResponseWriter, r *http.Request) {
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
		respondWithError(w, http.StatusForbidden, "cannot change account type to admin")
		return
	}
	uid := userIDFromContext(r.Context())
	if uid == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	existingUser, err := h.users.GetUserByID(uid)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	if existingUser.Role == targetRole {
		respondWithJSON(w, http.StatusOK, dto.APIResponse{
			Success: true,
			Message: fmt.Sprintf("account is already configured as %s", targetRole),
			Data: map[string]interface{}{
				"user": existingUser,
				"role": existingUser.Role,
			},
		})
		return
	}

	// Check 24-hour cooldown
	if existingUser.RoleChangedAt != nil {
		elapsed := time.Since(*existingUser.RoleChangedAt)
		if elapsed < 24*time.Hour {
			remaining := 24*time.Hour - elapsed
			hours := int(remaining.Hours())
			minutes := int(remaining.Minutes()) % 60
			cooldownMsg := fmt.Sprintf("Account type can only be changed once every 24 hours. Please wait %dh %dm or contact admin for review.", hours, minutes)
			recordAudit(r, "user.change_account_type_rejected", "UPDATE", "user", uid, cooldownMsg, false, map[string]interface{}{
				"current_role":    existingUser.Role,
				"requested_role":  targetRole,
				"role_changed_at": existingUser.RoleChangedAt,
			})
			respondWithError(w, http.StatusTooManyRequests, cooldownMsg)
			return
		}
	}

	if err := h.users.ChangeAccountType(uid, targetRole, false); err != nil {
		if errors.Is(err, postgres.ErrRoleChangeCooldown) || strings.Contains(err.Error(), "24 hours") {
			respondWithError(w, http.StatusTooManyRequests, "account type can only be changed once every 24 hours. Contact admin for review.")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to update account type: "+err.Error())
		return
	}

	updatedUser, err := h.users.GetUserByID(uid)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to reload user profile")
		return
	}

	// Generate fresh JWT token matching the new role so the client can continue immediately
	newToken, _ := middleware.GenerateJWT(updatedUser.ID, updatedUser.Role, 72)

	recordAudit(r, "user.change_account_type", "UPDATE", "user", uid,
		fmt.Sprintf("User switched account type from %s to %s", existingUser.Role, updatedUser.Role), true,
		map[string]interface{}{
			"previous_role":   existingUser.Role,
			"new_role":        updatedUser.Role,
			"role_changed_at": updatedUser.RoleChangedAt,
		})

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Message: fmt.Sprintf("account type successfully changed to %s", updatedUser.Role),
		Data: map[string]interface{}{
			"user":            updatedUser,
			"token":           newToken,
			"token_type":      "Bearer",
			"role":            updatedUser.Role,
			"role_changed_at": updatedUser.RoleChangedAt,
			"cooldown_hours":  24,
		},
	})
}

// GetProfileCompletion analyzes the user's role and checks which fields and documents have been filled
// versus what is still missing (e.g. ID verification document, skills, hourly rate, bio, logo, display name).
func (h *ClientHandler) GetProfileCompletion(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	if uid == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.users.GetUserByID(uid)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	report := h.computeCompletionReport(r.Context(), u)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data:    report,
	})
}

func (h *ClientHandler) computeCompletionReport(ctx context.Context, u *models.User) *dto.ProfileCompletionReport {
	report := &dto.ProfileCompletionReport{
		UserID:         u.ID,
		Role:           u.Role,
		IdentityStatus: u.IdentityStatus,
		VerifiedName:   u.VerifiedName,
		UploadInstructions: map[string]string{
			"media_upload":      "POST /api/v1/media/upload (multipart form: kind=profile, portfolio, identity_document, isp_logo)",
			"media_signature":   "POST /api/v1/media/upload-signature (for direct Cloudinary client-side upload)",
			"identity_verify":   "POST /api/v1/identity/verify (id_photo_url, id_photo_back_url, id_number, document_type)",
			"fill_missing":      "PATCH /api/v1/my/profile/fill-missing (submit all unfulfilled fields in one payload)",
			"display_name":      "PATCH /api/v1/my/display-name (set public moniker e.g. 'Leshark')",
			"portfolio_profile": "PUT /api/v1/my/portfolio/profile (technician bio, hourly rate, skills, availability)",
			"skills":            "POST /api/v1/my/skills (add specific technician skills)",
			"packages":          "POST /api/v1/my/packages (create ISP packages)",
		},
	}

	// 1. Common user fields
	if strings.TrimSpace(u.Phone) != "" {
		report.FilledFields = append(report.FilledFields, "phone")
	} else {
		report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
			Field: "phone", Label: "Phone Number", Category: "profile",
			Description: "Account phone number is required for verification",
			Endpoint: "/api/v1/my/profile", Method: "PATCH",
		})
	}
	report.TotalRequired++

	if strings.TrimSpace(u.Name) != "" {
		report.FilledFields = append(report.FilledFields, "name")
	} else {
		report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
			Field: "name", Label: "Full Name", Category: "profile",
			Description: "Your legal name",
			Endpoint: "/api/v1/my/profile", Method: "PATCH",
		})
	}
	report.TotalRequired++

	if strings.TrimSpace(u.DisplayName) != "" {
		report.FilledFields = append(report.FilledFields, "display_name")
	} else {
		report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
			Field: "display_name", Label: "Display Name / Moniker", Category: "profile",
			Description: "Public moniker shown on discovery cards (e.g. 'Leshark')",
			Endpoint: "/api/v1/my/display-name", Method: "PATCH",
		})
	}
	report.TotalRequired++

	if strings.TrimSpace(u.AvatarURL) != "" {
		report.FilledFields = append(report.FilledFields, "avatar_url")
	} else {
		report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
			Field: "avatar_url", Label: "Profile Picture / Avatar", Category: "profile",
			Description: "Upload a profile photo via /media/upload?kind=profile",
			Endpoint: "/api/v1/media/upload?kind=profile", Method: "POST",
		})
	}
	report.TotalRequired++

	if strings.TrimSpace(u.County) != "" && strings.TrimSpace(u.Town) != "" {
		report.FilledFields = append(report.FilledFields, "location")
	} else {
		report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
			Field: "location", Label: "County & Town", Category: "location",
			Description: "Location to match nearby clients and jobs",
			Endpoint: "/api/v1/my/profile", Method: "PATCH",
		})
	}
	report.TotalRequired++

	// 2. Identity Verification (Technician, ISP, Both)
	if u.Role == models.RoleTechnician || u.Role == models.RoleISP || u.Role == models.RoleBoth {
		report.TotalRequired++
		switch u.IdentityStatus {
		case "id_verified", "business_verified", "both":
			report.FilledFields = append(report.FilledFields, "identity_verification")
		case "pending":
			report.FilledFields = append(report.FilledFields, "identity_verification (pending admin review)")
		default:
			report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
				Field: "id_photo_url", Label: "Identity Verification Document", Category: "identity",
				Description: "Upload ID photo/passport or business permit to receive verified badge",
				Endpoint: "/api/v1/identity/verify", Method: "POST",
			})
		}
	}

	// 3. Role-specific checks
	isTech := u.Role == models.RoleTechnician || u.Role == models.RoleBoth
	isISP := u.Role == models.RoleISP || u.Role == models.RoleBoth

	if isTech {
		report.TotalRequired += 3
		var bio string
		var pricePerHour float64
		var skillCount int
		if h.db != nil {
			_ = h.db.QueryRowContext(ctx, `SELECT COALESCE(bio,''), COALESCE(price_per_hour,0) FROM technician_profiles WHERE technician_id=$1`, u.ID).Scan(&bio, &pricePerHour)
			_ = h.db.QueryRowContext(ctx, `SELECT count(*) FROM technician_profile_skills WHERE technician_id=$1`, u.ID).Scan(&skillCount)
		}

		if strings.TrimSpace(bio) != "" {
			report.FilledFields = append(report.FilledFields, "bio")
		} else {
			report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
				Field: "bio", Label: "Technician Bio / Experience", Category: "services",
				Description: "Describe your experience, past installations, and specialties",
				Endpoint: "/api/v1/my/portfolio/profile", Method: "PUT",
			})
		}

		if pricePerHour > 0 {
			report.FilledFields = append(report.FilledFields, "price_per_hour")
		} else {
			report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
				Field: "price_per_hour", Label: "Hourly Service Rate (KES)", Category: "services",
				Description: "Set your hourly rate for marketplace card display",
				Endpoint: "/api/v1/my/portfolio/profile", Method: "PUT",
			})
		}

		if skillCount > 0 {
			report.FilledFields = append(report.FilledFields, "skills")
		} else {
			report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
				Field: "skills", Label: "Technical Skills", Category: "services",
				Description: "Add skills (fiber, wifi, cctv, splicing, networking)",
				Endpoint: "/api/v1/my/skills", Method: "POST",
			})
		}
	}

	if isISP {
		report.TotalRequired += 3
		var logoURL, careNumber, desc string
		var pkgCount int
		if h.db != nil {
			_ = h.db.QueryRowContext(ctx, `SELECT COALESCE(logo_url,''), COALESCE(customer_care_number,''), COALESCE(description,'') FROM isps WHERE id=$1`, u.ID).Scan(&logoURL, &careNumber, &desc)
			_ = h.db.QueryRowContext(ctx, `SELECT count(*) FROM isp_packages WHERE isp_id=$1 AND archived_at IS NULL`, u.ID).Scan(&pkgCount)
		}

		if strings.TrimSpace(logoURL) != "" {
			report.FilledFields = append(report.FilledFields, "logo_url")
		} else {
			report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
				Field: "logo_url", Label: "Company Logo", Category: "profile",
				Description: "Upload your company logo via /media/upload?kind=isp_logo",
				Endpoint: "/api/v1/media/upload?kind=isp_logo", Method: "POST",
			})
		}

		if strings.TrimSpace(careNumber) != "" {
			report.FilledFields = append(report.FilledFields, "customer_care_number")
		} else {
			report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
				Field: "customer_care_number", Label: "Customer Care Phone", Category: "profile",
				Description: "Customer support helpline number",
				Endpoint: "/api/v1/my/profile", Method: "PATCH",
			})
		}

		if pkgCount > 0 {
			report.FilledFields = append(report.FilledFields, "packages")
		} else {
			report.MissingFields = append(report.MissingFields, dto.MissingFieldItem{
				Field: "packages", Label: "Internet Packages", Category: "services",
				Description: "Publish at least one internet package",
				Endpoint: "/api/v1/my/packages", Method: "POST",
			})
		}
	}

	report.FilledCount = len(report.FilledFields)
	if report.TotalRequired > 0 {
		report.CompletionPercentage = (report.FilledCount * 100) / report.TotalRequired
	}
	report.IsComplete = len(report.MissingFields) == 0
	return report
}

// FillMissingProfile allows users (especially after role switching or new registration)
// to upload and submit any or all unfulfilled profile data in a single request.
func (h *ClientHandler) FillMissingProfile(w http.ResponseWriter, r *http.Request) {
	var req dto.FillMissingProfileRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	uid := userIDFromContext(r.Context())
	if uid == "" {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.users.GetUserByID(uid)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "user not found")
		return
	}

	// 1. Update Display Name if provided
	if strings.TrimSpace(req.DisplayName) != "" {
		_ = h.users.SetDisplayName(uid, strings.TrimSpace(req.DisplayName))
		u.DisplayName = strings.TrimSpace(req.DisplayName)
	}

	// 2. Update general user profile fields
	userUpdated := false
	if strings.TrimSpace(req.Name) != "" {
		u.Name = strings.TrimSpace(req.Name)
		userUpdated = true
	}
	if strings.TrimSpace(req.Email) != "" {
		u.Email = strings.TrimSpace(req.Email)
		userUpdated = true
	}
	if strings.TrimSpace(req.AvatarURL) != "" {
		u.AvatarURL = strings.TrimSpace(req.AvatarURL)
		userUpdated = true
	}
	if strings.TrimSpace(req.County) != "" {
		u.County = strings.TrimSpace(req.County)
		userUpdated = true
	}
	if strings.TrimSpace(req.Town) != "" {
		u.Town = strings.TrimSpace(req.Town)
		userUpdated = true
	}
	if req.Location != nil {
		u.Location = &models.Coordinates{Lat: req.Location.Lat, Lng: req.Location.Lng}
		userUpdated = true
	}
	if userUpdated {
		_ = h.users.UpdateUser(u)
	}

	// 3. Technician fields
	isTech := u.Role == models.RoleTechnician || u.Role == models.RoleBoth
	if isTech && h.db != nil {
		p := &models.TechnicianProfile{
			TechnicianID: uid,
			County:       u.County,
			Town:         u.Town,
			Village:      strings.TrimSpace(req.Village),
			IsAvailable:  true,
		}
		if req.IsAvailable != nil {
			p.IsAvailable = *req.IsAvailable
		}
		if req.PricePerHour != nil {
			p.PricePerHour = *req.PricePerHour
		}
		if req.ExperienceYears != nil {
			p.ExperienceYears = *req.ExperienceYears
		}
		if strings.TrimSpace(req.Bio) != "" {
			p.Bio = strings.TrimSpace(req.Bio)
		}
		p.Skills = req.Skills

		_, _ = h.db.ExecContext(r.Context(), `
			INSERT INTO technician_profiles(technician_id, bio, experience_years, price_per_hour, is_available, county, town, village, updated_at)
			VALUES($1, $2, $3, $4, $5, $6, $7, $8, now())
			ON CONFLICT(technician_id) DO UPDATE SET
				bio = CASE WHEN $2 <> '' THEN $2 ELSE technician_profiles.bio END,
				experience_years = CASE WHEN $3 > 0 THEN $3 ELSE technician_profiles.experience_years END,
				price_per_hour = CASE WHEN $4 > 0 THEN $4 ELSE technician_profiles.price_per_hour END,
				is_available = $5,
				county = CASE WHEN $6 <> '' THEN $6 ELSE technician_profiles.county END,
				town = CASE WHEN $7 <> '' THEN $7 ELSE technician_profiles.town END,
				village = CASE WHEN $8 <> '' THEN $8 ELSE technician_profiles.village END,
				updated_at = now()
		`, uid, p.Bio, p.ExperienceYears, p.PricePerHour, p.IsAvailable, p.County, p.Town, p.Village)

		if len(req.Skills) > 0 {
			for _, skill := range req.Skills {
				if strings.TrimSpace(skill) != "" {
					_, _ = h.db.ExecContext(r.Context(), `INSERT INTO technician_profile_skills(technician_id, skill) VALUES($1, $2) ON CONFLICT DO NOTHING`, uid, strings.TrimSpace(skill))
					_, _ = h.db.ExecContext(r.Context(), `INSERT INTO technician_skills(technician_id, skill) SELECT id, $2 FROM technicians WHERE id=$1 OR user_id=$1 ON CONFLICT DO NOTHING`, uid, strings.TrimSpace(skill))
				}
			}
		}
		if req.Location != nil {
			_, _ = h.db.ExecContext(r.Context(), `UPDATE technicians SET lat=$1, lon=$2, updated_at=now() WHERE id=$3 OR user_id=$3`, req.Location.Lat, req.Location.Lng, uid)
		}
	}

	// 4. ISP fields
	isISP := u.Role == models.RoleISP || u.Role == models.RoleBoth
	if isISP && h.db != nil {
		logo := strings.TrimSpace(req.LogoURL)
		care := strings.TrimSpace(req.CustomerCareNumber)
		desc := strings.TrimSpace(req.Description)
		_, _ = h.db.ExecContext(r.Context(), `
			INSERT INTO isps(id, name, logo_url, customer_care_number, description, county, created_at, updated_at)
			VALUES($1, $2, $3, $4, $5, $6, now(), now())
			ON CONFLICT(id) DO UPDATE SET
				logo_url = CASE WHEN $3 <> '' THEN $3 ELSE isps.logo_url END,
				customer_care_number = CASE WHEN $4 <> '' THEN $4 ELSE isps.customer_care_number END,
				description = CASE WHEN $5 <> '' THEN $5 ELSE isps.description END,
				county = CASE WHEN $6 <> '' THEN $6 ELSE isps.county END,
				updated_at = now()
		`, uid, u.Name, logo, care, desc, u.County)
	}

	// 5. Identity Verification Submission if ID photo provided
	if strings.TrimSpace(req.IDPhotoURL) != "" && h.db != nil {
		docType := strings.ToLower(strings.TrimSpace(req.DocumentType))
		if docType == "" {
			docType = "national_id"
		}
		verificationID := uuid.NewString()
		_, _ = h.db.ExecContext(r.Context(), `
			INSERT INTO identity_verifications(id, user_id, phone, id_photo_url, id_photo_back_url, id_number, document_type, status, verification_badge, notes, submitted_at, created_at, updated_at)
			VALUES($1, $2, $3, $4, $5, $6, $7, 'pending', 'unverified', $8, now(), now(), now())
			ON CONFLICT(id) DO UPDATE SET
				id_photo_url = EXCLUDED.id_photo_url,
				id_photo_back_url = EXCLUDED.id_photo_back_url,
				id_number = EXCLUDED.id_number,
				status = 'pending',
				updated_at = now()
		`, verificationID, uid, u.Phone, strings.TrimSpace(req.IDPhotoURL), strings.TrimSpace(req.IDPhotoBackURL), strings.TrimSpace(req.IDNumber), docType, strings.TrimSpace(req.VerificationNotes))

		_, _ = h.db.ExecContext(r.Context(), `UPDATE users SET identity_status='pending', updated_at=now() WHERE id=$1`, uid)
		u.IdentityStatus = "pending"
	}

	// Reload fresh user object
	refreshedUser, err := h.users.GetUserByID(uid)
	if err == nil {
		u = refreshedUser
	}

	report := h.computeCompletionReport(r.Context(), u)
	recordAudit(r, "user.profile_fill_missing", "UPDATE", "user", uid,
		fmt.Sprintf("User completed missing profile fields (completion: %d%%)", report.CompletionPercentage), true,
		map[string]interface{}{
			"completion_percentage": report.CompletionPercentage,
			"filled_fields":         report.FilledFields,
			"missing_fields_count":  len(report.MissingFields),
		})

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Message: "profile information updated successfully",
		Data: map[string]interface{}{
			"user":              u,
			"completion_report": report,
		},
	})
}
func (h *ClientHandler) RequestDeleteProfile(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	if err := h.users.RequestDeleteUser(uid, "deletion_requested"); err != nil {
		respondWithError(w, 500, "failed to request deletion")
		return
	}
	recordAudit(r, "user.delete_request", "REQUEST", "user", uid, "User requested account deletion under KDPA 2019", true, nil)
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "profile deletion requested"})
}
func (h *ClientHandler) GetInstallations(w http.ResponseWriter, r *http.Request) {
	items, err := h.installations.GetInstallationsByClientID(userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, 500, "failed to get installations")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}

func (h *ClientHandler) CreateInstallation(w http.ResponseWriter, r *http.Request) {
	var req dto.JobRequestRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	var date *time.Time
	if req.PreferredDate != "" {
		parsed, err := time.Parse(time.RFC3339, req.PreferredDate)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "preferred_date must be RFC3339")
			return
		}
		date = &parsed
	}
	job, err := h.jobs.Create(userIDFromContext(r.Context()), &models.JobRequest{RequestType: "internet", Mode: req.Mode, TargetISPID: req.TargetISPID, TechnicianID: req.TargetTechnicianID, LocationID: req.LocationID, County: req.County, Town: req.Town, Village: req.Village, ServiceType: req.ServiceType, SpeedMbps: req.SpeedMbps, Description: req.Description, Budget: req.Budget, PreferredDate: date})
	if err != nil {
		recordAudit(r, "installation.requested", "REQUEST", "service_request", "", err.Error(), false, map[string]interface{}{"county": req.County, "service_type": req.ServiceType})
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	recordAudit(r, "installation.requested", "REQUEST", "service_request", job.ID, "installation requested", true, map[string]interface{}{"county": req.County, "service_type": req.ServiceType})
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Data: job})
}

func (h *ClientHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var req dto.JobRequestRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid request payload")
		return
	}
	var date *time.Time
	if req.PreferredDate != "" {
		parsed, err := time.Parse(time.RFC3339, req.PreferredDate)
		if err != nil {
			respondWithError(w, 400, "preferred_date must be RFC3339")
			return
		}
		date = &parsed
	}
	requestType := strings.TrimSpace(req.RequestType)
	if requestType == "" {
		requestType = "technician_service"
	}
	job, err := h.jobs.Create(userIDFromContext(r.Context()), &models.JobRequest{RequestType: requestType, Mode: req.Mode, TargetISPID: req.TargetISPID, TechnicianID: req.TargetTechnicianID, LocationID: req.LocationID, County: req.County, Town: req.Town, Village: req.Village, ServiceType: req.ServiceType, SpeedMbps: req.SpeedMbps, Description: req.Description, Budget: req.Budget, PreferredDate: date})
	if err != nil {
		recordAudit(r, "customer_request.created", "CREATE", "service_request", "", err.Error(), false, map[string]interface{}{"request_type": requestType})
		respondWithError(w, 400, err.Error())
		return
	}
	recordAudit(r, "customer_request.created", "CREATE", "service_request", job.ID, "customer service request created", true, map[string]interface{}{"request_type": requestType})
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: job})
}
func (h *ClientHandler) GetJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.jobs.ListForCustomer(userIDFromContext(r.Context()), strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		respondWithError(w, 500, "failed to list jobs")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: jobs})
}
func (h *ClientHandler) GetJobApplications(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/jobs/")
	apps, err := h.jobs.Applications(userIDFromContext(r.Context()), id)
	if err != nil {
		respondWithError(w, 404, "job not found")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: apps})
}
func (h *ClientHandler) AssignJob(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/jobs/")
	var req dto.JobAssignmentRequest
	if decodeJSON(w, r, &req) != nil || req.ApplicationID == "" {
		respondWithError(w, 400, "application_id is required")
		return
	}
	job, err := h.jobs.Assign(userIDFromContext(r.Context()), id, req.ApplicationID)
	if err != nil {
		respondWithError(w, 409, "job is no longer available")
		return
	}
	recordAudit(r, "job.assign", "UPDATE", "job", id, "Client assigned job "+id+" to technician application "+req.ApplicationID, true, map[string]interface{}{"job_id": id, "application_id": req.ApplicationID})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: job})
}
func (h *ClientHandler) SetJobAvailability(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/jobs/")
	var req dto.JobAvailabilityRequest
	if decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid request payload")
		return
	}
	if err := h.jobs.Availability(userIDFromContext(r.Context()), id, req.Available); err != nil {
		respondWithError(w, 404, "job not found")
		return
	}
	recordAudit(r, "job.availability", "UPDATE", "job", id, fmt.Sprintf("Client set job availability to %v", req.Available), true, map[string]interface{}{"job_id": id, "available": req.Available})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "job availability updated"})
}
func (h *ClientHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/jobs/")
	if err := h.jobs.Delete(userIDFromContext(r.Context()), id); err != nil {
		respondWithError(w, 404, "job not found")
		return
	}
	recordAudit(r, "job.delete", "DELETE", "job", id, "Client deleted job "+id, true, map[string]interface{}{"job_id": id})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "job deleted"})
}

func (h *ClientHandler) TerminateJob(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/my/jobs/")
	var req dto.JobLifecycleRequest
	if id == "" || decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid lifecycle request")
		return
	}
	at := time.Now().UTC()
	if req.EndsAt != "" {
		v, err := time.Parse(time.RFC3339, req.EndsAt)
		if err != nil {
			respondWithError(w, 400, "ends_at must be RFC3339")
			return
		}
		at = v
	}
	if err := h.jobs.Terminate(userIDFromContext(r.Context()), id, req.Reason, at); err != nil {
		respondWithError(w, 409, "job could not be terminated")
		return
	}
	recordAudit(r, "job.terminate", "UPDATE", "job", id, "Job terminated: "+req.Reason, true, map[string]interface{}{"job_id": id, "reason": req.Reason})
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "job terminated"})
}
func (h *ClientHandler) ReservePackage(w http.ResponseWriter, r *http.Request) {
	var req dto.PackageSubscriptionRequest
	if decodeJSON(w, r, &req) != nil || strings.TrimSpace(req.PackageID) == "" {
		respondWithError(w, 400, "package_id is required")
		return
	}
	reservationID, err := h.packages.ReservePackage(req.PackageID, userIDFromContext(r.Context()), req.County)
	if err != nil {
		respondWithError(w, 409, err.Error())
		return
	}
	recordAudit(r, "package.reserve", "CREATE", "package_reservation", reservationID, "Client reserved ISP package "+req.PackageID, true, map[string]interface{}{"package_id": req.PackageID, "reservation_id": reservationID})
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: map[string]interface{}{"reservation_id": reservationID, "expires_in": 900}})
}
func (h *ClientHandler) SubscribePackage(w http.ResponseWriter, r *http.Request) {
	reservationID := strings.TrimSuffix(pathParam(r.URL.Path, "/api/v1/package-reservations/"), "/subscribe")
	if reservationID == "" {
		respondWithError(w, 400, "reservation id is required")
		return
	}
	subscription, err := h.packages.Subscribe(reservationID, userIDFromContext(r.Context()))
	if err != nil {
		respondWithError(w, 409, "reservation is unavailable or expired")
		return
	}
	recordAudit(r, "package.subscribe", "CREATE", "package_subscription", subscription.ID, "Client subscribed to ISP package from reservation "+reservationID, true, map[string]interface{}{"subscription_id": subscription.ID})
	respondWithJSON(w, 201, dto.APIResponse{Success: true, Data: subscription})
}
func (h *ClientHandler) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	items, err := h.packages.ListSubscriptions(userIDFromContext(r.Context()), "customer", r.URL.Query().Get("status"), intQuery(r.URL.Query().Get("limit")))
	if err != nil {
		respondWithError(w, 500, "failed to list subscriptions")
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Data: items})
}
func (h *ClientHandler) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/subscriptions/")
	var req dto.PackageSubscriptionStatusRequest
	if id == "" || decodeJSON(w, r, &req) != nil {
		respondWithError(w, 400, "invalid subscription update")
		return
	}
	var endsAt *time.Time
	if req.EndsAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.EndsAt)
		if err != nil {
			respondWithError(w, 400, "ends_at must be RFC3339")
			return
		}
		endsAt = &parsed
	}
	if err := h.packages.UpdateSubscription(id, userIDFromContext(r.Context()), req.Status, endsAt); err != nil {
		respondWithError(w, 400, err.Error())
		return
	}
	respondWithJSON(w, 200, dto.APIResponse{Success: true, Message: "subscription updated"})
}

func (h *ClientHandler) SetSubscriptionAutoRenew(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r.URL.Path, "/api/v1/subscriptions/")
	var req dto.PackageAutoRenewRequest
	if id == "" || decodeJSON(w, r, &req) != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid auto-renew request")
		return
	}
	if err := h.packages.SetAutoRenew(id, userIDFromContext(r.Context()), req.AutoRenew); err != nil {
		respondWithErrorRaw(w, http.StatusNotFound, "subscription not found")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "auto-renew preference updated"})
}
