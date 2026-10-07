#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
: "${CIVU_MEDIA_DIR:?Set CIVU_MEDIA_DIR to the verified local media directory}"
: "${CIVU_MEDIA_MANIFEST:?Set CIVU_MEDIA_MANIFEST to a new protected manifest path outside the source tree}"
[[ -d "$CIVU_MEDIA_DIR" ]] || fail 'Media source is not a directory.'
[[ ! -e "$CIVU_MEDIA_MANIFEST" ]] || fail 'Manifest already exists; refusing to overwrite.'
command -v python3 >/dev/null 2>&1 || fail 'python3 is required.'
command -v sha256sum >/dev/null 2>&1 || fail 'sha256sum is required.'

python3 - "$CIVU_MEDIA_DIR" "$CIVU_MEDIA_MANIFEST" <<'PY'
import datetime, hashlib, json, os, pathlib, sys
root = pathlib.Path(sys.argv[1]).resolve(strict=True)
target = pathlib.Path(sys.argv[2])
if target.exists():
    raise SystemExit("manifest already exists")
rows, total = [], 0
for path in sorted(root.rglob("*")):
    if not path.is_file() or path.is_symlink():
        continue
    resolved = path.resolve(strict=True)
    if root not in resolved.parents:
        raise SystemExit("media path escapes source root")
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            size += len(block)
            digest.update(block)
    total += size
    rows.append({"relative_path": path.relative_to(root).as_posix(), "bytes": size, "sha256": digest.hexdigest()})
data = {"created_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source_root_label": "verified media directory", "file_count": len(rows),
        "total_bytes": total, "files": rows}
fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, "w", encoding="utf-8") as stream:
    json.dump(data, stream, indent=2)
    stream.write("\n")
print(f"Inventory only: files={len(rows)} bytes={total}. No media copied or backed up.")
PY
