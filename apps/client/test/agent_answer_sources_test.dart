import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_answer_sources.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _taskID = '8f7f6f05-f557-4a62-98e8-f841f57190be';
const _query = "St. Mary's 的开放时间";
Map<String, dynamic> sourcedWire(
  DateTime now, {
  String request = 'request-original',
}) => {
  'cityId': 'aberdeen-gb',
  'query': _query,
  'mode': 'model',
  'message': '按本次检索的官网信息说明。[1]',
  'taskId': _taskID,
  'conversationId': _taskID,
  'requestId': request,
  'activities': [],
  'places': [],
  'people': [],
  'groups': [],
  'organizations': [],
  'task': {
    'id': _taskID,
    'query': '找地点',
    'status': 'COMPLETED',
    'cityContext': 'aberdeen-gb',
    'principalType': 'PERSON',
    'principalId': 'owner-original',
    'actingUserId': 'owner-original',
    'filters': {'currentQuery': _query},
    'conversation': [
      {'role': 'user', 'text': _query},
      {'role': 'assistant', 'text': '原领域回答'},
    ],
    'updatedAt': now.toIso8601String(),
  },
  'resultSet': {
    'id': '$_taskID:original-turn',
    'taskId': _taskID,
    'schema': 'typed-agent-results-v1',
    'status': 'empty',
    'entities': [],
    'items': [],
    'generatedAt': now.toIso8601String(),
    'sources': [
      {
        'id': 'web-1',
        'title': "St. Mary's — Opening times",
        'url': 'https://official.example/opening',
        'site': 'Official site',
      },
    ],
    'answerBinding': {
      'taskId': _taskID,
      'requestId': request,
      'currentQueryDigest': agentAnswerQueryDigest(_query),
      'taskSnapshotDigest': List.filled(64, 'a').join(),
      'sourceEvidenceDigest': List.filled(64, 'b').join(),
      'runId': 'original-run',
      'generatedAt': now.toIso8601String(),
      'validUntil': now.add(const Duration(minutes: 1)).toIso8601String(),
    },
  },
  'mapEffects': {'camera': 'preserve', 'pinEntityIds': []},
};

Map<String, dynamic> _set(Map<String, dynamic> wire) =>
    wire['resultSet'] as Map<String, dynamic>;
Map<String, dynamic> _binding(Map<String, dynamic> wire) =>
    _set(wire)['answerBinding'] as Map<String, dynamic>;

