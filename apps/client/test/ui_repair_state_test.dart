import 'dart:async';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/now_context_query_api.dart';
import 'package:flutter_test/flutter_test.dart';

AgentResult _reply(
  int turn,
  List<String> ids, {
  AgentTask? task,
  LocalPublicQueryContext? localContext,
}) => AgentResult(
  entities: const [],
  activities: const [],
  places: const [],
  note: '',
  message: '第 $turn 轮真实来源投影的测试替身',
  task: task,
  resultSet: AgentResultSet(
    id: 'result-$turn',
    status: 'ready',
    entities: [for (final id in ids) AgentResultRef(type: 'place', id: id)],
    generatedAt: DateTime.utc(2026, 10, 8, 0, turn),
    publicQueryContext: localContext,
    items: [
      for (final id in ids)
        AgentResultItem(
          entity: AgentResultRef(type: 'place', id: id),
          title: '地点 $id',
          summary: '仅用于状态边界测试',
          scope: 'AUTHORIZED_VIEW',
          detail: AgentResultRef(type: 'place', id: id),
          anchor: const AgentResultAnchor(
            precision: 'point',
            latitude: 57.14,
            longitude: -2.1,
          ),
        ),
    ],
  ),
);

class _Turns extends AgentTaskSource {
  final results = <AgentResult>[];
  final messages = <AgentMessage>[];
  final continued = <AgentTask>[];
  int turn = 0;

  AgentResult _next(String query) {
    final number = ++turn;
    messages.addAll([
      AgentMessage(role: 'user', text: query),
      AgentMessage(role: 'assistant', text: '原消息 $number'),
    ]);
    final task = AgentTask(
      id: 'authority-task',
      query: '原始地点请求',
      status: 'COMPLETED',
      cityID: 'aberdeen',
      intent: 'FIND_PLACE',
      principalType: 'ORGANIZATION',
      principalID: 'organization-one',
      actingUserID: 'person-one',
      filters: {'category': 'culture', 'serverTurn': '$number'},
      messages: List.of(messages),
    );
    final result = _reply(number, ['p$number', 'common'], task: task);
    results.add(result);
    return result;
  }

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => _next(query);

  @override
  Future<AgentResult> followUp(
    AgentTask task,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    continued.add(task);
    return _next(query);
  }

  @override
  Future<AgentResult> restore(
    AgentTask task,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => results.last;
}

class _Pending extends AgentTaskSource {
  final requests = <Completer<AgentResult>>[];
  final recentRead = Completer<List<AgentTask>>();

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) {
    final request = Completer<AgentResult>();
    requests.add(request);
    return request.future;
  }

  @override
  Future<AgentResult> followUp(
    AgentTask task,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => resolve(query, activities, places);

  @override
  Future<List<AgentTask>> loadRecent() => recentRead.future;
}

class _AnonymousTurns extends AgentTaskSource {
  final results = <AgentResult>[];
  int turn = 0;

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    final context = LocalPublicQueryContext.decode(
      {'targetIntent': 'FIND_ACTIVITY', 'category': 'culture'},
      'aberdeen',
      this,
    );
    final result = _reply(++turn, ['p$turn', 'common'], localContext: context);
    results.add(result);
    return result;
  }
}

