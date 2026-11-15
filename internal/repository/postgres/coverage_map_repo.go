package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"ispilolite/internal/models"
	"ispilolite/pkg/database"
)

type CoverageMapRepository struct{ reader, writer *sql.DB }

func NewCoverageMapRepository() (*CoverageMapRepository, error) {
	reader, err := database.MustReader()
	if err != nil {
		return nil, err
	}
	writer, err := database.MustWriter()
	if err != nil {
		return nil, err
	}
	return &CoverageMapRepository{reader: reader, writer: writer}, nil
}

func (r *CoverageMapRepository) Create(ctx context.Context, item *models.CoveragePolygon) error {
	geometry, err := json.Marshal(item.Geometry)
	if err != nil {
		return err
	}
	_, err = r.writer.ExecContext(ctx, `INSERT INTO isp_coverage_polygons(id,isp_id,name,county,geojson,min_lat,max_lat,min_lon,max_lon,stroke_color,fill_color,active,created_at,updated_at) SELECT $1,u.id,$3,$4,$5,$6,$7,$8,$9,$10,$11,true,$12,$12 FROM users u WHERE u.id=$2 AND u.role='isp' AND u.status='active'`, item.ID, item.ISPID, item.Name, item.County, geometry, item.MinLat, item.MaxLat, item.MinLon, item.MaxLon, item.StrokeColor, item.FillColor, item.CreatedAt)
	return err
}

func (r *CoverageMapRepository) List(ctx context.Context, ispID, county string, lat, lon *float64, limit int) ([]*models.CoveragePolygon, error) {
	rows, err := r.reader.QueryContext(ctx, `SELECT p.id,p.isp_id,COALESCE(u.name,''),p.name,p.county,p.geojson,p.stroke_color,p.fill_color,p.active,p.min_lat,p.max_lat,p.min_lon,p.max_lon,p.created_at,p.updated_at FROM isp_coverage_polygons p JOIN users u ON u.id=p.isp_id WHERE p.active=true AND ($1='' OR p.isp_id=$1::uuid) AND ($2='' OR lower(p.county)=lower($2)) AND ($3::float8 IS NULL OR ($3 BETWEEN p.min_lat AND p.max_lat AND $4 BETWEEN p.min_lon AND p.max_lon)) ORDER BY p.updated_at DESC LIMIT $5`, ispID, county, lat, lon, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.CoveragePolygon, 0)
	for rows.Next() {
		item := &models.CoveragePolygon{}
		var raw []byte
		if err := rows.Scan(&item.ID, &item.ISPID, &item.ISPName, &item.Name, &item.County, &raw, &item.StrokeColor, &item.FillColor, &item.Active, &item.MinLat, &item.MaxLat, &item.MinLon, &item.MaxLon, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Geometry); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *CoverageMapRepository) Delete(ctx context.Context, id, ispID string) error {
	var result sql.Result
	var err error
	if ispID == "" {
		result, err = r.writer.ExecContext(ctx, `UPDATE isp_coverage_polygons SET active=false, updated_at=NOW() WHERE id=$1 AND active=true`, id)
	} else {
		result, err = r.writer.ExecContext(ctx, `UPDATE isp_coverage_polygons SET active=false, updated_at=NOW() WHERE id=$1 AND isp_id=$2::uuid AND active=true`, id, ispID)
	}
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
