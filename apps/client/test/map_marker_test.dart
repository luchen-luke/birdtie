import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';
import 'package:birdtie_client/src/workspace/map_pin_category.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

MapEntity pin(
  String id, {
  String title = '地点名称',
  String category = 'cafe',
  MapEntityKind kind = MapEntityKind.place,
  MapRecentMoments? recent,
}) => MapEntity(
  id: id,
  kind: kind,
  title: title,
  subtitle: '',
  latitude: 31,
  longitude: 121,
  categoryCode: category,
  recentMoments: recent,
);

void main() {
  test(
    'authoritative category aliases stay local and unknown compound stays neutral with readable contrast',
    () {
      final known = [
        'restaurant',
        'cafe',
        'bar',
        'museum',
        'park',
        'sport',
        'shopping',
      ];
      expect(
        known.map((code) => MapPinCategory.forCode(code).kind).toSet(),
        hasLength(7),
      );
      for (final code in [...known, 'unclassified']) {
        final category = MapPinCategory.forCode(code);
        expect(
          1.05 / (category.fill.computeLuminance() + .05),
          greaterThanOrEqualTo(3),
        );
        expect(
          1.05 / (category.label.computeLuminance() + .05),
          greaterThanOrEqualTo(4.5),
        );
      }
      expect(MapPinCategory.forCode('museum+cafe'), same(MapPinCategory.other));
      expect(MapPinCategory.forCode('coffee_shop'), same(MapPinCategory.cafe));
      expect(MapPinCategory.forCode(''), same(MapPinCategory.other));
      expect(MapPinCategory.forCode('gallery'), same(MapPinCategory.culture));
    },
  );
  test(
    'measured scale controls geometry independently of provider zoom and counts',
    () {
      final entity = pin('place:p');
      MapDisplayPlan plan(double zoom, double scale, {String? selected}) =>
          MapDisplayPolicy.plan(
            entities: [entity],
            positions: {entity.id: const Offset(100, 100)},
            zoom: zoom,
            metersPerPixel: scale,
            selectedID: selected,
          );
      expect(plan(4, 2).glyphSizes[entity.id], 24);
      expect(plan(18, 50).glyphSizes[entity.id], 7);
      expect(plan(4, 50, selected: entity.id).glyphSizes[entity.id], 28);
      final distance = MapDisplayPolicy.scaleFromSegment(
        const Offset(0, 0),
        const Offset(.001, 0),
        100,
      );
      expect(distance, closeTo(1.11195, .002));
      expect(
        MapDisplayPolicy.scaleFromSegment(Offset.zero, Offset.zero, 0),
        isNull,
      );
    },
  );
  test(
    'label tries opposite side and removes auxiliary before hiding name within content bounds',
    () {
      final now = DateTime.now().toUtc();
      final entity = pin(
        'place:p',
        recent: MapRecentMoments.visible(
          count: 3,
          checkedAt: now,
          validUntil: now.add(const Duration(seconds: 30)),
          sourceVersion: 'current',
        ),
      );
      final neighbour = pin('place:q');
      final plan = MapDisplayPolicy.plan(
        entities: [entity, neighbour],
        zoom: 18,
        metersPerPixel: 2,
        positions: {
          entity.id: const Offset(150, 80),
          neighbour.id: const Offset(180, 80),
        },
        selectedID: entity.id,
        contentRect: const Rect.fromLTWH(0, 0, 200, 160),
      );
      final label = plan.placements[entity.id]!;
      expect(label.side, MapLabelSide.left);
      expect(label.fontSize, 13);
      expect(label.rect.width, lessThanOrEqualTo(112));
      expect(label.auxiliary, '3条动态');
      final narrow = MapDisplayPolicy.plan(
        entities: [entity],
        zoom: 18,
        metersPerPixel: 2,
        positions: {entity.id: const Offset(150, 80)},
        selectedID: entity.id,
        contentRect: const Rect.fromLTRB(0, 65, 200, 95),
      );
      expect(narrow.placements[entity.id]!.auxiliary, isNull);
      expect(narrow.placements[entity.id]!.name, '地点名称');
      final overview = MapDisplayPolicy.plan(
        entities: [entity],
        zoom: 18,
        metersPerPixel: 50,
        positions: {entity.id: const Offset(150, 80)},
      );
      expect(overview.placements, isEmpty);
    },
  );
  testWidgets(
    'place and moment visual geometry have independent 48dp targets and no generic moment label',
    (t) async {
      final source = [
        pin('place:p'),
        pin('moment:m', kind: MapEntityKind.moment, title: '动态'),
      ];
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Row(
              children: [
                for (final e in source)
                  IndividualMapMarker(
                    entity: e,
                    selected: false,
                    showLabel: true,
                    onTap: () {},
                  ),
              ],
            ),
          ),
        ),
      );
      expect(
        t.getSize(find.byKey(const ValueKey('individual-map-shape:place:p'))),
        const Size(24, 24),
      );
      expect(
        t.getSize(find.byKey(const ValueKey('individual-map-shape:moment:m'))),
        const Size(18, 18),
      );
      for (final e in source)
        expect(
          t.getSize(find.byKey(ValueKey('individual-map-target:${e.id}'))),
          const Size(48, 48),
        );
      expect(
        find.byKey(const ValueKey('individual-map-caption:moment:m')),
        findsNothing,
      );
      expect(find.byIcon(Icons.local_cafe_outlined), findsOneWidget);
      expect(t.takeException(), isNull);
    },
  );
}
