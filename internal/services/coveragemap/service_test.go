package coveragemap

import (
	"testing"

	"ispilolite/internal/models"
)

func TestValidatePolygonAndPointContainment(t *testing.T) {
	polygon := models.GeoJSONPolygon{Type: "Polygon", Coordinates: [][][]float64{{
		{36, -2}, {37, -2}, {37, -1}, {36, -1}, {36, -2},
	}}}
	if _, _, _, _, err := validatePolygon(polygon); err != nil {
		t.Fatalf("valid polygon rejected: %v", err)
	}
	if !pointInPolygon(36.5, -1.5, polygon.Coordinates[0]) {
		t.Fatal("point inside polygon was not detected")
	}
	if pointInPolygon(40, -1.5, polygon.Coordinates[0]) {
		t.Fatal("point outside polygon was accepted")
	}
}

func TestValidatePolygonRequiresClosedRing(t *testing.T) {
	polygon := models.GeoJSONPolygon{Type: "Polygon", Coordinates: [][][]float64{{
		{36, -2}, {37, -2}, {37, -1}, {36, -1},
	}}}
	if _, _, _, _, err := validatePolygon(polygon); err == nil {
		t.Fatal("open polygon ring was accepted")
	}
}
