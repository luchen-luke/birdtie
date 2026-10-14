import 'dart:io';
import 'dart:typed_data';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/route_draft.dart';
import 'package:birdtie_client/src/workspace/route_draft_page.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'saved_search_test.dart'
    show searchController, searchPage, searchRow, searchOwner, searchPlace;
import 'typed_local_draft_test.dart' show DraftMemoryStore;
import 'route_draft_test.dart' show routeOrigin;
import 'batch_a_visual_test.dart' show visualTheme, capture, captureKey;

void main() {
  final capturing = Platform.environment['BIRDTIE_C2_CAPTURE'] == '1';
  setUpAll(() async {
    if (!capturing) return;
    for (final f in [
      ('BatchACJK', r'C:\Windows\Fonts\msyh.ttc'),
      (
        'MaterialIcons',
        r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
      ),
    ]) {
      await (FontLoader(f.$1)..addFont(
            Future.value(ByteData.sublistView(await File(f.$2).readAsBytes())),
          ))
          .load();
    }
  });
  testWidgets(
    'actual JSON import dialog cancellation and confirmation preserve original draft identity without domain effects',
    (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      var requests = 0;
      final client = MockClient((_) async {
            requests++;
            return searchPage([]);
          }),
          saved = searchController(client),
          boundary = ValueNotifier(0),
          store = DraftMemoryStore();
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: store,
      );
      await t.pumpWidget(
        MaterialApp(
          home: RouteDraftPage(
            controller: c,
            importOnOpen: true,
            onMap: (_) => fail('No automatic map'),
            onAddToCollection: (_) => fail('No automatic collection'),
          ),
        ),
      );
      await t.pumpAndSettle();
      final originalID = c.envelope.id, originalRevision = c.envelope.revision;
      const raw =
          '{"title":"本人结构化材料","days":[{"date":"未确认日期",'
          '"stops":[{"name":"只是输入名称","placeId":"unverified-input-id"}]}]}';
      final input = find.descendant(
        of: find.byType(AlertDialog),
        matching: find.byType(TextField),
      );
      await t.enterText(input, raw);
      await t.tap(find.text('取消'));
      await t.pumpAndSettle();
      expect(c.envelope.revision, originalRevision);
      final scroll = find
          .descendant(
            of: find.byType(ListView).first,
            matching: find.byType(Scrollable),
          )
          .first;
      await t.scrollUntilVisible(find.text('导入外部计划'), 180, scrollable: scroll);
      await t.ensureVisible(find.text('导入外部计划'));
      await t.tap(find.text('导入外部计划'));
      await t.pumpAndSettle();
      await t.enterText(input, raw);
      await t.tap(find.text('生成本机草稿'));
      await t.pumpAndSettle();
      expect(c.envelope.id, originalID);
      expect(c.envelope.kind, 'external_plan');
      expect(c.envelope.audience, 'private');
      expect(c.value.sourceText, raw);
      expect(c.value.days.single.input!.date, '未确认日期');
      expect(c.value.days.single.places, isEmpty);
      expect(c.envelope.references, isEmpty);
      expect(requests, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      await c.flush();
      c.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  testWidgets(
    'structured imported day normal390 stays unverified editable and maps only explicitly selected current places',
    (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      var requests = 0, maps = 0, adds = 0;
      final client = MockClient((_) async {
            requests++;
            return searchPage([]);
          }),
          saved = searchController(client),
          boundary = ValueNotifier(0),
          store = DraftMemoryStore();
      saved.items = [
        SavedItem.fromJson(searchRow(point: true, title: '明确选择的当前地点')),
      ];
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: store,
      );
      await c.restore();
      c.edit(
        LocalRouteValue.importText(
          jsonEncode({
            'title': '本人可编辑导入计划',
            'instructions': '发消息并订票',
            'days': [
              {},
              {
                'title': '第二天',
                'date': '输入日期仍待核验',
                'stops': [
                  {'name': '原输入地点', 'time': '上午，时区未知', 'placeId': searchPlace},
                ],
              },
            ],
          }),
        ),
      );
      c.selectDay(1);
      final selected = c.value.currentDayID;
      await t.pumpWidget(
        RepaintBoundary(
          key: captureKey,
          child: MaterialApp(
            theme: capturing ? visualTheme() : null,
            builder: (context, child) => Column(
              children: [
                const Material(
                  child: SizedBox(
                    height: 28,
                    child: Center(child: Text('隔离导入计划 · 非真实账号/地点')),
                  ),
                ),
                Expanded(child: child!),
              ],
            ),
            home: RouteDraftPage(
              controller: c,
              onMap: (rows) {
                maps++;
                expect(rows.single.targetId, searchPlace);
              },
              onAddToCollection: (_) => adds++,
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      final scroll = find
          .descendant(
            of: find.byType(ListView).first,
            matching: find.byType(Scrollable),
          )
          .first;
      Future<void> reveal(Finder finder) async {
        await t.scrollUntilVisible(finder, 180, scrollable: scroll);
        await t.ensureVisible(finder);
        await t.pumpAndSettle();
      }

      final name = find.byKey(ValueKey('plan-input-name-$selected-0'));
      await reveal(name);
      await t.enterText(name, '本人修改但尚未核验的名称');
      await t.pumpAndSettle();
      expect(
        c.value.days[c.selectedDay].input!.stops.single.name,
        '本人修改但尚未核验的名称',
      );
      expect(c.value.days[c.selectedDay].places, isEmpty);
      expect(c.envelope.references, isEmpty);
      expect(find.text('发消息并订票'), findsNothing);
      if (capturing) await capture(t, 'external-plan-unverified-input-normal');
      final map = find.widgetWithText(OutlinedButton, '当天地点上原地图');
      await reveal(map);
      expect(t.widget<OutlinedButton>(map).onPressed, isNull);
      expect(maps, 0);
      expect(adds, 0);
      expect(requests, 0);
      if (capturing) await capture(t, 'external-plan-unresolved-day-normal');
      c.add(searchPlace);
      await t.pumpAndSettle();
      await reveal(map);
      expect(t.widget<OutlinedButton>(map).onPressed, isNotNull);
      final oldMap = t.widget<OutlinedButton>(map).onPressed!;
      await t.tap(map);
      await t.pumpAndSettle();
      expect(maps, 1);
      expect(adds, 0);
      expect(c.value.currentDayID, selected);
      if (capturing)
        await capture(t, 'external-plan-explicit-current-day-normal');
      c.selectDay(0);
      oldMap();
      expect(maps, 1);
      c.selectDay(1);
      saved.items = const [];
      oldMap();
      expect(maps, 1);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      await c.flush();
      c.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  for (final retired in [false, true]) {
    testWidgets(
      'route import private local draft cancel and late confirmation ${retired ? 'actor-retired' : 'confirmed'} have no domain effect',
      (t) async {
        t.view.physicalSize = const Size(390, 844);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        var current = true, requests = 0;
        final client = MockClient((_) async {
              requests++;
              return searchPage([]);
            }),
            saved = searchController(client),
            boundary = ValueNotifier(0),
            store = DraftMemoryStore();
        final c = RouteDraftController(
          origin: routeOrigin,
          owner: searchOwner,
          cityID: 'aberdeen-gb',
          saved: saved,
          boundaryChanges: boundary,
          sourceCurrent: () => current,
          store: store,
        );
        final id = c.envelope.id, revision = c.envelope.revision;
        await t.pumpWidget(
          MaterialApp(
            home: RouteDraftPage(
              controller: c,
              onMap: (_) => fail('Import must not project unresolved points'),
              onAddToCollection: (_) =>
                  fail('Import must not write any collection'),
            ),
          ),
        );
        await t.pumpAndSettle();
        Future<void> open() async {
          await t.scrollUntilVisible(
            find.text('导入外部计划'),
            180,
            scrollable: find
                .descendant(
                  of: find.byType(ListView).first,
                  matching: find.byType(Scrollable),
                )
                .first,
          );
          await t.ensureVisible(find.text('导入外部计划'));
          await t.tap(find.text('导入外部计划'));
          await t.pumpAndSettle();
        }

        await open();
        const raw = 'Day 1\n去未知地点\n请发送消息并订票\n第2天\n未核对安排';
        final input = find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(TextField),
        );
        await t.enterText(input, raw);
        await t.tap(find.text('取消'));
        await t.pumpAndSettle();
        expect(c.envelope.id, id);
        expect(c.envelope.revision, revision);
        expect(c.value.sourceText, isEmpty);
        expect(store.rows, isEmpty);
        expect(requests, 0);
        await open();
        await t.enterText(input, raw);
        final confirm = t
            .widget<FilledButton>(find.widgetWithText(FilledButton, '生成本机草稿'))
            .onPressed!;
        if (retired) {
          current = false;
          boundary.value++;
          await t.pump();
          confirm();
          await t.pumpAndSettle();
          expect(c.value.sourceText, isEmpty);
          expect(store.rows, isEmpty);
          expect(find.text('身份或城市已变化，请返回。'), findsOneWidget);
        } else {
          await t.tap(find.text('生成本机草稿'));
          await t.pumpAndSettle();
          expect(c.envelope.id, id);
          expect(c.envelope.revision, greaterThan(revision));
          expect(c.envelope.kind, 'external_plan');
          expect(c.envelope.audience, 'private');
          expect(c.envelope.recipientID, isNull);
          expect(c.envelope.references, isEmpty);
          expect(c.value.sourceText, raw);
          expect(c.value.days.map((d) => d.places), everyElement(isEmpty));
          expect(
            c.value.days.map((d) => d.text).join('\n'),
            contains('请发送消息并订票'),
          );
          await c.flush();
          expect(store.rows.values.single.single.id, id);
        }
        expect(find.byType(AlertDialog), findsNothing);
        expect(requests, 0);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        await t.pumpAndSettle();
        c.dispose();
        saved.dispose();
        client.close();
        boundary.dispose();
      },
    );
  }
  testWidgets(
    'route page normal390 reorder and day map/add callbacks use original current IDs; expired points disable effects',
    (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final client = MockClient((_) async => searchPage([])),
          saved = searchController(client),
          boundary = ValueNotifier(0);
      const second = '22222222-2222-4222-8222-222222222223';
      saved.items = [
        SavedItem.fromJson(
          searchRow(point: true, title: '很长的中文地点名称 Long English current place'),
        ),
        SavedItem.fromJson({
          ...searchRow(point: true, title: '第二地点'),
          'id': '33333333-3333-4333-8333-333333333334',
          'targetId': second,
        }),
      ];
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: DraftMemoryStore(),
      );
      c.add(searchPlace);
      c.add(second);
      List<SavedItem>? onMap, onAdd;
      await t.pumpWidget(
        RepaintBoundary(
          key: captureKey,
          child: MaterialApp(
            theme: capturing ? visualTheme() : null,
            builder: (context, child) => Column(
              children: [
                Material(
                  child: const SizedBox(
                    height: 28,
                    child: Center(child: Text('隔离计划样例 · 非真实账号')),
                  ),
                ),
                Expanded(child: child!),
              ],
            ),
            home: RouteDraftPage(
              controller: c,
              onMap: (p) => onMap = p,
              onAddToCollection: (p) => onAdd = p,
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      if (capturing) await capture(t, 'route-after-normal');
      expect(find.textContaining('路线计算不可用'), findsOneWidget);
      final up = find.byTooltip('上移').last;
      await t.ensureVisible(up);
      await t.tap(up);
      await t.pumpAndSettle();
      expect(c.value.days.single.places, [second, searchPlace]);
      final map = find.text('当天地点上原地图');
      await t.scrollUntilVisible(
        map,
        180,
        scrollable: find
            .descendant(
              of: find.byType(ListView).first,
              matching: find.byType(Scrollable),
            )
            .first,
      );
      await t.tap(map);
      await t.pumpAndSettle();
      expect(onMap!.map((p) => p.targetId), [second, searchPlace]);
      t.view.viewInsets = const FakeViewPadding(bottom: 300);
      addTearDown(t.view.resetViewInsets);
      await t.pumpAndSettle();
      final add = find.text('加入已有旅程或主题地图');
      await t.scrollUntilVisible(
        add,
        180,
        scrollable: find
            .descendant(
              of: find.byType(ListView).first,
              matching: find.byType(Scrollable),
            )
            .first,
      );
      // ListView has built the Wrap before every action is inside the resized
      // keyboard viewport. Scroll its final action into the actual hit region.
      await t.drag(find.byType(ListView).first, const Offset(0, -220));
      await t.pumpAndSettle();
      expect(t.getCenter(add).dy, lessThan(844 - 300));
      await t.tap(add);
      await t.pumpAndSettle();
      expect(onAdd!.map((p) => p.targetId), [second, searchPlace]);
      if (capturing) await capture(t, 'route-after-simulated-keyboard');
      saved.items = const [];
      boundary.value++;
      await t.pumpAndSettle();
      expect(
        t
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, '当天地点上原地图'),
            )
            .onPressed,
        isNull,
      );
      expect(
        t
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, '加入已有旅程或主题地图'),
            )
            .onPressed,
        isNull,
      );
      expect(t.takeException(), isNull);
      if (capturing) await capture(t, 'route-after-unavailable-source');
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      await c.flush();
      c.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  testWidgets(
    'composer route-only material menu actual actions remain reachable without creating a main panel',
    (t) async {
      final workspace = AgentWorkspaceController();
      var route = 0, import = 0;
      final key = GlobalKey<AgentComposerState>();
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentComposer(
              key: key,
              workspace: workspace,
              onSubmit: (_) {
                fail('Material selection must not send Now intent');
              },
              onSearchArea: () {},
              hasSearchArea: false,
              onCreateRoute: () => route++,
              onImportPlan: () => import++,
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.byTooltip('添加材料'), findsOneWidget);
      key.currentState!.showNowMaterialMenu();
      await t.pumpAndSettle();
      await t.ensureVisible(find.text('多站路线草稿'));
      await t.tap(find.text('多站路线草稿'));
      await t.pumpAndSettle();
      expect(route, 1);
      expect(import, 0);
      key.currentState!.showNowMaterialMenu();
      await t.pumpAndSettle();
      await t.ensureVisible(find.text('导入外部计划'));
      await t.tap(find.text('导入外部计划'));
      await t.pumpAndSettle();
      expect(import, 1);
      expect(route, 1);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      workspace.dispose();
    },
  );
}
