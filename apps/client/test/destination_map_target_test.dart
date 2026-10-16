import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/browse_destination.dart';
import 'package:birdtie_client/src/city/exploration_city_store.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'content_map_controller_test.dart' as content;
import 'destination_search_test.dart' show destination;

class PendingStore implements ExplorationCityStore {
  final read = Completer<String?>();
  final writes = <String>[];
  @override
  Future<String?> readCityID() => read.future;
  @override
  Future<void> writeCityID(String cityID) async => writes.add(cityID);
}

Map<String, dynamic> catalogCity() => {
  'id': 'unit-catalog',
  'name': 'Synthetic catalog',
  'region': 'Unit',
  'countryCode': 'GB',
  'contentStatus': 'unit',
  'source': {'label': 'unit', 'maintainer': 'unit'},
  'map': {
    'provider': 'mapbox',
    'latitude': 57.1,
    'longitude': -2.1,
    'defaultZoom': 12,
  },
};

void main() {
  test(
    'unregistered target survives late restore and catalog refresh without fake city API or persistence',
    () async {
      final store = PendingStore();
      final requests = <String>[];
      final city = PublicCityController(
        apiBaseUrl: 'https://unit.invalid',
        explorationStore: store,
        client: MockClient((r) async {
          requests.add(r.url.path);
          return http.Response(
            jsonEncode({
              'data': r.url.path == '/v1/cities' ? [catalogCity()] : [],
            }),
            200,
          );
        }),
      );
      addTearDown(city.dispose);
      await city.loadCities();
      expect(city.selectDestination(destination), true);
      requests.clear();
      store.read.complete('unit-catalog');
      await city.explorationRestoreSettled;
      await city.loadCities();
      await city.loadPlaces();
      await city.loadActivities();
      await city.loadOrganizationPins();
      expect(city.selectedCity, isNull);
      expect(city.selectedDestination, same(destination));
      expect(city.contentCityID, '');
      expect(city.browseKey, destination.browseKey);
      expect(city.renderCity?.map?.latitude, destination.latitude);
      expect(city.cities.map((v) => v.id), ['unit-catalog']);
      expect(requests, ['/v1/cities']);
      expect(store.writes, isEmpty);
      city.selectCity('unit-catalog');
      await city.explorationWritesSettled;
      expect(city.selectedDestination, isNull);
      expect(city.contentCityID, 'unit-catalog');
      expect(store.writes, ['unit-catalog']);
    },
  );

  test('Mapbox domestic target cannot silently cross to AMap renderer', () {
    final city = PublicCityController();
    addTearDown(city.dispose);
    expect(
      city.selectDestination(
        const BrowseDestination(
          provider: 'mapbox',
          externalID: 'unit-cn',
          name: 'Synthetic CN',
          countryCode: 'CN',
          region: 'Synthetic',
          latitude: 31,
          longitude: 121,
          featureType: 'place',
          attribution: 'UNIT',
          storageMode: DestinationStorageMode.temporary,
        ),
      ),
      false,
    );
    expect(city.renderCity, isNull);
    expect(
      city.selectDestination(
        const BrowseDestination(
          provider: 'amap',
          externalID: 'unit-native-cn',
          name: 'Synthetic native CN',
          countryCode: 'CN',
          region: 'Synthetic',
          latitude: 31,
          longitude: 121,
          featureType: 'place',
          attribution: 'UNIT',
          storageMode: DestinationStorageMode.temporary,
        ),
      ),
      true,
    );
    expect(city.renderCity?.map?.provider, 'amap');
    expect(city.selectedCity, isNull);
    expect(city.contentCityID, '');
  });

  testWidgets(
    'external viewport queries omit cityId and keeps same map subtree and click fencing',
    (t) async {
      final city = PublicCityController()..selectDestination(destination);
      final workspace = AgentWorkspaceController();
      final map = MapViewportState();
      final discovery = NowDiscoveryController(authorizationHeader: () => null);
      final fixture = content.Fixture();
      final changes = ChangeNotifier();
      final owned = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => content.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => null,
        identityChanges: changes,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      await t.pumpWidget(
        MaterialApp(
          home: MapCanvas(
            city: city,
            workspace: workspace,
            mapState: map,
            discovery: discovery,
            mapContent: owned,
            onInitialViewport: (_) {},
            onMapUnavailable: (_) {},
          ),
        ),
      );
      final element = find.byType(PublicCityMapView).evaluate().single;
      var view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      expect(view.city?.id, destination.browseKey);
      expect(view.onContentViewportSettled, isNotNull);
      expect(view.onViewportInitialized, isNotNull);
      view.onContentViewportSettled!(content.bounds, 12);
      await content.drain(t, () => owned.current && !owned.loading);
      await t.pump();
      expect(
        fixture.requests.last.url.queryParameters.containsKey('cityId'),
        false,
      );
      expect(owned.cityID, '');
      view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      expect(view.entities, hasLength(1));
      final oldClick = view.onEntitySelected!;
      final oldEntity = view.entities.single;
      oldClick(oldEntity);
      expect(workspace.selectedEntityId, oldEntity.id);
      workspace.selectEntity(null);
      city.selectDestination(
        const BrowseDestination(
          provider: 'mapbox',
          externalID: 'unit-second',
          name: 'Synthetic second',
          countryCode: 'US',
          region: 'Synthetic',
          latitude: 40,
          longitude: -75,
          featureType: 'place',
          attribution: 'UNIT',
          storageMode: DestinationStorageMode.temporary,
        ),
      );
      await t.pump();
      oldClick(oldEntity);
      expect(workspace.selectedEntityId, isNull);
      expect(find.byType(PublicCityMapView).evaluate().single, same(element));
      await t.pumpWidget(const SizedBox());
      owned.dispose();
      changes.dispose();
      discovery.dispose();
      map.dispose();
      workspace.dispose();
      city.dispose();
    },
  );
}
