import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

enum MapRouteRegion { mainland, hongKong, macao, taiwan, overseas, unknown }

class MapRouteRegionResult {
  const MapRouteRegionResult(this.region, this.version, {this.code = ''});
  final MapRouteRegion region;
  final String version, code;
  bool get isDefinite => region != MapRouteRegion.unknown;
  String? get provider => !isDefinite
      ? null
      : region == MapRouteRegion.mainland
      ? 'amap'
      : 'mapbox';
}

/// Local service-area classification only. The source's map-unit identifiers
/// are never displayed as geopolitical labels or used as business identities.
/// Bounding boxes only accelerate an actual ring test; they never choose a map.
class MapRouteRegionIndex {
  MapRouteRegionIndex._(
    this.version,
    this.sourceVersion,
    this.sourceSHA256,
    this.boundaryToleranceKm,
    this._regions,
  );

  static const assetPath = 'assets/map_routing/region_routes_v1.json';
  static Future<MapRouteRegionIndex>? _loading;
  final String version, sourceVersion, sourceSHA256;
  final double boundaryToleranceKm;
  final List<_RouteUnit> _regions;

  static Future<MapRouteRegionIndex> load() => _loading ??= () async {
    try {
      final text = await rootBundle.loadString(assetPath);
      return await compute(_decodeRegions, text);
    } catch (_) {
      _loading = null;
      rethrow;
    }
  }();

  factory MapRouteRegionIndex.fromJson(Object? value) {
    if (value is! Map ||
        value['schema'] != 'birdtie-map-route-regions-v1' ||
        value['coordinateSystem'] != 'wgs84' ||
        value['scale'] != 1000000 ||
        value['version'] is! String ||
        (value['version'] as String).isEmpty ||
        value['sourceVersion'] is! String ||
        value['sourceSHA256'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(value['sourceSHA256'] as String) ||
        value['boundaryToleranceKm'] is! num ||
        !(value['boundaryToleranceKm'] as num).isFinite ||
        (value['boundaryToleranceKm'] as num) < .1 ||
        (value['boundaryToleranceKm'] as num) > 10 ||
        value['regions'] is! List ||
        (value['regions'] as List).isEmpty ||
        (value['regions'] as List).length > 512) {
      throw const FormatException('地图服务区域数据无效');
    }
    var total = 0;
    final units = <_RouteUnit>[];
    for (final raw in value['regions'] as List) {
      if (raw is! Map ||
          raw['code'] is! String ||
          !RegExp(r'^[A-Z0-9-]{2,8}$').hasMatch(raw['code'] as String) ||
          !const ['amap', 'mapbox'].contains(raw['route']) ||
          (raw['route'] == 'amap') != (raw['code'] == 'CHN') ||
          raw['rings'] is! List ||
          (raw['rings'] as List).isEmpty) {
        throw const FormatException('地图服务区域数据无效');
      }
      final rings = <_RouteRing>[];
      for (final rawRing in raw['rings'] as List) {
        if (rawRing is! List ||
            rawRing.length < 8 ||
            rawRing.length.isOdd ||
            rawRing.any((v) => v is! int)) {
          throw const FormatException('地图服务区域边界无效');
        }
        total += rawRing.length ~/ 2;
        if (total > 650000) {
          throw const FormatException('地图服务区域数据超限');
        }
        rings.add(_RouteRing.decode(rawRing.cast<int>()));
      }
      units.add(_RouteUnit(raw['code'] as String, List.unmodifiable(rings)));
    }
    return MapRouteRegionIndex._(
      value['version'] as String,
      value['sourceVersion'] as String,
      value['sourceSHA256'] as String,
      (value['boundaryToleranceKm'] as num).toDouble(),
      List.unmodifiable(units),
    );
  }

  MapRouteRegionResult classify(double latitude, double longitude) {
    MapRouteRegionResult unknown() =>
        MapRouteRegionResult(MapRouteRegion.unknown, version);
    if (!latitude.isFinite ||
        !longitude.isFinite ||
        latitude.abs() > 90 ||
        longitude.abs() > 180) {
      return unknown();
    }
    final x = longitude * 1000000, y = latitude * 1000000;
    final lonKm =
        math.max(.0001, math.cos(latitude * math.pi / 180).abs()) *
        111.195 /
        1000000;
    const latKm = 111.195 / 1000000;
    final matches = <String>[];
    for (final unit in _regions) {
      var inside = false;
      for (final ring in unit.rings) {
        final pointX = ring.nearLongitude(x);
        if (!ring.nearBounds(pointX, y, boundaryToleranceKm, lonKm, latKm)) {
          continue;
        }
        // Polar/antimeridian winding without a closed planar representation
        // remains unknown. Never turn its broad extent into positive land.
        if (ring.ambiguous) return unknown();
        if (ring.nearBoundary(pointX, y, boundaryToleranceKm, lonKm, latKm)) {
          return unknown();
        }
        if (ring.contains(pointX, y)) inside = !inside;
      }
      if (inside) matches.add(unit.code);
    }
    if (matches.isEmpty ||
        matches.contains('CHN') && matches.any((code) => code != 'CHN')) {
      return unknown();
    }
    final code = matches.first;
    final region = switch (code) {
      'CHN' => MapRouteRegion.mainland,
      'HKG' => MapRouteRegion.hongKong,
      'MAC' => MapRouteRegion.macao,
      'TWN' => MapRouteRegion.taiwan,
      _ => MapRouteRegion.overseas,
    };
    return MapRouteRegionResult(region, version, code: code);
  }
}

MapRouteRegionIndex _decodeRegions(String text) =>
    MapRouteRegionIndex.fromJson(jsonDecode(text));

class _RouteUnit {
  const _RouteUnit(this.code, this.rings);
  final String code;
  final List<_RouteRing> rings;
}

class _RouteRing {
  _RouteRing(
    this.points,
    this.west,
    this.south,
    this.east,
    this.north,
    this.ambiguous,
  );
  final Int32List points;
  final int west, south, east, north;
  final bool ambiguous;

