import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';

const mapTestID = '12345678-1234-1234-1234-123456789abc';
const mapTestPlace = '22345678-1234-1234-1234-123456789abc';
const mapTestBounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.3);
Map<String, dynamic> mapTestItem(String kind) {
  final id = kind == 'OPPORTUNITY' ? '$mapTestPlace:$mapTestID' : mapTestID;
  return {
    'kind': kind,
    'id': id,
    'title': '真实来源合成标题',
    'entityRef': {'type': kind, 'id': id},
    'detailRef': {
      'type': kind == 'OPPORTUNITY' ? 'ACTIVITY' : kind,
      'id': kind == 'OPPORTUNITY' ? mapTestID : id,
    },
    'anchor': {
      'placeId': kind == 'ORGANIZATION' ? '' : mapTestPlace,
      'label': '公开场地',
      'coordinateSystem': 'wgs84',
      'precision': 'point',
      'latitude': 57.2,
      'longitude': -2.1,
    },
    'sourceVersion': 'specific-version-1',
  };
}

Map<String, dynamic> mapTestView({
  bool private = false,
  int milliseconds = 120000,
}) => {
  'schemaVersion': 'typed-map-layers-v1',
  'cityId': 'owned-city',
  'scope': private ? 'SELF_PRIVATE' : 'PUBLIC',
  'observedAt': '2026-10-04T12:00:00+08:00',
  'validUntil': DateTime.utc(
    2026,
    10,
    4,
    4,
  ).add(Duration(milliseconds: milliseconds)).toIso8601String(),
  'truncated': false,
  'items': [
    for (final k
        in (private
            ? ['OPPORTUNITY']
            : mapLayerKinds.where((e) => e != 'OPPORTUNITY')))
      mapTestItem(k),
  ],
};
void main() {
  test('six closed types real anchor original detail IDs and UTC offset', () {
    final v = TypedMapView.decode(
      mapTestView(),
      'owned-city',
      mapTestBounds,
      false,
    );
    expect(v.items.length, 5);
    expect(v.observedAt, DateTime.utc(2026, 10, 4, 4));
    for (final i in v.items) {
      expect(i.entity.id, i.mapID);
      expect(i.detailID, i.id);
    }
    final own = TypedMapView.decode(
      mapTestView(private: true),
      'owned-city',
      mapTestBounds,
      true,
    );
    expect(own.items.single.detailID, mapTestID);
    expect(own.items.single.entity.kind, MapEntityKind.opportunity);
  });
  for (final mode in [
    'body',
    'privatePublic',
    'duplicate',
    'unknown',
    'noPoint',
    'noPlace',
    'invalidDate',
    'invalidOffset',
    'expiry',
    'infinite',
    'city',
    'detail',
    'extraEnvelope',
  ]) {
    test('strict native wire rejects $mode', () async {
      final v = mapTestView();
      final items = v['items'] as List;
      switch (mode) {
        case 'body':
          (items[0] as Map)['body'] = 'private-body';
        case 'privatePublic':
          items.add(mapTestItem('OPPORTUNITY'));
        case 'duplicate':
          items.add(items[0]);
        case 'unknown':
          (items[0] as Map)['kind'] = 'PERSON';
        case 'noPoint':
          (items[0]['anchor'] as Map)['precision'] = 'area';
        case 'noPlace':
          (items[0]['anchor'] as Map)['placeId'] = '';
        case 'invalidDate':
          v['observedAt'] = '2026-13-04T12:00:00Z';
        case 'invalidOffset':
          v['observedAt'] = '2026-10-04T12:00:00+08:60';
        case 'expiry':
          v['validUntil'] = v['observedAt'];
        case 'infinite':
          v['validUntil'] = '2099-10-04T12:00:00Z';
        case 'city':
          v['cityId'] = 'other';
        case 'detail':
          (items[0]['detailRef'] as Map)['id'] = mapTestPlace;
      }
      final api = MapLayersApi(
        client: MockClient(
          (r) async => http.Response(
            jsonEncode({
              'data': v,
              if (mode == 'extraEnvelope') 'private': 'secret',
            }),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          ),
        ),
        apiBaseUrl: 'http://owned.test',
      );
      await expectLater(
        api.read('owned-city', mapTestBounds),
        throwsFormatException,
      );
      api.dispose();
    });
  }
  test(
    'bounded GET uses original route no body no organization or owner',
    () async {
      final api = MapLayersApi(
        client: MockClient((r) async {
          expect(r.method, 'GET');
          expect(r.url.path, '/v1/me/cities/owned-city/map-opportunities');
          expect(r.body, '');
          expect(r.url.queryParameters.keys.toSet(), {
            'west',
            'south',
            'east',
            'north',
          });
          expect(r.headers['Authorization'], 'Bearer owner');
          return http.Response(
            jsonEncode({'data': mapTestView(private: true)}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }),
        apiBaseUrl: 'http://owned.test',
      );
      await api.read(
        'owned-city',
        mapTestBounds,
        private: true,
        authorization: 'Bearer owner',
      );
      api.dispose();
    },
  );
}
