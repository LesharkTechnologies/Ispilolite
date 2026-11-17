package postgres

import (
	"context"
	"database/sql"
	"strings"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type CardsRepository struct {
	reader *sql.DB
}

func NewCardsRepository() (*CardsRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	return &CardsRepository{reader: reader}, nil
}

// Helper to determine product-like icon type for services
func iconTypeForService(serviceType string) string {
	st := strings.ToLower(serviceType)
	switch {
	case strings.Contains(st, "fiber") || strings.Contains(st, "fibre") || strings.Contains(st, "splice"):
		return "fiber"
	case strings.Contains(st, "cctv") || strings.Contains(st, "camera") || strings.Contains(st, "security"):
		return "cctv"
	case strings.Contains(st, "wifi") || strings.Contains(st, "wireless") || strings.Contains(st, "hotspot"):
		return "wifi"
	case strings.Contains(st, "lan") || strings.Contains(st, "cable") || strings.Contains(st, "ethernet") || strings.Contains(st, "routing"):
		return "network"
	case strings.Contains(st, "repair") || strings.Contains(st, "maintenance") || strings.Contains(st, "troubleshoot"):
		return "maintenance"
	default:
		return "internet"
	}
}

// ListISPCards returns all active ISPs formatted as e-commerce cards.
func (r *CardsRepository) ListISPCards(ctx context.Context, county string, limit int) ([]*models.ISPCard, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	query := `
		SELECT 
			i.id, i.name, COALESCE(u.display_name, i.display_name, ''),
			COALESCE(NULLIF(u.avatar_url,''), NULLIF(i.avatar_url,''), NULLIF(i.logo_url,''), ''),
			COALESCE(NULLIF(i.logo_url,''), NULLIF(u.avatar_url,''), ''),
			i.rating, i.review_count, i.county, i.sub_county,
			COALESCE(u.town, ''), COALESCE(i.avg_price, 0), i.technicians_available,
			COALESCE(u.identity_status, i.identity_status, 'unverified'),
			COALESCE(u.profile_path, '/u/'||u.id), i.is_active
		FROM isps i
		LEFT JOIN users u ON u.id = i.id
		WHERE i.is_active = true AND ($1 = '' OR lower(i.county) = lower($1))
		ORDER BY i.rating DESC, i.review_count DESC
		LIMIT $2
	`
	rows, err := r.reader.QueryContext(ctx, query, strings.TrimSpace(county), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := []*models.ISPCard{}
	for rows.Next() {
		c := &models.ISPCard{}
		if err := rows.Scan(
			&c.ID, &c.Name, &c.DisplayName, &c.IconURL, &c.LogoURL,
			&c.Rating, &c.ReviewCount, &c.County, &c.SubCounty,
			&c.Town, &c.StartingPrice, &c.TechniciansCount,
			&c.IdentityStatus, &c.ProfilePath, &c.IsActive,
		); err != nil {
			return nil, err
		}
		if c.DisplayName != "" {
			c.PrimaryName = c.DisplayName
		} else {
			c.PrimaryName = c.Name
		}
		c.BadgeLabel = models.FormatVerificationBadge(c.IdentityStatus)
		cards = append(cards, c)
	}
	return cards, rows.Err()
}

// ListTechnicianCards returns all available technicians formatted as e-commerce cards.
func (r *CardsRepository) ListTechnicianCards(ctx context.Context, county, town, skill string, limit int) ([]*models.TechnicianCard, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	query := `
		SELECT 
			t.id, t.user_id, t.name, COALESCE(u.display_name, t.display_name, ''),
			COALESCE(u.verified_name, t.verified_name, ''),
			COALESCE(u.identity_status, t.identity_status, 'unverified'),
			COALESCE(NULLIF(t.avatar_url,''), NULLIF(u.avatar_url,''), ''),
			t.rating, t.jobs_done, COALESCE(tp.price_per_hour, 0),
			COALESCE(t.county, u.county, ''), COALESCE(tp.town, u.town, ''),
			t.is_available, COALESCE(u.profile_path, '/u/'||u.id),
			COALESCE(u.isp_id::text, ''), COALESCE(isp.name, '')
		FROM technicians t
		JOIN users u ON u.id = t.user_id OR u.id = t.id
		LEFT JOIN technician_profiles tp ON tp.technician_id = u.id
		LEFT JOIN isps isp ON isp.id = u.isp_id
		WHERE u.status = 'active'
		  AND ($1 = '' OR lower(t.county) = lower($1) OR lower(u.county) = lower($1))
		  AND ($2 = '' OR lower(tp.town) = lower($2) OR lower(u.town) = lower($2))
		ORDER BY t.rating DESC, t.jobs_done DESC
		LIMIT $3
	`
	rows, err := r.reader.QueryContext(ctx, query, strings.TrimSpace(county), strings.TrimSpace(town), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := []*models.TechnicianCard{}
	userIDs := []string{}
	for rows.Next() {
		c := &models.TechnicianCard{Skills: []string{}}
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.Name, &c.DisplayName, &c.VerifiedName,
			&c.IdentityStatus, &c.IconURL, &c.Rating, &c.JobsDone,
			&c.PricePerHour, &c.County, &c.Town, &c.IsAvailable, &c.ProfilePath,
			&c.ISPID, &c.ISPName,
		); err != nil {
			return nil, err
		}
		if c.DisplayName != "" {
			c.PrimaryName = c.DisplayName
		} else {
			c.PrimaryName = c.Name
		}
		legal := c.VerifiedName
		if legal == "" {
			legal = c.Name
		}
		if legal != "" {
			c.IdentityLabel = "identity [" + legal + "]"
		}
		c.BadgeLabel = models.FormatVerificationBadge(c.IdentityStatus)
		cards = append(cards, c)
		userIDs = append(userIDs, c.UserID)
	}

	// Fetch skills for technicians
	if len(userIDs) > 0 {
		skillQuery := `
			SELECT technician_id, skill FROM technician_profile_skills
			WHERE technician_id = ANY($1)
			ORDER BY skill
		`
		if sRows, err := r.reader.QueryContext(ctx, skillQuery, userIDs); err == nil {
			defer sRows.Close()
			skillMap := make(map[string][]string)
			for sRows.Next() {
				var tid, sk string
				if err := sRows.Scan(&tid, &sk); err == nil {
					skillMap[tid] = append(skillMap[tid], sk)
				}
			}
			for _, c := range cards {
				if sks, ok := skillMap[c.UserID]; ok {
					c.Skills = sks
				}
			}
		}
	}

	return cards, rows.Err()
}

// ListCustomerWithJobsCards returns customers with active/posted jobs formatted as e-commerce cards.
func (r *CardsRepository) ListCustomerWithJobsCards(ctx context.Context, county string, limit int) ([]*models.CustomerWithJobsCard, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	query := `
		SELECT 
			u.id, u.name, COALESCE(u.display_name, ''), COALESCE(u.verified_name, ''),
			COALESCE(u.identity_status, 'unverified'), COALESCE(u.avatar_url, ''),
			COALESCE(u.county, ''), COALESCE(u.town, ''),
			count(s.id) as active_job_count,
			COALESCE(u.isp_id::text, ''), COALESCE(isp.name, '')
		FROM users u
		JOIN service_requests s ON s.customer_id = u.id
		LEFT JOIN isps isp ON isp.id = u.isp_id
		WHERE s.deleted_at IS NULL AND s.status IN ('open', 'pending', 'assigned', 'in_progress')
		  AND ($1 = '' OR lower(u.county) = lower($1) OR lower(s.county) = lower($1))
		GROUP BY u.id, u.name, u.display_name, u.verified_name, u.identity_status, u.avatar_url, u.county, u.town, u.isp_id, isp.name
		ORDER BY active_job_count DESC
		LIMIT $2
	`
	rows, err := r.reader.QueryContext(ctx, query, strings.TrimSpace(county), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := []*models.CustomerWithJobsCard{}
	for rows.Next() {
		c := &models.CustomerWithJobsCard{}
		if err := rows.Scan(
			&c.CustomerID, &c.Name, &c.DisplayName, &c.VerifiedName,
			&c.IdentityStatus, &c.IconURL, &c.County, &c.Town,
			&c.ActiveJobsCount, &c.ISPID, &c.ISPName,
		); err != nil {
			return nil, err
		}
		if c.DisplayName != "" {
			c.PrimaryName = c.DisplayName
		} else {
			c.PrimaryName = c.Name
		}
		legal := c.VerifiedName
		if legal == "" {
			legal = c.Name
		}
		if legal != "" {
			c.IdentityLabel = "identity [" + legal + "]"
		}
		c.BadgeLabel = models.FormatVerificationBadge(c.IdentityStatus)

		// Fetch latest job details for this customer
		var jTitle, jService string
		var jBudget float64
		var jDate sql.NullTime
		if err := r.reader.QueryRowContext(ctx, `
			SELECT service_type, service_type, budget, created_at
			FROM service_requests
			WHERE customer_id = $1 AND deleted_at IS NULL AND status IN ('open','pending','assigned','in_progress')
			ORDER BY created_at DESC LIMIT 1
		`, c.CustomerID).Scan(&jTitle, &jService, &jBudget, &jDate); err == nil {
			c.LatestJobTitle = jTitle
			c.LatestJobService = jService
			c.LatestJobBudget = jBudget
			if jDate.Valid {
				c.LatestJobDate = &jDate.Time
			}
		}

		cards = append(cards, c)
	}
	return cards, rows.Err()
}

// ListJobCards returns all active/open service requests formatted as e-commerce product cards.
func (r *CardsRepository) ListJobCards(ctx context.Context, county, town, serviceType string, limit int) ([]*models.JobCard, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	query := `
		SELECT 
			s.id, s.service_type, s.description, s.budget, s.county, s.town, s.status,
			s.customer_id, u.name, COALESCE(u.display_name, ''), COALESCE(u.verified_name, ''),
			COALESCE(u.identity_status, 'unverified'), COALESCE(u.avatar_url, ''),
			(SELECT count(*) FROM service_request_applications a WHERE a.request_id = s.id),
			s.preferred_date, s.created_at,
			COALESCE(s.target_isp_id::text, ''), COALESCE(isp.name, '')
		FROM service_requests s
		JOIN users u ON u.id = s.customer_id
		LEFT JOIN isps isp ON isp.id = s.target_isp_id
		WHERE s.deleted_at IS NULL AND s.status IN ('open', 'pending')
		  AND ($1 = '' OR lower(s.county) = lower($1))
		  AND ($2 = '' OR lower(s.town) = lower($2))
		  AND ($3 = '' OR s.service_type = $3)
		ORDER BY s.created_at DESC
		LIMIT $4
	`
	rows, err := r.reader.QueryContext(ctx, query, strings.TrimSpace(county), strings.TrimSpace(town), strings.TrimSpace(serviceType), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := []*models.JobCard{}
	for rows.Next() {
		j := &models.JobCard{Currency: "KES"}
		var prefDate sql.NullTime
		var verifiedName string
		if err := rows.Scan(
			&j.ID, &j.ServiceType, &j.Description, &j.Budget, &j.County, &j.Town, &j.Status,
			&j.CustomerID, &j.CustomerName, &j.CustomerDisplayName, &verifiedName,
			&j.CustomerBadge, &j.CustomerAvatarURL, &j.ApplicationsCount,
			&prefDate, &j.CreatedAt,
			&j.TargetISPID, &j.TargetISPName,
		); err != nil {
			return nil, err
		}
		if prefDate.Valid {
			j.PreferredDate = &prefDate.Time
		}
		j.Title = j.ServiceType
		j.IconType = iconTypeForService(j.ServiceType)
		if j.CustomerDisplayName != "" {
			j.CustomerPrimaryName = j.CustomerDisplayName
		} else {
			j.CustomerPrimaryName = j.CustomerName
		}
		legal := verifiedName
		if legal == "" {
			legal = j.CustomerName
		}
		if legal != "" {
			j.CustomerIdentity = "identity [" + legal + "]"
		}
		j.CustomerBadge = models.FormatVerificationBadge(j.CustomerBadge)
		cards = append(cards, j)
	}
	return cards, rows.Err()
}

// GetMarketplaceCardsOverview returns an aggregated view of all 4 card categories.
func (r *CardsRepository) GetMarketplaceCardsOverview(ctx context.Context, county string, limit int) (*models.MarketplaceCardsOverview, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	isps, err := r.ListISPCards(ctx, county, limit)
	if err != nil {
		return nil, err
	}
	techs, err := r.ListTechnicianCards(ctx, county, "", "", limit)
	if err != nil {
		return nil, err
	}
	custs, err := r.ListCustomerWithJobsCards(ctx, county, limit)
	if err != nil {
		return nil, err
	}
	jobs, err := r.ListJobCards(ctx, county, "", "", limit)
	if err != nil {
		return nil, err
	}

	return &models.MarketplaceCardsOverview{
		ISPs:              isps,
		Technicians:       techs,
		CustomersWithJobs: custs,
		Jobs:              jobs,
		TotalISPs:         len(isps),
		TotalTechnicians:  len(techs),
		TotalCustomers:    len(custs),
		TotalJobs:         len(jobs),
	}, nil
}
