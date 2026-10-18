import 'package:birdtie_client/src/config/birdtie_environment.dart';
import 'package:birdtie_client/src/workspace/map_content_routes.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'paused Journey edges cannot replace original current Moment thread edges',
    () {
      expect(BirdtieEnvironment.journeysEnabled, isFalse);
      MapEntity node(int position) {
        final e = MapEntity(
          id: 'm$position',
          kind: MapEntityKind.moment,
          title: 'isolated',
          subtitle: '',
          latitude: 31.2,
          longitude: 121.5,
          placeID: 'place$position',
          sourceVersion: 'current$position',
          threadID: 'original-thread',
          threadPosition: position,
          nextContentIDs: position == 0 ? ['m1'] : [],
          journeyEdges: position == 0
              ? [
                  const MapJourneyEdge(
                    journeyID: 'paused',
                    publicationRevision: 1,
                    nextID: 'm1',
                    fromPosition: 0,
                    toPosition: 1,
                    sourceVersion: 'current',
                  ),
                ]
              : [],
        );
        return e.withMediaPixels(
          null,
          readGrant: MapEntityReadGrant(
            source: e,
            validUntil: DateTime.now().toUtc().add(const Duration(minutes: 1)),
            current: () => true,
          ),
        );
      }

      final edges = currentMapContentEdges([node(0), node(1)]);
      expect(edges, hasLength(1));
      expect(edges.single.id, 'original-thread:m0:m1');
      expect(edges.single.relationID, isNull);
    },
  );
}
