import 'dart:convert';
import 'package:birdtie_client/src/workspace/model_egress_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const egressOwner = '11111111-1111-4111-8111-111111111111';
const egressTask = '22222222-2222-4222-8222-222222222222';
const egressRoot = '33333333-3333-4333-8333-333333333333';
const egressPreviewID = '44444444-4444-4444-8444-444444444444';
const egressAgent = '55555555-5555-4555-8555-555555555555';
String egressStamp([DateTime? t]) =>
    (t ?? DateTime.now().toUtc().add(const Duration(minutes: 1)))
        .toIso8601String();
Map<String, dynamic> egressTarget() => {
  'destination': {
    'Provider': 'fake',
    'Model': 'local',
    'Version': 'v1',
    'WireContract': 'offline.v1',
  },
  'region': 'UK',
  'retention': 'NO_STATE_NO_STORAGE',
  'currency': 'GBP',
  'evidence': 'LOCAL_SYNTHETIC',
};
Map<String, dynamic> egressOptionWire() => {
  'rootTraceId': egressRoot,
  'taskId': egressTask,
  'taskQuery': '帮我找周末羽毛球',
  'priceVersion': 'local.v1',
  'configurationVersion': 'config.v1',
  'promptVersion': 'prompt.v1',
  'inputSchemaVersion': 'air.messages.v1',
  'outputSchemaVersion': 'air.answer.v1',
  ...egressTarget(),
  'maxOutputTokens': 128,
  'maxDeadlineAt': egressStamp(),
};
Map<String, dynamic> egressOptionsWire([List<dynamic>? options]) => {
  'schemaVersion': 'air.model_egress_human.v1',
  'ownerId': egressOwner,
  'observedAt': DateTime.now().toUtc().toIso8601String(),
  'options': options ?? [egressOptionWire()],
  'modelAccess': 'UNAVAILABLE',
};
Map<String, dynamic> egressPreviewWire({
  String? deadline,
  String status = 'DRAFT',
}) {
  deadline ??= egressStamp();
  return {
    'schemaVersion': 'air.model_egress_budget.v1',
    'status': status,
    'id': egressPreviewID,
    'requestDigest': 'a' * 64,
    'scope': 'SELF_TASK_QUERY',
    'purpose': 'MODEL_CONTEXT_EGRESS',
    'priceVersion': 'local.v1',
    ...egressTarget(),
    'request': {
      'schema_version': 'air.model_request.v1',
      'run_id': egressAgent,
      'agent_ref': {
        'AgentID': egressAgent,
        'Principal': {'type': 'PERSON', 'id': egressOwner},
        'Role': 'PERSONAL',
      },
      'task_kind': 'ACTIVITY_QUERY',
      'prompt_version': 'prompt.v1',
      'input_schema_version': 'air.messages.v1',
      'output_schema_version': 'air.answer.v1',
      'context_snapshot_ref': egressTask,
      'data_policy_ref': egressRoot,
      'budget_ref': egressRoot,
      'budget': {'max_output_tokens': 64},
      'messages': [
        {'role': 'system', 'content': '合成系统提示词；不执行外部动作。'},
        {'role': 'user', 'content': '帮我找周末羽毛球'},
      ],
      'output_mode': 'STRUCTURED',
      'tool_allowlist': [],
      'capabilities_required': ['text'],
      'deadline_at': deadline,
    },
    'upper': {'inputTokens': 100, 'outputTokens': 64, 'costMicros': 392},
    'expiresAt': deadline,
    'priceExpiresAt': egressStamp(
      DateTime.now().toUtc().add(const Duration(hours: 1)),
    ),
    'inputMicrosPerToken': 2,
    'outputMicrosPerToken': 3,
    'modelAccess': 'UNAVAILABLE',
  };
}

Map<String, dynamic> egressReceiptWire({
  String status = 'DRAFT',
  Map<String, dynamic>? review,
  bool withReview = false,
}) {
  final p = review ?? (withReview ? egressPreviewWire(status: status) : null);
  return {
    'schemaVersion': 'air.model_egress_human.v1',
    'ownerId': egressOwner,
    'previewId': egressPreviewID,
    'rootTraceId': egressRoot,
    'taskId': egressTask,
    'priceVersion': 'local.v1',
    'maxOutputTokens': 64,
    'status': status,
    'revision': status == 'DRAFT'
        ? 1
        : status == 'APPROVED'
        ? 2
        : 3,
    'createdAt': DateTime.now().toUtc().toIso8601String(),
    'expiresAt': p?['expiresAt'] ?? egressStamp(),
    'reviewable': p != null,
    'approvable': status == 'DRAFT' && p != null,
    'revocable': status != 'REVOKED',
    'modelAccess': 'UNAVAILABLE',
    'review': ?p,
  };
}

