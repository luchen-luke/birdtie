import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/oidc_pending_store.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'native_oidc_auth_test.dart' show FakeNativeOidcGateway;

const originA = 'https://synthetic-a.invalid';
const originB = 'https://synthetic-b.invalid';
const owner = '93000000-0000-4000-8000-000000000001';

http.Response _wire(Object data, {int status = 200}) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

String _challenge(String verifier) => base64UrlEncode(
  sha256.convert(ascii.encode(verifier)).bytes,
).replaceAll('=', '');

PendingOidcLogin _pending(
  String origin, {
  String verifier = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  String channel = 'NATIVE',
  DateTime? createdAt,
}) => PendingOidcLogin(
  origin: origin,
  channel: channel,
  verifier: verifier,
  challenge: _challenge(verifier),
  createdAt: createdAt ?? DateTime.now().toUtc(),
);

class _TrackingClient extends http.BaseClient {
  final calls = <http.BaseRequest>[];
  int closes = 0;
  final bool configured = true;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    if (closes != 0) throw StateError('borrowed client closed');
    calls.add(request);
    final r = switch (request.url.path) {
      '/v1/auth/dev-phone/status' => _wire({'enabled': true}),
      '/v1/auth/oidc/status' => _wire({'configured': configured}),
      '/v1/auth/oidc/exchange' => _wire({'accessToken': 'synthetic-issued'}),
      '/v1/auth/dev-phone/verify' => _wire({'accessToken': 'synthetic-issued'}),
      '/v1/me' => _wire({'id': owner}),
      _ when request.url.path == '/v1/accounts/' + owner + '/profile' => _wire({
        'displayName': '隔离样例本人',
      }),
      '/v1/session/logout' => http.Response('', 204),
      _ => http.Response('', 200),
    };
    return http.StreamedResponse(
      Stream.value(r.bodyBytes),
      r.statusCode,
      headers: r.headers,
    );
  }

  @override
  void close() => closes++;
}

