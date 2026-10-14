import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/external_place_history_store.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'external_place_candidates_test.dart' show externalQueryWire;

const query = '有没有好的餐厅';
const api = 'https://consumer-followup.test';

Map<String, dynamic> publicSearch(String requestID, String question, int turn) {
  final raw =
      (jsonDecode(
                File(
                  '../../docs/testing/evidence/restaurant-history-cache-2026-10-10/api-01/guest-query.json',
                ).readAsStringSync(),
              )
              as Map<String, dynamic>)['onlinePlaceSearch']
          as Map<String, dynamic>;
  raw.addAll({
    'requestId': requestID,
    'query': question,
    'cityId': 'alpha',
    'turnDigest': sha256.convert(utf8.encode(question)).toString(),
    'snapshotId': '00000000-0000-4000-8000-${turn.toString().padLeft(12, '0')}',
    'messageIndex': 1,
    'origin': 'CACHE',
  });
  raw.remove('taskId');
  for (final row in raw['places'] as List) {
    (row as Map<String, dynamic>)['cityId'] = 'alpha';
  }
  return raw;
}

class FollowupFixture {
  FollowupFixture() {
    source = RemoteAgentTaskSource(
      cityID: () => city,
      authorizationHeader: () => token,
      organizationWorkspaceID: () => organization,
      publicEvidenceEpoch: () => epoch,
      publicEvidenceNow: () => clock,
      apiBaseUrl: api,
      externalPlaceHistoryStore: store,
      client: MockClient((request) async {
        requests.add(request);
        if (request.method == 'GET') {
          return http.Response('{"data":[]}', 200);
        }
        if (failure != null) {
          return http.Response(
            jsonEncode({
              'error': {'code': failure},
            }),
            failureStatus,
          );
        }
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        final wire = externalQueryWire(
          clock,
          request.headers['X-Request-ID']!,
          query: body['query'],
        );
        wire['onlinePlaceSearch'] = publicSearch(
          request.headers['X-Request-ID']!,
          body['query'],
          requests.length,
        );
        wire['message'] = '本轮答案：${body['query']}';
        if (pending != null) await pending!.future;
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': wire})),
          200,
        );
      }),
    );
    workspace = AgentWorkspaceController(source: source);
  }
  String city = 'alpha';
  String? token, organization, failure;
  int epoch = 0;
  int failureStatus = 409;
  final store = MemoryExternalPlaceHistoryStore();
  final requests = <http.Request>[];
  late final RemoteAgentTaskSource source;
  late final AgentWorkspaceController workspace;
  Completer<void>? pending;
  DateTime clock = DateTime.parse(
    publicSearch('r', query, 1)['observedAt'],
  ).add(const Duration(hours: 1));
  Future<void> send(String q) => workspace.submit(q, [], [], cityID: city);
  Map<String, dynamic> get body =>
      jsonDecode(requests.last.body) as Map<String, dynamic>;
  void dispose() {
    workspace.dispose();
    source.dispose();
  }
}