void main() {
  final originalNow = DateTime.utc(2026, 10, 8, 1);
  test('旧API默认空来源；无绑定来源不进入可点消息', () {
    final old = AgentResultSet(
      id: 'old',
      status: 'ready',
      entities: [],
      generatedAt: originalNow,
    );
    expect(old.sources, isEmpty);
    expect(
      AgentAnswerSources.read(
        {'resultSet': {}},
        current: () => true,
        now: () => originalNow,
      ),
      isNull,
    );
    final wire = sourcedWire(originalNow);
    _set(wire).remove('answerBinding');
    expect(
      AgentAnswerSources.read(
        wire,
        current: () => true,
        now: () => originalNow,
      ),
      isNull,
    );
  });

  test('来源保留原文名字、顺序和HTTP地址，数据不可变', () {
    final wire = sourcedWire(originalNow);
    final answer = AgentAnswerSources.read(
      wire,
      current: () => true,
      now: () => originalNow,
    )!;
    expect(answer.taskID, _taskID);
    expect(answer.sources.single.title, "St. Mary's — Opening times");
    expect(
      answer.sources.single.uri.toString(),
      'https://official.example/opening',
    );
    expect(answer.current, isTrue);
    expect(() => answer.sources.clear(), throwsUnsupportedError);
    ((_set(wire)['sources'] as List).single as Map)['title'] = 'changed caller';
    expect(answer.sources.single.title, "St. Mary's — Opening times");
  });

  for (final field in [
    'task',
    'request',
    'query',
    'zero-digest',
    'unknown-field',
    'duplicate-source',
    'too-many',
    'future-expiry',
    'date-overflow',
  ]) {
    test('来源绑定拒绝 $field', () {
      final wire = sourcedWire(originalNow);
      switch (field) {
        case 'task':
          _binding(wire)['taskId'] = 'other-task';
        case 'request':
          _binding(wire)['requestId'] = 'other-request';
        case 'query':
          _binding(wire)['currentQueryDigest'] = agentAnswerQueryDigest(
            'different',
          );
        case 'zero-digest':
          _binding(wire)['sourceEvidenceDigest'] = List.filled(64, '0').join();
        case 'unknown-field':
          _binding(wire)['grant'] = 'MODEL_EGRESS';
        case 'duplicate-source':
          (_set(wire)['sources'] as List).add(
            (_set(wire)['sources'] as List).single,
          );
        case 'too-many':
          _set(wire)['sources'] = List.filled(11, {});
        case 'future-expiry':
          _binding(wire)['validUntil'] = originalNow
              .add(const Duration(minutes: 16))
              .toIso8601String();
        case 'date-overflow':
          _binding(wire)['validUntil'] = '2026-10-32T01:00:00Z';
      }
      expect(
        () => AgentAnswerSources.read(
          wire,
          current: () => true,
          now: () => originalNow,
        ),
        throwsFormatException,
      );
    });
  }

  for (final url in [
    'javascript:alert(1)',
    'dev-seed://place',
    '/relative',
    'https://user:secret@example.org/',
    'https://example.org/with space',
    r'https://example.org/\path',
  ]) {
    test('来源链接拒绝 $url', () {
      final wire = sourcedWire(originalNow);
      ((_set(wire)['sources'] as List).single as Map)['url'] = url;
      expect(
        () => AgentAnswerSources.read(
          wire,
          current: () => true,
          now: () => originalNow,
        ),
        throwsFormatException,
      );
    });
  }

  test('到期、身份撤销或源epoch变化永久停用，时钟回退不恢复', () {
    var now = originalNow, current = true;
    final answer = AgentAnswerSources.read(
      sourcedWire(now),
      current: () => current,
      now: () => now,
    )!;
    current = false;
    expect(answer.current, isFalse);
    current = true;
    expect(answer.current, isFalse);
    final expired = AgentAnswerSources.read(
      sourcedWire(now),
      current: () => true,
      now: () => now,
    )!;
    now = now.add(const Duration(minutes: 1));
    expect(expired.current, isFalse);
    now = originalNow;
    expect(expired.current, isFalse);
  });

  test('Remote DTO来源和same ResultSet保持一体；不生成未知地点/地图点位', () async {
    String? token = 'Bearer original-owner';
    Object epoch = Object();
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => token,
      publicEvidenceEpoch: () => epoch,
      publicEvidenceNow: () => originalNow,
      apiBaseUrl: 'https://api.test',
      client: MockClient(
        (request) async => http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': sourcedWire(
                originalNow,
                request: request.headers['X-Request-ID']!,
              ),
            }),
          ),
          200,
        ),
      ),
    );
    final result = await source.resolve(_query, [], []);
    expect(result.resultSet!.sources, hasLength(1));
    expect(result.entities, isEmpty);
    expect(result.places, isEmpty);
    expect(result.projectionItems, isEmpty);
    expect(result.mapEffects!.pinEntityIDs, isEmpty);
    expect(result.responseMessage, '按本次检索的官网信息说明。[1]');
    final answer = result.resultSet!.answerSources!;
    expect(answer.current, isTrue);
    epoch = Object();
    expect(answer.current, isFalse);
    token = null;
    source.dispose();
  });

  testWidgets('窄屏深色大字来源可读；48触控和真实链接，无效来源无调用', (t) async {
    var current = true;
    final answer = AgentAnswerSources.read(
      sourcedWire(originalNow),
      current: () => current,
      now: () => originalNow,
    )!;
    final opened = <Uri>[];
    await t.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark(),
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 700),
            textScaler: TextScaler.linear(2),
          ),
          child: Scaffold(
            body: SizedBox(
              width: 320,
              child: AgentAnswerSourcesPanel(
                answer: answer,
                replyCurrent: () => true,
                openSource: (uri) async {
                  opened.add(uri);
                  return true;
                },
              ),
            ),
          ),
        ),
      ),
    );
    await t.pump();
    final button = find.byKey(const ValueKey('agent-answer-source-web-1'));
    expect(t.getSize(button).height, greaterThanOrEqualTo(48));
    expect(find.textContaining("St. Mary's"), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.tap(button);
    await t.pump();
    expect(opened.single.toString(), 'https://official.example/opening');
    current = false;
    await t.tap(button);
    await t.pump();
    expect(opened, hasLength(1));
    expect(find.text('来源已失效，请重新查询。'), findsOneWidget);
    await t.pumpWidget(const SizedBox.shrink());
  });

  test('历史reply来源只随原task可见，不改变map选择', () {
    final answer = AgentAnswerSources.read(
      sourcedWire(originalNow),
      current: () => true,
      now: () => originalNow,
    )!;
    final result = AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '',
      taskID: _taskID,
      resultSet: AgentResultSet(
        id: 'original',
        status: 'empty',
        entities: [],
        generatedAt: originalNow,
        answerSources: answer,
      ),
    );
    final workspace = AgentWorkspaceController()
      ..task = const AgentTask(id: _taskID, query: _query, status: 'COMPLETED')
      ..result = result
      ..selectedEntityId = 'place:original-place';
    expect(workspace.retainsAnswerSources(result), isTrue);
    workspace.task = const AgentTask(
      id: 'other-task',
      query: '别的问题',
      status: 'COMPLETED',
    );
    expect(workspace.retainsAnswerSources(result), isFalse);
    expect(workspace.selectedEntityId, 'place:original-place');
    workspace.dispose();
  });

  testWidgets('解释、原实体卡和来源同属于一次assistant reply；切图不新增实体', (t) async {
    final answer = AgentAnswerSources.read(
      sourcedWire(originalNow),
      current: () => true,
      now: () => originalNow,
    )!;
    final task = AgentTask.fromJson(
      sourcedWire(originalNow)['task'] as Map<String, dynamic>,
    );
    const ref = AgentResultRef(type: 'place', id: 'original-place');
    const item = AgentResultItem(
      entity: ref,
      title: "St. Mary's",
      summary: '已发布原实体',
      scope: 'AUTHORIZED_VIEW',
      detail: ref,
      anchor: AgentResultAnchor(
        precision: 'point',
        latitude: 57.14,
        longitude: -2.1,
      ),
    );
    final result = AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '',
      message: '本轮实际来源说明。[1]',
      task: task,
      taskID: task.id,
      resultSet: AgentResultSet(
        id: 'original-set',
        status: 'ready',
        entities: [ref],
        items: [item],
        generatedAt: originalNow,
        answerSources: answer,
      ),
    );
    final workspace = AgentWorkspaceController(source: _ReplySource(result));
    await workspace.reopen(task, [], []);
    workspace.selectedEntityId = 'place:original-place';
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: workspace,
            availableHeight: 650,
            onSuggestion: (_) {},
            onRetry: () {},
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('本轮实际来源说明。[1]'), findsOneWidget);
    expect(
      find.byKey(const Key('agent-entity-place:original-place')),
      findsOneWidget,
    );
    final sources = find.byKey(const Key('agent-answer-sources'));
    expect(sources, findsOneWidget);
    expect(
      find.ancestor(of: sources, matching: find.byType(AgentConversation)),
      findsOneWidget,
    );
    expect(workspace.replies, hasLength(1));
    expect(workspace.replies.single.result, same(result));
    expect(workspace.result!.entities, hasLength(1));
    workspace.showReplyOnMap(result, 'place:original-place');
    expect(workspace.presentedResult, same(result));
    expect(workspace.selectedEntityId, 'place:original-place');
    expect(
      workspace.presentedResult!.entities.single.id,
      'place:original-place',
    );
    await t.pumpWidget(const SizedBox.shrink());
    workspace.dispose();
  });
}

class _ReplySource extends AgentTaskSource {
  const _ReplySource(this.result);
  final AgentResult result;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => result;
}
