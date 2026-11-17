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
func (f *fakeSpeedTestRepo) GetByID(ctx context.Context, id, userID string) (*models.SpeedTest, error) {
	if f.created != nil && f.created.ID == id {
		return f.created, nil
	}
	return &models.SpeedTest{ID: id, UserID: userID, DownloadMbps: 30, UploadMbps: 15, LatencyMs: 20, County: "Machakos", Town: "Machakos Town"}, nil
}
func (f *fakeSpeedTestRepo) Delete(ctx context.Context, id, userID string) error {
	return nil
}
func (f *fakeSpeedTestRepo) Stats(ispID, county, town string) (*models.SpeedTestStats, error) {
	return &models.SpeedTestStats{ISPID: ispID, County: county, Town: town}, nil
}
func (f *fakeSpeedTestRepo) GetCountyMonthlyStats(ctx context.Context, county, town string, year, month int) (*models.CountyMonthlySpeedStats, error) {
	return &models.CountyMonthlySpeedStats{
		County:                county,
		Town:                  town,
		Year:                  2026,
		Month:                 11,
		SampleCount:           15,
		MinDownloadMbps:       5.0,
		MaxDownloadMbps:       50.0,
		AvgDownloadMbps:       30.0,
		AvgUploadMbps:         15.0,
		AvgLatencyMs:          18.0,
		RecommendationSummary: "Machakos Town, Machakos internet speeds range from 5.0 Mbps to 50.0 Mbps, averaging 30.0 Mbps based on 15 customer speed test(s) in November 2026.",
	}, nil
}
func (f *fakeSpeedTestRepo) ListCountyBenchmarks(ctx context.Context, year, month int) ([]*models.CountySpeedBenchmark, error) {
	return []*models.CountySpeedBenchmark{
		{County: "Mombasa", SampleCount: 120, MinDownloadMbps: 10, MaxDownloadMbps: 150, AvgDownloadMbps: 80, AvgUploadMbps: 40, AvgLatencyMs: 15},
		{County: "Kisumu", SampleCount: 85, MinDownloadMbps: 8, MaxDownloadMbps: 120, AvgDownloadMbps: 56, AvgUploadMbps: 25, AvgLatencyMs: 22},
		{County: "Nairobi", SampleCount: 350, MinDownloadMbps: 10, MaxDownloadMbps: 200, AvgDownloadMbps: 50, AvgUploadMbps: 30, AvgLatencyMs: 12},
		{County: "Machakos", SampleCount: 45, MinDownloadMbps: 5, MaxDownloadMbps: 50, AvgDownloadMbps: 30, AvgUploadMbps: 15, AvgLatencyMs: 20},
	}, nil
}
func (f *fakeSpeedTestRepo) ListTownStats(ctx context.Context, county string, year, month int) ([]*models.CountyMonthlySpeedStats, error) {
	return []*models.CountyMonthlySpeedStats{
		{County: county, Town: "Machakos Town", AvgDownloadMbps: 30, MinDownloadMbps: 5, MaxDownloadMbps: 50, SampleCount: 30},
		{County: county, Town: "Athi River", AvgDownloadMbps: 45, MinDownloadMbps: 10, MaxDownloadMbps: 80, SampleCount: 15},
	}, nil
}
func (f *fakeSpeedTestRepo) AggregateMonthlySpeedStats(ctx context.Context, year, month int) error {
	return nil
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

func TestSpeedTestCountyStatsAndRecommendation(t *testing.T) {
	repo := &fakeSpeedTestRepo{}
	h := newSpeedTestHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/speed-stats?county=Machakos&town=Machakos%20Town&include_towns=true", nil)
	rec := httptest.NewRecorder()
	h.CountyStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var res struct {
		Success bool `json:"success"`
		Data    struct {
			Stats models.CountyMonthlySpeedStats   `json:"stats"`
			Towns []models.CountyMonthlySpeedStats `json:"towns"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatal("expected success=true")
	}
	if res.Data.Stats.AvgDownloadMbps != 30.0 || res.Data.Stats.MinDownloadMbps != 5.0 || res.Data.Stats.MaxDownloadMbps != 50.0 {
		t.Fatalf("unexpected stats: %+v", res.Data.Stats)
	}
	if !strings.Contains(res.Data.Stats.RecommendationSummary, "Machakos Town") {
		t.Fatalf("unexpected recommendation: %s", res.Data.Stats.RecommendationSummary)
	}
	if len(res.Data.Towns) != 2 {
		t.Fatalf("expected 2 towns, got %d", len(res.Data.Towns))
	}
}

func TestSpeedTestCountyBenchmarks(t *testing.T) {
	repo := &fakeSpeedTestRepo{}
	h := newSpeedTestHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/county-speed-benchmarks?year=2026&month=11", nil)
	rec := httptest.NewRecorder()
	h.CountyBenchmarks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var res struct {
		Success bool `json:"success"`
		Data    struct {
			Benchmarks []models.CountySpeedBenchmark `json:"benchmarks"`
			Count      int                           `json:"count"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if len(res.Data.Benchmarks) < 4 {
		t.Fatalf("expected at least 4 benchmarks, got %d", len(res.Data.Benchmarks))
	}
	if res.Data.Benchmarks[0].County != "Mombasa" || res.Data.Benchmarks[0].AvgDownloadMbps != 80.0 {
		t.Fatalf("unexpected top benchmark: %+v", res.Data.Benchmarks[0])
	}
}

func TestSpeedTestUserGetAndDelete(t *testing.T) {
	repo := &fakeSpeedTestRepo{}
	h := newSpeedTestHandler(repo)

	ctx := context.WithValue(context.Background(), "userID", "usr-123")

	// Get
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/my/speed-tests/st-1", nil).WithContext(ctx)
	getRec := httptest.NewRecorder()
	h.GetSpeedTest(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}

	// Delete
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/my/speed-tests/st-1", nil).WithContext(ctx)
	delRec := httptest.NewRecorder()
	h.DeleteSpeedTest(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRec.Code, delRec.Body.String())
	}
}
