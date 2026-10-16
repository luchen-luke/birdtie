import 'dart:convert';
import 'package:flutter/material.dart';
import 'public_city_controller.dart';
import '../workspace/map_entities.dart';
import '../workspace/map_display_policy.dart';
import 'map_coordinate_boundary.dart';

/// SDK returns GCJ-02 screen corners. Only this boundary makes WGS84 query
/// bounds; it is never used to rewrite a stored object or infer its country.
MapBounds? canonicalAMapViewport(List<Offset> sdkPoints) {
  if (sdkPoints.length != 5 ||
      sdkPoints.any(
        (p) =>
            !p.dx.isFinite ||
            !p.dy.isFinite ||
            p.dx.abs() > 180 ||
            p.dy.abs() > 90,
      )) {
    return null;
  }
  final points = sdkPoints.map((p) {
    final wgs = MapCoordinateBoundary.fromAmap(p.dy, p.dx);
    return Offset(wgs.longitude, wgs.latitude);
  }).toList();
  return MapDisplayPolicy.projectedViewport(
    corners: points.take(4).toList(),
    center: points.last,
  );
}

/// Shared source/projection boundary, also used by the web adapter. These
/// types require neither JavaScript nor an AMap key for boundary tests.
class AMapSourceGate {
  int revision = 0, _latestFrame = 0;
  Object? _scope;
  String? _fingerprint;
  bool _disposed = false;
  Map<String, MapEntity> _rows = const {};
  List<MapEntity> get rows => List.unmodifiable(_rows.values);
  void retireProjection() {
    if (_disposed) return;
    ++revision;
    _latestFrame = 0;
  }

  bool replace(List<MapEntity> source, Object? scope) {
    if (_disposed) return false;
    final unique = <String, MapEntity>{};
    for (final e in source) {
      if (!e.hasMapAnchor || e.id.isEmpty) continue;
      unique.putIfAbsent(
        e.id,
        () => MapEntity(
          id: e.id,
          kind: e.kind,
          title: e.title,
          subtitle: e.subtitle,
          latitude: e.latitude,
          longitude: e.longitude,
          memberIDs: List.unmodifiable(e.memberIDs),
          placeID: e.placeID,
          precision: e.precision,
          publicLocation: e.publicLocation,
          groupSummary: e.groupSummary,
          isTaskResult: e.isTaskResult,
          placePhoto: e.placePhoto,
          momentPhoto: e.momentPhoto,
          recentMoments: e.recentMoments,
          mediaPixels: e.mediaPixels,
          sourceVersion: e.sourceVersion,
          momentID: e.momentID,
          momentRevision: e.momentRevision,
          categoryCode: e.categoryCode,
          anchorID: e.anchorID,
          entryID: e.entryID,
          threadID: e.threadID,
          contentCount: e.contentCount,
          anchorCount: e.anchorCount,
        ),
      );
    }
    final ids = unique.keys.toList()..sort();
    final fingerprint = jsonEncode([
      for (final id in ids)
        () {
          final e = unique[id]!;
          return [
            e.id,
            e.kind.name,
            e.title,
            e.subtitle,
            e.latitude,
            e.longitude,
            e.precision,
            e.placeID,
            e.memberIDs,
            e.publicLocation,
            e.groupSummary,
            e.isTaskResult,
            e.decorationKey,
            e.mediaPixels?.validUntil?.microsecondsSinceEpoch,
            e.sourceVersion,
            e.momentID,
            e.momentRevision,
            e.anchorID,
            e.entryID,
            e.threadID,
            e.contentCount,
            e.anchorCount,
          ];
        }(),
    ]);
    if (_fingerprint == fingerprint && _scope == scope) return false;
    ++revision;
    _scope = scope;
    _fingerprint = fingerprint;
    _latestFrame = 0;
    _rows = Map.unmodifiable({for (final id in ids) id: unique[id]!});
    return true;
  }

  MapEntity? resolve(int sourceRevision, String id, String kind) {
    if (_disposed || sourceRevision != revision) return null;
    final row = _rows[id];
    return row?.kind.name == kind ? row : null;
  }

  AMapDisplayFrame? acceptFrame(
    String raw, {
    String? selectedID,
    Rect? contentRect,
  }) {
    if (_disposed) return null;
    try {
      final v = jsonDecode(raw);
      if (v is! Map ||
          v.length != 5 ||
          v['schema'] != 'birdtie-amap-frame-v1' ||
          v['revision'] != revision ||
          v['frame'] is! int ||
          (v['frame'] as int) <= _latestFrame ||
          v['zoom'] is! num ||
          !(v['zoom'] as num).isFinite ||
          (v['zoom'] as num) < 1 ||
          (v['zoom'] as num) > 20 ||
          v['positions'] is! List)
        return null;
      final positions = <String, Offset>{};
      for (final p in v['positions'] as List) {
        if (p is! Map ||
            p.length != 3 ||
            p['id'] is! String ||
            p['x'] is! num ||
            p['y'] is! num ||
            !(p['x'] as num).isFinite ||
            !(p['y'] as num).isFinite ||
            (p['x'] as num).abs() > 10000000 ||
            (p['y'] as num).abs() > 10000000 ||
            !_rows.containsKey(p['id']) ||
            positions.containsKey(p['id']))
          return null;
        positions[p['id'] as String] = Offset(
          (p['x'] as num).toDouble(),
          (p['y'] as num).toDouble(),
        );
      }
      if (positions.length != _rows.length) return null;
      _latestFrame = v['frame'] as int;
      final offsets = MapDisplayPolicy.coincidentPointOffsets(
        rows,
        positions: positions,
      );
      final plan = MapDisplayPolicy.plan(
        entities: rows,
        positions: positions,
        zoom: (v['zoom'] as num).toDouble(),
        selectedID: selectedID,
        contentRect: contentRect,
      );
      return AMapDisplayFrame(
        revision,
        _latestFrame,
        rows,
        Map.unmodifiable(positions),
        offsets,
        plan,
      );
    } catch (_) {
      return null;
    }
  }

