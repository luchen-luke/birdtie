import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'package:crypto/crypto.dart';
import 'package:flutter/painting.dart';

import 'map_entities.dart';
import 'map_marker_visual.dart';

/// A single native mutation is allowed to run, with one replaceable pending
/// frame. Callers fence awaits with [serial]; no Future chain grows per image.
class LatestMapRenderQueue {
  LatestMapRenderQueue(this.render);
  final Future<void> Function(int serial) render;
  int serial = 0, requested = 0, passes = 0, coalesced = 0, peakPending = 0;
  bool _pending = false, _closed = false;
  Completer<void>? _flight;

  Future<void> schedule() {
    if (_closed) return Future<void>.value();
    serial++;
    requested++;
    if (_pending) coalesced++;
    _pending = true;
    peakPending = 1;
    final existing = _flight;
    if (existing != null) return existing.future;
    final flight = _flight = Completer<void>();
    unawaited(_drain(flight));
    return flight.future;
  }

  Future<void> _drain(Completer<void> flight) async {
    while (!_closed && _pending) {
      _pending = false;
      passes++;
      try {
        await render(serial);
      } catch (_) {
        // A failed provider update does not poison subsequent latest work.
      }
    }
    _flight = null;
    flight.complete();
  }

  void close() {
    _closed = true;
    _pending = false;
    serial++;
  }
}

/// Display-only output: the authorized ResultSet and its list are untouched.
class MapDisplayPlan {
  const MapDisplayPlan(
    this.entities,
    this.labelIDs,
    this.omittedIDs, {
    this.denseIDs = const {},
    this.clusterPositions = const {},
  });
  final List<MapEntity> entities;
  final Set<String> labelIDs, omittedIDs, denseIDs;
  final Map<String, Offset> clusterPositions;
}

class MapDisplayLevel {
  const MapDisplayLevel(this.maxZoomExclusive, this.iconCellLogicalPixels);
  final double maxZoomExclusive, iconCellLogicalPixels;
  double get collisionRadius => iconCellLogicalPixels / 2;
}

/// Provider-independent policy. Input positions are map-local logical pixels
/// returned by the current camera, after the source has enforced its ACL/lease.
class MapDisplayPolicy {
  /// Longitude is unwrapped around the actual projected centre, so a rotated
  /// viewport spanning 180 degrees stays a short crossing interval. Inputs
  /// come from provider screen projection, never a city/GPS approximation.
  static MapBounds? projectedViewport({
    required List<Offset> corners,
    required Offset center,
  }) {
    if (corners.length != 4 ||
        [center, ...corners].any(
          (p) => !p.dx.isFinite || !p.dy.isFinite || p.dy < -90 || p.dy > 90,
        )) {
      return null;
    }
    double wrap(double longitude) => (longitude + 180) % 360 - 180;
    final longitudes = corners.map((p) => center.dx + wrap(p.dx - center.dx));
    final west = longitudes.reduce(math.min),
        east = longitudes.reduce(math.max);
    final bounds = MapBounds(
      west: wrap(west),
      east: wrap(east),
      south: corners.map((p) => p.dy).reduce(math.min),
      north: corners.map((p) => p.dy).reduce(math.max),
    );
    return bounds.isValid ? bounds : null;
  }

  static bool contentEntity(MapEntity entity) =>
      entity.id.startsWith('owned-map:');

  static bool displayCluster(MapEntity entity) =>
      entity.kind == MapEntityKind.cluster &&
      entity.id.startsWith('display-cluster:');

  static String _membersVersion(List<MapEntity> members) => sha256
      .convert(
        utf8.encode(
          jsonEncode([
            for (final e in members)
              [
                e.id,
                e.kind.name,
                e.sourceVersion,
                e.latitude,
                e.longitude,
                e.title,
                e.subtitle,
                e.precision,
              ],
          ]),
        ),
      )
      .toString();

