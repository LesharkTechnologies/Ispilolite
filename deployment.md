# Ispilo Lite — VPS Deployment Guide

Host everything on **one Linux VPS** with no prior configuration. This guide assumes a fresh Ubuntu 22.04/24.04 server, root access, and the domain **`lite.ispilo.co.ke`** pointing at the VPS IP.

> Companion docs: [`API_KEYS.md`](./API_KEYS.md) (Flutter config, keys, request/response), [`server.md`](./server.md) (ports, architecture), [`ispiloliteapi.md`](./ispiloliteapi.md) (full API reference).

---

## 0. Architecture on a single VPS

All services bind to `127.0.0.1` and Nginx terminates public TLS on `443` and proxies to the local Go APIs.

```
Public internet ── 443 (TLS) ── Nginx ── 127.0.0.1:8001  → cmd/auth  (main API: auth, users, jobs, quotations, geo, admin)
                                                 └─ /api/v1/search/* ── 127.0.0.1:8081  → cmd/search
                                                 └─ /api/v1/matching/* ── 127.0.0.1:8082 → cmd/matching (health/worker)
                                                 └─ /api/v1/geo/* ── 127.0.0.1:8083      → cmd/geospatial (health)
Internal only (never public): Postgres 5432 · Redis 6379 · RabbitMQ 5672
```

Flutter app base URL: `https://lite.ispilo.co.ke/api/v1`

---

## 1. VPS requirements

| Item | Minimum | Recommended |
|---|---|---|
| vCPU | 2 | 4 |
| RAM | 4 GB | 8 GB |
| SSD | 50 GB | 100 GB+ |
| OS | Ubuntu 22.04 LTS | Ubuntu 24.04 LTS |
| Public IPv4 | 1 | 1 |
| Domain | `lite.ispilo.co.ke` with A record → VPS IP | same |

DNS: create an **A record** `lite.ispilo.co.ke` → `<VPS_IP>`. Wait for propagation before requesting SSL.

---

## 2. Initial server hardening (run as root)

```bash
# Create a non-root deploy user
adduser ispilo
usermod -aG sudo ispilo

# Switch to the deploy user for most work
su - ispilo
```

### Firewall (UFW)

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow ssh
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

Never open `5432`, `6379`, `5672`, `8001`, `8081`, `8082`, `8083`, `9091` to the public internet — bind them to `127.0.0.1` only.

---

## 3. Install system dependencies

```bash
sudo apt update && sudo apt upgrade -y

# Core build tools
sudo apt install -y git curl wget build-essential software-properties-common gnupg lsb-release

# PostgreSQL + Redis + RabbitMQ + Nginx + Certbot
sudo apt install -y postgresql postgresql-contrib redis-server rabbitmq-server nginx certbot python3-certbot-nginx

# Go toolchain (check latest at go.dev/dl; this example uses 1.22)
wget https://go.dev/dl/go1.22.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.linux-amd64.tar.gz
rm go1.22.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.profile
source ~/.profile
go version   # should print go1.22.x

# Enable & start infra services
sudo systemctl enable --now postgresql
sudo systemctl enable --now redis-server
sudo systemctl enable --now rabbitmq-server
sudo systemctl enable --now nginx
```

### Secure Redis (bind to localhost, require password)

Edit `/etc/redis/redis.conf`:

```text
bind 127.0.0.1 ::1
protected-mode yes
requirepass <STRONG_REDIS_PASSWORD>
```

```bash
sudo systemctl restart redis-server
```

### Secure PostgreSQL

Postgres defaults to local socket auth. We'll create a dedicated app database and user, and keep it on localhost only.

```bash
sudo -u postgres psql
```

```sql
CREATE DATABASE ispilolite;
CREATE USER ispilo_app WITH PASSWORD '<STRONG_DB_PASSWORD>';
GRANT ALL PRIVILEGES ON DATABASE ispilolite TO ispilo_app;
\q
```

Apply to localhost only: ensure `postgresql.conf` has `listen_addresses = 'localhost'` and `pg_hba.conf` uses `scram-sha-256` for local connections, then:

