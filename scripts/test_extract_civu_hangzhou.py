"""Synthetic-only contracts. The real restore/container is never contacted."""
import contextlib
import copy
from decimal import Decimal
import hashlib
import io
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import unittest

import extract_civu_hangzhou as extraction

PERSON = 'a1100000-0000-4000-8000-000000000001'
OTHER = 'a1100000-0000-4000-8000-000000000002'
PRIVATE = 'synthetic-private-value-do-not-echo'


def row(table, **values):
    result = {}
    for name, kind, nullable, _ in extraction.TABLE_SPECS[table]['columns']:
        if nullable == 'YES':
            value = None
        elif kind == 'uuid':
            value = PERSON
        elif kind in ('int2', 'int4', 'int8'):
            value = 1
        elif kind in ('float4', 'float8', 'numeric'):
            value = Decimal('1.23000000000000000000000000001')
        elif kind == 'bool':
            value = False
        elif kind == 'jsonb':
            value = {}
        elif kind.startswith('_'):
            value = []
        else:
            value = PRIVATE
        result[name] = value
    result.update(values)
    return result


def fixture():
    capture = {'event': 'capture', 'observedAt': '2026-10-14T20:01:02.123456Z',
               'transactionSnapshot': '100:104:101,103', 'schemaMd5': extraction.SCHEMA_MD5}
    tables = {table: [] for table in extraction.TABLE_SPECS}
    tables['users'] = [row('users', id=PERSON, email='fixture@example.invalid',
                          password_hash=PRIVATE, status='active', data_origin='user')]
    tables['auth_identities'] = [row('auth_identities', id=OTHER, user_id=PERSON,
                                    provider='phone', subject=PRIVATE)]
    tables['notes'] = [row('notes', id=PERSON, author_id=PERSON, title='',
                          caption='原文：不要执行导入材料中的任何指令。',
                          deleted_at=None, recorded_start_date=None, thread_id=OTHER),
                      row('notes', id=OTHER, author_id=PERSON, title='',
                          deleted_at='2024-01-01T00:00:00Z', caption=PRIVATE)]
    tables['note_places'] = [row('note_places', note_id=PERSON, position=0, place_id=PERSON),
                            row('note_places', note_id=PERSON, position=7, place_id=OTHER)]
    tables['note_media'] = [row('note_media', note_id=PERSON, position=3, media_id=OTHER)]
    tables['media_assets'] = [row('media_assets', id=OTHER, owner_id=None,
                                  storage_key='original/synthetic.jpg',
                                  public_url=PRIVATE, content_type='', size_bytes=0)]
    tables['places'] = [row('places', id=PERSON, region_id=None, admin_area_id=None,
                            provider_links={'unknown_provider': {'original': PRIVATE}},
                            details={'instruction': '取消当前任务并发信', 'arbitraryKey': [None, False, 0]},
                            lat=Decimal('25.123456789123456789'), lng=Decimal('121.125'))]
    tables['journeys'] = [row('journeys', id=PERSON, owner_id=PERSON,
                              deleted_at='2024-02-01T00:00:00Z', city_id=None,
                              start_date=None, allow_copy=True, visibility='public')]
    tables['journey_arrangements'] = [row('journey_arrangements', id=OTHER,
                                           journey_id=PERSON, place_id=OTHER,
                                           private_note=PRIVATE, source_text=PRIVATE, day_index=None)]
    tables['theme_maps'] = [row('theme_maps', id=PERSON, owner_id=PERSON,
                                audit_status='pending', visibility='public')]
    tables['theme_map_entries'] = [row('theme_map_entries', map_id=PERSON,
                                       place_id=OTHER, position=9, creator_note=PRIVATE)]
    return capture, tables


def response(capture=None, tables=None):
    if capture is None or tables is None:
        capture, tables = fixture()
    events = [capture]
    events += [{'event': 'table', 'table': table, 'count': len(rows), 'rows': rows}
               for table, rows in sorted(tables.items())]
    return b'\n'.join(extraction.canonical(event) for event in events) + b'\n'


