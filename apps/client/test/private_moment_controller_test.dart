import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/authenticated_client.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/native_oidc.dart';
import 'package:birdtie_client/src/auth/oidc_pending_store.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
import 'package:birdtie_client/src/content/private_moment_media_controller.dart';
import 'package:birdtie_client/src/content/private_moment_media_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _owner = '11111111-1111-4111-8111-111111111111';
const _momentID = '22222222-2222-4222-8222-222222222222';
const _base = 'https://unit.invalid';

http.Response _response(Object data, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);

Map<String, Object> _record() => {
  'id': _momentID,
  'authorAccountId': _owner,
  'visibility': 'private',
  'cityId': 'aberdeen-gb',
  'title': '本地合成私人草稿',
  'body': 'synthetic private content',
  'timePrecision': 'unknown',
  'locationPrecision': 'city',
  'status': 'draft',
  'revision': 1,
};

class _NoNativeLogin implements NativeOidcGateway {
  @override
  Stream<Uri> get links => const Stream.empty();
  @override
  Future<bool> open(Uri uri) async => false;
}

Future<({BirdtieAuthController auth, MemorySessionVault vault})> _auth({
  bool signedIn = true,
}) async {
  final vault = MemorySessionVault();
  if (signedIn) {
    vault.session = const StoredSession(
      token: 'synthetic-initial',
      method: 'dev_phone',
      origin: _base,
    );
  }
  final auth = BirdtieAuthController(
    apiBaseUrl: _base,
    sessionVault: vault,
    oidcPendingStore: MemoryOidcPendingStore(),
    nativeGateway: _NoNativeLogin(),
    client: MockClient(
      (r) async => switch (r.url.path) {
        '/v1/me' => _response({'id': _owner}),
        '/v1/auth/dev-phone/status' => _response({'enabled': true}),
        '/v1/auth/oidc/status' => _response({'configured': false}),
        '/v1/auth/dev-phone/verify' => _response({
          'accessToken': 'synthetic-renewed',
        }),
        '/v1/session/logout' => http.Response('', 204),
        _ => _response({'displayName': '合成账号'}),
      },
    ),
  );
  await auth.initialize();
  addTearDown(auth.dispose);
  return (auth: auth, vault: vault);
}

PrivateMomentController _moments(
  BirdtieAuthController auth,
  http.Client client,
) => PrivateMomentController(
  authorizationHeader: () => auth.authorizationHeader,
  ownerID: () => auth.accountID,
  identityChanges: auth,
  apiBaseUrl: _base,
  client: client,
  pendingStore: MemoryMomentControlPendingStore(),
);

