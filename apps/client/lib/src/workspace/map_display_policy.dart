import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'package:crypto/crypto.dart';
import 'package:flutter/painting.dart';
import 'map_pin_category.dart';

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
    this.placements = const {},
    this.glyphSizes = const {},
    this.metersPerPixel,
    this.occludedGlyphIDs = const {},
  });
  final List<MapEntity> entities;
  final Set<String> labelIDs, omittedIDs, denseIDs;
  final Map<String, Offset> clusterPositions;
  final Map<String, MapLabelPlacement> placements;
  final Map<String, double> glyphSizes;
  final double? metersPerPixel;
  // Display clipping only. Entities and original cluster members remain in
  // the authorized source/list; no coordinate is moved to avoid app chrome.
  final Set<String> occludedGlyphIDs;
}

class MapDisplayLevel {
  const MapDisplayLevel(this.maxZoomExclusive, this.iconCellLogicalPixels);
  final double maxZoomExclusive, iconCellLogicalPixels;
  double get collisionRadius => iconCellLogicalPixels / 2;
}

/// Provider-independent policy. Input positions are map-local logical pixels
/// returned by the current camera, after the source has enforced its ACL/lease.
class MapDisplayPolicy {
  // Real Shanghai calibration: the SDK's rounded "2 km" scale can still mean
  // 18.49 m/dp. A 375dp viewport covering 6km is already a city overview;
  // count rows belong to a closer neighbourhood view, independent of SDK zoom.
  static const overviewMetersPerLogicalPixel = 16.0;

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
                e.categoryCode,
                e.contentCount,
                e.anchorCount,
                e.placeID,
                e.momentID,
                e.momentRevision,
                e.anchorID,
                e.entryID,
                e.threadID,
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

  static double? scaleFromSegment(Offset a, Offset b, double logicalPixels) {
    if (!logicalPixels.isFinite ||
        logicalPixels <= 0 ||
        !a.dx.isFinite ||
        !a.dy.isFinite ||
        !b.dx.isFinite ||
        !b.dy.isFinite)
      return null;
    const radians = math.pi / 180;
    final dlat = (b.dy - a.dy) * radians, dlon = (b.dx - a.dx) * radians;
    final h =
        math.pow(math.sin(dlat / 2), 2) +
        math.cos(a.dy * radians) *
            math.cos(b.dy * radians) *
            math.pow(math.sin(dlon / 2), 2);
    final distance = 12742000 * math.asin(math.sqrt(h.clamp(0, 1)));
    final value = distance / logicalPixels;
    return value.isFinite && value > 0 ? value : null;
  }

  static double? _sourceScale(
    List<MapEntity> rows,
    Map<String, Offset> points,
  ) {
    final values = <double>[];
    for (var i = 1; i < rows.length && values.length < 12; i++) {
      final a = rows.first, b = rows[i];
      final pixels = (points[a.id]! - points[b.id]!).distance;
      if (pixels < 24) continue;
      final scale = scaleFromSegment(
        Offset(a.longitude, a.latitude),
        Offset(b.longitude, b.latitude),
        pixels,
      );
      if (scale != null) values.add(scale);
    }
    if (values.isEmpty) return null;
    values.sort();
    return values[values.length ~/ 2];
  }

