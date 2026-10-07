import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'agent_seed_controller_test.dart' show seedResponse;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'profile_completion_sheet_test.dart' show completeSeed;
import 'social_now_controller_test.dart';

void main() {
  testWidgets('Now与设置有同一近况入口，保留完善/社交偏好；不先索要私密设置', (tester) async {
    final auth = SeedTestAuth()..owner = nowOwner,
        city = PublicCityController();
    final calls = <String>[];
    final client = MockClient((r) async {
      calls.add(r.url.path);
      if (r.url.path == '/v1/me/agent-seed') {
        return seedResponse(completeSeed());
      }
      return nowEmpty(r);
    });
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'http://local',
    );
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
    expect(find.text('完善我的选择'), findsOneWidget);
    await tester.tap(find.text('我的社交近况'));
    await tester.pumpAndSettle();
    expect(find.text('好友明确分享的意图'), findsOneWidget);
    await tester.pageBack();
    await tester.pumpAndSettle();
    await tester.pumpWidget(
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
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.text('我的社交近况'),
      200,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.ensureVisible(find.text('我的社交近况'));
    await tester.pumpAndSettle();
    expect(find.text('我的社交近况').hitTestable(), findsOneWidget);
    await tester.tap(find.text('我的社交近况'));
    await tester.pumpAndSettle();
    expect(find.text('好友明确分享的意图'), findsOneWidget);
    expect(calls.where((p) => p == '/v1/me/ties').length, 2);
    expect(calls, isNot(contains('/v1/me/agent-relationship-context')));
    await tester.pageBack();
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.text('社交偏好'),
      -150,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.ensureVisible(find.text('社交偏好'));
    await tester.pumpAndSettle();
    expect(find.text('社交偏好'), findsOneWidget);
    expect(find.text('社交偏好').hitTestable(), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    moments.dispose();
    city.dispose();
    auth.dispose();
  });
}
