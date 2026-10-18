import 'dart:async';
import 'dart:typed_data';

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
import 'package:flutter/services.dart';
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

class _LiveMapSurface extends MethodChannelAMapFlutterMap
    with MockPlatformInterfaceMixin {
  bool createdOnce = false;
  @override
  Widget buildView(
    Map<String, dynamic> creationParams,
    Set<Factory<OneSequenceGestureRecognizer>> gestureRecognizers,
    void Function(int) created,
  ) {
    if (!createdOnce) {
      createdOnce = true;
      WidgetsBinding.instance.addPostFrameCallback((_) => created(73));
    }
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
    final now = DateTime.utc(2026, 10, 18);
    MapMediaPixels grant(
      bool current,
      DateTime until, {
      bool differentBytes = false,
    }) => MapMediaPixels(
      key: pixels.key,
      image: image,
      normalPNG: differentBytes
          ? Uint8List.fromList(pixels.normalPNG)
          : pixels.normalPNG,
      selectedPNG: pixels.selectedPNG,
      devicePixelRatio: pixels.devicePixelRatio,
      current: () => current,
      validUntil: until,
    );
    final until = now.add(const Duration(seconds: 30));
    final replaced = row(grant(false, until));
    final renewed = row(grant(true, now.add(const Duration(minutes: 1))));
    bool renewalRetires(MapEntity old, MapEntity fresh, {DateTime? at}) =>
        amapRequiresNativeRetirement(
          previousAuthority: 'a',
          authority: 'a',
          previous: [old],
          next: {fresh.id: fresh},
          copiedMediaIDs: {old.id},
          copiedAuxiliaryIDs: {},
          now: at ?? now,
        );
    expect(
      renewalRetires(replaced, renewed),
      isFalse,
      reason:
          'A fresh real grant may authorize the identical cached bytes before expiry',
    );
    expect(
      renewalRetires(replaced, renewed, at: until),
      isTrue,
      reason: 'A fresh grant never revives an expired native copy',
    );
    expect(renewalRetires(replaced, row(grant(false, until))), isTrue);
    expect(
      renewalRetires(replaced, row(grant(true, until, differentBytes: true))),
      isTrue,
      reason:
          'Matching IDs and keys alone cannot authorize different immutable PNGs',
    );
    expect(renewalRetires(replaced, row(grant(true, now))), isTrue);
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
    'native clear acknowledges during motion and rejects superseded authority',
    (tester) async {
      tester.view.devicePixelRatio = 1;
      tester.view.physicalSize = const Size(800, 600);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.view.resetPhysicalSize);
      AMapFlutterPlatform.instance = _LiveMapSurface();
      final consentController = MapPrivacyConsentController(
        store: MemoryMapPrivacyConsentStore(),
      );
      await consentController.setDecision(true);
      final consent = AMapSessionConsent.replaceControllerForTesting(
        consentController,
      );
      const native = MethodChannel('amap_map_73');
      Completer<Map<String, int>>? held;
      var holdProjection = false;
      var projectCalls = 0;
      final markerUpdates = <dynamic>[];
      binding.defaultBinaryMessenger.setMockMethodCallHandler(
        AMapNativeConfiguration.channel,
        (_) async => true,
      );
      binding.defaultBinaryMessenger.setMockMethodCallHandler(native, (
        call,
      ) async {
        switch (call.method) {
          case 'map#toScreenCoordinate':
            projectCalls++;
            if (holdProjection) {
              held = Completer<Map<String, int>>();
              return held!.future;
            }
            final coordinate = call.arguments as List;
            return (coordinate.first as num) > 31.25
                ? {'x': 10000, 'y': 10000}
                : (coordinate.first as num) > 31.215
                ? {'x': 500, 'y': 300}
                : {'x': 200, 'y': 300};
          case 'map#fromScreenCoordinate':
            return [31.2, 121.4];
          case 'markers#update':
            markerUpdates.add((call.arguments as Map)['markerIdsToRemove']);
        }
        return null;
      });
      final changes = ValueNotifier(0);
      var reader = 0;
      var authority = 'a';
      final image = (await tester.runAsync(mapFixtureImage))!;
      final pixels = (await tester.runAsync(
        () => fixturePixels(image, () => true, kind: MapEntityKind.moment),
      ))!;
      addTearDown(image.dispose);
      var oldGrantLive = true;
      var sourceLive = true;
      MapMediaPixels grant(bool Function() current, DateTime until) =>
          MapMediaPixels(
            key: pixels.key,
            image: image,
            normalPNG: pixels.normalPNG,
            selectedPNG: pixels.selectedPNG,
            devicePixelRatio: 1,
            current: current,
            validUntil: until,
          );
      MapEntity row(MapMediaPixels media) {
        final source = MapEntity(
          id: 'isolated-clear',
          kind: MapEntityKind.moment,
          title: '隔离单测动态',
          subtitle: '',
          latitude: 31.2,
          longitude: 121.4,
          sourceVersion: 'v1',
          mediaPixels: media,
        );
        return source.withMediaPixels(
          media,
          readGrant: MapEntityReadGrant(
            source: source,
            validUntil: media.validUntil!,
            copiedPixels: media,
            current: () => sourceLive,
          ),
        );
      }

      List<MapEntity> rows = [
        row(
          grant(
            () => oldGrantLive,
            DateTime.now().toUtc().add(const Duration(seconds: 2)),
          ),
        ),
        () {
          const other = MapEntity(
            id: 'isolated-longer-copy',
            kind: MapEntityKind.place,
            title: '隔离较长期限',
            subtitle: '',
            latitude: 31.22,
            longitude: 121.4,
            sourceVersion: 'other-v1',
          );
          return other.withMediaPixels(
            null,
            readGrant: MapEntityReadGrant(
              source: other,
              validUntil: DateTime.now().toUtc().add(
                const Duration(minutes: 1),
              ),
              current: () => sourceLive,
            ),
          );
        }(),
        const MapEntity(
          id: 'isolated-unpainted',
          kind: MapEntityKind.place,
          title: '隔离未绘制地点',
          subtitle: '',
          latitude: 31.3,
          longitude: 121.4,
          sourceVersion: 'v1',
        ),
      ];
      addTearDown(() {
        changes.dispose();
        AMapFlutterPlatform.instance = _MapSurface();
        AMapSessionConsent.replaceControllerForTesting(consent);
        consentController.dispose();
        binding.defaultBinaryMessenger.setMockMethodCallHandler(native, null);
        binding.defaultBinaryMessenger.setMockMethodCallHandler(
          AMapNativeConfiguration.channel,
          null,
        );
      });
      const city = PublicCity(
        id: 'isolated-clear-city',
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
      Widget host({String? selected}) => MaterialApp(
        home: Scaffold(
          body: AMapCityMapView(
            city: city,
            places: const [],
            entities: const [],
            selectedEntityId: selected,
            onPlaceSelected: (_) {},
            onEntitySelected: (_) {},
            fullBleed: true,
            contentRect: const Rect.fromLTWH(0, 0, 800, 600),
            readerChanges: changes,
            currentReaderEntities: () => rows,
            currentReaderScope: () => reader,
            currentRenderAuthorityScope: () => authority,
          ),
        ),
      );
      await tester.pumpWidget(host());
      Future<void> drain([int count = 8]) async {
        for (var i = 0; i < count; i++) {
          await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)),
          );
          await tester.pump(const Duration(milliseconds: 40));
        }
      }

      amap.AMapWidget mapWidget() =>
          tester.widget<amap.AMapWidget>(find.byType(amap.AMapWidget));
      void move(int step) => mapWidget().onCameraMove!(
        amap.CameraPosition(
          target: base.LatLng(31.2 + step / 10000, 121.4),
          zoom: 11,
        ),
      );
      await drain(20);
      expect(mapWidget().markers, hasLength(2));
      expect(
        mapWidget().markers
            .singleWhere((m) => m.id == 'isolated-clear')
            .icon
            .toMap()[1],
        orderedEquals(pixels.normalPNG),
        reason:
            'This test must actually copy the verified photo PNG, not a DPR fallback glyph',
      );
      final element = tester.element(find.byType(amap.AMapWidget));
      holdProjection = true;
      await tester.pumpWidget(host(selected: 'isolated-clear'));
      await drain(3);
      expect(held, isNotNull);
      final pendingACK = held!;
      final beforeRetry = projectCalls;
      reader++;
      changes.value++;
      await tester.pump();
      pendingACK.complete({'x': 200, 'y': 300});
      await drain(3);
      expect(
        projectCalls,
        greaterThan(beforeRetry),
        reason:
            'A superseded native write needs a new ACK even with identical marker keys',
      );
      expect(held, isNot(same(pendingACK)));
      held!.complete({'x': 200, 'y': 300});
      holdProjection = false;
      await drain(3);
      move(1);
      oldGrantLive = false;
      rows = [
        row(
          grant(
            () => true,
            DateTime.now().toUtc().add(const Duration(minutes: 1)),
          ),
        ),
      ];
      reader++;
      changes.value++;
      await drain(3);
      expect(find.byKey(const Key('amap-retiring-cover')), findsNothing);
      expect(mapWidget().markers, hasLength(2));
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 2100)),
      );
      await tester.pump(const Duration(seconds: 3));
      expect(
        find.byKey(const Key('amap-retiring-cover')),
        findsNothing,
        reason:
            'One missing longer-lived copy must not block another copy from taking its fresh deadline',
      );
      expect(mapWidget().markers, hasLength(2));
      holdProjection = true;
      rows = [];
      reader++;
      changes.value++;
      await drain(3);
      expect(held?.isCompleted ?? true, isTrue);
      expect(mapWidget().markers, hasLength(2));
      expect(
        find.byKey(const Key('amap-retiring-cover')),
        findsNothing,
        reason:
            'Leaving the viewport does not revoke an independently live copied source',
      );
      sourceLive = false;
      reader++;
      changes.value++;
      await drain(3);
      expect(held, isNotNull);
      expect(mapWidget().markers, isEmpty);
      expect(
        markerUpdates.expand((ids) => ids as List),
        contains('isolated-clear'),
      );
      expect(find.byKey(const Key('amap-retiring-cover')), findsOneWidget);
      for (var i = 2; i < 6; i++) {
        move(i);
        await tester.pump();
      }
      held!.complete({'x': 200, 'y': 300});
      holdProjection = false;
      await drain(3);
      expect(
        find.byKey(const Key('amap-retiring-cover')),
        findsNothing,
        reason: 'Camera ticks must not keep a completed native clear masked',
      );
      expect(tester.element(find.byType(amap.AMapWidget)), same(element));

      holdProjection = true;
      held = null;
      authority = 'b';
      reader++;
      changes.value++;
      await drain(3);
      final oldACK = held!;
      final oldCalls = projectCalls;
      authority = 'a';
      reader++;
      changes.value++;
      await tester.pump();
      oldACK.complete({'x': 200, 'y': 300});
      await drain(3);
      expect(projectCalls, greaterThan(oldCalls));
      expect(
        find.byKey(const Key('amap-retiring-cover')),
        findsOneWidget,
        reason: 'An old native ACK cannot uncover a superseding ABA authority',
      );
      expect(held, isNot(same(oldACK)));
      held!.complete({'x': 200, 'y': 300});
      holdProjection = false;
      await drain(3);
      expect(find.byKey(const Key('amap-retiring-cover')), findsNothing);
      expect(mapWidget().markers, isEmpty);

      sourceLive = true;
      const bare = MapEntity(
        id: 'isolated-bare',
        kind: MapEntityKind.moment,
        title: '隔离私有名字与坐标',
        subtitle: '',
        latitude: 31.2,
        longitude: 121.4,
        sourceVersion: 'bare-v1',
      );
      final bareGrant = MapEntityReadGrant(
        source: bare,
        validUntil: DateTime.now().toUtc().add(const Duration(seconds: 2)),
        current: () => sourceLive,
      );
      rows = [bare.withMediaPixels(null, readGrant: bareGrant)];
      reader++;
      changes.value++;
      mapWidget().onCameraMoveEnd!(
        amap.CameraPosition(target: base.LatLng(31.2, 121.4), zoom: 11),
      );
      await drain(8);
      expect(mapWidget().markers, hasLength(1));
      expect(find.byKey(const Key('amap-retiring-cover')), findsNothing);
      holdProjection = true;
      held = null;
      await tester.pumpWidget(host(selected: bare.id));
      await drain(6);
      expect(held, isNotNull);
      final pendingBareACK = held!;
      move(20);
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 2100)),
      );
      await tester.pump(const Duration(milliseconds: 2100));
      expect(bareGrant.currentAt(DateTime.now().toUtc()), isFalse);
      expect(
        find.byKey(const Key('amap-retiring-cover')),
        findsOneWidget,
        reason:
            'Bare private names/coordinates expire while moving and while a native update is pending',
      );
      pendingBareACK.complete({'x': 200, 'y': 300});
      await drain(3);
      expect(
        find.byKey(const Key('amap-retiring-cover')),
        findsOneWidget,
        reason: 'An expired old pending ACK cannot uncover the source',
      );
      expect(mapWidget().markers, isEmpty);
      expect(held, isNot(same(pendingBareACK)));
      held!.complete({'x': 200, 'y': 300});
      holdProjection = false;
      await drain(3);
      expect(find.byKey(const Key('amap-retiring-cover')), findsNothing);
      expect(mapWidget().markers, isEmpty);
      await tester.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
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
