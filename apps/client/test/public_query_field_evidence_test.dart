import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/public_query_field_evidence.dart';
import 'package:flutter_test/flutter_test.dart';
import 'remote_public_field_evidence_test.dart' show publicEvidenceWire;

Map<String, dynamic> evidenceData([
  String label = 'SYNTHETIC_REGISTERED_PUBLIC_TASK_GET_WIRE',
]) =>
    (jsonDecode(publicEvidenceWire(label)) as Map<String, dynamic>)['data']
        as Map<String, dynamic>;
Map<String, dynamic> evidenceMetadata(Map<String, dynamic> data) =>
    (data['resultSet'] as Map<String, dynamic>)['publicFieldEvidence']
        as Map<String, dynamic>;
DateTime evidenceClock(Map<String, dynamic> data) =>
    DateTime.parse(evidenceMetadata(data)['observedAt'] as String);
PublicQueryFieldEvidence readEvidence(
  Map<String, dynamic> data, {
  DateTime Function()? now,
  bool Function()? current,
  Duration waited = Duration.zero,
}) => PublicQueryFieldEvidence.read(
  data,
  expectedOwner: data['principalId'] as String,
  now: now ?? () => evidenceClock(data),
  current: current ?? () => true,
  waited: waited,
);
// Only deliberately mutated negative/omission unit fixtures are re-budgeted.
// Original saved registered-handler wires are read without changing any byte.
void rebudget(Map<String, dynamic> data) {
  final p = evidenceMetadata(data), b = p['budget'] as Map<String, dynamic>;
  for (var n = 0; n < 8; n++) {
    final length = utf8.encode(jsonEncode(p)).length;
    if (b['used'] == length) return;
    b['used'] = length;
  }
  throw StateError('budget fixture did not converge');
}

Map<String, dynamic> firstClaim(Map<String, dynamic> data) =>
    ((evidenceMetadata(data)['fieldEvidenceSet'] as Map)['claims'] as List)
            .first
        as Map<String, dynamic>;

