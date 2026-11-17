// Package bootstrap wires the process-wide background workers that every API
// deployment must run, regardless of which entrypoint (cmd/auth or cmd/core)
// is deployed. Centralising them here removes the drift where one binary
// expires stale matches or overdue jobs/vacancies and the other silently does
// not.
package bootstrap

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/search"
	"ispilolite/internal/services/matching"
	"ispilolite/pkg/audit"
	"ispilolite/pkg/worklife"
)

// StartBackgroundWorkers launches the recurring workers on the supplied context.
// All workers stop when ctx is cancelled. A nil database or matcher is logged
// and skipped so the caller does not need to nil-check.
func StartBackgroundWorkers(ctx context.Context, matcher *matching.MatchingService, db *sql.DB, logger *log.Logger) {
	if logger == nil {
		logger = log.Default()
	}
	if db == nil {
		logger.Printf("background workers skipped: database is not initialized")
		return
	}

	// Ensure super admin account from environment variables exists with full system access.
	if err := EnsureAdminAccount(ctx, db, logger); err != nil {
		logger.Printf("admin account bootstrap warning: %v", err)
	}

	// Expire overdue vacancies and service requests every hour.
	worklife.StartDeadlineScheduler(ctx, db, logger)
	search.StartDefaultSyncer(ctx)

	// Roll up audit events into dated archives.
	audit.Configure(db, os.Getenv("AUDIT_ARCHIVE_ROOT")).StartScheduler(ctx, time.Local, logger)

	// Expire stale provider matches so a request can be re-matched.
	if matcher != nil {
		matcher.StartExpiryWorker(ctx, time.Hour, 24*time.Hour)
	}

	// Aggregate speed tests into monthly county and town statistics every 6 hours (and once at startup).
	go func() {
		speedRepo, err := postgres.NewSpeedTestRepository()
		if err != nil {
			logger.Printf("speed test background aggregator skipped: %v", err)
			return
		}
		runRollup := func() {
			now := time.Now().UTC()
			if err := speedRepo.AggregateMonthlySpeedStats(ctx, now.Year(), int(now.Month())); err != nil {
				logger.Printf("speed test monthly rollup failed: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
			runRollup()
		}
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runRollup()
			}
		}
	}()

	// Kenya Data Protection Act 2019 (KDPA): Daily purge of personal data whose 2-year retention window has elapsed.
	go func() {
		runRetentionPurge := func() {
			res, err := db.ExecContext(ctx, `
				UPDATE users
				SET phone='deleted-'||id::text,
					username='',
					name='Deleted User',
					email='',
					password_hash='',
					latitude=NULL,
					longitude=NULL,
					avatar_url='',
					updated_at=now()
				WHERE status='deleted' AND retention_until IS NOT NULL AND retention_until <= now()
			`)
			if err != nil {
				logger.Printf("kdpa retention purge worker failed: %v", err)
			} else if count, _ := res.RowsAffected(); count > 0 {
				logger.Printf("kdpa retention purge: anonymized %d expired user accounts", count)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
			runRetentionPurge()
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runRetentionPurge()
			}
		}
	}()
}
