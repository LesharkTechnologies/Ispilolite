package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ispilolite/internal/models"
	"ispilolite/pkg/profilelink"
)

type fakeCoverageRepo struct {
	page     *models.PublicCoveragePage
	err      error
	calls    int
	viewType string
	viewKey  string
	viewRef  string
}

func (f *fakeCoverageRepo) Coverage(context.Context, string, string, string) (*models.PublicCoveragePage, error) {
	f.calls++
	return f.page, f.err
}

func (f *fakeCoverageRepo) RecordView(_ context.Context, pageType, pageKey, referrer string) error {
	f.viewType = pageType
	f.viewKey = pageKey
	f.viewRef = referrer
	return nil
}

func testCoverageHandler(t *testing.T, repo *fakeCoverageRepo) *PublicDiscoveryHandler {
	t.Helper()
	links, err := profilelink.New("https://lite.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	return newPublicDiscoveryHandler(repo, links)
}

func TestPublicDiscoveryWebRendersShareableCoveragePage(t *testing.T) {
	repo := &fakeCoverageRepo{page: &models.PublicCoveragePage{
		County:       "Nairobi",
		ISPCount:     1,
		PackageCount: 1,
		ISPs: []*models.PublicCoverageISP{{
			ID:             "isp-1",
			Name:           "Amina & Fibre",
			Description:    "Reliable home internet.",
			LogoURL:        "https://cdn.example.com/logo.png",
			ProfilePath:    "/u/amina-2026-00112233445566778899aabbccddeeff",
			Rating:         4.7,
			ReviewCount:    12,
			Serviceability: "serviceable",
			FromPrice:      2500,
			Packages: []*models.PublicCoveragePackage{{
				ID:           "pkg-1",
				Name:         "Home 10",
				SpeedValue:   10,
				SpeedUnit:    "Mbps",
				Price:        2500,
				BillingCycle: "monthly",
			}},
		}},
	}}
	handler := testCoverageHandler(t, repo)
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/places/nairobi", nil)
	req.Header.Set("Referer", "https://wa.me/status?secret=ignored")
	rec := httptest.NewRecorder()

	handler.Web(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<title>Internet providers in Nairobi | Ispilo Lite</title>",
		`<meta property="og:url" content="https://lite.example.com/places/nairobi">`,
		`<link rel="canonical" href="https://lite.example.com/places/nairobi">`,
		`<meta property="og:image" content="https://cdn.example.com/logo.png">`,
		"Amina &amp; Fibre",
		"Home 10",
		"KES 2500",
		"https://lite.example.com/u/amina-2026-00112233445566778899aabbccddeeff",
		"Serviceable in this area",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page missing %q\n%s", want, body)
		}
	}
	if repo.viewType != "county" || repo.viewKey != "nairobi||" || repo.viewRef != "https://wa.me" {
		t.Fatalf("view = %q %q %q", repo.viewType, repo.viewKey, repo.viewRef)
	}
}

func TestPublicDiscoveryJSONReturnsAreaData(t *testing.T) {
	repo := &fakeCoverageRepo{page: &models.PublicCoveragePage{
		County:   "Nairobi",
		ISPCount: 1,
		ISPs: []*models.PublicCoverageISP{{
			ID:          "isp-1",
			Name:        "Amina Fibre",
			ProfilePath: "/u/amina-2026-00112233445566778899aabbccddeeff",
		}},
	}}
	handler := testCoverageHandler(t, repo)
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/v1/public/coverage?county=nairobi&town=westlands", nil)
	rec := httptest.NewRecorder()

	handler.JSON(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Success bool                      `json:"success"`
		Data    models.PublicCoveragePage `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Success {
		t.Fatal("success = false")
	}
	if response.Data.Town != "Westlands" || response.Data.PageType != "town" {
		t.Fatalf("unexpected area: %+v", response.Data)
	}
	if response.Data.CanonicalURL != "https://lite.example.com/places/nairobi/westlands" {
		t.Fatalf("canonical = %q", response.Data.CanonicalURL)
	}
	if len(response.Data.ISPs) != 1 || response.Data.ISPs[0].ProfileURL != "https://lite.example.com/u/amina-2026-00112233445566778899aabbccddeeff" {
		t.Fatalf("unexpected ISPs: %+v", response.Data.ISPs)
	}
}

func TestPublicDiscoveryJSONRejectsMissingCounty(t *testing.T) {
	repo := &fakeCoverageRepo{}
	handler := testCoverageHandler(t, repo)
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/v1/public/coverage?town=westlands", nil)
	rec := httptest.NewRecorder()

	handler.JSON(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if repo.calls != 0 {
		t.Fatalf("repo called %d times", repo.calls)
	}

	req = httptest.NewRequest(http.MethodGet, "http://api.example.com/api/v1/public/coverage?county=nairobi&estate=kiambu-road", nil)
	rec = httptest.NewRecorder()
	handler.JSON(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("estate without town status = %d", rec.Code)
	}
	if repo.calls != 0 {
		t.Fatalf("repo called after invalid estate: %d", repo.calls)
	}
}

func TestParsePlacePath(t *testing.T) {
	tests := []struct {
		path   string
		county string
		town   string
		estate string
		ok     bool
	}{
		{"/places/nairobi", "nairobi", "", "", true},
		{"/places/nairobi/westlands", "nairobi", "westlands", "", true},
		{"/places/nairobi/westlands/kiambu-road", "nairobi", "westlands", "kiambu-road", true},
		{"/places/", "", "", "", false},
		{"/places/nairobi/westlands/estate/extra", "", "", "", false},
		{"/places/nairobi/<script>", "", "", "", false},
	}
	for _, test := range tests {
		county, town, estate, ok := parsePlacePath(test.path)
		if county != test.county || town != test.town || estate != test.estate || ok != test.ok {
			t.Fatalf("%s = %q %q %q %v", test.path, county, town, estate, ok)
		}
	}
}

func TestPlaceSlugAndDisplay(t *testing.T) {
	if got := placeSlug("Kiambu Road"); got != "kiambu-road" {
		t.Fatalf("placeSlug = %q", got)
	}
	if got := placeSlug("Murang'a"); got != "murang-a" {
		t.Fatalf("placeSlug = %q", got)
	}
	if got := displayPlace("kiambu-road"); got != "Kiambu Road" {
		t.Fatalf("displayPlace = %q", got)
	}
	if got := displayPlace(""); got != "" {
		t.Fatalf("displayPlace empty = %q", got)
	}
	if got := displayPlace("---"); got != "" {
		t.Fatalf("displayPlace hyphens = %q", got)
	}
}

func TestCoveragePageType(t *testing.T) {
	if got := coveragePageType("", ""); got != "county" {
		t.Fatalf("county = %q", got)
	}
	if got := coveragePageType("Westlands", ""); got != "town" {
		t.Fatalf("town = %q", got)
	}
	if got := coveragePageType("Westlands", "Kiambu Road"); got != "estate" {
		t.Fatalf("estate = %q", got)
	}
}

func TestValidPlaceSegment(t *testing.T) {
	for _, value := range []string{"Nairobi", "Kiambu Road", "Murang'a", "Trans-Nzoia"} {
		if !validPlaceSegment(value) {
			t.Fatalf("rejected %q", value)
		}
	}
	for _, value := range []string{"", " ", "<script>", strings.Repeat("a", 81)} {
		if validPlaceSegment(value) {
			t.Fatalf("accepted %q", value)
		}
	}
}
