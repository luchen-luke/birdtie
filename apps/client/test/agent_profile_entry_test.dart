import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_profile_api_test.dart';
import 'package:birdtie_client/src/workspace/agent_profile_page.dart';

void main() {
  for (final mode in ['workspace', 'transport']) {
    testWidgets('真实Settings入口相同身份来源与$mode ABA退休', (t) async {
      final auth = SeedTestAuth()..owner = profileOwner;
      final workspace = ValueNotifier<String?>(null),
          swapped = ValueNotifier<bool>(false);
      final requests = <http.Request>[];
      http.Response respond(http.Request r) {
        requests.add(r);
        return profileResponse(profileData(r.url.path));
      }

      final client = ProfileTrackedClient((r) async => respond(r));
      final other = ProfileTrackedClient((r) async => respond(r));
      final city = PublicCityController();
      final moments = PrivateMomentController(
        client: client,
        apiBaseUrl: 'http://local',
        authorizationHeader: () => auth.authorizationHeader,
      );
      addTearDown(auth.dispose);
      addTearDown(workspace.dispose);
      addTearDown(swapped.dispose);
      addTearDown(city.dispose);
      addTearDown(moments.dispose);
      addTearDown(client.close);
      addTearDown(other.close);
      String? org() => workspace.value;
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<bool>(
            valueListenable: swapped,
            builder: (_, b, _) => Scaffold(
              body: SettingsPage(
                key: const ValueKey('same-settings'),
                auth: auth,
                city: city,
                moments: moments,
                client: b ? other : client,
                apiBaseUrl: b ? 'http://next' : 'http://local',
                workspaceChanges: workspace,
                organizationWorkspaceID: org,
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.ensureVisible(find.text('我的智能体'));
      await t.tap(find.text('我的智能体'));
      await t.pumpAndSettle();
      expect(find.byType(AgentProfilePage), findsOneWidget);
      expect(find.textContaining('本人填写的合成补充'), findsOneWidget);
      final reads = requests
          .where(
            (r) => {
              '/v1/me/agent-private-profile',
              '/v1/me/agent-memories',
              '/v1/me/agent-policies',
              '/v1/me/community-interests',
              '/v1/me/activity-participation-disclosures',
            }.contains(r.url.path),
          )
          .toList();
      expect(reads.length, 5);
      expect(
        reads.every(
          (r) =>
              r.method == 'GET' &&
              r.body.isEmpty &&
              r.headers['Authorization'] == auth.authorizationHeader &&
              r.url.host == 'local',
        ),
        true,
      );
      if (mode == 'workspace') {
        workspace.value = profileAgent;
        await t.pump();
        workspace.value = null;
      } else {
        swapped.value = true;
        await t.pump();
        swapped.value = false;
      }
      await t.pumpAndSettle();
      expect(find.textContaining('本人填写的合成补充'), findsNothing);
      expect(find.textContaining('工作身份或来源已变化'), findsOneWidget);
      expect(requests.every((r) => r.method == 'GET'), true);
      await t.pumpWidget(const SizedBox());
      expect(client.closeCount, 0);
      expect(other.closeCount, 0);
    });
  }
  testWidgets('个人设置有可达的我的智能体入口', (t) async {
    final auth = SeedTestAuth()..owner = '82000000-0000-4000-8000-000000000001';
    final client = MockClient((r) async => http.Response('{"data":[]}', 200));
    final city = PublicCityController();
    final moments = PrivateMomentController(
      client: client,
      apiBaseUrl: 'http://local',
      authorizationHeader: () => auth.authorizationHeader,
    );
    addTearDown(auth.dispose);
    addTearDown(city.dispose);
    addTearDown(moments.dispose);
    addTearDown(client.close);
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
    await t.pumpAndSettle();
    expect(find.text('我的智能体'), findsOneWidget);
  });
}
