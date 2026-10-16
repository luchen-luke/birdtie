import 'dart:math' as math;

import 'map_entities.dart';

/// Geographic coverage, separate from content/permission leases. Rectangles
/// crossing 180 degrees are split before subtraction; no centre is invented.
class MapContentCoverage {
  static List<MapBounds> split(MapBounds b) => b.west < b.east
      ? [b]
      : [
          if (b.west < 180)
            MapBounds(west: b.west, south: b.south, east: 180, north: b.north),
          if (b.east > -180)
            MapBounds(west: -180, south: b.south, east: b.east, north: b.north),
        ];

  static bool intersects(MapBounds a, MapBounds b) => split(a).any(
    (x) => split(b).any(
      (y) =>
          x.west <= y.east &&
          x.east >= y.west &&
          x.south <= y.north &&
          x.north >= y.south,
    ),
  );

  static List<MapBounds> missing(
    MapBounds target,
    Iterable<MapBounds> covered,
  ) {
    var result = split(target);
    for (final area in covered.expand(split)) {
      result = [for (final part in result) ..._subtract(part, area)];
      if (result.isEmpty) break;
    }
    return result;
  }

  static List<MapBounds> _subtract(MapBounds a, MapBounds b) {
    final w = math.max(a.west, b.west),
        e = math.min(a.east, b.east),
        s = math.max(a.south, b.south),
        n = math.min(a.north, b.north);
    if (w >= e || s >= n) return [a];
    return [
      if (a.west < w)
        MapBounds(west: a.west, east: w, south: a.south, north: a.north),
      if (e < a.east)
        MapBounds(west: e, east: a.east, south: a.south, north: a.north),
      if (a.south < s) MapBounds(west: w, east: e, south: a.south, north: s),
      if (n < a.north) MapBounds(west: w, east: e, south: n, north: a.north),
    ];
  }

  static MapBounds padded(MapBounds b, {double fraction = .15}) {
    final dx = b.longitudeSpan * fraction, dy = (b.north - b.south) * fraction;
    double wrap(double v) => (v + 180) % 360 - 180;
    final world = b.longitudeSpan + 2 * dx >= 360;
    return MapBounds(
      west: world ? -180 : wrap(b.west - dx),
      east: world ? 180 : wrap(b.east + dx),
      south: (b.south - dy).clamp(-90, 90),
      north: (b.north + dy).clamp(-90, 90),
    );
  }
}