List<Map<String, dynamic>> egressBudgetsWire() => [
  for (final scope in ['TENANT_PERSON', 'SUBJECT_PERSON', 'ROOT', 'TASK'])
    {
      'scope': scope,
      'limits': <String, dynamic>{
        'requests': 4,
        'inputTokens': 10000,
        'outputTokens': 10000,
        'costMicros': 1000000,
      },
      'allocated': <String, dynamic>{
        'requests': 0,
        'inputTokens': 0,
        'outputTokens': 0,
        'costMicros': 0,
      },
      'currency': 'GBP',
    },
];
http.Response egressResponse(dynamic data, {int status = 200}) => http.Response(
  jsonEncode({'data': data, 'modelAccess': 'UNAVAILABLE'}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
void main() {
  test(
    'actual human DTO accepts signed offset response and rejects cost/provenance changes',
    () {
      final utc = egressTime(egressStamp());
      final offset =
          '${utc.add(const Duration(hours: 8)).toIso8601String().replaceFirst('Z', '')}+08:00';
      final p = egressPreviewWire(deadline: offset);
      final r = EgressReceipt(egressReceiptWire(review: p), egressOwner);
      expect(r.review!.expiry, utc);
      for (final mutate in <void Function(Map<String, dynamic>)>[
        (v) {
          v['upper']['costMicros'] = 1;
        },
        (v) {
          v['request']['agent_ref']['Role'] = 'ORGANIZATION';
        },
        (v) {
          v['request']['data_policy_ref'] = egressTask;
        },
        (v) {
          v['request']['messages'][0]['role'] = 'user';
        },
        (v) {
          v['request']['budget']['max_output_tokens'] = 63;
        },
      ]) {
        final bad = egressPreviewWire(deadline: egressStamp());
        mutate(bad);
        expect(() => EgressPreview(bad, egressOwner), throwsFormatException);
      }
    },
  );
  test(
    'RFC3339 accepts Z and signed offsets, rejects normalized invalid dates',
    () {
      expect(
        egressTime('2026-10-04T12:00:00+08:00'),
        egressTime('2026-10-04T04:00:00Z'),
      );
      expect(
        egressTime('2026-10-04T00:30:00-03:30'),
        egressTime('2026-10-04T04:00:00Z'),
      );
      for (final v in [
        '2026-02-30T00:00:00Z',
        '2026-13-01T00:00:00Z',
        '2026-10-04T24:00:00Z',
        '2026-10-04T00:00:60Z',
        '2026-10-04T00:00:00+24:00',
        '2026-10-04T00:00:00+08:60',
        '2026-10-04T00:00:00',
        '2026-10-04T00:00:00+0800',
        '0000-01-01T00:00:00Z',
      ]) {
        expect(() => egressTime(v), throwsFormatException, reason: v);
      }
    },
  );
  test('preview binds owner and immutable fields with no tool context', () {
    final deadline = egressStamp();
    final p = EgressPreview(egressPreviewWire(deadline: deadline), egressOwner);
    expect(p.taskID, egressTask);
    expect(() => p.data['request']['messages'].add({}), throwsUnsupportedError);
    expect(
      () => EgressPreview(
        egressPreviewWire(deadline: deadline),
        '66666666-6666-4666-8666-666666666666',
      ),
      throwsFormatException,
    );
    final bad = egressPreviewWire(deadline: deadline);
    bad['request']['tool_allowlist'] = ['send'];
    expect(() => EgressPreview(bad, egressOwner), throwsFormatException);
  });
  test('stale or revoked receipt cannot attach an approval body', () {
    final p = egressPreviewWire(deadline: egressStamp());
    final r = egressReceiptWire(review: p);
    r['reviewable'] = false;
    expect(() => EgressReceipt(r, egressOwner), throwsFormatException);
    final revoked = egressReceiptWire(status: 'REVOKED');
    revoked['approvable'] = true;
    expect(() => EgressReceipt(revoked, egressOwner), throwsFormatException);
    final wrong = egressReceiptWire();
    wrong['ownerId'] = egressTask;
    expect(() => EgressReceipt(wrong, egressOwner), throwsFormatException);
  });
  test(
    'API submits only original concrete preview selector and consumes budget scopes',
    () async {
      final seen = <http.Request>[];
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          seen.add(r);
          if (r.url.path.endsWith('/options')) {
            return egressResponse(egressOptionsWire());
          }
          if (r.url.path.endsWith('/budget')) {
            return egressResponse(egressBudgetsWire());
          }
          if (r.method == 'POST' && r.url.path.endsWith('/previews')) {
            return egressResponse(
              egressPreviewWire(
                deadline: (jsonDecode(r.body) as Map)['deadlineAt'],
              ),
            );
          }
          return egressResponse({
            'previewId': egressPreviewID,
            'status': r.method == 'DELETE' ? 'REVOKED' : 'APPROVED',
            'evidence': 'LOCAL_SYNTHETIC',
            'modelAccess': 'UNAVAILABLE',
          });
        }),
      );
      final o = (await api.options('Bearer own', egressOwner)).single;
      expect(await api.budgets('Bearer own', o.rootID, o.taskID), hasLength(4));
      final p = await api.preview(
        'Bearer own',
        egressOwner,
        o,
        64,
        egressTime(o.data['maxDeadlineAt']),
      );
      await api.approve('Bearer own', p);
      await api.revoke('Bearer own', p.id);
      expect(
        (jsonDecode(seen[2].body) as Map).keys,
        unorderedEquals([
          'rootTraceId',
          'taskId',
          'priceVersion',
          'maxOutputTokens',
          'deadlineAt',
        ]),
      );
      expect(
        (jsonDecode(seen[3].body) as Map).keys,
        unorderedEquals(['previewId', 'requestDigest']),
      );
      expect(seen.last.body, isEmpty);
      expect(
        seen.every((r) => r.headers['Authorization'] == 'Bearer own'),
        isTrue,
      );
    },
  );
  test('budget read rejects duplicated scope and noninteger amount', () async {
    for (final bad in [
      egressBudgetsWire()..[3]['scope'] = 'ROOT',
      egressBudgetsWire()..[0]['allocated']['costMicros'] = 1.5,
    ]) {
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((_) async => egressResponse(bad)),
      );
      await expectLater(
        api.budgets('token', egressRoot, egressTask),
        throwsFormatException,
      );
    }
  });
}
