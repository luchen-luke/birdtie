import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_bitmap.dart';
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';
import 'package:birdtie_client/src/workspace/public_moment_photo.dart';
import 'package:birdtie_client/src/city/amap_city_map_stub.dart';
import 'entity_cover_test.dart' show coverJPEG;
import 'map_media_controller_test.dart' show mediaEntity, distinctMediaEntity;

Future<ui.Image> mapFixtureImage() async {
  final codec = await ui.instantiateImageCodec(coverJPEG());
  try {
    return (await codec.getNextFrame()).image;
  } finally {
    codec.dispose();
  }
}

Future<MapMediaPixels> fixturePixels(
  ui.Image image,
  bool Function() current, {
  MapEntityKind kind = MapEntityKind.place,
}) async => MapMediaPixels(
  key: 'isolated-synthetic-map-photo',
  image: image,
  normalPNG: await MapMarkerBitmap.render(
    kind: kind,
    selected: false,
    photo: image,
  ),
  selectedPNG: await MapMarkerBitmap.render(
    kind: kind,
    selected: true,
    photo: image,
  ),
  devicePixelRatio: 1,
  current: current,
);

const captureMap = Key('map1-isolated-390');
Future<void> capture(WidgetTester t, String name) async {
  final dir = Platform.environment['BIRDTIE_MAP_MEDIA_SCREENSHOT_DIR'];
  if (dir == null) return;
  await t.pump();
  final boundary = t.renderObject<RenderRepaintBoundary>(
    find.byKey(captureMap),
  );
  await t.runAsync(() async {
    final image = await boundary.toImage(pixelRatio: 1);
    try {
      final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
      final file = File('$dir/WIDGET_FIXTURE_NOT_DEVICE_390x844_$name.png');
      await file.parent.create(recursive: true);
      await file.writeAsBytes(bytes!.buffer.asUint8List());
    } finally {
      image.dispose();
    }
  });
}

