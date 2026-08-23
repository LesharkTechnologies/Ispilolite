package worklife

import (
	"context"
	"database/sql"
	"log"
	"time"
)

func StartDeadlineScheduler(ctx context.Context, db *sql.DB, logger *log.Logger) {
	if logger == nil {
		logger = log.Default()
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := db.ExecContext(ctx, `UPDATE isp_vacancies SET status='expired',updated_at=now() WHERE status='open' AND deadline_at IS NOT NULL AND deadline_at<=now()`); err != nil {
					logger.Printf("vacancy expiry failed: %v", err)
				}
				if _, err := db.ExecContext(ctx, `UPDATE service_requests SET status='cancelled',is_available=false,terminated_at=now(),termination_reason='deadline expired',updated_at=now() WHERE status IN ('open','pending','assigned','accepted','in_progress') AND deadline_at IS NOT NULL AND deadline_at<=now() AND terminated_at IS NULL`); err != nil {
					logger.Printf("request deadline expiry failed: %v", err)
				}
			}
		}
	}()
}
