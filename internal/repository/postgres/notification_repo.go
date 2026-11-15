package postgres

import (
	"database/sql"
	"encoding/json"
	"time"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
	"ispilolite/pkg/notifications"
	"ispilolite/pkg/queue"
)

type notificationRepository struct{ dbReader, dbWriter *sql.DB }

type NotificationRepositoryAdapter struct{ *notificationRepository }

func NewNotificationRepository() (*notificationRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &notificationRepository{dbReader: reader, dbWriter: writer}, nil
}

func NewNotificationRepositoryAdapter() (*NotificationRepositoryAdapter, error) {
	repo, err := NewNotificationRepository()
	if err != nil {
		return nil, err
	}
	return &NotificationRepositoryAdapter{repo}, nil
}

func (r *notificationRepository) CreateNotification(notification *models.Notification) error {
	data, err := json.Marshal(notification.Data)
	if err != nil {
		return err
	}
	_, err = r.dbWriter.Exec(`INSERT INTO notifications (id,user_id,type,title,message,data,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (user_id,type,(data->>'location_id')) WHERE type='coverage_recommendation' DO UPDATE SET title=EXCLUDED.title,message=EXCLUDED.message,data=EXCLUDED.data`, notification.ID, notification.UserID, notification.Type, notification.Title, notification.Message, data, notification.CreatedAt)
	if err == nil {
		notifications.Default.Publish(notification)
		rows, queryErr := r.dbWriter.Query(`SELECT token FROM user_device_tokens WHERE user_id=$1 AND enabled=true ORDER BY last_seen_at DESC LIMIT 20`, notification.UserID)
		if queryErr == nil {
			tokens := make([]string, 0)
			for rows.Next() {
				var token string
				if rows.Scan(&token) == nil {
					tokens = append(tokens, token)
				}
			}
			_ = rows.Close()
			if len(tokens) > 0 {
				queue.PublishBestEffort(queue.NotificationExchange, queue.NotificationPushQueue, queue.Event{Type: "notification.push", AggregateID: notification.ID, RecipientID: notification.UserID, Data: map[string]interface{}{"tokens": tokens, "title": notification.Title, "body": notification.Message, "notification_type": notification.Type, "data": notification.Data}})
			}
		}
	}
	return err
}

func (r *notificationRepository) ListNotifications(userID string, unreadOnly bool, limit int) ([]*models.Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.dbReader.Query(`SELECT id,user_id,type,title,message,data,read_at,created_at FROM notifications WHERE user_id=$1 AND (NOT $2 OR read_at IS NULL) ORDER BY created_at DESC LIMIT $3`, userID, unreadOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.Notification, 0)
	for rows.Next() {
		item := &models.Notification{}
		var data []byte
		var readAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.UserID, &item.Type, &item.Title, &item.Message, &data, &readAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		if readAt.Valid {
			value := readAt.Time
			item.ReadAt = &value
		}
		if err := json.Unmarshal(data, &item.Data); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *notificationRepository) MarkNotificationRead(userID, notificationID string) error {
	result, err := r.dbWriter.Exec(`UPDATE notifications SET read_at=$1 WHERE id=$2 AND user_id=$3`, time.Now().UTC(), notificationID, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *notificationRepository) UnreadCount(userID string) (int, error) {
	var count int
	err := r.dbReader.QueryRow(`SELECT count(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL`, userID).Scan(&count)
	return count, err
}

func (r *notificationRepository) MarkAllRead(userID string) (int64, error) {
	result, err := r.dbWriter.Exec(`UPDATE notifications SET read_at=now() WHERE user_id=$1 AND read_at IS NULL`, userID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
