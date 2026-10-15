import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/birdtie_login_sheet.dart';
import 'package:birdtie_client/src/auth/dev_phone_login_sheet.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _CountingVault extends MemorySessionVault {
  int reads = 0;
  @override
  Future<StoredSession?> read() {
    ++reads;
    return super.read();
  }
}

void main() {
  testWidgets(
    'Settings recovers failed configuration through the existing login sheet without automatic authentication',
    (tester) async {
      FlutterSecureStorage.setMockInitialValues({});
      final calls = <http.Request>[];
      var offline = true;
      final client = MockClient((request) async {
        calls.add(request);
        if (offline) throw TimeoutException('isolated unavailable fixture');
        return http.Response(
          jsonEncode({
            'data': {
              'enabled': request.url.path == '/v1/auth/dev-phone/status',
            },
          }),
          200,
        );
      });
      final vault = _CountingVault();
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'https://settings-recovery.test',
        sessionVault: vault,
      );
      await auth.initialize();
      expect(auth.configurationChecked, isTrue);
      expect(auth.available || auth.devPhoneAvailable, isFalse);
      final city = PublicCityController();
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
      );
      final workspace = ValueNotifier<String?>(null);
      final navigator = GlobalKey<NavigatorState>();
      await tester.pumpWidget(
        MaterialApp(
          navigatorKey: navigator,
          home: Scaffold(
            body: SettingsPage(
              auth: auth,
              city: city,
              moments: moments,
              client: client,
              apiBaseUrl: 'https://settings-recovery.test',
              workspaceChanges: workspace,
              organizationWorkspaceID: () => workspace.value,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('登录服务暂不可用，点此重新检查'), findsOneWidget);
      final loginEntry = tester.widget<ListTile>(
        find.widgetWithText(ListTile, '登录 Birdtie'),
      );
      expect(loginEntry.onTap, isNotNull);
      final initialReads = calls.length;
      await tester.tap(find.text('登录 Birdtie'));
      await tester.pumpAndSettle();
      expect(find.byType(BirdtieLoginSheet), findsOneWidget);
      expect(find.text('重新检查'), findsOneWidget);
      expect(calls.length, initialReads);

      offline = false;
      await tester.tap(find.text('重新检查'));
      await tester.pumpAndSettle();
      expect(auth.devPhoneAvailable, isTrue);
      expect(find.text('手机号测试登录'), findsOneWidget);
      expect(find.text('重新检查'), findsNothing);
      expect(auth.signedIn, isFalse);
      expect(vault.reads, 1);
      await tester.tap(find.text('返回'));
      await tester.pumpAndSettle();
      expect(find.byType(BirdtieLoginSheet), findsNothing);
      expect(find.text('登录服务暂不可用，点此重新检查'), findsNothing);

      // The recovered Settings entry reaches the existing fixed-code form.
      // Entering or leaving that form never requests or verifies a code.
      await tester.tap(find.text('登录 Birdtie'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('手机号测试登录'));
      await tester.pumpAndSettle();
      expect(find.byType(DevPhoneLoginSheet), findsOneWidget);
      expect(find.text('获取测试验证码'), findsOneWidget);
      expect(find.text('手机号'), findsOneWidget);
      navigator.currentState!.pop();
      await tester.pumpAndSettle();
      expect(find.byType(DevPhoneLoginSheet), findsNothing);
      expect(auth.signedIn, isFalse);

      // An entry cannot revive after a workspace A -> B -> A transition.
      await tester.tap(find.text('登录 Birdtie'));
      await tester.pumpAndSettle();
      workspace.value = 'synthetic-organization';
      await tester.pump();
      workspace.value = null;
      await tester.pumpAndSettle();
      expect(find.text('账户或入口已变化，请返回后重新打开。'), findsOneWidget);
      expect(find.text('手机号测试登录'), findsNothing);
      await tester.tap(find.text('返回'));
      await tester.pumpAndSettle();

      // A retained tap callback cannot open authentication from a covered route.
      unawaited(
        navigator.currentState!.push<void>(
          MaterialPageRoute(builder: (_) => const Scaffold(body: Text('其他页面'))),
        ),
      );
      await tester.pumpAndSettle();
      loginEntry.onTap!();
      await tester.pumpAndSettle();
      expect(find.byType(BirdtieLoginSheet), findsNothing);
      navigator.currentState!.pop();
      await tester.pumpAndSettle();
      expect(calls.every((r) => r.method == 'GET'), isTrue);
      expect(calls.every((r) => r.url.path.endsWith('/status')), isTrue);
      expect(
        calls.every((r) => !r.headers.containsKey('Authorization')),
        isTrue,
      );
      expect(auth.signedIn, isFalse);
      expect(auth.authorizationHeader, isNull);
      expect(vault.reads, 1);
      expect(tester.takeException(), isNull);

      await tester.pumpWidget(const SizedBox());
      workspace.dispose();
      city.dispose();
      moments.dispose();
      auth.dispose();
      client.close();
    },
  );
}
