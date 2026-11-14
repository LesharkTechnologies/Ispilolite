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

	// Expire overdue vacancies and service requests every hour.
	worklife.StartDeadlineScheduler(ctx, db, logger)
	search.StartDefaultSyncer(ctx)

	// Roll up audit events into dated archives.
	audit.Configure(db, os.Getenv("AUDIT_ARCHIVE_ROOT")).StartScheduler(ctx, time.Local, logger)

	// Expire stale provider matches so a request can be re-matched.
	if matcher != nil {
		matcher.StartExpiryWorker(ctx, time.Hour, 24*time.Hour)
	}
}
