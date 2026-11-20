package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/services/user"
)

type fakeUserRepoForAccountType struct {
	users map[string]*models.User
}

func newFakeUserRepo() *fakeUserRepoForAccountType {
	return &fakeUserRepoForAccountType{
		users: make(map[string]*models.User),
	}
}

func (r *fakeUserRepoForAccountType) CreateUser(u *models.User) error {
	r.users[u.ID] = u
	return nil
}

func (r *fakeUserRepoForAccountType) GetUserByPhone(string) (*models.User, error) { return nil, nil }
func (r *fakeUserRepoForAccountType) GetUserByUsername(string) (*models.User, error) {
	return nil, nil
}

func (r *fakeUserRepoForAccountType) GetUserByID(userID string) (*models.User, error) {
	u, ok := r.users[userID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	// Return a copy so caller modifications don't mutate state unexpectedly
	copyUser := *u
	return &copyUser, nil
}

func (r *fakeUserRepoForAccountType) UpdateUser(u *models.User) error {
	r.users[u.ID] = u
	return nil
}

func (r *fakeUserRepoForAccountType) SetDisplayName(userID, displayName string) error {
	u, ok := r.users[userID]
	if !ok {
		return sql.ErrNoRows
	}
	u.DisplayName = displayName
	return nil
}

func (r *fakeUserRepoForAccountType) ChangeAccountType(userID, targetRole string, bypassCooldown bool) error {
	u, ok := r.users[userID]
	if !ok {
		return sql.ErrNoRows
	}
	if u.Role == targetRole {
		return nil
	}
	if !bypassCooldown && u.RoleChangedAt != nil {
		if time.Since(*u.RoleChangedAt) < 24*time.Hour {
			return postgres.ErrRoleChangeCooldown
		}
	}
	now := time.Now().UTC()
	u.Role = targetRole
	u.RoleChangedAt = &now
	return nil
}

func (r *fakeUserRepoForAccountType) SetUserStatus(userID, status string) error {
	u, ok := r.users[userID]
	if !ok {
		return sql.ErrNoRows
	}
	u.Status = status
	return nil
}

func (r *fakeUserRepoForAccountType) SetISPTechnicianStatus(ispID, technicianID, status string) error {
	u, ok := r.users[technicianID]
	if !ok || (u.ISP_ID != ispID && u.Role != "technician") {
		return sql.ErrNoRows
	}
	u.Status = status
	return nil
}

func (r *fakeUserRepoForAccountType) GetTechniciansByISPID(ispID string) ([]*models.User, error) {
	var list []*models.User
	for _, u := range r.users {
		if u.ISP_ID == ispID {
			list = append(list, u)
		}
	}
	return list, nil
}

func (r *fakeUserRepoForAccountType) GetUsersByStatus(status string) ([]*models.User, error) {
	var list []*models.User
	for _, u := range r.users {
		if u.Status == status {
			list = append(list, u)
		}
	}
	return list, nil
}

func (r *fakeUserRepoForAccountType) ListUsersForMessaging(string, []string) ([]*models.User, error) {
	return nil, nil
}
func (r *fakeUserRepoForAccountType) RequestDeleteUser(string, string) error { return nil }
func (r *fakeUserRepoForAccountType) SanitizeAndDeleteUser(string) error     { return nil }
func (r *fakeUserRepoForAccountType) PurgeExpiredUsers() (int64, error)      { return 0, nil }
func (r *fakeUserRepoForAccountType) CreateRefreshSession(string, string, string, time.Time) error {
	return nil
}
func (r *fakeUserRepoForAccountType) RefreshSessionActive(string, string) (bool, error) {
	return true, nil
}
func (r *fakeUserRepoForAccountType) RevokeAllRefreshSessions(string) error { return nil }

func TestClientHandler_ChangeAccountType_Success(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["user-1"] = &models.User{
		ID:     "user-1",
		Phone:  "+254700000001",
		Name:   "Alice Kimani",
		Role:   "customer",
		Status: "active",
	}

	handler := &ClientHandler{
		users: user.NewUserService(repo),
	}

	// Change from customer (C) to technician (T)
	body := `{"account_type": "technician", "password": "Password123!", "username": "alice_tech"}`
	req := authRequest("PATCH", "/api/v1/my/account-type", body, "user-1", "customer")
	w := httptest.NewRecorder()

	handler.ChangeAccountType(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp dto.APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode json response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected response.success=true")
	}

	data := resp.Data.(map[string]interface{})
	if data["role"] != "technician" {
		t.Errorf("expected role 'technician', got %v", data["role"])
	}
	if data["token"] == "" {
		t.Errorf("expected a newly minted JWT token in response")
	}

	// Verify database record was updated
	updated := repo.users["user-1"]
	if updated.Role != "technician" {
		t.Errorf("expected repo role to be 'technician', got %s", updated.Role)
	}
	if updated.RoleChangedAt == nil {
		t.Fatalf("expected RoleChangedAt to be set")
	}
}

func TestClientHandler_ChangeAccountType_Both_DualRole(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["user-2"] = &models.User{
		ID:     "user-2",
		Phone:  "+254700000002",
		Name:   "Bob Ndegwa",
		Role:   "customer",
		Status: "active",
	}

	handler := &ClientHandler{
		users: user.NewUserService(repo),
	}

	// Change to dual role: both (T & I)
	body := `{"account_type": "both", "password": "Password123!", "username": "bob_tech"}`
	req := authRequest("PATCH", "/api/v1/my/account-type", body, "user-2", "customer")
	w := httptest.NewRecorder()

	handler.ChangeAccountType(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	updated := repo.users["user-2"]
	if updated.Role != "both" {
		t.Errorf("expected role 'both', got %s", updated.Role)
	}
}

func TestClientHandler_ChangeAccountType_Cooldown_24Hours(t *testing.T) {
	repo := newFakeUserRepo()
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour)
	repo.users["user-cooldown"] = &models.User{
		ID:            "user-cooldown",
		Phone:         "+254700000003",
		Name:          "Charlie Omondi",
		Role:          "technician",
		Status:        "active",
		RoleChangedAt: &twoHoursAgo,
	}

	handler := &ClientHandler{
		users: user.NewUserService(repo),
	}

	// Try to switch within 24 hours (2 hours ago)
	body := `{"account_type": "isp", "password": "Password123!", "username": "charlie_isp", "company_name": "Charlie Net"}`
	req := authRequest("PATCH", "/api/v1/my/account-type", body, "user-cooldown", "technician")
	w := httptest.NewRecorder()

	handler.ChangeAccountType(w, req)

	// Must be rejected with 429 Too Many Requests
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "24 hours") {
		t.Errorf("expected 24 hours cooldown message, got: %s", w.Body.String())
	}

	// Now simulate 25 hours elapsed
	twentyFiveHoursAgo := time.Now().UTC().Add(-25 * time.Hour)
	repo.users["user-cooldown"].RoleChangedAt = &twentyFiveHoursAgo

	w2 := httptest.NewRecorder()
	req2 := authRequest("PATCH", "/api/v1/my/account-type", body, "user-cooldown", "technician")
	handler.ChangeAccountType(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK after 24h, got %d: %s", w2.Code, w2.Body.String())
	}
	if repo.users["user-cooldown"].Role != "isp" {
		t.Errorf("expected role 'isp', got %s", repo.users["user-cooldown"].Role)
	}
}