  /// A display container carries no independent authority. Every original
  /// member and revision must still be present before its reader can open.
  static List<MapEntity> currentDisplayClusterMembers(
    MapEntity cluster,
    Iterable<MapEntity> source,
  ) {
    if (!displayCluster(cluster)) return const [];
    final byID = {for (final e in source) e.id: e};
    final members = [
      for (final id in cluster.memberIDs)
        if (byID[id] != null) byID[id]!,
    ];
    return members.length == cluster.memberIDs.length &&
            _membersVersion(members) == cluster.sourceVersion
        ? members
        : const [];
  }

  static ({List<MapEntity> entities, Map<String, Offset> positions})
  _boundedTasks(
    List<MapEntity> source,
    Map<String, Offset> positions,
    String? selectedID,
    int budget,
  ) {
    final keep = source
        .where((e) => contentEntity(e) || e.id == selectedID)
        .toList();
    final candidates = source
        .where((e) => !contentEntity(e) && e.id != selectedID)
        .toList();
    final slots = math.max(1, budget - keep.length);
    if (candidates.length <= slots)
      return (entities: source, positions: const {});
    Map<(int, int), List<MapEntity>> cells = {};
    var edge = 64.0;
    for (var attempt = 0; attempt < 24; attempt++, edge *= 2) {
      cells = {};
      for (final entity in candidates) {
        final p = positions[entity.id]!;
        (cells[((p.dx / edge).floor(), (p.dy / edge).floor())] ??= []).add(
          entity,
        );
      }
      if (cells.length <= slots) break;
    }
    var groups = cells.values.toList();
    if (groups.length > slots) {
      // At low budgets a screen may straddle the grid origin. Spatially
      // ordered partitions stay bounded without dropping any member.
      candidates.sort((a, b) {
        final x = positions[a.id]!.dx.compareTo(positions[b.id]!.dx);
        return x != 0 ? x : positions[a.id]!.dy.compareTo(positions[b.id]!.dy);
      });
      final count = (candidates.length / slots).ceil();
      groups = [
        for (var i = 0; i < candidates.length; i += count)
          candidates.sublist(i, math.min(i + count, candidates.length)),
      ];
    }
    final result = <MapEntity>[...keep], projected = <String, Offset>{};
    for (final members in groups) {
      if (members.length == 1) {
        result.add(members.single);
        continue;
      }
      members.sort((a, b) => a.id.compareTo(b.id));
      final id =
          'display-cluster:${sha256.convert(utf8.encode(jsonEncode(members.map((e) => e.id).toList())))}';
      // The container is anchored on a real member, never a fabricated POI.
      final anchor = members.first;
      result.add(
        MapEntity(
          id: id,
          kind: MapEntityKind.cluster,
          title: '${members.length}项内容',
          subtitle: '查看当前范围内的对象',
          latitude: anchor.latitude,
          longitude: anchor.longitude,
          precision: 'area',
          memberIDs: members.map((e) => e.id).toList(),
          sourceVersion: _membersVersion(members),
          groupSummary: '${members.length}个当前对象',
          isTaskResult: true,
        ),
      );
      projected[id] = positions[anchor.id]!;
    }
    return (entities: result, positions: projected);
  }

  // Concept mapping from the reviewed Civu proposed display policy. These
  // fields are consumed here; no Civu config or unpublished taxonomy is loaded.
  static const levels = [
    MapDisplayLevel(10.5, 104),
    MapDisplayLevel(12.5, 86),
    MapDisplayLevel(14.5, 68),
    MapDisplayLevel(16, 54),
    MapDisplayLevel(double.infinity, 44),
  ];
  static EdgeInsets focusPadding(Size mapSize, Rect? contentRect) {
    if (contentRect == null ||
        contentRect.isEmpty ||
        !mapSize.width.isFinite ||
        !mapSize.height.isFinite) {
      return const EdgeInsets.fromLTRB(48, 150, 48, 180);
    }
    final visible = contentRect.intersect(Offset.zero & mapSize);
    if (visible.isEmpty) return const EdgeInsets.all(24);
    final edge = math.min(24.0, math.min(visible.width, visible.height) / 4);
    return EdgeInsets.fromLTRB(
      visible.left + edge,
      visible.top + edge,
      mapSize.width - visible.right + edge,
      mapSize.height - visible.bottom + edge,
    );
  }

