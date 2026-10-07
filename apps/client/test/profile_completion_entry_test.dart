import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_controller_test.dart' show seedResponse;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'profile_completion_sheet_test.dart' show completeSeed, completionTap;

void main() {
  testWidgets('Now与Settings复用同一真实GET/组件；不重复强制初始设置', (tester) async {
    final auth = SeedTestAuth();
    final city = PublicCityController();
    var seedReads = 0;
    final client = MockClient((request) async {
      if (request.url.path == '/v1/me/agent-seed') {
        seedReads++;
        return seedResponse(completeSeed());
      }
      return http.Response(
        '{"data":[]}',
        200,
        headers: {'content-type': 'application/json'},
      );
    });
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'http://local',
    );
    addTearDown(auth.dispose);
    addTearDown(city.dispose);
    addTearDown(moments.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          moments: moments,
          auth: auth,
          seedClient: client,
          seedApiBaseUrl: 'http://local',
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('初始设置'), findsNothing);
    await completionTap(tester, '完善我的选择');
    expect(find.text('这次想完善哪一项？'), findsOneWidget);
    await tester.pageBack();
    await tester.pumpAndSettle();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            city: city,
            moments: moments,
            auth: auth,
            client: client,
            apiBaseUrl: 'http://local',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await completionTap(tester, '完善我的选择');
    expect(find.text('这次想完善哪一项？'), findsOneWidget);
    expect(seedReads, 3);
    await tester.pageBack();
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('社交偏好'), 150);
    expect(find.text('社交偏好'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    await tester.pump();
  });
}
