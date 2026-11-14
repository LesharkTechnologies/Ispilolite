# Ispilo Lite — API Keys & Flutter Integration Reference

This document is the single source of truth for:

1. Every **API key / secret / environment variable** that exists in the Ispilo Lite Go codebase.
2. Which of those are **safe to use in the Flutter app** and which must stay **server-side only**.
3. The **request / response structure** for every endpoint the Flutter app calls.
4. Ready-to-paste **Dart** configuration and HTTP examples.

> Canonical API narrative lives in [`ispiloliteapi.md`](./ispiloliteapi.md) and [`api.md`](./api.md). This file is the practical, app-facing extract.

---

## 0. ⚠️ Security rule — read before copying keys

The Flutter app is a **public client**. Anything shipped inside the APK / IPA can be extracted by anyone. Therefore:

| Category | Belongs in Flutter? | Why |
|---|---|---|
| `API_BASE_URL` | ✅ Yes | Public endpoint, not a secret |
| JWT access / refresh tokens | ✅ Yes (runtime only, stored in secure storage) | Per-user tokens issued at login |
| `JWT_SECRET` | ❌ **Never** | Signing key — full auth bypass if leaked |
| Database URLs / passwords | ❌ **Never** | Direct data access |
| `TALKSASA_API_TOKEN` | ❌ **Never** | SMS gateway billing/abuse |
| `ES_API_KEY`, ES credentials | ❌ **Never** | Search cluster access |
| `RABBITMQ_*`, Grafana creds | ❌ **Never** | Infrastructure |

**Only the base URL is configured in Flutter. All other keys in the inventory below are backend / DevOps values.** They are listed here so the team has one authoritative reference, not so they can be embedded in the app.

The Flutter app authenticates users **only** with JWTs returned by `/api/v1/auth/*`. It never talks to Postgres, Redis, RabbitMQ, Elasticsearch, or Talksasa directly.

---

## 1. Flutter app configuration

### 1.1 Base URLs (found in `config/config.yaml`, `config/config.local.yaml`, `ispiloliteapi.md`)

| Environment | Base URL | Source |
|---|---|---|
| Production | `https://lite.ispilo.co.ke/api/v1` | `config/config.yaml` → `server.base_url_prod` |
| Production (Go project base) | `https://lite.ispilo.co.ke` | this project's chosen domain |
| Local (default config) | `http://localhost:8080/api/v1` | `config/config.yaml` → `server.base_url_local` |
| Local (local config) | `http://localhost:8001/api/v1` | `config/config.local.yaml` |

### 1.2 `--dart-define` config

`lib/config/env.dart`:

```dart
class Env {
  static const apiBaseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: 'https://lite.ispilo.co.ke/api/v1',
  );

  // WebSocket base is derived from the API host (see ispiloliteapi.md §4).
  static const wsBaseUrl = String.fromEnvironment(
    'WS_BASE_URL', 
    defaultValue: 'wss://lite.ispilo.co.ke',
  );
}
```

Run with:

```bash
# Android emulator -> host
flutter run --dart-define=API_BASE_URL=http://10.0.2.2:8001/api/v1

# iOS simulator / desktop
flutter run --dart-define=API_BASE_URL=http://localhost:8001/api/v1

# Production build
flutter build apk --dart-define=API_BASE_URL=https://lite.ispilo.co.ke/api/v1
```

### 1.3 Required HTTP headers

```
Content-Type: application/json
Accept: application/json
Authorization: Bearer <access_token>     # every endpoint except /auth/* and /health
Idempotency-Key: <uuid>                 # optional/recommended for mutations; replays 2xx with Idempotency-Replayed: true
```

### 1.4 Endpoint map (which port / service serves what)

| Service | Code entrypoint | Default port (code) | Routes |
|---|---|---|---|
| Auth + Core (modular monolith) | `cmd/auth`, `cmd/core` | `8001` (`PORT`) | `/api/v1/auth/*`, `/api/v1/isps`, `/api/v1/my/*`, `/api/v1/quotations`, `/api/v1/installations`, `/api/v1/geo/*`, admin |
| Search | `cmd/search` | `8081` (`SEARCH_ADDR`) | `/api/v1/search/*` |
| Matching (health/expiry worker) | `cmd/matching` | `8082` (`MATCHING_ADDR`) | `/health`, `/metrics` |
| Geospatial (health) | `cmd/geospatial` | `8083` (`GEOSPATIAL_ADDR`) | `/health`, `/metrics` |
| Notification worker | `cmd/notification` | `9091` (`MONITORING_ADDR`) | `/health`, `/metrics` |

> In practice the Flutter app only needs **one** base URL: the public API (port `8001` behind TLS). Search is a separate service on `8081` in local dev; in production it is normally reverse-proxied onto the same host under `/api/v1/search/*`.

---

## 2. Complete key / environment-variable inventory

Every value below is read via `os.Getenv` / `getenv` in the Go code. "Client-safe" = may exist in the Flutter app.

### 2.1 Runtime & server

