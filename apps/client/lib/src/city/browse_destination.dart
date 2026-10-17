import 'dart:math' as math;

import '../workspace/map_entities.dart';

enum DestinationStorageMode { temporary, catalog }

/// A browsing target, never a profile location, publication anchor or City row.
class BrowseDestination {
  const BrowseDestination({
    required this.provider,
    required this.externalID,
    required this.name,
    required this.countryCode,
    required this.region,
    required this.latitude,
    required this.longitude,
    required this.featureType,
    required this.attribution,
    required this.storageMode,
    this.bounds,
    this.internalCityID,
  });

  final String provider, externalID, name, countryCode, region, featureType;
  final String attribution;
  final double latitude, longitude;
  final MapBounds? bounds;
  final DestinationStorageMode storageMode;
  final String? internalCityID;

  bool get isTemporary => storageMode == DestinationStorageMode.temporary;
  String get browseKey => 'destination:$provider:$externalID';

  /// This checks the known country, administrative level and renderer boundary
  /// only. A user must still inspect and explicitly choose the matching city;
  /// equal names alone never establish a catalog identity.
  bool canLocateCatalogCity(String catalogCountryCode) =>
      valid &&
      isTemporary &&
      countryCode == catalogCountryCode.trim().toUpperCase() &&
      featureType == 'place' &&
      ((provider == 'amap' && countryCode == 'CN') ||
          (provider == 'mapbox' && countryCode != 'CN'));

  bool get valid =>
      provider.isNotEmpty &&
      externalID.isNotEmpty &&
      name.trim().isNotEmpty &&
      RegExp(r'^[A-Z]{2}$').hasMatch(countryCode) &&
      latitude.isFinite &&
      longitude.isFinite &&
      latitude.abs() <= 85.05112878 &&
      longitude.abs() <= 180 &&
      (bounds == null || bounds!.isValid);

  /// Conservative phone framing using provider bounds, not content coordinates.
  /// This is a view hint only; no destination boundary is written to content.
  double get defaultZoom {
    final area = bounds;
    if (area != null && area.isValid) {
      double mercator(double lat) {
        final radians = lat.clamp(-85.05112878, 85.05112878) * math.pi / 180;
        return math.log(math.tan(math.pi / 4 + radians / 2));
      }

      final longitudeFraction = area.longitudeSpan / 360;
      final latitudeFraction =
          (mercator(area.north) - mercator(area.south)).abs() / (2 * math.pi);
      if (longitudeFraction > 0 && latitudeFraction > 0) {
        final horizontal = math.log(256 / (512 * longitudeFraction)) / math.ln2;
        final vertical = math.log(384 / (512 * latitudeFraction)) / math.ln2;
        return math.min(horizontal, vertical).clamp(1, 16).toDouble();
      }
    }
    return switch (featureType) {
      'country' => 4,
      'region' => 7,
      'district' => 9,
      'locality' => 13,
      _ => 11,
    };
  }
}
