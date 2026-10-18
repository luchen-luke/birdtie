import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';

import 'package:birdtie_client/src/city/mapbox_style.dart';
import 'package:birdtie_client/src/city/native_city_map_io.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_bitmap.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:latlong2/latlong.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart'
    hide Size, MapOptions;

MapEntity row(
  String id, {
  MapEntityKind kind = MapEntityKind.moment,
  double lat = 57.14,
  double lng = -2.1,
  String revision = '1',
  List<String> members = const [],
}) => MapEntity(
  id: id,
  kind: kind,
  title: '合成内容',
  subtitle: '当前来源',
  latitude: lat,
  longitude: lng,
  sourceVersion: revision,
  memberIDs: members,
  precision: kind == MapEntityKind.cluster ? 'area' : 'point',
);

void main() {
  testWidgets('map semantics keep full labels and distinct 48dp actions', (
    t,
  ) async {
    final semantics = t.ensureSemantics();
    final selected = <String>[];
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Stack(
            children: [
              for (var i = 0; i < 2; i++)
                Positioned(
                  left: 100,
                  top: 100 + 50.0 * i,
                  width: 48,
                  height: 48,
                  child: IndividualMapMarker(
                    entity: row(
                      'owned-map:${i == 0 ? 'place' : 'moment'}:test',
                      kind: i == 0 ? MapEntityKind.place : MapEntityKind.moment,
                    ),
                    selected: false,
                    showLabel: true,
                    onTap: () => selected.add(i == 0 ? 'place' : 'moment'),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
    final place = find.bySemanticsIdentifier(
      'birdtie-map-point:owned-map:place:test',
    );
    final moment = find.bySemanticsIdentifier(
      'birdtie-map-point:owned-map:moment:test',
    );
    expect(place, findsOneWidget);
    expect(moment, findsOneWidget);
    final placeNode = t.getSemantics(place),
        momentNode = t.getSemantics(moment);
    expect(placeNode.rect.size, const Size(48, 48));
    expect(momentNode.rect.size, const Size(48, 48));
    expect(placeNode.getSemanticsData().label, contains('地点 · 合成内容'));
    expect(momentNode.getSemanticsData().label, contains('动态 · 合成内容'));
    await t.tapAt(const Offset(124, 174));
    expect(selected, ['moment']);
    t.binding.pipelineOwner.semanticsOwner!.performAction(
      momentNode.id,
      ui.SemanticsAction.tap,
    );
    expect(selected, ['moment', 'moment']);
    t.binding.pipelineOwner.semanticsOwner!.performAction(
      placeNode.id,
      ui.SemanticsAction.tap,
    );
    expect(selected, ['moment', 'moment', 'place']);
    semantics.dispose();
  });

  test(
    'bounded latest render, crossing viewport and complete independent task points',
    () async {
      final gate = Completer<void>(), serials = <int>[];
      final queue = LatestMapRenderQueue((serial) async {
        serials.add(serial);
        if (serial == 1) await gate.future;
      });
      final first = queue.schedule();
      for (var i = 0; i < 1000; i++) queue.schedule();
      gate.complete();
      await first;
      expect(serials, [1, 1001]);
      expect(queue.peakPending, 1);
      expect(queue.coalesced, 999);
      queue.close();
      await queue.schedule();
      expect(serials.length, 2);
      final crossing = MapDisplayPolicy.projectedViewport(
        corners: const [
          Offset(179, 51),
          Offset(-179, 50),
          Offset(-178, 48),
          Offset(178, 49),
        ],
        center: const Offset(180, 49.5),
      )!;
      expect(crossing.west, 178);
      expect(crossing.east, -178);
      expect(crossing.north, 51);
      expect(crossing.south, 48);
      expect(
        MapDisplayPolicy.projectedViewport(
          corners: const [],
          center: Offset.zero,
        ),
        isNull,
      );
      expect(mapboxProviderPoiLayer('poi-label', 'symbol'), isTrue);
      expect(
        mapboxProviderPoiLayer(
          'custom-icons',
          'symbol',
          sourceLayer: 'poi_label',
        ),
        isTrue,
      );
      for (final id in [
        'road-label',
        'place-label',
        'airport-label',
        'point-annotations',
      ]) {
        expect(mapboxProviderPoiLayer(id, 'symbol'), isFalse);
      }
      expect(mapboxProviderPoiLayer('poi-fill', 'fill'), isFalse);
      for (final count in [100, 1000, 10000]) {
        final source = [
          for (var i = 0; i < count; i++)
            row('task:$i', lng: -2.1 + i / 100000),
        ];
        final positions = {
          for (var i = 0; i < count; i++)
            source[i].id: Offset((i % 100) * 12.0, (i ~/ 100) * 12.0),
        };
        final plan = MapDisplayPolicy.plan(
          entities: source,
          positions: positions,
          zoom: 12,
          selectedID: source.last.id,
          protectedIDs: source.map((e) => e.id).toSet(),
        );
        expect(plan.entities.length, count);
        expect(plan.entities.any(MapDisplayPolicy.displayCluster), isFalse);
        expect(plan.entities.any((e) => e.id == source.last.id), isTrue);
        final reachable = <String>{};
        for (final entity in plan.entities) {
          if (MapDisplayPolicy.displayCluster(entity)) {
            final members = MapDisplayPolicy.currentDisplayClusterMembers(
              entity,
              source,
            );
            expect(members.length, entity.memberIDs.length);
            reachable.addAll(members.map((e) => e.id));
            expect(
              MapDisplayPolicy.currentDisplayClusterMembers(
                entity,
                source.where((e) => e.id != members.first.id),
              ),
              isEmpty,
            );
          } else {
            reachable.add(entity.id);
          }
        }
        expect(reachable.length, count);
        expect(
          positions.length,
          count,
          reason: 'projection input remains unchanged',
        );
      }
      final content = [
        row('owned-map:moment:a'),
        row('owned-map:place:b', kind: MapEntityKind.place),
      ];
      final plan = MapDisplayPolicy.plan(
        entities: content,
        positions: {for (final e in content) e.id: const Offset(50, 50)},
        zoom: 3,
      );
      expect(plan.denseIDs, content.map((e) => e.id).toSet());
      expect(plan.entities.map((e) => e.id), content.map((e) => e.id));
    },
  );

  testWidgets(
    'actual Web content overlap and server cluster preserve one map and dispatch to Now',
    (t) async {
      final changes = ChangeNotifier(), camera = MapController();
      var source = [row('owned-map:moment:a'), row('owned-map:moment:b')];
      List<MapEntity>? overlap;
      MapEntity? selected;
      Rect contentRect = const _ProfileRect(0, 0, 800, 600);
      Future<void> mount() => t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: FlutterMap(
              key: const ValueKey('persistent-content-map'),
              mapController: camera,
              options: const MapOptions(
                initialCenter: LatLng(57.14, -2.1),
                initialZoom: 13,
              ),
              children: [
                WebMapDisplayLayer(
                  places: const [],
                  entities: source,
                  selectedID: null,
                  contentRect: contentRect,
                  revision: 1,
                  readerChanges: changes,
                  currentReaderEntities: () => source,
                  currentReaderScope: () => 1,
                  onSelected: (e) => selected = e,
                  onContentOverlap: (e) => overlap = e,
                ),
              ],
            ),
          ),
        ),
      );
      await mount();
      await t.pumpAndSettle();
      final element = find.byType(FlutterMap).evaluate().single;
      final center = camera.camera.center;
      // Profile/release delete dart:ui toString overrides. The actual layer
      // must invalidate coverage even when both rectangles stringify alike.
      const covered = _ProfileRect(0, 0, 800, 0);
      expect(contentRect.toString(), covered.toString());
      for (final e in source) {
        expect(
          find.byKey(ValueKey('individual-map-target:${e.id}')),
          findsOneWidget,
        );
      }
      contentRect = covered;
      await mount();
      await t.pumpAndSettle();
      for (final e in source) {
        expect(
          find.byKey(ValueKey('individual-map-target:${e.id}')),
          findsNothing,
        );
      }
      contentRect = const _ProfileRect(0, 0, 800, 600);
      await mount();
      await t.pumpAndSettle();
      for (final e in source) {
        expect(
          find.byKey(ValueKey('individual-map-target:${e.id}')),
          findsOneWidget,
        );
      }

      await t.tap(
        find.byKey(const ValueKey('individual-map-target:owned-map:moment:b')),
      );
      await t.pumpAndSettle();
      expect(
        overlap?.map((e) => e.id).toSet(),
        source.map((e) => e.id).toSet(),
      );
      expect(find.byKey(const Key('current-map-point-choices')), findsNothing);
      source = [row('owned-map:cluster:one', kind: MapEntityKind.cluster)];
      await mount();
      await t.pumpAndSettle();
      final layer = t.widget<MapEntityLayer>(find.byType(MapEntityLayer));
      layer.onSelected(source.single);
      await t.pumpAndSettle();
      expect(selected?.id, source.single.id);
      expect(
        identical(element, find.byType(FlutterMap).evaluate().single),
        isTrue,
      );
      expect(camera.camera.center, center);
      expect(
        find.byKey(const Key('current-map-cluster-members')),
        findsNothing,
      );
      await t.pumpWidget(const SizedBox());
      changes.dispose();
      camera.dispose();
    },
  );

  testWidgets(
    'actual Native touch and initial screen viewport use content routing without replacing annotations',
    (t) async {
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
        () => t.binding.defaultBinaryMessenger.setMockMessageHandler(
          channel,
          null,
        ),
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
      final map = _Map();
      PublicCity? city;
      var focusRevision = 0;
      var source = [row('owned-map:moment:a'), row('owned-map:moment:b')];
      List<MapEntity>? overlap;
      MapEntity? selected;
      final viewports = <(MapBounds, double)>[];
      await t.runAsync(() async {
        await MapMarkerBitmap.render(
          kind: MapEntityKind.moment,
          selected: false,
          dense: true,
        );
        await MapMarkerBitmap.render(
          kind: MapEntityKind.moment,
          selected: false,
        );
        await MapMarkerBitmap.render(
          kind: MapEntityKind.cluster,
          selected: false,
          approximate: true,
          count: 0,
        );
      });
      late ui.Image photo;
      late Uint8List normal, highlighted;
      var oldGrant = true;
      await t.runAsync(() async {
        final recorder = ui.PictureRecorder();
        Canvas(recorder).drawColor(Colors.teal, BlendMode.src);
        final picture = recorder.endRecording();
        photo = await picture.toImage(16, 16);
        picture.dispose();
        normal = await MapMarkerBitmap.render(
          kind: MapEntityKind.moment,
          selected: false,
          photo: photo,
        );
        highlighted = await MapMarkerBitmap.render(
          kind: MapEntityKind.moment,
          selected: true,
          photo: photo,
        );
      });
      addTearDown(photo.dispose);
      MapMediaPixels pixels(bool Function() current) => MapMediaPixels(
        key: 'synthetic-media:revision-1',
        image: photo,
        normalPNG: normal,
        selectedPNG: highlighted,
        devicePixelRatio: 1,
        current: current,
      );
      source = [
        source.first.withMediaPixels(pixels(() => oldGrant)),
        source.last,
      ];
      Future<void> mount() => t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NativeCityMapView(
              city: city,
              contextKey: 'idle',
              focusRevision: focusRevision,
              places: const [],
              entities: source,
              onPlaceSelected: (_) {},
              currentReaderEntities: () => source,
              currentReaderScope: () => 1,
              accessToken: 'pk.synthetic-unit-only',
              contentRect: const Rect.fromLTRB(0, 50, 430, 350),
              onEntitySelected: (e) => selected = e,
              onContentOverlap: (e) => overlap = e,
              onContentViewportSettled: (bounds, zoom) =>
                  viewports.add((bounds, zoom)),
            ),
          ),
        ),
      );
      await mount();
      final sdk = t.widget<MapWidget>(find.byType(MapWidget));
      sdk.onMapCreated!(map);
      sdk.onStyleLoadedListener!(
        StyleLoadedEventData.fromJson({
          'timeInterval': {'begin': 0, 'end': 1},
        }),
      );
      await t.pumpAndSettle();
      await t.pump(const Duration(milliseconds: 300));
      await t.pumpAndSettle();
      for (var i = 0; i < 10 && viewports.isEmpty; i++) {
        await t.pump(const Duration(milliseconds: 100));
      }
      expect(map.points.creates, 2);
      expect(viewports, hasLength(1));
      expect(viewports.single.$1.west, 179);
      expect(viewports.single.$1.east, -179);
      expect(viewports.single.$2, 12);
      expect(map.capture!.map((p) => (p!.x, p.y)).toList(), [
        (0.0, 50.0),
        (430.0, 50.0),
        (430.0, 350.0),
        (0.0, 350.0),
        (215.0, 200.0),
      ]);
      void tap() => t.widget<MapWidget>(find.byType(MapWidget)).onTapListener!(
        MapContentGestureContext(
          touchPosition: ScreenCoordinate(x: 100, y: 200),
          point: Point(coordinates: Position(-2.1, 57.14)),
          gestureState: GestureState.ended,
        ),
      );
      tap();
      await t.pumpAndSettle();
      expect(overlap, hasLength(2));
      expect(find.byKey(const Key('current-map-point-choices')), findsNothing);
      final sdkElement = find.byType(MapWidget).evaluate().single;
      await mount();
      await t.pumpAndSettle();
      expect(map.points.creates, 2);
      expect(map.points.updates, 0);
      expect(
        identical(sdkElement, find.byType(MapWidget).evaluate().single),
        isTrue,
      );
      // A fresh permission lease replaces the selector, not the bitmap.
      // The old selector is already invalid when the renderer observes it.
      oldGrant = false;
      source = [
        row('owned-map:moment:a').withMediaPixels(pixels(() => true)),
        source.last,
      ];
      await mount();
      await t.pumpAndSettle();
      expect(map.points.creates, 2);
      expect(
        map.points.updates,
        0,
        reason:
            'Same media/version with a fresh grant must not flash fallback or upload again.',
      );
      source = [row('owned-map:cluster:one', kind: MapEntityKind.cluster)];
      await mount();
      await t.pumpAndSettle();
      tap();
      await t.pumpAndSettle();
      expect(selected?.id, source.single.id);
      expect(
        find.byKey(const Key('current-map-cluster-members')),
        findsNothing,
      );
      PublicCity focusCity(String cityID, double longitude) => PublicCity(
        id: cityID,
        name: '合成城市',
        region: '',
        contentStatus: 'ready',
        source: const PublicSource(
          label: 'unit',
          maintainer: 'unit',
          freshness: 'unit',
          updatedAt: null,
        ),
        map: PublicCityMap(
          provider: 'mapbox',
          latitude: 50,
          longitude: longitude,
          defaultZoom: 12,
          sourceRef: 'unit',
        ),
      );
      final beforeFocus = viewports.length;
      city = focusCity('first', 20);
      await mount();
      await t.pump();
      expect(map.flies, 1);
      await t.pump(const Duration(milliseconds: 250));
      expect(
        viewports.length,
        beforeFocus,
        reason: 'Do not query the middle of a city flyTo.',
      );
      await t.pump(const Duration(milliseconds: 550));
      await t.pump();
      expect(viewports.length, beforeFocus + 1);
      expect(viewports.last.$1.west, 19);
      expect(viewports.last.$1.east, 21);
      city = focusCity('second', 40);
      await mount();
      await t.pump(const Duration(milliseconds: 100));
      final touch = t
          .widgetList<Listener>(
            find.descendant(
              of: find.byType(NativeCityMapView),
              matching: find.byType(Listener),
            ),
          )
          .firstWhere((w) => w.onPointerDown != null);
      touch.onPointerDown!(const PointerDownEvent());
      map.userMove(60);
      final liveSDK = t.widget<MapWidget>(find.byType(MapWidget));
      liveSDK.onCameraChangeListener!(
        CameraChangedEventData.fromJson({
          'timestamp': 1,
          'cameraState': {
            'center': {
              'type': 'Point',
              'coordinates': [60.0, 50.0],
            },
            'zoom': 12.0,
            'bearing': 0.0,
            'pitch': 0.0,
            'padding': {'top': 0.0, 'left': 0.0, 'right': 0.0, 'bottom': 0.0},
          },
        }),
      );
      liveSDK.onMapIdleListener!(MapIdleEventData.fromJson({'timestamp': 2}));
      await t.pump();
      expect(viewports.length, beforeFocus + 2);
      expect(viewports.last.$1.west, 59);
      await t.pump(const Duration(milliseconds: 800));
      await t.pump();
      expect(
        viewports.length,
        beforeFocus + 2,
        reason: 'Superseded city focus cannot publish over a user gesture.',
      );
      focusRevision++;
      await mount();
      await t.pump(const Duration(milliseconds: 800));
      await t.pump();
      expect(
        viewports.length,
        beforeFocus + 3,
        reason: 'Explicit focus publishes its final real viewport.',
      );
      expect(viewports.last.$1.west, 39);
      expect(
        identical(sdkElement, find.byType(MapWidget).evaluate().single),
        true,
      );
      await t.pumpWidget(const SizedBox());
    },
  );

  test(
    'actual JS vector style hides only provider POI and keeps roads and map instance',
    () async {
      final process = await Process.run(
        Platform.environment['BIRDTIE_TEST_NODE'] ?? 'node',
        ['-e', _poiHarness, File('web/birdtie_mapbox.js').absolute.path],
      );
      expect(process.exitCode, 0, reason: process.stderr.toString());
      expect(process.stdout.toString().trim(), 'PROVIDER_POI_PASS');
    },
  );
}