| Variable | Purpose | Default | Required | Client-safe |
|---|---|---|---|---|
| `ENVIRONMENT` | `development` / `production` | `development` | prod: yes | ❌ |
| `PORT` | HTTP port for `cmd/auth` & `cmd/core` | `8001` | no | ❌ |
| `CONFIG_PATH` | Path to `config.yaml` | `config/config.yaml` | no | ❌ |
| `GIN_MODE` | Gin release/debug | `release` (search) | no | ❌ |
| `SEARCH_ADDR` | Search service listen addr | `:8081` | no | ❌ |
| `MATCHING_ADDR` | Matching service listen addr | `:8082` | no | ❌ |
| `GEOSPATIAL_ADDR` | Geospatial service listen addr | `:8083` | no | ❌ |
| `MONITORING_ADDR` | Notification worker metrics addr | `:9091` | no | ❌ |
| `CORS_ALLOWED_ORIGINS` | Comma-separated origins allowed for CORS | empty (disabled) | no | ❌ |

### 2.2 Authentication / JWT

| Variable | Purpose | Default | Required | Client-safe |
|---|---|---|---|---|
| `JWT_SECRET` | HMAC-SHA256 signing key | `ispilolite-dev-secret` | **yes in production** | ❌ |
| `JWT_ISSUER` | Token `iss` claim | `ispilolite` | no | ❌ |
| `JWT_ACCESS_TTL` | Access token lifetime | `15m` | no | ❌ |
| `JWT_REFRESH_TTL` | Refresh token lifetime | `168h` (7d) | no | ❌ |

> `config/config.yaml` also has `jwt.secret: "your-super-secret-key"`, `jwt.issuer: "ispilolite"`, `jwt.expiry_hours: 72`. The runtime actually uses `JWT_SECRET` / `JWT_ACCESS_TTL` / `JWT_REFRESH_TTL` from the environment (`pkg/config/config.go`, `internal/services/auth/auth_service.go`). Rotate the YAML defaults before going live.

### 2.3 Database (Postgres — read/write split)

| Variable | Purpose | Default | Required | Client-safe |
|---|---|---|---|---|
| `DATABASE_URL` | Pooled writer DSN (Neon/prod) | — | yes (prod) | ❌ |
| `DB_URL` | Alias for `DATABASE_URL` | — | optional | ❌ |
| `DATABASE_READ_URL` | Read-replica DSN | falls back to writer | no | ❌ |
| `DB_READ_URL` | Alias for `DATABASE_READ_URL` | — | optional | ❌ |
| `DB_WRITER_MAX_CONNS` | Writer pool cap | `25` | no | ❌ |
| `DB_READER_MAX_CONNS` | Reader pool cap | `50` | no | ❌ |
| `MIGRATIONS_PATH` | SQL migrations folder | `migrations` | no | ❌ |
| `SKIP_MIGRATIONS` | `true` to skip auto-migrate | — | no | ❌ |

YAML placeholders in `config/config.yaml` (expanded from env):

| Placeholder | Meaning |
|---|---|
| `DB_WRITER_HOST` / `DB_WRITER_USER` / `DB_WRITER_PASSWORD` | Writer host / user / password |
| `DB_READER_HOST` / `DB_READER_USER` / `DB_READER_PASSWORD` | Reader host / user / password |
| `DB_NAME` | Database name |
| `REDIS_HOST` | Redis host |

Local placeholders in `config/config.local.yaml`:

| Placeholder | Meaning |
|---|---|
| `LOCAL_DB_HOST` / `LOCAL_DB_PORT` / `LOCAL_DB_USER` / `LOCAL_DB_PASSWORD` / `LOCAL_DB_NAME` | Local Postgres |
| `LOCAL_REDIS_HOST` / `LOCAL_REDIS_PORT` | Local Redis |

### 2.4 Redis

| Variable | Purpose | Default | Required | Client-safe |
|---|---|---|---|---|
| `REDIS_URL` | Full Redis URL, e.g. `redis://:pass@host:6379/0` | built from YAML | yes | ❌ |

### 2.5 Elasticsearch (search service)

| Variable | Purpose | Default | Required | Client-safe |
|---|---|---|---|---|
| `ES_ADDRESSES` | Comma-separated node URLs | `http://localhost:9200` | no | ❌ |
| `ES_USERNAME` | Basic-auth user | — | no | ❌ |
| `ES_PASSWORD` | Basic-auth password | — | no | ❌ |
| `ES_API_KEY` | ES API key (preferred) | — | no | ❌ |
| `ES_ENSURE_INDICES` | Create indices on boot | `true` | no | ❌ |
| `ES_INDEX_VERSION` | Versioned index alias migration | — | no | ❌ |

### 2.6 SMS — Talksasa

| Variable | Purpose | Default | Required | Client-safe |
|---|---|---|---|---|
| `TALKSASA_API_TOKEN` | SMS gateway bearer token | — | yes for SMS | ❌ |
| `TALKSASA_SENDER_ID` | Sender ID (e.g. `TALKSASA`) | — | yes for SMS | ❌ |
| `TALKSASA_BASE_URL` | Gateway base | `https://bulksms.talksasa.com/api/v3` | no | ❌ |

### 2.7 Message queue — RabbitMQ

