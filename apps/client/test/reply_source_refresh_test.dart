import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_reply_membership.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'reply_history_restore_test.dart' as history;

// Public citation/proof fields from API24 expired-reply-read24-01's native GET.
// Task/Place IDs are substituted with existing synthetic protocol fixture IDs;
// the mocked response grants a fresh30s read. The persisted +08:00 observation
// remains unchanged and expired: it is not used to authorize or renew a read.
// No native/provider request, licence review, model Run or URL launch occurs.
const _sourceURL = 'https://www.openstreetmap.org/node/290633294';
const _title = '© OpenStreetMap contributors (ODbL 1.0)';
const _digests = [
  '63e36ba8c7a672f32881098fff1183fe6644b6eea359e9a0b065d0bc6018600f',
  '44f033c0adaedacc94ddb09dcfc533677dc45c7717d175fd602ef96aced65b19',
];
const _observed = [
  '2026-10-09T03:50:09.001635+08:00',
  '2026-10-09T03:53:02.207871+08:00',
];
const _texts = [
  'Glenhouse的地址是：201 Union Street AB11 6BB Aberdeen [source-1]。',
  'Glenhouse的营业时间记录：“Mo-Su 09:00-22:30”。该字段最近核验于2026-10-08T19:22:45Z；需在2026-10-09T01:18:23Z前刷新，不代表当前开门状态 [source-1]。',
];
const _facts = [
  ('6a4159bf-4a48-4da9-8411-6e438c646060', 'address'),
  ('58441212-2378-4da8-b546-e9d7b8b942b0', 'coordinates'),
  ('1c6f757d-5eca-49e4-ae0f-a1b886d6a671', 'name'),
  ('f7da2171-d8cb-46bd-aee7-8e3f1d2895ee', 'opening_hours'),
];

Map<String, dynamic> _localWire(
  DateTime now, {
  bool omitFirst = false,
  bool omitAll = false,
}) {
  final wire = history.historyWire(now);
  final task = wire['task'] as Map<String, dynamic>;
  task['createdAt'] = '2026-10-08T19:50:08Z';
  task['updatedAt'] = '2026-10-08T19:53:01Z';
  final messages = task['conversation'] as List;
  final rounds = wire['messageResults'] as List;
  for (var turn = 0; turn < 2; turn++) {
    final index = turn * 2 + 1;
    final message = messages[index] as Map<String, dynamic>;
    message['text'] = _texts[turn];
    message['sources'] = [
      {'id': 'source-1', 'title': _title, 'url': _sourceURL},
    ];
    message['sourceEvidenceDigest'] = _digests[turn];
    message['localReplyEvidence'] = {
      'schema': 'birdtie.local-place-reply.v1',
      'id': 'local-native:${history.taskID}:$index',
      'sourceEvidenceDigest': _digests[turn],
      'validUntil': _observed[turn],
      'facts': [
        for (final fact in _facts.take(turn == 0 ? 3 : 4))
          {
            'evidenceId': fact.$1,
            'placeId': history.placeA,
            'field': fact.$2,
            'version': 1,
            'permissionId': '1a9ef88a-4ac5-46f4-aa3a-d5218aabb7e0',
          },
      ],
      'memoryUses': [],
      'reason': 'LOCAL_FIELDS_SUFFICIENT',
      'searchCalls': 0,
      'modelCalls': 0,
    };
    final digest = agentReplyTurnDigest(
      messages
          .take(index + 1)
          .map((m) => (role: m['role'] as String, text: m['text'] as String)),
    );
    final setID = 'reply:${history.taskID}:${digest.substring(0, 24)}';
    final ref = {'type': 'place', 'id': history.placeA};
    message['resultMembership']['turnDigest'] = digest;
    message['resultMembership']['resultSetId'] = setID;
    message['resultMembership']['refs'] = [ref];
    final round = rounds[turn] as Map<String, dynamic>;
    round['turnDigest'] = digest;
    round['validUntil'] = now
        .add(const Duration(seconds: 30))
        .toIso8601String();
    final set = round['resultSet'] as Map<String, dynamic>;
    set['id'] = setID;
    set['entities'] = [ref];
    final item = (set['items'] as List).single as Map<String, dynamic>;
    item['entityRef'] = ref;
    item['detailRef'] = ref;
    item['title'] = 'Glenhouse';
    item['anchor']['latitude'] = 57.14;
    round['mapEffects']['pinEntityIds'] = ['place:${history.placeA}'];
  }
  wire['message'] = messages.last['text'];
  wire['resultSet'] = rounds.last['resultSet'];
  wire['mapEffects'] = rounds.last['mapEffects'];
  if (omitFirst) rounds.removeAt(0);
  if (omitAll) {
    rounds.clear();
    wire['resultSet']['status'] = 'empty';
    wire['resultSet']['entities'] = [];
    wire['resultSet']['items'] = [];
    wire['mapEffects']['pinEntityIds'] = [];
  }
  return jsonDecode(jsonEncode(wire)) as Map<String, dynamic>;
}

