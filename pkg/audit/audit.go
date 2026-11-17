package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Event struct {
	ActorID      string
	ActorRole    string
	Event        string
	Action       string
	ResourceType string
	ResourceID   string
	Description  string
	Success      bool
	RequestID    string
	IPAddress    string
	UserAgent    string
	Metadata     map[string]interface{}
	OccurredAt   time.Time
}

type EventView struct {
	ID            string                 `json:"id"`
	ActorID       string                 `json:"actor_id"`
	UserID        string                 `json:"user_id"`
	ActorRole     string                 `json:"actor_role"`
	UserCategory  string                 `json:"user_category"`
	ActorName     string                 `json:"actor_name,omitempty"`
	ActorUsername string                 `json:"actor_username,omitempty"`
	ActorEmail    string                 `json:"actor_email,omitempty"`
	ActorPhone    string                 `json:"actor_phone,omitempty"`
	Event         string                 `json:"event"`
	Action        string                 `json:"action"`
	ResourceType  string                 `json:"resource_type"`
	ResourceID    string                 `json:"resource_id"`
	Description   string                 `json:"description"`
	Success       bool                   `json:"success"`
	Status        string                 `json:"status"`
	RequestID     string                 `json:"request_id"`
	IPAddress     string                 `json:"ip_address"`
	UserAgent     string                 `json:"user_agent"`
	Metadata      map[string]interface{} `json:"metadata"`
	OccurredAt    time.Time              `json:"occurred_at"`
	Time          string                 `json:"time"`
}

