import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/browse_destination.dart';
import 'package:birdtie_client/src/city/exploration_city_store.dart';
import 'package:birdtie_client/src/city/map_camera_focus.dart';
import 'package:birdtie_client/src/city/map_navigation_target.dart';
import 'package:birdtie_client/src/city/map_route_region.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'map_route_region_test.dart' show fixture, unit;

// Isolated navigation fixtures only: no production IDs, requests or GPS facts.
class CameraMemory implements ExplorationCityStore, ExplorationCameraStore {
  String? city;
  final cameras = <String, MapCameraSnapshot>{};
  final writes = <MapNavigationTarget>[];
  Completer<MapCameraSnapshot?>? gate;
  @override
  Future<String?> readCityID() async => city;
  @override
  Future<void> writeCityID(String value) async {
    city = value;
  }

  @override
  Future<MapCameraSnapshot?> readCamera(String id) async =>
      gate == null ? cameras[id] : await gate!.future;
  @override
  Future<void> writeCamera(MapNavigationTarget target) async {
    expect(target.canPersistCamera, isTrue);
    writes.add(target);
    cameras[target.catalogCityID!] = target.camera;
  }
}

class PendingMapSource extends AgentTaskSource {
  final pending = Completer<AgentResult>();
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => pending.future;
}

Map<String, Object?> city(
  String id,
  String country,
  double lat,
  double lon, {
  bool mapped = true,
  String provider = 'mapbox',
}) => {
  'id': id,
  'name': '隔离$id',
  'region': country,
  'countryCode': country,
  'contentStatus': 'test',
  'source': {
    'label': 'unit fixture',
    'maintainer': 'unit',
    'freshness': 'test',
  },
  if (mapped)
    'map': {
      'provider': provider,
      'latitude': lat,
      'longitude': lon,
      'defaultZoom': 12,
      'sourceRef': 'unit://canonical',
    },
};

