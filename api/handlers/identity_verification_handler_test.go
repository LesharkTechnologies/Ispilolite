package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
)

type fakeIdentityStore struct {
	created          *models.IdentityVerification
	approvedID       string
	approvedName     string
	approvedBadge    string
	approvedAdminID  string
	rejectedID       string
	rejectedReason   string
	rejectedAdminID  string
	err              error
	itemToReturn     *models.IdentityVerification
	listItems        []*models.IdentityVerification
	listTotal        int
}

func (s *fakeIdentityStore) CreateVerification(_ context.Context, item *models.IdentityVerification) error {
	if s.err != nil {
		return s.err
	}
	s.created = item
	return nil
}

func (s *fakeIdentityStore) GetVerificationByID(_ context.Context, id string) (*models.IdentityVerification, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.itemToReturn != nil {
		return s.itemToReturn, nil
	}
	return nil, postgres.ErrVerificationNotFound
}

func (s *fakeIdentityStore) GetLatestVerificationByUserID(_ context.Context, userID string) (*models.IdentityVerification, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.itemToReturn != nil {
		return s.itemToReturn, nil
	}
	return nil, postgres.ErrVerificationNotFound
}

func (s *fakeIdentityStore) ListVerifications(_ context.Context, status, badge string, limit, offset int) ([]*models.IdentityVerification, int, error) {
	if s.err != nil {
		return nil, 0, s.err
	}
	return s.listItems, s.listTotal, nil
}

func (s *fakeIdentityStore) ApproveVerification(_ context.Context, id, adminID, verifiedName, badge, notes string) (*models.IdentityVerification, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.approvedID = id
	s.approvedAdminID = adminID
	s.approvedName = verifiedName
	s.approvedBadge = badge
	res := &models.IdentityVerification{
		ID:                id,
		UserID:            "tech-1",
		Phone:             "+254712345678",
		Status:            models.VerificationStatusApproved,
		VerificationBadge: badge,
		VerifiedName:      verifiedName,
		Notes:             notes,
	}
	return res, nil
}

func (s *fakeIdentityStore) RejectVerification(_ context.Context, id, adminID, reason, notes string) (*models.IdentityVerification, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.rejectedID = id
	s.rejectedAdminID = adminID
	s.rejectedReason = reason
	res := &models.IdentityVerification{
		ID:                id,
		UserID:            "tech-1",
		Phone:             "+254712345678",
		Status:            models.VerificationStatusRejected,
		VerificationBadge: models.VerificationBadgeRejected,
		RejectionReason:   reason,
		Notes:             notes,
	}
	return res, nil
}

type fakeUserRepoForIdentity struct {
	user *models.User
	err  error
}

func (r *fakeUserRepoForIdentity) CreateUser(*models.User) error                         { return nil }
func (r *fakeUserRepoForIdentity) GetUserByPhone(string) (*models.User, error)          { return nil, nil }
func (r *fakeUserRepoForIdentity) GetUserByUsername(string) (*models.User, error)       { return nil, nil }
func (r *fakeUserRepoForIdentity) GetUserByID(userID string) (*models.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.user != nil {
		return r.user, nil
	}
	return nil, errors.New("user not found")
}
func (r *fakeUserRepoForIdentity) UpdateUser(*models.User) error                                  { return nil }
func (r *fakeUserRepoForIdentity) SetDisplayName(string, string) error                           { return nil }
func (r *fakeUserRepoForIdentity) ChangeAccountType(string, string, bool) error                  { return nil }
func (r *fakeUserRepoForIdentity) SetUserStatus(string, string) error                            { return nil }
func (r *fakeUserRepoForIdentity) SetISPTechnicianStatus(string, string, string) error           { return nil }
func (r *fakeUserRepoForIdentity) GetTechniciansByISPID(string) ([]*models.User, error)           { return nil, nil }
func (r *fakeUserRepoForIdentity) GetUsersByStatus(string) ([]*models.User, error)                { return nil, nil }
func (r *fakeUserRepoForIdentity) ListUsersForMessaging(string, []string) ([]*models.User, error) { return nil, nil }
func (r *fakeUserRepoForIdentity) RequestDeleteUser(string, string) error                         { return nil }
func (r *fakeUserRepoForIdentity) SanitizeAndDeleteUser(string) error                             { return nil }
func (r *fakeUserRepoForIdentity) PurgeExpiredUsers() (int64, error)                              { return 0, nil }
func (r *fakeUserRepoForIdentity) CreateRefreshSession(string, string, string, time.Time) error   { return nil }
func (r *fakeUserRepoForIdentity) RefreshSessionActive(string, string) (bool, error)              { return true, nil }
func (r *fakeUserRepoForIdentity) RevokeAllRefreshSessions(string) error                          { return nil }

