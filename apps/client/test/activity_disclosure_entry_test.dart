import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/activity_participation_disclosure_page.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'model_egress_api_test.dart' show egressOwner, egressAgent;
import 'model_egress_page_test.dart' show egressAuth;

void main() {
  for (final mode in ['return', 'transport-aba', 'workspace-aba']) {
    testWidgets('本人设置报名披露入口 $mode 只读且旧来源永久退出', (t) async {
      final auth = await egressAuth();
      final city = PublicCityController();
      final transport = ValueNotifier<bool>(false);
      final workspace = ValueNotifier<String?>(null);
      final requests = <http.Request>[];
      final nextRequests = <http.Request>[];
      final client = MockClient((r) async {
        requests.add(r);
        if (r.url.path.startsWith(
          '/v1/me/activity-participation-disclosures',
        )) {
          return http.Response(
            jsonEncode({
              'data': {
                'schemaVersion': 'human-activity-participation-disclosure-v1',
                'ownerId': egressOwner,
                'agentId': egressAgent,
                'observedAt': DateTime.now().toUtc().toIso8601String(),
                'records': <dynamic>[],
                'limit': 100,
                'truncated': false,
                'modelAccess': false,
                'sendAllowed': false,
                'membershipGranted': false,
              },
            }),
            200,
          );
        }
        return http.Response('{"data":[]}', 200);
      });
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
                key: const ValueKey('same-settings-participation'),
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
        find.text('我的报名可见范围'),
        300,
        scrollable: find.byType(Scrollable).first,
      );
      await t.ensureVisible(find.text('我的报名可见范围'));
      await t.pumpAndSettle();
      await t.tap(find.text('我的报名可见范围'));
      await t.pumpAndSettle();
      expect(find.byType(ActivityParticipationDisclosurePage), findsOneWidget);
      expect(
        requests.where(
          (r) => r.url.path.contains('activity-participation-disclosures'),
        ),
        isNotEmpty,
      );
      if (mode == 'return') {
        await t.binding.handlePopRoute();
        await t.pumpAndSettle();
        expect(find.text('我的报名可见范围'), findsOneWidget);
      } else {
        if (mode == 'transport-aba') {
          transport.value = true;
        } else {
          workspace.value = '66666666-6666-4666-8666-666666666666';
        }
        await t.pumpAndSettle();
        expect(find.byType(ActivityParticipationDisclosurePage), findsNothing);
        expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
        transport.value = false;
        workspace.value = null;
        await t.pumpAndSettle();
        expect(find.byType(ActivityParticipationDisclosurePage), findsNothing);
      }
      expect(requests.where((r) => r.method != 'GET'), isEmpty);
      expect(
        nextRequests.where(
          (r) => r.url.path.contains('activity-participation-disclosures'),
        ),
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
