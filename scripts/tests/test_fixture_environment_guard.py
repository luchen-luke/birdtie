import importlib.util
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('fixture_guard', ROOT / 'scripts/fixture_environment_guard.py')
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


class FixtureEnvironmentGuardTests(unittest.TestCase):
    def test_exact_database_marker_and_explicit_opt_in_are_all_required(self):
        database = 'birdtie_fixture_direct_01'
        marker = guard.MARKER_PREFIX + database
        guard.require_fixture_database(database, marker, guard.OPT_IN)
        owned = 'birdtie_owned_migration_' + 'a1' * 12
        guard.require_fixture_database(owned, guard.MARKER_PREFIX + owned, guard.OPT_IN)
        for name, proof, opt_in in [
            ('birdtie', 'BIRDTIE_ISOLATED_FIXTURE_V1:birdtie', guard.OPT_IN),
            ('birdtie_native_hz_20261015', marker, guard.OPT_IN),
            ('birdtie_owned_migration_' + 'a' * 23, marker, guard.OPT_IN),
            ('birdtie_owned_migration_' + 'z' * 24, marker, guard.OPT_IN),
            (owned, guard.MARKER_PREFIX + owned, None),
            (database, guard.MARKER_PREFIX + 'birdtie_fixture_other', guard.OPT_IN),
            (database, marker, None),
            (database, marker, 'true'),
            (database + "'; SELECT 1;--", marker, guard.OPT_IN),
        ]:
            with self.subTest(name=name, opt_in=opt_in):
                with self.assertRaises(guard.FixtureEnvironmentError):
                    guard.require_fixture_database(name, proof, opt_in)

    def test_api_origin_needs_literal_loopback_and_separate_high_port(self):
        for origin in ['http://127.0.0.1:48651', 'http://[::1]:48651/']:
            guard.require_fixture_api_origin(origin)
        for origin in [
            'https://47.110.154.133', 'http://127.0.0.1:18114',
            'http://localhost:48651', 'http://127.0.0.1:55432',
            'http://u:p@127.0.0.1:48651', 'http://127.0.0.1:48651?target=remote',
            'http://127.0.0.1:48651/v1', 'http://127.0.0.1:48651#fragment',
        ]:
            with self.subTest(origin=origin):
                with self.assertRaises(guard.FixtureEnvironmentError):
                    guard.require_fixture_api_origin(origin)

    def test_session_sql_cannot_be_generated_without_context(self):
        database = 'birdtie_fixture_direct_01'
        marker = guard.MARKER_PREFIX + database
        with self.assertRaises(guard.FixtureEnvironmentError):
            guard.fixture_session_sql(database, marker)
        sql = guard.fixture_session_sql(database, marker, guard.OPT_IN)
        self.assertEqual(sql, "SET birdtie.fixture_opt_in = 'isolated-fixtures-v1'; "
                         "SET birdtie.fixture_database = 'birdtie_fixture_direct_01';")

    def test_retired_production_flows_have_no_opt_in_bypass(self):
        self.assertEqual(len(guard.RETIRED_PRODUCTION_SCRIPTS), 7)
        for name in guard.RETIRED_PRODUCTION_SCRIPTS:
            with self.subTest(name=name):
                with self.assertRaisesRegex(guard.FixtureEnvironmentError, 'RETIRED_USE_ISOLATED_RUNNER'):
                    guard.refuse_historical_production_fixture(name)
        with self.assertRaisesRegex(guard.FixtureEnvironmentError, 'UNREVIEWED'):
            guard.refuse_historical_production_fixture('unknown.py')

    def test_all_sql_seeds_guard_before_rows_without_changing_fixtures(self):
        guards = []
        for name in ['001_badminton.sql', '002_functional_mvp.sql', '003_community_social.sql']:
            actual = (ROOT / 'apps/api/dev-seeds' / name).read_bytes()
            original = (ROOT / 'docs/testing/evidence/dgs-20261016/injection-guards' /
                        (name + '.before.txt')).read_bytes()
            start = actual.index(b'-- DGS')
            end = actual.index(b'$birdtie_fixture_guard$;', start) + len(b'$birdtie_fixture_guard$;')
            block = actual[start:end].decode().replace('\r\n', '\n')
            self.assertLess(end, actual.upper().index(b'INSERT'))
            self.assertIn('current_database()', block)
            self.assertIn('current_user::text', block)
            self.assertIn("current_setting('birdtie.fixture_database', true)", block)
            # The only permitted seed change is the inserted guard and whitespace.
            self.assertEqual((actual[:start] + actual[end:]).split(), original.split())
            guards.append(block)
        self.assertEqual(guards[0], guards[1])
        self.assertEqual(guards[1], guards[2])


if __name__ == '__main__':
    unittest.main()
