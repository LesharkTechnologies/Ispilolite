package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	dto "ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository"
	"ispilolite/internal/services/notification"
	"ispilolite/internal/utils"

	"github.com/lib/pq"
)

// e164PhoneRE validates E.164 international phone numbers (e.g. +254712345678).
var e164PhoneRE = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// ValidatePhone returns an error if the phone number is not a valid E.164 number.
func ValidatePhone(phone string) error {
	clean := strings.TrimSpace(phone)
	if !e164PhoneRE.MatchString(clean) {
		return errors.New("invalid phone number: must be in E.164 format (e.g. +254712345678)")
	}
	return nil
}

var (
	ErrUserAlreadyExists  = errors.New("user already exists")
	ErrPhoneNotRegistered = errors.New("phone not registered")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidOTP         = errors.New("invalid OTP")
	ErrOTPExpired         = errors.New("OTP expired")
	ErrInvalidToken       = errors.New("invalid token")
	ErrInvalidPassword    = errors.New("invalid credentials")
	ErrAccountUnverified  = errors.New("account is not verified")
	ErrTokenRevoked       = errors.New("token revoked")
	ErrRateLimited        = errors.New("too many attempts, try again later")
	ErrInvalidReferral    = errors.New("invalid referral code")
)

// MaxOTPAttemptsPerWindow is the maximum number of OTP requests per phone
// within the OTP attempt window.
const MaxOTPAttemptsPerWindow = 3

// OTPAttemptWindow is the time window for OTP attempt rate limiting.
const OTPAttemptWindow = 15 * time.Minute

// OTPResendCooldown prevents repeated taps from generating and sending a new
// code before the previous SMS has had time to arrive.
const OTPResendCooldown = 30 * time.Second

// MaxOTPRequestsPerIP limits unauthenticated OTP abuse from one source IP.
const MaxOTPRequestsPerIP = 10

// MaxOTPVerificationAttempts limits guessing of one active code.
const MaxOTPVerificationAttempts = 5

// AuthService handles authentication logic.
type AuthService struct {
	userRepo   repository.UserRepository
	cacheRepo  repository.CacheRepository
	notifier   notification.Sender
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
	otpTTL     time.Duration
}

// NewAuthService creates a new AuthService.
func NewAuthService(userRepo repository.UserRepository, cacheRepo repository.CacheRepository) *AuthService {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "ispilolite-dev-secret"
	}

	issuer := os.Getenv("JWT_ISSUER")
	if issuer == "" {
		issuer = "ispilolite"
	}

	return &AuthService{
		userRepo:   userRepo,
		cacheRepo:  cacheRepo,
		notifier:   notification.NewSenderFromEnv(),
		secret:     []byte(secret),
		issuer:     issuer,
		accessTTL:  authTTL("JWT_ACCESS_TTL", 15*time.Minute),
		refreshTTL: authTTL("JWT_REFRESH_TTL", 7*24*time.Hour),
		otpTTL:     authTTL("OTP_TTL", 10*time.Minute),
	}
}

func authTTL(key string, fallback time.Duration) time.Duration {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

// CreateUser registers a new user.
func (s *AuthService) CreateUser(req dto.RegisterRequest) (*models.User, error) {
	phone := strings.TrimSpace(req.Phone)
	if _, err := s.userRepo.GetUserByPhone(phone); err == nil {
		return nil, ErrUserAlreadyExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check phone uniqueness: %w", err)
	}
	if username := strings.TrimSpace(req.Username); username != "" {
		if _, err := s.userRepo.GetUserByUsername(username); err == nil {
			return nil, ErrUserAlreadyExists
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("check username uniqueness: %w", err)
		}
	}

	passwordHash := ""
	if normalizeRole(req.Role) != "customer" {
		var err error
		passwordHash, err = utils.HashPassword(req.Password)
		if err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	referralCode, err := generateReferralCode()
	if err != nil {
		return nil, err
	}
	user := &models.User{
		ID:             utils.GenerateID(),
		Phone:          phone,
		Username:       strings.TrimSpace(req.Username),
		Name:           strings.TrimSpace(req.Name),
		Email:          strings.TrimSpace(req.Email),
		Role:           normalizeRole(req.Role),
		PasswordHash:   passwordHash,
		IsVerified:     false,
		Rating:         0,
		TotalRatings:   0,
		Status:         "active",
		Joined:         now,
		CreatedAt:      now,
		UpdatedAt:      now,
		ReferralCode:   referralCode,
		ReferredByCode: strings.ToUpper(strings.TrimSpace(req.ReferralCode)),
		Locale:         normalizeLocale(req.Locale),
	}

	if err := s.userRepo.CreateUser(user); err != nil {
		// The pre-checks provide a useful response for normal requests; unique
		// indexes remain authoritative when two registrations race.
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrUserAlreadyExists
		}
		if errors.Is(err, sql.ErrNoRows) && user.ReferredByCode != "" {
			return nil, ErrInvalidReferral
		}
		return nil, err
	}

	return user, nil
}

// AuthenticateWithPassword verifies a phone/password pair and returns the user.
// It performs a constant-time password comparison and does not disclose whether
// the phone or the password was the failing factor.
func (s *AuthService) AuthenticateWithPassword(phone, password string) (*models.User, error) {
	user, err := s.userRepo.GetUserByPhone(strings.TrimSpace(phone))
	if err != nil {
		return nil, ErrInvalidPassword
	}
	if user.Status != "active" {
		return nil, ErrInvalidPassword
	}

	ok, err := utils.VerifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		return nil, ErrInvalidPassword
	}
	if user.Role == "customer" {
		return nil, ErrInvalidPassword
	}
	if !user.IsVerified {
		return nil, ErrAccountUnverified
	}

	return user, nil
}

