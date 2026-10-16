// WGS84/GCJ02 formula adapted from wandergis/coordtransform (MIT).
// Copyright (c) 2015 记忆的残骸. See third_party/coordtransform.LICENSE.
// Birdtie additions: validation, named axes and bounded iterative inversion.
import 'dart:math' as math;

/// Coordinates stored in Birdtie and sent to its APIs remain WGS84.
/// Use these methods only at an explicitly selected AMap boundary. The numeric
/// conversion envelope is not a country classifier or evidence of a point's CRS.
abstract final class MapCoordinateBoundary {
  static const _axis = 6378245.0;
  static const _eccentricitySquared = 0.00669342162296594323;

  static void _validate(double latitude, double longitude) {
    if (!latitude.isFinite ||
        !longitude.isFinite ||
        latitude.abs() > 90 ||
        longitude.abs() > 180) {
      throw const FormatException('地图坐标无效');
    }
  }

  static bool _outside(double latitude, double longitude) =>
      longitude < 72.004 ||
      longitude > 137.8347 ||
      latitude < 0.8293 ||
      latitude > 55.8271;

  /// Canonical WGS84 -> AMap GCJ02 display coordinates, exactly once.
  static ({double latitude, double longitude}) toAmap(
    double latitude,
    double longitude,
  ) {
    _validate(latitude, longitude);
    if (_outside(latitude, longitude)) {
      return (latitude: latitude, longitude: longitude);
    }
    return _transform(latitude, longitude);
  }

  // Keep the smooth transform separate from the discontinuous numeric envelope
  // so inverse iterations do not bounce between shifted and identity branches.
  static ({double latitude, double longitude}) _transform(
    double latitude,
    double longitude,
  ) {
    final x = longitude - 105, y = latitude - 35;
    final wave =
        (20 * math.sin(6 * x * math.pi) + 20 * math.sin(2 * x * math.pi)) *
        2 /
        3;
    final latitudeWave =
        -100 +
        2 * x +
        3 * y +
        .2 * y * y +
        .1 * x * y +
        .2 * math.sqrt(x.abs()) +
        wave +
        (20 * math.sin(y * math.pi) + 40 * math.sin(y * math.pi / 3)) * 2 / 3 +
        (160 * math.sin(y * math.pi / 12) + 320 * math.sin(y * math.pi / 30)) *
            2 /
            3;
    final longitudeWave =
        300 +
        x +
        2 * y +
        .1 * x * x +
        .1 * x * y +
        .1 * math.sqrt(x.abs()) +
        wave +
        (20 * math.sin(x * math.pi) + 40 * math.sin(x * math.pi / 3)) * 2 / 3 +
        (150 * math.sin(x * math.pi / 12) + 300 * math.sin(x * math.pi / 30)) *
            2 /
            3;
    final radians = latitude * math.pi / 180;
    final sine = math.sin(radians);
    final factor = 1 - _eccentricitySquared * sine * sine;
    final root = math.sqrt(factor);
    return (
      latitude:
          latitude +
          latitudeWave *
              180 /
              ((_axis * (1 - _eccentricitySquared)) /
                  (factor * root) *
                  math.pi),
      longitude:
          longitude +
          longitudeWave * 180 / (_axis / root * math.cos(radians) * math.pi),
    );
  }

  /// AMap SDK coordinates -> canonical query/selection coordinates. The
  /// inversion is bounded and checks convergence; it never changes stored data.
  static ({double latitude, double longitude}) fromAmap(
    double latitude,
    double longitude,
  ) {
    _validate(latitude, longitude);
    // A shifted point at an envelope edge can land just outside the WGS bounds.
    // In this narrow ambiguous strip prefer a converged in-envelope preimage;
    // otherwise retain the identity branch. This is not a country decision.
    const padding = .05;
    if (longitude < 72.004 - padding ||
        longitude > 137.8347 + padding ||
        latitude < .8293 - padding ||
        latitude > 55.8271 + padding) {
      return (latitude: latitude, longitude: longitude);
    }
    var lat = latitude, lon = longitude;
    for (var round = 0; round < 8; round++) {
      final displayed = _transform(lat, lon);
      final latitudeError = displayed.latitude - latitude;
      final longitudeError = displayed.longitude - longitude;
      if (latitudeError.abs() <= 1e-8 && longitudeError.abs() <= 1e-8) {
        if (_outside(lat, lon)) {
          return (latitude: latitude, longitude: longitude);
        }
        return (latitude: lat, longitude: lon);
      }
      lat -= latitudeError;
      lon -= longitudeError;
    }
    throw const FormatException('地图坐标转换未收敛');
  }
}
