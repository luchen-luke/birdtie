import 'dart:math' as math;
import 'package:flutter/painting.dart';

import 'map_entities.dart';
import 'map_marker_visual.dart';

/// Display-only output: the authorized ResultSet and its list are untouched.
class MapDisplayPlan {
  const MapDisplayPlan(
    this.entities,
    this.labelIDs,
    this.omittedIDs, {
    this.denseIDs = const {},
  });
  final List<MapEntity> entities;
  final Set<String> labelIDs, omittedIDs, denseIDs;
}

class MapDisplayLevel {
  const MapDisplayLevel(this.maxZoomExclusive, this.iconCellLogicalPixels);
  final double maxZoomExclusive, iconCellLogicalPixels;
  double get collisionRadius => iconCellLogicalPixels / 2;
}

/// Provider-independent policy. Input positions are map-local logical pixels
/// returned by the current camera, after the source has enforced its ACL/lease.
class MapDisplayPolicy {
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
    int ambientLimit = 64,
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
    final ordered = unique.values.toList()
      ..sort((a, b) => a.id.compareTo(b.id));
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
      if (emphasized.contains(entity.id) || entity.precision != 'point')
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
    for (final entity in [
      ...ordered.where((e) => e.id == selectedID),
      ...ordered.where((e) => e.id != selectedID && emphasized.contains(e.id)),
    ]) {
      if (labels.length >= MapMarkerVisual.labelBudget) break;
      labels.add(entity.id);
    }
    if (zoom >= 16) {
      for (final entity in ordered) {
        if (labels.length >= MapMarkerVisual.labelBudget) break;
        if (!dense.contains(entity.id)) labels.add(entity.id);
      }
    }
    // Source ACL, scenario, viewport and lease filters own eligibility.
    // Chrome/contentRect, prior visibility and ambientLimit may not erase it.
    // Coincident anchors retain their original kind, ID and WGS84 coordinate;
    // overlapping hit areas use a current-object chooser in each provider.
    return MapDisplayPlan(
      List.unmodifiable(ordered),
      Set.unmodifiable(labels),
      const <String>{},
      denseIDs: Set.unmodifiable(dense),
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