func (s *AuthService) AuthenticateWithUsername(username, password string) (*models.User, error) {
	username = strings.TrimSpace(username)
	user, err := s.userRepo.GetUserByUsername(username)
	if err != nil {
		return nil, ErrInvalidPassword
	}
	if user.Status != "active" {
		return nil, ErrInvalidPassword
	}
	if user.Role == "customer" {
		return nil, ErrInvalidPassword
	}
	ok, err := utils.VerifyPassword(password, user.PasswordHash)
	if !ok {
		envAdminUser := strings.TrimSpace(os.Getenv("ADMIN_USERNAME"))
		envAdminPass := strings.TrimSpace(os.Getenv("ADMIN_PASSWORD"))
		if user.Role == "admin" && envAdminUser != "" && strings.EqualFold(user.Username, envAdminUser) && password == envAdminPass {
			ok = true
			err = nil
		}
	}
	if err != nil || !ok {
		return nil, ErrInvalidPassword
	}
	if !user.IsVerified {
		return nil, ErrAccountUnverified
	}
	return user, nil
}

// GetUserByPhone fetches a user by phone number.
func (s *AuthService) GetUserByPhone(phone string) (*models.User, error) {
	return s.userRepo.GetUserByPhone(strings.TrimSpace(phone))
}

// GetUserByID fetches a user by ID.
func (s *AuthService) GetUserByID(userID string) (*models.User, error) {
	return s.userRepo.GetUserByID(userID)
}

// IssueOTP creates, stores, and delivers an OTP for a user.
func (s *AuthService) IssueOTP(userID string) (string, int, error) {
	return s.IssueOTPFromIP(userID, "")
}

