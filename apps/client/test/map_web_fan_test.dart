import 'dart:math' as math;

import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/city/mapbox_web_basemap.dart';
import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:latlong2/latlong.dart';

MapEntity _point(
  String id, {
  MapEntityKind kind = MapEntityKind.moment,
  LatLng coordinate = const LatLng(57.145, -2.1),
  String precision = 'point',
}) => MapEntity(
  id: id,
  kind: kind,
  title: '当前公开 $id',
  subtitle: '已公开地点情境',
  latitude: coordinate.latitude,
  longitude: coordinate.longitude,
  placeID: 'anchor',
  precision: precision,
);

List<MapEntity> _source([int count = 7]) => [
  _point('place:anchor', kind: MapEntityKind.place),
  for (var i = 0; i < count; i++)
    _point('moment:${i.toString().padLeft(3, '0')}'),
];

class _Fixture {
  _Fixture([int count = 7]) : rows = ValueNotifier(_source(count));
  final ValueNotifier<List<MapEntity>> rows;
  final scope = ValueNotifier(0), revision = ValueNotifier(0);
  final controller = MapController();
  late final changes = Listenable.merge([rows, scope, revision]);
  String? selectedID;
  final selected = <MapEntity>[];
  final anchors = <({String? id, Offset? point})>[];
  final visibility = <({Object token, bool visible})>[];

  Map<String, Offset> get positions => {
    for (final entity in rows.value)
      entity.id: () {
        final p = controller.camera.latLngToScreenPoint(
          LatLng(entity.latitude, entity.longitude),
        );
        return Offset(p.x, p.y);
      }(),
  };

  Finder shape(String id) => find.byKey(ValueKey('individual-map-shape:$id'));
  Finder target(String id) => find.byKey(ValueKey('individual-map-target:$id'));
  final chooser = find.byKey(const Key('current-map-point-choices'));

