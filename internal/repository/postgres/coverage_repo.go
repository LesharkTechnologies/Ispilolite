package postgres

import (
	"database/sql"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type coverageRepository struct{ dbReader, dbWriter *sql.DB }

const joinedLocationColumns = `l.id, l.name, l.type, COALESCE(l.parent_id::text, ''), COALESCE(l.county, ''), COALESCE(l.sub_county, ''), COALESCE(l.ward, ''), l.latitude, l.longitude, l.status, l.is_verified, l.submission_count, l.popularity_score, l.created_at`

func NewCoverageRepository() (*coverageRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &coverageRepository{dbReader: reader, dbWriter: writer}, nil
}

func (r *coverageRepository) ListISPCoverage(ispID, county string) ([]*models.Location, error) {
	rows, err := r.dbReader.Query(`SELECT `+joinedLocationColumns+` FROM locations l JOIN isp_coverage_locations c ON c.location_id=l.id WHERE c.isp_id=$1 AND ($2='' OR lower(l.county)=lower($2)) ORDER BY l.county,l.type,l.name`, ispID, county)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLocations(rows)
}

func (r *coverageRepository) AddISPCoverage(ispID, locationID string) error {
	_, err := r.dbWriter.Exec(`INSERT INTO isp_coverage_locations (isp_id,location_id,created_at) SELECT $1,id,now() FROM locations WHERE id=$2 ON CONFLICT (isp_id,location_id) DO NOTHING`, ispID, locationID)
	if err != nil {
		return err
	}
	_, _ = r.dbWriter.Exec(`
		INSERT INTO isp_serviceability (id, isp_id, location_id, county, town, estate, building, technology, status, estimated_days, reason, active)
		SELECT gen_random_uuid(), $1, l.id, l.county, l.name, '', '', 'Fiber / Fixed Wireless', 'serviceable', 2, 'Verified network coverage', true
		FROM locations l
		WHERE l.id = $2
		ON CONFLICT (isp_id, COALESCE(location_id::text, ''), lower(county), lower(town), lower(estate), lower(building))
		DO UPDATE SET active = true, updated_at = now()`, ispID, locationID)
	return nil
}

func (r *coverageRepository) RemoveISPCoverage(ispID, locationID string) error {
	result, err := r.dbWriter.Exec(`DELETE FROM isp_coverage_locations WHERE isp_id=$1 AND location_id=$2`, ispID, locationID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	_, _ = r.dbWriter.Exec(`UPDATE isp_serviceability SET active=false, updated_at=now() WHERE isp_id=$1 AND location_id=$2`, ispID, locationID)
	return nil
}

func (r *coverageRepository) ListCoverageRecommendations(ispID, county string, limit int) ([]*models.Location, error) {
	rows, err := r.dbReader.Query(`SELECT `+joinedLocationColumns+` FROM locations l WHERE l.type IN ('town','sub_county','ward','village') AND l.submission_count > 0 AND ($2='' OR lower(l.county)=lower($2)) AND NOT EXISTS (SELECT 1 FROM isp_coverage_locations c WHERE c.isp_id=$1 AND c.location_id=l.id) ORDER BY l.is_verified DESC,l.popularity_score DESC,l.submission_count DESC,l.name LIMIT $3`, ispID, county, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLocations(rows)
}
