package models

import "time"

// GeoJSONPolygon uses GeoJSON coordinate order: longitude, latitude.
type GeoJSONPolygon struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

type CoveragePolygon struct {
	ID          string         `json:"id"`
	ISPID       string         `json:"isp_id"`
	ISPName     string         `json:"isp_name,omitempty"`
	Name        string         `json:"name"`
	County      string         `json:"county"`
	Geometry    GeoJSONPolygon `json:"geometry"`
	StrokeColor string         `json:"stroke_color"`
	FillColor   string         `json:"fill_color"`
	Active      bool           `json:"active"`
	MinLat      float64        `json:"-"`
	MaxLat      float64        `json:"-"`
	MinLon      float64        `json:"-"`
	MaxLon      float64        `json:"-"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type CoverageFeatureCollection struct {
	Type     string             `json:"type"`
	Features []*CoverageFeature `json:"features"`
}

type CoverageFeature struct {
	Type       string                 `json:"type"`
	ID         string                 `json:"id"`
	Geometry   GeoJSONPolygon         `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}