// IssueOTPFromIP creates, stores, and delivers an OTP while applying both
// account and source-IP abuse controls. The IP is used only as a Redis key and
// is not persisted by the authentication service.
func (s *AuthService) IssueOTPFromIP(userID, sourceIP string) (string, int, error) {
	user, err := s.userRepo.GetUserByID(userID)
	if err != nil {
		return "", 0, ErrUserNotFound
	}
	if user.Status != "active" {
		return "", 0, ErrUserNotFound
	}

	// A successful cooldown claim makes concurrent taps collapse into one SMS.
	if ok, err := s.cacheRepo.SetIfAbsent("otp_cooldown:"+user.Phone, 1, OTPResendCooldown); err != nil {
		return "", 0, fmt.Errorf("OTP rate limiter unavailable: %w", err)
	} else if !ok {
		return "", 0, ErrRateLimited
	}

	// Per-phone rate limiting to prevent SMS flooding and abuse.
	count, allowed := s.cacheRepo.IncrementOTPAttempts(user.Phone, MaxOTPAttemptsPerWindow, OTPAttemptWindow)
	if !allowed {
		return "", 0, fmt.Errorf("%w (attempt %d/%d in %v window)", ErrRateLimited, count, MaxOTPAttemptsPerWindow, OTPAttemptWindow)
	}
	if strings.TrimSpace(sourceIP) != "" {
		count, allowed = s.cacheRepo.IncrementOTPAttempts("ip:"+strings.TrimSpace(sourceIP), MaxOTPRequestsPerIP, OTPAttemptWindow)
		if !allowed {
			return "", 0, fmt.Errorf("%w (source limit)", ErrRateLimited)
		}
	}

	code, err := generateOTP()
	if err != nil {
		return "", 0, err
	}

	if err := s.cacheRepo.SetOTP(userID, code, s.otpTTL); err != nil {
		return "", 0, err
	}
	s.cacheRepo.ResetOTPAttempts("verify:" + userID)

	// Delivery failures must not leave a dangling OTP the caller can't receive.
	if s.notifier == nil {
		_ = s.cacheRepo.DeleteOTP(userID)
		return "", 0, fmt.Errorf("SMS sender is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	expiresMin := int(s.otpTTL.Minutes())
	if expiresMin <= 0 {
		expiresMin = 10
	}
	msg := fmt.Sprintf("Your Ispilo Lite verification code is %s. It expires in %d minutes.", code, expiresMin)
	if err := s.notifier.Send(ctx, notification.Message{To: user.Phone, Subject: "Verification code", Body: msg}); err != nil {
		_ = s.cacheRepo.DeleteOTP(userID)
		return "", 0, fmt.Errorf("failed to deliver OTP: %w", err)
	}

	return code, int(s.otpTTL.Seconds()), nil
}

// RequestLoginOTP validates phone and issues an OTP.
func (s *AuthService) RequestLoginOTP(phone string) (*models.User, int, error) {
	return s.RequestLoginOTPFromIP(phone, "")
}

func (s *AuthService) RequestLoginOTPFromIP(phone, sourceIP string) (*models.User, int, error) {
	user, err := s.userRepo.GetUserByPhone(strings.TrimSpace(phone))
	if err != nil {
		return nil, 0, ErrPhoneNotRegistered
	}
	if user.Status != "active" {
		return nil, 0, ErrPhoneNotRegistered
	}
	if !user.IsVerified {
		return nil, 0, ErrAccountUnverified
	}
	if user.Role != "customer" {
		return nil, 0, ErrInvalidPassword
	}

	_, expiresIn, err := s.IssueOTPFromIP(user.ID, sourceIP)
	if err != nil {
		return nil, 0, err
	}

	return user, expiresIn, nil
}

// VerifyOTP checks the OTP and marks the user as verified.
func (s *AuthService) VerifyOTP(userID, otp string) (*models.User, error) {
	user, err := s.userRepo.GetUserByID(userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	if user.Status != "active" {
		return nil, ErrUserNotFound
	}
	_, allowed := s.cacheRepo.IncrementOTPAttempts("verify:"+userID, MaxOTPVerificationAttempts, s.otpTTL)
	if !allowed {
		return nil, fmt.Errorf("%w (verification limit)", ErrRateLimited)
	}

	storedOTP, err := s.cacheRepo.GetOTP(userID)
	if err != nil {
		return nil, ErrInvalidOTP
	}

	if strings.TrimSpace(otp) != storedOTP {
		return nil, ErrInvalidOTP
	}

	_ = s.cacheRepo.DeleteOTP(userID)
	// Reset OTP attempt counter after successful verification.
	s.cacheRepo.ResetOTPAttempts(user.Phone)

	user.IsVerified = true
	user.UpdatedAt = time.Now().UTC()
	if err := s.userRepo.UpdateUser(user); err != nil {
		return nil, err
	}
	if qualifier, ok := s.userRepo.(interface {
		QualifyReferral(string, float64) error
	}); ok {
		if err := qualifier.QualifyReferral(user.ID, 250); err != nil {
			return nil, err
		}
	}
	if refreshed, err := s.userRepo.GetUserByID(user.ID); err == nil {
		user = refreshed
	}

	return user, nil
}

func generateReferralCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return "ISP" + string(buf), nil
}
func normalizeLocale(value string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "sw") {
		return "sw"
	}
	return "en"
}

// GenerateAccessToken returns a signed access token.
func (s *AuthService) GenerateAccessToken(user *models.User) (string, int, error) {
	return s.generateToken(user, s.accessTTL, "access")
}

// GenerateRefreshToken returns a signed refresh token.
func (s *AuthService) GenerateRefreshToken(user *models.User) (string, error) {
	token, _, err := s.generateToken(user, s.refreshTTL, "refresh")
	if err != nil {
		return "", err
	}
	claims, err := s.parseToken(token)
	if err != nil {
		return "", err
	}
	if err := s.userRepo.CreateRefreshSession(claims.ID, user.ID, tokenHash(token), time.Unix(claims.Expires, 0)); err != nil {
		return "", err
	}
	return token, nil
}

// RefreshAccessToken swaps a valid refresh token for a new access token.
// The old refresh token is revoked (rotated) to prevent replay attacks:
// if a refresh token is stolen and used, the attacker and the legitimate
// user will both be invalidated on the next use.
func (s *AuthService) RefreshAccessToken(refreshToken string) (string, int, error) {
	claims, err := s.parseToken(refreshToken)
	if err != nil {
		return "", 0, err
	}

	if claims.TokenUse != "refresh" {
		return "", 0, ErrInvalidToken
	}

	// JTI replay protection: a refresh token must only be used once.
	jtiKey := "jti:" + claims.ID
	used, err := s.cacheRepo.Get(jtiKey)
	if err == nil && used != "" {
		// Token already used — possible theft. Revoke all sessions for this user.
		_ = s.userRepo.RevokeAllRefreshSessions(claims.UserID)
		return "", 0, ErrInvalidToken
	}

	active, err := s.userRepo.RefreshSessionActive(claims.ID, tokenHash(refreshToken))
	if err != nil || !active {
		return "", 0, ErrInvalidToken
	}

	user, err := s.GetUserByID(claims.UserID)
	if err != nil {
		return "", 0, err
	}
	if user.Status != "active" {
		return "", 0, ErrInvalidToken
	}

	// Mark this JTI as used (prevents replay) with a TTL equal to the token's
	// remaining lifetime.
	ttl := time.Until(time.Unix(claims.Expires, 0))
	if ttl > 0 {
		_ = s.cacheRepo.Set(jtiKey, "1", ttl)
	}

	// Revoke the old refresh token (rotation).
	_ = s.cacheRepo.SetRevokedToken(refreshToken, ttl)

	return s.GenerateAccessToken(user)
}

func tokenHash(token string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(token))))
}

