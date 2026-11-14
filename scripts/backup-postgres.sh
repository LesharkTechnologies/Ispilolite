#!/usr/bin/env bash
#
# Nightly PostgreSQL backup for Ispilolite.
#
# - Creates a compressed custom-format dump and verifies it with pg_restore.
# - Optionally encrypts the offsite copy with GPG.
# - Optionally uploads to any rclone remote (S3, B2, GCS, SFTP, ...).
# - Prunes local dumps older than the retention window.
# - Writes node_exporter textfile metrics so Prometheus can alert on staleness.
#
# Required:
#   DATABASE_URL   writer DSN (or a dedicated backup role with SELECT + pg_read_all_data)
#
# Optional:
#   BACKUP_DIR                 local directory (default /var/backups/ispilolite)
#   BACKUP_RETENTION_DAYS      local retention in days (default 30)
#   BACKUP_OFFSITE_REMOTE      rclone target, e.g. s3-backup:ispilolite/postgres
#   BACKUP_OFFSITE_CMD         custom upload command; receives file paths as "$@"
#   BACKUP_GPG_RECIPIENT       encrypt the offsite copy with this GPG key
#   BACKUP_METRICS_FILE        textfile path, e.g.
#                              /var/lib/node_exporter/textfile/ispilo_backup.prom
#   BACKUP_OFFSITE_PREFIX      filename prefix (default ispilolite)

set -Eeuo pipefail

log() { printf '%s backup: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

trap 'log "ERROR: command failed at line $LINENO"' ERR

: "${DATABASE_URL:?DATABASE_URL must be set}"
command -v pg_dump >/dev/null 2>&1 || die "pg_dump not found"
command -v pg_restore >/dev/null 2>&1 || die "pg_restore not found"

BACKUP_DIR="${BACKUP_DIR:-/var/backups/ispilolite}"
RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
OFFSITE_REMOTE="${BACKUP_OFFSITE_REMOTE:-}"
OFFSITE_CMD="${BACKUP_OFFSITE_CMD:-}"
OFFSITE_REQUIRED="${BACKUP_OFFSITE_REQUIRED:-false}"
GPG_RECIPIENT="${BACKUP_GPG_RECIPIENT:-}"
METRICS_FILE="${BACKUP_METRICS_FILE:-}"
PREFIX="${BACKUP_OFFSITE_PREFIX:-ispilolite}"

if [[ "$OFFSITE_REQUIRED" == "true" && -z "$OFFSITE_REMOTE" && -z "$OFFSITE_CMD" ]]; then
  die "offsite backup is required but neither BACKUP_OFFSITE_REMOTE nor BACKUP_OFFSITE_CMD is configured"
fi

# Backups and checksums are sensitive: owner-only permissions.
umask 077
mkdir -p "$BACKUP_DIR"

# Serialise runs so two timers cannot overlap.
exec 9>"$BACKUP_DIR/.backup.lock"
flock -n 9 || die "another backup is already running"

started_at="$(date +%s)"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
base="${PREFIX}_${stamp}"
dump="$BACKUP_DIR/${base}.dump"

log "dumping database to $dump"
pg_dump --dbname="$DATABASE_URL" --format=custom --compress=9 --no-owner --file="$dump"

# Validate the archive header before publishing it as a successful backup.
pg_restore --list "$dump" >/dev/null

hash_file() {
  local file="$1"
  local checksum
  checksum="$(sha256sum "$file" | awk '{print $1}')"
  printf '%s  %s\n' "$checksum" "$(basename "$file")" > "$file.sha256"
}

hash_file "$dump"
log "dump verified: $(du -h "$dump" | cut -f1)"

# Files that leave the host.
upload_files=("$dump" "${dump}.sha256")

if [[ -n "$GPG_RECIPIENT" ]]; then
  command -v gpg >/dev/null 2>&1 || die "GPG_RECIPIENT is set but gpg is not installed"
  log "encrypting offsite copy for recipient $GPG_RECIPIENT"
  gpg --batch --yes --trust-model always \
    --encrypt --recipient "$GPG_RECIPIENT" \
    --output "${dump}.gpg" "$dump"
  hash_file "${dump}.gpg"
  upload_files=("${dump}.gpg" "${dump}.gpg.sha256")
fi

if [[ -n "$OFFSITE_REMOTE" ]]; then
  command -v rclone >/dev/null 2>&1 || die "BACKUP_OFFSITE_REMOTE is set but rclone is not installed"
  for file in "${upload_files[@]}"; do
    log "uploading $(basename "$file") to $OFFSITE_REMOTE"
    rclone copy --immutable "$file" "$OFFSITE_REMOTE"
  done
fi

if [[ -n "$OFFSITE_CMD" ]]; then
  log "running custom offsite upload command"
  bash -c "$OFFSITE_CMD" bash "${upload_files[@]}"
fi

# Keep the newest N days locally. Offsite retention is handled by the remote
# provider's lifecycle policy (see docs/disaster-recovery.md).
deleted="$(find "$BACKUP_DIR" -maxdepth 1 -type f -name "${PREFIX}_*" -mtime +"$RETENTION_DAYS" -print -delete | wc -l)"
if [[ "$deleted" -gt 0 ]]; then
  log "pruned $deleted local file(s) older than ${RETENTION_DAYS} days"
fi

if [[ -n "$METRICS_FILE" ]]; then
  mkdir -p "$(dirname "$METRICS_FILE")"
  tmp="$(mktemp "${METRICS_FILE}.XXXXXX")"
  cat >"$tmp" <<EOF
# HELP ispilo_backup_last_success_timestamp_seconds Unix time of the last successful database backup.
# TYPE ispilo_backup_last_success_timestamp_seconds gauge
ispilo_backup_last_success_timestamp_seconds $(date +%s)
# HELP ispilo_backup_size_bytes Size of the most recent custom-format dump.
# TYPE ispilo_backup_size_bytes gauge
ispilo_backup_size_bytes $(stat -c '%s' "$dump")
# HELP ispilo_backup_duration_seconds Wall-clock duration of the most recent backup run.
# TYPE ispilo_backup_duration_seconds gauge
ispilo_backup_duration_seconds $(( $(date +%s) - started_at ))
EOF
  chmod 0644 "$tmp"
  mv "$tmp" "$METRICS_FILE"
fi

log "backup completed successfully"