type ListFilter struct {
	Event     string
	ActorID   string
	ActorRole string
	Action    string
	IPAddress string
	Status    string
	Search    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

type Store struct {
	DB   *sql.DB
	Root string
}

var current struct {
	sync.RWMutex
	store *Store
}

var validActions = map[string]bool{
	"CREATE":   true,
	"UPDATE":   true,
	"DELETE":   true,
	"GET":      true,
	"LOGIN":    true,
	"REGISTER": true,
	"REQUEST":  true,
	"EXPORT":   true,
	"SYSTEM":   true,
}

func cleanIP(addr string) string {
	addr = strings.TrimSpace(addr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func cleanActorID(id string) string {
	id = strings.TrimSpace(id)
	if _, err := uuid.Parse(id); err != nil {
		return ""
	}
	return id
}

func normalizeAction(action, event string) string {
	a := strings.ToUpper(strings.TrimSpace(action))
	if validActions[a] {
		return a
	}
	ev := strings.ToLower(event)
	switch {
	case strings.Contains(ev, "login"):
		return "LOGIN"
	case strings.Contains(ev, "register"):
		return "REGISTER"
	case strings.Contains(ev, "create") || strings.Contains(ev, "provision") || strings.Contains(ev, "add"):
		return "CREATE"
	case strings.Contains(ev, "update") || strings.Contains(ev, "moderate") || strings.Contains(ev, "resolve") || strings.Contains(ev, "sync"):
		return "UPDATE"
	case strings.Contains(ev, "delete") || strings.Contains(ev, "remove") || strings.Contains(ev, "purge") || strings.Contains(ev, "withdraw"):
		return "DELETE"
	case strings.Contains(ev, "export"):
		return "EXPORT"
	case strings.Contains(ev, "request") || strings.Contains(ev, "apply"):
		return "REQUEST"
	default:
		return "SYSTEM"
	}
}

// UserCategory maps the internal actor role to a human-friendly user category:
// "client" (for customers/clients), "isp", "technicians", "admin", or "anonymous".
func UserCategory(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "customer", "client":
		return "client"
	case "isp":
		return "isp"
	case "technician", "technicians":
		return "technicians"
	case "admin":
		return "admin"
	case "":
		return "anonymous"
	default:
		return strings.ToLower(strings.TrimSpace(role))
	}
}

func Configure(db *sql.DB, root string) *Store {
	if strings.TrimSpace(root) == "" {
		root = "storage/audit"
	}
	s := &Store{DB: db, Root: root}
	current.Lock()
	current.store = s
	current.Unlock()
	return s
}

func Record(ctx context.Context, event Event) error {
	current.RLock()
	s := current.store
	current.RUnlock()
	if s == nil || s.DB == nil {
		return nil
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	event.ActorID = cleanActorID(event.ActorID)
	event.Action = normalizeAction(event.Action, event.Event)
	event.IPAddress = cleanIP(event.IPAddress)
	if event.Metadata == nil {
		event.Metadata = make(map[string]interface{})
	}
	meta, _ := json.Marshal(event.Metadata)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO audit_events(id,actor_id,actor_role,event,action,resource_type,resource_id,description,success,request_id,ip_address,user_agent,metadata,occurred_at) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, uuid.NewString(), event.ActorID, event.ActorRole, event.Event, event.Action, event.ResourceType, event.ResourceID, event.Description, event.Success, event.RequestID, event.IPAddress, event.UserAgent, meta, event.OccurredAt)
	return err
}

func (s *Store) List(ctx context.Context, filter ListFilter) ([]EventView, int, error) {
	if s == nil || s.DB == nil {
		return []EventView{}, 0, nil
	}

	var conditions []string
	var args []interface{}
	argIdx := 1

	if filter.Event != "" {
		conditions = append(conditions, fmt.Sprintf("a.event ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(filter.Event)+"%")
		argIdx++
	}

	if filter.ActorID != "" {
		actorUUID := cleanActorID(filter.ActorID)
		if actorUUID != "" {
			conditions = append(conditions, fmt.Sprintf("a.actor_id = $%d::uuid", argIdx))
			args = append(args, actorUUID)
			argIdx++
		}
	}

	if filter.ActorRole != "" {
		roleLower := strings.ToLower(strings.TrimSpace(filter.ActorRole))
		if roleLower == "client" || roleLower == "customer" {
			conditions = append(conditions, "(lower(a.actor_role) = 'client' OR lower(a.actor_role) = 'customer')")
		} else if strings.HasPrefix(roleLower, "tech") {
			conditions = append(conditions, "lower(a.actor_role) LIKE 'tech%'")
		} else {
			conditions = append(conditions, fmt.Sprintf("lower(a.actor_role) = $%d", argIdx))
			args = append(args, roleLower)
			argIdx++
		}
	}

	if filter.Action != "" {
		conditions = append(conditions, fmt.Sprintf("a.action = $%d", argIdx))
		args = append(args, strings.ToUpper(strings.TrimSpace(filter.Action)))
		argIdx++
	}

	if filter.IPAddress != "" {
		conditions = append(conditions, fmt.Sprintf("a.ip_address ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(filter.IPAddress)+"%")
		argIdx++
	}

	if filter.Status != "" {
		st := strings.ToLower(strings.TrimSpace(filter.Status))
		if st == "success" || st == "true" || st == "1" {
			conditions = append(conditions, "a.success = true")
		} else if st == "failed" || st == "failure" || st == "false" || st == "0" {
			conditions = append(conditions, "a.success = false")
		}
	}

	if filter.Search != "" {
		searchPattern := "%" + strings.TrimSpace(filter.Search) + "%"
		conditions = append(conditions, fmt.Sprintf("(a.description ILIKE $%d OR a.event ILIKE $%d OR a.ip_address ILIKE $%d OR COALESCE(u.name,'') ILIKE $%d OR COALESCE(u.username,'') ILIKE $%d)", argIdx, argIdx, argIdx, argIdx, argIdx))
		args = append(args, searchPattern)
		argIdx++
	}

	if filter.StartTime != nil && !filter.StartTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("a.occurred_at >= $%d", argIdx))
		args = append(args, *filter.StartTime)
		argIdx++
	}

	if filter.EndTime != nil && !filter.EndTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("a.occurred_at <= $%d", argIdx))
		args = append(args, *filter.EndTime)
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
		SELECT 
			a.id,
			COALESCE(a.actor_id::text, ''),
			a.actor_role,
			COALESCE(u.name, ''),
			COALESCE(u.username, ''),
			COALESCE(u.email, ''),
			COALESCE(u.phone, ''),
			a.event,
			a.action,
			a.resource_type,
			a.resource_id,
			a.description,
			a.success,
			a.request_id,
			a.ip_address,
			a.user_agent,
			a.metadata,
			a.occurred_at,
			count(*) OVER() AS total_count
		FROM audit_events a
		LEFT JOIN users u ON u.id = a.actor_id
		%s
		ORDER BY a.occurred_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)

	args = append(args, limit, offset)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]EventView, 0)
	total := 0

	for rows.Next() {
		var item EventView
		var metaBytes []byte
		var success bool
		var at time.Time

		err := rows.Scan(
			&item.ID,
			&item.ActorID,
			&item.ActorRole,
			&item.ActorName,
			&item.ActorUsername,
			&item.ActorEmail,
			&item.ActorPhone,
			&item.Event,
			&item.Action,
			&item.ResourceType,
			&item.ResourceID,
			&item.Description,
			&success,
			&item.RequestID,
			&item.IPAddress,
			&item.UserAgent,
			&metaBytes,
			&at,
			&total,
		)
		if err != nil {
			return nil, 0, err
		}

		item.UserID = item.ActorID
		item.Success = success
		if success {
			item.Status = "SUCCESS"
		} else {
			item.Status = "FAILED"
		}

		item.UserCategory = UserCategory(item.ActorRole)
		item.OccurredAt = at
		item.Time = at.UTC().Format("2006-01-02 15:04:05 UTC")

		if len(metaBytes) > 0 {
			_ = json.Unmarshal(metaBytes, &item.Metadata)
		}
		if item.Metadata == nil {
			item.Metadata = make(map[string]interface{})
		}

		items = append(items, item)
	}

	return items, total, rows.Err()
}

