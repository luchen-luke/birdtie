import 'map_content_route_geometry.dart';
import 'package:flutter/foundation.dart' show listEquals;
import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:latlong2/latlong.dart';

import 'map_display_policy.dart';
import 'map_marker_visual.dart';
import 'map_marker_widget.dart';
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
      west.isFinite &&
      east.isFinite &&
      south.isFinite &&
      north.isFinite &&
      west >= -180 &&
      west <= 180 &&
      east >= -180 &&
      east <= 180 &&
      south >= -90 &&
      north <= 90 &&
      west != east &&
      south < north;

  bool contains(double latitude, double longitude) =>
      isValid &&
      latitude.isFinite &&
      longitude.isFinite &&
      latitude >= south &&
      latitude <= north &&
      (west < east
          ? longitude >= west && longitude <= east
          : longitude >= west || longitude <= east);
  double get longitudeSpan => west < east ? east - west : 360 - west + east;

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
  double zoom = 12;

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

/// Finite issuer authority for a source already read by this controller.
/// Used only to retire native copies; never authorizes a new read or action.
class MapEntityReadGrant {
  MapEntityReadGrant({
    required MapEntity source,
    required this.validUntil,
    required bool Function() current,
    MapMediaPixels? copiedPixels,
    Object? issuerScope,
    this.observedAt,
  }) : _source = source,
       _current = current,
       _pixels = copiedPixels,
       _issuerScope = issuerScope;
  final MapEntity _source;
  final DateTime validUntil;
  final bool Function() _current;
  final MapMediaPixels? _pixels;
  final Object? _issuerScope;
  final DateTime? observedAt;
  bool _retired = false;

  bool currentAt(DateTime at) {
    if (_retired) return false;
    if (!at.isBefore(validUntil) || !_current()) _retired = true;
    return !_retired;
  }

  bool authorizes(MapEntity entity, DateTime at) =>
      currentAt(at) &&
      entity.sourceVersion == _source.sourceVersion &&
      _sameNativeFacts(entity);

  bool _sameNativeFacts(MapEntity entity) =>
      entity.id == _source.id &&
      entity.kind == _source.kind &&
      entity.latitude == _source.latitude &&
      entity.longitude == _source.longitude &&
      entity.precision == _source.precision &&
      entity.publicLocation == _source.publicLocation &&
      entity.isTaskResult == _source.isTaskResult &&
      entity.groupSummary == _source.groupSummary &&
      entity.title == _source.title &&
      entity.subtitle == _source.subtitle &&
      entity.categoryCode == _source.categoryCode &&
      entity.placeID == _source.placeID &&
      entity.momentID == _source.momentID &&
      entity.momentRevision == _source.momentRevision &&
      entity.anchorID == _source.anchorID &&
      entity.entryID == _source.entryID &&
      entity.threadID == _source.threadID &&
      entity.contentTime == _source.contentTime &&
      entity.threadPosition == _source.threadPosition &&
      listEquals(entity.nextContentIDs, _source.nextContentIDs) &&
      listEquals(entity.journeyEdges, _source.journeyEdges) &&
      listEquals(entity.memberIDs, _source.memberIDs) &&
      entity.contentCount == _source.contentCount &&
      entity.anchorCount == _source.anchorCount;

  /// A later independent proof can authorize the exact same displayed facts.
  /// This never changes or revives the previous grant, its reads, or deadline.
  bool authorizesNativeReplacement(MapEntity previous, DateTime at) {
    final old = previous.readGrant;
    return old != null &&
        _issuerScope != null &&
        _issuerScope == old._issuerScope &&
        observedAt != null &&
        old.observedAt != null &&
        observedAt!.isAfter(old.observedAt!) &&
        at.isBefore(old.validUntil) &&
        currentAt(at) &&
        _sameNativeFacts(previous);
  }

  bool authorizesCopiedPixels(MapMediaPixels pixels, DateTime at) =>
      currentAt(at) &&
      _pixels != null &&
      _pixels.key == pixels.key &&
      identical(_pixels.normalPNG, pixels.normalPNG) &&
      identical(_pixels.selectedPNG, pixels.selectedPNG) &&
      _pixels.devicePixelRatio == pixels.devicePixelRatio &&
      pixels.validUntil != null &&
      at.isBefore(pixels.validUntil!);
}

