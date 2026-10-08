import 'dart:convert';

import 'public_city_controller.dart';
import '../workspace/map_entities.dart';

/// A data-only view snapshot of the existing public map projection. It owns
/// neither entities, selection, task authority nor the user's viewport.
class MapCameraFocusInput {
  MapCameraFocusInput({
    required PublicCity? city,
    required this.contextKey,
    required List<PublicPlace> places,
    required List<MapEntity> entities,
    required this.selectedEntityId,
  }) : cityKey = jsonEncode([
         city?.id,
         city?.map?.provider,
         city?.map?.latitude,
         city?.map?.longitude,
         city?.map?.defaultZoom,
       ]),
       cityCenter = city?.map == null
           ? null
           : MapCameraPoint('', city!.map!.latitude, city.map!.longitude),
       cityZoom = city?.map?.defaultZoom {
    final publicPoints = <MapCameraPoint>[
      for (final entity in entities)
        MapCameraPoint(entity.id, entity.latitude, entity.longitude),
      for (final place in places)
        if (place.location.hasPublicPoint)
          MapCameraPoint(
            'place:${place.id}',
            place.location.latitude!,
            place.location.longitude!,
          ),
    ].where((point) => point.isValid).toList();
    selected = publicPoints
        .where((point) => point.id == selectedEntityId)
        .firstOrNull;
    final unique = <String, MapCameraPoint>{};
    for (final point in publicPoints) {
      unique[jsonEncode([point.id, point.latitude, point.longitude])] = point;
    }
    final keys = unique.keys.toList()..sort();
    points = List.unmodifiable(keys.map((key) => unique[key]!));
    coordinatesKey = jsonEncode(keys);
  }

  final String cityKey;
  final String contextKey;
  final String? selectedEntityId;
  final MapCameraPoint? cityCenter;
  final double? cityZoom;
  late final List<MapCameraPoint> points;
  late final MapCameraPoint? selected;
  late final String coordinatesKey;

  bool hasSameViewInputs(MapCameraFocusInput other) =>
      cityKey == other.cityKey &&
      contextKey == other.contextKey &&
      coordinatesKey == other.coordinatesKey &&
      selectedEntityId == other.selectedEntityId &&
      selected == other.selected;

  bool shouldFocusAfter(MapCameraFocusInput previous) {
    if (cityKey != previous.cityKey || contextKey != previous.contextKey) {
      return true;
    }
    if (selectedEntityId != previous.selectedEntityId && selected != null) {
      return true;
    }
    if (coordinatesKey == previous.coordinatesKey) return false;
    // Catalog refreshes in idle browsing do not undo a user's camera movement.
    return contextKey != 'idle' ||
        (selected != null && selected != previous.selected);
  }

  MapCameraPoint? get center {
    if (selected != null) return selected;
    if (contextKey == 'idle') {
      return cityCenter?.isValid == true ? cityCenter : null;
    }
    if (points.isEmpty) return null;
    return MapCameraPoint(
      '',
      points.fold<double>(0, (sum, point) => sum + point.latitude) /
          points.length,
      points.fold<double>(0, (sum, point) => sum + point.longitude) /
          points.length,
    );
  }

  bool get fitsBounds =>
      contextKey != 'idle' &&
      selected == null &&
      points.map((point) => (point.latitude, point.longitude)).toSet().length >
          1;

  double get zoom => selected != null
      ? 14
      : contextKey == 'idle'
      ? cityZoom ?? 1
      : 12.8;
}

class MapCameraPoint {
  const MapCameraPoint(this.id, this.latitude, this.longitude);
  final String id;
  final double latitude;
  final double longitude;

  bool get isValid =>
      latitude.isFinite &&
      longitude.isFinite &&
      latitude >= -90 &&
      latitude <= 90 &&
      longitude >= -180 &&
      longitude <= 180;

  @override
  bool operator ==(Object other) =>
      other is MapCameraPoint &&
      id == other.id &&
      latitude == other.latitude &&
      longitude == other.longitude;

  @override
  int get hashCode => Object.hash(id, latitude, longitude);
}