  static double radiusForZoom(double zoom) => levels
      .firstWhere((level) => zoom < level.maxZoomExclusive)
      .collisionRadius;

  static MapDisplayPlan plan({
    required List<MapEntity> entities,
    required Map<String, Offset> positions,
    required double zoom,
    String? selectedID,
    Rect? contentRect,
    Set<String> previouslyVisibleIDs = const {},
    Set<String> protectedIDs = const {},
    double? collisionRadius,
    int ambientLimit = 40,
  }) {
    final unique = <String, MapEntity>{};
    for (final entity in entities) {
      final point = positions[entity.id];
      if (entity.hasMapAnchor &&
          point != null &&
          point.dx.isFinite &&
          point.dy.isFinite) {
        unique.putIfAbsent(entity.id, () => entity);
      }
    }
    final bounded = _boundedTasks(
      unique.values.toList(),
      positions,
      selectedID,
      ambientLimit.clamp(1, 40),
    );
    positions = {...positions, ...bounded.positions};
    final ordered = bounded.entities..sort((a, b) => a.id.compareTo(b.id));
    final emphasized = {...protectedIDs, ?selectedID};
    final requestedRadius = collisionRadius ?? radiusForZoom(zoom);
    final radius = requestedRadius.isFinite && requestedRadius > 0
        ? requestedRadius
        : 24.0;
    final cells = <(int, int), List<MapEntity>>{};
    (int, int) cell(Offset p) =>
        ((p.dx / radius).floor(), (p.dy / radius).floor());
    for (final entity in ordered) {
      (cells[cell(positions[entity.id]!)] ??= []).add(entity);
    }
    final dense = <String>{};
    for (final entity in ordered) {
      if (emphasized.contains(entity.id) ||
          entity.precision != 'point' ||
          contentEntity(entity))
        continue;
      final point = positions[entity.id]!, key = cell(point);
      var nearby = false;
      for (var dx = -1; dx <= 1 && !nearby; dx++) {
        for (var dy = -1; dy <= 1 && !nearby; dy++) {
          nearby = (cells[(key.$1 + dx, key.$2 + dy)] ?? const <MapEntity>[])
              .any(
                (other) =>
                    other.id != entity.id &&
                    (positions[other.id]! - point).distance <= radius,
              );
        }
      }
      if (nearby) dense.add(entity.id);
    }
    final labels = <String>{};
    // Civu content is useful at the opening city zoom too. Keep a small,
    // collision-aware set of real photo/story points legible among catalog
    // points. Density changes presentation only, never source coordinates.
    final content = ordered
        .where((e) => e.id.startsWith('owned-map:'))
        .toList();
    int contentRank(MapEntity e) =>
        (e.recentMoments?.labelAt(DateTime.now().toUtc()) != null ? 4 : 0) +
        (e.mediaPixels?.current() == true ? 2 : 0);
    content.sort((a, b) {
      final rank = contentRank(b).compareTo(contentRank(a));
      return rank != 0 ? rank : a.id.compareTo(b.id);
    });
    Rect labelRect(MapEntity e) {
      final p = positions[e.id]!;
      final selected = e.id == selectedID;
      final start = e.kind == MapEntityKind.cluster
          ? (selected ? 25.0 : 23.0)
          : MapMarkerVisual.diameter(
                      selected: selected,
                      dense: dense.contains(e.id),
                    ) /
                    2 +
                MapMarkerVisual.labelGap;
      return Rect.fromLTWH(
        p.dx + start,
        p.dy - 20,
        MapMarkerVisual.labelWidth,
        40,
      );
    }

    // Icon overlap remains resolvable through the existing content chooser.
    // Labels must not paint over another current glyph, especially the count
    // inside a content cluster. This changes no identity or real coordinate.
    final glyphs = <String, Rect>{
      for (final e in ordered)
        if (e.kind == MapEntityKind.cluster ||
            (e.precision == 'point' &&
                {MapEntityKind.place, MapEntityKind.moment}.contains(e.kind)))
          e.id: Rect.fromCenter(
            center: positions[e.id]!,
            width: e.kind == MapEntityKind.cluster
                ? 44
                : MapMarkerVisual.diameter(
                    selected: e.id == selectedID,
                    dense: dense.contains(e.id),
                  ),
            height: e.kind == MapEntityKind.cluster
                ? 44
                : MapMarkerVisual.diameter(
                    selected: e.id == selectedID,
                    dense: dense.contains(e.id),
                  ),
          ),
    };
    final occupied = <Rect>[];
    bool addLabel(MapEntity entity) {
      if (labels.contains(entity.id)) return true;
      if (labels.length >= MapMarkerVisual.labelBudget) return false;
      final rect = labelRect(entity);
      if (occupied.any((other) => other.overlaps(rect.inflate(4))) ||
          glyphs.entries.any(
            (other) =>
                other.key != entity.id && other.value.overlaps(rect.inflate(4)),
          ))
        return false;
      labels.add(entity.id);
      occupied.add(rect);
      return true;
    }

    for (final entity in [
      ...ordered.where((e) => e.id == selectedID),
      ...ordered.where((e) => e.id != selectedID && emphasized.contains(e.id)),
    ]) {
      addLabel(entity);
    }
    for (final entity in content) {
      if (labels.length >= MapMarkerVisual.labelBudget) break;
      if (labels.contains(entity.id)) continue;
      if (contentRect != null && !contentRect.contains(positions[entity.id]!))
        continue;
      if (!addLabel(entity)) continue;
      if (contentRank(entity) > 0) dense.remove(entity.id);
    }
    if (zoom >= 16) {
      for (final entity in ordered) {
        if (labels.length >= MapMarkerVisual.labelBudget) break;
        if (!dense.contains(entity.id)) addLabel(entity);
      }
    }
    // Source ACL, scenario, viewport and lease filters own eligibility.
    // Task overflow is represented by temporary, revalidated member containers;
    // no source record or route is deleted and owned content stays server-bound.
    return MapDisplayPlan(
      List.unmodifiable(ordered),
      Set.unmodifiable(labels),
      const <String>{},
      denseIDs: Set.unmodifiable(dense),
      clusterPositions: Map.unmodifiable(bounded.positions),
    );
  }

