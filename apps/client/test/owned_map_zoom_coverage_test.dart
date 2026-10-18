import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

String id(int n) => '17000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
String hash(String s) => sha256.convert(utf8.encode(s)).toString();
const broad = MapBounds(west: -2.4, south: 57, east: -1.8, north: 57.4);
const narrow = MapBounds(west: -2.14, south: 57.17, east: -2.06, north: 57.23);

class Scenario {
  final identity = ValueNotifier(0);
  final calls = <Uri>[];
  DateTime now = DateTime.now().toUtc();
  Completer<void>? hold;
  bool partialHighZoom = false;
  bool highUsesOldAnchor = false;
  late final controller = OwnedMapController(
    client: MockClient((request) async {
      calls.add(request.url);
      await hold?.future;
      expect(request.method, 'GET');
      expect(request.headers['Authorization'], 'Bearer isolated-zoom-fixture');
      final p = request.url.queryParameters;
      final zoom = double.parse(p['zoom']!);
      final low = zoom == 8;
      final second = p['cursor'] != null;
      final n = low && !second ? 1 : 2;
      final source = hash('isolated-zoom-$zoom');
      return http.Response(
        jsonEncode({
          'data': {
            'schemaVersion': 'map-content-v2',
            'scope': 'MAP_CONTENT',
            'viewerAccountId': id(9001),
            'cityId': 'isolated-zoom',
            'bounds': {
              for (final k in ['west', 'south', 'east', 'north'])
                k: double.parse(p[k]!),
            },
            'zoom': zoom,
            'filterScope': 'DEFAULT_CONTENT',
            'sourceVersion': source,
            'observedAt': now.toIso8601String(),
            'validUntil': now
                .add(const Duration(seconds: 30))
                .toIso8601String(),
            'pins': [
              {
                'kind': 'PLACE_CONTENT',
                'markerId': 'place:${id(n)}',
                'entityId': id(n),
                'placeId': id(n),
                'name': '隔离缩放夹具$n',
                'categoryCode': 'culture.museum',
                'cityId': null,
                'anchor': {
                  'coordinateSystem': 'wgs84',
                  'precision': 'point',
                  'latitude': n == 1 || !low && highUsesOldAnchor
                      ? 57.35
                      : 57.2,
                  'longitude': n == 1 || !low && highUsesOldAnchor
                      ? -2.25
                      : -2.1,
                },
                'visibilitySources': ['SELF_PRIVATE'],
                'sourceVersion': hash('isolated-leaf-$n'),
                'anchorCount': 1,
                'momentCount': 1,
                'momentsTruncated': false,
                'cover': null,
                'moments': [
                  {
                    'id': id(100 + n),
                    'revision': 1,
                    'visibilitySource': 'SELF_PRIVATE',
                    'detailPath': '/v1/me/map-content/moments/${id(100 + n)}',
                  },
                ],
              },
            ],
            'totalContentCount': 2,
            'totalAnchorCount': 2,
            'truncated': (low || partialHighZoom) && !second,
            'nextCursor': (low || partialHighZoom) && !second
                ? 'isolated-second-page'
                : null,
            'viewportPath': Uri(
              path: '/v1/me/map-content',
              queryParameters: {
                ...Map<String, String>.of(p)..remove('cursor'),
                'sourceVersion': source,
              },
            ).toString(),
          },
        }),
        200,
        headers: {
          'content-type': 'application/json',
          'cache-control': 'no-store',
        },
      );
    }),
    apiBaseUrl: () => 'https://isolated-zoom.test',
    ownerID: () => id(9001),
    authorizationHeader: () => 'Bearer isolated-zoom-fixture',
    organizationWorkspaceID: () => null,
    identityChanges: identity,
    refreshAllowed: () => true,
    now: () => now,
  );
  Future<void> read(MapBounds bounds, double zoom, {String? selected}) async {
    controller.sync(
      cityID: 'isolated-zoom',
      bounds: bounds,
      zoom: zoom,
      selectedMarkerID: selected,
    );
    for (var i = 0; i < 100 && controller.loading; i++) {
      await Future<void>.delayed(Duration.zero);
    }
    expect(controller.loading, isFalse);
  }

