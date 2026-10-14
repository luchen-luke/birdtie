import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:latlong2/latlong.dart';

// Actual FlutterMap/MarkerLayer and the existing current-object chooser. This
// local widget suite has no provider/network or device acceptance claim.
List<MapEntity> anchorRows() => [
  const MapEntity(
    id: 'place:anchor',
    kind: MapEntityKind.place,
    title: '公开场地',
    subtitle: '当前合法来源',
    latitude: 57.145,
    longitude: -2.1,
  ),
  for (var i = 0; i < 114; i++)
    MapEntity(
      id: 'moment:${i.toString().padLeft(3, '0')}',
      kind: MapEntityKind.moment,
      title: '动态$i · Current public record',
      subtitle: '当前合法来源',
      latitude: 57.145,
      longitude: -2.1,
    ),
];

void main() {
  for (final scale in [1.0, 2.0]) {
    testWidgets(
      '115 overlapping originals stay centered and the last original is selectable at scale $scale',
      (t) async {
        final rows = ValueNotifier(anchorRows());
        MapEntity? selected;
        final semantics = t.ensureSemantics();
        try {
          await t.pumpWidget(
            MaterialApp(
              home: MediaQuery(
                data: MediaQueryData(
                  size: const Size(320, 740),
                  textScaler: TextScaler.linear(scale),
                ),
                child: Scaffold(
                  body: FlutterMap(
                    options: const MapOptions(
                      initialCenter: LatLng(57.145, -2.1),
                      initialZoom: 14,
                    ),
                    children: [
                      WebMapDisplayLayer(
                        places: const [],
                        entities: rows.value,
                        selectedID: null,
                        contentRect: null,
                        revision: 1,
                        readerChanges: rows,
                        currentReaderEntities: () => rows.value,
                        currentReaderScope: () => 'same-current-owner/source',
                        onSelected: (entity) => selected = entity,
                      ),
                    ],
                  ),
                ),
              ),
            ),
          );
          await t.pumpAndSettle();
          final layer = t.widget<MarkerLayer>(find.byType(MarkerLayer));
          expect(layer.markers, hasLength(115));
          expect(
            layer.markers.map((m) => m.key).toSet(),
            rows.value.map((e) => ValueKey(e.id)).toSet(),
          );
          expect(
            layer.markers.every(
              (m) =>
                  m.point == const LatLng(57.145, -2.1) &&
                  m.alignment == Alignment.center &&
                  m.width == 48 &&
                  m.height == 48,
            ),
            isTrue,
          );
          expect(
            find.byKey(const ValueKey('map-point-leader:moment:113')),
            findsNothing,
          );
          final actual = t.getCenter(
            find.byKey(const ValueKey('individual-map-target:place:anchor')),
          );
          for (final e in rows.value) {
            final target = find.byKey(
              ValueKey('individual-map-target:${e.id}'),
            );
            expect(t.getCenter(target), actual);
            expect(t.getSize(target), const Size(48, 48));
            final shape =
                t
                        .widget<Container>(
                          find.byKey(ValueKey('individual-map-shape:${e.id}')),
                        )
                        .decoration!
                    as BoxDecoration;
            expect(
              shape.shape,
              e.kind == MapEntityKind.place
                  ? BoxShape.circle
                  : BoxShape.rectangle,
            );
          }
          await t.tapAt(actual);
          await t.pumpAndSettle();
          final chooser = find.byKey(const Key('current-map-point-choices'));
          expect(chooser, findsOneWidget);
          final list = t.widget<ListView>(
            find.descendant(of: chooser, matching: find.byType(ListView)),
          );
          final entries = (list.childrenDelegate as SliverChildListDelegate)
              .children
              .cast<ListTile>();
          expect(
            entries.map((e) => e.key).toSet(),
            rows.value.map((e) => ValueKey('map-point-choice:${e.id}')).toSet(),
          );
          final last = find.byKey(
            const ValueKey('map-point-choice:moment:113'),
          );
          await t.scrollUntilVisible(
            last,
            240,
            maxScrolls: 150,
            scrollable: find
                .descendant(of: chooser, matching: find.byType(Scrollable))
                .first,
          );
          await t.ensureVisible(last);
          expect(t.getSize(last).height, greaterThanOrEqualTo(48));
          await t.tap(last);
          await t.pumpAndSettle();
          expect(selected?.id, 'moment:113');
          expect(selected?.longitude, -2.1);
          expect(selected?.latitude, 57.145);
          expect(chooser, findsNothing);
          expect(t.takeException(), isNull);
        } finally {
          await t.pumpWidget(const SizedBox());
          semantics.dispose();
          rows.dispose();
        }
      },
    );
  }
}
