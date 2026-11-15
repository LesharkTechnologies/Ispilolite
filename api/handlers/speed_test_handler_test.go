package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ispilolite/internal/models"
)

type fakeSpeedTestRepo struct {
	created   *models.SpeedTest
	createdIP string
	options   []*models.SpeedTestISPOption
}

func (f *fakeSpeedTestRepo) ISPOptions(string, string) ([]*models.SpeedTestISPOption, error) {
	return f.options, nil
}
func (f *fakeSpeedTestRepo) Create(item *models.SpeedTest, ip string) error {
	f.created = item
	f.createdIP = ip
	return nil
}
func (f *fakeSpeedTestRepo) ListMine(string, int) ([]*models.SpeedTest, error) {
	return []*models.SpeedTest{}, nil
}
func (f *fakeSpeedTestRepo) Stats(ispID, county, town string) (*models.SpeedTestStats, error) {
	return &models.SpeedTestStats{ISPID: ispID, County: county, Town: town}, nil
}

func TestSpeedTestCreateCapturesTrustedRemoteIPAndPreservesTypedISP(t *testing.T) {
	repo := &fakeSpeedTestRepo{}
	h := newSpeedTestHandler(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/speed-tests", strings.NewReader(`{
		"download_mbps":87.42,"upload_mbps":42.18,"latency_ms":12.5,
		"county":"Machakos","town":"Machakos Town","isp_name":"HomeNet",
		"include_in_public_stats":false
	}`))
	req.RemoteAddr = "198.51.100.20:1234"
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.created == nil {
		t.Fatal("result was not saved")
	}
	if repo.created.ISPSelectionSource != "user_entered" {
		t.Fatalf("source=%q", repo.created.ISPSelectionSource)
	}
	if repo.created.ISPName != "HomeNet" || repo.created.County != "Machakos" {
		t.Fatalf("item=%+v", repo.created)
	}
	if repo.createdIP != "198.51.100.20" {
		t.Fatalf("captured IP=%q", repo.createdIP)
	}
	if repo.created.UserID != "" {
		t.Fatalf("anonymous user=%q", repo.created.UserID)
	}
}

func TestSpeedTestCreateRequiresCountyAndRejectsBadMeasurements(t *testing.T) {
	repo := &fakeSpeedTestRepo{}
	h := newSpeedTestHandler(repo)
	for _, body := range []string{
		`{"download_mbps":10,"upload_mbps":2,"latency_ms":20}`,
		`{"download_mbps":-1,"upload_mbps":2,"latency_ms":20,"county":"Nairobi"}`,
		`{"download_mbps":10,"upload_mbps":2,"latency_ms":70000,"county":"Nairobi"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/speed-tests", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, rec.Code)
		}
	}
	if repo.created != nil {
		t.Fatal("invalid tests were saved")
	}
}

func TestSpeedTestISPOptionsAndStats(t *testing.T) {
	repo := &fakeSpeedTestRepo{options: []*models.SpeedTestISPOption{{ID: "isp-1", Name: "HomeNet", County: "Machakos"}}}
	h := newSpeedTestHandler(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/speed-tests/isps?county=Machakos&town=Machakos%20Town", nil)
	rec := httptest.NewRecorder()
	h.ISPOptions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("options status=%d", rec.Code)
	}
	var response map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["success"] != true {
		t.Fatal("options were not successful")
	}

	ctx := context.WithValue(context.Background(), "userID", "isp-1")
	req = httptest.NewRequest(http.MethodGet, "/api/v1/my/speed-tests/stats?county=Machakos", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	h.ISPStats(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status=%d", rec.Code)
	}
}
