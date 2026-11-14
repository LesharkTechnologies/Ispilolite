package search

import (
	"context"
	"log"
	"reflect"
	"strconv"
	"strings"
	"time"

	"ispilolite/api/dto"
	"ispilolite/pkg/monitoring"
)

// Source labels used in SearchMeta.Source.
const (
	sourceES = "elasticsearch"
	sourcePG = "postgres_fallback"
)

// FallbackStrategy selects how the primary and fallback backends combine.
const (
	StrategyESFirst = "es_first" // try ES, fall back to PG on failure (default)
	StrategyPGFirst = "pg_first" // try PG, fall back to ES on failure
	StrategyESOnly  = "es_only"  // ES only; error if unavailable
	StrategyPGOnly  = "pg_only"  // PG only; ignore ES
)

// ServiceConfig tunes the service's timeouts, breaker and fallback policy.
type ServiceConfig struct {
	// ESTimeout bounds each Elasticsearch call. On expiry we fall back.
	ESTimeout time.Duration
	// FallbackTimeout bounds each fallback call.
	FallbackTimeout time.Duration
	// Breaker config shared by all per-endpoint breakers.
	Breaker CircuitBreakerConfig
	// Strategy selects es_first / pg_first / es_only / pg_only.
	Strategy string
	// EndpointOverrides allows per-endpoint breaker configs.
	EndpointOverrides map[string]CircuitBreakerConfig
}

// DefaultServiceConfig returns production-sane defaults.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		ESTimeout:       800 * time.Millisecond,
		FallbackTimeout: 2 * time.Second,
		Breaker:         DefaultCircuitBreakerConfig(),
		Strategy:        StrategyESFirst,
		EndpointOverrides: map[string]CircuitBreakerConfig{
			"suggest": {
				Threshold:            3,
				Cooldown:             10 * time.Second,
				FailureRateThreshold: 0.4,
			},
			"near": {
				Threshold:            8,
				Cooldown:             20 * time.Second,
				FailureRateThreshold: 0.6,
			},
		},
	}
}

// Service is the public entry point used by the HTTP handlers. It owns the
// ES / Postgres decision, per-endpoint circuit breakers, fallback strategies,
// result enveloping and metrics emission.
type Service struct {
	primary  Repository // Elasticsearch
	fallback Repository // Postgres
	registry *breakerRegistry
	cfg      ServiceConfig
	logger   *log.Logger
	health   *HealthChecker
}

// NewService wires the primary (ES) and fallback (Postgres) repositories.
// Either may be nil: with a nil primary every request uses the fallback; with
// a nil fallback an ES failure surfaces as an error (unless pg_only).
func NewService(primary, fallback Repository, cfg ServiceConfig, logger *log.Logger) *Service {
	if logger == nil {
		logger = log.Default()
	}
	if cfg.Strategy == "" {
		cfg.Strategy = StrategyESFirst
	}
	s := &Service{
		primary:  primary,
		fallback: fallback,
		registry: newBreakerRegistry(cfg.Breaker),
		cfg:      cfg,
		logger:   logger,
	}
	// Apply per-endpoint overrides.
	for ep, override := range cfg.EndpointOverrides {
		cb := newCircuitBreaker(override)
		s.registry.mu.Lock()
		s.registry.items[ep] = cb
		s.registry.mu.Unlock()
	}
	return s
}

// WithHealthChecker attaches a health checker so the service can report
// backend status on the /search/health endpoint.
func (s *Service) WithHealthChecker(hc *HealthChecker) *Service {
	s.health = hc
	return s
}

// breakerFor returns the circuit breaker for the given endpoint.
func (s *Service) breakerFor(endpoint string) *circuitBreaker {
	return s.registry.Get(endpoint)
}

// queryFn is one repository operation, parameterised over the two backends.
type queryFn func(ctx context.Context, repo Repository) (*Page, error)