  void close() {
    controller.dispose();
    identity.dispose();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'later native proof cannot borrow a different issuer, metadata, or expired source',
    () {
      const before = MapEntity(
        id: 'isolated-proof',
        kind: MapEntityKind.place,
        title: '隔离',
        subtitle: '',
        latitude: 31.2,
        longitude: 121.4,
        sourceVersion: 'v1',
      );
      const after = MapEntity(
        id: 'isolated-proof',
        kind: MapEntityKind.place,
        title: '隔离',
        subtitle: '',
        latitude: 31.2,
        longitude: 121.4,
        sourceVersion: 'v2',
      );
      final now = DateTime.utc(2026, 10, 18);
      final issuer = Object();
      final oldGrant = MapEntityReadGrant(
        source: before,
        validUntil: now.add(const Duration(seconds: 30)),
        current: () => false,
        issuerScope: issuer,
        observedAt: now,
      );
      final old = before.withMediaPixels(null, readGrant: oldGrant);
      expect(oldGrant.currentAt(now), isFalse);
      MapEntityReadGrant fresh({
        Object? scope,
        DateTime? observed,
        bool live = true,
      }) => MapEntityReadGrant(
        source: after,
        validUntil: now.add(const Duration(seconds: 60)),
        current: () => live,
        issuerScope: scope ?? issuer,
        observedAt: observed ?? now.add(const Duration(seconds: 1)),
      );
      expect(fresh().authorizesNativeReplacement(old, now), isTrue);
      expect(oldGrant.currentAt(now), isFalse);
      expect(
        fresh(scope: Object()).authorizesNativeReplacement(old, now),
        isFalse,
      );
      expect(
        fresh(observed: now).authorizesNativeReplacement(old, now),
        isFalse,
      );
      expect(fresh(live: false).authorizesNativeReplacement(old, now), isFalse);
      expect(
        fresh().authorizesNativeReplacement(
          old.withAnchorMetadata(placeID: 'changed', precision: 'area'),
          now,
        ),
        isFalse,
      );
      expect(
        fresh().authorizesNativeReplacement(
          old,
          now.add(const Duration(seconds: 30)),
        ),
        isFalse,
      );
    },
  );
  test(
    'zoom keeps a leased display baseline while fetching and incomplete pages do not prove absence',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      await s.read(broad, 8);
      final old = s.controller.items.single;
      final oldUntil = old.validUntil;
      s.hold = Completer<void>();
      s.partialHighZoom = true;
      s.controller.sync(cityID: 'isolated-zoom', bounds: broad, zoom: 15);
      expect(s.controller.loading, isTrue);
      expect(s.controller.items.single, same(old));
      expect(s.controller.view!.zoom, 15);
      expect(
        s.controller.hasNextMapPage,
        isFalse,
        reason: 'An old zoom cursor is never new coverage',
      );
      s.hold!.complete();
      s.hold = null;
      for (var i = 0; i < 100 && s.controller.loading; i++) {
        await Future<void>.delayed(Duration.zero);
      }
      expect(
        s.controller.items.map((p) => p.placeID),
        containsAll([id(1), id(2)]),
      );
      await s.controller.nextMapPage();
      expect(s.calls.last.queryParameters['cursor'], 'isolated-second-page');
      expect(
        s.controller.items.map((p) => p.placeID),
        containsAll([id(1), id(2)]),
        reason:
            'A last continuation page cannot remove a different loaded anchor',
      );
      expect(old.validUntil, oldUntil);
      s.partialHighZoom = false;
      await s.controller.refresh();
      expect(
        s.controller.items.map((p) => p.placeID),
        [id(2)],
        reason: 'A fresh complete single-page read can replace the baseline',
      );
      s.controller.retire();
      expect(s.controller.items, isEmpty);
      expect(s.controller.sourceCurrent(old), isFalse);
    },
  );
  test(
    'zoom changes fetch fresh coverage instead of reporting an empty old first page',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      await s.read(broad, 8);
      expect(s.controller.error, isNull);
      expect(s.controller.items.map((p) => p.placeID), contains(id(1)));
      final count = s.calls.length;
      final oldEntity = s.controller.entities.single;
      final oldGrant = oldEntity.readGrant!;
      await s.read(narrow, 15);
      expect(s.calls.length, greaterThan(count));
      expect(s.calls.last.queryParameters['zoom'], '15.0');
      expect(s.calls.last.queryParameters.containsKey('cursor'), isFalse);
      expect(s.controller.items.single.placeID, id(2));
      expect(s.controller.view!.zoom, 15);
      expect(
        oldGrant.authorizes(oldEntity, s.now),
        isTrue,
        reason:
            'Display interest changes do not revoke the original finite source grant',
      );
      final afterFresh = s.calls.length;
      s.controller.sync(cityID: 'isolated-zoom', bounds: broad, zoom: 8);
      expect(
        s.controller.items.map((p) => p.placeID),
        containsAll([id(1), id(2)]),
      );
      expect(
        s.calls.length,
        afterFresh,
        reason: 'A still-leased matching zoom remains reusable',
      );
      s.controller.retire();
      expect(oldGrant.authorizes(oldEntity, s.now), isFalse);
      await s.read(broad, 8);
      expect(
        s.controller.entities.single.readGrant!.authorizes(
          s.controller.entities.single,
          s.now,
        ),
        isTrue,
      );
      expect(
        oldGrant.authorizes(oldEntity, s.now),
        isFalse,
        reason:
            'The same ID/source in a new controller epoch cannot revive an old native grant',
      );
    },
  );
  test(
    'current anchor representation replaces old zoom without adding counts',
    () async {
      final s = Scenario()
        ..highUsesOldAnchor = true
        ..partialHighZoom = true;
      addTearDown(s.close);
      await s.read(broad, 8);
      final old = s.controller.items.single;
      await s.read(broad, 15);
      expect(s.controller.items.map((p) => p.placeID), [id(2)]);
      expect(s.controller.items.single.momentCount, 1);
      expect(
        s.controller.sourceCurrent(old),
        isTrue,
        reason: 'Display replacement does not borrow or revoke the old reader',
      );
      s.now = s.now.add(const Duration(seconds: 31));
      expect(s.controller.entities, isEmpty);
      expect(s.controller.sourceCurrent(old), isFalse);
    },
  );
  test(
    'refreshing a last page cannot withdraw the first page of the same viewport',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      await s.read(broad, 8);
      final first = s.controller.items.single;
      await s.controller.nextMapPage();
      expect(
        s.controller.items.map((p) => p.placeID),
        containsAll([id(1), id(2)]),
      );
      await s.controller.refresh();
      expect(s.calls.last.queryParameters['cursor'], 'isolated-second-page');
      expect(
        s.controller.items.map((p) => p.placeID),
        containsAll([id(1), id(2)]),
      );
      expect(s.controller.sourceCurrent(first), isTrue);
    },
  );
  test(
    'zoom display and paging keep selected original source under its own lease',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      await s.read(broad, 8);
      final selected = s.controller.items.single;
      final oldUntil = selected.validUntil;
      await s.read(broad, 15, selected: selected.canonicalMarkerID);
      expect(s.calls.any((u) => u.queryParameters['zoom'] == '15.0'), isTrue);
      expect(s.controller.sourceCurrent(selected), isTrue);
      expect(s.controller.find(selected.mapID), isNotNull);
      expect(s.controller.items.map((p) => p.placeID), contains(id(2)));
      expect(
        s.controller.hasNextMapPage,
        isFalse,
        reason: 'An old zoom cursor is not a current viewport continuation',
      );
      final before = s.calls.length;
      await s.controller.refresh();
      final zooms = s.calls.skip(before).map((u) => u.queryParameters['zoom']);
      expect(zooms, containsAll(['8.0', '15.0']));
      expect(
        selected.validUntil,
        oldUntil,
        reason: 'A fresh request never changes the old reader grant',
      );
      s.now = s.now.add(const Duration(seconds: 31));
      expect(s.controller.sourceCurrent(selected), isFalse);
    },
  );
  test(
    'finite source grants latch retirement and survive safe entity decoration copies',
    () {
      const source = MapEntity(
        id: 'isolated-bare',
        kind: MapEntityKind.moment,
        title: '隔离名字',
        subtitle: '',
        latitude: 31.2,
        longitude: 121.4,
        sourceVersion: 'v1',
      );
      final now = DateTime.utc(2026, 10, 18);
      var live = true;
      final grant = MapEntityReadGrant(
        source: source,
        validUntil: now.add(const Duration(seconds: 30)),
        current: () => live,
      );
      final copied = source.withMediaPixels(null, readGrant: grant);
      expect(copied.withMediaPixels(null).readGrant, same(grant));
      final metadataCopy = copied.withAnchorMetadata(
        placeID: copied.placeID,
        precision: copied.precision,
      );
      expect(metadataCopy.readGrant, same(grant));
      expect(grant.authorizes(metadataCopy, now), isTrue);
      expect(
        grant.authorizes(
          copied.withAnchorMetadata(placeID: 'different', precision: 'area'),
          now,
        ),
        isFalse,
      );
      live = false;
      expect(grant.currentAt(now), isFalse);
      live = true;
      expect(grant.currentAt(now), isFalse);
      final deadline = MapEntityReadGrant(
        source: source,
        validUntil: now,
        current: () => true,
      );
      expect(deadline.currentAt(now), isFalse);
      expect(
        deadline.currentAt(now.subtract(const Duration(seconds: 1))),
        isFalse,
      );
    },
  );
}
