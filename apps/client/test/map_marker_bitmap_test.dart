import 'dart:typed_data';
import 'dart:convert';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_bitmap.dart';
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';
import 'package:birdtie_client/src/workspace/map_marker_widget.dart';
import 'map_media_rendering_test.dart' show mapFixtureImage;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
    'visible all-time totals and exact category survive decoration and expire independently',
    (tester) async {
      final now = DateTime.now().toUtc();
      final count = MapRecentMoments.visible(
        count: 7,
        checkedAt: now,
        validUntil: now.add(const Duration(seconds: 30)),
        sourceVersion: 'a' * 64,
      );
      final entity = MapEntity(
        id: 'owned-map:qa',
        kind: MapEntityKind.place,
        title: '有分类的地点',
        subtitle: '',
        latitude: 1,
        longitude: 1,
        categoryCode: 'museum',
        recentMoments: count,
      );
      expect(entity.withMediaPixels(null).categoryCode, 'museum');
      expect(
        entity
            .withAnchorMetadata(placeID: 'qa', precision: 'point')
            .categoryCode,
        'museum',
      );
      expect(count.labelAt(now), '7条动态');
      expect(count.labelAt(now.add(const Duration(seconds: 31))), isNull);
      expect(count.sourceKey, contains('visible:'));
      await tester.pumpWidget(
        MaterialApp(
          home: Center(
            child: IndividualMapMarker(
              entity: entity,
              selected: false,
              showLabel: true,
              onTap: () {},
            ),
          ),
        ),
      );
      expect(find.byIcon(Icons.museum_outlined), findsOneWidget);
      expect(find.text('7条动态'), findsNWidgets(2));
      expect(find.textContaining('近30天'), findsNothing);
      await tester.runAsync(() async {
        final font = FontLoader('MaterialIcons')
          ..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'));
        await font.load();
        final museum = await MapMarkerBitmap.render(
          kind: MapEntityKind.place,
          selected: false,
          categoryCode: 'museum',
        );
        final cafe = await MapMarkerBitmap.render(
          kind: MapEntityKind.place,
          selected: false,
          categoryCode: 'cafe',
        );
        expect(museum, isNot(orderedEquals(cafe)));
        expect(
          identical(
            museum,
            await MapMarkerBitmap.render(
              kind: MapEntityKind.place,
              selected: false,
              categoryCode: 'museum',
            ),
          ),
          isTrue,
        );
      });
      expect(MapMarkerVisual.placeIcon('unknown-type'), Icons.place_outlined);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
  testWidgets(
    'compact photo marker keeps right name count and 48dp target while retired pixels disappear',
    (tester) async {
      late ui.Image image;
      late Uint8List normal, selected;
      await tester.runAsync(() async {
        image = await mapFixtureImage();
        normal = await MapMarkerBitmap.render(
          kind: MapEntityKind.place,
          selected: false,
          photo: image,
        );
        selected = await MapMarkerBitmap.render(
          kind: MapEntityKind.place,
          selected: true,
          photo: image,
        );
        final normalPixels = await _pixels(normal);
        final selectedPixels = await _pixels(selected);
        expect(normalPixels.bounds(220).width, closeTo(28, 2.1));
        expect(selectedPixels.bounds(220).width, closeTo(36, 2.1));
        final whiteRim = (48 * selectedPixels.width + 33) * 4;
        expect(selectedPixels.bytes.sublist(whiteRim, whiteRim + 4), [
          255,
          255,
          255,
          255,
        ]);
      });
      var current = true, taps = 0;
      final now = DateTime.now().toUtc();
      final pixels = MapMediaPixels(
        key: 'current-approved-pixels',
        image: image,
        normalPNG: normal,
        selectedPNG: selected,
        devicePixelRatio: 1,
        current: () => current,
      );
      MapRecentMoments count(DateTime until) => MapRecentMoments(
        count: 4,
        checkedAt: now,
        windowStart: now.subtract(const Duration(days: 30)),
        validUntil: until,
      );
      Widget marker(MapRecentMoments recent) => MaterialApp(
        home: Scaffold(
          body: Center(
            child: IndividualMapMarker(
              entity: MapEntity(
                id: 'place:qa-place',
                kind: MapEntityKind.place,
                title: '有真实封面的地点 · North Harbour',
                subtitle: '当前可见地点',
                latitude: 30,
                longitude: 120,
                mediaPixels: pixels,
                recentMoments: recent,
              ),
              selected: true,
              showLabel: true,
              onTap: () => taps++,
            ),
          ),
        ),
      );
      try {
        await tester.pumpWidget(
          marker(count(now.add(const Duration(seconds: 30)))),
        );
        const target = ValueKey('individual-map-target:place:qa-place');
        const caption = ValueKey('individual-map-caption:place:qa-place');
        expect(tester.getSize(find.byKey(target)), const Size(48, 48));
        expect(find.byType(CompactMapPointMarker), findsOneWidget);
        expect(
          find.byKey(const ValueKey('map-place-photo:place:qa-place')),
          findsOneWidget,
        );
        expect(
          tester.getRect(find.byKey(caption)).left,
          greaterThan(tester.getCenter(find.byKey(target)).dx + 18),
        );
        expect(find.text('近30天 · 4条动态'), findsNWidgets(2));
        expect(
          MapMarkerVisual.nativeLabelOffset(selected: true, dense: false),
          [21 / 12, 0],
        );
        await tester.tap(find.byKey(target));
        expect(taps, 1);

        current = false;
        await tester.pumpWidget(
          marker(count(now.subtract(const Duration(seconds: 1)))),
        );
        expect(
          find.byKey(const ValueKey('map-place-photo:place:qa-place')),
          findsNothing,
        );
        expect(find.byIcon(Icons.place_outlined), findsOneWidget);
        expect(find.text('近30天 · 4条动态'), findsNothing);
        expect(find.byKey(caption), findsOneWidget);
        expect(tester.takeException(), isNull);
      } finally {
        await tester.pumpWidget(const SizedBox.shrink());
        image.dispose();
      }
    },
  );
  test(
    'public Moment actual JPEG uses square crop without record overlay and dense fallback remains6',
    () async {
      final image = await mapFixtureImage();
      try {
        final square = await _pixels(
          await MapMarkerBitmap.render(
            kind: MapEntityKind.moment,
            selected: false,
            photo: image,
          ),
        );
        final circle = await _pixels(
          await MapMarkerBitmap.render(
            kind: MapEntityKind.place,
            selected: false,
            photo: image,
          ),
        );
        final record = await _pixels(
          await MapMarkerBitmap.render(
            kind: MapEntityKind.moment,
            selected: false,
          ),
        );
        final dense = await _pixels(
          await MapMarkerBitmap.render(
            kind: MapEntityKind.moment,
            selected: false,
            dense: true,
            photo: image,
          ),
        );
        expect(square.alphaAt(36, 36), greaterThan(220));
        expect(circle.alphaAt(36, 36), lessThan(30));
        final center = (48 * square.width + 48) * 4;
        expect(
          square.bytes.sublist(center, center + 4),
          isNot(record.bytes.sublist(center, center + 4)),
        );
        expect(square.bounds(220).width, closeTo(28, 2.1));
        expect(dense.bounds(220).width, closeTo(6, 2.1));
        expect(dense.colorCount([135, 104, 60]), greaterThan(0));
      } finally {
        image.dispose();
      }
    },
  );
  test(
    'retired numeric offsets share the centered bitmap without displacement',
    () async {
      // Previously distinct ring positions are now deliberately ignored in
      // debug/profile/release. They cannot resurrect an off-anchor bitmap.
      const firstOffset = Offset(2.01, 0);
      const secondOffset = Offset(2.049, 0);
      final first = await MapMarkerBitmap.render(
        kind: MapEntityKind.moment,
        selected: false,
        dense: true,
        devicePixelRatio: 8,
        visualOffset: firstOffset,
      );
      final second = await MapMarkerBitmap.render(
        kind: MapEntityKind.moment,
        selected: false,
        dense: true,
        devicePixelRatio: 8,
        visualOffset: secondOffset,
      );
      expect(identical(first, second), isTrue);
      expect(first, orderedEquals(second));
      expect(
        identical(
          first,
          await MapMarkerBitmap.render(
            kind: MapEntityKind.moment,
            selected: false,
            dense: true,
            devicePixelRatio: 8,
            visualOffset: firstOffset,
          ),
        ),
        isTrue,
      );
    },
  );

  test(
    'all eight retired slots paint the same opaque square at the true anchor',
    () async {
      const offsets = [
        Offset(0, -28),
        Offset(19.79899, -19.79899),
        Offset(28, 0),
        Offset(19.79899, 19.79899),
        Offset(0, 28),
        Offset(-19.79899, 19.79899),
        Offset(-28, 0),
        Offset(-19.79899, -19.79899),
      ];
      final unique = <String>{};
      final images = <Uint8List>[];
      for (final offset in offsets) {
        final bytes = await MapMarkerBitmap.render(
          kind: MapEntityKind.moment,
          selected: false,
          dense: true,
          devicePixelRatio: 3.25,
          visualOffset: offset,
        );
        unique.add(base64Encode(bytes));
        images.add(bytes);
        final pixels = await _pixels(bytes);
        // Dense marks retain a small opaque center at the original anchor.
        // A transparent image or an off-anchor cached slot cannot pass.
        final x = (48 * 3.25).floor();
        final y = (48 * 3.25).floor();
        final index = (y * pixels.width + x) * 4;
        expect(pixels.bytes.sublist(index, index + 4), [0x87, 0x68, 0x3c, 255]);
      }
      expect(unique, hasLength(1));
      for (var i = 0; i < offsets.length; i++) {
        expect(
          identical(
            images[i],
            await MapMarkerBitmap.render(
              kind: MapEntityKind.moment,
              selected: false,
              dense: true,
              devicePixelRatio: 3.25,
              visualOffset: offsets[i],
            ),
          ),
          isTrue,
        );
      }
    },
  );
  for (final ratio in [1.0, 2.0, 3.25]) {
    for (final kind in [MapEntityKind.place, MapEntityKind.moment]) {
      test(
        '${kind.name} actual opaque glyph has readable density at DPR $ratio',
        () async {
          for (final state in [
            (false, true, 6.0),
            (false, false, 28.0),
            (true, true, 36.0),
          ]) {
            final bytes = await MapMarkerBitmap.render(
              kind: kind,
              selected: state.$1,
              dense: state.$2,
              devicePixelRatio: ratio,
            );
            final pixels = await _pixels(bytes);
            expect(pixels.width, (96 * ratio).ceil());
            expect(pixels.height, (96 * ratio).ceil());
            final opaque = pixels.bounds(220);
            // Actual painted alpha bounds, not merely a nonempty transparent PNG.
            expect(opaque.width, closeTo(state.$3 * ratio, 2.1));
            expect(opaque.height, closeTo(state.$3 * ratio, 2.1));
            final small = state.$2 && !state.$1;
            final color = kind == MapEntityKind.place
                ? small
                      ? [0x67, 0x87, 0x79]
                      : [0x19, 0x3b, 0x32]
                : [0x87, 0x68, 0x3c];
            expect(
              pixels.colorCount(color),
              greaterThan((small ? 6 : 35) * ratio * ratio),
            );
            if (!small)
              expect(
                pixels.colorCount([255, 255, 255]),
                greaterThan(15 * ratio * ratio),
              );
          }
        },
      );
    }
  }
  test(
    'true-anchor glyph has no connector and preserves bounded DPR cache',
    () async {
      final centered = await MapMarkerBitmap.render(
        kind: MapEntityKind.moment,
        selected: false,
        dense: true,
      );
      final moved = await MapMarkerBitmap.render(
        kind: MapEntityKind.moment,
        selected: false,
        dense: true,
        visualOffset: const Offset(28, 0),
      );
      final hd = await MapMarkerBitmap.render(
        kind: MapEntityKind.moment,
        selected: false,
        dense: true,
        devicePixelRatio: 3.25,
        visualOffset: const Offset(28, 0),
      );
      expect(moved, orderedEquals(centered));
      expect(hd, isNot(orderedEquals(moved)));
      final pixels = await _pixels(moved);
      expect(pixels.bounds(220).right, lessThanOrEqualTo(58));
      expect(pixels.alphaAt(48, 48), greaterThan(220));
      expect(pixels.alphaAt(66, 48), 0);
      expect(
        identical(
          moved,
          await MapMarkerBitmap.render(
            kind: MapEntityKind.moment,
            selected: false,
            dense: true,
            visualOffset: const Offset(28, 0),
          ),
        ),
        isTrue,
      );
      for (var i = 1; i <= MapMarkerBitmap.cacheLimit; i++) {
        await MapMarkerBitmap.render(
          kind: MapEntityKind.place,
          selected: false,
          count: i,
        );
      }
      expect(
        identical(
          moved,
          await MapMarkerBitmap.render(
            kind: MapEntityKind.moment,
            selected: false,
            dense: true,
            visualOffset: const Offset(28, 0),
          ),
        ),
        isFalse,
      );
    },
  );

  for (final ratio in [1.0, 3.25]) {
    test(
      'coincident opaque shapes stay within the real anchor at DPR $ratio',
      () async {
        const offsets = [
          Offset(0, -28),
          Offset(19.79899, -19.79899),
          Offset(28, 0),
          Offset(19.79899, 19.79899),
          Offset(0, 28),
          Offset(-19.79899, 19.79899),
          Offset(-28, 0),
          Offset(-19.79899, -19.79899),
        ];
        final fan = <Uint8List>[];
        for (final offset in offsets) {
          fan.add(
            await MapMarkerBitmap.render(
              kind: MapEntityKind.moment,
              selected: false,
              dense: true,
              devicePixelRatio: ratio,
              visualOffset: offset,
            ),
          );
        }
        for (final state in [(false, true), (false, false), (true, true)]) {
          final anchor = await MapMarkerBitmap.render(
            kind: MapEntityKind.place,
            selected: state.$1,
            dense: state.$2,
            devicePixelRatio: ratio,
          );
          final before = await _pixels(anchor);
          final after = await _composite([anchor, ...fan]);
          final diameter = state.$1
              ? 36.0
              : state.$2
              ? 6.0
              : 28.0;
          expect(before.bounds(220).width, closeTo(diameter * ratio, 2.1));
          expect(after.bounds(220).width, closeTo(diameter * ratio, 2.1));
          expect(after.bounds(220).height, closeTo(diameter * ratio, 2.1));
          expect(
            before.colorCount(
              state.$2 && !state.$1 ? [0x67, 0x87, 0x79] : [0x19, 0x3b, 0x32],
            ),
            greaterThan((state.$2 && !state.$1 ? 6 : 35) * ratio * ratio),
          );
          expect(
            after.colorCount([0x87, 0x68, 0x3c]),
            greaterThan(6 * ratio * ratio),
          );
        }
        // Former cardinal slots have no connector and the glyph occupies its
        // real anchor. Overlap is resolved by the complete current-ID chooser.
        final east = await _pixels(fan[2]);
        expect(east.alphaAt((66 * ratio).floor(), (48 * ratio).floor()), 0);
        expect(
          east.alphaAt((48 * ratio).floor(), (48 * ratio).floor()),
          greaterThan(220),
        );
      },
    );
  }

  test(
    'all six native glyphs distinct cached and selection distinct',
    () async {
      final signatures = <String>{};
      for (final kind in [
        MapEntityKind.place,
        MapEntityKind.activity,
        MapEntityKind.moment,
        MapEntityKind.organization,
        MapEntityKind.business,
        MapEntityKind.opportunity,
      ]) {
        final b = await MapMarkerBitmap.render(kind: kind, selected: false);
        expect(b.length, greaterThan(100));
        signatures.add(b.toString());
        expect(
          identical(
            b,
            await MapMarkerBitmap.render(kind: kind, selected: false),
          ),
          true,
        );
        expect(
          b.toString(),
          isNot(
            (await MapMarkerBitmap.render(
              kind: kind,
              selected: true,
            )).toString(),
          ),
        );
      }
      expect(signatures.length, 6);
    },
  );
}

