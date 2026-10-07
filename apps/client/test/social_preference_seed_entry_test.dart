import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'social_preference_seed_controller_test.dart'
    show socialJson, socialResponse;
import 'social_preference_seed_sheet_test.dart' show SocialTestAuth;

void main() {
  testWidgets('设置直接入口真实读取，重新打开保留服务端偏好，初始设置仍保留', (tester) async {
    final auth = SocialTestAuth(), city = PublicCityController();
    var reads = 0;
    final client = MockClient((r) async {
      if (r.url.path == '/v1/me/agent-private-profile') {
        reads++;
        return socialResponse(socialJson());
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
      find.text('社交偏好'),
      150,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.ensureVisible(find.text('社交偏好'));
    await tester.pumpAndSettle();
    expect(find.text('社交偏好').hitTestable(), findsOneWidget);
    await tester.tap(find.text('社交偏好'));
    await tester.pumpAndSettle();
    expect(find.text('原有明确描述'), findsOneWidget);
    await tester.pageBack();
    await tester.pumpAndSettle();
    await tester.drag(find.byType(ListView), const Offset(0, 1000));
    await tester.pumpAndSettle();
    expect(find.text('我的初始设置'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('社交偏好'),
      150,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.ensureVisible(find.text('社交偏好'));
    await tester.pumpAndSettle();
    expect(find.text('社交偏好').hitTestable(), findsOneWidget);
    await tester.tap(find.text('社交偏好'));
    await tester.pumpAndSettle();
    expect(reads, 2);
    await tester.pumpWidget(const SizedBox());
    moments.dispose();
    city.dispose();
    auth.dispose();
  });
}
