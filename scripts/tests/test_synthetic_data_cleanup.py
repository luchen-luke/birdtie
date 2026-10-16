import copy
import importlib.util
import json
from pathlib import Path
import sys
import unittest

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
import audit_birdtie_synthetic_data as audit
import purge_birdtie_synthetic_data as purge


def minimal_manifest():
    value = {'protocol': audit.PROTOCOL, 'roots': copy.deepcopy(audit.ROOTS),
        'actors': list(audit.ACTORS), 'databaseOID': audit.OID, 'release': audit.RELEASE,
        'binarySHA256': audit.BINARY_SHA, 'sourceSHA256': audit.SOURCE_SHA,
        'blockers': [], 'tables': {}, 'referenceChecks': {},
        'referenceSchema': {'columns': {}, 'primaryKeys': {}, 'foreignKeys': []},
        'protectedGlobal': {}, 'deleteTriggers': [], 'deletionOrder': []}
    value['manifestSHA256'] = audit.digest(value)
    return value


class SyntheticCleanupSafetyTest(unittest.TestCase):
    def test_rehashed_expanded_roots_and_unreviewed_dependencies_are_rejected(self):
        for mutation in ('root', 'environment', 'table', 'blocker'):
            manifest = minimal_manifest()
            if mutation == 'root':
                manifest['roots']['moments'].append('00000000-0000-4000-8000-000000000001')
            elif mutation == 'environment':
                manifest['databaseOID'] = 1
            elif mutation == 'table':
                manifest['tables']['accounts'] = {}
            else:
                manifest['blockers'] = ['real-person-reference']
            manifest['manifestSHA256'] = audit.digest({k: v for k, v in manifest.items() if k != 'manifestSHA256'})
            with self.assertRaises(RuntimeError, msg=mutation):
                audit.validate_manifest(manifest)

    def test_actor_is_never_a_root_in_reference_closure(self):
        mid, actor = audit.ROOTS['moments'][0], audit.ACTORS[0]
        meta = {'columns': {'inbox_items': ['resource_id', 'recipient_account_id'],
            'moments': ['author_account_id']}, 'primaryKeys': {}, 'foreignKeys': []}
        result = audit.reference_predicates(meta, {'operations': [{'key': [actor, mid]}]})
        self.assertIn(mid, result['inbox_items'])
        self.assertNotIn(actor, result['inbox_items'])
        self.assertNotIn('moments', result)

    def test_foreign_key_order_and_cycle_fail_closed(self):
        fks = [{'table': 'conversation_messages', 'target': 'conversations'},
            {'table': 'conversation_member_states', 'target': 'conversation_messages'},
            {'table': 'conversations', 'target': 'connection_requests'}]
        order = audit.deletion_order(['conversations', 'conversation_messages',
            'conversation_member_states', 'connection_requests'], fks)
        self.assertLess(order.index('conversation_member_states'), order.index('conversation_messages'))
        self.assertLess(order.index('conversation_messages'), order.index('conversations'))
        self.assertLess(order.index('conversations'), order.index('connection_requests'))
        with self.assertRaisesRegex(RuntimeError, 'CYCLE'):
            audit.deletion_order(['a', 'b'], [{'table': 'a', 'target': 'b'}, {'table': 'b', 'target': 'a'}])

    def test_apply_requires_durable_restore_and_never_relaxes_all_triggers(self):
        manifest = minimal_manifest()
        with self.assertRaisesRegex(RuntimeError, 'MATCHING_RESTORE_PROOF_REQUIRED'):
            purge.apply(manifest, {})
        sql = purge.transaction_sql(manifest, 123)
        self.assertNotIn('session_replication_role', sql)
        self.assertNotIn('DISABLE TRIGGER ALL', sql)
        self.assertNotIn('DISABLE TRIGGER USER', sql)
        self.assertNotIn('TRUNCATE', sql)
        self.assertIn('SET CONSTRAINTS ALL IMMEDIATE', sql)
        self.assertIn('DATABASE_OID_CHANGED', sql)
        self.assertIn('COMMIT;', sql)

    def test_manifest_identifier_data_cannot_execute_sql(self):
        with self.assertRaisesRegex(RuntimeError, 'UNSAFE_IDENTIFIER'):
            audit.row_predicate(['id); DROP TABLE accounts; --'], [{'key': ['x']}])
        predicate = audit.row_predicate(['id'], [{'key': ["x' OR true --"]}])
        self.assertIn("'x'' OR true --'", predicate)

    def test_restore_compatibility_changes_header_only(self):
        header = b"SELECT pg_catalog.set_config('search_path', '', false);\n"
        output, changed = purge.restore_header_line(header, True)
        self.assertTrue(changed)
        self.assertIn(b'public,pg_catalog', output)
        self.assertEqual((header, False), purge.restore_header_line(header, False))
        row = b"a\tSELECT pg_catalog.set_config('search_path', '', false);\n"
        self.assertEqual((row, False), purge.restore_header_line(row, True))


if __name__ == '__main__':
    unittest.main()
