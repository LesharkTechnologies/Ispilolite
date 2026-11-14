package coveragemap

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"ispilolite/internal/models"
	"ispilolite/internal/utils"
)

var ErrInvalid = errors.New("invalid coverage polygon")

type Repository interface {
	Create(context.Context, *models.CoveragePolygon) error
	List(context.Context, string, string, *float64, *float64, int) ([]*models.CoveragePolygon, error)
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Create(ctx context.Context, ispID string, item *models.CoveragePolygon) (*models.CoveragePolygon, error) {
	if item == nil || strings.TrimSpace(item.Name) == "" {
		return nil, ErrInvalid
	}
	minLat, maxLat, minLon, maxLon, err := validatePolygon(item.Geometry)
	if err != nil {
		return nil, err
	}
	item.ID, item.ISPID = utils.GenerateID(), ispID
	item.Name, item.County = strings.TrimSpace(item.Name), strings.TrimSpace(item.County)
	item.MinLat, item.MaxLat, item.MinLon, item.MaxLon = minLat, maxLat, minLon, maxLon
	if item.StrokeColor == "" {
		item.StrokeColor = "#2563EB"
	}
	if item.FillColor == "" {
		item.FillColor = "#3B82F6"
	}
	item.Active = true
	item.CreatedAt = time.Now().UTC()
	item.UpdatedAt = item.CreatedAt
	if err := s.repo.Create(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) FeatureCollection(ctx context.Context, ispID, county string, lat, lon *float64, limit int) (*models.CoverageFeatureCollection, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items, err := s.repo.List(ctx, ispID, county, lat, lon, limit)
	if err != nil {
		return nil, err
	}
	result := &models.CoverageFeatureCollection{Type: "FeatureCollection", Features: make([]*models.CoverageFeature, 0)}
	for _, item := range items {
		if lat != nil && lon != nil && !pointInPolygon(*lon, *lat, item.Geometry.Coordinates[0]) {
			continue
		}
		result.Features = append(result.Features, &models.CoverageFeature{
			Type: "Feature", ID: item.ID, Geometry: item.Geometry,
			Properties: map[string]interface{}{
				"isp_id": item.ISPID, "isp_name": item.ISPName, "name": item.Name,
				"county": item.County, "stroke_color": item.StrokeColor, "fill_color": item.FillColor,
			},
		})
	}
	return result, nil
}

func validatePolygon(p models.GeoJSONPolygon) (float64, float64, float64, float64, error) {
	if p.Type != "Polygon" || len(p.Coordinates) == 0 || len(p.Coordinates[0]) < 4 {
		return 0, 0, 0, 0, ErrInvalid
	}
	points := 0
	minLat, maxLat, minLon, maxLon := 90.0, -90.0, 180.0, -180.0
	for _, ring := range p.Coordinates {
		if len(ring) < 4 {
			return 0, 0, 0, 0, ErrInvalid
		}
		for _, point := range ring {
			points++
			if points > 5000 || len(point) < 2 || math.IsNaN(point[0]) || math.IsNaN(point[1]) ||
				point[0] < -180 || point[0] > 180 || point[1] < -90 || point[1] > 90 {
				return 0, 0, 0, 0, ErrInvalid
			}
			minLon, maxLon = math.Min(minLon, point[0]), math.Max(maxLon, point[0])
			minLat, maxLat = math.Min(minLat, point[1]), math.Max(maxLat, point[1])
		}
		first, last := ring[0], ring[len(ring)-1]
		if first[0] != last[0] || first[1] != last[1] {
			return 0, 0, 0, 0, ErrInvalid
		}
	}
	return minLat, maxLat, minLon, maxLon, nil
}

func pointInPolygon(x, y float64, ring [][]float64) bool {
	inside := false
	j := len(ring) - 1
	for i := 0; i < len(ring); i++ {
		xi, yi := ring[i][0], ring[i][1]
		xj, yj := ring[j][0], ring[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
}
