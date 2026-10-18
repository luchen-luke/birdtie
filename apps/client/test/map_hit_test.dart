import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

MapEntity row(
  String id, {
  MapEntityKind kind = MapEntityKind.place,
  String source = 'v1',
}) => MapEntity(
  id: id,
  kind: kind,
  title: id,
  subtitle: '',
  latitude: 31,
  longitude: 121,
  sourceVersion: source,
  momentID: kind == MapEntityKind.moment ? 'moment-one' : '',
);
void main() {
  test(
    'bounded local clusters retain all originals once and reject missing or changed member authority',
    () {
      final source = [for (var i = 0; i < 115; i++) row('place:$i')];
      final plan = MapDisplayPolicy.plan(
        entities: source,
        zoom: 1,
        metersPerPixel: 50,
        positions: {for (final e in source) e.id: const Offset(100, 100)},
        selectedID: 'place:114',
      );
      final represented = <String>[];
      for (final e in plan.entities) {
        if (MapDisplayPolicy.displayCluster(e)) {
          expect(e.memberIDs.length, lessThanOrEqualTo(32));
          expect(e.categoryCode, 'place_cluster');
          represented.addAll(
            MapDisplayPolicy.currentDisplayClusterMembers(
              e,
              source,
            ).map((e) => e.id),
          );
          expect(
            MapDisplayPolicy.currentDisplayClusterMembers(
              e,
              source.where((v) => v.id != e.memberIDs.first),
            ),
            isEmpty,
          );
          expect(
            MapDisplayPolicy.currentDisplayClusterMembers(e, [
              for (final v in source)
                if (v.id == e.memberIDs.first) row(v.id, source: 'v2') else v,
            ]),
            isEmpty,
          );
        } else {
          represented.add(e.id);
        }
        expect((e.latitude, e.longitude), (31, 121));
      }
      expect(represented, hasLength(115));
      expect(represented.toSet(), source.map((e) => e.id).toSet());
      expect(plan.entities.first.id, 'place:114');
    },
  );
  test(
    'cluster circles use identical typed count semantics and never infer unknown kind',
    () {
      expect(
        MapMarkerVisual.clusterCount(
          categoryCode: 'place_cluster',
          contentCount: 19,
          anchorCount: 4,
        ),
        4,
      );
      expect(
        MapMarkerVisual.clusterCount(
          categoryCode: 'moment_cluster',
          contentCount: 3,
          anchorCount: 8,
        ),
        3,
      );
      expect(
        MapMarkerVisual.clusterCount(
          categoryCode: 'mixed_cluster',
          contentCount: 19,
          anchorCount: 4,
        ),
        4,
      );
      expect(
        MapMarkerVisual.clusterCount(
          categoryCode: 'moment',
          contentCount: 19,
          anchorCount: 4,
        ),
        4,
      );
      expect(MapMarkerVisual.clusterUnit('moment'), '项');
      final source = [
        row('moment:a', kind: MapEntityKind.moment),
        row('moment:b', kind: MapEntityKind.moment),
      ];
      final plan = MapDisplayPolicy.plan(
        entities: source,
        zoom: 16,
        metersPerPixel: 2,
        positions: {for (final e in source) e.id: const Offset(100, 100)},
      );
      expect(plan.entities.single.contentCount, 1);
      expect(plan.entities.single.anchorCount, 2);
      expect(plan.entities.single.categoryCode, 'moment_cluster');
    },
  );
  test(
    'local groups do not chain across a viewport and real taps stay 48dp',
    () {
      final source = [row('a'), row('b'), row('c')];
      final positions = {
        'a': const Offset(100, 100),
        'b': const Offset(120, 100),
        'c': const Offset(140, 100),
      };
      final plan = MapDisplayPolicy.plan(
        entities: source,
        positions: positions,
        zoom: 15,
        metersPerPixel: 2,
      );
      final cluster = plan.entities.singleWhere(
        MapDisplayPolicy.displayCluster,
      );
      expect(cluster.memberIDs, ['a', 'b']);
      expect(plan.entities.any((e) => e.id == 'c'), isTrue);
      expect(
        MapDisplayPolicy.hitTargetsAt(
          entities: source,
          positions: positions,
          touchPosition: const Offset(76, 100),
        ).map((e) => e.id),
        ['a'],
      );
      expect(
        MapDisplayPolicy.hitTargetsAt(
          entities: source,
          positions: positions,
          touchPosition: const Offset(75.9, 100),
        ),
        isEmpty,
      );
    },
  );
}
