import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/shared_social_context_panel.dart';
import 'package:birdtie_client/src/workspace/social_disclosure_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('共同信息显示授权信号，换账号隐藏旧数据', (tester) async {
    var token = 'Bearer first';
    final next = Completer<http.Response>();
    final client = MockClient((request) async {
      if (request.headers['Authorization'] != 'Bearer first') {
        return next.future;
      }
      return http.Response.bytes(
        utf8.encode(
          jsonEncode({
            'data': {
              'schema': 'shared-relationship-history-v1',
              'viewerId': '11111111-1111-4111-8111-111111111111',
              'targetId': '22222222-2222-4222-8222-222222222222',
              'observedAt': DateTime.now().toUtc().toIso8601String(),
              'validUntil': DateTime.now()
                  .toUtc()
                  .add(const Duration(seconds: 29))
                  .toIso8601String(),
              'attendance': 'UNKNOWN',
              'visit': 'UNKNOWN',
              'communitiesTruncated': false,
              'activitiesTruncated': false,
              'mutualCount': 2,
              'communities': [
                {
                  'id': '33333333-3333-4333-8333-333333333333',
                  'title': '公开羽毛球社群',
                },
              ],
              'activities': [
                {
                  'id': '44444444-4444-4444-8444-444444444444',
                  'title': '周末羽毛球',
                  'startsAt': DateTime.now()
                      .toUtc()
                      .add(const Duration(hours: 1))
                      .toIso8601String(),
                  'endsAt': DateTime.now()
                      .toUtc()
                      .add(const Duration(hours: 2))
                      .toIso8601String(),
                  'timeZone': 'Europe/London',
                  'modality': 'in_person',
                },
              ],
            },
          }),
        ),
        200,
      );
    });
    Widget screen() => MaterialApp(
      home: Scaffold(
        body: SharedSocialContextPanel(
          accountID: '22222222-2222-4222-8222-222222222222',
          authorizationHeader: () => token,
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpWidget(screen());
    await tester.pumpAndSettle();
    expect(find.text('可展示的共同好友：2 位'), findsOneWidget);
    expect(find.text('公开羽毛球社群'), findsOneWidget);
    expect(find.text('周末羽毛球'), findsOneWidget);
    token = 'Bearer second';
    await tester.pumpWidget(screen());
    await tester.pump();
    expect(find.text('公开羽毛球社群'), findsNothing);
    next.complete(
      http.Response(
        jsonEncode({
          'data': {
            'schema': 'shared-relationship-history-v1',
            'viewerId': '11111111-1111-4111-8111-111111111111',
            'targetId': '22222222-2222-4222-8222-222222222222',
            'observedAt': DateTime.now().toUtc().toIso8601String(),
            'validUntil': DateTime.now()
                .toUtc()
                .add(const Duration(seconds: 29))
                .toIso8601String(),
            'attendance': 'UNKNOWN',
            'visit': 'UNKNOWN',
            'mutualCount': 0,
            'communities': [],
            'activities': [],
            'communitiesTruncated': false,
            'activitiesTruncated': false,
          },
        }),
        200,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('暂无可展示的共同信息。'), findsOneWidget);
    client.close();
  });

  testWidgets('不可访问的目标不显示共同信息，服务失败可重试', (tester) async {
    var code = 500;
    final client = MockClient((_) async => http.Response('{}', code));
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SharedSocialContextPanel(
            accountID: '22222222-2222-4222-8222-222222222222',
            authorizationHeader: () => 'Bearer person',
            apiBaseUrl: 'https://api.test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('共同信息暂不可用，点击重试。'), findsOneWidget);
    code = 404;
    await tester.tap(find.text('共同信息暂不可用，点击重试。'));
    await tester.pumpAndSettle();
    expect(find.text('你们的共同信息'), findsNothing);
    expect(find.text('暂无可展示的共同信息。'), findsNothing);
    client.close();
  });

  testWidgets('展示开关默认关闭，服务端确认后生效，失败保留原值', (tester) async {
    final authClient = MockClient((request) async {
      switch ('${request.method} ${request.url.path}') {
        case 'GET /v1/auth/dev-phone/status':
          return http.Response('{"data":{"enabled":true}}', 200);
        case 'POST /v1/auth/dev-phone/code':
          return http.Response('{"data":{"expiresInSeconds":300}}', 200);
        case 'POST /v1/auth/dev-phone/verify':
          return http.Response('{"data":{"accessToken":"test-session"}}', 200);
        case 'GET /v1/me':
          return http.Response('{"data":{"id":"person"}}', 200);
        case 'GET /v1/accounts/person/profile':
          return http.Response('{"data":{"displayName":"合成测试"}}', 200);
        case 'POST /v1/session/logout':
          return http.Response('', 204);
        default:
          return http.Response('', 404);
      }
    });
    final auth = BirdtieAuthController(
      client: authClient,
      apiBaseUrl: 'https://api.test',
      sessionVault: MemorySessionVault(),
    );
    await auth.initialize();
    await auth.requestDevPhoneCode('13800138000');
    await auth.verifyDevPhoneCode('13800138000', '123456');
    var values = {
      'mutualTies': false,
      'sharedCommunities': false,
      'sharedActivities': false,
    };
    var fail = false;
    final settingsClient = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer test-session');
      expect(request.url.path, '/v1/me/social-disclosure');
      if (request.method == 'PUT') {
        if (fail) return http.Response('', 500);
        values = (jsonDecode(request.body) as Map<String, dynamic>)
            .cast<String, bool>();
      }
      return http.Response(jsonEncode({'data': values}), 200);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SocialDisclosurePage(
          auth: auth,
          apiBaseUrl: 'https://api.test',
          client: settingsClient,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(
      tester
          .widgetList<SwitchListTile>(find.byType(SwitchListTile))
          .every((s) => !s.value),
      isTrue,
    );
    await tester.tap(find.text('共同好友数量'));
    await tester.pumpAndSettle();
    expect(values['mutualTies'], isTrue);
    fail = true;
    await tester.tap(find.text('共同公开社群'));
    await tester.pumpAndSettle();
    expect(values['sharedCommunities'], isFalse);
    expect(find.text('保存结果暂未确认，请刷新核对当前设置。'), findsOneWidget);
    await auth.signOut();
    await tester.pumpAndSettle();
    expect(find.byType(SwitchListTile), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    authClient.close();
    settingsClient.close();
  });
}
