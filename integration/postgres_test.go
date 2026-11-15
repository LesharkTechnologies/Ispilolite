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
	socialsvc "ispilolite/internal/services/social"
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
		var provisioned bool
		switch role {
		case "isp":
			if err := database.GetWriter().QueryRow(`SELECT EXISTS(SELECT 1 FROM isps WHERE id=$1)`, id).Scan(&provisioned); err != nil || !provisioned {
				t.Fatalf("ISP role profile was not provisioned transactionally: %v", err)
			}
		case "technician":
			if err := database.GetWriter().QueryRow(`SELECT EXISTS(SELECT 1 FROM technicians WHERE user_id=$1) AND EXISTS(SELECT 1 FROM technician_profiles WHERE technician_id=$1)`, id).Scan(&provisioned); err != nil || !provisioned {
				t.Fatalf("technician search/profile rows were not provisioned: %v", err)
			}
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
	jobs, err := postgres.NewJobRequestRepo()
	if err != nil {
		t.Fatal(err)
	}
	job := &models.JobRequest{ID: uuid.NewString(), CustomerID: customer, RequesterRole: "customer", RequestType: "internet", Mode: "direct", TargetISPID: ispID, ServiceType: "fiber_installation", Status: "open", IsAvailable: true, CreatedAt: now, UpdatedAt: now}
	if err := jobs.CreateJobRequest(job); err != nil {
		t.Fatal(err)
	}
	installations, err := postgres.NewInstallationRepo()
	if err != nil {
		t.Fatal(err)
	}
	customerInstallations, err := installations.GetInstallationsByClientID(customer)
	if err != nil || len(customerInstallations) != 1 || customerInstallations[0].ID != job.ID {
		t.Fatalf("service request did not appear as customer installation: %#v %v", customerInstallations, err)
	}
	ispInstallations, err := installations.GetInstallationsByISPID(ispID)
	if err != nil || len(ispInstallations) != 1 || ispInstallations[0].ID != job.ID {
		t.Fatalf("targeted request did not appear in ISP queue: %#v %v", ispInstallations, err)
	}
	if _, err := jobs.AssignTechnician(job.ID, ispID, employee2); err != nil {
		t.Fatalf("ISP could not assign its active linked technician: %v", err)
	}
	if err := jobs.UpdateJobStatus(job.ID, employee2, "completed"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("invalid assigned-to-completed transition was accepted: %v", err)
	}
	for _, status := range []string{"accepted", "in_progress", "completed"} {
		if err := jobs.UpdateJobStatus(job.ID, employee2, status); err != nil {
			t.Fatalf("valid job transition %s failed: %v", status, err)
		}
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
	vacancies, err := postgres.NewVacancyRepository()
	if err != nil {
		t.Fatal(err)
	}
	vacancy := &models.ISPVacancy{ID: uuid.NewString(), ISPID: ispID, Title: "CI vacancy", ServiceType: "fiber", Slots: 1, Status: "open", CreatedAt: now, UpdatedAt: now}
	if err := vacancies.Create(vacancy); err != nil {
		t.Fatal(err)
	}
	if err := vacancies.Apply(vacancy.ID, independent, "available"); err != nil {
		t.Fatal(err)
	}
	applications, err := vacancies.Applications(ispID, vacancy.ID)
	if err != nil || len(applications) != 1 {
		t.Fatalf("vacancy applications: %#v %v", applications, err)
	}
	if err := vacancies.Decide(ispID, vacancy.ID, applications[0].ID, "accepted"); err != nil {
		t.Fatal(err)
	}
	socialRepo, err := postgres.NewSocialRepository()
	if err != nil {
		t.Fatal(err)
	}
	billingRepo, err := postgres.NewBillingRepository()
	if err != nil {
		t.Fatal(err)
	}
	billing := &models.BillingSystem{Name: "CI Billing", Slug: "ci-billing-" + strings.ReplaceAll(uuid.NewString(), "-", ""), OwnerCompany: "CI ISP", WebsiteURL: "https://example.com", LoginURL: "https://example.com/login", ReferralURL: "https://example.com/referral", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := billingRepo.Create(context.Background(), billing, ispID); err != nil {
		t.Fatal(err)
	}
	manager, err := billingRepo.Manager(context.Background(), billing.ID, ispID)
	if err != nil || !manager {
		t.Fatal(err)
	}
	price := 100.0
	billing.Pricing = append(billing.Pricing, &models.BillingPrice{ServiceType: "hotspot", Currency: "KES", BillingPeriod: "monthly", Amount: &price, SetupFee: 0, Available: true})
	if err := billingRepo.UpsertPricing(context.Background(), billing.ID, ispID, billing.Pricing, now); err != nil {
		t.Fatal(err)
	}
	review := &models.BillingReview{BillingSystemID: billing.ID, ReviewerID: customer, Reliability: 5, EaseOfUse: 4, Support: 4, Features: 5, ValueForMoney: 4, Comment: "works", UseVerified: true}
	if err := billingRepo.CreateReview(context.Background(), review); err != nil {
		t.Fatal(err)
	}
	reviews, total, err := billingRepo.Reviews(context.Background(), billing.ID, 10, 0)
	if err != nil || total != 1 || len(reviews) != 1 {
		t.Fatalf("billing reviews: %#v %d %v", reviews, total, err)
	}
	flag := &models.BillingFlag{BillingSystemID: billing.ID, ReporterID: customer, Reason: "test flag", Details: "details"}
	if err := billingRepo.Flag(context.Background(), flag); err != nil {
		t.Fatal(err)
	}
	flags, err := billingRepo.ListFlags(context.Background(), "open", 10)
	if err != nil || len(flags) == 0 {
		t.Fatalf("billing flags: %#v %v", flags, err)
	}
	claim := &models.BillingClaim{BillingSystemID: billing.ID, ClaimantID: customer, Evidence: "evidence"}
	if err := billingRepo.Claim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if err := billingRepo.ResolveClaim(context.Background(), claim.ID, ispID, "approved"); err != nil {
		t.Fatal(err)
	}
	promotionRepo, err := postgres.NewPromotionRepository()
	if err != nil {
		t.Fatal(err)
	}
	listing, err := promotionRepo.ResolveListing(context.Background(), ispID, "isp", models.PromotionListingBillingSystem, billing.ID)
	if err != nil || listing.Name != "CI Billing" || listing.InternalPath == "" {
		t.Fatalf("promotion listing resolution: %#v %v", listing, err)
	}
	promotion := &models.Promotion{
		OwnerID:         ispID,
		ListingType:     listing.Type,
		ListingID:       listing.ID,
		ListingName:     listing.Name,
		DestinationType: "internal",
		DestinationURL:  listing.InternalPath,
		Placement:       "directory",
		StartsAt:        now,
		ExpiresAt:       now.AddDate(0, 0, 30),
		Amount:          250,
		Currency:        "KES",
	}
	if err := promotionRepo.Create(context.Background(), promotion, &models.PromotionPayment{Amount: 250, Currency: "KES"}); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = database.GetWriter().Exec(`DELETE FROM promotions WHERE id=$1`, promotion.ID) }()
	storedPromotion, err := promotionRepo.Get(context.Background(), promotion.ID, ispID, "isp")
	if err != nil || storedPromotion.Status != models.PromotionPendingApproval {
		t.Fatalf("new promotion: %#v %v", storedPromotion, err)
	}
	payments, err := promotionRepo.ListPayments(context.Background(), promotion.ID)
	if err != nil || len(payments) != 1 {
		t.Fatalf("promotion payments: %#v %v", payments, err)
	}
	if err := promotionRepo.ConfirmPayment(context.Background(), promotion.ID, payments[0].ID, ispID, "manual", "CI-REF-1", "receipt"); err != nil {
		t.Fatal(err)
	}
	storedPromotion, err = promotionRepo.Get(context.Background(), promotion.ID, ispID, "isp")
	if err != nil || storedPromotion.Status != models.PromotionPendingApproval || storedPromotion.PaymentStatus != "paid" {
		t.Fatalf("paid promotion should still await approval: %#v %v", storedPromotion, err)
	}
	active, err := promotionRepo.Active(context.Background(), "directory", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range active {
		if item.ID == promotion.ID {
			t.Fatal("unapproved promotion was visible to customers")
		}
	}
	if err := promotionRepo.Approve(context.Background(), promotion.ID, ispID); err != nil {
		t.Fatal(err)
	}
	storedPromotion, err = promotionRepo.Get(context.Background(), promotion.ID, ispID, "isp")
	if err != nil || storedPromotion.Status != models.PromotionActive || storedPromotion.ApprovedAt == nil {
		t.Fatalf("approved promotion: %#v %v", storedPromotion, err)
	}
	if recorded, err := promotionRepo.RecordEvent(context.Background(), promotion.ID, "impression", "viewer-1", "customer", "Nairobi", "directory", now.Unix()/300); err != nil || !recorded {
		t.Fatalf("record impression: %v %v", recorded, err)
	}
	if recorded, err := promotionRepo.RecordEvent(context.Background(), promotion.ID, "impression", "viewer-1", "customer", "Nairobi", "directory", now.Unix()/300); err != nil || recorded {
		t.Fatalf("duplicate impression was counted: %v %v", recorded, err)
	}
	if recorded, err := promotionRepo.RecordEvent(context.Background(), promotion.ID, "click", "viewer-1", "customer", "Nairobi", "directory", now.Unix()/60); err != nil || !recorded {
		t.Fatalf("record click: %v %v", recorded, err)
	}
	if recorded, err := promotionRepo.RecordEvent(context.Background(), promotion.ID, "impression", "viewer-2", "isp", "Mombasa", "directory", now.Unix()/900); err != nil || !recorded {
		t.Fatalf("record second viewer: %v %v", recorded, err)
	}
	insights, err := promotionRepo.Insights(context.Background(), promotion.ID, now.Add(-time.Hour), now.Add(time.Hour), "")
	if err != nil || insights.Totals.Impressions != 2 || insights.Totals.UniqueViewers != 2 || insights.Totals.Clicks != 1 || insights.Totals.UniqueClickers != 1 {
		t.Fatalf("promotion insights: %#v %v", insights, err)
	}
	if _, err := promotionRepo.CreateEnquiry(context.Background(), promotion.ID, customer, "Customer", "", "", "Contact me"); err != nil {
		t.Fatal(err)
	}
	enquiries, total, err := promotionRepo.ListEnquiries(context.Background(), promotion.ID, 10, 0)
	if err != nil || total != 1 || len(enquiries) != 1 {
		t.Fatalf("promotion enquiries: %#v %d %v", enquiries, total, err)
	}
	if _, err := promotionRepo.Get(context.Background(), promotion.ID, otherISP, "isp"); !errors.Is(err, postgres.ErrPromotionNotFound) {
		t.Fatalf("unrelated ISP could read promotion: %v", err)
	}
	social := socialsvc.NewService(socialRepo)
	if created, err := social.Favorite(context.Background(), customer, "isp", ispID); err != nil || !created {
		t.Fatalf("favorite failed: %v %v", created, err)
	}
	favorites, total, _, _, err := social.Favorites(context.Background(), customer, "", 20, 0)
	if err != nil || total != 1 || len(favorites) != 1 {
		t.Fatalf("favorite list: %#v %d %v", favorites, total, err)
	}
	if created, err := social.React(context.Background(), customer, "user_profile", independent, "like"); err != nil || !created {
		t.Fatalf("profile like failed: %v %v", created, err)
	}
	reactions, err := social.Reactions(context.Background(), customer, "user_profile", independent)
	if err != nil || reactions.Counts["like"] != 1 {
		t.Fatalf("reaction summary: %#v %v", reactions, err)
	}
	device, err := social.RegisterDevice(context.Background(), customer, "android", "ci-device-token-1234567890")
	if err != nil || device.ID == "" {
		t.Fatalf("device registration: %#v %v", device, err)
	}
}

func ptrFloat(v float64) *float64 { return &v }