| Variable | Purpose | Default | Required | Client-safe |
|---|---|---|---|---|
| `RABBITMQ_URL` | AMQP connection URL | — | worker: yes | ❌ |
| `RABBITMQ_CONSUMER_NAME` | Consumer tag | `ispilolite-api` / `notification-worker` | no | ❌ |
| `RABBITMQ_PREFETCH` | Prefetch count | `20` | no | ❌ |
| `RABBITMQ_REQUIRED` | `true` to fail startup if down | `false` | no | ❌ |
| `RABBITMQ_USER` | Docker-compose default user | `ispilo` | no | ❌ |
| `RABBITMQ_PASSWORD` | Docker-compose default password | `ispilo-local` | no | ❌ |

### 2.8 Notifications, storage, audit, telemetry

| Variable | Purpose | Default | Required | Client-safe |
|---|---|---|---|---|
| `ADMIN_ALERT_PHONES` | Comma-separated admin SMS numbers | — | no | ❌ |
| `NOTIFICATION_WEBHOOK_URL` | Push webhook for notification worker | — | worker: yes | ❌ |
| `DOCUMENT_STORAGE_ROOT` | Quotation PDF storage root | — | no | ❌ |
| `AUDIT_ARCHIVE_ROOT` | Audit export archive root | `storage/audit` | no | ❌ |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP tracing endpoint | — | no | ❌ |

### 2.9 Observability stack (docker-compose)

| Variable | Purpose | Default | Client-safe |
|---|---|---|---|
| `GRAFANA_USER` | Grafana admin user | `admin` | ❌ |
| `GRAFANA_PASSWORD` | Grafana admin password | `admin` | ❌ |

### 2.10 Sample backend `.env` (server only — never ship to Flutter)

```dotenv
# --- Runtime ---
ENVIRONMENT=production
PORT=8001
CONFIG_PATH=config/config.yaml

# --- Auth ---
JWT_SECRET=change-me-to-a-64-char-random-string
JWT_ISSUER=ispilolite
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=168h

# --- Postgres (writer + reader) ---
DATABASE_URL=postgres://user:pass@writer-host:5432/ispilolite?sslmode=require
DATABASE_READ_URL=postgres://user:pass@reader-host:5432/ispilolite?sslmode=require
DB_WRITER_MAX_CONNS=25
DB_READER_MAX_CONNS=50

# --- Redis ---
REDIS_URL=redis://:pass@redis-host:6379/0

# --- Search ---
ES_ADDRESSES=https://es-host:9200
ES_API_KEY=your-elastic-api-key
ES_ENSURE_INDICES=true

# --- SMS ---
TALKSASA_BASE_URL=https://bulksms.talksasa.com/api/v3
TALKSASA_API_TOKEN=replace-with-a-rotated-token
TALKSASA_SENDER_ID=TALKSASA

# --- Queue ---
RABBITMQ_URL=amqp://ispilo:pass@rabbitmq-host:5672/
RABBITMQ_REQUIRED=true

# --- Misc ---
ADMIN_ALERT_PHONES=+254712345678
DOCUMENT_STORAGE_ROOT=storage/documents
AUDIT_ARCHIVE_ROOT=storage/audit
OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318
```

> `.env` is git-ignored (`.gitignore`) and auto-loaded by `pkg/database.loadDotEnv`.

---

## 3. Standard response envelope

Every JSON endpoint (except `/health`, `/readyz`, `/metrics`) returns:

### Success

```json
{
  "success": true,
  "data": { },
  "message": "Human readable message (optional)"
}
```

### Error

```json
{
  "success": false,
  "data": null,
  "message": "Error description",
  "errors": { "field": "validation error" }
}
```

Go struct (`api/dto/response.go`):

```go
type APIResponse struct {
    Success bool        `json:"success"`
    Data    interface{} `json:"data"`
    Message string      `json:"message,omitempty"`
    Errors  interface{} `json:"errors,omitempty"`
}
```

### Dart model

```dart
class ApiResponse<T> {
  final bool success;
  final T? data;
  final String? message;
  final dynamic errors;

  ApiResponse({required this.success, this.data, this.message, this.errors});

  factory ApiResponse.fromJson(
    Map<String, dynamic> json,
    T Function(dynamic) fromData,
  ) =>
      ApiResponse(
        success: json['success'] as bool? ?? false,
        data: json['data'] == null ? null : fromData(json['data']),
        message: json['message'] as String?,
        errors: json['errors'],
      );
}
```

---

## 4. Authentication — request / response

> **Validation & Rate Limits**:
> - Phone numbers must strictly use **E.164** international format (`+254712345678`).
> - OTP requests are rate-limited to **3 attempts per 15-minute window** per phone number.
> - Auth endpoints (`/api/v1/auth/*`) are rate-limited to **5 requests per minute** per IP.
> - Refresh tokens are single-use (**token rotation**); reusing a spent refresh token revokes all user sessions.

Customers sign in with **phone + OTP**. Staff (ISP / technician / admin) sign in with **username + password**. Public registration is **customer-only** — the server forces `role = "customer"`.

### 4.1 Register (customer only)

`POST /api/v1/auth/register` — **no auth**

Request:
```json
{
  "phone": "+254712345678",
  "name": "John Doe",
  "email": "john@example.com"
}
```

