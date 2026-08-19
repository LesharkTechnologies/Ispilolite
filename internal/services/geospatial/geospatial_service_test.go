package geospatial

import (
	"context"
	"testing"

	"ispilolite/internal/models"
)

type testGeoRepo struct{}

func (testGeoRepo) NearbyISPs(context.Context, float64, float64, float64, int) ([]*models.ISP, error) {
	return []*models.ISP{{ID: "isp-1"}}, nil
}
func (testGeoRepo) NearbyTechnicians(context.Context, float64, float64, float64, []string) ([]*models.Technician, error) {
	return []*models.Technician{{ID: "tech-1"}}, nil
}
func (testGeoRepo) CheckCoverage(context.Context, float64, float64) ([]*models.ISP, error) {
	return nil, nil
}
func (testGeoRepo) SearchLocations(context.Context, string, string, int) ([]*models.Location, error) {
	return nil, nil
}

func TestGeospatialDefaultsAndValidation(t *testing.T) {
	service := NewGeospatialService(testGeoRepo{})
	result, err := service.FindNearbyISPs(context.Background(), -1.28, 36.82, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.RadiusKM != 10 || result.Total != 1 {
		t.Fatalf("unexpected defaults/result: %+v", result)
	}
	if _, err := service.FindNearbyISPs(context.Background(), 100, 36.82, 10, 10); err == nil {
		t.Fatal("invalid coordinates were accepted")
	}
}
