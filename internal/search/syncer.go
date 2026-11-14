package search

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/lib/pq"

	"ispilolite/internal/models"
)

// Syncer periodically copies Postgres source-of-truth rows into Elasticsearch.
// Full bulk upserts are deliberately idempotent; deployments can later replace
// this with CDC without changing the read path.
type Syncer struct {
	db       *sql.DB
	indexer  *Indexer
	logger   *log.Logger
	interval time.Duration
}

func NewSyncer(db *sql.DB, client *Client, logger *log.Logger) *Syncer {
	if logger == nil {
		logger = log.Default()
	}
	interval := time.Minute
	if raw := os.Getenv("ES_SYNC_INTERVAL"); raw != "" {
		if value, err := time.ParseDuration(raw); err == nil && value >= 10*time.Second {
			interval = value
		}
	}
	return &Syncer{db: db, indexer: NewIndexer(client), logger: logger, interval: interval}
}

// Run performs an immediate sync then repeats until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context) {
	if s == nil || s.db == nil || s.indexer == nil {
		return
	}
	s.syncWithTimeout(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncWithTimeout(ctx)
		}
	}
}

func (s *Syncer) syncWithTimeout(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	counts, err := s.SyncAll(ctx)
	if err != nil {
		s.logger.Printf("search index sync failed: %v", err)
		return
	}
	s.logger.Printf("search index sync complete: isps=%d technicians=%d locations=%d", counts["isps"], counts["technicians"], counts["locations"])
}

func (s *Syncer) SyncAll(ctx context.Context) (map[string]int, error) {
	isps, err := s.loadISPs(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.indexer.BulkIndexISPs(ctx, isps); err != nil {
		return nil, err
	}
	technicians, err := s.loadTechnicians(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.indexer.BulkIndexTechnicians(ctx, technicians); err != nil {
		return nil, err
	}
	locations, err := s.loadLocations(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.indexer.BulkIndexLocations(ctx, locations); err != nil {
		return nil, err
	}
	return map[string]int{"isps": len(isps), "technicians": len(technicians), "locations": len(locations)}, nil
}

func (s *Syncer) loadISPs(ctx context.Context) ([]*models.ISP, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,COALESCE(description,''),COALESCE(avatar_url,''),COALESCE(county,''),COALESCE(sub_county,''),COALESCE(village,''),COALESCE(rating,0),COALESCE(review_count,0),COALESCE(is_active,true),created_at FROM isps`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.ISP, 0)
	for rows.Next() {
		item := &models.ISP{}
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.AvatarURL, &item.County, &item.SubCounty, &item.Village, &item.Rating, &item.ReviewCount, &item.IsActive, &item.CreatedAt); err != nil {
			return nil, err
		}
		coverageRows, err := s.db.QueryContext(ctx, `SELECT l.name FROM isp_coverage_locations c JOIN locations l ON l.id=c.location_id WHERE c.isp_id=$1 ORDER BY l.name`, item.ID)
		if err == nil {
			for coverageRows.Next() {
				var name string
				if coverageRows.Scan(&name) == nil {
					item.Coverage = append(item.Coverage, name)
				}
			}
			coverageRows.Close()
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Syncer) loadTechnicians(ctx context.Context) ([]*models.Technician, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.user_id,t.name,COALESCE(t.avatar_url,''),COALESCE(t.isp_id::text,''),COALESCE(t.isp_name,''),COALESCE(t.county,''),COALESCE(t.sub_county,''),COALESCE(t.village,''),COALESCE(t.rating,0),COALESCE(t.jobs_done,0),COALESCE(t.is_available,false),COALESCE(array_agg(DISTINCT r.role) FILTER(WHERE r.role IS NOT NULL),'{}'),COALESCE(array_agg(DISTINCT sk.skill) FILTER(WHERE sk.skill IS NOT NULL),'{}'),t.lat,t.lon FROM technicians t LEFT JOIN technician_roles r ON r.technician_id=t.id LEFT JOIN technician_skills sk ON sk.technician_id=t.id GROUP BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.Technician, 0)
	for rows.Next() {
		item := &models.Technician{}
		var roles, skills pq.StringArray
		var lat, lon sql.NullFloat64
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &item.AvatarURL, &item.ISPID, &item.ISPName, &item.County, &item.SubCounty, &item.Village, &item.Rating, &item.JobsDone, &item.IsAvailable, &roles, &skills, &lat, &lon); err != nil {
			return nil, err
		}
		item.Roles, item.Skills = roles, skills
		if lat.Valid && lon.Valid {
			item.Location = &models.GeoPoint{Lat: lat.Float64, Lon: lon.Float64}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Syncer) loadLocations(ctx context.Context) ([]*models.Location, error) {
	limit := 100000
	if raw := os.Getenv("ES_LOCATION_SYNC_LIMIT"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			limit = value
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT l.id,l.name,l.type,COALESCE(l.parent_id::text,''),COALESCE(l.county,''),COALESCE(l.sub_county,''),COALESCE(l.ward,''),l.latitude,l.longitude,l.created_at,COALESCE(l.status,''),COALESCE(l.is_verified,false),COALESCE(l.submission_count,0),COALESCE(l.popularity_score,0),COALESCE(array_agg(a.alias) FILTER(WHERE a.status='approved'),'{}') FROM locations l LEFT JOIN location_aliases a ON a.location_id=l.id GROUP BY l.id ORDER BY l.popularity_score DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*models.Location, 0)
	for rows.Next() {
		item := &models.Location{}
		var lat, lon sql.NullFloat64
		var aliases pq.StringArray
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.ParentID, &item.County, &item.SubCounty, &item.Ward, &lat, &lon, &item.CreatedAt, &item.Status, &item.IsVerified, &item.SubmissionCount, &item.PopularityScore, &aliases); err != nil {
			return nil, err
		}
		item.Aliases = aliases
		if lat.Valid && lon.Valid {
			item.Point = &models.GeoPoint{Lat: lat.Float64, Lon: lon.Float64}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

var defaultSyncer struct {
	sync.RWMutex
	value *Syncer
}

func SetDefaultSyncer(value *Syncer) {
	defaultSyncer.Lock()
	defaultSyncer.value = value
	defaultSyncer.Unlock()
}
func StartDefaultSyncer(ctx context.Context) {
	defaultSyncer.RLock()
	value := defaultSyncer.value
	defaultSyncer.RUnlock()
	if value != nil {
		go value.Run(ctx)
	}
}