void main() {
  test('逐轮卡片保留各自实体集合与对应助手消息，不被新回复替换', () async {
    final source = _Turns();
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    await workspace.submit('找地点', [], [], cityID: 'aberdeen');
    final first = source.results.single;
    await workspace.submit('再找一个', [], [], cityID: 'aberdeen');
    final second = source.results.last;
    expect(workspace.replies.map((r) => r.messageIndex), [1, 3]);
    expect(workspace.replies.first.result, same(first));
    expect(workspace.replies.last.result, same(second));
    expect(first.entities.map((e) => e.id), ['place:p1', 'place:common']);
    expect(second.entities.map((e) => e.id), ['place:p2', 'place:common']);
    expect(workspace.conversation[1].text, first.responseMessage);
    expect(workspace.conversation[3].text, second.responseMessage);
    expect(workspace.retainsReply(first), isTrue);
    expect(workspace.retainsReply(_reply(1, ['p1'])), isFalse);
  });

  test('会话、面板高度与输入焦点变化不丢失同一实体选择或结果', () async {
    final source = _Turns();
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    await workspace.submit('找地点', [], [], cityID: 'aberdeen');
    final result = workspace.result;
    final task = workspace.task;
    final epoch = workspace.taskEpoch;
    workspace.selectEntity('place:common');
    workspace.showContent(AgentContentMode.conversation);
    workspace.setSheetExtent(AgentSheetExtent.peek);
    workspace.beginTyping();
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    workspace.stopTyping();
    workspace.showContent(AgentContentMode.results);
    expect(workspace.selectedEntityId, 'place:common');
    expect(workspace.result, same(result));
    expect(workspace.presentedResult, same(result));
    expect(workspace.task, same(task));
    expect(workspace.taskEpoch, epoch);
    expect(workspace.replies.single.result, same(result));
  });

  test('新回复仍含已选实体则保留选择，消失的实体才清除', () async {
    final source = _Turns();
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    await workspace.submit('找地点', [], [], cityID: 'aberdeen');
    workspace.selectEntity('place:common');
    await workspace.submit('近一点', [], [], cityID: 'aberdeen');
    expect(workspace.selectedEntityId, 'place:common');
    workspace.selectEntity('place:p2');
    await workspace.submit('换一个', [], [], cityID: 'aberdeen');
    expect(workspace.selectedEntityId, isNull);
    expect(workspace.replies, hasLength(3));
  });

  test('历史结果驱动地图而最新任务、主体和追问上下文继续独立', () async {
    final source = _Turns();
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    await workspace.submit('找地点', [], [], cityID: 'aberdeen');
    final first = source.results.single;
    await workspace.submit('近一点', [], [], cityID: 'aberdeen');
    final second = source.results.last;
    final latestTask = workspace.task;
    final epoch = workspace.taskEpoch;
    workspace.showReplyOnMap(first, 'place:p1');
    expect(workspace.presentedResult, same(first));
    expect(workspace.result, same(second));
    expect(workspace.selectedEntityId, 'place:p1');
    expect(workspace.task, same(latestTask));
    expect(workspace.taskEpoch, epoch);
    expect(workspace.selectMapEntity('place:common'), isTrue);
    workspace.showContent(AgentContentMode.conversation);
    expect(workspace.presentedResult, same(first));
    await workspace.submit('周末也可以吗', [], [], cityID: 'aberdeen');
    expect(source.continued.last, same(latestTask));
    expect(source.continued.last.principalID, 'organization-one');
    expect(source.continued.last.actingUserID, 'person-one');
    expect(source.continued.last.filters['serverTurn'], '2');
    expect(workspace.presentedResult, same(source.results.last));
    expect(workspace.result, same(source.results.last));
  });

  test('匿名历史快照也可点选其余地图实体，不改变最新续问条件', () async {
    final source = _AnonymousTurns();
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    await workspace.submit('找地点', [], [], cityID: 'aberdeen');
    final first = source.results.single;
    expect(workspace.selectMapEntity('place:p1'), isTrue);
    await workspace.submit('再找一个', [], [], cityID: 'aberdeen');
    final latest = workspace.task!.lastSuccessfulPublicQuery;
    expect(latest, same(source.results.last.resultSet!.publicQueryContext));
    workspace.showReplyOnMap(first, 'place:p1');
    expect(workspace.presentedResult, same(first));
    expect(workspace.selectMapEntity('place:common'), isTrue);
    expect(workspace.selectedEntityId, 'place:common');
    expect(workspace.task!.lastSuccessfulPublicQuery, same(latest));
  });

  test('不属于当前任务的结果、不存在实体和线上结果不能激活地图', () async {
    final source = _Turns();
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    await workspace.submit('找地点', [], [], cityID: 'aberdeen');
    final initial = workspace.presentedResult;
    workspace.selectEntity('place:common');
    workspace.showReplyOnMap(_reply(99, ['outside']), 'place:outside');
    workspace.showReplyOnMap(initial!, 'place:missing');
    expect(workspace.presentedResult, same(initial));
    expect(workspace.selectedEntityId, 'place:common');
    workspace.queryContextType = 'ONLINE';
    workspace.showReplyOnMap(initial, 'place:p1');
    expect(workspace.selectedEntityId, 'place:common');
    final online = AgentResult(
      entities: initial.entities,
      activities: const [],
      places: const [],
      note: '线上替身',
      onlineContext: const NowOnlineContext(id: 'online', label: '线上'),
    );
    workspace.queryContextType = 'CITY';
    workspace.result = online;
    workspace.showReplyOnMap(online, 'place:p1');
    expect(workspace.selectedEntityId, 'place:common');
  });

  test('同会话重新打开仍保留前轮快照，刷新当前轮不重写早期轮次', () async {
    final source = _Turns();
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    await workspace.submit('找地点', [], [], cityID: 'aberdeen');
    final first = source.results.single;
    await workspace.submit('换一个', [], [], cityID: 'aberdeen');
    final previous = workspace.task!;
    workspace.newTask();
    expect(workspace.replies, isEmpty);
    expect(workspace.retainsReply(first), isFalse);
    await workspace.reopen(previous, [], []);
    expect(workspace.replies, hasLength(2));
    expect(workspace.replies.first.messageIndex, 1);
    expect(workspace.replies.first.result, same(first));
    expect(workspace.replies.last.messageIndex, 3);
    expect(workspace.replies.last.result, same(source.results.last));
  });

  for (final retireOnly in [false, true]) {
    test('${retireOnly ? 'retire' : 'clear'}账号context清除快照且拦截迟到回复和历史', () async {
      final source = _Pending();
      final workspace = AgentWorkspaceController(source: source);
      addTearDown(workspace.dispose);
      final firstRead = workspace.submit('找地点', [], [], cityID: 'aberdeen');
      final first = _reply(1, ['p1']);
      source.requests.single.complete(first);
      await firstRead;
      final previous = workspace.task!;
      workspace.showReplyOnMap(first, 'place:p1');
      final pendingRead = workspace.submit('再找一个', [], [], cityID: 'aberdeen');
      final pendingHistory = workspace.loadRecent();
      if (retireOnly) {
        workspace.retireAccountContext();
      } else {
        workspace.clearAccountContext();
      }
      expect(workspace.retainsReply(first), isFalse);
      expect(workspace.replies, isEmpty);
      expect(workspace.task, isNull);
      expect(workspace.result, isNull);
      expect(workspace.presentedResult, isNull);
      expect(workspace.selectedEntityId, isNull);
      expect(workspace.conversation, isEmpty);
      source.requests.last.complete(_reply(2, ['p2'], task: previous));
      source.recentRead.complete([previous]);
      await Future.wait([pendingRead, pendingHistory]);
      expect(workspace.task, isNull);
      expect(workspace.result, isNull);
      expect(workspace.presentedResult, isNull);
      expect(workspace.recent, isEmpty);
      expect(workspace.replies, isEmpty);
      workspace.showReplyOnMap(first, 'place:p1');
      expect(workspace.selectedEntityId, isNull);
    });
  }
}
