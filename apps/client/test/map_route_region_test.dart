import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/city/map_navigation_target.dart';
import 'package:birdtie_client/src/city/map_provider_policy.dart';
import 'package:birdtie_client/src/city/map_route_region.dart';
import 'package:flutter_test/flutter_test.dart';

List<int> encodeRing(List<(double, double)> vertices) {
  final result = <int>[];
  var x = 0, y = 0;
  for (final vertex in vertices) {
    final nx = (vertex.$1 * 1000000).round();
    final ny = (vertex.$2 * 1000000).round();
    result.addAll([nx - x, ny - y]);
    x = nx;
    y = ny;
  }
  return result;
}

Map<String, Object> fixture(List<Map<String, Object>> regions) => {
  'schema': 'birdtie-map-route-regions-v1',
  'version': 'isolated-ring-fixture',
  'coordinateSystem': 'wgs84',
  'scale': 1000000,
  'sourceVersion': 'fixture',
  'sourceSHA256': 'a' * 64,
  'boundaryToleranceKm': .5,
  'regions': regions,
};
Map<String, Object> unit(String code, List<List<(double, double)>> rings) => {
  'code': code,
  'route': code == 'CHN' ? 'amap' : 'mapbox',
  'rings': rings.map(encodeRing).toList(),
};

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'real official map units provide positive mainland and overseas land',
    () async {
      final index = await MapRouteRegionIndex.load();
      expect(index.sourceVersion, '5.1.1');
      expect(
        index.sourceSHA256,
        '45bebe2aaf8bf42b9daf4428594925bcb11eabd32fe9dc6e0acf681438053eb5',
      );
      for (final (lat, lon, expected) in [
        (31.2304, 121.4737, MapRouteRegion.mainland),
        (55.9533, -3.1883, MapRouteRegion.overseas),
        (57.1497, -2.0943, MapRouteRegion.overseas),
        (48.8566, 2.3522, MapRouteRegion.overseas),
        (22.35, 114.18, MapRouteRegion.hongKong),
        // Interior of the actual MAC source polygon, >1km from its outline.
        // This fixture is a region test, never a Place or GPS fact.
        (22.152, 113.55, MapRouteRegion.macao),
        (25.033, 121.5654, MapRouteRegion.taiwan),
      ]) {
        expect(index.classify(lat, lon).region, expected, reason: '$lat,$lon');
      }
      expect(index.classify(0, -140).region, MapRouteRegion.unknown);
      expect(index.classify(double.nan, 0).region, MapRouteRegion.unknown);
      final viewport = MapNavigationTarget(
        key: 'stable-foreign-land',
        source: MapNavigationSource.viewport,
        generation: 9,
        sourceVersion: 'isolated-current-camera',
        camera: const MapCameraSnapshot(
          latitude: 55.9533,
          longitude: -3.1883,
          zoom: 2,
        ),
        viewportLongitudeSpan: 1,
        viewportLatitudeSpan: 1,
      );
      expect(
        resolveMapRoute(
          viewport,
          regions: index,
          previousProvider: 'amap',
        ).provider,
        'mapbox',
        reason: 'positive land and actual coverage, not SDK zoom',
      );
      final raw =
          jsonDecode(await File(MapRouteRegionIndex.assetPath).readAsString())
              as Map;
      final ring =
          ((raw['regions'] as List).first['rings'] as List).first as List;
      expect(
        index
            .classify((ring[1] as num) / 1000000, (ring[0] as num) / 1000000)
            .region,
        MapRouteRegion.unknown,
      );
    },
  );
  test(
    'holes, borders and antimeridian are ring tests, not bounding boxes',
    () {
      final index = MapRouteRegionIndex.fromJson(
        fixture([
          unit('CHN', [
            [(0, 0), (10, 0), (10, 10), (0, 10), (0, 0)],
            [(3, 3), (7, 3), (7, 7), (3, 7), (3, 3)],
          ]),
          unit('UNIT', [
            [(179, -10), (-179, -10), (-179, 10), (179, 10), (179, -10)],
          ]),
        ]),
      );
      expect(index.classify(1, 1).region, MapRouteRegion.mainland);
      expect(index.classify(5, 5).region, MapRouteRegion.unknown);
      expect(index.classify(5, 0).region, MapRouteRegion.unknown);
      expect(index.classify(0, -179.5).region, MapRouteRegion.overseas);
      expect(index.classify(0, 179.5).region, MapRouteRegion.overseas);
      expect(index.classify(0, 90).region, MapRouteRegion.unknown);
      expect(
        () => MapRouteRegionIndex.fromJson({
          ...fixture([]),
          'coordinateSystem': 'gcj02',
        }),
        throwsFormatException,
      );
    },
  );
}