  /// Exact authorized point siblings stay on their real anchors. A current
  /// object chooser resolves overlapping touch targets; no layout offset is a
  /// location claim, synthetic entity, or reason to omit an original ID.
  static Map<String, Offset> coincidentPointOffsets(
    List<MapEntity> entities, {
    Map<String, Offset>? positions,
  }) {
    final groups = <(double, double), Set<String>>{};
    for (final entity in entities) {
      if (!entity.hasMapAnchor ||
          entity.precision != 'point' ||
          !{MapEntityKind.place, MapEntityKind.moment}.contains(entity.kind)) {
        continue;
      }
      (groups[(entity.latitude, entity.longitude)] ??= {}).add(entity.id);
    }
    return Map.unmodifiable({
      for (final group in groups.values)
        if (group.length > 1)
          for (final id in group) id: Offset.zero,
    });
  }

  /// Candidate hit targets intersect a 48 logical-pixel square around the
  /// tapped object's true SDK projection. This is a selection menu, not a
  /// synthesized entity or a permission to obtain another object's content.
  static List<MapEntity> overlappingHitTargets({
    required List<MapEntity> entities,
    required Map<String, Offset> positions,
    required String tappedID,
    double hitSize = 48,
  }) {
    final center = positions[tappedID];
    if (center == null ||
        !center.dx.isFinite ||
        !center.dy.isFinite ||
        !hitSize.isFinite ||
        hitSize < 48 ||
        !entities.any((e) => e.id == tappedID && e.hasMapAnchor)) {
      return const [];
    }
    final unique = <String, MapEntity>{};
    for (final entity in entities) {
      final point = positions[entity.id];
      if (entity.hasMapAnchor &&
          point != null &&
          point.dx.isFinite &&
          point.dy.isFinite &&
          (point.dx - center.dx).abs() <= hitSize &&
          (point.dy - center.dy).abs() <= hitSize) {
        unique.putIfAbsent(entity.id, () => entity);
      }
    }
    return unique.values.toList()..sort((a, b) => a.id.compareTo(b.id));
  }

