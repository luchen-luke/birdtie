import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_display_policy.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_visual.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'map_layers_api_test.dart' show mapTestBounds;
import 'now_scope_recovery_test.dart' show NowFixtureCity;

class _City extends PublicCityController {
  @override
  PublicCity? get selectedCity => const PublicCity(
    id: 'map-content-test',
    name: '合成地图范围',
    region: '合成',
    contentStatus: 'building',
    map: null,
    source: PublicSource(
      label: '合成',
      maintainer: 'fixture',
      freshness: 'fixture',
      updatedAt: null,
    ),
  );
}

class _Content extends OwnedMapController {
  _Content(ValueNotifier<int> identity)
    : super(
        client: MockClient((_) async => http.Response('', 404)),
        apiBaseUrl: () => 'https://map-unit.test',
        ownerID: () => null,
        authorizationHeader: () => null,
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => false,
      );
  var serial = 1;
  var available = true;
  static const pin = OwnedMapPin(
    placeID: 'a1700000-0000-4000-8000-000000000001',
    name: '合成照片地点',
    categoryCode: 'culture.museum',
    cityID: 'map-content-test',
    latitude: 57.15,
    longitude: -2.1,
    visibilitySources: ['SELF_PRIVATE'],
    sourceVersion: 'fixture-current',
    momentCount: 2,
    moments: [],
    momentsTruncated: true,
    cover: null,
  );
  @override
  int get epoch => serial;
  @override
  String? get cityID => 'map-content-test';
  @override
  bool get current => available;
  @override
  List<OwnedMapPin> get items => available ? [pin] : [];
  @override
  List<MapEntity> get entities => items.map((p) => p.entity).toList();
  @override
  bool sourceCurrent(OwnedMapPin value) => available && identical(value, pin);
  void revoke() {
    available = false;
    ++serial;
    notifyListeners();
  }
}

