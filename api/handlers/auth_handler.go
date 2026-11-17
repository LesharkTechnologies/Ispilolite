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
	// Public registration is customer-only. ISP, technician, and admin
	// accounts must be provisioned by an authorized internal/admin workflow.
	req.Role = "customer"
	req.Username = ""
	req.Password = ""
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

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	phone := strings.TrimSpace(req.Phone)
	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	if phone == "" && username == "" {
		respondWithError(w, http.StatusBadRequest, "phone is required for customers; username is required for staff")
		return
	}
	if phone != "" {
		if err := authsvc.ValidatePhone(phone); err != nil {
			recordAudit(r, "user.login", "LOGIN", "user", "", "invalid phone", false, map[string]interface{}{"phone": phone})
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if username != "" {
		if phone != "" || password == "" {
			recordAudit(r, "user.login", "LOGIN", "user", "", "invalid staff login shape", false, map[string]interface{}{"username": username})
			respondWithError(w, http.StatusBadRequest, "staff login requires username and password only")
			return
		}
		var user *models.User
		var err error
		user, err = h.authService.AuthenticateWithUsername(username, password)
		if err != nil {
			recordAuditWithActor(r, "", "staff", "user.login", "LOGIN", "user", "", "Password login failed for username "+username, false, map[string]interface{}{"phone": req.Phone, "username": req.Username})
			if errors.Is(err, authsvc.ErrAccountUnverified) {
				respondWithError(w, http.StatusForbidden, "verify your phone number before logging in")
				return
			}
			respondWithError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		h.respondWithTokens(w, user)
		recordAuditWithActor(r, user.ID, user.Role, "user.login", "LOGIN", "user", user.ID, user.Role+" user "+user.Username+" logged in successfully", true, map[string]interface{}{"username": user.Username, "role": user.Role})
		return
	}
	if password != "" {
		recordAudit(r, "user.login", "LOGIN", "user", "", "customer password login rejected", false, map[string]interface{}{"phone": phone})
		respondWithError(w, http.StatusBadRequest, "customers sign in with phone OTP; staff sign in with username and password")
		return
	}
	user, expiresIn, err := h.authService.RequestLoginOTPFromIP(phone, middleware.ClientIP(r))
	if err != nil {
		recordAudit(r, "user.login", "LOGIN", "user", "", "OTP login request failed", false, map[string]interface{}{"phone": phone})
		if errors.Is(err, authsvc.ErrRateLimited) {
			w.Header().Set("Retry-After", "30")
			respondWithError(w, http.StatusTooManyRequests, "please wait before requesting another verification code")
			return
		}
		respondWithError(w, http.StatusNotFound, err.Error())
		return
	}
	recordAuditWithActor(r, user.ID, user.Role, "user.login_otp_requested", "LOGIN", "user", user.ID, "login verification code sent to "+phone, true, map[string]interface{}{"phone": phone})
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "OTP sent to your phone", Data: map[string]any{"user_id": user.ID, "expires_in": expiresIn}})
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
	accessToken, expiresIn, err := h.authService.RefreshAccessToken(req.RefreshToken)
	if err != nil {
		recordAudit(r, "user.refresh_token", "LOGIN", "token", "", "access token refresh failed", false, nil)
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}
	recordAudit(r, "user.refresh_token", "LOGIN", "token", "", "access token refreshed successfully", true, nil)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"access_token": accessToken, "token_type": "Bearer", "expires_in": expiresIn}})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if token, err := bearerTokenFromRequest(r); err == nil {
		h.authService.Logout(token)
	}
	recordAudit(r, "user.logout", "LOGIN", "user", userIDFromContext(r.Context()), "user logged out successfully", true, nil)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Message: "logged out successfully"})
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
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{"access_token": accessToken, "refresh_token": refreshToken, "token_type": "Bearer", "expires_in": expiresIn, "user": user}})
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