func authRequest(method, path, body, userID, role string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), "userID", userID)
	ctx = context.WithValue(ctx, "userRole", role)
	return r.WithContext(ctx)
}

func TestSubmitIdentityVerification_UnverifiedPhone(t *testing.T) {
	store := &fakeIdentityStore{}
	users := &fakeUserRepoForIdentity{
		user: &models.User{
			ID:         "tech-1",
			Phone:      "+254712345678",
			IsVerified: false, // Phone not verified!
			Role:       "technician",
		},
	}
	h := newIdentityVerificationHandlerWithDeps(store, users)

	rec := httptest.NewRecorder()
	req := authRequest("POST", "/api/v1/identity/verify", `{"id_photo_url":"https://example.com/id.jpg"}`, "tech-1", "technician")
	h.Submit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unverified phone, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must be verified") {
		t.Fatalf("expected error message to mention verification, got: %s", rec.Body.String())
	}
}

func TestSubmitIdentityVerification_SuccessLinksVerifiedPhone(t *testing.T) {
	store := &fakeIdentityStore{}
	users := &fakeUserRepoForIdentity{
		user: &models.User{
			ID:         "tech-1",
			Name:       "John Doe",
			Phone:      "+254712345678",
			IsVerified: true,
			Role:       "technician",
		},
	}
	h := newIdentityVerificationHandlerWithDeps(store, users)

	rec := httptest.NewRecorder()
	payload := `{"id_photo_url":"https://example.com/my-id.jpg","document_type":"national_id","id_number":"12345678"}`
	req := authRequest("POST", "/api/v1/identity/verify", payload, "tech-1", "technician")
	h.Submit(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.created == nil {
		t.Fatal("expected verification record to be created")
	}
	// Verify that the record is linked to the phone number the user verified their account with
	if store.created.Phone != "+254712345678" {
		t.Fatalf("expected phone '+254712345678', got %q", store.created.Phone)
	}
	if store.created.IDPhotoURL != "https://example.com/my-id.jpg" {
		t.Fatalf("expected photo URL 'https://example.com/my-id.jpg', got %q", store.created.IDPhotoURL)
	}
	if store.created.Status != models.VerificationStatusPending {
		t.Fatalf("expected pending status, got %q", store.created.Status)
	}
}

func TestAdminApprove_IDVerified(t *testing.T) {
	store := &fakeIdentityStore{}
	users := &fakeUserRepoForIdentity{}
	h := newIdentityVerificationHandlerWithDeps(store, users)

	rec := httptest.NewRecorder()
	payload := `{"verified_name":"John Tech Kip","verification_badge":"id_verified"}`
	req := authRequest("POST", "/api/v1/admin/identity-verifications/v-123/verify", payload, "admin-1", "admin")
	h.AdminApprove(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.approvedID != "v-123" {
		t.Fatalf("expected approvedID 'v-123', got %q", store.approvedID)
	}
	if store.approvedName != "John Tech Kip" {
		t.Fatalf("expected approvedName 'John Tech Kip', got %q", store.approvedName)
	}
	if store.approvedBadge != models.VerificationBadgeIDVerified {
		t.Fatalf("expected badge 'id_verified', got %q", store.approvedBadge)
	}
}

func TestAdminApprove_BusinessVerified(t *testing.T) {
	store := &fakeIdentityStore{}
	users := &fakeUserRepoForIdentity{}
	h := newIdentityVerificationHandlerWithDeps(store, users)

	rec := httptest.NewRecorder()
	payload := `{"verified_name":"Acme Fiber Networks","verification_badge":"business verified"}`
	req := authRequest("POST", "/api/v1/admin/identity-verifications/v-456/verify", payload, "admin-1", "admin")
	h.AdminApprove(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.approvedBadge != models.VerificationBadgeBusinessVerified {
		t.Fatalf("expected badge 'business_verified', got %q", store.approvedBadge)
	}
	if store.approvedName != "Acme Fiber Networks" {
		t.Fatalf("expected approvedName 'Acme Fiber Networks', got %q", store.approvedName)
	}
}

func TestAdminApprove_Both(t *testing.T) {
	store := &fakeIdentityStore{}
	users := &fakeUserRepoForIdentity{}
	h := newIdentityVerificationHandlerWithDeps(store, users)

	rec := httptest.NewRecorder()
	payload := `{"verified_name":"Sam ISP & Technician","status":"both"}`
	req := authRequest("POST", "/api/v1/admin/identity-verifications/v-789/verify", payload, "admin-1", "admin")
	h.AdminApprove(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.approvedBadge != models.VerificationBadgeBoth {
		t.Fatalf("expected badge 'both', got %q", store.approvedBadge)
	}
	if store.approvedName != "Sam ISP & Technician" {
		t.Fatalf("expected approvedName 'Sam ISP & Technician', got %q", store.approvedName)
	}
}

func TestAdminApprove_RequiresVerifiedName(t *testing.T) {
	store := &fakeIdentityStore{}
	users := &fakeUserRepoForIdentity{}
	h := newIdentityVerificationHandlerWithDeps(store, users)

	rec := httptest.NewRecorder()
	payload := `{"verification_badge":"id_verified"}` // Missing verified_name!
	req := authRequest("POST", "/api/v1/admin/identity-verifications/v-123/verify", payload, "admin-1", "admin")
	h.AdminApprove(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "verified_name is required") {
		t.Fatalf("expected error mentioning verified_name, got: %s", rec.Body.String())
	}
}

func TestAdminReject_Success(t *testing.T) {
	store := &fakeIdentityStore{}
	users := &fakeUserRepoForIdentity{}
	h := newIdentityVerificationHandlerWithDeps(store, users)

	rec := httptest.NewRecorder()
	payload := `{"rejection_reason":"Document is blurry and expired"}`
	req := authRequest("POST", "/api/v1/admin/identity-verifications/v-123/reject", payload, "admin-1", "admin")
	h.AdminReject(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.rejectedReason != "Document is blurry and expired" {
		t.Fatalf("expected rejection reason, got %q", store.rejectedReason)
	}
}

func TestAdminReject_RequiresReason(t *testing.T) {
	store := &fakeIdentityStore{}
	users := &fakeUserRepoForIdentity{}
	h := newIdentityVerificationHandlerWithDeps(store, users)

	rec := httptest.NewRecorder()
	payload := `{"rejection_reason":""}`
	req := authRequest("POST", "/api/v1/admin/identity-verifications/v-123/reject", payload, "admin-1", "admin")
	h.AdminReject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}
}

func TestGetStatus_ReturnsBadge(t *testing.T) {
	store := &fakeIdentityStore{
		itemToReturn: &models.IdentityVerification{
			ID:                "v-123",
			UserID:            "tech-1",
			Phone:             "+254712345678",
			Status:            models.VerificationStatusApproved,
			VerificationBadge: models.VerificationBadgeIDVerified,
			VerifiedName:      "Verified Tech Name",
			BadgeLabel:        "ID verified",
		},
	}
	users := &fakeUserRepoForIdentity{
		user: &models.User{
			ID:             "tech-1",
			IdentityStatus: models.VerificationBadgeIDVerified,
			VerifiedName:   "Verified Tech Name",
			IsVerified:     true,
		},
	}
	h := newIdentityVerificationHandlerWithDeps(store, users)

	rec := httptest.NewRecorder()
	req := authRequest("GET", "/api/v1/identity/status", "", "tech-1", "technician")
	h.GetStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dto.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected data format: %T", resp.Data)
	}
	if data["verification_badge"] != models.VerificationBadgeIDVerified {
		t.Fatalf("expected badge 'id_verified', got %v", data["verification_badge"])
	}
	if data["verified_name"] != "Verified Tech Name" {
		t.Fatalf("expected verified name 'Verified Tech Name', got %v", data["verified_name"])
	}
}
