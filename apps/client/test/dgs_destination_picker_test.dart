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
        expect(find.text('选择城市或目的地'), findsOneWidget);
        await t.enterText(find.byKey(const Key('now-city-search')), 'Paris');
        await t.pump(const Duration(milliseconds: 350));
        await t.pumpAndSettle();
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
}