Response `201`:
```json
{
  "success": true,
  "message": "verification code sent",
  "data": {
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "expires_in": 300
  }
}
```

### 4.2 Login / request OTP (customer)

`POST /api/v1/auth/login` — **no auth**

Request:
```json
{ "phone": "+254712345678" }
```

Response `200`:
```json
{
  "success": true,
  "message": "OTP sent to your phone",
  "data": {
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "expires_in": 300
  }
}
```

### 4.3 Login (staff: ISP / technician / admin)

`POST /api/v1/auth/login` — **no auth**

Request:
```json
{ "username": "safaricom_admin", "password": "SecurePass123" }
```

Response `200`: token payload (see 4.4).

### 4.4 Verify OTP → tokens

`POST /api/v1/auth/verify-otp` — **no auth**

Request:
```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "otp": "123456"
}
```

Response `200`:
```json
{
  "success": true,
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIs...",
    "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
    "token_type": "Bearer",
    "expires_in": 900,
    "user": {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "John Doe",
      "phone": "+254712345678",
      "role": "customer",
      "is_verified": true
    }
  }
}
```

### 4.5 Refresh access token

`POST /api/v1/auth/refresh` — **no auth**

Request:
```json
{ "refresh_token": "eyJhbGciOiJIUzI1NiIs..." }
```

Response `200`:
```json
{
  "success": true,
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIs...",
    "token_type": "Bearer",
    "expires_in": 900
  }
}
```

### 4.6 Logout

`POST /api/v1/auth/logout` — `Authorization: Bearer <access_token>`

Response `200`:
```json
{ "success": true, "message": "logged out successfully" }
```

### 4.7 JWT claims

```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "role": "customer",
  "token_use": "access",
  "iss": "ispilolite",
  "sub": "550e8400-e29b-41d4-a716-446655440000",
  "exp": 1705334400
}
```

Roles enforced by the API: `customer`, `isp`, `technician`, `admin`.
Middleware rejects refresh tokens on protected routes (`token_use` must be `access`) and revoked tokens (`401`).

---

## 5. Endpoints — request / response by group

All paths below are relative to the base URL (e.g. `https://lite.ispilo.co.ke/api/v1`). "Auth" column shows the required role; `—` = public.

### 5.1 Public discovery

| Method | Path | Auth |
|---|---|---|
| `GET` | `/isps` | — |
| `GET` | `/isps/:id` | — |
| `GET` | `/isps/:id/packages` | — |
| `GET` | `/isps/:id/reviews` | — |
| `GET` | `/isp-packages` | — |
| `GET` | `/package-units` | — |

`GET /isps?county=Nairobi&q=fibre&page=1&page_size=20`

```json
{
  "success": true,
  "data": {
    "items": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "name": "Safaricom Fibre",
        "description": "Home fibre ISP",
        "avatar_url": "https://cdn.ispilolite.com/isps/safaricom.jpg",
        "county": "Nairobi",
        "sub_county": "Westlands",
        "rating": 4.5,
        "review_count": 120,
        "is_active": true
      }
    ],
    "total": 12,
    "page": 1,
    "page_size": 20
  }
}
```

### 5.2 Geospatial

| Method | Path | Auth | Query / body |
|---|---|---|---|
| `GET` | `/geo/nearby/isps` | — | `lat`, `lng`, `radius` (m), `limit` |
| `GET` | `/geo/nearby/technicians` | — | `lat`, `lng`, `radius`, `skills` |
| `GET` | `/geo/coverage` | — | `location_id` or `lat`+`lng` |
| `GET` | `/geo/search` | — | `q`, `county`, `village`, `limit` |
| `GET` | `/geo/locations/search` | — | `q`, `type`, `limit` |
| `GET` | `/geo/locations/by-county` | — | `county` |
| `GET` | `/geo/locations/:id` | — | — |
| `POST` | `/geo/locations` | any | body below |
| `POST` | `/geo/locations/:id/aliases` | any | `{ "alias": "Kiserian Town" }` |

`GET /geo/nearby/isps?lat=-1.2921&lng=36.8219&radius=10000`

```json
{
  "success": true,
  "data": {
    "results": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "name": "Safaricom Fibre",
        "rating": 4.5,
        "distance": 2500,
        "coverage": true,
        "technicians_available": 3,
        "avg_response_time": 15,
        "image": "https://cdn.ispilolite.com/isps/safaricom.jpg"
      }
    ],
    "total": 12,
    "query": { "lat": -1.2921, "lng": 36.8219, "radius": 10000 }
  }
}
```

`POST /geo/locations`

```json
{
  "name": "Kiserian",
  "type": "town",
  "county": "Kajiado",
  "parent_id": "550e8400-e29b-41d4-a716-446655440000",
  "lat": -1.4182,
  "lng": 36.6845
}
```

```json
{
  "success": true,
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "pending",
    "submissions_needed": 3,
    "current_submissions": 1
  }
}
```

### 5.3 Profiles

