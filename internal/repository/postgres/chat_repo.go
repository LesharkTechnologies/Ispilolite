package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type ChatRepository struct {
	reader *sql.DB
	writer *sql.DB
}

func NewChatRepository() (*ChatRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &ChatRepository{reader: reader, writer: writer}, nil
}

func (r *ChatRepository) Create(ctx context.Context, message *models.ChatMessage) error {
	attachments, err := json.Marshal(message.AttachmentURLs)
	if err != nil {
		return err
	}
	result, err := r.writer.ExecContext(ctx, `INSERT INTO job_messages(id,request_id,sender_id,body,attachment_urls,created_at)
		SELECT $1,j.id,$3,$4,$5,$6 FROM service_requests j WHERE j.id=$2 AND j.deleted_at IS NULL AND
		(j.customer_id=$3 OR j.target_isp_id=$3 OR j.target_technician_id=$3 OR j.assigned_isp_id=$3 OR j.assigned_technician_id=$3)`,
		message.ID, message.JobRequestID, message.SenderID, message.Body, attachments, message.CreatedAt)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *ChatRepository) List(ctx context.Context, jobID, userID string, before time.Time, limit int) ([]*models.ChatMessage, error) {
	if before.IsZero() {
		before = time.Now().UTC().Add(time.Minute)
	}
	rows, err := r.reader.QueryContext(ctx, `SELECT m.id,m.request_id,m.sender_id,u.name,u.role,m.body,m.attachment_urls,
		(SELECT read_at FROM job_message_receipts rr WHERE rr.message_id=m.id AND rr.user_id=$2),m.created_at,m.edited_at
		FROM job_messages m JOIN users u ON u.id=m.sender_id JOIN service_requests j ON j.id=m.request_id
		WHERE m.request_id=$1 AND m.deleted_at IS NULL AND m.created_at<$3 AND
		(j.customer_id=$2 OR j.target_isp_id=$2 OR j.target_technician_id=$2 OR j.assigned_isp_id=$2 OR j.assigned_technician_id=$2)
		ORDER BY m.created_at DESC LIMIT $4`, jobID, userID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.ChatMessage, 0)
	for rows.Next() {
		message := &models.ChatMessage{}
		var attachments []byte
		var readAt, editedAt sql.NullTime
		if err := rows.Scan(&message.ID, &message.JobRequestID, &message.SenderID, &message.SenderName,
			&message.SenderRole, &message.Body, &attachments, &readAt, &message.CreatedAt, &editedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(attachments, &message.AttachmentURLs)
		if readAt.Valid {
			value := readAt.Time
			message.ReadAt = &value
		}
		if editedAt.Valid {
			value := editedAt.Time
			message.EditedAt = &value
		}
		items = append(items, message)
	}
	return items, rows.Err()
}

func (r *ChatRepository) MarkRead(ctx context.Context, jobID, userID string) error {
	result, err := r.writer.ExecContext(ctx, `INSERT INTO job_message_receipts(message_id,user_id,read_at)
		SELECT m.id,$2,now() FROM job_messages m JOIN service_requests j ON j.id=m.request_id
		WHERE m.request_id=$1 AND m.sender_id<>$2 AND m.deleted_at IS NULL AND
		(j.customer_id=$2 OR j.target_isp_id=$2 OR j.target_technician_id=$2 OR j.assigned_isp_id=$2 OR j.assigned_technician_id=$2)
		ON CONFLICT(message_id,user_id) DO UPDATE SET read_at=EXCLUDED.read_at`, jobID, userID)
	if err != nil {
		return err
	}
	_, err = result.RowsAffected()
	return err
}

func (r *ChatRepository) Participants(ctx context.Context, jobID, userID string) ([]string, error) {
	var customer, targetISP, targetTech, assignedISP, assignedTech sql.NullString
	err := r.reader.QueryRowContext(ctx, `SELECT customer_id::text,target_isp_id::text,target_technician_id::text,
		assigned_isp_id::text,assigned_technician_id::text FROM service_requests WHERE id=$1 AND deleted_at IS NULL AND
		(customer_id=$2 OR target_isp_id=$2 OR target_technician_id=$2 OR assigned_isp_id=$2 OR assigned_technician_id=$2)`, jobID, userID).
		Scan(&customer, &targetISP, &targetTech, &assignedISP, &assignedTech)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]string, 0, 5)
	for _, value := range []sql.NullString{customer, targetISP, targetTech, assignedISP, assignedTech} {
		if value.Valid && value.String != "" && !seen[value.String] {
			seen[value.String] = true
			out = append(out, value.String)
		}
	}
	return out, nil
}
