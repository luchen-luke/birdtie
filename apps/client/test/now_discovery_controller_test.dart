import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _bounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.3);

String _response(String cityID, List<double> bounds, {int total = 1}) =>
    jsonEncode({
      'data': {
        'cityId': cityID,
        'bounds': bounds,
        'status': total == 0 ? 'empty' : 'populated',
        'total': total,
        'categories': total == 0
            ? []
            : [
                {'code': 'badminton', 'count': total},
              ],
        'activities': total == 0
            ? []
            : [
                {
                  'id': 'b1700000-0000-4000-8000-000000000005',
                  'title': '公开活动',
                  'summary': '',
                  'startsAt': '2026-10-03T10:00:00Z',
                  'endsAt': '2026-10-03T12:00:00Z',
                  'timeZone': 'Europe/London',
                  'status': 'upcoming',
                  'source': {'label': 'Birdtie'},
                  'location': {
                    'coordinateSystem': 'wgs84',
                    'precision': 'point',
                    'latitude': 57.14,
                    'longitude': -2.1,
                  },
                },
              ],
        'truncated': false,
      },
    });

http.Response _okResponse(
  String cityID,
  List<double> bounds, {
  int total = 1,
}) => http.Response.bytes(
  utf8.encode(_response(cityID, bounds, total: total)),
  200,
);

void main() {
  test(
    'map failure fallback loads current city Activities from search API',
    () async {
      final controller = NowDiscoveryController(
        authorizationHeader: () => null,
        apiBaseUrl: 'http://127.0.0.1:3694',
        client: MockClient((request) async {
          expect(request.url.path, '/v1/cities/aberdeen-gb/activities');
          expect(request.url.queryParameters, {'category': ''});
          final pulse =
              jsonDecode(_response('aberdeen-gb', [-2.2, 57.1, -2, 57.3]))
                  as Map<String, dynamic>;
          final activities =
              (pulse['data'] as Map<String, dynamic>)['activities'];
          return http.Response.bytes(
            utf8.encode(jsonEncode({'data': activities})),
            200,
          );
        }),
      );
      await controller.loadCityActivities('aberdeen-gb');
      expect(controller.cityError, isNull);
      expect(controller.cityActivities.single.title, '公开活动');
      controller.dispose();
    },
  );

  test('Now discovery loads real pulse and stable Activity ID', () async {
    var requests = 0;
    final controller = NowDiscoveryController(
      authorizationHeader: () => null,
      apiBaseUrl: 'http://127.0.0.1:3694',
      client: MockClient((request) async {
        requests++;
        expect(request.url.queryParameters['bounds'], '-2.2,57.1,-2.0,57.3');
        return _okResponse('aberdeen-gb', [-2.2, 57.1, -2, 57.3]);
      }),
    );
    await controller.load('aberdeen-gb', _bounds);
    expect(controller.error, isNull);
    expect(controller.pulse?.total, 1);
    expect(controller.pulse?.categories.single.code, 'badminton');
    expect(
      controller.pulse?.activities.single.id,
      'b1700000-0000-4000-8000-000000000005',
    );
    await controller.load('aberdeen-gb', _bounds);
    expect(requests, 1);
    controller.dispose();
  });

  test('empty and failed requests are distinct', () async {
    var fail = false;
    final controller = NowDiscoveryController(
      authorizationHeader: () => null,
      apiBaseUrl: 'http://127.0.0.1:3694',
      client: MockClient(
        (_) async => fail
            ? http.Response('unavailable', 503)
            : _okResponse('aberdeen-gb', [-2.2, 57.1, -2, 57.3], total: 0),
      ),
    );
    await controller.load('aberdeen-gb', _bounds);
    expect(controller.pulse?.status, 'empty');
    expect(controller.pulse?.activities, isEmpty);
    fail = true;
    controller.clear();
    await controller.load('aberdeen-gb', _bounds);
    expect(controller.pulse, isNull);
    expect(controller.error, isNotNull);
    controller.dispose();
  });

  test('older response cannot replace a newer area', () async {
    final first = Completer<http.Response>();
    var requests = 0;
    final controller = NowDiscoveryController(
      authorizationHeader: () => null,
      apiBaseUrl: 'http://127.0.0.1:3694',
      client: MockClient((request) {
        requests++;
        if (requests == 1) return first.future;
        return Future.value(
          _okResponse('aberdeen-gb', [-2.1, 57.1, -2, 57.3], total: 0),
        );
      }),
    );
    final older = controller.load('aberdeen-gb', _bounds);
    const newerBounds = MapBounds(
      west: -2.1,
      south: 57.1,
      east: -2,
      north: 57.3,
    );
    await controller.load('aberdeen-gb', newerBounds);
    first.complete(_okResponse('aberdeen-gb', [-2.2, 57.1, -2, 57.3]));
    await older;
    expect(controller.pulse?.bounds, newerBounds);
    expect(controller.pulse?.total, 0);
    controller.dispose();
  });

  test('returning to displayed area cancels a newer pending area', () async {
    final pending = Completer<http.Response>();
    var requests = 0;
    final controller = NowDiscoveryController(
      authorizationHeader: () => null,
      apiBaseUrl: 'http://127.0.0.1:3694',
      client: MockClient((request) {
        requests++;
        if (requests == 1) {
          return Future.value(
            _okResponse('aberdeen-gb', [-2.2, 57.1, -2, 57.3]),
          );
        }
        return pending.future;
      }),
    );
    await controller.load('aberdeen-gb', _bounds);
    const other = MapBounds(west: -2.1, south: 57.1, east: -2, north: 57.3);
    final inFlight = controller.load('aberdeen-gb', other);
    expect(controller.loading, isTrue);
    await controller.load('aberdeen-gb', _bounds);
    expect(controller.loading, isFalse);
    pending.complete(_okResponse('aberdeen-gb', [-2.1, 57.1, -2, 57.3]));
    await inFlight;
    expect(controller.pulse?.bounds, _bounds);
    expect(requests, 2);
    controller.dispose();
  });
}
