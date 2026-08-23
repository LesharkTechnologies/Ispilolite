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
| Separate migration step | Implemented | `cmd/migrate` runs migrations explicitly; `SKIP_MIGRATIONS=true` prevents normal startup from executing them. |

## Matching Service

| Capability | Status | Evidence / gap |
|---|---|---|
| Matching service package | Implemented | `internal/services/matching/matching_service.go` validates requests, creates requests/quotations, matches through its repository, and accepts quotations. |
| Synchronous request matching | Implemented | Broadcast `jobrequest.Create` invokes the configured matcher after persistence. |
| Candidate ranking weights | Implemented | Migration `0016_matching.sql` stores weights and the PostgreSQL adapter applies them to candidate scores. |
| Persisted matches and notified timestamps | Implemented | `matches` table and `MatchingRepository.FindMatches` upsert ranked rows. |
| Provider notification events | Implemented | Match results call the reusable notification service, which persists in-app events and publishes to SSE subscribers. |
| Assign provider / chat opening | Partial | ISP technician-work requests and customer-request technician assignment are wired; chat opening remains pending. |
| Re-match and stale-match worker | Partial | `StartExpiryWorker` expires stale matches and is started by `cmd/matching`; radius expansion/re-match retry remains pending. |
| Matching HTTP routes | Implemented for match actions | Match list, provider respond, provider decline, ISP technician-work request, and technician assignment routes are wired; chat route remains pending. |

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

1. Add provider assignment/respond/decline matching routes and chat opening.
2. Add radius-expanding re-match retry after stale match expiry.
3. Normalize technician location/coverage queries for precise geospatial ranking.
4. Inject `pkg/config.Config` through every constructor instead of remaining direct environment reads.

## Identity Terminology / Runtime Safety

The database has one identity table: `users`. The role column distinguishes `customer`, `isp`, `technician`, and `admin`. “Client” is legacy API/model terminology and is not a second table or identity type. Foreign keys use `customer_id`, `isp_id`, and `technician_id` against `users.id`, with role predicates in provider queries. No duplicate client/user identity structure was introduced.

Known runtime risks remain: the geospatial technician adapter currently uses a broad active-technician query until technician location/coverage tables are normalized, and matching repository listing/assignment routes are incomplete. These are recorded as pending rather than silently treated as production complete.

## Marketplace Scenarios

| Scenario | API / behavior | Status |
|---|---|---|
| Customer requests an independent technician | `POST /api/v1/technicians/:id/requests` creates a direct `technician_service` request targeting the technician. | Implemented |
| Customer requests an ISP for Wi-Fi/internet/hotspot work | `POST /api/v1/my/jobs` with `target_isp_id`, `request_type=internet`, and service type such as `wifi_installation` or `hotspot_installation`. | Implemented |
| Customer broadcasts a service request | `POST /api/v1/my/jobs` with broadcast mode; matching ranks active ISP/technician candidates and sends in-app notifications. | Implemented |
| ISP hires a technician independently | `POST /api/v1/my/technician-work-requests` creates a direct request with `requester_role=isp`. | Implemented |
| ISP assigns its technician to a customer request | `PUT /api/v1/my/requests/:id/assign-technician` validates `users.role='technician'` and `users.isp_id=<isp>` before assignment. | Implemented |
| Provider acknowledges a match | `POST /api/v1/matches/:id/respond`. | Implemented |
| Provider declines a match | `DELETE /api/v1/matches/:id/decline`. | Implemented |
| Assignment opens customer/provider chat | Chat service/room repository is not present in this checkout. | Pending |

## Company Technicians, Vacancies, and Work Lifecycle

| Capability | Status | API / behavior |
|---|---|---|
| Link technician to ISP | Implemented | `POST /api/v1/my/technicians/:id/link`; creates a stable company technician number and links `users.isp_id`. |
| List ISP technicians | Implemented | `GET /api/v1/my/technicians`; returns linked technician users and company metadata. |
| End company technician link | Implemented | `POST /api/v1/my/technicians/:id/end`; records end time and clears active `users.isp_id`. |
| Technician “works with” profile | Implemented | Public technician profile includes `works_with` with company name, company technician ID, phone snapshot, and start/end dates. |
| Independent technician reviews | Implemented | Existing technician review endpoint supports personal target reviews. |
| Company-scoped technician reviews | Implemented | Review payload accepts `company_id` and `engagement_id` so customer-service work can be attributed to an ISP engagement. |
| ISP vacancies | Implemented | `POST/GET /api/v1/my/vacancies`, `DELETE /api/v1/my/vacancies/:id`. |
| Technician vacancy applications | Implemented | `POST /api/v1/vacancies/:id/apply`; application remains linked to `technician_id`. |
| Vacancy deadlines | Implemented | Deadline dates are stored and hourly expiry marks overdue vacancies as `expired`. |
| Job termination | Implemented | `POST /api/v1/my/jobs/:id/terminate`; customer, ISP, or assigned technician can terminate with reason/date. |
| Job deadlines | Implemented | `service_requests.deadline_at`; hourly scheduler cancels overdue active requests. |
| Work engagement history | Schema implemented | `work_engagements` stores customer/ISP/technician start/end/termination history; creation hooks for every assignment are still pending. |
