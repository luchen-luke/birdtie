import 'dart:async';

import 'package:birdtie_client/src/city/native_city_map_io.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';

import 'now_scope_recovery_test.dart' show NowFixture;
import 'package:birdtie_client/src/workspace/map_marker_bitmap.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart' hide Size;

// Actual NativeCityMapView and annotation queue; only the native SDK transport
// is controlled. These are synthetic public points, not real account fixtures.
const _visible = Rect.fromLTRB(0, 130, 430, 610);
const _covered = Rect.fromLTRB(0, 130, 430, 130);
final _city = PublicCity(
  id: 'reader-native-synthetic',
  name: '合成城市',
  region: 'test',
  contentStatus: 'test',
  source: const PublicSource(
    label: '合成测试来源',
    maintainer: '测试',
    freshness: 'test',
    updatedAt: null,
  ),
  map: PublicCityMap(
    provider: 'mapbox',
    latitude: 57.145,
    longitude: -2.1,
    defaultZoom: 12,
    sourceRef: 'synthetic://native-reader',
  ),
);

List<MapEntity> _rows({String revision = '1'}) => [
  for (var i = 0; i < 3; i++)
    MapEntity(
      id: 'place:reader-$i',
      kind: MapEntityKind.place,
      title: '合成地点$i版本$revision',
      subtitle: '当前合成来源',
      latitude: 57.145,
      longitude: -2.1 + i / 1000,
      placeID: 'reader-$i',
      publicLocation: true,
      sourceVersion: revision,
    ),
];