class _Reads {
  DateTime now = DateTime.utc(2026, 10, 8, 19, 57, 4);
  String? token = 'Bearer synthetic-local-proof', org;
  String city = history.cityID;
  int epoch = 0, status = 200;
  bool visible = true, omitFirst = false, omitAll = false;
  Completer<http.Response>? pending;
  void Function(Map<String, dynamic>)? mutate;
  final methods = <String>[];
  late final RemoteAgentTaskSource source = RemoteAgentTaskSource(
    cityID: () => city,
    authorizationHeader: () => token,
    organizationWorkspaceID: () => org,
    publicEvidenceOwnerID: () => history.ownerID,
    publicEvidenceEpoch: () => epoch,
    publicEvidenceNow: () => now,
    apiBaseUrl: 'http://localhost:8080',
    client: MockClient((r) async {
      methods.add(r.method);
      expectSync(r.method, 'GET');
      expectSync(r.url.path, '/v1/me/agent-tasks/${history.taskID}');
      return pending?.future ?? Future.value(response());
    }),
  );
  late final AgentWorkspaceController workspace = AgentWorkspaceController(
    source: source,
    replyRefreshAllowed: () => visible,
  );
  http.Response response() {
    final wire = _localWire(now, omitFirst: omitFirst, omitAll: omitAll);
    mutate?.call(wire);
    return http.Response.bytes(
      utf8.encode(jsonEncode({'data': wire})),
      status,
      headers: {'content-type': 'application/json'},
    );
  }

  Future<void> open() => workspace.reopen(
    AgentTask.fromJson(_localWire(now)['task'] as Map<String, dynamic>),
    [],
    [],
  );
  Future<void> elapse(WidgetTester t, int seconds) async {
    now = now.add(Duration(seconds: seconds));
    await t.pump(Duration(seconds: seconds));
    await t.pump();
  }

  bool _disposed = false;
  void dispose() {
    if (!_disposed) {
      _disposed = true;
      workspace.dispose();
    }
  }
}

Future<void> _mountConversation(
  WidgetTester t,
  _Reads f,
  TextEditingController draft,
) => t.pumpWidget(
  MaterialApp(
    home: Scaffold(
      body: Column(
        children: [
          Expanded(
            child: AnimatedBuilder(
              animation: f.workspace,
              builder: (_, _) => AgentConversation(
                workspace: f.workspace,
                onSuggestion: (_) {},
                onRetry: () {},
              ),
            ),
          ),
          TextField(controller: draft),
        ],
      ),
    ),
  ),
);

void _sameImmutableMessages(List<AgentMessage> old, List<AgentMessage> fresh) {
  expect(fresh, hasLength(old.length));
  for (var i = 0; i < old.length; i++) {
    expect(fresh[i].role, old[i].role);
    expect(fresh[i].text, old[i].text);
    expect(fresh[i].resultMembership, same(old[i].resultMembership));
  }
}

