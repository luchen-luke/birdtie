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
  test('explicit painted label or glyph wins over nearby48dp halo, genuine overlap stays ambiguous',(){
    final a=row('moment:a',kind:MapEntityKind.moment),b=row('moment:b',kind:MapEntityKind.moment);
    final positions={'moment:a':const Offset(100,100),'moment:b':const Offset(120,100)};
    List<String> hit(Offset touch,{Map<String,Offset>? at,Map<String,MapLabelPlacement> labels=const {}})=>MapDisplayPolicy.visibleHitTargetsAt(entities:[a,b],positions:at??positions,touchPosition:touch,placements:labels,glyphSizes:{a.id:18,b.id:18}).map((e)=>e.id).toList();
    expect(hit(const Offset(100,100)),[a.id]);
    expect(hit(const Offset(120,100)),[b.id]);
    expect(hit(const Offset(110,100)),[a.id,b.id],reason:'Accessible halo fallback stays uncertain');
    expect(hit(const Offset(106,100),at:{a.id:const Offset(100,100),b.id:const Offset(110,100)}),[b.id,a.id]);
    final labels={a.id:const MapLabelPlacement(side:MapLabelSide.right,rect:Rect.fromLTWH(125,92,90,18),name:'原文标题',fontSize:12,color:Colors.black)};
    expect(hit(const Offset(129,100),labels:labels),[a.id],reason:'A label is explicit even beside another glyph/halo');
    expect(hit(const Offset(210,100),labels:labels),[a.id],reason:'Actual text extends beyond the48dp square');
    expect(hit(const Offset(300,100),labels:labels),isEmpty);
    expect((a.latitude,a.longitude,b.latitude,b.longitude),(31,121,31,121));
  });
  test('explicit painted targets never include expired or retired source grants',(){
    var live=true;final now=DateTime.now().toUtc();
    final original=row('moment:a',kind:MapEntityKind.moment);
    final a=original.withMediaPixels(null,readGrant:MapEntityReadGrant(source:original,validUntil:now.add(const Duration(seconds:30)),current:()=>live));
    final b=row('moment:b',kind:MapEntityKind.moment);
    List<String> hit()=>MapDisplayPolicy.visibleHitTargetsAt(entities:[a,b],positions:{a.id:const Offset(100,100),b.id:const Offset(100,100)},touchPosition:const Offset(100,100),glyphSizes:{a.id:18,b.id:18}).map((e)=>e.id).toList();
    expect(hit(),[a.id,b.id]);live=false;expect(hit(),[b.id]);live=true;expect(hit(),[b.id],reason:'A retired old grant cannot revive by identity ABA');
    final expired=original.withMediaPixels(null,readGrant:MapEntityReadGrant(source:original,validUntil:now.subtract(const Duration(seconds:1)),current:()=>true));
    expect(MapDisplayPolicy.samePointSource(expired,expired),isFalse);
  });
  test('known source original ID must retain source and actual facts after late callback',(){
    final a=row('moment:a',kind:MapEntityKind.moment);
    expect(MapDisplayPolicy.samePointSource(a,a),isTrue);
    expect(MapDisplayPolicy.samePointSource(a,row(a.id,kind:a.kind,source:'changed')),isFalse);
    final moved=MapEntity(id:a.id,kind:a.kind,title:a.title,subtitle:a.subtitle,latitude:31.1,longitude:121,sourceVersion:a.sourceVersion,momentID:a.momentID);
    expect(MapDisplayPolicy.samePointSource(a,moved),isFalse);
    final otherOriginal=MapEntity(id:a.id,kind:a.kind,title:a.title,subtitle:a.subtitle,latitude:a.latitude,longitude:a.longitude,sourceVersion:a.sourceVersion,momentID:'another-original');
    expect(MapDisplayPolicy.samePointSource(a,otherOriginal),isFalse);
  });

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
