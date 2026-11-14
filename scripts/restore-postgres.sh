#!/usr/bin/env bash
#
# Restore an Ispilolite PostgreSQL dump.
#
#   DATABASE_URL=postgres://... ./scripts/restore-postgres.sh /var/backups/ispilolite/ispilolite_20260101T023000Z.dump --yes
#
# WARNING: pg_restore runs with --clean --if-exists, so objects that exist in
# the target database are dropped and recreated. Always restore into an
# isolated instance first (see docs/disaster-recovery.md).

set -Eeuo pipefail

log() { printf '%s restore: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

usage() {
  cat >&2 <<'EOF'
Usage: restore-postgres.sh <dump-file> [--yes]

  dump-file   .dump (custom format) or .dump.gpg produced by backup-postgres.sh
  --yes       skip the interactive confirmation prompt

Environment:
  DATABASE_URL   target database DSN (required)
EOF
  exit 2
}

[[ $# -ge 1 ]] || usage
dump="$1"
shift

assume_yes=false
for arg in "$@"; do
  case "$arg" in
    --yes|-y) assume_yes=true ;;
    *) usage ;;
  esac
done

: "${DATABASE_URL:?DATABASE_URL must be set}"
command -v pg_restore >/dev/null 2>&1 || die "pg_restore not found"
[[ -f "$dump" ]] || die "dump file not found: $dump"

# Verify the checksum next to the dump before touching the target database.
if [[ -f "${dump}.sha256" ]]; then
  log "verifying checksum"
  ( cd "$(dirname "$dump")" && sha256sum -c "$(basename "$dump").sha256" )
fi

restore_file="$dump"
decrypted=""
cleanup() {
  if [[ -n "$decrypted" ]]; then
    rm -f "$decrypted"
  fi
}
trap cleanup EXIT

if [[ "$dump" == *.gpg ]]; then
  command -v gpg >/dev/null 2>&1 || die "encrypted dump requires gpg"
  decrypted="$(mktemp)"
  log "decrypting $dump"
  gpg --batch --yes --decrypt --output "$decrypted" "$dump"
  restore_file="$decrypted"
fi

# Validate the archive before invoking pg_restore.
pg_restore --list "$restore_file" >/dev/null || die "not a valid pg_dump custom-format archive"

if [[ "$assume_yes" != true ]]; then
  printf 'Restore %s into %s? This drops existing objects. [y/N] ' "$dump" "$DATABASE_URL" >&2
  read -r answer
  [[ "$answer" == "y" || "$answer" == "Y" ]] || { log "aborted"; exit 1; }
fi

log "restoring into target database"
pg_restore \
  --dbname="$DATABASE_URL" \
  --clean --if-exists \
  --no-owner \
  --single-transaction \
  "$restore_file"

log "restore completed successfully"
