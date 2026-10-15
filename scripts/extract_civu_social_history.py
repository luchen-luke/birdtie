"""Independent four-table protected history capture.

Reuses the reviewed 163 fixed-source reader; reads
original29 + sealed social8 + new history4 in ONE read-only snapshot. It changes
neither old source/manifest nor sealed social files, starts no process/provider
other than the fixed docker exec read, and exposes no rows in diagnostics.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys


FORMAT = 'birdtie-civu-social-history-source-v1'
MANIFEST_FORMAT = 'birdtie-civu-social-history-preservation-manifest-v1'
HISTORY_COLUMNS = {
    'ai_journey_threads': ('id', 'owner_id', 'journey_id', 'draft_context_id',
        'summary', 'status', 'context_revision', 'created_at', 'updated_at'),
    'ai_journey_messages': ('id', 'thread_id', 'role', 'content', 'created_at',
        'request_id'),
    'notifications': ('id', 'recipient_id', 'actor_id', 'type', 'aggregate_type',
        'aggregate_id', 'event_id', 'payload', 'read_at', 'created_at',
        'event_type', 'category', 'anchor_type', 'anchor_id', 'seen_at',
        'payload_schema_version', 'inbox_version', 'dedupe_key', 'deleted_at'),
    'notification_settings': ('user_id', 'settings', 'version', 'updated_at'),
}
HISTORY_KEYS = {'ai_journey_threads': ('id',),
    'ai_journey_messages': ('id',), 'notifications': ('id',),
    'notification_settings': ('user_id',)}


def load_reader():
    path = Path(__file__).resolve().with_name('extract_civu_social_supplement.py')
    spec = importlib.util.spec_from_file_location('social_source_reader', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--source-backup-sha256', required=True)
    parser.add_argument('--original-source', required=True, type=Path)
    parser.add_argument('--original-manifest', required=True, type=Path)
    parser.add_argument('--social-source', required=True, type=Path)
    parser.add_argument('--social-manifest', required=True, type=Path)
    parser.add_argument('--output-directory', required=True)
    args = parser.parse_args()
    try:
        reader = load_reader()
        x = reader.load_helpers()
        if (not re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', args.run_id) or
                args.source_backup_sha256 != x.BACKUP_SHA256):
            raise reader.Rejected('SOURCE_IDENTITY_DENIED')
        original, original_sha, original_manifest_sha = reader.original_inputs(
            x, args.original_source, args.original_manifest)
        social_raw = reader.read_private(args.social_source, reader.MAX_SOCIAL)
        social_manifest_raw = reader.read_private(args.social_manifest, reader.MAX_SOCIAL)
        social = x._load(social_raw)
        social_manifest = x._load(social_manifest_raw)
        x.closed(social, ('schemaVersion', 'source', 'capture', 'tables', 'deferredCounts'))
        social_source_sha = hashlib.sha256(social_raw).hexdigest()
        social_manifest_sha = hashlib.sha256(social_manifest_raw).hexdigest()
        binding = social['source']
        if (social['schemaVersion'] != reader.FORMAT or
                social_manifest.get('schemaVersion') != reader.MANIFEST_FORMAT or
                social_manifest.get('source') != binding or
                social_manifest.get('sourceFileSha256') != social_source_sha or
                binding.get('runId') != args.run_id or
                binding.get('sourceSystem') != x.INSTANCE_TAG or
                binding.get('backupCipherSha256') != x.BACKUP_SHA256 or
                binding.get('originalSourceFileSha256') != original_sha or
                binding.get('originalManifestFileSha256') != original_manifest_sha or
                binding.get('originalSnapshotSha256') != original['source']['snapshotSha256'] or
                set(social['tables']) != set(reader.COLUMNS)):
            raise reader.Rejected('SEALED_SOCIAL_BINDING')
        original_binding = {k: v for k, v in binding.items() if k != 'snapshotSha256'}
        if x.digest({'source': original_binding, 'tables': social['tables'],
                     'deferredCounts': social['deferredCounts']}) != binding.get('snapshotSha256'):
            raise reader.Rejected('SEALED_SOCIAL_SNAPSHOT')
        for table, rows in social['tables'].items():
            if (social_manifest.get('counts', {}).get(table) != len(rows) or
                    social_manifest.get('tableSha256', {}).get(table) != x.digest(rows)):
                raise reader.Rejected('SEALED_SOCIAL_TABLE')

        # Expand only explicit audited keys, never caller-supplied table/column
        # names. The reader compares all29 original tables before returning.
        social_names = tuple(sorted(reader.COLUMNS))
        reader.FULL_COLUMNS = {**reader.FULL_COLUMNS, **HISTORY_COLUMNS}
        reader.COLUMNS = {**reader.COLUMNS, **HISTORY_COLUMNS}
        reader.KEYS = {**reader.KEYS, **HISTORY_KEYS}
        reader.DEFERRED_TABLES = ()
        capture, reread, _, schema = reader.read_restore(x, original)
        if any(x.digest(reread[name]) != x.digest(social['tables'][name])
               for name in social_names):
            raise reader.Rejected('SEALED_SOCIAL_ROWS_CHANGED')
        history = {k: reread[k] for k in sorted(HISTORY_COLUMNS)}
        history_schema = {k: schema[k] for k in sorted(HISTORY_COLUMNS)}
        header = {'sourceSystem': x.INSTANCE_TAG, 'runId': args.run_id,
            'backupCipherSha256': x.BACKUP_SHA256,
            'originalSourceFileSha256': original_sha,
            'originalManifestFileSha256': original_manifest_sha,
            'originalSnapshotSha256': original['source']['snapshotSha256'],
            'socialSourceFileSha256': social_source_sha,
            'socialManifestFileSha256': social_manifest_sha,
            'socialSnapshotSha256': binding['snapshotSha256'],
            'historySchemaSha256': x.digest(history_schema)}
        header['snapshotSha256'] = x.digest({'source': header, 'tables': history})
        doc = {'schemaVersion': FORMAT, 'source': header, 'capture': capture,
            'tables': history}
        raw = x.canonical(doc) + b'\n'
        if len(raw) > reader.MAX_SOCIAL:
            raise reader.Rejected('HISTORY_OUTPUT_BUDGET')
        counts = {k: len(v) for k, v in history.items()}
        manifest = {'schemaVersion': MANIFEST_FORMAT, 'source': header,
            'sourceFileSha256': hashlib.sha256(raw).hexdigest(), 'counts': counts,
            'tableSha256': {k: x.digest(v) for k, v in history.items()},
            'execution': {'preservation': 'EXTRACTED_ONLY',
                'sourceWrites': 0, 'nativeWrites': 0, 'notificationEffects': 0,
                'modelCalls': 0, 'pushEffects': 0, 'runtimeStarted': False,
                'original29TablesRereadEqual': True,
                'sealedSocial8TablesRereadEqual': True}}
        directory = x.private_directory(args.output_directory)
        x._write_new(directory / 'history-source.private.json', raw)
        x._write_new(directory / 'history-manifest.private.json', x.canonical(manifest) + b'\n')
        print(json.dumps({'status': 'SOCIAL_HISTORY_PRESERVED_ONLY',
            'tables': 4, 'counts': counts, 'sourceWrites': 0,
            'nativeWrites': 0, 'notificationEffects': 0, 'modelCalls': 0,
            'pushEffects': 0, 'original29TablesRereadEqual': True,
            'sealedSocial8TablesRereadEqual': True}, sort_keys=True))
        return 0
    except Exception:
        print(json.dumps({'status': 'REJECTED', 'reason': 'HISTORY_PRESERVATION_FAILED'}))
        return 2


if __name__ == '__main__':
    sys.exit(main())
