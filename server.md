# Ispilo Lite — Server & Hosting Configuration Guide

How to host the Ispilo Lite Go backend and configure every port, environment variable, reverse proxy, and deployment target.

> Companion docs: [`API_KEYS.md`](./API_KEYS.md) (keys + request/response), [`ispiloliteapi.md`](./ispiloliteapi.md) (full API narrative), [`ispilolitearchitecture.md`](./ispilolitearchitecture.md) (high-scale design).

---

## 1. Services & ports

### 1.1 Ports actually defined in the code

| Service | Entrypoint | Env var | Default port | Notes |
|---|---|---|---|---|
| Auth / Core API (main public API) | `cmd/auth` | `PORT` | `8001` | Full Gin router: auth, users, ISPs, jobs, quotations, geo, admin. `cmd/core` also defaults to `8001` and is the modular-monolith variant. |
| Search API | `cmd/search` | `SEARCH_ADDR` | `:8081` | Elasticsearch fast path + Postgres fallback |
| Matching worker (health/metrics) | `cmd/matching` | `MATCHING_ADDR` | `:8082` | Expiry worker; `/health`, `/metrics` only |
| Geospatial worker (health/metrics) | `cmd/geospatial` | `GEOSPATIAL_ADDR` | `:8083` | `/health`, `/metrics` only |
| Notification worker (metrics) | `cmd/notification` | `MONITORING_ADDR` | `:9091` | Consumes RabbitMQ, `/health`, `/metrics` |
| Database migration job | `cmd/migrate` | — | — | One-shot; runs `migrations/*.sql` |
| DLQ replay job | `cmd/dlq-replay` | — | — | One-shot; `-queue <name> -limit <n>` |

`config/config.yaml` also declares `server.port: 8080` and `server.base_url_local: http://localhost:8080/api/v1`; `config/config.local.yaml` uses `8001`. The **runtime port is controlled by `PORT`**, not by the YAML `server.port`.

> `cmd/auth` and `cmd/core` both default to `8001`. Deploy **one** of them — never both on the same host without changing `PORT`.

### 1.2 Documented target ports (`ispiloliteapi.md`, `ispilolitearchitecture.md`)

| Service | Documented port |
|---|---|
| Auth Service | 8001 |
| Core Service | 8002 |
| Geospatial Service | 8003 |
| Matching Service | 8004 |
| Quotation Service | 8005 |
| Notification Service | 8006 |
| Chat Service | 8007 |
| File Service | 8008 |

Use these only if you split the monolith into separate deployments. Today the single `cmd/auth` binary serves 8001.

### 1.3 Infrastructure ports (Docker)

| Component | Port(s) | Source |
|---|---|---|
| PostgreSQL | `5432` | `scripts/run-local.ps1`, `config/*.yaml` |
| Redis | `6379` | `scripts/run-local.ps1`, `config/*.yaml` |
| RabbitMQ AMQP | `5672` | `docker/docker-compose.yaml` |
| RabbitMQ management UI | `15672` | `docker/docker-compose.yaml` |
| Prometheus | `9090` | `docker/docker-compose.yaml` |
| Grafana | `3000` | `docker/docker-compose.yaml` |
| Jaeger UI | `16686` | `docker/docker-compose.yaml` |
| Jaeger OTLP HTTP | `4318` | `docker/docker-compose.yaml` |
| Elasticsearch | `9200` | `cmd/search` default `ES_ADDRESSES` |
| Public HTTP / HTTPS | `80` / `443` | reverse proxy (below) |

### 1.4 Precedence summary

```
Public internet
    │ 443 (TLS)
    ▼
Nginx / Caddy / cloud LB  ──►  127.0.0.1:8001   cmd/auth (public API)
                                  └─ /api/v1/search/*  ──►  127.0.0.1:8081  cmd/search
    ├── 127.0.0.1:8002  (optional core split)
    ├── 127.0.0.1:8082  matching worker (internal only)
    ├── 127.0.0.1:8083  geospatial worker (internal only)
    └── 127.0.0.1:9091  notification worker (internal only)

Internal only, never public:
    Postgres 5432 • Redis 6379 • RabbitMQ 5672/15672 • Prometheus 9090 • Grafana 3000
```

---

## 2. Environment variables to set on the host