```bash
sudo systemctl restart postgresql
```

### RabbitMQ (localhost only)

```bash
sudo rabbitmq-plugins enable rabbitmq_management
sudo systemctl restart rabbitmq-server
```

Create a RabbitMQ user/vhost:

```bash
sudo rabbitmqctl add_user ispilo <STRONG_RABBIT_PASSWORD>
sudo rabbitmqctl add_vhost ispilo_vhost
sudo rabbitmqctl set_permissions -p ispilo_vhost ispilo ".*" ".*" ".*"
```

Management UI is on `15672` — keep it localhost-only via Nginx or SSH tunnel if you need to access it.

---

## 4. Clone the repository

```bash
sudo mkdir -p /opt/ispilolite
sudo chown ispilo:ispilo /opt/ispilolite
cd /opt/ispilolite
git clone <your-repo-url> .
git checkout main   # or your production branch
```

---

## 5. Configuration

### 5.1 Environment file (`.env`)

Create `/opt/ispilolite/.env`. **Never commit this file** (it is already in `.gitignore`).

```dotenv
# --- Runtime ---
ENVIRONMENT=production
PORT=8001
CONFIG_PATH=config/config.yaml

# --- Auth / JWT (REQUIRED in production; no fallback) ---
JWT_SECRET=<64+ random characters, e.g. openssl rand -hex 32>
JWT_ISSUER=ispilolite
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=8760h

# --- OTP hashing (REQUIRED in production) ---
# Verification codes are stored in Redis only as HMAC-SHA256 digests keyed by
# this secret. Rotating it instantly invalidates every outstanding OTP.
OTP_HASH_KEY=<64+ random characters, e.g. openssl rand -hex 32>

# --- Super Admin Account (Environment Provisioned) ---
# Username and password used by administrators and audit officers to sign in with full system access.
ADMIN_USERNAME=admin
ADMIN_PASSWORD=<STRONG_ADMIN_PASSWORD>
ADMIN_EMAIL=admin@ispilo.co.ke
ADMIN_PHONE=+254700000000

# --- Database (writer + reader share the same local Postgres) ---
DATABASE_URL=postgres://ispilo_app:<STRONG_DB_PASSWORD>@127.0.0.1:5432/ispilolite?sslmode=disable
DB_WRITER_MAX_CONNS=25
DB_READER_MAX_CONNS=50

# --- Redis ---
REDIS_URL=redis://:<STRONG_REDIS_PASSWORD>@127.0.0.1:6379/0

# --- Search (Elasticsearch) ---
# Optional in production if you don't run ES; search degrades to Postgres fallback.
ES_ADDRESSES=http://127.0.0.1:9200
# ES_API_KEY=<your-elastic-api-key>
# ES_ENSURE_INDICES=true

# --- SMS (Talksasa) ---
TALKSASA_BASE_URL=https://bulksms.talksasa.com/api/v3
TALKSASA_API_TOKEN=<your-talksasa-token>
TALKSASA_SENDER_ID=TALK-SASA

# --- Message queue ---
RABBITMQ_URL=amqp://ispilo:<STRONG_RABBIT_PASSWORD>@127.0.0.1:5672/ispilo_vhost
RABBITMQ_REQUIRED=false
RABBITMQ_CONSUMER_NAME=ispilolite-api
RABBITMQ_PREFETCH=20

# --- Notifications ---
# Must be a separate delivery provider; the Go API has no /notifications/webhook route.
NOTIFICATION_WEBHOOK_URL=https://notifications.example.com/deliver
ADMIN_ALERT_PHONES=+2547XXXXXXXX

# --- Media storage (Cloudflare R2 presigned PUT uploads) ---
R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
R2_ACCESS_KEY_ID=<your-r2-access-key-id>
R2_SECRET_ACCESS_KEY=<your-r2-secret-access-key>
R2_BUCKET=ispilo-media
R2_PUBLIC_BASE_URL=https://media.ispilo.co.ke
R2_REGION=auto
R2_FOLDER_PREFIX=ispilolite
R2_ALLOWED_HOSTS=media.ispilo.co.ke

# --- Storage / audit ---
DOCUMENT_STORAGE_ROOT=/var/lib/ispilolite/documents
AUDIT_ARCHIVE_ROOT=/var/lib/ispilolite/audit

# --- Observability ---
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
```

