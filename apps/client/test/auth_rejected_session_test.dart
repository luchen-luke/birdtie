import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

http.Response _wire(Object data) => http.Response(
  jsonEncode({'data': data}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
void main() {
  test(
    'real-session 401 retires synchronously and stale 401 cannot sign out new login',
    () async {
      final vault = MemorySessionVault();
      var token = 'first-session', owner = 'first-owner';
      final calls = <String>[];
      final client = MockClient((r) async {
        calls.add(r.url.path);
        return switch (r.url.path) {
          '/v1/auth/dev-phone/status' => _wire({'enabled': true}),
          '/v1/auth/dev-phone/verify' => _wire({'accessToken': token}),
          '/v1/me' => _wire({'id': owner}),
          _ when r.url.path.endsWith('/profile') => _wire({
            'displayName': '合成本人',
          }),
          _ => http.Response('', 404),
        };
      });
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: 'https://unit.invalid',
        sessionVault: vault,
      );
      addTearDown(auth.dispose);
      await auth.initialize();
      expect(await auth.verifyDevPhoneCode('13800138000', '123456'), isTrue);
      final rejecting = auth.invalidateRejectedSession('Bearer first-session');
      expect(auth.signedIn, isFalse);
      expect(auth.accountID, isNull);
      expect(auth.rejectedSessionOwnerID, 'first-owner');
      expect(await rejecting, isTrue);
      expect(vault.session, isNull);
      token = 'new-session';
      owner = 'second-owner';
      expect(await auth.verifyDevPhoneCode('13800138000', '123456'), isTrue);
      final before = calls.length;
      expect(
        await auth.invalidateRejectedSession('Bearer first-session'),
        isFalse,
      );
      expect(auth.authorizationHeader, 'Bearer new-session');
      expect(auth.accountID, 'second-owner');
      expect(vault.session?.token, 'new-session');
      expect(calls.length, before);
      expect(auth.rejectedSessionOwnerID, isNull);
    },
  );
  test(
    'repeated rejected-session callers cannot duplicate retirement notifications',
    () async {
      final vault = MemorySessionVault()
        ..session = const StoredSession(
          origin: 'https://unit.invalid',
          token: 'saved',
          method: 'dev_phone',
        );
      final auth = BirdtieAuthController(
        apiBaseUrl: 'https://unit.invalid',
        sessionVault: vault,
        client: MockClient(
          (r) async => r.url.path == '/v1/me'
              ? _wire({'id': 'owner'})
              : r.url.path.endsWith('/profile')
              ? _wire({'displayName': '合成本人'})
              : _wire({'enabled': true}),
        ),
      );
      addTearDown(auth.dispose);
      await auth.initialize();
      var notifications = 0;
      auth.addListener(() => notifications++);
      final results = await Future.wait([
        auth.invalidateRejectedSession('Bearer saved'),
        auth.invalidateRejectedSession('Bearer saved'),
      ]);
      expect(results, [true, false]);
      expect(notifications, 1);
      expect(vault.session, isNull);
    },
  );
  test(
    'signOut after rejected identity synchronously cancels recovery without null-token logout',
    () async {
      final vault = MemorySessionVault()
        ..session = const StoredSession(
          origin: 'https://unit.invalid',
          token: 'expired-session',
          method: 'dev_phone',
        );
      final requests = <http.Request>[];
      final auth = BirdtieAuthController(
        apiBaseUrl: 'https://unit.invalid',
        sessionVault: vault,
        client: MockClient((request) async {
          requests.add(request);
          return request.url.path == '/v1/me'
              ? _wire({'id': 'original-owner'})
              : request.url.path.endsWith('/profile')
              ? _wire({'displayName': '原本人'})
              : _wire({'enabled': true});
        }),
      );
      addTearDown(auth.dispose);
      await auth.initialize();
      await auth.invalidateRejectedSession('Bearer expired-session');
      expect(auth.rejectedSessionOwnerID, 'original-owner');
      expect(auth.authorizationHeader, isNull);
      final states = <(String?, bool, String?)>[];
      auth.addListener(
        () => states.add((
          auth.rejectedSessionOwnerID,
          auth.busy,
          auth.authorizationHeader,
        )),
      );
      final logout = auth.signOut();
      // Assert at the gesture, before awaiting signOut's future.
      expect(auth.rejectedSessionOwnerID, isNull);
      expect(auth.busy, isFalse);
      expect(states, [(null, false, null)]);
      expect(auth.signedIn, isFalse);
      expect(auth.accountID, isNull);
      expect(auth.displayName, isNull);
      expect(auth.loginMethod, isNull);
      await logout;
      await auth.signOut();
      expect(states, hasLength(1));
      expect(vault.session, isNull);
      expect(
        requests.where((r) => r.url.path == '/v1/session/logout'),
        isEmpty,
      );
      expect(
        requests.where((r) => r.headers['Authorization'] == 'Bearer null'),
        isEmpty,
      );
    },
  );
  for (final stage in ['verify', 'identity']) {
    test(
      'explicit logout while expired retires pending $stage login reply without restoring identity',
      () async {
        final vault = MemorySessionVault()
          ..session = const StoredSession(
            origin: 'https://unit.invalid',
            token: 'expired-session',
            method: 'dev_phone',
          );
        final waiting = Completer<http.Response>(), started = Completer<void>();
        final requests = <http.Request>[];
        var recovering = false;
        final auth = BirdtieAuthController(
          apiBaseUrl: 'https://unit.invalid',
          sessionVault: vault,
          client: MockClient((request) async {
            requests.add(request);
            if (request.url.path == '/v1/session/logout') {
              return http.Response('', 204);
            }
            if (recovering &&
                ((stage == 'verify' &&
                        request.url.path == '/v1/auth/dev-phone/verify') ||
                    (stage == 'identity' && request.url.path == '/v1/me'))) {
              started.complete();
              return waiting.future;
            }
            return switch (request.url.path) {
              '/v1/auth/dev-phone/status' => _wire({'enabled': true}),
              '/v1/auth/dev-phone/verify' => _wire({
                'accessToken': 'late-issued-session',
              }),
              '/v1/me' => _wire({'id': 'original-owner'}),
              _ when request.url.path.endsWith('/profile') => _wire({
                'displayName': '原本人',
              }),
              _ => http.Response('', 404),
            };
          }),
        );
        addTearDown(auth.dispose);
        await auth.initialize();
        await auth.invalidateRejectedSession('Bearer expired-session');
        recovering = true;
        final verifying = auth.verifyDevPhoneCode('13800138000', '123456');
        await started.future;
        expect(auth.busy, isTrue);
        expect(auth.rejectedSessionOwnerID, 'original-owner');
        expect(auth.authorizationHeader, isNull);
        final visible = <(bool, String?, bool)>[];
        auth.addListener(
          () => visible.add((
            auth.signedIn,
            auth.rejectedSessionOwnerID,
            auth.busy,
          )),
        );
        final logout = auth.signOut();
        expect(auth.busy, isFalse);
        expect(auth.rejectedSessionOwnerID, isNull);
        expect(visible, [(false, null, false)]);
        expect(auth.authorizationHeader, isNull);
        expect(auth.accountID, isNull);
        expect(auth.displayName, isNull);
        expect(auth.loginMethod, isNull);
        await logout;
        expect(
          requests.where((r) => r.url.path == '/v1/session/logout'),
          isEmpty,
        );
        waiting.complete(
          stage == 'verify'
              ? _wire({'accessToken': 'late-issued-session'})
              : _wire({'id': 'original-owner'}),
        );
        expect(await verifying, isFalse);
        expect(auth.signedIn, isFalse);
        expect(auth.authorizationHeader, isNull);
        expect(auth.accountID, isNull);
        expect(auth.displayName, isNull);
        expect(auth.loginMethod, isNull);
        expect(auth.rejectedSessionOwnerID, isNull);
        expect(auth.busy, isFalse);
        expect(vault.session, isNull);
        expect(visible.every((state) => !state.$1 && state.$2 == null), isTrue);
        final revocations = requests
            .where((r) => r.url.path == '/v1/session/logout')
            .toList();
        expect(
          revocations,
          hasLength(1),
          reason: 'only the late real issued credential is revoked',
        );
        expect(
          revocations.single.headers['Authorization'],
          'Bearer late-issued-session',
        );
        expect(
          requests.where((r) => r.headers['Authorization'] == 'Bearer null'),
          isEmpty,
        );
        expect(
          requests.where((r) => r.url.path.endsWith('/profile')),
          hasLength(1),
          reason:
              'the retired recovery never reads/publishes a new private profile',
        );
      },
    );
  }
  for (final logoutStatus in [204, 503]) {
    test(
      'signOut immediately clears visible identity while remote logout is pending; status=$logoutStatus',
      () async {
        final vault = MemorySessionVault()
          ..session = const StoredSession(
            origin: 'https://unit.invalid',
            token: 'saved',
            method: 'dev_phone',
          );
        final logoutStarted = Completer<void>(),
            logoutResponse = Completer<http.Response>();
        var logoutRequests = 0;
        final client = MockClient((request) async {
          if (request.url.path == '/v1/session/logout') {
            logoutRequests++;
            expect(request.headers['Authorization'], 'Bearer saved');
            logoutStarted.complete();
            return logoutResponse.future;
          }
          return request.url.path == '/v1/me'
              ? _wire({'id': 'owner'})
              : request.url.path.endsWith('/profile')
              ? _wire({'displayName': '已登录姓名'})
              : _wire({'enabled': true});
        });
        final auth = BirdtieAuthController(
          apiBaseUrl: 'https://unit.invalid',
          sessionVault: vault,
          client: client,
        );
        addTearDown(auth.dispose);
        await auth.initialize();
        expect(auth.accountID, 'owner');
        expect(auth.displayName, '已登录姓名');
        final visibleStates = <(bool, String?, String?)>[];
        auth.addListener(
          () => visibleStates.add((
            auth.signedIn,
            auth.accountID,
            auth.displayName,
          )),
        );
        final logout = auth.signOut();
        // These assertions are before the first asynchronous logout response.
        expect(auth.signedIn, isFalse);
        expect(auth.authorizationHeader, isNull);
        expect(auth.accountID, isNull);
        expect(auth.displayName, isNull);
        expect(auth.loginMethod, isNull);
        expect(auth.rejectedSessionOwnerID, isNull);
        expect(visibleStates.last, (false, null, null));
        await logoutStarted.future;
        expect(logoutResponse.isCompleted, isFalse);
        expect(vault.session, isNull);
        await auth.signOut();
        expect(
          logoutRequests,
          1,
          reason: 'repeat logout cannot replay remote revocation',
        );
        logoutResponse.complete(http.Response('', logoutStatus));
        await logout;
        expect(auth.signedIn, isFalse);
        expect(auth.accountID, isNull);
        expect(auth.displayName, isNull);
        expect(auth.busy, isFalse);
        expect(auth.error, logoutStatus == 204 ? isNull : isNotNull);
      },
    );
  }
}
