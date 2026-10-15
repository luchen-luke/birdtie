import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:latlong2/latlong.dart';

import 'map_display_policy.dart';
import 'map_marker_visual.dart';
import 'entity_cover.dart';
import 'public_moment_photo.dart';

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
    this.placeID = '',
    this.precision = 'point',
    this.publicLocation = false,
    this.groupSummary = '',
    this.isTaskResult = false,
    this.placePhoto,
    this.momentPhoto,
    this.recentMoments,
    this.mediaPixels,
    this.sourceVersion = '',
    this.momentID = '',
    this.momentRevision = 0,
  });
  final String id;
  final MapEntityKind kind;
  final String title;
  final String subtitle;
  final double latitude;
  final double longitude;
  final List<String> memberIDs;
  final String placeID, precision, groupSummary;
  final bool publicLocation, isTaskResult;
  final EntityCoverMetadata? placePhoto;
  final PublicMomentPhotoMetadata? momentPhoto;
  final MapRecentMoments? recentMoments;
  final MapMediaPixels? mediaPixels;
  final String sourceVersion;
  // The native Moment selector is separate from a pin's compound map ID.
  final String momentID;
  final int momentRevision;

  /// A typed selector never weakens the complete pin/source binding. Legacy
  /// callers may still supply exactly moment:UUID plus a canonical revision.
  ({String id, int revision})? get publicMediaIdentity {
    if (kind != MapEntityKind.moment) return null;
    final path = id.split(':'), source = sourceVersion.split(':');
    if ((path.length != 2 && path.length != 3) ||
        path.first != 'moment' ||
        !entityCoverID(path[1]))
      return null;
    final revision = int.tryParse(source.first);
    if (revision == null ||
        revision < 2 ||
        revision > entityCoverMaxVersion ||
        revision.toString() != source.first)
      return null;
    final typed = momentID.isNotEmpty || momentRevision != 0;
    if (typed && (momentID != path[1] || momentRevision != revision)) {
      return null;
    }
    if (path.length == 2) {
      if (source.length != 1) return null;
    } else if (!typed ||
        !entityCoverID(path[2]) ||
        source.length != 3 ||
        source[1] != path[2] ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(source[2])) {
      return null;
    }
    return (id: path[1], revision: revision);
  }

  // Immutable fingerprints make a removal visible even when the old live
  // pixel guard already became false before a provider's didUpdateWidget.
  String get decorationKey =>
      '${mediaPixels?.key ?? ''}:${recentMoments?.sourceKey ?? ''}';
  MapEntity withMediaPixels(
    MapMediaPixels? pixels, {
    DateTime? now,
    bool clearRecent = false,
  }) => MapEntity(
    id: id,
    kind: kind,
    title: title,
    subtitle: subtitle,
    latitude: latitude,
    longitude: longitude,
    memberIDs: memberIDs,
    placeID: placeID,
    precision: precision,
    publicLocation: publicLocation,
    groupSummary: groupSummary,
    isTaskResult: isTaskResult,
    placePhoto: placePhoto,
    momentPhoto: momentPhoto,
    recentMoments:
        !clearRecent &&
            recentMoments?.currentAt(now ?? DateTime.now().toUtc()) == true
        ? recentMoments
        : null,
    mediaPixels: pixels,
    sourceVersion: sourceVersion,
    momentID: momentID,
    momentRevision: momentRevision,
  );
  bool get isCluster => kind == MapEntityKind.cluster || memberIDs.length > 1;
  bool get hasMapAnchor =>
      const {'point', 'area'}.contains(precision) &&
      latitude.isFinite &&
      longitude.isFinite &&
      latitude >= -90 &&
      latitude <= 90 &&
      longitude >= -180 &&
      longitude <= 180;

  MapEntity withAnchorMetadata({
    required String placeID,
    required String precision,
    bool publicLocation = false,
    bool isTaskResult = false,
  }) => MapEntity(
    id: id,
    kind: kind,
    title: title,
    subtitle: subtitle,
    latitude: latitude,
    longitude: longitude,
    memberIDs: memberIDs,
    placeID: placeID,
    precision: precision,
    publicLocation: publicLocation,
    groupSummary: groupSummary,
    isTaskResult: isTaskResult,
    placePhoto: placePhoto,
    momentPhoto: momentPhoto,
    recentMoments: recentMoments,
    mediaPixels: mediaPixels,
    sourceVersion: sourceVersion,
    momentID: momentID,
    momentRevision: momentRevision,
  );
}

/// Compatibility helper for callers without a map camera. Native and web
/// renderers use their provider's actual screen projection instead.
List<MapEntity> clusterMapEntities(
  List<MapEntity> entities, {
  required double zoom,
  String? selectedId,
  double thresholdPixels = 64,
}) {
  return MapDisplayPolicy.plan(
    entities: entities,
    positions: {
      for (final entity in entities)
        if (entity.hasMapAnchor)
          entity.id: MapDisplayPolicy.mercatorPosition(entity, zoom),
    },
    zoom: zoom,
    selectedID: selectedId,
    collisionRadius: thresholdPixels,
  ).entities;
}

class MapEntityLayer extends StatelessWidget {
  const MapEntityLayer({
    super.key,
    required this.entities,
    required this.selectedId,
    required this.onSelected,
    this.labelIDs,
    this.denseIDs = const {},
    this.visualOffsets = const {},
  });
  final List<MapEntity> entities;
  final String? selectedId;
  final ValueChanged<MapEntity> onSelected;
  final Set<String>? labelIDs;
  final Set<String> denseIDs;
  final Map<String, Offset> visualOffsets;