class _Points extends Fake implements PointAnnotationManager {
  int creates = 0, updates = 0;
  @override
  Future<void> setIconAllowOverlap(bool value) async {}
  @override
  Future<void> setIconIgnorePlacement(bool value) async {}
  @override
  Future<void> setTextAllowOverlap(bool value) async {}
  @override
  Future<void> setTextIgnorePlacement(bool value) async {}
  @override
  Future<void> setTextOptional(bool value) async {}
  @override
  Future<PointAnnotation> create(PointAnnotationOptions option) async {
    creates++;
    return PointAnnotation(
      id: 'annotation-$creates',
      geometry: option.geometry,
      image: option.image,
      iconAnchor: option.iconAnchor,
      iconSize: option.iconSize,
      customData: option.customData,
      textField: option.textField,
    );
  }

  @override
  Future<void> update(PointAnnotation annotation) async {
    updates++;
  }

  @override
  Future<void> delete(PointAnnotation annotation) async {}
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
  int flies = 0, motion = 0;
  double? longitude;
  void userMove(double value) {
    motion++;
    longitude = value;
  }

  final points = _Points();
  List<ScreenCoordinate?>? capture;
  @override
  AnnotationManager get annotations => _Annotations(points);
  @override
  Future<CameraState> getCameraState() async => CameraState(
    center: Point(coordinates: Position(longitude ?? 180, 50)),
    zoom: 12,
    bearing: 20,
    pitch: 0,
    padding: MbxEdgeInsets(top: 0, left: 0, right: 0, bottom: 0),
  );
  @override
  Future<List<ScreenCoordinate?>> pixelsForCoordinates(
    List<Point> coordinates,
  ) async => [for (final _ in coordinates) ScreenCoordinate(x: 100, y: 200)];
  @override
  Future<List<Point?>> coordinatesForPixels(
    List<ScreenCoordinate?> pixels,
  ) async {
    if (pixels.length == 2) {
      return [
        for (final pixel in pixels.whereType<ScreenCoordinate>())
          Point(coordinates: Position(-2.1 + pixel.x / 60000, 57.14)),
      ];
    }
    capture = pixels;
    if (longitude case final lon?)
      return [
        for (final offset in [
          Offset(lon - 1, 51),
          Offset(lon + 1, 51),
          Offset(lon + 1, 49),
          Offset(lon - 1, 49),
          Offset(lon, 50),
        ])
          Point(coordinates: Position(offset.dx, offset.dy)),
      ];
    return [
      for (final p in const [
        Offset(179, 51),
        Offset(-179, 51),
        Offset(-179, 49),
        Offset(179, 49),
        Offset(180, 50),
      ])
        Point(coordinates: Position(p.dx, p.dy)),
    ];
  }

