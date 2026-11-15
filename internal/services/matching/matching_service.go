package matching

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

type Request struct {
	ID, CustomerID, LocationID, County, ServiceType, Description, Status string
	Budget                                                               float64
	PreferredDate                                                        *time.Time
	Requirements                                                         map[string]interface{}
	Attachments                                                          []Attachment
	CreatedAt, UpdatedAt                                                 time.Time
}
type Attachment struct{ Type, URL string }
type Quotation struct {
	ID, RequestID, ProviderID, Description, Status string
	Amount                                         float64
	Breakdown                                      map[string]float64
	ValidUntil                                     *time.Time
	Terms                                          []string
	CreatedAt                                      time.Time
}
type Job struct {
	ID, RequestID, QuotationID, CustomerID, ProviderID, TechnicianID, Status string
	Price                                                                    float64
	CreatedAt, UpdatedAt                                                     time.Time
}

type Repository interface {
	CreateRequest(context.Context, *Request) error
	GetRequest(context.Context, string) (*Request, error)
	ListRequests(context.Context, string, int, int) ([]*Request, int, error)
	FindMatches(context.Context, *Request, MatchOptions) ([]Match, error)
	CreateQuotation(context.Context, *Quotation) error
	GetQuotation(context.Context, string) (*Quotation, error)
	AcceptQuotation(context.Context, *Quotation) (*Job, error)
}

type ExpiryRepository interface {
	ExpireStale(context.Context, time.Time) error
}
type MatchActionRepository interface {
	Respond(context.Context, string, string) error
	Decline(context.Context, string, string) error
	ListForProvider(context.Context, string, string, int) ([]Match, error)
}
type Notifier interface {
	Notify(context.Context, string, string, map[string]interface{}) error
}
type MatchOptions struct {
	RadiusKM             float64
	MinRating, MaxPrice  float64
	AvailabilityRequired bool
}
type Match struct {
	ID            string     `json:"id"`
	RequestID     string     `json:"request_id"`
	ProviderID    string     `json:"provider_id"`
	ProviderType  string     `json:"provider_type"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	Rating        float64    `json:"rating"`
	DistanceKM    float64    `json:"distance_km"`
	PriceEstimate float64    `json:"price_estimate"`
	MatchScore    float64    `json:"match_score"`
	NotifiedAt    *time.Time `json:"notified_at,omitempty"`
	ShouldNotify  bool       `json:"-"`
}
type MatchingService struct {
	repo     Repository
	notifier Notifier
}

func NewMatchingService(repo Repository, notifier Notifier) *MatchingService {
	return &MatchingService{repo: repo, notifier: notifier}
}
func (s *MatchingService) CreateRequest(ctx context.Context, r *Request) (*Request, error) {
	if r == nil || strings.TrimSpace(r.CustomerID) == "" || strings.TrimSpace(r.LocationID) == "" || strings.TrimSpace(r.ServiceType) == "" {
		return nil, fmt.Errorf("customer, location, and service type are required")
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.Status == "" {
		r.Status = "open"
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	r.UpdatedAt = r.CreatedAt
	if err := s.repo.CreateRequest(ctx, r); err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	return r, nil
}
func (s *MatchingService) GetRequest(ctx context.Context, id string) (*Request, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("request id is required")
	}
	r, e := s.repo.GetRequest(ctx, id)
	if e != nil {
		return nil, fmt.Errorf("get request: %w", e)
	}
	return r, nil
}
func (s *MatchingService) ListRequests(ctx context.Context, status string, limit, offset int) ([]*Request, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	r, n, e := s.repo.ListRequests(ctx, status, limit, offset)
	if e != nil {
		return nil, 0, fmt.Errorf("list requests: %w", e)
	}
	return r, n, nil
}
func (s *MatchingService) MatchProviders(ctx context.Context, id string, o MatchOptions) ([]Match, error) {
	r, e := s.GetRequest(ctx, id)
	if e != nil {
		return nil, e
	}
	if r.Status != "open" {
		return nil, fmt.Errorf("request is not open for matching")
	}
	m, e := s.repo.FindMatches(ctx, r, o)
	if e != nil {
		return nil, fmt.Errorf("match providers: %w", e)
	}
	for _, match := range m {
		if s.notifier != nil && match.ShouldNotify {
			_ = s.notifier.Notify(ctx, match.ProviderID, "service_request.matched", map[string]interface{}{"request_id": id, "match_id": match.ID, "score": match.MatchScore})
		}
	}
	return m, nil
}

func (s *MatchingService) MatchProvidersForCustomer(ctx context.Context, id, customerID string, o MatchOptions) ([]Match, error) {
	request, err := s.GetRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	if request.CustomerID != customerID {
		return nil, fmt.Errorf("request not found")
	}
	return s.MatchProviders(ctx, id, o)
}

func (s *MatchingService) ListForProvider(ctx context.Context, providerID, status string, limit int) ([]Match, error) {
	repo, ok := s.repo.(MatchActionRepository)
	if !ok {
		return nil, fmt.Errorf("match actions are not configured")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return repo.ListForProvider(ctx, providerID, strings.TrimSpace(status), limit)
}

func (s *MatchingService) StartExpiryWorker(ctx context.Context, interval, expiry time.Duration) {
	repo, ok := s.repo.(ExpiryRepository)
	if !ok {
		return
	}
	if interval <= 0 {
		interval = time.Hour
	}
	if expiry <= 0 {
		expiry = 24 * time.Hour
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				_ = repo.ExpireStale(ctx, now.Add(-expiry))
			}
		}
	}()
}

func (s *MatchingService) Respond(ctx context.Context, matchID, providerID string) error {
	repo, ok := s.repo.(MatchActionRepository)
	if !ok {
		return fmt.Errorf("match actions are not configured")
	}
	return repo.Respond(ctx, matchID, providerID)
}
func (s *MatchingService) Decline(ctx context.Context, matchID, providerID string) error {
	repo, ok := s.repo.(MatchActionRepository)
	if !ok {
		return fmt.Errorf("match actions are not configured")
	}
	return repo.Decline(ctx, matchID, providerID)
}
func (s *MatchingService) CreateQuotation(ctx context.Context, q *Quotation) (*Quotation, error) {
	if q == nil || q.RequestID == "" || q.ProviderID == "" || q.Amount < 0 {
		return nil, fmt.Errorf("request, provider, and non-negative amount are required")
	}
	if q.ID == "" {
		q.ID = uuid.NewString()
	}
	if q.Status == "" {
		q.Status = "draft"
	}
	if q.CreatedAt.IsZero() {
		q.CreatedAt = time.Now().UTC()
	}
	if e := s.repo.CreateQuotation(ctx, q); e != nil {
		return nil, fmt.Errorf("create quotation: %w", e)
	}
	return q, nil
}
func (s *MatchingService) AcceptQuotation(ctx context.Context, id string) (*Job, error) {
	q, e := s.repo.GetQuotation(ctx, id)
	if e != nil {
		return nil, fmt.Errorf("get quotation: %w", e)
	}
	if q.Status == "accepted" {
		return nil, fmt.Errorf("quotation already accepted")
	}
	j, e := s.repo.AcceptQuotation(ctx, q)
	if e != nil {
		return nil, fmt.Errorf("accept quotation: %w", e)
	}
	if s.notifier != nil {
		_ = s.notifier.Notify(ctx, q.ProviderID, "quotation.accepted", map[string]interface{}{"quotation_id": q.ID, "job_id": j.ID})
	}
	return j, nil
}
