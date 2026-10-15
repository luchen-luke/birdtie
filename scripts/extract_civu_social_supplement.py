"""Read-only closed8-table social supplement preservation.

Derived from Birdtie's reviewed prepare_social_supplement163.py capture. Reads
only the isolated backup and compares all original29 tables in the same read-
only snapshot. Starts no service and writes no source/native database rows.
Private exports use operator-only permissions and exclusive new files.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys


REPO = Path(__file__).resolve().parents[1]
FORMAT = 'birdtie-civu-social-source-v1'
MANIFEST_FORMAT = 'birdtie-civu-social-preservation-manifest-v1'
MAX_CAPTURE = 256 * 1024 * 1024
MAX_SOCIAL = 32 * 1024 * 1024
MAX_ROWS = 100000

# Exact audited source schema names. Secret invitation/upload capabilities are
# checked as schema only and excluded from the selected private supplement.
FULL_COLUMNS = {
    'follows': ('follower_id', 'followee_id', 'created_at'),
    'friendships': ('user_low_id', 'user_high_id', 'requester_id', 'status',
                    'created_at', 'updated_at', 'responded_at'),
    'conversations': ('id', 'type', 'user_low_id', 'user_high_id', 'status',
                      'consent_epoch', 'last_seq', 'last_message_id',
                      'last_message_at', 'created_at', 'updated_at', 'title',
                      'join_policy', 'member_limit', 'history_for_new_members',
                      'journey_id', 'owner_id', 'school_label', 'invite_token',
                      'archived_at', 'show_on_profile'),
    'conversation_members': ('conversation_id', 'user_id', 'last_read_seq',
                             'last_delivered_seq', 'unread_count', 'muted_until',
                             'joined_at', 'updated_at', 'pinned', 'hidden',
                             'marked_unread', 'cleared_through_seq', 'role',
                             'removed_at'),
    'messages': ('id', 'conversation_id', 'sender_id', 'seq',
                 'client_message_id', 'payload_hash', 'type', 'text',
                 'entity_type', 'entity_id', 'context_type', 'context_id',
                 'request_id', 'recalled_at', 'moderated_at', 'created_at',
                 'sticker_pack_id', 'sticker_id'),
    'message_requests': ('id', 'conversation_id', 'epoch', 'requester_id',
                         'recipient_id', 'status', 'first_message_id', 'seen_at',
                         'resolved_at', 'retry_after', 'created_at'),
    'message_attachments': ('message_id', 'media_id', 'position'),
    'private_message_media': ('id', 'owner_id', 'conversation_id', 'status',
                              'width', 'height', 'size_bytes', 'upload_lease',
                              'upload_until', 'storage_key', 'created_at',
                              'ready_at', 'kind', 'duration_ms', 'file_name'),
}
EXCLUDED = {'invite_token', 'upload_lease', 'upload_until'}
COLUMNS = {k: tuple(c for c in v if c not in EXCLUDED)
           for k, v in FULL_COLUMNS.items()}
KEYS = {
    'follows': ('follower_id', 'followee_id'),
    'friendships': ('user_low_id', 'user_high_id'),
    'conversations': ('id',),
    'conversation_members': ('conversation_id', 'user_id'),
    'messages': ('id',),
    'message_requests': ('id',),
    'message_attachments': ('message_id', 'position'),
    'private_message_media': ('id',),
}
DEFERRED_TABLES = ('ai_journey_messages', 'follow_requests',
                   'group_join_requests', 'message_reports',
                   'notification_settings', 'notifications', 'user_blocks')


class Rejected(Exception):
    pass


def load_helpers():
    path = REPO / 'scripts' / 'extract_civu_hangzhou.py'
    spec = importlib.util.spec_from_file_location('preservation_helpers', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def read_private(path, maximum):
    if not path.is_absolute() or path.is_symlink() or not path.is_file():
        raise Rejected('ORIGINAL_INPUT_UNAVAILABLE')
    if path.stat().st_size > maximum:
        raise Rejected('ORIGINAL_INPUT_BUDGET')
    return path.read_bytes()


def original_inputs(x, source_path, manifest_path):
    source_raw = read_private(source_path, x.MAX_SOURCE_BYTES)
    manifest_raw = read_private(manifest_path, x.MAX_SOURCE_BYTES)
    source, manifest = x._load(source_raw), x._load(manifest_raw)
    x.closed(source, ('schemaVersion', 'source', 'capture', 'tables'))
    if source['schemaVersion'] != x.SOURCE_VERSION:
        raise Rejected('ORIGINAL_PROTOCOL')
    if manifest.get('schemaVersion') != x.MANIFEST_VERSION:
        raise Rejected('ORIGINAL_PROTOCOL')
    header = source['source']
    if (manifest.get('source') != header or
            manifest.get('sourceFileSha256') != hashlib.sha256(source_raw).hexdigest() or
            header.get('backupCipherSha256') != x.BACKUP_SHA256 or
            header.get('schemaSha256') != x.SCHEMA_SHA256 or
            header.get('instanceTag') != x.INSTANCE_TAG or
            header.get('container') != x.CONTAINER or
            header.get('database') != x.DATABASE or
            set(source['tables']) != set(x.TABLE_SPECS)):
        raise Rejected('ORIGINAL_BINDING')
    base_header = {k: v for k, v in header.items() if k != 'snapshotSha256'}
    if x.digest({'source': base_header, 'tables': source['tables']}) != header.get('snapshotSha256'):
        raise Rejected('ORIGINAL_SNAPSHOT')
    for table, rows in source['tables'].items():
        expected = {c[0] for c in x.TABLE_SPECS[table]['columns']}
        if not isinstance(rows, list) or len(rows) > MAX_ROWS:
            raise Rejected('ORIGINAL_BUDGET')
        for row in rows:
            x.closed(row, expected)
        if (manifest.get('counts', {}).get(table) != len(rows) or
                manifest.get('tableSha256', {}).get(table) != x.digest(rows)):
            raise Rejected('ORIGINAL_TABLE_BINDING')
    return source, hashlib.sha256(source_raw).hexdigest(), hashlib.sha256(manifest_raw).hexdigest()


def social_sql(x):
    # All original closed tables are re-read in the SAME read-only snapshot and
    # compared in memory, before any supplement is retained. No wildcard SELECT.
    prefix, final = x.snapshot_sql().rsplit('COMMIT;', 1)
    if final.strip():
        raise Rejected('INTERNAL_SQL_BOUNDARY')
    names = ','.join("'" + n + "'" for n in sorted(FULL_COLUMNS))
    statements = [prefix,
        "SELECT json_build_object('event','social_schema','columns',json_object_agg(t,cols ORDER BY t)) "
        "FROM(SELECT table_name t,json_agg(column_name ORDER BY ordinal_position) cols "
        "FROM information_schema.columns WHERE table_schema='public' AND table_name IN(" + names + ") GROUP BY table_name)s;"
    ]
    for table, cols in sorted(COLUMNS.items()):
        relation = 'public.' + x._identifier(table)
        statements.append("DO $social_budget$ BEGIN IF (SELECT count(*) FROM " + relation + ")>" + str(MAX_ROWS) + " OR pg_relation_size('" + relation + "'::regclass)>" + str(MAX_SOCIAL) + " THEN RAISE EXCEPTION 'SOCIAL_SOURCE_BUDGET';END IF;END;$social_budget$;")
        selected = ','.join(x._identifier(c) for c in cols)
        ordering = ','.join('r.' + x._identifier(c) for c in KEYS[table])
        statements.append("SELECT json_build_object('event','social_table','table','" + table + "','count',count(*),'rows',coalesce(json_agg(row_to_json(r) ORDER BY " + ordering + "),'[]'::json)) FROM(SELECT " + selected + ' FROM ' + relation + ')r;')
    fields = []
    for table in DEFERRED_TABLES:
        fields.extend(["'" + table + "'", '(SELECT count(*) FROM public.' + x._identifier(table) + ')'])
    statements.append("SELECT json_build_object('event','social_deferred_counts','counts',json_build_object(" + ','.join(fields) + '));')
    statements.append('COMMIT;')
    return ('\n'.join(statements) + '\n').encode('utf-8')


def read_restore(x, original):
    inspected = x._run(['docker', 'inspect', '--format',
        '{{json .HostConfig.NetworkMode}}|{{json .NetworkSettings.Ports}}|{{.State.Running}}',
        x.CONTAINER], timeout=15)
    if inspected.returncode != 0:
        raise Rejected('RESTORE_UNAVAILABLE')
    try:
        network, ports, running = inspected.stdout.decode('utf-8').strip().split('|')
        isolated = json.loads(network) == 'none' and json.loads(ports) in ({}, None) and running == 'true'
    except (ValueError, UnicodeError):
        isolated = False
    if not isolated:
        raise Rejected('RESTORE_NOT_ISOLATED')
    result = x._run(['docker', 'exec', '-i', x.CONTAINER, 'psql', '-X', '-U',
        'postgres', '-d', x.DATABASE, '-q', '-A', '-t', '-v',
        'ON_ERROR_STOP=1', '--pset=pager=off'], input=social_sql(x), timeout=120)
    if result.returncode != 0 or len(result.stdout) > MAX_CAPTURE:
        # Never print database stderr: it can contain private row values.
        raise Rejected('SOCIAL_SOURCE_READ_REJECTED')
    originals, tables, columns, deferred = [], {}, None, None
    for line in result.stdout.splitlines():
        event = x._load(line)
        kind = event.get('event')
        if kind in ('capture', 'table'):
            originals.append(line)
        elif kind == 'social_schema' and columns is None:
            x.closed(event, ('event', 'columns'))
            columns = event['columns']
        elif kind == 'social_deferred_counts' and deferred is None:
            x.closed(event, ('event', 'counts'))
            deferred = event['counts']
        elif kind == 'social_table':
            x.closed(event, ('event', 'table', 'count', 'rows'))
            name, rows = event['table'], event['rows']
            if (name not in COLUMNS or name in tables or not isinstance(rows, list) or
                    type(event['count']) is not int or event['count'] != len(rows) or len(rows) > MAX_ROWS):
                raise Rejected('SOCIAL_SOURCE_PROTOCOL')
            seen = set()
            for row in rows:
                x.closed(row, COLUMNS[name])
                key = x.canonical([row[k] for k in KEYS[name]])
                if any(row[k] is None for k in KEYS[name]) or key in seen:
                    raise Rejected('SOCIAL_SOURCE_PRIMARY_KEY')
                seen.add(key)
            tables[name] = rows
        else:
            raise Rejected('SOCIAL_SOURCE_PROTOCOL')
    capture, reread = x.parse_capture(b'\n'.join(originals) + b'\n')
    if x.digest(reread) != x.digest(original['tables']):
        raise Rejected('ORIGINAL_RESTORE_ROWS_CHANGED')
    if (columns != {k: list(v) for k, v in FULL_COLUMNS.items()} or
            set(tables) != set(COLUMNS) or not isinstance(deferred, dict) or
            set(deferred) != set(DEFERRED_TABLES) or
            any(type(v) is not int or v < 0 for v in deferred.values())):
        raise Rejected('SOCIAL_SOURCE_SCHEMA_CHANGED')
    return capture, tables, deferred, columns


def main():
    # argparse errors expose only option names; no positional credentials exist.
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--source-backup-sha256', required=True)
    parser.add_argument('--original-source', required=True, type=Path)
    parser.add_argument('--original-manifest', required=True, type=Path)
    parser.add_argument('--output-directory', required=True)
    args = parser.parse_args()
    try:
        x = load_helpers()
        if not re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', args.run_id):
            raise Rejected('RUN_ID_INVALID')
        if args.source_backup_sha256 != x.BACKUP_SHA256:
            raise Rejected('BACKUP_IDENTITY_DENIED')
        original, source_sha, manifest_sha = original_inputs(x, args.original_source, args.original_manifest)
        capture, tables, deferred, schema = read_restore(x, original)
        binding = {'sourceSystem': x.INSTANCE_TAG, 'runId': args.run_id,
            'backupCipherSha256': x.BACKUP_SHA256,
            'originalSourceFileSha256': source_sha,
            'originalManifestFileSha256': manifest_sha,
            'originalSnapshotSha256': original['source']['snapshotSha256'],
            'socialSchemaSha256': x.digest(schema)}
        binding['snapshotSha256'] = x.digest({'source': binding, 'tables': tables, 'deferredCounts': deferred})
        doc = {'schemaVersion': FORMAT, 'source': binding, 'capture': capture,
            'tables': tables, 'deferredCounts': deferred}
        raw = x.canonical(doc) + b'\n'
        if len(raw) > MAX_SOCIAL:
            raise Rejected('SOCIAL_OUTPUT_BUDGET')
        manifest = {'schemaVersion': MANIFEST_FORMAT, 'source': binding,
            'sourceFileSha256': hashlib.sha256(raw).hexdigest(),
            'counts': {k: len(v) for k, v in sorted(tables.items())},
            'tableSha256': {k: x.digest(v) for k, v in sorted(tables.items())},
            'deferredCounts': deferred, 'excludedCapabilityFields': sorted(EXCLUDED),
            'execution': {'preservation': 'EXTRACTED_ONLY', 'sourceWrites': 0,
                'nativeWrites': 0, 'notificationEffects': 0, 'sessionMigration': 'NOT_RUN',
                'mediaBytesMatched': 'NOT_RUN', 'relationshipEffects': 0,
                'original29TablesRereadEqual': True, 'runtimeStarted': False}}
        directory = x.private_directory(args.output_directory)
        x._write_new(directory / 'social-source.private.json', raw)
        x._write_new(directory / 'social-manifest.private.json', x.canonical(manifest) + b'\n')
        print(json.dumps({'status': 'SOCIAL_PRESERVED_ONLY', 'tables': len(tables),
            'counts': manifest['counts'], 'deferredCounts': deferred,
            'sourceWrites': 0, 'nativeWrites': 0, 'relationshipEffects': 0,
            'notificationEffects': 0, 'original29TablesRereadEqual': True}, sort_keys=True))
        return 0
    except Exception:
        # Exception strings, source rows, paths and provider values stay private.
        print(json.dumps({'status': 'REJECTED', 'reason': 'SOCIAL_PRESERVATION_FAILED'}))
        return 2


if __name__ == '__main__':
    sys.exit(main())
