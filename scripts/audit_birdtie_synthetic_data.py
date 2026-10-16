"""DGS exact-receipt audit. Default: read-only; never authenticates to the API.

Only the reviewed 2026-10-15 remote fixtures are roots. Account membership,
display names, dates, and text prefixes are never deletion selectors.
"""
from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys

PROTOCOL = 'BIRDTIE_DGS_EXACT_CLEANUP_V1'
DB = 'birdtie_native_hz_20261015'
OID = 54087
RELEASE = '/opt/birdtie/releases/social173-20261016-build30'
BINARY_SHA = '4cc70262b66cffa0061a79fa91fdc7f622de23aedd5552a713b5e1070a7b9900'
SOURCE_SHA = 'ba6b9b5d2d404ada806a858cbc0931628a5958c4b2abdbd8b7d50b62b4e6f40d'
RECEIPTS = {
 'remote-friend-chat-flow01': '50f1be60ec36aedf4ee03a30de3b523541d4bd41050f16a591b3841f12c5dfc7',
 'remote-friend-chat-flow02': '3e6a62d3144ccbbc38cccdf717d9b45aa2cc42e8e199e770e41fed6a1bb43554',
 'remote-media-thread-flow01': '5d6584837974f1263581b6dd3e0a79657b41d0300b4221405b1b326248c29cff',
 'remote-crosscity166-01': '41e8c001e6db017550a816c970afdbb1a12dd0ad383b7f31c5c154ff7472580f',
 'remote-multilocation166-01': '93228775c53b6278fd08abbff17acc8044583e6f7d1c56b6f49f9cb1b1427451',
}
ROOTS = {
 'moments': [
  'b8b27221-24dd-40ce-8fc3-58868c332df5', 'c2a5e3ea-afbd-408f-b158-ea426c7da9e7',
  'b14d84e8-f7e7-48a3-8acb-c425c744e98a', 'c90cf108-a9fe-4596-bb1e-b4a8204b76fe',
  'e8062b7d-b6f9-47eb-9f53-6f88548e8c56', '964cc4c6-a188-4936-9dca-63d9cdb8d4d6',
  '4323975d-8dca-4112-b81c-7bc9f7a9179d', '47ce7ce5-dc4a-44c5-b58f-32d63d1518e1'],
 'conversation_messages': ['6067adb5-e55b-44ec-b4d1-b5f15257ff85', 'bb226b82-1385-42ae-8bfb-70de2c89b5bc', 'd94008f8-de79-4de9-b032-0de001877335'],
 'conversations': ['0261299f-845d-4381-971c-76683a2c86c3'],
 'connection_requests': ['8676a012-9e61-4d1e-b96f-4098303e8b12', '97688296-58c0-4008-9f27-656001c8d596'],
}
ACTORS = ['00429dde-9620-413f-b816-2b7312e348dc', '43d4a612-fd94-4c5c-9f5f-bf4734877d53',
 '5022d283-0b94-4348-ab53-e1df75024fca', 'ce5ffa90-a75c-4047-b87c-1061a3097852',
 'ebf5db2b-b05f-444b-ac84-32f1e43bf38a', 'fac2c5b5-ed2c-499a-8030-d26861db080a']
ALLOWED_TABLES = set(ROOTS) | set('''moment_threads moment_thread_members moment_thread_operations
 moment_thread_edit_states moment_thread_entry_contexts moment_thread_route_legs
 moment_thread_display_members moment_location_links moment_location_epochs
 moment_location_media_rebindings moment_location_compatibility_backfill
 public_moment_media_sets public_moment_media_attachments public_moment_media_video_payloads
 public_moment_media_operations public_moment_photo_operations public_moment_photos
 human_moment_control_operations moment_interaction_operations moment_comments moment_likes
 moment_activity_links moment_community_links moment_journey_links moment_organization_links
 private_moment_images private_moment_videos private_moment_media_contexts
 inbox_items person_ties person_close_connections conversation_member_states
 human_entity_share_operations native_notification_decisions connection_request_policy_bindings
 connection_request_decision_receipts human_conversation_audio human_conversation_images'''.split())
REFERENCE_COLUMNS = {'moment_id', 'root_moment_id', 'from_moment_id', 'to_moment_id',
 'thread_id', 'conversation_id', 'target_conversation_id', 'message_id',
 'last_read_message_id', 'request_id', 'connection_request_id', 'entity_id',
 'resource_id', 'target_id', 'subject_id', 'source_entity_id', 'destination_id'}
IMMUTABLE_AUDIT_TABLES = {'audit_events', 'agent_run_audit', 'admin_audit_events',
 'business_console_audit_events', 'business_public_profile_audit', 'model_budget_audit',
 'organization_announcement_audit', 'organization_map_location_audit'}
