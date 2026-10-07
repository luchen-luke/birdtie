import 'package:birdtie_client/src/city/native_map_failure.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('tile 403 explains denied basemap resources', () {
    final failure = NativeMapFailure.fromEvent(
      'TILE',
      'Failed to load tile: HTTP status code 403. Forbidden',
    );
    expect(failure.resource, NativeMapResource.tile);
    expect(failure.reason, NativeMapFailureReason.denied);
    expect(failure.httpStatus, 403);
    expect(failure.message, contains('地图瓦片'));
    expect(failure.message, contains('403'));
  });

  for (final resource in ['STYLE', 'SOURCE', 'SPRITE', 'GLYPHS']) {
    test('$resource 403 is visible through the same failure state', () {
      final state = NativeMapFailureState();
      expect(state.report(resource, 'HTTP status code 403. Forbidden'), isTrue);
      expect(state.failure!.reason, NativeMapFailureReason.denied);
      expect(state.takeUnavailableNotification(), isTrue);
      expect(state.takeUnavailableNotification(), isFalse);
    });
  }

  test('failed tiles notify once even when raw tile coordinates differ', () {
    final state = NativeMapFailureState();
    expect(
      state.report('TILE', 'HTTP status code 403 tile 12/2033/1346'),
      isTrue,
    );
    expect(state.takeUnavailableNotification(), isTrue);
    for (var tile = 0; tile < 40; tile++) {
      expect(
        state.report('TILE', 'HTTP status code 403 tile 12/$tile/1346'),
        isFalse,
      );
      expect(state.takeUnavailableNotification(), isFalse);
    }
    expect(state.failure, isNotNull);
    expect(state.failure!.httpStatus, 403);
  });

  test('resource changes update the message without notifying twice', () {
    final state = NativeMapFailureState()..report('STYLE', 'HTTP 403');
    expect(state.takeUnavailableNotification(), isTrue);
    expect(state.report('TILE', 'HTTP 403'), isTrue);
    expect(state.failure!.resource, NativeMapResource.tile);
    expect(state.takeUnavailableNotification(), isFalse);
  });

  test('retry preserves the failure and blocks duplicate retry requests', () {
    final state = NativeMapFailureState()..report('TILE', 'HTTP 403');
    final failure = state.failure;
    expect(state.beginRetry(), isTrue);
    expect(state.failure, same(failure));
    expect(state.retrying, isTrue);
    expect(state.beginRetry(), isFalse);
    expect(state.retryFinishedWithoutRecovery(), isTrue);
    expect(state.failure, same(failure));
    expect(state.retrying, isFalse);
    expect(state.retryFinishedWithoutRecovery(), isFalse);
  });

  test(
    'repeated denial after retry remains visible without repeat notification',
    () {
      final state = NativeMapFailureState()..report('TILE', 'HTTP 403');
      expect(state.takeUnavailableNotification(), isTrue);
      expect(state.beginRetry(), isTrue);
      expect(state.report('TILE', 'HTTP 403'), isTrue);
      expect(state.retrying, isFalse);
      expect(state.failure!.httpStatus, 403);
      expect(state.takeUnavailableNotification(), isFalse);
    },
  );

  test(
    'fully loaded map recovers once and permits a new failure notification',
    () {
      final state = NativeMapFailureState();
      expect(state.mapLoaded(), isFalse);
      expect(state.beginRetry(), isFalse);
      state.report('TILE', 'HTTP 403');
      expect(state.takeUnavailableNotification(), isTrue);
      state.beginRetry();
      expect(state.mapLoaded(), isTrue);
      expect(state.failure, isNull);
      expect(state.retrying, isFalse);
      expect(state.mapLoaded(), isFalse);
      expect(state.takeUnavailableNotification(), isFalse);
      expect(state.report('TILE', 'HTTP 403'), isTrue);
      expect(state.takeUnavailableNotification(), isTrue);
    },
  );

  test('network, rate limit and unknown errors have readable reasons', () {
    expect(
      NativeMapFailure.fromEvent('TILE', 'Connection timed out').reason,
      NativeMapFailureReason.network,
    );
    final rateLimited = NativeMapFailure.fromEvent('SOURCE', 'HTTP status 429');
    expect(rateLimited.reason, NativeMapFailureReason.rateLimited);
    expect(rateLimited.message, contains('稍后重试'));
    final server = NativeMapFailure.fromEvent(
      'unknown',
      'HTTP status code 503',
    );
    expect(server.resource, NativeMapResource.other);
    expect(server.httpStatus, 503);
    expect(server.message, contains('重试'));
  });

  test('diagnostics redact credential values and complete request URLs', () {
    final failure = NativeMapFailure.fromEvent(
      'TILE',
      'HTTP 403 pk.example.signature sk.example.secret '
          'https://api.example.test/tile?access_token=credential',
    );
    expect(failure.safeDiagnostic, isNot(contains('pk.example')));
    expect(failure.safeDiagnostic, isNot(contains('sk.example')));
    expect(failure.safeDiagnostic, isNot(contains('access_token')));
    expect(failure.safeDiagnostic, isNot(contains('credential')));
    expect(failure.message, isNot(contains('credential')));
  });
}