Generate secrets:

```bash
openssl rand -hex 32    # JWT_SECRET
openssl rand -hex 32    # OTP_HASH_KEY (verify codes are hashed at rest)
openssl rand -base64 24 # DB / Redis / Rabbit passwords
```

Create storage directories:

```bash
sudo mkdir -p /var/lib/ispilolite/documents /var/lib/ispilolite/audit /var/log/ispilolite
sudo chown -R ispilo:ispilo /var/lib/ispilolite /var/log/ispilolite
```

### 5.2 `config/config.yaml`

The repo already has `config/config.yaml` with `${VAR}` placeholders expanded from environment at runtime (`os.ExpandEnv`). No changes required unless you want to override defaults:

```yaml
server:
  base_url_prod: "https://lite.ispilo.co.ke/api/v1"
  base_url_local: "http://localhost:8001/api/v1"
  port: 8001
```

---

## 6. Database migrations

Migrations live in `migrations/*.sql` and are tracked in a `schema_migrations` table (checksummed, idempotent — safe to re-run).

### 6.1 Run migrations manually (first deploy)

```bash
cd /opt/ispilolite
export CONFIG_PATH=config/config.yaml
export DATABASE_URL="postgres://ispilo_app:<PASS>@127.0.0.1:5432/ispilolite?sslmode=disable"
export SKIP_MIGRATIONS=true   # migrate command runs migrations itself
go run ./cmd/migrate
```

Or build once and reuse:

```bash
go build -o bin/migrate ./cmd/migrate
CONFIG_PATH=config/config.yaml DATABASE_URL=... ./bin/migrate
```

### 6.2 Migration cron job (safety net)

The user wants migrations to run whenever code updates. Because migrations are idempotent, a cron can safely re-apply them. Create:

**`/opt/ispilolite/scripts/run-migrations.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

cd /opt/ispilolite

# Load environment
if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi

# Defaults
export CONFIG_PATH="${CONFIG_PATH:-config/config.yaml}"
export DATABASE_URL="${DATABASE_URL:?DATABASE_URL is required}"
export SKIP_MIGRATIONS="${SKIP_MIGRATIONS:-true}"
export MIGRATIONS_PATH="${MIGRATIONS_PATH:-migrations}"

LOG_FILE="/var/log/ispilolite/migrate-$(date +%F).log"

echo "=== $(date -Iseconds) starting migrations ===" | tee -a "$LOG_FILE"

if ! command -v go >/dev/null 2>&1; then
  echo "Go not found; attempting to use prebuilt binary" | tee -a "$LOG_FILE"
  if [ -x ./bin/migrate ]; then
    ./bin/migrate >> "$LOG_FILE" 2>&1
  else
    echo "ERROR: no go and no ./bin/migrate" | tee -a "$LOG_FILE"
    exit 1
  fi
else
  go run ./cmd/migrate >> "$LOG_FILE" 2>&1
fi

echo "=== $(date -Iseconds) migrations finished ===" | tee -a "$LOG_FILE"
```

```bash
chmod +x /opt/ispilolite/scripts/run-migrations.sh
sudo chown ispilo:ispilo /opt/ispilolite/scripts/run-migrations.sh
```

Cron (run as `ispilo`):

```cron
# Run migrations every 15 minutes — idempotent, safe to re-run
*/15 * * * * /opt/ispilolite/scripts/run-migrations.sh >> /var/log/ispilolite/migrate-cron.log 2>&1
```

Install:

```bash
crontab -l 2>/dev/null | { cat; echo "*/15 * * * * /opt/ispilolite/scripts/run-migrations.sh >> /var/log/ispilolite/migrate-cron.log 2>&1"; } | crontab -
```

> Recommended workflow: **always** run migrations as part of your deploy script (see §10) rather than relying only on cron. The cron is a safety net in case a deploy forgets to migrate.

---

## 7. Build Go binaries

