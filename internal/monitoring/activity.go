// Package monitoring records watched activity for users on the trust-and-safety
// monitoring list. Recording is best effort: it never fails the parent request
// and is bounded by a short write timeout. Users who are not monitored pay only
// a single indexed EXISTS probe inside the insert.
package monitoring

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"ispilolite/internal/middleware"
	"ispilolite/pkg/database"
)

// Events recorded for monitored users.
const (
	EventJobApplication = "job_application"
	EventJobClaim       = "job_claim"
	EventClientView     = "client_view"
)

const recordTimeout = 2 * time.Second

// Record stores one watched activity for the authenticated caller. It must be
// called synchronously from the handler that performed the action, after the
// action succeeded, so the trail reflects real behaviour only.
func Record(r *http.Request, event, subjectType, subjectID string, metadata map[string]interface{}) {
	userID, _ := r.Context().Value("userID").(string)
	RecordFor(r, userID, event, subjectType, subjectID, metadata)
}

// RecordFor records activity for an explicit user, used when the watched user
// is not the caller (for example an ISP assigning a technician to a job).
func RecordFor(r *http.Request, userID, event, subjectType, subjectID string, metadata map[string]interface{}) {
	if userID == "" {
		return
	}
	writer := database.GetWriter()
	if writer == nil {
		return
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return
	}
	// WithoutCancel keeps the write alive if the client disconnects mid-response.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), recordTimeout)
	defer cancel()
	if _, err := writer.ExecContext(ctx, `
		INSERT INTO monitored_activity (user_id, event, subject_type, subject_id, metadata, ip_address)
		SELECT $1,$2,$3,$4,$5,NULLIF($6,'')::inet
		WHERE EXISTS (SELECT 1 FROM user_monitoring WHERE user_id=$1 AND active)`,
		userID, event, subjectType, subjectID, encoded, middleware.ClientIP(r)); err != nil {
		log.Printf("monitoring: record %s for user %s failed: %v", event, userID, err)
	}
}