class _ForeignWriteVault extends MemorySessionVault {
  @override
  Future<void> write(StoredSession value) async {
    session = StoredSession(
      token: value.token,
      method: value.method,
      origin: originB,
    );
    throw StateError('synthetic replacement while write failed');
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const storage = FlutterSecureStorage();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  test(
    'canonical origin v2 keys normalize explicit default port but reject malformed input',
    () {
      expect(canonicalAuthOrigin('https://SYNTHETIC-A.invalid:443/'), originA);
      expect(
        SecureSessionVault.keyForOrigin(originA),
        SecureSessionVault.keyForOrigin('https://SYNTHETIC-A.invalid:443/'),
      );
      expect(
        SecureSessionVault.keyForOrigin(originA),
        isNot(SecureSessionVault.keyForOrigin(originB)),
      );
      expect(
        SecureSessionVault.keyForOrigin(originA),
        isNot(SecureSessionVault.keyForOrigin('http://synthetic-a.invalid')),
      );
      expect(canonicalAuthOrigin(''), '');
      for (final input in [
        'https://user:password@synthetic-a.invalid',
        'https://synthetic-a.invalid?x=1',
        'https://synthetic-a.invalid#x',
        'https://synthetic-a.invalid/other-api',
        'file:///secret',
        'relative.invalid',
      ]) {
        expect(() => canonicalAuthOrigin(input), throwsFormatException);
      }
    },
  );

  test(
    'legacy global v1 session and pending are retained but never sent to new origin',
    () async {
      final p = _pending(originA);
      final legacySession = jsonEncode({
        'token': 'legacy-local',
        'method': 'oidc',
      });
      final legacyPending = jsonEncode({
        'verifier': p.verifier,
        'challenge': p.challenge,
        'createdAt': p.createdAt.toIso8601String(),
      });
      FlutterSecureStorage.setMockInitialValues({
        'birdtie.session.v1': legacySession,
        'birdtie.oidc.pending.v1': legacyPending,
      });
      final client = _TrackingClient(), gateway = FakeNativeOidcGateway();
      final auth = BirdtieAuthController(
        apiBaseUrl: originB,
        client: client,
        nativeGateway: gateway,
      );
      await auth.initialize();
      await auth.resumeNativeCallback(
        Uri.parse(
          'birdtie-auth://callback?code=synthetic-old&challenge=' + p.challenge,
        ),
      );
      expect(auth.signedIn, isFalse);
      expect(
        client.calls.where((r) => r.headers.containsKey('Authorization')),
        isEmpty,
      );
      expect(client.calls.where((r) => r.url.path == '/v1/me'), isEmpty);
      expect(
        client.calls.where((r) => r.url.path.endsWith('/exchange')),
        isEmpty,
      );
      expect(await storage.read(key: 'birdtie.session.v1'), legacySession);
      expect(await storage.read(key: 'birdtie.oidc.pending.v1'), legacyPending);
      auth.dispose();
      await gateway.controller.close();
      client.close();
    },
  );

  test(
    'secure v2 credential payload and exact key isolate signout across two origins',
    () async {
      const a = SecureSessionVault(origin: originA);
      const b = SecureSessionVault(origin: originB);
      await a.write(
        const StoredSession(
          token: 'same-synthetic-token',
          method: 'oidc',
          origin: originA,
        ),
      );
      await b.write(
        const StoredSession(
          token: 'same-synthetic-token',
          method: 'oidc',
          origin: originB,
        ),
      );
      final payload = jsonDecode(
        (await storage.read(key: SecureSessionVault.keyForOrigin(originA)))!,
      );
      expect(payload, {
        'schema': 'birdtie-session-v2',
        'origin': originA,
        'token': 'same-synthetic-token',
        'method': 'oidc',
      });
      expect((await a.read())?.origin, originA);
      expect((await b.read())?.origin, originB);
      final client = _TrackingClient();
      final auth = BirdtieAuthController(client: client, apiBaseUrl: originA);
      await auth.initialize();
      expect(auth.signedIn, isTrue);
      await auth.signOut();
      expect(await a.read(), isNull);
      expect((await b.read())?.token, 'same-synthetic-token');
      expect(
        client.calls
            .where((r) => r.url.path == '/v1/session/logout')
            .single
            .url
            .origin,
        originA,
      );
      auth.dispose();
      expect(client.closes, 0);
      client.close();
    },
  );

  test(
    'foreign payload at local v2 key is rejected without deleting foreign key or legacy record',
    () async {
      const a = SecureSessionVault(origin: originA);
      const b = SecureSessionVault(origin: originB);
      await b.write(
        const StoredSession(token: 'foreign', method: 'oidc', origin: originB),
      );
      await storage.write(key: 'birdtie.session.v1', value: 'unlabelled-old');
      await storage.write(
        key: SecureSessionVault.keyForOrigin(originA),
        value: jsonEncode({
          'schema': 'birdtie-session-v2',
          'origin': originB,
          'token': 'foreign',
          'method': 'oidc',
        }),
      );
      expect(await a.read(), isNull);
      expect(
        await storage.read(key: SecureSessionVault.keyForOrigin(originA)),
        isNull,
      );
      expect((await b.read())?.token, 'foreign');
      expect(await storage.read(key: 'birdtie.session.v1'), 'unlabelled-old');
      await expectLater(
        a.write(
          const StoredSession(
            token: 'foreign',
            method: 'oidc',
            origin: originB,
          ),
        ),
        throwsFormatException,
      );
    },
  );

  for (final source in ['', originA]) {
    test(
      'injected origin ' +
          (source.isEmpty ? 'missing' : 'foreign') +
          ' cannot restore or consume native pending at B',
      () async {
        final vault = MemorySessionVault()
          ..session = StoredSession(
            token: 'must-not-forward',
            method: 'oidc',
            origin: source,
          );
        final valid = _pending(originA);
        final original = PendingOidcLogin(
          verifier: valid.verifier,
          challenge: valid.challenge,
          createdAt: valid.createdAt,
          origin: source,
        );
        final pending = MemoryOidcPendingStore()..pending = original;
        final client = _TrackingClient(), gateway = FakeNativeOidcGateway();
        final auth = BirdtieAuthController(
          client: client,
          apiBaseUrl: originB,
          sessionVault: vault,
          oidcPendingStore: pending,
          nativeGateway: gateway,
        );
        await auth.initialize();
        await auth.resumeNativeCallback(
          Uri.parse(
            'birdtie-auth://callback?code=synthetic&challenge=' +
                original.challenge,
          ),
        );
        expect(auth.authorizationHeader, isNull);
        expect(
          client.calls.where((r) => r.headers.containsKey('Authorization')),
          isEmpty,
        );
        expect(
          client.calls.where((r) => r.url.path.endsWith('/exchange')),
          isEmpty,
        );
        expect(identical(pending.pending, original), isTrue);
        expect(vault.session?.token, 'must-not-forward');
        auth.dispose();
        await gateway.controller.close();
        client.close();
      },
    );
  }

  test(
    'native secure pending v2 exact origin and channel leave A operation untouched by B callback',
    () async {
      const a = SecureOidcPendingStore(origin: originA);
      const b = SecureOidcPendingStore(origin: originB);
      final original = _pending(originA);
      await a.write(original);
      expect((await a.read())?.origin, originA);
      expect(await b.read(), isNull);
      final client = _TrackingClient(), gateway = FakeNativeOidcGateway();
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: originB,
        nativeGateway: gateway,
      );
      await auth.initialize();
      await auth.resumeNativeCallback(
        Uri.parse(
          'birdtie-auth://callback?code=synthetic&challenge=' +
              original.challenge,
        ),
      );
      expect(
        client.calls.where((r) => r.url.path.endsWith('/exchange')),
        isEmpty,
      );
      expect((await a.read())?.challenge, original.challenge);
      await expectLater(
        a.write(_pending(originA, channel: 'WEB')),
        throwsFormatException,
      );
      expect((await a.read())?.challenge, original.challenge);
      auth.dispose();
      await gateway.controller.close();
      client.close();
    },
  );

