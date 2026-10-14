"""Stream an explicitly selected Civu tar; never extract or authorize media.

All object selectors and hashes in the returned value are private migration
material. Callers must authenticate the encrypted stream before accepting it.
This module makes no network request and has no database writer.
"""
from collections import Counter
import argparse
import hashlib
import io
import json
from pathlib import Path
import re
import sys
import tarfile
import uuid


class InvalidArchive(ValueError):
    pass


def safe_path(value):
    if (not isinstance(value, str) or not value or len(value.encode('utf-8')) > 1024
            or '\\' in value or '\0' in value or ':' in value or value.startswith('/')
            or any(ord(c) < 32 for c in value)):
        raise InvalidArchive('UNSAFE_PATH')
    parts = value.split('/')
    if any(p in ('', '.', '..') for p in parts):
        raise InvalidArchive('UNSAFE_PATH')
    return value


def identifier(value):
    if not isinstance(value, str):
        raise InvalidArchive('INVALID_SELECTOR')
    try:
        if str(uuid.UUID(value)) != value:
            raise ValueError()
    except ValueError:
        raise InvalidArchive('INVALID_SELECTOR') from None
    return value


def sniff(value):
    # Format evidence only; magic is not a decoder, antivirus scan or EXIF scrub.
    if value.startswith(b'\xff\xd8\xff'):
        return 'image/jpeg'
    if value.startswith(b'\x89PNG\r\n\x1a\n'):
        return 'image/png'
    if value[:4] == b'RIFF' and value[8:12] == b'WEBP':
        return 'image/webp'
    if value[:6] in (b'GIF87a', b'GIF89a'):
        return 'image/gif'
    if value[:4] == b'RIFF' and value[8:12] == b'WAVE':
        return 'audio/wav'
    if value.startswith(b'OggS'):
        return 'application/ogg'
    if value.startswith(b'%PDF-'):
        return 'application/pdf'
    if len(value) >= 12 and value[4:8] == b'ftyp':
        return 'video/quicktime' if value[8:12] == b'qt  ' else 'video/mp4'
    return None


class _BoundedStream:
    def __init__(self, source, limit):
        self.source, self.limit = source, limit
        self.size = 0
        self.sha = hashlib.sha256()

    def read(self, size=-1):
        if size < 0:
            size = 1024 * 1024
        value = self.source.read(min(size, 1024 * 1024))
        self.size += len(value)
        if self.size > self.limit:
            raise InvalidArchive('ARCHIVE_BYTE_LIMIT')
        self.sha.update(value)
        return value