func (s *Store) Export(ctx context.Context, kind string, start, end time.Time, createdBy string) (string, int, error) {
	if !end.After(start) {
		return "", 0, fmt.Errorf("archive end must be after start")
	}
	if err := os.MkdirAll(s.Root, 0750); err != nil {
		return "", 0, err
	}
	name := archiveName(kind, start)
	path := filepath.Join(s.Root, name)
	rows, err := s.DB.QueryContext(ctx, `SELECT id,COALESCE(actor_id::text,''),actor_role,event,action,resource_type,resource_id,description,success,request_id,ip_address,user_agent,metadata,occurred_at FROM audit_events WHERE occurred_at >= $1 AND occurred_at < $2 ORDER BY occurred_at,id`, start, end)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", 0, err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"id", "actor_id", "actor_role", "event", "action", "resource_type", "resource_id", "description", "success", "request_id", "ip_address", "user_agent", "metadata", "occurred_at"})
	count := 0
	for rows.Next() {
		var values [12]string
		var success bool
		var meta []byte
		var at time.Time
		if err := rows.Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6], &values[7], &success, &values[8], &values[9], &values[10], &meta, &at); err != nil {
			f.Close()
			return "", count, err
		}
		_ = w.Write([]string{values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7], strconv.FormatBool(success), values[8], values[9], values[10], string(meta), at.UTC().Format(time.RFC3339Nano)})
		count++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return "", count, err
	}
	if err := f.Close(); err != nil {
		return "", count, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", count, err
	}
	sum := sha256.Sum256(data)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO audit_archives(id,period_type,period_start,period_end,file_name,storage_path,checksum,event_count,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid) ON CONFLICT(period_type,period_start,period_end) DO UPDATE SET file_name=EXCLUDED.file_name,storage_path=EXCLUDED.storage_path,checksum=EXCLUDED.checksum,event_count=EXCLUDED.event_count,created_at=now()`, uuid.NewString(), strings.ToUpper(kind), start, end, name, path, hex.EncodeToString(sum[:]), count, createdBy)
	return path, count, err
}

func archiveName(kind string, start time.Time) string {
	switch strings.ToUpper(kind) {
	case "DAILY":
		return start.Format("20060102") + "D.csv"
	case "WEEKLY":
		return start.Format("20060102") + "W.csv"
	case "MONTHLY":
		return start.Format("200601") + "M.csv"
	case "YEARLY":
		return start.Format("2006") + "Y.csv"
	default:
		return start.Format("20060102") + "-custom.csv"
	}
}

func (s *Store) DeleteCovered(ctx context.Context, start, end time.Time, event string) (int64, error) {
	var covered bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM audit_archives WHERE period_start <= $1 AND period_end >= $2 AND checksum<>'')`, start, end).Scan(&covered); err != nil {
		return 0, err
	}
	if !covered {
		return 0, fmt.Errorf("audit deletion requires a completed archive covering the date range")
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM audit_events WHERE occurred_at >= $1 AND occurred_at < $2 AND ($3='' OR event=$3)`, start, end, event)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
