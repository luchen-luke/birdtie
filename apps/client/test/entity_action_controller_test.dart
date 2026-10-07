import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/entity_action_api.dart';
import 'package:birdtie_client/src/workspace/entity_action_controller.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_action_contract_test.dart' show actionWire, actionRef;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('正常具体源重复核验，撤权还原版本变化拒绝旧批准', () async {
    String version = 'a' * 64;
    final api = EntityActionApi(
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({'data': actionWire(version: version)}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      ),
      apiBaseUrl: 'http://127.0.0.1:3707',
    );
    final c = EntityActionController(
      api: api,
      ref: actionRef,
      identity: () => ('Bearer a', 'owner', null),
    );
    final v = await c.load();
    expect(v, isNotNull);
    expect(await c.recheck(v!, EntityActionKind.save), isNotNull);
    final old = c.view!;
    version = 'b' * 64;
    expect(await c.recheck(old, EntityActionKind.save), isNull);
    expect(c.error, contains('来源已变化'));
    c.dispose();
    api.dispose();
  });
  test('A-B-A永久退休，晚回包不能恢复旧source或旧批准', () async {
    final result = Completer<http.Response>();
    EntityActionIdentity identity = ('Bearer a', 'a', null);
    final api = EntityActionApi(
      client: MockClient((_) => result.future),
      apiBaseUrl: 'http://127.0.0.1:3707',
    );
    final c = EntityActionController(
      api: api,
      ref: actionRef,
      identity: () => identity,
    );
    final pending = c.load();
    identity = ('Bearer b', 'b', null);
    c.sync();
    identity = ('Bearer a', 'a', null);
    c.sync();
    result.complete(
      http.Response(
        jsonEncode({'data': actionWire()}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    expect(await pending, isNull);
    expect(c.view, isNull);
    expect(c.current, false);
    expect(await c.load(), isNull);
    c.dispose();
    api.dispose();
  });
  test('自然到期清空旧动作且不会写或自动重新GET', () async {
    int calls = 0;
    final api = EntityActionApi(
      client: MockClient((_) async {
        calls++;
        return http.Response(
          jsonEncode({
            'data': actionWire(ttl: const Duration(milliseconds: 150)),
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }),
      apiBaseUrl: 'http://127.0.0.1:3707',
    );
    final c = EntityActionController(
      api: api,
      ref: actionRef,
      identity: () => ('Bearer a', 'owner', null),
    );
    await c.load();
    expect(c.view, isNotNull);
    await Future<void>.delayed(const Duration(milliseconds: 300));
    c.expire();
    expect(c.view, isNull);
    expect(calls, 1);
    c.dispose();
    api.dispose();
  });
}