| Method | Path | Auth |
|---|---|---|
| `GET` | `/my/profile` | customer / isp / technician |
| `PUT` | `/my/profile` | customer / isp |
| `DELETE` | `/my/profile` | customer |
| `GET` | `/my/business-profile` | any |
| `PUT` | `/my/business-profile` | any |
| `GET` | `/technicians/:id/profile` | — |
| `GET` | `/technicians/:id/reviews` | — |
| `POST` | `/technicians/:id/reviews` | customer |
| `POST` | `/technicians/:id/requests` | customer |

`GET /my/profile` → `200`

```json
{
  "success": true,
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "John Doe",
    "phone": "+254712345678",
    "email": "john@example.com",
    "role": "customer",
    "is_verified": true,
    "rating": 4.5,
    "total_ratings": 12,
    "joined": "2024-01-15T10:30:00Z",
    "location": { "lat": -1.2921, "lng": 36.8219 },
    "statistics": {
      "completed_installations": 5,
      "pending_requests": 1,
      "favorite_isps": 3,
      "favorite_technicians": 2
    }
  }
}
```

`PUT /my/profile`

```json
{
  "name": "John Smith",
  "email": "john.smith@example.com",
  "location": { "lat": -1.2921, "lng": 36.8219 }
}
```

```json
{
  "success": true,
  "message": "Profile updated successfully",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "John Smith",
    "email": "john.smith@example.com"
  }
}
```

### 5.4 Installation requests & jobs

| Method | Path | Auth |
|---|---|---|
| `POST` | `/installations` | customer |
| `GET` | `/my/installations` | customer / isp |
| `PUT` | `/installations/*path` | isp |
| `GET` | `/my/jobs` | customer |
| `POST` | `/my/jobs` | customer |
| `GET` | `/my/jobs/:id` | customer |
| `PUT` | `/my/jobs/:id` | customer (assign) |
| `PATCH` | `/my/jobs/:id` | customer (availability) |
| `DELETE` | `/my/jobs/:id` | customer |
| `POST` | `/my/jobs/:id/terminate` | customer / isp / technician |
| `GET` | `/available-jobs` | technician / isp |
| `GET` | `/my/technician-jobs` | technician |
| `POST` | `/jobs/:id/apply` | any |
| `PUT` | `/jobs/:id` | any |

`POST /installations` (or `POST /my/jobs`)

```json
{
  "request_type": "installation",
  "mode": "open",
  "target_isp_id": "550e8400-e29b-41d4-a716-446655440000",
  "target_technician_id": "",
  "location_id": "550e8400-e29b-41d4-a716-446655440001",
  "county": "Nairobi",
  "town": "Westlands",
  "village": "Parklands",
  "service_type": "fiber_installation",
  "speed_mbps": 20,
  "description": "Need fiber connection installed for office",
  "budget": 3000,
  "preferred_date": "2024-02-01T09:00:00Z"
}
```

```json
{
  "success": true,
  "data": {
    "request_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "pending",
    "matched_providers": 5,
    "estimated_response_time": 300,
    "created_at": "2024-01-15T10:30:00Z"
  }
}
```

`GET /my/jobs?status=pending&limit=20&offset=0`

```json
{
  "success": true,
  "data": {
    "jobs": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "service_type": "fiber_installation",
        "status": "quoting",
        "location": "Nairobi CBD",
        "budget": 3000,
        "quotation_count": 3,
        "created_at": "2024-01-15T10:30:00Z"
      }
    ],
    "pagination": { "total": 5, "limit": 20, "offset": 0 }
  }
}
```

### 5.5 Matching & responses

| Method | Path | Auth |
|---|---|---|
| `GET` | `/matches/request/:id` | customer |
| `POST` | `/matches/:id/respond` | isp / technician |
| `DELETE` | `/matches/:id/decline` | isp / technician |

> Note: the repository now implements a real, paginated
> `MatchingRepository.ListRequests`, but there is **no registered route** that
> exposes a global request list (the `matching/*` paths in `ispiloliteapi.md`
> §3.4 are not wired). For the caller's own requests use `GET /my/jobs`
> (customer) or `GET /my/installations` (isp).

`GET /matches/request/:id` → `200`

```json
{
  "success": true,
  "data": {
    "matched_providers": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "type": "isp",
        "name": "Safaricom Fibre",
        "rating": 4.5,
        "distance": 2500,
        "price_estimate": 2500,
        "response_time": 10,
        "availability_score": 0.95,
        "match_score": 0.92
      }
    ],
    "matching_summary": {
      "total_matched": 5,
      "average_match_score": 0.85,
      "best_match": { "id": "550e8400-e29b-41d4-a716-446655440000", "score": 0.92 }
    }
  }
}
```

`POST /matches/:id/respond`

```json
{
  "amount": 2500,
  "description": "Full fiber installation including router",
  "available": true
}
```

### 5.6 Quotations

| Method | Path | Auth |
|---|---|---|
| `GET` | `/public/quotations/:code` | — |
| `POST` | `/quotations` | any |
| `GET` | `/quotations` | any |
| `GET` | `/quotations/:id` | any |
| `POST` | `/quotations/:id/respond` | customer |
| `GET` | `/documents/:id/download` | any |
| `GET` | `/quotation-units` | any |
| `POST` | `/quotation-units` | any |
| `GET` | `/tax-rates` | any |
| `POST` | `/tax-rates` | any |

