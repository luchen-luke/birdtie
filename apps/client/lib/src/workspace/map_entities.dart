import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:latlong2/latlong.dart';

enum MapEntityKind {
  person,
  peopleCluster,
  activity,
  group,
  community,
  organization,
  place,
  cluster,
  moment,
  business,
  opportunity,
}

String mapActivityStatusLabel(String status) => switch (status) {
  'upcoming' => '即将开始',
  'ongoing' => '进行中',
  'ended' => '已结束',
  'cancelled' => '已取消',
  _ => '活动状态待核对',
};

class MapBounds {
  const MapBounds({
    required this.west,
    required this.south,
    required this.east,
    required this.north,
  });

  final double west;
  final double south;
  final double east;
  final double north;

  bool get isValid =>
      west >= -180 &&
      east <= 180 &&
      south >= -90 &&
      north <= 90 &&
      west < east &&
      south < north;

  Map<String, double> toJson() => {
    'west': west,
    'south': south,
    'east': east,
    'north': north,
  };

  factory MapBounds.fromFilters(Map<String, String> filters) {
    return MapBounds(
      west: double.tryParse(filters['mapWest'] ?? '') ?? double.nan,
      south: double.tryParse(filters['mapSouth'] ?? '') ?? double.nan,
      east: double.tryParse(filters['mapEast'] ?? '') ?? double.nan,
      north: double.tryParse(filters['mapNorth'] ?? '') ?? double.nan,
    );
  }

  @override
  bool operator ==(Object other) =>
      other is MapBounds &&
      other.west == west &&
      other.south == south &&
      other.east == east &&
      other.north == north;

  @override
  int get hashCode => Object.hash(west, south, east, north);
}

class MapViewportState extends ChangeNotifier {
  MapBounds? viewportBounds;
  MapBounds? searchAreaBounds;
  String? mapError;
  bool cameraMoving = false;

  void reset() {
    viewportBounds = null;
    searchAreaBounds = null;
    mapError = null;
    cameraMoving = false;
    notifyListeners();
  }

  void initializeViewport(MapBounds bounds) {
    if (!bounds.isValid || viewportBounds == bounds) return;
    viewportBounds = bounds;
    mapError = null;
    notifyListeners();
  }

  void mapUnavailable(String message) {
    if (mapError == message) return;
    mapError = message;
    viewportBounds = null;
    searchAreaBounds = null;
    cameraMoving = false;
    notifyListeners();
  }

  void cameraStarted() {
    if (cameraMoving) return;
    cameraMoving = true;
    searchAreaBounds = null;
    notifyListeners();
  }

  void cameraSettled(MapBounds bounds) {
    if (!bounds.isValid) return;
    cameraMoving = false;
    viewportBounds = bounds;
    searchAreaBounds = bounds;
    notifyListeners();
  }

  void beginSearch() {
    if (searchAreaBounds == null) return;
    searchAreaBounds = null;
    notifyListeners();
  }
}

class MapEntity {
  const MapEntity({
    required this.id,
    required this.kind,
    required this.title,
    required this.subtitle,
    required this.latitude,
    required this.longitude,
    this.memberIDs = const [],
  });
  final String id;
  final MapEntityKind kind;
  final String title;
  final String subtitle;
  final double latitude;
  final double longitude;
  final List<String> memberIDs;
  bool get isCluster => kind == MapEntityKind.cluster;
}

/// Clusters nearby map projections using the current Web Mercator zoom.
/// Stable IDs are built from sorted member IDs, so UI selection never depends
/// on a transient list index.
List<MapEntity> clusterMapEntities(
  List<MapEntity> entities, {
  required double zoom,
  String? selectedId,
  double thresholdPixels = 64,
}) {
  if (entities.length < 2) return entities;
  final visited = <int>{};
  final result = <MapEntity>[];
  for (var i = 0; i < entities.length; i++) {
    if (visited.contains(i)) continue;
    final seed = entities[i];
    if (seed.id == selectedId || seed.isCluster) {
      visited.add(i);
      result.add(seed);
      continue;
    }
    final members = <int>{i};
    final queue = <int>[i];
    visited.add(i);
    while (queue.isNotEmpty) {
      final current = entities[queue.removeLast()];
      for (var j = 0; j < entities.length; j++) {
        if (visited.contains(j) ||
            entities[j].id == selectedId ||
            entities[j].isCluster) {
          continue;
        }
        if (_distanceMeters(current, entities[j]) <=
            _clusterRadiusMeters(current.latitude, zoom, thresholdPixels)) {
          visited.add(j);
          members.add(j);
          queue.add(j);
        }
      }
    }
    if (members.length == 1) {
      result.add(seed);
      continue;
    }
    final grouped = members.map((index) => entities[index]).toList()
      ..sort((a, b) => a.id.compareTo(b.id));
    final ids = grouped.map((entity) => entity.id).toList(growable: false);
    result.add(
      MapEntity(
        id: 'cluster:${ids.join('|')}',
        kind: MapEntityKind.cluster,
        title:
            '${grouped.length} ${grouped.every((i) => i.kind == MapEntityKind.place) ? '个地点' : '条内容'}',
        subtitle: '缩放地图以查看',
        latitude:
            grouped.map((entity) => entity.latitude).reduce((a, b) => a + b) /
            grouped.length,
        longitude:
            grouped.map((entity) => entity.longitude).reduce((a, b) => a + b) /
            grouped.length,
        memberIDs: ids,
      ),
    );
  }
  return result;
}

