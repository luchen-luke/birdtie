import 'dart:convert';

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'map_layers_api_test.dart' show mapTestBounds;
import 'now_scope_recovery_test.dart' show NowFixtureCity;

void main() {
  testWidgets(
    'actual Now reader clips covered map points and restores the same map on close',
    (t) async {
      const placeID = '73000000-0000-4000-8000-000000000001';
      const momentID = '73000000-0000-4000-8000-000000000002';
      const source =
          'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
      const body = '隔离组件正文；不请求任何真实地图、账号或媒体。';
      final auth = SeedTestAuth()
        ..owner = '73000000-0000-4000-8000-000000009001';
      final city = NowFixtureCity();
      final methods = <String>[];
      var mapReads = 0, pageReads = 0;
      http.Response json(Object value) => http.Response.bytes(
        utf8.encode(jsonEncode({'data': value})),
        200,
        headers: {
          'content-type': 'application/json',
          'cache-control': 'no-store',
        },
      );
      final client = MockClient((request) async {
        methods.add(request.method);
        final now = DateTime.now().toUtc();
        final lease = {
          'observedAt': now.toIso8601String(),
          'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
        };
        if (request.url.path == '/v1/me/map-content') {
          mapReads++;
          return json({
            'schemaVersion': 'map-content-v2',
            'scope': 'MAP_CONTENT',
            'viewerAccountId': auth.accountID,
            'cityId': 'alpha',
            'bounds': {
              for (final key in ['west', 'south', 'east', 'north'])
                key: double.parse(request.url.queryParameters[key]!),
            },
            'sourceVersion': source,
            'truncated': false,
            'zoom': double.parse(request.url.queryParameters['zoom'] ?? '12'),
            'filterScope': 'DEFAULT_CONTENT',
            'totalContentCount': 1,
            'totalAnchorCount': 1,
            'nextCursor': null,
            ...lease,
            'pins': [
              {
                'markerId': 'place:$placeID',
                'kind': 'PLACE_CONTENT',
                'entityId': placeID,
                'anchorCount': 1,
                'placeId': placeID,
                'name': '合成阅读地点',
                'categoryCode': 'museum',
                'cityId': null,
                'anchor': {
                  'coordinateSystem': 'wgs84',
                  'precision': 'point',
                  'latitude': 57.2,
                  'longitude': -2.1,
                },
                'visibilitySources': ['SELF_PRIVATE'],
                'sourceVersion': source,
                'momentCount': 1,
                'moments': [
                  {
                    'id': momentID,
                    'revision': 1,
                    'visibilitySource': 'SELF_PRIVATE',
                    'detailPath': '/v1/me/map-content/moments/$momentID',
                  },
                ],
                'momentsTruncated': false,
                'cover': null,
              },
            ],
          });
        }
        if (request.url.path == '/v1/me/map-content/places/$placeID/moments') {
          pageReads++;
          return json({
            'schemaVersion': 'map-content-place-moments-v1',
            'viewerAccountId': auth.accountID,
            'placeId': placeID,
            'sourceVersion': source,
            'nextCursor': null,
            ...lease,
            'items': [
              {
                'schemaVersion': 'map-content-moment-v1',
                'viewerAccountId': auth.accountID,
                'id': momentID,
                'revision': 1,
                'visibilitySource': 'SELF_PRIVATE',
                'canEdit': true,
                'status': 'draft',
                'visibility': 'private',
                'title': '',
                'body': body,
                'createdAt': '2026-01-01T00:00:00Z',
                'locations': [
                  {'placeId': placeID, 'name': '合成阅读地点', 'anchor': null},
                ],
                'sourceVersion': source,
                ...lease,
                'media': [],
              },
            ],
          });
        }
        if (request.url.path.endsWith('/thread')) {
          // This geometry fixture retains the supported old-API route fallback.
          return http.Response(
            '404 page not found\n',
            404,
            headers: {'content-type': 'text/plain; charset=utf-8'},
          );
        }
        if (request.url.path == '/v1/cities/alpha/map-layers') {
          return json({
            'schemaVersion': 'typed-map-layers-v1',
            'scope': 'PUBLIC',
            'cityId': 'alpha',
            ...lease,
            'truncated': false,
            'items': [],
          });
        }
        if (request.url.path.endsWith('/activities') ||
            request.url.path == '/v1/me/moments') {
          return json([]);
        }
        return http.Response('{}', 404);
      });
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://map-reader-occlusion.test',
      );
      t.view.physicalSize = const Size(430, 900);
      t.view.devicePixelRatio = 1;
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      MapCanvas canvas() => t.widget<MapCanvas>(find.byType(MapCanvas));
      Future<void> waitFor(bool Function() complete) async {
        for (var n = 0; n < 50 && !complete(); n++) {
          await t.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)),
          );
          await t.pump();
        }
        expect(complete(), isTrue);
      }

      try {
        await t.pumpWidget(
          MaterialApp(
            theme: birdtieTheme(Brightness.light),
            home: MapWorkspace(
              city: city,
              auth: auth,
              moments: moments,
              seedClient: client,
              seedApiBaseUrl: 'https://map-reader-occlusion.test',
              placeDetailClient: client,
              placeDetailApiBaseUrl: 'https://map-reader-occlusion.test',
            ),
          ),
        );
        await t.pumpAndSettle();
        await canvas().layers!.load('alpha', mapTestBounds);
        canvas().mapState.initializeViewport(mapTestBounds);
        canvas().mapContent!.sync(cityID: 'alpha', bounds: mapTestBounds);
        await waitFor(() => canvas().mapContent!.current);
        await t.pumpAndSettle();
        final owned = canvas().mapContent!;
        final epoch = owned.epoch;
        final pin = owned.items.single;
        final before = canvas().contentRect!.value!;
        final mapElement = t.element(find.byType(PublicCityMapView));
        final canvasElement = t.element(find.byType(MapCanvas));
        final mapView = t.widget<PublicCityMapView>(
          find.byType(PublicCityMapView),
        );
        mapView.onEntitySelected!(owned.entities.single);
        await t.pump();
        await waitFor(() => find.text(body).evaluate().isNotEmpty);
        await t.pumpAndSettle();
        final panel = find.byType(NowMapContentPanel);
        final preview = t.getRect(panel);
        expect(preview.height, 288);
        final previewClip = canvas().contentRect!.value!;
        expect(previewClip.top, before.top);
        expect(previewClip.bottom, closeTo(preview.top, .1));
        final coveredPoint = Offset(preview.center.dx, preview.top + 80);
        MapDisplayPlan plan(Rect rect) => MapDisplayPolicy.plan(
          entities: owned.entities,
          positions: {pin.mapID: coveredPoint},
          zoom: 14,
          contentRect: rect,
        );
        expect(plan(before).occludedGlyphIDs, isNot(contains(pin.mapID)));
        expect(plan(previewClip).occludedGlyphIDs, contains(pin.mapID));
        final panelState = t.state(panel);
        await t.tap(find.text('查看此地点的全部1条动态'));
        await t.pumpAndSettle();
        final expandedClip = canvas().contentRect!.value!;
        expect(expandedClip.isEmpty, isTrue);
        expect(expandedClip.top, closeTo(t.getTopLeft(panel).dy, .1));
        expect(plan(expandedClip).occludedGlyphIDs, contains(pin.mapID));
        expect(t.state(panel), same(panelState));
        expect(t.element(find.byType(PublicCityMapView)), same(mapElement));
        expect(t.element(find.byType(MapCanvas)), same(canvasElement));
        expect(owned.items.single, same(pin));
        expect(owned.sourceCurrent(pin), isTrue);
        expect(owned.epoch, epoch);
        // Marker clipping must not globally hide SDK attribution/chrome.
        expect(
          find.ancestor(
            of: find.byType(MapCanvas),
            matching: find.byType(ExcludeSemantics),
          ),
          findsNothing,
        );
        expect(find.byTooltip('返回地图').hitTestable(), findsOneWidget);
        final readsBeforeClose = (mapReads, pageReads);
        await t.tap(find.byTooltip('返回地图'));
        await t.pumpAndSettle();
        expect(find.byType(NowMapContentPanel), findsNothing);
        expect(canvas().contentRect!.value, before);
        expect(
          plan(canvas().contentRect!.value!).occludedGlyphIDs,
          isNot(contains(pin.mapID)),
        );
        expect(t.element(find.byType(PublicCityMapView)), same(mapElement));
        expect(owned.items.single, same(pin));
        expect(owned.epoch, epoch);
        expect(canvas().mapState.viewportBounds, mapTestBounds);
        expect(canvas().workspace.selectedEntityId, pin.mapID);
        expect((mapReads, pageReads), readsBeforeClose);
        expect(methods.every((method) => method == 'GET'), isTrue);
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox.shrink());
        await t.pump();
        moments.dispose();
        auth.dispose();
        city.dispose();
        client.close();
        t.view.resetPhysicalSize();
        t.view.resetDevicePixelRatio();
      }
    },
  );
}
