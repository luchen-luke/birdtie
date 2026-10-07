#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

: "${CIVU_BACKUP_DIR:?Set CIVU_BACKUP_DIR to a new directory on encrypted offsite storage}"
[[ -n "${PGHOST:-}${PGSERVICE:-}${DATABASE_URL:-}" ]] ||
  fail 'Configure PostgreSQL using a protected service file, .pgpass, or environment; never pass credentials as arguments.'
[[ ! -e "$CIVU_BACKUP_DIR" ]] || fail 'Backup target already exists; refusing to overwrite.'

for cmd in psql pg_dump pg_restore df sha256sum; do
  command -v "$cmd" >/dev/null 2>&1 || fail "Required PostgreSQL/verification tool is missing: $cmd"
done

mkdir -m 700 -p -- "$CIVU_BACKUP_DIR"
tmp="$CIVU_BACKUP_DIR/database.dump.partial"
dump="$CIVU_BACKUP_DIR/database.dump"
manifest_tmp="$CIVU_BACKUP_DIR/manifest.json.partial"
manifest="$CIVU_BACKUP_DIR/manifest.json"
trap 'rm -f -- "$tmp" "$manifest_tmp"' EXIT

version=$(psql -X -v ON_ERROR_STOP=1 -Atqc 'SHOW server_version') || fail 'Could not read PostgreSQL server version.'
db_name=$(psql -X -v ON_ERROR_STOP=1 -Atqc 'SELECT current_database()') || fail 'Could not read database name.'
db_bytes=$(psql -X -v ON_ERROR_STOP=1 -Atqc 'SELECT pg_database_size(current_database())') || fail 'Could not estimate database size.'
[[ "$db_bytes" =~ ^[0-9]+$ ]] || fail 'Database size result was invalid.'

available=$(df -Pk -- "$CIVU_BACKUP_DIR" | awk 'NR==2 {print $4 * 1024}')
[[ "$available" =~ ^[0-9]+$ ]] || fail 'Could not read backup target free space.'
required=$((db_bytes + db_bytes / 4 + 1073741824))
(( available >= required )) || fail "Insufficient target space: require estimated DB size + 25% + 1 GiB; no dump written."

printf 'Preparing custom-format backup: database=%s, server_version=%s, estimated_bytes=%s\n' "$db_name" "$version" "$db_bytes"
pg_dump --format=custom --no-owner --no-acl --verbose --file="$tmp" || fail 'pg_dump failed; partial file will be removed.'
[[ -s "$tmp" ]] || fail 'pg_dump output is empty.'
pg_restore --list "$tmp" >/dev/null || fail 'pg_restore could not read dump catalog.'
mv -- "$tmp" "$dump"

dump_sha=$(sha256sum -- "$dump" | awk '{print $1}')
dump_bytes=$(stat -c '%s' -- "$dump")
migrations=$(psql -X -v ON_ERROR_STOP=1 -Atqc "SELECT COALESCE(max(name), 'unknown') FROM civu_schema_migrations") ||
  migrations='ledger-unavailable'

python3 - "$manifest_tmp" "$db_name" "$version" "$db_bytes" "$migrations" "$dump" "$dump_bytes" "$dump_sha" <<'PY'
import datetime, json, os, sys
path, db, version, estimate, migrations, dump, size, digest = sys.argv[1:]
data = {
    "created_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "database_name": db,
    "postgres_server_version": version,
    "estimated_database_bytes": int(estimate),
    "highest_civu_migration": migrations,
    "dump_file": os.path.basename(dump),
    "dump_bytes": int(size),
    "dump_sha256": digest,
    "format": "pg_dump custom",
    "scope_note": "Database only. External media files and OSS objects require separate inventory and encrypted copy.",
}
with open(path, "x", encoding="utf-8") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PY
chmod 600 -- "$dump" "$manifest_tmp"
mv -- "$manifest_tmp" "$manifest"
sha256sum --check <(printf '%s  %s\n' "$dump_sha" "$dump")
sha256sum -- "$manifest" > "$CIVU_BACKUP_DIR/manifest.sha256"
chmod 600 -- "$CIVU_BACKUP_DIR/manifest.sha256"
printf 'Verified local artifacts: %s\n' "$CIVU_BACKUP_DIR/manifest.json"
printf 'NOTE: This only verifies a database dump. It is not an offsite-copy or isolated-restore receipt.\n'
