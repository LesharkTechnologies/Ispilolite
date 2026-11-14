package warranty

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
	ErrInvalid   = errors.New("invalid warranty claim")
	ErrForbidden = errors.New("warranty unavailable or expired")
)

type Repository interface {
	CreateClaim(context.Context, *models.WarrantyClaim) (*models.JobRequest, error)
	List(context.Context, string, string, int) ([]*models.WarrantyClaim, error)
	Resolve(context.Context, string, string, string, string) error
}
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }
func (s *Service) Create(ctx context.Context, jobID, customerID, description string, photos []string) (*models.WarrantyClaim, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return nil, ErrInvalid
	}
	urls, err := validation.CloudinaryURLs(photos, 8)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	claim := &models.WarrantyClaim{ID: utils.GenerateID(), OriginalRequestID: jobID, CustomerID: customerID, Description: description, PhotoURLs: urls, Status: "open", CreatedAt: now, UpdatedAt: now}
	replacement, err := s.repo.CreateClaim(ctx, claim)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	if err != nil {
		return nil, err
	}
	queue.PublishBestEffort(queue.JobExchange, "job.created", queue.Event{Type: "warranty.rematch", AggregateID: replacement.ID, Data: map[string]interface{}{"original_request_id": jobID, "claim_id": claim.ID, "customer_id": customerID}})
	return claim, nil
}
func (s *Service) List(ctx context.Context, userID, role string, limit int) ([]*models.WarrantyClaim, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.List(ctx, userID, role, limit)
}
func (s *Service) Resolve(ctx context.Context, claimID, actorID, status, note string) error {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "resolved" && status != "rejected" && status != "in_progress" {
		return ErrInvalid
	}
	if err := s.repo.Resolve(ctx, claimID, actorID, status, strings.TrimSpace(note)); errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	} else {
		return err
	}
}
