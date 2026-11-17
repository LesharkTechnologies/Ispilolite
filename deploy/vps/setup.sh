#!/usr/bin/env bash
# ==============================================================================
# Ispilolite Production VPS Setup & Deployment Script
# Target OS: Ubuntu 22.04 / 24.04 LTS, Debian 12
# ==============================================================================
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}======================================================${NC}"
echo -e "${BLUE}       Ispilolite Production VPS Setup Script         ${NC}"
echo -e "${BLUE}======================================================${NC}"

if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}Error: This script must be run as root (or with sudo).${NC}"
   exit 1
fi

APP_USER="ispilo"
APP_DIR="/opt/ispilolite"
LOG_DIR="/var/log/ispilolite"
STORAGE_DIR="/var/lib/ispilolite/storage"

# 1. System packages
echo -e "\n${YELLOW}[1/8] Updating apt packages and installing dependencies...${NC}"
apt-get update -y
apt-get install -y curl git ufw nginx certbot python3-certbot-nginx postgresql postgresql-contrib redis-server jq

# 2. Dedicated service user
echo -e "\n${YELLOW}[2/8] Creating service user '${APP_USER}'...${NC}"
if ! id "${APP_USER}" &>/dev/null; then
    useradd --system --shell /usr/sbin/nologin --home-dir "${APP_DIR}" "${APP_USER}"
    echo -e "${GREEN}Created service user: ${APP_USER}${NC}"
else
    echo -e "${GREEN}Service user '${APP_USER}' already exists.${NC}"
fi

# 3. Create required directories
echo -e "\n${YELLOW}[3/8] Creating directories and setting permissions...${NC}"
mkdir -p "${APP_DIR}/bin" "${APP_DIR}/migrations" "${APP_DIR}/config" "${LOG_DIR}"
mkdir -p "${STORAGE_DIR}/documents" "${STORAGE_DIR}/audit"

chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}" "${LOG_DIR}" "${STORAGE_DIR}"
chmod 750 "${APP_DIR}" "${LOG_DIR}" "${STORAGE_DIR}"

# 4. Configure Redis and PostgreSQL
echo -e "\n${YELLOW}[4/8] Ensuring Redis and PostgreSQL services are active...${NC}"
systemctl enable --now redis-server
systemctl enable --now postgresql

# Create postgres db & user if not exists
DB_NAME="ispilolite"
DB_USER="ispilolite"
DB_PASS=$(openssl rand -hex 16)

su - postgres -c "psql -tc \"SELECT 1 FROM pg_roles WHERE rolname='${DB_USER}'\" | grep -q 1 || psql -c \"CREATE USER ${DB_USER} WITH PASSWORD '${DB_PASS}';\""
su - postgres -c "psql -tc \"SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'\" | grep -q 1 || psql -c \"CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};\""

# 5. Environment configuration file
echo -e "\n${YELLOW}[5/8] Setting up environment configuration at ${APP_DIR}/.env...${NC}"
if [ ! -f "${APP_DIR}/.env" ]; then
    JWT_SECRET=$(openssl rand -hex 32)
    REFRESH_SECRET=$(openssl rand -hex 32)
    OTP_HASH_KEY=$(openssl rand -hex 32)
    ADMIN_USER_VAL="${ADMIN_USERNAME:-admin}"
    ADMIN_PASS_VAL="${ADMIN_PASSWORD:-$(openssl rand -base64 16)}"
    cat <<EOF > "${APP_DIR}/.env"
ENVIRONMENT=production
PORT=8001
CONFIG_PATH=config/config.yaml
BASE_URL=https://lite.ispilo.co.ke
DATABASE_URL=postgres://${DB_USER}:${DB_PASS}@127.0.0.1:5432/${DB_NAME}?sslmode=disable
DB_WRITER_DSN=postgres://${DB_USER}:${DB_PASS}@127.0.0.1:5432/${DB_NAME}?sslmode=disable
DB_READER_DSN=postgres://${DB_USER}:${DB_PASS}@127.0.0.1:5432/${DB_NAME}?sslmode=disable
REDIS_URL=redis://127.0.0.1:6379/0
JWT_SECRET=${JWT_SECRET}
JWT_ISSUER=ispilolite
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=168h
REFRESH_SECRET=${REFRESH_SECRET}
OTP_HASH_KEY=${OTP_HASH_KEY}

# --- Super Admin Credentials (Auto-bootstrapped to DB on startup) ---
ADMIN_USERNAME=${ADMIN_USER_VAL}
ADMIN_PASSWORD=${ADMIN_PASS_VAL}
ADMIN_EMAIL=admin@ispilo.co.ke
ADMIN_PHONE=+254700000000

DOCUMENT_STORAGE_ROOT=${STORAGE_DIR}/documents
AUDIT_ARCHIVE_ROOT=${STORAGE_DIR}/audit
DOCUMENT_ROOT=${STORAGE_DIR}/documents
TRUSTED_PROXIES=127.0.0.1,::1
METRICS_TOKEN=$(openssl rand -hex 16)
MIGRATIONS_PATH=${APP_DIR}/migrations
TALKSASA_BASE_URL=https://bulksms.talksasa.com/api/v3
TALKSASA_API_TOKEN=${TALKSASA_API_TOKEN:-}
TALKSASA_SENDER_ID=${TALKSASA_SENDER_ID:-TALK-SASA}
OTP_TTL=10m
EOF
    chown root:"${APP_USER}" "${APP_DIR}/.env"
    chmod 640 "${APP_DIR}/.env"
    echo -e "${GREEN}Created new configuration file at ${APP_DIR}/.env.${NC}"
    echo -e "${GREEN}Provisioned Admin User: ${ADMIN_USER_VAL}${NC}"
    echo -e "${GREEN}Provisioned Admin Password: ${ADMIN_PASS_VAL}${NC}"
