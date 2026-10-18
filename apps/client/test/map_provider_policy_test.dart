import 'package:birdtie_client/src/city/map_navigation_target.dart';
import 'package:birdtie_client/src/city/map_provider_policy.dart';
import 'package:flutter_test/flutter_test.dart';

MapNavigationTarget target(
  String country, {
  MapNavigationSource source = MapNavigationSource.catalogCity,
  int generation = 1,
  double zoom = 12,
  double? longitudeSpan,
  double? latitudeSpan,
  String coordinateSystem = 'wgs84',
}) => MapNavigationTarget(
  key: 'view',
  source: source,
  generation: generation,
  camera: MapCameraSnapshot(
    latitude: 31.2304,
    longitude: 121.4737,
    zoom: zoom,
    coordinateSystem: coordinateSystem,
  ),
  sourceVersion: 'synthetic-source',
  countryCode: country,
  viewportLongitudeSpan: longitudeSpan,
  viewportLatitudeSpan: latitudeSpan,
);

void main() {
  test(
    'explicit region wins immediately across A B A; no provider preference',
    () {
      var provider = 'mapbox';
      for (final (generation, country, expected) in [
        (1, 'CN', 'amap'),
        (2, 'GB', 'mapbox'),
        (3, 'CHN', 'amap'),
        (4, 'HK', 'mapbox'),
        (5, 'MO', 'mapbox'),
        (6, 'TW', 'mapbox'),
      ]) {
        final result = resolveMapRoute(
          target(country, generation: generation),
          previousProvider: provider,
        );
        expect(result.provider, expected);
        expect(result.isDefinite, true);
        expect(result.targetGeneration, generation);
        provider = result.provider;
      }
      expect(
        mapProviderForCountry(countryCode: 'ZZ', configuredProvider: 'amap'),
        'amap',
      );
      expect(
        mapProviderForCountry(countryCode: 'GBR', configuredProvider: 'amap'),
        'mapbox',
      );
    },
  );
  test(
    'viewport world coverage uses real spans, never SDK zoom equivalence',
    () {
      for (final zoom in [1.0, 15.0]) {
        final world = resolveMapRoute(
          target(
            '',
            source: MapNavigationSource.viewport,
            zoom: zoom,
            longitudeSpan: 150,
            latitudeSpan: 30,
          ),
          previousProvider: 'amap',
        );
        expect(world.reason, 'world-viewport');
        expect(world.provider, 'amap');
        expect(world.isDefinite, false);
      }
      final missing = resolveMapRoute(
        target('CN', source: MapNavigationSource.viewport),
        previousProvider: 'mapbox',
      );
      expect(missing.reason, 'viewport-coverage-missing');
      expect(missing.provider, 'mapbox');
      final invalid = resolveMapRoute(
        target('CN', coordinateSystem: 'gcj02'),
        previousProvider: 'mapbox',
      );
      expect(invalid.isDefinite, false);
      expect(invalid.provider, 'mapbox');
    },
  );
}
