import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/map_content_route_geometry.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';

String routeID(int n) =>
    '18500000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
String routeHash(String s) => sha256.convert(utf8.encode(s)).toString();
MapContentRouteQuery routeQuery(
  DateTime now, {
  int n = 1,
  int seconds = 30,
}) => MapContentRouteQuery(
  threadID: routeID(100),
  expectedProvider: 'mapbox',
  validUntil: now.add(Duration(seconds: seconds)),
  from: MapContentRouteEndpoint(
    viewportPath:
        '/v1/me/map-content?west=-2.3&south=57&east=-1.9&north=57.4&zoom=17&sourceVersion=${routeHash('page')}&cursor=original-${n}',
    markerID: 'anchor:${routeID(n)}',
    sourceVersion: routeHash('from-$n'),
  ),
  to: MapContentRouteEndpoint(
    viewportPath:
        '/v1/me/map-content?west=-2.3&south=57&east=-1.9&north=57.4&zoom=17&sourceVersion=${routeHash('page')}&cursor=original-${n + 1}',
    markerID: 'anchor:${routeID(n + 1)}',
    sourceVersion: routeHash('to-$n'),
  ),
);
Map<String, Object?> routeJSON(
  MapContentRouteQuery q,
  DateTime now, {
  String status = 'AVAILABLE',
}) => {
  'schemaVersion': 'map-content-thread-route-v1',
  'viewerAccountId': routeID(900),
  'threadId': q.threadID,
  'fromMarkerId': q.from.markerID,
  'toMarkerId': q.to.markerID,
  'fromSourceVersion': q.from.sourceVersion,
  'toSourceVersion': q.to.sourceVersion,
  'status': status,
  'mode': status == 'AVAILABLE' ? 'walking' : '',
  'provider': status == 'AVAILABLE' ? 'mapbox' : '',
  'coordinateSystem': 'wgs84',
  'geometryKind': status == 'AVAILABLE' ? 'ROAD' : '',
  'segments': status == 'AVAILABLE'
      ? [
          [
            {'latitude': 57.1, 'longitude': -2.1},
            {'latitude': 57.15, 'longitude': -2.08},
            {'latitude': 57.2, 'longitude': -2.05},
          ],
          [
            {'latitude': 57.22, 'longitude': -2.04},
            {'latitude': 57.23, 'longitude': -2.02},
          ],
        ]
      : [],
  'distanceMeters': status == 'AVAILABLE' ? 350 : 0,
  'durationSeconds': status == 'AVAILABLE' ? 300 : 0,
  'sourceVersion': routeHash('route'),
  'observedAt': now.toIso8601String(),
  'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
};
MapContentRouteGeometry routeGeometry(
  MapContentRouteQuery q,
  DateTime now, {
  String status = 'AVAILABLE',
}) => MapContentRouteGeometry.decode(
  routeJSON(q, now, status: status),
  ownerID: routeID(900),
  query: q,
  now: now,
);