def match_archive(assets, stream, *, key_prefix, max_bytes=4 * 1024**3,
                  max_members=30000, max_object_bytes=2 * 1024**3):
    prefix = safe_path(key_prefix.rstrip('/')) + '/'
    if not isinstance(assets, list) or len(assets) > 100000:
        raise InvalidArchive('ASSET_LIMIT')
    wanted, rows, asset_ids = {}, [], set()
    for asset in assets:
        if not isinstance(asset, dict):
            raise InvalidArchive('INVALID_ASSET')
        aid = identifier(asset.get('id'))
        if aid in asset_ids:
            raise InvalidArchive('DUPLICATE_ASSET')
        asset_ids.add(aid)
        owner = asset.get('owner_id')
        if owner is not None:
            identifier(owner)
        for role, key_name, mime_name, size_name in (
            ('original', 'storage_key', 'content_type', 'size_bytes'),
            ('motion', 'motion_storage_key', 'motion_content_type', 'motion_size_bytes'),
        ):
            key = asset.get(key_name, '')
            if role == 'motion' and not key:
                continue
            row = dict(sourceMediaId=aid, sourceOwnerId=owner, role=role,
                       sourceStatus=asset.get('status'), sourceStorageKey=key,
                       sourceMIME=asset.get(mime_name), sourceBytes=asset.get(size_name),
                       archiveMember=None, bytes=None, sha256=None, observedMIME=None,
                       matched=False, reasons=['RIGHTS_NOT_REVIEWED', 'NOT_SCANNED'])
            rows.append(row)
            if not owner:
                row['reasons'].append('OWNER_MISSING')
            if asset.get('status') != 'ready':
                row['reasons'].append('SOURCE_NOT_READY')
            try:
                path = prefix + safe_path(key)
            except InvalidArchive:
                row['reasons'].append('KEY_MISSING_OR_UNSAFE')
                continue
            wanted.setdefault(path, []).append(row)
    wrapped = _BoundedStream(stream, max_bytes)
    seen, members, files, unmatched = set(), 0, 0, 0
    try:
        with tarfile.open(fileobj=wrapped, mode='r|') as archive:
            for member in archive:
                members += 1
                if members > max_members:
                    raise InvalidArchive('ARCHIVE_MEMBER_LIMIT')
                name = safe_path(member.name.rstrip('/') if member.isdir() else member.name)
                if name in seen:
                    raise InvalidArchive('DUPLICATE_MEMBER')
                seen.add(name)
                if member.isdir():
                    continue
                if not member.isfile() or member.issym() or member.islnk() or member.sparse:
                    raise InvalidArchive('UNSUPPORTED_MEMBER_TYPE')
                if not 0 <= member.size <= max_object_bytes:
                    raise InvalidArchive('OBJECT_BYTE_LIMIT')
                files += 1
                candidates = wanted.get(name, [])
                if not candidates:
                    unmatched += 1
                reader = archive.extractfile(member)
                if reader is None:
                    raise InvalidArchive('UNREADABLE_MEMBER')
                sha, size, header = hashlib.sha256(), 0, b''
                while True:
                    chunk = reader.read(1024 * 1024)
                    if not chunk:
                        break
                    size += len(chunk)
                    sha.update(chunk)
                    if len(header) < 64:
                        header += chunk[:64-len(header)]
                if size != member.size:
                    raise InvalidArchive('TRUNCATED_MEMBER')
                for row in candidates:
                    row.update(archiveMember=name, bytes=size, sha256=sha.hexdigest(),
                               observedMIME=sniff(header))
                    if len(candidates) != 1:
                        row['reasons'].append('DUPLICATE_STORAGE_KEY')
                    if type(row['sourceBytes']) is not int or row['sourceBytes'] <= 0:
                        row['reasons'].append('SOURCE_SIZE_UNKNOWN')
                    elif row['sourceBytes'] != size:
                        row['reasons'].append('SIZE_MISMATCH')
                    if not row['sourceMIME']:
                        row['reasons'].append('SOURCE_MIME_UNKNOWN')
                    elif row['observedMIME'] is None:
                        row['reasons'].append('MAGIC_UNSUPPORTED')
                    elif row['sourceMIME'].lower().strip() != row['observedMIME']:
                        row['reasons'].append('MIME_MISMATCH')
                    row['matched'] = len(candidates) == 1 and not any(
                        r in row['reasons'] for r in ('SIZE_MISMATCH', 'SOURCE_SIZE_UNKNOWN',
                                                     'SOURCE_MIME_UNKNOWN', 'MAGIC_UNSUPPORTED',
                                                     'MIME_MISMATCH'))
        # Consume the authenticated stream's complete bytes, including tar tail.
        while wrapped.read(1024 * 1024):
            pass
    except (tarfile.TarError, EOFError, OSError):
        raise InvalidArchive('INVALID_TAR') from None
    for path, candidates in wanted.items():
        if path not in seen:
            for row in candidates:
                row['reasons'].append('ARCHIVE_OBJECT_MISSING')
    for row in rows:
        row['reasons'] = sorted(set(row['reasons']))
    counts = Counter(r for row in rows for r in row['reasons'])
    return dict(schemaVersion='birdtie-civu-media-match-v1',
                archiveBytes=wrapped.size, archivePlaintextSHA256=wrapped.sha.hexdigest(),
                archiveMembers=members, archiveFiles=files, unreferencedArchiveFiles=unmatched,
                selectors=rows, summary=dict(assets=len(assets), selectors=len(rows),
                    matched=sum(r['matched'] for r in rows), reasons=dict(sorted(counts.items()))),
                streamAuthentication='NOT_VERIFIED', publicationRights='NOT_AUTHORIZED',
                scanner='NOT_RUN', exifCleaning='NOT_RUN', databaseWrites=0, extractedFiles=0)