`POST /quotations`

```json
{
  "customer_id": "550e8400-e29b-41d4-a716-446655440000",
  "request_id": "550e8400-e29b-41d4-a716-446655440001",
  "company_address": "Nairobi, Kenya",
  "currency": "KES",
  "items": [
    {
      "item": "Fiber installation",
      "description": "Cabling + router",
      "unit_id": "unit-1",
      "quantity": 1,
      "unit_price": 2500,
      "discount_type": "NONE",
      "discount_value": 0
    }
  ],
  "transport_amount": 200,
  "tax_rate_id": "tax-1",
  "tax_mode": "EXCLUSIVE",
  "payment_method": "MPESA",
  "payment_details": { "paybill": "123456" },
  "terms": ["Installation within 24 hours", "1-year warranty"],
  "notes": "Please confirm availability",
  "expires_at": "2024-02-01T23:59:59Z"
}
```

```json
{
  "success": true,
  "data": {
    "quotation_id": "550e8400-e29b-41d4-a716-446655440000",
    "public_code": "abc123",
    "pdf_url": "https://cdn.ispilolite.com/quotes/quote-12345.pdf",
    "shareable_link": "https://ispilolite.com/q/abc123",
    "status": "FINALIZED",
    "created_at": "2024-01-15T10:40:00Z"
  }
}
```

`POST /quotations/:id/respond` (customer accepts / rejects)

```json
{ "status": "ACCEPTED", "payment_method": "MPESA", "notes": "Proceed" }
```

```json
{
  "success": true,
  "message": "Quotation accepted",
  "data": {
    "job_id": "550e8400-e29b-41d4-a716-446655440001",
    "status": "ACCEPTED",
    "assigned_technician": {
      "id": "550e8400-e29b-41d4-a716-446655440002",
      "name": "Peter Mwangi",
      "phone": "+254723456789"
    },
    "next_steps": [
      "Wait for technician confirmation",
      "Prepare the installation site",
      "Ensure power is available"
    ]
  }
}
```

### 5.7 ISP operations

| Method | Path | Auth |
|---|---|---|
| `POST` | `/isps/:id/reviews` | customer |
| `POST` | `/my/technicians` | isp |
| `POST` | `/my/technicians/:id/link` | isp |
| `GET` | `/my/technicians` | isp |
| `POST` | `/my/technicians/:id/end` | isp |
| `DELETE` | `/technicians/:id` | isp |
| `POST` | `/my/packages` | isp |
| `PUT` | `/packages/:id` | isp |
| `PUT` | `/packages/:id/prices` | isp |
| `POST` | `/packages/:id/archive` | isp |
| `DELETE` | `/packages/:id` | isp |
| `GET` | `/my/coverage` | isp |
| `POST` | `/my/coverage` | isp |
| `GET` | `/my/coverage/recommendations` | isp |
| `POST` | `/my/vacancies` | isp |
| `GET` | `/my/vacancies` | isp |
| `DELETE` | `/my/vacancies/:id` | isp |
| `POST` | `/vacancies/:id/apply` | technician |
| `POST` | `/my/technician-work-requests` | isp |
| `PUT` | `/my/requests/:id/assign-technician` | isp |
| `POST` | `/package-reservations` | customer |
| `POST` | `/package-reservations/:id/subscribe` | customer |
| `GET` | `/my/subscriptions` | isp / customer |
| `PUT` | `/subscriptions/:id` | isp / customer |

`POST /my/packages`

```json
{
  "name": "Home Fibre 20Mbps",
  "category": "fiber",
  "speed_value": 20,
  "speed_unit_id": "unit-mbps",
  "base_price": 2500,
  "billing_cycle": "monthly",
  "capacity_type": "unlimited",
  "description": "Unlimited home fibre",
  "is_active": true
}
```

`POST /isps/:id/reviews`

```json
{
  "rating": 5,
  "comment": "Excellent service! Fast and professional.",
  "categories": {
    "professionalism": 5,
    "quality": 5,
    "timeliness": 4,
    "communication": 5
  }
}
```

### 5.8 Notifications

| Method | Path | Auth |
|---|---|---|
| `GET` | `/notifications` | any |
| `PUT` | `/notifications/:id/read` | any |
| `GET` | `/notifications/stream` | any (SSE) |
| `GET` | `/my/notifications` | isp |
| `PUT` | `/my/notifications/:id` | isp |
| `GET` | `/my/notifications/stream` | any (SSE) |

`GET /notifications` → `200`

```json
{
  "success": true,
  "data": {
    "notifications": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "title": "New quotation",
        "body": "Safaricom Fibre sent you a quotation",
        "read": false,
        "created_at": "2024-01-15T10:30:00Z"
      }
    ],
    "unread": 1
  }
}
```

### 5.9 Technician portfolio

| Method | Path | Auth |
|---|---|---|
| `PUT` | `/my/portfolio/profile` | technician |
| `GET` | `/my/portfolio/posts` | technician |
| `POST` | `/my/portfolio/posts` | technician |
| `PUT` | `/my/portfolio/posts/:id` | technician |

`POST /my/portfolio/posts`

