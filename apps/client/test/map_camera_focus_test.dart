import 'dart:async';

import 'package:birdtie_client/src/city/map_camera_focus.dart';
import 'package:birdtie_client/src/city/native_city_map_io.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart';

// Real widget update/camera call paths with an explicitly fake SDK transport.
// No platform paint, provider request, tiles or device camera proof is claimed.
const _source = PublicSource(
  label: '合成测试来源',
  maintainer: '测试',
  freshness: 'test',
  updatedAt: null,
);
PublicCity _city({String id = 'city-a', double latitude = 57}) => PublicCity(
  id: id,
  name: '合成城市',
  region: 'test',
  contentStatus: 'test',
  source: _source,
  map: PublicCityMap(
    provider: 'mapbox',
    latitude: latitude,
    longitude: -2,
    defaultZoom: 11,
    sourceRef: 'synthetic://camera-test',
  ),
);
MapEntity _entity(String id, double latitude, {String title = '合成地点'}) =>
    MapEntity(
      id: id,
      kind: MapEntityKind.place,
      title: title,
      subtitle: '测试',
      latitude: latitude,
      longitude: -2,
    );
MapCameraFocusInput _input({
  String context = 'task-a',
  String? selected,
  List<MapEntity> entities = const [],
  List<PublicPlace> places = const [],
  PublicCity? city,
}) => MapCameraFocusInput(
  city: city ?? _city(),
  contextKey: context,
  entities: entities,
  places: places,
  selectedEntityId: selected,
);