void main() {
  // Actual API05 owned GET after the phone exposed a successful POST whose
  // absent secondary history capability was serialized as null. Keep the
  // complete public task history, including its change to a different topic.
  Map<String, dynamic> ownedUnsupportedWire() =>
      jsonDecode(
            File(
              '../../docs/testing/evidence/consumer-agent-repair-2026-10-10/backend-agent/phone-followup-inspection-01/actual-get-response.json',
            ).readAsStringSync(),
          )
          as Map<String, dynamic>;

  for (final historyAvailable in [true, false]) {
    test(
      historyAvailable
          ? 'owned ordinary reply consumes authoritative empty history and does not revive an earlier restaurant turn'
          : 'owned reply without an authoritative history list is still rejected',
      () async {
        final wire = ownedUnsupportedWire();
        final data = wire['data'] as Map<String, dynamic>;
        final task = data['task'] as Map<String, dynamic>;
        if (!historyAvailable) data['messageResults'] = null;
        final source = RemoteAgentTaskSource(
          cityID: () => task['cityContext'] as String,
          authorizationHeader: () => 'Bearer owned-fixture',
          publicEvidenceOwnerID: () => task['principalId'] as String,
          publicEvidenceNow: () => DateTime.parse(
            (data['resultSet'] as Map<String, dynamic>)['generatedAt'],
          ).add(const Duration(seconds: 1)),
          apiBaseUrl: api,
          externalPlaceHistoryStore: MemoryExternalPlaceHistoryStore(),
          client: MockClient((request) async {
            expect(request.method, 'POST');
            return http.Response.bytes(utf8.encode(jsonEncode(wire)), 200);
          }),
        );
        addTearDown(source.dispose);
        final pending = source.resolve(data['query'] as String, [], []);
        if (!historyAvailable) {
          await expectLater(
            pending,
            throwsA(predicate((e) => e.toString().contains('无法读取'))),
          );
          return;
        }
        final reply = await pending;
        expect(reply.task!.messages, hasLength(6));
        expect(reply.messageResults, isEmpty);
        expect(reply.onlinePlaceSearch, isNull);
        expect(reply.task!.messages.last.externalPlaceReply, isNull);
        expect(
          reply.task!.messages[1].externalPlaceReply!.places,
          hasLength(15),
        );
        expect(reply.message, data['message']);
        expect(reply.projectionItems, isEmpty);
      },
    );
  }

  test(
    'actual API06 owned POST and GET envelopes decode across restaurant follow-up and a new topic',
    () async {
      const names = [
        'restaurant',
        'first-address',
        'sports-new-topic',
        'ordinal-after-sports',
      ];
      Map<String, dynamic> wire(String name, String method) =>
          jsonDecode(
                File(
                  '../../docs/testing/evidence/consumer-agent-repair-2026-10-10/backend-agent/api06-signed-envelope-02/$name-$method-response.json',
                ).readAsStringSync(),
              )
              as Map<String, dynamic>;
      final first = wire(names.first, 'post')['data'] as Map<String, dynamic>;
      final task = first['task'] as Map<String, dynamic>;
      final clock = names
          .map(
            (name) => DateTime.parse(
              ((wire(name, 'post')['data'] as Map)['resultSet']
                  as Map)['generatedAt'],
            ),
          )
          .reduce((a, b) => a.isAfter(b) ? a : b)
          .add(const Duration(seconds: 1));
      var turn = -1;
      final requests = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => task['cityContext'] as String,
        authorizationHeader: () => 'Bearer owned-fixture',
        publicEvidenceOwnerID: () => task['principalId'] as String,
        publicEvidenceNow: () => clock,
        apiBaseUrl: api,
        externalPlaceHistoryStore: MemoryExternalPlaceHistoryStore(),
        client: MockClient((request) async {
          requests.add(request);
          if (request.method == 'POST') ++turn;
          final response = wire(names[turn], request.method.toLowerCase());
          final data = response['data'] as Map<String, dynamic>;
          if (request.method == 'POST') {
            final body = jsonDecode(request.body) as Map;
            expect(body['query'], data['query']);
            expect(body['taskId'], turn == 0 ? null : task['id']);
            expect(body.containsKey('onlinePlaceContext'), turn == 1);
            // Only the current mock transport correlation is rebound. The
            // recorded task, preceding turns, facts, digests and indices are
            // left intact; these files came from the actual local HTTP run.
            data['requestId'] = request.headers['X-Request-ID'];
            if (data['onlinePlaceSearch'] case final Map<String, dynamic> top) {
              top['requestId'] = request.headers['X-Request-ID'];
              final messages = (data['task'] as Map)['conversation'] as List;
              (messages.last['externalPlaceReply'] as Map)['requestId'] =
                  request.headers['X-Request-ID'];
            }
          }
          return http.Response.bytes(utf8.encode(jsonEncode(response)), 200);
        }),
      );
      final workspace = AgentWorkspaceController(source: source);
      addTearDown(() {
        workspace.dispose();
        source.dispose();
      });
      DateTime? observed;
      for (final (index, name) in names.indexed) {
        final data = wire(name, 'post')['data'] as Map<String, dynamic>;
        await workspace.submit(
          data['query'] as String,
          [],
          [],
          cityID: task['cityContext'] as String,
        );
        expect(workspace.requestError, isNull, reason: name);
        expect(workspace.task!.messages, hasLength((index + 1) * 2));
        expect(workspace.result!.messageResults, isEmpty);
        if (index < 2) {
          final search = workspace.result!.onlinePlaceSearch!;
          expect(search.places, hasLength(15));
          observed ??= search.observedAt;
          expect(search.observedAt, observed);
        } else {
          expect(workspace.result!.onlinePlaceSearch, isNull);
          expect(workspace.task!.messages.last.externalPlaceReply, isNull);
          expect(
            workspace.task!.messages[1].externalPlaceReply!.places,
            hasLength(15),
          );
        }
        final restored = await source.restore(workspace.task!, [], []);
        expect(restored.task!.messages, hasLength((index + 1) * 2));
        expect(restored.messageResults, isEmpty);
        expect(restored.message, data['message']);
        expect(
          restored.onlinePlaceSearch?.places.length,
          index < 2 ? 15 : null,
        );
      }
      expect(requests.where((r) => r.method == 'POST'), hasLength(4));
      expect(requests.where((r) => r.method == 'GET'), hasLength(4));
    },
  );

  for (final question in [
    '第一家地址',
    '比较前两家',
    '哪个离市中心近',
    "Nando's在哪里",
    '这些餐厅现在开门吗',
  ]) {
    test(
      'guest follows up $question with only current public identifiers and preserves all cards',
      () async {
        final f = FollowupFixture();
        addTearDown(f.dispose);
        await f.send(query);
        final taskID = f.workspace.task!.id;
        final first = f.workspace.result!.onlinePlaceSearch!;
        await f.send(question);
        expect(f.body['query'], question);
        expect(f.body['taskId'], isNull);
        expect(f.body['onlinePlaceContext'], {
          'cacheVersion': first.cacheVersion,
          'scope': first.scope,
          'placeIds': first.places.map((p) => p.id).toList(),
        });
        expect((f.body['onlinePlaceContext'] as Map).keys.toSet(), {
          'cacheVersion',
          'scope',
          'placeIds',
        });
        expect(f.workspace.task!.id, taskID);
        expect(f.workspace.conversation.map((m) => m.text), [
          query,
          '本轮答案：$query',
          question,
          '本轮答案：$question',
        ]);
        expect(f.workspace.result!.onlinePlaceSearch!.places, hasLength(15));
        expect(f.workspace.replies, hasLength(2));
        expect(
          f.workspace.canUseExternalPlaces(f.workspace.replies.first.result),
          true,
        );
        expect(
          f.workspace.result!.onlinePlaceSearch!.observedAt,
          first.observedAt,
        );
        expect(await f.store.read(api), hasLength(1));
      },
    );
  }
  for (final fresh in [query, '更新餐厅信息', '找羽毛球活动', '找组织']) {
    test(
      'explicit new query $fresh does not inherit restaurant cache references',
      () async {
        final f = FollowupFixture();
        addTearDown(f.dispose);
        await f.send(query);
        await f.send(fresh);
        expect(f.body.containsKey('onlinePlaceContext'), false);
      },
    );
  }
  test(
    'a later unrelated assistant reply prevents reusing any older restaurant turn',
    () async {
      final f = FollowupFixture();
      addTearDown(f.dispose);
      await f.send(query);
      final old = f.workspace.task!;
      final newer = AgentTask(
        id: old.id,
        query: old.query,
        status: 'COMPLETED',
        cityID: 'alpha',
        messages: [
          ...old.messages,
          const AgentMessage(role: 'user', text: '找活动'),
          const AgentMessage(role: 'assistant', text: '活动列表'),
        ],
      );
      await f.source.followUp(newer, '第一家地址', [], []);
      expect(f.body.containsKey('onlinePlaceContext'), false);
    },
  );
  test(
    'city and identity retirement cannot attach an old public reference',
    () async {
      final f = FollowupFixture();
      addTearDown(f.dispose);
      await f.send(query);
      final task = f.workspace.task!;
      f.city = 'beta';
      ++f.epoch;
      await expectLater(
        f.source.followUp(task, '第一家地址', [], []),
        throwsA(predicate((e) => e.toString().contains('无法读取'))),
      );
      expect(f.body.containsKey('onlinePlaceContext'), false);
    },
  );
  test(
    'explicit area search does not carry old city result identifiers',
    () async {
      final f = FollowupFixture();
      addTearDown(f.dispose);
      await f.send(query);
      // Call uses an explicit new viewport; its facts need a separate query.
      await f.source.searchArea(
        f.workspace.task,
        query,
        const MapBounds(west: -10, south: 50, east: 0, north: 60),
        [],
        [],
      );
      expect(f.body.containsKey('onlinePlaceContext'), false);
      expect(f.body.containsKey('mapBounds'), true);
    },
  );
  test(
    'cache version conflict keeps the preceding answer, cards and original observation time',
    () async {
      final f = FollowupFixture();
      addTearDown(f.dispose);
      await f.send(query);
      final previous = f.workspace.result!;
      f.failure = 'online_place_context_changed';
      await f.send('第一家地址');
      expect(f.workspace.result, same(previous));
      expect(f.workspace.requestError, contains('原回答和卡片仍保留'));
      expect(f.workspace.canUseExternalPlaces(previous), true);
      expect(previous.onlinePlaceSearch!.places, hasLength(15));
    },
  );
  test(
    'unavailable current place facts explain recovery and preserve previous conversation cards',
    () async {
      final f = FollowupFixture();
      addTearDown(f.dispose);
      await f.send(query);
      final previous = f.workspace.result!;
      final previousMessages = f.workspace.conversation
          .map((m) => m.text)
          .toList();
      f.failure = 'place_answer_unavailable';
      f.failureStatus = 503;
      await f.send('Prego的地址');
      expect(f.workspace.result, same(previous));
      expect(f.workspace.requestError, contains('当前可核验的地点资料不足'));
      expect(f.workspace.requestError, contains('原对话仍保留'));
      expect(f.workspace.requestError, isNot(contains('请明确地点名称')));
      expect(
        f.workspace.conversation
            .take(previousMessages.length)
            .map((m) => m.text),
        previousMessages,
      );
      expect(f.workspace.conversation.last.role, 'user');
      expect(f.workspace.conversation.last.text, 'Prego的地址');
      expect(f.workspace.replies, hasLength(1));
      expect(f.workspace.canUseExternalPlaces(previous), true);
      expect(previous.onlinePlaceSearch!.places, hasLength(15));
    },
  );
  test(
    'AI service unavailable keeps previous answer and all cards without retrying the query',
    () async {
      final f = FollowupFixture();
      addTearDown(f.dispose);
      await f.send(query);
      final previous = f.workspace.result!;
      final previousMessages = f.workspace.conversation
          .map((m) => m.text)
          .toList();
      f.failure = 'ai_service_unavailable';
      f.failureStatus = 503;
      await f.send('Prego 的地址在哪里？');
      expect(f.workspace.result, same(previous));
      expect(
        f.workspace.conversation
            .take(previousMessages.length)
            .map((m) => m.text),
        previousMessages,
      );
      expect(f.workspace.conversation.last.role, 'user');
      expect(f.workspace.conversation.last.text, 'Prego 的地址在哪里？');
      expect(f.workspace.replies, hasLength(1));
      expect(f.workspace.canUseExternalPlaces(previous), true);
      expect(previous.onlinePlaceSearch!.places, hasLength(15));
      expect(f.requests.where((r) => r.method == 'POST'), hasLength(2));
      // The existing guest fixture has no owned recent-task GET authority.
      expect(f.requests.where((r) => r.method == 'GET'), isEmpty);
      expect(f.workspace.requestError, 'AI 服务目前不可用，本轮没有生成新回答。原对话仍保留。');
      expect(
        f.workspace.requestFailure!.recoveryMessage,
        '可从最近对话查看之前的内容，服务恢复后再发起查询。',
      );
      expect(f.workspace.permitsUnchangedRetry, false);
      final requests = f.requests.length;
      await f.workspace.retry();
      expect(f.requests, hasLength(requests));
      expect(f.workspace.result, same(previous));
      // An explicit new query after recovery still uses the original path.
      f.failure = null;
      await f.send('比较这两家餐厅的地址');
      expect(f.workspace.requestFailure, isNull);
      expect(f.requests.where((r) => r.method == 'POST'), hasLength(3));
      expect(f.workspace.replies, hasLength(2));
    },
  );
  test(
    'restoring the follow-up on a new source reads local history without HTTP',
    () async {
      final f = FollowupFixture();
      addTearDown(f.dispose);
      await f.send(query);
      await f.send('比较前两家');
      final id = f.workspace.task!.id;
      final restart = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => null,
        publicEvidenceNow: () => f.clock,
        apiBaseUrl: api,
        externalPlaceHistoryStore: f.store,
        client: MockClient(
          (_) async => throw StateError('history must not issue HTTP'),
        ),
      );
      addTearDown(restart.dispose);
      final tasks = await restart.loadRecent();
      final restored = await restart.restore(
        tasks.singleWhere((t) => t.id == id),
        [],
        [],
      );
      expect(restored.task!.messages, hasLength(4));
      expect(restored.task!.messages.last.text, '本轮答案：比较前两家');
      expect(restored.onlinePlaceSearch!.places, hasLength(15));
    },
  );
}
