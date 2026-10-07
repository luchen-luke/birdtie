import 'package:http/http.dart' as http;
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:http/testing.dart';
import 'agent_task_detail_page_test.dart'
    show taskDestinationID, taskDestinationData, taskReply;
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter_test/flutter_test.dart';
import 'dart:async';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';

class _RetryDecisionSource extends AgentTaskSource {
  _RetryDecisionSource(this.failure);
  final AgentRequestFailure failure;
  int calls = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    calls++;
    if (calls == 1) throw failure;
    return const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: 'Synthetic recovered read',
    );
  }
}

class _UnknownOnlineSource extends AgentTaskSource {
  int calls = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    calls++;
    throw const AgentRequestFailure(
      '提交结果尚未确认，请从最近对话核实；不会自动重复提交。',
      code: 'online_result_unknown',
    );
  }
}

class _Source extends AgentTaskSource {
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => const AgentResult(
    entities: [],
    activities: [],
    places: [],
    note: 'preview',
  );
}

class _NativeTypedAnswerSource extends AgentTaskSource {
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => AgentResult(
    entities: const [],
    activities: const [],
    places: const [],
    note: '',
    message: '找到 1 个公开社区。',
    task: const AgentTask(
      id: 'native-original-task',
      query: '找社区',
      status: 'COMPLETED',
      messages: [
        AgentMessage(role: 'user', text: '以前的问题'),
        AgentMessage(role: 'assistant', text: '以前的已确认回答'),
        AgentMessage(role: 'user', text: '找社区'),
        AgentMessage(role: 'assistant', text: '正在按当前原生公开来源整理结果。'),
      ],
    ),
    resultSet: AgentResultSet(
      id: 'native-original-result',
      status: 'ready',
      entities: const [],
      items: const [],
      generatedAt: DateTime.utc(2026),
    ),
  );
}

class _ChangingSource extends AgentTaskSource {
  int calls = 0;

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => AgentResult(
    entities: const [],
    activities: const [],
    places: const [],
    note: 'version ${++calls}',
  );
}

class _FollowUpSource extends AgentTaskSource {
  AgentTask? continuedTask;

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => const AgentResult(
    entities: [],
    activities: [],
    places: [],
    note: 'first result',
  );

  @override
  Future<AgentResult> followUp(
    AgentTask task,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    continuedTask = task;
    return AgentResult(
      entities: const [],
      activities: const [],
      places: const [],
      note: 'follow-up result',
      task: AgentTask(
        id: task.id,
        query: task.query,
        status: 'COMPLETED',
        cityID: task.cityID,
        messages: const [
          AgentMessage(role: 'user', text: 'Find badminton this weekend'),
          AgentMessage(role: 'assistant', text: 'Found activities.'),
          AgentMessage(role: 'user', text: 'Anything closer?'),
          AgentMessage(role: 'assistant', text: 'Showing closer activities.'),
        ],
      ),
    );
  }
}

class _StaleSource extends AgentTaskSource {
  final requests = <String, Completer<AgentResult>>{};

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => (requests[query] = Completer<AgentResult>()).future;
}

class _AreaSource extends AgentTaskSource {
  MapBounds? receivedBounds;
  String? receivedQuery;
  String? continuedTaskID;

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => const AgentResult(
    entities: [],
    activities: [],
    places: [],
    note: 'activity response',
  );

  @override
  Future<AgentResult> searchArea(
    AgentTask? task,
    String query,
    MapBounds bounds,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    receivedBounds = bounds;
    receivedQuery = query;
    continuedTaskID = task?.id;
    return const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: 'area response',
    );
  }
}

class _PendingAreaSource extends AgentTaskSource {
  final requests = <double, Completer<AgentResult>>{};

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => const AgentResult(
    entities: [
      MapEntity(
        id: 'activity:one',
        kind: MapEntityKind.activity,
        title: '原有活动',
        subtitle: '',
        latitude: 57.14,
        longitude: -2.1,
      ),
    ],
    activities: [],
    places: [],
    note: 'old result',
  );