  @override
  Widget build(BuildContext context) => MarkerLayer(
    markers: [
      for (final entity in entities)
        Marker(
          key: ValueKey(entity.id),
          point: LatLng(entity.latitude, entity.longitude),
          // Independent shapes and 48 dp targets share the real anchor.
          // Retired visualOffsets must not reintroduce displacement or lines.
          alignment: Alignment.center,
          rotate: {
            MapEntityKind.place,
            MapEntityKind.moment,
          }.contains(entity.kind),
          width:
              {MapEntityKind.place, MapEntityKind.moment}.contains(entity.kind)
              ? 48
              : labelIDs != null && !labelIDs!.contains(entity.id)
              ? 48
              : selectedId == entity.id
              ? 142
              : 112,
          height:
              {MapEntityKind.place, MapEntityKind.moment}.contains(entity.kind)
              ? 48
              : 58,
          child:
              {MapEntityKind.place, MapEntityKind.moment}.contains(entity.kind)
              ? IndividualMapMarker(
                  entity: entity,
                  selected: selectedId == entity.id,
                  dense: denseIDs.contains(entity.id),
                  showLabel: labelIDs == null || labelIDs!.contains(entity.id),
                  onTap: () => onSelected(entity),
                )
              : labelIDs != null && !labelIDs!.contains(entity.id)
              ? CompactMapMarker(
                  entity: entity,
                  onTap: () => onSelected(entity),
                )
              : switch (entity.kind) {
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

/// The shape has an independent 48 dp target; labels never resize that target.
/// Only independently current, verified Place pixels decorate this point.
class IndividualMapMarker extends StatelessWidget {
  const IndividualMapMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
    this.dense = false,
    this.showLabel = false,
  });
  final MapEntity entity;
  final bool selected, dense, showLabel;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) {
    final moment = entity.kind == MapEntityKind.moment;
    final pale = dense && !selected;
    final size = MapMarkerVisual.diameter(selected: selected, dense: pale);
    final pixels = !pale && entity.mediaPixels?.current() == true
        ? entity.mediaPixels
        : null;
    final recent = !moment
        ? entity.recentMoments?.labelAt(DateTime.now().toUtc())
        : null;
    final color = moment ? const Color(0xFF87683C) : const Color(0xFF193B32);
    return Semantics(
      button: true,
      selected: selected,
      label: '${moment ? '动态' : '地点'} · ${entity.title} · ${entity.subtitle}',
      excludeSemantics: true,
      onTap: onTap,
      child: SizedBox(
        width: 48,
        height: 48,
        child: Stack(
          clipBehavior: Clip.none,
          children: [
            IconButton(
              key: ValueKey('individual-map-target:${entity.id}'),
              tooltip: entity.title.isEmpty && moment ? '查看动态' : entity.title,
              constraints: const BoxConstraints.tightFor(width: 48, height: 48),
              padding: EdgeInsets.zero,
              onPressed: onTap,
              icon: Container(
                key: ValueKey('individual-map-shape:${entity.id}'),
                width: size,
                height: size,
                decoration: BoxDecoration(
                  shape: moment ? BoxShape.rectangle : BoxShape.circle,
                  borderRadius: moment
                      ? BorderRadius.circular(
                          MapMarkerVisual.radius(dense: pale),
                        )
                      : null,
                  color: color,
                  border: Border.all(
                    color: selected ? MapMarkerVisual.placeColor : Colors.white,
                    width: selected ? 2 : 1.5,
                  ),
                ),
                child: pixels != null
                    ? ClipRRect(
                        borderRadius: BorderRadius.circular(moment ? 4 : 999),
                        child: RawImage(
                          key: ValueKey(
                            '${moment ? 'map-moment-photo' : 'map-place-photo'}:${entity.id}',
                          ),
                          image: pixels.image,
                          fit: BoxFit.cover,
                        ),
                      )
                    : Icon(
                        moment ? Icons.notes_outlined : Icons.place_outlined,
                        color: Colors.white,
                        size: selected
                            ? 18
                            : pale
                            ? 10
                            : 14,
                      ),
              ),
            ),
            if (showLabel &&
                !pale &&
                (entity.title.isNotEmpty || recent != null))
              Positioned(
                top: 46,
                left: -48,
                width: MapMarkerVisual.labelWidth,
                child: IgnorePointer(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      if (entity.title.isNotEmpty)
                        Text(
                          entity.title,
                          maxLines: selected ? 2 : 1,
                          overflow: TextOverflow.ellipsis,
                          textAlign: TextAlign.center,
                          style: const TextStyle(
                            fontSize: MapMarkerVisual.labelFontSize,
                            color: Color(0xFF193B32),
                          ),
                        ),
                      if (recent != null)
                        Text(
                          recent,
                          key: ValueKey('map-place-recent:${entity.id}'),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 12,
                            color: Color(0xFF193B32),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class CompactMapMarker extends StatelessWidget {
  const CompactMapMarker({
    super.key,
    required this.entity,
    required this.onTap,
  });
  final MapEntity entity;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => Semantics(
    button: true,
    label: '${entity.title} · ${entity.subtitle}',
    child: Tooltip(
      message: entity.title,
      child: IconButton(
        constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
        onPressed: onTap,
        icon: Icon(
          switch (entity.kind) {
            MapEntityKind.activity => Icons.event_outlined,
            MapEntityKind.moment => Icons.notes_outlined,
            MapEntityKind.organization ||
            MapEntityKind.group ||
            MapEntityKind.community => Icons.groups_outlined,
            MapEntityKind.business => Icons.storefront_outlined,
            MapEntityKind.opportunity => Icons.lightbulb_outline,
            MapEntityKind.person => Icons.person_outline,
            _ => Icons.place_outlined,
          },
          size: 24,
          color: const Color(0xFF193B32),
        ),
      ),
    ),
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