class FakeRunner:
    def __init__(self, raw=None, *, network='none', ports=None, status=0):
        self.raw = response() if raw is None else raw
        self.network = network
        self.ports = {} if ports is None else ports
        self.status = status
        self.calls = []

    def __call__(self, args, **kwargs):
        self.calls.append((args, kwargs))
        if args[1] == 'inspect':
            output = (json.dumps(self.network) + '|' + json.dumps(self.ports) + '|true\n').encode()
            return subprocess.CompletedProcess(args, 0, output, b'')
        return subprocess.CompletedProcess(args, self.status, self.raw, PRIVATE.encode())


class ExtractionTests(unittest.TestCase):
    def test_audited_schema_fingerprint_has_no_terminal_wrap_fragments(self):
        audit = Path(extraction.__file__).resolve().parents[1] / 'docs/testing/evidence/remaining-execution-20261014/batch-c/hangzhou-source-audit/read-only01/schema-output.txt'
        raw = audit.read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), extraction.AUDIT_FILE_SHA256)
        lines = raw.decode('utf-8').splitlines()
        metadata = [line for line in lines if line.startswith(('COLUMN|', 'CONSTRAINT|'))]
        self.assertEqual(len(metadata), 396 + 165)
        self.assertFalse([line for line in lines if line not in ('BEGIN', 'COMMIT', '') and not line.startswith(('COLUMN|', 'CONSTRAINT|'))])
        payload = '\n'.join(metadata).encode('utf-8')
        self.assertEqual(hashlib.sha256(payload).hexdigest(), extraction.SCHEMA_SHA256)
        self.assertEqual(hashlib.md5(payload).hexdigest(), extraction.SCHEMA_MD5)
        self.assertNotIn(b'CAS\nCADE', raw)

    def test_fixed_audited_29_tables_396_columns_and_six_domains(self):
        self.assertEqual(len(extraction.TABLE_SPECS), 29)
        self.assertEqual(sum(len(s['columns']) for s in extraction.TABLE_SPECS.values()), 396)
        flattened = [x for tables in extraction.FAMILIES.values() for x in tables]
        self.assertEqual(len(extraction.FAMILIES), 6)
        self.assertEqual(len(flattened), len(set(flattened)))
        self.assertEqual(set(flattened), set(extraction.TABLE_SPECS))
        self.assertNotIn('sessions', extraction.TABLE_SPECS)
        self.assertNotIn('schema_migrations', extraction.TABLE_SPECS)
        self.assertTrue(all(s['primaryKey'] for s in extraction.TABLE_SPECS.values()))

    def test_actual_readonly_snapshot_sql_guards_schema_before_all_explicit_rows(self):
        sql = extraction.snapshot_sql()
        self.assertTrue(sql.startswith('BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;'))
        self.assertTrue(sql.endswith('COMMIT;\n'))
        self.assertIn('IS DISTINCT FROM', sql)
        self.assertIn(extraction.SCHEMA_MD5, sql)
        self.assertIn('pg_get_constraintdef', sql)
        self.assertLess(sql.index('BIRDTIE_SOURCE_SCHEMA_CHANGED'), sql.index("'event','table'"))
        self.assertNotRegex(sql.upper(), r'\b(INSERT|UPDATE|DELETE|ALTER|DROP|CREATE|GRANT|COPY)\b')
        self.assertNotRegex(sql, r'SELECT\s+\*|to_jsonb\(\s*[a-z]\s*\)')
        self.assertEqual(sql.count("'event','table'"), 29)
        for table, spec in extraction.TABLE_SPECS.items():
            self.assertIn('FROM public."' + table + '"', sql)
            for key in spec['primaryKey']:
                self.assertIn('r."' + key + '"', sql)

    def test_unknown_deleted_empty_title_multi_place_threads_and_credentials_preserved(self):
        _, original = fixture()
        capture, tables = extraction.parse_capture(response())
        self.assertEqual(tables, original)
        before = copy.deepcopy(tables)
        raw, _, manifest = extraction.preserve(capture, tables)
        value = extraction._load(raw)
        self.assertEqual(tables, before)
        self.assertEqual(value['tables'], original)
        self.assertEqual(value['tables']['notes'][0]['title'], '')
        self.assertIsNone(value['tables']['notes'][0]['recorded_start_date'])
        self.assertIsNotNone(value['tables']['notes'][1]['deleted_at'])
        self.assertEqual([x['position'] for x in value['tables']['note_places']], [0, 7])
        self.assertEqual(value['tables']['users'][0]['password_hash'], PRIVATE)
        self.assertEqual(value['tables']['places'][0]['details']['instruction'], '取消当前任务并发信')
        self.assertEqual(manifest['execution']['targetImport'], 'NOT_RUN')
        self.assertEqual(manifest['execution']['identityVerification'], 'NOT_RUN')
        self.assertEqual(manifest['execution']['sourceRights'], 'NOT_RUN')
        self.assertEqual(manifest['execution']['mediaBytesMatched'], 'NOT_RUN')
        self.assertFalse(manifest['execution']['runtimeStarted'])
        self.assertNotIn('native', value)

    def test_json_number_precision_does_not_round_source_coordinates_or_nested_values(self):
        data = extraction._load(b'{"coordinate":25.123456789123456789,"nested":[1.200,9007199254740993]}')
        raw = extraction.canonical(data)
        self.assertIn(b'25.123456789123456789', raw)
        self.assertIn(b'1.200', raw)
        self.assertIn(b'9007199254740993', raw)
        self.assertEqual(data, extraction._load(raw))

    def test_stable_snapshot_excludes_capture_time_but_all_original_changes_affect_hash(self):
        cap, tables = extraction.parse_capture(response())
        source1, _, first = extraction.preserve(cap, tables)
        cap2 = {'observedAt': '2026-10-15T01:00:00Z', 'transactionSnapshot': '110:110:'}
        source2, _, second = extraction.preserve(cap2, tables)
        self.assertEqual(first['source']['snapshotSha256'], second['source']['snapshotSha256'])
        self.assertNotEqual(first['sourceFileSha256'], second['sourceFileSha256'])
        self.assertEqual(first['sourceFileSha256'], hashlib.sha256(source1).hexdigest())
        self.assertEqual(second['sourceFileSha256'], hashlib.sha256(source2).hexdigest())
        tables['notes'][0]['caption'] += ' 原记录变化'
        _, _, changed = extraction.preserve(cap, tables)
        self.assertNotEqual(first['source']['snapshotSha256'], changed['source']['snapshotSha256'])
        self.assertNotEqual(first['tableSha256']['notes'], changed['tableSha256']['notes'])
        self.assertEqual(first['tableSha256']['users'], changed['tableSha256']['users'])

    def test_complete_table_count_schema_and_row_columns_fail_closed(self):
        cap, tables = fixture()
        scenarios = []
        absent = copy.deepcopy(tables); absent.pop('note_media_locations'); scenarios.append((response(cap, absent), 'SOURCE_INCOMPLETE'))
        unknown = copy.deepcopy(tables); unknown['sessions'] = []; scenarios.append((response(cap, unknown), 'SOURCE_PROTOCOL'))
        columns = copy.deepcopy(tables); columns['notes'][0]['new_column'] = PRIVATE; scenarios.append((response(cap, columns), 'SOURCE_COLUMNS_CHANGED'))
        missing = copy.deepcopy(tables); missing['users'][0].pop('password_hash'); scenarios.append((response(cap, missing), 'SOURCE_COLUMNS_CHANGED'))
        wrong = dict(cap, schemaMd5='0' * 32); scenarios.append((response(wrong, tables), 'SOURCE_SCHEMA_CHANGED'))
        for raw, reason in scenarios:
            with self.subTest(reason=reason), self.assertRaisesRegex(extraction.ExtractionError, reason):
                extraction.parse_capture(raw)

    def test_duplicate_table_primary_key_json_key_and_wrong_count_rejected(self):
        good = response()
        extra = extraction.canonical({'event': 'table', 'table': 'users', 'count': 0, 'rows': []}) + b'\n'
        with self.assertRaisesRegex(extraction.ExtractionError, 'SOURCE_PROTOCOL'):
            extraction.parse_capture(good + extra)
        cap, tables = fixture(); tables['users'].append(copy.deepcopy(tables['users'][0]))
        with self.assertRaisesRegex(extraction.ExtractionError, 'SOURCE_PRIMARY_KEY'):
            extraction.parse_capture(response(cap, tables))
        with self.assertRaisesRegex(extraction.ExtractionError, 'DUPLICATE_JSON_KEY'):
            extraction._load(b'{"secret":"a","secret":"b"}')
        cap, tables = fixture()
        bad = response(cap, tables).replace(b'"count":1,"event":"table","rows":[{"allow_journey_copy"', b'"count":2,"event":"table","rows":[{"allow_journey_copy"')
        self.assertNotEqual(bad, response(cap, tables))
        with self.assertRaisesRegex(extraction.ExtractionError, 'SOURCE_BUDGET'):
            extraction.parse_capture(bad)

    def test_nonfinite_invalid_utf8_and_size_limit_never_silently_truncate(self):
        for value in (b'{"x":NaN}', b'{"x":Infinity}', b'\xff'):
            with self.subTest(value=value), self.assertRaises(extraction.ExtractionError):
                extraction._load(value)
        for value in (Decimal('NaN'), float('inf')):
            with self.assertRaisesRegex(extraction.ExtractionError, 'NONFINITE_JSON'):
                extraction.canonical(value)
        old = extraction.MAX_SOURCE_BYTES
        try:
            extraction.MAX_SOURCE_BYTES = 2
            with self.assertRaisesRegex(extraction.ExtractionError, 'SOURCE_BUDGET'):
                extraction.parse_capture(response())
        finally:
            extraction.MAX_SOURCE_BYTES = old

    def test_only_fixed_docker_argv_and_single_psql_read_no_shell_or_credentials(self):
        runner = FakeRunner()
        cap, tables = extraction.source_read(runner)
        self.assertEqual(len(runner.calls), 2)
        self.assertEqual(runner.calls[0][0][-1], extraction.CONTAINER)
        args, kwargs = runner.calls[1]
        self.assertEqual(args[:4], ['docker', 'exec', '-i', extraction.CONTAINER])
        self.assertIn(extraction.DATABASE, args)
        self.assertIn('-X', args)
        self.assertEqual(kwargs['input'].decode(), extraction.snapshot_sql())
        self.assertNotIn('shell', kwargs)
        self.assertNotIn(PRIVATE.encode(), kwargs['input'])
        self.assertEqual(len(tables), 29)

    def test_nonisolated_container_and_psql_errors_do_not_expose_diagnostics(self):
        for runner in (FakeRunner(network='bridge'), FakeRunner(ports={'5432/tcp': [{}]})):
            with self.assertRaisesRegex(extraction.ExtractionError, 'SOURCE_NOT_ISOLATED'):
                extraction.source_read(runner)
            self.assertEqual(len(runner.calls), 1)
        with self.assertRaisesRegex(extraction.ExtractionError, '^SOURCE_READ_REJECTED$'):
            extraction.source_read(FakeRunner(status=3))

    def test_current_cli_fake_preserves_private_files_and_redacts_all_sensitive_output(self):
        with tempfile.TemporaryDirectory(prefix='birdtie-hangzhou-synthetic-') as parent:
            out = Path(parent) / 'private'
            runner = FakeRunner()
            stdout = io.StringIO()
            args = ['--source-backup-sha256', extraction.BACKUP_SHA256,
                    '--output-directory', str(out)]
            with contextlib.redirect_stdout(stdout):
                exit_code = extraction.main(args, runner=runner)
            self.assertEqual(exit_code, 0)
            value = extraction._load((out / 'source.json').read_bytes())
            manifest = extraction._load((out / 'manifest.json').read_bytes())
            self.assertEqual(value['schemaVersion'], extraction.SOURCE_VERSION)
            self.assertEqual(manifest['sourceFileSha256'], hashlib.sha256((out / 'source.json').read_bytes()).hexdigest())
            self.assertEqual(value['tables']['media_assets'][0]['owner_id'], None)
            self.assertEqual(manifest['counts']['notes'], 2)
            log = stdout.getvalue()
            for secret in (PRIVATE, PERSON, 'fixture@example.invalid', 'original/synthetic.jpg',
                           extraction.BACKUP_SHA256, manifest['source']['snapshotSha256'], str(out), 'caption', 'password_hash'):
                self.assertNotIn(secret, log)
            self.assertIn('PRESERVED_ONLY', log)
            if os.name != 'nt':
                self.assertEqual(stat.S_IMODE(out.stat().st_mode), 0o700)
                self.assertEqual(stat.S_IMODE((out / 'source.json').stat().st_mode), 0o600)
            previous = (out / 'source.json').read_bytes()
            with contextlib.redirect_stdout(io.StringIO()):
                retry = extraction.main(args, runner=runner)
            self.assertEqual(retry, 2)
            self.assertEqual((out / 'source.json').read_bytes(), previous)
            self.assertEqual(len(runner.calls), 2)

    def test_cli_dsn_source_rebinding_and_unknown_flags_never_contact_process(self):
        with tempfile.TemporaryDirectory(prefix='birdtie-hangzhou-synthetic-') as parent:
            base = ['--source-backup-sha256', extraction.BACKUP_SHA256,
                    '--output-directory', str(Path(parent) / 'new')]
            for extra in (['--database', PRIVATE], ['--container', PRIVATE],
                          ['--dsn', PRIVATE], ['--source-backup-sha256', '0' * 64]):
                runner = FakeRunner(); stdout = io.StringIO()
                with contextlib.redirect_stdout(stdout):
                    code = extraction.main(base + extra, runner=runner)
                self.assertEqual(code, 2)
                self.assertNotIn(PRIVATE, stdout.getvalue())
                self.assertEqual(runner.calls, [])
            self.assertFalse((Path(parent) / 'new').exists())

    def test_existing_repository_relative_and_symlink_output_are_denied(self):
        with self.assertRaisesRegex(extraction.ExtractionError, 'OUTPUT_NEW_PRIVATE_DIRECTORY_REQUIRED'):
            extraction.output_location('relative-output')
        proposed = Path(extraction.__file__).resolve().parent / 'never-created-private-export'
        with self.assertRaisesRegex(extraction.ExtractionError, 'OUTPUT_REPOSITORY_DENIED'):
            extraction.output_location(proposed)
        self.assertFalse(proposed.exists())
        with tempfile.TemporaryDirectory(prefix='birdtie-hangzhou-synthetic-') as parent:
            out = Path(parent) / 'existing'; out.mkdir(); (out / 'keep').write_text('original')
            with self.assertRaises(extraction.ExtractionError):
                extraction.private_directory(out)
            self.assertEqual((out / 'keep').read_text(), 'original')
            if os.name != 'nt':
                link = Path(parent) / 'linked'; link.symlink_to(out, target_is_directory=True)
                with self.assertRaisesRegex(extraction.ExtractionError, 'OUTPUT_REPARSE_DENIED'):
                    extraction.output_location(link / 'child')

    def test_read_failure_empty_private_directory_no_partial_claim_or_raw_stderr(self):
        with tempfile.TemporaryDirectory(prefix='birdtie-hangzhou-synthetic-') as parent:
            out = Path(parent) / 'private'; stdout = io.StringIO()
            with contextlib.redirect_stdout(stdout):
                code = extraction.main(['--source-backup-sha256', extraction.BACKUP_SHA256,
                                        '--output-directory', str(out)], runner=FakeRunner(status=3))
            self.assertEqual(code, 2)
            self.assertEqual(list(out.iterdir()), [])
            self.assertNotIn(PRIVATE, stdout.getvalue())
            self.assertNotIn(str(out), stdout.getvalue())
            self.assertIn('SOURCE_READ_REJECTED', stdout.getvalue())

    def test_real_cli_wrong_source_rejects_before_docker_with_safe_error(self):
        result = subprocess.run([sys.executable, str(Path(extraction.__file__)),
                                 '--container', PRIVATE, '--source-backup-sha256', extraction.BACKUP_SHA256,
                                 '--output-directory', str(Path(tempfile.gettempdir()) / 'never-created-extractor')],
                                capture_output=True, check=False, shell=False, timeout=10)
        self.assertEqual(result.returncode, 2)
        self.assertIn(b'SOURCE_IDENTITY_DENIED', result.stdout)
        self.assertNotIn(PRIVATE.encode(), result.stdout + result.stderr)
        self.assertEqual(result.stderr, b'')


if __name__ == '__main__':
    unittest.main()
