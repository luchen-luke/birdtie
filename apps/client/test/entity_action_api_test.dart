import 'dart:convert';
import 'package:birdtie_client/src/workspace/entity_action_api.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_action_contract_test.dart' show actionWire, actionRef;

void main() {
  test('失效身份401不得自动降级公开读取；匿名明确public零虚假身份', () async {
    final paths = <String>[];
    final client = MockClient((r) async {
      paths.add(r.url.path);
      return http.Response('{}', 401);
    });
    final api = EntityActionApi(client: client, apiBaseUrl: 'https://api.test');
    await expectLater(
      api.read('Bearer expired', actionRef),
      throwsA(
        isA<EntityActionFailure>().having((e) => e.status, 'status', 401),
      ),
    );
    expect(paths, ['/v1/me/entity-actions/activity/${actionRef.id}']);
    api.dispose();
    client.close();
  });
  test('只读固定原Ref，当前token明确绑定，借用client不关闭', () async {
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return http.Response(
        jsonEncode({'data': actionWire()}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    final api = EntityActionApi(
      client: client,
      apiBaseUrl: 'http://127.0.0.1:3707',
    );
    final v = await api.read('Bearer synthetic-a', actionRef);
    expect(v.entity, actionRef);
    expect(calls.single.method, 'GET');
    expect(calls.single.url.query, '');
    expect(
      calls.single.url.path,
      '/v1/me/entity-actions/activity/${actionRef.id}',
    );
    expect(calls.single.headers['Authorization'], 'Bearer synthetic-a');
    expect(calls.single.body, '');
    api.dispose();
    api.dispose();
    await client.get(Uri.parse('http://127.0.0.1:3707/borrowed'));
    expect(calls.length, 2);
    expect(
      () => api.read('Bearer a', actionRef),
      throwsA(isA<EntityActionFailure>()),
    );
  });
  test('匿名/未知ID/缺配置零请求，403/404/409/503保持真实不可用无fallback', () async {
    for (final status in [403, 404, 409, 503]) {
      int calls = 0;
      final api = EntityActionApi(
        client: MockClient((r) async {
          calls++;
          return http.Response('{}', status);
        }),
        apiBaseUrl: 'http://127.0.0.1:3707',
      );
      await expectLater(
        api.read('', actionRef),
        throwsA(isA<EntityActionFailure>()),
      );
      expect(calls, 0);
      await expectLater(
        api.read('Bearer a', const EntityActionRef('opportunity', 'bad')),
        throwsA(isA<EntityActionFailure>()),
      );
      expect(calls, 0);
      await expectLater(
        api.read('Bearer a', actionRef),
        throwsA(
          isA<EntityActionFailure>().having((e) => e.status, 'status', status),
        ),
      );
      expect(calls, 1);
      api.dispose();
    }
  });
}