  test(
    'browser v2 selector and payload bind API origin challenge channel and finite PKCE lease',
    () {
      final pending = _pending(originA, channel: 'WEB');
      final a = oidcBrowserSelector(originA, pending.challenge);
      final b = oidcBrowserSelector(originB, pending.challenge);
      expect(a, isNot(b));
      expect(decodeOidcBrowserSelector(a), (
        origin: originA,
        challenge: pending.challenge,
      ));
      expect(
        () => decodeOidcBrowserSelector(pending.challenge),
        throwsFormatException,
      );
      expect(
        PendingOidcLogin.decode(pending.toJson(), originA, 'WEB')?.verifier,
        pending.verifier,
      );
      expect(PendingOidcLogin.decode(pending.toJson(), originB, 'WEB'), isNull);
      expect(
        PendingOidcLogin.decode(pending.toJson(), originA, 'NATIVE'),
        isNull,
      );
      expect(
        PendingOidcLogin.decode(
          {...pending.toJson(), 'origin': ''},
          originA,
          'WEB',
        ),
        isNull,
      );
      expect(
        PendingOidcLogin.decode(
          {...pending.toJson(), 'verifier': 'b' * 43},
          originA,
          'WEB',
        ),
        isNull,
      );
      expect(
        PendingOidcLogin.decode(
          pending.toJson(),
          originA,
          'WEB',
          now: pending.createdAt.add(const Duration(minutes: 5)),
        ),
        isNull,
      );
      expect(
        PendingOidcLogin.decode(
          pending.toJson(),
          originA,
          'WEB',
          now: pending.createdAt.subtract(const Duration(seconds: 1)),
        ),
        isNull,
      );
    },
  );

