import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/external_place_candidates.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'now_scope_recovery_test.dart'
    show NowFixture, NowFixtureCity, NowFixtureSource, nowTap;

const longTitle = '阿伯丁大学城的中文长标题与 Aberdeen Restaurant 餐厅资料';
const longAddress = '苏格兰阿伯丁旧城大学附近的一条很长的中文地址 Guild Street AB11 5RG';

class _LongCity extends NowFixtureCity {
  static const entries = [
    PublicCity(
      id: 'alpha',
      name: '阿伯丁大学城 Aberdeen',
      region: '苏格兰 Scotland · 城市目录说明',
      contentStatus: 'test',
      map: null,
      source: PublicSource(
        label: '夹具',
        maintainer: 'test',
        freshness: 'test',
        updatedAt: null,
      ),
    ),
    PublicCity(
      id: 'beta',
      name: '长中文城市名称的另一座测试城市 Edinburgh',
      region: '中英文混排的地区说明 Scotland · UK',
      contentStatus: 'test',
      map: null,
      source: PublicSource(
        label: '夹具',
        maintainer: 'test',
        freshness: 'test',
        updatedAt: null,
      ),
    ),
  ];
  PublicCity choice = entries.first;
  @override
  List<PublicCity> get cities => entries;
  @override
  PublicCity get selectedCity => choice;
  @override
  void selectCity(String id, {bool rememberExploration = true}) {
    choice = entries.singleWhere((c) => c.id == id);
    notifyListeners();
  }
}

void main() {
  for (final brightness in Brightness.values) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets(
        'card long mixed text $brightness scale $scale wraps and actions work',
        (t) async {
          t.view.physicalSize = const Size(320, 780);
          t.view.devicePixelRatio = 1;
          addTearDown(t.view.resetPhysicalSize);
          addTearDown(t.view.resetDevicePixelRatio);
          final now = DateTime.utc(2026, 10, 10);
          final candidate = ExternalPlaceCandidate(
            id: 'osm:node:123',
            cityID: 'alpha',
            name: longTitle,
            addressLabel: longAddress,
            latitude: 57.1,
            longitude: -2.1,
            sourceUri: Uri.parse('https://www.openstreetmap.org/node/123'),
            observedAt: now,
            expiresAt: now.add(const Duration(days: 1)),
          );
          var maps = 0, details = 0;
          await t.pumpWidget(
            MaterialApp(
              theme: birdtieTheme(brightness),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(
                  context,
                ).copyWith(textScaler: TextScaler.linear(scale)),
                child: child!,
              ),
              home: Scaffold(
                body: SingleChildScrollView(
                  padding: const EdgeInsets.all(16),
                  child: ExternalPlaceCandidateCard(
                    place: candidate,
                    onOpen: () => details++,
                    onMap: () => maps++,
                  ),
                ),
              ),
            ),
          );
          await t.pumpAndSettle();
          final title = t.widget<Text>(find.text(longTitle));
          expect(title.maxLines, isNull);
          expect(title.overflow, isNull);
          expect(title.style!.fontSize, 16);
          expect(title.style!.fontWeight, FontWeight.w500);
          final cardMaterial = find
              .descendant(
                of: find.byType(ExternalPlaceCandidateCard),
                matching: find.byType(Material),
              )
              .first;
          expect(
            t.widget<Material>(cardMaterial).color,
            birdtieTheme(brightness).colorScheme.surface,
          );
          for (final label in ['地图查看', '查看详情']) {
            final button = find.widgetWithText(TextButton, label);
            await t.ensureVisible(button);
            await t.pumpAndSettle();
            expect(button.hitTestable(), findsOneWidget);
            expect(t.getSize(button).width, greaterThanOrEqualTo(48));
            expect(t.getSize(button).height, greaterThanOrEqualTo(48));
            await t.tap(button);
            await t.pump();
          }
          expect(maps, 1);
          expect(details, 1);
          expect(t.takeException(), isNull);
        },
      );

      testWidgets(
        'real city picker narrow mixed text $brightness scale $scale preserves map',
        (t) async {
          t.view.physicalSize = const Size(320, 780);
          t.view.devicePixelRatio = 1;
          addTearDown(t.view.resetPhysicalSize);
          addTearDown(t.view.resetDevicePixelRatio);
          final f = NowFixture();
          final city = _LongCity();
          final source = NowFixtureSource(city);
          addTearDown(f.dispose);
          addTearDown(city.dispose);
          await t.pumpWidget(
            MaterialApp(
              theme: birdtieTheme(brightness),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(
                  context,
                ).copyWith(textScaler: TextScaler.linear(scale)),
                child: child!,
              ),
              home: MapWorkspace(
                city: city,
                auth: f.auth,
                moments: f.moments,
                agentTaskSource: source,
                seedClient: f.client,
                seedApiBaseUrl: f.base,
              ),
            ),
          );
          await t.pumpAndSettle();
          final map = find.byType(MapCanvas).evaluate().single;
          expect(find.byType(AgentComposer), findsOneWidget);
          final chrome = t.widget<Text>(
            find.descendant(
              of: find.byType(TopControls),
              matching: find.text(_LongCity.entries.first.name),
            ),
          );
          expect(chrome.maxLines, isNull);
          expect(chrome.overflow, isNull);
          await nowTap(t, find.byKey(const Key('now-city-picker')));
          final row = find.byKey(const Key('now-scope-city-beta'));
          await t.ensureVisible(row);
          await t.pumpAndSettle();
          expect(row.hitTestable(), findsOneWidget);
          expect(t.getSize(row).height, greaterThanOrEqualTo(48));
          expect(
            t
                .widget<Text>(
                  find.descendant(
                    of: row,
                    matching: find.text(_LongCity.entries.last.name),
                  ),
                )
                .maxLines,
            isNull,
          );
          await t.tap(row);
          await t.pumpAndSettle();
          expect(city.selectedCity.id, 'beta');
          expect(find.byType(MapCanvas).evaluate().single, same(map));
          expect(find.byType(AgentComposer), findsOneWidget);
          expect(t.takeException(), isNull);
          await f.unmount(t);
        },
      );
    }
  }
}