  @override
  Future<CoordinateBounds> coordinateBoundsForCamera(
    CameraOptions camera,
  ) async => CoordinateBounds(
    southwest: Point(coordinates: Position(179, 49)),
    northeast: Point(coordinates: Position(-179, 51)),
    infiniteBounds: false,
  );
  @override
  Future<void> flyTo(
    CameraOptions camera,
    MapAnimationOptions? animation,
  ) async {
    final serial = ++motion;
    final target = camera.center!.coordinates.lng.toDouble();
    flies++;
    longitude = ((longitude ?? 180) + target) / 2;
    Timer(const Duration(milliseconds: 450), () {
      if (serial == motion) longitude = target;
    });
  }
}

const _poiHarness = r'''
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const created = [], hidden = [], layers = [
  {id:'poi-label',type:'symbol'}, {id:'custom',type:'symbol','source-layer':'poi_label'},
  {id:'road-label',type:'symbol'}, {id:'place-label',type:'symbol'},
  {id:'poi-fill',type:'fill'}, {id:'point-annotations',type:'symbol'}];
class Map {
  constructor(options) { this.options=options; this.listeners={}; created.push(this); }
  on(kind,fn) { this.listeners[kind]=fn; } jumpTo(value) { this.camera=value; }
  getStyle() { return {layers}; } setLayoutProperty(id,key,value) { hidden.push([id,key,value]); }
  resize() {} remove() {}
}
const element={isConnected:true,clientWidth:430,clientHeight:900,style:{}};
const window={mapboxgl:{Map,supported:()=>true}}, document={
  createElement:()=>({remove(){}}), getElementById:()=>element,
  head:{appendChild:el=>queueMicrotask(()=>el.onload())}};
class ResizeObserver { observe() {} disconnect() {} }
vm.runInNewContext(fs.readFileSync(process.argv[1],'utf8'),
  {window,document,ResizeObserver,setTimeout,clearTimeout,performance});
(async()=>{
  await window.birdtieMapboxMount('map',JSON.stringify({accessToken:'pk.synthetic',
    style:'mapbox://styles/synthetic',camera:{center:[179,50],zoom:12,bearing:0,pitch:0}}),()=>{});
  const map=created[0]; map.listeners.load();
  assert.deepEqual(hidden.map(v=>v[0]),['poi-label','custom']);
  assert(hidden.every(v=>v[1]==='visibility'&&v[2]==='none'));
  window.birdtieMapboxUpdate('map',JSON.stringify({center:[-179,50],zoom:13,bearing:20,pitch:0}));
  assert.equal(created.length,1); assert.equal(map.options.attributionControl,false);
  window.birdtieMapboxDestroy('map'); console.log('PROVIDER_POI_PASS');
})().catch(e=>{console.error(e.message);process.exitCode=1});
''';

class _ProfileRect extends Rect {
  const _ProfileRect(double left, double top, double right, double bottom)
    : super.fromLTRB(left, top, right, bottom);
  @override
  String toString() => "Instance of 'Rect'";
}