  @override
  Future<AgentResult> searchArea(
    AgentTask? task,
    String query,
    MapBounds bounds,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => (requests[bounds.west] = Completer<AgentResult>()).future;
}

void main() {
  const publicPin = AgentResultItem(
    entity: AgentResultRef(type: 'activity', id: 'public-pin'),
    title: '公开活动', summary: '单元测试公开读取投影', scope: 'AUTHORIZED_VIEW',
    anchor: AgentResultAnchor(precision: 'point', latitude: 57, longitude: -2),
  );
  const privatePin = AgentResultItem(
    entity: AgentResultRef(type: 'opportunity', id: 'owned:private-pin'),
    detail: AgentResultRef(type: 'activity', id: 'private-pin'),
    title: '私人机会', summary: '', scope: 'SELF_PRIVATE',
    anchor: AgentResultAnchor(precision: 'point', latitude: 57, longitude: -2),
  );
  AgentResult pinResult(AgentResultItem item, {String status = 'ready'}) => AgentResult(
    entities: const [], activities: const [], places: const [], note: '测试读取',
    resultSet: AgentResultSet(id: 'current-pin-result', status: status,
      entities: [item.entity], items: [item], generatedAt: DateTime.utc(2026)),
  );
  AgentWorkspaceController pinWorkspace(String? intent, {
    AgentResultItem item = publicPin, String context = 'CITY',
    String resultStatus = 'ready', String? target,
  }) => AgentWorkspaceController(source: _Source())
    ..task = AgentTask(id: 'current-native-task', query: '原任务', status: 'COMPLETED',
        cityID: 'test-city', contextType: context, intent: intent,
        filters: {'targetIntent': ?target,
            'unchangedApprovalVersion': 'original-version'})
    ..result = pinResult(item, status: resultStatus)
    ..state = AgentViewState.results
    ..queryContextType = context
    ..contentMode = AgentContentMode.conversation
    ..sheetExtent = AgentSheetExtent.expanded
    ..selectedEntityId = 'activity:previous-selection'
    ..conversation.add(const AgentMessage(role: 'user', text: '原安全对话'));

  test('地图来源原生公开只读选择仅显露轻卡不改任务版本或对话', () {
    for (final intent in ['FIND_ACTIVITY', 'FIND_ORGANIZATION', 'FIND_PLACE',
        'AREA_DISCOVERY', 'FIND_PUBLIC_PERSON', 'FIND_COMMUNITY', 'FIND_BUSINESS',
        'REFINE_RESULTS', 'COMPARE_RESULTS']) {
      final w = pinWorkspace(intent, target: 'FIND_ACTIVITY');
      final task = w.task, result = w.result, epoch = w.taskEpoch;
      final history = List.of(w.conversation);
      expect(publicPin.valid, isTrue);
      expect(w.selectMapEntity(publicPin.entity.mapID), isTrue, reason: intent);
      expect(w.selectedEntityId, publicPin.entity.mapID);
      expect(w.sheetExtent, AgentSheetExtent.peek);
      expect(w.contentMode, AgentContentMode.conversation);
      expect(w.task, same(task));
      expect(w.task!.filters['unchangedApprovalVersion'], 'original-version');
      expect(w.result, same(result));
      expect(w.taskEpoch, epoch);
      expect(w.conversation, history);
      w.dispose();
    }
  });

  test('地图来源未知私密线上写批准或失效结果保持当前工作区', () {
    final variants = <AgentWorkspaceController>[
      pinWorkspace('CREATE_ACTIVITY'),
      pinWorkspace('PERSONAL_RELATIONSHIP_CONTEXT'),
      pinWorkspace('FIND_OWN_OPPORTUNITY', item: privatePin),
      pinWorkspace('FIND_ACTIVITY', item: privatePin),
      pinWorkspace('FIND_ACTIVITY', context: 'ONLINE'),
      pinWorkspace('NEW_UNKNOWN_INTENT'),
      pinWorkspace(null),
      pinWorkspace('REFINE_RESULTS', target: 'CREATE_ACTIVITY'),
      pinWorkspace('COMPARE_RESULTS'),
      pinWorkspace('FIND_ACTIVITY', resultStatus: 'unsupported'),
      pinWorkspace('FIND_ACTIVITY', resultStatus: 'empty'),
      pinWorkspace('FIND_ACTIVITY')..state = AgentViewState.searching,
      pinWorkspace('FIND_ACTIVITY')..requestError = '本轮尚未确认',
      pinWorkspace('FIND_ACTIVITY', item: const AgentResultItem(
          entity: AgentResultRef(type: 'activity', id: 'public-pin'),
          title: '只有列表，无公开点位', summary: '', scope: 'AUTHORIZED_VIEW')),
    ];
    expect(privatePin.valid, isTrue);
    expect(privatePin.mapEntity, isNotNull);
    for (final w in variants) {
      final task = w.task, result = w.result, epoch = w.taskEpoch;
      final history = List.of(w.conversation);
      var notifications = 0;
      w.addListener(() => notifications++);
      expect(w.selectMapEntity(w.result!.projectionItems!.single.entity.mapID), isFalse);
      expect(w.selectedEntityId, 'activity:previous-selection');
      expect(w.sheetExtent, AgentSheetExtent.expanded);
      expect(w.contentMode, AgentContentMode.conversation);
      expect(w.task, same(task));
      expect(w.task!.filters['unchangedApprovalVersion'], 'original-version');
      expect(w.result, same(result));
      expect(w.taskEpoch, epoch);
      expect(w.conversation, history);
      expect(notifications, 0);
      w.dispose();
    }
  });

  test('地图来源未知实体拒绝，原列表选择不改变展开和对话模式', () {
    final w = pinWorkspace('FIND_ACTIVITY');
    final task = w.task, result = w.result, epoch = w.taskEpoch;
    expect(w.selectMapEntity('activity:not-in-current-result'), isFalse);
    expect(w.selectedEntityId, 'activity:previous-selection');
    w.selectEntity(publicPin.entity.mapID);
    expect(w.selectedEntityId, publicPin.entity.mapID);
    expect(w.sheetExtent, AgentSheetExtent.expanded);
    expect(w.contentMode, AgentContentMode.conversation);
    expect(w.task, same(task));
    expect(w.result, same(result));
    expect(w.taskEpoch, epoch);
    w.dispose();
  });

  for (final path in [
    'submit',
    'area',
    'reopen',
    'history-retry',
    'query-retry',
  ]) {
    for (final outcome in ['success', 'network']) {
      test(
        'Lifecycle actual Remote $path late $outcome after dispose is ignored without notifying dead controller',
        () async {
          final held = Completer<http.Response>();
          var requests = 0;
          var recentReads = 0;
          final source = RemoteAgentTaskSource(
            cityID: () => 'alpha',
            authorizationHeader: () => 'Bearer synthetic-owner',
            apiBaseUrl: 'https://lifecycle-fixture.test',
            client: MockClient((r) async {
              if (r.method == 'GET' && r.url.path == '/v1/me/agent-tasks') {
                recentReads++;
                return http.Response('{"data":[]}', 200);
              }
              requests++;
              expect(
                r.method,
                path == 'reopen' || path == 'history-retry' ? 'GET' : 'POST',
              );
              if (requests == 1 &&
                  (path == 'history-retry' || path == 'query-retry')) {
                return http.Response('{}', 503);
              }
              final reply = await held.future;
              if (outcome == 'network') {
                throw http.ClientException('synthetic late network');
              }
              return reply;
            }),
          );
          final w = AgentWorkspaceController(source: source);
          Future<void> pending;
          const previous = AgentTask(
            id: taskDestinationID,
            query: '原合成查询',
            status: 'COMPLETED',
            cityID: 'alpha',
            messages: [AgentMessage(role: 'assistant', text: '原历史回答')],
          );
          if (path == 'submit') {
            pending = w.submit('合成待返回查询', [], [], cityID: 'alpha');
          } else if (path == 'area') {
            pending = w.searchThisArea(
              [],
              [],
              cityID: 'alpha',
              bounds: const MapBounds(
                west: -2.2,
                south: 57.1,
                east: -2.0,
                north: 57.3,
              ),
            );
          } else if (path == 'reopen') {
            pending = w.reopen(previous, [], []);
          } else {
            if (path == 'history-retry') {
              await w.reopen(previous, [], []);
            } else {
              await w.submit('合成待重读查询', [], [], cityID: 'alpha');
            }
            expect(w.queryState, AgentQueryState.error);
            pending = w.retry();
          }
          expect(w.queryState, AgentQueryState.loading);
          final task = w.task,
              result = w.result,
              error = w.requestError,
              failure = w.requestFailure,
              messages = List<AgentMessage>.of(w.conversation),
              extent = w.sheetExtent,
              mode = w.contentMode;
          w.dispose();
          // Source remains a borrowed real Remote transport; disposal must not
          // manufacture a response or avoid exercising the pending controller.
          held.complete(taskReply(taskDestinationData()));
          await expectLater(pending, completes);
          expect(
            requests,
            path == 'history-retry' || path == 'query-retry' ? 2 : 1,
          );
          expect(recentReads, path == 'query-retry' ? 1 : 0);
          expect(w.task, same(task));
          expect(w.result, same(result));
          expect(w.requestError, error);
          expect(w.requestFailure, same(failure));
          expect(w.conversation, messages);
          expect(w.sheetExtent, extent);
          expect(w.contentMode, mode);
        },
      );
    }
  }

  for (final status in [401, 403]) {
    test(
      'Recent list typed HTTP$status does not loop unchanged retry and fresh recovered read stays allowed',
      () async {
        var reads = 0;
        final source = RemoteAgentTaskSource(
          cityID: () => null,
          authorizationHeader: () => 'Bearer synthetic-owner',
          apiBaseUrl: 'https://recent-list-fixture.test',
          client: MockClient((r) async {
            expect(r.method, 'GET');
            reads++;
            if (reads == 1) return http.Response('{}', status);
            return http.Response('{"data":[]}', 200);
          }),
        );
        final w = AgentWorkspaceController(source: source);
        addTearDown(w.dispose);
        await w.loadRecent();
        expect(w.recentFailure?.statusCode, status);
        expect(w.recentLoading, false);
        expect(w.recentError, isNotNull);
        await w.retryRecent();
        expect(reads, 1);
        await w.loadRecent();
        expect(reads, 2);
        expect(w.recentError, isNull);
        expect(w.recentFailure, isNull);
        expect(w.recentLoading, false);
        expect(w.recent, isEmpty);
        expect(w.requestError, isNull);
      },
    );
  }
  for (final late in ['401', '403', 'success']) {
    for (final retirement in ['newer-read', 'account', 'organization']) {
      test(
        'Recent list late $late after $retirement cannot replace current history or error',
        () async {
          final held = Completer<http.Response>();
          var reads = 0;
          final source = RemoteAgentTaskSource(
            cityID: () => null,
            authorizationHeader: () => 'Bearer synthetic-owner',
            apiBaseUrl: 'https://recent-list-fixture.test',
            client: MockClient((r) async {
              expect(r.method, 'GET');
              reads++;
              if (reads == 1) return held.future;
              return http.Response(
                '{"data":[{"id":"new-history","query":"新的当前合成历史","status":"COMPLETED"}]}',
                200,
                headers: {'content-type': 'application/json; charset=utf-8'},
              );
            }),
          );
          final w = AgentWorkspaceController(source: source);
          addTearDown(w.dispose);
          final old = w.loadRecent();
          expect(w.recentLoading, true);
          if (retirement == 'account') w.retireAccountContext();
          if (retirement == 'organization') w.clearAccountContext();
          await w.loadRecent();
          expect(w.recent.single.id, 'new-history');
          expect(w.recentLoading, false);
          held.complete(
            late == 'success'
                ? http.Response(
                    '{"data":[{"id":"old-history","query":"旧合成历史","status":"COMPLETED"}]}',
                    200,
                    headers: {
                      'content-type': 'application/json; charset=utf-8',
                    },
                  )
                : http.Response('{}', int.parse(late)),
          );
          await old;
          expect(w.recent.single.id, 'new-history');
          expect(w.recentLoading, false);
          expect(w.recentError, isNull);
          expect(w.recentFailure, isNull);
          expect(w.requestError, isNull);
          expect(reads, 2);
        },
      );
    }
  }
  test(
    'Recent list late read after disposal cannot notify a dead controller',
    () async {
      final held = Completer<http.Response>();
      final source = RemoteAgentTaskSource(
        cityID: () => null,
        authorizationHeader: () => 'Bearer synthetic-owner',
        apiBaseUrl: 'https://recent-list-fixture.test',
        client: MockClient((r) => held.future),
      );
      final w = AgentWorkspaceController(source: source);
      final old = w.loadRecent();
      w.dispose();
      held.complete(http.Response('{}', 503));
      await old;
    },
  );

  test(
    'Recent list failure notifies its consumers while retaining current task and local history',
    () async {
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => 'Bearer synthetic-owner',
        apiBaseUrl: 'https://recent-list-fixture.test',
        client: MockClient((r) async => http.Response('{}', 503)),
      );
      final w = AgentWorkspaceController(source: source);
      addTearDown(w.dispose);
      const current = AgentTask(
        id: 'local-current',
        query: '未改变的当前合成查询',
        status: 'COMPLETED',
      );
      const currentResult = AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '原当前回答',
      );
      w.task = current;
      w.result = currentResult;
      w.selectedEntityId = 'place:original';
      w.recent.add(current);
      w.conversation.add(const AgentMessage(role: 'assistant', text: '原当前回答'));
      var updates = 0;
      w.addListener(() => updates++);
      await w.loadRecent();
      expect(w.task, same(current));
      expect(w.result, same(currentResult));
      expect(w.selectedEntityId, 'place:original');
      expect(w.recent, [current]);
      expect(w.requestError, isNull);
      expect(w.conversation.single.text, '原当前回答');
      expect(updates, greaterThan(0));
    },
  );

  test(
    'GET recovery retry late permission failure preserves metadata and fresh explicit reopen works',
    () async {
      var reads = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => 'Bearer synthetic-owner',
        apiBaseUrl: 'https://get-recovery-fixture.test',
        client: MockClient((r) async {
          expect(r.method, 'GET');
          expect(r.url.path, '/v1/me/agent-tasks/$taskDestinationID');
          reads++;
          if (reads == 1) {
            return http.Response(
              '{"error":{"code":"service_unavailable"}}',
              503,
            );
          }
          if (reads == 2) {
            return http.Response('{"error":{"code":"forbidden"}}', 403);
          }
          return taskReply(taskDestinationData());
        }),
      );
      final w = AgentWorkspaceController(source: source);
      addTearDown(w.dispose);
      const original = AgentTask(
        id: taskDestinationID,
        query: '原合成历史查询',
        status: 'COMPLETED',
        messages: [AgentMessage(role: 'assistant', text: '原历史回答')],
      );
      await w.reopen(original, [], []);
      expect(w.requestFailure?.statusCode, 503);
      await w.retry();
      expect(reads, 2);
      expect(w.requestFailure?.statusCode, 403);
      expect(w.requestFailure?.code, 'forbidden');
      expect(w.result, isNull);
      expect(w.conversation, original.messages);
      expect(w.task, same(original));
      await w.retry();
      expect(reads, 2);
      // A new explicit human read is allowed after restoring access; the old
      // failed unchanged retry is not a permanent ban on this original task ID.
      await w.reopen(original, [], []);
      expect(reads, 3);
      expect(w.task?.id, taskDestinationID);
      expect(w.requestFailure, isNull);
      expect(w.requestError, isNull);
      expect(w.result?.taskID, taskDestinationID);
    },
  );

  for (final failure in [
    const AgentRequestFailure('合成会话失效', statusCode: 401, code: 'unauthorized'),
    const AgentRequestFailure('合成无权读取', statusCode: 403, code: 'forbidden'),
    const AgentRequestFailure(
      '合成服务暂不可用',
      statusCode: 503,
      code: 'service_unavailable',
    ),
    const AgentRequestFailure('合成网络暂不可用'),
    const AgentRequestFailure(
      'Synthetic unresolved online submission',
      code: 'online_result_unknown',
    ),
  ]) {
    test(
      'GET recovery controller ${failure.statusCode}/${failure.code} keeps history and no fake empty',
      () async {
        var reads = 0;
        final source = RemoteAgentTaskSource(
          cityID: () => 'alpha',
          authorizationHeader: () => 'Bearer synthetic-owner',
          apiBaseUrl: 'https://get-recovery-fixture.test',
          client: MockClient((r) async {
            expect(r.method, 'GET');
            reads++;
            if (reads == 1) throw failure;
            return taskReply(taskDestinationData());
          }),
        );
        final w = AgentWorkspaceController(source: source);
        addTearDown(w.dispose);
        const previous = AgentTask(
          id: taskDestinationID,
          query: '原合成查询',
          status: 'COMPLETED',
          cityID: 'alpha',
          messages: [AgentMessage(role: 'assistant', text: '原已确认历史回答')],
        );
        await w.reopen(previous, [], []);
        expect(w.task, same(previous));
        expect(w.conversation, previous.messages);
        expect(w.result, isNull);
        expect(w.requestFailure, same(failure));
        expect(w.requestError, failure.userMessage);
        expect(w.queryState, AgentQueryState.error);
        await w.retry();
        expect(reads, failure.permitsUnchangedRetry ? 2 : 1);
        if (failure.permitsUnchangedRetry) {
          expect(w.requestError, isNull);
          expect(w.requestFailure, isNull);
          expect(w.result?.taskID, taskDestinationID);
          expect(w.queryState, AgentQueryState.empty);
        } else {
          expect(w.task, same(previous));
          expect(w.conversation, previous.messages);
          expect(w.requestFailure, same(failure));
        }
      },
    );
  }

  for (final failure in [
    const AgentRequestFailure(
      'Synthetic expired session',
      statusCode: 401,
      code: 'unauthorized',
    ),
    const AgentRequestFailure(
      'Synthetic forbidden request',
      statusCode: 403,
      code: 'forbidden',
    ),
    const AgentRequestFailure(
      'Synthetic outcome requires reconciliation',
      code: 'online_result_unknown',
    ),
  ]) {
    test(
      'typed failure ${failure.statusCode}/${failure.code} requires recovery before same-request retry',
      () async {
        final source = _RetryDecisionSource(failure);
        final workspace = AgentWorkspaceController(source: source);
        addTearDown(workspace.dispose);
        await workspace.submit('合成原请求', [], [], cityID: 'alpha');
        final task = workspace.task,
            messages = List<AgentMessage>.of(workspace.conversation),
            extent = workspace.sheetExtent,
            mode = workspace.contentMode;
        expect(workspace.requestFailure, same(failure));
        expect(workspace.queryState, AgentQueryState.error);
        await workspace.retry();
        expect(source.calls, 1);
        expect(workspace.task, same(task));
        expect(workspace.conversation, messages);
        expect(workspace.sheetExtent, extent);
        expect(workspace.contentMode, mode);
        expect(workspace.requestFailure, same(failure));
        expect(workspace.requestError, failure.userMessage);
      },
    );
  }
  for (final failure in [
    const AgentRequestFailure(
      'Synthetic recoverable service error',
      statusCode: 503,
      code: 'service_unavailable',
    ),
    const AgentRequestFailure('Synthetic network interruption'),
  ]) {
    test(
      'typed recoverable ${failure.statusCode}/${failure.code} keeps explicit current retry',
      () async {
        final source = _RetryDecisionSource(failure);
        final workspace = AgentWorkspaceController(source: source);
        addTearDown(workspace.dispose);
        await workspace.submit('合成原请求', [], [], cityID: 'alpha');
        expect(workspace.queryState, AgentQueryState.error);
        await workspace.retry();
        expect(source.calls, 2);
        expect(workspace.requestError, isNull);
        expect(workspace.queryState, AgentQueryState.empty);
        expect(workspace.task?.query, '合成原请求');
        expect(workspace.result?.responseMessage, 'Synthetic recovered read');
      },
    );
  }
  test(
    'native typed current answer replaces only current display placeholder and restores',
    () async {
      final workspace = AgentWorkspaceController(
        source: _NativeTypedAnswerSource(),
      );
      await workspace.submit('找社区', [], []);
      expect(workspace.conversation.last.text, '找到 1 个公开社区。');
      expect(workspace.conversation[1].text, '以前的已确认回答');
      final original = workspace.task!;
      expect(original.id, 'native-original-task');
      expect(original.messages.last.text, '正在按当前原生公开来源整理结果。');
      await workspace.reopen(original, [], []);
      expect(workspace.conversation.last.text, '找到 1 个公开社区。');
      expect(workspace.conversation[1].text, '以前的已确认回答');
      expect(workspace.task!.id, original.id);
      workspace.dispose();
    },
  );
  test('选择查看范围保留原Task Result Pin Conversation并拒绝迟到查询', () async {
    final source = _StaleSource();
    final workspace = AgentWorkspaceController(source: source);
    const result = AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '原结果',
    );
    const task = AgentTask(
      id: 'original-task',
      query: '原查询',
      status: 'COMPLETED',
      cityID: 'original-city',
    );
    workspace.task = task;
    workspace.result = result;
    workspace.selectedEntityId = 'activity:original';
    workspace.conversation.add(
      const AgentMessage(role: 'assistant', text: '原回复'),
    );
    final pending = workspace.submit(
      '尚未返回的查询',
      [],
      [],
      cityID: 'original-city',
    );
    final conversation = List<AgentMessage>.of(workspace.conversation);
    workspace.retirePendingQueryForViewChange();
    expect(workspace.task, same(task));
    expect(workspace.result, same(result));
    expect(workspace.selectedEntityId, 'activity:original');
    expect(workspace.conversation, conversation);
    expect(workspace.state, AgentViewState.results);
    source.requests['尚未返回的查询']!.complete(
      const AgentResult(entities: [], activities: [], places: [], note: '迟到结果'),
    );
    await pending;
    expect(workspace.result, same(result));
    expect(workspace.task, same(task));
    workspace.dispose();
  });
  test('unconfirmed online task cannot blindly replay through retry', () async {
    final source = _UnknownOnlineSource();
    final controller = AgentWorkspaceController(source: source)
      ..queryContextType = 'ONLINE';
    await controller.submit('阅读', [], []);
    expect(controller.requestError, contains('提交结果尚未确认'));
    await controller.retry();
    expect(source.calls, 1);
    controller.dispose();
  });
  test('map markers cluster nearby entities with stable member IDs', () {
    final entities = [
      const MapEntity(
        id: 'activity:a',
        kind: MapEntityKind.activity,
        title: '活动 A',
        subtitle: '',
        latitude: 57.14,
        longitude: -2.1,
      ),
      const MapEntity(
        id: 'place:b',
        kind: MapEntityKind.place,
        title: '地点 B',
        subtitle: '',
        latitude: 57.1402,
        longitude: -2.1002,
      ),
      const MapEntity(
        id: 'activity:c',
        kind: MapEntityKind.activity,
        title: '活动 C',
        subtitle: '',
        latitude: 57.16,
        longitude: -2.12,
      ),
    ];
    final clustered = clusterMapEntities(entities, zoom: 16);
    expect(clustered, hasLength(2));
    final cluster = clustered.singleWhere((entity) => entity.isCluster);
    expect(cluster.memberIDs, ['activity:a', 'place:b']);
    expect(cluster.id, startsWith('cluster:'));
    expect(
      clusterMapEntities(
        entities.reversed.toList(),
        zoom: 16,
      ).singleWhere((entity) => entity.isCluster).id,
      cluster.id,
    );
    expect(
      clusterMapEntities(entities, zoom: 16, selectedId: 'activity:a'),
      hasLength(3),
    );
  });

  test(
    'intent moves through typing, results and conversation, then New clears context',
    () async {
      final workspace = AgentWorkspaceController(source: _Source());
      workspace.beginTyping();
      expect(workspace.state, AgentViewState.typing);
      await workspace.submit('  badminton this weekend  ', [], []);
      expect(workspace.state, AgentViewState.results);
      expect(workspace.task?.query, 'badminton this weekend');
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      expect(workspace.recent.single.query, 'badminton this weekend');
      expect(workspace.conversation.map((message) => message.role), [
        'user',
        'assistant',
      ]);
      await workspace.submit('Anything closer?', [], []);
      expect(workspace.recent, hasLength(1));
      expect(workspace.task?.query, 'badminton this weekend');
      workspace.setSheetExtent(AgentSheetExtent.expanded);
      expect(workspace.state, AgentViewState.results);
      expect(workspace.contentMode, AgentContentMode.conversation);
      workspace.showContent(AgentContentMode.results);
      expect(workspace.state, AgentViewState.results);
      expect(workspace.contentMode, AgentContentMode.results);
      workspace.showContent(AgentContentMode.conversation);
      expect(workspace.state, AgentViewState.results);
      expect(workspace.contentMode, AgentContentMode.conversation);
      workspace.newTask();
      expect(workspace.state, AgentViewState.idle);
      expect(workspace.task, isNull);
      expect(workspace.result, isNull);
      expect(workspace.conversation, isEmpty);
      expect(workspace.recent, hasLength(1));
      workspace.dispose();
    },
  );

  test('Recent refreshes results instead of reusing an old snapshot', () async {
    final source = _ChangingSource();
    final workspace = AgentWorkspaceController(source: source);
    await workspace.submit('badminton', [], []);
    final previous = workspace.task!;
    expect(workspace.result?.note, 'version 1');
    await workspace.reopen(previous, [], []);
    expect(source.calls, 2);
    expect(workspace.result?.note, 'version 2');
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    expect(workspace.contentMode, AgentContentMode.conversation);
    workspace.dispose();
  });

  test('follow-up keeps one task and New preserves it in Recent', () async {
    final source = _FollowUpSource();
    final workspace = AgentWorkspaceController(source: source);
    await workspace.submit(
      'Find badminton this weekend',
      [],
      [],
      cityID: 'aberdeen',
    );
    final id = workspace.task!.id;
    final firstResponse = workspace.result!.responseMessage;
    await workspace.submit('Anything closer?', [], [], cityID: 'aberdeen');
    expect(source.continuedTask?.id, id);
    expect(workspace.task?.id, id);
    expect(workspace.task?.query, 'Find badminton this weekend');
    expect(workspace.conversation.map((message) => message.text), [
      'Find badminton this weekend',
      firstResponse,
      'Anything closer?',
      'Showing closer activities.',
    ]);
    workspace.newTask();
    expect(workspace.task, isNull);
    expect(workspace.recent.single.id, id);
    workspace.dispose();
  });

  test('New starts a separate task and Recent keeps both contexts', () async {
    final workspace = AgentWorkspaceController(source: _Source());
    await workspace.submit('badminton', [], [], cityID: 'aberdeen');
    final first = workspace.task!;
    workspace.newTask();
    await workspace.submit('badminton', [], [], cityID: 'edinburgh');
    expect(workspace.conversation.map((message) => message.text), [
      'badminton',
      'preview',
    ]);
    expect(workspace.recent, hasLength(2));
    expect(workspace.recent.first.cityID, 'edinburgh');
    expect(workspace.recent.last.cityID, 'aberdeen');
    expect(workspace.recent.last.id, first.id);
    await workspace.reopen(first, [], []);
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    expect(workspace.contentMode, AgentContentMode.conversation);
    expect(workspace.conversation.map((message) => message.text), [
      'badminton',
      'preview',
    ]);
    workspace.dispose();
  });

  test(
    'focus and camera movement preserve result and selected entity',
    () async {
      final workspace = AgentWorkspaceController(source: _Source());
      await workspace.submit('find activities', [], []);
      final result = workspace.result;
      final extent = workspace.sheetExtent;
      workspace.selectEntity('activity:one');
      workspace.beginTyping();
      final mapState = MapViewportState();
      mapState.cameraSettled(
        const MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.3),
      );
      expect(workspace.result, same(result));
      expect(workspace.selectedEntityId, 'activity:one');
      expect(workspace.sheetExtent, extent);
      expect(mapState.searchAreaBounds, mapState.viewportBounds);
      workspace.dispose();
      mapState.dispose();
    },
  );

  test('area search action waits for the latest camera to settle', () {
    final mapState = MapViewportState();
    const first = MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.3);
    const second = MapBounds(west: -2.3, south: 57.0, east: -2.1, north: 57.2);
    mapState.cameraSettled(first);
    expect(mapState.searchAreaBounds, first);
    mapState.cameraStarted();
    expect(mapState.cameraMoving, isTrue);
    expect(mapState.searchAreaBounds, isNull);
    mapState.cameraSettled(second);
    expect(mapState.cameraMoving, isFalse);
    expect(mapState.searchAreaBounds, second);
    mapState.dispose();
  });

  test(
    'an older asynchronous search cannot replace the latest result',
    () async {
      final source = _StaleSource();
      final workspace = AgentWorkspaceController(source: source);
      final older = workspace.submit('A', [], []);
      final newer = workspace.submit('B', [], []);
      source.requests['B']!.complete(
        const AgentResult(entities: [], activities: [], places: [], note: 'B'),
      );
      await newer;
      source.requests['A']!.complete(
        const AgentResult(entities: [], activities: [], places: [], note: 'A'),
      );
      await older;
      expect(workspace.result?.note, 'B');
      expect(workspace.task?.query, 'B');
      workspace.dispose();
    },
  );

  test('rapid A/B/C queries keep C despite reverse completion', () async {
    final source = _StaleSource();
    final workspace = AgentWorkspaceController(source: source);
    final a = workspace.submit('A', [], []);
    final b = workspace.submit('B', [], []);
    final c = workspace.submit('C', [], []);
    source.requests['C']!.complete(
      const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: 'C result',
      ),
    );
    await c;
    source.requests['B']!.complete(
      const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: 'B result',
      ),
    );
    source.requests['A']!.complete(
      const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: 'A result',
      ),
    );
    await Future.wait([a, b]);
    expect(workspace.result?.note, 'C result');
    expect(workspace.task?.query, 'C');
    expect(workspace.conversation.map((message) => message.text), [
      'C',
      'C result',
    ]);
    workspace.dispose();
  });

  test(
    'pending follow-up keeps old result and selection until replacement',
    () async {
      final source = _StaleSource();
      final workspace = AgentWorkspaceController(source: source);
      final firstRequest = workspace.submit('first', [], []);
      source.requests['first']!.complete(
        const AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: 'first result',
        ),
      );
      await firstRequest;
      final previousResult = workspace.result;
      workspace.selectEntity('activity:one');

      final nextRequest = workspace.submit('follow up', [], []);
      expect(workspace.result, same(previousResult));
      expect(workspace.selectedEntityId, 'activity:one');
      source.requests['follow up']!.complete(
        const AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: 'next result',
        ),
      );
      await nextRequest;
      expect(workspace.result?.note, 'next result');
      expect(workspace.selectedEntityId, isNull);
      workspace.dispose();
    },
  );

  test('failed search keeps a retry path in controller state', () async {
    final source = _StaleSource();
    final workspace = AgentWorkspaceController(source: source);
    final first = workspace.submit('retry me', [], []);
    source.requests['retry me']!.completeError(StateError('offline'));
    await first;
    expect(workspace.requestError, isNotNull);
    expect(workspace.conversation.map((message) => message.role), ['user']);
    final retry = workspace.retry();
    expect(workspace.state, AgentViewState.searching);
    source.requests['retry me']!.complete(
      const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: 'recovered',
      ),
    );
    await retry;
    expect(workspace.result?.note, 'recovered');
    expect(workspace.requestError, isNull);
    workspace.dispose();
  });

  test('selecting a map entity does not create an Agent task', () {
    final workspace = AgentWorkspaceController(source: _Source());
    workspace.selectEntity('activity:pin');
    expect(workspace.selectedEntityId, 'activity:pin');
    expect(workspace.task, isNull);
    expect(workspace.result, isNull);
    expect(workspace.sheetExtent, AgentSheetExtent.peek);
    workspace.dispose();
  });

  test('a new query clears the previous pin preview', () async {
    final workspace = AgentWorkspaceController(source: _Source());
    workspace.selectEntity('activity:pin');
    await workspace.submit('找周末羽毛球', [], []);
    expect(workspace.selectedEntityId, isNull);
    expect(workspace.task, isNotNull);
    workspace.dispose();
  });

  test('an area search clears the previous pin preview', () async {
    final workspace = AgentWorkspaceController(source: _AreaSource());
    workspace.selectEntity('activity:pin');
    await workspace.searchThisArea(
      [],
      [],
      bounds: const MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.3),
    );
    expect(workspace.selectedEntityId, isNull);
    expect(workspace.task, isNotNull);
    workspace.dispose();
  });

  test('Search this area explicitly forwards the settled bounds', () async {
    final source = _AreaSource();
    final workspace = AgentWorkspaceController(source: source);
    await workspace.submit('find activities', [], []);
    final taskID = workspace.task!.id;
    const bounds = MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.3);
    await workspace.searchThisArea([], [], bounds: bounds, cityID: 'aberdeen');
    expect(source.receivedBounds, bounds);
    expect(source.receivedQuery, '搜索此区域');
    expect(source.continuedTaskID, taskID);
    expect(
      workspace.conversation[workspace.conversation.length - 2].text,
      '搜索此区域',
    );
    expect(workspace.result?.note, 'area response');
    workspace.dispose();
  });

  test(
    'area searches replace results atomically and ignore stale response',
    () async {
      final source = _PendingAreaSource();
      final workspace = AgentWorkspaceController(source: source);
      await workspace.submit('找活动', [], []);
      workspace.selectEntity('activity:one');
      final previous = workspace.result;
      const olderBounds = MapBounds(
        west: -2.2,
        south: 57.1,
        east: -2.0,
        north: 57.3,
      );
      const newerBounds = MapBounds(
        west: -2.3,
        south: 57.0,
        east: -2.1,
        north: 57.2,
      );
      final older = workspace.searchThisArea([], [], bounds: olderBounds);
      expect(workspace.result, same(previous));
      expect(workspace.selectedEntityId, 'activity:one');
      final newer = workspace.searchThisArea([], [], bounds: newerBounds);
      source.requests[newerBounds.west]!.complete(
        const AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: 'new result',
        ),
      );
      await newer;
      expect(workspace.result?.note, 'new result');
      expect(workspace.selectedEntityId, isNull);
      source.requests[olderBounds.west]!.complete(
        const AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: 'stale result',
        ),
      );
      await older;
      expect(workspace.result?.note, 'new result');
      workspace.dispose();
    },
  );
}