PAUSED_DELETE_GUARDS = {
 'connection_request_decision_receipts': 'connection_decision_receipt_guard',
 'person_close_connections': 'close_connection_guard',
 'human_entity_share_operations': 'entity_share_operation_guard',
 'human_moment_control_operations': 'human_moment_control_immutable',
 'public_moment_media_operations': 'public_moment_media_receipts_immutable',
 'moment_thread_members': 'moment_thread_members_immutable',
 'moment_threads': 'moment_threads_guard',
 'moment_thread_operations': 'moment_thread_operations_immutable',
 'public_moment_media_video_payloads': 'public_video_payload_guard',
 'moment_location_compatibility_backfill': 'moment_location_backfill_immutable',
 'moment_location_epochs': 'moment_location_epoch_guard',
 'moment_location_links': 'moment_location_identity',
}
GLOBAL_PROTECTION = '''accounts user_profiles media_assets legacy_entity_mappings
 legacy_native_adaptation_entities legacy_native_context_entities legacy_reference_media_relations
 legacy_private_media_import_items legacy_private_video_import_items legacy_private_image_recovery_items
 legacy_social_import_entities legacy_social_history_facts legacy_map_anchor_facts legacy_moment_author_facts'''.split()


def require(ok, code):
    if not ok:
        raise RuntimeError(code)


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()).hexdigest()


def identifier(value):
    require(isinstance(value, str) and re.fullmatch('[a-z_][a-z_0-9]*', value), 'UNSAFE_IDENTIFIER')
    return '"' + value + '"'


def literal(value):
    if value is None:
        return 'NULL'
    return "'" + str(value).replace("'", "''") + "'"


def ids(values):
    require(all(re.fullmatch('[0-9a-f-]{36}', x) for x in values), 'INVALID_EXACT_ID')
    return ','.join(literal(x) for x in sorted(values)) or 'NULL'


def row_predicate(pk, records, alias='t'):
    if not records:
        return 'false'
    return '(' + ' OR '.join('(' + ' AND '.join(
        f'{alias}.{identifier(c)} IS NOT DISTINCT FROM {literal(v)}' for c, v in zip(pk, row['key'])) + ')'
        for row in records) + ')'


def deletion_order(tables, fks):
    """Delete referencing tables first. Cycles fail closed rather than disabling FKs."""
    remaining = set(tables)
    order = []
    while remaining:
        ready = sorted(t for t in remaining if not any(
            f['target'] == t and f['table'] in remaining and f['table'] != t for f in fks))
        require(bool(ready), 'UNSUPPORTED_DEPENDENCY_CYCLE')
        order.extend(ready)
        remaining.difference_update(ready)
    return order


def validate_manifest(manifest):
    require(manifest.get('protocol') == PROTOCOL, 'MANIFEST_PROTOCOL')
    require(manifest.get('roots') == ROOTS, 'ROOT_SCOPE_CHANGED')
    require(manifest.get('actors') == ACTORS, 'ACTOR_SCOPE_CHANGED')
    require(manifest.get('databaseOID') == OID and manifest.get('release') == RELEASE and manifest.get('binarySHA256') == BINARY_SHA and manifest.get('sourceSHA256') == SOURCE_SHA, 'ENVIRONMENT_SCOPE_CHANGED')
    require(not manifest.get('blockers'), 'UNREVIEWED_DEPENDENCY')
    require(all(t in ALLOWED_TABLES for t in manifest['tables']), 'UNREVIEWED_TABLE')
    expected = digest({k: v for k, v in manifest.items() if k not in {'manifestSHA256', 'observedAt'}})
    require(manifest.get('manifestSHA256') == expected, 'MANIFEST_DIGEST_MISMATCH')


def command(args, timeout=30, **kwargs):
    result = subprocess.run(args, capture_output=True, timeout=timeout, **kwargs)
    if result.returncode:
        # Export only our uppercase SQL assertion codes, never SQL context/rows.
        error = result.stderr.decode(errors='replace') if isinstance(result.stderr, bytes) else result.stderr
        own_code = re.search(r'ERROR:\s+([A-Z][A-Z0-9_]{3,})(?:\s|$)', error or '')
        raise RuntimeError('COMMAND_FAILED_' + Path(args[0]).name + (':' + own_code[1] if own_code else ''))
    return result.stdout


def query(sql, database=DB):
    text = command(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-A', '-t', '-q', '-v', 'ON_ERROR_STOP=1', '-d', database],
        input=('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout=60000;\n' + sql + ';\nROLLBACK;\n').encode(), timeout=75)
    return json.loads(text)


