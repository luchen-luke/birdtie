import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_answer_sources.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _task = '8f7f6f05-f557-4a62-98e8-f841f57190be';
Map<String, dynamic> _message(int turn) => {
  'role': 'assistant',
  'text': '第 $turn 轮持久回答。[1]',
  'sources': [
    {
      'id': 'web-$turn',
      'title': turn == 1 ? 'Aberdeen Maritime Museum 官网' : "St. Mary's 开放时间",
      'url': 'https://example.org/source-$turn',
      'site': '来源原名',
    },
  ],
  'sourceRunId': 'source-run-$turn',
  'sourceEvidenceDigest': List.filled(64, turn == 1 ? 'a' : 'b').join(),
};

Map<String, dynamic> _wire({int turns = 2}) => {
  'taskId': _task,
  'conversationId': _task,
  'requestId': 'read-request',
  'message': '第 $turns 轮持久回答。[1]',
  'activities': [],
  'places': [],
  'people': [],
  'groups': [],
  'organizations': [],
  'task': {
    'id': _task,
    'query': '原始问题',
    'status': 'COMPLETED',
    'cityContext': 'aberdeen-gb',
    'principalType': 'PERSON',
    'principalId': 'owner-original',
    'filters': {'currentQuery': '第 $turns 轮问题'},
    'conversation': [
      for (var i = 1; i <= turns; i++) ...[
        {'role': 'user', 'text': '第 $i 轮问题'},
        _message(i),
      ],
    ],
  },
  'resultSet': {
    'id': '$_task:result-$turns',
    'taskId': _task,
    'schema': 'typed-agent-results-v1',
    'status': 'ready',
    'generatedAt': '2026-10-08T01:00:00Z',
    'entities': [
      {'type': 'place', 'id': 'original-place'},
    ],
    'items': [
      {
        'entityRef': {'type': 'place', 'id': 'original-place'},
        'detailRef': {'type': 'place', 'id': 'original-place'},
        'title': '原地图地点',
        'summary': '原实体',
        'scope': 'AUTHORIZED_VIEW',
        'anchor': {
          'coordinateSystem': 'wgs84',
          'precision': 'point',
          'latitude': 57.14,
          'longitude': -2.1,
        },
      },
    ],
    'sources': [],
  },
  'mapEffects': {
    'camera': 'preserve',
    'pinEntityIds': ['place:original-place'],
  },
};

http.Response _response(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json'},
);

RemoteAgentTaskSource _remote(
  http.Client client, {
  String? Function()? token,
  Object? Function()? epoch,
}) => RemoteAgentTaskSource(
  cityID: () => 'aberdeen-gb',
  authorizationHeader: token ?? () => 'Bearer owner-original',
  publicEvidenceEpoch: epoch,
  apiBaseUrl: 'http://localhost:8080',
  client: client,
);

