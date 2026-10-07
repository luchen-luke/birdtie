import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/now_context_query_api.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const onlineContextID = 'a1700000-0000-4000-8000-000000000001';
const onlineIntentID = 'a1700000-0000-4000-8000-000000000002';
const onlineTaskID = 'a1700000-0000-4000-8000-000000000003';
const onlineOwnerID = 'a1700000-0000-4000-8000-000000000004';
Map<String, dynamic> onlineWire({bool task = true}) => {
  'schema': 'now-public-online-query-v1',
  'context': {'id': onlineContextID, 'label': '阅读伙伴', 'type': 'ONLINE'},
  'query': '阅读',
  'answer': '找到 1 条当前公开线上意图；仅为规则匹配。',
  'modelAccess': 'UNAVAILABLE',
  'promotion': false,
  'truncated': false,
  'observedAt': '2026-10-04T12:00:00.123456+08:00',
  'items': [
    {
      'id': onlineIntentID,
      'title': '一起线上阅读',
      'modality': 'ONLINE',
      'contextId': onlineContextID,
      'sourceUpdatedAt': '2026-10-04T11:00:00+08:00',
      'expiresAt': '2026-10-04T13:00:00+08:00',
    },
  ],
  if (task)
    'task': {
      'id': onlineTaskID,
      'principalType': 'person',
      'principalId': onlineOwnerID,
      'actingUserId': onlineOwnerID,
      'cityContext': '',
      'contextType': 'ONLINE',
      'contextId': onlineContextID,
      'query': '阅读',
      'intent': 'FIND_PUBLIC_ONLINE_INTENT',
      'status': 'COMPLETED',
      'filters': {'currentQuery': '阅读'},
      'conversation': [
        {'role': 'user', 'text': '阅读'},
        {'role': 'assistant', 'text': '找到 1 条当前公开线上意图；仅为规则匹配。'},
      ],
      'createdAt': '2026-10-04T04:00:00Z',
      'updatedAt': '2026-10-04T04:00:00.123456Z',
    },
};
http.Response _jsonResponse(String body, int status) => http.Response(
  body,
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
void main() {
  test(
    'strict RFC3339 includes actual Go offset and refuses normalized calendar',
    () {
      expect(
        onlineStamp('2026-10-04T12:00:00.123456+08:00'),
        DateTime.utc(2026, 10, 4, 4, 0, 0, 123, 456),
      );
      expect(
        onlineStamp('2026-10-04T00:00:00-04:00'),
        DateTime.utc(2026, 10, 4, 4),
      );
      for (final x in [
        '2026-13-04T00:00:00Z',
        '2026-02-30T00:00:00Z',
        '2026-10-04T25:00:00Z',
        '2026-10-04T00:00:60Z',
        '2026-10-04T00:00:00+24:00',
        '2026-10-04T00:00:00+08:60',
        '2026-10-04T00:00:00',
        '0000-10-04T00:00:00Z',
      ]) {
        expect(() => onlineStamp(x), throwsFormatException);
      }
    },
  );
  test('online projection has stable typed IDs and no map effects', () {
    final r = NowOnlineResponse.decode(
      onlineWire(),
      expectedTaskID: onlineTaskID,
    );
    expect(r.task!.cityID, '');
    expect(r.task!.contextType, 'ONLINE');
    expect(r.items.single.id, onlineIntentID);
    expect(r.result().entities, isEmpty);
    expect(r.result().mapEffects, isNull);
  });
  for (final kind in [
    'schema',
    'private',
    'fakeCity',
    'expired',
    'wrongContext',
    'duplicate',
    'taskVersion',
    'privateFilters',
    'promoted',
  ]) {
    test('reject $kind wire', () {
      final x = onlineWire();
      switch (kind) {
        case 'schema':
          x['schema'] = 'legacy';
        case 'private':
          (x['items'] as List).first['constraints'] = 'PRIVATE_CANARY';
        case 'fakeCity':
          (x['task'] as Map)['cityContext'] = 'aberdeen-gb';
        case 'expired':
          (x['items'] as List).first['expiresAt'] = x['observedAt'];
        case 'wrongContext':
          (x['items'] as List).first['contextId'] = onlineTaskID;
        case 'duplicate':
          (x['items'] as List).add((x['items'] as List).first);
        case 'taskVersion':
          (x['task'] as Map)['updatedAt'] = '2026-13-01T00:00:00Z';
        case 'privateFilters':
          (x['task'] as Map)['filters']['memory'] = 'private';
        case 'promoted':
          x['promotion'] = true;
      }
      expect(() => NowOnlineResponse.decode(x), throwsFormatException);
    });
  }
  test(
    'query sends exact human body and followup CAS; borrowed transport stays open',
    () async {
      final bodies = <Map<String, dynamic>>[];
      final client = MockClient((r) async {
        expect(r.url.path, '/v1/me/now/online/tasks');
        expect(r.headers['Authorization'], 'Bearer own');
        expect(
          r.headers.containsKey('X-Birdtie-Organization-Workspace'),
          false,
        );
        bodies.add(jsonDecode(r.body));
        return _jsonResponse(jsonEncode({'data': onlineWire()}), 200);
      });
      final api = NowContextQueryApi(
        authorizationHeader: () => 'Bearer own',
        client: client,
        apiBaseUrl: 'http://localhost',
      );
      final first = await api.query(onlineContextID, '阅读');
      await api.query(onlineContextID, '阅读', task: first.task);
      expect(bodies.first, {
        'contextId': onlineContextID,
        'query': '阅读',
        'taskId': '',
        'expectedTaskUpdatedAt': '',
      });
      expect(bodies.last['taskId'], onlineTaskID);
      expect(
        bodies.last['expectedTaskUpdatedAt'],
        '2026-10-04T04:00:00.123456Z',
      );
      api.dispose();
      await client.post(
        Uri.parse('http://localhost/v1/me/now/online/tasks'),
        headers: {'Authorization': 'Bearer own'},
        body: jsonEncode(bodies.last),
      );
      client.close();
    },
  );
  test(
    'late response after permanent epoch change is denied including A B A',
    () async {
      final reply = Completer<http.Response>();
      var token = 'Bearer a', epoch = 0;
      final api = NowContextQueryApi(
        authorizationHeader: () => token,
        current: () => epoch == 0,
        client: MockClient((_) => reply.future),
        apiBaseUrl: 'http://localhost',
      );
      final pending = api.query(onlineContextID, '阅读');
      final assertion = expectLater(
        pending,
        throwsA(isA<AgentRequestFailure>()),
      );
      token = 'Bearer b';
      epoch++;
      token = 'Bearer a';
      reply.complete(_jsonResponse(jsonEncode({'data': onlineWire()}), 200));
      await assertion;
      api.dispose();
    },
  );
  test('unknown post is not automatically replayed', () async {
    var calls = 0;
    final api = NowContextQueryApi(
      authorizationHeader: () => 'Bearer own',
      client: MockClient((_) async {
        calls++;
        throw http.ClientException('offline');
      }),
      apiBaseUrl: 'http://localhost',
    );
    await expectLater(
      api.query(onlineContextID, '阅读'),
      throwsA(
        isA<AgentRequestFailure>().having(
          (x) => x.code,
          'code',
          'online_result_unknown',
        ),
      ),
    );
    expect(calls, 1);
    api.dispose();
  });
  for (final bad in ['missingTask', 'wrongOwner', 'malformed']) {
    test(
      'successful HTTP with $bad is an unknown submission, never replayed',
      () async {
        var calls = 0;
        final data = onlineWire(task: bad != 'missingTask');
        if (bad == 'wrongOwner') {
          (data['task'] as Map)['principalId'] = onlineContextID;
        }
        final api = NowContextQueryApi(
          authorizationHeader: () => 'Bearer own',
          ownerID: () => onlineOwnerID,
          client: MockClient((_) async {
            calls++;
            return _jsonResponse(
              bad == 'malformed' ? '{' : jsonEncode({'data': data}),
              200,
            );
          }),
          apiBaseUrl: 'http://localhost',
        );
        await expectLater(
          api.query(onlineContextID, '阅读'),
          throwsA(
            isA<AgentRequestFailure>().having(
              (x) => x.code,
              'code',
              'online_result_unknown',
            ),
          ),
        );
        expect(calls, 1);
        api.dispose();
      },
    );
  }
}