PrivateMomentMediaController _media(
  BirdtieAuthController auth,
  PrivateMomentController moments,
) => PrivateMomentMediaController(
  momentID: _momentID,
  momentRevision: 1,
  authorizationHeader: () => auth.authorizationHeader,
  ownerID: () => auth.accountID,
  identityChanges: auth,
  sourceCurrent: () => true,
  apiBaseUrl: moments.apiBaseUrl,
  client: moments.borrowedClient,
  pendingStore: MemoryPrivateImagePendingStore(),
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('guest moments never read or save private records', () async {
    final fixture = await _auth(signedIn: false);
    var calls = 0;
    final transport = MockClient((_) async {
      calls++;
      return _response([]);
    });
    addTearDown(transport.close);
    final moments = _moments(fixture.auth, transport);
    addTearDown(moments.dispose);
    await moments.refresh();
    expect(
      await moments.create(cityID: 'aberdeen-gb', title: '草稿', body: ''),
      isFalse,
    );
    expect(calls, 0);
    expect(moments.moments, isEmpty);
    expect(moments.error, isNull);
  });

  test(
    'moments and private media borrow a live transport after disposal',
    () async {
      final fixture = await _auth();
      var recoveries = 0;
      final transport = MockClient((_) async => _response([]));
      addTearDown(transport.close);
      final client = AuthenticatedClient(
        auth: fixture.auth,
        client: transport,
        onUnauthorized: (_) async {
          recoveries++;
        },
      );
      addTearDown(client.close);
      final moments = _moments(fixture.auth, client);
      final media = _media(fixture.auth, moments);
      await media.load();
      media.dispose();
      await moments.refresh();
      expect(moments.error, isNull);
      moments.dispose();
      final response = await client.get(
        Uri.parse('$_base/v1/me/moments'),
        headers: {'Authorization': fixture.auth.authorizationHeader!},
      );
      expect(response.statusCode, 200);
      expect(recoveries, 0);
    },
  );

  test(
    'concurrent moments and media 401 retire once; login never replays a save',
    () async {
      final fixture = await _auth();
      final reached = Completer<void>(), release = Completer<void>();
      var pending = 0, writes = 0, recoveries = 0;
      final transport = MockClient((request) async {
        if (request.method == 'POST' ||
            request.url.path.endsWith('private-images')) {
          if (request.method == 'POST') writes++;
          if (++pending == 2) reached.complete();
          await release.future;
          return http.Response('', 401);
        }
        return _response([_record()]);
      });
      addTearDown(transport.close);
      final client = AuthenticatedClient(
        auth: fixture.auth,
        client: transport,
        onUnauthorized: (token) async {
          recoveries++;
          await fixture.auth.invalidateRejectedSession(token);
        },
      );
      addTearDown(client.close);
      final moments = _moments(fixture.auth, client);
      addTearDown(moments.dispose);
      await moments.refresh();
      expect(moments.moments.single.body, 'synthetic private content');
      final media = _media(fixture.auth, moments);
      addTearDown(media.dispose);
      final save = moments.previewCreate(
        cityID: 'aberdeen-gb',
        title: '明确保存',
        body: '',
      );
      final read = media.load();
      await reached.future;
      release.complete();
      expect(await save, isNull);
      await read;
      expect(recoveries, 1);
      expect(fixture.auth.signedIn, isFalse);
      expect(fixture.auth.rejectedSessionOwnerID, _owner);
      expect(fixture.vault.session, isNull);
      expect(moments.moments, isEmpty);
      expect(media.retired, isTrue);
      expect(media.images, isEmpty);
      expect(writes, 1);
      expect(
        await fixture.auth.verifyDevPhoneCode(
          'synthetic-phone',
          'synthetic-code',
        ),
        isTrue,
      );
      await Future<void>.delayed(Duration.zero);
      expect(fixture.auth.accountID, _owner);
      expect(writes, 1, reason: 'renewal may read, but must never repeat POST');
      expect(recoveries, 1);
    },
  );

  for (final status in [403, 404, 429, 503]) {
    test(
      '$status from moments remains an access/service failure without login',
      () async {
        final fixture = await _auth();
        var recoveries = 0, writes = 0, reads = 0;
        final transport = MockClient((request) async {
          if (request.method == 'POST') writes++;
          if (request.method == 'GET' && ++reads == 1) {
            return _response([_record()]);
          }
          return http.Response('', status);
        });
        addTearDown(transport.close);
        final client = AuthenticatedClient(
          auth: fixture.auth,
          client: transport,
          onUnauthorized: (_) async {
            recoveries++;
          },
        );
        addTearDown(client.close);
        final moments = _moments(fixture.auth, client);
        addTearDown(moments.dispose);
        await moments.refresh();
        expect(moments.moments, hasLength(1));
        await moments.refresh();
        expect(moments.readStatus, status);
        expect(
          moments.error,
          contains(switch (status) {
            403 => '无权',
            404 => '不可访问',
            429 => '过于频繁',
            _ => '服务暂不可用',
          }),
        );
        expect(
          moments.moments,
          status == 403 || status == 404 ? isEmpty : hasLength(1),
        );
        expect(
          await moments.previewCreate(
            cityID: 'aberdeen-gb',
            title: '明确保存',
            body: '',
          ),
          isNull,
        );
        expect(recoveries, 0);
        expect(fixture.auth.signedIn, isTrue);
        expect(writes, 1);
        expect(
          moments.creationUncertain,
          isFalse,
          reason: "a failed preview has no write effect",
        );
        if (status >= 500) {
          expect(
            await moments.create(
              cityID: 'aberdeen-gb',
              title: '重复保存',
              body: '',
            ),
            isFalse,
          );
          expect(
            writes,
            1,
            reason: 'raw SDK cannot bypass explicit preview and confirmation',
          );
        } else {
          expect(
            moments.error,
            contains(
              status == 403
                  ? '无权'
                  : status == 404
                  ? '不可访问'
                  : '过于频繁',
            ),
          );
        }
      },
    );
  }

  for (final timeout in [false, true]) {
    test(
      'network failure keeps current private cache without forcing login; timeout=$timeout',
      () async {
        final fixture = await _auth();
        var recoveries = 0, reads = 0;
        final transport = MockClient((_) async {
          if (++reads == 1) return _response([_record()]);
          if (timeout) throw TimeoutException('synthetic timeout');
          throw http.ClientException('synthetic offline');
        });
        addTearDown(transport.close);
        final client = AuthenticatedClient(
          auth: fixture.auth,
          client: transport,
          onUnauthorized: (_) async {
            recoveries++;
          },
        );
        addTearDown(client.close);
        final moments = _moments(fixture.auth, client);
        addTearDown(moments.dispose);
        await moments.refresh();
        await moments.refresh();
        expect(moments.moments, hasLength(1));
        expect(moments.readStatus, isNull);
        expect(moments.error, contains(timeout ? '超时' : '无法连接'));
        expect(recoveries, 0);
        expect(fixture.auth.signedIn, isTrue);
      },
    );
  }

  test('late private read cannot reappear after explicit logout', () async {
    final fixture = await _auth();
    final started = Completer<void>(), late = Completer<http.Response>();
    var recoveries = 0;
    final transport = MockClient((_) async {
      started.complete();
      return late.future;
    });
    addTearDown(transport.close);
    final client = AuthenticatedClient(
      auth: fixture.auth,
      client: transport,
      onUnauthorized: (_) async {
        recoveries++;
      },
    );
    addTearDown(client.close);
    final moments = _moments(fixture.auth, client);
    addTearDown(moments.dispose);
    final read = moments.refresh();
    await started.future;
    await fixture.auth.signOut();
    late.complete(_response([_record()]));
    await read;
    expect(moments.moments, isEmpty);
    expect(moments.error, isNull);
    expect(fixture.auth.rejectedSessionOwnerID, isNull);
    expect(recoveries, 0);
  });
}
