package chat

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"ispilolite/internal/models"
	"ispilolite/internal/utils"
	"ispilolite/internal/validation"
	"ispilolite/pkg/notifications"
	"ispilolite/pkg/queue"
)

var (
	ErrInvalid   = errors.New("message text or attachment is required")
	ErrForbidden = errors.New("job not found or chat access denied")
)

type Repository interface {
	Create(context.Context, *models.ChatMessage) error
	List(context.Context, string, string, time.Time, int) ([]*models.ChatMessage, error)
	MarkRead(context.Context, string, string) error
	Participants(context.Context, string, string) ([]string, error)
	Delete(context.Context, string, string, bool) error
}

type Service struct {
	repo     Repository
	notifier *notifications.Service
	hub      *notifications.Hub
}

func NewService(repo Repository, notifier *notifications.Service, hub *notifications.Hub) *Service {
	if hub == nil {
		hub = notifications.Default
	}
	return &Service{repo: repo, notifier: notifier, hub: hub}
}

func (s *Service) Send(ctx context.Context, jobID, senderID, body string, attachments []string) (*models.ChatMessage, error) {
	body = strings.TrimSpace(body)
	if len(body) > 4000 {
		return nil, ErrInvalid
	}
	urls, err := validation.CloudinaryURLs(attachments, 5)
	if err != nil {
		return nil, err
	}
	if body == "" && len(urls) == 0 {
		return nil, ErrInvalid
	}
	message := &models.ChatMessage{ID: utils.GenerateID(), JobRequestID: jobID, SenderID: senderID,
		Body: body, AttachmentURLs: urls, CreatedAt: time.Now().UTC()}
	if err := s.repo.Create(ctx, message); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, err
	}
	s.hub.PublishTopic("job:"+jobID, "chat_message", message)
	queue.PublishBestEffort(queue.NotificationExchange, "notification.push", queue.Event{Type: "chat.message",
		AggregateID: jobID, Data: map[string]interface{}{"message_id": message.ID, "sender_id": senderID}})
	if s.notifier != nil {
		participants, _ := s.repo.Participants(ctx, jobID, senderID)
		for _, participant := range participants {
			if participant == senderID {
				continue
			}
			_ = s.notifier.SendInApp(participant, "chat_message", "New job message", "You have a new message about your job.",
				map[string]interface{}{"job_request_id": jobID, "message_id": message.ID})
		}
	}
	return message, nil
}

func (s *Service) List(ctx context.Context, jobID, userID string, before time.Time, limit int) ([]*models.ChatMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if _, err := s.repo.Participants(ctx, jobID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, err
	}
	items, err := s.repo.List(ctx, jobID, userID, before, limit)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrForbidden
	}
	return items, err
}

func (s *Service) MarkRead(ctx context.Context, jobID, userID string) error {
	if _, err := s.repo.Participants(ctx, jobID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return err
	}
	return s.repo.MarkRead(ctx, jobID, userID)
}

func (s *Service) Delete(ctx context.Context, messageID, userID, role string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return ErrInvalid
	}
	isAdmin := role == "admin"
	if err := s.repo.Delete(ctx, messageID, userID, isAdmin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return err
	}
	return nil
}