void main() {
  testWidgets(
    'Map2 isolated normal390 square photo selection dense and retirement preserve48 targets',
    (t) async {
      t.view.devicePixelRatio = 1;
      t.view.physicalSize = const Size(390, 844);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPhysicalSize);
      final loaded = (await t.runAsync(() async {
        final bytes = await File(
          'test/fixtures/map-media/isolation-b.jpg',
        ).readAsBytes();
        final codec = await ui.instantiateImageCodec(bytes);
        try {
          return (bytes, (await codec.getNextFrame()).image);
        } finally {
          codec.dispose();
        }
      }))!;
      final bytes = loaded.$1, image = loaded.$2;
      var live = true, taps = 0;
      final pixels = (await t.runAsync(
        () => fixturePixels(image, () => live, kind: MapEntityKind.moment),
      ))!;
      final base = mediaEntity(1),
          photo = PublicMomentPhotoMetadata(
            momentID: base.placeID,
            momentRevision: 3,
            photoID: base.placeID,
            version: 1,
            path: publicMomentPhotoPath(base.placeID, 3, base.placeID, 1),
            byteCount: bytes.length,
            digest: sha256.convert(bytes).toString(),
            expiresAt: DateTime.now().toUtc().add(const Duration(seconds: 50)),
            attribution: '仅本机几何样例',
          );
      final entity = MapEntity(
        id: 'moment:${base.placeID}',
        kind: MapEntityKind.moment,
        title: '隔离公开记录 · 长中文与 Aberdeen 名称',
        subtitle: '关联地点，不是拍摄位置',
        latitude: base.latitude,
        longitude: base.longitude,
        momentPhoto: photo,
        mediaPixels: pixels,
      );
      Future<void> mount({
        bool selected = false,
        bool dense = false,
        bool showPhoto = true,
      }) async {
        await t.pumpWidget(
          MaterialApp(
            theme: ThemeData(fontFamily: 'MapMediaFixtureCJK'),
            home: RepaintBoundary(
              key: captureMap,
              child: Scaffold(
                body: Stack(
                  children: [
                    const Positioned.fill(
                      child: ColoredBox(color: Color(0xFFE6EDE7)),
                    ),
                    const Positioned(
                      left: 20,
                      right: 20,
                      top: 52,
                      child: Text(
                        '公开动态照片绘制隔离样例\n正常字号 · 非真机 · 非真实POI',
                        style: TextStyle(fontSize: 14),
                      ),
                    ),
                    Positioned(
                      left: 150,
                      top: 310,
                      child: IndividualMapMarker(
                        entity: showPhoto
                            ? entity
                            : entity.withMediaPixels(null),
                        selected: selected,
                        dense: dense,
                        showLabel: true,
                        onTap: () => ++taps,
                      ),
                    ),
                    const Positioned(
                      left: 20,
                      right: 20,
                      bottom: 54,
                      child: Text(
                        '图片仅表达公开内容；\n点位沿原关联地点，不推断拍摄坐标。',
                        style: TextStyle(fontSize: 14),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        );
        await t.pump();
        expect(t.takeException(), isNull);
        expect(
          t.getSize(find.byKey(ValueKey('individual-map-target:${entity.id}'))),
          const Size(48, 48),
        );
      }

      await mount(showPhoto: false);
      expect(find.byType(RawImage), findsNothing);
      await capture(t, 'map2_before');
      await mount();
      expect(
        find.byKey(ValueKey('map-moment-photo:${entity.id}')),
        findsOneWidget,
      );
      expect(
        t
            .widget<ClipRRect>(
              find.ancestor(
                of: find.byType(RawImage),
                matching: find.byType(ClipRRect),
              ),
            )
            .borderRadius,
        BorderRadius.circular(4),
      );
      expect(
        t.getSize(find.byKey(ValueKey('individual-map-shape:${entity.id}'))),
        const Size(28, 28),
      );
      await capture(t, 'map2_after');
      await mount(selected: true);
      expect(
        t.getSize(find.byKey(ValueKey('individual-map-shape:${entity.id}'))),
        const Size(36, 36),
      );
      await capture(t, 'map2_selected');
      await t.tap(find.byKey(ValueKey('individual-map-target:${entity.id}')));
      expect(taps, 1);
      await mount(dense: true);
      expect(find.byType(RawImage), findsNothing);
      expect(
        t.getSize(find.byKey(ValueKey('individual-map-shape:${entity.id}'))),
        const Size(16, 16),
      );
      await capture(t, 'map2_dense');
      live = false;
      await mount(selected: true);
      expect(find.byType(RawImage), findsNothing);
      expect(
        t.getSize(find.byKey(ValueKey('individual-map-shape:${entity.id}'))),
        const Size(36, 36),
      );
      await capture(t, 'map2_retired');
      await t.pumpWidget(const SizedBox());
      image.dispose();
    },
  );
  setUpAll(() async {
    final font = Platform.environment['BIRDTIE_MAP_MEDIA_FIXTURE_FONT'];
    if (font == null) return;
    await (FontLoader('MapMediaFixtureCJK')..addFont(
          Future.value(ByteData.sublistView(await File(font).readAsBytes())),
        ))
        .load();
    await (FontLoader('MaterialIcons')..addFont(
          Future.value(
            ByteData.sublistView(
              await File(
                r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
              ).readAsBytes(),
            ),
          ),
        ))
        .load();
  });
  testWidgets(
    'live Place round photo and square record keep 48 targets, expiry only retires pixels',
    (t) async {
      final image = (await t.runAsync(mapFixtureImage))!;
      var live = true, taps = 0;
      final pixels = (await t.runAsync(
        () => fixturePixels(image, () => live),
      ))!;
      final base = mediaEntity(1);
      final count = MapRecentMoments(
        count: 241,
        checkedAt: DateTime.now().toUtc(),
        windowStart: DateTime.now().toUtc().subtract(const Duration(days: 30)),
        validUntil: DateTime.now().toUtc().add(const Duration(seconds: 30)),
      );
      MapEntity row({bool photo = true}) => MapEntity(
        id: base.id,
        kind: base.kind,
        title: '当前受控照片地点 Aberdeen 合成长名称供完整详情核实',
        subtitle: base.subtitle,
        latitude: base.latitude,
        longitude: base.longitude,
        placePhoto: base.placePhoto,
        recentMoments: count,
        mediaPixels: photo ? pixels : null,
      );
      Future<void> mount() => t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Center(
              child: IndividualMapMarker(
                entity: row(),
                selected: true,
                showLabel: true,
                onTap: () {
                  ++taps;
                },
              ),
            ),
          ),
        ),
      );
      await mount();
      expect(find.byType(RawImage), findsOneWidget);
      expect(
        t.getSize(find.byKey(ValueKey('individual-map-target:${base.id}'))),
        const Size(48, 48),
      );
      expect(
        t.getSize(find.byKey(ValueKey('individual-map-shape:${base.id}'))),
        const Size(36, 36),
      );
      expect(find.text('近30天 · 241条动态'), findsOneWidget);
      await t.tap(find.byKey(ValueKey('individual-map-target:${base.id}')));
      expect(taps, 1);
      live = false;
      await mount();
      expect(find.byType(RawImage), findsNothing);
      expect(find.textContaining('当前受控照片地点'), findsOneWidget);
      expect(find.text('近30天 · 241条动态'), findsOneWidget);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      image.dispose();
    },
  );
  testWidgets(
    'AMap boundary keeps pixels/count but sees immutable removal fingerprint',
    (t) async {
      final image = (await t.runAsync(mapFixtureImage))!;
      var live = true;
      final pixels = (await t.runAsync(
        () => fixturePixels(image, () => live),
      ))!;
      final gate = AMapSourceGate(), base = mediaEntity(1);
      final decorated = base.withMediaPixels(pixels);
      expect(gate.replace([decorated], 'same-source'), isTrue);
      expect(gate.rows.single.mediaPixels, same(pixels));
      final oldRevision = gate.revision;
      live = false;
      expect(gate.replace([base.withMediaPixels(null)], 'same-source'), isTrue);
      expect(gate.revision, oldRevision + 1);
      expect(gate.rows.single.mediaPixels, isNull);
      expect(
        (gate.rows.single.id, gate.rows.single.longitude),
        (base.id, base.longitude),
      );
      gate.dispose();
      image.dispose();
    },
  );
  testWidgets(
    'Map1 isolated 390 CJK before after expiry ordinary font visual fixture',
    (t) async {
      t.view.devicePixelRatio = 1;
      t.view.physicalSize = const Size(390, 844);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPhysicalSize);
      final geometricBytes = (await t.runAsync(
        () => File('test/fixtures/map-media/isolation-a.jpg').readAsBytes(),
      ))!;
      final image = (await t.runAsync(() async {
        final codec = await ui.instantiateImageCodec(geometricBytes);
        try {
          return (await codec.getNextFrame()).image;
        } finally {
          codec.dispose();
        }
      }))!;
      var live = true;
      final pixels = (await t.runAsync(
        () => fixturePixels(image, () => live),
      ))!;
      final base = distinctMediaEntity(1, geometricBytes),
          now = DateTime.now().toUtc();
      final count = MapRecentMoments(
        count: 7,
        checkedAt: now,
        windowStart: now.subtract(const Duration(days: 30)),
        validUntil: now.add(const Duration(seconds: 30)),
      );
      Future<void> mount({required bool after}) async {
        final theme = ThemeData(fontFamily: 'MapMediaFixtureCJK');
        final entity = MapEntity(
          id: base.id,
          kind: base.kind,
          title: '隔离样例地点 · 长中文 Aberdeen 名称',
          subtitle: '仅合成媒体，不代表真实地点',
          latitude: base.latitude,
          longitude: base.longitude,
          placePhoto: base.placePhoto,
          mediaPixels: after ? pixels : null,
          recentMoments: after ? count : null,
        );
        await t.pumpWidget(
          MaterialApp(
            theme: theme,
            home: RepaintBoundary(
              key: captureMap,
              child: Scaffold(
                body: Stack(
                  children: [
                    const Positioned.fill(
                      child: ColoredBox(color: Color(0xFFE6EDE7)),
                    ),
                    const Positioned(
                      left: 20,
                      right: 20,
                      top: 52,
                      child: Text(
                        '地图绘制隔离样例\n普通字号 · 非真机 · 非真实POI',
                        style: TextStyle(fontSize: 16),
                      ),
                    ),
                    Positioned(
                      left: 112,
                      top: 294,
                      child: IndividualMapMarker(
                        entity: entity,
                        selected: true,
                        showLabel: true,
                        onTap: () {},
                      ),
                    ),
                    Positioned(
                      left: 268,
                      top: 432,
                      child: IndividualMapMarker(
                        entity: const MapEntity(
                          id: 'moment:isolated',
                          kind: MapEntityKind.moment,
                          title: '明确公开的记录',
                          subtitle: '合成记录',
                          latitude: 57.2,
                          longitude: -2.1,
                        ),
                        selected: false,
                        showLabel: true,
                        onTap: () {},
                      ),
                    ),
                    Positioned(
                      left: 66,
                      top: 440,
                      child: IndividualMapMarker(
                        entity: base,
                        selected: false,
                        dense: true,
                        onTap: () {},
                      ),
                    ),
                    const Positioned(
                      left: 20,
                      right: 20,
                      bottom: 54,
                      child: Text(
                        '点位、选择与相机保持；到期仅回退原点图标。\n此图是获准组件的本机隔离夹具。',
                        style: TextStyle(fontSize: 14),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        );
        await t.pump();
        expect(t.takeException(), isNull);
      }

      await mount(after: false);
      await capture(t, 'before');
      await mount(after: true);
      expect(find.byType(RawImage), findsOneWidget);
      await capture(t, 'after');
      live = false;
      await mount(after: true);
      expect(find.byType(RawImage), findsNothing);
      await capture(t, 'expired');
      await t.pumpWidget(const SizedBox());
      image.dispose();
    },
  );
}
