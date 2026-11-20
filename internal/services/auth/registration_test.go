package auth

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository"
	"ispilolite/internal/utils"
	otphash "ispilolite/pkg/otp"
)

type registrationUsers struct {
	repository.UserRepository
	created *models.User
}

func (r *registrationUsers) GetUserByPhone(string) (*models.User, error) {
	if r.created != nil {
		return r.created, nil
	}
	return nil, sql.ErrNoRows
}
func (r *registrationUsers) GetUserByUsername(string) (*models.User, error) {
	if r.created != nil {
		return r.created, nil
	}
	return nil, sql.ErrNoRows
}
func (r *registrationUsers) CreateUser(user *models.User) error {
	r.created = user
	return nil
}
func (r *registrationUsers) GetUserByID(id string) (*models.User, error) {
	if r.created != nil && r.created.ID == id {
		return r.created, nil
	}
	return nil, sql.ErrNoRows
}
func (r *registrationUsers) UpdateUser(user *models.User) error {
	r.created = user
	return nil
}
func (r *registrationUsers) RevokeAllRefreshSessions(string) error { return nil }

type recoveryCache struct {
	values map[string]string
	code   string
}

func (c *recoveryCache) SetOTP(userID, code string, _ time.Duration) error {
	if c.values == nil {
		c.values = map[string]string{}
	}
	c.code = code
	c.values["otp:"+userID] = otphash.Hash(code)
	return nil
}
func (c *recoveryCache) GetOTP(userID string) (string, error) {
	value, ok := c.values["otp:"+userID]
	if !ok {
		return "", errors.New("missing")
	}
	return value, nil
}
func (c *recoveryCache) DeleteOTP(userID string) error {
	delete(c.values, "otp:"+userID)
	return nil
}
func (c *recoveryCache) Set(key string, value interface{}, _ time.Duration) error {
	if c.values == nil {
		c.values = map[string]string{}
	}
	c.values[key] = "set"
	return nil
}
func (c *recoveryCache) SetIfAbsent(key string, value interface{}, _ time.Duration) (bool, error) {
	if c.values == nil {
		c.values = map[string]string{}
	}
	if _, exists := c.values[key]; exists {
		return false, nil
	}
	c.values[key] = "set"
	return true, nil
}
func (c *recoveryCache) Get(key string) (string, error) {
	value, ok := c.values[key]
	if !ok {
		return "", errors.New("missing")
	}
	return value, nil
}
func (c *recoveryCache) SetRevokedToken(string, time.Duration) error { return nil }
func (c *recoveryCache) IsTokenRevoked(string) bool                  { return false }
func (c *recoveryCache) IncrementOTPAttempts(string, int, time.Duration) (int, bool) {
	return 1, true
}
func (c *recoveryCache) ResetOTPAttempts(string) {}

func TestRegisterPreservesProviderLocationAndCredentials(t *testing.T) {
	for _, role := range []string{"technician", "isp", "customer"} {
		t.Run(role, func(t *testing.T) {
			users := &registrationUsers{}
			svc := NewAuthService(users, nil)
			user, err := svc.CreateUser(dto.RegisterRequest{
				Phone: "+254712345678", Name: "Jane Kamau", Role: role,
				Username: "jkamau", Password: "SecurePass123", County: " Kiambu ", Town: " Ruiru ",
			})
			if err != nil {
				t.Fatal(err)
			}
			if user.Town != "Ruiru" || user.County != "Kiambu" || user.Username != "jkamau" || user.Role != role {
				t.Fatal("registration discarded location or account details")
			}
			ok, err := utils.VerifyPassword("SecurePass123", user.PasswordHash)
			if err != nil || !ok || user.PasswordHash == "SecurePass123" {
				t.Fatal("password was not securely hashed")
			}
			user.IsVerified = true
			if _, err := svc.AuthenticateWithPassword(user.Phone, "SecurePass123"); err != nil {
				t.Fatalf("phone/password login failed for %s: %v", role, err)
			}
			if _, err := svc.AuthenticateWithUsername(user.Username, "SecurePass123"); err != nil {
				t.Fatalf("username/password login failed for %s: %v", role, err)
			}
		})
	}
}

func TestForgotAndResetPasswordWorksForPhoneAndUsername(t *testing.T) {
	for _, identifier := range []string{"+254712345678", "jkamau"} {
		t.Run(identifier, func(t *testing.T) {
			users := &registrationUsers{created: &models.User{
				ID: "user-1", Phone: "+254712345678", Username: "jkamau",
				Role: "customer", Status: "active", IsVerified: true,
			}}
			cache := &recoveryCache{}
			svc := NewAuthService(users, cache)
			user, expires, err := svc.ForgotPassword(identifier, "127.0.0.1")
			if err != nil || user.ID != "user-1" || expires <= 0 || cache.code == "" {
				t.Fatalf("forgot failed: user=%v expires=%d err=%v", user, expires, err)
			}
			updated, err := svc.ResetPasswordWithOTP(user.ID, user.Phone, cache.code, "NewSecurePass123")
			if err != nil {
				t.Fatalf("reset failed: %v", err)
			}
			ok, err := utils.VerifyPassword("NewSecurePass123", updated.PasswordHash)
			if err != nil || !ok {
				t.Fatal("new password was not saved")
			}
		})
	}
}
