import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _nativeID = '77777777-7777-4777-8777-777777777777';
const _failure = AgentRequestFailure(
  '合成服务错误，原查询未确认完成。',
  statusCode: 500,
  code: 'synthetic_internal_error',
);
const _retainedTask = AgentTask(
  id: _nativeID,
  query: '合成周末羽毛球查询',
  status: 'ACTIVE',
  cityID: 'aberdeen-gb',
  principalID: 'synthetic-owner',
  messages: [AgentMessage(role: 'user', text: '合成周末羽毛球查询')],
);
Map<String, dynamic> _nativeRecord() => {
  'id': _nativeID,
  'query': _retainedTask.query,
  'status': 'ACTIVE',
  'cityContext': 'aberdeen-gb',
  'principalType': 'person',
  'principalId': 'synthetic-owner',
  'actingUserId': 'synthetic-owner',
  'conversation': [
    {'role': 'user', 'text': _retainedTask.query},
  ],
};

http.Response _response(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

class _FailedTurnSource extends AgentTaskSource {
  _FailedTurnSource(this.read);
  final Future<List<AgentTask>> Function(int count) read;
  int submissions = 0;
  int historyReads = 0;
  int restores = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    submissions++;
    if (submissions == 1) throw _failure;
    return const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '新的合成回答',
    );
  }

  @override
  Future<List<AgentTask>> loadRecent() => read(++historyReads);

  @override
  Future<AgentResult> restore(
    AgentTask task,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) {
    restores++;
    throw StateError('No automatic restore is allowed in this fixture');
  }
}