// run executes fn against the configured backends according to Strategy,
// applying timeouts, per-endpoint circuit breakers and metrics. It builds the
// SearchMeta envelope recording source, timing and fallback flags.
func (s *Service) run(ctx context.Context, endpoint, query string, page, pageSize, offset int, fn queryFn) dto.SearchResult {
	start := time.Now()
	attempt := func(repo Repository, timeout time.Duration) (*Page, error) {
		if repo == nil {
			return nil, context.Canceled // treated as unavailable, without I/O
		}
		bCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		res, err := fn(bCtx, repo)
		return res, err
	}
	attemptES := func() (*Page, error) {
		if s.primary == nil {
			return nil, context.Canceled
		}
		breaker := s.breakerFor(endpoint)
		if !breaker.Allow() {
			ObserveFallback(endpoint, "breaker_open")
			return nil, context.DeadlineExceeded
		}
		started := time.Now()
		res, err := attempt(s.primary, s.cfg.ESTimeout)
		ObserveLatency(sourceES, endpoint, time.Since(started))
		if err == nil {
			breaker.Success()
			ObserveBreakerState(endpoint, "closed")
			return res, nil
		}
		breaker.Failure()
		ObserveBreakerState(endpoint, breaker.State())
		s.logger.Printf("search(%s): elasticsearch failed (breaker=%s): %v", endpoint, breaker.State(), err)
		return nil, err
	}
	attemptPG := func() (*Page, error) {
		started := time.Now()
		res, err := attempt(s.fallback, s.cfg.FallbackTimeout)
		ObserveLatency(sourcePG, endpoint, time.Since(started))
		if err != nil {
			s.logger.Printf("search(%s): postgres failed: %v", endpoint, err)
		}
		return res, err
	}
	empty := func(source string) dto.SearchResult {
		return s.envelope(&Page{Items: []interface{}{}}, source, query, page, pageSize, offset, start, true, true)
	}

	switch s.cfg.Strategy {
	case StrategyPGOnly:
		if res, err := attemptPG(); err == nil {
			return s.envelope(res, sourcePG, query, page, pageSize, offset, start, false, false)
		}
		return empty(sourcePG)
	case StrategyESOnly:
		if res, err := attemptES(); err == nil {
			return s.envelope(res, sourceES, query, page, pageSize, offset, start, false, false)
		}
		return empty(sourceES)
	case StrategyPGFirst:
		if res, err := attemptPG(); err == nil {
			return s.envelope(res, sourcePG, query, page, pageSize, offset, start, false, false)
		}
		if res, err := attemptES(); err == nil {
			ObserveFallback(endpoint, "postgres_error")
			return s.envelope(res, sourceES, query, page, pageSize, offset, start, true, true)
		}
		return empty(sourceES)
	default: // es_first
		if res, err := attemptES(); err == nil {
			return s.envelope(res, sourceES, query, page, pageSize, offset, start, false, false)
		}
		ObserveFallback(endpoint, "es_error")
		if res, err := attemptPG(); err == nil {
			return s.envelope(res, sourcePG, query, page, pageSize, offset, start, true, true)
		}
		return empty(sourcePG)
	}
}

// runSuggest executes a suggest operation with breaker + fallback.
func (s *Service) runSuggest(ctx context.Context, endpoint, domain, prefix string, size int, fn func(ctx context.Context, repo Repository) ([]dto.Suggestion, error)) ([]dto.Suggestion, string) {
	tryES := func() ([]dto.Suggestion, error) {
		if s.primary == nil {
			return nil, context.Canceled
		}
		breaker := s.breakerFor(endpoint)
		if !breaker.Allow() {
			return nil, context.DeadlineExceeded
		}
		esCtx, cancel := context.WithTimeout(ctx, s.cfg.ESTimeout)
		out, err := fn(esCtx, s.primary)
		cancel()
		if err == nil {
			breaker.Success()
			ObserveBreakerState(endpoint, "closed")
			return out, nil
		}
		breaker.Failure()
		ObserveBreakerState(endpoint, breaker.State())
		s.logger.Printf("suggest(%s): elasticsearch failed, falling back: %v", endpoint, err)
		return nil, err
	}
	tryPG := func() ([]dto.Suggestion, error) {
		if s.fallback == nil {
			return nil, context.Canceled
		}
		pgCtx, cancel := context.WithTimeout(ctx, s.cfg.FallbackTimeout)
		out, err := fn(pgCtx, s.fallback)
		cancel()
		if err != nil {
			s.logger.Printf("suggest(%s): postgres failed: %v", endpoint, err)
		}
		return out, err
	}
	finish := func(out []dto.Suggestion, source string) ([]dto.Suggestion, string) {
		ObserveQuery(endpoint, source, len(out) == 0)
		return out, source
	}
	switch s.cfg.Strategy {
	case StrategyPGOnly:
		if out, err := tryPG(); err == nil {
			return finish(out, sourcePG)
		}
	case StrategyESOnly:
		if out, err := tryES(); err == nil {
			return finish(out, sourceES)
		}
	case StrategyPGFirst:
		if out, err := tryPG(); err == nil {
			return finish(out, sourcePG)
		}
		if out, err := tryES(); err == nil {
			ObserveFallback(endpoint, "postgres_error")
			return finish(out, sourceES)
		}
	default:
		if out, err := tryES(); err == nil {
			return finish(out, sourceES)
		}
		ObserveFallback(endpoint, "es_error")
		if out, err := tryPG(); err == nil {
			return finish(out, sourcePG)
		}
	}
	return []dto.Suggestion{}, sourcePG
}

