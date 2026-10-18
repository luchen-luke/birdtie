import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/city/native_city_map_io.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/workspace/map_marker_bitmap.dart';
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';
import 'map_media_rendering_test.dart' show mapFixtureImage, fixturePixels;
import 'map_media_controller_test.dart' show mediaEntity, mediaWait;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart' hide Size;

// Actual NativeCityMapView/annotation/callback paths, fake SDK transport.
// PUBLIC capture is an offline replay; no device paint or real network proof.
void main() {
  testWidgets(
    'actual touch native selection ignores distant transparent canvases and retains semantic and source-cluster actions',
    (t) async {
      MapEntity row(
        String id,
        double longitude, {
        MapEntityKind kind = MapEntityKind.place,
        List<String> members = const [],
      }) => MapEntity(
        id: id,
        kind: kind,
        title: id,
        subtitle: '合成当前来源',
        latitude: 57.145,
        longitude: longitude,
        memberIDs: members,
        precision: kind == MapEntityKind.cluster ? 'area' : 'point',
      );
      final target = row('place:z-target', -2.1);
      final near = row('moment:a-near', -2.099, kind: MapEntityKind.moment);
      final wrong = row('place:a-wrong', -2.098);
      final first = row('place:member-one', -2.097);
      final second = row('place:member-two', -2.096);
      final cluster = row(
        'cluster:original',
        -2.095,
        kind: MapEntityKind.cluster,
        members: [first.id, second.id],
      );
      final source = [wrong, near, target, first, second, cluster];
      final map = _Map();
      final positions = {
        target.id: const Offset(160, 365),
        near.id: const Offset(180, 370),
        wrong.id: const Offset(160, 306.5),
        first.id: const Offset(40, 520),
        second.id: const Offset(390, 520),
        cluster.id: const Offset(330, 330),
      };
      for (final entity in source) {
        final p = positions[entity.id]!;
        map.positions['${entity.longitude}:${entity.latitude}'] =
            ScreenCoordinate(x: p.dx, y: p.dy);
      }
      MapEntity? selected;
      await _mount(
        t,
        map,
        source,
        selected: target.id,
        onSelected: (e) => selected = e,
      );
      await mediaWait(t, () => map.points.created.length == source.length);
      // The adapter must not subscribe to SDK image-box selections: that sink
      // consumes the actual map tap before touchPosition can reach Flutter.
      expect(map.points.onTap, isNull);
      _tapAt(t, positions[target.id]!);
      await t.pumpAndSettle();
      final chooser = find.byKey(const Key('current-map-point-choices'));
      final list = t.widget<ListView>(
        find.descendant(of: chooser, matching: find.byType(ListView)),
      );
      final rows = (list.childrenDelegate as SliverChildListDelegate).children
          .cast<ListTile>()
          .toList();
      expect(rows.map((r) => r.key), [
        ValueKey('map-point-choice:${target.id}'),
        ValueKey('map-point-choice:${near.id}'),
      ]);
      expect(
        find.byKey(ValueKey('map-point-choice:${wrong.id}')),
        findsNothing,
      );
      await t.tap(find.byKey(ValueKey('map-point-choice:${target.id}')));
      await t.pumpAndSettle();
      expect(selected?.id, target.id);
      selected = null;
      _tapAt(t, const Offset(160, 425));
      await t.pumpAndSettle();
      expect(chooser, findsNothing);
      expect(selected, isNull);

      // Accessibility still identifies a real object directly.
      t
          .widget<Semantics>(
            find.byKey(ValueKey('native-map-point-semantics:${wrong.id}')),
          )
          .properties
          .onTap!();
      await t.pumpAndSettle();
      expect(selected?.id, wrong.id);
      selected = null;

      // A genuine source cluster retains its original BOTTOM-painted canvas,
      // even where its visible graphic lies above a point's 48 dp target.
      _tapAt(t, const Offset(330, 330));
      await t.pumpAndSettle();
      final members = find.byKey(const Key('current-map-cluster-members'));
      expect(members, findsOneWidget);
      expect(
        find.byKey(ValueKey('current-map-member:${first.id}')),
        findsOneWidget,
      );
      expect(
        find.byKey(ValueKey('current-map-member:${second.id}')),
        findsOneWidget,
      );
      Navigator.of(t.element(members)).pop();
      await t.pumpAndSettle();
      expect(selected, isNull);
      for (final entity in source) {
        final annotation = map.points.created[entity.id]!;
        expect(annotation.geometry.coordinates.lng, entity.longitude);
        expect(annotation.geometry.coordinates.lat, entity.latitude);
      }
      expect(map.points.created.length, source.length);

      // A tap awaiting a screen projection cannot select a retired source.
      final gate = Completer<List<ScreenCoordinate?>>();
      map.projectionGate = gate;
      _tapAt(t, positions[target.id]!);
      await t.pump();
      await _update(t, map, const [], onSelected: (e) => selected = e);
      map.projectionGate = null;
      gate.complete([
        for (final e in source) map.positions['${e.longitude}:${e.latitude}'],
      ]);
      await t.pumpAndSettle();
      expect(selected, isNull);
      expect(chooser, findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );

  testWidgets(
    'Map1 native retires same-point pixels before held camera, without camera reset',
    (t) async {
      final image = (await t.runAsync(mapFixtureImage))!;
      var live = true;
      final pixels = (await t.runAsync(
        () => fixturePixels(image, () => live),
      ))!;
      final base = mediaEntity(1);
      final now = DateTime.now().toUtc();
      final row = MapEntity(
        id: base.id,
        kind: base.kind,
        title: base.title,
        subtitle: base.subtitle,
        latitude: base.latitude,
        longitude: base.longitude,
        placeID: base.placeID,
        placePhoto: base.placePhoto,
        mediaPixels: pixels,
        recentMoments: MapRecentMoments(
          count: 241,
          checkedAt: now,
          windowStart: now.subtract(const Duration(days: 30)),
          validUntil: now.add(const Duration(seconds: 30)),
        ),
      );
      final map = _Map();
      await _mount(t, map, [row], selected: row.id);
      await mediaWait(t, () => map.points.created.containsKey(row.id));
      final local = (await t.runAsync(
        () => MapMarkerBitmap.render(kind: MapEntityKind.place, selected: true),
      ))!;
      expect(map.points.created[row.id]!.image, orderedEquals(local));
      expect(map.points.created[row.id]!.textField, isNot(contains('241')));
      expect(
        map.points.created['map-aux:${row.id}']!.textField,
        '近30天 · 241条动态',
      );
      final native = find.byType(NativeCityMapView).evaluate().single,
          sdk = find.byType(MapWidget).evaluate().single;
      map.flights.clear();
      live = false;
      map.cameraGate = Completer<CameraState>();
      await _update(t, map, [base], selected: row.id);
      await mediaWait(t, () => map.points.updated.containsKey(row.id));
      final early = map.points.updated[row.id]!;
      expect(early.image, orderedEquals(local));
      expect(map.points.created.containsKey('map-aux:${row.id}'), isFalse);
      expect(early.geometry.coordinates.lng, base.longitude);
      expect(early.textField, base.title);
      expect(map.flights, isEmpty);
      expect(find.byType(NativeCityMapView).evaluate().single, same(native));
      expect(find.byType(MapWidget).evaluate().single, same(sdk));
      final gate = map.cameraGate!;
      map.cameraGate = null;
      gate.complete(await map.getCameraState());
      await mediaWait(t, () => map.projectionCalls >= 2);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      image.dispose();
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'native same-anchor overlap uses raw SDK pixels for images, semantics and choice',
    (t) async {
      final place = MapEntity(
        id: 'place:anchor',
        kind: MapEntityKind.place,
        title: '场地',
        subtitle: '公开',
        latitude: 57.145,
        longitude: -2.1,
      );
      final moments = [
        for (var i = 0; i < 7; i++)
          MapEntity(
            id: 'moment:$i',
            kind: MapEntityKind.moment,
            title: '记录$i',
            subtitle: '公开场地记录',
            latitude: 57.145,
            longitude: -2.1,
          ),
      ];
      final neighbour = MapEntity(
        id: 'place:neighbour',
        kind: MapEntityKind.place,
        title: '另一地点',
        subtitle: '公开',
        latitude: 57.145,
        longitude: -2.099,
      );
      final source = [place, ...moments, neighbour];
      final positions = {
        for (final e in [place, ...moments]) e.id: const Offset(100, 300),
        neighbour.id: const Offset(129.5, 297.8),
      };
      final offsets = MapDisplayPolicy.coincidentPointOffsets(
        source,
        positions: positions,
      );
      final map = _Map();
      map.positions['-2.1:57.145'] = ScreenCoordinate(x: 100, y: 300);
      map.positions['-2.099:57.145'] = ScreenCoordinate(x: 129.5, y: 297.8);
      await t.runAsync(() async {
        for (final offset in offsets.values) {
          await MapMarkerBitmap.render(
            kind: MapEntityKind.moment,
            selected: false,
            dense: true,
            visualOffset: offset,
          );
        }
      });
      await _mount(t, map, source);
      final plan = _display(map, source);
      expect(plan.entities.toSet(), source.toSet());
      expect(plan.entities.any(MapDisplayPolicy.displayCluster), isFalse);
      for (final entity in source) {
        final option = map.points.created[entity.id]!;
        expect(option.geometry.coordinates.lng, entity.longitude);
        expect(option.geometry.coordinates.lat, entity.latitude);
        expect(
          t.getSize(
            find.byKey(ValueKey('native-map-point-semantics:${entity.id}')),
          ),
          const Size(48, 48),
        );
      }
      _tapAt(t, const Offset(100, 300));
      await t.pumpAndSettle();
      final members = find.byKey(const Key('current-map-point-choices'));
      expect(members, findsOneWidget);
      final list = t.widget<ListView>(
        find.descendant(of: members, matching: find.byType(ListView)),
      );
      final keys = (list.childrenDelegate as SliverChildListDelegate).children
          .cast<ListTile>()
          .where((v) => v.key != null)
          .map((v) => v.key)
          .toSet();
      for (final e in [place, ...moments])
        expect(keys, contains(ValueKey('map-point-choice:${e.id}')));
      Navigator.of(t.element(members)).pop();
      await t.pumpAndSettle();
      await _update(t, map, [place, ...moments]);
      expect(
        find.byKey(ValueKey('native-map-point-semantics:${neighbour.id}')),
        findsNothing,
      );
      expect(map.points.created[place.id]!.geometry.coordinates.lng, -2.1);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'every overlapping exact-anchor sibling stays selectable until source retirement',
    (t) async {
      final source = [
        MapEntity(
          id: 'place:anchor',
          kind: MapEntityKind.place,
          title: '地点',
          subtitle: '公开',
          latitude: 57.145,
          longitude: -2.1,
        ),
        for (var i = 0; i < 8; i++)
          MapEntity(
            id: 'moment:$i',
            kind: MapEntityKind.moment,
            title: '记录 $i',
            subtitle: '公开场地记录',
            latitude: 57.145,
            longitude: -2.1,
          ),
      ];
      final map = _Map();
      MapEntity? chosen;
      await _mount(t, map, source, onSelected: (e) => chosen = e);
      expect(_display(map, source).entities.toSet(), source.toSet());
      _tapMarker(t, map, map.points.created[source.first.id]!);
      await t.pumpAndSettle();
      final choices = find.byKey(const Key('current-map-point-choices'));
      expect(choices, findsOneWidget);
      final list = t.widget<ListView>(
        find.descendant(of: choices, matching: find.byType(ListView)),
      );
      final rows = (list.childrenDelegate as SliverChildListDelegate).children
          .cast<ListTile>()
          .where((r) => r.key != null)
          .toList();
      expect(
        rows.map((r) => r.key).toSet(),
        source.map((e) => ValueKey('map-point-choice:${e.id}')).toSet(),
      );
      final oldTap = rows.first.onTap!;
      await _update(t, map, const [], onSelected: (e) => chosen = e);
      await t.pumpAndSettle();
      expect(choices, findsNothing);
      oldTap();
      expect(chosen, isNull);
      expect(map.points.created, isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'DPR 3.25 paints readable separate same-anchor objects without changing coordinates',
    (t) async {
      final place = MapEntity(
        id: 'place:anchor',
        kind: MapEntityKind.place,
        title: '地点',
        subtitle: '公开场地',
        latitude: 57.145,
        longitude: -2.1,
      );
      final moment = MapEntity(
        id: 'moment:record',
        kind: MapEntityKind.moment,
        title: '动态',
        subtitle: '场地记录而非作者位置',
        latitude: 57.145,
        longitude: -2.1,
      );
      final map = _Map();
      await _mount(
        t,
        map,
        [moment, place],
        selected: moment.id,
        pixelRatio: 3.25,
      );
      expect(map.points.iconOverlap, isTrue);
      expect(map.points.textOptional, isTrue);
      expect(map.points.created.length, 2);
      final option = map.points.created[moment.id]!;
      expect(option.geometry.coordinates.lng, -2.1);
      expect(option.geometry.coordinates.lat, 57.145);
      expect(option.iconSize, 1);
      await t.runAsync(() async {
        final codec = await ui.instantiateImageCodec(option.image!);
        final frame = await codec.getNextFrame();
        expect(frame.image.width, 156);
        expect(frame.image.height, 156);
        frame.image.dispose();
        codec.dispose();
      });
      Offset center(String id) =>
          t.getCenter(find.byKey(ValueKey('native-map-point-semantics:$id')));
      final placeCenter = center(place.id);
      final momentCenter = center(moment.id);
      expect(momentCenter - placeCenter, Offset.zero);
      expect(
        t.getSize(
          find.byKey(ValueKey('native-map-point-semantics:${moment.id}')),
        ),
        const Size(48, 48),
      );
      // Withdrawal removes the original record; no stale object or displaced
      // place remains after a current source replacement.
      await _update(t, map, [place]);
      expect(map.points.created.keys, [place.id]);
      expect(
        find.byKey(ValueKey('native-map-point-semantics:${moment.id}')),
        findsNothing,
      );
      expect(center(place.id), placeCenter);
      final sdk = find.byType(MapWidget).evaluate().single;
      await t.runAsync(() async {
        await MapMarkerBitmap.render(
          kind: MapEntityKind.place,
          selected: false,
          devicePixelRatio: 2,
        );
      });
      t.view.physicalSize = const Size(860, 1800);
      t.view.devicePixelRatio = 2;
      await t.pumpAndSettle();
      expect(identical(sdk, find.byType(MapWidget).evaluate().single), isTrue);
      final updated = map.points.updated[place.id]!;
      expect(updated.geometry.coordinates.lng, -2.1);
      expect(updated.geometry.coordinates.lat, 57.145);
      await t.runAsync(() async {
        final codec = await ui.instantiateImageCodec(updated.image!);
        final frame = await codec.getNextFrame();
        expect(frame.image.width, 96);
        expect(frame.image.height, 96);
        frame.image.dispose();
        codec.dispose();
      });
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );

  testWidgets(
    'actual public capture renders nineteen original annotations and overlapping typed choices',
    (t) async {
      final raw =
          jsonDecode(
                File(
                  'test/fixtures/now_v2_public_map_diagnostic.json',
                ).readAsStringSync(),
              )
              as Map;
      final source = [
        for (final item in (raw['items'] as List).cast<Map>())
          MapEntity(
            id: '${(item['kind'] as String).toLowerCase()}:${item['entityID']}',
            kind: item['kind'] == 'PLACE'
                ? MapEntityKind.place
                : MapEntityKind.moment,
            title: item['kind'] == 'PLACE'
                ? '真实回执地点 ${item['entityID']}'
                : '真实回执公开记录',
            subtitle: '公开精确场地',
            latitude: (item['latitude'] as num).toDouble(),
            longitude: (item['longitude'] as num).toDouble(),
            placeID: item['placeID'] as String,
            publicLocation: true,
          ),
      ];
      final map = _Map();
      var ordinal = 0;
      for (final entity in source) {
        map.positions.putIfAbsent('${entity.longitude}:${entity.latitude}', () {
          final i = ordinal++;
          return ScreenCoordinate(x: 30 + (i % 6) * 65, y: 150 + (i ~/ 6) * 65);
        });
      }
      MapEntity? hit;
      await _mount(
        t,
        map,
        source,
        selected: source.singleWhere((e) => e.kind == MapEntityKind.moment).id,
        onSelected: (entity) => hit = entity,
      );
      final options = map.points.created.values;
      expect(options.length, 19);
      expect(
        options.any(
          (option) =>
              (option.customData!['entityId'] as String).startsWith('cluster:'),
        ),
        isFalse,
      );
      final moment = source.singleWhere((e) => e.kind == MapEntityKind.moment);
      final marker = options.singleWhere(
        (option) => option.customData!['entityId'] == moment.id,
      );
      expect(marker.customData!['kind'], 'moment');
      expect(marker.geometry.coordinates.lng, moment.longitude);
      expect(marker.geometry.coordinates.lat, moment.latitude);
      expect(marker.iconAnchor, IconAnchor.CENTER);
      expect(marker.iconSize, 1);
      _tapMarker(t, map, marker);
      // Await both the async SDK projection and the sheet's entrance animation
      // before a physical hit test. The first frame can start that animation.
      await t.pumpAndSettle();
      expect(
        find.byKey(const Key('current-map-point-choices')),
        findsOneWidget,
      );
      final choice = find.byKey(ValueKey('map-point-choice:${moment.id}'));
      final sameAnchor = source.where(
        (entity) =>
            entity.latitude == moment.latitude &&
            entity.longitude == moment.longitude,
      );
      expect(sameAnchor, hasLength(2));
      for (final entity in sameAnchor) {
        expect(
          find.byKey(ValueKey('map-point-choice:${entity.id}')),
          findsOneWidget,
        );
      }
      await t.ensureVisible(choice);
      await t.pumpAndSettle();
      expect(choice.hitTestable(), findsOneWidget);
      await t.tap(choice);
      await t.pump();
      await t.pump(const Duration(milliseconds: 350));
      expect(hit?.id, moment.id);
      expect(hit?.placeID, moment.placeID);
      expect(source.length, 19);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );

  testWidgets(
    'actual native pipeline keeps all 114 coincident points independently reachable',
    (t) async {
      final source = [
        for (var n = 0; n < 114; n++)
          MapEntity(
            id: 'moment:$n',
            kind: MapEntityKind.moment,
            title: '合成动态 $n',
            subtitle: '当前合法来源',
            latitude: 57.145,
            longitude: -2.1,
          ),
      ];
      final map = _Map();
      final semantics = t.ensureSemantics();
      await _mount(t, map, source);
      final plan = _display(map, source);
      expect(
        map.points.created.keys.toSet(),
        plan.entities.map((e) => e.id).toSet(),
      );
      final represented = <String>[];
      for (final entity in plan.entities) {
        expect(MapDisplayPolicy.displayCluster(entity), isFalse);
        represented.add(entity.id);
        final option = map.points.created[entity.id]!;
        expect(
          (option.geometry.coordinates.lng, option.geometry.coordinates.lat),
          (-2.1, 57.145),
        );
        final node = find.byKey(
          ValueKey('native-map-point-semantics:${entity.id}'),
        );
        expect(t.getSize(node), const Size(48, 48));
        expect(t.widget<Semantics>(node).properties.onTap, isNotNull);
      }
      expect(represented, hasLength(114));
      expect(represented.toSet(), source.map((e) => e.id).toSet());
      expect(t.takeException(), isNull);
      semantics.dispose();
      await t.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );

  testWidgets(
    'same-ID explicit focus revision targets visible content while IME rectangle preserves map and markers',
    (t) async {
      final entity = MapEntity(
        id: 'place:known',
        kind: MapEntityKind.place,
        title: '已明确地点',
        subtitle: '测试',
        latitude: 57.145,
        longitude: -2.1,
        placeID: 'known',
        publicLocation: true,
      );
      final map = _Map();
      await _mount(t, map, [entity], selected: entity.id, revision: 0);
      final native = find.byType(NativeCityMapView).evaluate().single;
      final sdk = find.byType(MapWidget).evaluate().single;
      map.flights.clear();
      final projections = map.projectionCalls;
      await _update(
        t,
        map,
        [entity],
        selected: entity.id,
        revision: 0,
        contentRect: const Rect.fromLTRB(0, 130, 430, 450),
      );
      expect(map.flights, isEmpty);
      expect(map.projectionCalls, greaterThan(projections));
      expect(
        identical(native, find.byType(NativeCityMapView).evaluate().single),
        isTrue,
      );
      expect(identical(sdk, find.byType(MapWidget).evaluate().single), isTrue);
      await _update(
        t,
        map,
        [entity],
        selected: entity.id,
        revision: 0,
        contentRect: const Rect.fromLTRB(0, 250, 430, 450),
      );
      expect(
        find.byKey(ValueKey('native-map-point-semantics:${entity.id}')),
        findsNothing,
      );
      expect(
        map.points.created.values.where(
          (v) => v.customData?['entityId'] == entity.id,
        ),
        isEmpty,
      );
      await _update(
        t,
        map,
        [entity],
        selected: entity.id,
        revision: 0,
        contentRect: const Rect.fromLTRB(0, 130, 430, 450),
      );
      expect(
        find.byKey(ValueKey('native-map-point-semantics:${entity.id}')),
        findsOneWidget,
      );
      await _update(t, map, [entity], selected: entity.id, revision: 1);
      final flight = map.flights.single;
      expect(flight.center!.coordinates.lng, entity.longitude);
      expect(flight.center!.coordinates.lat, entity.latitude);
      expect(flight.zoom, 14);
      expect(
        [
          flight.padding!.left,
          flight.padding!.top,
          flight.padding!.right,
          flight.padding!.bottom,
        ],
        [24, 154, 24, 314],
      );
      expect(identical(sdk, find.byType(MapWidget).evaluate().single), isTrue);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  for (final scale in [1.0, 1.3, 2.0]) {
    testWidgets(
      'full-bleed native compass is left 48dp and keeps north-only SDK operation at scale $scale',
      (t) async {
        final map = _Map();
        // Do not request style/tiles. This is the real NativeCityMapView host,
        // with fake SDK transport solely for geometry and operation arguments.
        await _mount(
          t,
          map,
          const [],
          fullBleed: true,
          textScale: scale,
          loadStyle: false,
        );
        final nativeElement = find.byType(NativeCityMapView).evaluate().single;
        final sdkElement = find.byType(MapWidget).evaluate().single;
        final compass = find.byKey(const Key('now-map-compass'));
        expect(compass, findsOneWidget);
        expect(t.getSize(compass), const Size(48, 48));
        final mapRect = t.getRect(find.byType(NativeCityMapView));
        final compassRect = t.getRect(compass);
        expect(compassRect.left - mapRect.left, 16);
        expect(compassRect.right, lessThan(mapRect.center.dx));
        expect(compassRect.top - mapRect.top, 100);
        await t.tap(
          find.descendant(of: compass, matching: find.byType(IconButton)),
        );
        await t.pump();
        expect(map.northEases, hasLength(1));
        final operation = map.northEases.single;
        expect(operation.bearing, 0);
        expect(operation.center, isNull);
        expect(operation.zoom, isNull);
        expect(operation.pitch, isNull);
        expect(operation.padding, isNull);
        expect(map.northAnimationDurations, [300]);
        expect(map.flights, isEmpty);
        expect(
          identical(
            nativeElement,
            find.byType(NativeCityMapView).evaluate().single,
          ),
          isTrue,
        );
        expect(
          identical(sdkElement, find.byType(MapWidget).evaluate().single),
          isTrue,
        );
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
      },
      variant: TargetPlatformVariant.only(TargetPlatform.android),
    );
  }
}

const _source = PublicSource(
  label: '合成测试来源',
  maintainer: '测试',
  freshness: 'test',
  updatedAt: null,
);
final _city = PublicCity(
  id: 'synthetic-city',
  name: '测试城市',
  region: 'test',
  contentStatus: 'test',
  source: _source,
  map: PublicCityMap(
    provider: 'mapbox',
    latitude: 57.145,
    longitude: -2.1,
    defaultZoom: 12,
    sourceRef: 'synthetic://display-unit',
  ),
);

MapDisplayPlan _display(_Map map, List<MapEntity> source, {String? selected}) =>
    MapDisplayPolicy.plan(
      entities: source,
      zoom: 12,
      metersPerPixel: 1,
      selectedID: selected,
      positions: {
        for (final e in source)
          e.id: Offset(
            map.positions['${e.longitude}:${e.latitude}']?.x ?? 100,
            map.positions['${e.longitude}:${e.latitude}']?.y ?? 200,
          ),
      },
      contentRect: const Rect.fromLTRB(0, 130, 430, 610),
    );

Future<void> _mount(
  WidgetTester t,
  _Map map,
  List<MapEntity> entities, {
  String? selected,
  int revision = 0,
  ValueChanged<MapEntity>? onSelected,
  bool fullBleed = false,
  double textScale = 1,
  bool loadStyle = true,
  double pixelRatio = 1,
}) async {
  t.view.physicalSize = Size(430 * pixelRatio, 900 * pixelRatio);
  t.view.devicePixelRatio = pixelRatio;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  const channel =
      'dev.flutter.pigeon.mapbox_maps_flutter._MapboxOptions.setAccessToken';
  t.binding.defaultBinaryMessenger.setMockMessageHandler(
    channel,
    (_) async => const StandardMessageCodec().encodeMessage([null]),
  );
  addTearDown(
    () => t.binding.defaultBinaryMessenger.setMockMessageHandler(channel, null),
  );
  final pending = Completer<Object?>();
  t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
    SystemChannels.platform_views,
    (call) async => call.method == 'create' ? pending.future : null,
  );
  addTearDown(
    () => t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform_views,
      null,
    ),
  );
  await t.runAsync(() async {
    await MapMarkerBitmap.render(kind: MapEntityKind.place, selected: false);
    await MapMarkerBitmap.render(kind: MapEntityKind.place, selected: true);
    final offsets = MapDisplayPolicy.coincidentPointOffsets(entities);
    for (final entity in entities) {
      for (final selected in [false, true]) {
        for (final dense in [false, true]) {
          await MapMarkerBitmap.render(
            kind: entity.kind,
            selected: selected,
            dense: dense,
            approximate: entity.precision == 'area',
            devicePixelRatio: pixelRatio,
            visualOffset: offsets[entity.id] ?? Offset.zero,
          );
          // A current source can remove a coincident peer without a new DPR.
          await MapMarkerBitmap.render(
            kind: entity.kind,
            selected: selected,
            dense: dense,
            approximate: entity.precision == 'area',
            devicePixelRatio: pixelRatio,
          );
        }
      }
    }
    for (final e in _display(
      map,
      entities,
      selected: selected,
    ).entities.where((e) => e.isCluster)) {
      await MapMarkerBitmap.render(
        kind: MapEntityKind.cluster,
        selected: false,
        count: MapMarkerVisual.clusterCount(
          categoryCode: e.categoryCode,
          contentCount: e.contentCount,
          anchorCount: e.anchorCount,
          fallback: e.memberIDs.length,
        ),
        categoryCode: e.categoryCode,
        approximate: true,
        devicePixelRatio: pixelRatio,
      );
    }
    await MapMarkerBitmap.render(
      kind: MapEntityKind.cluster,
      selected: false,
      count: 2,
    );
  });
  await _update(
    t,
    map,
    entities,
    selected: selected,
    revision: revision,
    onSelected: onSelected,
    fullBleed: fullBleed,
    textScale: textScale,
  );
  final widget = t.widget<MapWidget>(find.byType(MapWidget));
  widget.onMapCreated!(map);
  if (loadStyle) {
    widget.onStyleLoadedListener!(
      StyleLoadedEventData.fromJson({
        'timeInterval': {'begin': 0, 'end': 1},
      }),
    );
  }
  await t.pump();
  await t.pump();
}

Future<void> _update(
  WidgetTester t,
  _Map map,
  List<MapEntity> entities, {
  String? selected,
  int revision = 0,
  Rect contentRect = const Rect.fromLTRB(0, 130, 430, 610),
  ValueChanged<MapEntity>? onSelected,
  bool fullBleed = false,
  double textScale = 1,
}) async {
  await t.pumpWidget(
    MaterialApp(
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(
          context,
        ).copyWith(textScaler: TextScaler.linear(textScale)),
        child: child!,
      ),
      home: Scaffold(
        body: SizedBox.expand(
          child: NativeCityMapView(
            key: const ValueKey('v2-native-fixture'),
            city: _city,
            places: const [],
            entities: entities,
            selectedEntityId: selected,
            focusRevision: revision,
            contentRect: contentRect,
            contextKey: 'task-fixture',
            accessToken: 'pk.synthetic_unit_only',
            fullBleed: fullBleed,
            ornamentTop: fullBleed ? 100 : null,
            onPlaceSelected: (_) {},
            onEntitySelected: onSelected,
          ),
        ),
      ),
    ),
  );
  await t.pump();
  await t.pump();
}

PointAnnotation _annotation(PointAnnotationOptions option) => PointAnnotation(
  id: '${option.textSize == 11 ? 'map-aux:' : ''}${option.customData!['entityId']}',
  geometry: option.geometry,
  customData: option.customData,
  textField: option.textField,
  image: option.image,
  iconAnchor: option.iconAnchor,
  iconSize: option.iconSize,
);

void _tapAt(WidgetTester t, Offset position) {
  t.widget<MapWidget>(find.byType(MapWidget)).onTapListener!(
    MapContentGestureContext(
      touchPosition: ScreenCoordinate(x: position.dx, y: position.dy),
      point: Point(coordinates: Position(-2.1, 57.145)),
      gestureState: GestureState.ended,
    ),
  );
}

void _tapMarker(WidgetTester t, _Map map, PointAnnotationOptions option) {
  final coordinate = option.geometry.coordinates;
  final position = map.positions['${coordinate.lng}:${coordinate.lat}'];
  _tapAt(t, Offset(position?.x ?? 100, position?.y ?? 200));
}

class _Cancel extends Fake implements Cancelable {
  @override
  void cancel() {}
}

class _Points extends Fake implements PointAnnotationManager {
  final created = <String, PointAnnotationOptions>{};
  final updated = <String, PointAnnotation>{};
  Function(PointAnnotation)? onTap;
  @override
  String get id => 'synthetic-point-manager';
  bool? iconOverlap, textOptional;
  @override
  Future<void> setIconAllowOverlap(bool value) async {
    iconOverlap = value;
  }

  @override
  Future<void> setIconIgnorePlacement(bool value) async {}
  @override
  Future<void> setTextAllowOverlap(bool value) async {}
  @override
  Future<void> setTextIgnorePlacement(bool value) async {}

  @override
  Future<void> setTextOptional(bool value) async {
    textOptional = value;
  }

  @override
  Cancelable tapEvents({required Function(PointAnnotation) onTap}) {
    this.onTap = onTap;
    return _Cancel();
  }

  @override
  Future<PointAnnotation> create(PointAnnotationOptions option) async {
    created[_annotation(option).id] = option;
    return _annotation(option);
  }

  @override
  Future<void> update(PointAnnotation annotation) async {
    updated[annotation.id] = annotation;
  }

  @override
  Future<void> delete(PointAnnotation annotation) async {
    created.remove(annotation.id);
  }
}

class _Annotations extends Fake implements AnnotationManager {
  _Annotations(this.points);
  final _Points points;
  @override
  Future<PointAnnotationManager> createPointAnnotationManager({
    String? id,
    String? below,
  }) async => points;
}

class _Map extends Fake implements MapboxMap {
  final points = _Points();
  final positions = <String, ScreenCoordinate>{};
  final flights = <CameraOptions>[];
  final northEases = <CameraOptions>[];
  final northAnimationDurations = <int?>[];
  int projectionCalls = 0;
  Completer<CameraState>? cameraGate;
  Completer<List<ScreenCoordinate?>>? projectionGate;
  @override
  AnnotationManager get annotations => _Annotations(points);
  @override
  Future<CameraState> getCameraState() async {
    if (cameraGate != null) return await cameraGate!.future;
    return CameraState(
      center: Point(coordinates: Position(-2.1, 57.145)),
      zoom: 12,
      bearing: 0,
      pitch: 0,
      padding: MbxEdgeInsets(top: 0, left: 0, right: 0, bottom: 0),
    );
  }

  @override
  Future<List<ScreenCoordinate?>> pixelsForCoordinates(
    List<Point> coordinates,
  ) async {
    projectionCalls++;
    if (projectionGate != null) return projectionGate!.future;
    return [
      for (final point in coordinates)
        positions['${point.coordinates.lng}:${point.coordinates.lat}'] ??
            ScreenCoordinate(x: 100, y: 200),
    ];
  }

  @override
  Future<List<Point?>> coordinatesForPixels(
    List<ScreenCoordinate?> pixels,
  ) async => [
    for (final p in pixels.whereType<ScreenCoordinate>())
      Point(coordinates: Position(-2.1 + p.x / 60000, 57.145 - p.y / 111000)),
  ];

  @override
  Future<void> flyTo(
    CameraOptions camera,
    MapAnimationOptions? animation,
  ) async {
    flights.add(camera);
  }

  @override
  Future<void> easeTo(
    CameraOptions camera,
    MapAnimationOptions? animation,
  ) async {
    northEases.add(camera);
    northAnimationDurations.add(animation?.duration);
  }

  @override
  Future<CameraOptions> cameraForCoordinateBounds(
    CoordinateBounds bounds,
    MbxEdgeInsets padding,
    double? bearing,
    double? pitch,
    double? maxZoom,
    ScreenCoordinate? offset,
  ) async =>
      CameraOptions(center: bounds.southwest, zoom: 12, padding: padding);
}
