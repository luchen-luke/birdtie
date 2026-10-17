import 'package:birdtie_client/src/city/amap_city_map_io.dart';
import 'package:birdtie_client/src/city/native_city_map_io.dart';
import 'package:birdtie_client/src/city/native_map_diagnostics.dart';
import 'package:birdtie_client/src/city/native_map_failure.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'both native adapters compile and diagnostics default to profile only',
    () {
      expect([NativeCityMapView, AMapCityMapView], hasLength(2));
      final sent = <Map<String, int>>[];
      final diagnostics = NativeMapDiagnostics(
        provider: NativeMapDiagnosticProvider.mapbox,
        onEvent: sent.add,
      );
      expect(diagnostics.enabled, kProfileMode);
      expect(kProfileMode, isFalse);
      diagnostics.mounted();
      diagnostics.created();
      diagnostics.cameraMoving();
      diagnostics.renderFrame(
        fullyRendered: true,
        durationMicros: 400,
        needsRepaint: false,
      );
      diagnostics.dispose();
      expect(sent, isEmpty);
      expect(diagnostics.events, isEmpty);
    },
  );

  test(
    'one motion coalesces callbacks and exposes frame gaps and partials',
    () {
      var now = 0;
      final sent = <Map<String, int>>[];
      final diagnostics = NativeMapDiagnostics(
        provider: NativeMapDiagnosticProvider.mapbox,
        enabled: true,
        elapsedMicros: () => now,
        onEvent: sent.add,
      );
      diagnostics.mounted();
      diagnostics.created();
      diagnostics.styleLoaded();
      diagnostics.mapLoaded();
      for (var callback = 0; callback < 20; callback++) {
        diagnostics.cameraMoving();
      }
      diagnostics.renderFrame(
        fullyRendered: false,
        durationMicros: 30000,
        needsRepaint: true,
      );
      now = 100;
      diagnostics.renderFrame(
        fullyRendered: true,
        durationMicros: 1000,
        needsRepaint: false,
      );
      now = 1500100;
      diagnostics.cameraSettled();
      diagnostics.cameraSettled();
      final settled = sent.last;
      expect(settled['motion'], 1);
      expect(settled['cameraCallbacks'], 20);
      expect(settled['frames'], 2);
      expect(settled['fullFrames'], 1);
      expect(settled['maxFrameUs'], 30000);
      expect(settled['lastFrameAgeUs'], 1500000);
      expect(
        sent.where(
          (e) => e['event'] == NativeMapDiagnosticEvent.frameSummary.index,
        ),
        hasLength(1),
      );
      expect(
        sent.where(
          (e) => e['event'] == NativeMapDiagnosticEvent.motionStarted.index,
        ),
        hasLength(1),
      );
      diagnostics.cameraMoving();
      expect(sent.last['motion'], 2);
    },
  );

  test(
    'AMap preparation and lifecycle acknowledgement do not invent frames',
    () {
      final sent = <Map<String, int>>[];
      final diagnostics = NativeMapDiagnostics(
        provider: NativeMapDiagnosticProvider.amap,
        enabled: true,
        onEvent: sent.add,
      );
      diagnostics.stylePrepared();
      diagnostics.created();
      diagnostics.mapLoaded();
      diagnostics.lifecycle(4);
      diagnostics.renderingState(paused: true, fps: 5);
      expect(sent.last['provider'], NativeMapDiagnosticProvider.amap.index);
      expect(sent.last['styles'], 0);
      expect(sent.last['preparedStyles'], 1);
      expect(sent.last['frames'], 0);
      expect(sent.last['lastFrameAgeUs'], -1);
      expect(sent.last['paused'], 1);
      expect(sent.last['fps'], 5);
      diagnostics.renderingState(paused: null, fps: 30);
      expect(sent.last['paused'], -1);
    },
  );

  test('instances distinguish recreation and retire late callbacks', () {
    final sent = <Map<String, int>>[];
    final diagnostics = NativeMapDiagnostics(
      provider: NativeMapDiagnosticProvider.mapbox,
      enabled: true,
      onEvent: sent.add,
    );
    diagnostics.created();
    final first = sent.last;
    diagnostics.created();
    expect(sent.last['host'], first['host']);
    expect(sent.last['instance'], isNot(first['instance']));
    expect(sent.last['creations'], 2);
    diagnostics.dispose();
    final count = sent.length;
    diagnostics.created();
    diagnostics.styleLoaded();
    diagnostics.cameraMoving();
    diagnostics.lifecycle(1);
    diagnostics.dispose();
    expect(sent, hasLength(count));
    expect(
      () => diagnostics.events.last['instance'] = 5,
      throwsUnsupportedError,
    );
  });

  test('logs are bounded numeric categories and same errors are coalesced', () {
    final sent = <Map<String, int>>[];
    final diagnostics = NativeMapDiagnostics(
      provider: NativeMapDiagnosticProvider.mapbox,
      enabled: true,
      capacity: 3,
      eventLimit: 4,
      elapsedMicros: () => 0,
      onEvent: sent.add,
    );
    diagnostics.created();
    for (var error = 0; error < 30; error++) {
      diagnostics.resourceError(
        resource: NativeMapResource.tile,
        reason: NativeMapFailureReason.denied,
        httpStatus: 403,
      );
    }
    expect(sent.last['status'], 403);
    expect(sent, hasLength(2));
    for (var event = 0; event < 8; event++) {
      diagnostics.lifecycle(1);
    }
    diagnostics.dispose();
    expect(sent, hasLength(5));
    expect(diagnostics.events, hasLength(3));
    expect(sent.last['errors'], 30);
    expect(sent.last['dropped'], 6);
    expect(sent.last['event'], NativeMapDiagnosticEvent.disposed.index);
    expect(
      sent.every((event) => event.values.every((value) => value is int)),
      isTrue,
    );
    expect(
      sent
          .expand((event) => event.keys)
          .any(
            (key) => RegExp(
              'token|url|coordinate|city|account|entity|media',
            ).hasMatch(key),
          ),
      isFalse,
    );
  });

  test('a failing diagnostics sink cannot interrupt map work', () {
    final diagnostics = NativeMapDiagnostics(
      provider: NativeMapDiagnosticProvider.amap,
      enabled: true,
      onEvent: (_) => throw StateError('test sink unavailable'),
    );
    expect(diagnostics.created, returnsNormally);
    expect(diagnostics.dispose, returnsNormally);
  });
}
