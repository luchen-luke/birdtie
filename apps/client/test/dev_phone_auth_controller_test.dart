import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
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
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'http://127.0.0.1:8080',
      );
      await auth.initialize();
      expect(auth.devPhoneAvailable, isTrue);
      expect(await auth.requestDevPhoneCode(' 13800138000 '), isTrue);
      expect(await auth.verifyDevPhoneCode('13800138000', '123456'), isTrue);
      expect(auth.displayName, 'Birdtie tester');
      expect(auth.loginMethod, 'dev_phone');
      expect(auth.authorizationHeader, 'Bearer test-session');
      await auth.signOut();
      expect(auth.signedIn, isFalse);
      expect(auth.authorizationHeader, isNull);
      expect(calls, [
        'GET /v1/auth/dev-phone/status',
        'POST /v1/auth/dev-phone/code',
        'POST /v1/auth/dev-phone/verify',
        'GET /v1/me',
        'GET /v1/accounts/account-1/profile',
        'POST /v1/session/logout',
      ]);
      auth.dispose();
    },
  );
}