def load_private_source(path):
    # Reuse the reviewed extractor's private output and exact snapshot contract.
    from extract_civu_hangzhou import (TABLE_SPECS, SOURCE_VERSION, BACKUP_SHA256,
                                      SCHEMA_SHA256, digest, _reparse, _load)
    path = Path(path)
    manifest_path = path.parent / 'manifest.json'
    if (not path.is_absolute() or not path.is_file() or not manifest_path.is_file()
            or any(_reparse(p) for p in (path, manifest_path, *path.parents))):
        raise InvalidArchive('PRIVATE_SOURCE_REQUIRED')
    roots = (Path(__file__).resolve().parents[1], Path(r'D:\BirdTie'), Path(r'D:\Program\Civu'))
    if any(root == path.resolve() or root in path.resolve().parents for root in roots):
        raise InvalidArchive('PRIVATE_SOURCE_REQUIRED')
    if path.stat().st_size > 64 * 1024**2:
        raise InvalidArchive('SOURCE_BYTE_LIMIT')
    raw = path.read_bytes()
    source = _load(raw)
    manifest = _load(manifest_path.read_bytes())
    if (set(source) != {'schemaVersion', 'source', 'capture', 'tables'}
            or source['schemaVersion'] != SOURCE_VERSION
            or manifest.get('sourceFileSha256') != hashlib.sha256(raw).hexdigest()
            or source['source'] != manifest.get('source')
            or set(source['tables']) != set(TABLE_SPECS)
            or source['source'].get('backupCipherSha256') != BACKUP_SHA256
            or source['source'].get('schemaSha256') != SCHEMA_SHA256):
        raise InvalidArchive('SOURCE_RECEIPT_MISMATCH')
    header = dict(source['source'])
    expected = header.pop('snapshotSha256')
    if digest({'source': header, 'tables': source['tables']}) != expected:
        raise InvalidArchive('SOURCE_SNAPSHOT_MISMATCH')
    for name, rows in source['tables'].items():
        if manifest['tableSha256'].get(name) != digest(rows):
            raise InvalidArchive('SOURCE_TABLE_MISMATCH')
    return source, manifest['sourceFileSha256']


def main(argv=None):
    from extract_civu_hangzhou import private_directory, _write_new, canonical, ExtractionError
    parser = argparse.ArgumentParser(description='Private exact tar object matching; no media extraction or authorization.')
    parser.add_argument('--source-private', required=True)
    parser.add_argument('--output-directory', required=True)
    args = parser.parse_args(argv)
    try:
        source, source_hash = load_private_source(args.source_private)
        out = private_directory(args.output_directory)
        result = match_archive(source['tables']['media_assets'], sys.stdin.buffer, key_prefix='media')
        result['source'] = source['source']
        result['sourceFileSha256'] = source_hash
        result['storageKeyPolicy'] = 'exact media/ + original DB storage_key; no basename inference'
        _write_new(out / 'matched.private.json', canonical(result) + b'\n')
        print(json.dumps(dict(status='MATCHED_PRIVATE_UNAUTHENTICATED_STREAM', **result['summary'],
                              archiveMembers=result['archiveMembers'], archiveFiles=result['archiveFiles']), sort_keys=True))
        return 0
    except (InvalidArchive, ExtractionError) as error:
        print(json.dumps(dict(status='REJECTED', reason=str(error)), sort_keys=True))
        return 2
    except (OSError, ValueError, KeyError, TypeError):
        print(json.dumps(dict(status='REJECTED', reason='MATCHING_FAILED'), sort_keys=True))
        return 2


if __name__ == '__main__':
    raise SystemExit(main())