void main() {
  test('首POST500后仅GET恢复原ACTIVE任务到Recent，不自动选择或重发', () async {
    final requests = <http.Request>[];
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => 'Bearer synthetic-owner',
      publicEvidenceOwnerID: () => 'synthetic-owner',
      publicEvidenceEpoch: () => 1,
      apiBaseUrl: 'https://failure-recovery-fixture.test',
      client: MockClient((r) async {
        requests.add(r);
        if (r.method == 'POST') {
          final body = jsonDecode(r.body) as Map<String, dynamic>;
          expect(body['taskId'], isNull);
          return http.Response(
            '{"error":{"code":"synthetic_internal_error"}}',
            500,
          );
        }
        if (r.url.path == '/v1/me/agent-tasks') {
          return _response([_nativeRecord()]);
        }
        expect(r.url.path, '/v1/me/agent-tasks/$_nativeID');
        return _response({
          'taskId': _nativeID,
          'task': _nativeRecord(),
          'activities': [],
          'places': [],
          'people': [],
          'groups': [],
          'organizations': [],
          'note': '合成任务尚未完成检索。',
        });
      }),
    );
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    await workspace.submit(_retainedTask.query, [], [], cityID: 'aberdeen-gb');
    expect(requests.map((r) => r.method), ['POST', 'GET']);
    expect(workspace.requestFailure?.statusCode, 500);
    expect(workspace.queryState, AgentQueryState.error);
    expect(workspace.task!.id, startsWith('local-'));
    expect(workspace.task!.query, _retainedTask.query);
    expect(workspace.result, isNull);
    expect(workspace.conversation.map((m) => m.role), ['user']);
    expect(workspace.recent.single.id, _nativeID);
    expect(workspace.recent.single.status, 'ACTIVE');
    expect(workspace.recent.single.query, _retainedTask.query);
    expect(workspace.recentLoading, isFalse);
    expect(workspace.recentError, isNull);

    // Explicit user selection reuses the existing GET recovery path.
    await workspace.reopen(workspace.recent.single, [], []);
    expect(requests.map((r) => r.method), ['POST', 'GET', 'GET']);
    expect(workspace.task!.id, _nativeID);
    expect(workspace.task!.status, 'ACTIVE');
    expect(workspace.requestError, isNull);
  });

  test('Recent读失败保留已知历史、原地图和原请求错误，不伪造完成', () async {
    final source = _FailedTurnSource(
      (_) async => throw const AgentRequestFailure('合成历史服务错误', statusCode: 503),
    );
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    const oldResult = AgentResult(
      entities: [
        MapEntity(
          id: 'place:original',
          kind: MapEntityKind.place,
          title: '原公开地点',
          subtitle: '合成测试',
          latitude: 57.14,
          longitude: -2.1,
        ),
      ],
      activities: [],
      places: [],
      note: '原合成回答',
    );
    workspace.task = _retainedTask;
    workspace.result = oldResult;
    workspace.mapBackgroundResult = oldResult;
    workspace.mapBackgroundCityID = 'aberdeen-gb';
    workspace.selectedEntityId = 'place:original';
    workspace.recent.add(_retainedTask);
    workspace.conversation.add(
      const AgentMessage(role: 'assistant', text: '原合成回答'),
    );
    await workspace.submit('合成更近一点', [], [], cityID: 'aberdeen-gb');
    expect(source.submissions, 1);
    expect(source.historyReads, 1);
    expect(source.restores, 0);
    expect(workspace.task, same(_retainedTask));
    expect(workspace.recent.single, same(_retainedTask));
    expect(workspace.result, same(oldResult));
    expect(workspace.mapBackgroundResult, same(oldResult));
    expect(workspace.mapBackgroundCityID, 'aberdeen-gb');
    expect(workspace.selectedEntityId, 'place:original');
    expect(workspace.requestFailure, same(_failure));
    expect(workspace.requestError, _failure.userMessage);
    expect(workspace.recentFailure?.statusCode, 503);
    expect(workspace.recentLoading, isFalse);
    expect(workspace.conversation.map((m) => m.text), ['原合成回答', '合成更近一点']);
  });

  for (final late in ['success', 'failure']) {
    test('旧失败Recent的$late不能覆盖新查询或错误状态', () async {
      final held = Completer<List<AgentTask>>();
      final started = Completer<void>();
      final source = _FailedTurnSource((_) {
        started.complete();
        return held.future;
      });
      final workspace = AgentWorkspaceController(source: source);
      addTearDown(workspace.dispose);
      final old = workspace.submit('原合成失败查询', [], [], cityID: 'aberdeen-gb');
      await started.future;
      expect(workspace.requestFailure, same(_failure));
      await workspace.submit('新的合成查询', [], [], cityID: 'aberdeen-gb');
      final recent = List<AgentTask>.of(workspace.recent);
      final currentTask = workspace.task;
      if (late == 'success') {
        held.complete([_retainedTask]);
      } else {
        held.completeError(
          const AgentRequestFailure('旧合成读失败', statusCode: 503),
        );
      }
      await old;
      expect(workspace.task, same(currentTask));
      expect(workspace.result!.note, '新的合成回答');
      expect(workspace.recent, recent);
      expect(workspace.requestError, isNull);
      expect(workspace.requestFailure, isNull);
      expect(workspace.recentError, isNull);
      expect(workspace.recentLoading, isFalse);
      expect(source.submissions, 2);
      expect(source.historyReads, 1);
      expect(source.restores, 0);
    });

    test('旧失败Recent的$late不改变更新读取的列表和spinner', () async {
      final older = Completer<List<AgentTask>>();
      final newer = Completer<List<AgentTask>>();
      final started = Completer<void>();
      final source = _FailedTurnSource((count) {
        if (count == 1) {
          started.complete();
          return older.future;
        }
        return newer.future;
      });
      final workspace = AgentWorkspaceController(source: source);
      addTearDown(workspace.dispose);
      final failed = workspace.submit('合成失败查询', [], [], cityID: 'aberdeen-gb');
      await started.future;
      final fresh = workspace.loadRecent();
      if (late == 'success') {
        older.complete([_retainedTask]);
      } else {
        older.completeError(
          const AgentRequestFailure('旧合成读失败', statusCode: 503),
        );
      }
      await failed;
      expect(workspace.recentLoading, isTrue);
      expect(workspace.recent, isEmpty);
      expect(workspace.recentError, isNull);
      expect(workspace.requestFailure, same(_failure));
      newer.complete([_retainedTask]);
      await fresh;
      expect(workspace.recent.single.id, _nativeID);
      expect(workspace.recentLoading, isFalse);
      expect(workspace.requestFailure, same(_failure));
      expect(source.submissions, 1);
      expect(source.historyReads, 2);
    });
  }

  for (final retirement in [
    'account',
    'organization',
    'city',
    'epoch',
    'dispose',
  ]) {
    test('失败后的Recent在$retirement变更后拒绝迟到旧任务', () async {
      final held = Completer<http.Response>();
      final reading = Completer<void>();
      var token = 'Bearer synthetic-owner';
      String? organization;
      var city = 'aberdeen-gb';
      var epoch = 1;
      final requests = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => city,
        authorizationHeader: () => token,
        organizationWorkspaceID: () => organization,
        publicEvidenceEpoch: () => epoch,
        apiBaseUrl: 'https://failure-retirement-fixture.test',
        client: MockClient((r) async {
          requests.add(r);
          if (r.method == 'POST') return http.Response('{}', 500);
          reading.complete();
          return held.future;
        }),
      );
      final workspace = AgentWorkspaceController(source: source);
      final pending = workspace.submit('合成失败查询', [], [], cityID: city);
      await reading.future;
      final failure = workspace.requestFailure;
      if (retirement == 'account') {
        token = 'Bearer synthetic-other-owner';
        workspace.retireAccountContext();
      } else if (retirement == 'organization') {
        organization = 'synthetic-workspace';
        workspace.clearAccountContext();
      } else if (retirement == 'city') {
        city = 'other-city';
        workspace.retirePendingQueryForViewChange();
      } else if (retirement == 'epoch') {
        epoch++;
      } else {
        workspace.dispose();
      }
      var notifications = 0;
      if (retirement != 'dispose') {
        workspace.addListener(() => notifications++);
      }
      held.complete(_response([_nativeRecord()]));
      await expectLater(pending, completes);
      expect(requests.map((r) => r.method), ['POST', 'GET']);
      expect(workspace.recent, isEmpty);
      if (retirement == 'epoch') {
        expect(workspace.requestFailure, same(failure));
        expect(workspace.recentFailure?.code, 'PUBLIC_CONTEXT_EXPIRED');
      }
      if (retirement == 'account' || retirement == 'organization') {
        expect(notifications, 0);
        expect(workspace.recentError, isNull);
      }
      if (retirement != 'dispose') {
        expect(workspace.recentLoading, isFalse);
        workspace.dispose();
      }
    });
  }

  for (final route in ['recent', 'restore']) {
    for (final retirement in [
      'token',
      'organization',
      'city',
      'online',
      'owner',
      'epoch',
      'dispose',
    ]) {
      test('原Remote $route GET在$retirement变更后不交付旧数据', () async {
        final held = Completer<http.Response>();
        final started = Completer<void>();
        var token = 'Bearer synthetic-owner';
        String? organization, online;
        var city = 'aberdeen-gb', owner = 'synthetic-owner';
        var epoch = 1;
        var reads = 0;
        final source = RemoteAgentTaskSource(
          cityID: () => city,
          authorizationHeader: () => token,
          organizationWorkspaceID: () => organization,
          onlineContextID: () => online,
          publicEvidenceOwnerID: () => owner,
          publicEvidenceEpoch: () => epoch,
          apiBaseUrl: 'https://task-read-retirement-fixture.test',
          client: MockClient((r) async {
            expect(r.method, 'GET');
            reads++;
            started.complete();
            return held.future;
          }),
        );
        final Future<Object?> pending = route == 'recent'
            ? source.loadRecent()
            : source.restore(_retainedTask, [], []);
        final rejection = expectLater(
          pending,
          throwsA(
            isA<AgentRequestFailure>().having(
              (e) => e.code,
              'retired scope',
              'PUBLIC_CONTEXT_EXPIRED',
            ),
          ),
        );
        await started.future;
        switch (retirement) {
          case 'token':
            token = 'Bearer synthetic-other-owner';
          case 'organization':
            organization = 'synthetic-workspace';
          case 'city':
            city = 'other-city';
          case 'online':
            online = 'synthetic-online-context';
          case 'owner':
            owner = 'synthetic-other-owner';
          case 'epoch':
            epoch++;
          case 'dispose':
            source.dispose();
        }
        held.complete(
          route == 'recent' ? _response([_nativeRecord()]) : _response({}),
        );
        await rejection;
        expect(reads, 1);
        if (retirement != 'dispose') source.dispose();
      });
    }
  }
}