```json
{
  "title": "Fibre install in Kilimani",
  "description": "Installed 50m drop cable and configured router",
  "service_type": "fiber_installation",
  "media_urls": ["https://cdn.ispilolite.com/portfolio/1.jpg"],
  "status": "published"
}
```

### 5.10 Admin

| Method | Path | Auth |
|---|---|---|
| `GET` | `/admin/deletion-requests` | admin |
| `POST` | `/admin/approve-deletion` | admin |
| `GET` | `/admin/tax-rates` | admin |
| `POST` | `/admin/tax-rates` | admin |
| `POST` | `/admin/system-units` | admin |
| `PUT` | `/admin/business-profiles/:id` | admin |
| `PUT` | `/admin/packages/:id/moderate` | admin |
| `GET` | `/admin/audit` | admin |
| `POST` | `/admin/audit/export` | admin |
| `DELETE` | `/admin/audit` | admin |
| `GET` | `/admin/reviews` | admin |
| `PUT` | `/admin/reviews/:id` | admin |
| `POST` | `/admin/sms` | admin |
| `POST` | `/admin/critical-alerts` | admin |
| `POST` | `/reviews/:id/report` | any |

`POST /admin/sms`

```json
{ "role": "customer", "message": "Scheduled maintenance tonight." }
```

Use `{ "all": true, "message": "..." }` for all active users or `{ "user_ids": ["..."], "message": "..." }` for selected users.

### 5.11 Search service (`cmd/search`, port `8081`)

| Method | Path | Query |
|---|---|---|
| `GET` | `/search/isp` | `q`, `county`, `village`, `sort`, `min_rating`, `active`, `page`, `page_size`, `cursor`, `fuzzy` |
| `GET` | `/search/isp/:county` | same |
| `GET` | `/search/isp/:county/:village` | same |
| `GET` | `/search/tech` | `q`, `skills`, `role`, `available`, `min_rating`, paging |
| `GET` | `/search/tech/near` | `lat`, `lon`, `radius_km` (or `radius` in metres) + filters |
| `GET` | `/search/role` | `role` (required) |
| `GET` | `/search/location` | `q`, `type`, paging |
| `GET` | `/search/suggest` | `domain` (default `location`), `q`, `limit` (≤ 25) |
| `GET` | `/search/health` | — |

Every list endpoint returns:

```json
{
  "success": true,
  "data": {
    "items": [],
    "suggestions": [
      { "text": "Machakos", "type": "county", "score": 1.2 }
    ],
    "did_you_mean": "machakos",
    "meta": {
      "source": "elasticsearch",
      "total": 42,
      "page": 1,
      "page_size": 20,
      "took_ms": 12,
      "query": "mac",
      "fallback": false,
      "degraded": false,
      "next_cursor": "MjA=",
      "has_more": true
    }
  }
}
```

> `meta.source` is `elasticsearch` or `postgres_fallback`. `meta.fallback`/`meta.degraded` are **soft signals** — the HTTP status is still `200`.

### 5.12 Health / readiness

| Method | Path | Response |
|---|---|---|
| `GET` | `/health` | `200` `ok` |
| `GET` | `/readyz` | `200` `ready` / `503` `dependencies unavailable` |
| `GET` | `/metrics` | Prometheus text |
| `GET` | `/healthz` | `{ "status": "ok" }` (search service) |

### 5.13 WebSocket — ⚠️ NOT IMPLEMENTED IN THIS REPOSITORY

There is **no WebSocket/chat handler in the Go code**. A repository-wide search finds no `gorilla/websocket`, no `/ws/` route, and no chat repository (see `rem.md`: "Chat service/room repository is not present in this checkout").

**Do not code the Flutter app against these endpoints yet** — they will `404`. The shapes below are the intended design from `ispiloliteapi.md` §4, kept here for planning only.

```
ws://lite.ispilo.co.ke/ws/chat?token=<jwt_token>   # not implemented
ws://lite.ispilo.co.ke/ws/jobs?token=<jwt_token>   # not implemented
```

Use the implemented alternatives today:

- Real-time-ish updates: Server-Sent Events at `GET /api/v1/notifications/stream`.
- Job/match actions: `POST /api/v1/matches/:id/respond`, `DELETE /api/v1/matches/:id/decline`.

Chat message (planned, not implemented):
```json
{
  "type": "message",
  "data": {
    "to": "550e8400-e29b-41d4-a716-446655440000",
    "message": "Hello, I have a question about the installation",
    "attachments": [{ "type": "image", "url": "https://cdn.ispilo.co.ke/temp/photo.jpg" }]
  }
}
```

Job status update (planned, not implemented):
```json
{
  "type": "job_update",
  "data": {
    "job_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "in_progress",
    "current_location": { "lat": -1.2921, "lng": 36.8219 },
    "eta_minutes": 15,
    "technician": { "name": "Peter Mwangi", "phone": "+254723456789" }
  }
}
```

### 5.14 Payment verification & watermark entitlement — ⚠️ NOT IMPLEMENTED

`quotation.md` §32 requires server-side payment verification before the ISPilo watermark is removed. The backend does **not** implement a payment gateway adapter or watermark entitlement check, and there is no `payments` verification route.

