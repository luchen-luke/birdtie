import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/city/browse_destination.dart';
import 'package:birdtie_client/src/city/destination_search_service.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart' show nowField;
import 'content_map_controller_test.dart' as owned;

class _Search implements DestinationSearchService {
  final queries = <String>[];
  bool fail = false;
  @override
  Future<DestinationSearchResult> search(
    String query, {
    Future<void>? abortTrigger,
  }) async {
    queries.add(query);
    if (fail) throw const DestinationSearchFailure('目的地搜索失败，请重试。');
    return const DestinationSearchResult(
      destinations: [
        BrowseDestination(
          provider: 'mapbox',
          externalID: 'fixture-paris',
          name: '巴黎',
          countryCode: 'FR',
          region: '法兰西岛 · 法国',
          latitude: 48.8566,
          longitude: 2.3522,
          featureType: 'place',
          attribution: 'Mapbox fixture',
          storageMode: DestinationStorageMode.temporary,
        ),
      ],
    );
  }

  @override
  void dispose() {}
}

void main() {
  testWidgets(
    'external destination selected from actual Now picker preserves text and reads bounds without city',
    (t) async {
      final auth = SeedTestAuth()..owner = owned.id(9000);
      final search = _Search(), requests = <Uri>[];
      final fixture = owned.Fixture()..count = 0;
      final client = MockClient((r) async {
        requests.add(r.url);
        if (r.url.path == '/v1/me/map-content') {
          fixture.now = DateTime.now().toUtc();
          fixture.owner = auth.accountID!;
          return http.Response(
            jsonEncode({'data': fixture.page(r.url)}),
            200,
            headers: {
              'content-type': 'application/json',
              'cache-control': 'no-store',
            },
          );
        }
        return http.Response(
          '{"data":[]}',
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      final city = PublicCityController(
        client: client,
        apiBaseUrl: owned.base,
        loadUnrequestedPlaces: false,
      );
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: owned.base,
      );
      try {
        t.view.physicalSize = const Size(390, 844);
        t.view.devicePixelRatio = 1;
        t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
        await t.pumpWidget(
          MaterialApp(
            home: MapWorkspace(
              city: city,
              auth: auth,
              moments: moments,
              seedClient: client,
              seedApiBaseUrl: owned.base,
              destinationSearchService: search,
            ),
          ),
        );
        await t.pumpAndSettle();
        await t.enterText(nowField(), '仍然保留的未发布正文');
        await t.tap(find.byKey(const Key('now-city-picker')));
        await t.pumpAndSettle();
        expect(
          find.descendant(
            of: find.byKey(const Key('now-scope-picker')),
            matching: find.text('选择目的地'),
          ),
          findsOneWidget,
        );
        await t.tap(find.widgetWithText(ChoiceChip, '国内'));
        await t.enterText(find.byKey(const Key('now-city-search')), 'Paris');
        await t.pump(const Duration(milliseconds: 350));
        await t.pumpAndSettle();
        expect(
          search.queries,
          ['Paris'],
          reason: 'catalog filter is not a global-search restriction',
        );
        await t.tap(
          find.byKey(
            const Key('now-destination-destination:mapbox:fixture-paris'),
          ),
        );
        await t.pumpAndSettle();
        expect(city.selectedCity, isNull);
        expect(city.contentCityID, '');
        expect(city.selectedDestination!.name, '巴黎');
        expect(t.widget<TextField>(nowField()).controller!.text, '仍然保留的未发布正文');
        final map = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
        expect(map.city!.name, '巴黎');
        map.onContentViewportSettled!(owned.bounds, 13);
        await t.pumpAndSettle();
        final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
        await owned.drain(t, () => !canvas.mapContent!.loading);
        expect(canvas.mapContent!.current, isTrue);
        expect(canvas.mapContent!.items, isEmpty);
        expect(
          requests
              .where((r) => r.path == '/v1/me/map-content')
              .last
              .queryParameters['cityId'],
          isNull,
        );
        expect(requests.any((r) => r.path.contains('destination:')), isFalse);
        // Failed subsequent search must keep the already selected destination.
        search.fail = true;
        await t.tap(find.byKey(const Key('now-city-picker')));
        await t.pumpAndSettle();
        await t.enterText(find.byKey(const Key('now-city-search')), 'Next');
        await t.pump(const Duration(milliseconds: 350));
        await t.pumpAndSettle();
        expect(find.text('重试搜索'), findsOneWidget);
        expect(city.selectedDestination!.name, '巴黎');
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox.shrink());
        moments.dispose();
        city.dispose();
        auth.dispose();
        client.close();
        t.view.resetPhysicalSize();
        t.view.resetDevicePixelRatio();
        await t.pump();
      }
    },
  );

  testWidgets(
    'catalog card without map requires explicit camera confirmation, cancel preserves view and draft',
    (t) async {
      final auth = SeedTestAuth()..owner = owned.id(9001);
      final search = _Search();
      final client = MockClient(
        (request) async => http.Response(
          jsonEncode({
            'data': request.url.path == '/v1/cities'
                ? [
                    {
                      'id': 'unit-current',
                      'name': '合成当前城市',
                      'region': '合成地区',
                      'countryCode': 'GB',
                      'contentStatus': 'unit',
                      'source': {'label': 'unit', 'maintainer': 'unit'},
                      'map': {
                        'provider': 'mapbox',
                        'latitude': 57.1,
                        'longitude': -2.1,
                        'defaultZoom': 12,
                      },
                    },
                    {
                      'id': 'unit-no-map',
                      'name': '合成巴黎',
                      'region': '法兰西岛',
                      'countryCode': 'FR',
                      'contentStatus': 'unit',
                      'source': {'label': 'unit', 'maintainer': 'unit'},
                      'map': null,
                    },
                  ]
                : [],
          }),
          200,
          headers: {'content-type': 'application/json'},
        ),
      );
      final city = PublicCityController(
        client: client,
        apiBaseUrl: owned.base,
        loadUnrequestedPlaces: false,
        defaultExplorationCityID: 'unit-current',
      );
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: owned.base,
      );
      try {
        t.view.physicalSize = const Size(390, 844);
        t.view.devicePixelRatio = 1;
        t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
        await t.runAsync(city.loadCities);
        await t.pumpWidget(
          MaterialApp(
            home: MapWorkspace(
              city: city,
              auth: auth,
              moments: moments,
              seedClient: client,
              seedApiBaseUrl: owned.base,
              destinationSearchService: search,
            ),
          ),
        );
        await t.pumpAndSettle();
        await t.enterText(nowField(), '跨城市仍保留的草稿正文');
        await t.tap(find.byKey(const Key('now-city-picker')));
        await t.pumpAndSettle();
        final mapElement = find.byType(PublicCityMapView).evaluate().single;
        final backgroundRect = t.getRect(nowField());
        final searchField = find.byKey(const Key('now-city-search'));
        await t.tap(searchField);
        t.view.viewInsets = const FakeViewPadding(bottom: 300);
        await t.pumpAndSettle();
        expect(t.widget<TextField>(searchField).focusNode?.hasFocus, true);
        expect(t.widget<TextField>(nowField()).focusNode?.hasFocus, false);
        expect(
          t.getRect(nowField()),
          backgroundRect,
          reason: 'destination keyboard does not move background composer',
        );
        expect(city.selectedCity?.id, 'unit-current');
        t.view.resetViewInsets();
        await t.pumpAndSettle();
        await t.tap(find.byTooltip('关闭城市选择'));
        await t.pumpAndSettle();
        expect(city.selectedCity?.id, 'unit-current');
        expect(city.selectedDestination, isNull);
        expect(t.widget<TextField>(nowField()).controller!.text, '跨城市仍保留的草稿正文');

        await t.tap(find.byKey(const Key('now-city-picker')));
        await t.pumpAndSettle();
        final card = find.byKey(const Key('now-scope-city-unit-no-map'));
        await t.ensureVisible(card);
        await t.tap(card);
        await t.pumpAndSettle();
        expect(find.text('为合成巴黎选择地图位置'), findsOneWidget);
        expect(search.queries, ['合成巴黎 法兰西岛 FR']);
        expect(
          city.selectedCity?.id,
          'unit-current',
          reason: 'search is not confirmation',
        );
        expect(city.renderCity?.map?.latitude, 57.1);
        await t.tap(find.text('返回目录'));
        await t.pumpAndSettle();
        expect(city.selectedCity?.id, 'unit-current');
        expect(city.selectedDestination, isNull);
        await t.ensureVisible(card);
        await t.tap(card);
        await t.pumpAndSettle();
        await t.tap(
          find.byKey(
            const Key('now-destination-destination:mapbox:fixture-paris'),
          ),
        );
        await t.pumpAndSettle();
        expect(city.selectedCity?.id, 'unit-no-map');
        expect(city.selectedCity?.map, isNull);
        expect(city.contentCityID, 'unit-no-map');
        expect(city.selectedDestination?.internalCityID, 'unit-no-map');
        expect(city.selectedDestination?.isTemporary, true);
        expect(city.renderCity?.id, 'unit-no-map');
        expect(city.renderCity?.map?.latitude, 48.8566);
        expect(city.renderCity?.map?.longitude, 2.3522);
        expect(city.recentCityIDs, ['unit-no-map']);
        expect(t.widget<TextField>(nowField()).controller!.text, '跨城市仍保留的草稿正文');
        expect(
          find.byType(PublicCityMapView).evaluate().single,
          same(mapElement),
        );
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox.shrink());
        moments.dispose();
        city.dispose();
        auth.dispose();
        client.close();
        t.view.resetViewInsets();
        t.view.resetPhysicalSize();
        t.view.resetDevicePixelRatio();
        await t.pump();
      }
    },
  );
}
