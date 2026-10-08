import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_reply_membership.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const taskID = '8f7f6f05-f557-4a62-98e8-f841f57190be';
const ownerID = 'e88f778c-b37c-421b-b570-1c3c0706c93e';
const cityID = 'aberdeen-gb';
const placeA = 'abf17e14-6e08-461b-855c-bf996b5a65ac';
const placeB = '6df8267d-8afb-44dd-884f-96df80ff98a9';

Map<String, dynamic> historyWire(DateTime now, {bool revokeA = false}) {
  final messages = <Map<String, dynamic>>[
    {'role': 'user', 'text': '找地点 A'},
    {'role': 'assistant', 'text': '第 1 轮真实站内结果。'},
    {'role': 'user', 'text': '找地点 B'},
    {'role': 'assistant', 'text': '第 2 轮真实站内结果。'},
  ];
  final histories = <Map<String, dynamic>>[];
  for (final (turn, id) in [placeA, placeB].indexed) {
    final index = turn * 2 + 1;
    final digest = agentReplyTurnDigest(
      messages
          .take(index + 1)
          .map((m) => (role: m['role'] as String, text: m['text'] as String)),
    );
    final setID = 'reply:$taskID:${digest.substring(0, 24)}';
    final ref = {'type': 'place', 'id': id};
    messages[index]['resultMembership'] = {
      'schema': agentReplyMembershipSchema,
      'taskId': taskID,
      'cityId': cityID,
      'kind': 'place',
      'turnDigest': digest,
      'resultSetId': setID,
      'refs': [ref],
    };
    final visible = !(turn == 0 && revokeA);
    histories.add({
      'messageIndex': index,
      'turnDigest': digest,
      'validUntil': now.add(const Duration(minutes: 1)).toIso8601String(),
      'resultSet': {
        'schema': 'typed-agent-results-v1',
        'id': setID,
        'taskId': taskID,
        'cityId': cityID,
        'query': '',
        'status': visible ? 'ready' : 'empty',
        'generatedAt': now.toIso8601String(),
        'filters': {},
        'sources': [],
        'entities': [if (visible) ref],
        'items': [
          if (visible)
            {
              'entityRef': ref,
              'detailRef': ref,
              'title': turn == 0 ? '地点 A' : '地点 B',
              'summary': 'synthetic native projection fixture',
              'scope': 'AUTHORIZED_VIEW',
              'anchor': {
                'coordinateSystem': 'wgs84',
                'precision': 'point',
                'latitude': 57.14 + turn * .01,
                'longitude': -2.1,
              },
            },
        ],
      },
      'places': [],
      'activities': [],
      'organizations': [],
      'mapEffects': {
        'camera': 'preserve',
        'pinEntityIds': [if (visible) 'place:$id'],
      },
    });
  }
  return {
    'taskId': taskID,
    'conversationId': taskID,
    'requestId': 'synthetic-history-get',
    'cityId': cityID,
    'message': messages.last['text'],
    'activities': [],
    'places': [],
    'people': [],
    'groups': [],
    'organizations': [],
    'resultSet': histories.last['resultSet'],
    'mapEffects': histories.last['mapEffects'],
    'messageResults': histories,
    'task': {
      'id': taskID,
      'query': '找地点 A',
      'status': 'COMPLETED',
      'cityContext': cityID,
      'contextType': 'CITY',
      'principalType': 'person',
      'principalId': ownerID,
      'actingUserId': ownerID,
      'intent': 'FIND_PLACE',
      'filters': {'currentQuery': '找地点 B', 'searchTerm': 'B'},
      'conversation': messages,
    },
  };
}

RemoteAgentTaskSource historyRemote(
  Object Function() wire,
  List<String> methods,
  DateTime Function() now,
) => RemoteAgentTaskSource(
  cityID: () => cityID,
  authorizationHeader: () => 'Bearer synthetic-original',
  publicEvidenceOwnerID: () => ownerID,
  publicEvidenceNow: now,
  apiBaseUrl: 'http://localhost:8080',
  client: MockClient((request) async {
    methods.add(request.method);
    return http.Response.bytes(
      utf8.encode(jsonEncode({'data': wire()})),
      200,
      headers: {'content-type': 'application/json'},
    );
  }),
);