  factory _RouteRing.decode(List<int> encoded) {
    final points = Int32List(encoded.length);
    var x = 0, y = 0, previous = 0;
    var west = 2000000001, south = 90000001;
    var east = -2000000001, north = -90000001;
    for (var i = 0; i < encoded.length; i += 2) {
      x += encoded[i];
      y += encoded[i + 1];
      if (x.abs() > 180000000 || y.abs() > 90000000) {
        throw const FormatException('地图服务区域坐标无效');
      }
      var unwrapped = x;
      if (i > 0) {
        while (unwrapped - previous > 180000000) unwrapped -= 360000000;
        while (unwrapped - previous < -180000000) unwrapped += 360000000;
      }
      if (unwrapped.abs() > 2000000000) {
        throw const FormatException('地图服务区域坐标超限');
      }
      points[i] = unwrapped;
      points[i + 1] = y;
      previous = unwrapped;
      west = math.min(west, unwrapped);
      east = math.max(east, unwrapped);
      south = math.min(south, y);
      north = math.max(north, y);
    }
    return _RouteRing(
      points,
      west,
      south,
      east,
      north,
      points[0] != points[points.length - 2] || points[1] != points.last,
    );
  }

  double nearLongitude(double x) =>
      x + (((west + east) / 2 - x) / 360000000).round() * 360000000;

  bool nearBounds(double x, double y, double km, double lonKm, double latKm) =>
      x >= west - km / lonKm &&
      x <= east + km / lonKm &&
      y >= south - km / latKm &&
      y <= north + km / latKm;

  bool contains(double x, double y) {
    if (x < west || x > east || y < south || y > north) return false;
    var result = false;
    for (var i = 0, j = points.length - 2; i < points.length; j = i, i += 2) {
      final a = points[i], b = points[i + 1];
      final c = points[j], d = points[j + 1];
      if ((b > y) != (d > y) && x < (c - a) * (y - b) / (d - b) + a) {
        result = !result;
      }
    }
    return result;
  }

  bool nearBoundary(
    double x,
    double y,
    double tolerance,
    double lonKm,
    double latKm,
  ) {
    final squared = tolerance * tolerance;
    for (var i = 2; i < points.length; i += 2) {
      final ax = (points[i - 2] - x) * lonKm;
      final ay = (points[i - 1] - y) * latKm;
      final bx = (points[i] - x) * lonKm;
      final by = (points[i + 1] - y) * latKm;
      if (math.min(ax, bx) > tolerance ||
          math.max(ax, bx) < -tolerance ||
          math.min(ay, by) > tolerance ||
          math.max(ay, by) < -tolerance)
        continue;
      final dx = bx - ax, dy = by - ay, length = dx * dx + dy * dy;
      final t = length == 0
          ? 0.0
          : (-(ax * dx + ay * dy) / length).clamp(0.0, 1.0);
      final px = ax + t * dx, py = ay + t * dy;
      if (px * px + py * py <= squared) return true;
    }
    return false;
  }
}
