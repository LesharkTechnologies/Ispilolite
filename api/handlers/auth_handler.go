package handlers

import (
	"errors"
	"net/http"
	"strings"

	"ispilolite/api/dto"
	"ispilolite/internal/middleware"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/repository/redis"
	authsvc "ispilolite/internal/services/auth"
)

type AuthHandler struct{ authService *authsvc.AuthService }

func NewAuthHandler() (*AuthHandler, error) {
	userRepo, err := postgres.NewUserRepo()
	if err != nil {
		return nil, err
	}
	cacheRepo, err := redis.NewCacheRepo()
	if err != nil {
		return nil, err
	}
	return &AuthHandler{authService: authsvc.NewAuthService(userRepo, cacheRepo)}, nil
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req dto.RegisterRequest
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		recordAudit(r, "user.register", "REGISTER", "user", "", "invalid registration request", false, map[string]interface{}{"phone": req.Phone})
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	if err := authsvc.ValidatePhone(req.Phone); err != nil {
		recordAudit(r, "user.register", "REGISTER", "user", "", "invalid phone", false, map[string]interface{}{"phone": req.Phone})
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Public registration allows customer, technician, or isp role. Every
	// account uses a password; the SMS code verifies ownership of the phone and
	// is not a passwordless sign-in method.
	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role == "" || (role != "customer" && role != "technician" && role != "isp") {
		role = "customer"
	}
	req.Role = role
	req.Username = strings.TrimSpace(req.Username)
	req.County = strings.TrimSpace(req.County)
	req.Town = strings.TrimSpace(req.Town)
	if len(req.County) > 80 || len(req.Town) > 100 {
		respondWithError(w, http.StatusBadRequest, "county or town is too long")
		return
	}
	if len(req.Password) < 8 {
		respondWithError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	user, err := h.authService.CreateUser(req)
	if err != nil {
		recordAudit(r, "user.register", "REGISTER", "user", "", err.Error(), false, map[string]interface{}{"phone": req.Phone})
		status := http.StatusInternalServerError
		if errors.Is(err, authsvc.ErrUserAlreadyExists) {
			status = http.StatusConflict
		}
		if errors.Is(err, authsvc.ErrInvalidReferral) {
			status = http.StatusBadRequest
		}
		if status == http.StatusInternalServerError {
			respondWithInternalError(w, err)
		} else {
			respondWithError(w, status, err.Error())
		}
		return
	}
	_, expiresIn, err := h.authService.IssueOTPFromIP(user.ID, middleware.ClientIP(r))
	if err != nil {
		recordAudit(r, "user.register", "REGISTER", "user", user.ID, "registration OTP delivery failed", false, nil)
		if errors.Is(err, authsvc.ErrRateLimited) {
			w.Header().Set("Retry-After", "30")
			respondWithError(w, http.StatusTooManyRequests, "please wait before requesting another verification code")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to send verification code")
		return
	}
	recordAudit(r, "user.register", "REGISTER", "user", user.ID, "user registered and verification code sent", true, nil)
	respondWithJSON(w, http.StatusCreated, dto.APIResponse{Success: true, Message: "verification code sent", Data: map[string]any{"user_id": user.ID, "expires_in": expiresIn}})
}

// NormalizePhone standardizes Kenyan mobile numbers to E.164 (+254...).
func NormalizePhone(phone string) string {
	clean := strings.TrimSpace(phone)
	clean = strings.ReplaceAll(clean, " ", "")
	clean = strings.ReplaceAll(clean, "-", "")
	clean = strings.ReplaceAll(clean, "(", "")
	clean = strings.ReplaceAll(clean, ")", "")
	if strings.HasPrefix(clean, "00") {
		clean = clean[2:]
	}
	if strings.HasPrefix(clean, "0") && len(clean) == 10 {
		clean = "+254" + clean[1:]
	} else if strings.HasPrefix(clean, "254") && len(clean) == 12 {
		clean = "+" + clean
	} else if (strings.HasPrefix(clean, "7") || strings.HasPrefix(clean, "1")) && len(clean) == 9 {
		clean = "+254" + clean
	}
	return clean
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	phone := strings.TrimSpace(req.Phone)
	username := strings.TrimSpace(req.Username)
	rawIdentifier := strings.TrimSpace(req.Identifier)
	password := req.Password

	// If identifier is provided, resolve whether it is a phone or username.
	if rawIdentifier != "" {
		if phone == "" && username == "" {
			normalized := NormalizePhone(rawIdentifier)
			if authsvc.ValidatePhone(normalized) == nil {
				phone = normalized
			} else {
				username = rawIdentifier
			}
		}
	}

	if phone != "" {
		phone = NormalizePhone(phone)
	} else if username != "" {
		// If username looks like a phone number, normalize and check
		normalized := NormalizePhone(username)
		if authsvc.ValidatePhone(normalized) == nil {
			phone = normalized
			username = ""
		}
	}

	if phone == "" && username == "" {
		respondWithError(w, http.StatusBadRequest, "provide phone number or username")
		return
	}
	if password == "" {
		respondWithError(w, http.StatusBadRequest, "password is required")
		return
	}
	if phone != "" {
		if err := authsvc.ValidatePhone(phone); err != nil {
			recordAudit(r, "user.login", "LOGIN", "user", "", "invalid phone", false, map[string]interface{}{"phone": phone})
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var user *models.User
	var err error
	identifier := username
	if phone != "" {
		identifier = phone
		user, err = h.authService.AuthenticateWithPassword(phone, password)
	} else {
		user, err = h.authService.AuthenticateWithUsername(username, password)
	}
	if err != nil {
		recordAudit(r, "user.login", "LOGIN", "user", "", "password login failed", false, map[string]interface{}{"identifier": identifier})
		if errors.Is(err, authsvc.ErrAccountUnverified) {
			respondWithError(w, http.StatusForbidden, "verify your phone number before logging in")
			return
		}
		respondWithError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	h.respondWithTokens(w, user)
	recordAuditWithActor(r, user.ID, user.Role, "user.login", "LOGIN", "user", user.ID, user.Role+" user logged in successfully", true, map[string]interface{}{"identifier": identifier, "role": user.Role})
}

func (h *AuthHandler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req dto.VerifyOTPRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	user, err := h.authService.VerifyOTP(req.UserID, req.OTP)
	if err != nil {
		recordAuditWithActor(r, req.UserID, "customer", "user.verify_otp", "LOGIN", "user", req.UserID, "OTP verification failed", false, nil)
		if errors.Is(err, authsvc.ErrRateLimited) {
			w.Header().Set("Retry-After", "900")
			respondWithError(w, http.StatusTooManyRequests, "too many verification attempts; request a new code later")
			return
		}
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}
	h.respondWithTokens(w, user)
	recordAuditWithActor(r, user.ID, user.Role, "user.verify_otp", "LOGIN", "user", user.ID, user.Role+" user verified OTP and signed in", true, map[string]interface{}{"phone": user.Phone, "role": user.Role})
}

func (h *AuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req dto.RefreshTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	accessToken, refreshToken, expiresIn, err := h.authService.RefreshAccessToken(req.RefreshToken)
	if err != nil {
		recordAudit(r, "user.refresh_token", "LOGIN", "token", "", "access token refresh failed", false, nil)
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}
	recordAudit(r, "user.refresh_token", "LOGIN", "token", "", "access token refreshed successfully", true, nil)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"access_token": accessToken, "refresh_token": refreshToken, "token_type": "Bearer", "expires_in": expiresIn}})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if token, err := bearerTokenFromRequest(r); err == nil {
		h.authService.Logout(token)
	}
	recordAudit(r, "user.logout", "LOGIN", "user", userIDFromContext(r.Context()), "user logged out successfully", true, nil)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "logged out successfully"})
}

func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req dto.ForgotPasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	identifier := strings.TrimSpace(req.Phone)
	if identifier == "" {
		identifier = strings.TrimSpace(req.Username)
	}
	if identifier == "" {
		respondWithError(w, http.StatusBadRequest, "phone number or username is required")
		return
	}

	user, expiresIn, err := h.authService.ForgotPassword(identifier, middleware.ClientIP(r))
	if err != nil {
		recordAudit(r, "user.forgot_password", "PASSWORD_RESET", "user", "", "forgot password request failed for "+identifier, false, map[string]interface{}{"identifier": identifier})
		if errors.Is(err, authsvc.ErrRateLimited) {
			w.Header().Set("Retry-After", "30")
			respondWithError(w, http.StatusTooManyRequests, "please wait before requesting another reset code")
			return
		}
		respondWithError(w, http.StatusNotFound, "account not found")
		return
	}

	recordAuditWithActor(r, user.ID, user.Role, "user.forgot_password", "PASSWORD_RESET", "user", user.ID, "password reset code dispatched to "+user.Phone, true, nil)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Message: "password reset code sent to your phone",
		Data: map[string]any{
			"user_id":    user.ID,
			"phone":      user.Phone,
			"expires_in": expiresIn,
		},
	})
}

