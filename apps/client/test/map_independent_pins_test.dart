import 'package:birdtie_client/src/city/amap_city_map_stub.dart';
import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';
import 'package:birdtie_client/src/workspace/map_marker_widget.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

MapEntity _row(int index) => MapEntity(
  id: '${index.isEven ? 'place' : 'moment'}:$index',
  kind: index.isEven ? MapEntityKind.place : MapEntityKind.moment,
  title: '合成独立对象$index',
  subtitle: '当前合法来源',
  latitude: 31.2,
  longitude: 121.4 + index / 100000,
  sourceVersion: 'unit-$index',
  categoryCode: 'museum',
);

void main() {
  test(
    'AMap frame clips marker transport and stale actions, then restores originals',
    () {
      final source = [_row(0), _row(1)];
      final points = {
        source[0].id: const Offset(150, 150),
        source[1].id: const Offset(150, 180),
      };
      AMapDisplayFrame at(Rect rect) => AMapDisplayFrame(
        1,
        1,
        source,
        points,
        const {},
        MapDisplayPolicy.plan(
          entities: source,
          positions: points,
          zoom: 12,
          metersPerPixel: 16,
          contentRect: rect,
        ),
      );
      final covered = at(const Rect.fromLTRB(0, 170, 400, 600));
      final markers = (covered.layout(null)['markers']! as List).cast<Map>();
      expect(markers.map((m) => m['id']).toSet(), {source[1].id});
      expect(covered.resolveDisplay(source[0].id, source[0].kind.name), isNull);
      expect(covered.candidates(source[0].id), isEmpty);
      expect(covered.candidates(source[1].id), [source[1]]);
      final restored = at(const Rect.fromLTRB(0, 100, 400, 600));
      expect((restored.layout(null)['markers']! as List).length, 2);
      expect(
        restored.resolveDisplay(source[0].id, source[0].kind.name),
        same(source[0]),
      );
      expect(restored.candidates(source[1].id).toSet(), source.toSet());
      expect(covered.rows, same(source));
      expect(restored.rows, same(source));
    },
  );

  test(
    'density never aggregates, omits or moves any original source point',
    () {
      final source = [for (var i = 0; i < 240; i++) _row(i)];
      final before = [for (final e in source) (e.id, e.latitude, e.longitude)];
      final points = {
        for (var i = 0; i < source.length; i++)
          source[i].id: Offset(40 + i % 20 * 12.0, 80 + i ~/ 20 * 12.0),
      };
      for (final scale in [1.0, 8.0, 16.0, 100.0]) {
        for (final zoom in [3.0, 18.0]) {
          final plan = MapDisplayPolicy.plan(
            entities: source,
            positions: points,
            zoom: zoom,
            metersPerPixel: scale,
            selectedID: source.last.id,
            ambientLimit: 1,
            contentRect: const Rect.fromLTWH(0, 0, 400, 600),
          );
          expect(plan.entities.length, source.length);
          expect(plan.entities.every(source.contains), isTrue);
          expect(plan.entities.any(MapDisplayPolicy.displayCluster), isFalse);
          expect(plan.clusterPositions, isEmpty);
          expect(plan.omittedIDs, isEmpty);
          expect(plan.glyphSizes[source.last.id], 34);
          expect(plan.denseIDs, isNot(contains(source.last.id)));
        }
      }
      expect([for (final e in source) (e.id, e.latitude, e.longitude)], before);
    },
  );

  test(
    'exact-coordinate siblings keep every ID and complete overlap choices',
    () {
      final source = [for (var i = 0; i < 115; i++) _row(i)];
      final positions = {for (final e in source) e.id: const Offset(150, 250)};
      final plan = MapDisplayPolicy.plan(
        entities: source,
        positions: positions,
        zoom: 18,
        metersPerPixel: 1,
      );
      expect(plan.entities.toSet(), source.toSet());
      expect(plan.denseIDs.length, 115);
      for (final e in source) {
        expect(plan.glyphSizes[e.id], e.kind == MapEntityKind.place ? 7 : 8);
      }
      expect(
        MapDisplayPolicy.hitTargetsAt(
          entities: plan.entities,
          positions: positions,
          touchPosition: const Offset(150, 250),
        ).toSet(),
        source.toSet(),
      );
      expect(plan.placements, isEmpty);
    },
  );

  test('detail reduces before micro glyphs and selected remains explicit', () {
    final now = DateTime.now().toUtc();
    final place = MapEntity(
      id: 'place:detail',
      kind: MapEntityKind.place,
      title: '合成地点',
      subtitle: '',
      latitude: 31.2,
      longitude: 121.4,
      recentMoments: MapRecentMoments.visible(
        count: 3,
        checkedAt: now,
        validUntil: now.add(const Duration(seconds: 30)),
        sourceVersion: 'synthetic',
      ),
    );
    MapDisplayPlan at(double scale, {String? selected}) =>
        MapDisplayPolicy.plan(
          entities: [place],
          positions: {place.id: const Offset(150, 250)},
          zoom: 12,
          metersPerPixel: scale,
          selectedID: selected,
        );
    expect(at(1).placements[place.id]!.auxiliary, '3条动态');
    expect(at(8).placements[place.id]!.auxiliary, isNull);
    expect(at(8).glyphSizes[place.id], 24);
    expect(at(16).glyphSizes[place.id], 7);
    expect(at(16).placements, isEmpty);
    expect(at(100, selected: place.id).glyphSizes[place.id], 28);
    expect(at(100, selected: place.id).placements[place.id]!.auxiliary, '3条动态');
  });

  testWidgets('circle and square micro pins retain independent 48dp actions', (
    t,
  ) async {
    final selected = <String>[];
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Column(
            children: [
              for (final moment in [false, true])
                CompactMapPointMarker(
                  id: moment ? 'moment:micro' : 'place:micro',
                  title: '合成对象',
                  subtitle: '',
                  moment: moment,
                  selected: false,
                  dense: true,
                  showLabel: true,
                  categoryCode: 'museum',
                  onTap: () => selected.add(moment ? 'moment' : 'place'),
                ),
            ],
          ),
        ),
      ),
    );
    for (final moment in [false, true]) {
      final id = moment ? 'moment:micro' : 'place:micro';
      expect(
        t.getSize(find.byKey(ValueKey('individual-map-target:$id'))),
        const Size(48, 48),
      );
      final shape = find.byKey(ValueKey('individual-map-shape:$id'));
      expect(t.getSize(shape), Size.square(moment ? 8 : 7));
      expect(t.widget<Container>(shape).decoration is BoxDecoration, isTrue);
      final box = t.widget<Container>(shape).decoration! as BoxDecoration;
      expect(box.shape, moment ? BoxShape.rectangle : BoxShape.circle);
      expect(find.byKey(ValueKey('individual-map-caption:$id')), findsNothing);
      await t.tap(find.byKey(ValueKey('individual-map-target:$id')));
    }
    expect(selected, ['place', 'moment']);
    expect(find.byType(RawImage), findsNothing);
    expect(find.byIcon(Icons.museum_outlined), findsNothing);
    expect(MapMarkerVisual.hitSize, 48);
    await t.pumpWidget(const SizedBox.shrink());
  });
}
