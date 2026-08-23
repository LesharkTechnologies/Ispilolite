package routes

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ispilolite/api/handlers"
	"ispilolite/internal/middleware"
	"ispilolite/internal/repository/postgres"
	"ispilolite/internal/services/geospatial"
	"ispilolite/internal/services/jobrequest"
	"ispilolite/internal/services/matching"
	"ispilolite/internal/services/notification"
	"ispilolite/pkg/database"
	"ispilolite/pkg/monitoring"
	appnotifications "ispilolite/pkg/notifications"
)

// SetupRouter builds the application's Gin router. Business handlers still use
// net/http signatures so they can be migrated independently; stdHandler and
// authHandler provide the compatibility boundary during that transition.
func SetupRouter() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	auth := handlers.NewAuthHandler()
	isp := handlers.NewISPHandler()
	client := handlers.NewClientHandler()
	ispEndpoints := handlers.NewISPEndpointsHandler()
	technician := handlers.NewTechnicianHandler()
	admin := handlers.NewAdminHandler()
	location := handlers.NewLocationHandler()
	quotation := handlers.NewQuotationHandler()
	reviewAdmin := handlers.NewReviewAdminHandler()
	smsAdmin := handlers.NewSMSAdminHandler()
	notificationsHandler := handlers.NewNotificationHandler()
	criticalAlerts := handlers.NewCriticalAlertHandler()
	vacancies := handlers.NewVacancyHandler()
	geoHandler := handlers.NewGeospatialHandler(geospatial.NewGeospatialService(postgres.NewGeospatialRepository()))
	matcher := matching.NewMatchingService(postgres.NewMatchingRepository(), appnotifications.NewService(postgres.NewNotificationRepositoryAdapter(), notification.NewSenderFromEnv()))
	matchingHandler := handlers.NewMatchingHandler(matcher)
	jobrequest.SetMatcher(matcher)

	router.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/readyz", readiness)
	router.GET("/metrics", gin.WrapH(monitoring.Handler()))

	api := router.Group("/api/v1")
	api.POST("/auth/register", stdHandler(auth.Register))
	api.POST("/auth/login", stdHandler(auth.Login))
	api.POST("/auth/verify-otp", stdHandler(auth.VerifyOTP))
	api.POST("/auth/refresh", stdHandler(auth.RefreshToken))
	api.POST("/auth/logout", stdHandler(auth.Logout))

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
	api.GET("/geo/search", stdHandler(geoHandler.Locations))
	api.GET("/geo/locations/by-county", stdHandler(location.ListCountyLocations))
	api.POST("/geo/locations", authHandler("", location.SubmitLocation))
	api.GET("/geo/locations/:id", stdHandler(location.GetLocation))
	api.POST("/geo/locations/:id/aliases", authHandler("", location.AddAlias))
	api.GET("/matches/request/:id", authHandler("customer", matchingHandler.Match))
	api.POST("/matches/:id/respond", authHandlerByRole(map[string]http.HandlerFunc{"isp": matchingHandler.Respond, "technician": matchingHandler.Respond}))
	api.DELETE("/matches/:id/decline", authHandlerByRole(map[string]http.HandlerFunc{"isp": matchingHandler.Decline, "technician": matchingHandler.Decline}))

	api.GET("/public/quotations/:code", stdHandler(quotation.Public))
	api.POST("/quotations", authHandler("", quotation.Finalize))
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
	api.GET("/my/jobs", authHandler("customer", client.GetJobs))
	api.POST("/my/jobs", authHandler("customer", client.CreateJob))
	api.GET("/my/jobs/:id", authHandler("customer", client.GetJobApplications))
	api.PUT("/my/jobs/:id", authHandler("customer", client.AssignJob))
	api.PATCH("/my/jobs/:id", authHandler("customer", client.SetJobAvailability))
	api.DELETE("/my/jobs/:id", authHandler("customer", client.DeleteJob))
	api.POST("/my/jobs/:id/terminate", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.TerminateJob, "isp": client.TerminateJob, "technician": client.TerminateJob}))

	api.GET("/my/installations", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.GetInstallations, "isp": ispEndpoints.GetInstallations}))
	api.GET("/my/profile", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.GetProfile, "isp": ispEndpoints.GetProfile, "technician": technician.GetProfile}))
	api.PUT("/my/profile", authHandlerByRole(map[string]http.HandlerFunc{"customer": client.UpdateProfile, "isp": ispEndpoints.UpdateProfile}))
	api.DELETE("/my/profile", authHandler("customer", client.RequestDeleteProfile))

	api.POST("/my/technicians", authHandler("isp", ispEndpoints.AddTechnician))
	api.POST("/my/technicians/:id/link", authHandler("isp", ispEndpoints.LinkTechnician))
	api.GET("/my/technicians", authHandler("isp", ispEndpoints.ListCompanyTechnicians))
	api.POST("/my/technicians/:id/end", authHandler("isp", ispEndpoints.EndTechnicianLink))
	api.POST("/my/vacancies", authHandler("isp", vacancies.Create))
	api.GET("/my/vacancies", authHandler("isp", vacancies.List))
	api.DELETE("/my/vacancies/:id", authHandler("isp", vacancies.Close))
	api.POST("/vacancies/:id/apply", authHandler("technician", vacancies.Apply))
	api.POST("/my/technician-work-requests", authHandler("isp", ispEndpoints.CreateTechnicianWorkRequest))
	api.PUT("/my/requests/:id/assign-technician", authHandler("isp", ispEndpoints.AssignTechnician))
	api.GET("/technicians/:id/profile", stdHandler(technician.GetPublicProfile))
	api.POST("/technicians/:id/requests", authHandler("customer", technician.CreateJobRequest))
	api.POST("/technicians/:id/reviews", authHandler("customer", technician.CreateTechnicianReview))
	api.GET("/technicians/:id/reviews", stdHandler(technician.GetTechnicianReviews))
	api.DELETE("/technicians/:id", authHandler("isp", ispEndpoints.RemoveTechnician))

	api.PUT("/my/portfolio/profile", authHandler("technician", technician.UpdatePortfolioProfile))
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

	api.GET("/my/coverage", authHandler("isp", ispEndpoints.GetCoverageAreas))
	api.POST("/my/coverage", authHandler("isp", ispEndpoints.AddCoverageArea))
	api.GET("/my/coverage/recommendations", authHandler("isp", ispEndpoints.GetCoverageRecommendations))
	api.GET("/my/notifications", authHandler("isp", ispEndpoints.GetNotifications))
	api.PUT("/my/notifications/:id", authHandler("isp", ispEndpoints.ReadNotification))
	api.GET("/my/notifications/stream", authHandler("", ispEndpoints.StreamNotifications))
	api.GET("/notifications", authHandler("", notificationsHandler.List))
	api.PUT("/notifications/:id/read", authHandler("", notificationsHandler.Read))
	api.GET("/notifications/stream", authHandler("", notificationsHandler.Stream))
	api.GET("/available-jobs", authHandlerByRole(map[string]http.HandlerFunc{"technician": technician.GetAvailableJobs, "isp": technician.GetAvailableJobs}))
	api.GET("/my/technician-jobs", authHandler("technician", technician.GetJobs))
	api.POST("/jobs/:id/apply", authHandler("", technician.ApplyToJob))
	api.PUT("/jobs/:id", authHandler("", technician.UpdateJobStatus))

	api.GET("/admin/deletion-requests", authHandler("admin", admin.GetDeletionRequests))
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
	api.POST("/reviews/:id/report", authHandler("", technician.ReportReview))

	limiter := middleware.NewRateLimiter(database.GetRedis(), 120, time.Minute)
	return middleware.RequestLogger(log.Default())(limiter.Middleware(monitoring.Middleware("api", router)))
}

func stdHandler(handler http.HandlerFunc) gin.HandlerFunc { return gin.WrapF(handler) }

func authHandler(role string, handler http.HandlerFunc) gin.HandlerFunc {
	return gin.WrapH(middleware.AuthMiddleware(role)(http.HandlerFunc(handler)))
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
