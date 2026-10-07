"""Append reviewed AGE/AIR deltas; preserve all existing task objects.

Default is a dry run. The source proposal is an audit input, not executable code.
No task is started/completed and no implementation/provider is enabled here.
"""
import argparse
import copy
import hashlib
import json
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

import taskctl

ROOT = Path(__file__).resolve().parents[1]
PLAN = ROOT / 'work/v5-queue-increment-plan.json'
QUEUE = ROOT / 'automation/codex_task_queue.json'
MAP = ROOT / 'automation/v5_requirement_mapping.json'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def candidate(data, plan):
    output = copy.deepcopy(data)
    original = copy.deepcopy(data['tasks'])
    known = {t['id']: t for t in original}
    proposed = plan['proposed_task_rows']
    proposed_ids = [t['id'] for t in proposed]
    assert len(proposed_ids) == len(set(proposed_ids)) == 108, 'Duplicate/missing proposed IDs'
    assert all(isinstance(tid, str) and tid.startswith('BT-V5-') for tid in proposed_ids), 'Invalid V5 ID'
    expected_sources = {f'AGE-{n:03d}' for n in range(1, 82)} | {f'BT-V5-AIR-{n:03d}' for n in range(1, 57)}
    mappings = plan['mapping_rows']
    assert len(mappings) == 137 and {m['source_id'] for m in mappings} == expected_sources, 'Source IDs differ'
    all_ids = set(known) | set(proposed_ids)

    def require_refs(refs):
        assert isinstance(refs, list) and all(isinstance(ref, str) and ref in all_ids for ref in refs), 'Unknown task reference'

    aliases = plan['source_dependency_aliases']
    assert isinstance(aliases, dict) and set(aliases) == expected_sources, 'Missing/unknown source alias'
    for refs in aliases.values():
        require_refs(refs)
    capabilities = plan['capability_aliases']
    assert isinstance(capabilities, dict)
    for capability in capabilities.values():
        assert isinstance(capability, dict)
        for field in ('hard_code_prerequisites', 'hard_prerequisite', 'bindings'):
            if field in capability:
                require_refs(capability[field])
    gates = plan['external_gates']
    assert gates['closed_pilot_ready'] == gates['consumer_beta_ready'] == 'NO', 'Release gate changed'
    for field in ('model_egress_enabled', 'agent_real_writes_enabled', 'vision_enabled', 'a2a_enabled'):
        assert gates[field] is False, 'Capability enabled by import'
    expected_live = {f'BT-V5-AIR-{n:03d}-LIVE' for n in (9, 12, 31, 34, 54)}
    assert set(gates['provider_live_rows']) == expected_live
    assert {tid for tid in proposed_ids if tid.endswith('-LIVE')} == expected_live
    require_refs(gates['existing_p0_pilot_refs'])
    for row in proposed:
        if row['id'] in expected_live:
            assert row['status'] == 'BLOCKED' and row['completion_scope'] == 'LIVE_ONLY' and row.get('blocked_reason')
    coverage = set()
    for mapping in mappings:
        assert mapping['operation'] in ('APPEND_DELTA', 'VERIFY_EXISTING', 'REUSE_EXISTING')
        for field in ('existing_refs', 'proposed_task_ids', 'dependency_alias_targets', 'proposed_dependencies',
                      'source_completion_requires_all', 'source_existing_refs'):
            if field in mapping:
                require_refs(mapping[field])
        for field in ('prerequisite_task_ids',):
            if field in mapping.get('verification_contract', {}):
                require_refs(mapping['verification_contract'][field])
        if mapping['operation'] == 'APPEND_DELTA':
            assert mapping['proposed_task_ids'] and set(mapping['proposed_task_ids']) <= set(proposed_ids)
            coverage.update(mapping['proposed_task_ids'])
        else:
            assert not mapping.get('proposed_task_ids')
    assert coverage == set(proposed_ids), 'Unmapped proposed task'
    for row in proposed:
        require_refs(row.get('existing_requirement_refs', []))
        assert set(row['source_ids']) <= expected_sources
        for edge in row.get('dependency_provenance', []) + row.get('in_group_dependencies', []):
            require_refs([edge['target']])
    additions = []
    for row in proposed:
        row = copy.deepcopy(row)
        if row['id'] in known:
            # Previously appended tasks may now be in progress/completed. Never
            # replace their state, evidence or fields with the proposal snapshot.
            continue
        assert row['id'].startswith('BT-V5-')
        assert row['status'] in ('TODO', 'BLOCKED') and not row.get('evidence')
        if row['id'].endswith('-LIVE'):
            assert row['status'] == 'BLOCKED' and row['completion_scope'] == 'LIVE_ONLY'
        elif 'source completion also requires -LIVE' in row.get('completion_scope', ''):
            row['title'] += '（代码/本地；live 另验）'
            row['acceptance'] = [
                ('原文整体验收由 source_requirements 保留；当前项限定代码/本地合同，真实模型请求/生产证据须关联 -LIVE 通过。' if ' 正向：' in text else text)
                for text in row['acceptance']
            ]
            row['verify'] = [
                '当前项：自有假 HTTP/合成数据验证适配、权限、schema、拒绝和恢复；无 live canary。源条件及关联 -LIVE 仍需真实证据。'
            ] + [v for v in row['verify'] if not v.startswith(row['id'] + '：')]
            row['goal'] += ' 当前只验证服务端代码与本地合同，不声称取得供应商批准、真实调用或原 source 全部完成。'
        row['requirement_mapping'] = 'automation/v5_requirement_mapping.json'
        row['ux_contract'] = 'docs/ux/GLOBAL-UX-INTERACTION-CONTRACT.md'
        row['consumer_acceptance_reference'] = 'docs/product/BirdTie-Consumer-Readiness-Assessment-2026-10-02 (1).md'
        additions.append(row)
        known[row['id']] = row
    output['tasks'].extend(additions)
    taskctl.validate(output)
    assert output['tasks'][:len(original)] == original, 'Existing rows changed'
    assert set(proposed_ids) <= set(known)
    return output, additions


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--apply', action='store_true')
    args = p.parse_args()
    plan = json.loads(PLAN.read_text(encoding='utf-8-sig'))
    for material in plan['materials']:
        assert digest(ROOT / material['path']) == material['sha256'], 'Material changed: ' + material['path']
    data = taskctl.load(QUEUE)
    checkpoint = taskctl.get_task(data, 'BT-V4-SAF-004')
    assert checkpoint['status'] == 'DONE' and 'agent-social-safety-2026-10-02' in checkpoint['evidence']
    original_hash = digest(QUEUE)
    output, additions = candidate(data, plan)
    repeated, repeated_additions = candidate(output, plan)
    assert not repeated_additions and repeated['tasks'] == output['tasks']
    review = {
        'at': datetime.now(timezone.utc).isoformat(),
        'mode': 'APPLIED' if args.apply else 'DRY_RUN', 'proposalSHA256': digest(PLAN),
        'checkpoint': 'BT-V4-SAF-004 DONE with actual code/DB/build/device evidence',
        'beforeSHA256': original_hash, 'originalRows': len(data['tasks']),
        'addedRows': len(additions), 'total': len(output['tasks']),
        'existingRowsPreservedExactly': output['tasks'][:len(data['tasks'])] == data['tasks'],
        'secondAppendNewRows': len(repeated_additions), 'taskctlValidate': 'PASS',
        'sourceOperations': dict(Counter(m['operation'] for m in plan['mapping_rows'])),
        'counts': dict(Counter(t['status'] for t in output['tasks'])),
        'p0Counts': dict(Counter(t['status'] for t in output['tasks'] if t['priority'] == 'P0')),
        'closedPilotReady': False, 'liveEnabled': False,
        'decisions': [
            'Merge only common authority contracts; every source remains mapped and retains original text.',
            'ORG050/BIZ054/AGE061 are VERIFY overlays; task DONE alone does not satisfy new authority/runtime evidence.',
            'AGE026/AIR025/AIR042 keep narrow adapters; AIR042 P2/PostPilot and disabled.',
            'Provider/vision/canary code and LIVE are separate; source completion requires all linked evidence.',
            'AGE003 uses explicit visibility types and denies undefined close-friend rules; old SAF002 blocker remains.',
            'AGE071 full suite includes P1 reinforcement; each P0 has immediate tests and AIR P0 does not wait whole catalogue.',
            'Consumer/UIUX are acceptance references with zero new functional task IDs.',
        ],
    }
    if args.apply and additions:
        assert digest(QUEUE) == original_hash, 'Queue changed during review'
        backup = ROOT / 'work/v5-queue-before-append.json'
        assert not backup.exists(), 'Backup already exists; inspect before overwriting'
        backup.write_bytes(QUEUE.read_bytes())
        mapping = {
            'schema_version': 'birdtie.v5.requirement_mapping.v1',
            'scope': 'Source coverage/aliases only; live status belongs exclusively to codex_task_queue.json.',
            'queue': 'automation/codex_task_queue.json', 'source_requirements': 137,
            'mapping_rows': plan['mapping_rows'], 'source_dependency_aliases': plan['source_dependency_aliases'],
            'capability_aliases': plan['capability_aliases'], 'canonical_constraints': plan['canonical_constraints'],
            'consumer_reference': 'docs/product/BirdTie-Consumer-Readiness-Assessment-2026-10-02 (1).md',
            'audit': 'docs/research/BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md',
            'integration_decisions': review['decisions'],
            'source_completion_not_inferred_from_task_done_alone': True,
        }
        MAP.write_text(json.dumps(mapping, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        output['v5_material_integration'] = {
            'date': '2026-10-02', 'checkpoint': 'BT-V4-SAF-004',
            'requirement_mapping': 'automation/v5_requirement_mapping.json',
            'added_task_ids': [t['id'] for t in additions],
            'source_count': 137, 'consumer_new_task_count': 0, 'uiux_new_task_count': 0,
            'closed_pilot_gate_unchanged': True,
        }
        taskctl.save(QUEUE, output)
        saved = taskctl.load(QUEUE)
        assert saved['tasks'][:len(data['tasks'])] == data['tasks']
        assert len(candidate(saved, plan)[1]) == 0
    review['afterSHA256'] = digest(QUEUE)
    if not args.apply:
        assert review['afterSHA256'] == original_hash
    (ROOT / ('work/v5-queue-append-result.json' if args.apply else 'work/v5-queue-append-dry-run.json')).write_text(
        json.dumps(review, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(json.dumps({k: review[k] for k in ('mode', 'originalRows', 'addedRows', 'total', 'existingRowsPreservedExactly', 'secondAppendNewRows', 'counts')}, ensure_ascii=False))


if __name__ == '__main__':
    main()
