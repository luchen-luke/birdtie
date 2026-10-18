/// A camera is a view preference, never a device-location or publication fact.
/// Callers must obtain the declared CRS from their verified source; merely
/// changing this label does not convert or authorize coordinates.
class MapCameraSnapshot {
  const MapCameraSnapshot({
    required this.latitude,
    required this.longitude,
    required this.zoom,
    this.bearing = 0,
    this.pitch = 0,
    this.coordinateSystem = 'wgs84',
  });

  final double latitude, longitude, zoom, bearing, pitch;
  final String coordinateSystem;

  bool get isValid =>
      coordinateSystem == 'wgs84' &&
      latitude.isFinite &&
      longitude.isFinite &&
      latitude.abs() <= 90 &&
      longitude.abs() <= 180 &&
      zoom.isFinite &&
      zoom >= 0 &&
      zoom <= 24 &&
      bearing.isFinite &&
      bearing.abs() <= 360 &&
      pitch.isFinite &&
      pitch >= 0 &&
      pitch <= 85;

  Map<String, Object> toJson() => {
    'latitude': latitude,
    'longitude': longitude,
    'zoom': zoom,
    'bearing': bearing,
    'pitch': pitch,
    'coordinateSystem': coordinateSystem,
  };

  static MapCameraSnapshot? fromJson(Object? value) {
    if (value is! Map ||
        value.length != 6 ||
        value['coordinateSystem'] != 'wgs84' ||
        !const [
          'latitude',
          'longitude',
          'zoom',
          'bearing',
          'pitch',
        ].every((key) => value[key] is num)) {
      return null;
    }
    final camera = MapCameraSnapshot(
      latitude: (value['latitude'] as num).toDouble(),
      longitude: (value['longitude'] as num).toDouble(),
      zoom: (value['zoom'] as num).toDouble(),
      bearing: (value['bearing'] as num).toDouble(),
      pitch: (value['pitch'] as num).toDouble(),
    );
    return camera.isValid ? camera : null;
  }
}

enum MapNavigationSource {
  catalogCity,
  // A retained conversation explicitly brought back onto the map. Its camera
  // is never a persisted catalog preference.
  task,
  temporaryDestination,
  place,
  moment,
  journey,
  thematicMap,
  viewport,
}

/// A navigation snapshot, not a grant, membership, City row or model context.
/// The owner keeps its current identity/permission check alongside this value.
class MapNavigationTarget {
  const MapNavigationTarget({
    required this.key,
    required this.source,
    required this.generation,
    required this.camera,
    required this.sourceVersion,
    this.countryCode = '',
    this.regionCode = '',
    this.catalogCityID,
    this.viewportLongitudeSpan,
    this.viewportLatitudeSpan,
  });

  final String key, sourceVersion, countryCode, regionCode;
  final String? catalogCityID;
  final MapNavigationSource source;
  final int generation;
  final MapCameraSnapshot camera;
  final double? viewportLongitudeSpan, viewportLatitudeSpan;

  bool get isExplicit => source != MapNavigationSource.viewport;
  bool get isValid =>
      _text(key, 512) &&
      _text(sourceVersion, 512) &&
      generation >= 0 &&
      camera.isValid &&
      countryCode.length <= 8 &&
      regionCode.length <= 32 &&
      (viewportLongitudeSpan == null ||
          viewportLongitudeSpan!.isFinite &&
              viewportLongitudeSpan! > 0 &&
              viewportLongitudeSpan! <= 360) &&
      (viewportLatitudeSpan == null ||
          viewportLatitudeSpan!.isFinite &&
              viewportLatitudeSpan! > 0 &&
              viewportLatitudeSpan! <= 180) &&
      (catalogCityID == null || _text(catalogCityID!, 160));

  // A temporary provider result stays temporary even if it was matched to a
  // catalog City. Its source cannot be promoted just to persist its geometry.
  bool get canPersistCamera =>
      isValid &&
      source == MapNavigationSource.catalogCity &&
      catalogCityID != null;

  static bool _text(String value, int max) =>
      value.isNotEmpty &&
      value == value.trim() &&
      value.length <= max &&
      !value.runes.any((r) => r < 32 || r == 127);
}
