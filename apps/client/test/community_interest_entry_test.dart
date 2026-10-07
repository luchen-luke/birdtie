import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/person_community_interest_page.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'model_egress_page_test.dart' show egressAuth;
import 'person_community_interest_api_test.dart';
import 'person_community_interest_controller_test.dart' show interestReads;
import 'person_community_interest_page_test.dart' show interestTap;

void main() {
  for (final mode in ['cancel', 'transport-aba', 'workspace-aba']) {
    testWidgets('真实设置社群声明入口 $mode 清除原批准且零提交', (t) async {
      final auth = await egressAuth();
      final city = PublicCityController();
      final transport = ValueNotifier<bool>(false);
      final workspace = ValueNotifier<String?>(null);
      final requests = <http.Request>[];
      final client = MockClient((r) async {
        requests.add(r);
        if (r.url.path.endsWith('/preview')) {
          return interestResponse(interestPreview());
        }
        if (r.url.path.startsWith('/v1/me/community-interests')) {
          return interestReads(r);
        }
        return http.Response('{"data":[]}', 200);
      });
      final nextRequests = <http.Request>[];
      final nextClient = MockClient((r) async {
        nextRequests.add(r);
        return http.Response('{"data":[]}', 200);
      });
      final moments = PrivateMomentController(
        client: client,
        apiBaseUrl: 'http://source-a',
        authorizationHeader: () => auth.authorizationHeader,
      );
      String? currentWorkspace() => workspace.value;
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<bool>(
            valueListenable: transport,
            builder: (_, next, _) => Scaffold(
              body: SettingsPage(
                key: const ValueKey('same-settings-community'),
                auth: auth,
                city: city,
                moments: moments,
                client: next ? nextClient : client,
                apiBaseUrl: next ? 'http://source-b' : 'http://source-a',
                workspaceChanges: workspace,
                organizationWorkspaceID: currentWorkspace,
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.scrollUntilVisible(
        find.text('我的社群兴趣'),
        300,
        scrollable: find.byType(Scrollable).first,
      );
      await t.pumpAndSettle();
      expect(find.text('我的社群兴趣').hitTestable(), findsOneWidget);
      await t.tap(find.text('我的社群兴趣'));
      await t.pumpAndSettle();
      expect(find.byType(PersonCommunityInterestPage), findsOneWidget);
      await interestTap(t, '合成公开社群');
      await interestTap(t, '检查兴趣声明');
      expect(find.text('检查这次具体声明'), findsOneWidget);
      expect(find.text('仅自己可见'), findsWidgets);
      expect(requests.where((r) => r.url.path.endsWith('/preview')).length, 1);
      if (mode == 'cancel') {
        await interestTap(t, '取消');
        await t.binding.handlePopRoute();
        await t.pumpAndSettle();
        expect(find.text('我的社群兴趣'), findsOneWidget);
      } else {
        if (mode == 'transport-aba') {
          transport.value = true;
        } else {
          workspace.value = '55555555-5555-4555-8555-555555555555';
        }
        await t.pumpAndSettle();
        expect(find.byType(AlertDialog), findsNothing);
        expect(find.byType(PersonCommunityInterestPage), findsNothing);
        expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
        transport.value = false;
        workspace.value = null;
        await t.pumpAndSettle();
        expect(find.text('合成公开社群'), findsNothing);
        expect(find.text('批准这个版本'), findsNothing);
        expect(find.byType(PersonCommunityInterestPage), findsNothing);
      }
      expect(requests.where((r) => r.url.path.endsWith('/approve')), isEmpty);
      expect(
        nextRequests.where((r) => r.url.path.contains('community-interests')),
        isEmpty,
      );
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      moments.dispose();
      city.dispose();
      auth.dispose();
      transport.dispose();
      workspace.dispose();
      client.close();
      nextClient.close();
    });
  }
}
