import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'agent_seed_controller_test.dart' show seedResponse;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'profile_completion_sheet_test.dart' show completeSeed;
import 'social_now_controller_test.dart';
import 'social_preference_seed_controller_test.dart'
    show socialJson, socialResponse;

void main() {
  testWidgets('Now005首页移走近况和完善，设置保留真实近况与偏好路径且不自动推断关系', (tester) async {
    final auth = SeedTestAuth()..owner = nowOwner,
        city = PublicCityController();
    final calls = <String>[];
    final client = MockClient((r) async {
      calls.add(r.url.path);
      if (r.url.path == '/v1/me/agent-seed') {
        return seedResponse(completeSeed()..['ownerId'] = nowOwner);
      }
      if (r.url.path == '/v1/me/agent-private-profile') {
        return socialResponse(socialJson(owner: nowOwner));
      }
      return nowEmpty(r);
    });
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'http://local',
    );
    addTearDown(client.close);
    addTearDown(moments.dispose);
    addTearDown(city.dispose);
    addTearDown(auth.dispose);
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
    expect(find.text('完善我的选择'), findsNothing);
    expect(find.text('我的社交近况'), findsNothing);
    expect(calls.where((p) => p == '/v1/me/ties'), isEmpty);
    expect(calls, isNot(contains('/v1/me/agent-relationship-context')));
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
      find.text('完善我的选择'),
      200,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.ensureVisible(find.text('完善我的选择'));
    await tester.tap(find.text('完善我的选择'));
    await tester.pumpAndSettle();
    expect(find.text('这次想完善哪一项？'), findsOneWidget);
    await tester.pageBack();
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
    expect(
      calls.where((p) => p == '/v1/me/ties').length,
      1,
      reason: '首页不隐式读取；唯一设置近况点击只读取一次好友来源',
    );
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
    await tester.tap(find.text('社交偏好'));
    await tester.pumpAndSettle();
    expect(find.text('原有明确描述'), findsOneWidget);
    expect(calls.where((p) => p == '/v1/me/agent-private-profile').length, 1);
    expect(calls, isNot(contains('/v1/me/agent-relationship-context')));
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('个人资料保留近况回调，打开资料不自动调用近况或推断关系', (tester) async {
    final auth = SeedTestAuth()..owner = nowOwner;
    final city = PublicCityController();
    final calls = <String>[];
    final client = MockClient((r) async {
      calls.add(r.url.path);
      return nowEmpty(r);
    });
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'http://local',
    );
    addTearDown(client.close);
    addTearDown(moments.dispose);
    addTearDown(city.dispose);
    addTearDown(auth.dispose);
    var opened = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: LegacyProfilePage(
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: 'http://local',
            onOpenSocialNow: () => opened++,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(opened, 0);
    await tester.ensureVisible(find.text('我的社交近况'));
    expect(find.text('我的社交近况').hitTestable(), findsOneWidget);
    await tester.tap(find.text('我的社交近况'));
    await tester.pumpAndSettle();
    expect(opened, 1);
    expect(calls.where((p) => p == '/v1/me/ties'), isEmpty);
    expect(calls, isNot(contains('/v1/me/agent-relationship-context')));
    await tester.pumpWidget(const SizedBox());
  });
}
