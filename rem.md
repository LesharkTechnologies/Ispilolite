# ISPILO Runtime Review

This file tracks the implementation against the modular-monolith and matching/geospatial design supplied for the project.

## Core Entry Point

| Capability | Status | Evidence / gap |
|---|---|---|
| Single API startup entrypoint | Implemented | `cmd/auth/main.go` and `cmd/core/main.go` start the production API; `pkg/config` now defines the typed startup configuration contract. |
| Configuration and environment validation | Partial | Database, Redis, JWT, RabbitMQ, telemetry, SMS, and audit settings are read by their respective packages. A single typed config object is not yet used everywhere. |
| PostgreSQL and Redis startup checks | Implemented in auth entrypoint | `pkg/database` initializes and pings both pools and Redis. |
| Repository/service dependency graph | Partial | Existing handlers construct repositories locally; full constructor injection is not yet complete. |
| Middleware stack | Implemented | Request ID, request logging, rate limiting, JWT auth, recovery, Prometheus, and tracing are present. |
| Graceful shutdown | Implemented in auth entrypoint | HTTP, queue, Redis, database, telemetry, and collectors are shut down. |
| Separate migration step | Partial | `cmd/migrate` exists, but `InitDB` still executes migrations during startup and needs a no-migrate database open mode before this is complete. |

## Matching Service

| Capability | Status | Evidence / gap |
|---|---|---|
| Matching service package | Partial | `internal/services/matching/matching_service.go` validates requests, creates requests/quotations, matches through its repository, and accepts quotations. |
| Synchronous request matching | Partial | `MatchProviders` exists, but request creation does not automatically call it. |
| Candidate ranking weights | Missing | No configurable score weights table or composite ranking implementation is wired. |
| Persisted matches and notified timestamps | Missing | No matching repository/table is connected to the service package. |
| Provider notification events | Partial | Quotation acceptance can notify; request-match fanout is not wired. |
| Assign provider / chat opening | Missing | No matching handler or assignment flow is wired. |
| Re-match and stale-match worker | Missing | No expiry/re-match worker is wired. |
| Matching HTTP routes | Partial | `GET /api/v1/matches/request/:id` is mounted in the core API; provider assignment/respond/decline routes remain pending. |

## Geospatial Service

| Capability | Status | Evidence / gap |
|---|---|---|
| Coordinate validation and radius bounds | Implemented | `internal/services/geospatial/geospatial_service.go`. |
| Nearby ISPs | Implemented at service contract level | Requires a repository implementing `NearbyISPs`. |
| Nearby technicians | Implemented at service contract level | Requires a repository implementing `NearbyTechnicians`. |
| Coverage check | Implemented at service contract level | Requires a repository implementing `CheckCoverage`. |
| Location search | Implemented at service contract level | Requires a repository implementing `SearchLocations`. |
| Repository-backed geospatial HTTP server | Implemented in core API | `GeospatialRepository` and `GeospatialHandler` provide nearby ISP/technician, coverage, and location search routes. Standalone command remains health-only. |

## Verification

- `cmd/core`, `cmd/matching`, and `cmd/geospatial` are valid Go entrypoints.
- Focused service packages are tested where repository dependencies permit.
- Full repository verification remains subject to existing unrelated compile errors documented in the task log, including `models.Flag` and duplicate search index declarations.

Build/test verification completed:

```text
go build ./cmd/core
go build ./cmd/matching
go build ./cmd/geospatial
go test ./internal/services/matching ./internal/services/geospatial ./pkg/monitoring
```

All commands above pass.

Windows build note: when the workspace already contains `matching.exe`, use
explicit `-o` paths for multiple command builds to avoid Go's executable-name
collision. Explicit-output builds for all three commands pass.

## Next Implementation Order

1. Add matching tables/repository and wire `CreateRequest -> MatchProviders -> notifications`.
2. Add configurable ranking weights and stale-match expiry/re-match worker.
3. Add a repository adapter for `internal/services/geospatial` and mount nearby/coverage/location routes.
4. Move remaining startup configuration into a typed config and separate migration command.
