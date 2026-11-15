package dispute

import (
	"context"
	"database/sql"
	"errors"
	"ispilolite/internal/models"
	"ispilolite/internal/utils"
	"ispilolite/internal/validation"
	"ispilolite/pkg/queue"
	"strings"
	"time"
)

var (
	ErrInvalid   = errors.New("invalid dispute")
	ErrForbidden = errors.New("dispute access denied")
)

type Repository interface {
	Create(context.Context, *models.Dispute) error
	Get(context.Context, string, string, string) (*models.Dispute, error)
	List(context.Context, string, int) ([]*models.Dispute, error)
	AddComment(context.Context, *models.DisputeComment) error
	Resolve(context.Context, string, string, string, string, float64) error
}
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }
func (s *Service) Create(ctx context.Context, jobID, userID, reason, description string, urls []string) (*models.Dispute, error) {
	reason, description = strings.TrimSpace(reason), strings.TrimSpace(description)
	if reason == "" || description == "" {
		return nil, ErrInvalid
	}
	evidence, err := validation.CloudinaryURLs(urls, 10)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	item := &models.Dispute{ID: utils.GenerateID(), JobRequestID: jobID, OpenedBy: userID, Reason: reason, Description: description, EvidenceURLs: evidence, Status: "open", CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Create(ctx, item); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	} else if err != nil {
		return nil, err
	}
	queue.PublishBestEffort(queue.NotificationExchange, "notification.push", queue.Event{Type: "dispute.opened", AggregateID: item.ID, Data: map[string]interface{}{"job_request_id": jobID}})
	return item, nil
}
func (s *Service) Get(ctx context.Context, id, userID, role string) (*models.Dispute, error) {
	item, err := s.repo.Get(ctx, id, userID, role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	return item, err
}
func (s *Service) List(ctx context.Context, status string, limit int) ([]*models.Dispute, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.List(ctx, strings.ToLower(status), limit)
}
func (s *Service) Comment(ctx context.Context, id, userID, body string, urls []string) (*models.DisputeComment, error) {
	evidence, err := validation.CloudinaryURLs(urls, 10)
	if err != nil {
		return nil, err
	}
	body = strings.TrimSpace(body)
	if body == "" && len(evidence) == 0 {
		return nil, ErrInvalid
	}
	item := &models.DisputeComment{ID: utils.GenerateID(), DisputeID: id, AuthorID: userID, Body: body, EvidenceURLs: evidence, CreatedAt: time.Now().UTC()}
	if err = s.repo.AddComment(ctx, item); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	return item, err
}
func (s *Service) Resolve(ctx context.Context, id, adminID, resolution, note string, amount float64) error {
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	// Partial refunds require an explicit provider contract for splitting the
	// remainder between refund and payout. Fail closed instead of marking the
	// entire escrow refunded after only a partial rail settlement.
	if resolution != "release" && resolution != "refund" {
		return ErrInvalid
	}
	if err := s.repo.Resolve(ctx, id, adminID, resolution, strings.TrimSpace(note), amount); err != nil {
		return err
	}
	queue.PublishBestEffort(queue.NotificationExchange, "notification.push", queue.Event{Type: "dispute.resolved", AggregateID: id, Data: map[string]interface{}{"resolution": resolution, "amount": amount}})
	return nil
}
