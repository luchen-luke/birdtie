import copy
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import quarantine_birdtie_fixture_accounts as quarantine


def plan():
    return json.loads((ROOT / 'docs/testing/evidence/dgs-20261016/injection-guards/quarantine-prepare02.json').read_text(encoding='utf-8'))


class FixtureAccountQuarantineTests(unittest.TestCase):
    def test_exact_plan_accepts_only_reviewed_six_accounts_and_environment(self):
        original = plan()
        quarantine.validate_plan(original)
        for key, value in [('databaseOID', 1), ('release', '/other'), ('sourceSHA256', '0' * 64)]:
            changed = copy.deepcopy(original)
            changed[key] = value
            with self.assertRaises(RuntimeError):
                quarantine.validate_plan(changed)
        changed = copy.deepcopy(original)
        changed['actors'].pop()
        changed['manifestSHA256'] = quarantine.audit.digest({k: v for k, v in changed.items() if k not in {'observedAt', 'manifestSHA256'}})
        with self.assertRaisesRegex(RuntimeError, 'EXACT_QUARANTINE_PLAN_REQUIRED'):
            quarantine.validate_plan(changed)

    def test_status_only_transaction_keeps_triggers_and_all_other_rows(self):
        facts = {'tables': ['accounts', 'sessions', 'person_close_connections', 'moments'], 'triggers': []}
        sql = quarantine.transaction_sql(plan(), 123, facts, 'apply')
        self.assertIn("UPDATE accounts SET status='suspended' WHERE id IN(", sql)
        self.assertIn('QUARANTINE_DATABASE_OID_CHANGED', sql)
        self.assertIn('QUARANTINE_OTHER_ROWS_CHANGED', sql)
        self.assertIn('QUARANTINE_TRIGGER_CODE_CHANGED', sql)
        self.assertIn('SET CONSTRAINTS ALL IMMEDIATE', sql)
        for forbidden in ['DELETE FROM', 'TRUNCATE', 'DISABLE TRIGGER', 'session_replication_role', 'UPDATE sessions', 'SET status=\'active\'']:
            self.assertNotIn(forbidden, sql)

    def test_restore_requires_exact_post_apply_rows_and_restores_only_status(self):
        facts = {'tables': ['accounts', 'sessions', 'person_close_connections'], 'triggers': []}
        with self.assertRaisesRegex(RuntimeError, 'EXACT_STATUS_SNAPSHOT_REQUIRED'):
            quarantine.transaction_sql(plan(), 123, facts, 'restore')
        previous = [{**r, 'status': 'suspended'} for r in quarantine.original_rows(plan())]
        restore = quarantine.transaction_sql(plan(), 123, facts, 'restore', previous)
        self.assertIn('QUARANTINE_ACCOUNT_ROWS_CHANGED', restore)
        self.assertIn("UPDATE accounts SET status='active'", restore)
        self.assertIn('QUARANTINE_RESTORE_MISMATCH', restore)
        verify = quarantine.transaction_sql(plan(), 123, facts, 'verify', previous)
        self.assertNotIn('UPDATE accounts SET', verify)
        self.assertIn('ALREADY_QUARANTINED', verify)

    def test_production_apply_rejects_missing_restore_without_side_effects(self):
        with self.assertRaisesRegex(RuntimeError, 'MATCHING_QUARANTINE_RESTORE_REQUIRED'):
            quarantine.apply(plan(), {})

    def test_identity_recheck_and_complete_snapshot_never_select_by_author(self):
        sql = quarantine.no_new_identity_sql()
        for proof in ['validSessions', 'fixedIdentities', 'civuUsers', 'phoneBindings', 'emailBindings', 'activeClose']:
            self.assertIn(proof, sql)
        snapshot = quarantine.snapshot_sql(['accounts', 'moments'])
        self.assertEqual(snapshot.count('WHERE'), 1)
        self.assertIn('WHERE t.id NOT IN(', snapshot)
        self.assertNotIn('author_account_id', snapshot)
        with self.assertRaisesRegex(RuntimeError, 'UNSAFE_IDENTIFIER'):
            quarantine.snapshot_sql(['accounts; DROP TABLE moments'])


if __name__ == '__main__':
    unittest.main()
