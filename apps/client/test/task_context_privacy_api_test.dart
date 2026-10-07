import 'dart:convert';
import 'package:birdtie_client/src/workspace/task_context_privacy_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

const taskOwner = '81000000-0000-4000-8000-000000000001',
    taskAgent = '81000000-0000-4000-8000-000000000002',
    taskGrantID = '81000000-0000-4000-8000-000000000003',
    taskID = '81000000-0000-4000-8000-000000000004';
Map<String, dynamic> taskGrantWire(
  DateTime at, {
  int revision = 1,
  bool revoked = false,
  bool expired = false,
}) => {
  'id': taskGrantID,
  'revision': revision,
  'purpose': 'TASK_CONTEXT_READ',
  'taskId': taskID,
  'cityId': 'aberdeen',
  'taskUpdatedAt': at.subtract(const Duration(minutes: 3)).toIso8601String(),
  'profileFields': ['personalPreferences'],
  'memoryIds': <String>[],
  'placeIds': <String>[],
  'activityIds': <String>[],
  'relationshipTieIds': <String>[],
  'policyFamilies': <String>[],
  'createdAt': at.subtract(const Duration(minutes: 2)).toIso8601String(),
  'expiresAt': at.add(Duration(seconds: expired ? -1 : 60)).toIso8601String(),
  if (revoked) 'revokedAt': at.toIso8601String(),
};
Map<String, dynamic> taskInventoryWire(DateTime at, {List<dynamic>? grants}) =>
    {
      'schemaVersion': 'agent-task-context-inventory-v1',
      'owner': {'type': 'PERSON', 'id': taskOwner},
      'agentId': taskAgent,
      'observedAt': at.toIso8601String(),
      'validUntil': at.add(const Duration(seconds: 30)).toIso8601String(),
      'limit': 50,
      'truncated': false,
      'grants': grants ?? [taskGrantWire(at)],
    };
Map<String, dynamic> taskOriginalWire(Map<String, dynamic> g) => {
  for (final k in [
    'id',
    'revision',
    'purpose',
    'createdAt',
    'expiresAt',
    'revokedAt',
  ])
    if (g.containsKey(k)) k: g[k],
  'selection': {
    'agentId': taskAgent,
    'queryDigest': 'a' * 64,
    'deadlineAt': g['expiresAt'],
    for (final k in [
      'taskId',
      'cityId',
      'taskUpdatedAt',
      'profileFields',
      'memoryIds',
      'placeIds',
      'activityIds',
      'relationshipTieIds',
      'policyFamilies',
    ])
      k: g[k],
  },
  'sources': <dynamic>[],
};
http.StreamedResponse taskResponse(dynamic data, {int status = 200}) =>
    http.StreamedResponse(
      Stream.value(utf8.encode(jsonEncode({'data': data}))),
      status,
    );

class TaskPrivacyWire extends http.BaseClient {
  TaskPrivacyWire(this.handler);
  Future<http.StreamedResponse> Function(http.BaseRequest) handler;
  final sent = <http.BaseRequest>[];
  bool closed = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    sent.add(r);
    return handler(r);
  }

  @override
  void close() {
    closed = true;
    super.close();
  }
}

void main() {
  test('清单严格本人元数据，默认GET无其它许可或正文', () async {
    final at = DateTime.now().toUtc(),
        wire = TaskPrivacyWire(
          (r) async => taskResponse(taskInventoryWire(DateTime.now().toUtc())),
        );
    final api = TaskContextPrivacyAPI(
      client: wire,
      apiBaseUrl: 'https://context.test',
    );
    final v = await api.list('Bearer unit', taskOwner);
    expect(v.grants.single.profileFields, ['personalPreferences']);
    expect(wire.sent.single.url.path, taskContextPrivacyPath);
    expect(wire.sent.single.method, 'GET');
    expect(
      wire.sent.single.headers.containsKey('X-Birdtie-Organization-Workspace'),
      false,
    );
    expect((wire.sent.single as http.Request).body, isEmpty);
    api.dispose();
    expect(wire.closed, false);
    expect(taskGrantWire(at).containsKey('queryDigest'), false);
  });
  test('元数据不接受跨本人、未知用途、私人额外字段或假截断', () {
    final at = DateTime.now().toUtc();
    for (final edit in <void Function(Map<String, dynamic>)>[
      (v) => v['owner'] = {'type': 'PERSON', 'id': taskAgent},
      (v) => (v['grants'][0] as Map)['purpose'] = 'MODEL_EGRESS',
      (v) => v['queryDigest'] = 'a' * 64,
      (v) => v['truncated'] = true,
    ]) {
      final v = taskInventoryWire(at);
      edit(v);
      expect(
        () => TaskContextPrivacyInventory.read(v, taskOwner),
        throwsFormatException,
      );
    }
    expect(
      TaskContextPrivacyInventory.read(
        taskInventoryWire(at, grants: []),
        taskOwner,
      ).grants,
      isEmpty,
    );
  });
  test('原GET和DELETE沿用具体ID版本，完整响应不能冒充另一许可', () async {
    final at = DateTime.now().toUtc(),
        grant = TaskContextPrivacyGrant.read(
          taskGrantWire(DateTime.now().toUtc()),
          DateTime.now().toUtc(),
        );
    final g = taskGrantWire(at);
    final original = TaskContextPrivacyGrant.read(g, at);
    final wire = TaskPrivacyWire(
      (r) async => taskResponse(
        taskOriginalWire(
          r.method == 'DELETE'
              ? {...g, 'revision': 2, 'revokedAt': at.toIso8601String()}
              : g,
        ),
      ),
    );
    final api = TaskContextPrivacyAPI(
      client: wire,
      apiBaseUrl: 'https://context.test',
    );
    expect(
      (await api.readGrant('Bearer unit', taskAgent, original, () => at)).id,
      taskGrantID,
    );
    expect(
      (await api.revoke('Bearer unit', taskAgent, original, () => at)).revoked,
      true,
    );
    expect(jsonDecode((wire.sent.last as http.Request).body), {
      'expectedRevision': 1,
    });
    expect(wire.sent.last.url.path, '$taskContextPrivacyPath/$taskGrantID');
    expect(grant.revision, 1);
    wire.handler = (r) async =>
        taskResponse(taskOriginalWire({...g, 'id': taskAgent}));
    await expectLater(
      api.readGrant('Bearer unit', taskAgent, original, () => at),
      throwsFormatException,
    );
    api.dispose();
  });
  test('服务不可用与超界响应是真失败而非空清单', () async {
    final wire = TaskPrivacyWire((r) async => taskResponse(null, status: 403));
    final api = TaskContextPrivacyAPI(
      client: wire,
      apiBaseUrl: 'https://context.test',
    );
    await expectLater(
      api.list('Bearer unit', taskOwner),
      throwsA(
        isA<TaskContextPrivacyHTTPError>().having(
          (e) => e.status,
          'status',
          403,
        ),
      ),
    );
    wire.handler = (r) async =>
        http.StreamedResponse(Stream.value(List.filled(65537, 65)), 200);
    await expectLater(
      api.list('Bearer unit', taskOwner),
      throwsFormatException,
    );
    api.dispose();
  });
}