def environment_guard():
    settings = json.loads(Path('/etc/birdtie/deployment.json').read_text())
    require(settings.get('database') == DB and settings.get('oid') == OID and settings.get('owner') == 'birdtie_native_hz', 'DEPLOYMENT_GUARD')
    live = query("SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(datdba),'marker',shobj_description(oid,'pg_database'),'schemaMax',(SELECT max(version)FROM birdtie_schema_migrations),'schemaCount',(SELECT count(*)FROM birdtie_schema_migrations),'readOnly',current_setting('transaction_read_only'))FROM pg_database WHERE datname=current_database()")
    require(live['oid'] == OID and live['owner'] == settings['owner'] and live['marker'] == settings['marker'] and live['schemaMax'] == live['schemaCount'] == 173 and live['readOnly'] == 'on', 'NATIVE_DATABASE_GUARD')
    state = dict(x.split('=', 1) for x in command(['systemctl', 'show', 'birdtie-api.service', '-p', 'MainPID', '-p', 'ActiveState', '-p', 'SubState']).decode().splitlines())
    require(state['ActiveState'] == 'active' and state['SubState'] == 'running', 'API_UNIT_GUARD')
    executable = Path('/proc') / state['MainPID'] / 'exe'
    require(os.readlink(executable) == RELEASE + '/bin/birdtie-api' and hashlib.sha256(executable.read_bytes()).hexdigest() == BINARY_SHA, 'ACTIVE_BINARY_GUARD')
    source = Path('/srv/birdtie-data/migration/source.json')
    manifest = json.loads(source.with_name('manifest.json').read_text())
    require(source.stat().st_mode & 0o777 == 0o600 and hashlib.sha256(source.read_bytes()).hexdigest() == SOURCE_SHA == manifest['sourceFileSha256'], 'SOURCE_BACKUP_GUARD')


def schema(database=DB):
    return query("""SELECT jsonb_build_object(
 'columns',(SELECT jsonb_object_agg(table_name,columns) FROM(SELECT c.table_name,jsonb_agg(c.column_name ORDER BY c.ordinal_position) columns FROM information_schema.columns c JOIN information_schema.tables t ON t.table_schema=c.table_schema AND t.table_name=c.table_name AND t.table_type='BASE TABLE' WHERE c.table_schema='public' GROUP BY c.table_name)x),
 'primaryKeys',(SELECT jsonb_object_agg(t.relname,(SELECT jsonb_agg(a.attname ORDER BY k.ord)FROM unnest(c.conkey)WITH ORDINALITY k(attnum,ord)JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum))FROM pg_constraint c JOIN pg_class t ON t.oid=c.conrelid WHERE c.contype='p'AND c.connamespace='public'::regnamespace),
 'foreignKeys',(SELECT jsonb_agg(jsonb_build_object('table',t.relname,'target',p.relname,'columns',(SELECT jsonb_agg(a.attname ORDER BY k.ord)FROM unnest(c.conkey)WITH ORDINALITY k(attnum,ord)JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=k.attnum),'references',(SELECT jsonb_agg(a.attname ORDER BY k.ord)FROM unnest(c.confkey)WITH ORDINALITY k(attnum,ord)JOIN pg_attribute a ON a.attrelid=p.oid AND a.attnum=k.attnum)))FROM pg_constraint c JOIN pg_class t ON t.oid=c.conrelid JOIN pg_class p ON p.oid=c.confrelid WHERE c.contype='f'AND c.connamespace='public'::regnamespace))""", database)


def rows_sql(table, pk, predicate, columns):
    keys = ','.join('t.' + identifier(c) for c in pk)
    actor_cols = [c for c in columns if c.endswith('_account_id')]
    actors = ','.join('t.' + identifier(c) for c in actor_cols)
    legacy = '(t.legacy_source_id IS NOT NULL)' if 'legacy_source_id' in columns else 'false'
    return f"SELECT coalesce(jsonb_agg(jsonb_build_object('key',jsonb_build_array({keys}),'sha256',encode(sha256(convert_to(to_jsonb(t)::text,'UTF8')),'hex'),'actors',jsonb_build_array({actors}),'legacy',{legacy}) ORDER BY {keys}),'[]')FROM {identifier(table)} t WHERE {predicate}"


def table_protected_sql(table, pk, records):
    # Full rows stay inside PostgreSQL; only a digest and count leave the host.
    return f"SELECT jsonb_build_object('count',count(*),'sha256',encode(sha256(convert_to(coalesce(string_agg(encode(sha256(convert_to(to_jsonb(t)::text,'UTF8')),'hex'),'' ORDER BY "+','.join('t.' + identifier(c) for c in pk)+f"),''),'UTF8')),'hex'))FROM {identifier(table)} t WHERE NOT {row_predicate(pk, records)}"


