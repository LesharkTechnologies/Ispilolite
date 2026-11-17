package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
)

type fakeFlagStore struct {
	created *models.CustomerFlag
	err     error

	reviewStatus   string
	reviewFeedback string
	reviewAction   string
	reviewID       string
	cancelledID    string
}

func (f *fakeFlagStore) CreateFlag(_ context.Context, item *models.CustomerFlag) error {
	if f.err != nil {
		return f.err
	}
	f.created = item
	return nil
}

func (f *fakeFlagStore) ListFlagsByReporter(context.Context, string, int) ([]*models.CustomerFlag, error) {
	return []*models.CustomerFlag{{ID: "flag-1", Status: models.FlagPending}}, f.err
}

func (f *fakeFlagStore) CancelFlag(_ context.Context, id, _ string) error {
	f.cancelledID = id
	return f.err
}

func (f *fakeFlagStore) ListFlags(context.Context, string, int) ([]*models.CustomerFlag, error) {
	return []*models.CustomerFlag{}, f.err
}

func (f *fakeFlagStore) ReviewFlag(_ context.Context, id, _ string, status, feedback, action string) error {
	f.reviewID, f.reviewStatus, f.reviewFeedback, f.reviewAction = id, status, feedback, action
	return f.err
}

func (f *fakeFlagStore) ListMonitored(context.Context, bool, int) ([]*models.MonitoredUser, error) {
	return []*models.MonitoredUser{}, f.err
}

func (f *fakeFlagStore) ReleaseMonitoring(context.Context, string, string) error { return f.err }

func (f *fakeFlagStore) ListActivity(context.Context, string, int) ([]*models.MonitoredActivity, error) {
	return []*models.MonitoredActivity{}, f.err
}

func flagRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), "userID", "reporter-1")
	ctx = context.WithValue(ctx, "userRole", "customer")
	return r.WithContext(ctx)
}

func adminFlagRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	r := flagRequest(t, method, path, body)
	ctx := context.WithValue(r.Context(), "userID", "admin-1")
	ctx = context.WithValue(ctx, "userRole", "admin")
	return r.WithContext(ctx)
}

func TestFlagCreateAcceptsValidReport(t *testing.T) {
	store := &fakeFlagStore{}
	h := newCustomerFlagHandlerWithStore(store)
	body := `{"reported_user_id":"user-2","category":"harassment",
		"description":"Sent repeated abusive messages after the job was cancelled.",
		"image_urls":["https://res.cloudinary.com/demo/image/upload/a.png"]}`
	w := httptest.NewRecorder()
	h.Create(w, flagRequest(t, http.MethodPost, "/api/v1/flags", body))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if store.created == nil {
		t.Fatal("report was not stored")
	}
	if store.created.Status != models.FlagPending || store.created.AccountAction != models.FlagActionNone {
		t.Fatalf("unexpected initial state: %s/%s", store.created.Status, store.created.AccountAction)
	}
	if store.created.ReporterID != "reporter-1" || store.created.Category != "harassment" {
		t.Fatalf("unexpected report fields: %+v", store.created)
	}
	if len(store.created.ImageURLs) != 1 {
		t.Fatalf("expected 1 image, got %d", len(store.created.ImageURLs))
	}
}

func TestFlagCreateValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing description", `{"reported_user_id":"user-2","category":"fraud","description":"short"}`},
		{"bad category", `{"reported_user_id":"user-2","category":"nonsense","description":"This is a long enough description."}`},
		{"self report", `{"reported_user_id":"reporter-1","category":"fraud","description":"Reporting myself for testing."}`},
		{"no subject", `{"category":"fraud","description":"No user and no phone supplied here."}`},
		{"bad phone", `{"reported_phone":"abc","category":"fraud","description":"Phone number is not valid here."}`},
		{"too many images", `{"reported_user_id":"user-2","category":"fraud","description":"Too many screenshots attached to this report.","image_urls":["https://res.cloudinary.com/demo/a.png","https://res.cloudinary.com/demo/b.png","https://res.cloudinary.com/demo/c.png","https://res.cloudinary.com/demo/d.png"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeFlagStore{}
			h := newCustomerFlagHandlerWithStore(store)
			w := httptest.NewRecorder()
			h.Create(w, flagRequest(t, http.MethodPost, "/api/v1/flags", tc.body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			if store.created != nil {
				t.Fatal("invalid report must not reach storage")
			}
		})
	}
}

func TestFlagCreateDuplicateReturnsConflict(t *testing.T) {
	store := &fakeFlagStore{err: postgres.ErrFlagDuplicate}
	h := newCustomerFlagHandlerWithStore(store)
	body := `{"reported_user_id":"user-2","category":"fraud","description":"Duplicate open report for the same account."}`
	w := httptest.NewRecorder()
	h.Create(w, flagRequest(t, http.MethodPost, "/api/v1/flags", body))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFlagAdminApproveWithSuspend(t *testing.T) {
	store := &fakeFlagStore{}
	h := newCustomerFlagHandlerWithStore(store)
	body := `{"status":"approved","admin_feedback":"Evidence confirmed by review.","account_action":"suspend"}`
	w := httptest.NewRecorder()
	h.AdminReview(w, adminFlagRequest(t, http.MethodPatch, "/api/v1/admin/flags/flag-1", body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.reviewID != "flag-1" || store.reviewStatus != models.FlagApproved || store.reviewAction != models.FlagActionSuspend {
		t.Fatalf("unexpected review call: id=%s status=%s action=%s", store.reviewID, store.reviewStatus, store.reviewAction)
	}
}

func TestFlagAdminReviewValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"approve without feedback", `{"status":"approved","account_action":"monitor"}`},
		{"action on cancel", `{"status":"cancelled","admin_feedback":"No evidence provided.","account_action":"suspend"}`},
		{"unknown status", `{"status":"closed","admin_feedback":"whatever","account_action":"none"}`},
		{"unknown action", `{"status":"approved","admin_feedback":"Evidence confirmed.","account_action":"delete"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeFlagStore{}
			h := newCustomerFlagHandlerWithStore(store)
			w := httptest.NewRecorder()
			h.AdminReview(w, adminFlagRequest(t, http.MethodPatch, "/api/v1/admin/flags/flag-1", tc.body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			if store.reviewID != "" {
				t.Fatal("invalid review must not reach storage")
			}
		})
	}
}

func TestFlagCancelUsesPathID(t *testing.T) {
	store := &fakeFlagStore{}
	h := newCustomerFlagHandlerWithStore(store)
	w := httptest.NewRecorder()
	h.Cancel(w, flagRequest(t, http.MethodPost, "/api/v1/flags/flag-9/cancel", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.cancelledID != "flag-9" {
		t.Fatalf("expected flag-9, got %q", store.cancelledID)
	}
}

func TestFlagAdminListRejectsUnknownStatus(t *testing.T) {
	h := newCustomerFlagHandlerWithStore(&fakeFlagStore{})
	w := httptest.NewRecorder()
	h.AdminList(w, adminFlagRequest(t, http.MethodGet, "/api/v1/admin/flags?status=bogus", ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestFlagListMineReturnsOwnReports(t *testing.T) {
	h := newCustomerFlagHandlerWithStore(&fakeFlagStore{})
	w := httptest.NewRecorder()
	h.ListMine(w, flagRequest(t, http.MethodGet, "/api/v1/my/flags", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool                  `json:"success"`
		Data    []models.CustomerFlag `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !resp.Success || len(resp.Data) != 1 || resp.Data[0].ID != "flag-1" {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
}
