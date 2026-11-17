package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ispilolite/api/dto"
	"ispilolite/internal/models"
)

type fakeCardsStore struct {
	isps        []*models.ISPCard
	techs       []*models.TechnicianCard
	custs       []*models.CustomerWithJobsCard
	jobs        []*models.JobCard
	overview    *models.MarketplaceCardsOverview
	err         error
}

func (f *fakeCardsStore) ListISPCards(ctx context.Context, county string, limit int) ([]*models.ISPCard, error) {
	return f.isps, f.err
}

func (f *fakeCardsStore) ListTechnicianCards(ctx context.Context, county, town, skill string, limit int) ([]*models.TechnicianCard, error) {
	return f.techs, f.err
}

func (f *fakeCardsStore) ListCustomerWithJobsCards(ctx context.Context, county string, limit int) ([]*models.CustomerWithJobsCard, error) {
	return f.custs, f.err
}

func (f *fakeCardsStore) ListJobCards(ctx context.Context, county, town, serviceType string, limit int) ([]*models.JobCard, error) {
	return f.jobs, f.err
}

func (f *fakeCardsStore) GetMarketplaceCardsOverview(ctx context.Context, county string, limit int) (*models.MarketplaceCardsOverview, error) {
	return f.overview, f.err
}

func TestCardsHandler_ListTechniciansWithDisplayNameAndIdentity(t *testing.T) {
	store := &fakeCardsStore{
		techs: []*models.TechnicianCard{
			{
				ID:             "tech-1",
				UserID:         "user-1",
				Name:           "Collins Muthomi",
				DisplayName:    "Leshark",
				PrimaryName:    "Leshark",
				VerifiedName:   "Collins Muthomi",
				IdentityLabel:  "identity [Collins Muthomi]",
				IdentityStatus: "id_verified",
				BadgeLabel:     "ID verified",
				IconURL:        "https://example.com/avatar.jpg",
				Rating:         4.9,
				JobsDone:       34,
				PricePerHour:   1500,
				Skills:         []string{"Fiber splicing", "WiFi setup"},
				County:         "Nairobi",
				Town:           "Westlands",
				IsAvailable:    true,
			},
		},
	}
	h := newCardsHandlerWithStore(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/cards/technicians?county=Nairobi", nil)
	h.ListTechnicians(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dto.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	itemsBytes, _ := json.Marshal(resp.Data)
	var cards []*models.TechnicianCard
	_ = json.Unmarshal(itemsBytes, &cards)

	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	card := cards[0]
	if card.PrimaryName != "Leshark" {
		t.Errorf("expected primary name 'Leshark', got %q", card.PrimaryName)
	}
	if card.DisplayName != "Leshark" {
		t.Errorf("expected display name 'Leshark', got %q", card.DisplayName)
	}
	if card.IdentityLabel != "identity [Collins Muthomi]" {
		t.Errorf("expected identity label 'identity [Collins Muthomi]', got %q", card.IdentityLabel)
	}
	if card.BadgeLabel != "ID verified" {
		t.Errorf("expected badge 'ID verified', got %q", card.BadgeLabel)
	}
}

func TestCardsHandler_ListISPs(t *testing.T) {
	store := &fakeCardsStore{
		isps: []*models.ISPCard{
			{
				ID:            "isp-1",
				Name:          "Safaricom Home",
				DisplayName:   "Safaricom",
				PrimaryName:   "Safaricom",
				IconURL:       "https://example.com/safaricom.png",
				Rating:        4.8,
				County:        "Nairobi",
				StartingPrice: 2999,
				BadgeLabel:    "Business verified",
				IsActive:      true,
			},
			{
				ID:            "isp-2",
				Name:          "HomeNet Kenya",
				DisplayName:   "HomeNet",
				PrimaryName:   "HomeNet",
				IconURL:       "https://example.com/homenet.png",
				Rating:        4.7,
				County:        "Kiambu",
				StartingPrice: 2499,
				BadgeLabel:    "Business verified",
				IsActive:      true,
			},
			{
				ID:            "isp-3",
				Name:          "Wima Networks",
				DisplayName:   "Wima",
				PrimaryName:   "Wima",
				IconURL:       "https://example.com/wima.png",
				Rating:        4.6,
				County:        "Mombasa",
				StartingPrice: 1999,
				BadgeLabel:    "Business verified",
				IsActive:      true,
			},
		},
	}
	h := newCardsHandlerWithStore(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/cards/isps", nil)
	h.ListISPs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var resp dto.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	dataBytes, _ := json.Marshal(resp.Data)
	var isps []*models.ISPCard
	_ = json.Unmarshal(dataBytes, &isps)

	if len(isps) != 3 {
		t.Fatalf("expected 3 distinct ISPs, got %d", len(isps))
	}
	if isps[0].PrimaryName != "Safaricom" || isps[1].PrimaryName != "HomeNet" || isps[2].PrimaryName != "Wima" {
		t.Errorf("expected Safaricom, HomeNet, Wima; got %s, %s, %s", isps[0].PrimaryName, isps[1].PrimaryName, isps[2].PrimaryName)
	}
}

func TestCardsHandler_ListCustomersWithJobs(t *testing.T) {
	now := time.Now()
	store := &fakeCardsStore{
		custs: []*models.CustomerWithJobsCard{
			{
				CustomerID:       "cust-1",
				Name:             "Alice Wanjiku",
				DisplayName:      "AliceTech",
				PrimaryName:      "AliceTech",
				IdentityLabel:    "identity [Alice Wanjiku]",
				ISPName:          "Safaricom",
				ActiveJobsCount:  2,
				LatestJobTitle:   "Fiber Installation",
				LatestJobService: "Fiber Installation",
				LatestJobBudget:  5000,
				LatestJobDate:    &now,
			},
			{
				CustomerID:       "cust-2",
				Name:             "Brian Omondi",
				DisplayName:      "BrianHotspot",
				PrimaryName:      "BrianHotspot",
				IdentityLabel:    "identity [Brian Omondi]",
				ISPName:          "HomeNet",
				ActiveJobsCount:  1,
				LatestJobTitle:   "WiFi Coverage Extension",
				LatestJobService: "WiFi Setup",
				LatestJobBudget:  3000,
				LatestJobDate:    &now,
			},
			{
				CustomerID:       "cust-3",
				Name:             "Catherine Mwende",
				DisplayName:      "CateWireless",
				PrimaryName:      "CateWireless",
				IdentityLabel:    "identity [Catherine Mwende]",
				ISPName:          "Wima",
				ActiveJobsCount:  1,
				LatestJobTitle:   "CCTV & Wireless Bridge",
				LatestJobService: "CCTV Installation",
				LatestJobBudget:  7500,
				LatestJobDate:    &now,
			},
		},
	}
	h := newCardsHandlerWithStore(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/cards/customers-with-jobs", nil)
	h.ListCustomersWithJobs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var resp dto.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	dataBytes, _ := json.Marshal(resp.Data)
	var customers []*models.CustomerWithJobsCard
	_ = json.Unmarshal(dataBytes, &customers)

	if len(customers) != 3 {
		t.Fatalf("expected 3 customers, got %d", len(customers))
	}
	if customers[0].ISPName != "Safaricom" || customers[1].ISPName != "HomeNet" || customers[2].ISPName != "Wima" {
		t.Errorf("expected Safaricom, HomeNet, Wima; got %s, %s, %s", customers[0].ISPName, customers[1].ISPName, customers[2].ISPName)
	}
}

func TestCardsHandler_ListJobs(t *testing.T) {
	store := &fakeCardsStore{
		jobs: []*models.JobCard{
			{
				ID:                  "job-1",
				Title:               "Fiber Splicing",
				ServiceType:         "Fiber Splicing",
				IconType:            "fiber",
				Budget:              4500,
				Currency:            "KES",
				Status:              "open",
				CustomerID:          "cust-1",
				CustomerName:        "Alice Wanjiku",
				CustomerDisplayName: "AliceTech",
				CustomerPrimaryName: "AliceTech",
				CustomerIdentity:    "identity [Alice Wanjiku]",
			},
		},
	}
	h := newCardsHandlerWithStore(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/cards/jobs", nil)
	h.ListJobs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
}

func TestCardsHandler_Overview(t *testing.T) {
	store := &fakeCardsStore{
		overview: &models.MarketplaceCardsOverview{
			TotalISPs:        5,
			TotalTechnicians: 12,
			TotalCustomers:   7,
			TotalJobs:        9,
		},
	}
	h := newCardsHandlerWithStore(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/cards/overview", nil)
	h.Overview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
}
