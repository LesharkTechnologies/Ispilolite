package payment

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"ispilolite/internal/models"
)

type failingProvider struct{}

func (failingProvider) Name() string { return "mpesa" }
func (failingProvider) Initiate(context.Context, InitiateRequest) (*InitiateResult, error) {
	return nil, ErrProvider
}
func (failingProvider) ParseWebhook(http.Header, []byte) (*models.PaymentProviderEvent, error) {
	return nil, ErrProvider
}

type paymentRepoStub struct {
	stored       *models.PaymentIntent
	status       string
	initiateRuns int
}

func (r *paymentRepoStub) CreateIntent(_ context.Context, item *models.PaymentIntent) (*models.PaymentIntent, bool, error) {
	if r.stored != nil {
		return r.stored, false, nil
	}
	r.stored = item
	r.initiateRuns++
	return item, true, nil
}
func (r *paymentRepoStub) SetProviderReference(_ context.Context, _ string, _ string, status string, _ map[string]interface{}) error {
	r.stored.Status = status
	r.status = status
	return nil
}
func (*paymentRepoStub) ApplyProviderEvent(context.Context, string, *models.PaymentProviderEvent, []byte) (*models.Escrow, bool, error) {
	return nil, false, nil
}
func (*paymentRepoStub) EscrowForParticipant(context.Context, string, string) (*models.Escrow, error) {
	return nil, nil
}
func (*paymentRepoStub) RequestRelease(context.Context, string, string, string) (*models.EscrowMilestone, error) {
	return nil, nil
}
func (*paymentRepoStub) ConfirmSettlement(context.Context, string, string, string, string, float64) error {
	return nil
}

func TestInitiateRejectsFractionalMPesaBeforePersist(t *testing.T) {
	repo := &paymentRepoStub{}
	s := NewService(repo, []Provider{failingProvider{}}, nil)
	_, err := s.Initiate(context.Background(), "job", "payer", "mpesa", "254700000000", "key", 12.50, nil)
	if !errors.Is(err, ErrInvalid) || repo.stored != nil {
		t.Fatalf("expected pre-persistence validation error; got %v, stored=%v", err, repo.stored != nil)
	}
}

func TestFailedInitiationDoesNotLookPendingOnRetry(t *testing.T) {
	repo := &paymentRepoStub{}
	s := NewService(repo, []Provider{failingProvider{}}, nil)
	_, err := s.Initiate(context.Background(), "job", "payer", "mpesa", "254700000000", "key", 12, nil)
	if !errors.Is(err, ErrProvider) || repo.status != "failed" {
		t.Fatalf("expected persisted failure, got status=%q err=%v", repo.status, err)
	}
	_, err = s.Initiate(context.Background(), "job", "payer", "mpesa", "254700000000", "key", 12, nil)
	if !errors.Is(err, ErrProvider) || !strings.Contains(err.Error(), "reconcile") || repo.initiateRuns != 1 {
		t.Fatalf("expected no blind retry; got err=%v attempts=%d", err, repo.initiateRuns)
	}
}
