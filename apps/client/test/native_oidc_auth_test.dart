import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/native_oidc.dart';
import 'package:birdtie_client/src/auth/oidc_pending_store.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class FakeNativeOidcGateway implements NativeOidcGateway {
  final controller = StreamController<Uri>.broadcast(sync: true);
  Uri? opened;

  @override
  Stream<Uri> get links => controller.stream;

  @override
  Future<bool> open(Uri authorizationUrl) async {
    opened = authorizationUrl;
    return true;
  }
}

void main() {
  test(
    'native callback uses pending PKCE, verifies /me and stores session',
    () async {
      final gateway = FakeNativeOidcGateway();
      final pending = MemoryOidcPendingStore();
      final vault = MemorySessionVault();
      var exchanges = 0;
      var validSession = true;
      var pendingValueVerifier = '';
      final client = MockClient((request) async {
        switch ('${request.method} ${request.url.path}') {
          case 'GET /v1/auth/dev-phone/status':
            return http.Response('{"data":{"enabled":false}}', 200);
          case 'GET /v1/auth/oidc/status':
            return http.Response('{"data":{"configured":true}}', 200);
          case 'POST /v1/auth/oidc/exchange':
            exchanges++;
            expect(jsonDecode(request.body)['code'], 'one-time-code');
            expect(jsonDecode(request.body)['verifier'], pendingValueVerifier);
            return http.Response(
              '{"data":{"accessToken":"verified-session"}}',
              200,
            );
          case 'GET /v1/me':
            expect(request.headers['Authorization'], 'Bearer verified-session');
            return validSession
                ? http.Response('{"data":{"id":"person-1"}}', 200)
                : http.Response('{"error":{"code":"unauthorized"}}', 401);
        case 'GET /v1/accounts/person-1/profile':
          return http.Response.bytes(
            utf8.encode('{"data":{"displayName":"已验证用户"}}'),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
          case 'POST /v1/session/logout':
            return http.Response('', 204);
          default:
            return http.Response('', 404);
        }
      });
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'https://api.birdtie.example',
        sessionVault: vault,
        oidcPendingStore: pending,
        nativeGateway: gateway,
      );
      await auth.initialize();
      expect(auth.available, isTrue);
      await auth.signIn();
      final challenge = pending.pending!.challenge;
      pendingValueVerifier = pending.pending!.verifier;
      expect(gateway.opened!.path, '/v1/auth/oidc/start');
      expect(gateway.opened!.queryParameters['challenge'], challenge);

      await auth.resumeNativeCallback(
        Uri.parse(
          'birdtie-auth://callback?code=one-time-code&challenge=$challenge',
        ),
      );
      expect(auth.signedIn, isTrue);
      expect(auth.loginMethod, 'oidc');
      expect(auth.displayName, '已验证用户');
      expect(vault.session?.token, 'verified-session');
      expect(pending.pending, isNull);
      await auth.resumeNativeCallback(
        Uri.parse(
          'birdtie-auth://callback?code=one-time-code&challenge=$challenge',
        ),
      );
      expect(exchanges, 1);

      auth.dispose();
      validSession = false;
      final expired = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'https://api.birdtie.example',
        sessionVault: vault,
        oidcPendingStore: pending,
        nativeGateway: gateway,
      );
      await expired.initialize();
      expect(expired.signedIn, isFalse);
      expect(vault.session, isNull);
      expired.dispose();
      await gateway.controller.close();
    },
  );

  test('mismatched callback cannot exchange', () async {
    final gateway = FakeNativeOidcGateway();
    final pending = MemoryOidcPendingStore();
    var exchanges = 0;
    final client = MockClient((request) async {
      if (request.url.path == '/v1/auth/oidc/exchange') exchanges++;
      return http.Response('{"data":{"configured":true}}', 200);
    });
    final auth = BirdtieAuthController(
      client: client,
      apiBaseUrl: 'https://api.birdtie.example',
      sessionVault: MemorySessionVault(),
      oidcPendingStore: pending,
      nativeGateway: gateway,
    );
    await auth.initialize();
    await auth.signIn();
    await auth.resumeNativeCallback(
      Uri.parse(
        'birdtie-auth://other?code=stolen&challenge=${pending.pending!.challenge}',
      ),
    );
    expect(pending.pending, isNotNull);
    await auth.resumeNativeCallback(
      Uri.parse('birdtie-auth://callback?code=stolen&challenge=wrong'),
    );
    expect(auth.signedIn, isFalse);
    expect(exchanges, 0);
    expect(pending.pending, isNull);
    auth.dispose();
    await gateway.controller.close();
  });
}