```bash
cd /opt/ispilolite

# Main public API (recommended single entrypoint)
go build -o bin/auth ./cmd/auth

# Optional split entrypoints (only if you want them as separate processes)
go build -o bin/core ./cmd/core
go build -o bin/search ./cmd/search
go build -o bin/matching ./cmd/matching
go build -o bin/geospatial ./cmd/geospatial
go build -o bin/notification ./cmd/notification

# One-shot tools
go build -o bin/migrate ./cmd/migrate
go build -o bin/dlq-replay ./cmd/dlq-replay
```

---

## 8. systemd services

Run everything as `ispilo`. Create these unit files.

### 8.1 Main API — `/etc/systemd/system/ispilolite-api.service`

```ini
[Unit]
Description=Ispilo Lite API (cmd/auth)
After=network-online.target postgresql.service redis-server.service rabbitmq-server.service
Wants=network-online.target

[Service]
Type=simple
User=ispilo
Group=ispilo
WorkingDirectory=/opt/ispilolite
EnvironmentFile=/opt/ispilolite/.env
ExecStart=/opt/ispilolite/bin/auth
Restart=always
RestartSec=5
LimitNOFILE=65535

# Safety: never expose directly; Nginx is the only public entry.
Environment=PORT=8001

[Install]
WantedBy=multi-user.target
```

### 8.2 Core (optional second API instance on 8002) — `/etc/systemd/system/ispilolite-core.service`

Only if you want `cmd/core` alongside `cmd/auth`. Change `PORT=8002`.

```ini
[Unit]
Description=Ispilo Lite Core API (cmd/core)
After=network-online.target postgresql.service redis-server.service rabbitmq-server.service
Wants=network-online.target

[Service]
Type=simple
User=ispilo
Group=ispilo
WorkingDirectory=/opt/ispilolite
EnvironmentFile=/opt/ispilolite/.env
ExecStart=/opt/ispilolite/bin/core
Restart=always
RestartSec=5
Environment=PORT=8002

[Install]
WantedBy=multi-user.target
```

### 8.3 Matching worker — `/etc/systemd/system/ispilolite-matching.service`

```ini
[Unit]
Description=Ispilo Lite Matching worker (health + expiry)
After=network-online.target postgresql.service redis-server.service rabbitmq-server.service
Wants=network-online.target

[Service]
Type=simple
User=ispilo
Group=ispilo
WorkingDirectory=/opt/ispilolite
EnvironmentFile=/opt/ispilolite/.env
ExecStart=/opt/ispilolite/bin/matching
Restart=always
RestartSec=5
Environment=MATCHING_ADDR=:8082

[Install]
WantedBy=multi-user.target
```

### 8.4 Geospatial worker — `/etc/systemd/system/ispilolite-geospatial.service`

```ini
[Unit]
Description=Ispilo Lite Geospatial worker (health)
After=network-online.target

[Service]
Type=simple
User=ispilo
Group=ispilo
WorkingDirectory=/opt/ispilolite
EnvironmentFile=/opt/ispilolite/.env
ExecStart=/opt/ispilolite/bin/geospatial
Restart=always
RestartSec=5
Environment=GEOSPATIAL_ADDR=:8083

[Install]
WantedBy=multi-user.target
```

### 8.5 Notification worker — `/etc/systemd/system/ispilolite-notification.service`

```ini
[Unit]
Description=Ispilo Lite Notification worker
After=network-online.target rabbitmq-server.service
Wants=network-online.target

[Service]
Type=simple
User=ispilo
Group=ispilo
WorkingDirectory=/opt/ispilolite
EnvironmentFile=/opt/ispilolite/.env
ExecStart=/opt/ispilolite/bin/notification
Restart=always
RestartSec=5
Environment=MONITORING_ADDR=:9091

[Install]
WantedBy=multi-user.target
```

### 8.6 Enable & start

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ispilolite-api
sudo systemctl enable --now ispilolite-matching
sudo systemctl enable --now ispilolite-geospatial
sudo systemctl enable --now ispilolite-notification