void main() {
  testWidgets(
    'actual Now idle inset commit requests its own frame and restores the same map',
    (t) async {
      final fixture = NowFixture();
      t.view.physicalSize = const Size(430, 900);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      Widget host(double inset) => MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(viewInsets: EdgeInsets.only(bottom: inset)),
          child: child!,
        ),
        // Stop unrelated cosmetic tickers so the host timer has to request
        // its own frame instead of borrowing an animation's next vsync.
        home: TickerMode(
          enabled: false,
          child: MapWorkspace(
            city: fixture.city,
            auth: fixture.auth,
            moments: fixture.moments,
            agentTaskSource: fixture.source,
            seedClient: fixture.client,
            seedApiBaseUrl: fixture.base,
          ),
        ),
      );
      Future<void> idleFrames() async {
        for (var n = 0; n < 10 && t.binding.hasScheduledFrame; n++) {
          await t.pump();
        }
        expect(t.binding.hasScheduledFrame, isFalse);
      }

      try {
        await t.pumpWidget(host(0));
        await t.pumpAndSettle();
        final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
        final element = t.element(find.byType(MapCanvas));
        final before = canvas.contentRect!.value!;
        await t.pumpWidget(host(300));
        await idleFrames();
        expect(canvas.contentRect!.value, before);
        // No pumpAndSettle/forced frame: let the real host's 140ms inset
        // timer fire while the scheduler is idle, as after IME settles.
        await t.pump(const Duration(milliseconds: 150));
        final inset = canvas.contentRect!.value!;
        expect(inset.bottom, lessThan(before.bottom));
        await t.pumpAndSettle();
        await t.pumpWidget(host(0));
        await idleFrames();
        expect(canvas.contentRect!.value, inset);
        await t.pump(const Duration(milliseconds: 150));
        expect(canvas.contentRect!.value, before);
        expect(t.element(find.byType(MapCanvas)), same(element));
        expect(t.takeException(), isNull);
      } finally {
        await fixture.unmount(t);
        fixture.dispose();
      }
    },
  );
  testWidgets(
    'profile rectangle string collision cannot delay reader return',
    (t) async {
      final map = _Map();
      final source = _rows();
      await _mount(t, map, source);
      await t.pumpAndSettle();
      final native = t.element(find.byType(NativeCityMapView));
      final sdk = t.element(find.byType(MapWidget));
      final expected = source.map((e) => e.id).toSet();
      map.flights.clear();
      // Flutter profile/release strips dart:ui toString overrides. Mirror
      // that runtime behavior while exercising the actual renderer, with a
      // retained selection and unchanged authorized source/camera.
      const covered = _ProfileRect(0, 130, 430, 130);
      const visible = _ProfileRect(0, 130, 430, 610);
      expect(covered.toString(), visible.toString());
      expect(covered, isNot(visible));
      await _update(t, source, covered, selectedEntityId: source.first.id);
      await t.pumpAndSettle();
      expect(map.points.created, isEmpty);
      await _update(t, source, visible, selectedEntityId: source.first.id);
      await t.pumpAndSettle();
      expect(map.points.created.keys.toSet(), expected);
      for (final entity in source) {
        expect(
          find.byKey(ValueKey('native-map-point-semantics:${entity.id}')),
          findsOneWidget,
        );
      }
      // Subsequent full coverage and restoration use the same source and
      // selection too; neither operation may rely on the lease timer.
      await _update(t, source, covered, selectedEntityId: source.first.id);
      await t.pumpAndSettle();
      expect(map.points.created, isEmpty);
      await _update(t, source, visible, selectedEntityId: source.first.id);
      await t.pumpAndSettle();
      expect(map.points.created.keys.toSet(), expected);
      expect(t.element(find.byType(NativeCityMapView)), same(native));
      expect(t.element(find.byType(MapWidget)), same(sdk));
      expect(map.flights, isEmpty);
      expect(map.points.clearAllCalls, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );

  for (final change in ['preserveViewport', 'contextKey']) {
    testWidgets(
      'pending reader restore restarts after only $change changes',
      (t) async {
        final map = _Map();
        final source = _rows();
        await _mount(t, map, source);
        await t.pumpAndSettle();
        final native = t.element(find.byType(NativeCityMapView));
        final sdk = t.element(find.byType(MapWidget));
        await _update(t, source, _covered);
        await t.pumpAndSettle();
        expect(map.points.created, isEmpty);
        map.flights.clear();
        final hold = map.holdNextCamera = Completer<CameraState>();
        await _update(t, source, _visible);
        await _pumpUntil(t, () => map.cameraBlocked);
        await _update(
          t,
          source,
          _visible,
          preserveViewport: change != 'preserveViewport',
          contextKey: change == 'contextKey'
              ? 'reader-synthetic-next'
              : 'reader-synthetic',
        );
        hold.complete(map.camera);
        await t.pumpAndSettle();
        expect(
          map.points.created.keys.toSet(),
          source.map((e) => e.id).toSet(),
        );
        for (final entity in source) {
          expect(
            find.byKey(ValueKey('native-map-point-semantics:${entity.id}')),
            findsOneWidget,
          );
          final point = map.points.created[entity.id]!;
          expect(point.geometry.coordinates.lng, entity.longitude);
          expect(point.geometry.coordinates.lat, entity.latitude);
        }
        expect(t.element(find.byType(NativeCityMapView)), same(native));
        expect(t.element(find.byType(MapWidget)), same(sdk));
        // Preserve the existing camera policy: only context changes focus.
        expect(map.flights.length, change == 'contextKey' ? 1 : 0);
        expect(map.points.clearAllCalls, 0);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox.shrink());
      },
      variant: TargetPlatformVariant.only(TargetPlatform.android),
    );
  }

  testWidgets(
    'reader return restores native glyphs and semantics after interrupted delete',
    (t) async {
      final map = _Map();
      final source = _rows();
      await _mount(t, map, source);
      await t.pumpAndSettle();
      final expected = source.map((e) => e.id).toSet();
      expect(map.points.created.keys.toSet(), expected);
      expect(map.points.iconOverlap, isTrue);
      expect(map.points.ignoreIconPlacement, isTrue);
      expect(map.points.textOptional, isTrue);
      final native = t.element(find.byType(NativeCityMapView));
      final sdk = t.element(find.byType(MapWidget));
      map.flights.clear();
      final hold = map.points.holdFinalDelete = Completer<void>();

      await _update(t, source, _covered);
      await _pumpUntil(t, () => map.points.deleteBlocked);
      expect(map.points.created, isEmpty);
      for (final entity in source) {
        expect(
          find.byKey(ValueKey('native-map-point-semantics:${entity.id}')),
          findsNothing,
        );
      }
      await _update(t, source, _visible);
      hold.complete();
      await t.pumpAndSettle();

      expect(map.points.created.keys.toSet(), expected);
      for (final entity in source) {
        expect(
          find.byKey(ValueKey('native-map-point-semantics:${entity.id}')),
          findsOneWidget,
        );
        final point = map.points.created[entity.id]!;
        expect(point.geometry.coordinates.lng, entity.longitude);
        expect(point.geometry.coordinates.lat, entity.latitude);
      }
      expect(t.element(find.byType(NativeCityMapView)), same(native));
      expect(t.element(find.byType(MapWidget)), same(sdk));
      expect(map.flights, isEmpty);
      expect(map.points.clearAllCalls, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );

  testWidgets(
    'late reader delete ACK uses only the latest source after removal and same-ID replacement',
    (t) async {
      final map = _Map();
      final original = _rows();
      await _mount(t, map, original);
      await t.pumpAndSettle();
      final native = t.element(find.byType(NativeCityMapView));
      final sdk = t.element(find.byType(MapWidget));
      map.flights.clear();
      final hold = map.points.holdFinalDelete = Completer<void>();

      await _update(t, original, _covered);
      await _pumpUntil(t, () => map.points.deleteBlocked);
      await _update(t, const [], _visible);
      expect(
        find.byKey(ValueKey('native-map-point-semantics:${original.first.id}')),
        findsNothing,
      );
      final replacement = _rows(revision: '2').take(1).toList();
      await _update(t, replacement, _visible);
      hold.complete();
      await t.pumpAndSettle();

      expect(map.points.created.keys.toSet(), {replacement.single.id});
      expect(
        map.points.created[replacement.single.id]!.textField,
        replacement.single.title,
      );
      for (final removed in original.skip(1)) {
        expect(
          find.byKey(ValueKey('native-map-point-semantics:${removed.id}')),
          findsNothing,
        );
      }
      expect(t.element(find.byType(NativeCityMapView)), same(native));
      expect(t.element(find.byType(MapWidget)), same(sdk));
      expect(map.flights, isEmpty);
      expect(map.points.clearAllCalls, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
}

Future<void> _pumpUntil(WidgetTester t, bool Function() ready) async {
  for (var i = 0; i < 30 && !ready(); i++) {
    await t.pump();
  }
  expect(ready(), isTrue);
}

Future<void> _mount(WidgetTester t, _Map map, List<MapEntity> source) async {
  t.view.physicalSize = const Size(430, 900);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  const channel =
      'dev.flutter.pigeon.mapbox_maps_flutter._MapboxOptions.setAccessToken';
  t.binding.defaultBinaryMessenger.setMockMessageHandler(
    channel,
    (_) async => const StandardMessageCodec().encodeMessage([null]),
  );
  addTearDown(
    () => t.binding.defaultBinaryMessenger.setMockMessageHandler(channel, null),
  );
  final pending = Completer<Object?>();
  t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
    SystemChannels.platform_views,
    (call) async => call.method == 'create' ? pending.future : null,
  );
  addTearDown(
    () => t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform_views,
      null,
    ),
  );
  await t.runAsync(() async {
    for (final selected in [false, true]) {
      for (final dense in [false, true]) {
        await MapMarkerBitmap.render(
          kind: MapEntityKind.place,
          selected: selected,
          dense: dense,
        );
      }
    }
  });
  await _update(t, source, _visible);
  final widget = t.widget<MapWidget>(find.byType(MapWidget));
  widget.onMapCreated!(map);
  widget.onStyleLoadedListener!(
    StyleLoadedEventData.fromJson({
      'timeInterval': {'begin': 0, 'end': 1},
    }),
  );
  await t.pump();
  await t.pump();
}

Future<void> _update(
  WidgetTester t,
  List<MapEntity> source,
  Rect rect, {
  bool preserveViewport = true,
  String contextKey = 'reader-synthetic',
  String? selectedEntityId,
}) async {
  await t.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: NativeCityMapView(
          key: const ValueKey('reader-persistent-native-map'),
          city: _city,
          places: const [],
          entities: source,
          selectedEntityId: selectedEntityId,
          contextKey: contextKey,
          preserveViewport: preserveViewport,
          contentRect: rect,
          accessToken: 'pk.synthetic_unit_only',
          onPlaceSelected: (_) {},
        ),
      ),
    ),
  );
  await t.pump();
  await t.pump();
}

