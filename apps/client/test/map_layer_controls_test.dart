import 'package:birdtie_client/src/workspace/map_layer_controls.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'now_recording_content_hierarchy_test.dart' show recordingMapItem;

void main() {
  testWidgets(
    'recording layer list explains group once and retains each entity navigation',
    (t) async {
      final transport = MockClient((_) async => http.Response('{}', 503));
      final c = MapLayersController(
        api: MapLayersApi(client: transport, apiBaseUrl: 'https://unit.test'),
        authorizationHeader: () => null,
        organizationWorkspaceID: () => null,
      );
      final rows = [for (var n = 1; n <= 3; n++) recordingMapItem('MOMENT', n)];
      final now = DateTime.now().toUtc();
      c.publicView = TypedMapView(
        cityID: 'synthetic-city',
        scope: 'PUBLIC',
        observedAt: now,
        validUntil: now.add(const Duration(minutes: 1)),
        truncated: false,
        items: rows,
      );
      final opened = <TypedMapItem>[];
      var current = true;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MapLayerControls(
              controller: c,
              current: () => current,
              onRefresh: () {},
              onOpen: opened.add,
            ),
          ),
        ),
      );
      final note = find.text('公开记录来自本人明确公开的地点情境，不表示作者当前位置。');
      await t.ensureVisible(note);
      await t.pumpAndSettle();
      expect(note, findsOneWidget);
      for (final row in rows) {
        final tile = find.byKey(ValueKey('map-layer-card-${row.mapID}'));
        await t.ensureVisible(tile);
        await t.pumpAndSettle();
        expect(
          find.descendant(of: tile, matching: find.text(row.rowSubtitle)),
          findsOneWidget,
        );
        expect(t.getSize(tile).height, greaterThanOrEqualTo(48));
      }
      final last = find.byKey(ValueKey('map-layer-card-${rows.last.mapID}'));
      expect(
        t
            .widget<Text>(
              find.descendant(of: last, matching: find.text(rows.last.title)),
            )
            .style!
            .fontWeight,
        FontWeight.w500,
      );
      await t.tap(last);
      await t.pumpAndSettle();
      expect(opened.single, same(rows.last));
      final stale = t.widget<ListTile>(last).onTap!;
      current = false;
      c.retire();
      await t.pumpAndSettle();
      stale();
      expect(opened, hasLength(1));
      expect(find.text('账号或情境已变化，请关闭后重新打开。'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      transport.close();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    'recording layer headings and last row survive 360dp large type',
    (t) async {
      t.view.physicalSize = const Size(360, 800);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final transport = MockClient((_) async => http.Response('{}', 503));
      final c = MapLayersController(
        api: MapLayersApi(client: transport, apiBaseUrl: 'https://unit.test'),
        authorizationHeader: () => null,
        organizationWorkspaceID: () => null,
      );
      final now = DateTime.now().toUtc();
      final row = recordingMapItem('PLACE', 1, repeatedName: true);
      c.publicView = TypedMapView(
        cityID: 'synthetic-city',
        scope: 'PUBLIC',
        observedAt: now,
        validUntil: now.add(const Duration(minutes: 1)),
        truncated: false,
        items: [row],
      );
      var opens = 0;
      final openedIDs = <String>[];
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(3)),
            child: child!,
          ),
          home: Scaffold(
            body: MapLayerControls(
              controller: c,
              current: () => true,
              onRefresh: () {},
              onOpen: (item) {
                opens++;
                openedIDs.add(item.detailID);
              },
            ),
          ),
        ),
      );
      expect(
        t.widget<Text>(find.text('地图图层')).style!.fontSize,
        NowUiTokens.of(t.element(find.text('地图图层'))).pageTitle.fontSize,
      );
      final tile = find.byKey(ValueKey('map-layer-card-${row.mapID}'));
      // Large text places this row beyond ListView's lazy cache. Scroll the
      // real viewport until mounted, then retain the original click/ID checks.
      await t.scrollUntilVisible(
        tile,
        160,
        scrollable: find.byType(Scrollable).first,
        maxScrolls: 30,
      );
      await t.pumpAndSettle();
      expect(tile.hitTestable(), findsOneWidget);
      await t.tap(tile);
      await t.pumpAndSettle();
      expect(opens, 1);
      expect(openedIDs, [row.detailID]);
      expect(openedIDs.single, row.id);
      expect(
        find.descendant(of: tile, matching: find.text('地点')),
        findsOneWidget,
      );
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      transport.close();
    },
  );
}