// envelope wraps a Page in the transport DTO with populated metadata.
func (s *Service) envelope(p *Page, source, query string, page, pageSize, offset int, start time.Time, fallback, degraded bool) dto.SearchResult {
	if p == nil {
		p = &Page{Items: []interface{}{}}
	}
	monitoring.SearchRequests.WithLabelValues(source, strconv.FormatBool(degraded)).Inc()

	// Determine zero-result via reflection since p.Items is interface{}.
	zero := p.Total == 0
	value := reflect.ValueOf(p.Items)
	if value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Array) {
		zero = zero && value.Len() == 0
	}
	ObserveQuery("search", source, zero)

	returned := 0
	if value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Array) {
		returned = value.Len()
	}
	hasMore := offset+returned < p.Total
	nextCursor := ""
	if hasMore {
		nextCursor = dto.EncodeCursor(offset + returned)
	}
	return dto.SearchResult{
		Items:       p.Items,
		Suggestions: p.Suggestions,
		DidYouMean:  p.DidYouMean,
		Meta: dto.SearchMeta{
			Source:     source,
			Total:      p.Total,
			Page:       page,
			PageSize:   pageSize,
			TookMS:     time.Since(start).Milliseconds(),
			Query:      query,
			Fallback:   fallback,
			Degraded:   degraded,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}
}

// ---------------------------------------------------------------------------
// Public API — one method per search endpoint.
// ---------------------------------------------------------------------------

// SearchISPs backs GET /search/isp and /search/isp/{county}.
func (s *Service) SearchISPs(ctx context.Context, p dto.SearchParams) dto.SearchResult {
	query := p.Query
	p = s.resolveLocation(ctx, p)
	return s.run(ctx, "isp", query, p.Page, p.PageSize, p.Offset(), func(ctx context.Context, repo Repository) (*Page, error) {
		return repo.SearchISPs(ctx, p)
	})
}

// SearchTechnicians backs GET /search/tech and /search/role.
func (s *Service) SearchTechnicians(ctx context.Context, p dto.SearchParams) dto.SearchResult {
	query := p.Query
	p = s.resolveLocation(ctx, p)
	return s.run(ctx, "tech", query, p.Page, p.PageSize, p.Offset(), func(ctx context.Context, repo Repository) (*Page, error) {
		return repo.SearchTechnicians(ctx, p)
	})
}

func (s *Service) resolveLocation(ctx context.Context, p dto.SearchParams) dto.SearchParams {
	postgres, ok := s.fallback.(*PostgresRepository)
	if !ok || (p.Query == "" && p.Village == "") {
		return p
	}
	resolved, _, err := postgres.expandLearnedPlace(ctx, p)
	if err != nil {
		s.logger.Printf("search: learned location resolution failed: %v", err)
		return p
	}
	return resolved
}

// SearchTechniciansNear backs GET /search/tech/near.
func (s *Service) SearchTechniciansNear(ctx context.Context, p dto.GeoParams) dto.SearchResult {
	return s.run(ctx, "near", p.Query, p.Page, p.PageSize, p.Offset(), func(ctx context.Context, repo Repository) (*Page, error) {
		return repo.SearchTechniciansNear(ctx, p)
	})
}

