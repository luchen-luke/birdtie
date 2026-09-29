import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('Settings manages blocks and profile grants', (tester) async {
    final authClient = MockClient((request) async {
      switch ('${request.method} ${request.url.path}') {
        case 'GET /v1/auth/dev-phone/status':
          return http.Response('{"data":{"enabled":true}}', 200);
        case 'POST /v1/auth/dev-phone/code':
          return http.Response('{"data":{"expiresInSeconds":300}}', 200);
        case 'POST /v1/auth/dev-phone/verify':
          return http.Response('{"data":{"accessToken":"test-session"}}', 200);
        case 'GET /v1/me':
          return http.Response('{"data":{"id":"account-1"}}', 200);
        case 'GET /v1/accounts/account-1/profile':
          return http.Response(
            '{"data":{"displayName":"Birdtie tester"}}',
            200,
          );
        default:
          return http.Response('', 404);
      }
    });
    final auth = BirdtieAuthController(
      client: authClient,
      apiBaseUrl: 'http://localhost:8080',
    );
    await auth.initialize();
    await auth.requestDevPhoneCode('13800138000');
    await auth.verifyDevPhoneCode('13800138000', '123456');
    var blocked = true;
    var granted = true;
    final settingsClient = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer test-session');
      switch ('${request.method} ${request.url.path}') {
        case 'GET /v1/me/blocks':
          return http.Response(
            blocked ? '{"data":[{"accountId":"blocked-1"}]}' : '{"data":[]}',
            200,
          );
        case 'GET /v1/me/consents':
          return http.Response(
            granted
                ? '{"data":[{"id":"grant-1","recipientAccountId":"recipient-1","expiresAt":"2099-01-01T00:00:00Z"}]}'
                : '{"data":[]}',
            200,
          );
        case 'DELETE /v1/me/blocks/blocked-1':
          blocked = false;
          return http.Response('', 204);
        case 'DELETE /v1/me/consents/grant-1':
          granted = false;
          return http.Response('', 204);
        default:
          return http.Response('', 404);
      }
    });
    final city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: settingsClient,
            apiBaseUrl: 'http://localhost:8080',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('blocked-1'), findsOneWidget);
    expect(find.text('recipient-1'), findsOneWidget);
    await tester.tap(find.text('Unblock'));
    await tester.pumpAndSettle();
    expect(find.text('No blocked accounts.'), findsOneWidget);
    await tester.tap(find.text('Revoke'));
    await tester.pumpAndSettle();
    expect(find.text('No active profile access grants.'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    city.dispose();
    moments.dispose();
    settingsClient.close();
  });
}
