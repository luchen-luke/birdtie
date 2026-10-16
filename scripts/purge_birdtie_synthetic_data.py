"""Exact DGS fixture cleanup. Default is dry-run; backup and apply are explicit.

The production action requires a matching full-backup/restore/double-apply proof.
No account, legacy archive, object-store object, or original Civu data is deleted.
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
import threading
import uuid

import audit_birdtie_synthetic_data as audit

BACKUP_ROOT = Path('/srv/birdtie-data/backups/dgs-cleanup')
STAGE = 'NOT_STARTED'


def assert_sql(sql, expected, code):
    return "DO $dgs$ BEGIN IF (" + sql + ") IS DISTINCT FROM " + audit.literal(json.dumps(expected, separators=(',', ':'))) + "::jsonb THEN RAISE EXCEPTION '" + code + "'; END IF; END $dgs$;"


def selected_rows_sql(table, spec):
    columns = ['t.' + audit.identifier(c) for c in spec['primaryKey']]
    return "SELECT coalesce(jsonb_agg(jsonb_build_object('key',jsonb_build_array(" + ','.join(columns) + "),'sha256',encode(sha256(convert_to(to_jsonb(t)::text,'UTF8')),'hex'))ORDER BY " + ','.join(columns) + "),'[]')FROM " + audit.identifier(table) + " t WHERE " + audit.row_predicate(spec['primaryKey'], spec['rows'])


def protected_checks(manifest):
    statements = []
    for table, spec in manifest['tables'].items():
        statements.append(assert_sql(audit.table_protected_sql(table, spec['primaryKey'], spec['rows']), spec['protectedRows'], 'PROTECTED_ROWS_CHANGED_' + table))
    for table, spec in manifest['protectedGlobal'].items():
        statements.append(assert_sql(audit.table_protected_sql(table, spec['primaryKey'], []), spec['rows'], 'GLOBAL_PROTECTION_CHANGED_' + table))
    return statements


def transaction_sql(manifest, database_oid, mode='apply'):
    audit.validate_manifest(manifest)
    audit.require(mode in {'apply', 'verify-clean'}, 'INVALID_TRANSACTION_MODE')
    tables = sorted(set(manifest['tables']) | set(manifest['protectedGlobal']) | {'agent_memory_source_invalidations'})
    sql = ['BEGIN ISOLATION LEVEL SERIALIZABLE;', "SET LOCAL lock_timeout='5s';", "SET LOCAL statement_timeout='60s';", "SELECT pg_advisory_xact_lock(193719,54087);",
        'LOCK TABLE ' + ','.join(audit.identifier(t) for t in tables) + ' IN ACCESS EXCLUSIVE MODE;',
        "DO $dgs$ BEGIN IF (SELECT oid::bigint FROM pg_database WHERE datname=current_database())<>" + str(database_oid) + " THEN RAISE EXCEPTION 'DATABASE_OID_CHANGED'; END IF; END $dgs$;",
        assert_sql(audit.triggers_sql(manifest['tables']), manifest['deleteTriggers'], 'TRIGGER_STATE_CHANGED')]
    references = sorted(set(manifest['referenceChecks']) - set(tables))
    if references:
        sql.append('LOCK TABLE ' + ','.join(audit.identifier(t) for t in references) + ' IN SHARE ROW EXCLUSIVE MODE;')
    predicates = audit.reference_predicates(manifest['referenceSchema'], {t: s['rows'] for t, s in manifest['tables'].items()})
    audit.require(set(predicates) == set(manifest['referenceChecks']), 'REFERENCE_SCHEMA_CHANGED')
    for table, check in manifest['referenceChecks'].items():
        expected = check['count'] if mode == 'apply' else 0
        sql.append("DO $dgs$ BEGIN IF (SELECT count(*)FROM " + audit.identifier(table) + ' t WHERE ' + predicates[table] + ')<>' + str(expected) + " THEN RAISE EXCEPTION 'REFERENCE_CLOSURE_CHANGED_" + table + "';END IF;END $dgs$;")
    sql.extend(protected_checks(manifest))
    for table, spec in manifest['tables'].items():
        expected = [{'key': x['key'], 'sha256': x['sha256']} for x in spec['rows']] if mode == 'apply' else []
        # Compare sets rather than Python/SQL ordering of numeric compound keys.
        check = "SELECT jsonb_build_object('count',count(*),'digest',encode(sha256(convert_to(coalesce(string_agg(x::text,''ORDER BY x::text),''),'UTF8')),'hex'))FROM jsonb_array_elements((" + selected_rows_sql(table, spec) + '))x'
        expected_check = "SELECT jsonb_build_object('count',count(*),'digest',encode(sha256(convert_to(coalesce(string_agg(x::text,''ORDER BY x::text),''),'UTF8')),'hex'))FROM jsonb_array_elements(" + audit.literal(json.dumps(expected)) + '::jsonb)x'
        sql.append("DO $dgs$ BEGIN IF (" + check + ") IS DISTINCT FROM (" + expected_check + ") THEN RAISE EXCEPTION 'SELECTED_ROWS_CHANGED_" + table + "'; END IF; END $dgs$;")
    if mode == 'apply':
        sql.append('CREATE TEMP TABLE dgs_cache_baseline ON COMMIT DROP AS SELECT coalesce(max(id),0) max_id,count(*) n,encode(sha256(convert_to(coalesce(string_agg(to_jsonb(t)::text,\'\'ORDER BY id),\'\'),\'UTF8\')),\'hex\') digest FROM agent_memory_source_invalidations t;')
        for trigger in manifest['deleteTriggers']:
            if audit.PAUSED_DELETE_GUARDS.get(trigger['table']) == trigger['trigger']:
                sql.append('ALTER TABLE ' + audit.identifier(trigger['table']) + ' DISABLE TRIGGER ' + audit.identifier(trigger['trigger']) + ';')
        for table in manifest['deletionOrder']:
            spec = manifest['tables'][table]
            sql.append('DELETE FROM ' + audit.identifier(table) + ' t WHERE ' + audit.row_predicate(spec['primaryKey'], spec['rows']) + ';')
        for trigger in manifest['deleteTriggers']:
            if audit.PAUSED_DELETE_GUARDS.get(trigger['table']) == trigger['trigger']:
                sql.append('ALTER TABLE ' + audit.identifier(trigger['table']) + ' ENABLE TRIGGER ' + audit.identifier(trigger['trigger']) + ';')
        # Keep the real invalidation trigger; verify its only insertions are the 8 exact roots.
        sql.append("DO $dgs$ BEGIN IF (SELECT count(*)FROM agent_memory_source_invalidations WHERE id>(SELECT max_id FROM dgs_cache_baseline))<>8 OR EXISTS(SELECT 1 FROM agent_memory_source_invalidations WHERE id>(SELECT max_id FROM dgs_cache_baseline)AND(source_type<>'MOMENT'OR source_id NOT IN(" + audit.ids(audit.ROOTS['moments']) + ")OR owner_id NOT IN(" + audit.ids(audit.ACTORS) + "))) OR (SELECT jsonb_build_array(count(*),encode(sha256(convert_to(coalesce(string_agg(to_jsonb(t)::text,''ORDER BY id),''),'UTF8')),'hex'))FROM agent_memory_source_invalidations t WHERE id<=(SELECT max_id FROM dgs_cache_baseline)) IS DISTINCT FROM (SELECT jsonb_build_array(n,digest)FROM dgs_cache_baseline)THEN RAISE EXCEPTION 'UNEXPECTED_CACHE_INVALIDATION_EFFECT';END IF;END $dgs$;")
    for table, spec in manifest['tables'].items():
        sql.append(assert_sql(selected_rows_sql(table, spec), [], 'DEPENDENCY_NOT_REMOVED_' + table))
    sql.extend(protected_checks(manifest))
    sql.append(assert_sql(audit.triggers_sql(manifest['tables']), manifest['deleteTriggers'], 'TRIGGERS_NOT_RESTORED'))
    sql.append('SET CONSTRAINTS ALL IMMEDIATE;')
    sql.append("SELECT jsonb_build_object('status'," + audit.literal('CLEANED' if mode == 'apply' else 'ALREADY_CLEAN') + ",'manifestSHA256'," + audit.literal(manifest['manifestSHA256']) + ",'protectedRowsUnchanged',true,'triggersRestored',true,'foreignKeysEnabled',true,'cacheInvalidationsAdded'," + ('8' if mode == 'apply' else '0') + ');')
    sql.append('COMMIT;')
    return '\n'.join(sql)


def execute_transaction(manifest, database, database_oid, mode):
    raw = audit.command(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-A', '-t', '-q', '-v', 'ON_ERROR_STOP=1', '-d', database], input=transaction_sql(manifest, database_oid, mode).encode(), timeout=90)
    lines = [x for x in raw.decode().splitlines() if x.strip().startswith('{')]
    audit.require(len(lines) == 1, 'TRANSACTION_OUTCOME_NOT_CONFIRMED')
    return json.loads(lines[0])


def check_unchanged(manifest):
    current = audit.build_manifest(manifest['proofs'])
    audit.require(current['manifestSHA256'] == manifest['manifestSHA256'], 'CURRENT_PLAN_CHANGED')


def sha_file(path):
    h = hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def restore_header_line(line, before_objects):
    """Only pg_restore's header changes; COPY rows and SQL bodies are untouched."""
    expected = b"SELECT pg_catalog.set_config('search_path', '', false);\n"
    if before_objects and line == expected:
        return b"SELECT pg_catalog.set_config('search_path', 'public,pg_catalog', false);\n", True
    return line, False


