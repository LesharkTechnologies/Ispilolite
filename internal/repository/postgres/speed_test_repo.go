package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

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
	if err == nil {
		now := time.Now().UTC()
		go func(y, m int) {
			_ = r.AggregateMonthlySpeedStats(context.Background(), y, m)
		}(now.Year(), int(now.Month()))
	}
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

func (r *SpeedTestRepository) GetByID(ctx context.Context, id, userID string) (*models.SpeedTest, error) {
	item := &models.SpeedTest{}
	err := r.reader.QueryRowContext(ctx, `SELECT id,test_session_id,COALESCE(isp_id::text,''),isp_name,isp_selection_source,county,town,download_mbps,upload_mbps,latency_ms,jitter_ms,packet_loss_percent,isp_plan_speed_mbps,include_in_public_stats,status,rejection_reason,created_at FROM speed_tests WHERE id=$1 AND ($2='' OR user_id=NULLIF($2,'')::uuid)`, id, userID).Scan(&item.ID, &item.TestSessionID, &item.ISPID, &item.ISPName, &item.ISPSelectionSource, &item.County, &item.Town, &item.DownloadMbps, &item.UploadMbps, &item.LatencyMs, &item.JitterMs, &item.PacketLossPercent, &item.ISPPlanSpeedMbps, &item.IncludeInPublicStats, &item.Status, &item.RejectionReason, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	item.UserID = userID
	return item, nil
}

func (r *SpeedTestRepository) Delete(ctx context.Context, id, userID string) error {
	res, err := r.writer.ExecContext(ctx, `DELETE FROM speed_tests WHERE id=$1 AND ($2='' OR user_id=NULLIF($2,'')::uuid)`, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
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

func (r *SpeedTestRepository) AggregateMonthlySpeedStats(ctx context.Context, year, month int) error {
	now := time.Now().UTC()
	if year <= 0 {
		year = now.Year()
	}
	if month <= 0 || month > 12 {
		month = int(now.Month())
	}

	tx, err := r.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Aggregate per county, town, and ISP
	_, err = tx.ExecContext(ctx, `
		INSERT INTO county_monthly_speed_stats (
			id, year, month, county, town, isp_id, isp_name,
			sample_count, min_download_mbps, max_download_mbps, avg_download_mbps,
			min_upload_mbps, max_upload_mbps, avg_upload_mbps, avg_latency_ms,
			created_at, updated_at
		)
		SELECT
			gen_random_uuid(),
			$1, $2,
			st.county,
			st.town,
			st.isp_id,
			COALESCE(MAX(NULLIF(st.isp_name,'')),''),
			COUNT(*),
			COALESCE(MIN(st.download_mbps), 0),
			COALESCE(MAX(st.download_mbps), 0),
			COALESCE(ROUND(AVG(st.download_mbps), 2), 0),
			COALESCE(MIN(st.upload_mbps), 0),
			COALESCE(MAX(st.upload_mbps), 0),
			COALESCE(ROUND(AVG(st.upload_mbps), 2), 0),
			COALESCE(ROUND(AVG(st.latency_ms), 2), 0),
			NOW(), NOW()
		FROM speed_tests st
		WHERE EXTRACT(YEAR FROM st.created_at) = $1
		  AND EXTRACT(MONTH FROM st.created_at) = $2
		  AND st.status IN ('accepted', 'pending')
		  AND trim(st.county) <> ''
		GROUP BY st.county, st.town, st.isp_id
		ON CONFLICT (year, month, lower(county), lower(town), COALESCE(isp_id, '00000000-0000-0000-0000-000000000000'::uuid))
		DO UPDATE SET
			isp_name = EXCLUDED.isp_name,
			sample_count = EXCLUDED.sample_count,
			min_download_mbps = EXCLUDED.min_download_mbps,
			max_download_mbps = EXCLUDED.max_download_mbps,
			avg_download_mbps = EXCLUDED.avg_download_mbps,
			min_upload_mbps = EXCLUDED.min_upload_mbps,
			max_upload_mbps = EXCLUDED.max_upload_mbps,
			avg_upload_mbps = EXCLUDED.avg_upload_mbps,
			avg_latency_ms = EXCLUDED.avg_latency_ms,
			updated_at = NOW()`, year, month)
	if err != nil {
		return fmt.Errorf("aggregate isp stats: %w", err)
	}

	// 2. Aggregate per county and town across all providers (isp_id = NULL)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO county_monthly_speed_stats (
			id, year, month, county, town, isp_id, isp_name,
			sample_count, min_download_mbps, max_download_mbps, avg_download_mbps,
			min_upload_mbps, max_upload_mbps, avg_upload_mbps, avg_latency_ms,
			created_at, updated_at
		)
		SELECT
			gen_random_uuid(),
			$1, $2,
			st.county,
			st.town,
			NULL,
			'All Providers',
			COUNT(*),
			COALESCE(MIN(st.download_mbps), 0),
			COALESCE(MAX(st.download_mbps), 0),
			COALESCE(ROUND(AVG(st.download_mbps), 2), 0),
			COALESCE(MIN(st.upload_mbps), 0),
			COALESCE(MAX(st.upload_mbps), 0),
			COALESCE(ROUND(AVG(st.upload_mbps), 2), 0),
			COALESCE(ROUND(AVG(st.latency_ms), 2), 0),
			NOW(), NOW()
		FROM speed_tests st
		WHERE EXTRACT(YEAR FROM st.created_at) = $1
		  AND EXTRACT(MONTH FROM st.created_at) = $2
		  AND st.status IN ('accepted', 'pending')
		  AND trim(st.county) <> ''
		GROUP BY st.county, st.town
		ON CONFLICT (year, month, lower(county), lower(town), COALESCE(isp_id, '00000000-0000-0000-0000-000000000000'::uuid))
		DO UPDATE SET
			isp_name = 'All Providers',
			sample_count = EXCLUDED.sample_count,
			min_download_mbps = EXCLUDED.min_download_mbps,
			max_download_mbps = EXCLUDED.max_download_mbps,
			avg_download_mbps = EXCLUDED.avg_download_mbps,
			min_upload_mbps = EXCLUDED.min_upload_mbps,
			max_upload_mbps = EXCLUDED.max_upload_mbps,
			avg_upload_mbps = EXCLUDED.avg_upload_mbps,
			avg_latency_ms = EXCLUDED.avg_latency_ms,
			updated_at = NOW()`, year, month)
	if err != nil {
		return fmt.Errorf("aggregate town stats: %w", err)
	}

	// 3. Aggregate overall county stats (town = '' and isp_id = NULL)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO county_monthly_speed_stats (
			id, year, month, county, town, isp_id, isp_name,
			sample_count, min_download_mbps, max_download_mbps, avg_download_mbps,
			min_upload_mbps, max_upload_mbps, avg_upload_mbps, avg_latency_ms,
			created_at, updated_at
		)
		SELECT
			gen_random_uuid(),
			$1, $2,
			st.county,
			'',
			NULL,
			'County Overall',
			COUNT(*),
			COALESCE(MIN(st.download_mbps), 0),
			COALESCE(MAX(st.download_mbps), 0),
			COALESCE(ROUND(AVG(st.download_mbps), 2), 0),
			COALESCE(MIN(st.upload_mbps), 0),
			COALESCE(MAX(st.upload_mbps), 0),
			COALESCE(ROUND(AVG(st.upload_mbps), 2), 0),
			COALESCE(ROUND(AVG(st.latency_ms), 2), 0),
			NOW(), NOW()
		FROM speed_tests st
		WHERE EXTRACT(YEAR FROM st.created_at) = $1
		  AND EXTRACT(MONTH FROM st.created_at) = $2
		  AND st.status IN ('accepted', 'pending')
		  AND trim(st.county) <> ''
		GROUP BY st.county
		ON CONFLICT (year, month, lower(county), lower(town), COALESCE(isp_id, '00000000-0000-0000-0000-000000000000'::uuid))
		DO UPDATE SET
			isp_name = 'County Overall',
			sample_count = EXCLUDED.sample_count,
			min_download_mbps = EXCLUDED.min_download_mbps,
			max_download_mbps = EXCLUDED.max_download_mbps,
			avg_download_mbps = EXCLUDED.avg_download_mbps,
			min_upload_mbps = EXCLUDED.min_upload_mbps,
			max_upload_mbps = EXCLUDED.max_upload_mbps,
			avg_upload_mbps = EXCLUDED.avg_upload_mbps,
			avg_latency_ms = EXCLUDED.avg_latency_ms,
			updated_at = NOW()`, year, month)
	if err != nil {
		return fmt.Errorf("aggregate county stats: %w", err)
	}

	return tx.Commit()
}

func (r *SpeedTestRepository) GetCountyMonthlyStats(ctx context.Context, county, town string, year, month int) (*models.CountyMonthlySpeedStats, error) {
	county = strings.TrimSpace(county)
	town = strings.TrimSpace(town)
	now := time.Now().UTC()
	if year <= 0 {
		year = now.Year()
	}
	if month <= 0 || month > 12 {
		month = int(now.Month())
	}

	stats := &models.CountyMonthlySpeedStats{
		Year:   year,
		Month:  month,
		County: county,
		Town:   town,
	}

	// 1. Try querying pre-aggregated table
	query := `
		SELECT id, year, month, county, town, COALESCE(isp_id::text,''), isp_name,
		       sample_count, min_download_mbps, max_download_mbps, avg_download_mbps,
		       min_upload_mbps, max_upload_mbps, avg_upload_mbps, avg_latency_ms,
		       created_at, updated_at
		FROM county_monthly_speed_stats
		WHERE lower(county) = lower($1)
		  AND ($2 = '' OR lower(town) = lower($2))
		  AND year = $3 AND month = $4
		ORDER BY (CASE WHEN lower(town) = lower($2) THEN 0 ELSE 1 END), sample_count DESC
		LIMIT 1`

	row := r.reader.QueryRowContext(ctx, query, county, town, year, month)
	err := row.Scan(&stats.ID, &stats.Year, &stats.Month, &stats.County, &stats.Town, &stats.ISPID, &stats.ISPName,
		&stats.SampleCount, &stats.MinDownloadMbps, &stats.MaxDownloadMbps, &stats.AvgDownloadMbps,
		&stats.MinUploadMbps, &stats.MaxUploadMbps, &stats.AvgUploadMbps, &stats.AvgLatencyMs,
		&stats.CreatedAt, &stats.UpdatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		// 2. Fall back to on-the-fly aggregation from raw speed_tests table
		liveQuery := `
			SELECT
				COUNT(*),
				COALESCE(MIN(download_mbps), 0),
				COALESCE(MAX(download_mbps), 0),
				COALESCE(ROUND(AVG(download_mbps), 2), 0),
				COALESCE(MIN(upload_mbps), 0),
				COALESCE(MAX(upload_mbps), 0),
				COALESCE(ROUND(AVG(upload_mbps), 2), 0),
				COALESCE(ROUND(AVG(latency_ms), 2), 0)
			FROM speed_tests
			WHERE ($1 = '' OR lower(county) = lower($1))
			  AND ($2 = '' OR lower(town) = lower($2))
			  AND ($3 = 0 OR EXTRACT(YEAR FROM created_at) = $3)
			  AND ($4 = 0 OR EXTRACT(MONTH FROM created_at) = $4)
			  AND status IN ('accepted', 'pending')`

		liveRow := r.reader.QueryRowContext(ctx, liveQuery, county, town, year, month)
		if scanErr := liveRow.Scan(&stats.SampleCount, &stats.MinDownloadMbps, &stats.MaxDownloadMbps,
			&stats.AvgDownloadMbps, &stats.MinUploadMbps, &stats.MaxUploadMbps,
			&stats.AvgUploadMbps, &stats.AvgLatencyMs); scanErr != nil {
			return nil, scanErr
		}
		stats.ID = uuid.NewString()
		stats.CreatedAt = now
		stats.UpdatedAt = now
	} else if err != nil {
		return nil, err
	}

	// 3. Build human-friendly customer recommendation summary
	if stats.SampleCount > 0 {
		locationLabel := stats.County
		if stats.Town != "" {
			locationLabel = fmt.Sprintf("%s, %s", stats.Town, stats.County)
		}
		monthName := time.Month(stats.Month).String()
		if stats.MinDownloadMbps == stats.MaxDownloadMbps {
			stats.RecommendationSummary = fmt.Sprintf("%s internet speeds average %.1f Mbps based on %d customer test(s) in %s %d.",
				locationLabel, stats.AvgDownloadMbps, stats.SampleCount, monthName, stats.Year)
		} else {
			stats.RecommendationSummary = fmt.Sprintf("%s internet speeds range from %.1f Mbps to %.1f Mbps, averaging %.1f Mbps based on %d customer speed test(s) in %s %d.",
				locationLabel, stats.MinDownloadMbps, stats.MaxDownloadMbps, stats.AvgDownloadMbps, stats.SampleCount, monthName, stats.Year)
		}
	} else {
		stats.RecommendationSummary = fmt.Sprintf("No speed tests recorded yet for %s in %s %d.", county, time.Month(month).String(), year)
	}

	return stats, nil
}

func (r *SpeedTestRepository) ListCountyBenchmarks(ctx context.Context, year, month int) ([]*models.CountySpeedBenchmark, error) {
	now := time.Now().UTC()
	if year <= 0 {
		year = now.Year()
	}
	if month <= 0 || month > 12 {
		month = int(now.Month())
	}

	// 1. Try pre-aggregated table
	rows, err := r.reader.QueryContext(ctx, `
		SELECT county, sample_count, min_download_mbps, max_download_mbps, avg_download_mbps, avg_upload_mbps, avg_latency_ms
		FROM county_monthly_speed_stats
		WHERE year = $1 AND month = $2 AND town = '' AND isp_id IS NULL
		ORDER BY avg_download_mbps DESC`, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*models.CountySpeedBenchmark, 0)
	for rows.Next() {
		b := &models.CountySpeedBenchmark{}
		if err := rows.Scan(&b.County, &b.SampleCount, &b.MinDownloadMbps, &b.MaxDownloadMbps, &b.AvgDownloadMbps, &b.AvgUploadMbps, &b.AvgLatencyMs); err != nil {
			return nil, err
		}
		items = append(items, b)
	}

	// 2. If pre-aggregated table has no rows for this month, aggregate live from speed_tests
	if len(items) == 0 {
		liveRows, err := r.reader.QueryContext(ctx, `
			SELECT
				county,
				COUNT(*),
				COALESCE(MIN(download_mbps), 0),
				COALESCE(MAX(download_mbps), 0),
				COALESCE(ROUND(AVG(download_mbps), 2), 0),
				COALESCE(ROUND(AVG(upload_mbps), 2), 0),
				COALESCE(ROUND(AVG(latency_ms), 2), 0)
			FROM speed_tests
			WHERE ($1 = 0 OR EXTRACT(YEAR FROM created_at) = $1)
			  AND ($2 = 0 OR EXTRACT(MONTH FROM created_at) = $2)
			  AND status IN ('accepted', 'pending')
			  AND trim(county) <> ''
			GROUP BY county
			ORDER BY AVG(download_mbps) DESC`, year, month)
		if err != nil {
			return nil, err
		}
		defer liveRows.Close()

		for liveRows.Next() {
			b := &models.CountySpeedBenchmark{}
			if err := liveRows.Scan(&b.County, &b.SampleCount, &b.MinDownloadMbps, &b.MaxDownloadMbps, &b.AvgDownloadMbps, &b.AvgUploadMbps, &b.AvgLatencyMs); err != nil {
				return nil, err
			}
			items = append(items, b)
		}
	}

	return items, nil
}

func (r *SpeedTestRepository) ListTownStats(ctx context.Context, county string, year, month int) ([]*models.CountyMonthlySpeedStats, error) {
	now := time.Now().UTC()
	if year <= 0 {
		year = now.Year()
	}
	if month <= 0 || month > 12 {
		month = int(now.Month())
	}

	rows, err := r.reader.QueryContext(ctx, `
		SELECT id, year, month, county, town, COALESCE(isp_id::text,''), isp_name,
		       sample_count, min_download_mbps, max_download_mbps, avg_download_mbps,
		       min_upload_mbps, max_upload_mbps, avg_upload_mbps, avg_latency_ms,
		       created_at, updated_at
		FROM county_monthly_speed_stats
		WHERE lower(county) = lower($1) AND town <> '' AND isp_id IS NULL
		  AND year = $2 AND month = $3
		ORDER BY avg_download_mbps DESC`, strings.TrimSpace(county), year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*models.CountyMonthlySpeedStats, 0)
	for rows.Next() {
		stats := &models.CountyMonthlySpeedStats{}
		if err := rows.Scan(&stats.ID, &stats.Year, &stats.Month, &stats.County, &stats.Town, &stats.ISPID, &stats.ISPName,
			&stats.SampleCount, &stats.MinDownloadMbps, &stats.MaxDownloadMbps, &stats.AvgDownloadMbps,
			&stats.MinUploadMbps, &stats.MaxUploadMbps, &stats.AvgUploadMbps, &stats.AvgLatencyMs,
			&stats.CreatedAt, &stats.UpdatedAt); err != nil {
			return nil, err
		}
		monthName := time.Month(stats.Month).String()
		stats.RecommendationSummary = fmt.Sprintf("%s (%s) speeds range from %.1f Mbps to %.1f Mbps, averaging %.1f Mbps based on %d test(s) in %s %d.",
			stats.Town, stats.County, stats.MinDownloadMbps, stats.MaxDownloadMbps, stats.AvgDownloadMbps, stats.SampleCount, monthName, stats.Year)
		items = append(items, stats)
	}

	return items, nil
}