void main() {
  test('selected-only change targets the same canonical entity', () {
    final entities = [_entity('place:a', 57), _entity('place:b', 58)];
    final old = _input(entities: entities),
        next = _input(entities: entities, selected: 'place:b');
    expect(next.shouldFocusAfter(old), true);
    expect(next.center, const MapCameraPoint('place:b', 58, -2));
    expect(next.zoom, 14);
    expect(next.fitsBounds, false);
  });
  test('clearing selection alone preserves user camera', () {
    final entities = [_entity('place:a', 57)];
    expect(
      _input(
        entities: entities,
      ).shouldFocusAfter(_input(entities: entities, selected: 'place:a')),
      false,
    );
  });
  test('unknown selected ID never invents a selected point', () {
    final old = _input(entities: [_entity('place:a', 57)]);
    final next = _input(
      entities: [_entity('place:a', 57)],
      selected: 'place:missing',
    );
    expect(next.selected, isNull);
    expect(next.shouldFocusAfter(old), false);
  });
  test('same task and coordinates metadata refresh does not reframe', () {
    final old = _input(entities: [_entity('place:a', 57)]);
    final next = _input(entities: [_entity('place:a', 57, title: '新名称')]);
    expect(next.hasSameViewInputs(old), true);
    expect(next.shouldFocusAfter(old), false);
  });
  test('result order alone preserves camera', () {
    final a = _entity('place:a', 57), b = _entity('place:b', 58);
    expect(
      _input(entities: [b, a]).shouldFocusAfter(_input(entities: [a, b])),
      false,
    );
  });
  test('same ID moved coordinates is an actual camera change', () {
    final old = _input(entities: [_entity('place:a', 57)], selected: 'place:a');
    final next = _input(
      entities: [_entity('place:a', 58)],
      selected: 'place:a',
    );
    expect(next.shouldFocusAfter(old), true);
    expect(next.center!.latitude, 58);
  });
  test('new task context requests current coordinates', () {
    final old = _input(entities: [_entity('place:a', 57)]);
    expect(
      _input(
        context: 'task-b',
        entities: [_entity('place:a', 57)],
      ).shouldFocusAfter(old),
      true,
    );
  });
  test('same city ID changed viewport is a current context change', () {
    expect(_input(city: _city(latitude: 58)).shouldFocusAfter(_input()), true);
  });
  test('idle catalog refresh does not reset user pan', () {
    expect(
      _input(
        context: 'idle',
        entities: [_entity('place:b', 58)],
      ).shouldFocusAfter(
        _input(context: 'idle', entities: [_entity('place:a', 57)]),
      ),
      false,
    );
  });
  test('idle explicit selection still focuses existing point', () {
    final entities = [_entity('place:a', 57)];
    final next = _input(
      context: 'idle',
      entities: entities,
      selected: 'place:a',
    );
    expect(
      next.shouldFocusAfter(_input(context: 'idle', entities: entities)),
      true,
    );
    expect(next.center!.id, 'place:a');
  });
  test('empty task projection does not manufacture a city entity target', () {
    expect(_input().center, isNull);
    expect(_input(context: 'idle').center!.latitude, 57);
  });
  test('invalid coordinates never drive focus', () {
    final input = _input(
      entities: [_entity('place:a', double.nan), _entity('place:b', 91)],
      selected: 'place:b',
    );
    expect(input.points, isEmpty);
    expect(input.selected, isNull);
    expect(input.center, isNull);
  });
  test('legacy place selection and duplicated projection share one target', () {
    final place = PublicPlace(
      id: 'a',
      name: '合成地点',
      categoryCode: 'culture',
      summary: '测试',
      source: _source,
      location: const PublicPlaceLocation(
        coordinateSystem: 'wgs84',
        precision: 'point',
        latitude: 57,
        longitude: -2,
      ),
    );
    final input = _input(
      places: [place],
      entities: [_entity('place:a', 57)],
      selected: 'place:a',
    );
    expect(input.points, hasLength(1));
    expect(input.center!.id, 'place:a');
  });

  for (final change in [
    'selection',
    'context',
    'coordinates',
    'pointer',
    'dispose',
  ]) {
    testWidgets(
      'late native bounds cannot overwrite newer $change',
      (t) async {
        final f = await _Fixture.mount(t);
        final pending = Completer<CameraOptions>();
        f.map.pendingFit = pending;
        await f.update(
          entities: [_entity('place:a', 57), _entity('place:b', 58)],
        );
        expect(f.map.boundsRequests, 1);
        if (change == 'selection') {
          await f.update(
            entities: [_entity('place:a', 57), _entity('place:b', 58)],
            selected: 'place:b',
          );
        } else if (change == 'context') {
          await f.update(context: 'task-b', entities: [_entity('place:c', 59)]);
        } else if (change == 'coordinates') {
          await f.update(entities: [_entity('place:a', 60)]);
        } else if (change == 'pointer') {
          final listener = t.widget<Listener>(
            find
                .ancestor(
                  of: find.byType(MapWidget),
                  matching: find.byType(Listener),
                )
                .first,
          );
          listener.onPointerDown!(const PointerDownEvent());
        } else {
          await t.pumpWidget(const SizedBox());
        }
        final count = f.map.flights.length;
        pending.complete(
          CameraOptions(center: Point(coordinates: Position(10, 10)), zoom: 5),
        );
        await t.pump();
        expect(f.map.flights, hasLength(count));
        expect(t.takeException(), isNull);
        await f.close();
      },
      variant: TargetPlatformVariant.only(TargetPlatform.android),
    );
  }
  testWidgets(
    'failed native bounds keeps map alive for the next selection',
    (t) async {
      final f = await _Fixture.mount(t);
      final pending = Completer<CameraOptions>();
      f.map.pendingFit = pending;
      await f.update(
        entities: [_entity('place:a', 57), _entity('place:b', 58)],
      );
      pending.completeError(StateError('synthetic camera transport failure'));
      await t.pump();
      expect(t.takeException(), isNull);
      await f.update(entities: [_entity('place:a', 57)], selected: 'place:a');
      expect(f.map.flights.single.center!.coordinates.lat, 57);
      await f.close();
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'selected-only native update focuses once without replacing SDK/map elements',
    (t) async {
      final f = await _Fixture.mount(t);
      await f.update(entities: [_entity('place:a', 57)]);
      f.map.flights.clear();
      final native = find.byType(NativeCityMapView).evaluate().single,
          sdk = find.byType(MapWidget).evaluate().single;
      await f.update(entities: [_entity('place:a', 57)], selected: 'place:a');
      expect(f.map.flights, hasLength(1));
      expect(f.map.flights.single.center!.coordinates.lat, 57);
      expect(f.map.flights.single.zoom, 14);
      expect(
        identical(native, find.byType(NativeCityMapView).evaluate().single),
        true,
      );
      expect(identical(sdk, find.byType(MapWidget).evaluate().single), true);
      await f.close();
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'native metadata refresh and deselection preserve existing camera',
    (t) async {
      final f = await _Fixture.mount(t);
      await f.update(entities: [_entity('place:a', 57)], selected: 'place:a');
      f.map.flights.clear();
      await f.update(
        entities: [_entity('place:a', 57, title: '更长的新名称')],
        selected: 'place:a',
      );
      await f.update(entities: [_entity('place:a', 57, title: '更长的新名称')]);
      expect(f.map.flights, isEmpty);
      await f.close();
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'valid native retry restores user camera without refitting same result',
    (t) async {
      final f = await _Fixture.mount(t);
      await f.update(entities: [_entity('place:a', 57)]);
      f.map.flights.clear();
      await f.failure();
      await t.tap(find.byKey(const Key('native-map-retry')));
      await t.pump();
      expect(f.map.styleReloads, 1);
      await f.styleLoaded();
      expect(f.map.restored, hasLength(1));
      expect(f.map.restored.single.center!.coordinates.lat, 54);
      expect(f.map.flights, isEmpty);
      await f.close();
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'native retry snapshot awaited across changed context is rejected',
    (t) async {
      final f = await _Fixture.mount(t);
      await f.failure();
      final pending = Completer<CameraState>();
      f.map.pendingCamera = pending;
      await t.tap(find.byKey(const Key('native-map-retry')));
      await t.pump();
      await f.update(context: 'task-b');
      pending.complete(_camera());
      await t.pump();
      expect(f.map.styleReloads, 0);
      expect(f.map.restored, isEmpty);
      await f.close();
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'native pointer input invalidates retry restore without stealing gestures',
    (t) async {
      final f = await _Fixture.mount(t);
      await f.failure();
      await t.tap(find.byKey(const Key('native-map-retry')));
      await t.pump();
      final listener = t.widget<Listener>(
        find
            .ancestor(
              of: find.byType(MapWidget),
              matching: find.byType(Listener),
            )
            .first,
      );
      expect(listener.behavior, HitTestBehavior.deferToChild);
      listener.onPointerDown!(const PointerDownEvent());
      await f.styleLoaded();
      expect(f.map.restored, isEmpty);
      expect(f.map.flights, isEmpty);
      await f.close();
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
}

CameraState _camera() => CameraState(
  center: Point(coordinates: Position(-3, 54)),
  padding: MbxEdgeInsets(top: 0, left: 0, bottom: 0, right: 0),
  zoom: 9,
  bearing: 0,
  pitch: 0,
);

class _Cancel extends Fake implements Cancelable {
  @override
  void cancel() {}
}

class _Points extends Fake implements PointAnnotationManager {
  @override
  Future<void> setIconAllowOverlap(bool value) async {}
  @override
  Cancelable tapEvents({required Function(PointAnnotation) onTap}) => _Cancel();
  @override
  Future<PointAnnotation> create(PointAnnotationOptions option) async =>
      PointAnnotation(
        id: option.customData!['entityId'] as String,
        geometry: option.geometry,
        image: option.image,
        customData: option.customData,
      );
  @override
  Future<void> update(PointAnnotation annotation) async {}
  @override
  Future<void> delete(PointAnnotation annotation) async {}
}

class _Annotations extends Fake implements AnnotationManager {
  final _Points points = _Points();
  @override
  Future<PointAnnotationManager> createPointAnnotationManager({
    String? id,
    String? below,
  }) async => points;
}

class _Map extends Fake implements MapboxMap {
  @override
  final AnnotationManager annotations = _Annotations();
  final flights = <CameraOptions>[], restored = <CameraOptions>[];
  Completer<CameraOptions>? pendingFit;
  Completer<CameraState>? pendingCamera;
  int boundsRequests = 0, styleReloads = 0;
  @override
  Future<CameraState> getCameraState() {
    final pending = pendingCamera;
    pendingCamera = null;
    return pending?.future ?? Future.value(_camera());
  }

  @override
  Future<CameraOptions> cameraForCoordinateBounds(
    CoordinateBounds bounds,
    MbxEdgeInsets padding,
    double? bearing,
    double? pitch,
    double? maxZoom,
    ScreenCoordinate? offset,
  ) {
    boundsRequests++;
    final pending = pendingFit;
    pendingFit = null;
    return pending?.future ??
        Future.value(CameraOptions(center: bounds.southwest, zoom: 12));
  }

  @override
  Future<void> flyTo(
    CameraOptions camera,
    MapAnimationOptions? animation,
  ) async {
    flights.add(camera);
  }

  @override
  Future<void> setCamera(CameraOptions camera) async {
    restored.add(camera);
  }

  @override
  Future<void> loadStyleURI(String style) async {
    styleReloads++;
  }
}

class _Fixture {
  _Fixture(this.t);
  final WidgetTester t;
  final map = _Map();
  static Future<_Fixture> mount(WidgetTester t) async {
    const channel =
        'dev.flutter.pigeon.mapbox_maps_flutter._MapboxOptions.setAccessToken';
    t.binding.defaultBinaryMessenger.setMockMessageHandler(
      channel,
      (_) async => const StandardMessageCodec().encodeMessage([null]),
    );
    addTearDown(
      () =>
          t.binding.defaultBinaryMessenger.setMockMessageHandler(channel, null),
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
    final f = _Fixture(t);
    await f.update();
    t.widget<MapWidget>(find.byType(MapWidget)).onMapCreated!(f.map);
    await f.styleLoaded();
    f.map.flights.clear();
    return f;
  }

  Future<void> update({
    List<MapEntity> entities = const [],
    String? selected,
    String context = 'task-a',
  }) async {
    await t.pumpWidget(
      MaterialApp(
        home: NativeCityMapView(
          key: const ValueKey('native-camera-fixture'),
          city: _city(),
          places: const [],
          entities: entities,
          selectedEntityId: selected,
          contextKey: context,
          accessToken: 'pk.synthetic_widget_only',
          onPlaceSelected: (_) {},
        ),
      ),
    );
    await t.pump();
  }

  Future<void> styleLoaded() async {
    t.widget<MapWidget>(find.byType(MapWidget)).onStyleLoadedListener!(
      StyleLoadedEventData.fromJson({
        'timeInterval': {'begin': 0, 'end': 1},
      }),
    );
    await t.pump();
    await t.pump();
  }

  Future<void> failure() async {
    t.widget<MapWidget>(find.byType(MapWidget)).onMapLoadErrorListener!(
      MapLoadingErrorEventData.fromJson({
        'type': MapLoadErrorType.TILE.index,
        'timestamp': 0,
        'message': 'HTTP 403',
        'sourceId': null,
        'tileId': null,
      }),
    );
    await t.pump();
  }

  Future<void> close() async {
    await t.pumpWidget(const SizedBox());
    await t.pump();
  }
}
