import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';

class City extends PublicCityController {
  @override
  PublicCity? get selectedCity => const PublicCity(
    id: 'test-city',
    name: '合成城市',
    region: '',
    contentStatus: 'building',
    source: PublicSource(
      label: '合成',
      maintainer: 'test',
      freshness: 'test',
      updatedAt: null,
    ),
    map: null,
  );
}

void main() {
  testWidgets(
    'saved personal collection projects original IDs and retires stale picks',
    (t) async {
      final city = City(),
          ws = AgentWorkspaceController(),
          viewport = MapViewportState(),
          discovery = NowDiscoveryController(authorizationHeader: () => null);
      var valid = true;
      final picks = <String>[];
      const point = MapEntity(
        id: 'place:22222222-2222-4222-8222-222222222222',
        kind: MapEntityKind.place,
        title: '合成地点',
        subtitle: '合成主题',
        latitude: 57.1,
        longitude: -2.1,
        placeID: '22222222-2222-4222-8222-222222222222',
        precision: 'point',
        publicLocation: true,
      );
      final view = PersonalCollectionProjection(
        id: 'theme:test:1',
        cityID: 'test-city',
        title: '合成主题',
        entities: [point],
        sourceCurrent: () => valid,
      );
      final projection = ValueNotifier<PersonalCollectionProjection?>(view);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MapCanvas(
              city: city,
              workspace: ws,
              mapState: viewport,
              discovery: discovery,
              personalCollection: projection,
              onInitialViewport: (_) {},
              onMapUnavailable: (_) {},
              onEntityPicked: picks.add,
            ),
          ),
        ),
      );
      await t.pump();
      final shown = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      expect(shown.entities.single.id, point.id);
      expect(shown.places, isEmpty);
      expect(shown.contextKey, view.id);
      shown.onEntitySelected!(point);
      await t.pump();
      expect(picks, [point.id]);
      valid = false;
      projection.notifyListeners();
      await t.pump();
      expect(
        t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
        isEmpty,
      );
      shown.onEntitySelected!(point);
      expect(picks, [point.id]);
      projection.value = null;
      await t.pump();
      expect(
        t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).contextKey,
        'idle',
      );
      await t.pumpWidget(const SizedBox());
      projection.dispose();
      city.dispose();
      ws.dispose();
      viewport.dispose();
      discovery.dispose();
    },
  );
}