# Check status
sudo systemctl status ispilolite-api --no-pager
sudo journalctl -u ispilolite-api -n 50 --no-pager
```

---

## 9. Nginx reverse proxy + SSL (Let's Encrypt)

### 9.1 Nginx site config

`/etc/nginx/sites-available/ispilolite`:

```nginx
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
    ssl_prefer_server_ciphers on;

    client_max_body_size 25m;          # job-completion photo uploads

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "no-referrer-when-downgrade" always;

    # Search service first (specific prefix)
    location /api/v1/search/ {
        proxy_pass http://ispilo_search;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Everything else → main API
    location / {
        proxy_pass http://ispilo_api;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Server-Sent Events stream (notifications)
        location ~* /notifications/stream {
            proxy_buffering off;
            proxy_cache off;
            proxy_read_timeout 3600s;
            proxy_send_timeout 3600s;
        }
    }

    # Internal-only endpoints — never expose publicly
    location /metrics { deny all; }
    location /readyz  { deny all; }
    location /healthz { deny all; }

    # Health check allowed from localhost (for uptime monitors)
    location = /health {
        allow 127.0.0.1;
        allow ::1;
        deny all;
        proxy_pass http://ispilo_api;
    }
}
```

Enable it:

```bash
sudo ln -sf /etc/nginx/sites-available/ispilolite /etc/nginx/sites-enabled/ispilolite
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl reload nginx
```

### 9.2 SSL certificate

```bash
sudo certbot --nginx -d lite.ispilo.co.ke --email admin@lite.ispilo.co.ke --agree-tos --non-interactive
sudo certbot renew --dry-run
```

Certbot auto-adds a renewal cron. Verify:

```bash
sudo systemctl list-timers certbot.timer
```

---

## 10. Deployment / update workflow

Whenever you push new code:

```bash
# On the VPS, as ispilo
cd /opt/ispilolite
git pull origin main

# 1. Run migrations (idempotent)
./scripts/run-migrations.sh

# 2. Rebuild binaries
go build -o bin/auth ./cmd/auth
go build -o bin/core ./cmd/core    # if used
go build -o bin/search ./cmd/search
go build -o bin/matching ./cmd/matching
go build -o bin/geospatial ./cmd/geospatial
go build -o bin/notification ./cmd/notification

# 3. Restart services
sudo systemctl restart ispilolite-api
sudo systemctl restart ispilolite-matching
sudo systemctl restart ispilolite-geospatial
sudo systemctl restart ispilolite-notification

# 4. Verify
curl -s https://lite.ispilo.co.ke/health
sudo systemctl status ispilolite-api --no-pager
```

### Optional: one-command deploy script

`/opt/ispilolite/scripts/deploy.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd /opt/ispilolite

git pull origin main
./scripts/run-migrations.sh

go build -o bin/auth ./cmd/auth
go build -o bin/core ./cmd/core
go build -o bin/search ./cmd/search
go build -o bin/matching ./cmd/matching
go build -o bin/geospatial ./cmd/geospatial
go build -o bin/notification ./cmd/notification

sudo systemctl restart ispilolite-api ispilolite-core ispilolite-matching ispilolite-geospatial ispilotine-notification
sudo systemctl reload nginx

echo "Deploy complete at $(date -Iseconds)"
```

```bash
chmod +x /opt/ispilolite/scripts/deploy.sh
```

---

## 11. Health checks & monitoring

| Check | Command | Expected |
|---|---|---|
| API liveness | `curl -s http://127.0.0.1:8001/health` | `ok` |
| API readiness | `curl -s http://127.0.0.1:8001/readyz` | `ready` (503 if DB/Redis down) |
| Public HTTPS | `curl -s -o /dev/null -w "%{http_code}" https://lite.ispilo.co.ke/api/v1/isps` | `200` |
| Search | `curl -s http://127.0.0.1:8081/healthz` | `{"status":"ok"}` |
| Matching | `curl -s http://127.0.0.1:8082/health` | `ok` |
| Geospatial | `curl -s http://127.0.0.1:8083/health` | `ok` |