  static MapDisplayPlan plan({
    required List<MapEntity> entities,
    required Map<String, Offset> positions,
    required double zoom,
    String? selectedID,
    Rect? contentRect,
    Set<String> previouslyVisibleIDs = const {},
    Map<String, MapLabelPlacement> previousPlacements = const {},
    Set<String> protectedIDs = const {},
    double? collisionRadius,
    double? metersPerPixel,
    int ambientLimit = 40,
  }) {
    final unique = <String, MapEntity>{};
    for (final e in entities) {
      final p = positions[e.id];
      if (e.hasMapAnchor && p != null && p.dx.isFinite && p.dy.isFinite)
        unique.putIfAbsent(e.id, () => e);
    }
    final original = unique.values.toList()
      ..sort((a, b) => a.id.compareTo(b.id));
    final scale = metersPerPixel ?? _sourceScale(original, positions);
    // Unknown scale is conservatively compact. Provider zoom numbers are not
    // treated as interchangeable geographic units.
    final overview = scale == null || scale >= overviewMetersPerLogicalPixel;
    // Density changes decoration only. Every legal source object retains its
    // original identity and coordinate, including exact-coordinate siblings.
    int priority(MapEntity e) => e.id == selectedID
        ? 0
        : protectedIDs.contains(e.id) || e.isTaskResult
        ? 1
        : previouslyVisibleIDs.contains(e.id)
        ? 2
        : 3;
    final displayed = List<MapEntity>.of(original)
      ..sort((a, b) {
        final rank = priority(a).compareTo(priority(b));
        return rank != 0 ? rank : a.id.compareTo(b.id);
      });
    bool point(MapEntity e) =>
        e.precision == 'point' &&
        {MapEntityKind.place, MapEntityKind.moment}.contains(e.kind);
    double fullSize(MapEntity e) => MapMarkerVisual.diameter(
      selected: e.id == selectedID,
      dense: false,
      moment: e.kind == MapEntityKind.moment,
    );
    final dense = <String>{
      if (overview)
        for (final e in displayed)
          if (point(e) && e.id != selectedID) e.id,
    };
    // A small screen-space neighbourhood replaces pairwise city aggregation.
    // Touch targets stay 48dp; only colliding nonselected glyphs become dots.
    if (!overview) {
      const cell = 48.0;
      final grid = <(int, int), List<MapEntity>>{};
      for (final e in displayed.where(point)) {
        final p = positions[e.id]!;
        final x = (p.dx / cell).floor(), y = (p.dy / cell).floor();
        MapEntity? collision;
        for (var dx = -1; dx <= 1 && collision == null; dx++) {
          for (var dy = -1; dy <= 1 && collision == null; dy++) {
            for (final other in grid[(x + dx, y + dy)] ?? const <MapEntity>[]) {
              if ((p - positions[other.id]!).distance <=
                  (fullSize(e) + fullSize(other)) / 2 + 4) {
                collision = other;
                break;
              }
            }
          }
        }
        if (collision != null) {
          if (e.id != selectedID) dense.add(e.id);
          if (collision.id != selectedID) dense.add(collision.id);
        }
        (grid[(x, y)] ??= []).add(e);
      }
    }
    double size(MapEntity e) =>
        !{
          MapEntityKind.place,
          MapEntityKind.moment,
          MapEntityKind.cluster,
        }.contains(e.kind)
        ? 48
        : e.kind == MapEntityKind.cluster
        ? (e.id == selectedID ? 30 : 28)
        : MapMarkerVisual.diameter(
            selected: e.id == selectedID,
            dense: dense.contains(e.id),
            moment: e.kind == MapEntityKind.moment,
          );
    final sizes = {for (final e in displayed) e.id: size(e)};
    final glyphs = {
      for (final e in displayed)
        e.id: Rect.fromCenter(
          center: positions[e.id]!,
          width: sizes[e.id]!,
          height: sizes[e.id]!,
        ),
    };
    final placed = <String, MapLabelPlacement>{}, occupied = <Rect>[];
    final occluded = <String>{
      if (contentRect != null)
        for (final entry in glyphs.entries)
          if (!contentRect.contains(entry.value.topLeft) ||
              !contentRect.contains(
                entry.value.bottomRight - const Offset(.01, .01),
              ))
            entry.key,
    };
    final maximumLabels = overview
        ? 1
        : scale != null && scale >= 8
        ? 6
        : 10;
    for (final e in displayed) {
      if (occluded.contains(e.id) || dense.contains(e.id)) continue;
      final selected = e.id == selectedID;
      if (placed.length >= maximumLabels && !selected) continue;
      if (e.kind == MapEntityKind.moment && !selected ||
          e.kind == MapEntityKind.cluster && !selected)
        continue;
      final raw = e.kind == MapEntityKind.cluster
          ? '${MapMarkerVisual.clusterCount(categoryCode: e.categoryCode, contentCount: e.contentCount, anchorCount: e.anchorCount, fallback: e.memberIDs.length)}${MapMarkerVisual.clusterUnit(e.categoryCode)}'
          : e.title;
      if (raw.isEmpty || e.kind == MapEntityKind.moment && raw == '动态')
        continue;
      final name = MapMarkerVisual.nativeLabel(raw, selected: selected);
      final font = selected ? 13.0 : 12.0;
      final namePainter = TextPainter(
        text: TextSpan(
          text: name,
          style: TextStyle(fontSize: font, fontWeight: FontWeight.w500),
        ),
        textDirection: TextDirection.ltr,
        maxLines: 1,
      )..layout(maxWidth: 112);
      final nameWidth = math.max(16.0, namePainter.width);
      namePainter.dispose();
      final auxiliary =
          e.kind == MapEntityKind.place &&
              (selected || scale != null && scale < 8)
          ? e.recentMoments?.labelAt(DateTime.now().toUtc())
          : null;
      final p = positions[e.id]!,
          radius = sizes[e.id]! / 2 + MapMarkerVisual.labelGap;
      final prior = previousPlacements[e.id]?.side;
      final sides = {
        if (prior != null) prior,
        MapLabelSide.right,
        MapLabelSide.left,
        MapLabelSide.top,
        MapLabelSide.bottom,
      };
      for (final aux in [if (auxiliary != null) auxiliary, null]) {
        if (placed.containsKey(e.id)) break;
        var width = nameWidth;
        if (aux != null) {
          final painter = TextPainter(
            text: TextSpan(text: aux, style: const TextStyle(fontSize: 11)),
            textDirection: TextDirection.ltr,
            maxLines: 1,
          )..layout(maxWidth: 112);
          width = math.max(width, painter.width);
          painter.dispose();
        }
        final height = font * 1.25 + (aux == null ? 0 : 15);
        for (final side in sides) {
          final origin = switch (side) {
            MapLabelSide.right => Offset(p.dx + radius, p.dy - height / 2),
            MapLabelSide.left => Offset(
              p.dx - radius - width,
              p.dy - height / 2,
            ),
            MapLabelSide.top => Offset(
              p.dx - width / 2,
              p.dy - radius - height,
            ),
            MapLabelSide.bottom => Offset(p.dx - width / 2, p.dy + radius),
          };
          final rect = origin & Size(width, height);
          if (contentRect != null &&
              (!contentRect.contains(rect.topLeft) ||
                  !contentRect.contains(
                    rect.bottomRight - const Offset(.01, .01),
                  )))
            continue;
          if (occupied.any((v) => v.overlaps(rect.inflate(2))) ||
              glyphs.entries.any(
                (v) => v.key != e.id && v.value.overlaps(rect.inflate(2)),
              ))
            continue;
          placed[e.id] = MapLabelPlacement(
            side: side,
            rect: rect,
            name: name,
            fontSize: font,
            color: e.kind == MapEntityKind.place
                ? MapPinCategory.forCode(e.categoryCode).label
                : MapPinCategory.other.label,
            auxiliary: aux,
            offset: origin - p,
          );
          occupied.add(rect);
          break;
        }
      }
    }
    return MapDisplayPlan(
      List.unmodifiable(displayed),
      Set.unmodifiable(placed.keys),
      const {},
      denseIDs: Set.unmodifiable(dense),
      clusterPositions: const {},
      placements: Map.unmodifiable(placed),
      glyphSizes: Map.unmodifiable(sizes),
      metersPerPixel: scale,
      occludedGlyphIDs: Set.unmodifiable(occluded),
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
