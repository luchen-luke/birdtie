import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/person_contexts_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('本人以中文管理当前城市和线上情境', (tester) async {
    final declarations = <Map<String, dynamic>>[];
    final client = MockClient((request) async {
      switch ('${request.method} ${request.url.path}') {
        case 'GET /v1/auth/dev-phone/status':
          return http.Response('{"data":{"enabled":true}}', 200);
        case 'POST /v1/auth/dev-phone/code':
          return http.Response('{"data":{"expiresInSeconds":300}}', 200);
        case 'POST /v1/auth/dev-phone/verify':
          return http.Response('{"data":{"accessToken":"test-session"}}', 200);
        case 'GET /v1/me':
          return http.Response('{"data":{"id":"account-1"}}', 200);
        case 'GET /v1/accounts/account-1/profile':
          return http.Response('{"data":{"displayName":"测试用户"}}', 200);
        case 'GET /v1/cities':
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': [
                  {'id': 'aberdeen-gb', 'name': '阿伯丁'},
                  {'id': 'london-gb', 'name': '伦敦'},
                ],
              }),
            ),
            200,
          );
        case 'GET /v1/me/contexts':
          expect(request.headers['Authorization'], 'Bearer test-session');
          return http.Response.bytes(
            utf8.encode(jsonEncode({'data': declarations})),
            200,
          );
        case 'POST /v1/me/contexts':
          expect(request.headers['Authorization'], 'Bearer test-session');
          final body = jsonDecode(request.body) as Map<String, dynamic>;
          final item = <String, dynamic>{
            'contextId': '11111111-1111-4111-8111-111111111111',
            'contextType': body['contextType'],
            'sourceKey': body['sourceKey'],
            'label': body['sourceKey'] == 'aberdeen-gb'
                ? '阿伯丁'
                : body['sourceKey'],
            'relation': body['relation'],
            'visibility': 'private',
          };
          declarations.add(item);
          return http.Response.bytes(
            utf8.encode(jsonEncode({'data': item})),
            201,
          );
        case 'DELETE /v1/me/contexts/11111111-1111-4111-8111-111111111111/current':
          declarations.removeWhere((item) => item['relation'] == 'current');
          return http.Response('', 204);
        default:
          return http.Response('', 404);
      }
    });
    final auth = BirdtieAuthController(
      client: client,
      apiBaseUrl: 'https://api.test',
      sessionVault: MemorySessionVault(),
    );
    await auth.initialize();
    await auth.requestDevPhoneCode('13800138000');
    await auth.verifyDevPhoneCode('13800138000', '123456');
    await tester.pumpWidget(
      MaterialApp(
        home: PersonContextsPage(
          auth: auth,
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('我的生活情境'), findsOneWidget);
    expect(find.textContaining('仅自己可见'), findsOneWidget);
    await tester.ensureVisible(find.text('保存情境'));
    await tester.tap(find.text('保存情境'));
    await tester.pumpAndSettle();
    expect(declarations.single['sourceKey'], 'aberdeen-gb');
    expect(declarations.single['relation'], 'current');
    expect(find.text('阿伯丁'), findsWidgets);
    await tester.tap(find.byIcon(Icons.close).first);
    await tester.pumpAndSettle();
    expect(declarations, isEmpty);
    await tester.ensureVisible(find.text('当前城市').last);
    await tester.tap(find.text('当前城市').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('线上兴趣或社群').last);
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), '线上羽毛球');
    await tester.ensureVisible(find.text('保存情境'));
    await tester.tap(find.text('保存情境'));
    await tester.pumpAndSettle();
    expect(declarations.single['contextType'], 'ONLINE');
    expect(declarations.single['relation'], 'interest');
    expect(find.text('线上羽毛球'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    client.close();
  });
}
