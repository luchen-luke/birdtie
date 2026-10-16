import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const origin = 'https://session-recovery.invalid';
const owner = '77000000-0000-4000-8000-000000000001';
const oldRefresh = 'btr1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
const newRefresh = 'btr1_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
const saved = StoredSession(
  token: 'synthetic-old-access',
  method: 'dev_phone',
  origin: origin,
  refreshToken: oldRefresh,
  ownerID: owner,
);
http.Response wire(Object value, [int status = 200]) => http.Response(
  jsonEncode({'data': value}),
  status,
  headers: {'content-type': 'application/json'},
);
http.Response rotated() => wire({
  'accessToken': 'synthetic-new-access',
  'refreshToken': newRefresh,
  'accountId': owner,
  'authMethod': 'dev_phone',
  'refreshExpiresAt': '2030-01-01T00:00:00Z',
});

class Fixture {
  Fixture({
    this.refresh,
    this.me,
    this.verify,
    StoredSession? entry = saved,
    MemorySessionVault? sharedVault,
  }) : vault = sharedVault ?? MemorySessionVault() {
    if (sharedVault == null) vault.session = entry;
    auth = BirdtieAuthController(
      apiBaseUrl: origin,
      sessionVault: vault,
      client: MockClient((request) async {
        calls.add(request);
        switch (request.url.path) {
          case '/v1/me':
            if (me != null) return me!(request);
            return request.headers['Authorization'] ==
                    'Bearer synthetic-new-access'
                ? wire({'id': owner})
                : wire({}, 401);
          case '/v1/session/refresh':
            return refresh == null ? rotated() : refresh!(request);
          case '/v1/session/logout':
            return http.Response('', 204);
          case '/v1/auth/dev-phone/verify':
            return verify == null ? rotated() : verify!(request);
          case '/v1/auth/dev-phone/status':
            return wire({'enabled': true});
          default:
            return wire({'displayName': '隔离用户'});
        }
      }),
    );
  }
  final Future<http.Response> Function(http.Request)? refresh;
  final Future<http.Response> Function(http.Request)? me;
  final Future<http.Response> Function(http.Request)? verify;
  final MemorySessionVault vault;
  final calls = <http.Request>[];
  late final BirdtieAuthController auth;
  Iterable<http.Request> get refreshes =>
      calls.where((r) => r.url.path == '/v1/session/refresh');
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'explicit phone login stores returned persistent pair for the verified owner',
    () async {
      final f = Fixture(entry: null);
      addTearDown(f.auth.dispose);
      await f.auth.initialize();
      expect(await f.auth.verifyDevPhoneCode('+19999999001', '123456'), isTrue);
      expect(f.auth.accountID, owner);
      expect(f.vault.session!.refreshToken, newRefresh);
      expect(f.vault.session!.ownerID, owner);
      expect(f.refreshes, isEmpty);
    },
  );
  test(
    'expired access restores original owner with rotated device grant, never fixed code',
    () async {
      final f = Fixture();
      addTearDown(f.auth.dispose);
      await f.auth.initialize();
      expect(f.auth.signedIn, isTrue);
      expect(f.auth.accountID, owner);
      expect(f.refreshes.length, 1);
      expect(f.vault.session!.refreshToken, newRefresh);
      expect(f.vault.session!.pendingRefreshOperationID, isNull);
      expect(f.auth.sessionRecoveryRequired, isFalse);
      expect(
        f.calls.where(
          (r) => r.url.path.endsWith('/verify') || r.url.path.endsWith('/code'),
        ),
        isEmpty,
      );
    },
  );
  test(
    'temporary cold-start network failure retains sealed session and supports retry',
    () async {
      var offline = true;
      final f = Fixture(
        me: (_) async {
          if (offline) throw http.ClientException('isolated offline');
          return wire({'id': owner});
        },
      );
      addTearDown(f.auth.dispose);
      await f.auth.initialize();
      expect(f.auth.signedIn, isFalse);
      expect(f.auth.sessionRecoveryFailed, isTrue);
      expect(f.vault.session, same(saved));
      offline = false;
      expect(await f.auth.recoverSession(), isTrue);
      expect(f.auth.sessionRecoveryFailed, isFalse);
      expect(f.refreshes, isEmpty);
    },
  );
  test(
    'lost refresh response persists operation and retries exactly that rotation',
    () async {
      var requests = 0;
      final f = Fixture(
        refresh: (_) async {
          if (++requests == 1) throw http.ClientException('response lost');
          return rotated();
        },
      );
      addTearDown(f.auth.dispose);
      await f.auth.initialize();
      expect(f.auth.sessionRecoveryFailed, isTrue);
      final pending = f.vault.session!.pendingRefreshOperationID;
      expect(pending, isNotNull);
      expect(await f.auth.recoverSession(), isTrue);
      final bodies = f.refreshes.map((r) => jsonDecode(r.body)).toList();
      expect(bodies[0], bodies[1]);
      expect(bodies[0]['operationId'], pending);
      expect(f.vault.session!.refreshToken, newRefresh);
    },
  );
  test(
    'definitive refresh rejection requires login and does not retain old grant',
    () async {
      final f = Fixture(refresh: (_) async => wire({}, 401));
      addTearDown(f.auth.dispose);
      await f.auth.initialize();
      expect(f.auth.signedIn, isFalse);
      expect(f.auth.sessionRecoveryRequired, isTrue);
      expect(f.auth.sessionRecoveryFailed, isFalse);
      expect(f.auth.rejectedSessionOwnerID, owner);
      expect(f.vault.session, isNull);
    },
  );
  test(
    'legacy expired access has explicit recovery requirement, never automatic login',
    () async {
      final f = Fixture(
        entry: const StoredSession(
          token: 'legacy-expired',
          method: 'dev_phone',
          origin: origin,
        ),
      );
      addTearDown(f.auth.dispose);
      await f.auth.initialize();
      expect(f.auth.sessionRecoveryRequired, isTrue);
      expect(f.refreshes, isEmpty);
      expect(f.vault.session, isNull);
    },
  );
  test(
    'concurrent 401s share refresh and late old credential cannot retire new owner',
    () async {
      final response = Completer<http.Response>();
      final f = Fixture(
        me: (_) async => wire({'id': owner}),
        refresh: (_) => response.future,
      );
      addTearDown(f.auth.dispose);
      await f.auth.initialize();
      final first = f.auth.recoverSession(
        expectedAuthorization: 'Bearer synthetic-old-access',
      );
      final second = f.auth.recoverSession(
        expectedAuthorization: 'Bearer synthetic-old-access',
      );
      expect(identical(first, second), isTrue);
      await Future<void>.delayed(Duration.zero);
      expect(f.auth.signedIn, isFalse);
      expect(f.refreshes.length, 1);
      response.complete(rotated());
      expect(await first, isTrue);
      expect(
        await f.auth.recoverSession(
          expectedAuthorization: 'Bearer synthetic-old-access',
        ),
        isTrue,
      );
      expect(f.auth.authorizationHeader, 'Bearer synthetic-new-access');
      expect(f.refreshes.length, 1);
    },
  );
  test(
    'explicit logout cancels pending rotation and revokes late issued family',
    () async {
      final response = Completer<http.Response>();
      final f = Fixture(refresh: (_) => response.future);
      addTearDown(f.auth.dispose);
      final initialization = f.auth.initialize();
      await Future<void>.delayed(Duration.zero);
      await f.auth.signOut();
      expect(f.vault.session, isNull);
      response.complete(rotated());
      await initialization;
      expect(f.auth.signedIn, isFalse);
      expect(f.vault.session, isNull);
      final logoutBodies = f.calls
          .where((r) => r.url.path.endsWith('/logout'))
          .map((r) => jsonDecode(r.body)['refreshToken'])
          .toList();
      expect(logoutBodies, containsAll([oldRefresh, newRefresh]));
    },
  );
  test(
    'disposed controller cannot revoke rotation adopted by its replacement',
    () async {
      final response = Completer<http.Response>();
      final first = Fixture(refresh: (_) => response.future);
      final initialization = first.auth.initialize();
      await Future<void>.delayed(Duration.zero);
      expect(first.refreshes.length, 1);
      first.auth.dispose();

      final replacement = Fixture(sharedVault: first.vault);
      addTearDown(replacement.auth.dispose);
      await replacement.auth.initialize();
      expect(replacement.auth.accountID, owner);
      expect(replacement.vault.session!.refreshToken, newRefresh);
      expect(
        jsonDecode(replacement.refreshes.single.body),
        jsonDecode(first.refreshes.single.body),
      );
      response.complete(rotated());
      await initialization;
      expect(first.calls.where((r) => r.url.path.endsWith('/logout')), isEmpty);
      expect(replacement.vault.session!.refreshToken, newRefresh);
      expect(
        replacement.auth.authorizationHeader,
        'Bearer synthetic-new-access',
      );
    },
  );
  test(
    'old controller logout preserves a different account saved by replacement',
    () async {
      const otherOwner = '77000000-0000-4000-8000-000000000002';
      final first = Fixture(me: (_) async => wire({'id': owner}));
      addTearDown(first.auth.dispose);
      await first.auth.initialize();
      final replacement = Fixture(
        sharedVault: first.vault,
        me: (request) async => wire({
          'id':
              request.headers['Authorization'] == 'Bearer synthetic-new-access'
              ? otherOwner
              : owner,
        }),
        verify: (_) async => wire({
          'accessToken': 'synthetic-new-access',
          'refreshToken': newRefresh,
          'accountId': otherOwner,
          'authMethod': 'dev_phone',
        }),
      );
      addTearDown(replacement.auth.dispose);
      await replacement.auth.initialize();
      await replacement.auth.signOut();
      expect(
        await replacement.auth.verifyDevPhoneCode('+19999999002', '123456'),
        isTrue,
      );
      final next = replacement.vault.session;
      expect(next!.ownerID, otherOwner);

      await first.auth.signOut();
      expect(replacement.vault.session, same(next));
      expect(replacement.auth.accountID, otherOwner);
      final oldLogout = first.calls.singleWhere(
        (r) => r.url.path.endsWith('/logout'),
      );
      expect(oldLogout.headers['Authorization'], 'Bearer synthetic-old-access');
      expect(jsonDecode(oldLogout.body)['refreshToken'], oldRefresh);
    },
  );
  test(
    'secure origin vault persists v3 pair and retry operation, rejects foreign origin',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      const v = SecureSessionVault(origin: origin);
      final entry = saved.withRefreshOperation(
        '77000000-0000-4000-8000-000000000009',
      );
      await v.write(entry);
      final read = await v.read();
      expect(read!.token, entry.token);
      expect(read.refreshToken, oldRefresh);
      expect(read.pendingRefreshOperationID, entry.pendingRefreshOperationID);
      expect(
        await const SecureSessionVault(
          origin: 'https://another.invalid',
        ).read(),
        isNull,
      );
      await v.delete();
      expect(await v.read(), isNull);
    },
  );
}
