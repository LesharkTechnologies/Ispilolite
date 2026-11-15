package routes

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ispilolite/api/handlers"
	"ispilolite/internal/i18n"
	"ispilolite/internal/middleware"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/search"
	availabilitysvc "ispilolite/internal/services/availability"
	chatsvc "ispilolite/internal/services/chat"
	coveragemapsvc "ispilolite/internal/services/coveragemap"
	disputesvc "ispilolite/internal/services/dispute"
	earningssvc "ispilolite/internal/services/earnings"
	"ispilolite/internal/services/geospatial"
	"ispilolite/internal/services/jobrequest"
	"ispilolite/internal/services/matching"
	"ispilolite/internal/services/notification"
	paymentsvc "ispilolite/internal/services/payment"
	realtimesvc "ispilolite/internal/services/realtime"
	referralsvc "ispilolite/internal/services/referral"
	warrantysvc "ispilolite/internal/services/warranty"
	"ispilolite/pkg/database"
	"ispilolite/pkg/idempotency"
	"ispilolite/pkg/monitoring"
	appnotifications "ispilolite/pkg/notifications"
	"ispilolite/pkg/profilelink"
)

// SetupRouter builds the application's Gin router. Business handlers still use
// net/http signatures so they can be migrated independently; stdHandler and
// authHandler provide the compatibility boundary during that transition.
//
// It fails fast when a dependency is unavailable so a misconfigured deployment
// reports a clear error instead of panicking on a later nil dereference. The
// returned matcher is the same instance used by the routes, so entrypoints can
// start its expiry worker.
func SetupRouter() (http.Handler, *matching.MatchingService, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	accountRepo, err := postgres.NewUserRepo()
	if err != nil {
		return nil, nil, err
	}
	middleware.SetAccountActiveChecker(accountRepo.AccountActive)

	authHandlerInst, err := handlers.NewAuthHandler()
	if err != nil {
		return nil, nil, err
	}
	isp, err := handlers.NewISPHandler()
	if err != nil {
		return nil, nil, err
	}
	client, err := handlers.NewClientHandler()
	if err != nil {
		return nil, nil, err
	}
	profileLinks, err := profilelink.FromEnv()
	if err != nil {
		return nil, nil, err
	}
	followHandler, err := handlers.NewFollowHandler(profileLinks)
	if err != nil {
		return nil, nil, err
	}
	profileHandler, err := handlers.NewPublicProfileHandler(profileLinks)
	if err != nil {
		return nil, nil, err
	}
	socialHandler, err := handlers.NewSocialHandler()
	if err != nil {
		return nil, nil, err
	}
	ispEndpoints, err := handlers.NewISPEndpointsHandler()
	if err != nil {
		return nil, nil, err
	}
	technician, err := handlers.NewTechnicianHandler()
	if err != nil {
		return nil, nil, err
	}
	admin, err := handlers.NewAdminHandler()
	if err != nil {
		return nil, nil, err
	}
	location, err := handlers.NewLocationHandler()
	if err != nil {
		return nil, nil, err
	}
	quotation, err := handlers.NewQuotationHandler()
	if err != nil {
		return nil, nil, err
	}
	reviewAdmin, err := handlers.NewReviewAdminHandler()
	if err != nil {
		return nil, nil, err
	}
	smsAdmin, err := handlers.NewSMSAdminHandler()
	if err != nil {
		return nil, nil, err
	}
	notificationsHandler, err := handlers.NewNotificationHandler()
	if err != nil {
		return nil, nil, err
	}
	criticalAlerts, err := handlers.NewCriticalAlertHandler()
	if err != nil {
		return nil, nil, err
	}
	vacancies, err := handlers.NewVacancyHandler()
	if err != nil {
		return nil, nil, err
	}
	geoRepo, err := postgres.NewGeospatialRepository()
	if err != nil {
		return nil, nil, err
	}
	geoHandler := handlers.NewGeospatialHandler(geospatial.NewGeospatialService(geoRepo))
	matchingRepo, err := postgres.NewMatchingRepository()
	if err != nil {
		return nil, nil, err
	}
	notificationRepo, err := postgres.NewNotificationRepositoryAdapter()
	if err != nil {
		return nil, nil, err
	}
	notificationService := appnotifications.NewService(notificationRepo, notification.NewSenderFromEnv())
	matcher := matching.NewMatchingService(matchingRepo, notificationService)
	matchingHandler := handlers.NewMatchingHandler(matcher)
	jobrequest.SetMatcher(matcher)
	realtimeRepo, err := postgres.NewRealtimeRepository()
	if err != nil {
		return nil, nil, err
	}
	realtimeService := realtimesvc.NewService(realtimeRepo, database.GetRedis(), notificationService, appnotifications.Default)
	realtimeHandler := handlers.NewRealtimeHandler(realtimeService)
	jobrequest.SetEventPublisher(realtimeService)
	chatRepo, err := postgres.NewChatRepository()
	if err != nil {
		return nil, nil, err
	}
	chatHandler := handlers.NewChatHandler(chatsvc.NewService(chatRepo, notificationService, appnotifications.Default))
	availabilityRepo, err := postgres.NewAvailabilityRepository()
	if err != nil {
		return nil, nil, err
	}
	availabilityHandler := handlers.NewAvailabilityHandler(availabilitysvc.NewService(availabilityRepo))
	paymentRepo, err := postgres.NewPaymentRepository()
	if err != nil {
		return nil, nil, err
	}
	paymentHandler := handlers.NewPaymentHandler(paymentsvc.NewService(paymentRepo, paymentsvc.ProvidersFromEnv(), notificationService))
	warrantyRepo, err := postgres.NewWarrantyRepository()
	if err != nil {
		return nil, nil, err
	}
	warrantyHandler := handlers.NewWarrantyHandler(warrantysvc.NewService(warrantyRepo))
	coverageMapRepo, err := postgres.NewCoverageMapRepository()
	if err != nil {
		return nil, nil, err
	}
	coverageMapHandler := handlers.NewCoverageMapHandler(coveragemapsvc.NewService(coverageMapRepo))
	referralRepo, err := postgres.NewReferralRepository()
	if err != nil {
		return nil, nil, err
	}
	referralHandler := handlers.NewReferralHandler(referralsvc.NewService(referralRepo))
	disputeRepo, err := postgres.NewDisputeRepository()
	if err != nil {
		return nil, nil, err
	}
	disputeHandler := handlers.NewDisputeHandler(disputesvc.NewService(disputeRepo))
	earningsRepo, err := postgres.NewEarningsRepository()
	if err != nil {
		return nil, nil, err
	}
	earningsHandler := handlers.NewEarningsHandler(earningssvc.NewService(earningsRepo))

	// --- Search service: Elasticsearch fast path + Postgres fallback -------
	// Wire Elasticsearch when ES_ADDRESSES is set; otherwise search runs
	// Postgres-only (graceful degradation). The same DB reader powers the
	// fallback so no new connection pool is needed.
	var esRepo search.Repository
	var esClient *search.Client
	if esAddrs := os.Getenv("ES_ADDRESSES"); envBool("ES_ENABLED", true) && esAddrs != "" {
		cfg := search.Config{
			Addresses:   splitAddrsEnv(esAddrs),
			Username:    os.Getenv("ES_USERNAME"),
			Password:    os.Getenv("ES_PASSWORD"),
			APIKey:      os.Getenv("ES_API_KEY"),
			EnableSniff: envBool("ES_ENABLE_SNIFF", false),
		}
		c, err := search.NewClient(cfg)
		if err != nil {
			log.Printf("WARN search: elasticsearch client failed, running postgres-only: %v", err)
		} else {
			esClient = c
			esRepo = search.NewESRepository(esClient)
			// Probe + ensure indices (non-fatal).
			bootCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if perr := esClient.Ping(bootCtx); perr != nil {
				log.Printf("WARN search: elasticsearch unreachable at startup, running postgres-only: %v", perr)
			} else {
				log.Printf("search: elasticsearch reachable")
				if envBool("ES_ENSURE_INDICES", true) {
					if err := esClient.EnsureIndices(bootCtx); err != nil {
						log.Printf("WARN search: could not ensure indices: %v", err)
					}
				}
			}
			cancel()
		}
	}

	pgRepo := search.NewPostgresRepository(database.GetReader())
	searchConfig := search.DefaultServiceConfig()
	searchConfig.ESTimeout = envDuration("ES_REQUEST_TIMEOUT", searchConfig.ESTimeout)
	searchConfig.FallbackTimeout = envDuration("ES_FALLBACK_TIMEOUT", searchConfig.FallbackTimeout)
	searchConfig.Breaker.Threshold = envInt("ES_BREAKER_THRESHOLD", searchConfig.Breaker.Threshold)
	searchConfig.Breaker.Cooldown = envDuration("ES_BREAKER_COOLDOWN", searchConfig.Breaker.Cooldown)
	searchConfig.Strategy = getenv("ES_FALLBACK_STRATEGY", search.StrategyESFirst)
	svc := search.NewService(esRepo, pgRepo, searchConfig, log.Default())
	if esClient != nil {
		svc.WithHealthChecker(search.NewHealthChecker(esClient.ES()))
		search.SetDefaultSyncer(search.NewSyncer(database.GetReader(), esClient, log.Default()))
	} else {
		svc.WithHealthChecker(search.NewHealthChecker(nil))
	}
	searchHandler := handlers.NewSearchHandler(svc)

	// Search routes are registered below after the /api/v1 group is defined.

	// CORS: restrict to configured origins in production. Empty = disabled.
	corsOrigins := strings.Split(getenv("CORS_ALLOWED_ORIGINS", ""), ",")
	for i, o := range corsOrigins {
		corsOrigins[i] = strings.TrimSpace(o)
	}

	// Separate rate limiters: auth endpoints are much stricter.
	authLimiter := middleware.NewRateLimiter(database.GetRedis(), 5, time.Minute)  // 5/min for register/login/OTP
	apiLimiter := middleware.NewRateLimiter(database.GetRedis(), 300, time.Minute) // 300/min for standard API

	router.Use(stdMiddleware(middleware.SecurityHeaders()))
	router.Use(stdMiddleware(middleware.StrictTransportSecurity(24 * time.Hour)))
	router.Use(stdMiddleware(middleware.CORS(corsOrigins)))
	router.Use(stdMiddleware(i18n.Middleware()))

	// Public health/metrics are not rate-limited.
	router.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/readyz", readiness)
	// /metrics is private: guarded by METRICS_TOKEN in production (404 when
	// unset) and open only for local development.
	metricsHandler := middleware.MetricsGuard(getenv("METRICS_TOKEN", ""), getenv("ENVIRONMENT", "development"))(monitoring.Handler())
	router.GET("/metrics", gin.WrapH(metricsHandler))
	// A stable, browser-viewable share link independent of the Flutter build.
	router.GET("/u/:slug", stdMiddleware(apiLimiter.Middleware), stdHandler(profileHandler.Web))

	api := router.Group("/api/v1")
	api.Use(stdMiddleware(apiLimiter.Middleware))
	idempotencyStore, err := idempotency.NewGuardedStore()
	if err != nil {
		return nil, nil, err
	}
	api.GET("/i18n/:locale", stdHandler(handlers.Translations))

	// Register /api/v1/search/* routes on the main API (ES fast path +
	// Postgres fallback). Requires the search service to have been created
	// above; svc/searchHandler are in scope from the earlier block.
	searchGroup := api.Group("/search")
	searchGroup.GET("/locations", stdHandler(searchHandler.SearchLocations))
	searchGroup.GET("/isps", stdHandler(searchHandler.SearchISPs))
	searchGroup.GET("/technicians", stdHandler(searchHandler.SearchTechnicians))
	searchGroup.GET("/suggest", stdHandler(searchHandler.Suggest))
	searchGroup.GET("/health", stdHandler(searchHandler.Health))
	// Provider callbacks authenticate with a configured webhook token/HMAC, not
	// a user JWT.
	api.POST("/payments/webhooks/:provider", stdHandler(paymentHandler.Webhook))
	api.POST("/payments/settlements", stdHandler(paymentHandler.Settlement))

	// Auth group: strict rate limit, no auth required.
	authGroup := api.Group("/auth")
	authGroup.Use(stdMiddleware(authLimiter.Middleware))
	authGroup.POST("/register", stdHandler(authHandlerInst.Register))
	authGroup.POST("/login", stdHandler(authHandlerInst.Login))
	authGroup.POST("/verify-otp", stdHandler(authHandlerInst.VerifyOTP))
	authGroup.POST("/refresh", stdHandler(authHandlerInst.RefreshToken))
	authGroup.POST("/logout", stdHandler(authHandlerInst.Logout))

	// Social follows connect users of any role. This does not grant employment,
	// job access, or ISP membership to the followed account.
	api.POST("/users/:id/follow", socialUserHandler(followHandler.Follow))
	api.DELETE("/users/:id/follow", socialUserHandler(followHandler.Unfollow))
	api.GET("/users/:id/follow-stats", socialUserHandler(followHandler.Summary))
	api.GET("/users/:id/followers", socialUserHandler(followHandler.Followers))
	api.GET("/users/:id/following", socialUserHandler(followHandler.Following))
	api.GET("/my/follow-stats", socialUserHandler(followHandler.Summary))
	api.GET("/my/followers", socialUserHandler(followHandler.Followers))
	api.GET("/my/following", socialUserHandler(followHandler.Following))
	api.POST("/favorites/:type/:id", socialUserHandler(socialHandler.Favorite))
	api.DELETE("/favorites/:type/:id", socialUserHandler(socialHandler.Unfavorite))
	api.GET("/my/favorites", socialUserHandler(socialHandler.Favorites))
	api.POST("/users/favorites/:type/:id", socialUserHandler(socialHandler.Favorite))
	api.DELETE("/users/favorites/:type/:id", socialUserHandler(socialHandler.Unfavorite))
	api.GET("/users/favorites", socialUserHandler(socialHandler.Favorites))
	api.POST("/reactions/:type/:id", socialUserHandler(socialHandler.React))
	api.DELETE("/reactions/:type/:id", socialUserHandler(socialHandler.Unreact))
	api.GET("/reactions/:type/:id", socialUserHandler(socialHandler.Reactions))
	api.POST("/my/devices", socialUserHandler(socialHandler.RegisterDevice))
	api.DELETE("/my/devices", socialUserHandler(socialHandler.RemoveDevice))
	api.GET("/my/public-profile", socialUserHandler(profileHandler.Mine))
	api.GET("/profiles/:slug", stdHandler(profileHandler.JSON))

	api.GET("/isps", stdHandler(isp.GetISPs))
	api.GET("/isps/:id", ispDispatcher(isp))
	api.GET("/isps/:id/packages", ispDispatcher(isp))
	api.GET("/isps/:id/reviews", ispDispatcher(isp))
	api.POST("/isps/:id/reviews", authHandler("customer", isp.CreateISPReview))
	api.GET("/isp-packages", stdHandler(isp.ListPackages))
	api.GET("/package-units", stdHandler(isp.ListPackageUnits))

	api.GET("/geo/locations/search", stdHandler(location.SearchLocations))
	api.GET("/geo/nearby/isps", stdHandler(geoHandler.NearbyISPs))
	api.GET("/geo/nearby/technicians", stdHandler(geoHandler.NearbyTechnicians))
	api.GET("/geo/coverage", stdHandler(geoHandler.Coverage))
	api.GET("/geo/coverage-map", stdHandler(coverageMapHandler.List))
	api.GET("/geo/search", stdHandler(geoHandler.Locations))
	api.GET("/geo/locations/by-county", stdHandler(location.ListCountyLocations))
	api.POST("/geo/locations", authHandler("", location.SubmitLocation))
	api.GET("/geo/locations/:id", stdHandler(location.GetLocation))
	api.POST("/geo/locations/:id/aliases", authHandler("", location.AddAlias))
	api.GET("/matches/request/:id", authHandler("customer", matchingHandler.Match))
	api.GET("/my/matches", authHandlerByRole(map[string]http.HandlerFunc{"isp": matchingHandler.MyMatches, "technician": matchingHandler.MyMatches}))
	api.POST("/matches/:id/respond", authHandlerByRole(map[string]http.HandlerFunc{"isp": matchingHandler.Respond, "technician": matchingHandler.Respond}))
	api.DELETE("/matches/:id/decline", authHandlerByRole(map[string]http.HandlerFunc{"isp": matchingHandler.Decline, "technician": matchingHandler.Decline}))

	api.GET("/public/quotations/:code", stdHandler(quotation.Public))
	api.POST("/quotations", idempotentAuthHandler("", quotation.Finalize, idempotencyStore))
	api.GET("/quotations", authHandler("", quotation.List))
	api.GET("/quotations/:id", authHandler("", quotation.Get))
	api.GET("/documents/:id/download", authHandler("", quotation.DownloadDocument))
	api.POST("/quotations/:id/respond", authHandler("customer", quotation.Respond))
	api.GET("/quotation-units", authHandler("", quotation.Units))
	api.POST("/quotation-units", authHandler("", quotation.CreateUnit))
	api.GET("/tax-rates", authHandler("", admin.MyTaxRates))
	api.POST("/tax-rates", authHandler("", admin.CreateCustomTaxRate))
	api.GET("/my/business-profile", authHandler("", admin.MyBusinessProfile))
	api.PUT("/my/business-profile", authHandler("", admin.MyBusinessProfile))

	api.POST("/installations", authHandler("customer", client.CreateInstallation))
	api.PUT("/installations/*path", authHandler("isp", ispEndpoints.UpdateInstallation))
	api.GET("/my/jobs", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.GetJobs, "isp": ispEndpoints.GetJobs}))
	api.POST("/my/jobs", idempotentAuthHandler("customer", client.CreateJob, idempotencyStore))
	api.GET("/my/jobs/:id", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.GetJobApplications, "isp": ispEndpoints.GetJob}))
	api.PUT("/my/jobs/:id", authHandler("customer", client.AssignJob))
	api.PATCH("/my/jobs/:id", authHandler("customer", client.SetJobAvailability))
	api.DELETE("/my/jobs/:id", authHandler("customer", client.DeleteJob))
	api.POST("/my/jobs/:id/terminate", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.TerminateJob, "isp": client.TerminateJob, "technician": client.TerminateJob}))

	api.GET("/my/installations", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.GetInstallations, "isp": ispEndpoints.GetInstallations}))
	api.GET("/my/profile", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.GetProfile, "isp": ispEndpoints.GetProfile, "technician": technician.GetProfile}))
	api.PUT("/my/profile", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.UpdateProfile, "isp": ispEndpoints.UpdateProfile, "technician": client.UpdateProfile}))
	api.DELETE("/my/profile", authHandler("customer", client.RequestDeleteProfile))
	api.GET("/my/referrals", authHandler("customer", referralHandler.Summary))

	api.POST("/my/technicians", authHandler("isp", ispEndpoints.AddTechnician))
	api.POST("/my/technicians/:id/link", authHandler("isp", ispEndpoints.LinkTechnician))
	api.GET("/my/technicians", authHandler("isp", ispEndpoints.ListCompanyTechnicians))
	api.POST("/my/technicians/:id/end", authHandler("isp", ispEndpoints.EndTechnicianLink))
	api.POST("/my/vacancies", authHandler("isp", vacancies.Create))
	api.GET("/my/vacancies", authHandler("isp", vacancies.List))
	api.DELETE("/my/vacancies/:id", authHandler("isp", vacancies.Close))
	api.GET("/my/vacancies/:id/applications", authHandler("isp", vacancies.Applications))
	api.PATCH("/my/vacancies/:id/applications/:application_id", authHandler("isp", vacancies.Decide))
	api.POST("/vacancies/:id/apply", authHandler("technician", vacancies.Apply))
	api.DELETE("/vacancies/:id/application", authHandler("technician", vacancies.Withdraw))
	api.POST("/my/technician-work-requests", authHandler("isp", ispEndpoints.CreateTechnicianWorkRequest))
	api.GET("/my/isp-jobs", authHandler("isp", ispEndpoints.GetJobs))
	api.GET("/my/isp-jobs/:id", authHandler("isp", ispEndpoints.GetJob))
	api.PUT("/my/requests/:id/assign-technician", authHandler("isp", ispEndpoints.AssignTechnician))
	api.GET("/technicians/:id/profile", stdHandler(technician.GetPublicProfile))
	api.GET("/technicians/:id/availability", stdHandler(availabilityHandler.Slots))
	api.POST("/technicians/:id/requests", authHandler("customer", technician.CreateJobRequest))
	api.POST("/technicians/:id/reviews", authHandler("customer", technician.CreateTechnicianReview))
	api.GET("/technicians/:id/reviews", stdHandler(technician.GetTechnicianReviews))
	api.DELETE("/technicians/:id", authHandler("isp", ispEndpoints.RemoveTechnician))

	api.PUT("/my/portfolio/profile", authHandler("technician", technician.UpdatePortfolioProfile))
	api.PUT("/my/availability", authHandler("technician", availabilityHandler.Replace))
	api.POST("/my/availability/exceptions", authHandler("technician", availabilityHandler.AddException))
	api.GET("/my/portfolio/posts", authHandler("technician", technician.GetMyPortfolioPosts))
	api.POST("/my/portfolio/posts", authHandler("technician", technician.CreatePortfolioPost))
	api.PUT("/my/portfolio/posts/:id", authHandler("technician", technician.UpdatePortfolioPost))

	api.POST("/my/packages", authHandler("isp", ispEndpoints.CreatePackage))
	api.PUT("/packages/:id/prices", authHandler("isp", ispEndpoints.SetPackageCountyPrice))
	api.POST("/packages/:id/archive", authHandler("isp", ispEndpoints.ArchivePackage))
	api.PUT("/packages/:id", authHandler("isp", ispEndpoints.UpdatePackage))
	api.DELETE("/packages/:id", authHandler("isp", ispEndpoints.DeletePackage))
	api.POST("/package-reservations", authHandler("customer", client.ReservePackage))
	api.POST("/package-reservations/:id/subscribe", authHandler("customer", client.SubscribePackage))
	api.GET("/my/subscriptions", authHandlerByRole(map[string]http.HandlerFunc{"isp": ispEndpoints.ListSubscriptions, "customer": client.ListSubscriptions}))
	api.PUT("/subscriptions/:id", authHandlerByRole(map[string]http.HandlerFunc{"isp": ispEndpoints.UpdateSubscription, "customer": client.UpdateSubscription}))
	api.PATCH("/subscriptions/:id/auto-renew", authHandler("customer", client.SetSubscriptionAutoRenew))
	api.POST("/subscriptions/:id/usage", authHandler("isp", ispEndpoints.RecordSubscriptionUsage))

	api.GET("/my/coverage", authHandler("isp", ispEndpoints.GetCoverageAreas))
	api.POST("/my/coverage", authHandler("isp", ispEndpoints.AddCoverageArea))
	api.POST("/my/coverage/polygons", authHandler("isp", coverageMapHandler.Create))
	api.GET("/my/coverage/recommendations", authHandler("isp", ispEndpoints.GetCoverageRecommendations))
	api.GET("/my/notifications", authHandler("isp", ispEndpoints.GetNotifications))
	api.PUT("/my/notifications/:id", authHandler("isp", ispEndpoints.ReadNotification))
	api.GET("/my/notifications/stream", authHandler("", ispEndpoints.StreamNotifications))
	api.GET("/notifications", authHandler("", notificationsHandler.List))
	api.GET("/notifications/unread-count", authHandler("", notificationsHandler.UnreadCount))
	api.PUT("/notifications/read-all", authHandler("", notificationsHandler.ReadAll))
	api.PUT("/notifications/:id/read", authHandler("", notificationsHandler.Read))
	api.GET("/notifications/stream", authHandler("", notificationsHandler.Stream))
	api.GET("/available-jobs", authHandlerByRole(map[string]http.HandlerFunc{"technician": technician.GetAvailableJobs, "isp": technician.GetAvailableJobs}))
	api.GET("/my/technician-jobs", authHandler("technician", technician.GetJobs))
	api.GET("/my/earnings", authHandler("technician", earningsHandler.Dashboard))
	api.POST("/jobs/:id/apply", authHandlerByRole(map[string]http.HandlerFunc{"isp": technician.ApplyToJob, "technician": technician.ApplyToJob}))
	api.PUT("/jobs/:id", authHandlerByRole(map[string]http.HandlerFunc{"isp": technician.UpdateJobStatus, "technician": technician.UpdateJobStatus}))
	api.GET("/jobs/:id/stream", authHandler("", realtimeHandler.JobStream))
	api.GET("/jobs/:id/events", authHandler("", realtimeHandler.Events))
	api.GET("/jobs/:id/location", authHandler("", realtimeHandler.GetLocation))
	api.POST("/jobs/:id/location", authHandler("technician", realtimeHandler.UpdateLocation))
	api.GET("/jobs/:id/messages", authHandler("", chatHandler.List))
	api.POST("/jobs/:id/messages", authHandler("", chatHandler.Send))
	api.POST("/jobs/:id/messages/read", authHandler("", chatHandler.MarkRead))
	api.POST("/jobs/:id/payments", authHandler("customer", paymentHandler.Initiate))
	api.GET("/jobs/:id/escrow", authHandler("", paymentHandler.GetEscrow))
	api.POST("/escrows/:id/release", authHandler("customer", paymentHandler.Release))
	api.POST("/jobs/:id/warranty-claims", authHandler("customer", warrantyHandler.Create))
	api.POST("/jobs/:id/disputes", authHandler("", disputeHandler.Create))
	api.GET("/disputes/:id", authHandler("", disputeHandler.Get))
	api.POST("/disputes/:id/comments", authHandler("", disputeHandler.Comment))
	api.GET("/warranty-claims", authHandler("", warrantyHandler.List))
	api.PATCH("/warranty-claims/:id", authHandlerByRole(map[string]http.HandlerFunc{"isp": warrantyHandler.Resolve, "technician": warrantyHandler.Resolve}))

	api.GET("/admin/deletion-requests", authHandler("admin", admin.GetDeletionRequests))
	api.POST("/admin/users", authHandler("admin", admin.CreateUser))
	api.GET("/admin/users", authHandler("admin", admin.ListUsers))
	api.PATCH("/admin/users/:id", authHandler("admin", admin.UpdateUser))
	api.GET("/admin/settlements/outbox", authHandler("admin", admin.SettlementOutbox))
	api.POST("/admin/settlements/outbox/:id/retry", authHandler("admin", admin.RetrySettlement))
	api.GET("/admin/tax-rates", authHandler("admin", admin.TaxRates))
	api.POST("/admin/tax-rates", authHandler("admin", admin.CreateTaxRate))
	api.POST("/admin/system-units", authHandler("admin", admin.CreateSystemUnit))
	api.PUT("/admin/business-profiles/:id", authHandler("admin", admin.BusinessProfile))
	api.PUT("/admin/packages/:id/moderate", authHandler("admin", admin.ModeratePackage))
	api.POST("/admin/audit/export", authHandler("admin", admin.AuditExport))
	api.GET("/admin/audit", authHandler("admin", admin.AuditList))
	api.DELETE("/admin/audit", authHandler("admin", admin.AuditDelete))
	api.POST("/admin/approve-deletion", authHandler("admin", admin.ApproveDeletion))
	api.GET("/admin/reviews", authHandler("admin", reviewAdmin.Pending))
	api.PUT("/admin/reviews/:id", authHandler("admin", reviewAdmin.Moderate))
	api.POST("/admin/sms", authHandler("admin", smsAdmin.Send))
	api.POST("/admin/critical-alerts", authHandler("admin", criticalAlerts.Send))
	api.GET("/admin/disputes", authHandler("admin", disputeHandler.AdminList))
	api.PATCH("/admin/disputes/:id", authHandler("admin", disputeHandler.Resolve))
	api.POST("/reviews/:id/report", authHandler("", technician.ReportReview))

	handler := middleware.RequestLogger(log.Default())(monitoring.Middleware("api", router))
	return handler, matcher, nil
}