PointAnnotation _annotation(PointAnnotationOptions option) => PointAnnotation(
  id: '${option.textSize == 11 ? 'map-aux:' : ''}${option.customData!['entityId']}',
  geometry: option.geometry,
  customData: option.customData,
  textField: option.textField,
  image: option.image,
  iconAnchor: option.iconAnchor,
  iconSize: option.iconSize,
);

class _Points extends Fake implements PointAnnotationManager {
  final created = <String, PointAnnotationOptions>{};
  Completer<void>? holdFinalDelete;
  bool deleteBlocked = false;
  bool? iconOverlap, ignoreIconPlacement, textOptional;
  int clearAllCalls = 0;
  @override
  String get id => 'reader-controlled-point-manager';
  @override
  Future<void> setIconAllowOverlap(bool value) async {
    iconOverlap = value;
  }

  @override
  Future<void> setIconIgnorePlacement(bool value) async {
    ignoreIconPlacement = value;
  }

  @override
  Future<void> setTextAllowOverlap(bool value) async {}
  @override
  Future<void> setTextIgnorePlacement(bool value) async {}
  @override
  Future<void> setTextOptional(bool value) async {
    textOptional = value;
  }

  @override
  Future<PointAnnotation> create(PointAnnotationOptions option) async {
    final annotation = _annotation(option);
    created[annotation.id] = option;
    return annotation;
  }

