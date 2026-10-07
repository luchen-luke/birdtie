import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_memory_correction_api_test.dart';
import 'agent_memory_correction_page_test.dart' show correctionSettle;
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_introduction_controller_test.dart' show introReads;
import 'model_egress_page_test.dart' show egressAuth;

void main() {
  testWidgets('Settings本人直入口管理记忆 原GET真空body无自动写', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final auth = SeedTestAuth()..owner = correctionOwner;
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      expect(r.method, 'GET');
      expect(r.bodyBytes, isEmpty);
      if (r.url.path.endsWith('agent-memories')) {
        return correctionResponse({
          'data': [correctionMemoryRaw(at: DateTime.now().toUtc())],
        });
      }
      if (r.url.path.endsWith('agent-memory-candidates')) {
        return correctionResponse({'data': []});
      }
      return correctionResponse({'data': []});
    });
    final city = PublicCityController(),
        moments = PrivateMomentController(
          authorizationHeader: () => auth.authorizationHeader,
        );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: 'http://local',
          ),
        ),
      ),
    );
    await correctionSettle(t);
    await t.scrollUntilVisible(find.text('管理我的记忆'), 150);
    await t.tap(find.text('管理我的记忆'));
    await correctionSettle(t);
    expect(find.text('修改这条记忆'), findsOneWidget);
    expect(calls.any((r) => r.url.path == '/v1/me/agent-memories'), true);
    expect(calls.every((r) => r.method == 'GET'), true);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    city.dispose();
    moments.dispose();
    client.close();
  });
  testWidgets('设置页API重绑关闭已打开的旧偏好批准，切回也不复活', (tester) async {
    final auth = await egressAuth();
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      expectSync(r.method, 'GET');
      if (r.url.path == '/v1/me/blocks' || r.url.path == '/v1/me/consents') {
        return http.Response('{"data":[]}', 200);
      }
      return introReads(r, consent: false, intents: []);
    });
    final base = ValueNotifier<String>('http://fixture');
    final city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<String>(
          valueListenable: base,
          builder: (context, value, _) => Scaffold(
            body: SettingsPage(
              key: const ValueKey('same-settings'),
              auth: auth,
              city: city,
              moments: moments,
              client: client,
              apiBaseUrl: value,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('引荐与社交许可'), 150);
    await tester.pumpAndSettle();
    await tester.tap(find.text('引荐与社交许可'));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('检查本人社交偏好'), 150);
    await tester.pumpAndSettle();
    await tester.tap(find.text('检查本人社交偏好'));
    await tester.pumpAndSettle();
    expect(find.text('编辑本人社交偏好'), findsOneWidget);
    base.value = 'http://fixture2';
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(requests.any((r) => r.url.host == 'fixture2'), true);
    base.value = 'http://fixture';
    await tester.pumpAndSettle();
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(find.text('保存本人偏好'), findsNothing);
    expect(requests.every((r) => r.method == 'GET'), true);
    await tester.pumpWidget(const SizedBox());
    auth.dispose();
    city.dispose();
    moments.dispose();
    base.dispose();
    client.close();
  });

  testWidgets('Settings引荐入口复用本人接口和原管理页，不自动授权或发送', (tester) async {
    final auth = await egressAuth();
    final calls = <http.Request>[];
    final client = MockClient((request) async {
      calls.add(request);
      expectSync(request.method, 'GET');
      expectSync(request.url.host, 'fixture');
      if (request.url.path == '/v1/me/blocks' ||
          request.url.path == '/v1/me/consents') {
        return http.Response('{"data":[]}', 200);
      }
      return introReads(request, consent: false, intents: []);
    });
    final city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    final workspace = ValueNotifier<String?>(null);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: 'http://fixture',
            workspaceChanges: workspace,
            organizationWorkspaceID: () => workspace.value,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('引荐与社交许可'), 150);
    await tester.pumpAndSettle();
    await tester.tap(find.text('引荐与社交许可'));
    await tester.pumpAndSettle();
    expect(find.text('看看共同意图'), findsOneWidget);
    expect(calls.any((r) => r.url.path == '/v1/me/agent-policies'), true);
    await tester.scrollUntilVisible(find.text('前往找新朋友'), 150);
    await tester.pumpAndSettle();
    await tester.tap(find.text('前往找新朋友'));
    await tester.pumpAndSettle();
    expect(find.text('尚未开启，不查询候选或发送新朋友邀请。'), findsOneWidget);
    final count = calls.length;
    workspace.value = '22222222-2222-4222-8222-222222222222';
    await tester.pumpAndSettle();
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(calls.length, count);
    expect(calls.every((r) => r.method == 'GET'), true);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    city.dispose();
    moments.dispose();
    workspace.dispose();
    client.close();
  });

  testWidgets('Settings opens same human model budget with current workspace', (
    tester,
  ) async {
    const owner = '11111111-1111-4111-8111-111111111111';
    final authClient = MockClient((request) async {
      switch ('${request.method} ${request.url.path}') {
        case 'GET /v1/auth/dev-phone/status':
          return http.Response('{"data":{"enabled":true}}', 200);
        case 'POST /v1/auth/dev-phone/code':
          return http.Response('{"data":{"expiresInSeconds":300}}', 200);
        case 'POST /v1/auth/dev-phone/verify':
          return http.Response(
            '{"data":{"accessToken":"settings-session"}}',
            200,
          );
        case 'GET /v1/me':
          return http.Response('{"data":{"id":"$owner"}}', 200);
        case 'GET /v1/accounts/$owner/profile':
          return http.Response('{"data":{"displayName":"本地测试本人"}}', 200);
        default:
          return http.Response('', 404);
      }
    });
    final auth = BirdtieAuthController(
      client: authClient,
      apiBaseUrl: 'http://settings.test',
      sessionVault: MemorySessionVault(),
    );
    await auth.initialize();
    await auth.requestDevPhoneCode('13800138000');
    await auth.verifyDevPhoneCode('13800138000', '123456');
    final requests = <String>[];
    final client = MockClient((request) async {
      expect(request.url.host, 'settings.test');
      expect(request.headers['Authorization'], 'Bearer settings-session');
      expect(request.method, 'GET');
      requests.add(request.url.path);
      if (request.url.path == '/v1/me/model-egress/options') {
        return http.Response(
          '{"data":{"schemaVersion":"air.model_egress_human.v1",'
          '"ownerId":"$owner","modelAccess":"UNAVAILABLE",'
          '"observedAt":"2026-10-04T05:00:00+08:00","options":[]}}',
          200,
        );
      }
      if (request.url.path == '/v1/me/model-egress/previews') {
        return http.Response('{"data":[],"modelAccess":"UNAVAILABLE"}', 200);
      }
      return http.Response('{"data":[]}', 200);
    });
    final city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    final workspace = ValueNotifier<String?>(null);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: 'http://settings.test',
            workspaceChanges: workspace,
            organizationWorkspaceID: () => workspace.value,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('模型请求与预算'), 100);
    await tester.pumpAndSettle();
    await tester.tap(find.text('模型请求与预算'));
    await tester.pumpAndSettle();
    expect(find.text('本人任务的人审记录'), findsOneWidget);
    expect(requests, contains('/v1/me/model-egress/options'));
    expect(requests, contains('/v1/me/model-egress/previews'));
    final count = requests.length;
    workspace.value = '22222222-2222-4222-8222-222222222222';
    await tester.pumpAndSettle();
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(requests.length, count);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    city.dispose();
    moments.dispose();
    workspace.dispose();
    client.close();
  });

  testWidgets('Settings manages blocks and profile grants', (tester) async {
    final authClient = MockClient((request) async {
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
          return http.Response(
            '{"data":{"displayName":"Birdtie tester"}}',
            200,
          );
        default:
          return http.Response('', 404);
      }
    });
    final auth = BirdtieAuthController(
      client: authClient,
      apiBaseUrl: 'http://localhost:8080',
      sessionVault: MemorySessionVault(),
    );
    await auth.initialize();
    await auth.requestDevPhoneCode('13800138000');
    await auth.verifyDevPhoneCode('13800138000', '123456');
    var blocked = true;
    var granted = true;
    final settingsClient = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer test-session');
      switch ('${request.method} ${request.url.path}') {
        case 'GET /v1/me/blocks':
          return http.Response(
            blocked ? '{"data":[{"accountId":"blocked-1"}]}' : '{"data":[]}',
            200,
          );
        case 'GET /v1/me/consents':
          return http.Response(
            granted
                ? '{"data":[{"id":"grant-1","recipientAccountId":"recipient-1","expiresAt":"2099-01-01T00:00:00Z"}]}'
                : '{"data":[]}',
            200,
          );
        case 'DELETE /v1/me/blocks/blocked-1':
          blocked = false;
          return http.Response('', 204);
        case 'DELETE /v1/me/consents/grant-1':
          granted = false;
          return http.Response('', 204);
        default:
          return http.Response('', 404);
      }
    });
    final city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: settingsClient,
            apiBaseUrl: 'http://localhost:8080',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('blocked-1'), 100);
    expect(find.text('blocked-1'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('recipient-1'), 100);
    expect(find.text('recipient-1'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('取消屏蔽'), -100);
    await tester.tap(find.text('取消屏蔽'));
    await tester.pumpAndSettle();
    expect(find.text('没有已屏蔽的账号。'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('撤销授权'), 100);
    await tester.tap(find.text('撤销授权'));
    await tester.pumpAndSettle();
    expect(find.text('当前没有有效的个人资料访问授权。'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    city.dispose();
    moments.dispose();
    settingsClient.close();
  });
}