// SearchLocations backs GET /search/location.
func (s *Service) SearchLocations(ctx context.Context, p dto.SearchParams) dto.SearchResult {
	return s.run(ctx, "location", p.Query, p.Page, p.PageSize, p.Offset(), func(ctx context.Context, repo Repository) (*Page, error) {
		return repo.SearchLocations(ctx, p)
	})
}

// Suggest backs GET /search/suggest?domain=..&q=.. It is best-effort: an ES
// failure trips the breaker and falls back to Postgres prefix matching.
func (s *Service) Suggest(ctx context.Context, domain, prefix string, size int) ([]dto.Suggestion, string) {
	return s.runSuggest(ctx, "suggest", domain, prefix, size, func(ctx context.Context, repo Repository) ([]dto.Suggestion, error) {
		return repo.Suggest(ctx, domain, prefix, size)
	})
}

// SuggestAcross autocompletes across locations, ISPs, and technicians in one
// request. Results are de-duplicated case-insensitively and capped globally.
func (s *Service) SuggestAcross(ctx context.Context, prefix string, size int) ([]dto.Suggestion, string) {
	if size <= 0 {
		size = 8
	}
	return s.runSuggest(ctx, "suggest_all", "all", prefix, size, func(ctx context.Context, repo Repository) ([]dto.Suggestion, error) {
		out := make([]dto.Suggestion, 0, size)
		seen := make(map[string]bool)
		for _, domain := range []string{"location", "isp", "technician"} {
			items, err := repo.Suggest(ctx, domain, prefix, size)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				key := strings.ToLower(strings.TrimSpace(item.Type + ":" + item.Text))
				if key == ":" || seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, item)
			}
		}
		if len(out) > size {
			out = out[:size]
		}
		return out, nil
	})
}

// RecommendISPs backs recommendation endpoints.
func (s *Service) RecommendISPs(ctx context.Context, p dto.RecommendParams) dto.SearchResult {
	return s.run(ctx, "recommend_isp", "", p.Page, p.PageSize, p.Offset(), func(ctx context.Context, repo Repository) (*Page, error) {
		return repo.RecommendISPs(ctx, p)
	})
}

// SimilarISPs backs "more like this" for ISPs.
func (s *Service) SimilarISPs(ctx context.Context, p dto.RecommendParams) dto.SearchResult {
	return s.run(ctx, "similar_isp", "", p.Page, p.PageSize, p.Offset(), func(ctx context.Context, repo Repository) (*Page, error) {
		return repo.SimilarISPs(ctx, p)
	})
}

// RecommendTechnicians backs technician recommendations.
func (s *Service) RecommendTechnicians(ctx context.Context, p dto.RecommendParams) dto.SearchResult {
	return s.run(ctx, "recommend_tech", "", p.Page, p.PageSize, p.Offset(), func(ctx context.Context, repo Repository) (*Page, error) {
		return repo.RecommendTechnicians(ctx, p)
	})
}

// SimilarTechnicians backs "more like this" for technicians.
func (s *Service) SimilarTechnicians(ctx context.Context, p dto.RecommendParams) dto.SearchResult {
	return s.run(ctx, "similar_tech", "", p.Page, p.PageSize, p.Offset(), func(ctx context.Context, repo Repository) (*Page, error) {
		return repo.SimilarTechnicians(ctx, p)
	})
}

// BreakerState exposes the ES breaker state for a specific endpoint.
func (s *Service) BreakerState(endpoint string) string {
	return s.breakerFor(endpoint).State()
}

// BreakerStates exposes all breaker states for health/metrics endpoints.
func (s *Service) BreakerStates() map[string]string {
	return s.registry.States()
}

// Health returns the latest backend health snapshot.
func (s *Service) Health(ctx context.Context) map[string]*BackendStatus {
	out := make(map[string]*BackendStatus)
	if s.health != nil {
		if s.primary != nil {
			esStatus := s.health.CheckES(ctx)
			out["elasticsearch"] = &esStatus
		}
		if s.fallback != nil {
			pgPing := func(c context.Context) error {
				if s.fallback.Healthy(c) {
					return nil
				}
				return context.DeadlineExceeded
			}
			pgStatus := s.health.CheckPostgres(ctx, pgPing)
			out["postgres"] = &pgStatus
		}
	}
	return out
}