void main() {
  test('消息来源解析不可变，旧消息和缺少metadata的来源兼容', () {
    final raw = _message(1);
    final message = AgentMessage.fromJson(raw, sourceCurrent: () => true);
    expect(message.sources.single.title, 'Aberdeen Maritime Museum 官网');
    expect(message.sourceReferences!.runID, 'source-run-1');
    expect(message.sourceReferences!.current, isTrue);
    expect(() => message.sources.clear(), throwsUnsupportedError);
    (raw['sources'] as List).first['title'] = 'caller modified';
    expect(message.sources.single.title, 'Aberdeen Maritime Museum 官网');
    expect(
      message.withText('另一个展示文本').sourceReferences,
      same(message.sourceReferences),
    );
    expect(
      AgentMessage.fromJson({'role': 'assistant', 'text': '旧回答'}).sources,
      isEmpty,
    );
    final unbound = _message(1)..remove('sourceRunId');
    expect(AgentMessage.fromJson(unbound).sources, isEmpty);
    final user = _message(1)..['role'] = 'user';
    expect(AgentMessage.fromJson(user).sources, isEmpty);
    expect(
      AgentMessage.fromJson(_message(1)).sourceReferences!.current,
      isFalse,
    );
  });

  for (final url in [
    'javascript:alert(1)',
    'dev-seed://place',
    '/relative',
    'https://user:secret@example.org/a',
    'https://example.org/with space',
  ]) {
    test('持久消息来源拒绝链接 $url', () {
      final raw = _message(1);
      (raw['sources'] as List).first['url'] = url;
      expect(() => AgentMessage.fromJson(raw), throwsFormatException);
    });
  }

  test('Recent和GET按UTF8恢复每条消息来源，不调用POST模型', () async {
    final methods = <String>[];
    final source = _remote(
      MockClient((request) async {
        methods.add(request.method);
        return _response(
          request.url.path.endsWith('/agent-tasks')
              ? [_wire()['task']]
              : _wire(),
        );
      }),
    );
    final recent = await source.loadRecent();
    expect(
      recent.single.messages[1].sources.single.title,
      'Aberdeen Maritime Museum 官网',
    );
    final result = await source.restore(recent.single, [], []);
    expect(result.task!.messages[3].sources.single.title, "St. Mary's 开放时间");
    expect(result.messageSources!.runID, 'source-run-2');
    expect(result.entities.single.id, 'place:original-place');
    expect(methods, ['GET', 'GET']);
    source.dispose();
  });

  test('POST到后续GET恢复来源历史，当前结果集合和地图选择保持', () async {
    final methods = <String>[];
    final source = _remote(
      MockClient((request) async {
        methods.add(request.method);
        return _response(_wire(turns: request.method == 'POST' ? 1 : 2));
      }),
    );
    final first = await source.resolve('第 1 轮问题', [], []);
    expect(
      first.task!.messages.last.sources.single.title,
      'Aberdeen Maritime Museum 官网',
    );
    final workspace = AgentWorkspaceController(source: source);
    await workspace.reopen(first.task!, [], []);
    workspace.selectedEntityId = 'place:original-place';
    final current = workspace.result!;
    final currentSet = current.resultSet;
    expect(workspace.conversation[1].sourceReferences!.runID, 'source-run-1');
    expect(workspace.conversation[3].sourceReferences!.runID, 'source-run-2');
    expect(workspace.replies, hasLength(2));
    final historical = workspace.replies.first.result;
    expect(historical.hasOnlyMessageSources, isTrue);
    expect(historical.entities, isEmpty);
    expect(historical.projectionItems, isNull);
    workspace.showReplyOnMap(historical, 'place:original-place');
    expect(workspace.presentedResult, same(current));
    expect(workspace.result!.resultSet, same(currentSet));
    expect(workspace.selectedEntityId, 'place:original-place');
    expect(workspace.result!.entities.map((e) => e.id), [
      'place:original-place',
    ]);
    expect(methods, ['POST', 'GET']);
    workspace.dispose();
  });

  test('最新typed计数不覆盖已认证持久回答，身份epoch变化永久停止历史链接', () async {
    var epoch = 1;
    final wire = _wire()..['message'] = '找到 1 个当前地点。';
    final source = _remote(
      MockClient((_) async => _response(wire)),
      epoch: () => epoch,
    );
    final workspace = AgentWorkspaceController(source: source);
    await workspace.reopen(
      AgentTask.fromJson(wire['task'] as Map<String, dynamic>),
      [],
      [],
    );
    final first = workspace.conversation[1].sourceReferences!;
    final latest = workspace.conversation[3].sourceReferences!;
    expect(workspace.conversation.last.text, '第 2 轮持久回答。[1]');
    expect(first.current, isTrue);
    expect(latest.current, isTrue);
    epoch = 2;
    expect(first.current, isFalse);
    expect(latest.current, isFalse);
    epoch = 1;
    expect(first.current, isFalse);
    expect(latest.current, isFalse);
    workspace.dispose();
  });

  for (final changed in [
    'account',
    'organization',
    'city',
    'online',
    'owner',
    'epoch',
    'dispose',
  ]) {
    test('历史来源在 $changed 变化后永久停止，旧身份不能复活', () async {
      var token = 'Bearer original';
      String? organization;
      var city = 'aberdeen-gb';
      String? online;
      var owner = 'owner-original';
      var epoch = 1;
      final source = RemoteAgentTaskSource(
        cityID: () => city,
        authorizationHeader: () => token,
        organizationWorkspaceID: () => organization,
        onlineContextID: () => online,
        publicEvidenceOwnerID: () => owner,
        publicEvidenceEpoch: () => epoch,
        apiBaseUrl: 'http://localhost:8080',
        client: MockClient((_) async => _response(_wire())),
      );
      final result = await source.restore(
        AgentTask.fromJson(_wire()['task'] as Map<String, dynamic>),
        [],
        [],
      );
      final refs = result.task!.messages[1].sourceReferences!;
      expect(refs.current, isTrue);
      switch (changed) {
        case 'account':
          token = 'Bearer different';
        case 'organization':
          organization = 'organization-other';
        case 'city':
          city = 'edinburgh-gb';
        case 'online':
          online = 'online-other';
        case 'owner':
          owner = 'owner-other';
        case 'epoch':
          epoch = 2;
        case 'dispose':
          source.dispose();
      }
      expect(refs.current, isFalse);
      token = 'Bearer original';
      organization = null;
      city = 'aberdeen-gb';
      online = null;
      owner = 'owner-original';
      epoch = 1;
      expect(refs.current, isFalse);
      if (changed != 'dispose') source.dispose();
    });
  }

  testWidgets('恢复两条回答各有一组来源，同消息流且不重复卡片', (t) async {
    final source = _remote(MockClient((_) async => _response(_wire())));
    final workspace = AgentWorkspaceController(source: source);
    await workspace.reopen(
      AgentTask.fromJson(_wire()['task'] as Map<String, dynamic>),
      [],
      [],
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: workspace,
            availableHeight: 700,
            onSuggestion: (_) {},
            onRetry: () {},
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    final panels = find.byKey(const Key('agent-answer-sources'));
    expect(panels, findsNWidgets(2));
    expect(find.byType(AgentConversation), findsOneWidget);
    for (var index = 0; index < 2; index++) {
      expect(
        find.ancestor(
          of: panels.at(index),
          matching: find.byType(AgentConversation),
        ),
        findsOneWidget,
      );
    }
    expect(find.text('第 1 轮持久回答。[1]'), findsOneWidget);
    expect(find.text('第 2 轮持久回答。[1]'), findsOneWidget);
    expect(find.text('[1] Aberdeen Maritime Museum 官网 · 来源原名'), findsOneWidget);
    expect(find.text("[1] St. Mary's 开放时间 · 来源原名"), findsOneWidget);
    expect(
      find.byKey(const Key('agent-entity-place:original-place')),
      findsOneWidget,
    );
    expect(find.text('当前没有可展示的结果。'), findsNothing);
    await t.pumpWidget(const SizedBox.shrink());
    workspace.dispose();
  });

  testWidgets('续问保留旧消息来源；同消息引用不受旧livebinding过期影响', (t) async {
    var now = DateTime.utc(2026, 10, 8, 1);
    var calls = 0;
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => 'Bearer owner-original',
      apiBaseUrl: 'http://localhost:8080',
      publicEvidenceNow: () => now,
      client: MockClient((request) async {
        calls++;
        final wire = _wire(turns: calls);
        if (calls == 1) {
          final set = wire['resultSet'] as Map<String, dynamic>;
          final requestID = request.headers['x-request-id']!;
          wire['requestId'] = requestID;
          set['sources'] = _message(1)['sources'];
          set['answerBinding'] = {
            'taskId': _task,
            'requestId': requestID,
            'currentQueryDigest': agentAnswerQueryDigest('第 1 轮问题'),
            'taskSnapshotDigest': List.filled(64, 'c').join(),
            'sourceEvidenceDigest': List.filled(64, 'a').join(),
            'runId': 'source-run-1',
            'generatedAt': now.toIso8601String(),
            'validUntil': now.add(const Duration(minutes: 1)).toIso8601String(),
          };
        }
        return _response(wire);
      }),
    );
    final workspace = AgentWorkspaceController(source: source);
    await workspace.submit('第 1 轮问题', [], [], cityID: 'aberdeen-gb');
    final firstReply = workspace.replies.single.result;
    final firstMessage = workspace.conversation[1];
    expect(firstReply.resultSet!.answerSources!.current, isTrue);
    now = now.add(const Duration(minutes: 2));
    await workspace.submit('第 2 轮问题', [], [], cityID: 'aberdeen-gb');
    expect(workspace.replies.first.result, same(firstReply));
    expect(firstMessage.sources.single.title, 'Aberdeen Maritime Museum 官网');
    expect(firstMessage.sourceReferences!.runID, 'source-run-1');
    expect(firstReply.resultSet!.answerSources!.current, isFalse);
    expect(workspace.conversation[1].text, firstMessage.text);
    expect(workspace.conversation[1].sourceReferences!.current, isTrue);
    expect(workspace.conversation[3].sourceReferences!.runID, 'source-run-2');
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: workspace,
            availableHeight: 700,
            onSuggestion: (_) {},
            onRetry: () {},
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('这条回答的来源'), findsNWidgets(2));
    expect(find.text('来源已失效，请重新查询。'), findsNothing);
    final earlier = find.byKey(const ValueKey('agent-answer-source-web-1'));
    expect(t.widget<TextButton>(earlier).onPressed, isNotNull);
    expect(
      find.byKey(const Key('agent-entity-place:original-place')),
      findsNWidgets(2),
    );
    await t.pumpWidget(const SizedBox.shrink());
    workspace.dispose();
  });

  testWidgets('历史来源可点HTTP且撤权后停用；没有新实体或模型权限', (t) async {
    var current = true;
    final refs = AgentMessage.fromJson(
      _message(1),
      sourceCurrent: () => current,
    ).sourceReferences!;
    final opened = <Uri>[];
    Widget page() => MaterialApp(
      home: Scaffold(
        body: AgentAnswerSourcesPanel.persisted(
          sources: refs,
          replyCurrent: () => true,
          openSource: (uri) async {
            opened.add(uri);
            return true;
          },
        ),
      ),
    );
    await t.pumpWidget(page());
    final button = find.byKey(const ValueKey('agent-answer-source-web-1'));
    expect(t.getSize(button).height, greaterThanOrEqualTo(48));
    await t.tap(button);
    await t.pump();
    expect(opened.single.toString(), 'https://example.org/source-1');
    current = false;
    await t.pumpWidget(page());
    expect(t.widget<TextButton>(button).onPressed, isNull);
    current = true;
    await t.pumpWidget(page());
    expect(t.widget<TextButton>(button).onPressed, isNull);
    await t.pumpWidget(const SizedBox.shrink());
  });
}
