import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/message_request_policy_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const policyOwner = '11000000-0000-4000-8000-000000000001';
const policyPeer = '11000000-0000-4000-8000-000000000002';
const policyAgent = '22000000-0000-4000-8000-000000000001';
DateTime policyClock() => DateTime.utc(2026, 10, 6, 12);
// Synthetic original registered-HTTP DTO shape; not a native DB receipt.
Map<String, dynamic> policyData({
  int version = 0,
  String incoming = 'REQUEST',
  String owner = policyOwner,
  String? agent,
  String? status,
  DateTime? expires,
}) => {
  'schemaVersion': 'agent-message-request-policy-v1',
  'ownerId': owner,
  'agentId': agent ?? policyAgent,
  'nativeRevision': version,
  'configured': version > 0,
  'status': status ?? (version > 0 ? 'ACTIVE' : 'UNCONFIGURED'),
  'incomingRequests': incoming,
  'observedAt': policyClock().toIso8601String(),
  if (version > 0)
    'validFrom': policyClock()
        .subtract(const Duration(hours: 1))
        .toIso8601String(),
  if (version > 0)
    'expiresAt': (expires ?? policyClock().add(const Duration(days: 7)))
        .toIso8601String(),
};
http.Response policyWire(Map<String, dynamic> p) => http.Response(
  jsonEncode({'data': p}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  test('本人读取不写入；有限期和具体版本审批仅生成原三字段PUT', () async {
    final requests = <http.Request>[];
    final c = MessageRequestPolicyController(
      authorizationHeader: () => 'Bearer fixture',
      accountID: () => policyOwner,
      apiBaseUrl: 'http://policy.test',
      now: policyClock,
      client: MockClient((r) async {
        requests.add(r);
        if (r.method == 'GET') return policyWire(policyData());
        final b = jsonDecode(r.body) as Map<String, dynamic>;
        return policyWire(
          policyData(
            version: 1,
            incoming: b['incomingRequests'] as String,
            expires: DateTime.parse(b['expiresAt'] as String),
          ),
        );
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    expect(requests.single.method, 'GET');
    expect(c.preview(), isNull);
    c.edit(
      MessageRequestDraft('SCREEN', policyClock().add(const Duration(days: 1))),
    );
    final a = c.preview()!;
    await c.save(a);
    await c.save(a);
    expect(requests.where((r) => r.method == 'PUT').length, 1);
    expect(c.policy!.version, 1);
    expect(c.message, '消息请求设置已保存。');
    final put = requests.last;
    expect(put.headers['Authorization'], 'Bearer fixture');
    expect(put.url.path, '/v1/me/message-request-policy');
    expect(put.url.query, isEmpty);
    expect((jsonDecode(put.body) as Map).keys.toSet(), {
      'expectedVersion',
      'incomingRequests',
      'expiresAt',
    });
  });
  for (final bad in [
    'ALLOW',
    'AUTOMATIC_SCREEN',
    'confirmed',
    'BLOCK_ALL_MESSAGES',
  ]) {
    test('未知或赋权枚举 $bad 无法成为收件设置', () async {
      final c = MessageRequestPolicyController(
        authorizationHeader: () => 'Bearer fixture',
        accountID: () => policyOwner,
        now: policyClock,
        client: MockClient((r) async => policyWire(policyData(incoming: bad))),
      );
      addTearDown(c.dispose);
      await c.load();
      expect(c.policy, isNull);
      expect(c.preview(), isNull);
    });
  }
  for (final days in [0, 31]) {
    test('不接受过期或超过30天草稿 $days', () async {
      final c = MessageRequestPolicyController(
        authorizationHeader: () => 'Bearer fixture',
        accountID: () => policyOwner,
        now: policyClock,
        client: MockClient((r) async => policyWire(policyData())),
      );
      addTearDown(c.dispose);
      await c.load();
      c.edit(
        MessageRequestDraft('BLOCK', policyClock().add(Duration(days: days))),
      );
      expect(c.preview(), isNull);
    });
  }
  test('过期配置如实保留并要求重新选期限，未设置不默默创建', () async {
    final c = MessageRequestPolicyController(
      authorizationHeader: () => 'Bearer fixture',
      accountID: () => policyOwner,
      now: policyClock,
      client: MockClient(
        (r) async => policyWire(
          policyData(
            version: 2,
            status: 'EXPIRED',
            expires: policyClock().subtract(const Duration(minutes: 1)),
          ),
        ),
      ),
    );
    addTearDown(c.dispose);
    await c.load();
    expect(c.policy!.status, 'EXPIRED');
    expect(c.draft!.expiresAt, isNull);
    expect(c.preview(), isNull);
  });
  test('修改或重新读取当前来源废除旧预览，不借相同字段复活批准', () async {
    var puts = 0;
    final c = MessageRequestPolicyController(
      authorizationHeader: () => 'Bearer fixture',
      accountID: () => policyOwner,
      now: policyClock,
      client: MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return policyWire(policyData(version: 1));
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    final a = c.preview()!;
    c.edit(MessageRequestDraft('BLOCK', c.draft!.expiresAt));
    c.edit(a.draft);
    await c.save(a);
    expect(puts, 0);
    final b = c.preview()!;
    await c.load();
    await c.save(b);
    expect(puts, 0);
  });
  for (final failure in [
    'throw',
    '500',
    '404',
    'invalid200',
    'wrongVersion',
    'wrongAgent',
  ]) {
    test('未知PUT $failure 只GET核实，相同version+1内容不伪称原保存成功', () async {
      var puts = 0, reads = 0;
      final end = policyClock().add(const Duration(days: 7));
      final c = MessageRequestPolicyController(
        authorizationHeader: () => 'Bearer fixture',
        accountID: () => policyOwner,
        now: policyClock,
        client: MockClient((r) async {
          if (r.method == 'GET') {
            reads++;
            return policyWire(
              policyData(version: reads == 1 ? 1 : 2, expires: end),
            );
          }
          puts++;
          return switch (failure) {
            'throw' => throw http.ClientException('PRIVATE_CANARY'),
            '500' => http.Response('{}', 500),
            '404' => http.Response('{}', 404),
            'invalid200' => http.Response('{}', 200),
            'wrongVersion' => policyWire(policyData(version: 8)),
            _ => policyWire(
              policyData(
                version: 2,
                agent: '22000000-0000-4000-8000-000000000099',
              ),
            ),
          };
        }),
      );
      addTearDown(c.dispose);
      await c.load();
      final a = c.preview()!;
      await c.save(a);
      expect(c.uncertain, isTrue);
      expect(c.preview(), isNull);
      expect(c.message, contains('保存结果尚未确认'));
      await c.save(a);
      expect(puts, 1);
      await c.load();
      expect(puts, 1);
      expect(reads, 2);
      expect(c.uncertain, isFalse);
      expect(c.policy!.version, 2);
      expect(c.message, contains('不能确认上次保存是否成功'));
      expect(c.message, isNot(contains('已保存。')));
    });
  }
  for (final code in [400, 401, 403, 409]) {
    test('拒绝响应 $code 不重发；会话迟到拒绝仍保存未知，重读重审', () async {
      var puts = 0, reads = 0;
      final c = MessageRequestPolicyController(
        authorizationHeader: () => 'Bearer fixture',
        accountID: () => policyOwner,
        now: policyClock,
        client: MockClient((r) async {
          if (r.method == 'GET') {
            reads++;
            return policyWire(policyData(version: reads));
          }
          puts++;
          return http.Response('{}', code);
        }),
      );
      addTearDown(c.dispose);
      await c.load();
      final a = c.preview()!;
      await c.save(a);
      expect(c.policy, isNull);
      expect(c.error, isNotNull);
      expect(c.uncertain, code == 401 || code == 403);
      if (code == 401 || code == 403) expect(c.error, contains('保存结果尚未确认'));
      await c.save(a);
      expect(puts, 1);
      await c.load();
      expect(c.policy!.version, 2);
      await c.save(a);
      expect(puts, 1);
    });
  }
  for (final mode in ['owner', 'token', 'org']) {
    test('通知中的 $mode ABA 移除旧批准及迟到结果', () async {
      var owner = policyOwner, token = 'Bearer A';
      String? org;
      final changes = ValueNotifier(0), gate = Completer<http.Response>();
      var gets = 0, puts = 0;
      final c = MessageRequestPolicyController(
        authorizationHeader: () => token,
        accountID: () => owner,
        organizationWorkspaceID: () => org,
        identityChanges: changes,
        now: policyClock,
        client: MockClient((r) async {
          if (r.method == 'GET') {
            gets++;
            return policyWire(policyData());
          }
          puts++;
          expect(r.headers['Authorization'], 'Bearer A');
          return gate.future;
        }),
      );
      addTearDown(c.dispose);
      addTearDown(changes.dispose);
      await c.load();
      c.edit(
        MessageRequestDraft(
          'BLOCK',
          policyClock().add(const Duration(days: 1)),
        ),
      );
      final a = c.preview()!;
      final save = c.save(a);
      if (mode == 'owner') {
        owner = policyPeer;
      } else if (mode == 'token') {
        token = 'Bearer B';
      } else {
        org = policyPeer;
      }
      changes.value++;
      owner = policyOwner;
      token = 'Bearer A';
      org = null;
      changes.value++;
      gate.complete(
        policyWire(
          policyData(
            version: 1,
            incoming: 'BLOCK',
            expires: policyClock().add(const Duration(days: 1)),
          ),
        ),
      );
      await save;
      expect(c.policy, isNull);
      expect(c.message, isNull);
      expect(c.draft, isNull);
      await c.save(a);
      expect(puts, 1);
      expect(gets, 1);
    });
  }
  test('当前本人不匹配或有未知权限字段的GET失败关闭', () async {
    var peer = true;
    final c = MessageRequestPolicyController(
      authorizationHeader: () => 'Bearer fixture',
      accountID: () => policyOwner,
      now: policyClock,
      client: MockClient(
        (r) async => policyWire(
          peer
              ? policyData(owner: policyPeer)
              : {...policyData(), 'confirmed': true},
        ),
      ),
    );
    addTearDown(c.dispose);
    await c.load();
    expect(c.policy, isNull);
    peer = false;
    await c.load();
    expect(c.policy, isNull);
  });
  test('保存前通知期间身份退休，捕获旧参数也不得继续发PUT', () async {
    var token = 'Bearer A', puts = 0;
    final changes = ValueNotifier(0);
    final c = MessageRequestPolicyController(
      authorizationHeader: () => token,
      accountID: () => policyOwner,
      identityChanges: changes,
      now: policyClock,
      client: MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return policyWire(policyData(version: 1));
      }),
    );
    addTearDown(c.dispose);
    addTearDown(changes.dispose);
    await c.load();
    final a = c.preview()!;
    c.addListener(() {
      if (c.saving && token == 'Bearer A') {
        token = 'Bearer B';
        changes.value++;
      }
    });
    await c.save(a);
    expect(puts, 0);
    expect(c.policy, isNull);
    expect(c.draft, isNull);
  });
}
