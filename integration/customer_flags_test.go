package integration_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	"ispilolite/pkg/database"
)

// TestCustomerFlagApprovalSuspendsAccount proves the trust-and-safety path end
// to end: report -> duplicate protection -> admin approval -> account suspended
// -> monitoring entry -> watched activity -> release.
func TestCustomerFlagApprovalSuspendsAccount(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("DATABASE_READ_URL", dsn)
	t.Setenv("SKIP_MIGRATIONS", "true")
	if err := database.InitDB("../config/config.yaml"); err != nil {
		t.Fatal(err)
	}
	defer database.CloseDB()

	users, err := postgres.NewUserRepo()
	if err != nil {
		t.Fatal(err)
	}
	flags, err := postgres.NewFlagRepository()
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	suffix := now.Format("05040502")
	makeUser := func(role, phonePrefix string) *models.User {
		user := &models.User{
			ID: uuid.NewString(), Phone: "+2547" + phonePrefix + suffix,
			Name: "CI flag " + role, Role: role,
			Joined: now, CreatedAt: now, UpdatedAt: now, Locale: "en",
		}
		if err := users.CreateUser(user); err != nil {
			t.Fatalf("create %s: %v", role, err)
		}
		return user
	}
	reporter := makeUser("customer", "1")
	reported := makeUser("customer", "2")
	admin := makeUser("admin", "3")
	defer func() {
		_, _ = database.GetWriter().Exec(`DELETE FROM customer_flags WHERE reporter_id=$1`, reporter.ID)
		for _, id := range []string{reported.ID, admin.ID, reporter.ID} {
			_, _ = database.GetWriter().Exec(`DELETE FROM users WHERE id=$1`, id)
		}
	}()

	reportedID := reported.ID
	item := &models.CustomerFlag{
		ID:            uuid.NewString(),
		ReporterID:    reporter.ID,
		ReportedUser:  &reportedID,
		Category:      "harassment",
		Description:   "Repeated abusive messages after a cancelled installation job.",
		ImageURLs:     []string{},
		Status:        models.FlagPending,
		AccountAction: models.FlagActionNone,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := flags.CreateFlag(context.Background(), item); err != nil {
		t.Fatalf("create flag: %v", err)
	}

	duplicate := *item
	duplicate.ID = uuid.NewString()
	if err := flags.CreateFlag(context.Background(), &duplicate); !errors.Is(err, postgres.ErrFlagDuplicate) {
		t.Fatalf("expected duplicate protection, got %v", err)
	}

	if err := flags.ReviewFlag(context.Background(), item.ID, admin.ID,
		models.FlagApproved, "Confirmed by admin review.", models.FlagActionSuspend); err != nil {
		t.Fatalf("approve flag: %v", err)
	}

	updated, err := users.GetUserByID(reported.ID)
	if err != nil {
		t.Fatalf("reload reported user: %v", err)
	}
	if updated.Status != "suspended" {
		t.Fatalf("expected suspended account, got status %q", updated.Status)
	}

	monitored, err := flags.ListMonitored(context.Background(), true, 50)
	if err != nil {
		t.Fatalf("list monitored: %v", err)
	}
	found := false
	for _, entry := range monitored {
		if entry.UserID == reported.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("reported user is missing from the monitoring list")
	}

	if err := flags.RecordActivity(context.Background(), reported.ID,
		"job_application", "job", "job-ci-1", map[string]interface{}{"source": "integration"}, "127.0.0.1"); err != nil {
		t.Fatalf("record activity: %v", err)
	}
	activity, err := flags.ListActivity(context.Background(), reported.ID, 10)
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}
	if len(activity) != 1 || activity[0].Event != "job_application" {
		t.Fatalf("expected one job_application event, got %+v", activity)
	}

	// Non-monitored users must not accumulate activity rows.
	if err := flags.RecordActivity(context.Background(), admin.ID,
		"client_view", "job", "job-ci-2", nil, "127.0.0.1"); err != nil {
		t.Fatalf("record activity for unmonitored user: %v", err)
	}
	none, err := flags.ListActivity(context.Background(), admin.ID, 10)
	if err != nil {
		t.Fatalf("list activity for unmonitored user: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("unmonitored user should have no activity rows, got %d", len(none))
	}

	if err := flags.ReleaseMonitoring(context.Background(), reported.ID, admin.ID); err != nil {
		t.Fatalf("release monitoring: %v", err)
	}
}