def triggers_sql(tables):
    return "SELECT coalesce(jsonb_agg(jsonb_build_object('table',c.relname,'trigger',t.tgname,'enabled',t.tgenabled,'sha256',encode(sha256(convert_to(pg_get_triggerdef(t.oid)||pg_get_functiondef(t.tgfoid),'UTF8')),'hex'))ORDER BY c.relname,t.tgname),'[]')FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE NOT t.tgisinternal AND(t.tgtype&8)>0 AND c.relname IN(" + ','.join(literal(t) for t in sorted(tables)) + ")AND c.relnamespace='public'::regnamespace"


def reference_predicates(meta, selected):
    """Compile references from identifier-only catalog data; manifests contain no SQL."""
    candidates = {}
    known_ids = set(x for records in selected.values() for r in records for x in r['key'] if isinstance(x, str) and re.fullmatch('[0-9a-f-]{36}', x)) - set(ACTORS)
    for table, columns in meta['columns'].items():
        if table in IMMUTABLE_AUDIT_TABLES:
            continue
        predicates = ['t.' + identifier(c) + '::text IN(' + ids(known_ids) + ')' for c in sorted(REFERENCE_COLUMNS.intersection(columns))]
        for fk in meta['foreignKeys']:
            if fk['table'] == table and selected.get(fk['target']):
                join = ' AND '.join('t.' + identifier(a) + '=p.' + identifier(b) for a, b in zip(fk['columns'], fk['references']))
                predicates.append('EXISTS(SELECT 1 FROM ' + identifier(fk['target']) + ' p WHERE ' + join + ' AND ' + row_predicate(meta['primaryKeys'][fk['target']], selected[fk['target']], 'p') + ')')
        if predicates:
            candidates[table] = '(' + ' OR '.join(predicates) + ')'
    return candidates


