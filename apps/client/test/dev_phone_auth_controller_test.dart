import 'dart:convert';
import 'dart:async';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test(
    'local development phone challenge creates and revokes a session',
    () async {
      final calls = <String>[];
      final client = MockClient((request) async {
        calls.add('${request.method} ${request.url.path}');
        switch ('${request.method} ${request.url.path}') {
          case 'GET /v1/auth/dev-phone/status':
            return http.Response('{"data":{"enabled":true}}', 200);
          case 'GET /v1/auth/dev-email/status':
            return http.Response('{"data":{"enabled":false}}', 200);
          case 'POST /v1/auth/dev-phone/code':
            expect(jsonDecode(request.body)['phone'], '13800138000');
            return http.Response('{"data":{"expiresInSeconds":300}}', 200);
          case 'POST /v1/auth/dev-phone/verify':
            expect(jsonDecode(request.body), {
              'phone': '13800138000',
              'code': '123456',
            });
            return http.Response(
              '{"data":{"accessToken":"test-session"}}',
              200,
            );
          case 'GET /v1/me':
            expect(request.headers['Authorization'], 'Bearer test-session');
            return http.Response('{"data":{"id":"account-1"}}', 200);
          case 'GET /v1/accounts/account-1/profile':
            expect(request.headers['Authorization'], 'Bearer test-session');
            return http.Response(
              '{"data":{"displayName":"Birdtie tester"}}',
              200,
            );
          case 'POST /v1/session/logout':
            expect(request.headers['Authorization'], 'Bearer test-session');
            return http.Response('', 204);
          default:
            return http.Response('unexpected request', 500);
        }
      });
      final vault = MemorySessionVault();
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'http://127.0.0.1:8080',
        sessionVault: vault,
      );
      await auth.initialize();
      expect(auth.devPhoneAvailable, isTrue);
      expect(await auth.requestDevPhoneCode(' 13800138000 '), isTrue);
      expect(await auth.verifyDevPhoneCode('13800138000', '123456'), isTrue);
      expect(auth.displayName, 'Birdtie tester');
      expect(auth.loginMethod, 'dev_phone');
      expect(auth.authorizationHeader, 'Bearer test-session');
      expect((await vault.read())?.token, 'test-session');
      await auth.signOut();
      expect(auth.signedIn, isFalse);
      expect(auth.authorizationHeader, isNull);
      expect(await vault.read(), isNull);
      expect(calls, [
        'GET /v1/auth/dev-phone/status',
        'GET /v1/auth/dev-email/status',
        'POST /v1/auth/dev-phone/code',
        'POST /v1/auth/dev-phone/verify',
        'GET /v1/me',
        'GET /v1/accounts/account-1/profile',
        'POST /v1/session/logout',
      ]);
      auth.dispose();
    },
  );

  test(
    'saved Session resumes after restart and invalid Session is removed',
    () async {
      final vault = MemorySessionVault()
        ..session = const StoredSession(
          token: 'saved-session',
          method: 'dev_phone',
          origin: 'http://127.0.0.1:8080',
        );
      var valid = true;
      final client = MockClient((request) async {
        switch ('${request.method} ${request.url.path}') {
          case 'GET /v1/me':
            expect(request.headers['Authorization'], 'Bearer saved-session');
            return valid
                ? http.Response('{"data":{"id":"account-1"}}', 200)
                : http.Response('{"error":{"code":"unauthorized"}}', 401);
          case 'GET /v1/accounts/account-1/profile':
            return http.Response.bytes(
              utf8.encode('{"data":{"displayName":"测试用户"}}'),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          case 'GET /v1/auth/dev-phone/status':
            return http.Response('{"data":{"enabled":true}}', 200);
          default:
            return http.Response('', 404);
        }
      });
      final restored = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'http://127.0.0.1:8080',
        sessionVault: vault,
      );
      await restored.initialize();
      expect(restored.signedIn, isTrue);
      expect(restored.displayName, '测试用户');
      restored.dispose();

      valid = false;
      final rejected = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'http://127.0.0.1:8080',
        sessionVault: vault,
      );
      await rejected.initialize();
      expect(rejected.signedIn, isFalse);
      expect(await vault.read(), isNull);
      rejected.dispose();
    },
  );
  test(
    'explicit email fixed-code realm persists and restores its truthful kind',
    () async {
      final vault = MemorySessionVault();
      var emailRequests = 0;
      final client = MockClient((request) async {
        switch ('${request.method} ${request.url.path}') {
          case 'GET /v1/auth/dev-phone/status':
            return http.Response('{"data":{"enabled":true}}', 200);
          case 'GET /v1/auth/dev-email/status':
            return http.Response('{"data":{"enabled":true}}', 200);
          case 'POST /v1/auth/dev-email/code':
            emailRequests++;
            expect(jsonDecode(request.body), {
              'email': 'first@example.invalid',
            });
            return http.Response('{"data":{"expiresInSeconds":300}}', 200);
          case 'POST /v1/auth/dev-email/verify':
            emailRequests++;
            expect(jsonDecode(request.body), {
              'email': 'first@example.invalid',
              'code': '123456',
            });
            return http.Response(
              '{"data":{"accessToken":"email-session","credentialKind":"email"}}',
              200,
            );
          case 'GET /v1/me':
            expect(request.headers['Authorization'], 'Bearer email-session');
            return http.Response('{"data":{"id":"email-owner"}}', 200);
          case 'GET /v1/accounts/email-owner/profile':
            return http.Response(
              '{"data":{"displayName":"Synthetic owner"}}',
              200,
            );
          case 'POST /v1/session/logout':
            return http.Response('', 204);
          default:
            fail('email login crossed credential realm');
        }
      });
      BirdtieAuthController create() => BirdtieAuthController(
        client: client,
        apiBaseUrl: 'http://127.0.0.1:8080',
        sessionVault: vault,
      );
      final auth = create();
      await auth.initialize();
      expect(auth.devEmailAvailable, isTrue);
      expect(await auth.requestDevEmailCode(' first@example.invalid '), isTrue);
      expect(
        await auth.verifyDevEmailCode('first@example.invalid', '123456'),
        isTrue,
      );
      expect(auth.loginMethod, 'dev_email');
      expect(auth.accountID, 'email-owner');
      expect(vault.session?.method, 'dev_email');
      expect(vault.session?.belongsTo('http://127.0.0.1:8080'), isTrue);
      expect(vault.session?.belongsTo('https://foreign.invalid'), isFalse);
      auth.dispose();
      final restored = create();
      await restored.initialize();
      expect(restored.loginMethod, 'dev_email');
      expect(restored.accountID, 'email-owner');
      expect(emailRequests, 2);
      await restored.signOut();
      expect(vault.session, isNull);
      restored.dispose();
    },
  );
  test(
    'late email verify retires issued token without accepting owner or vault',
    () async {
      final response = Completer<http.Response>();
      final issued = Completer<void>();
      final vault = MemorySessionVault();
      var current = true;
      var logouts = 0;
      final client = MockClient((request) async {
        if (request.url.path.endsWith('/status'))
          return http.Response('{"data":{"enabled":true}}', 200);
        if (request.url.path == '/v1/auth/dev-email/verify') {
          issued.complete();
          return response.future;
        }
        if (request.url.path == '/v1/session/logout') {
          expect(request.headers['Authorization'], 'Bearer late-email-session');
          logouts++;
          return http.Response('', 204);
        }
        fail('retired email entry read an owner');
      });
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'http://127.0.0.1:8080',
        sessionVault: vault,
      );
      await auth.initialize();
      final pending = auth.verifyDevEmailCode(
        'first@example.invalid',
        '123456',
        isCurrent: () => current,
      );
      await issued.future;
      current = false;
      response.complete(
        http.Response(
          '{"data":{"accessToken":"late-email-session","credentialKind":"email"}}',
          200,
        ),
      );
      expect(await pending, isFalse);
      expect(auth.signedIn, isFalse);
      expect(auth.accountID, isNull);
      expect(vault.session, isNull);
      expect(logouts, 1);
      auth.dispose();
    },
  );
}