func TestClientHandler_ChangeAccountType_AdminForbidden(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["user-4"] = &models.User{
		ID:     "user-4",
		Phone:  "+254700000004",
		Role:   "customer",
		Status: "active",
	}

	handler := &ClientHandler{
		users: user.NewUserService(repo),
	}

	body := `{"account_type": "admin"}`
	req := authRequest("PATCH", "/api/v1/my/account-type", body, "user-4", "customer")
	w := httptest.NewRecorder()

	handler.ChangeAccountType(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for admin promotion, got %d", w.Code)
	}
}

func TestISPEndpoints_SetTechnicianStatus_SuspendAndActivate(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["tech-10"] = &models.User{
		ID:     "tech-10",
		Phone:  "+254700000010",
		Name:   "Dennis Tech",
		Role:   "technician",
		ISP_ID: "isp-1",
		Status: "active",
	}

	handler := &ISPEndpointsHandler{
		userService: user.NewUserService(repo),
	}

	// ISP suspends technician -> technician cannot use system
	body := `{"status": "suspended"}`
	req := authRequest("PATCH", "/api/v1/my/technicians/tech-10/status", body, "isp-1", "isp")
	w := httptest.NewRecorder()

	handler.SetTechnicianStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	if repo.users["tech-10"].Status != "suspended" {
		t.Errorf("expected technician status to be 'suspended', got %s", repo.users["tech-10"].Status)
	}

	// ISP reactivates technician
	bodyActivate := `{"status": "active"}`
	reqActivate := authRequest("PATCH", "/api/v1/my/technicians/tech-10/status", bodyActivate, "isp-1", "isp")
	wActivate := httptest.NewRecorder()

	handler.SetTechnicianStatus(wActivate, reqActivate)

	if wActivate.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on activate, got %d: %s", wActivate.Code, wActivate.Body.String())
	}
	if repo.users["tech-10"].Status != "active" {
		t.Errorf("expected technician status to be 'active', got %s", repo.users["tech-10"].Status)
	}
}

