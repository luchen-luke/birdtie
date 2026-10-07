import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:birdtie_client/src/workspace/notification_policy_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'inbox_panel_test.dart' show signedInAuth;
import 'notification_policy_controller_test.dart' show currentPolicy, wire;

void main() {
  testWidgets('AIR019实际Settings入口读取未配置不自动写入或调度', (t) async {
    final auth = await signedInAuth();
    final city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      return http.Response(
        r.url.path == '/v1/me/notification-schedule'
            ? '{"data":{"schemaVersion":"native-notification-schedule-v1","version":0,"agentId":"82000000-0000-4000-8000-000000000001","configured":false,"status":"UNCONFIGURED","budgetWindowHours":24,"enabled":false,"timeZone":"","localMinute":0,"gapPolicy":"","foldPolicy":"","quiet":null,"maxContactsPerDay":0,"categories":[]}}'
            : '{"data":[]}',
        200,
      );
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: 'http://schedule-entry.test',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(find.text('定时汇总计划'), 200, maxScrolls: 20);
    expect(find.text('定时汇总计划').hitTestable(), findsOneWidget);
    await t.tap(find.text('定时汇总计划'));
    await t.pumpAndSettle();
    expect(find.text('尚未设置定时汇总：不会自动创建每日计划。'), findsOneWidget);
    expect(
      requests
          .where((r) => r.url.path == '/v1/me/notification-schedule')
          .length,
      1,
    );
    expect(requests.where((r) => r.method != 'GET'), isEmpty);
    await t.pageBack();
    await t.pumpAndSettle();
    expect(find.byType(SettingsPage), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    city.dispose();
    moments.dispose();
    client.close();
  });

  testWidgets('Settings direct entry reads current policy without writing', (
    tester,
  ) async {
    final auth = await signedInAuth();
    final city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    var reads = 0, writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      if (r.url.path.endsWith('/notification-policy')) {
        reads++;
        return wire(currentPolicy());
      }
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('通知设置'), 200);
    await tester.pumpAndSettle();
    expect(find.text('通知设置').hitTestable(), findsOneWidget);
    await tester.tap(find.text('通知设置'));
    await tester.pumpAndSettle();
    expect(find.byType(NotificationPolicyPage), findsOneWidget);
    expect(reads, 1);
    expect(writes, 0);
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(find.byType(SettingsPage), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    city.dispose();
    moments.dispose();
    client.close();
  });
}