  Future<void> mount(WidgetTester t, {bool basemap = false}) async {
    t.view.physicalSize = const Size(600, 800);
    t.view.devicePixelRatio = 1;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AnimatedBuilder(
            animation: changes,
            builder: (_, __) => FlutterMap(
              mapController: controller,
              options: const MapOptions(
                initialCenter: LatLng(57.145, -2.1),
                initialZoom: 14,
              ),
              children: [
                if (basemap)
                  Builder(
                    builder: (mapContext) {
                      final camera = MapCamera.of(mapContext);
                      return MapboxWebBasemap(
                        accessToken: 'pk.synthetic-public-test',
                        camera: MapboxWebCamera(
                          latitude: camera.center.latitude,
                          longitude: camera.center.longitude,
                          zoom: camera.zoom,
                          rotation: camera.rotation,
                        ),
                      );
                    },
                  ),
                WebMapDisplayLayer(
                  places: const [],
                  entities: rows.value,
                  selectedID: selectedID,
                  contentRect: null,
                  revision: revision.value,
                  onSelected: selected.add,
                  readerChanges: changes,
                  currentReaderEntities: () => rows.value,
                  currentReaderScope: () => scope.value,
                  onSelectedAnchor: anchors.add,
                  onMapReaderVisibilityChanged: visibility.add,
                ),
              ],
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  Future<void> open(WidgetTester t, String id) async {
    // A real screen tap can hit an adjacent 48 dp target. The shared chooser
    // must still contain every same-anchor object, including the visible one.
    await t.tapAt(t.getCenter(shape(id)));
    await t.pumpAndSettle();
    expect(chooser, findsOneWidget);
  }

  Future<void> choose(WidgetTester t, String id) async {
    final row = find.byKey(ValueKey('map-point-choice:$id'));
    await t.scrollUntilVisible(
      row,
      220,
      scrollable: find.descendant(
        of: chooser,
        matching: find.byType(Scrollable),
      ),
      maxScrolls: 80,
    );
    // ensureVisible may jump to a cached row without pumping its new layout.
    // Wait for that real scroll frame before deriving the tap coordinates.
    await t.pumpAndSettle();
    expect(row.hitTestable(), findsOneWidget);
    final previousSelections = selected.length;
    await t.tap(row);
    await t.pumpAndSettle();
    expect(selected, hasLength(previousSelections + 1));
  }

  Future<void> close(WidgetTester t) async {
    await t.pumpWidget(const SizedBox.shrink());
    await t.pump();
    controller.dispose();
    rows.dispose();
    scope.dispose();
    revision.dispose();
    t.view.resetPhysicalSize();
    t.view.resetDevicePixelRatio();
  }
}

void _expectDisplayedProjection(WidgetTester t, _Fixture f) {
  final projected = f.positions;
  final expected = MapDisplayPolicy.coincidentPointOffsets(
    f.rows.value,
    positions: projected,
  );
  final origin = t.getTopLeft(find.byType(FlutterMap));
  for (final entity in f.rows.value) {
    final actual = t.getCenter(f.shape(entity.id)) - origin;
    expect(
      (actual - projected[entity.id]! - (expected[entity.id] ?? Offset.zero))
          .distance,
      lessThan(1.1),
      reason: entity.id,
    );
    expect(t.getSize(f.target(entity.id)), const Size(48, 48));
  }
  final markers = t.widget<MarkerLayer>(find.byType(MarkerLayer)).markers;
  for (final entity in f.rows.value) {
    final marker = markers.singleWhere((m) => m.key == ValueKey(entity.id));
    expect(marker.point, LatLng(entity.latitude, entity.longitude));
  }
}

void main() {
  testWidgets(
    'GL basemap camera follows the existing overlay across move and rotation',
    (t) async {
      final f = _Fixture();
      try {
        await f.mount(t, basemap: true);
        _expectDisplayedProjection(t, f);
        final originalMap = find.byType(FlutterMap).evaluate().single;
        f.controller.move(const LatLng(57.146, -2.099), 15.25);
        f.controller.rotate(37);
        await t.pumpAndSettle();
        final basemap = t.widget<MapboxWebBasemap>(
          find.byType(MapboxWebBasemap),
        );
        expect(basemap.camera.latitude, f.controller.camera.center.latitude);
        expect(basemap.camera.longitude, f.controller.camera.center.longitude);
        expect(basemap.camera.glZoom, f.controller.camera.zoom - 1);
        expect(basemap.camera.glBearing, -f.controller.camera.rotation);
        expect(find.byType(FlutterMap).evaluate().single, same(originalMap));
        _expectDisplayedProjection(t, f);
        await f.open(t, 'moment:000');
        final oldChoice = t
            .widget<ListTile>(
              find.byKey(const ValueKey('map-point-choice:moment:000')),
            )
            .onTap!;
        f.rows.value = f.rows.value.where((e) => e.id != 'moment:000').toList();
        oldChoice();
        await t.pumpAndSettle();
        expect(f.selected, isEmpty);
        expect(f.shape('moment:000'), findsNothing);
        expect(t.takeException(), isNull);
      } finally {
        await f.close(t);
      }
    },
  );

  testWidgets(
    'Web real viewport overlaps seven moments at the true anchor and preserves shapes, IDs and selection',
    (t) async {
      final f = _Fixture();
      try {
        await f.mount(t);
        _expectDisplayedProjection(t, f);
        final centers = f.rows.value
            .map((e) => t.getCenter(f.shape(e.id)))
            .toSet();
        expect(centers, hasLength(1));
        for (final entity in f.rows.value) {
          final box =
              t.widget<Container>(f.shape(entity.id)).decoration!
                  as BoxDecoration;
          expect(
            box.shape,
            entity.kind == MapEntityKind.place
                ? BoxShape.circle
                : BoxShape.rectangle,
          );
          expect(box.color!.a, 1);
        }
        final markers = t.widget<MarkerLayer>(find.byType(MarkerLayer)).markers;
        expect(markers.where((m) => m.child is IgnorePointer), isEmpty);
        expect(markers.every((m) => m.alignment == Alignment.center), isTrue);
        f.selectedID = 'moment:003';
        f.revision.value++;
        await t.pumpAndSettle();
        expect(f.anchors.last.id, f.selectedID);
        expect(
          (f.anchors.last.point! -
                  (t.getCenter(f.shape(f.selectedID!)) -
                      t.getTopLeft(find.byType(FlutterMap))))
              .distance,
          lessThan(1.1),
        );
        expect(t.takeException(), isNull);
      } finally {
        await f.close(t);
      }
    },
  );

  testWidgets(
    'Web real anchors and overlap taps use current rotated viewport projection',
    (t) async {
      final f = _Fixture();
      try {
        await f.mount(t);
        final anchor = f.positions['place:anchor']!;
        final neighbour = f.controller.camera.pointToLatLng(
          math.Point(anchor.dx + 29.5, anchor.dy - 2.2),
        );
        f.rows.value = [
          ...f.rows.value,
          _point(
            'place:neighbour',
            kind: MapEntityKind.place,
            coordinate: neighbour,
          ),
        ];
        await t.pumpAndSettle();
        var offsets = t
            .widget<MapEntityLayer>(find.byType(MapEntityLayer))
            .visualOffsets;
        expect(
          f.rows.value
              .where((e) => e.kind == MapEntityKind.moment)
              .map((e) => offsets[e.id] ?? Offset.zero),
          isNot(contains(const Offset(28, 0))),
        );
        _expectDisplayedProjection(t, f);
        final mapElement = find.byType(FlutterMap).evaluate().single;
        f.controller.rotate(37);
        f.revision.value++;
        await t.pumpAndSettle();
        _expectDisplayedProjection(t, f);
        f.controller.move(const LatLng(57.1453, -2.1005), 14);
        f.revision.value++;
        await t.pumpAndSettle();
        _expectDisplayedProjection(t, f);
        expect(find.byType(FlutterMap).evaluate().single, same(mapElement));
        await f.open(t, 'moment:000');
        await f.choose(t, 'moment:006');
        expect(f.selected.single.id, 'moment:006');
        f.rows.value = f.rows.value
            .where((e) => e.id != 'place:neighbour')
            .toList();
        await t.pumpAndSettle();
        offsets = t
            .widget<MapEntityLayer>(find.byType(MapEntityLayer))
            .visualOffsets;
        expect(offsets['moment:000'] ?? Offset.zero, Offset.zero);
        _expectDisplayedProjection(t, f);
        expect(t.takeException(), isNull);
      } finally {
        await f.close(t);
      }
    },
  );

  testWidgets(
    'every overlapping PLACE and MOMENT reaches its exact current object through real taps',
    (t) async {
      final f = _Fixture();
      try {
        await f.mount(t);
        for (final entity in f.rows.value) {
          await f.open(t, entity.id);
          await f.choose(t, entity.id);
          expect(
            (f.selected.last.id, f.selected.last.kind),
            (entity.id, entity.kind),
          );
          expect(f.selected.last, same(entity));
        }
        expect(f.selected, hasLength(8));
        expect(f.visibility.map((e) => e.visible), [
          for (var i = 0; i < 8; i++) ...[true, false],
        ]);
        expect(t.takeException(), isNull);
      } finally {
        await f.close(t);
      }
    },
  );

  testWidgets(
    'Web withdrawn marker and stale chooser callbacks cannot reopen data',
    (t) async {
      final f = _Fixture();
      try {
        await f.mount(t);
        final staleMarker = t
            .widget<IconButton>(f.target('moment:000'))
            .onPressed!;
        await f.open(t, 'moment:000');
        final staleRow = t
            .widget<ListTile>(
              find.byKey(const ValueKey('map-point-choice:moment:000')),
            )
            .onTap!;
        f.rows.value = f.rows.value.where((e) => e.id != 'moment:000').toList();
        await t.pumpAndSettle();
        staleRow();
        await t.pumpAndSettle();
        expect(f.selected, isEmpty);
        expect(f.shape('moment:000'), findsNothing);
        f.rows.value = [];
        await t.pumpAndSettle();
        staleMarker();
        staleRow();
        await t.pumpAndSettle();
        expect(f.selected, isEmpty);
        expect(f.chooser, findsNothing);
        expect(t.takeException(), isNull);
      } finally {
        await f.close(t);
      }
    },
  );

  testWidgets(
    'Web identity A B A retires the open chooser even when IDs reappear',
    (t) async {
      final f = _Fixture();
      try {
        await f.mount(t);
        await f.open(t, 'place:anchor');
        final stale = t
            .widget<ListTile>(
              find.byKey(const ValueKey('map-point-choice:moment:000')),
            )
            .onTap!;
        f.scope.value = 1;
        f.scope.value = 0;
        stale();
        await t.pumpAndSettle();
        expect(f.selected, isEmpty);
        expect(f.chooser, findsNothing);
        expect(t.takeException(), isNull);
      } finally {
        await f.close(t);
      }
    },
  );

  testWidgets(
    'Web same-anchor overlap keeps all 114 moments selectable and preserves area precision',
    (t) async {
      final f = _Fixture(114);
      try {
        f.rows.value = [
          ...f.rows.value,
          _point('moment:area', precision: 'area'),
        ];
        await f.mount(t);
        final layer = t.widget<MapEntityLayer>(find.byType(MapEntityLayer));
        expect(layer.entities, hasLength(116));
        expect(layer.visualOffsets.containsKey('moment:area'), isFalse);
        expect(layer.visualOffsets, isEmpty);
        expect(
          MapDisplayPolicy.coincidentPointOffsets(f.rows.value).values.toSet(),
          {Offset.zero},
        );
        final markers = t.widget<MarkerLayer>(find.byType(MarkerLayer)).markers;
        expect(markers.where((m) => m.child is! IgnorePointer), hasLength(116));
        await f.open(t, 'moment:113');
        await f.choose(t, 'moment:113');
        expect(f.selected.single.id, 'moment:113');
        expect(f.selected.single.precision, 'point');
        expect(t.takeException(), isNull);
      } finally {
        await f.close(t);
      }
    },
  );
}
