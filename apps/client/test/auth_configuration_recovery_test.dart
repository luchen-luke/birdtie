import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/birdtie_login_sheet.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:flutter/material.dart';
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
    'failed login configuration explicitly recovers without restoring or authenticating again',
    (tester) async {
      final calls = <String>[];
      final vault = _CountingVault();
      final entry = ValueNotifier(true);
      var offline = true, phoneEnabled = true;
      Completer<http.Response>? phoneResponse;
      http.Response reply(bool enabled) => http.Response(
        jsonEncode({
          'data': {'enabled': enabled},
        }),
        200,
      );
      final client = MockClient((request) async {
        calls.add('${request.method} ${request.url.path}');
        expectSync(request.method, 'GET');
        expectSync(request.headers.containsKey('Authorization'), isFalse);
        if (offline) throw TimeoutException('isolated unavailable fixture');
        if (request.url.path == '/v1/auth/dev-phone/status') {
          return phoneResponse?.future ?? reply(phoneEnabled);
        }
        if (request.url.path == '/v1/auth/dev-email/status')
          return reply(false);
        return http.Response('{}', 404);
      });
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'https://auth-recovery.test',
        sessionVault: vault,
      );
      final initialization = auth.initialize();
      await initialization;
      expect(vault.reads, 1);
      expect(auth.configurationChecked, isTrue);
      expect(auth.devPhoneAvailable, isFalse);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: BirdtieLoginSheet(
              auth: auth,
              isCurrent: () => entry.value,
              entryChanges: entry,
            ),
          ),
        ),
      );
      expect(find.text('登录服务暂不可用，请稍后再试。'), findsOneWidget);
      expect(find.text('重新检查'), findsOneWidget);

      offline = false;
      phoneResponse = Completer<http.Response>();
      final beforeRetry = calls.length;
      await tester.tap(find.text('重新检查'));
      await tester.pump();
      expect(auth.configurationChecking, isTrue);
      expect(auth.configurationChecked, isFalse);
      expect(find.text('正在检查…'), findsOneWidget);
      expect(
        tester
            .widget<TextButton>(find.widgetWithText(TextButton, '正在检查…'))
            .onPressed,
        isNull,
      );
      final first = auth.refreshLoginConfiguration();
      final second = auth.refreshLoginConfiguration();
      expect(identical(first, second), isTrue);
      expect(calls.length, beforeRetry + 1);
      phoneResponse.complete(reply(true));
      await first;
      await tester.pump();
      expect(auth.configurationChecking, isFalse);
      expect(auth.configurationChecked, isTrue);
      expect(auth.devPhoneAvailable, isTrue);
      expect(find.text('手机号测试登录'), findsOneWidget);
      expect(find.text('重新检查'), findsNothing);
      expect(auth.signedIn, isFalse);
      expect(auth.authorizationHeader, isNull);
      expect(vault.reads, 1);
      expect(identical(initialization, auth.initialize()), isTrue);
      expect(calls.length, beforeRetry + 2);

      // The next explicit read reports unavailable. Retiring the visible
      // entry during its retry cannot reopen it or start an auth action.
      phoneResponse = null;
      phoneEnabled = false;
      await auth.refreshLoginConfiguration();
      await tester.pump();
      expect(find.text('重新检查'), findsOneWidget);
      phoneResponse = Completer<http.Response>();
      await tester.tap(find.text('重新检查'));
      await tester.pump();
      final retiredRead = auth.refreshLoginConfiguration();
      entry.value = false;
      await tester.pump();
      phoneResponse.complete(reply(true));
      await retiredRead;
      await tester.pump();
      expect(find.text('账户或入口已变化，请返回后重新打开。'), findsOneWidget);
      expect(find.text('手机号测试登录'), findsNothing);
      expect(auth.signedIn, isFalse);
      expect(vault.reads, 1);

      // Disposing a controller while the public status GET is pending must
      // neither notify a disposed widget nor dispatch the next status call.
      await tester.pumpWidget(const SizedBox());
      phoneResponse = Completer<http.Response>();
      var notifications = 0;
      auth.addListener(() => notifications++);
      final disposedRead = auth.refreshLoginConfiguration();
      await tester.pump();
      final beforeDispose = calls.length;
      final beforeNotifications = notifications;
      auth.dispose();
      phoneResponse.complete(reply(true));
      await disposedRead;
      await tester.pump();
      expect(calls.length, beforeDispose);
      expect(notifications, beforeNotifications);
      expect(vault.reads, 1);
      expect(calls.every((call) => call.startsWith('GET /v1/auth/')), isTrue);
      expect(tester.takeException(), isNull);
      entry.dispose();
      client.close();
    },
  );
}
