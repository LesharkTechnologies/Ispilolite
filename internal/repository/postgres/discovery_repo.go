package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/lib/pq"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

// DiscoveryRepository reads the public, aggregate-only discovery surfaces.
// It never selects customer, technician or waitlist identities.
type DiscoveryRepository struct {
	dbReader *sql.DB
	dbWriter *sql.DB
}

func NewDiscoveryRepository() (*DiscoveryRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &DiscoveryRepository{dbReader: reader, dbWriter: writer}, nil
}

// Coverage returns the ISPs and public packages for a county, town or estate.
// A nil or empty town/estate broadens the search to the parent area.
func (r *DiscoveryRepository) Coverage(ctx context.Context, county, town, estate string) (*models.PublicCoveragePage, error) {
	county = strings.TrimSpace(county)
	town = strings.TrimSpace(town)
	estate = strings.TrimSpace(estate)

	page := &models.PublicCoveragePage{
		County: county,
		Town:   town,
		Estate: estate,
		ISPs:   []*models.PublicCoverageISP{},
	}

	rows, err := r.dbReader.QueryContext(ctx, `
		SELECT i.id,i.name,i.description,
		       COALESCE(NULLIF(i.logo_url,''),NULLIF(u.avatar_url,''),''),
		       i.rating,i.review_count,i.county,i.technicians_available,
		       COALESCE(u.profile_path,''),
		       COALESCE(sa.status,''),COALESCE(sa.estimated_days,0),
		       COALESCE(sa.town,''),COALESCE(sa.estate,'')
		FROM isps i
		JOIN users u ON u.id=i.id
		LEFT JOIN LATERAL (
			SELECT s.status,s.estimated_days,s.town,s.estate
			FROM isp_serviceability s
			WHERE s.isp_id=i.id AND s.active
			  AND (
				lower(s.county)=lower($1)
				OR regexp_replace(lower(s.county),'[^[:alnum:]]','','g')=regexp_replace(lower($1),'[^[:alnum:]]','','g')
			  )
			  AND (
				$2=''
				OR lower(s.town)=lower($2)
				OR regexp_replace(lower(s.town),'[^[:alnum:]]','','g')=regexp_replace(lower($2),'[^[:alnum:]]','','g')
			  )
			  AND (
				$3=''
				OR lower(s.estate)=lower($3)
				OR regexp_replace(lower(s.estate),'[^[:alnum:]]','','g')=regexp_replace(lower($3),'[^[:alnum:]]','','g')
			  )
			ORDER BY CASE s.status
				WHEN 'serviceable' THEN 0
				WHEN 'likely_serviceable' THEN 1
				WHEN 'survey_required' THEN 2
				ELSE 3 END, s.updated_at DESC
			LIMIT 1
		) sa ON true
		WHERE i.is_active=true AND u.status='active'
		  AND (
			sa.status IS NOT NULL
			OR lower(i.county)=lower($1)
			OR regexp_replace(lower(i.county),'[^[:alnum:]]','','g')=regexp_replace(lower($1),'[^[:alnum:]]','','g')
			OR EXISTS (
				SELECT 1
				FROM isp_coverage_locations c
				JOIN locations l ON l.id=c.location_id
				WHERE c.isp_id=i.id
				  AND (
					lower(l.county)=lower($1)
					OR regexp_replace(lower(l.county),'[^[:alnum:]]','','g')=regexp_replace(lower($1),'[^[:alnum:]]','','g')
				  )
				  AND (
					$2=''
					OR lower(l.name)=lower($2)
					OR lower(l.sub_county)=lower($2)
					OR lower(COALESCE(l.ward,''))=lower($2)
					OR regexp_replace(lower(l.name),'[^[:alnum:]]','','g')=regexp_replace(lower($2),'[^[:alnum:]]','','g')
					OR regexp_replace(lower(l.sub_county),'[^[:alnum:]]','','g')=regexp_replace(lower($2),'[^[:alnum:]]','','g')
					OR regexp_replace(lower(COALESCE(l.ward,'')),'[^[:alnum:]]','','g')=regexp_replace(lower($2),'[^[:alnum:]]','','g')
				  )
			)
		  )
		ORDER BY CASE COALESCE(sa.status,'')
			WHEN 'serviceable' THEN 0
			WHEN 'likely_serviceable' THEN 1
			WHEN 'survey_required' THEN 2
			ELSE 3 END,
			i.rating DESC,i.review_count DESC,i.name
		LIMIT 50`, county, town, estate)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*models.PublicCoverageISP)
	ispIDs := make([]string, 0, 50)
	for rows.Next() {
		isp := &models.PublicCoverageISP{Packages: []*models.PublicCoveragePackage{}}
		if err := rows.Scan(
			&isp.ID, &isp.Name, &isp.Description, &isp.LogoURL,
			&isp.Rating, &isp.ReviewCount, &isp.County, &isp.TechniciansAvailable,
			&isp.ProfilePath, &isp.Serviceability, &isp.EstimatedDays,
			&isp.Town, &isp.Estate,
		); err != nil {
			rows.Close()
			return nil, err
		}
		page.ISPs = append(page.ISPs, isp)
		byID[isp.ID] = isp
		ispIDs = append(ispIDs, isp.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if len(page.ISPs) > 0 && strings.TrimSpace(page.ISPs[0].County) != "" {
		page.County = page.ISPs[0].County
	}
	priceCounty := page.County
	if priceCounty == "" {
		priceCounty = county
	}

	if len(ispIDs) > 0 {
		pkgRows, err := r.dbReader.QueryContext(ctx, `
			SELECT p.id,p.isp_id,p.name,p.category,p.speed_value,
			       COALESCE(su.symbol,''),COALESCE(NULLIF(p.speed,''),''),
			       COALESCE(cp.price,p.base_price),p.billing_cycle,p.description
			FROM isp_packages p
			LEFT JOIN package_units su ON su.id=p.speed_unit_id
			LEFT JOIN isp_package_county_prices cp
			  ON cp.package_id=p.id AND lower(cp.county)=lower($1)
			WHERE p.isp_id = ANY($2)
			  AND p.is_active=true AND p.archived_at IS NULL
			ORDER BY COALESCE(cp.price,p.base_price),p.speed_value DESC,p.name
			LIMIT 500`, priceCounty, pq.Array(ispIDs))
		if err != nil {
			return nil, err
		}
		defer pkgRows.Close()
		for pkgRows.Next() {
			pkg := &models.PublicCoveragePackage{}
			var ispID string
			if err := pkgRows.Scan(
				&pkg.ID, &ispID, &pkg.Name, &pkg.Category, &pkg.SpeedValue,
				&pkg.SpeedUnit, &pkg.Speed, &pkg.Price, &pkg.BillingCycle, &pkg.Description,
			); err != nil {
				return nil, err
			}
			isp := byID[ispID]
			if isp == nil {
				continue
			}
			isp.Packages = append(isp.Packages, pkg)
			page.PackageCount++
			if pkg.Price > 0 && (page.FromPrice == 0 || pkg.Price < page.FromPrice) {
				page.FromPrice = pkg.Price
			}
			if pkg.Price > 0 && (isp.FromPrice == 0 || pkg.Price < isp.FromPrice) {
				isp.FromPrice = pkg.Price
			}
		}
		if err := pkgRows.Err(); err != nil {
			return nil, err
		}
	}

	page.ISPCount = len(page.ISPs)
	return page, nil
}

// RecordView stores a coarse demand signal for a public page. It deliberately
// accepts only a page key and a referrer host; caller IPs and user agents are
// never persisted.
func (r *DiscoveryRepository) RecordView(ctx context.Context, pageType, pageKey, referrer string) error {
	_, err := r.dbWriter.ExecContext(ctx, `
		INSERT INTO coverage_page_views (id,page_type,page_key,referrer)
		VALUES (gen_random_uuid(),$1,$2,$3)`, pageType, pageKey, referrer)
	return err
}