PublicCityController create(CameraMemory store) {
  final index = MapRouteRegionIndex.fromJson(
    fixture([
      unit('CHN', [
        [(100, 20), (130, 20), (130, 40), (100, 40), (100, 20)],
      ]),
      unit('GBR', [
        [(-10, 50), (5, 50), (5, 60), (-10, 60), (-10, 50)],
      ]),
    ]),
  );
  final client = MockClient(
    (request) async => http.Response(
      jsonEncode({
        'data': request.url.path == '/v1/cities'
            ? [
                city('aberdeen-gb', 'GB', 57, -2),
                city('unit-cn', 'CN', 31, 121, provider: 'mapbox'),
                city('unmapped', 'GB', 55, -3, mapped: false),
              ]
            : [],
      }),
      200,
      headers: {'content-type': 'application/json; charset=utf-8'},
    ),
  );
  addTearDown(client.close);
  final c = PublicCityController(
    client: client,
    apiBaseUrl: 'https://fixture.invalid',
    explorationStore: store,
    routeRegions: index,
  );
  addTearDown(c.dispose);
  return c;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('view cancellation cannot resume a detached retained Task layer', () {
    final ws = AgentWorkspaceController();
    addTearDown(ws.dispose);
    const task = AgentTask(
      id: 'isolated-task',
      query: 'retained',
      status: 'COMPLETED',
    );
    const result = AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: 'retained',
    );
    ws.task = task;
    ws.result = result;
    ws.selectEntity('place:old');
    ws.detachTaskMapProjection();
    ws.retirePendingQueryForViewChange();
    expect(ws.taskMapProjectionCurrent, isFalse);
    expect(ws.task, same(task));
    expect(ws.result, same(result));
    expect(ws.selectedEntityId, isNull);
    ws.queryContextType = 'ONLINE';
    ws.newTask(preserveMapSelection: true);
    expect(ws.taskMapProjectionCurrent, isTrue);
    expect(ws.mapBackgroundResult, isNull);
  });
  testWidgets(
    'explicit same-provider target retires old Task map without deleting its conversation',
    (t) async {
      final c = create(CameraMemory());
      await c.loadCities();
      final ws = AgentWorkspaceController();
      final viewport = MapViewportState();
      final discovery = NowDiscoveryController(authorizationHeader: () => null);
      addTearDown(ws.dispose);
      addTearDown(viewport.dispose);
      addTearDown(discovery.dispose);
      const task = AgentTask(
        id: 'isolated-retained-task',
        cityID: 'aberdeen-gb',
        query: 'retained',
        status: 'COMPLETED',
      );
      final old = MapEntity(
        id: 'place:old',
        kind: MapEntityKind.place,
        title: 'old',
        subtitle: '',
        latitude: 57,
        longitude: -2,
      );
      final result = AgentResult(
        entities: [old],
        activities: const [],
        places: const [],
        note: 'retained',
      );
      ws.task = task;
      ws.result = result;
      ws.conversation.add(const AgentMessage(role: 'user', text: '保留的原会话'));
      ws.selectEntity(old.id);
      await t.pumpWidget(
        MaterialApp(
          home: MapCanvas(
            city: c,
            workspace: ws,
            mapState: viewport,
            discovery: discovery,
            onInitialViewport: (_) {},
            onMapUnavailable: (_) {},
          ),
        ),
      );
      expect(
        t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).contextKey,
        task.id,
      );
      c.navigateMapTarget(
        const MapNavigationTarget(
          key: 'isolated-new-moment',
          source: MapNavigationSource.moment,
          generation: 0,
          camera: MapCameraSnapshot(latitude: 55, longitude: -3, zoom: 15),
          countryCode: 'GB',
          sourceVersion: 'isolated-current',
        ),
      );
      ws.retirePendingQueryForViewChange();
      await t.pump();
      final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      expect(view.contextKey, 'idle');
      expect(view.selectedEntityId, isNull);
      expect(view.entities, isEmpty);
      final focus = MapCameraFocusInput(
        city: view.city,
        contextKey: view.contextKey,
        places: view.places,
        entities: view.entities,
        selectedEntityId: view.selectedEntityId,
      );
      expect(
        (focus.center!.latitude, focus.center!.longitude, focus.zoom),
        (55, -3, 15),
      );
      expect(ws.task, same(task));
      expect(ws.result, same(result));
      expect(ws.conversation.single.text, '保留的原会话');
      expect(c.contentCityID, '');
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets('same-city explicit navigation fences a pending Task response', (
    t,
  ) async {
    final c = create(CameraMemory());
    await c.loadCities();
    final source = PendingMapSource();
    final ws = AgentWorkspaceController(source: source);
    final viewport = MapViewportState();
    final discovery = NowDiscoveryController(authorizationHeader: () => null);
    addTearDown(ws.dispose);
    addTearDown(viewport.dispose);
    addTearDown(discovery.dispose);
    const oldTask = AgentTask(
      id: 'isolated-pending-old',
      cityID: 'aberdeen-gb',
      query: 'retained',
      status: 'COMPLETED',
    );
    const oldResult = AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: 'old',
    );
    ws.task = oldTask;
    ws.result = oldResult;
    await t.pumpWidget(
      MaterialApp(
        home: MapCanvas(
          city: c,
          workspace: ws,
          mapState: viewport,
          discovery: discovery,
          onInitialViewport: (_) {},
          onMapUnavailable: (_) {},
        ),
      ),
    );
    final request = ws.submit('pending', [], [], cityID: 'aberdeen-gb');
    c.navigateMapTarget(
      const MapNavigationTarget(
        key: 'isolated-same-city',
        source: MapNavigationSource.moment,
        generation: 0,
        catalogCityID: 'aberdeen-gb',
        camera: MapCameraSnapshot(latitude: 57.1, longitude: -2.1, zoom: 15),
        countryCode: 'GB',
        sourceVersion: 'isolated-current',
      ),
    );
    source.pending.complete(
      const AgentResult(entities: [], activities: [], places: [], note: 'late'),
    );
    await request;
    await t.pump();
    expect(ws.taskMapProjectionCurrent, isFalse);
    expect(ws.task, same(oldTask));
    expect(ws.result, same(oldResult));
    final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(view.contextKey, 'idle');
    expect(view.city!.map!.latitude, 57.1);
    ws.queryContextType = 'ONLINE';
    ws.newTask(preserveMapSelection: true);
    await t.pump();
    expect(ws.mapBackgroundResult, isNull);
    expect(
      t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).contextKey,
      'idle',
    );
    await t.pumpWidget(const SizedBox());
  });
  test(
    'uncatalogued reference gets renderer only camera and retires on identity change',
    () async {
      final store = CameraMemory();
      final c = create(store);
      await c.loadCities();
      c.navigateMapTarget(
        const MapNavigationTarget(
          key: 'isolated-reference',
          source: MapNavigationSource.moment,
          generation: 0,
          camera: MapCameraSnapshot(latitude: 55, longitude: -3, zoom: 14),
          countryCode: 'GB',
          sourceVersion: 'isolated-reference-current',
        ),
      );
      expect(c.selectedCity, isNull);
      expect(c.renderCity!.map!.latitude, 55);
      expect(c.contentCityID, '');
      expect(c.cities.any((x) => x.id.startsWith('view:')), isFalse);
      await c.loadCities();
      expect(c.selectedCity, isNull);
      expect(c.renderCity!.map!.latitude, 55);
      c.mapCameraSettled(
        const MapCameraSnapshot(latitude: 55, longitude: -3, zoom: 14),
        c.navigationGeneration,
        longitudeSpan: .1,
        latitudeSpan: .1,
      );
      expect(store.writes, isEmpty);
      c.retireContentMapTarget();
      expect(c.mapTarget!.source, MapNavigationSource.catalogCity);
      expect(c.renderCity!.map!.latitude, 57);
      expect(store.writes, isEmpty);
    },
  );
  test(
    'Task city view does not borrow or persist exploration camera',
    () async {
      final store = CameraMemory();
      store.cameras['aberdeen-gb'] = const MapCameraSnapshot(
        latitude: 56,
        longitude: -4,
        zoom: 9,
      );
      final c = create(store);
      await c.loadCities();
      c.selectCity('aberdeen-gb', rememberExploration: false);
      expect(c.mapTarget!.source, MapNavigationSource.task);
      expect(c.renderCity!.map!.latitude, 57);
      c.mapCameraSettled(
        const MapCameraSnapshot(latitude: 55, longitude: -3, zoom: 14),
        c.navigationGeneration,
        longitudeSpan: .1,
        latitudeSpan: .1,
      );
      expect(store.writes, isEmpty);
    },
  );
  test(
    'structured destination overrides obsolete provider and fences A B A callbacks',
    () async {
      final c = create(CameraMemory());
      await c.loadCities();
      final first = c.navigationGeneration;
      c.selectCity('unit-cn');
      expect(c.mapProvider, 'amap');
      expect(c.hasUsableMap(c.selectedCity!), isTrue);
      final middle = c.navigationGeneration;
      c.selectCity('aberdeen-gb');
      expect(c.mapProvider, 'mapbox');
      expect(c.navigationGeneration, greaterThan(middle));
      expect(middle, greaterThan(first));
      c.markMapPresented(first);
      expect(c.displayedGeneration, isNot(first));
      final latest = c.navigationGeneration;
      c.mapCameraSettled(
        const MapCameraSnapshot(latitude: 31, longitude: 121, zoom: 16),
        middle,
        longitudeSpan: .1,
        latitudeSpan: .1,
      );
      expect(c.navigationGeneration, latest);
      expect(c.renderCity!.map!.latitude, 57);
    },
  );
  test(
    'catalog camera restores bearing and zoom while same-city explicit focus advances version',
    () async {
      final store = CameraMemory();
      final persisted = create(store);
      await persisted.loadCities();
      final generation = persisted.navigationGeneration;
      persisted.mapCameraSettled(
        const MapCameraSnapshot(
          latitude: 56.8,
          longitude: -2.4,
          zoom: 13.3,
          bearing: 45,
          pitch: 20,
        ),
        generation,
        longitudeSpan: .1,
        latitudeSpan: .1,
      );
      persisted.selectCity('unit-cn');
      persisted.selectCity('aberdeen-gb');
      expect(persisted.renderCity!.map!.latitude, 56.8);
      expect(persisted.renderCity!.map!.bearing, 45);
      expect(persisted.renderCity!.map!.defaultZoom, 13.3);
      final old = persisted.navigationGeneration;
      persisted.selectCity('aberdeen-gb');
      expect(persisted.navigationGeneration, greaterThan(old));
      expect(store.writes, hasLength(1));
    },
  );
  test('late camera read cannot undo a real camera gesture', () async {
    final gate = Completer<MapCameraSnapshot?>();
    final store = CameraMemory()..gate = gate;
    final c = create(store);
    await c.loadCities();
    c.mapCameraStarted(c.navigationGeneration);
    gate.complete(
      const MapCameraSnapshot(latitude: 56, longitude: -4, zoom: 9),
    );
    await Future<void>.delayed(Duration.zero);
    expect(c.renderCity!.map!.latitude, 57);
  });
  test(
    'temporary catalog resolution and explicit content never persist their geometry',
    () async {
      final store = CameraMemory();
      final tracked = create(store);
      await tracked.loadCities();
      expect(
        tracked.selectDestination(
          const BrowseDestination(
            provider: 'amap',
            externalID: 'temporary-unit',
            name: '隔离目标',
            countryCode: 'CN',
            region: '',
            latitude: 31.1,
            longitude: 121.2,
            featureType: 'place',
            attribution: 'unit only',
            storageMode: DestinationStorageMode.temporary,
            internalCityID: 'unit-cn',
          ),
        ),
        isTrue,
      );
      tracked.mapCameraSettled(
        const MapCameraSnapshot(latitude: 31.2, longitude: 121.3, zoom: 15),
        tracked.navigationGeneration,
        longitudeSpan: .1,
        latitudeSpan: .1,
      );
      expect(store.writes, isEmpty);
      expect(
        tracked.navigateMapTarget(
          const MapNavigationTarget(
            key: 'unit-moment',
            source: MapNavigationSource.moment,
            generation: 0,
            camera: MapCameraSnapshot(latitude: 57, longitude: -2, zoom: 14),
            sourceVersion: 'unit-current',
            countryCode: 'GB',
            catalogCityID: 'aberdeen-gb',
          ),
        ),
        isTrue,
      );
      tracked.mapCameraSettled(
        const MapCameraSnapshot(latitude: 57, longitude: -2, zoom: 14),
        tracked.navigationGeneration,
        longitudeSpan: .1,
        latitudeSpan: .1,
      );
      expect(store.writes, isEmpty);
    },
  );
  test(
    'unmapped city retires previous renderer target instead of borrowing old camera',
    () async {
      final c = create(CameraMemory());
      await c.loadCities();
      final generation = c.navigationGeneration;
      c.selectCity('unmapped', rememberExploration: false);
      expect(c.navigationGeneration, greaterThan(generation));
      expect(c.mapTarget, isNull);
      expect(c.renderCity!.id, 'unmapped');
      expect(c.renderCity!.map, isNull);
    },
  );
  test(
    'stable foreign viewport switches after hysteresis and carries real coverage into new SDK',
    () async {
      final c = create(CameraMemory());
      await c.loadCities();
      c.selectCity('unit-cn');
      final before = c.navigationGeneration;
      c.mapCameraSettled(
        const MapCameraSnapshot(latitude: 57, longitude: -2, zoom: 12),
        before,
        longitudeSpan: .25,
        latitudeSpan: .15,
      );
      expect(c.mapProvider, 'amap');
      await Future<void>.delayed(const Duration(milliseconds: 820));
      expect(c.mapProvider, 'mapbox');
      expect(c.navigationGeneration, greaterThan(before));
      expect(
        c.renderCity!.map!.cameraBounds!.longitudeSpan,
        closeTo(.25, 1e-8),
      );
      final after = c.navigationGeneration;
      c.mapCameraSettled(
        const MapCameraSnapshot(latitude: 0, longitude: -140, zoom: 3),
        after,
        longitudeSpan: 180,
        latitudeSpan: 100,
      );
      await Future<void>.delayed(const Duration(milliseconds: 820));
      expect(c.mapProvider, 'mapbox');
      expect(c.navigationGeneration, after);
    },
  );
  test(
    'viewport camera fit uses frame points while preserving selected entity identity',
    () {
      final view = PublicCity(
        id: 'unit',
        name: 'unit',
        region: '',
        contentStatus: 'test',
        source: const PublicSource(
          label: 'unit',
          maintainer: 'unit',
          freshness: 'test',
          updatedAt: null,
        ),
        map: const PublicCityMap(
          provider: 'mapbox',
          latitude: 57,
          longitude: -2,
          defaultZoom: 12,
          sourceRef: 'unit',
          navigationGeneration: 3,
          bearing: 35,
          cameraBounds: MapBounds(
            west: -2.2,
            south: 56.9,
            east: -1.8,
            north: 57.1,
          ),
        ),
      );
      final focus = MapCameraFocusInput(
        city: view,
        contextKey: 'idle',
        places: const [],
        entities: [
          MapEntity(
            id: 'place:old',
            kind: MapEntityKind.place,
            title: 'old',
            subtitle: '',
            latitude: 31,
            longitude: 121,
          ),
        ],
        selectedEntityId: 'place:old',
      );
      expect(focus.fitsBounds, isTrue);
      expect(focus.points, hasLength(2));
      expect(focus.points.every((p) => p.longitude < 0), isTrue);
      expect(focus.selectedEntityId, 'place:old');
      expect(focus.bearing, 35);
    },
  );
}