def restore_archive(backup, database):
    errors = bytearray()
    with backup.open('rb') as source:
        reader = subprocess.Popen(['runuser', '-u', 'postgres', '--', 'pg_restore', '--no-owner', '--file=-'], stdin=source, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        writer = subprocess.Popen(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', database], stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        def drain():
            for chunk in iter(lambda: writer.stderr.read(4096), b''):
                if len(errors) < 8192:
                    errors.extend(chunk[:8192 - len(errors)])
        drain_thread = threading.Thread(target=drain, daemon=True)
        drain_thread.start()
        patched = 0
        before_objects = True
        try:
            for line in reader.stdout:
                if line.startswith(b'-- Name:'):
                    before_objects = False
                output, changed = restore_header_line(line, before_objects)
                patched += int(changed)
                writer.stdin.write(output)
            writer.stdin.close()
            read_error = reader.stderr.read()
            read_code = reader.wait(timeout=300)
            write_code = writer.wait(timeout=300)
            drain_thread.join(timeout=5)
            return read_code or write_code, bytes(errors) or read_error, patched
        finally:
            for process in (reader, writer):
                if process.poll() is None:
                    process.kill()
                process.wait(timeout=10)


def backup_and_restore(manifest, existing_archive=None):
    global STAGE
    STAGE = 'SOURCE_PLAN_PRECHECK'
    audit.validate_manifest(manifest)
    check_unchanged(manifest)
    BACKUP_ROOT.mkdir(parents=True, mode=0o700, exist_ok=True)
    audit.require(BACKUP_ROOT.resolve() == BACKUP_ROOT and BACKUP_ROOT.stat().st_mode & 0o077 == 0, 'BACKUP_DIRECTORY_GUARD')
    stamp = dt.datetime.now(dt.timezone.utc).strftime('%Y%m%d%H%M%S')
    suffix = uuid.uuid4().hex[:12]
    STAGE = 'FULL_NATIVE_BACKUP'
    backup = Path(existing_archive) if existing_archive else BACKUP_ROOT / ('native-' + stamp + '-' + suffix + '.dump')
    if existing_archive:
        audit.require(backup.parent.resolve() == BACKUP_ROOT and re.fullmatch(r'native-[0-9]{14}-[0-9a-f]{12}\.dump', backup.name) and backup.stat().st_mode & 0o777 == 0o600, 'EXISTING_ARCHIVE_GUARD')
    else:
        with backup.open('xb') as output:
            os.chmod(backup, 0o600)
            result = subprocess.run(['runuser', '-u', 'postgres', '--', 'pg_dump', '-Fc', '-d', audit.DB], stdout=output, stderr=subprocess.PIPE, timeout=300)
        audit.require(result.returncode == 0, 'BACKUP_DUMP_FAILED')
    archive_sha = sha_file(backup)
    # PostgreSQL can read only this protected file through inherited stdin.
    restored = 'birdtie_dgs_restore_' + stamp + '_' + suffix
    marker = 'BIRDTIE_DGS_DISPOSABLE_RESTORE:' + suffix
    audit.require(re.fullmatch(r'birdtie_dgs_restore_[0-9]{14}_[0-9a-f]{12}', restored), 'RESTORE_NAME_GUARD')
    created = False
    restore_oid = None
    receipt = None
    try:
        STAGE = 'RESTORE_CREATE'
        audit.command(['runuser', '-u', 'postgres', '--', 'createdb', '--template=template0', '--owner=birdtie_native_hz', restored])
        created = True
        audit.command(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', restored], input=('COMMENT ON DATABASE ' + audit.identifier(restored) + ' IS ' + audit.literal(marker) + ';').encode())
        restore_oid = audit.query("SELECT oid::bigint FROM pg_database WHERE datname=current_database()", restored)
        audit.require(restore_oid != audit.OID, 'RESTORE_TARGET_IS_PRODUCTION')
        STAGE = 'RESTORE_ARCHIVE'
        restore_code, restore_error, header_changes = restore_archive(backup, restored)
        if restore_code:
            text = restore_error.decode(errors='replace')
            match = re.search(r'ERROR:\s*([^\n]+)', text)
            # PostgreSQL's first diagnostic only; omit quoted values and all COPY/context lines.
            detail = re.sub(r'"[^"]*"|\x27[^\x27]*\x27', '<quoted>', match[1]) if match else 'NO_POSTGRES_DIAGNOSTIC'
            detail = re.sub(r'[0-9a-f]{8}-[0-9a-f-]{27}|[0-9]{7,}', '<redacted>', detail)
            raise RuntimeError('RESTORE_FAILED:' + detail[:180])
        audit.require(header_changes == 1, 'RESTORE_HEADER_COMPATIBILITY_GUARD')
        STAGE = 'RESTORED_PLAN_COMPARE'
        restored_plan = audit.build_manifest(manifest['proofs'], restored)
        audit.require(restored_plan['manifestSHA256'] == manifest['manifestSHA256'], 'RESTORED_ROWS_DIFFER')
        STAGE = 'RESTORED_DELETE_TRANSACTION'
        first = execute_transaction(manifest, restored, restore_oid, 'apply')
        STAGE = 'RESTORED_SECOND_APPLY'
        second = execute_transaction(manifest, restored, restore_oid, 'verify-clean')
        check_unchanged(manifest)
        receipt = {'protocol': audit.PROTOCOL, 'status': 'BACKUP_RESTORE_VERIFIED', 'observedAt': dt.datetime.now(dt.timezone.utc).isoformat(),
            'manifestSHA256': manifest['manifestSHA256'], 'databaseOID': audit.OID, 'release': audit.RELEASE, 'sourceSHA256': audit.SOURCE_SHA,
            'backupPath': str(backup), 'backupBytes': backup.stat().st_size, 'backupSHA256': archive_sha, 'backupMode': '0600',
            'restoredDatabaseOID': restore_oid, 'restoredPlanIdentical': True, 'restoreCleanup': first, 'restoreSecondApply': second,
            'restoreSearchPathCompatibility': 'One pg_restore header statement only; public,pg_catalog; original dump unchanged; no COPY/body rewrite.',
            'productionWrites': 0, 'civuWrites': 0, 'apiLogins': 0, 'originalUnknownMomentsPreserved': 19}
    finally:
        if created:
            actual = audit.query("SELECT jsonb_build_object('oid',oid::bigint,'marker',shobj_description(oid,'pg_database'))FROM pg_database WHERE datname=current_database()", restored)
            audit.require(actual['oid'] == restore_oid and actual['oid'] != audit.OID and actual['marker'] == marker, 'RESTORE_DROP_GUARD')
            audit.command(['runuser', '-u', 'postgres', '--', 'dropdb', restored])
            if receipt is not None:
                receipt['disposableRestoreDatabaseDropped'] = True
    audit.require(receipt is not None, 'BACKUP_RESTORE_NOT_VERIFIED')
    record = backup.with_suffix('.receipt.json')
    audit.write_new(record, receipt)
    os.chmod(record, 0o600)
    return receipt


def apply(manifest, backup_receipt):
    audit.validate_manifest(manifest)
    audit.require(backup_receipt.get('status') == 'BACKUP_RESTORE_VERIFIED' and backup_receipt.get('manifestSHA256') == manifest['manifestSHA256'] and backup_receipt.get('disposableRestoreDatabaseDropped') is True and backup_receipt.get('restoreSecondApply', {}).get('status') == 'ALREADY_CLEAN', 'MATCHING_RESTORE_PROOF_REQUIRED')
    archive = Path(backup_receipt['backupPath'])
    audit.require(archive.parent.resolve() == BACKUP_ROOT and archive.stat().st_mode & 0o777 == 0o600 and sha_file(archive) == backup_receipt['backupSHA256'], 'BACKUP_ARCHIVE_CHANGED')
    durable = json.loads(archive.with_suffix('.receipt.json').read_text())
    audit.require(durable == backup_receipt, 'DURABLE_BACKUP_RECEIPT_MISMATCH')
    presence = [audit.query('SELECT count(*)FROM ' + audit.identifier(t) + ' x WHERE ' + audit.row_predicate(v['primaryKey'], v['rows'], 'x')) for t, v in manifest['tables'].items()]
    if not any(presence):
        return execute_transaction(manifest, audit.DB, audit.OID, 'verify-clean')
    check_unchanged(manifest)
    outcome = execute_transaction(manifest, audit.DB, audit.OID, 'apply')
    outcome.update({'protocol': audit.PROTOCOL, 'observedAt': dt.datetime.now(dt.timezone.utc).isoformat(), 'databaseOID': audit.OID,
        'deletedRows': {t: len(v['rows']) for t, v in manifest['tables'].items()}, 'backupSHA256': backup_receipt['backupSHA256'],
        'accountsDeleted': 0, 'unknownMomentsDeleted': 0, 'legacyRowsDeleted': 0, 'civuWrites': 0, 'apiLogins': 0})
    record = archive.with_suffix('.applied.json')
    audit.write_new(record, outcome)
    os.chmod(record, 0o600)
    return outcome


def remote_call(payload):
    helper_path = Path(audit.__file__)
    spec = importlib.util.spec_from_file_location('dgs_purge_transport', helper_path.parent / 'deploy_birdtie_hangzhou.py')
    transport = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(transport)
    encoded = base64.b64encode(json.dumps(payload).encode()).decode()
    source = ('import sys,types\nhelper=types.ModuleType("audit_birdtie_synthetic_data")\n'
        + 'exec(compile(' + repr(helper_path.read_text(encoding='utf-8')) + ',"dgs-audit","exec"),helper.__dict__)\n'
        + 'sys.modules["audit_birdtie_synthetic_data"]=helper\n'
        + 'sys.argv=["dgs-purge","--remote-execute",' + repr(encoded) + ']\n'
        + 'exec(compile(' + repr(Path(__file__).read_text(encoding='utf-8')) + ',"dgs-purge","exec"),{"__name__":"__main__"})\n')
    raw = audit.command(transport.SSH + ['sudo', '-n', 'python3', '-'], input=source.encode(), timeout=900)
    return json.loads(raw)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path)
    parser.add_argument('--output', type=Path)
    actions = parser.add_mutually_exclusive_group()
    actions.add_argument('--prepare-backup', action='store_true')
    actions.add_argument('--apply', action='store_true')
    parser.add_argument('--backup-receipt', type=Path)
    parser.add_argument('--existing-archive', help='Retry verification of an already protected full archive; never overwrite it.')
    parser.add_argument('--remote-execute', help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.remote_execute:
        payload = json.loads(base64.b64decode(args.remote_execute))
        audit.environment_guard()
        manifest = payload['manifest']
        audit.validate_manifest(manifest)
        try:
            if payload['action'] == 'backup':
                result = backup_and_restore(manifest, payload.get('existingArchive'))
            elif payload['action'] == 'apply':
                result = apply(manifest, payload['backupReceipt'])
            elif payload['action'] == 'dry-run':
                check_unchanged(manifest)
                result = {'status': 'DRY_RUN', 'manifestSHA256': manifest['manifestSHA256'], 'productionWrites': 0}
            else:
                raise RuntimeError('UNKNOWN_ACTION')
        except RuntimeError as error:
            result = {'status': 'FAIL', 'stage': STAGE, 'errorCode': str(error), 'productionWrites': 0 if payload['action'] != 'apply' else 'OUTCOME_MUST_BE_RECONCILED', 'manifestSHA256': manifest['manifestSHA256']}
        print(json.dumps(result))
        return
    audit.require(args.manifest is not None and args.output is not None, 'MANIFEST_AND_OUTPUT_REQUIRED')
    manifest = json.loads(args.manifest.read_text(encoding='utf-8'))
    audit.validate_manifest(manifest)
    payload = {'action': 'backup' if args.prepare_backup else 'apply' if args.apply else 'dry-run', 'manifest': manifest}
    if args.existing_archive:
        audit.require(args.prepare_backup, 'EXISTING_ARCHIVE_REQUIRES_BACKUP_VERIFICATION')
        payload['existingArchive'] = args.existing_archive
    if args.apply:
        audit.require(args.backup_receipt is not None, 'BACKUP_RECEIPT_REQUIRED')
        payload['backupReceipt'] = json.loads(args.backup_receipt.read_text(encoding='utf-8'))
    result = remote_call(payload)
    audit.write_new(args.output, result)
    print(json.dumps({k: v for k, v in result.items() if k not in {'backupPath', 'deletedRows'}}))
    if result['status'] == 'FAIL':
        raise SystemExit(1)


if __name__ == '__main__':
    main()