Future<_Pixels> _composite(List<Uint8List> layers) async {
  final recorder = ui.PictureRecorder();
  final canvas = ui.Canvas(recorder);
  final images = <ui.Image>[];
  for (final layer in layers) {
    final codec = await ui.instantiateImageCodec(layer);
    final image = (await codec.getNextFrame()).image;
    codec.dispose();
    images.add(image);
    canvas.drawImage(image, Offset.zero, ui.Paint());
  }
  final picture = recorder.endRecording();
  final image = await picture.toImage(images.first.width, images.first.height);
  picture.dispose();
  for (final layer in images) {
    layer.dispose();
  }
  final data = (await image.toByteData(format: ui.ImageByteFormat.rawRgba))!;
  final result = _Pixels(
    image.width,
    image.height,
    data.buffer.asUint8List(data.offsetInBytes, data.lengthInBytes),
  );
  image.dispose();
  return result;
}

Future<_Pixels> _pixels(Uint8List png) async {
  final codec = await ui.instantiateImageCodec(png);
  final frame = await codec.getNextFrame();
  final image = frame.image;
  final data = (await image.toByteData(format: ui.ImageByteFormat.rawRgba))!;
  final result = _Pixels(
    image.width,
    image.height,
    data.buffer.asUint8List(data.offsetInBytes, data.lengthInBytes),
  );
  image.dispose();
  codec.dispose();
  return result;
}

class _Pixels {
  _Pixels(this.width, this.height, this.bytes);
  final int width, height;
  final Uint8List bytes;
  int alphaAt(int x, int y) => bytes[(y * width + x) * 4 + 3];
  int colorCount(List<int> color) {
    var count = 0;
    for (var i = 0; i < bytes.length; i += 4) {
      if (bytes[i + 3] == 255 &&
          bytes[i] == color[0] &&
          bytes[i + 1] == color[1] &&
          bytes[i + 2] == color[2])
        count++;
    }
    return count;
  }

  Rect bounds(int minimumAlpha) {
    var minX = width, minY = height, maxX = -1, maxY = -1;
    for (var y = 0; y < height; y++) {
      for (var x = 0; x < width; x++) {
        if (alphaAt(x, y) < minimumAlpha) continue;
        if (x < minX) minX = x;
        if (y < minY) minY = y;
        if (x > maxX) maxX = x;
        if (y > maxY) maxY = y;
      }
    }
    return maxX < 0
        ? Rect.zero
        : Rect.fromLTRB(
            minX.toDouble(),
            minY.toDouble(),
            maxX + 1.0,
            maxY + 1.0,
          );
  }
}