func TestISPEndpoints_ChangeTechnicianAccountType(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["tech-20"] = &models.User{
		ID:     "tech-20",
		Phone:  "+254700000020",
		Name:   "Eric Tech",
		Role:   "technician",
		ISP_ID: "isp-1",
		Status: "active",
	}

	handler := &ISPEndpointsHandler{
		userService: user.NewUserService(repo),
	}

	// ISP upgrades technician to dual role: both (technician & isp)
	body := `{"account_type": "both"}`
	req := authRequest("PATCH", "/api/v1/my/technicians/tech-20/account-type", body, "isp-1", "isp")
	w := httptest.NewRecorder()

	handler.ChangeTechnicianAccountType(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	if repo.users["tech-20"].Role != "both" {
		t.Errorf("expected technician role 'both', got %s", repo.users["tech-20"].Role)
	}
}

func TestAdminHandler_ChangeUserAccountType_BypassCooldown(t *testing.T) {
	repo := newFakeUserRepo()
	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
	repo.users["user-bypass"] = &models.User{
		ID:            "user-bypass",
		Phone:         "+254700000030",
		Name:          "Faith Wambui",
		Role:          "customer",
		Status:        "active",
		RoleChangedAt: &oneHourAgo,
	}

	handler := &AdminHandler{
		users: repo,
	}

	// Admin changes user role from customer to technician, bypassing cooldown
	body := `{"account_type": "technician"}`
	req := authRequest("PATCH", "/api/v1/admin/users/user-bypass/account-type", body, "admin-1", "admin")
	w := httptest.NewRecorder()

	handler.ChangeUserAccountType(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	if repo.users["user-bypass"].Role != "technician" {
		t.Errorf("expected role 'technician', got %s", repo.users["user-bypass"].Role)
	}
}

func TestAdminHandler_SetUserStatus(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["user-admin-status"] = &models.User{
		ID:     "user-admin-status",
		Phone:  "+254700000040",
		Role:   "technician",
		Status: "active",
	}

	handler := &AdminHandler{
		users: repo,
	}

	body := `{"status": "suspended"}`
	req := authRequest("PATCH", "/api/v1/admin/users/user-admin-status/status", body, "admin-1", "admin")
	w := httptest.NewRecorder()

	handler.SetUserStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	if repo.users["user-admin-status"].Status != "suspended" {
		t.Errorf("expected status 'suspended', got %s", repo.users["user-admin-status"].Status)
	}
}

func TestClientHandler_GetProfileCompletion(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["user-completion-1"] = &models.User{
		ID:             "user-completion-1",
		Phone:          "+254700000050",
		Name:           "Grace Nduta",
		Role:           "technician",
		Status:         "active",
		IdentityStatus: "unverified",
	}

	handler := &ClientHandler{
		users: user.NewUserService(repo),
	}

	req := authRequest("GET", "/api/v1/my/profile/completion", "", "user-completion-1", "technician")
	w := httptest.NewRecorder()

	handler.GetProfileCompletion(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp dto.APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true")
	}

	dataBytes, _ := json.Marshal(resp.Data)
	var report dto.ProfileCompletionReport
	_ = json.Unmarshal(dataBytes, &report)

	if report.Role != "technician" {
		t.Errorf("expected role technician, got %s", report.Role)
	}
	if report.IsComplete {
		t.Errorf("expected is_complete=false for user with missing fields")
	}
	if len(report.MissingFields) == 0 {
		t.Errorf("expected missing fields list to be non-empty")
	}
	if len(report.UploadInstructions) == 0 {
		t.Errorf("expected upload instructions to be provided")
	}
}

func TestClientHandler_FillMissingProfile(t *testing.T) {
	repo := newFakeUserRepo()
	repo.users["user-fill-1"] = &models.User{
		ID:             "user-fill-1",
		Phone:          "+254700000060",
		Name:           "Peter Mwangi",
		Role:           "technician",
		Status:         "active",
		IdentityStatus: "unverified",
	}

	handler := &ClientHandler{
		users: user.NewUserService(repo),
	}

	body := `{
		"display_name": "PeterFiberPro",
		"county": "Kiambu",
		"town": "Thika",
		"avatar_url": "https://media.ispilo.co.ke/profiles/avatar.jpg"
	}`
	req := authRequest("PATCH", "/api/v1/my/profile/fill-missing", body, "user-fill-1", "technician")
	w := httptest.NewRecorder()

	handler.FillMissingProfile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	u := repo.users["user-fill-1"]
	if u.DisplayName != "PeterFiberPro" {
		t.Errorf("expected display_name to be PeterFiberPro, got %s", u.DisplayName)
	}
	if u.County != "Kiambu" || u.Town != "Thika" {
		t.Errorf("expected Kiambu/Thika, got %s/%s", u.County, u.Town)
	}
}
