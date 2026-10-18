import 'package:birdtie_client/src/city/map_navigation_target.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const camera = MapCameraSnapshot(
    latitude: 31.2304,
    longitude: 121.4737,
    zoom: 12,
    bearing: 35,
    pitch: 20,
  );
  test('camera round-trip is a closed canonical view snapshot', () {
    final saved = camera.toJson();
    expect(MapCameraSnapshot.fromJson(saved)!.toJson(), saved);
    for (final changed in [
      {...saved, 'coordinateSystem': 'gcj02'},
      {...saved, 'coordinateSystem': 'unknown'},
      {...saved, 'latitude': double.nan},
      {...saved, 'longitude': 181},
      {...saved, 'ownerId': 'private-fixture'},
    ]) {
      expect(MapCameraSnapshot.fromJson(changed), isNull);
    }
  });
  test('temporary or content geometry cannot become a durable City camera', () {
    for (final source in MapNavigationSource.values) {
      final target = MapNavigationTarget(
        key: 'synthetic-view',
        source: source,
        generation: 3,
        camera: camera,
        sourceVersion: 'current-fixture',
        catalogCityID: 'synthetic-city',
        countryCode: 'CN',
      );
      expect(target.isValid, true);
      expect(
        target.canPersistCamera,
        source == MapNavigationSource.catalogCity,
      );
    }
    const unverified = MapNavigationTarget(
      key: 'unknown-crs',
      source: MapNavigationSource.catalogCity,
      generation: 4,
      sourceVersion: 'legacy-private',
      catalogCityID: 'city',
      camera: MapCameraSnapshot(
        latitude: 31,
        longitude: 121,
        zoom: 12,
        coordinateSystem: 'unknown',
      ),
    );
    expect(unverified.isValid, false);
    expect(unverified.canPersistCamera, false);
  });
}
