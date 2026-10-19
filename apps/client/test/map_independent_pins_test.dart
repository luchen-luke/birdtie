import 'package:birdtie_client/src/city/amap_city_map_stub.dart';
import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_content_routes.dart';
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
    'default content edges preserve original gaps and retire with source grants',
    () {
      var current = true;
      final until = DateTime.now().toUtc().add(const Duration(minutes: 1));
      MapEntity node(int position, List<String> next, {DateTime? deadline}) {
        final source = MapEntity(
          id: 'content:$position',
          kind: MapEntityKind.moment,
          title: '隔离串文',
          subtitle: '',
          latitude: 31.2,
          longitude: 121.5,
          sourceVersion: 'current-$position',
          threadID: 'thread',
          threadPosition: position,
          nextContentIDs: next,
        );
        return source.withMediaPixels(
          null,
          readGrant: MapEntityReadGrant(
            source: source,
            validUntil: deadline ?? until,
            current: () => current,
          ),
        );
      }

      final first = node(0, []),
          third = node(2, ['content:3']),
          fourth = node(3, []);
      final source = [first, third, fourth];
      expect(
        currentMapContentEdges(
          source,
        ).map((e) => (e.from.threadPosition, e.to.threadPosition)),
        [(2, 3)],
      );
      final edge = currentMapContentEdges(source).single;
      expect(edge.selected(null), isFalse);
      expect(edge.selected(third.id), isTrue);
      expect(currentMapContentEdges(source).single.id, edge.id);
      expect(currentMapContentEdges([first, third]), isEmpty);
      expect(
        currentMapContentEdges([
          first,
          third,
          node(
            3,
            [],
            deadline: DateTime.now().toUtc().subtract(
              const Duration(seconds: 1),
            ),
          ),
        ]),
        isEmpty,
      );
      current = false;
      expect(currentMapContentEdges(source), isEmpty);
      current = true;
      expect(
        currentMapContentEdges(source),
        isEmpty,
        reason: 'retired grants never revive',
      );
    },
  );

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
          expect(plan.glyphSizes[source.last.id], 20);
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
        expect(plan.glyphSizes[e.id], e.kind == MapEntityKind.place ? 7 : 18);
      }
      expect(
        MapDisplayPolicy.hitTargetsAt(
          entities: plan.entities,
          positions: positions,
          touchPosition: const Offset(150, 250),
        ).toSet(),
        source.toSet(),
      );
      expect(plan.placements.length, lessThanOrEqualTo(6));
      expect(
        plan.labelIDs.every(
          (id) =>
              source.singleWhere((e) => e.id == id).kind ==
              MapEntityKind.moment,
        ),
        isTrue,
      );
      final labels = plan.placements.values.toList();
      for (var i = 0; i < labels.length; i++) {
        for (var j = i + 1; j < labels.length; j++) {
          expect(labels[i].rect.overlaps(labels[j].rect), isFalse);
        }
      }
    },
  );

  test(
    'overview keeps six nonoverlapping Moment names and compact original faces',
    () {
      final rows = [
        for (var i = 0; i < 12; i++)
          MapEntity(
            id: 'moment:overview:$i',
            kind: MapEntityKind.moment,
            title: '动态$i',
            subtitle: '动态$i',
            latitude: 31.2 + i / 100,
            longitude: 121.4,
            sourceVersion: 'synthetic-$i',
          ),
      ];
      final positions = {
        for (var i = 0; i < rows.length; i++)
          rows[i].id: Offset(60 + i % 4 * 140.0, 100 + i ~/ 4 * 100.0),
      };
      for (final scale in [16.0, 100.0]) {
        final plan = MapDisplayPolicy.plan(
          entities: rows,
          positions: positions,
          zoom: 4,
          metersPerPixel: scale,
          selectedID: rows.last.id,
          contentRect: const Rect.fromLTWH(0, 0, 600, 500),
        );
        expect(plan.entities.toSet(), rows.toSet());
        expect(plan.omittedIDs, isEmpty);
        expect(plan.labelIDs.length, 6);
        expect(plan.labelIDs.first, rows.last.id);
        for (final row in rows)
          expect(plan.glyphSizes[row.id], row == rows.last ? 20 : 18);
        final labels = plan.placements.values.toList();
        expect(
          labels.every((p) => p.auxiliary == null),
          isTrue,
          reason:
              'Unselected subtitles and selected title duplicates must not form a second row',
        );
        for (var i = 0; i < labels.length; i++)
          for (var j = i + 1; j < labels.length; j++) {
            expect(labels[i].rect.overlaps(labels[j].rect), isFalse);
          }
      }
      final unknown = MapDisplayPolicy.plan(
        entities: [rows.first],
        positions: {rows.first.id: const Offset(120, 120)},
        zoom: 4,
      );
      expect(unknown.glyphSizes[rows.first.id], 18);
      expect(unknown.labelIDs, contains(rows.first.id));
    },
  );

  test('fallback Moment names never consume meaningful overview labels', () {
    final rows = [
      for (var i = 0; i < 6; i++)
        MapEntity(
          id: 'moment:placeholder:$i',
          kind: MapEntityKind.moment,
          title: i.isEven ? '动态' : '  ',
          subtitle: '',
          latitude: 31.2 + i / 100,
          longitude: 121.4,
          momentID: 'placeholder-$i',
          sourceVersion: 'synthetic',
          contentTime: DateTime.utc(2026, 10, 19),
        ),
      for (var i = 0; i < 6; i++)
        MapEntity(
          id: 'moment:authored:$i',
          kind: MapEntityKind.moment,
          title: '确有正文$i',
          subtitle: '',
          latitude: 31.3 + i / 100,
          longitude: 121.4,
          momentID: 'authored-$i',
          sourceVersion: 'synthetic',
          contentTime: DateTime.utc(2026, 10, 18),
        ),
    ];
    final points = {
      for (var i = 0; i < rows.length; i++)
        rows[i].id: Offset(60 + i % 4 * 160.0, 100 + i ~/ 4 * 110.0),
    };
    MapDisplayPlan at([String? selected]) => MapDisplayPolicy.plan(
      entities: rows,
      positions: points,
      zoom: 8,
      metersPerPixel: 40,
      selectedID: selected,
      contentRect: const Rect.fromLTWH(0, 0, 700, 500),
    );
    final regular = at();
    expect(regular.labelIDs.toSet(), rows.skip(6).map((e) => e.id).toSet());
    expect(
      regular.placements.values.any((p) => p.name.contains('动态 · 动态')),
      isFalse,
    );
    final selected = at(rows.first.id);
    expect(selected.labelIDs.length, 6);
    expect(selected.labelIDs.first, rows.first.id);
    expect(selected.placements[rows.first.id]!.name, '动态');
    expect(
      selected.labelIDs.where((id) => id.startsWith('moment:authored:')).length,
      5,
    );
    expect(regular.entities.toSet(), rows.toSet());
    expect(regular.omittedIDs, isEmpty);
  });

  test(
    'one exact Moment Place projection label prefers selected without losing actions',
    () {
      final rows = [
        for (var i = 0; i < 4; i++)
          MapEntity(
            id: 'a-projection:$i',
            kind: MapEntityKind.moment,
            title: '正文',
            subtitle: '',
            latitude: 31.2,
            longitude: 121.4,
            placeID: 'place-original',
            momentID: 'original-moment',
            sourceVersion: 'source-$i',
          ),
        for (var i = 0; i < 6; i++)
          MapEntity(
            id: 'z-independent:$i',
            kind: MapEntityKind.moment,
            title: '独立正文$i',
            subtitle: '',
            latitude: 31.3 + i / 100,
            longitude: 121.4,
            momentID: 'independent-$i',
            sourceVersion: 'source-$i',
          ),
      ];
      final points = {
        for (var i = 0; i < rows.length; i++)
          rows[i].id: i < 4
              ? const Offset(360, 100)
              : Offset(80 + (i - 4) % 3 * 180.0, 240 + (i - 4) ~/ 3 * 120.0),
      };
      final plan = MapDisplayPolicy.plan(
        entities: rows,
        positions: points,
        zoom: 8,
        metersPerPixel: 40,
        selectedID: rows[3].id,
        contentRect: const Rect.fromLTWH(0, 0, 700, 500),
      );
      expect(plan.labelIDs.length, 6);
      expect(plan.labelIDs.where((id) => id.startsWith('a-projection:')), [
        rows[3].id,
      ]);
      expect(plan.entities.toSet(), rows.toSet());
      expect(plan.omittedIDs, isEmpty);
      expect(
        MapDisplayPolicy.hitTargetsAt(
          entities: plan.entities,
          positions: points,
          touchPosition: const Offset(360, 100),
        ).toSet(),
        rows.take(4).toSet(),
      );
      for (final row in rows) {
        expect(plan.glyphSizes[row.id], row.id == rows[3].id ? 20 : 18);
      }
    },
  );

  test(
    'labels retain distinct Moment IDs and real locations without text deduplication',
    () {
      final rows = [
        for (var i = 0; i < 4; i++)
          MapEntity(
            id: 'original:$i',
            kind: MapEntityKind.moment,
            title: '相同正文',
            subtitle: '',
            latitude: i == 1 ? 31.3 : 31.2,
            longitude: 121.4,
            placeID: i == 2 ? 'place-other' : 'place-original',
            momentID: i == 3 ? 'different-moment' : 'same-moment',
            sourceVersion: 'source-$i',
          ),
      ];
      final points = {
        rows[0].id: const Offset(300, 200),
        rows[1].id: const Offset(300, 400),
        rows[2].id: const Offset(300, 200),
        rows[3].id: const Offset(300, 200),
      };
      final plan = MapDisplayPolicy.plan(
        entities: rows,
        positions: points,
        zoom: 8,
        metersPerPixel: 40,
        contentRect: const Rect.fromLTWH(0, 0, 800, 600),
      );
      expect(plan.labelIDs.toSet(), rows.map((e) => e.id).toSet());
      expect(plan.entities.toSet(), rows.toSet());
      final labels = plan.placements.values.toList();
      for (var i = 0; i < labels.length; i++) {
        for (var j = i + 1; j < labels.length; j++) {
          expect(labels[i].rect.overlaps(labels[j].rect), isFalse);
        }
      }
    },
  );

  test(
    'selected Moment auxiliary removes only known repeated kind prefixes',
    () {
      for (final entry in const {
        '串文 · 2026年10月19日': '2026年10月19日',
        '单条动态 · 10月19日创建': '10月19日创建',
        '仅本人可见 · 来源待核实': '仅本人可见 · 来源待核实',
        '来源未知': '来源未知',
        '串文 · ': null,
      }.entries) {
        final row = MapEntity(
          id: 'moment:selected',
          kind: MapEntityKind.moment,
          title: '原始标题',
          subtitle: entry.key,
          latitude: 31.2,
          longitude: 121.4,
          threadID: 'authoritative-thread',
          sourceVersion: 'synthetic',
        );
        final plan = MapDisplayPolicy.plan(
          entities: [row],
          positions: {row.id: const Offset(150, 150)},
          zoom: 14,
          metersPerPixel: 2,
          selectedID: row.id,
          contentRect: const Rect.fromLTWH(0, 0, 500, 400),
        );
        expect(plan.placements[row.id]!.auxiliary, entry.value);
        expect(plan.placements[row.id]!.name, startsWith('串文 · '));
      }
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
      expect(t.getSize(shape), Size.square(moment ? 18 : 7));
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
