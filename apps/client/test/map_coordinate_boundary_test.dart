import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/city/map_coordinate_boundary.dart';

void main() {
  test('AMap display matches pinned independent public coordinate fixtures', () {
    // Produced by upstream coordtransform 606c6f3b, not this Dart implementation.
    // These are public city camera hints and a public example, not user locations.
    const fixtures = [
      (31.218398, 121.434559, 31.216534105133842, 121.43918142966909),
      (23.371952, 116.668080, 23.369272235585697, 116.67251633610343),
      (39.916527, 116.397128, 39.91793074924595, 116.40337249402477),
    ];
    for (final point in fixtures) {
      final display = MapCoordinateBoundary.toAmap(point.$1, point.$2);
      expect(display.latitude, closeTo(point.$3, 1e-10));
      expect(display.longitude, closeTo(point.$4, 1e-10));
      final query = MapCoordinateBoundary.fromAmap(point.$3, point.$4);
      expect(query.latitude, closeTo(point.$1, 1e-7));
      expect(query.longitude, closeTo(point.$2, 1e-7));
    }
  });

  test('foreign points remain unchanged and camera round trips stay bounded', () {
    for (final point in [(57.1497, -2.0943), (64.1466, -21.9426), (0.0, 0.0)]) {
      final expected = (latitude: point.$1, longitude: point.$2);
      expect(MapCoordinateBoundary.toAmap(point.$1, point.$2), expected);
      expect(MapCoordinateBoundary.fromAmap(point.$1, point.$2), expected);
    }
    // A city viewport has four independently transformed corners, not one
    // guessed fixed offset. This includes rotations' diagonal geographic extent.
    for (final lat in [23.2, 23.6, 31.0, 31.5]) {
      for (final lon in [116.3, 117.0, 121.1, 121.8]) {
        final display = MapCoordinateBoundary.toAmap(lat, lon);
        final query = MapCoordinateBoundary.fromAmap(
          display.latitude,
          display.longitude,
        );
        expect(query.latitude, closeTo(lat, 1e-7));
        expect(query.longitude, closeTo(lon, 1e-7));
      }
    }
  });

  test('shifted envelope edges invert without switching identity branches', () {
    for (final point in [
      (31.0, 137.834699),
      (55.827099, 121.0),
      (31.0, 72.004001),
      (.829301, 121.0),
    ]) {
      final display = MapCoordinateBoundary.toAmap(point.$1, point.$2);
      final query = MapCoordinateBoundary.fromAmap(
        display.latitude,
        display.longitude,
      );
      expect(query.latitude, closeTo(point.$1, 1e-7));
      expect(query.longitude, closeTo(point.$2, 1e-7));
    }
    // No valid shifted preimage in the envelope: retain identity, do not throw.
    expect(MapCoordinateBoundary.fromAmap(31, 72.003), (
      latitude: 31.0,
      longitude: 72.003,
    ));
  });

  test('invalid or unknown numeric coordinates cannot enter a provider', () {
    for (final point in [
      (double.nan, 121.0),
      (31.0, double.infinity),
      (91.0, 121.0),
      (31.0, 181.0),
    ]) {
      expect(
        () => MapCoordinateBoundary.toAmap(point.$1, point.$2),
        throwsFormatException,
      );
      expect(
        () => MapCoordinateBoundary.fromAmap(point.$1, point.$2),
        throwsFormatException,
      );
    }
  });
}
