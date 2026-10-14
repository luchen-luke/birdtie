import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/native_oidc.dart';
import 'package:birdtie_client/src/auth/oidc_pending_store.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _Gateway implements NativeOidcGateway {
  final controller = StreamController<Uri>.broadcast(sync: true);
  final opened = <Uri>[];
  Completer<bool>? waitingOpen;
  @override
  Stream<Uri> get links => controller.stream;
  @override
  Future<bool> open(Uri target) async {
    opened.add(target);
    return waitingOpen == null ? true : waitingOpen!.future;
  }
}

class _Pending extends MemoryOidcPendingStore {
  Completer<void>? waitingRead, waitingWrite;
  final readStarted = Completer<void>(), writeStarted = Completer<void>();
  @override
  Future<PendingOidcLogin?> read() async {
    if (waitingRead != null) {
      if (!readStarted.isCompleted) readStarted.complete();
      await waitingRead!.future;
    }
    return pending;
  }

  @override
  Future<void> write(PendingOidcLogin value) async {
    if (waitingWrite != null) {
      if (!writeStarted.isCompleted) writeStarted.complete();
      await waitingWrite!.future;
    }
    pending = value;
  }
}

class _Vault extends MemorySessionVault {
  final writeStarted = Completer<void>(), releaseWrite = Completer<void>();
  @override
  Future<void> write(StoredSession value) async {
    writeStarted.complete();
    await releaseWrite.future;
    session = value;
  }
}

class _Fixture {
  _Fixture({SessionVault? vault, _Pending? pending}) {
    this.pending = pending ?? _Pending();
    client = MockClient((request) async {
      calls.add('${request.method} ${request.url.path}');
      Object data = switch (request.url.path) {
        '/v1/auth/dev-phone/status' => {'enabled': true},
        '/v1/auth/oidc/status' => {'configured': true},
        '/v1/auth/oidc/exchange' ||
        '/v1/auth/dev-phone/verify' => {'accessToken': 'unit-session'},
        '/v1/me' => {'id': 'unit-owner'},
        _ => {'displayName': '本人'},
      };
      return http.Response(
        jsonEncode({'data': data}),
        request.url.path == '/v1/session/logout' ? 204 : 200,
      );
    });
    auth = BirdtieAuthController(
      client: client,
      apiBaseUrl: 'https://unit.example',
      nativeGateway: gateway,
      oidcPendingStore: this.pending,
      sessionVault: vault ?? MemorySessionVault(),
    );
  }
  final gateway = _Gateway(), calls = <String>[];
  late final _Pending pending;
  late final MockClient client;
  late final BirdtieAuthController auth;
  Uri callback(String challenge) =>
      Uri.parse('birdtie-auth://callback?code=unit-code&challenge=$challenge');
  Future<void> close() async {
    auth.dispose();
    await gateway.controller.close();
  }
}

void main() {
  test(
    'pending callback owns authentication before its asynchronous read',
    () async {
      final f = _Fixture();
      await f.auth.initialize();
      await f.auth.signIn();
      final challenge = f.pending.pending!.challenge;
      expect(f.pending.pending!.origin, 'https://unit.example');
      f.pending.waitingRead = Completer<void>();
      final callback = f.auth.resumeNativeCallback(f.callback(challenge));
      await f.pending.readStarted.future;
      expect(f.auth.busy, isTrue);
      expect(await f.auth.verifyDevPhoneCode('13800138000', '123456'), isFalse);
      expect(f.calls.where((c) => c.endsWith('/dev-phone/verify')), isEmpty);
      f.pending.waitingRead!.complete();
      await callback;
      expect(f.auth.signedIn, isTrue);
      expect(f.calls.where((c) => c.endsWith('/oidc/exchange')), hasLength(1));
      await f.close();
    },
  );

  test(
    'old callback cannot consume the newer PKCE pending challenge',
    () async {
      final f = _Fixture();
      await f.auth.initialize();
      await f.auth.signIn();
      final old = f.pending.pending!.challenge;
      await f.auth.signIn();
      final current = f.pending.pending!.challenge;
      expect(current, isNot(old));
      await f.auth.resumeNativeCallback(f.callback(old));
      expect(f.pending.pending!.challenge, current);
      expect(f.calls.where((c) => c.endsWith('/oidc/exchange')), isEmpty);
      await f.auth.resumeNativeCallback(f.callback(current));
      expect(f.auth.signedIn, isTrue);
      expect(f.calls.where((c) => c.endsWith('/oidc/exchange')), hasLength(1));
      await f.close();
    },
  );

  test(
    'matching callback arriving during browser launch is handled once',
    () async {
      final f = _Fixture();
      await f.auth.initialize();
      f.gateway.waitingOpen = Completer<bool>();
      final launching = f.auth.signIn();
      while (f.gateway.opened.isEmpty) {
        await Future<void>.delayed(Duration.zero);
      }
      final callback = f.callback(f.pending.pending!.challenge);
      f.gateway.controller.add(callback);
      f.gateway.controller.add(callback);
      expect(f.calls.where((c) => c.endsWith('/oidc/exchange')), isEmpty);
      f.gateway.waitingOpen!.complete(true);
      await launching;
      expect(f.auth.signedIn, isTrue);
      expect(f.calls.where((c) => c.endsWith('/oidc/exchange')), hasLength(1));
      await f.close();
    },
  );

  test(
    'retired OIDC origin during pending write never opens the browser',
    () async {
      final p = _Pending()..waitingWrite = Completer<void>();
      final f = _Fixture(pending: p);
      await f.auth.initialize();
      var current = true;
      final launching = f.auth.signIn(isCurrent: () => current);
      await p.writeStarted.future;
      current = false;
      p.waitingWrite!.complete();
      await launching;
      expect(f.gateway.opened, isEmpty);
      expect(p.pending, isNull);
      expect(f.auth.busy, isFalse);
      await f.close();
    },
  );

  test(
    'retirement during secure session write clears only issued token and revokes once',
    () async {
      final vault = _Vault();
      final actual = _Fixture(vault: vault);
      await actual.auth.initialize();
      var current = true;
      final verification = actual.auth.verifyDevPhoneCode(
        '13800138000',
        '123456',
        isCurrent: () => current,
      );
      await vault.writeStarted.future;
      current = false;
      vault.releaseWrite.complete();
      expect(await verification, isFalse);
      expect(vault.session, isNull);
      expect(actual.auth.signedIn, isFalse);
      expect(
        actual.calls.where((c) => c.endsWith('/session/logout')),
        hasLength(1),
      );
      expect(actual.auth.busy, isFalse);
      await actual.close();
    },
  );
}