def build_manifest(proofs, database=DB):
    meta = schema(database)
    meta['foreignKeys'].sort(key=lambda f: (f['table'], f['target'], f['columns']))
    selected = {}
    blockers = []
    for table, values in ROOTS.items():
        selected[table] = query(rows_sql(table, meta['primaryKeys'][table], 't.id IN(' + ids(values) + ')', meta['columns'][table]), database)
        require(len(selected[table]) == len(values), 'EXACT_ROOT_MISSING_' + table)
    actors = query("SELECT count(DISTINCT a.id)FROM accounts a JOIN account_auth_identities i ON i.account_id=a.id WHERE a.id IN(" + ids(ACTORS) + ")AND i.issuer='urn:birdtie:local-dev-phone'AND NOT EXISTS(SELECT 1 FROM legacy_entity_mappings l WHERE l.entity_kind='user'AND l.target_id=a.id)", database)
    require(actors == len(ACTORS), 'SYNTHETIC_ACTOR_PROVENANCE_CHANGED')
    require(query('SELECT count(*)FROM moments WHERE id IN(' + ids(ROOTS['moments']) + ")AND status='withdrawn'AND visibility='private'", database) == 8, 'MOMENT_STATE_CHANGED')
    for _ in range(12):
        candidates = reference_predicates(meta, selected)
        # Query only tables with matches; do not read content from unrelated tables.
        counts = query('SELECT jsonb_object_agg(label,n)FROM(' + ' UNION ALL '.join("SELECT " + literal(t) + " label,count(*) n FROM " + identifier(t) + ' t WHERE ' + p for t, p in candidates.items()) + ')x', database)
        changed = False
        for table, count in counts.items():
            if not count:
                continue
            if table not in ALLOWED_TABLES or table not in meta['primaryKeys']:
                blockers.append({'table': table, 'count': count, 'reason': 'UNREVIEWED_REFERENCE'})
                continue
            found = query(rows_sql(table, meta['primaryKeys'][table], candidates[table], meta['columns'][table]), database)
            before = {tuple(r['key']): r for r in selected.get(table, [])}
            for row in found:
                if row['legacy'] or any(x is not None and x not in ACTORS for x in row['actors']):
                    blockers.append({'table': table, 'count': 1, 'reason': 'REAL_OR_UNKNOWN_ACTOR_REFERENCE'})
                if tuple(row['key']) not in before:
                    changed = True
                before[tuple(row['key'])] = row
            selected[table] = sorted(before.values(), key=lambda r: json.dumps(r['key']))
        if not changed:
            break
    else:
        raise RuntimeError('DEPENDENCY_CLOSURE_LIMIT')
    for table, records in selected.items():
        for row in records:
            require(not row['legacy'] and all(x is None or x in ACTORS for x in row['actors']), 'NON_SYNTHETIC_ROW_' + table)
    selected = {t: r for t, r in sorted(selected.items()) if r}
    triggers = query(triggers_sql(selected), database)
    for trigger in triggers:
        permitted = PAUSED_DELETE_GUARDS.get(trigger['table']) == trigger['trigger'] or (trigger['table'], trigger['trigger']) == ('moments', 'memory_moment_invalidated')
        require(permitted and trigger['enabled'] == 'O', 'UNREVIEWED_DELETE_TRIGGER')
    plan = {'protocol': PROTOCOL, 'status': 'DRY_RUN', 'observedAt': dt.datetime.now(dt.timezone.utc).isoformat(),
        'databaseOID': OID, 'release': RELEASE, 'binarySHA256': BINARY_SHA, 'sourceSHA256': SOURCE_SHA,
        'roots': ROOTS, 'actors': ACTORS, 'proofs': proofs,
        'blockers': sorted({json.dumps(x, sort_keys=True) for x in blockers}),
        'tables': {t: {'primaryKey': meta['primaryKeys'][t], 'rows': rows,
            'protectedRows': query(table_protected_sql(t, meta['primaryKeys'][t], rows), database)} for t, rows in selected.items()},
        'deletionOrder': deletion_order(selected, meta['foreignKeys']),
        'referenceChecks': {t: {'count': counts[t]} for t in sorted(candidates)},
        'referenceSchema': {'columns': {t: sorted(REFERENCE_COLUMNS.intersection(meta['columns'][t])) for t in candidates},
            'primaryKeys': {t: meta['primaryKeys'][t] for t in selected},
            'foreignKeys': [f for f in meta['foreignKeys'] if f['table'] in candidates and f['target'] in selected]},
        'deleteTriggers': triggers,
        'protectedGlobal': {t: {'primaryKey': meta['primaryKeys'][t], 'rows': query(table_protected_sql(t, meta['primaryKeys'][t], []), database)} for t in GLOBAL_PROTECTION},
        'retainedUnknownMoments': 19, 'accountsDeleted': 0, 'apiLogins': 0, 'productionWrites': 0,
        'mediaRule': 'Only exact FK/semantic dependencies; any real/unknown actor or unreviewed table blocks apply; no filesystem or bucket deletion.',
        'fullNativeBackup': 'NOT_RUN', 'restoreVerification': 'NOT_RUN'}
    plan['manifestSHA256'] = digest({k: v for k, v in plan.items() if k != 'observedAt'})
    return plan


def local_proofs(root):
    result = []
    for name, expected in RECEIPTS.items():
        relative = 'docs/testing/evidence/github-remote-completion-20261015/' + name + '/receipt.json'
        raw = (root / relative).read_bytes()
        require(hashlib.sha256(raw).hexdigest() == expected, 'SOURCE_RECEIPT_CHANGED_' + name)
        result.append({'path': relative, 'sha256': expected})
    return result


def run_remote(payload):
    root = Path(__file__).resolve().parents[1]
    spec = importlib.util.spec_from_file_location('dgs_ssh_transport', root / 'scripts/deploy_birdtie_hangzhou.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    data = base64.b64encode(json.dumps(payload).encode()).decode()
    source = 'import sys\nsys.argv=["dgs-audit","--remote-execute",' + repr(data) + ']\n' + Path(__file__).read_text(encoding='utf-8').replace('from __future__ import annotations', '')
    output = command(module.SSH + ['sudo', '-n', 'python3', '-'], input=source.encode(), timeout=900)
    return json.loads(output)


def write_new(path, result):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('x', encoding='utf-8') as handle:
        json.dump(result, handle, ensure_ascii=False, indent=2)
        handle.write('\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--remote-execute', help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.remote_execute:
        payload = json.loads(base64.b64decode(args.remote_execute))
        environment_guard()
        require(payload.get('action') == 'audit', 'AUDIT_IS_READ_ONLY')
        print(json.dumps(build_manifest(payload['proofs'])))
        return
    require(args.output is not None, 'OUTPUT_REQUIRED')
    result = run_remote({'action': 'audit', 'proofs': local_proofs(Path(__file__).resolve().parents[1])})
    write_new(args.output, result)
    print(json.dumps({'status': result['status'], 'manifestSHA256': result['manifestSHA256'], 'tables': {t: len(v['rows']) for t, v in result['tables'].items()}, 'blockers': result['blockers'], 'productionWrites': 0}))


if __name__ == '__main__':
    main()