void main() {
  test('原Go角色文字摘要的HTML与Unicode转义跨语言一致', () {
    expect(
      agentReplyTurnDigest([
        (role: 'user', text: '中文 & <北>\u2028换行\n"引号"'),
        (role: 'assistant', text: '结果 > 地点\u2029'),
      ]),
      'c67a2531b3dd68e78f6676d1f889f89e2b0b3d0f8f7e220c4b40f398cfa413a0',
    );
  });
  test('新建controller从原GET恢复两轮独立实体集合，旧轮地图不覆盖当前查询', () async {
    final now = DateTime.utc(2026, 10, 8, 2);
    final wire = historyWire(now);
    final methods = <String>[];
    final workspace = AgentWorkspaceController(
      source: historyRemote(() => wire, methods, () => now),
    );
    addTearDown(workspace.dispose);
    await workspace.reopen(
      AgentTask.fromJson(wire['task'] as Map<String, dynamic>),
      [],
      [],
    );
    expect(workspace.replies, hasLength(2));
    final a = workspace.replies.first.result;
    final b = workspace.replies.last.result;
    expect(a.entities.single.id, 'place:$placeA');
    expect(b.entities.single.id, 'place:$placeB');
    final current = workspace.result;
    workspace.showReplyOnMap(a, 'place:$placeA');
    expect(workspace.presentedResult, same(a));
    expect(workspace.selectedEntityId, 'place:$placeA');
    expect(workspace.result, same(current));
    expect(workspace.result!.entities.single.id, 'place:$placeB');
    expect(methods, ['GET']);
  });

  test('最新轮保留当前结果对象和原收藏提醒追问入口判定', () async {
    final now = DateTime.utc(2026, 10, 8, 2);
    final wire = historyWire(now);
    final workspace = AgentWorkspaceController(
      source: historyRemote(() => wire, [], () => now),
    );
    addTearDown(workspace.dispose);
    await workspace.reopen(
      AgentTask.fromJson(wire['task'] as Map<String, dynamic>),
      [],
      [],
    );
    expect(workspace.replies.last.result, same(workspace.result));
    expect(workspace.replies.first.result, isNot(same(workspace.result)));
  });

  test('服务器撤权后的旧轮为空集合，不能拿最新地点替补或复活缓存', () async {
    final now = DateTime.utc(2026, 10, 8, 2);
    var wire = historyWire(now);
    final methods = <String>[];
    final workspace = AgentWorkspaceController(
      source: historyRemote(() => wire, methods, () => now),
    );
    addTearDown(workspace.dispose);
    final task = AgentTask.fromJson(wire['task'] as Map<String, dynamic>);
    await workspace.reopen(task, [], []);
    expect(workspace.replies.first.result.entities.single.id, 'place:$placeA');
    wire = historyWire(now, revokeA: true);
    await workspace.reopen(task, [], []);
    expect(workspace.replies, hasLength(2));
    final a = workspace.replies.first.result;
    expect(a.projectionItems, isEmpty);
    expect(a.entities, isEmpty);
    workspace.showReplyOnMap(a, 'place:$placeB');
    expect(workspace.presentedResult!.entities.single.id, 'place:$placeB');
    expect(methods, ['GET', 'GET']);
  });

  final mutations = <String, void Function(Map<String, dynamic>)>{
    '跨Task': (w) =>
        (w['messageResults'][0]['resultSet'] as Map)['taskId'] = 'foreign',
    '跨城市': (w) =>
        (w['messageResults'][0]['resultSet'] as Map)['cityId'] = 'foreign',
    '跨账号': (w) => (w['task'] as Map)['principalId'] = placeA,
    '代理主体变化': (w) => (w['task'] as Map)['actingUserId'] = placeA,
    '用户消息挂结果': (w) => w['messageResults'][0]['messageIndex'] = 0,
    '索引越界': (w) => w['messageResults'][0]['messageIndex'] = 99,
    '重复索引': (w) => (w['messageResults'] as List).add(w['messageResults'][0]),
    '历史数量无界': (w) =>
        w['messageResults'] = List.filled(31, w['messageResults'][0]),
    '轮摘要变化': (w) =>
        w['messageResults'][0]['turnDigest'] = List.filled(64, 'f').join(),
    '旧消息被替换': (w) => w['task']['conversation'][0]['text'] = '另一条原问题',
    '未知role': (w) => w['task']['conversation'][0]['role'] = 'system',
    '私有类型': (w) =>
        w['task']['conversation'][1]['resultMembership']['kind'] = 'person',
    '重复membership': (w) =>
        (w['task']['conversation'][1]['resultMembership']['refs'] as List).add({
          'type': 'place',
          'id': placeA,
        }),
    '未知membership字段': (w) =>
        w['task']['conversation'][1]['resultMembership']['grant'] = true,
    '伪造稳定结果ID': (w) =>
        w['task']['conversation'][1]['resultMembership']['resultSetId'] =
            'forged',
    '未知响应字段': (w) => w['messageResults'][0]['grant'] = true,
    '嵌套结果': (w) => w['messageResults'][0]['messageResults'] = [],
    '非法schema': (w) =>
        w['messageResults'][0]['resultSet']['schema'] = 'unknown',
    '非法status': (w) =>
        w['messageResults'][0]['resultSet']['status'] = 'completed',
    '不一致空状态': (w) => w['messageResults'][0]['resultSet']['status'] = 'empty',
    '伪造模型binding': (w) =>
        w['messageResults'][0]['resultSet']['answerBinding'] = {},
    '伪造公开字段证据': (w) =>
        w['messageResults'][0]['resultSet']['publicFieldEvidence'] = {},
    '未经本消息来源绑定的citation': (w) =>
        w['messageResults'][0]['resultSet']['sources'] = [
          {'id': 'x'},
        ],
    '额外地图pin': (w) => w['messageResults'][0]['mapEffects']['pinEntityIds'] = [
      'place:$placeA',
      'place:$placeB',
    ],
    '主结果嫁接第一轮': (w) => w['resultSet'] = w['messageResults'][0]['resultSet'],
    '过期': (w) => w['messageResults'][0]['validUntil'] = '2026-10-08T01:59:59Z',
    '未来观察时间': (w) => w['messageResults'][0]['resultSet']['generatedAt'] =
        '2026-10-08T02:00:30Z',
    '第三实体补旧轮': (w) {
      final set = w['messageResults'][0]['resultSet'];
      set['entities'] = [
        {'type': 'place', 'id': placeB},
      ];
      set['items'][0]['entityRef'] = {'type': 'place', 'id': placeB};
      set['items'][0]['detailRef'] = {'type': 'place', 'id': placeB};
      w['messageResults'][0]['mapEffects']['pinEntityIds'] = ['place:$placeB'];
    },
  };
  for (final entry in mutations.entries) {
    test('拒绝 ${entry.key}，不创建旧轮实体投影', () async {
      final now = DateTime.utc(2026, 10, 8, 2);
      final wire = historyWire(now);
      entry.value(wire);
      final source = historyRemote(() => wire, [], () => now);
      addTearDown(source.dispose);
      await expectLater(
        source.restore(
          const AgentTask(id: taskID, query: '旧问题', status: 'COMPLETED'),
          [],
          [],
        ),
        throwsFormatException,
      );
    });
  }

  test('过期不再显示或点选实体，原消息仍保留', () async {
    var now = DateTime.utc(2026, 10, 8, 2);
    final wire = historyWire(now);
    final workspace = AgentWorkspaceController(
      source: historyRemote(() => wire, [], () => now),
    );
    addTearDown(workspace.dispose);
    await workspace.reopen(AgentTask.fromJson(wire['task']), [], []);
    final a = workspace.replies.first.result;
    expect(workspace.canUseReplyProjection(a), isTrue);
    workspace.showReplyOnMap(a, 'place:$placeA');
    now = now.add(const Duration(minutes: 1));
    expect(a.entities, isEmpty);
    expect(a.projectionItems, isEmpty);
    expect(workspace.canUseReplyProjection(a), isFalse);
    expect(workspace.conversation[1].text, '第 1 轮真实站内结果。');
    now = now.subtract(const Duration(minutes: 1));
    expect(a.replyProjectionCurrent, isFalse);
  });

  test('authoritative空history移除旧membership缓存，不回填当前结果', () async {
    final now = DateTime.utc(2026, 10, 8, 2);
    var wire = historyWire(now);
    final workspace = AgentWorkspaceController(
      source: historyRemote(() => wire, [], () => now),
    );
    addTearDown(workspace.dispose);
    final task = AgentTask.fromJson(wire['task']);
    await workspace.reopen(task, [], []);
    expect(workspace.replies, hasLength(2));
    wire = historyWire(now)..['messageResults'] = <dynamic>[];
    await workspace.reopen(task, [], []);
    expect(workspace.replies, isEmpty);
    expect(workspace.result!.entities, isEmpty);
    expect(workspace.conversation, hasLength(4));
  });

  test('membership与读取投影深层不可变', () async {
    final now = DateTime.utc(2026, 10, 8, 2);
    final wire = historyWire(now);
    final source = historyRemote(() => wire, [], () => now);
    addTearDown(source.dispose);
    final reply = await source.restore(
      AgentTask.fromJson(wire['task']),
      [],
      [],
    );
    expect(() => reply.messageResults!.clear(), throwsUnsupportedError);
    expect(
      () => reply.task!.messages[1].resultMembership!.refs.clear(),
      throwsUnsupportedError,
    );
    final a = reply.messageResults![1]!;
    expect(a.entities.single.id, 'place:$placeA');
    wire['messageResults'][0]['resultSet']['items'][0]['title'] = 'changed';
    expect(a.projectionItems!.single.title, '地点 A');
  });

  for (final change in [
    'account',
    'organization',
    'city',
    'online',
    'owner',
    'epoch',
    'dispose',
  ]) {
    test('旧轮实体在 $change 变化后永久退役，身份ABA不恢复', () async {
      final now = DateTime.utc(2026, 10, 8, 2);
      final wire = historyWire(now);
      var token = 'Bearer original';
      String? organization, online;
      var city = cityID, owner = ownerID, epoch = 1;
      final source = RemoteAgentTaskSource(
        cityID: () => city,
        authorizationHeader: () => token,
        organizationWorkspaceID: () => organization,
        onlineContextID: () => online,
        publicEvidenceOwnerID: () => owner,
        publicEvidenceEpoch: () => epoch,
        publicEvidenceNow: () => now,
        apiBaseUrl: 'http://localhost:8080',
        client: MockClient(
          (_) async => http.Response.bytes(
            utf8.encode(jsonEncode({'data': wire})),
            200,
            headers: {'content-type': 'application/json'},
          ),
        ),
      );
      addTearDown(source.dispose);
      final result = await source.restore(
        AgentTask.fromJson(wire['task']),
        [],
        [],
      );
      final a = result.messageResults![1]!;
      expect(a.entities.single.id, 'place:$placeA');
      switch (change) {
        case 'account':
          token = 'Bearer another';
        case 'organization':
          organization = 'another';
        case 'city':
          city = 'another';
        case 'online':
          online = 'another';
        case 'owner':
          owner = placeA;
        case 'epoch':
          epoch = 2;
        case 'dispose':
          source.dispose();
      }
      expect(a.entities, isEmpty);
      expect(a.replyProjectionCurrent, isFalse);
      token = 'Bearer original';
      organization = null;
      online = null;
      city = cityID;
      owner = ownerID;
      epoch = 1;
      expect(a.entities, isEmpty);
      expect(a.replyProjectionCurrent, isFalse);
    });
  }

  test('旧投影过期不同时停止持久来源链接', () async {
    var now = DateTime.utc(2026, 10, 8, 2);
    final wire = historyWire(now);
    final message = wire['task']['conversation'][1] as Map<String, dynamic>;
    message['sources'] = [
      {
        'id': 'original-source',
        'title': '原消息来源',
        'url': 'https://example.org/original',
      },
    ];
    message['sourceRunId'] = 'original-source-run';
    message['sourceEvidenceDigest'] = List.filled(64, 'a').join();
    final workspace = AgentWorkspaceController(
      source: historyRemote(() => wire, [], () => now),
    );
    addTearDown(workspace.dispose);
    await workspace.reopen(AgentTask.fromJson(wire['task']), [], []);
    final a = workspace.replies.first.result;
    final sources = workspace.conversation[1].sourceReferences!;
    expect(workspace.retainsMessageSources(a, sources), isTrue);
    now = now.add(const Duration(minutes: 1));
    expect(workspace.canUseReplyProjection(a), isFalse);
    expect(workspace.retainsMessageSources(a, sources), isTrue);
    expect(sources.current, isTrue);
  });
}
