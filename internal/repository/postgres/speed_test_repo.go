package postgres

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type SpeedTestRepository struct{ reader, writer *sql.DB }

func NewSpeedTestRepository() (*SpeedTestRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &SpeedTestRepository{reader: reader, writer: writer}, nil
}

func (r *SpeedTestRepository) ISPOptions(county, town string) ([]*models.SpeedTestISPOption, error) {
	rows, err := r.reader.Query(`
		SELECT DISTINCT i.id,i.name,i.county
		FROM isps i JOIN users u ON u.id=i.id
		LEFT JOIN isp_serviceability s ON s.isp_id=i.id AND s.active=true
		WHERE i.is_active=true AND u.status='active'
		  AND ($1='' OR lower(i.county)=lower($1) OR lower(s.county)=lower($1))
		  AND ($2='' OR lower(s.town)=lower($2) OR lower(i.sub_county)=lower($2))
		ORDER BY i.name LIMIT 100`, strings.TrimSpace(county), strings.TrimSpace(town))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.SpeedTestISPOption, 0)
	for rows.Next() {
		item := &models.SpeedTestISPOption{}
		if err := rows.Scan(&item.ID, &item.Name, &item.County); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SpeedTestRepository) Create(item *models.SpeedTest, ip string) error {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.TestSessionID == "" {
		item.TestSessionID = uuid.NewString()
	}
	item.County = strings.TrimSpace(item.County)
	item.Town = strings.TrimSpace(item.Town)
	item.ISPName = strings.TrimSpace(item.ISPName)
	if item.ISPID != "" {
		var registeredName string
		if err := r.reader.QueryRow(`SELECT i.name FROM isps i JOIN users u ON u.id=i.id WHERE i.id=$1 AND i.is_active=true AND u.status='active'`, item.ISPID).Scan(&registeredName); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("selected ISP is not available")
			}
			return err
		}
		item.ISPSelectionSource = "registered"
		if item.ISPName == "" {
			item.ISPName = registeredName
		}
	} else if item.ISPName != "" {
		item.ISPSelectionSource = "user_entered"
	} else {
		item.ISPSelectionSource = "none"
	}
	_, err := r.writer.Exec(`INSERT INTO speed_tests
		(id,test_session_id,user_id,isp_id,isp_name,isp_selection_source,county,town,download_mbps,upload_mbps,latency_ms,jitter_ms,packet_loss_percent,isp_plan_speed_mbps,include_in_public_stats,ip_address,status)
		VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::inet,'pending')`,
		item.ID, item.TestSessionID, item.UserID, item.ISPID, item.ISPName, item.ISPSelectionSource, item.County, item.Town, item.DownloadMbps, item.UploadMbps, item.LatencyMs, item.JitterMs, item.PacketLossPercent, item.ISPPlanSpeedMbps, item.IncludeInPublicStats, ip)
	return err
}

func (r *SpeedTestRepository) ListMine(userID string, limit int) ([]*models.SpeedTest, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.reader.Query(`SELECT id,test_session_id,COALESCE(isp_id::text,''),isp_name,isp_selection_source,county,town,download_mbps,upload_mbps,latency_ms,jitter_ms,packet_loss_percent,isp_plan_speed_mbps,include_in_public_stats,status,rejection_reason,created_at FROM speed_tests WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSpeedTests(rows)
}

func scanSpeedTests(rows *sql.Rows) ([]*models.SpeedTest, error) {
	items := make([]*models.SpeedTest, 0)
	for rows.Next() {
		item := &models.SpeedTest{}
		if err := rows.Scan(&item.ID, &item.TestSessionID, &item.ISPID, &item.ISPName, &item.ISPSelectionSource, &item.County, &item.Town, &item.DownloadMbps, &item.UploadMbps, &item.LatencyMs, &item.JitterMs, &item.PacketLossPercent, &item.ISPPlanSpeedMbps, &item.IncludeInPublicStats, &item.Status, &item.RejectionReason, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SpeedTestRepository) Stats(ispID, county, town string) (*models.SpeedTestStats, error) {
	stats := &models.SpeedTestStats{ISPID: ispID, County: strings.TrimSpace(county), Town: strings.TrimSpace(town)}
	err := r.reader.QueryRow(`SELECT COALESCE((SELECT name FROM isps WHERE id=$1),''),COUNT(*) FILTER(WHERE status='accepted'),COUNT(*) FILTER(WHERE status='pending'),COUNT(*) FILTER(WHERE status IN ('suspicious','rejected')),COALESCE(AVG(download_mbps) FILTER(WHERE status='accepted'),0),COALESCE(AVG(upload_mbps) FILTER(WHERE status='accepted'),0),COALESCE(AVG(latency_ms) FILTER(WHERE status='accepted'),0),COALESCE(AVG(jitter_ms) FILTER(WHERE status='accepted'),0),COALESCE(AVG(packet_loss_percent) FILTER(WHERE status='accepted'),0) FROM speed_tests WHERE isp_id=$1 AND ($2='' OR lower(county)=lower($2)) AND ($3='' OR lower(town)=lower($3))`, ispID, stats.County, stats.Town).Scan(&stats.ISPName, &stats.AcceptedCount, &stats.PendingCount, &stats.SuspiciousCount, &stats.AverageDownload, &stats.AverageUpload, &stats.AverageLatency, &stats.AverageJitter, &stats.AveragePacketLoss)
	return stats, err
}

func (r *SpeedTestRepository) ListModeration(status string, limit int) ([]*models.SpeedTestModerationItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.reader.Query(`SELECT id,test_session_id,COALESCE(user_id::text,''),COALESCE(isp_id::text,''),isp_name,isp_selection_source,county,town,download_mbps,upload_mbps,latency_ms,jitter_ms,packet_loss_percent,isp_plan_speed_mbps,include_in_public_stats,status,rejection_reason,created_at,ip_address::text FROM speed_tests WHERE ($1='' OR status=$1) ORDER BY created_at ASC LIMIT $2`, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.SpeedTestModerationItem, 0)
	for rows.Next() {
		item := &models.SpeedTestModerationItem{}
		if err := rows.Scan(&item.ID, &item.TestSessionID, &item.UserID, &item.ISPID, &item.ISPName, &item.ISPSelectionSource, &item.County, &item.Town, &item.DownloadMbps, &item.UploadMbps, &item.LatencyMs, &item.JitterMs, &item.PacketLossPercent, &item.ISPPlanSpeedMbps, &item.IncludeInPublicStats, &item.Status, &item.RejectionReason, &item.CreatedAt, &item.IP); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SpeedTestRepository) Moderate(id, status, reason string) error {
	if status != "accepted" && status != "rejected" && status != "suspicious" {
		return errors.New("invalid moderation status")
	}
	result, err := r.writer.Exec(`UPDATE speed_tests SET status=$1,rejection_reason=$2 WHERE id=$3`, status, strings.TrimSpace(reason), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *SpeedTestRepository) ExistsSession(sessionID, userID string) (bool, error) {
	var exists bool
	err := r.reader.QueryRow(`SELECT EXISTS(SELECT 1 FROM speed_tests WHERE test_session_id=$1 AND COALESCE(user_id::text,'')=COALESCE(NULLIF($2,''),''))`, sessionID, userID).Scan(&exists)
	return exists, err
}
