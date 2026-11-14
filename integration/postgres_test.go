package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"ispilolite/api/dto"
	"ispilolite/internal/models"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/search"
	followsvc "ispilolite/internal/services/follow"
	"ispilolite/pkg/database"
)

// Run after cmd/migrate against an isolated empty database. CI sets
// TEST_DATABASE_URL; local tests skip when Postgres is unavailable.
func TestFreshDatabaseRegistrationAndSearch(t *testing.T) {
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
	repo, err := postgres.NewUserRepo()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	user := &models.User{ID: uuid.NewString(), Phone: "+2547" + now.Format("05040502"), Name: "CI customer", Role: "customer", Joined: now, CreatedAt: now, UpdatedAt: now, Locale: "en"}
	if err := repo.CreateUser(user); err != nil {
		t.Fatalf("fresh user registration failed: %v", err)
	}
	defer func() { _, _ = database.GetWriter().Exec(`DELETE FROM users WHERE id=$1`, user.ID) }()
	stored, err := repo.GetUserByID(user.ID)
	if err != nil || stored.ISP_ID != "" || stored.Status != "active" {
		t.Fatalf("reading customer with NULL isp_id failed: %+v, %v", stored, err)
	}
	searchRepo := search.NewPostgresRepository(database.GetReader())
	if _, err := searchRepo.SearchTechnicians(context.Background(), dto.SearchParams{Query: "CI", PageSize: 5}); err != nil {
		t.Fatalf("Postgres search fallback not wired: %v", err)
	}
	if _, err := searchRepo.SearchISPs(context.Background(), dto.SearchParams{Query: "CI", PageSize: 5}); err != nil {
		t.Fatalf("Postgres ISP search fallback not wired: %v", err)
	}
	if _, err := searchRepo.SearchLocations(context.Background(), dto.SearchParams{Query: "CI", PageSize: 5}); err != nil {
		t.Fatalf("Postgres location search fallback not wired: %v", err)
	}
}

func TestCompanyTechniciansAndCrossRoleFollows(t *testing.T) {
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
	now := time.Now().UTC()
	newUser := func(role string) string {
		t.Helper()
		id := uuid.NewString()
		phone := fmt.Sprintf("+2547%08d", time.Now().UnixNano()%100000000)
		user := &models.User{ID: id, Phone: phone, Name: "CI " + role, Role: role, Joined: now, CreatedAt: now, UpdatedAt: now, Locale: "en"}
		if err := users.CreateUser(user); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ispID := newUser("isp")
	otherISP := newUser("isp")
	independent := newUser("technician")
	employee1 := newUser("technician")
	employee2 := newUser("technician")
	customer := newUser("customer")
	defer func() {
		for _, id := range []string{customer, employee2, employee1, independent, otherISP, ispID} {
			_, _ = database.GetWriter().Exec(`DELETE FROM users WHERE id=$1`, id)
		}
	}()
	companies, err := postgres.NewCompanyRepository()
	if err != nil {
		t.Fatal(err)
	}
	first, err := companies.LinkTechnician(ispID, employee1)
	if err != nil || !strings.HasSuffix(first, "-TECH-0001") {
		t.Fatalf("first company link: %q, %v", first, err)
	}
	second, err := companies.LinkTechnician(ispID, employee2)
	if err != nil || !strings.HasSuffix(second, "-TECH-0002") {
		t.Fatalf("second company link: %q, %v", second, err)
	}
	if again, err := companies.LinkTechnician(ispID, employee1); err != nil || again != first {
		t.Fatalf("relink should preserve company ID: %q %v", again, err)
	}
	if _, err := companies.LinkTechnician(otherISP, employee1); !errors.Is(err, postgres.ErrTechnicianEmployed) {
		t.Fatalf("expected cross-company conflict, got %v", err)
	}
	team, err := companies.ListCompanyTechnicians(ispID)
	if err != nil || len(team) != 2 {
		t.Fatalf("company should show exactly 2 hired technicians, not the independent account: %d %v", len(team), err)
	}
	if err := companies.EndTechnicianLink(ispID, employee1, now); err != nil {
		t.Fatal(err)
	}
	if err := companies.EndTechnicianLink(ispID, employee1, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second end should report no active link: %v", err)
	}
	if _, err := companies.LinkTechnician(otherISP, employee1); err != nil {
		t.Fatalf("former employee could not join another ISP: %v", err)
	}
	followRepo, err := postgres.NewFollowRepository()
	if err != nil {
		t.Fatal(err)
	}
	follow := followsvc.NewService(followRepo)
	for _, edge := range [][2]string{{customer, independent}, {customer, ispID}, {ispID, employee2}, {independent, customer}} {
		created, err := follow.Follow(context.Background(), edge[0], edge[1])
		if err != nil || !created {
			t.Fatalf("follow %v failed: created=%v err=%v", edge, created, err)
		}
	}
	if created, err := follow.Follow(context.Background(), customer, independent); err != nil || created {
		t.Fatalf("duplicate follow not idempotent: %v %v", created, err)
	}
	if _, err := follow.Follow(context.Background(), customer, customer); !errors.Is(err, followsvc.ErrSelfFollow) {
		t.Fatalf("self-follow not rejected: %v", err)
	}
	summary, err := follow.Summary(context.Background(), customer, independent)
	if err != nil || summary.FollowingCount != 2 || summary.FollowerCount != 1 || !summary.IsFollowing {
		t.Fatalf("incorrect follow summary: %+v, %v", summary, err)
	}
	page, err := follow.Following(context.Background(), customer, 10, 0)
	if err != nil || len(page.Items) != 2 || page.Total != 2 {
		t.Fatalf("incorrect following list: %+v, %v", page, err)
	}
	if err := follow.Unfollow(context.Background(), customer, ispID); err != nil {
		t.Fatal(err)
	}
	if err := follow.Unfollow(context.Background(), customer, ispID); err != nil {
		t.Fatalf("unfollow not idempotent: %v", err)
	}
}