func stdHandler(handler http.HandlerFunc) gin.HandlerFunc { return gin.WrapF(handler) }

func stdMiddleware(mw func(http.Handler) http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		called := false
		mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			c.Request = r
			c.Next()
		})).ServeHTTP(c.Writer, c.Request)
		if !called && !c.IsAborted() {
			c.Abort()
		}
	}
}

func authHandler(role string, handler http.HandlerFunc) gin.HandlerFunc {
	return gin.WrapH(middleware.AuthMiddleware(role)(http.HandlerFunc(handler)))
}

func socialUserHandler(handler http.HandlerFunc) gin.HandlerFunc {
	return authHandlerByRole(map[string]http.HandlerFunc{
		"customer": handler, "technician": handler, "isp": handler,
	})
}

func idempotentAuthHandler(role string, handler http.HandlerFunc, store *idempotency.GuardedStore) gin.HandlerFunc {
	return gin.WrapH(middleware.AuthMiddleware(role)(store.Middleware(handler)))
}

func authHandlerByRole(handlersByRole map[string]http.HandlerFunc) gin.HandlerFunc {
	return gin.WrapH(middleware.AuthMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value("userRole").(string)
		if handler := handlersByRole[role]; handler != nil {
			handler(w, r)
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	})))
}

func readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	writer, reader, redisClient := database.GetWriter(), database.GetReader(), database.GetRedis()
	if writer == nil || reader == nil || redisClient == nil || writer.PingContext(ctx) != nil || reader.PingContext(ctx) != nil || redisClient.Ping(ctx).Err() != nil {
		c.String(http.StatusServiceUnavailable, "dependencies unavailable")
		return
	}
	c.String(http.StatusOK, "ready")
}

func ispDispatcher(h *handlers.ISPHandler) gin.HandlerFunc {
	return stdHandler(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/packages"):
			h.GetISPPackages(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/reviews"):
			h.GetISPReviews(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reviews"):
			middleware.AuthMiddleware("customer")(http.HandlerFunc(h.CreateISPReview)).ServeHTTP(w, r)
		case r.Method == http.MethodGet:
			h.GetISPByID(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// splitAddrsEnv turns a comma-separated list of ES node URLs into a slice.
func splitAddrsEnv(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key))); err == nil && value > 0 {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && value > 0 {
		return value
	}
	return fallback
}
