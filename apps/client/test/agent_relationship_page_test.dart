import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/agent_relationship_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

Future<BirdtieAuthController> signedIn() async {
  final auth = BirdtieAuthController(
    client: MockClient(
      (r) async => switch ('${r.method} ${r.url.path}') {
        'GET /v1/auth/dev-phone/status' => http.Response(
          '{"data":{"enabled":true}}',
          200,
        ),
        'POST /v1/auth/dev-phone/code' => http.Response(
          '{"data":{"expiresInSeconds":300}}',
          200,
        ),
        'POST /v1/auth/dev-phone/verify' => http.Response(
          '{"data":{"accessToken":"test-session"}}',
          200,
        ),
        'GET /v1/me' => http.Response('{"data":{"id":"person"}}', 200),
        'GET /v1/accounts/person/profile' => http.Response(
          '{"data":{"displayName":"合成测试"}}',
          200,
        ),
        'POST /v1/session/logout' => http.Response('', 204),
        _ => http.Response('', 404),
      },
    ),
    apiBaseUrl: 'https://api.test',
    sessionVault: MemorySessionVault(),
  );
  await auth.initialize();
  await auth.requestDevPhoneCode('13800138000');
  await auth.verifyDevPhoneCode('13800138000', '123456');
  return auth;
}

http.Response data(Map<String, dynamic> value) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': value})), 200);
Map<String, dynamic> signals() => {
  'enabled': true,
  'truncated': false,
  'peers': [
    {
      'displayName': '合成好友',
      'frequency': 'RECENT_REPEATED',
      'sentMessages': 5,
      'activeDays': 3,
      'sharedActivities': [
        {'title': '合成公开羽毛球'},
      ],
      'activitiesTruncated': false,
    },
  ],
};

void main() {
  testWidgets('关系信号默认关闭，开启可审阅，关闭立即隐藏等待确认', (tester) async {
    final auth = await signedIn();
    var enabled = false, reads = 0;
    Completer<http.Response>? revoke;
    final client = MockClient((r) async {
      expect(r.headers['Authorization'], 'Bearer test-session');
      if (r.url.path.endsWith('agent-relationship-context')) {
        reads++;
        return data(signals());
      }
      expect(r.url.path, '/v1/me/agent-relationship-consent');
      if (r.method == 'PUT') {
        enabled =
            (jsonDecode(r.body) as Map<String, dynamic>)['enabled'] as bool;
        if (!enabled && revoke != null) return revoke.future;
      }
      return data({'enabled': enabled});
    });
    await tester.pumpWidget(
      MaterialApp(
        home: AgentRelationshipPage(
          auth: auth,
          client: client,
          apiBaseUrl: 'https://api.test',
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(reads, 0);
    expect(find.text('尚未开启，不向 Agent 提供关系信号。'), findsOneWidget);
    await tester.tap(find.byType(SwitchListTile));
    await tester.pumpAndSettle();
    expect(find.text('合成好友'), findsOneWidget);
    expect(find.text('共同报名：合成公开羽毛球'), findsOneWidget);
    expect(find.textContaining('本人发送 5 条 · 3 天'), findsOneWidget);
    revoke = Completer<http.Response>();
    await tester.tap(find.byType(SwitchListTile));
    await tester.pump();
    expect(find.text('合成好友'), findsNothing);
    revoke.complete(data({'enabled': false}));
    await tester.pumpAndSettle();
    expect(find.text('尚未开启，不向 Agent 提供关系信号。'), findsOneWidget);
    expect(reads, 1);
    await tester.pumpWidget(const SizedBox());
    client.close();
    auth.dispose();
  });

  testWidgets('注销清除关系，迟到响应不恢复，停用 Agent 可撤销授权', (tester) async {
    final auth = await signedIn();
    final late = Completer<http.Response>();
    var count = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('consent')) return data({'enabled': true});
      if (++count == 1) return data(signals());
      return late.future;
    });
    await tester.pumpWidget(
      MaterialApp(
        home: AgentRelationshipPage(
          auth: auth,
          client: client,
          apiBaseUrl: 'https://api.test',
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('合成好友'), findsOneWidget);
    await tester.tap(find.byTooltip('刷新'));
    await tester.pump();
    await auth.signOut();
    await tester.pump();
    expect(find.text('合成好友'), findsNothing);
    expect(find.text('登录后管理本人的 Agent 关系信号。'), findsOneWidget);
    late.complete(data(signals()));
    await tester.pumpAndSettle();
    expect(find.text('合成好友'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    client.close();
    auth.dispose();
  });

  testWidgets('读取失败可重试，停用 Agent 仍可关闭关系授权', (tester) async {
    final auth = await signedIn();
    var enabled = true, code = 403;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('context')) return http.Response('{}', code);
      if (r.method == 'PUT') {
        enabled =
            (jsonDecode(r.body) as Map<String, dynamic>)['enabled'] as bool;
      }
      return data({'enabled': enabled});
    });
    await tester.pumpWidget(
      MaterialApp(
        home: AgentRelationshipPage(
          auth: auth,
          client: client,
          apiBaseUrl: 'https://api.test',
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('关系信号暂不可用'), findsOneWidget);
    expect(find.text('合成好友'), findsNothing);
    await tester.tap(find.byType(SwitchListTile));
    await tester.pumpAndSettle();
    expect(enabled, false);
    expect(find.text('尚未开启，不向 Agent 提供关系信号。'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    client.close();
    auth.dispose();
  });
}
