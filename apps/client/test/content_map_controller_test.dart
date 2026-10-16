import 'dart:async';
import 'package:birdtie_client/src/workspace/map_content_coverage.dart';
import 'dart:convert';
import 'dart:typed_data';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:birdtie_client/src/workspace/owned_map_media_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

String id(int n) => '17000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
String hash(String value) => sha256.convert(utf8.encode(value)).toString();
const base = 'https://content-map.test';
const bounds = MapBounds(west: -2.3, south: 57, east: -1.9, north: 57.4);
final jpg = base64Decode(
  '/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgNDRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjL/wAARCABaAKADASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwDiKKKKk8sKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigD//2Q==',
);

class Fixture extends http.BaseClient {
  DateTime now = DateTime.utc(2026, 10, 16);
  String owner = id(9000), source = hash('page');
  String? organization;
  bool clusters = false,
      covers = false,
      holdMedia = false,
      rotateCursors = false;
  int cursorRevision = 0;
  int count = 1, status = 200, aborted = 0;
  final requests = <http.BaseRequest>[];
  Completer<void>? delay;
  final mediaWaiters = <Completer<void>>[];
  Map<String, Object?> lease() => {
    'observedAt': now.toIso8601String(),
    'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
  };
  Map<String, Object?> selector(int n) => {
    'id': id(n),
    'revision': 1,
    'visibilitySource': 'SELF_PRIVATE',
    'detailPath': '/v1/me/map-content/moments/${id(n)}',
  };
  Map<String, Object?> cover(int n) => {
    'kind': 'MOMENT_IMAGE',
    'id': id(5000 + n),
    'revision': 1,
    'visibilitySource': 'SELF_PRIVATE',
    'mimeType': 'image/jpeg',
    'width': 160,
    'height': 90,
    'byteCount': jpg.length,
    'sha256': sha256.convert(jpg).toString(),
    'sourceVersion': hash('asset-$n'),
    'contentPath':
        '/v1/me/map-content/moments/${id(n)}/media/${id(5000 + n)}/content?variant=marker&sourceVersion=${hash('asset-$n')}',
  };
  Map<String, Object?> pin(int n, MapBounds b) => {
    'markerId': 'anchor:${id(1000 + n)}',
    'kind': 'MOMENT_ANCHOR',
    'entityId': id(n),
    'momentId': id(n),
    'anchorId': id(1000 + n),
    'entryId': id(n),
    'threadId': id(8000),
    'anchorCount': 1,
    'placeId': '',
    'name': '合成动态 $n',
    'categoryCode': '',
    'cityId': null,
    'anchor': {
      'coordinateSystem': 'wgs84',
      'precision': 'point',
      'latitude': (b.south + b.north) / 2,
      'longitude': (b.west + b.longitudeSpan / 2 + 180) % 360 - 180,
    },
    'visibilitySources': ['SELF_PRIVATE'],
    'sourceVersion': hash('pin-$n'),
    'momentCount': 1,
    'moments': [selector(n)],
    'momentsTruncated': false,
    'cover': covers ? cover(n) : null,
  };
  Map<String, Object?> page(Uri uri) {
    final q = uri.queryParameters;
    final b = MapBounds(
      west: double.parse(q['west']!),
      south: double.parse(q['south']!),
      east: double.parse(q['east']!),
      north: double.parse(q['north']!),
    );
    final cid = 'cluster:${hash('cluster')}';
    final member = q['clusterId'] != null;
    var pins = List.generate(
      member ? 1 : count,
      (i) => pin(member ? (q['cursor'] == null ? 1 : 2) : i + 1, b),
    );
    if (clusters && !member) {
      final p = pin(1, b);
      final lat = (p['anchor'] as Map)['latitude'] as double,
          lon = (p['anchor'] as Map)['longitude'] as double;
      pins = [
        {
          ...p,
          'kind': 'CLUSTER',
          'markerId': cid,
          'entityId': cid,
          'momentId': '',
          'anchorId': '',
          'entryId': '',
          'threadId': '',
          'anchorCount': 2,
          'momentCount': 2,
          'moments': [selector(1), selector(2)],
          'cover': null,
          'bounds': {
            'west': lon - .00001,
            'east': lon + .00001,
            'south': lat - .00001,
            'north': lat + .00001,
          },
          'membersPath': Uri(
            path: '/v1/me/map-content',
            queryParameters: {...q, 'clusterId': cid, 'sourceVersion': source},
          ).toString(),
        },
      ];
    }
    return {
      'schemaVersion': 'map-content-v2',
      'scope': 'MAP_CONTENT',
      'filterScope': 'DEFAULT_CONTENT',
      'viewerAccountId': owner,
      'cityId': q['cityId'] ?? '',
      'bounds': b.toJson(),
      'zoom': double.parse(q['zoom']!),
      'sourceVersion': source,
      'pins': pins,
      'truncated': false,
      'totalContentCount': clusters ? 2 : count,
      'totalAnchorCount': clusters ? 2 : count,
      'nextCursor': member && rotateCursors
          ? 'continuation-${cursorRevision++}'
          : member && q['cursor'] == null
          ? 'second-page'
          : null,
      ...lease(),
    };
  }