func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req dto.ResetPasswordRequest
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.OTP) == "" || len(strings.TrimSpace(req.NewPassword)) < 8 {
		respondWithError(w, http.StatusBadRequest, "a 6-digit OTP and an 8-character new password are required")
		return
	}

	user, err := h.authService.ResetPasswordWithOTP(req.UserID, req.Phone, req.OTP, req.NewPassword)
	if err != nil {
		recordAudit(r, "user.reset_password", "PASSWORD_RESET", "user", req.UserID, "password reset failed: "+err.Error(), false, nil)
		if errors.Is(err, authsvc.ErrRateLimited) {
			w.Header().Set("Retry-After", "900")
			respondWithError(w, http.StatusTooManyRequests, "too many verification attempts; request a new code later")
			return
		}
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	recordAuditWithActor(r, user.ID, user.Role, "user.reset_password", "PASSWORD_RESET", "user", user.ID, "password successfully reset", true, nil)
	h.respondWithTokens(w, user)
}

func (h *AuthHandler) respondWithTokens(w http.ResponseWriter, user *models.User) {
	accessToken, expiresIn, err := h.authService.GenerateAccessToken(user)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to create access token")
		return
	}
	refreshToken, err := h.authService.GenerateRefreshToken(user)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to create refresh token")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data: map[string]any{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"token_type":    "Bearer",
			"expires_in":    expiresIn,
			"role":          user.Role,
			"user":          user,
		},
	})
}

func bearerTokenFromRequest(r *http.Request) (string, error) {
	const prefix = "Bearer "
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, prefix) {
		return "", errors.New("invalid authorization header")
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", errors.New("missing bearer token")
	}
	return token, nil
}
