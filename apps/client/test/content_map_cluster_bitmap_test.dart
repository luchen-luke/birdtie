import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_bitmap.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Future<(int, int, Uint8List)> pixels(Uint8List png) async {
  final codec = await ui.instantiateImageCodec(png);
  final frame = await codec.getNextFrame();
  codec.dispose();
  try {
    final bytes = (await frame.image.toByteData(
      format: ui.ImageByteFormat.rawRgba,
    ))!.buffer.asUint8List();
    return (frame.image.width, frame.image.height, bytes);
  } finally {
    frame.image.dispose();
  }
}

Rect inkBounds((int, int, Uint8List) image) {
  var left = image.$1, top = image.$2, right = -1, bottom = -1;
  for (var y = 0; y < image.$2; y++) {
    for (var x = 0; x < image.$1; x++) {
      if (image.$3[(y * image.$1 + x) * 4 + 3] < 200) continue;
      if (x < left) left = x;
      if (x > right) right = x;
      if (y < top) top = y;
      if (y > bottom) bottom = y;
    }
  }
  return Rect.fromLTRB(
    left.toDouble(),
    top.toDouble(),
    right + 1.0,
    bottom + 1.0,
  );
}

void main() {
  testWidgets(
    'content cluster has opaque centred square and readable count at phone DPR; people cluster stays unchanged',
    (t) async {
      await t.runAsync(() async {
        final normal = await MapMarkerBitmap.render(
          kind: MapEntityKind.cluster,
          selected: false,
          count: 2,
          approximate: true,
          devicePixelRatio: 1,
        );
        final phone = await MapMarkerBitmap.render(
          kind: MapEntityKind.cluster,
          selected: false,
          count: 2,
          approximate: true,
          devicePixelRatio: 3.25,
        );
        final a = await pixels(normal), b = await pixels(phone);
        expect((a.$1, a.$2), (96, 96));
        expect((b.$1, b.$2), (312, 312));
        final one = inkBounds(a), large = inkBounds(b);
        expect(one.width, greaterThanOrEqualTo(37));
        expect(one.height, greaterThanOrEqualTo(37));
        expect(large.width / 3.25, closeTo(one.width, 1));
        expect(large.height / 3.25, closeTo(one.height, 1));
        expect(large.center.dx / 3.25, closeTo(50, 1));
        expect(large.center.dy / 3.25, closeTo(46, 1));
        // Front card centres on (48,48); the back card extends four dp up/right.
        final center = ((48 * 3.25).round() * b.$1 + (48 * 3.25).round()) * 4;
        expect(b.$3[center + 3], 255);
        var whiteCountPixels = 0;
        for (var y = (40 * 3.25).round(); y < (56 * 3.25).round(); y++) {
          for (var x = (40 * 3.25).round(); x < (56 * 3.25).round(); x++) {
            final i = (y * b.$1 + x) * 4;
            if (b.$3[i] > 245 &&
                b.$3[i + 1] > 245 &&
                b.$3[i + 2] > 245 &&
                b.$3[i + 3] > 245)
              whiteCountPixels++;
          }
        }
        expect(whiteCountPixels, greaterThan(50));
        final twelve = await MapMarkerBitmap.render(
          kind: MapEntityKind.cluster,
          selected: false,
          count: 12,
          approximate: true,
          devicePixelRatio: 3.25,
        );
        // Flutter's Ahem fixture font uses the same glyph for single digits.
        // A second digit still exercises the count-dependent painted width.
        expect(twelve, isNot(orderedEquals(phone)));
        final people = await MapMarkerBitmap.render(
          kind: MapEntityKind.peopleCluster,
          selected: false,
          count: 2,
          approximate: true,
          devicePixelRatio: 1,
        );
        final peoplePhone = await MapMarkerBitmap.render(
          kind: MapEntityKind.peopleCluster,
          selected: false,
          count: 2,
          approximate: true,
          devicePixelRatio: 3.25,
        );
        expect(peoplePhone, orderedEquals(people));
        final legacy = await pixels(people);
        expect((legacy.$1, legacy.$2), (72, 84));
      });
    },
  );

  test(
    'neighbouring and selected place labels cannot cover a cluster count at either zoom; original coordinates and IDs remain',
    () {
      MapEntity item(String id, MapEntityKind kind, double longitude) =>
          MapEntity(
            id: id,
            kind: kind,
            title: 'Long neighbouring content label',
            subtitle: '',
            latitude: 57,
            longitude: longitude,
            precision: kind == MapEntityKind.cluster ? 'area' : 'point',
            contentCount: kind == MapEntityKind.cluster ? 2 : 1,
            memberIDs: kind == MapEntityKind.cluster
                ? const ['entry-a', 'entry-b']
                : const [],
          );
      final source = [
        item('owned-map:place:a', MapEntityKind.place, 1),
        item('owned-map:cluster:b', MapEntityKind.cluster, 2),
        item('owned-map:place:c', MapEntityKind.place, 3),
      ];
      final positions = {
        source[0].id: const Offset(100, 100),
        source[1].id: const Offset(180, 100),
        source[2].id: const Offset(400, 100),
      };
      for (final zoom in [12.0, 17.0]) {
        final plan = MapDisplayPolicy.plan(
          entities: source,
          positions: positions,
          zoom: zoom,
          selectedID: source.first.id,
        );
        expect(plan.labelIDs, isNot(contains(source.first.id)));
        expect(plan.labelIDs, contains(source[1].id));
        expect(plan.entities, unorderedEquals(source));
        expect(
          plan.entities.singleWhere((e) => e.id == source[1].id).memberIDs,
          ['entry-a', 'entry-b'],
        );
        expect(
          source
              .map(
                (e) => (
                  plan.entities.singleWhere((p) => p.id == e.id).latitude,
                  plan.entities.singleWhere((p) => p.id == e.id).longitude,
                ),
              )
              .toList(),
          [(57.0, 1.0), (57.0, 2.0), (57.0, 3.0)],
        );
        expect(plan.omittedIDs, isEmpty);
      }
    },
  );
}
