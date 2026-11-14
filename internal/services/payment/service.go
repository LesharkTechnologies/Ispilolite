package payment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"ispilolite/internal/models"
	"ispilolite/internal/utils"
	"ispilolite/pkg/notifications"
	"ispilolite/pkg/queue"
)

type Repository interface {
	CreateIntent(context.Context, *models.PaymentIntent) (*models.PaymentIntent, bool, error)
	SetProviderReference(context.Context, string, string, string, map[string]interface{}) error
	ApplyProviderEvent(context.Context, string, *models.PaymentProviderEvent, []byte) (*models.Escrow, bool, error)
	EscrowForParticipant(context.Context, string, string) (*models.Escrow, error)
	RequestRelease(context.Context, string, string, string) (*models.EscrowMilestone, error)
	ConfirmSettlement(context.Context, string, string, string, string, float64) error
}

type Service struct {
	repo      Repository
	providers map[string]Provider
	notifier  *notifications.Service
}

func NewService(repo Repository, providers []Provider, notifier *notifications.Service) *Service {
	items := map[string]Provider{}
	for _, p := range providers {
		if p != nil {
			items[p.Name()] = p
		}
	}
	return &Service{repo: repo, providers: items, notifier: notifier}
}

func ProvidersFromEnv() []Provider {
	return []Provider{
		NewMPesaProvider(os.Getenv("MPESA_BASE_URL"), os.Getenv("MPESA_CONSUMER_KEY"), os.Getenv("MPESA_CONSUMER_SECRET"), os.Getenv("MPESA_SHORTCODE"), os.Getenv("MPESA_PASSKEY"), os.Getenv("MPESA_CALLBACK_URL"), os.Getenv("PAYMENT_WEBHOOK_SECRET")),
		NewAirtelProvider(os.Getenv("AIRTEL_BASE_URL"), os.Getenv("AIRTEL_CLIENT_ID"), os.Getenv("AIRTEL_CLIENT_SECRET"), os.Getenv("AIRTEL_CALLBACK_URL"), os.Getenv("PAYMENT_WEBHOOK_SECRET")),
	}
}

func (s *Service) Initiate(ctx context.Context, jobID, payerID, providerName, phone, idempotency string, amount float64, milestones []*models.EscrowMilestone) (*models.PaymentIntent, error) {
	provider := s.providers[strings.ToLower(strings.TrimSpace(providerName))]
	if provider == nil || amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) || math.Abs(amount*100-math.Round(amount*100)) > 0.000001 || strings.TrimSpace(phone) == "" || strings.TrimSpace(idempotency) == "" {
		return nil, ErrInvalid
	}
	// Daraja STK expects whole KES; rounding after persisting a decimal intent
	// causes its successful callback to fail amount validation.
	if provider.Name() == "mpesa" && amount != math.Trunc(amount) {
		return nil, fmt.Errorf("%w: M-Pesa requires a whole-KES amount", ErrInvalid)
	}
	if len(milestones) == 0 {
		milestones = []*models.EscrowMilestone{{Title: "Job completion", Amount: amount, Sequence: 1}}
	}
	if len(milestones) > 20 {
		return nil, ErrInvalid
	}
	var total float64
	now := time.Now().UTC()
	for i, m := range milestones {
		if m == nil || strings.TrimSpace(m.Title) == "" || m.Amount <= 0 {
			return nil, ErrInvalid
		}
		m.ID = utils.GenerateID()
		m.Title = strings.TrimSpace(m.Title)
		m.Sequence = i + 1
		m.Status = "pending"
		m.CreatedAt = now
		total += m.Amount
	}
	if fmt.Sprintf("%.2f", total) != fmt.Sprintf("%.2f", amount) {
		return nil, fmt.Errorf("%w: milestone amounts must equal payment amount", ErrInvalid)
	}
	intent := &models.PaymentIntent{ID: utils.GenerateID(), JobRequestID: jobID, PayerID: payerID, Provider: provider.Name(), Phone: phone, Currency: "KES", Amount: amount, Status: "created", IdempotencyKey: idempotency, Milestones: milestones, CreatedAt: now, UpdatedAt: now}
	stored, created, err := s.repo.CreateIntent(ctx, intent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	if err != nil {
		return nil, err
	}
	if !created {
		if stored.Provider != intent.Provider || stored.JobRequestID != jobID || stored.Phone != phone || stored.Amount != amount {
			return nil, fmt.Errorf("%w: idempotency key belongs to another payment", ErrInvalid)
		}
		if stored.Status == "failed" {
			return nil, fmt.Errorf("%w: previous attempt failed; reconcile with provider before starting a new payment", ErrProvider)
		}
		return stored, nil
	}
	result, err := provider.Initiate(ctx, InitiateRequest{Reference: intent.ID, Phone: phone, Amount: amount, Currency: "KES", Description: "Ispilo job escrow"})
	if err != nil {
		// A failed initiation must not look like an in-flight payment forever.
		// Never auto-retry the same key: an upstream timeout can mean the
		// provider processed it even though the response was lost.
		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if updateErr := s.repo.SetProviderReference(updateCtx, intent.ID, "", "failed", map[string]interface{}{}); updateErr != nil {
			return nil, fmt.Errorf("initiate payment: %v; persist failure: %w", err, updateErr)
		}
		return nil, err
	}
	if err = s.repo.SetProviderReference(ctx, intent.ID, result.ExternalID, result.Status, result.Raw); err != nil {
		return nil, err
	}
	intent.ExternalID, intent.Status = result.ExternalID, result.Status
	return intent, nil
}