- Do **not** trust a client-supplied `paymentSuccessful` flag.
- Until the backend endpoint exists, watermark removal must stay disabled in the Flutter app.
- Planned surface (not present in code): `POST /api/v1/quotations/:id/payments` + `GET /api/v1/quotations/:id/entitlements`.

---

## 6. Error codes

| Code | Description | HTTP |
|---|---|---|
| `AUTH_001` | Invalid credentials | 401 |
| `AUTH_002` | Token expired | 401 |
| `AUTH_003` | Invalid OTP | 400 |
| `AUTH_004` | Phone not registered | 404 |
| `AUTH_005` | Rate limit exceeded | 429 |
| `GEO_001` | Invalid coordinates | 400 |
| `GEO_002` | Location not found | 404 |
| `GEO_003` | Coverage not available | 404 |
| `REQ_001` | Request not found | 404 |
| `REQ_002` | Invalid status transition | 400 |
| `REQ_003` | Already matched | 400 |
| `REQ_004` | Quotation expired | 400 |
| `REQ_005` | Job already completed | 400 |
| `VAL_001` | Validation failed | 400 |
| `VAL_002` | Missing required field | 400 |
| `PERM_001` | Insufficient permissions | 403 |
| `PERM_002` | Resource not found | 404 |
| `SYS_001` | Internal server error | 500 |
| `SYS_002` | Service unavailable | 503 |

Actual middleware errors: missing/invalid `Authorization` header → `401 Unauthorized`; wrong role → `403 Forbidden`; refresh token used on protected route → `401 access token required`; revoked token → `401 token revoked`.

### Rate limiting (`internal/middleware/ratelimit.go`)

Default limiter: **120 requests / minute per IP + route bucket**. `429` responses include `Retry-After: <seconds>`.

Documented targets (`ispiloliteapi.md`):

| Endpoint category | Limit | Window |
|---|---|---|
| Auth (login, register) | 5 | minute |
| Auth (verify OTP) | 3 | minute |
| Search | 100 | minute |
| Standard API | 300 | minute |
| Admin | 50 | minute |

---

## 7. Flutter client sample

`lib/services/api_client.dart`:

```dart
import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/env.dart';

class ApiClient {
  ApiClient({required this.tokenStore});
  final TokenStore tokenStore;
  final http.Client _http = http.Client();

  Map<String, String> get _headers => {
        'Content-Type': 'application/json',
        'Accept': 'application/json',
        if (tokenStore.accessToken != null)
          'Authorization': 'Bearer ${tokenStore.accessToken}',
      };

  Future<Map<String, dynamic>> get(String path, [Map<String, String>? query]) async {
    final uri = Uri.parse('${Env.apiBaseUrl}$path')
        .replace(queryParameters: query);
    final res = await _http.get(uri, headers: _headers);
    return _decode(res);
  }

  Future<Map<String, dynamic>> post(String path, Map<String, dynamic> body) async {
    final res = await _http.post(
      Uri.parse('${Env.apiBaseUrl}$path'),
      headers: _headers,
      body: jsonEncode(body),
    );
    return _decode(res);
  }

  Map<String, dynamic> _decode(http.Response res) {
    final json = jsonDecode(res.body) as Map<String, dynamic>;
    if (res.statusCode >= 400 || json['success'] != true) {
      throw ApiException(
        statusCode: res.statusCode,
        message: json['message'] as String? ?? 'Request failed',
        errors: json['errors'],
      );
    }
    return json;
  }
}
```

Login (customer OTP):

```dart
final login = await api.post('/auth/login', {'phone': phone});
final userId = login['data']['user_id'] as String;

// After the user enters the SMS code:
final tokens = await api.post('/auth/verify-otp', {
  'user_id': userId,
  'otp': code,
});
await tokenStore.save(
  accessToken: tokens['data']['access_token'],
  refreshToken: tokens['data']['refresh_token'],
);
```

Auto-refresh on `401`:

```dart
Future<Map<String, dynamic>> _withRefresh(
  Future<Map<String, dynamic>> Function() call,
) async {
  try {
    return await call();
  } on ApiException catch (e) {
    if (e.statusCode != 401) rethrow;
    final refreshed = await api.post('/auth/refresh', {
      'refresh_token': tokenStore.refreshToken,
    });
    await tokenStore.saveAccessOnly(refreshed['data']['access_token']);
    return call();
  }
}
```

Store tokens in `flutter_secure_storage`, never in `SharedPreferences`.

---

## 8. Quick checklist

- [ ] `JWT_SECRET` is a long random value, set in the server environment, **not** in Flutter.
- [ ] YAML defaults (`your-super-secret-key`, `ispilolite-dev-secret`) are replaced in production.
- [ ] Flutter only knows `API_BASE_URL` (and optionally `WS_BASE_URL`).
- [ ] `.env` is never committed (already in `.gitignore`).
- [ ] Talksasa token, ES key, DB/Redis/RabbitMQ credentials are server-only.
- [ ] App sends `Authorization: Bearer <access_token>` on every protected call.
- [ ] App handles `401` with refresh, `403` with role error, `429` with `Retry-After`.