Full inventory with descriptions lives in [`API_KEYS.md` §2](./API_KEYS.md#2-complete-key--environment-variable-inventory). Minimum production set:

```dotenv
# Runtime
ENVIRONMENT=production
PORT=8001
CONFIG_PATH=config/config.yaml

# Auth
JWT_SECRET=<64-char random string>
JWT_ISSUER=ispilolite
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=168h

# Postgres (writer + reader)
DATABASE_URL=postgres://user:pass@writer:5432/ispilolite?sslmode=require
DATABASE_READ_URL=postgres://user:pass@reader:5432/ispilolite?sslmode=require
DB_WRITER_MAX_CONNS=25
DB_READER_MAX_CONNS=50

# Redis
REDIS_URL=redis://:pass@redis:6379/0

# Search
ES_ADDRESSES=https://es:9200
ES_API_KEY=<elastic-api-key>
ES_ENSURE_INDICES=true

# SMS
TALKSASA_BASE_URL=https://bulksms.talksasa.com/api/v3
TALKSASA_API_TOKEN=<rotated-token>
TALKSASA_SENDER_ID=TALK-SASA

# Queue
RABBITMQ_URL=amqp://ispilo:pass@rabbitmq:5672/
RABBITMQ_REQUIRED=true

# Ops
ADMIN_ALERT_PHONES=+2547XXXXXXXX
DOCUMENT_STORAGE_ROOT=/var/lib/ispilolite/documents
AUDIT_ARCHIVE_ROOT=/var/lib/ispilolite/audit
OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318
```

Notes:
- `.env` is auto-loaded by `pkg/database.loadDotEnv` from the process working directory. In containers use real environment variables (or mount `.env`).
- In `ENVIRONMENT=production`, the process **refuses to start without `JWT_SECRET`**.
- `SKIP_MIGRATIONS=true` disables auto-migration at API startup (recommended in production; run `cmd/migrate` as a separate step).
- Never expose the reader/writer DSNs, Redis, RabbitMQ, ES, Talksasa, or Grafana to the public internet.

---

## 3. Local development

### 3.1 One-command local stack

```powershell
./scripts/run-local.ps1
```

This starts throwaway Docker containers named `ispilo-pg` (Postgres 16) and `ispilo-redis` (Redis 7) with credentials `ispilo` / `ispilo-local` / db `ispilolite`, waits for Postgres, then runs `go run ./cmd/auth` on `PORT` (default `8001`).

Point the config at the local file:

```powershell
$env:CONFIG_PATH="config/config.local.yaml"
$env:LOCAL_DB_HOST="localhost"; $env:LOCAL_DB_PORT="5432"
$env:LOCAL_DB_USER="ispilo";    $env:LOCAL_DB_PASSWORD="ispilo-local"; $env:LOCAL_DB_NAME="ispilolite"
$env:LOCAL_REDIS_HOST="localhost"; $env:LOCAL_REDIS_PORT="6379"
go run ./cmd/auth
```

> `config/*.yaml` uses `sslmode=require`. For a local Postgres without TLS, supply a `DATABASE_URL` with `sslmode=disable` (it overrides the YAML DSN).

### 3.2 Run each service manually

```bash
go run ./cmd/auth          # :8001  public API
go run ./cmd/core          # :8001  modular monolith (pick one)
PORT=8002 go run ./cmd/core
go run ./cmd/search        # :8081  search
go run ./cmd/matching      # :8082  matching worker
go run ./cmd/geospatial    # :8083  geo worker
go run ./cmd/notification  # :9091  notification worker (needs RABBITMQ_URL + NOTIFICATION_WEBHOOK_URL)
go run ./cmd/migrate       # one-shot migrations
```

### 3.3 Local infra via docker-compose

```bash
cd docker
docker compose up -d      # rabbitmq, prometheus, grafana, jaeger
```

`docker/prometheus.yml` scrapes:
- `host.docker.internal:8001` → job `ispilolite-api`
- `host.docker.internal:9091` → job `notification-worker`

### 3.4 Build binaries

```bash
go build -o bin/auth      ./cmd/auth
go build -o bin/core      ./cmd/core
go build -o bin/search    ./cmd/search
go build -o bin/matching  ./cmd/matching
go build -o bin/geospatial ./cmd/geospatial
go build -o bin/notification ./cmd/notification
go build -o bin/migrate   ./cmd/migrate
go build -o bin/dlq-replay ./cmd/dlq-replay
```

---

## 4. Hosting options

### 4.1 Option A — Single VM / VPS (simplest)

Recommended for launch. One host running:

- `cmd/auth` on `127.0.0.1:8001`
- `cmd/search` on `127.0.0.1:8081`
- Nginx/Caddy terminating TLS on `443`
- Managed Postgres (writer + replica), managed Redis, managed RabbitMQ/Elasticsearch, or the docker-compose stack

Suggested sizing: **2–4 vCPU / 4–8 GB RAM** for the API + workers; scale vertically for Postgres.

### 4.2 Option B — Docker / Compose

Build images per service and run behind the same reverse proxy. See [`docker/docker-compose.yaml`](./docker/docker-compose.yaml) for the infra services. `docker/Dockerfile` is currently empty — create per-service images, e.g.:

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/auth ./cmd/auth

FROM alpine:3.20
RUN adduser -D -u 10001 ispilo
USER ispilo
WORKDIR /app
COPY --from=build /out/auth /app/auth
COPY migrations /app/migrations
COPY config /app/config
ENV PORT=8001 CONFIG_PATH=/app/config/config.yaml
EXPOSE 8001
ENTRYPOINT ["/app/auth"]
```

Build & run:

```bash
docker build -t ispilo/auth:latest .
docker run -d --name ispilo-auth \
  --env-file .env \
  -p 127.0.0.1:8001:8001 \
  -v /var/lib/ispilolite:/var/lib/ispilolite \
  ispilo/auth:latest
```

> Bind to `127.0.0.1`, not `0.0.0.0`, so only the reverse proxy is exposed.

### 4.3 Option C — Kubernetes (high scale)

The architecture doc proposes 10 API replicas + HPA (5–20), 10/8/6 replicas for core/geo/matching, and a service mesh. Key points:

- Container port `8001` (or your `PORT`).
- Liveness probe `GET /health`, readiness probe `GET /readyz`.
- Readiness **fails** if Postgres, its replica, or Redis is unreachable — so a rolling deploy won't cut traffic to a broken replica.
- Secrets from a `Secret` (never baked into the image): `JWT_SECRET`, `DATABASE_URL`, `DATABASE_READ_URL`, `REDIS_URL`, `TALKSASA_API_TOKEN`, `ES_API_KEY`, `RABBITMQ_URL`.

Example probe block:

```yaml
ports:
  - containerPort: 8001
livenessProbe:
  httpGet: { path: /health, port: 8001 }
  initialDelaySeconds: 15
  periodSeconds: 10
readinessProbe:
  httpGet: { path: /readyz, port: 8001 }
  initialDelaySeconds: 5
  periodSeconds: 5
```

---

## 5. Reverse proxy (TLS on 443 → API on 8001)

### 5.1 Nginx

```nginx
# /etc/nginx/sites-available/ispilolite.conf
upstream ispilo_api {
    server 127.0.0.1:8001;
    keepalive 64;
}
upstream ispilo_search {
    server 127.0.0.1:8081;
    keepalive 16;
}

server {
    listen 80;
    server_name lite.ispilo.co.ke;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name lite.ispilo.co.ke;

    ssl_certificate     /etc/letsencrypt/live/lite.ispilo.co.ke/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/lite.ispilo.co.ke/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    client_max_body_size 25m;   # job-completion photo uploads

    # Search service first (specific prefix)
    location /api/v1/search/ {
        proxy_pass http://ispilo_search;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Everything else -> main API
    location / {
        proxy_pass http://ispilo_api;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Server-Sent Events (/notifications/stream) — disable buffering
        proxy_buffering off;
        proxy_read_timeout 3600s;
    }

    # Never proxy these externally; keep them internal
    location /metrics { deny all; }
}
```

> `X-Forwarded-For` matters: the rate limiter keys on the first forwarded IP (`internal/middleware/ratelimit.go`).

### 5.2 Caddy (automatic TLS)

```caddyfile
lite.ispilo.co.ke {
    @search path /api/v1/search/*
    handle @search {
        reverse_proxy 127.0.0.1:8081
    }
    handle {
        reverse_proxy 127.0.0.1:8001
    }
    request_body {
        max_size 25MB
    }
}
```

### 5.3 Cloud load balancer

- TLS terminates at the LB; forward to the API instances on `8001`.
- Health check: `GET /readyz` (returns `503` when dependencies are down).
- Idle timeout ≥ 120s (the API sets `IdleTimeout: 120s` on `cmd/auth`).
- Enable sticky sessions only if you add WebSockets (chat/jobs) to the same target group.

---

## 6. systemd units

`/etc/systemd/system/ispilolite-api.service`:

```ini
[Unit]
Description=Ispilo Lite API
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ispilo
Group=ispilo
WorkingDirectory=/opt/ispilolite
EnvironmentFile=/etc/ispilolite/api.env
ExecStart=/opt/ispilolite/bin/auth
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

`/etc/systemd/system/ispilolite-search.service`:

```ini
[Unit]
Description=Ispilo Lite Search API
After=network-online.target

[Service]
Type=simple
User=ispilo
Group=ispilo
WorkingDirectory=/opt/ispilolite
EnvironmentFile=/etc/ispilolite/search.env
Environment=SEARCH_ADDR=:8081
ExecStart=/opt/ispilolite/bin/search
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ispilolite-api ispilolite-search
sudo systemctl status ispilolite-api
```

`EnvironmentFile` must be `chmod 600` and owned by `root` (it contains secrets).

---

## 7. Database migrations

Migrations are versioned SQL files in `migrations/` with a `schema_migrations` checksum table and a Postgres advisory lock (`pkg/database/postgres.go`).

- **Dev:** run automatically at API startup (unless `SKIP_MIGRATIONS=true`).
- **Prod:** run as a one-shot job before rollout:

```bash
export CONFIG_PATH=config/config.yaml
export MIGRATIONS_PATH=migrations
./bin/migrate
```

Then start the API with `SKIP_MIGRATIONS=true`. The API verifies the presence of the core tables (`users`, `isps`, `installations`, `locations`) on both writer and reader at boot.

---

## 8. Health checks, monitoring, logs

| Check | Endpoint | Expected |
|---|---|---|
| Liveness | `GET /health` (8001) | `200 ok` |
| Readiness | `GET /readyz` (8001) | `200 ready` / `503 dependencies unavailable` |
| Search liveness | `GET /healthz` (8081) | `{"status":"ok"}` |
| Search backends | `GET /api/v1/search/health` | breaker state |
| Metrics | `GET /metrics` (8001, 8081, 8082, 8083, 9091) | Prometheus text |

Monitoring stack (`docker/docker-compose.yaml`):
- Prometheus `:9090`, Grafana `:3000` (admin creds via `GRAFANA_USER` / `GRAFANA_PASSWORD`).
- Jaeger UI `:16686`, OTLP HTTP `:4318`. Enable tracing by setting `OTEL_EXPORTER_OTLP_ENDPOINT`.

Targets in `docker/prometheus.yml` scrape `host.docker.internal:8001` and `:9091` — update the hostnames when running containers in the same network (use the service names, e.g. `ispilo-auth:8001`).

---

## 9. Firewall / security group

| Port | Expose publicly? | Purpose |
|---|---|---|
| `80`, `443` | ✅ Yes | HTTP/HTTPS via reverse proxy |
| `8001` | ❌ Bind to localhost | API behind proxy |
| `8081` | ❌ Bind to localhost | Search behind proxy |
| `8082`, `8083`, `9091` | ❌ Internal only | Workers / metrics |
| `5432`, `6379`, `5672`, `15672`, `9200` | ❌ **Never** public | Datastores & brokers |
| `9090`, `3000`, `16686`, `4318` | ❌ Internal only (SSH tunnel for dashboards) | Observability |

Harden the host: non-root service user, `chmod 600` on env files, TLS 1.2+, rotate `JWT_SECRET` / Talksasa / ES / DB credentials, and keep `.env` out of git (already in `.gitignore`).

---

## 10. Deployment checklist

- [ ] `JWT_SECRET` generated and stored in a secret manager (not in YAML, not in Flutter).
- [ ] `DATABASE_URL` / `DATABASE_READ_URL` point at the writer and read replica; `sslmode=require`.
- [ ] `REDIS_URL` reachable and password-protected.
- [ ] `cmd/migrate` run; `SKIP_MIGRATIONS=true` set on API startup.
- [ ] `TALKSASA_*` configured so OTP SMS is delivered.
- [ ] `RABBITMQ_URL` + `NOTIFICATION_WEBHOOK_URL` set for the notification worker.
- [ ] `ES_ADDRESSES` + `ES_API_KEY` set for search; `ES_ENSURE_INDICES=true` on first boot.
- [ ] Reverse proxy routes `/api/v1/search/` → `8081`, everything else → `8001`.
- [ ] TLS certificate installed; `443` open, `8001`/`8081` bound to localhost.
- [ ] `/health` and `/readyz` wired to the platform health checks.
- [ ] Prometheus scraping `/metrics`; Jaeger receiving OTLP if tracing is enabled.
- [ ] Flutter `API_BASE_URL` points at `https://lite.ispilo.co.ke/api/v1`.
- [ ] Datastores and brokers blocked from the public internet.

---

## 11. Public URLs

| Environment | URL |
|---|---|
| Production API | `https://lite.ispilo.co.ke/api/v1` |
| Local API | `http://localhost:8001/api/v1` (or `:8080` per `config.yaml`) |
| Android emulator | `http://10.0.2.2:8001/api/v1` |
| WebSocket chat | not implemented in this repository |
| WebSocket jobs | not implemented in this repository |
