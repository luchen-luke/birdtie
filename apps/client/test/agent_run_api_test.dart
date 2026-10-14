import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_run_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const runOwner = '11111111-1111-4111-8111-111111111111',
    runAgent = '22222222-2222-4222-8222-222222222222',
    runID = '33333333-3333-4333-8333-333333333333';
Map<String, dynamic> runRecord({
  String id = runID,
  String state = 'WAITING_CONFIRMATION',
  int version = 1,
}) {
  final now = DateTime.now().toUtc();
  return {
    'schemaVersion': 'agent-enrichment-run-v1',
    'id': id,
    'owner': {'type': 'PERSON', 'id': runOwner},
    'agentId': runAgent,
    'source': {
      'type': 'MOMENT',
      'id': '44444444-4444-4444-8444-444444444444',
      'owner': {'type': 'PERSON', 'id': runOwner},
      'version': {'kind': 'REVISION', 'revision': 1},
    },
    'eventId': '55555555-5555-4555-8555-555555555555',
    'logicalOperationId': '66666666-6666-4666-8666-666666666666',
    'state': state,
    'checkpoint': state == 'SUCCEEDED' ? 'EFFECT_CONFIRMED' : 'SCHEDULED',
    'version': version,
    'attempt': 0,
    'reason': {
      'WAITING_CONFIRMATION': 'WAITING_APPROVAL',
      'CANCELLED': 'CANCELLED',
      'SUCCEEDED': 'COMMITTED',
      'QUEUED': 'QUEUED',
      'RUNNING': 'DISPATCHING',
      'FAILED': 'AUTHORITY_CHANGED',
      'EXPIRED': 'EXPIRED',
      'RETRY_WAIT': 'RETRY_UNAVAILABLE',
    }[state],
    'createdAt': now.subtract(const Duration(minutes: 1)).toIso8601String(),
    'updatedAt': now.toIso8601String(),
    'observedAt': now.toIso8601String(),
    'deadline': now.add(const Duration(minutes: 5)).toIso8601String(),
    'committed': state == 'SUCCEEDED',
    'modelAccess': false,
    'memoryPromotionAllowed': false,
    if (['QUEUED', 'RUNNING', 'RETRY_WAIT', 'SUCCEEDED'].contains(state))
      'retentionGrantId': '77777777-7777-4777-8777-777777777777',
    if (state == 'SUCCEEDED')
      'candidateId': '88888888-8888-4888-8888-888888888888',
  };
}

Map<String, dynamic> runPage({
  List<Map<String, dynamic>>? items,
  String? cursor,
}) {
  final now = DateTime.now().toUtc();
  return {
    'schema': 'agent-run-page-v1',
    'owner': {'type': 'PERSON', 'id': runOwner},
    'agentId': runAgent,
    'items': items ?? [runRecord()],
    'nextCursor': cursor,
    'observedAt': now.add(const Duration(milliseconds: 10)).toIso8601String(),
    'expiresAt': now.add(const Duration(minutes: 1)).toIso8601String(),
    'modelAccess': false,
  };
}

http.Response runResponse(dynamic value, {int code = 200}) => http.Response(
  jsonEncode(value),
  code,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
void main() {
  test('bare原Record与page闭集，metadata不授Memory或来源正文', () {
    expect(
      AgentRunRecord.read(runRecord(state: 'SUCCEEDED'), runOwner).state,
      'SUCCEEDED',
    );
    expect(AgentRunPageData.read(runPage(), runOwner).items.length, 1);
    for (final key in ['body', 'workerId', 'confirmed']) {
      final j = runRecord()..[key] = 'x';
      expect(() => AgentRunRecord.read(j, runOwner), throwsFormatException);
    }
    final j = runPage()..['modelAccess'] = true;
    expect(() => AgentRunPageData.read(j, runOwner), throwsFormatException);
    expect(
      () => AgentRunRecord.read({'data': runRecord()}, runOwner),
      throwsFormatException,
    );
  });
  test('只GET元数据与精确expectedVersion取消，不创建或恢复Run', () async {
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return runResponse(
        r.url.path.endsWith('/page')
            ? runPage()
            : runRecord(
                state: r.method == 'POST'
                    ? 'CANCELLED'
                    : 'WAITING_CONFIRMATION',
                version: r.method == 'POST' ? 2 : 1,
              ),
      );
    });
    final api = AgentRunAPI(client: client, apiBaseUrl: 'http://local');
    try {
      await api.page('Bearer synthetic', runOwner);
      final r = await api.read('Bearer synthetic', runOwner, runID);
      await api.cancel('Bearer synthetic', runOwner, r);
      expect(calls.map((r) => r.url.path), [
        '/v1/me/agent-runs/page',
        '/v1/me/agent-runs/$runID',
        '/v1/me/agent-runs/$runID/cancel',
      ]);
      expect(
        calls.where((r) => r.method == 'GET').every((r) => r.body.isEmpty),
        true,
      );
      expect(jsonDecode(calls.last.body), {'expectedVersion': 1});
    } finally {
      api.dispose();
      client.close();
    }
  });
}
