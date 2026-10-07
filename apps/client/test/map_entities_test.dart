import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';

void main() {
  testWidgets(
    'six typed markers have accessible distinct semantics original IDs',
    (t) async {
      for (final kind in [
        MapEntityKind.place,
        MapEntityKind.activity,
        MapEntityKind.moment,
        MapEntityKind.organization,
        MapEntityKind.business,
        MapEntityKind.opportunity,
      ]) {
        final entity = MapEntity(
          id: '${kind.name}:original-id',
          kind: kind,
          title: '合成标题',
          subtitle: kind == MapEntityKind.opportunity ? '仅你可见' : '明确公开点位',
          latitude: 57.2,
          longitude: -2.1,
        );
        var clicks = 0;
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: SizedBox(
                width: 142,
                height: 58,
                child: SelectedMarkerState(
                  entity: entity,
                  icon: Icons.place_outlined,
                  selected: true,
                  onTap: () {
                    clicks++;
                  },
                ),
              ),
            ),
          ),
        );
        final semantics = t.ensureSemantics();
        expect(
          find.bySemanticsLabel(RegExp(RegExp.escape(entity.subtitle))),
          findsOneWidget,
        );
        await t.tap(find.byType(InkWell));
        expect(clicks, 1);
        expect(t.takeException(), null);
        semantics.dispose();
      }
    },
  );
  test(
    'cluster membership stable typed originals without exposing a Person point',
    () {
      final a = MapEntity(
        id: 'moment:original',
        kind: MapEntityKind.moment,
        title: '公开记录',
        subtitle: '地点情境',
        latitude: 57.2,
        longitude: -2.1,
      );
      final b = MapEntity(
        id: 'business:original',
        kind: MapEntityKind.business,
        title: '场地',
        subtitle: '经营场地',
        latitude: 57.2,
        longitude: -2.1,
      );
      final x = clusterMapEntities([a, b], zoom: 10),
          y = clusterMapEntities([b, a], zoom: 10);
      expect(x.single.id, y.single.id);
      expect(x.single.memberIDs, ['business:original', 'moment:original']);
      expect(
        clusterMapEntities([a, b], zoom: 10, selectedId: a.id).map((i) => i.id),
        contains(a.id),
      );
    },
  );
}
