import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/external_place_candidates.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'external_place_candidates_test.dart' show externalSearchWire;

void main() {
  testWidgets(
    'full height place detail respects system insets at 320dp and 200 percent',
    (t) async {
      t.view.devicePixelRatio = 1;
      t.view.physicalSize = const Size(320, 697);
      t.view.padding = const FakeViewPadding(top: 36, bottom: 20);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetPadding);
      final now = DateTime.now().toUtc();
      final raw = externalSearchWire(now);
      final item = (raw['places'] as List).single as Map<String, dynamic>;
      item['name'] = "Frankie & Benny's";
      item['addressLabel'] = '很长的中文地址 Guild Street AB11 5RG Aberdeen';
      final search = ExternalPlaceSearch.read(
        raw,
        requestID: 'request-one',
        cityID: 'alpha',
        query: '有没有好的餐厅',
        current: () => true,
        now: () => now,
      )!;
      final changes = ChangeNotifier();
      addTearDown(changes.dispose);
      await t.pumpWidget(
        MaterialApp(
          theme: birdtieTheme(Brightness.dark),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: TextScaler.linear(2)),
            child: child!,
          ),
          home: Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                onPressed: () => showExternalPlaceDetails(
                  context,
                  search: search,
                  place: search.places.single,
                  current: () => true,
                  changes: changes,
                ),
                child: const Text('打开地点详情'),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('打开地点详情'));
      await t.pumpAndSettle();
      final close = find.byTooltip('关闭地点详情');
      expect(t.getTopLeft(close).dy, greaterThanOrEqualTo(36));
      expect(t.getSize(close).height, greaterThanOrEqualTo(48));
      expect(
        t.getTopLeft(find.text(item['name'])).dy,
        greaterThanOrEqualTo(36),
      );
      final source = find.widgetWithText(TextButton, '打开原始来源');
      await t.ensureVisible(source);
      await t.pump();
      expect(t.getBottomLeft(source).dy, lessThanOrEqualTo(677));
      expect(t.getSize(source).height, greaterThanOrEqualTo(48));
      await t.ensureVisible(close);
      await t.pump();
      await t.tap(close);
      await t.pumpAndSettle();
      expect(close, findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox.shrink());
    },
  );

  for (final dark in [false, true]) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets(
        '320dp long Chinese mixed title and card actions remain usable $dark/$scale',
        (t) async {
          t.view.devicePixelRatio = 1;
          t.view.physicalSize = const Size(320, 800);
          addTearDown(t.view.resetDevicePixelRatio);
          addTearDown(t.view.resetPhysicalSize);
          final now = DateTime.now().toUtc();
          final raw = externalSearchWire(now);
          final item = (raw['places'] as List).single as Map<String, dynamic>;
          item['name'] = "一家名字较长且可以完整阅读的餐厅 Aberdeen Bistro & Restaurant";
          item['addressLabel'] = '很长的中文地址与英文街道 Union Square AB11 5RG Aberdeen';
          final search = ExternalPlaceSearch.read(
            raw,
            requestID: 'request-one',
            cityID: 'alpha',
            query: '有没有好的餐厅',
            current: () => true,
            now: () => now,
          )!;
          final changes = ChangeNotifier();
          addTearDown(changes.dispose);
          var mapCalls = 0;
          await t.pumpWidget(
            MaterialApp(
              theme: birdtieTheme(dark ? Brightness.dark : Brightness.light),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(
                  context,
                ).copyWith(textScaler: TextScaler.linear(scale)),
                child: child!,
              ),
              home: Scaffold(
                body: SingleChildScrollView(
                  child: ExternalPlaceCandidatesPanel(
                    search: search,
                    current: () => true,
                    changes: changes,
                    onMap: (_) => mapCalls++,
                  ),
                ),
              ),
            ),
          );
          expect(find.text(item['name']), findsOneWidget);
          final title = t.widget<Text>(find.text(item['name']));
          expect(title.maxLines, isNull);
          expect(title.overflow, isNull);
          final map = find.widgetWithText(TextButton, '地图查看');
          await t.ensureVisible(map);
          await t.pump();
          expect(t.getSize(map).height, greaterThanOrEqualTo(48));
          await t.tap(map);
          await t.pump();
          expect(mapCalls, 1);
          final details = find.widgetWithText(TextButton, '查看详情');
          await t.ensureVisible(details);
          await t.pump();
          expect(t.getSize(details).height, greaterThanOrEqualTo(48));
          await t.tap(details);
          await t.pumpAndSettle();
          expect(find.byTooltip('关闭地点详情'), findsOneWidget);
          expect(find.text('餐厅 · OpenStreetMap'), findsOneWidget);
          expect(find.text('评分、营业时间和服务信息尚未核实，请查看原始来源。'), findsOneWidget);
          final source = find.widgetWithText(TextButton, '打开原始来源');
          await t.ensureVisible(source);
          await t.pump();
          expect(t.getSize(source).height, greaterThanOrEqualTo(48));
          final close = find.byTooltip('关闭地点详情');
          await t.ensureVisible(close);
          await t.pump();
          await t.tap(close);
          await t.pumpAndSettle();
          expect(find.byTooltip('关闭地点详情'), findsNothing);
          expect(mapCalls, 1);
          expect(t.takeException(), isNull);
          await t.pumpWidget(const SizedBox.shrink());
        },
      );
    }
  }
}