/// A published Journey's explicit adjacent, location-approved content edge.
class MapJourneyEdge {
  const MapJourneyEdge({
    required this.journeyID,
    required this.publicationRevision,
    required this.nextID,
    required this.fromPosition,
    required this.toPosition,
    required this.sourceVersion,
  });
  final String journeyID, nextID, sourceVersion;
  final int publicationRevision, fromPosition, toPosition;
  String get identity =>
      'journey:$journeyID:$publicationRevision:$fromPosition:$toPosition';
  @override
  bool operator ==(Object other) =>
      other is MapJourneyEdge &&
      journeyID == other.journeyID &&
      publicationRevision == other.publicationRevision &&
      nextID == other.nextID &&
      fromPosition == other.fromPosition &&
      toPosition == other.toPosition &&
      sourceVersion == other.sourceVersion;
  @override
  int get hashCode => Object.hash(
    journeyID,
    publicationRevision,
    nextID,
    fromPosition,
    toPosition,
    sourceVersion,
  );
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
    this.readGrant,
    this.contentRoute,
    this.sourceVersion = '',
    this.momentID = '',
    this.momentRevision = 0,
    this.contentTime,
    this.nextContentIDs = const [],
    this.journeyEdges = const [],
    this.threadPosition = 0,
    this.categoryCode = '',
    this.anchorID = '',
    this.entryID = '',
    this.threadID = '',
    this.contentCount = 0,
    this.anchorCount = 0,
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
  final MapEntityReadGrant? readGrant;
  final MapContentRouteGeometry? contentRoute;
  final String sourceVersion;
  final String categoryCode;
  final String anchorID, entryID, threadID;
  final int contentCount, anchorCount;
  // The native Moment selector is separate from a pin's compound map ID.
  final String momentID;
  final int momentRevision;
  final DateTime? contentTime;
  final List<String> nextContentIDs;
  final List<MapJourneyEdge> journeyEdges;
  final int threadPosition;

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
      '$categoryCode:${mediaPixels?.key ?? ''}:${recentMoments?.sourceKey ?? ''}:${contentRoute?.fingerprint ?? ''}';
  MapEntity withMediaPixels(
    MapMediaPixels? pixels, {
    DateTime? now,
    bool clearRecent = false,
    MapEntityReadGrant? readGrant,
    MapContentRouteGeometry? contentRoute,
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
    readGrant: readGrant ?? this.readGrant,
    contentRoute: contentRoute ?? this.contentRoute,
    sourceVersion: sourceVersion,
    momentID: momentID,
    momentRevision: momentRevision,
    contentTime: contentTime,
    nextContentIDs: nextContentIDs,
    journeyEdges: journeyEdges,
    threadPosition: threadPosition,
    categoryCode: categoryCode,
    anchorID: anchorID,
    entryID: entryID,
    threadID: threadID,
    contentCount: contentCount,
    anchorCount: anchorCount,
  );
  bool get isCluster => kind == MapEntityKind.cluster || memberIDs.length > 1;
  bool get hasMapAnchor =>
      const {'point', 'area', 'approximate'}.contains(precision) &&
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
    readGrant: readGrant,
    contentRoute: contentRoute,
    sourceVersion: sourceVersion,
    momentID: momentID,
    momentRevision: momentRevision,
    contentTime: contentTime,
    nextContentIDs: nextContentIDs,
    journeyEdges: journeyEdges,
    threadPosition: threadPosition,
    categoryCode: categoryCode,
    anchorID: anchorID,
    entryID: entryID,
    threadID: threadID,
    contentCount: contentCount,
    anchorCount: anchorCount,
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
    this.placements = const {},
    this.visualOffsets = const {},
  });
  final List<MapEntity> entities;
  final String? selectedId;
  final ValueChanged<MapEntity> onSelected;
  final Set<String>? labelIDs;
  final Set<String> denseIDs;
  final Map<String, MapLabelPlacement> placements;
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
              {
                MapEntityKind.place,
                MapEntityKind.moment,
                MapEntityKind.cluster,
              }.contains(entity.kind)
              ? 48
              : labelIDs != null && !labelIDs!.contains(entity.id)
              ? 48
              : selectedId == entity.id
              ? 142
              : 112,
          height:
              {
                MapEntityKind.place,
                MapEntityKind.moment,
                MapEntityKind.cluster,
              }.contains(entity.kind)
              ? 48
              : 58,
          child: entity.kind == MapEntityKind.cluster
              ? ContentClusterMapMarker(
                  entity: entity,
                  selected: selectedId == entity.id,
                  onTap: () => onSelected(entity),
                )
              : {
                  MapEntityKind.place,
                  MapEntityKind.moment,
                }.contains(entity.kind)
              ? IndividualMapMarker(
                  entity: entity,
                  selected: selectedId == entity.id,
                  dense: denseIDs.contains(entity.id),
                  placement: placements[entity.id],
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
    this.placement,
  });
  final MapEntity entity;
  final bool selected, dense, showLabel;
  final MapLabelPlacement? placement;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => CompactMapPointMarker(
    id: entity.id,
    title: entity.title,
    subtitle: entity.subtitle,
    moment: entity.kind == MapEntityKind.moment,
    selected: selected,
    onTap: onTap,
    dense: dense,
    showLabel: showLabel,
    placement: placement,
    mediaPixels: entity.mediaPixels,
    recentMoments: entity.recentMoments,
    categoryCode: entity.categoryCode,
  );
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

class ContentClusterMapMarker extends StatelessWidget {
  const ContentClusterMapMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final bool selected;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) {
    final count = MapMarkerVisual.clusterCount(
      categoryCode: entity.categoryCode,
      contentCount: entity.contentCount,
      anchorCount: entity.anchorCount,
      fallback: entity.memberIDs.length,
    );
    final label = '$count${MapMarkerVisual.clusterUnit(entity.categoryCode)}';
    final size = selected ? 30.0 : 28.0;
    return Semantics(
      identifier: 'birdtie-map-point:${entity.id}',
      button: true,
      label: label,
      child: SizedBox(
        width: 48,
        height: 48,
        child: IconButton(
          tooltip: label,
          padding: EdgeInsets.zero,
          onPressed: onTap,
          icon: Container(
            width: size,
            height: size,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: const Color(0xFF64707A),
              shape: BoxShape.circle,
              border: Border.all(color: Colors.white, width: 1.5),
            ),
            child: Text(
              count > 99 ? '99+' : '$count',
              style: const TextStyle(
                color: Colors.white,
                fontSize: 12,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
        ),
      ),
    );
  }
}
