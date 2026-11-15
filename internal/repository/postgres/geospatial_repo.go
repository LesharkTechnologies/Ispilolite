package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/lib/pq"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type GeospatialRepository struct{ db *sql.DB }

func NewGeospatialRepository() (*GeospatialRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	return &GeospatialRepository{db: reader}, nil
}

func (r *GeospatialRepository) NearbyISPs(ctx context.Context, lat, lon, radius float64, limit int) ([]*models.ISP, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT u.id,u.name,COALESCE(u.phone,''),COALESCE(u.rating,0),COALESCE(u.total_ratings,0),COALESCE(u.county,'') FROM users u JOIN isp_coverage_locations c ON c.isp_id=u.id JOIN locations l ON l.id=c.location_id WHERE u.role='isp' AND u.status='active' AND l.latitude IS NOT NULL AND l.longitude IS NOT NULL AND (6371*2*asin(sqrt(power(sin(radians(l.latitude-$1)/2),2)+cos(radians($1))*cos(radians(l.latitude))*power(sin(radians(l.longitude-$2)/2),2)))) <= $3 ORDER BY u.rating DESC LIMIT $4`, lat, lon, radius, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.ISP{}
	for rows.Next() {
		v := &models.ISP{}
		if err := rows.Scan(&v.ID, &v.Name, &v.CustomerCareNumber, &v.Rating, &v.ReviewCount, &v.County); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *GeospatialRepository) NearbyTechnicians(ctx context.Context, lat, lon, radius float64, skills []string) ([]*models.Technician, error) {
	for i := range skills {
		skills[i] = strings.ToLower(strings.TrimSpace(skills[i]))
	}
	rows, err := r.db.QueryContext(ctx, `WITH nearby AS (
		SELECT u.id,u.name,COALESCE(u.isp_id::text,'') isp_id,COALESCE(u.rating,0) rating,
		COALESCE(u.total_ratings,0) reviews,COALESCE(u.county,'') county,u.latitude,u.longitude,
		6371*2*asin(sqrt(power(sin(radians(u.latitude-$1)/2),2)+cos(radians($1))*cos(radians(u.latitude))*power(sin(radians(u.longitude-$2)/2),2))) distance_km
		FROM users u WHERE u.role='technician' AND u.status='active' AND u.latitude IS NOT NULL AND u.longitude IS NOT NULL)
		SELECT n.id,n.id,n.name,'','',n.isp_id,n.rating,n.reviews,n.county,n.latitude,n.longitude,n.distance_km
		FROM nearby n WHERE n.distance_km<=$3 AND (cardinality($4::text[])=0 OR EXISTS(
			SELECT 1 FROM technician_profile_skills s WHERE s.technician_id=n.id AND lower(s.skill)=ANY($4::text[])))
		ORDER BY n.distance_km,n.rating DESC LIMIT 100`, lat, lon, radius, pq.Array(skills))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.Technician{}
	for rows.Next() {
		v := &models.Technician{}
		var techLat, techLon float64
		if err := rows.Scan(&v.ID, &v.UserID, &v.Name, &v.Phone, &v.Email, &v.ISPID, &v.Rating, &v.ReviewCount, &v.County, &techLat, &techLon, &v.Distance); err != nil {
			return nil, err
		}
		v.Location = &models.GeoPoint{Lat: techLat, Lon: techLon}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *GeospatialRepository) CheckCoverage(ctx context.Context, lat, lon float64) ([]*models.ISP, error) {
	return r.NearbyISPs(ctx, lat, lon, 10, 100)
}

func (r *GeospatialRepository) SearchLocations(ctx context.Context, query, kind string, limit int) ([]*models.Location, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,type,COALESCE(parent_id::text,''),county,sub_county,ward,created_at,status,is_verified,submission_count,popularity_score FROM locations WHERE name ILIKE '%'||$1||'%' AND ($2='' OR type=$2) ORDER BY popularity_score DESC,name LIMIT $3`, query, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.Location{}
	for rows.Next() {
		v := &models.Location{}
		if err := rows.Scan(&v.ID, &v.Name, &v.Type, &v.ParentID, &v.County, &v.SubCounty, &v.Ward, &v.CreatedAt, &v.Status, &v.IsVerified, &v.SubmissionCount, &v.PopularityScore); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