func (s *Service) HandleWebhook(ctx context.Context, providerName string, header http.Header, body []byte) (*models.Escrow, error) {
	provider := s.providers[strings.ToLower(providerName)]
	if provider == nil {
		return nil, ErrProvider
	}
	event, err := provider.ParseWebhook(header, body)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(body, &event.Raw)
	escrow, created, err := s.repo.ApplyProviderEvent(ctx, provider.Name(), event, body)
	if err != nil {
		return nil, err
	}
	if created && escrow != nil {
		queue.PublishBestEffort(queue.NotificationExchange, "notification.push", queue.Event{Type: "escrow.held", AggregateID: escrow.ID, RecipientID: escrow.PayerID, Data: map[string]interface{}{"job_request_id": escrow.JobRequestID, "amount": escrow.Amount, "currency": escrow.Currency}})
		if s.notifier != nil {
			_ = s.notifier.SendInApp(escrow.PayerID, "escrow_held", "Payment secured", "Your payment is safely held until work is approved.", map[string]interface{}{"escrow_id": escrow.ID, "job_request_id": escrow.JobRequestID})
		}
	}
	return escrow, nil
}

func (s *Service) Escrow(ctx context.Context, jobID, userID string) (*models.Escrow, error) {
	item, err := s.repo.EscrowForParticipant(ctx, jobID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}
func (s *Service) Release(ctx context.Context, escrowID, payerID, milestoneID string) (*models.EscrowMilestone, error) {
	item, err := s.repo.RequestRelease(ctx, escrowID, payerID, milestoneID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	if err != nil {
		return nil, err
	}
	queue.PublishBestEffort(queue.NotificationExchange, "notification.push", queue.Event{Type: "escrow.release_requested", AggregateID: escrowID, Data: map[string]interface{}{"milestone_id": item.ID, "amount": item.Amount}})
	return item, nil
}

// ConfirmSettlement is called by the trusted payout worker after the external
// rail confirms a release/refund. It is separately HMAC/token authenticated.
func (s *Service) ConfirmSettlement(ctx context.Context, header http.Header, body []byte) error {
	secret := os.Getenv("PAYMENT_WEBHOOK_SECRET")
	if strings.TrimSpace(secret) == "" || (header.Get("X-Webhook-Token") != secret && !verifyHMAC(secret, body, header.Get("X-Signature"))) {
		return ErrForbidden
	}
	var input struct {
		EscrowID    string  `json:"escrow_id"`
		MilestoneID string  `json:"milestone_id,omitempty"`
		Action      string  `json:"action"`
		Reference   string  `json:"reference"`
		Amount      float64 `json:"amount,omitempty"`
	}
	if json.Unmarshal(body, &input) != nil || input.EscrowID == "" || input.Reference == "" {
		return ErrInvalid
	}
	return s.repo.ConfirmSettlement(ctx, input.EscrowID, input.MilestoneID, strings.ToLower(input.Action), input.Reference, input.Amount)
}