// Logout revokes a token until its natural expiry.
func (s *AuthService) Logout(token string) {
	claims, err := s.parseToken(token)
	if err != nil {
		return
	}

	ttl := time.Until(time.Unix(claims.Expires, 0))
	if ttl <= 0 {
		// Already expired; nothing to revoke.
		return
	}

	_ = s.cacheRepo.SetRevokedToken(token, ttl)
	_ = s.userRepo.RevokeAllRefreshSessions(claims.UserID)
}

// RevokeAllSessions invalidates every refresh session for a user. Call this
// after a password change, account recovery, or security-sensitive logout.
func (s *AuthService) RevokeAllSessions(userID string) error {
	return s.userRepo.RevokeAllRefreshSessions(userID)
}

// IsTokenRevoked reports whether a token has been explicitly revoked (e.g. via logout).
func (s *AuthService) IsTokenRevoked(token string) bool {
	return s.cacheRepo.IsTokenRevoked(token)
}

// ValidateAccessToken parses an access token and rejects it if revoked or not an access token.
func (s *AuthService) ValidateAccessToken(token string) (*utils.TokenClaims, error) {
	claims, err := s.parseToken(token)
	if err != nil {
		return nil, err
	}

	if claims.TokenUse != "access" {
		return nil, ErrInvalidToken
	}

	if s.IsTokenRevoked(token) {
		return nil, ErrTokenRevoked
	}

	return claims, nil
}

func (s *AuthService) generateToken(user *models.User, ttl time.Duration, tokenUse string) (string, int, error) {
	if user == nil {
		return "", 0, ErrUserNotFound
	}

	now := time.Now().UTC()
	claims := utils.TokenClaims{
		UserID:    user.ID,
		Role:      user.Role,
		TokenUse:  tokenUse,
		Issuer:    s.issuer,
		Subject:   user.ID,
		Expires:   now.Add(ttl).Unix(),
		NotBefore: now.Unix(),
		IssuedAt:  now.Unix(),
		ID:        utils.GenerateID(),
	}

	signed, err := utils.SignToken(s.secret, claims)
	if err != nil {
		return "", 0, err
	}

	return signed, int(ttl.Seconds()), nil
}

func (s *AuthService) parseToken(raw string) (*utils.TokenClaims, error) {
	tokenString := strings.TrimSpace(raw)
	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	claims, err := utils.ParseToken(s.secret, tokenString)
	if err != nil {
		return nil, ErrInvalidToken
	}
	// Reject tokens minted for a different issuer (e.g. another service that
	// shares the same signing secret).
	if s.issuer != "" && claims.Issuer != s.issuer {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

func generateOTP() (string, error) {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	value := int(buf[0])<<16 | int(buf[1])<<8 | int(buf[2])
	return fmt.Sprintf("%06d", value%1000000), nil
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "customer":
		return "customer"
	case "client":
		return "customer"
	case "isp":
		return "isp"
	case "technician":
		return "technician"
	default:
		return strings.ToLower(strings.TrimSpace(role))
	}
}
