import 'dart:async';

import 'package:amap_map/amap_map.dart' as amap;
import 'package:amap_map/src/core/amap_flutter_platform.dart';
import 'package:amap_map/src/core/method_channel_amap_map.dart';
import 'package:birdtie_client/src/city/amap_city_map_io.dart';
import 'package:birdtie_client/src/city/amap_native_configuration.dart';
import 'package:birdtie_client/src/city/amap_offline_style.dart';
import 'package:birdtie_client/src/city/amap_session_consent.dart';
import 'package:birdtie_client/src/city/map_privacy_consent.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:plugin_platform_interface/plugin_platform_interface.dart';
import 'package:x_amap_base/x_amap_base.dart' as base;
import 'map_media_rendering_test.dart' show mapFixtureImage, fixturePixels;

class _MapSurface extends MethodChannelAMapFlutterMap
    with MockPlatformInterfaceMixin {
  int builds = 0;
  @override
  Widget buildView(
    Map<String, dynamic> creationParams,
    Set<Factory<OneSequenceGestureRecognizer>> gestureRecognizers,
    void Function(int) created,
  ) {
    builds++;
    return const SizedBox.expand();
  }
}

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();
  test('native expiry ignores counts that were not painted in overview', () {
    final now = DateTime.utc(2026, 10, 18);
    final old = MapEntity(
      id: 'isolated-place',
      kind: MapEntityKind.place,
      title: '隔离',
      subtitle: '',
      latitude: 31.2,
      longitude: 121.4,
      sourceVersion: 'v1',
      recentMoments: MapRecentMoments.visible(
        count: 2,
        checkedAt: now,
        validUntil: now.add(const Duration(minutes: 1)),
        sourceVersion: 'v1',
      ),
    );
    expect(
      amapCopiedMarkerDeadlines(
        old,
        mediaCopied: false,
        auxiliaryCopied: false,
      ),
      isEmpty,
    );
    expect(
      amapCopiedMarkerDeadlines(old, mediaCopied: false, auxiliaryCopied: true),
      [old.recentMoments!.validUntil],
    );
    expect(
      amapRequiresNativeRetirement(
        previousAuthority: 'a',
        authority: 'a',
        previous: [old],
        next: {old.id: old},
        copiedMediaIDs: {},
        copiedAuxiliaryIDs: {},
        now: now.add(const Duration(minutes: 2)),
      ),
      isFalse,
    );
    expect(
      amapRequiresNativeRetirement(
        previousAuthority: 'a',
        authority: 'a',
        previous: [old],
        next: {old.id: old},
        copiedMediaIDs: {},
        copiedAuxiliaryIDs: {old.id},
        now: now.add(const Duration(minutes: 2)),
      ),
      isTrue,
    );
  });
  testWidgets('native retirement rejects retained invalid private pixels', (
    t,
  ) async {
    final image = (await t.runAsync(mapFixtureImage))!;
    addTearDown(image.dispose);
    var live = true;
    final pixels = (await t.runAsync(
      () => fixturePixels(image, () => live, kind: MapEntityKind.moment),
    ))!;
    MapEntity row(MapMediaPixels? media) => MapEntity(
      id: 'isolated-private',
      kind: MapEntityKind.moment,
      title: '隔离单测',
      subtitle: '',
      latitude: 31.2,
      longitude: 121.4,
      sourceVersion: 'v1',
      mediaPixels: media,
    );
    final old = row(pixels);
    bool retires(MapEntity fresh) => amapRequiresNativeRetirement(
      previousAuthority: 'a',
      authority: 'a',
      previous: [old],
      next: {fresh.id: fresh},
    );
    expect(retires(row(pixels)), isFalse);
    expect(retires(row(null)), isTrue);
    live = false;
    expect(
      retires(row(pixels)),
      isTrue,
      reason: 'Same IDs and source version never renew a revoked live guard',
    );
  });
  test(
    'native retirement distinguishes query refresh from revoked content',
    () {
      final now = DateTime.utc(2026, 10, 18);
      MapEntity row({String version = 'v1', MapRecentMoments? recent}) =>
          MapEntity(
            id: 'isolated-place',
            kind: MapEntityKind.place,
            title: '隔离单测地点',
            subtitle: '',
            latitude: 31.2,
            longitude: 121.4,
            sourceVersion: version,
            recentMoments: recent,
          );
      MapRecentMoments count(int number, DateTime until) =>
          MapRecentMoments.visible(
            count: number,
            checkedAt: now,
            validUntil: until,
            sourceVersion: 'v1',
          );
      bool retires(MapEntity old, MapEntity? fresh, {Object authority = 'a'}) =>
          amapRequiresNativeRetirement(
            previousAuthority: 'a',
            authority: authority,
            previous: [old],
            next: {if (fresh != null) fresh.id: fresh},
            now: now,
          );
      expect(
        retires(row(), row()),
        isFalse,
        reason:
            'A new query generation with the same grant keeps native pixels',
      );
      expect(retires(row(), row(), authority: 'b'), isTrue);
      expect(retires(row(), null), isTrue);
      expect(retires(row(), row(version: 'v2')), isTrue);
      final original = row(
        recent: count(2, now.add(const Duration(minutes: 1))),
      );
      expect(
        retires(
          original,
          row(recent: count(2, now.add(const Duration(minutes: 2)))),
        ),
        isFalse,
      );
      expect(
        retires(
          original,
          row(recent: count(1, now.add(const Duration(minutes: 2)))),
        ),
        isTrue,
      );
      expect(retires(original, row()), isTrue);
      expect(
        retires(row(recent: count(2, now)), original),
        isTrue,
        reason: 'Renewal cannot extend the authority of a copied expired count',
      );
    },
  );
  test(
    'visible inactive does not pause GL or change fps; hidden does',
    () async {
      final calls = <String>[];
      final control = AMapRenderControl(
        setPaused: (value) async => calls.add('pause:$value'),
        setFps: (value) async => calls.add('fps:$value'),
        current: () => true,
        applied: (_, __) {},
        failed: () => fail('unexpected native failure'),
      );
      await control.update(AppLifecycleState.resumed, covered: false);
      calls.clear();
      await control.update(AppLifecycleState.inactive, covered: false);
      await control.update(AppLifecycleState.resumed, covered: false);
      expect(calls, isEmpty);
      await control.update(AppLifecycleState.hidden, covered: false);
      expect(calls, ['pause:true', 'fps:5']);
      calls.clear();
      await control.update(AppLifecycleState.resumed, covered: false);
      expect(calls, ['pause:false', 'fps:30']);
      calls.clear();
      await control.update(AppLifecycleState.resumed, covered: true);
      expect(calls, ['fps:5']);
      control.close();
    },
  );
  test(
    'late native pause is serialized and cannot restore stale fps',
    () async {
      final calls = <String>[];
      final held = Completer<void>();
      final control = AMapRenderControl(
        setPaused: (value) async {
          calls.add('pause:$value');
          if (value) await held.future;
        },
        setFps: (value) async => calls.add('fps:$value'),
        current: () => true,
        applied: (_, __) {},
        failed: () => fail('unexpected native failure'),
      );
      await control.update(AppLifecycleState.resumed, covered: false);
      calls.clear();
      final pausing = control.update(AppLifecycleState.paused, covered: false);
      final resuming = control.update(
        AppLifecycleState.resumed,
        covered: false,
      );
      expect(calls, ['pause:true']);
      held.complete();
      await Future.wait([pausing, resuming]);
      expect(calls, ['pause:true', 'pause:false']);
      control.close();
    },
  );
  test('retired render control cannot continue after native await', () async {
    final calls = <String>[];
    final held = Completer<void>();
    final control = AMapRenderControl(
      setPaused: (value) async {
        calls.add('pause:$value');
        await held.future;
      },
      setFps: (value) async => calls.add('fps:$value'),
      current: () => true,
      applied: (_, __) => calls.add('applied'),
      failed: () => fail('unexpected native failure'),
    );
    final update = control.update(AppLifecycleState.paused, covered: false);
    control.close();
    held.complete();
    await update;
    expect(calls, ['pause:true']);
  });
  setUpAll(() async {
    await androidAMapOfflineStyle.load();
  });
  testWidgets(
    'one camera motion hides overlays once without rebuilding each tick',
    (tester) async {
      final surface = _MapSurface();
      AMapFlutterPlatform.instance = surface;
      final consentController = MapPrivacyConsentController(
        store: MemoryMapPrivacyConsentStore(),
      );
      await consentController.setDecision(true);
      final consent = AMapSessionConsent.replaceControllerForTesting(
        consentController,
      );
      binding.defaultBinaryMessenger.setMockMethodCallHandler(
        AMapNativeConfiguration.channel,
        (_) async => true,
      );
      addTearDown(() {
        AMapFlutterPlatform.instance = _MapSurface();
        AMapSessionConsent.replaceControllerForTesting(consent);
        consentController.dispose();
        binding.defaultBinaryMessenger.setMockMethodCallHandler(
          AMapNativeConfiguration.channel,
          null,
        );
      });
      var starts = 0;
      const city = PublicCity(
        id: 'isolated-motion-city',
        name: '隔离单测城市',
        region: '',
        countryCode: 'CN',
        contentStatus: 'ready',
        source: PublicSource(
          label: 'isolated',
          maintainer: 'isolated',
          freshness: 'test',
          updatedAt: null,
        ),
        map: PublicCityMap(
          provider: 'amap',
          latitude: 31.2,
          longitude: 121.4,
          defaultZoom: 11,
          sourceRef: 'isolated',
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AMapCityMapView(
              city: city,
              places: const [],
              entities: const [],
              selectedEntityId: null,
              onPlaceSelected: (_) {},
              onEntitySelected: (_) {},
              fullBleed: true,
              onCameraMotion: () {
                starts++;
              },
            ),
          ),
        ),
      );
      await tester.runAsync(() async {
        await androidAMapOfflineStyle.load();
      });
      await tester.pump();
      await tester.pump();
      expect(find.byType(amap.AMapWidget), findsOneWidget);
      final element = tester.element(find.byType(amap.AMapWidget));
      amap.CameraPosition camera(int i) => amap.CameraPosition(
        target: base.LatLng(31.2 + i / 10000, 121.4),
        zoom: 11,
      );
      tester
          .widget<amap.AMapWidget>(find.byType(amap.AMapWidget))
          .onCameraMove!(camera(0));
      await tester.pump();
      final firstMotionBuilds = surface.builds;
      for (var i = 1; i <= 8; i++) {
        tester
            .widget<amap.AMapWidget>(find.byType(amap.AMapWidget))
            .onCameraMove!(camera(i));
        await tester.pump();
      }
      expect(starts, 1);
      expect(tester.element(find.byType(amap.AMapWidget)), same(element));
      expect(
        surface.builds,
        firstMotionBuilds,
        reason:
            'Camera ticks must not rebuild the platform view or whole overlay tree',
      );
      await tester.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
}
