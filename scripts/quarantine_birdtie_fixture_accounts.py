"""Exact, reversible fixture account quarantine. Default action is read-only.

Only reviewed account status transitions are allowed. Existing identities and all
content remain intact. Production apply requires a successful isolated restore,
quarantine, repeat, restore-status and repeat-quarantine rehearsal.
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
import sys
import uuid

import audit_birdtie_synthetic_data as audit
import purge_birdtie_synthetic_data as cleanup

PROTOCOL = 'BIRDTIE_DGS_EXACT_ACCOUNT_QUARANTINE_V1'
PLAN_SHA = 'c10fde61ef9a7ed74cc1c0c20d8fb1cc316e078e4c54b14363d6deae49785dc9'
CLEANUP_SHA = '22333e1ce5e441f90c7879e0db7fe66c866483111b599a2328f3ab1a56eba802'
BACKUP = Path('/srv/birdtie-data/backups/dgs-cleanup/native-20261016091400-af763ccdaa7f.dump')
BACKUP_SHA = '0ec21f783bb0c05a255fccb78203e12287307baf5cc6dbb6e8bf4e2525414319'
STAGE = 'NOT_STARTED'
DIAGNOSTICS = {}


def validate_plan(plan):
    audit.require(plan.get('protocol') == PROTOCOL and plan.get('manifestSHA256') == PLAN_SHA, 'EXACT_QUARANTINE_PLAN_REQUIRED')
    audit.require(audit.digest({k: v for k, v in plan.items() if k not in {'observedAt', 'manifestSHA256'}}) == PLAN_SHA, 'QUARANTINE_PLAN_HASH_CHANGED')
    audit.require([a['id'] for a in plan['actors']] == audit.ACTORS and len(plan['actors']) == 6, 'QUARANTINE_ACCOUNT_SCOPE_CHANGED')
    audit.require(plan.get('databaseOID') == audit.OID and plan.get('databaseName') == audit.DB and plan.get('release') == audit.RELEASE and plan.get('binarySHA256') == audit.BINARY_SHA and plan.get('sourceSHA256') == audit.SOURCE_SHA, 'QUARANTINE_ENVIRONMENT_CHANGED')
    audit.require(plan.get('blockers') == [] and all(a['status'] == 'active' and a['accountType'] == 'person' and a['authIdentities'] == a['fixedCodeIdentities'] == 1 and a['legacyUsers'] == a['phoneBindings'] == a['emailBindings'] == a['validSessions'] == 0 for a in plan['actors']), 'QUARANTINE_IDENTITY_PROOF_CHANGED')


def account_rows_sql(status_override=None):
    row = 'to_jsonb(a)'
    if status_override:
        audit.require(status_override in {'active', 'suspended'}, 'INVALID_ACCOUNT_STATUS')
        row += "||jsonb_build_object('status'," + audit.literal(status_override) + ')'
    return "SELECT coalesce(jsonb_agg(jsonb_build_object('id',a.id,'status'," + (audit.literal(status_override) if status_override else 'a.status') + ",'sha256',encode(sha256(convert_to((" + row + ")::text,'UTF8')),'hex'))ORDER BY a.id),'[]')FROM accounts a WHERE a.id IN(" + audit.ids(audit.ACTORS) + ')'


def original_rows(plan):
    return [{'id': a['id'], 'status': 'active', 'sha256': a['rowSHA256']} for a in plan['actors']]


def trigger_facts_sql():
    # Review all account triggers (including FK triggers) and their exact code.
    # PostgreSQL assigns new internal trigger OIDs on restore. Normalize only
    # that generated name, retaining its constraint identity, event and function.
    return """SELECT coalesce(jsonb_agg(jsonb_build_object('name',name,'enabled',enabled,
      'definitionSHA256',definition)ORDER BY name,definition),'[]')FROM(SELECT
      CASE WHEN t.tgisinternal THEN c.conname::text||':'||t.tgtype::text ELSE t.tgname::text END name,
      t.tgenabled enabled,encode(sha256(convert_to(
       CASE WHEN t.tgisinternal THEN replace(pg_get_triggerdef(t.oid),t.tgname,'INTERNAL_FK_TRIGGER') ELSE pg_get_triggerdef(t.oid)END
       ||pg_get_functiondef(t.tgfoid),'UTF8')),'hex')definition
      FROM pg_trigger t LEFT JOIN pg_constraint c ON c.oid=t.tgconstraint
      WHERE t.tgrelid='accounts'::regclass)facts"""


def schema_hash(meta):
    return audit.digest({**meta, 'foreignKeys': sorted(meta['foreignKeys'], key=lambda x: json.dumps(x, sort_keys=True))})


def no_new_identity_sql():
    scope = audit.ids(audit.ACTORS)
    return """SELECT jsonb_build_object(
     'identities',(SELECT count(*)FROM account_auth_identities WHERE account_id IN(""" + scope + """)),
     'fixedIdentities',(SELECT count(*)FROM account_auth_identities WHERE account_id IN(""" + scope + """)AND issuer='urn:birdtie:local-dev-phone'),
     'validSessions',(SELECT count(*)FROM sessions WHERE account_id IN(""" + scope + """)AND revoked_at IS NULL AND expires_at>clock_timestamp()AND idle_expires_at>clock_timestamp()),
     'civuUsers',(SELECT count(*)FROM legacy_entity_mappings WHERE entity_kind='user'AND target_id IN(""" + scope + """)),
     'phoneBindings',(SELECT count(*)FROM legacy_dev_phone_bindings WHERE owner_account_id IN(""" + scope + """)),
     'emailBindings',(SELECT count(*)FROM legacy_dev_email_bindings WHERE owner_account_id IN(""" + scope + """)),
     'activeClose',(SELECT count(*)FROM person_close_connections WHERE is_active AND(owner_account_id IN(""" + scope + ')OR person_account_id IN(' + scope + '))))'


IDENTITY_FACTS = {'identities': 6, 'fixedIdentities': 6, 'validSessions': 0, 'civuUsers': 0,
                  'phoneBindings': 0, 'emailBindings': 0, 'activeClose': 0}


def snapshot_sql(tables):
    """Hash every other row without emitting row data, including tables without PKs."""
    statements = []
    for table in sorted(tables):
        predicate = ' WHERE t.id NOT IN(' + audit.ids(audit.ACTORS) + ')' if table == 'accounts' else ''
        statements.append('SELECT ' + audit.literal(table) + " label,jsonb_build_object('count',count(*),'sha256',encode(sha256(convert_to(coalesce(string_agg(h,''ORDER BY h),''),'UTF8')),'hex'))value FROM(SELECT encode(sha256(convert_to(to_jsonb(t)::text,'UTF8')),'hex')h FROM " + audit.identifier(table) + ' t' + predicate + ')rows')
    audit.require(statements, 'NO_DATABASE_TABLES')
    return 'SELECT jsonb_object_agg(label,value)FROM(' + ' UNION ALL '.join(statements) + ')checks'


def current_preflight(plan, database=audit.DB):
    validate_plan(plan)
    audit.require(audit.query(account_rows_sql(), database) == original_rows(plan), 'QUARANTINE_ACCOUNT_ROWS_CHANGED')
    audit.require(audit.query(no_new_identity_sql(), database) == IDENTITY_FACTS, 'QUARANTINE_IDENTITY_OR_DEPENDENCY_CHANGED')
    for table, spec in plan['protected'].items():
        records = [{'key': [x]} for x in audit.ACTORS] if table == 'accounts' else []
        audit.require(audit.query(audit.table_protected_sql(table, spec['primaryKey'], records), database) == spec['rows'], 'QUARANTINE_PROTECTED_ROWS_CHANGED_' + table)
    meta = audit.schema(database)
    triggers = audit.query(trigger_facts_sql(), database)
    audit.require(all(x['enabled'] == 'O' for x in triggers), 'ACCOUNT_TRIGGERS_DISABLED')
    return {'tables': sorted(meta['columns']), 'triggers': triggers, 'schemaSHA256': schema_hash(meta)}


def transaction_sql(plan, oid, schema_facts, mode, applied_rows=None):
    validate_plan(plan)
    audit.require(mode in {'apply', 'verify', 'restore'}, 'INVALID_QUARANTINE_MODE')
    audit.require(isinstance(oid, int) and oid > 0, 'INVALID_DATABASE_OID')
    expected = original_rows(plan) if mode == 'apply' else applied_rows
    audit.require(isinstance(expected, list) and len(expected) == 6 and [r['id'] for r in expected] == audit.ACTORS, 'EXACT_STATUS_SNAPSHOT_REQUIRED')
    tables = schema_facts['tables']
    audit.require('accounts' in tables and 'sessions' in tables and 'person_close_connections' in tables, 'INCOMPLETE_TABLE_PROTECTION')
    before_snapshot = snapshot_sql(tables)
    sql = ['BEGIN ISOLATION LEVEL SERIALIZABLE;', "SET LOCAL lock_timeout='5s';", "SET LOCAL statement_timeout='180s';",
        'SELECT pg_advisory_xact_lock(193720,54087);',
        'LOCK TABLE accounts,sessions,account_auth_identities,legacy_dev_phone_bindings,legacy_dev_email_bindings,person_close_connections IN SHARE ROW EXCLUSIVE MODE;',
        "DO $dgs$ BEGIN IF (SELECT oid::bigint FROM pg_database WHERE datname=current_database())<>" + str(oid) + " THEN RAISE EXCEPTION 'QUARANTINE_DATABASE_OID_CHANGED';END IF;END $dgs$;",
        cleanup.assert_sql(trigger_facts_sql(), schema_facts['triggers'], 'QUARANTINE_TRIGGER_CODE_CHANGED'),
        cleanup.assert_sql(no_new_identity_sql(), IDENTITY_FACTS, 'QUARANTINE_IDENTITY_OR_DEPENDENCY_CHANGED'),
        cleanup.assert_sql(account_rows_sql(), expected, 'QUARANTINE_ACCOUNT_ROWS_CHANGED')]
    for table, spec in plan['protected'].items():
        records = [{'key': [x]} for x in audit.ACTORS] if table == 'accounts' else []
        sql.append(cleanup.assert_sql(audit.table_protected_sql(table, spec['primaryKey'], records), spec['rows'], 'QUARANTINE_PROTECTED_ROWS_CHANGED_' + table))
    sql.append('CREATE TEMP TABLE dgs_other_rows ON COMMIT DROP AS ' + before_snapshot + ';')
    if mode == 'apply':
        sql.append("CREATE TEMP TABLE dgs_expected_accounts ON COMMIT DROP AS " + account_rows_sql('suspended') + ';')
        sql.append("UPDATE accounts SET status='suspended' WHERE id IN(" + audit.ids(audit.ACTORS) + ")AND status='active';")
        sql.append("DO $dgs$ BEGIN IF(" + account_rows_sql() + ")IS DISTINCT FROM(SELECT coalesce FROM dgs_expected_accounts)THEN RAISE EXCEPTION 'QUARANTINE_UNEXPECTED_ACCOUNT_EFFECT';END IF;END $dgs$;")
    elif mode == 'restore':
        sql.append("UPDATE accounts SET status='active' WHERE id IN(" + audit.ids(audit.ACTORS) + ")AND status='suspended';")
        sql.append(cleanup.assert_sql(account_rows_sql(), original_rows(plan), 'QUARANTINE_RESTORE_MISMATCH'))
    sql.append("DO $dgs$ BEGIN IF(" + before_snapshot + ")IS DISTINCT FROM(SELECT jsonb_object_agg FROM dgs_other_rows)THEN RAISE EXCEPTION 'QUARANTINE_OTHER_ROWS_CHANGED';END IF;END $dgs$;")
    sql.extend([cleanup.assert_sql(trigger_facts_sql(), schema_facts['triggers'], 'QUARANTINE_TRIGGERS_CHANGED'), 'SET CONSTRAINTS ALL IMMEDIATE;',
        "SELECT jsonb_build_object('status'," + audit.literal({'apply': 'QUARANTINED', 'verify': 'ALREADY_QUARANTINED', 'restore': 'STATUS_RESTORED'}[mode]) + ",'manifestSHA256'," + audit.literal(PLAN_SHA) + ",'accounts',(" + account_rows_sql() + "),'otherTablesChecked'," + str(len(tables)) + ",'otherRowsUnchanged',true,'triggersUnchanged',true,'identitiesPreserved',true,'accountRowsDeleted',0,'contentRowsDeleted',0,'sessionRowsChanged',0);", 'COMMIT;'])
    return '\n'.join(sql)


def execute(plan, database, oid, facts, mode, applied_rows=None):
    raw = audit.command(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-A', '-t', '-q', '-v', 'ON_ERROR_STOP=1', '-d', database], input=transaction_sql(plan, oid, facts, mode, applied_rows).encode(), timeout=240)
    rows = [x for x in raw.decode().splitlines() if x.startswith('{')]
    audit.require(len(rows) == 1, 'QUARANTINE_COMMIT_UNCONFIRMED')
    return json.loads(rows[0])


def verify_archive():
    audit.require(BACKUP.parent.resolve() == cleanup.BACKUP_ROOT and BACKUP.stat().st_mode & 0o777 == 0o600 and cleanup.sha_file(BACKUP) == BACKUP_SHA, 'QUARANTINE_BACKUP_CHANGED')
    receipt = json.loads(BACKUP.with_suffix('.receipt.json').read_text())
    audit.require(receipt.get('status') == 'BACKUP_RESTORE_VERIFIED' and receipt.get('manifestSHA256') == CLEANUP_SHA and receipt.get('backupSHA256') == BACKUP_SHA and receipt.get('disposableRestoreDatabaseDropped') is True, 'ORIGINAL_BACKUP_PROOF_CHANGED')


def rehearse(plan, cleanup_manifest):
    global STAGE
    facts = current_preflight(plan)
    verify_archive()
    audit.validate_manifest(cleanup_manifest)
    audit.require(cleanup_manifest['manifestSHA256'] == CLEANUP_SHA, 'ORIGINAL_CLEANUP_PLAN_CHANGED')
    suffix = uuid.uuid4().hex[:12]
    name = 'birdtie_dgs_quarantine_' + suffix
    marker = 'BIRDTIE_DGS_QUARANTINE_RESTORE:' + suffix
    created = False
    oid = None
    result = None
    try:
        STAGE = 'QUARANTINE_RESTORE_CREATE'
        audit.command(['runuser', '-u', 'postgres', '--', 'createdb', '--template=template0', '--owner=birdtie_native_hz', name])
        created = True
        audit.command(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', name], input=('COMMENT ON DATABASE ' + audit.identifier(name) + ' IS ' + audit.literal(marker) + ';').encode())
        oid = audit.query('SELECT oid::bigint FROM pg_database WHERE datname=current_database()', name)
        audit.require(oid != audit.OID, 'RESTORE_TARGET_IS_PRODUCTION')
        STAGE = 'QUARANTINE_RESTORE_ARCHIVE'
        code, _, patches = cleanup.restore_archive(BACKUP, name)
        audit.require(code == 0 and patches == 1, 'QUARANTINE_RESTORE_FAILED')
        STAGE = 'QUARANTINE_REPLAY_ALREADY_APPROVED_CLEANUP_ONLY_IN_RESTORE'
        previous = audit.build_manifest(cleanup_manifest['proofs'], name)
        audit.require(previous['manifestSHA256'] == CLEANUP_SHA, 'ORIGINAL_RESTORE_PLAN_CHANGED')
        cleanup.execute_transaction(cleanup_manifest, name, oid, 'apply')
        cleanup.execute_transaction(cleanup_manifest, name, oid, 'verify-clean')
        STAGE = 'QUARANTINE_RESTORED_CURRENT_COMPARE'
        restored = current_preflight(plan, name)
        if restored != facts:
            DIAGNOSTICS['restoredCompare'] = {
                'tablesEqual': restored['tables'] == facts['tables'],
                'schemaHashEqual': restored['schemaSHA256'] == facts['schemaSHA256'],
                'triggerFactsEqual': restored['triggers'] == facts['triggers'],
                'triggerDifferences': [{'source': a, 'restored': b} for a, b in zip(facts['triggers'], restored['triggers']) if a != b][:4]}
        audit.require(restored == facts, 'QUARANTINE_RESTORED_SCHEMA_CHANGED')
        STAGE = 'QUARANTINE_RESTORED_APPLY'
        first = execute(plan, name, oid, facts, 'apply')
        second = execute(plan, name, oid, facts, 'verify', first['accounts'])
        restored_status = execute(plan, name, oid, facts, 'restore', first['accounts'])
        repeated = execute(plan, name, oid, facts, 'apply')
        audit.require(repeated['accounts'] == first['accounts'], 'QUARANTINE_REPLAY_CHANGED_ROWS')
        current_preflight(plan)
        result = {'protocol': PROTOCOL, 'status': 'QUARANTINE_RESTORE_VERIFIED', 'manifestSHA256': PLAN_SHA,
            'observedAt': dt.datetime.now(dt.timezone.utc).isoformat(), 'databaseOID': audit.OID,
            'backupSHA256': BACKUP_SHA, 'backupPath': str(BACKUP), 'restoredDatabaseOID': oid,
            'schemaFacts': facts, 'first': first, 'second': second, 'statusRestore': restored_status,
            'repeat': repeated, 'productionWrites': 0, 'apiLogins': 0, 'civuWrites': 0,
            'unknownContentRowsPreserved': 19, 'originalCivuRowsPreserved': 83}
    finally:
        if created:
            actual = audit.query("SELECT jsonb_build_object('oid',oid::bigint,'marker',shobj_description(oid,'pg_database'))FROM pg_database WHERE datname=current_database()", name)
            audit.require(re.fullmatch(r'birdtie_dgs_quarantine_[0-9a-f]{12}', name) and actual['oid'] == oid and oid != audit.OID and actual['marker'] == marker, 'QUARANTINE_RESTORE_DROP_GUARD')
            audit.command(['runuser', '-u', 'postgres', '--', 'dropdb', name])
            if result is not None:
                result['restoreDatabaseDropped'] = True
    audit.require(result is not None, 'QUARANTINE_REHEARSAL_INCOMPLETE')
    result['receiptSHA256'] = audit.digest(result)
    path = BACKUP.with_suffix('.quarantine-' + result['receiptSHA256'][:16] + '.json')
    audit.write_new(path, result)
    os.chmod(path, 0o600)
    return result


def apply(plan, receipt):
    global STAGE
    validate_plan(plan)
    audit.require(receipt.get('protocol') == PROTOCOL and receipt.get('status') == 'QUARANTINE_RESTORE_VERIFIED' and receipt.get('manifestSHA256') == PLAN_SHA and receipt.get('restoreDatabaseDropped') is True and receipt.get('second', {}).get('status') == 'ALREADY_QUARANTINED' and receipt.get('statusRestore', {}).get('status') == 'STATUS_RESTORED', 'MATCHING_QUARANTINE_RESTORE_REQUIRED')
    audit.require(audit.digest({k: v for k, v in receipt.items() if k != 'receiptSHA256'}) == receipt.get('receiptSHA256'), 'QUARANTINE_RESTORE_RECEIPT_HASH_CHANGED')
    durable = BACKUP.with_suffix('.quarantine-' + receipt['receiptSHA256'][:16] + '.json')
    audit.require(durable.stat().st_mode & 0o777 == 0o600 and json.loads(durable.read_text()) == receipt, 'DURABLE_QUARANTINE_RESTORE_REQUIRED')
    verify_archive()
    rows = audit.query(account_rows_sql())
    audit.require(schema_hash(audit.schema()) == receipt['schemaFacts']['schemaSHA256'], 'QUARANTINE_SCHEMA_CHANGED')
    if rows == receipt['first']['accounts']:
        STAGE = 'QUARANTINE_PRODUCTION_VERIFY'
        return execute(plan, audit.DB, audit.OID, receipt['schemaFacts'], 'verify', rows)
    audit.require(current_preflight(plan) == receipt['schemaFacts'], 'QUARANTINE_PREFLIGHT_CHANGED')
    STAGE = 'QUARANTINE_PRODUCTION_APPLY'
    result = execute(plan, audit.DB, audit.OID, receipt['schemaFacts'], 'apply')
    result.update({'observedAt': dt.datetime.now(dt.timezone.utc).isoformat(), 'databaseOID': audit.OID,
                   'restoreProofSHA256': receipt['receiptSHA256'], 'unknownContentRowsPreserved': 19,
                   'unknownFixtureContentVisibilityIsolated': 18, 'originalCivuRowsPreserved': 83})
    record = durable.with_suffix('.applied.json')
    audit.write_new(record, result)
    os.chmod(record, 0o600)
    return result


def remote_call(payload):
    spec = importlib.util.spec_from_file_location('_quarantine_transport', Path(audit.__file__).parent / 'deploy_birdtie_hangzhou.py')
    transport = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(transport)
    source = 'import sys,types\n'
    for name, path in [('audit_birdtie_synthetic_data', Path(audit.__file__)), ('purge_birdtie_synthetic_data', Path(cleanup.__file__))]:
        source += 'module=types.ModuleType(' + repr(name) + ')\nexec(compile(' + repr(path.read_text(encoding='utf-8')) + ',' + repr(name) + ',"exec"),module.__dict__)\nsys.modules[' + repr(name) + ']=module\n'
    source += 'sys.argv=["quarantine","--remote-execute",' + repr(base64.b64encode(json.dumps(payload).encode()).decode()) + ']\n'
    source += 'exec(compile(' + repr(Path(__file__).read_text(encoding='utf-8')) + ',"quarantine","exec"),{"__name__":"__main__"})\n'
    return json.loads(audit.command(transport.SSH + ['sudo', '-n', 'python3', '-'], input=source.encode(), timeout=900))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path)
    parser.add_argument('--output', type=Path)
    group = parser.add_mutually_exclusive_group()
    group.add_argument('--prepare-backup', action='store_true')
    group.add_argument('--apply', action='store_true')
    parser.add_argument('--cleanup-manifest', type=Path)
    parser.add_argument('--backup-receipt', type=Path)
    parser.add_argument('--remote-execute', help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.remote_execute:
        payload = json.loads(base64.b64decode(args.remote_execute))
        audit.environment_guard()
        plan = payload['manifest']
        validate_plan(plan)
        try:
            if payload['action'] == 'rehearse':
                result = rehearse(plan, payload['cleanupManifest'])
            elif payload['action'] == 'apply':
                result = apply(plan, payload['backupReceipt'])
            elif payload['action'] == 'dry-run':
                result = {'status': 'DRY_RUN', 'manifestSHA256': PLAN_SHA, 'facts': current_preflight(plan), 'productionWrites': 0}
            else:
                raise RuntimeError('UNKNOWN_QUARANTINE_ACTION')
        except RuntimeError as error:
            result = {'status': 'FAIL', 'stage': STAGE, 'errorCode': str(error), 'manifestSHA256': PLAN_SHA,
                       'productionWrites': 'OUTCOME_MUST_BE_RECONCILED' if payload['action'] == 'apply' else 0}
            result['diagnostics'] = DIAGNOSTICS
        print(json.dumps(result))
        return
    audit.require(args.manifest is not None and args.output is not None, 'QUARANTINE_MANIFEST_OUTPUT_REQUIRED')
    plan = json.loads(args.manifest.read_text(encoding='utf-8'))
    validate_plan(plan)
    payload = {'manifest': plan, 'action': 'rehearse' if args.prepare_backup else 'apply' if args.apply else 'dry-run'}
    if args.prepare_backup:
        audit.require(args.cleanup_manifest is not None, 'ORIGINAL_CLEANUP_MANIFEST_REQUIRED')
        payload['cleanupManifest'] = json.loads(args.cleanup_manifest.read_text(encoding='utf-8'))
    if args.apply:
        audit.require(args.backup_receipt is not None, 'QUARANTINE_RESTORE_PROOF_REQUIRED')
        payload['backupReceipt'] = json.loads(args.backup_receipt.read_text(encoding='utf-8'))
    result = remote_call(payload)
    audit.write_new(args.output, result)
    print(json.dumps({k: v for k, v in result.items() if k in {'status', 'stage', 'errorCode', 'manifestSHA256', 'receiptSHA256', 'productionWrites', 'restoreDatabaseDropped', 'otherTablesChecked'}}))
    if result['status'] == 'FAIL':
        raise SystemExit(1)


if __name__ == '__main__':
    main()