void main() {
  testWidgets('Now preview expands retained content into full text and media', (
    t,
  ) async {
    String id(int n) =>
        '17000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
    final auth = SeedTestAuth()..owner = id(9001);
    final city = NowFixtureCity();
    final source = List.filled(64, 'b').join();
    final jpeg = File(
      'test/fixtures/map-media/isolation-a.jpg',
    ).readAsBytesSync();
    const body = '合成原正文第一行\n第二行内容\n第三行内容\n第四行完整内容';
    var mapReads = 0, pageReads = 0;
    final methods = <String>[];
    http.Response json(Object value) => http.Response.bytes(
      utf8.encode(jsonEncode({'data': value})),
      200,
      headers: {
        'content-type': 'application/json',
        'cache-control': 'no-store',
      },
    );
    final client = MockClient((r) async {
      methods.add(r.method);
      final now = DateTime.now().toUtc();
      final lease = {
        'observedAt': now.toIso8601String(),
        'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
      };
      if (r.url.path == '/v1/me/map-content') {
        mapReads++;
        return json({
          'schemaVersion': 'map-content-v1',
          'scope': 'MAP_CONTENT',
          'viewerAccountId': auth.accountID,
          'cityId': 'alpha',
          'bounds': mapTestBounds.toJson(),
          'sourceVersion': source,
          'truncated': false,
          ...lease,
          'pins': [
            {
              'placeId': id(1),
              'name': '合成展开地点',
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
                  'id': id(2),
                  'revision': 1,
                  'visibilitySource': 'SELF_PRIVATE',
                  'detailPath': '/v1/me/map-content/moments/${id(2)}',
                },
              ],
              'momentsTruncated': false,
              'cover': null,
            },
          ],
        });
      }
      if (r.url.path == '/v1/me/map-content/places/${id(1)}/moments') {
        pageReads++;
        return json({
          'schemaVersion': 'map-content-place-moments-v1',
          'viewerAccountId': auth.accountID,
          'placeId': id(1),
          'sourceVersion': source,
          'nextCursor': null,
          ...lease,
          'items': [
            {
              'schemaVersion': 'map-content-moment-v1',
              'viewerAccountId': auth.accountID,
              'id': id(2),
              'revision': 1,
              'visibilitySource': 'SELF_PRIVATE',
              'canEdit': true,
              'status': 'draft',
              'visibility': 'private',
              'title': '',
              'body': body,
              'createdAt': '2026-01-01T00:00:00Z',
              'locations': [
                {'placeId': id(1), 'name': '原文关联地点', 'anchor': null},
              ],
              'sourceVersion': source,
              ...lease,
              'media': [
                {
                  'kind': 'IMAGE',
                  'id': id(3),
                  'revision': 1,
                  'mimeType': 'image/jpeg',
                  'width': 320,
                  'height': 180,
                  'byteCount': jpeg.length,
                  'sha256': sha256.convert(jpeg).toString(),
                  'sourceVersion': source,
                  'contentPath':
                      '/v1/me/map-content/moments/${id(2)}/media/${id(3)}/content?sourceVersion=$source',
                  'durationMs': null,
                  'videoCodec': null,
                  'audioCodec': null,
                  'poster': null,
                },
              ],
            },
          ],
        });
      }
      if (r.url.path.endsWith('/content')) {
        return http.Response.bytes(
          jpeg,
          200,
          headers: {
            'content-type': 'image/jpeg',
            'cache-control': 'no-store',
            'x-content-type-options': 'nosniff',
            'content-length': '${jpeg.length}',
          },
        );
      }
      if (r.url.path == '/v1/cities/alpha/map-layers') {
        return json({
          'schemaVersion': 'typed-map-layers-v1',
          'scope': 'PUBLIC',
          'cityId': 'alpha',
          ...lease,
          'truncated': false,
          'items': [],
        });
      }
      if (r.url.path.endsWith('/activities') ||
          r.url.path == '/v1/me/moments') {
        return json([]);
      }
      return http.Response('{}', 404);
    });
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'https://map-reader-expand.test',
    );
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    MapCanvas canvas() => t.widget<MapCanvas>(find.byType(MapCanvas));
    Future<void> waitFor(bool Function() done) async {
      for (var i = 0; i < 50 && !done(); i++) {
        await t.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 20)),
        );
        await t.pump();
      }
      expect(done(), isTrue);
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
            seedApiBaseUrl: 'https://map-reader-expand.test',
            placeDetailClient: client,
            placeDetailApiBaseUrl: 'https://map-reader-expand.test',
          ),
        ),
      );
      await t.pumpAndSettle();
      await canvas().layers!.load('alpha', mapTestBounds);
      canvas().mapState.initializeViewport(mapTestBounds);
      canvas().mapContent!.sync(cityID: 'alpha', bounds: mapTestBounds);
      await waitFor(() => canvas().mapContent!.current);
      final owned = canvas().mapContent!, epoch = canvas().mapContent!.epoch;
      final viewport = canvas().mapState;
      final mapElement = t.element(find.byType(PublicCityMapView));
      final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
      t
          .widget<PublicCityMapView>(find.byType(PublicCityMapView))
          .onEntitySelected!(owned.entities.single);
      await t.pump();
      await waitFor(() => find.text(body).evaluate().isNotEmpty);
      await waitFor(
        () => find
            .descendant(of: find.byType(PageView), matching: find.byType(Image))
            .evaluate()
            .isNotEmpty,
      );
      await t.pump(const Duration(milliseconds: 200));
      final panelState = t.state(find.byType(NowMapContentPanel));
      expect(t.widget<Text>(find.text(body)).maxLines, 3);
      expect(t.getSize(find.byType(PageView)).height, 96);
      expect(find.byTooltip('关闭地点预览'), findsOneWidget);
      final readsBefore = (mapReads, pageReads);
      await t.tap(find.text('查看动态'));
      await t.pump();
      expect(t.state(find.byType(NowMapContentPanel)), same(panelState));
      expect(find.byTooltip('返回地图'), findsOneWidget);
      expect(find.text('查看动态'), findsNothing);
      expect(t.widget<Text>(find.text(body)).maxLines, isNull);
      expect(t.getSize(find.byType(PageView)).height, 240);
      await waitFor(
        () => find
            .descendant(of: find.byType(PageView), matching: find.byType(Image))
            .evaluate()
            .isNotEmpty,
      );
      expect(
        (mapReads, pageReads),
        readsBefore,
        reason: 'Expansion must not restart a content read.',
      );
      expect(owned.epoch, epoch);
      expect(owned.current, isTrue);
      expect(canvas().mapState, same(viewport));
      expect(viewport.viewportBounds, mapTestBounds);
      expect(t.element(find.byType(PublicCityMapView)), same(mapElement));
      expect(
        t.state<AgentComposerState>(find.byType(AgentComposer)),
        same(composer),
      );
      await t.tap(find.byTooltip('返回地图'));
      await t.pumpAndSettle();
      expect(find.byType(NowMapContentPanel), findsNothing);
      expect(owned.epoch, epoch);
      expect(viewport.viewportBounds, mapTestBounds);
      expect(canvas().workspace.task, isNull);
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
  });

  testWidgets(
    'Now map content is selectable in place, retains map, and stale readers cannot revive it',
    (t) async {
      final identity = ValueNotifier(0),
          collection = ValueNotifier<PersonalCollectionProjection?>(null);
      final content = _Content(identity),
          city = _City(),
          workspace = AgentWorkspaceController();
      final map = MapViewportState(),
          discovery = NowDiscoveryController(authorizationHeader: () => null);
      const bounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.2);
      map.initializeViewport(bounds);
      final opened = <String>[];
      await t.pumpWidget(
        MaterialApp(
          home: MapCanvas(
            city: city,
            workspace: workspace,
            mapState: map,
            discovery: discovery,
            mapContent: content,
            personalCollection: collection,
            onInitialViewport: (_) {},
            onMapUnavailable: (_) {},
            onEntityPicked: opened.add,
          ),
        ),
      );
      final element = find.byType(PublicCityMapView).evaluate().single;
      final reader = t.widget<PublicCityMapView>(
        find.byType(PublicCityMapView),
      );
      expect(reader.entities.single.id, _Content.pin.mapID);
      reader.onEntitySelected!(_Content.pin.entity);
      await t.pump();
      expect(opened, [_Content.pin.mapID]);
      expect(workspace.selectedEntityId, _Content.pin.mapID);
      expect(workspace.presentedResult, isNull);
      expect(workspace.task, isNull);
      expect(map.viewportBounds, bounds);
      expect(find.byType(PublicCityMapView).evaluate().single, same(element));
      collection.value = PersonalCollectionProjection(
        id: 'chosen-collection',
        cityID: 'map-content-test',
        title: '合成收藏',
        entities: [],
        sourceCurrent: () => true,
      );
      await t.pump();
      expect(
        t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
        isEmpty,
      );
      reader.onEntitySelected!(_Content.pin.entity);
      expect(opened, hasLength(1));
      collection.value = null;
      content.revoke();
      workspace.selectEntity(null, preserveViewport: true);
      await t.pump();
      reader.onEntitySelected!(_Content.pin.entity);
      expect(opened, hasLength(1));
      expect(workspace.selectedEntityId, isNull);
      expect(
        t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      content.dispose();
      identity.dispose();
      collection.dispose();
      discovery.dispose();
      map.dispose();
      workspace.dispose();
      city.dispose();
    },
  );

  test(
    'opening zoom keeps real content legible without moving or dropping dense places',
    () {
      final now = DateTime.now().toUtc();
      final entities = List.generate(
        20,
        (i) => MapEntity(
          id: 'owned-map:$i',
          kind: MapEntityKind.place,
          title: '合成地点$i',
          subtitle: '',
          latitude: 57.15,
          longitude: -2.1 + i / 10000,
          recentMoments: i == 10
              ? MapRecentMoments(
                  count: 2,
                  checkedAt: now,
                  windowStart: now.subtract(const Duration(days: 30)),
                  validUntil: now.add(const Duration(seconds: 30)),
                )
              : null,
        ),
      );
      final plan = MapDisplayPolicy.plan(
        entities: entities,
        positions: {
          for (var i = 0; i < 20; i++)
            entities[i].id: Offset(80.0 + i * 4, 150),
        },
        zoom: 13,
      );
      expect(plan.entities, hasLength(20));
      expect(plan.labelIDs, contains('owned-map:10'));
      expect(plan.denseIDs, isNot(contains('owned-map:10')));
      for (final entity in plan.entities) {
        final original = entities.singleWhere((e) => e.id == entity.id);
        expect(
          (entity.latitude, entity.longitude),
          (original.latitude, original.longitude),
        );
      }
    },
  );
}