  void dispose() {
    _disposed = true;
    ++revision;
    _rows = const {};
    _scope = null;
    _fingerprint = null;
  }
}

class AMapDisplayFrame {
  const AMapDisplayFrame(
    this.revision,
    this.frame,
    this.rows,
    this.positions,
    this.offsets,
    this.plan,
  );
  final int revision, frame;
  final List<MapEntity> rows;
  final Map<String, Offset> positions, offsets;
  final MapDisplayPlan plan;
  Map<String, Offset> get displayed =>
      Map.unmodifiable({...positions, ...plan.clusterPositions});
  MapEntity? resolveDisplay(String id, String kind) =>
      plan.entities.where((e) => e.id == id && e.kind.name == kind).firstOrNull;
  List<MapEntity> candidates(String id) {
    final tapped = rows.where((e) => e.id == id).firstOrNull;
    if (tapped == null) return const [];
    final ids = MapDisplayPolicy.overlappingHitTargets(
      entities: rows,
      positions: displayed,
      tappedID: id,
    ).map((e) => e.id).toSet();
    if (tapped.precision == 'point' &&
        {MapEntityKind.place, MapEntityKind.moment}.contains(tapped.kind)) {
      ids.addAll(
        rows
            .where(
              (e) =>
                  e.precision == 'point' &&
                  {
                    MapEntityKind.place,
                    MapEntityKind.moment,
                  }.contains(e.kind) &&
                  e.latitude == tapped.latitude &&
                  e.longitude == tapped.longitude,
            )
            .map((e) => e.id),
      );
    }
    return rows.where((e) => ids.contains(e.id)).toList();
  }

  Map<String, Object?> layout(String? selectedID) => {
    'schema': 'birdtie-amap-layout-v1',
    'revision': revision,
    'frame': frame,
    'markers': [
      for (final e in plan.entities)
        {
          'id': e.id,
          'dx': 0,
          'dy': 0,
          'selected': selectedID == e.id,
          'dense': plan.denseIDs.contains(e.id),
          'showLabel': plan.labelIDs.contains(e.id),
          if (MapDisplayPolicy.displayCluster(e)) ...{
            'cluster': {
              'id': e.id,
              'kind': e.kind.name,
              'title': e.title,
              'subtitle': e.subtitle,
              'memberIDs': e.memberIDs,
              'contentCount': e.memberIDs.length,
              'precision': 'area',
              'anchorID': e.memberIDs.first,
            },
          },
        },
    ],
  };
}

class AMapCityMapView extends StatelessWidget {
  const AMapCityMapView({
    super.key,
    required this.city,
    required this.places,
    required this.entities,
    required this.selectedEntityId,
    required this.onPlaceSelected,
    required this.onEntitySelected,
    required this.fullBleed,
    this.contextKey = '',
    this.readerChanges,
    this.currentReaderEntities,
    this.currentReaderScope,
    this.onMapReaderVisibilityChanged,
    this.onSelectedAnchor,
    this.onCameraMotion,
    this.onMapUnavailable,
    this.preserveViewport = false,
    this.focusRevision = 0,
    this.contentRect,
    this.ornamentTop,
    this.onViewportSettled,
    this.onViewportInitialized,
    this.onContentViewportSettled,
    this.onContentOverlap,
    this.onCoordinateSelected,
    this.coordinateSourceCurrent,
  });
  final PublicCity city;
  final List<PublicPlace> places;
  final List<MapEntity> entities;
  final String? selectedEntityId;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final ValueChanged<MapEntity>? onEntitySelected;
  final bool fullBleed, preserveViewport;
  final String contextKey;
  final Listenable? readerChanges;
  final List<MapEntity> Function()? currentReaderEntities;
  final Object? Function()? currentReaderScope;
  final ValueChanged<({Object token, bool visible})>?
  onMapReaderVisibilityChanged;
  final ValueChanged<({String? id, Offset? point})>? onSelectedAnchor;
  final VoidCallback? onCameraMotion;
  final ValueChanged<String>? onMapUnavailable;
  final int focusRevision;
  final Rect? contentRect;
  final double? ornamentTop;
  final ValueChanged<MapBounds>? onViewportSettled, onViewportInitialized;
  final void Function(MapBounds, double)? onContentViewportSettled;
  final ValueChanged<List<MapEntity>>? onContentOverlap;
  final void Function(double, double)? onCoordinateSelected;
  final bool Function()? coordinateSourceCurrent;
  @override
  Widget build(BuildContext context) => const Center(
    child: Padding(
      padding: EdgeInsets.all(24),
      child: Text('该设备上的高德地图尚未配置，请使用地点列表。'),
    ),
  );
}
