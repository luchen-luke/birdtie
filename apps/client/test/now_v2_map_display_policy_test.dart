import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_bitmap.dart';
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

MapEntity point(
  String id, {
  MapEntityKind kind = MapEntityKind.place,
  double latitude = 57.145,
  double longitude = -2.1,
  String placeID = '',
  String precision = 'point',
  bool task = false,
}) => MapEntity(
  id: id,
  kind: kind,
  title: '名称 $id',
  subtitle: '当前可见来源',
  latitude: latitude,
  longitude: longitude,
  placeID: placeID,
  precision: precision,
  isTaskResult: task,
);

void main() {
  test('chrome clips glyphs without moving or dropping authorized members', () {
    final a = point('place:a'), b = point('place:b'), c = point('place:c');
    final source = [a, b, c];
    final positions = {
      a.id: const Offset(100, 100),
      b.id: const Offset(102, 100),
      c.id: const Offset(250, 300),
    };
    MapDisplayPlan at(Rect rect) => MapDisplayPolicy.plan(
      entities: source,
      positions: positions,
      zoom: 18,
      metersPerPixel: 20,
      contentRect: rect,
    );
    final chrome = at(const Rect.fromLTRB(16, 110, 359, 550));
    final cluster = chrome.entities.singleWhere(
      MapDisplayPolicy.displayCluster,
    );
    expect(chrome.occludedGlyphIDs, {cluster.id});
    expect(chrome.placements.containsKey(cluster.id), false);
    expect(chrome.omittedIDs, isEmpty);
    expect(MapDisplayPolicy.currentDisplayClusterMembers(cluster, source), [
      a,
      b,
    ]);
    expect(chrome.clusterPositions[cluster.id], positions[a.id]);
    expect(chrome.entities.singleWhere((e) => e.id == c.id), same(c));
    final uncovered = at(const Rect.fromLTRB(16, 40, 359, 550));
    expect(uncovered.occludedGlyphIDs, isEmpty);
    expect(
      uncovered.entities.map((e) => e.id),
      chrome.entities.map((e) => e.id),
    );
    expect(uncovered.clusterPositions, chrome.clusterPositions);
  });
  test(
    'city ground coverage hides auxiliary counts independent of provider zoom',
    () {
      final now = DateTime.now().toUtc();
      final row = MapEntity(
        id: 'place:calibration',
        kind: MapEntityKind.place,
        title: '当前地点',
        subtitle: '',
        latitude: 57,
        longitude: -2,
        recentMoments: MapRecentMoments.visible(
          count: 1,
          checkedAt: now,
          validUntil: now.add(const Duration(minutes: 1)),
          sourceVersion: 'unit-current',
        ),
      );
      for (final zoom in [3.0, 18.0]) {
        for (final scale in [8.0, 15.99, 16.0, 18.49, 24.0, 40.0]) {
          final plan = MapDisplayPolicy.plan(
            entities: [row],
            positions: {row.id: const Offset(150, 200)},
            zoom: zoom,
            metersPerPixel: scale,
            contentRect: const Rect.fromLTWH(0, 0, 375, 600),
          );
          expect(plan.entities.single, same(row));
          expect(plan.glyphSizes[row.id], scale >= 16 ? 20 : 24);
          expect(
            plan.placements[row.id]!.auxiliary,
            scale >= 16 ? null : '1条动态',
          );
          expect(plan.metersPerPixel, scale);
        }
      }
      final selected = MapDisplayPolicy.plan(
        entities: [row],
        positions: {row.id: const Offset(150, 200)},
        zoom: 3,
        metersPerPixel: 40,
        selectedID: row.id,
      );
      expect(selected.glyphSizes[row.id], 28);
      expect(selected.placements[row.id]!.auxiliary, '1条动态');
    },
  );
  test(
    'actual touch uses 48dp point targets and nearest-first current candidates',
    () {
      final target = point('place:z-target');
      final neighbour = point('moment:a-neighbour', kind: MapEntityKind.moment);
      final distant = point('place:a-wrong');
      final cluster = point(
        'cluster:original',
        kind: MapEntityKind.cluster,
        precision: 'area',
      );
      final source = [distant, neighbour, target, cluster];
      final before = source
          .map((e) => (e.id, e.latitude, e.longitude, e.precision))
          .toList();
      final positions = {
        target.id: const Offset(160, 365),
        neighbour.id: const Offset(180, 370),
        distant.id: const Offset(160, 306.5),
        cluster.id: const Offset(330, 330),
      };
      List<MapEntity> at(Offset position) => MapDisplayPolicy.hitTargetsAt(
        entities: source,
        positions: positions,
        touchPosition: position,
        nonPointHitRects: {
          cluster.id: const Rect.fromLTRB(294, 246, 366, 354),
          // Point objects must ignore a provider's large transparent canvas.
          distant.id: const Rect.fromLTWH(100, 250, 200, 200),
        },
      );
      expect(at(const Offset(160, 365)).map((e) => e.id), [
        target.id,
        neighbour.id,
      ]);
      expect(at(const Offset(160, 425)), isEmpty);
      expect(at(const Offset(136, 365)).map((e) => e.id), [target.id]);
      expect(at(const Offset(135.9, 365)), isEmpty);
      expect(at(const Offset(330, 281)).single, same(cluster));
      expect(at(const Offset(double.nan, 365)), isEmpty);
      positions[distant.id] = const Offset(double.infinity, 0);
      expect(at(const Offset(160, 365)).map((e) => e.id), [
        target.id,
        neighbour.id,
      ]);
      expect(
        source
            .map((e) => (e.id, e.latitude, e.longitude, e.precision))
            .toList(),
        before,
      );
    },
  );

  test(
    'six labels prioritize selected and task without omitting authorized points',
    () {
      final source = [
        for (var i = 0; i < 20; i++) point('place:$i', task: i == 19),
      ];
      final plan = MapDisplayPolicy.plan(
        entities: source,
        positions: {
          for (var i = 0; i < source.length; i++)
            source[i].id: Offset(i * 100.0, 0),
        },
        zoom: 18,
        selectedID: 'place:18',
        protectedIDs: {'place:19'},
      );
      expect(plan.entities.length, 20);
      expect(plan.labelIDs.length, 6);
      expect(plan.labelIDs.take(2), ['place:18', 'place:19']);
      expect(plan.omittedIDs, isEmpty);
    },
  );
  test(
    'same-anchor objects stay on current SDK pixels beside a nearby point',
    () {
      final place = point('place:anchor');
      final moments = [
        for (var i = 0; i < 7; i++)
          point('moment:$i', kind: MapEntityKind.moment),
      ];
      final neighbour = point('place:neighbour', longitude: -2.099);
      final source = [place, ...moments, neighbour];
      final positions = {
        for (final e in [place, ...moments]) e.id: const Offset(100, 100),
        neighbour.id: const Offset(129.5, 97.8),
      };
      final original = MapDisplayPolicy.coincidentPointOffsets(source);
      expect(
        moments.map((e) => original[e.id]).every((o) => o == Offset.zero),
        isTrue,
      );
      final moved = MapDisplayPolicy.coincidentPointOffsets(
        source,
        positions: positions,
      );
      expect(moved[place.id], Offset.zero);
      expect(moved.containsKey(neighbour.id), isFalse);
      expect(moments.map((e) => moved[e.id]).toSet(), {Offset.zero});
      expect(
        moments.map((e) => moved[e.id]),
        isNot(contains(const Offset(28, 0))),
      );
      for (final moment in moments) {
        expect(
          ((positions[moment.id]! + moved[moment.id]!) -
                  positions[neighbour.id]!)
              .distance,
          greaterThan(18),
        );
        expect(moment.longitude, -2.1);
        expect(moment.latitude, 57.145);
      }
      expect(neighbour.longitude, -2.099);
      final translated = {
        for (final entry in positions.entries)
          entry.key: entry.value + const Offset(611, -903),
      };
      expect(
        MapDisplayPolicy.coincidentPointOffsets(
          source.reversed.toList(),
          positions: translated,
        ),
        moved,
      );
    },
  );

  test(
    'true anchors ignore stale projected IDs, approximate areas and invalid pixels',
    () {
      final place = point('place:anchor');
      final moments = [
        for (var i = 0; i < 7; i++)
          point('moment:$i', kind: MapEntityKind.moment),
      ];
      final source = [place, ...moments];
      final original = MapDisplayPolicy.coincidentPointOffsets(source);
      final positions = {
        for (final e in source) e.id: const Offset(100, 100),
        'place:retired': const Offset(128, 100),
      };
      expect(
        MapDisplayPolicy.coincidentPointOffsets(source, positions: positions),
        original,
      );
      final area = point('place:retired', longitude: -2.099, precision: 'area');
      expect(
        MapDisplayPolicy.coincidentPointOffsets([
          ...source,
          area,
        ], positions: positions),
        original,
      );
      final invalid = point('place:retired', longitude: -2.099);
      positions[invalid.id] = const Offset(double.nan, 100);
      expect(
        MapDisplayPolicy.coincidentPointOffsets([
          ...source,
          invalid,
        ], positions: positions),
        original,
      );
      positions[place.id] = const Offset(double.infinity, 100);
      expect(
        MapDisplayPolicy.coincidentPointOffsets(source, positions: positions),
        original,
      );
    },
  );
  test(
    'same-coordinate overlap preserves all 115 identities with zero displacement and excludes private areas',
    () {
      final place = point('place:anchor');
      final moments = [
        for (var i = 0; i < 114; i++)
          point(
            'moment:${i.toString().padLeft(3, '0')}',
            kind: MapEntityKind.moment,
          ),
      ];
      final source = [place, ...moments];
      final before = source
          .map((e) => (e.id, e.latitude, e.longitude, e.precision))
          .toList();
      final offsets = MapDisplayPolicy.coincidentPointOffsets(source);
      expect(offsets.length, 115);
      expect(offsets[place.id], Offset.zero);
      expect(offsets[moments.first.id], Offset.zero);
      expect(offsets.values.every((p) => p == Offset.zero), isTrue);
      expect(offsets.values.toSet(), {Offset.zero});
      expect(
        MapDisplayPolicy.coincidentPointOffsets(source.reversed.toList()),
        offsets,
      );
      expect(
        source
            .map((e) => (e.id, e.latitude, e.longitude, e.precision))
            .toList(),
        before,
      );
      final area = MapEntity(
        id: 'moment:area',
        kind: MapEntityKind.moment,
        title: '大致区域',
        subtitle: '不增强精度',
        latitude: place.latitude,
        longitude: place.longitude,
        precision: 'area',
      );
      expect(MapDisplayPolicy.coincidentPointOffsets([place, area]), isEmpty);
      expect(MapDisplayPolicy.coincidentPointOffsets([place]), isEmpty);
    },
  );
  test(
    'eighteen sparse anchors remain original and density follows reviewed radii',
    () {
      final source = [
        for (var i = 0; i < 18; i++)
          point(
            'place:$i',
            latitude: 57.14 + i * .001,
            longitude: -2.1 + i * .001,
          ),
      ];
      final plan = MapDisplayPolicy.plan(
        entities: source,
        zoom: 12,
        positions: {
          for (var i = 0; i < source.length; i++)
            source[i].id: Offset(30 + (i % 6) * 65, 150 + (i ~/ 6) * 65),
        },
      );
      expect(
        plan.entities.map((e) => e.id).toSet(),
        source.map((e) => e.id).toSet(),
      );
      expect(plan.denseIDs, isEmpty);
      for (final level in MapDisplayPolicy.levels) {
        expect(level.collisionRadius, level.iconCellLogicalPixels / 2);
      }
      final nearby = [point('a'), point('b')];
      final positions = {
        'a': const Offset(100, 100),
        'b': const Offset(130, 100),
      };
      expect(
        MapDisplayPolicy.plan(
          entities: nearby,
          positions: positions,
          zoom: 10,
        ).denseIDs,
        {'a', 'b'},
      );
      expect(
        MapDisplayPolicy.plan(
          entities: nearby,
          positions: positions,
          zoom: 18,
          metersPerPixel: 2,
        ).denseIDs,
        isEmpty,
      );
    },
  );
  test('shared place metadata never replaces conflicting real coordinates', () {
    final source = [
      point('place:p', placeID: 'p'),
      point(
        'activity:a',
        kind: MapEntityKind.activity,
        placeID: 'p',
        latitude: 57.25,
        longitude: -2.15,
      ),
    ];
    final plan = MapDisplayPolicy.plan(
      entities: source,
      zoom: 10,
      positions: {for (final e in source) e.id: const Offset(100, 100)},
    );
    for (final entity in plan.entities) {
      expect(entity, same(source.singleWhere((e) => e.id == entity.id)));
    }
    expect(plan.entities, hasLength(2));
  });
  for (final zoom in [3.0, 10.0, 12.0, 14.0, 16.0, 18.0]) {
    test(
      'all 114 eligible objects survive zoom $zoom, tiny budget and chrome',
      () {
        final source = [for (var n = 0; n < 114; n++) point('place:$n')];
        final plan = MapDisplayPolicy.plan(
          entities: source,
          positions: {for (final e in source) e.id: const Offset(100, 100)},
          zoom: zoom,
          ambientLimit: 1,
          contentRect: const Rect.fromLTWH(0, 0, 1, 1),
          previouslyVisibleIDs: {'withdrawn:old'},
          selectedID: 'place:113',
        );
        // New PMAP local display groups retain each original ID exactly once.
        final represented = <String>[];
        for (final entity in plan.entities) {
          if (MapDisplayPolicy.displayCluster(entity)) {
            expect(entity.memberIDs.length, lessThanOrEqualTo(32));
            final members = MapDisplayPolicy.currentDisplayClusterMembers(
              entity,
              source,
            );
            expect(members.length, entity.memberIDs.length);
            represented.addAll(members.map((e) => e.id));
          } else {
            represented.add(entity.id);
          }
        }
        expect(represented.toSet(), source.map((e) => e.id).toSet());
        expect(represented, hasLength(114));
        expect(plan.entities.any((e) => e.id == 'place:113'), isTrue);
        expect(plan.omittedIDs, isEmpty);
        expect(
          plan.labelIDs,
          isEmpty,
        ); // No label is allowed outside contentRect.
        expect(plan.denseIDs, isNot(contains('place:113')));
      },
    );
  }
  test(
    'coincident PLACE and MOMENT keep independent kind identity and true coordinate',
    () {
      final place = point('place:p', placeID: 'p');
      final moment = point(
        'moment:m',
        kind: MapEntityKind.moment,
        placeID: 'p',
      );
      final plan = MapDisplayPolicy.plan(
        entities: [place, moment],
        zoom: 18,
        selectedID: moment.id,
        positions: {
          place.id: const Offset(100, 100),
          moment.id: const Offset(100, 100),
        },
      );
      expect(plan.entities, hasLength(2));
      expect(plan.entities.singleWhere((e) => e.id == moment.id), same(moment));
      expect(plan.entities.singleWhere((e) => e.id == place.id), same(place));
      expect(plan.denseIDs, {place.id});
      expect(
        (moment.latitude, moment.longitude),
        (place.latitude, place.longitude),
      );
    },
  );
  test('screen neighbours and a transitive chain never create a cluster', () {
    final source = [point('a'), point('b'), point('c')];
    final plan = MapDisplayPolicy.plan(
      entities: source,
      zoom: 10,
      positions: {
        'a': const Offset(0, 0),
        'b': const Offset(30, 0),
        'c': const Offset(60, 0),
      },
    );
    expect(plan.entities.map((e) => e.id), ['a', 'b', 'c']);
    expect(plan.denseIDs, {'a', 'b', 'c'});
  });
  test(
    'withdrawal and reordering do not resurrect previous protected identities',
    () {
      final a = point('a'), b = point('b'), c = point('c');
      final positions = {
        'a': const Offset(0, 0),
        'b': const Offset(100, 0),
        'c': const Offset(200, 0),
      };
      final first = MapDisplayPolicy.plan(
        entities: [c, a, b],
        positions: positions,
        zoom: 18,
      );
      final next = MapDisplayPolicy.plan(
        entities: [b, b, c],
        positions: positions,
        zoom: 18,
        protectedIDs: {'a'},
        selectedID: 'a',
        previouslyVisibleIDs: first.entities.map((e) => e.id).toSet(),
      );
      expect(first.entities.map((e) => e.id), ['a', 'b', 'c']);
      expect(next.entities.map((e) => e.id), ['b', 'c']);
      expect(next.labelIDs, isNot(contains('a')));
    },
  );
  test(
    'unknown illegal and missing projections remain unpinned, even protected',
    () {
      final source = [
        point('unknown', precision: 'unknown'),
        point('invalid', latitude: double.nan),
        point('range', latitude: 91),
        point('missing'),
        point('infinite'),
        point('known'),
      ];
      final plan = MapDisplayPolicy.plan(
        entities: source,
        zoom: 18,
        selectedID: 'unknown',
        protectedIDs: source.map((e) => e.id).toSet(),
        positions: {
          for (final e in source.where((e) => e.id != 'missing'))
            e.id: e.id == 'infinite'
                ? const Offset(double.infinity, 0)
                : const Offset(10, 10),
        },
      );
      expect(plan.entities.map((e) => e.id), ['known']);
      expect(source, hasLength(6));
    },
  );
  test('approximate person area remains approximate beside exact objects', () {
    final area = point(
      'person:area',
      kind: MapEntityKind.person,
      precision: 'area',
    );
    final plan = MapDisplayPolicy.plan(
      entities: [area, point('place:p')],
      zoom: 12,
      positions: {
        'person:area': const Offset(10, 10),
        'place:p': const Offset(10, 10),
      },
    );
    expect(plan.entities.singleWhere((e) => e.id == area.id), same(area));
    expect(plan.denseIDs, isNot(contains(area.id)));
    expect(MapDisplayPolicy.countLabel([area]), '1个大致区域');
  });
  test(
    '48dp overlap choices identify originals without moving or authorizing them',
    () {
      final a = point('place:a'),
          b = point('moment:b', kind: MapEntityKind.moment),
          c = point('place:c');
      final hits = MapDisplayPolicy.overlappingHitTargets(
        entities: [a, b, c],
        positions: {
          'place:a': const Offset(100, 100),
          'moment:b': const Offset(148, 100),
          'place:c': const Offset(149, 100),
        },
        tappedID: a.id,
      );
      expect(hits.map((e) => e.id).toSet(), {a.id, b.id});
      expect(hits.singleWhere((e) => e.id == b.id), same(b));
      expect(
        MapDisplayPolicy.overlappingHitTargets(
          entities: [b, c],
          positions: {'place:a': const Offset(100, 100)},
          tappedID: a.id,
        ),
        isEmpty,
      );
    },
  );
  test(
    'existing public diagnostic has nineteen objects, including two at one coordinate',
    () {
      final fixture =
          jsonDecode(
                File(
                  'test/fixtures/now_v2_public_map_diagnostic.json',
                ).readAsStringSync(),
              )
              as Map;
      final items = (fixture['items'] as List).cast<Map>();
      final source = [
        for (final item in items)
          point(
            '${item['kind']}:${item['entityID']}',
            kind: item['kind'] == 'PLACE'
                ? MapEntityKind.place
                : MapEntityKind.moment,
            latitude: (item['latitude'] as num).toDouble(),
            longitude: (item['longitude'] as num).toDouble(),
            placeID: item['placeID'] as String,
          ),
      ];
      final plan = MapDisplayPolicy.plan(
        entities: source,
        zoom: 3,
        positions: {for (final e in source) e.id: const Offset(100, 100)},
      );
      expect(fixture['scope'], 'PUBLIC');
      expect(source, hasLength(19));
      final represented = [
        for (final e in plan.entities)
          if (MapDisplayPolicy.displayCluster(e))
            ...MapDisplayPolicy.currentDisplayClusterMembers(e, source)
          else
            e,
      ];
      expect(
        represented.map((e) => e.id).toSet(),
        source.map((e) => e.id).toSet(),
      );
      expect(
        represented.where((e) => e.kind == MapEntityKind.place),
        hasLength(18),
      );
      expect(
        represented.where((e) => e.kind == MapEntityKind.moment),
        hasLength(1),
      );
      expect(
        represented.map((e) => (e.latitude, e.longitude)).toSet(),
        hasLength(18),
      );
    },
  );
  test('focus insets still consume the visible content region', () {
    expect(
      MapDisplayPolicy.focusPadding(
        const Size(430, 900),
        const Rect.fromLTRB(0, 130, 430, 610),
      ),
      const EdgeInsets.fromLTRB(24, 154, 24, 314),
    );
  });
  for (final kind in [MapEntityKind.place, MapEntityKind.moment]) {
    for (final scale in [1.0, 2.0]) {
      testWidgets(
        '${kind.name} dense marker preserves 48dp target and full semantic label at scale $scale',
        (t) async {
          final entity = point('${kind.name}:original', kind: kind);
          var clicks = 0;
          final semantics = t.ensureSemantics();
          await t.pumpWidget(
            MaterialApp(
              home: MediaQuery(
                data: MediaQueryData(textScaler: TextScaler.linear(scale)),
                child: Scaffold(
                  body: Center(
                    child: IndividualMapMarker(
                      entity: entity,
                      selected: false,
                      dense: true,
                      showLabel: false,
                      onTap: () => clicks++,
                    ),
                  ),
                ),
              ),
            ),
          );
          final target = find.byKey(
            ValueKey('individual-map-target:${entity.id}'),
          );
          expect(t.getSize(target), const Size(48, 48));
          expect(
            find.bySemanticsLabel(RegExp(RegExp.escape(entity.title))),
            findsOneWidget,
          );
          final shape =
              t
                      .widget<Container>(
                        find.byKey(
                          ValueKey('individual-map-shape:${entity.id}'),
                        ),
                      )
                      .decoration!
                  as BoxDecoration;
          expect(
            t.getSize(
              find.byKey(ValueKey('individual-map-shape:${entity.id}')),
            ),
            kind == MapEntityKind.place
                ? const Size(20, 20)
                : const Size(30, 30),
          );
          expect(shape.color!.a, 1);
          expect(shape.border, isNotNull);
          expect(
            shape.shape,
            kind == MapEntityKind.place ? BoxShape.circle : BoxShape.rectangle,
          );
          await t.tap(target);
          expect(clicks, 1);
          expect(t.takeException(), isNull);
          semantics.dispose();
        },
      );
    }
  }
  test(
    'native precise circle square and pale bitmaps remain distinct and cached',
    () async {
      final circle = await MapMarkerBitmap.render(
        kind: MapEntityKind.place,
        selected: false,
      );
      final square = await MapMarkerBitmap.render(
        kind: MapEntityKind.moment,
        selected: false,
      );
      final pale = await MapMarkerBitmap.render(
        kind: MapEntityKind.moment,
        selected: false,
        dense: true,
      );
      expect(circle, isNot(orderedEquals(square)));
      expect(square, orderedEquals(pale));
      expect(
        identical(
          pale,
          await MapMarkerBitmap.render(
            kind: MapEntityKind.moment,
            selected: false,
            dense: true,
          ),
        ),
        isTrue,
      );
      final approximate = await MapMarkerBitmap.render(
        kind: MapEntityKind.person,
        selected: false,
        approximate: true,
      );
      expect(approximate, isNot(orderedEquals(circle)));
    },
  );
}