Nginx exposes `/metrics` internally — deny it publicly (done in §9.1). If you want Prometheus scraping, add a local scrape target on `127.0.0.1:8001/metrics` via a firewall-restricted node exporter or keep it off the public interface.

Logs:

```bash
sudo journalctl -u ispilolite-api -f
sudo journalctl -u ispilolite-matching -f
tail -f /var/log/ispilolite/migrate-cron.log
```

---

## 12. Backups

### PostgreSQL (daily, verified, offsite)

`scripts/backup-postgres.sh` creates a compressed, checksummed dump, verifies
it with `pg_restore`, prunes old files, uploads offsite (rclone or a custom
command), and emits metrics for the `IspiloliteBackupStale` alert. Full setup,
offsite encryption, and restore drills are in
[`docs/disaster-recovery.md`](./docs/disaster-recovery.md).

```bash
sudo install -d -o ispilo -g ispilo -m 0700 /var/backups/ispilolite
sudo install -d -o ispilo -g ispilo -m 0755 /var/lib/node_exporter/textfile
sudo install -d -o root -g root -m 0750 /etc/ispilolite
sudo cp deploy/systemd/ispilolite-backup.env.example /etc/ispilolite/backup.env
sudo chmod 600 /etc/ispilolite/backup.env
sudo editor /etc/ispilolite/backup.env   # DATABASE_URL, BACKUP_OFFSITE_REMOTE, GPG key

sudo cp deploy/systemd/ispilolite-backup.{service,timer} /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ispilolite-backup.timer
sudo systemctl start ispilolite-backup.service   # prove it now
```

### Documents & audit

```bash
0 3 * * * tar -czf /var/backups/ispilolite/storage-$(date +%F).tar.gz -C /var/lib/ispilolite documents audit
```

The PostgreSQL script uploads its own offsite copy; also copy documents and
audit archives off-server (rsync to another host or S3) — a VPS disk failure loses everything otherwise.

---

## 13. Firewall summary (final)

| Port | Service | Public? | Bind |
|---|---|---|---|
| 22 | SSH | Yes (restrict by IP if possible) | 0.0.0.0 |
| 80 | HTTP → HTTPS redirect | Yes | 0.0.0.0 |
| 443 | HTTPS API | Yes | 0.0.0.0 |
| 5432 | PostgreSQL | **No** | 127.0.0.1 |
| 6379 | Redis | **No** | 127.0.0.1 |
| 5672 | RabbitMQ | **No** | 127.0.0.1 |
| 8001 | Go API | **No** (Nginx only) | 127.0.0.1 |
| 8081/8082/8083/9091 | Workers / metrics | **No** | 127.0.0.1 |

UFW rules already cover this if you only allow 22/80/443.

---

## 14. Flutter app configuration

In `lib/config/env.dart` (or equivalent):

```dart
class Env {
  static const apiBaseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: 'https://lite.ispilo.co.ke/api/v1',
  );
}
```

Build:

```bash
flutter build apk --dart-define=API_BASE_URL=https://lite.ispilo.co.ke/api/v1
```

The app authenticates via JWT from `/api/v1/auth/*` and sends `Authorization: Bearer <access_token>` on protected calls.

---

## 15. Troubleshooting checklist

1. **API won't start** → `sudo journalctl -u ispilolite-api -n 100`. Check `JWT_SECRET` is set in `.env` and `DATABASE_URL` is valid.
2. **Migrations fail** → confirm `pg_hba.conf` allows `ispilo_app` password auth on localhost and `DATABASE_URL` uses `sslmode=disable` for local Postgres.
3. **502 Bad Gateway** → API not listening on `127.0.0.1:8001`; check `systemctl status ispilolite-api`.
4. **OTP SMS not arriving** → verify `TALKSASA_API_TOKEN`, `TALKSASA_SENDER_ID`, and that `TALKSASA_BASE_URL` is reachable from the VPS.
5. **WebSocket / chat** → not implemented in this repository (see `API_KEYS.md` §5.13). Use SSE `GET /api/v1/notifications/stream` instead.
6. **Certificate renewal** → `sudo certbot renew --dry-run` and check `certbot.timer` is active.
