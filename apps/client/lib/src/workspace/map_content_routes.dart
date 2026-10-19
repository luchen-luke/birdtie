import 'map_content_route_geometry.dart';
import 'dart:math' as math;
import '../config/birdtie_environment.dart';
import 'map_entities.dart';

/// Edges are explicit current server relationships, never inferred geometry.
class MapContentEdge {
  const MapContentEdge(this.from, this.to, [this.relationID]);
  final String? relationID;
  final MapEntity from, to;
  String get id => '${relationID ?? from.threadID}:${from.id}:${to.id}';
  bool selected(String? id) => id == from.id || id == to.id;
  MapContentRouteGeometry? get geometry {
    final g = from.contentRoute;
    return g != null &&
            g.query.valid &&
            g.query.threadID == from.threadID &&
            g.query.from.markerID == from.id.replaceFirst('owned-map:', '') &&
            g.query.to.markerID == to.id.replaceFirst('owned-map:', '') &&
            g.query.from.sourceVersion == from.sourceVersion &&
            g.query.to.sourceVersion == to.sourceVersion &&
            g.availableAt(DateTime.now().toUtc())
        ? g
        : null;
  }
}

List<MapContentEdge> currentMapContentEdges(Iterable<MapEntity> entities) {
  final now = DateTime.now().toUtc();
  final byID = {
    for (final e in entities)
      if (e.hasMapAnchor && e.readGrant?.authorizes(e, now) == true) e.id: e,
  };
  final edges = <MapContentEdge>[];
  for (final e in byID.values) {
    for (final route
        in BirdtieEnvironment.journeysEnabled
            ? e.journeyEdges
            : const <MapJourneyEdge>[]) {
      final next = byID[route.nextID];
      if (next != null &&
          e.placeID.isNotEmpty &&
          next.placeID.isNotEmpty &&
          route.toPosition == route.fromPosition + 1)
        edges.add(MapContentEdge(e, next, route.identity));
    }
    if (e.threadID.isEmpty) continue;
    for (final id in e.nextContentIDs) {
      final next = byID[id];
      if (next != null &&
          next.threadID == e.threadID &&
          next.threadPosition == e.threadPosition + 1)
        edges.add(MapContentEdge(e, next));
    }
  }
  edges.sort((a, b) => a.id.compareTo(b.id));
  return edges;
}

/// Candidate adjacency is not a road. Only separately authorized saved-leg
/// geometry is drawable; a missing provider result never becomes a straight line.
List<MapContentEdge> renderableMapContentEdges(Iterable<MapEntity> entities) =>
    currentMapContentEdges(
      entities,
    ).where((edge) => edge.geometry != null).toList(growable: false);

class MapContentRouteSegment {
  const MapContentRouteSegment(this.edge, this.index, this.points);
  final MapContentEdge edge;
  final int index;
  final List<MapContentRoutePoint> points;
  String get id => '${edge.id}:$index';
  String get fingerprint => '${edge.geometry?.fingerprint}:$index';
  bool selected(String? id) => edge.selected(id);

  /// A direction glyph lies on a supplied road segment, never on an inferred
  /// bridge between separate segments or on a moved original map pin.
  ({double latitude, double longitude, double bearing})? get direction {
    if (points.length < 2) return null;
    for (var offset = 0; offset < points.length - 1; offset++) {
      final i = ((points.length - 2) ~/ 2 + offset) % (points.length - 1);
      final a = points[i], b = points[i + 1];
      if (a == b) continue;
      final lat1 = a.latitude * math.pi / 180,
          lat2 = b.latitude * math.pi / 180;
      var dl = (b.longitude - a.longitude + 540) % 360 - 180;
      final radians = dl * math.pi / 180;
      final bearing =
          math.atan2(
            math.sin(radians) * math.cos(lat2),
            math.cos(lat1) * math.sin(lat2) -
                math.sin(lat1) * math.cos(lat2) * math.cos(radians),
          ) *
          180 /
          math.pi;
      return (
        latitude: (a.latitude + b.latitude) / 2,
        longitude: ((a.longitude + dl / 2 + 540) % 360) - 180,
        bearing: (bearing + 360) % 360,
      );
    }
    return null;
  }
}

List<MapContentRouteSegment> currentMapContentSegments(
  Iterable<MapEntity> entities,
) => [
  for (final edge in renderableMapContentEdges(entities))
    for (final row in edge.geometry!.segments.indexed)
      MapContentRouteSegment(edge, row.$1, row.$2),
];