  @override
  Future<void> update(PointAnnotation annotation) async {
    final previous = created[annotation.id]!;
    previous.textField = annotation.textField;
  }

  @override
  Future<void> delete(PointAnnotation annotation) async {
    created.remove(annotation.id);
    if (created.isEmpty && holdFinalDelete != null && !deleteBlocked) {
      deleteBlocked = true;
      await holdFinalDelete!.future;
    }
  }

  @override
  Future<void> deleteAll() async {
    clearAllCalls++;
    created.clear();
  }
}

class _Annotations extends Fake implements AnnotationManager {
  _Annotations(this.points);
  final _Points points;
  @override
  Future<PointAnnotationManager> createPointAnnotationManager({
    String? id,
    String? below,
  }) async => points;
}

class _Map extends Fake implements MapboxMap {
  final points = _Points();
  final flights = <CameraOptions>[];
  Completer<CameraState>? holdNextCamera;
  bool cameraBlocked = false;
  @override
  AnnotationManager get annotations => _Annotations(points);
  @override
  Future<CameraState> getCameraState() async {
    final held = holdNextCamera;
    if (held != null) {
      holdNextCamera = null;
      cameraBlocked = true;
      return held.future;
    }
    return camera;
  }

  CameraState get camera => CameraState(
    center: Point(coordinates: Position(-2.1, 57.145)),
    zoom: 12,
    bearing: 0,
    pitch: 0,
    padding: MbxEdgeInsets(top: 0, left: 0, right: 0, bottom: 0),
  );

  @override
  Future<List<ScreenCoordinate?>> pixelsForCoordinates(
    List<Point> coordinates,
  ) async => [
    for (final point in coordinates)
      ScreenCoordinate(x: 80 + (point.coordinates.lng + 2.1) * 130000, y: 250),
  ];

  @override
  Future<List<Point?>> coordinatesForPixels(
    List<ScreenCoordinate?> pixels,
  ) async => [
    for (final pixel in pixels.whereType<ScreenCoordinate>())
      Point(
        coordinates: Position(
          -2.1 + pixel.x / 60000,
          57.145 - pixel.y / 111000,
        ),
      ),
  ];

  @override
  Future<void> flyTo(
    CameraOptions camera,
    MapAnimationOptions? animation,
  ) async => flights.add(camera);

  @override
  Future<CameraOptions> cameraForCoordinateBounds(
    CoordinateBounds bounds,
    MbxEdgeInsets padding,
    double? bearing,
    double? pitch,
    double? maxZoom,
    ScreenCoordinate? offset,
  ) async =>
      CameraOptions(center: bounds.southwest, zoom: 12, padding: padding);
}

// Reproduces the actual AOT/profile diagnostic representation; dimensions
// retain Rect value semantics and are never fabricated production records.
class _ProfileRect extends Rect {
  const _ProfileRect(double left, double top, double right, double bottom)
    : super.fromLTRB(left, top, right, bottom);

  @override
  String toString() => "Instance of 'Rect'";
}