void main() {
  for (final label in [
    'SYNTHETIC_REGISTERED_PUBLIC_TASK_GET_WIRE',
    'SYNTHETIC_REGISTERED_CURRENT_PLACE_LATE_false_WIRE',
    'SYNTHETIC_REGISTERED_PUBLIC_METADATA_unknown_expiry_WIRE',
    'SYNTHETIC_REGISTERED_PUBLIC_METADATA_unknown_updated_WIRE',
    'SYNTHETIC_REGISTERED_PUBLIC_METADATA_expired_source_WIRE',
    'SYNTHETIC_REGISTERED_PUBLIC_METADATA_invited_WIRE',
    'SYNTHETIC_REGISTERED_PUBLIC_METADATA_missing_public_ref_WIRE',
    'SYNTHETIC_REGISTERED_PUBLIC_METADATA_empty_WIRE',
  ]) {
    test('直接解析原注册来源 wire $label', () {
      final data = evidenceData(label), p = evidenceMetadata(data);
      final result = readEvidence(data);
      expect(result.current, isTrue);
      expect(result.status, p['status']);
      expect(result.resultSetID, (data['resultSet'] as Map)['id']);
      expect(result.queryKind, p['queryKind']);
      expect(
        result.readExpiresAt!.difference(result.observedAt!),
        const Duration(seconds: 20),
      );
      if (label.contains('unknown_expiry')) {
        expect(result.times.values.single.sourceExpiresAt, isNull);
      }
      if (label.contains('expired_source') || label.contains('invited')) {
        expect(result.times, isEmpty);
      }
    });
  }
  final bad = <String, void Function(Map<String, dynamic>)>{
    '外层主体不符': (d) => d['principalType'] = 'ORGANIZATION',
    '任务主体不符': (d) => (d['task'] as Map)['principalId'] =
        '40000000-0000-4000-8000-000000000099',
    'acting user不符': (d) => (d['task'] as Map)['actingUserId'] =
        '40000000-0000-4000-8000-000000000099',
    '线上任务不得借来源': (d) => (d['task'] as Map)['contextType'] = 'ONLINE',
    'Task ID不符': (d) => d['taskId'] = '40000000-0000-4000-8000-000000000099',
    'ResultSet ID一纳秒不符': (d) => (d['resultSet'] as Map)['id'] =
        '${(d['task'] as Map)['id']}:1791305739079451901',
    '结果生成时间一纳秒不符': (d) => (d['resultSet'] as Map)['generatedAt'] =
        '2026-10-06T16:55:39.079451901Z',
    '附加主体不符': (d) => (evidenceMetadata(d)['owner'] as Map)['id'] =
        '40000000-0000-4000-8000-000000000099',
    '未知schema': (d) => evidenceMetadata(d)['schemaVersion'] = 'other',
    '未知wrapper字段': (d) => evidenceMetadata(d)['authorization'] = 'approved',
    '模型不能获准': (d) => evidenceMetadata(d)['modelAccess'] = 'AVAILABLE',
    '元数据不赋权': (d) => evidenceMetadata(d)['grantsAuthority'] = true,
    '媒体不能获准': (d) =>
        (evidenceMetadata(d)['fieldEvidenceSet'] as Map)['mediaAccess'] =
            'AVAILABLE',
    '不能伪造无冲突核验': (d) =>
        (evidenceMetadata(d)['fieldEvidenceSet'] as Map)['conflicts'] = [
          {'confirmed': true},
        ],
    '不能伪造概率': (d) => firstClaim(d)['confidence'] = 0.99,
    '不能伪造拍摄时间': (d) =>
        firstClaim(d)['capturedAt'] = evidenceMetadata(d)['observedAt'],
    '不能借人物或GPS来源': (d) =>
        (firstClaim(d)['source'] as Map)['kind'] = 'PERSON_LOCATION',
    '来源ID不符': (d) => (firstClaim(d)['source'] as Map)['id'] =
        '40000000-0000-4000-8000-000000000099',
    '来源版本不能作CAS': (d) =>
        ((firstClaim(d)['source'] as Map)['version'] as Map)['kind'] =
            'ACTIONS_SOURCE_VERSION',
    '来源version坏格式': (d) =>
        ((firstClaim(d)['source'] as Map)['version'] as Map)['token'] = 'a',
    '同组混合版本': (d) =>
        ((firstClaim(d)['source'] as Map)['version'] as Map)['token'] =
            'a' * 64,
    '私密活动不能借PUBLIC': (d) =>
        (d['activities'] as List).first['visibility'] = 'private',
    '不存在领域对象': (d) => d['activities'] = [],
    '真实来源更新时间不同': (d) =>
        ((d['activities'] as List).first['source'] as Map)['updatedAt'] =
            '2026-10-06T15:54:39Z',
    '部分字段组拒绝': (d) =>
        ((evidenceMetadata(d)['fieldEvidenceSet'] as Map)['claims'] as List)
            .removeLast(),
    '重复字段拒绝': (d) =>
        ((evidenceMetadata(d)['fieldEvidenceSet'] as Map)['claims'] as List)
            .add(firstClaim(d)),
    '未知字段拒绝': (d) => firstClaim(d)['field'] = 'activity.gps',
    'Source到期不能延长读租期': (d) =>
        evidenceMetadata(d)['validUntil'] = '2026-10-07T16:55:39.0794519Z',
    '无效日历时间': (d) => evidenceMetadata(d)['observedAt'] = '2026-02-30T16:55:39Z',
    '未来观察': (d) => evidenceMetadata(d)['observedAt'] = '2027-10-06T16:55:39Z',
    '预算计数未知key': (d) =>
        (evidenceMetadata(d)['budget'] as Map)['omitted']['OTHER'] = 1,
    '预算负数': (d) =>
        (evidenceMetadata(d)['budget'] as Map)['omitted']['EXPIRED_SOURCE'] =
            -1,
    '错误状态': (d) => evidenceMetadata(d)['status'] = 'NO_PUBLIC_FIELDS',
  };
  for (final e in bad.entries) {
    test('拒绝污染来源说明：${e.key}', () {
      final d = evidenceData();
      e.value(d);
      rebudget(d);
      expect(() => readEvidence(d), throwsFormatException);
    });
  }
  test('wrapper实际UTF8预算和100声明硬上限', () {
    final d = evidenceData();
    (evidenceMetadata(d)['budget'] as Map)['used'] = 1;
    expect(() => readEvidence(d), throwsFormatException);
    final p = evidenceData();
    (evidenceMetadata(p)['fieldEvidenceSet'] as Map)['claims'] = List.generate(
      101,
      (_) => firstClaim(d),
    );
    rebudget(p);
    expect(() => readEvidence(p), throwsFormatException);
  });
  test('预算省略是说明不足且不删除原refs', () {
    final d = evidenceData(), p = evidenceMetadata(d);
    (p['fieldEvidenceSet'] as Map)['claims'] = [];
    (p['fieldEvidenceSet'] as Map)['scope'] = 'BUDGETED_CONTEXT';
    (p['budget'] as Map)['omitted']['OMITTED_BUDGET'] = 1;
    p['status'] = 'UNAVAILABLE';
    rebudget(d);
    final result = readEvidence(d);
    expect(result.times, isEmpty);
    expect(result.omitted['OMITTED_BUDGET'], 1);
    expect(
      result.hasItem(decodeAgentResultItems(d['resultSet']).single),
      isTrue,
    );
  });
  test('当前来源通知后ABA永久退役，时钟回退不续租', () {
    final d = evidenceData();
    var current = true;
    var now = evidenceClock(d);
    final result = readEvidence(d, current: () => current, now: () => now);
    current = false;
    expect(result.current, isFalse);
    current = true;
    expect(result.current, isFalse);
    final lease = readEvidence(d, now: () => now);
    now = now.subtract(const Duration(days: 1));
    expect(lease.remaining, lessThanOrEqualTo(const Duration(seconds: 20)));
    now = evidenceClock(d).add(const Duration(seconds: 20));
    expect(lease.current, isFalse);
    now = evidenceClock(d);
    expect(lease.current, isFalse);
    expect(
      () => readEvidence(d, waited: const Duration(seconds: 20)),
      throwsFormatException,
    );
  });
}
