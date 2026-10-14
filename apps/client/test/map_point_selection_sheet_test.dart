import 'package:birdtie_client/src/city/map_point_selection_sheet.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'map_media_rendering_test.dart' show mapFixtureImage, fixturePixels;
import 'map_media_controller_test.dart' show mediaEntity;

MapEntity point(String id, MapEntityKind kind, {String? title}) => MapEntity(
  id: id,
  kind: kind,
  title: title ?? id,
  subtitle: '当前合法来源',
  latitude: 57.2,
  longitude: -2.1,
);

class Fixture {
  final rows = ValueNotifier([
    point('place:p', MapEntityKind.place),
    point('moment:m', MapEntityKind.moment),
  ]);
  int scope = 0;
  MapEntity? selected;
  final visibility = <({Object token, bool visible})>[];
  Future<void> open(WidgetTester t, {double scale = 1}) async {
    await t.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: MediaQueryData(
            size: const Size(320, 640),
            textScaler: TextScaler.linear(scale),
          ),
          child: Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                child: const Text('选择重叠对象'),
                onPressed: () async {
                  selected = await showCurrentMapPointChoices(
                    context: context,
                    changes: rows,
                    currentCandidates: () => rows.value,
                    currentScope: () => scope,
                    onVisibilityChanged: visibility.add,
                  );
                },
              ),
            ),
          ),
        ),
      ),
    );
    await t.tap(find.text('选择重叠对象'));
    await t.pumpAndSettle();
  }

  Future<void> close(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pump();
    rows.dispose();
  }
}

void main() {
  testWidgets(
    'same-coordinate public Moment chooser square photo retires independently and returns original ID',
    (t) async {
      final image = (await t.runAsync(mapFixtureImage))!;
      var live = true;
      final pixels = (await t.runAsync(
        () => fixturePixels(image, () => live, kind: MapEntityKind.moment),
      ))!;
      final f = Fixture(),
          moment = point('moment:m', MapEntityKind.moment),
          place = point('place:p', MapEntityKind.place);
      f.rows.value = [place, moment.withMediaPixels(pixels)];
      await f.open(t);
      final picture = find.byKey(
        const ValueKey('map-point-choice-photo:moment:m'),
      );
      expect(picture, findsOneWidget);
      expect(
        t
            .widget<ClipRRect>(
              find.ancestor(of: picture, matching: find.byType(ClipRRect)),
            )
            .borderRadius,
        BorderRadius.circular(4),
      );
      final callback = t
          .widget<ListTile>(
            find.byKey(const ValueKey('map-point-choice:moment:m')),
          )
          .onTap!;
      live = false;
      f.rows.value = [place, moment];
      await t.pump();
      expect(picture, findsNothing);
      callback();
      await t.pumpAndSettle();
      expect(f.selected!.id, moment.id);
      expect(f.selected!.latitude, moment.latitude);
      expect(f.selected!.longitude, moment.longitude);
      await f.close(t);
      image.dispose();
    },
  );
  testWidgets(
    'current chooser photo expiry keeps same legal row and selection callback',
    (t) async {
      final image = (await t.runAsync(mapFixtureImage))!;
      var live = true;
      final pixels = (await t.runAsync(
        () => fixturePixels(image, () => live),
      ))!;
      final f = Fixture(), base = mediaEntity(1);
      f.rows.value = [
        base.withMediaPixels(pixels),
        point('moment:m', MapEntityKind.moment),
      ];
      await f.open(t);
      expect(
        find.byKey(ValueKey('map-point-choice-photo:${base.id}')),
        findsOneWidget,
      );
      final callback = t
          .widget<ListTile>(find.byKey(ValueKey('map-point-choice:${base.id}')))
          .onTap!;
      live = false;
      f.rows.value = [base, point('moment:m', MapEntityKind.moment)];
      await t.pump();
      expect(
        find.byKey(ValueKey('map-point-choice-photo:${base.id}')),
        findsNothing,
      );
      expect(
        find.byKey(const Key('current-map-point-choices')),
        findsOneWidget,
      );
      callback();
      await t.pumpAndSettle();
      expect(f.selected!.id, base.id);
      await f.close(t);
      image.dispose();
    },
  );
  testWidgets(
    'coincident identities stay individually reachable with refreshed rows and large type',
    (t) async {
      final f = Fixture();
      await f.open(t, scale: 2);
      final semantics = t.ensureSemantics();
      expect(find.textContaining('地点'), findsOneWidget);
      expect(find.textContaining('动态'), findsOneWidget);
      f.rows.value = [
        point('place:p', MapEntityKind.place),
        point('moment:m', MapEntityKind.moment, title: '最新合法动态 café'),
      ];
      await t.pumpAndSettle();
      final tile = find.byKey(const ValueKey('map-point-choice:moment:m'));
      expect(t.getSize(tile).height, greaterThanOrEqualTo(48));
      await t.tap(tile);
      await t.pumpAndSettle();
      expect(
        (f.selected?.id, f.selected?.kind),
        ('moment:m', MapEntityKind.moment),
      );
      expect(f.selected?.title, '最新合法动态 café');
      expect(f.visibility.map((e) => e.visible), [true, false]);
      expect(
        identical(f.visibility.first.token, f.visibility.last.token),
        isTrue,
      );
      expect(t.takeException(), isNull);
      semantics.dispose();
      await f.close(t);
    },
  );
  testWidgets(
    'withdrawn old tile callback cannot select, surviving place remains reachable',
    (t) async {
      final f = Fixture();
      await f.open(t);
      final old = t
          .widget<ListTile>(
            find.byKey(const ValueKey('map-point-choice:moment:m')),
          )
          .onTap!;
      f.rows.value = [point('place:p', MapEntityKind.place)];
      await t.pumpAndSettle();
      old();
      await t.pumpAndSettle();
      expect(f.selected, isNull);
      expect(
        find.byKey(const Key('current-map-point-choices')),
        findsOneWidget,
      );
      await t.tap(find.byKey(const ValueKey('map-point-choice:place:p')));
      await t.pumpAndSettle();
      expect(f.selected?.id, 'place:p');
      await f.close(t);
    },
  );
  testWidgets('scope A B A permanently retires chooser before stale callback', (
    t,
  ) async {
    final f = Fixture();
    await f.open(t);
    final old = t
        .widget<ListTile>(
          find.byKey(const ValueKey('map-point-choice:moment:m')),
        )
        .onTap!;
    f.scope = 1;
    f.rows.value = [point('place:p', MapEntityKind.place)];
    f.scope = 0;
    f.rows.value = [
      point('place:p', MapEntityKind.place),
      point('moment:m', MapEntityKind.moment),
    ];
    old();
    await t.pumpAndSettle();
    expect(f.selected, isNull);
    expect(find.byKey(const Key('current-map-point-choices')), findsNothing);
    await f.close(t);
  });
}