  /// A physical tap belongs to its screen position, not to an SDK annotation's
  /// transparent bitmap canvas. Keep 48 dp point targets and nearest-first order.
  static List<MapEntity> hitTargetsAt({
    required List<MapEntity> entities,
    required Map<String, Offset> positions,
    required Offset touchPosition,
    Map<String, Rect> nonPointHitRects = const {},
  }) {
    if (!touchPosition.dx.isFinite || !touchPosition.dy.isFinite)
      return const [];
    final unique = <String, MapEntity>{};
    for (final entity in entities) {
      final point = positions[entity.id];
      if (!entity.hasMapAnchor ||
          point == null ||
          !point.dx.isFinite ||
          !point.dy.isFinite)
        continue;
      final compactPoint =
          entity.precision == 'point' &&
          {MapEntityKind.place, MapEntityKind.moment}.contains(entity.kind);
      final rect =
          (!compactPoint ? nonPointHitRects[entity.id] : null) ??
          Rect.fromCenter(center: point, width: 48, height: 48);
      if (rect.isFinite &&
          !rect.isEmpty &&
          touchPosition.dx >= rect.left &&
          touchPosition.dx <= rect.right &&
          touchPosition.dy >= rect.top &&
          touchPosition.dy <= rect.bottom) {
        unique.putIfAbsent(entity.id, () => entity);
      }
    }
    return unique.values.toList()..sort((a, b) {
      final distance = (positions[a.id]! - touchPosition).distanceSquared
          .compareTo((positions[b.id]! - touchPosition).distanceSquared);
      return distance != 0 ? distance : a.id.compareTo(b.id);
    });
  }

  static String countLabel(Iterable<MapEntity> entities) {
    final counts = <MapEntityKind, int>{};
    for (final entity in entities) {
      counts.update(entity.kind, (count) => count + 1, ifAbsent: () => 1);
    }
    const labels = {
      MapEntityKind.place: '个地点',
      MapEntityKind.activity: '个活动',
      MapEntityKind.moment: '条公开记录',
      MapEntityKind.organization: '个组织',
      MapEntityKind.business: '个经营场地',
      MapEntityKind.opportunity: '个活动机会',
      MapEntityKind.person: '个大致区域',
      MapEntityKind.community: '个社群',
      MapEntityKind.group: '个群组',
    };
    return [
      for (final kind in MapEntityKind.values)
        if (counts[kind] case final count?) '$count${labels[kind] ?? '条内容'}',
    ].join(' · ');
  }

  /// Only the compatibility helper/test uses 256-world Mercator. Renderers
  /// supply SDK pixels and never infer device/Mapbox scale from this formula.
  static Offset mercatorPosition(MapEntity entity, double zoom) {
    final world = 256 * math.pow(2, zoom);
    final lat =
        entity.latitude.clamp(-85.05112878, 85.05112878) * math.pi / 180;
    return Offset(
      (entity.longitude + 180) / 360 * world,
      (1 - math.log(math.tan(lat) + 1 / math.cos(lat)) / math.pi) / 2 * world,
    );
  }
}