void main() {
  testWidgets('原DTO与过去观察时间在下一原GET严格解析为全新30s读权限', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    f.now = f.now.add(const Duration(seconds: 31));
    final fresh = await f.source.rereadReplyProjections(f.workspace.task!);
    final original = f.workspace.task!;
    final actual = fresh!.task!;
    expect(actual.id, original.id);
    expect(actual.query, original.query);
    expect(actual.status, original.status);
    expect(actual.intent, original.intent);
    expect(actual.cityID, original.cityID);
    expect(actual.contextType, original.contextType);
    expect(actual.contextID, original.contextID);
    expect(actual.principalType, original.principalType);
    expect(actual.principalID, original.principalID);
    expect(actual.actingUserID, original.actingUserID);
    expect(actual.filters, original.filters);
    expect(actual.createdAt, original.createdAt);
    expect(actual.updatedAt, original.updatedAt);
    for (var i = 0; i < original.messages.length; i++) {
      expect(actual.messages[i].role, original.messages[i].role);
      expect(actual.messages[i].text, original.messages[i].text);
      expect(
        actual.messages[i].resultMembership?.turnDigest,
        original.messages[i].resultMembership?.turnDigest,
      );
      expect(
        actual.messages[i].resultMembership?.resultSetID,
        original.messages[i].resultMembership?.resultSetID,
      );
      expect(
        actual.messages[i].resultMembership?.refs,
        original.messages[i].resultMembership?.refs,
      );
    }
    expect(fresh.messageResults![1]!.replyProjection!.current, true);
    expect(fresh.task!.messages[1].sourceReferences!.current, true);
    f.dispose();
  });
  testWidgets('API24本地proof旧30s到期后，原GET同时恢复消息来源与同实体地图，不延期旧ref', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace, task = w.task, taskEpoch = w.taskEpoch;
    final messages = List<AgentMessage>.of(w.conversation);
    final oldRefs = w.conversation[1].sourceReferences!;
    final oldReply = w.replies.first.result;
    expect(
      oldRefs.current,
      true,
      reason: 'Initial native GET must grant this exact local round',
    );
    final draft = TextEditingController(text: '未发送的输入');
    addTearDown(draft.dispose);
    w.showReplyOnMap(oldReply, 'place:${history.placeA}');
    w.beginTyping();
    await _mountConversation(t, f, draft);
    f.pending = Completer<http.Response>();
    await f.elapse(t, 25);
    expect(f.methods, ['GET', 'GET']);
    await f.elapse(t, 6);
    expect(oldRefs.current, false);
    expect(w.presentedResult!.entities, isEmpty);
    expect(w.selectedEntityId, 'place:${history.placeA}');
    expect(
      t
          .widget<TextButton>(find.widgetWithText(TextButton, '来源 1').first)
          .onPressed,
      isNull,
    );
    final flight = w.refreshReplyProjections();
    f.pending!.complete(f.response());
    await t.pump();
    expect(await flight, true);
    await t.pump();
    final refs = w.conversation[1].sourceReferences!;
    expect(refs, isNot(same(oldRefs)));
    expect(
      refs.current,
      true,
      reason:
          'fresh remaining=${refs.remaining}; projection=${w.result!.messageResults![1]!.replyProjection!.current}; now=${f.now}; until=${w.result!.messageResults![1]!.replyProjection!.validUntil}',
    );
    expect(refs.remaining!.inSeconds, 30);
    expect(refs.localObservedValidUntil, oldRefs.localObservedValidUntil);
    expect(refs.localObservedValidUntil!.isBefore(f.now), true);
    expect(refs.sourceEvidenceDigest, _digests[0]);
    expect(refs.localReplyID, 'local-native:${history.taskID}:1');
    expect(
      w.conversation[3].sourceReferences!.sourceEvidenceDigest,
      _digests[1],
    );
    expect(oldRefs.current, false);
    expect(w.retainsMessageSources(oldReply, oldRefs), false);
    expect(w.presentedResult!.entities.single.id, 'place:${history.placeA}');
    expect(w.selectedEntityId, 'place:${history.placeA}');
    expect(w.preserveReplyCamera, true);
    expect(w.task, same(task));
    expect(w.taskEpoch, taskEpoch);
    _sameImmutableMessages(messages, w.conversation);
    expect(draft.text, '未发送的输入');
    expect(w.inputFocused, true);
    expect(w.contentMode, AgentContentMode.conversation);
    expect(w.requestError, isNull);
    await t.tap(find.widgetWithText(TextButton, '来源 1').first);
    await t.pumpAndSettle();
    expect(find.text('来源详情'), findsOneWidget);
    expect(find.text(_title), findsOneWidget);
    expect(find.text(_sourceURL), findsOneWidget);
    expect(
      t.widget<TextButton>(find.widgetWithText(TextButton, '打开原文')).onPressed,
      isNotNull,
    );
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });

  testWidgets('每25s自动GET换当轮lambda，超过原TTL后两轮同source-1仍各有自己digest', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace, task = w.task;
    final originals = [
      w.conversation[1].sourceReferences!,
      w.conversation[3].sourceReferences!,
    ];
    for (var round = 0; round < 2; round++) {
      await f.elapse(t, 25);
      for (var i = 0; i < 2; i++) {
        final refs = w.conversation[i * 2 + 1].sourceReferences!;
        expect(refs, isNot(same(originals[i])));
        expect(refs.current, true);
        expect(refs.sourceEvidenceDigest, _digests[i]);
        expect(
          refs.localReplyID,
          'local-native:${history.taskID}:${i * 2 + 1}',
        );
        expect(refs.sources.single.uri.toString(), _sourceURL);
      }
    }
    expect(originals.every((s) => !s.current), true);
    expect(w.task, same(task));
    expect(f.methods, ['GET', 'GET', 'GET']);
    f.dispose();
  });

  testWidgets('旧轮缺read projection时新来源保持不可点，不能借当前同Place同URL的lease', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace, old = w.conversation[1].sourceReferences!;
    w.showReplyOnMap(w.replies.first.result, 'place:${history.placeA}');
    f.omitFirst = true;
    expect(await w.refreshReplyProjections(), true);
    expect(w.conversation[1].sourceReferences, isNot(same(old)));
    expect(w.conversation[1].sourceReferences!.current, false);
    expect(w.conversation[3].sourceReferences!.current, true);
    expect(w.result!.entities.single.id, 'place:${history.placeA}');
    expect(w.presentedResult!.entities, isEmpty);
    expect(w.selectedEntityId, isNull);
    f.dispose();
  });

  testWidgets('撤权GET空history关闭每轮来源与pin，不保留旧source或改Task正文', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace, original = w.task;
    final messages = List<AgentMessage>.of(w.conversation);
    w.showReplyOnMap(w.replies.first.result, 'place:${history.placeA}');
    f.omitAll = true;
    expect(await w.refreshReplyProjections(), true);
    for (final index in [1, 3]) {
      expect(
        w.conversation[index].sourceReferences,
        isNot(same(messages[index].sourceReferences)),
      );
      expect(w.conversation[index].sourceReferences!.current, false);
      expect(
        w.retainsMessageSources(w.result!, messages[index].sourceReferences!),
        false,
      );
    }
    expect(w.presentedResult!.entities, isEmpty);
    expect(w.selectedEntityId, isNull);
    expect(w.task, same(original));
    _sameImmutableMessages(messages, w.conversation);
    f.dispose();
  });

  testWidgets('失败GET保留消息source对象与输入状态，旧lease仍到期且不循环或POST', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace, task = w.task;
    final messages = List<AgentMessage>.of(w.conversation);
    w.beginTyping();
    f.status = 503;
    await f.elapse(t, 25);
    await f.elapse(t, 90);
    expect(w.task, same(task));
    expect(w.conversation, orderedEquals(messages));
    expect(w.conversation[1].sourceReferences!.current, false);
    expect(w.inputFocused, true);
    expect(w.contentMode, AgentContentMode.conversation);
    expect(w.requestError, isNull);
    expect(f.methods, ['GET', 'GET']);
    f.dispose();
  });

  testWidgets('fresh Task或filters不匹配先拒绝，不能部分写入source', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace, task = w.task;
    final messages = List<AgentMessage>.of(w.conversation);
    f.mutate = (wire) {
      wire['task']['filters']['searchTerm'] = '其他地点';
    };
    expect(await w.refreshReplyProjections(), false);
    expect(w.task, same(task));
    expect(w.conversation, orderedEquals(messages));
    expect(w.requestError, isNull);
    f.dispose();
  });

  testWidgets('新Task期间迟到GET不把原来源或消息写入新会话', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    f.pending = Completer<http.Response>();
    final flight = f.workspace.refreshReplyProjections();
    await t.pump();
    f.workspace.newTask();
    f.pending!.complete(f.response());
    await t.pump();
    expect(await flight, false);
    expect(f.workspace.task, isNull);
    expect(f.workspace.conversation, isEmpty);
    expect(f.workspace.replies, isEmpty);
    expect(f.methods, ['GET', 'GET']);
    f.dispose();
  });

  for (final boundary in ['token', 'org', 'city', 'epoch']) {
    testWidgets('$boundary ABA同步退休后迟到GET不更新conversation source', (t) async {
      final f = _Reads();
      addTearDown(f.dispose);
      await f.open();
      final w = f.workspace, task = w.task;
      final messages = List<AgentMessage>.of(w.conversation);
      f.pending = Completer<http.Response>();
      final flight = w.refreshReplyProjections();
      await t.pump();
      w.retireReplyRefresh();
      switch (boundary) {
        case 'token':
          f.token = 'Bearer other';
          f.token = 'Bearer synthetic-local-proof';
        case 'org':
          f.org = 'another';
          f.org = null;
        case 'city':
          f.city = 'other';
          f.city = history.cityID;
        case 'epoch':
          ++f.epoch;
      }
      f.pending!.complete(f.response());
      await t.pump();
      expect(await flight, false);
      expect(w.task, same(task));
      expect(w.conversation, orderedEquals(messages));
      expect(w.requestError, isNull);
      expect(f.methods, ['GET', 'GET']);
      f.dispose();
    });
  }

  testWidgets('后台过期零GET，恢复前台原GET后来源与选择恢复且草稿未变', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace, task = w.task;
    w.showReplyOnMap(w.replies.first.result, 'place:${history.placeA}');
    final old = w.conversation[1].sourceReferences!;
    final draft = TextEditingController(text: '仍未发送');
    addTearDown(draft.dispose);
    await _mountConversation(t, f, draft);
    f.visible = false;
    w.updateReplyRefreshVisibility();
    await f.elapse(t, 31);
    expect(old.current, false);
    expect(f.methods, ['GET']);
    expect(w.presentedResult!.entities, isEmpty);
    f.visible = true;
    w.updateReplyRefreshVisibility();
    await t.pump();
    expect(w.conversation[1].sourceReferences!.current, true);
    expect(w.conversation[1].sourceReferences, isNot(same(old)));
    expect(w.presentedResult!.entities.single.id, 'place:${history.placeA}');
    expect(w.selectedEntityId, 'place:${history.placeA}');
    expect(w.task, same(task));
    expect(draft.text, '仍未发送');
    expect(f.methods, ['GET', 'GET']);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
}
