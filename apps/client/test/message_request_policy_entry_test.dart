import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'message_request_policy_controller_test.dart'
    show policyData, policyWire;

const messagePolicyOwner = '11000000-0000-4000-8000-000000000001';

void main() {
  testWidgets('AGE042已有Settings真实入口应能找到消息请求设置', (t) async {
    final auth = SeedTestAuth()..owner = messagePolicyOwner;
    final city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      return r.url.path == '/v1/me/message-request-policy'
          ? policyWire(policyData())
          : http.Response('{"data":[]}', 200);
    });
    addTearDown(() {
      auth.dispose();
      city.dispose();
      moments.dispose();
      client.close();
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: 'http://message-policy-entry.test',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(find.text('消息请求设置'), 220, maxScrolls: 30);
      await t.pumpAndSettle();
    expect(find.text('消息请求设置').hitTestable(), findsOneWidget);
    await t.tap(find.text('消息请求设置'));
    await t.pumpAndSettle();
    expect(find.text('尚未设置：沿用原普通请求流程。'), findsOneWidget);
    expect(
      requests
          .where((r) => r.url.path == '/v1/me/message-request-policy')
          .length,
      1,
    );
    expect(requests.where((r) => r.method != 'GET'), isEmpty);
    await t.pageBack();
    await t.pumpAndSettle();
    expect(find.byType(SettingsPage), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });
  for (final mode in ['owner', 'org', 'base', 'transport']) {
    testWidgets('Settings真实消息策略目的地 $mode ABA退休，可返回且零旧PUT', (t) async {
      final auth = SeedTestAuth()..owner = messagePolicyOwner;
      final city = PublicCityController(),
          workspace = ValueNotifier<String?>(null);
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
      );
      final requests = <http.Request>[];
      Future<http.Response> respond(http.Request r) async {
        requests.add(r);
        return r.url.path == '/v1/me/message-request-policy'
            ? policyWire(policyData(owner: auth.owner))
            : http.Response('{"data":[]}', 200);
      }

      final client = MockClient(respond), other = MockClient(respond);
      final source = ValueNotifier<(http.Client, String)>((
        client,
        'http://message-policy-entry.test',
      ));
      String? org() => workspace.value;
      addTearDown(() {
        auth.dispose();
        city.dispose();
        moments.dispose();
        workspace.dispose();
        source.dispose();
        client.close();
        other.close();
      });
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<(http.Client, String)>(
            valueListenable: source,
            builder: (context, s, _) => Scaffold(
              body: SettingsPage(
                auth: auth,
                city: city,
                moments: moments,
                client: s.$1,
                apiBaseUrl: s.$2,
                workspaceChanges: workspace,
                organizationWorkspaceID: org,
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.scrollUntilVisible(find.text('消息请求设置'), 220, maxScrolls: 30);
      await t.pumpAndSettle();
      await t.tap(find.text('消息请求设置'));
      await t.pumpAndSettle();
      expect(find.text('尚未设置：沿用原普通请求流程。'), findsOneWidget);
      await t.scrollUntilVisible(
        find.text('1 天'),
        180,
        scrollable: find.byType(Scrollable).last,
      );
      await t.pumpAndSettle();
      expect(find.text('1 天').hitTestable(), findsOneWidget);
      await t.tap(find.text('1 天'));
      await t.pumpAndSettle();
      await t.scrollUntilVisible(
        find.text('检查并保存设置'),
        180,
        scrollable: find.byType(Scrollable).last,
      );
      await t.pumpAndSettle();
      expect(find.text('检查并保存设置').hitTestable(), findsOneWidget);
      await t.tap(find.text('检查并保存设置'));
      await t.pumpAndSettle();
      expect(find.text('确认保存'), findsOneWidget);
      if (mode == 'owner') {
        auth.changeIdentity(
          'Bearer peer',
          nextOwner: '11000000-0000-4000-8000-000000000002',
        );
      } else if (mode == 'org') {
        workspace.value = '11000000-0000-4000-8000-000000000002';
      } else if (mode == 'base') {
        source.value = (client, 'http://message-policy-other.test');
      } else {
        source.value = (other, 'http://message-policy-entry.test');
      }
      await t.pumpAndSettle();
      auth.changeIdentity('Bearer owner', nextOwner: messagePolicyOwner);
      workspace.value = null;
      source.value = (client, 'http://message-policy-entry.test');
      await t.pumpAndSettle();
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
      expect(find.text('确认保存'), findsNothing);
      expect(requests.where((r) => r.method == 'PUT'), isEmpty);
      await t.pageBack();
      await t.pumpAndSettle();
      expect(find.byType(SettingsPage), findsOneWidget);
      await t.pumpWidget(const SizedBox());
    });
  }
}