  Map<String, Object?> detail(int n) => {
    'schemaVersion': 'map-content-moment-v1',
    'viewerAccountId': owner,
    'id': id(n),
    'revision': 1,
    'visibilitySource': 'SELF_PRIVATE',
    'canEdit': true,
    'status': 'published',
    'visibility': 'private',
    'title': '',
    'body': '合成正文',
    'createdAt': '2026-01-01T00:00:00Z',
    'locations': [],
    'media': [],
    'sourceVersion': hash('detail-$n'),
    ...lease(),
  };
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    requests.add(request);
    expectSync(request.headers['Authorization'], 'Bearer fixture');
    expectSync(request.followRedirects, false);
    final responseStatus = status;
    final media = request.url.path.endsWith('/content');
    final body = media
        ? Uint8List.fromList(jpg)
        : utf8.encode(
            jsonEncode({
              'data': request.url.path == '/v1/me/map-content'
                  ? page(request.url)
                  : detail(
                      int.parse(
                        request.url.path.split('/').last.split('-').last,
                      ),
                    ),
            }),
          );
    final wait = delay;
    delay = null;
    Completer<void>? mw;
    if (media && holdMedia) {
      mw = Completer<void>();
      mediaWaiters.add(mw);
    }
    final abort = (request as http.AbortableRequest).abortTrigger;
    final waits = <Future<void>>[
      if (wait != null) wait.future,
      if (mw != null) mw.future,
    ];
    if (waits.isNotEmpty) {
      await Future.any([
        Future.wait(waits),
        if (abort != null)
          abort.then((_) {
            aborted++;
            throw http.RequestAbortedException(request.url);
          }),
      ]);
    }
    return http.StreamedResponse(
      Stream.value(body),
      responseStatus,
      contentLength: body.length,
      headers: {
        'cache-control': 'no-store',
        'content-type': media ? 'image/jpeg' : 'application/json',
        'x-content-type-options': 'nosniff',
        'content-length': '${body.length}',
      },
    );
  }

  void releaseMedia() {
    holdMedia = false;
    for (final wait in mediaWaiters) {
      if (!wait.isCompleted) wait.complete();
    }
  }
}

Future<void> drain(WidgetTester t, bool Function() done) async {
  for (var i = 0; i < 150 && !done(); i++) {
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 10)),
    );
    await t.pump();
  }
  expect(done(), isTrue);
}

