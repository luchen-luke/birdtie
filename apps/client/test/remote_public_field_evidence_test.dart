import 'dart:convert';
import 'dart:io';
import 'dart:async';

import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// Immutable actual registered Go-handler SYNTHETIC_TRANSPORT bodies, not
// native PostgreSQL, live activities, production identity or deployment.
String publicEvidenceWire([
  String label = 'SYNTHETIC_REGISTERED_PUBLIC_TASK_GET_WIRE',
]) => File(
  '../../docs/testing/evidence/public-rules-field-evidence-2026-10-07/freeze06/wires/$label.json',
).readAsStringSync();
Map<String, dynamic> publicEvidenceData() =>
    (jsonDecode(publicEvidenceWire()) as Map<String, dynamic>)['data']
        as Map<String, dynamic>;

void main() {
  test('公开来源原注册响应 places null 仍保留同一活动结果', () async {
    final raw = publicEvidenceWire();
    final data = publicEvidenceData();
    expect(data['places'], isNull);
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => 'Bearer synthetic-private',
      client: MockClient((r) async {
        expect(r.method, 'GET');
        expect(r.url.path, '/v1/me/agent-tasks/${(data['task'] as Map)['id']}');
        return http.Response(
          raw,
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }),
      apiBaseUrl: 'https://public-evidence-fixture.test',
    );
    final result = await source.readByID(
      (data['task'] as Map)['id'] as String,
      ownerID: data['principalId'] as String,
    );
    expect(
      result.projectionItems!.single.entity.id,
      ((data['resultSet'] as Map)['entities'] as List).single['id'],
    );
    expect(result.activities.single.title, '同一公开活动');
    expect(result.places, isEmpty);
    source.dispose();
  });
  for (final label in [
    'SYNTHETIC_REGISTERED_PUBLIC_TASK_GET_WIRE',
    'SYNTHETIC_REGISTERED_CURRENT_PLACE_LATE_false_WIRE',
    'SYNTHETIC_REGISTERED_PUBLIC_METADATA_invited_WIRE',
    'SYNTHETIC_REGISTERED_PUBLIC_METADATA_expired_source_WIRE',
  ]) {
    test('真实Remote消费原注册响应并保留元数据 $label', () async {
      final raw = publicEvidenceWire(label);
      final d = (jsonDecode(raw) as Map)['data'] as Map<String, dynamic>;
      final p = (d['resultSet'] as Map)['publicFieldEvidence'] as Map;
      var calls = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => 'Bearer synthetic-private',
        publicEvidenceOwnerID: () => d['principalId'] as String,
        publicEvidenceEpoch: () => 1,
        publicEvidenceNow: () => DateTime.parse(p['observedAt'] as String),
        client: MockClient((r) async {
          calls++;
          return http.Response(
            raw,
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }),
        apiBaseUrl: 'https://public-evidence-fixture.test',
      );
      final result = await source.readByID(
        d['taskId'] as String,
        ownerID: d['principalId'] as String,
      );
      expect(calls, 1);
      expect(result.resultSet!.publicFieldEvidence!.current, isTrue);
      expect(result.resultSet!.publicFieldEvidence!.status, p['status']);
      expect(
        result.projectionItems!.single.entity.id,
        ((d['resultSet'] as Map)['entities'] as List).single['id'],
      );
      expect(result.activities.length + result.places.length, 1);
      if (label.contains('invited')) {
        expect((d['activities'] as List).single['visibility'], 'invite_only');
      }
      source.dispose();
      expect(result.resultSet!.publicFieldEvidence!.current, isFalse);
    });
  }
  test('没有附加字段保持旧结果；坏附加字段只降级说明', () async {
    for (final malformed in [false, true]) {
      final d = publicEvidenceData(), set = d['resultSet'] as Map;
      if (malformed) {
        set['publicFieldEvidence'] = {'authorization': 'approved'};
      } else {
        set.remove('publicFieldEvidence');
      }
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => 'Bearer synthetic-private',
        publicEvidenceOwnerID: () => d['principalId'] as String,
        publicEvidenceEpoch: () => 1,
        client: MockClient(
          (_) async => http.Response(
            jsonEncode({'data': d}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          ),
        ),
        apiBaseUrl: 'https://public-evidence-fixture.test',
      );
      final result = await source.readByID(
        d['taskId'] as String,
        ownerID: d['principalId'] as String,
      );
      expect(result.activities.single.title, '同一公开活动');
      expect(
        result.projectionItems!.single.detail!.id,
        result.activities.single.id,
      );
      if (malformed) {
        expect(result.resultSet!.publicFieldEvidence!.invalid, isTrue);
        expect(result.resultSet!.publicFieldEvidence!.times, isEmpty);
      } else {
        expect(result.resultSet!.publicFieldEvidence, isNull);
      }
      source.dispose();
    }
  });
  test('仅活动地点null兼容，坏nonlist及缺失与原必需列表拒绝', () async {
    for (final key in ['activities', 'places', 'people', 'groups']) {
      for (final missing in [false, true]) {
        final d = publicEvidenceData();
        if (missing) {
          d.remove(key);
        } else {
          d[key] = {'not': 'a list'};
        }
        final source = RemoteAgentTaskSource(
          cityID: () => 'aberdeen-gb',
          authorizationHeader: () => 'Bearer synthetic-private',
          client: MockClient(
            (_) async => http.Response(
              jsonEncode({'data': d}),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            ),
          ),
          apiBaseUrl: 'https://public-evidence-fixture.test',
        );
        await expectLater(
          source.readByID(
            d['taskId'] as String,
            ownerID: d['principalId'] as String,
          ),
          throwsA(anything),
        );
        source.dispose();
      }
    }
    final d = publicEvidenceData();
    d['activities'] = null;
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => 'Bearer synthetic-private',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({'data': d}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      ),
      apiBaseUrl: 'https://public-evidence-fixture.test',
    );
    expect(
      (await source.readByID(
        d['taskId'] as String,
        ownerID: d['principalId'] as String,
      )).activities,
      isEmpty,
    );
    source.dispose();
  });
  test('请求前owner和epoch绑定，身份来源ABA不能附回旧说明', () async {
    final d = publicEvidenceData(),
        p = (d['resultSet'] as Map)['publicFieldEvidence'] as Map;
    var owner = d['principalId'] as String, epoch = 1;
    final response = Completer<http.Response>();
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => 'Bearer synthetic-private',
      publicEvidenceOwnerID: () => owner,
      publicEvidenceEpoch: () => epoch,
      publicEvidenceNow: () => DateTime.parse(p['observedAt'] as String),
      client: MockClient((_) => response.future),
      apiBaseUrl: 'https://public-evidence-fixture.test',
    );
    final future = source.readByID(d['taskId'] as String, ownerID: owner);
    owner = '40000000-0000-4000-8000-000000000099';
    epoch++;
    owner = d['principalId'] as String;
    epoch++;
    response.complete(
      http.Response(
        publicEvidenceWire(),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    final result = await future;
    expect(result.activities.single.title, '同一公开活动');
    expect(result.resultSet!.publicFieldEvidence!.current, isFalse);
    expect(result.resultSet!.publicFieldEvidence!.times, isEmpty);
    source.dispose();
  });
  test('原readByID token和组织晚变更仍拒绝整响应', () async {
    for (final changeToken in [false, true]) {
      final d = publicEvidenceData();
      var token = 'Bearer synthetic-private';
      String? org;
      final response = Completer<http.Response>();
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => token,
        organizationWorkspaceID: () => org,
        publicEvidenceOwnerID: () => d['principalId'] as String,
        publicEvidenceEpoch: () => 1,
        client: MockClient((_) => response.future),
        apiBaseUrl: 'https://public-evidence-fixture.test',
      );
      final future = source.readByID(
        d['taskId'] as String,
        ownerID: d['principalId'] as String,
      );
      final assertion = expectLater(future, throwsA(anything));
      if (changeToken) {
        token = 'Bearer another-synthetic';
      } else {
        org = '40000000-0000-4000-8000-000000000099';
      }
      response.complete(
        http.Response(
          publicEvidenceWire(),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      );
      await assertion;
      source.dispose();
    }
  });
  test('原resolve和restore同样消费可选说明不派发新动作', () async {
    final d = publicEvidenceData(),
        p = (d['resultSet'] as Map)['publicFieldEvidence'] as Map;
    final methods = <String>[];
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => 'Bearer synthetic-private',
      publicEvidenceOwnerID: () => d['principalId'] as String,
      publicEvidenceEpoch: () => 1,
      publicEvidenceNow: () => DateTime.parse(p['observedAt'] as String),
      client: MockClient((r) async {
        methods.add(r.method);
        return http.Response(
          publicEvidenceWire(),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }),
      apiBaseUrl: 'https://public-evidence-fixture.test',
    );
    final created = await source.resolve('找活动', const [], const []);
    final restored = await source.restore(
      AgentTask.fromJson(d['task'] as Map<String, dynamic>),
      const [],
      const [],
    );
    expect(methods, ['POST', 'GET']);
    expect(created.resultSet!.publicFieldEvidence!.current, isTrue);
    expect(restored.resultSet!.publicFieldEvidence!.current, isTrue);
    expect(
      restored.projectionItems!.single.entity,
      created.projectionItems!.single.entity,
    );
    source.dispose();
  });
}
