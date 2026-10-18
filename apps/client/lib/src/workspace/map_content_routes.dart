import 'map_entities.dart';

/// Edges are explicit current server relationships, never inferred geometry.
class MapContentEdge {
  const MapContentEdge(this.from, this.to, [this.relationID]);
  final String? relationID;
  final MapEntity from, to;
  String get id => '${relationID ?? from.threadID}:${from.id}:${to.id}';
  bool selected(String? id) => id == from.id || id == to.id;
}

List<MapContentEdge> currentMapContentEdges(Iterable<MapEntity> entities) {
  final now = DateTime.now().toUtc();
  final byID = {
    for (final e in entities)
      if (e.hasMapAnchor && e.readGrant?.authorizes(e, now) == true) e.id: e,
  };
  final edges = <MapContentEdge>[];
  for (final e in byID.values) {
    for (final route in e.journeyEdges) {
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