void main() {
  test(
    'strict route geometry preserves road bends and gaps and explicit saved transfer',
    () {
      final now = DateTime.utc(2026, 10, 19),
          q = routeQuery(DateTime.utc(2026, 10, 19));
      final value = routeGeometry(q, now);
      expect(value.segments.map((s) => s.length), [3, 2]);
      expect(value.segments.first[1].longitude, -2.08);
      expect(value.availableAt(now), isTrue);
      expect(value.availableAt(value.validUntil), isFalse);
      for (final status in ['BLOCKED', 'NOT_AVAILABLE']) {
        final v = routeGeometry(q, now, status: status);
        expect(v.segments, isEmpty);
        expect(v.availableAt(now), isFalse);
      }
      final transfer = routeJSON(q, now)
        ..['mode'] = 'train'
        ..['provider'] = 'saved-transfer'
        ..['geometryKind'] = 'TRANSFER'
        ..['distanceMeters'] = 0
        ..['durationSeconds'] = 0;
      expect(
        MapContentRouteGeometry.decode(
          transfer,
          ownerID: routeID(900),
          query: q,
          now: now,
        ).geometryKind,
        'TRANSFER',
      );
    },
  );
  test(
    'geometry rejects source identity mode CRS malformed gap and original endpoint changes',
    () {
      final now = DateTime.utc(2026, 10, 19),
          q = routeQuery(DateTime.utc(2026, 10, 19));
      final bad = <Map<String, Object?>>[
        routeJSON(q, now)..['viewerAccountId'] = routeID(901),
        routeJSON(q, now)..['fromSourceVersion'] = routeHash('changed'),
        routeJSON(q, now)..['coordinateSystem'] = 'gcj02',
        routeJSON(q, now)..['mode'] = 'train',
        routeJSON(q, now)..['provider'] = 'amap',
        routeJSON(q, now)..['mode'] = 'recommended',
        routeJSON(q, now)
          ..['segments'] = [
            [
              {'latitude': 57.1, 'longitude': -2.1},
            ],
          ],
        routeJSON(q, now)
          ..['segments'] = [
            [
              {'latitude': double.nan, 'longitude': -2.1},
              {'latitude': 57.2, 'longitude': -2.2},
            ],
          ],
        routeJSON(q, now)..['validUntil'] = now.toIso8601String(),
        routeJSON(q, now)..['unexpected'] = true,
        routeJSON(q, now, status: 'NOT_AVAILABLE')
          ..['segments'] = routeJSON(q, now)['segments'],
      ];
      for (final json in bad) {
        expect(
          () => MapContentRouteGeometry.decode(
            json,
            ownerID: routeID(900),
            query: q,
            now: now,
          ),
          throwsFormatException,
        );
      }
    },
  );
  test(
    'route API posts exact original page selectors and preserves cursor without provider coordinates',
    () async {
      final now = DateTime.now().toUtc(),
          q = routeQuery(DateTime.now().toUtc());
      http.Request? request;
      final api = OwnedMapAPI(
        apiBaseUrl: () => 'https://route.invalid',
        client: MockClient((r) async {
          request = r;
          return http.Response(
            jsonEncode({'data': routeJSON(q, now)}),
            200,
            headers: {
              'content-type': 'application/json',
              'cache-control': 'no-store',
            },
          );
        }),
      );
      final abort = Completer<void>();
      try {
        final value = await api.readThreadRoute(
          q,
          ownerID: routeID(900),
          authorization: 'Bearer synthetic-route',
          origin: 'https://route.invalid',
          current: () => true,
          now: () => now,
          abortTrigger: abort.future,
        );
        expect(request!.method, 'POST');
        expect(request!.url.path, '/v1/me/map-content/thread-route');
        expect(jsonDecode(request!.body), q.toJson());
        expect(request!.body, contains('original-1'));
        expect(jsonDecode(request!.body), isNot(contains('latitude')));
        expect(value.segments.length, 2);
      } finally {
        abort.complete();
        api.dispose();
      }
    },
  );
  test(
    'route missing mux is compatible but domain denial retains callback semantics',
    () async {
      final now = DateTime.now().toUtc(),
          q = routeQuery(DateTime.now().toUtc());
      var domain = false, global = 0, local = 0;
      final api = OwnedMapAPI(
        apiBaseUrl: () => 'https://route.invalid',
        onDenied: (_) => global++,
        client: MockClient(
          (_) async => domain
              ? http.Response(
                  jsonEncode({
                    'error': {'code': 'not_found'},
                  }),
                  404,
                  headers: {'content-type': 'application/json'},
                )
              : http.Response(
                  '404 page not found\n',
                  404,
                  headers: {'content-type': 'text/plain'},
                ),
        ),
      );
      Future<void> read() async {
        await api.readThreadRoute(
          q,
          ownerID: routeID(900),
          authorization: 'Bearer synthetic-route',
          origin: 'https://route.invalid',
          current: () => true,
          now: () => now,
          abortTrigger: Completer<void>().future,
          onReadDenied: (_) => local++,
        );
      }

      try {
        await expectLater(read(), throwsA(isA<OwnedMapReadFailure>()));
        expect(local, 0);
        expect(global, 0);
        domain = true;
        await expectLater(read(), throwsA(isA<OwnedMapReadFailure>()));
        expect(local, 1);
        expect(global, 0);
      } finally {
        api.dispose();
      }
    },
  );
}