double _clusterRadiusMeters(double latitude, double zoom, double pixels) =>
    pixels *
    156543.03392 *
    math.cos(latitude * math.pi / 180) /
    math.pow(2, zoom);

double _distanceMeters(MapEntity a, MapEntity b) {
  const earthRadius = 6371000.0;
  final lat1 = a.latitude * math.pi / 180;
  final lat2 = b.latitude * math.pi / 180;
  final dLat = lat2 - lat1;
  final dLon = (b.longitude - a.longitude) * math.pi / 180;
  final haversine =
      math.sin(dLat / 2) * math.sin(dLat / 2) +
      math.cos(lat1) * math.cos(lat2) * math.sin(dLon / 2) * math.sin(dLon / 2);
  return earthRadius *
      2 *
      math.atan2(math.sqrt(haversine), math.sqrt(1 - haversine));
}

class MapEntityLayer extends StatelessWidget {
  const MapEntityLayer({
    super.key,
    required this.entities,
    required this.selectedId,
    required this.onSelected,
  });
  final List<MapEntity> entities;
  final String? selectedId;
  final ValueChanged<MapEntity> onSelected;

  @override
  Widget build(BuildContext context) => MarkerLayer(
    markers: [
      for (final entity in entities)
        Marker(
          key: ValueKey(entity.id),
          point: LatLng(entity.latitude, entity.longitude),
          width: selectedId == entity.id ? 142 : 112,
          height: 58,
          child: switch (entity.kind) {
            MapEntityKind.person => PersonMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.peopleCluster => PeopleClusterMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.activity => ActivityMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.group || MapEntityKind.community => GroupMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.organization => GroupMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.place => SelectedMarkerState(
              entity: entity,
              icon: Icons.place_outlined,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.cluster => PeopleClusterMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.moment ||
            MapEntityKind.business ||
            MapEntityKind.opportunity => SelectedMarkerState(
              entity: entity,
              icon: switch (entity.kind) {
                MapEntityKind.moment => Icons.notes_outlined,
                MapEntityKind.business => Icons.storefront_outlined,
                _ => Icons.lightbulb_outline,
              },
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
          },
        ),
    ],
  );
}

class SelectedMarkerState extends StatelessWidget {
  const SelectedMarkerState({
    super.key,
    required this.entity,
    required this.icon,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final IconData icon;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Center(
    child: Semantics(
      button: true,
      label:
          '${entity.title} · ${entity.subtitle}${entity.kind == MapEntityKind.person ? '，公开的大致区域' : ''}',
      child: Material(
        color: selected ? const Color(0xFF193B32) : Colors.white,
        elevation: selected ? 7 : 3,
        borderRadius: BorderRadius.circular(28),
        child: InkWell(
          borderRadius: BorderRadius.circular(28),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  icon,
                  size: 18,
                  color: selected ? Colors.white : const Color(0xFF193B32),
                ),
                const SizedBox(width: 5),
                Flexible(
                  child: Text(
                    entity.kind == MapEntityKind.person
                        ? '≈ ${entity.title}'
                        : entity.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w600,
                      color: selected ? Colors.white : const Color(0xFF193B32),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    ),
  );
}

class PersonMarker extends StatelessWidget {
  const PersonMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final bool selected;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => SelectedMarkerState(
    entity: entity,
    icon: Icons.person_outline,
    selected: selected,
    onTap: onTap,
  );
}

class PeopleClusterMarker extends StatelessWidget {
  const PeopleClusterMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final bool selected;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => SelectedMarkerState(
    entity: entity,
    icon: Icons.people_outline,
    selected: selected,
    onTap: onTap,
  );
}

class ActivityMarker extends StatelessWidget {
  const ActivityMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final bool selected;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => SelectedMarkerState(
    entity: entity,
    icon: Icons.event_outlined,
    selected: selected,
    onTap: onTap,
  );
}

class GroupMarker extends StatelessWidget {
  const GroupMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final bool selected;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => SelectedMarkerState(
    entity: entity,
    icon: Icons.group_outlined,
    selected: selected,
    onTap: onTap,
  );
}
