import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/city/amap_city_map_stub.dart';
import 'package:birdtie_client/src/city/map_point_selection_sheet.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';

MapEntity _point(
  String id, {
  MapEntityKind kind = MapEntityKind.moment,
  String precision = 'point',
  String title = '当前合法对象',
}) => MapEntity(
  id: id,
  kind: kind,
  title: title,
  subtitle: '原公开场地',
  latitude: 31.2,
  longitude: 121.4,
  precision: precision,
  placeID: 'anchor',
);
List<MapEntity> _rows([int count = 114]) => [
  _point('place:anchor', kind: MapEntityKind.place),
  for (var i = 0; i < count; i++)
    _point('moment:${i.toString().padLeft(3, '0')}'),
];
String _frame(AMapSourceGate gate, int frame, {double dx = 0, int? revision}) =>
    jsonEncode({
      'schema': 'birdtie-amap-frame-v1',
      'revision': revision ?? gate.revision,
      'frame': frame,
      'zoom': 14,
      'positions': [
        for (final e in gate.rows) {'id': e.id, 'x': 280.0 + dx, 'y': 400.0},
      ],
    });

void main() {
  test('重新挂载退休旧投影和点击，重新接受同来源地图的第一帧', () {
    final gate = AMapSourceGate();
    gate.replace(_rows(1), 'same-actor');
    final old = gate.revision;
    expect(gate.acceptFrame(_frame(gate, 500)), isNotNull);
    gate.retireProjection();
    expect(gate.replace(_rows(1), 'same-actor'), isFalse);
    expect(gate.resolve(old, 'moment:000', 'moment'), isNull);
    expect(gate.acceptFrame(_frame(gate, 501, revision: old)), isNull);
    expect(gate.acceptFrame(_frame(gate, 1)), isNotNull);
    expect(
      gate.resolve(gate.revision, 'moment:000', 'moment')?.id,
      'moment:000',
    );
    gate.dispose();
  });
  test(
    'AMap actual projected pixels retain 115 original IDs, zero layout offsets and area precision',
    () {
      final gate = AMapSourceGate();
      gate.replace([
        ..._rows(),
        _point('person:area', kind: MapEntityKind.person, precision: 'area'),
      ], 'actor-a');
      final frame = gate.acceptFrame(_frame(gate, 1))!;
      expect(frame.plan.entities, hasLength(116));
      expect(frame.plan.omittedIDs, isEmpty);
      expect(frame.offsets['place:anchor'], Offset.zero);
      expect(frame.offsets['person:area'], isNull);
      expect(frame.offsets.values.toSet(), {Offset.zero});
      expect(frame.displayed, frame.positions);
      expect(
        (frame.layout(null)['markers'] as List).every(
          (m) => m['dx'] == 0 && m['dy'] == 0,
        ),
        isTrue,
      );
      expect(
        frame.candidates('moment:113').map((e) => e.id).toSet(),
        containsAll(_rows().map((e) => e.id)),
      );
      for (final row in frame.rows) {
        expect((row.latitude, row.longitude), (31.2, 121.4));
        expect(gate.resolve(gate.revision, row.id, row.kind.name)?.id, row.id);
      }
      final moved = gate.acceptFrame(_frame(gate, 2, dx: 137.5))!;
      expect(
        moved.displayed['moment:113']! - frame.displayed['moment:113']!,
        const Offset(137.5, 0),
      );
      expect(moved.offsets, frame.offsets);
      expect(gate.revision, 1);
      gate.dispose();
    },
  );
  test(
    'AMap source/identity ABA and same-ID metadata changes permanently retire old click and frame',
    () {
      final gate = AMapSourceGate();
      gate.replace(_rows(1), ('actor', 1));
      final old = gate.revision;
      expect(gate.replace(_rows(1).reversed.toList(), ('actor', 1)), isFalse);
      gate.replace(const [], ('actor', 1));
      gate.replace(_rows(1), ('actor', 1));
      expect(gate.resolve(old, 'moment:000', 'moment'), isNull);
      expect(gate.acceptFrame(_frame(gate, 1, revision: old)), isNull);
      final restored = gate.revision;
      gate.replace(_rows(1), ('actor', 2));
      gate.replace(_rows(1), ('actor', 1));
      expect(gate.resolve(restored, 'moment:000', 'moment'), isNull);
      final oldMetadata = gate.revision;
      gate.replace([_point('moment:000', title: '最新合法标题')], ('actor', 1));
      expect(gate.resolve(oldMetadata, 'moment:000', 'moment'), isNull);
      expect(gate.resolve(gate.revision, 'moment:000', 'place'), isNull);
      gate.dispose();
      expect(gate.rows, isEmpty);
      expect(gate.resolve(gate.revision, 'moment:000', 'moment'), isNull);
    },
  );
  test(
    'AMap bridge frames reject unknown, duplicate, missing IDs, duplicate frame and malformed shape',
    () {
      final gate = AMapSourceGate();
      gate.replace(_rows(1), 'a');
      expect(gate.acceptFrame(_frame(gate, 1)), isNotNull);
      expect(gate.acceptFrame(_frame(gate, 1)), isNull);
      for (final positions in [
        [
          {'id': 'unknown', 'x': 1, 'y': 1},
        ],
        [
          {'id': 'place:anchor', 'x': 1, 'y': 1},
        ],
        [
          {'id': 'place:anchor', 'x': 1, 'y': 1},
          {'id': 'place:anchor', 'x': 1, 'y': 1},
        ],
      ]) {
        expect(
          gate.acceptFrame(
            jsonEncode({
              'schema': 'birdtie-amap-frame-v1',
              'revision': gate.revision,
              'frame': 2,
              'zoom': 14,
              'positions': positions,
            }),
          ),
          isNull,
        );
      }
      expect(gate.acceptFrame('{}'), isNull);
      expect(gate.acceptFrame(_frame(gate, 2)), isNotNull);
    },
  );
  testWidgets(
    'AMap complete original-ID chooser reaches the 114th moment with large text',
    (t) async {
      final gate = AMapSourceGate();
      gate.replace(_rows(), 'actor-a');
      final frame = gate.acceptFrame(_frame(gate, 1))!;
      final changes = ValueNotifier(0);
      MapEntity? selected;
      await t.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(
              size: Size(320, 640),
              textScaler: TextScaler.linear(2),
            ),
            child: Scaffold(
              body: Builder(
                builder: (context) => TextButton(
                  child: const Text('选择同坐标对象'),
                  onPressed: () async {
                    selected = await showCurrentMapPointChoices(
                      context: context,
                      changes: changes,
                      currentCandidates: () => frame.candidates('place:anchor'),
                      currentScope: () => gate.revision,
                    );
                  },
                ),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('选择同坐标对象'));
      await t.pumpAndSettle();
      final last = find.byKey(const ValueKey('map-point-choice:moment:113'));
      await t.scrollUntilVisible(
        last,
        220,
        maxScrolls: 150,
        scrollable: find.descendant(
          of: find.byKey(const Key('current-map-point-choices')),
          matching: find.byType(Scrollable),
        ),
      );
      await t.pumpAndSettle();
      expect(t.getSize(last).height, greaterThanOrEqualTo(48));
      await t.tap(last);
      await t.pumpAndSettle();
      expect(selected?.id, 'moment:113');
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      changes.dispose();
      gate.dispose();
    },
  );
  testWidgets(
    'AMap withdrawn and restored same-ID source cannot revive the old chooser tile',
    (t) async {
      final gate = AMapSourceGate();
      gate.replace(_rows(1), 'actor-a');
      final changes = ValueNotifier(0);
      MapEntity? selected;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                child: const Text('选择对象'),
                onPressed: () async {
                  selected = await showCurrentMapPointChoices(
                    context: context,
                    changes: changes,
                    currentCandidates: () => gate.rows,
                    currentScope: () => gate.revision,
                  );
                },
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('选择对象'));
      await t.pumpAndSettle();
      final old = t
          .widget<ListTile>(
            find.byKey(const ValueKey('map-point-choice:moment:000')),
          )
          .onTap!;
      gate.replace(const [], 'actor-b');
      changes.value++;
      gate.replace(_rows(1), 'actor-a');
      changes.value++;
      old();
      await t.pumpAndSettle();
      expect(selected, isNull);
      expect(find.byKey(const Key('current-map-point-choices')), findsNothing);
      await t.pumpWidget(const SizedBox());
      changes.dispose();
      gate.dispose();
    },
  );
}