else
    echo -e "${GREEN}Configuration file already exists at ${APP_DIR}/.env.${NC}"
fi

# 6. Build and install Go binaries
echo -e "\n${YELLOW}[6/8] Building application binaries...${NC}"
if ! command -v go &>/dev/null; then
    echo -e "${YELLOW}Installing Go toolchain...${NC}"
    GO_VERSION="${GO_VERSION:-1.24.0}"
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
    rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    if ! grep -q "/usr/local/go/bin" /etc/profile; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
    fi
fi
export PATH=$PATH:/usr/local/go/bin
export GOTOOLCHAIN=auto

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Copy config and migrations if running from a different clone location
if [ "${REPO_ROOT}" != "${APP_DIR}" ]; then
    cp -r "${REPO_ROOT}/config/"* "${APP_DIR}/config/"
    cp -r "${REPO_ROOT}/migrations/"* "${APP_DIR}/migrations/"
fi
chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}/config" "${APP_DIR}/migrations"

# Build core API, notification service, and migration runner
go build -buildvcs=false -o "${APP_DIR}/bin/auth" "${REPO_ROOT}/cmd/auth"
go build -buildvcs=false -o "${APP_DIR}/bin/core" "${REPO_ROOT}/cmd/core"
go build -buildvcs=false -o "${APP_DIR}/bin/notification" "${REPO_ROOT}/cmd/notification"
go build -buildvcs=false -o "${APP_DIR}/bin/migrate" "${REPO_ROOT}/cmd/migrate"
chmod +x "${APP_DIR}/bin/"*
chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}/bin"

# Run database migrations
echo -e "\n${YELLOW}[7/8] Running database migrations...${NC}"
set -a
# shellcheck disable=SC1091
source "${APP_DIR}/.env"
set +a
(cd "${APP_DIR}" && "${APP_DIR}/bin/migrate")

# 7. Systemd & Nginx configuration
echo -e "\n${YELLOW}[8/8] Installing systemd services and Nginx configuration...${NC}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Copy systemd units
cp "${REPO_ROOT}/deploy/systemd/ispilolite-api.service" /etc/systemd/system/
cp "${REPO_ROOT}/deploy/systemd/ispilolite-notification.service" /etc/systemd/system/
if [ -f "${REPO_ROOT}/deploy/systemd/ispilolite-backup.service" ]; then
    cp "${REPO_ROOT}/deploy/systemd/ispilolite-backup.service" /etc/systemd/system/
    cp "${REPO_ROOT}/deploy/systemd/ispilolite-backup.timer" /etc/systemd/system/
fi

systemctl daemon-reload
systemctl enable --now ispilolite-api
if grep -q "^NOTIFICATION_WEBHOOK_URL=https" "${APP_DIR}/.env" 2>/dev/null; then
    systemctl enable --now ispilolite-notification || true
else
    echo -e "${YELLOW}Notification worker idle (configure NOTIFICATION_WEBHOOK_URL in .env to activate).${NC}"
fi

# Ensure SSL certificate directory and bootstrap cert exist so Nginx starts cleanly before Certbot
CERT_DIR="/etc/letsencrypt/live/lite.ispilo.co.ke"
if [ ! -f "${CERT_DIR}/fullchain.pem" ]; then
    echo -e "${YELLOW}Generating bootstrap SSL certificate for initial Nginx startup...${NC}"
    mkdir -p "${CERT_DIR}"
    openssl req -x509 -nodes -days 1 -newkey rsa:2048 \
        -keyout "${CERT_DIR}/privkey.pem" \
        -out "${CERT_DIR}/fullchain.pem" \
        -subj "/CN=lite.ispilo.co.ke" >/dev/null 2>&1
fi

# Nginx setup
cp "${REPO_ROOT}/deploy/nginx/ispilolite.conf" /etc/nginx/sites-available/ispilolite.conf
ln -sf /etc/nginx/sites-available/ispilolite.conf /etc/nginx/sites-enabled/ispilolite.conf
rm -f /etc/nginx/sites-enabled/default

# Firewall
ufw allow 22/tcp || true
ufw allow 80/tcp || true
ufw allow 443/tcp || true
ufw --force enable || true

nginx -t && systemctl reload nginx

echo -e "\n${GREEN}======================================================${NC}"
echo -e "${GREEN}   Ispilolite Deployment Completed Successfully!     ${NC}"
echo -e "${GREEN}======================================================${NC}"
echo -e "API Status:        $(systemctl is-active ispilolite-api)"
echo -e "Health Check:      curl -i http://127.0.0.1:8001/health"
echo -e "Config File:       ${APP_DIR}/.env"
echo -e "Logs:              journalctl -u ispilolite-api -f"
echo -e "Admin Login API:   POST https://lite.ispilo.co.ke/api/v1/auth/admin/login"
echo -e "\nNext step for SSL: certbot --nginx -d lite.ispilo.co.ke"