  test(
    'cold native original callback at its bound API authenticates without profile or browser guess',
    () async {
      const store = SecureOidcPendingStore(origin: originA);
      final original = _pending(originA);
      await store.write(original);
      final client = _TrackingClient(), gateway = FakeNativeOidcGateway();
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: originA,
        nativeGateway: gateway,
      );
      await auth.initialize();
      await auth.resumeNativeCallback(
        Uri.parse(
          'birdtie-auth://callback?code=synthetic-original&challenge=' +
              original.challenge,
        ),
      );
      expect(auth.accountID, owner);
      expect(auth.loginMethod, 'oidc');
      expect(auth.apiOrigin, originA);
      expect(
        client.calls.where((r) => r.url.path.endsWith('/exchange')),
        hasLength(1),
      );
      final readback = await const SecureSessionVault(origin: originA).read();
      expect(readback?.origin, originA);
      expect(readback?.token, 'synthetic-issued');
      expect(await store.read(), isNull);
      auth.dispose();
      auth.dispose();
      expect(client.closes, 0);
      expect(
        (await client.get(Uri.parse(originA + '/healthz'))).statusCode,
        200,
      );
      await gateway.controller.close();
      client.close();
    },
  );

  test(
    'origin A B A newer native attempt rejects old callback and disposed owner never revives it',
    () async {
      final client = _TrackingClient();
      final gateways = [
        FakeNativeOidcGateway(),
        FakeNativeOidcGateway(),
        FakeNativeOidcGateway(),
      ];
      final oldA = BirdtieAuthController(
        client: client,
        apiBaseUrl: originA,
        nativeGateway: gateways[0],
      );
      await oldA.initialize();
      await oldA.signIn();
      final old = (await const SecureOidcPendingStore(origin: originA).read())!;
      oldA.dispose();
      final b = BirdtieAuthController(
        client: client,
        apiBaseUrl: originB,
        nativeGateway: gateways[1],
      );
      await b.initialize();
      await b.signIn();
      final currentB = (await const SecureOidcPendingStore(
        origin: originB,
      ).read())!;
      final freshA = BirdtieAuthController(
        client: client,
        apiBaseUrl: originA,
        nativeGateway: gateways[2],
      );
      await freshA.initialize();
      await freshA.signIn();
      final currentA = (await const SecureOidcPendingStore(
        origin: originA,
      ).read())!;
      final callback = Uri.parse(
        'birdtie-auth://callback?code=stale&challenge=' + old.challenge,
      );
      await oldA.resumeNativeCallback(callback);
      await b.resumeNativeCallback(callback);
      await freshA.resumeNativeCallback(callback);
      expect(
        client.calls.where((r) => r.url.path.endsWith('/exchange')),
        isEmpty,
      );
      expect(
        (await const SecureOidcPendingStore(origin: originA).read())?.challenge,
        currentA.challenge,
      );
      expect(
        (await const SecureOidcPendingStore(origin: originB).read())?.challenge,
        currentB.challenge,
      );
      await freshA.resumeNativeCallback(
        Uri.parse(
          'birdtie-auth://callback?code=current&challenge=' +
              currentA.challenge,
        ),
      );
      expect(freshA.signedIn, isTrue);
      expect(
        client.calls.where((r) => r.url.path.endsWith('/exchange')),
        hasLength(1),
      );
      expect(
        (await const SecureOidcPendingStore(origin: originB).read())?.challenge,
        currentB.challenge,
      );
      b.dispose();
      freshA.dispose();
      for (final gateway in gateways) await gateway.controller.close();
      expect(client.closes, 0);
      client.close();
    },
  );

  test(
    'failed A session storage never deletes foreign same-token B record and revokes at original A only',
    () async {
      final vault = _ForeignWriteVault(), client = _TrackingClient();
      final auth = BirdtieAuthController(
        client: client,
        apiBaseUrl: originA,
        sessionVault: vault,
      );
      await auth.initialize();
      expect(await auth.verifyDevPhoneCode('13800138000', '123456'), isFalse);
      expect(auth.signedIn, isFalse);
      expect(vault.session?.origin, originB);
      expect(vault.session?.token, 'synthetic-issued');
      final revoke = client.calls
          .where((r) => r.url.path == '/v1/session/logout')
          .single;
      expect(revoke.url.origin, originA);
      expect(revoke.headers['Authorization'], 'Bearer synthetic-issued');
      auth.dispose();
      client.close();
    },
  );

  test(
    'secure storage completed key lock does not retain a prior event-loop tail',
    () async {
      const vault = SecureSessionVault(origin: originA);
      await vault.write(
        const StoredSession(token: 'one', method: 'oidc', origin: originA),
      );
      await vault.delete();
      await vault.write(
        const StoredSession(token: 'two', method: 'oidc', origin: originA),
      );
      expect((await vault.read())?.token, 'two');
    },
  );
}
