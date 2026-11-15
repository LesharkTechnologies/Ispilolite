package matching

import (
	"context"
	"testing"
)

type testMatchRepo struct{}

func (testMatchRepo) CreateRequest(context.Context, *Request) error { return nil }
func (testMatchRepo) GetRequest(context.Context, string) (*Request, error) {
	return &Request{ID: "request-1", CustomerID: "customer-1", LocationID: "location-1", ServiceType: "internet", Status: "open"}, nil
}
func (testMatchRepo) ListRequests(context.Context, string, int, int) ([]*Request, int, error) {
	return nil, 0, nil
}
func (testMatchRepo) FindMatches(context.Context, *Request, MatchOptions) ([]Match, error) {
	return []Match{{ProviderID: "isp-1", MatchScore: .9}}, nil
}
func (testMatchRepo) CreateQuotation(context.Context, *Quotation) error { return nil }
func (testMatchRepo) GetQuotation(context.Context, string) (*Quotation, error) {
	return &Quotation{ID: "q-1"}, nil
}
func (testMatchRepo) AcceptQuotation(context.Context, *Quotation) (*Job, error) {
	return &Job{ID: "job-1"}, nil
}

func TestMatchingServiceValidatesAndMatches(t *testing.T) {
	service := NewMatchingService(testMatchRepo{}, nil)
	if _, err := service.CreateRequest(context.Background(), &Request{}); err == nil {
		t.Fatal("invalid request was accepted")
	}
	matches, err := service.MatchProviders(context.Background(), "request-1", MatchOptions{RadiusKM: 10})
	if err != nil || len(matches) != 1 || matches[0].ProviderID != "isp-1" {
		t.Fatalf("unexpected matches: %#v, %v", matches, err)
	}
}

func TestCustomerCannotRematchAnotherCustomersRequest(t *testing.T) {
	service := NewMatchingService(testMatchRepo{}, nil)
	if _, err := service.MatchProvidersForCustomer(context.Background(), "request-1", "customer-2", MatchOptions{RadiusKM: 10}); err == nil {
		t.Fatal("non-owner was allowed to rematch and notify providers")
	}
	items, err := service.MatchProvidersForCustomer(context.Background(), "request-1", "customer-1", MatchOptions{RadiusKM: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("owner could not match: %#v %v", items, err)
	}
}