void main() {
  testWidgets(
    'v2 coordinate anchors and bounded cluster pages retain target grants and reject POI-only pins',
    (t) async {
      final f = Fixture()..clusters = true;
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: f,
        apiBaseUrl: () => base,
        ownerID: () => f.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => f.organization,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => f.now,
      );
      c.updateQuery(
        '',
        bounds,
        zoom: 11,
        selectedMarkerID: 'owned-map:anchor:${id(1001)}',
      );
      await drain(t, () => c.current && !c.loading);
      final parent = c.items.single;
      expect(parent.isCluster, true);
      expect(parent.entity.kind, MapEntityKind.cluster);
      final first = await t.runAsync(() => c.readClusterMembers(parent));
      expect(first!.bounds.contains(parent.latitude, parent.longitude), true);
      expect(
        first.bounds,
        MapContentCoverage.padded(OwnedMapController.queryBounds(bounds, 11)),
      );
      expect(first.nextCursor, 'second-page');
      final child = first.items.single;
      expect(child.isMomentAnchor, true);
      expect(child.placeID, '');
      expect(child.entity.kind, MapEntityKind.moment);
      expect(child.entity.entryID, id(1));
      expect(c.find(child.mapID), child);
      expect(c.sourceCurrent(child), true);
      final detail = await t.runAsync(() => c.readMoment(child.moments.single));
      expect(detail!.id, id(1));
      expect(c.detailCurrent(detail), true);
      f.now = f.now.add(const Duration(seconds: 31));
      expect(
        c.detailCurrent(detail),
        false,
        reason: 'Position cursor never extends content authorization.',
      );
      await t.runAsync(c.refresh);
      final second = await t.runAsync(
        () => c.readClusterMembers(c.items.single, cursor: first.nextCursor),
      );
      expect(second!.items.single.momentID, id(2));
      expect(
        f.requests
            .where((r) => r.url.queryParameters.containsKey('clusterId'))
            .every(
              (r) =>
                  r.url.queryParameters['west'] ==
                  Uri.parse(c.items.single.membersPath).queryParameters['west'],
            ),
        true,
      );
      final permission = c.permissionEpoch;
      c.updateQuery('', bounds, zoom: 11);
      await drain(t, () => c.current && !c.loading);
      expect(c.permissionEpoch, permission);
      final resumed = await t.runAsync(
        () => c.readClusterMembers(c.items.single, cursor: first.nextCursor),
      );
      expect(resumed!.items.single.momentID, id(2));
      f.rotateCursors = true;
      await t.runAsync(() async {
        for (var renewal = 0; renewal < 6; renewal++) {
          var cursor = first.nextCursor;
          for (var page = 0; page < 8; page++) {
            final current = await c.readClusterMembers(
              c.items.single,
              cursor: cursor,
            );
            cursor = current.nextCursor;
          }
        }
        final retained = await c.readClusterMembers(
          c.items.single,
          cursor: first.nextCursor,
        );
        expect(
          retained.items.single.momentID,
          id(2),
          reason:
              'Current window starting position survives more than32 freshly signed continuation keys.',
        );
      });
      final bad = f.pin(1, c.view!.bounds)
        ..['momentCount'] = 0
        ..['moments'] = [];
      expect(
        () => OwnedMapPin.decode(bad, c.view!.bounds),
        throwsFormatException,
      );
      c.retire();
      expect(c.detailCurrent(detail), false);
      expect(c.sourceCurrent(child), false);
      await expectLater(c.readClusterMembers(parent), throwsFormatException);
      c.dispose();
      identity.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'settled query reuse, cancellation, LRU bytes and permission withdrawal',
    (t) async {
      final f = Fixture();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: f,
        apiBaseUrl: () => base,
        ownerID: () => f.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => f.organization,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => f.now,
        maxCachedViews: 2,
        maxCachedPins: 2,
        maxCachedBytes: 18000,
      );
      f.delay = Completer<void>();
      c.updateQuery('', bounds);
      final a = c.refresh();
      final b = c.refresh();
      expect(identical(a, b), true);
      expect(
        c.requests,
        1,
        reason: 'settled event starts transport without a second debounce',
      );
      const other = MapBounds(west: 1, south: 1, east: 2, north: 2);
      c.updateQuery('', other);
      await drain(t, () => c.current && !c.loading);
      expect(f.aborted, 1);
      expect(c.cancellations, 1);
      expect(c.view!.bounds, OwnedMapController.queryBounds(other, 12));
      expect(c.requests, 2);
      for (var i = 0; i < 20; i++) c.updateQuery('', other);
      expect(c.requests, 2);
      c.updateQuery('', bounds);
      await drain(t, () => c.current && !c.loading);
      c.updateQuery('', other);
      expect(c.current, true);
      expect(c.cacheHits, 1);
      expect(c.requests, 3);
      final stable = c.items.single;
      f.status = 503;
      await t.runAsync(c.refresh);
      expect(c.current, true);
      expect(c.sourceCurrent(stable), true);
      f.status = 403;
      await t.runAsync(c.refresh);
      expect(c.current, false);
      expect(c.cachedViews, 0);
      expect(c.cachedMediaItems, 0);
      f.status = 200;
      for (var i = 0; i < 4; i++) {
        c.updateQuery(
          '',
          MapBounds(west: i + 10, south: 1, east: i + 10.5, north: 2),
        );
        await drain(t, () => c.current && !c.loading);
      }
      expect(c.cachedViews, 2);
      expect(c.cachedPins, 2);
      expect(c.cachedBytes, lessThanOrEqualTo(18000));
      c.updateQuery(
        '',
        const MapBounds(west: 179, south: -1, east: -179, north: 1),
      );
      await drain(t, () => c.current && !c.loading);
      expect(c.view!.bounds.west, greaterThan(c.view!.bounds.east));
      f.organization = id(99);
      identity.value++;
      expect(c.current, false);
      expect(c.cachedViews, 0);
      f.organization = null;
      identity.value++;
      await drain(t, () => c.current && !c.loading);
      f.now = f.now.add(const Duration(seconds: 31));
      expect(c.current, false);
      c.dispose();
      identity.dispose();
      await t.pump();
      // A response bigger than the entire cache can be used for this view but
      // cannot survive as an unbounded future viewport entry.
      final small = OwnedMapController(
        client: Fixture(),
        apiBaseUrl: () => base,
        ownerID: () => id(9000),
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => null,
        identityChanges: ValueNotifier(0),
        refreshAllowed: () => true,
        now: () => DateTime.utc(2026, 10, 16),
        maxCachedBytes: 1,
      );
      small.updateQuery('', bounds);
      await drain(t, () => small.current);
      expect(small.cachedViews, 0);
      small.dispose();
      await t.pump();
    },
  );
  testWidgets(
    '12 covers 4 transports selected first, real DPR decode and renewed grants reuse pixels',
    (t) async {
      final f = Fixture()
        ..covers = true
        ..count = 20
        ..holdMedia = true;
      final api = OwnedMapAPI(client: f, apiBaseUrl: () => base);
      final q = Uri(
        path: '/v1/me/map-content',
        queryParameters: {
          'west': '-2.3',
          'east': '-1.9',
          'south': '57',
          'north': '57.4',
          'zoom': '12',
        },
      );
      var allowed = true;
      final media = OwnedMapMediaController(
        api: api,
        current: () => allowed,
        authorizationHeader: () => 'Bearer fixture',
        now: () => f.now,
      );
      OwnedMapView view() => OwnedMapView.decode(
        f.page(q),
        ownerID: f.owner,
        cityID: '',
        bounds: bounds,
        now: f.now,
      );
      final first = view();
      media.sync(
        first,
        devicePixelRatio: 2,
        selectedMarkerID: 'anchor:${id(1020)}',
      );
      await drain(t, () => f.mediaWaiters.length == 4);
      expect(media.inFlight, 4);
      expect(media.cachedItems, 12);
      expect(f.requests.first.url.path, contains('/${id(20)}/'));
      f.releaseMedia();
      await drain(t, () => media.decodes == 12 && media.inFlight == 0);
      expect(media.requests, 12);
      expect(media.inFlightPeak, 4);
      expect(media.cachedBytes, lessThanOrEqualTo(24 * 1024 * 1024));
      final pixels = media.decorate(first.items.last).mediaPixels!;
      expect(pixels.image.width, lessThanOrEqualTo(72));
      expect(pixels.current(), true);
      media.suspend();
      expect(pixels.current(), false);
      expect(media.cachedItems, 12);
      f.source = hash('other-viewport');
      final renewed = view();
      media.sync(
        renewed,
        devicePixelRatio: 2,
        selectedMarkerID: 'anchor:${id(1020)}',
      );
      expect(media.requests, 12);
      expect(media.decodes, 12);
      expect(media.cacheHits, 12);
      final next = media.decorate(renewed.items.last).mediaPixels!;
      expect(next.current(), true);
      expect(pixels.current(), false);
      allowed = false;
      media.retire();
      expect(next.current(), false);
      expect(media.cachedItems, 0);
      media.dispose();
      api.dispose();
      await t.pump();
    },
  );
}
