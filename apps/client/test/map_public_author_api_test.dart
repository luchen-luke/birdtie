import 'dart:convert';

import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'map_layers_api_test.dart' as legacy;

const authorID = '27000000-0000-4000-8000-000000000011';
const otherAuthorID = '27000000-0000-4000-8000-000000000012';

Map<String, dynamic> authorView({int count = 1, bool truncated = false}) => {
  'schemaVersion': 'typed-map-layers-v2',
  'cityId': 'owned-city',
  'scope': 'PUBLIC_AUTHOR',
  'authorId': authorID,
  'observedAt': '2026-10-09T03:23:14.659932Z',
  'validUntil': '2026-10-09T03:25:14.659932Z',
  'truncated': truncated,
  'items': [
    for (var n = 1; n <= count; n++)
      {
        ...legacy.mapTestItem('MOMENT'),
        'id': 'f0000000-0000-4000-8000-${n.toRadixString(16).padLeft(12, '0')}',
        'entityRef': {
          'type': 'MOMENT',
          'id':
              'f0000000-0000-4000-8000-${n.toRadixString(16).padLeft(12, '0')}',
        },
        'detailRef': {
          'type': 'MOMENT',
          'id':
              'f0000000-0000-4000-8000-${n.toRadixString(16).padLeft(12, '0')}',
        },
      },
  ],
};

TypedMapView decodeAuthor(
  dynamic raw, {
  String? author = authorID,
  bool private = false,
}) => TypedMapView.decode(
  raw,
  'owned-city',
  legacy.mapTestBounds,
  private,
  authorID: author,
);

http.Response frame(dynamic raw, {int status = 200}) => http.Response(
  jsonEncode({'data': raw}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  test(
    'explicit public author accepts exact v2 and original Moment identity',
    () {
      final view = decodeAuthor(authorView());
      expect(view.authorID, authorID);
      expect(view.scope, 'PUBLIC_AUTHOR');
      expect(view.items.single.kind, 'MOMENT');
      expect(view.items.single.entity.kind, MapEntityKind.moment);
      expect(view.items.single.entity.id, view.items.single.mapID);
      expect(view.items.single.detailType, 'MOMENT');
      expect(view.items.single.detailID, view.items.single.id);
      expect(view.items.single.placeID, legacy.mapTestPlace);
      expect(view.items.single.sourceVersion, 'specific-version-1');
      expect(view.observedAt, DateTime.utc(2026, 10, 9, 3, 23, 14, 659, 932));
      expect(
        view.validUntil.difference(view.observedAt),
        const Duration(minutes: 2),
      );
      expect(() => view.items.add(view.items.single), throwsUnsupportedError);
    },
  );

  test('empty author is a typed empty set with target retained', () {
    final view = decodeAuthor(authorView(count: 0));
    expect(view.authorID, authorID);
    expect(view.scope, 'PUBLIC_AUTHOR');
    expect(view.items, isEmpty);
    expect(view.truncated, false);
  });

  test('thirty typed Moments with truncated keeps original bounded set', () {
    final view = decodeAuthor(authorView(count: 30, truncated: true));
    expect(view.items, hasLength(30));
    expect(view.items.map((i) => i.mapID).toSet(), hasLength(30));
    expect(view.truncated, true);
    expect(view.authorID, authorID);
  });

  for (final mode in [
    'missingAuthor',
    'extraOwner',
    'extraProfile',
    'otherAuthor',
    'invalidAuthor',
    'emptyAuthor',
    'nullAuthor',
    'schemaV1',
    'otherSchema',
    'publicScope',
    'privateScope',
    'otherCity',
    'missingLease',
    'truncatedText',
    'itemsNull',
    'itemsObject',
    'overBound',
    'duplicate',
    'leaseZero',
    'leaseBeyondTwoMinutes',
    'invalidDate',
    'invalidOffset',
  ]) {
    test('public author exact target and envelope reject $mode', () {
      final raw = authorView();
      switch (mode) {
        case 'missingAuthor':
          raw.remove('authorId');
        case 'extraOwner':
          raw['ownerPermissions'] = {'readPrivate': true};
        case 'extraProfile':
          raw['profile'] = {'privateNotes': 'not accepted'};
        case 'otherAuthor':
          raw['authorId'] = otherAuthorID;
        case 'invalidAuthor':
          raw['authorId'] = 'not-a-uuid';
        case 'emptyAuthor':
          raw['authorId'] = '';
        case 'nullAuthor':
          raw['authorId'] = null;
        case 'schemaV1':
          raw['schemaVersion'] = 'typed-map-layers-v1';
        case 'otherSchema':
          raw['schemaVersion'] = 'typed-map-layers-v3';
        case 'publicScope':
          raw['scope'] = 'PUBLIC';
        case 'privateScope':
          raw['scope'] = 'SELF_PRIVATE';
        case 'otherCity':
          raw['cityId'] = 'other-city';
        case 'missingLease':
          raw.remove('validUntil');
        case 'truncatedText':
          raw['truncated'] = 'true';
        case 'itemsNull':
          raw['items'] = null;
        case 'itemsObject':
          raw['items'] = {};
        case 'overBound':
          raw['items'] = authorView(count: 31)['items'];
        case 'duplicate':
          (raw['items'] as List).add((raw['items'] as List).single);
        case 'leaseZero':
          raw['validUntil'] = raw['observedAt'];
        case 'leaseBeyondTwoMinutes':
          raw['validUntil'] = '2026-10-09T03:25:14.659933Z';
        case 'invalidDate':
          raw['observedAt'] = '2026-13-09T03:23:14Z';
        case 'invalidOffset':
          raw['observedAt'] = '2026-10-09T03:23:14+08:60';
      }
      expect(() => decodeAuthor(raw), throwsFormatException);
    });
  }

  for (final kind in [
    'PLACE',
    'ACTIVITY',
    'ORGANIZATION',
    'BUSINESS',
    'OPPORTUNITY',
    'PERSON',
  ]) {
    test('public author does not broaden to $kind', () {
      final raw = authorView();
      raw['items'] = [legacy.mapTestItem(kind)];
      expect(() => decodeAuthor(raw), throwsFormatException);
    });
  }

  for (final mode in [
    'extraBody',
    'extraAuthorProfile',
    'entityType',
    'entityID',
    'detailType',
    'detailID',
    'invalidID',
    'noPlace',
    'coordinates',
    'precision',
    'south',
    'north',
    'west',
    'east',
    'nonfinite',
    'label',
    'sourceEmpty',
    'sourceLong',
    'sourceNull',
    'title',
  ]) {
    test('author Moment retains original strict item gate $mode', () {
      final raw = authorView();
      final item = (raw['items'] as List).single as Map<String, dynamic>;
      final anchor = item['anchor'] as Map;
      switch (mode) {
        case 'extraBody':
          item['body'] = 'not part of map';
        case 'extraAuthorProfile':
          item['authorPublicProfile'] = {'accountId': authorID};
        case 'entityType':
          (item['entityRef'] as Map)['type'] = 'PLACE';
        case 'entityID':
          (item['entityRef'] as Map)['id'] = otherAuthorID;
        case 'detailType':
          (item['detailRef'] as Map)['type'] = 'PLACE';
        case 'detailID':
          (item['detailRef'] as Map)['id'] = otherAuthorID;
        case 'invalidID':
          item['id'] = 'not-a-uuid';
          (item['entityRef'] as Map)['id'] = item['id'];
        case 'noPlace':
          anchor['placeId'] = '';
        case 'coordinates':
          anchor['coordinateSystem'] = 'gcj02';
        case 'precision':
          anchor['precision'] = 'area';
        case 'south':
          anchor['latitude'] = 57.09;
        case 'north':
          anchor['latitude'] = 57.31;
        case 'west':
          anchor['longitude'] = -2.21;
        case 'east':
          anchor['longitude'] = -1.99;
        case 'nonfinite':
          anchor['latitude'] = double.nan;
        case 'label':
          anchor['label'] = ' 公开场地';
        case 'sourceEmpty':
          item['sourceVersion'] = '';
        case 'sourceLong':
          item['sourceVersion'] = List.filled(181, '1').join();
        case 'sourceNull':
          item['sourceVersion'] = null;
        case 'title':
          item['title'] = ' 标题';
      }
      expect(() => decodeAuthor(raw), throwsFormatException);
    });
  }

  test('item direct decode cannot broaden public author to other kind', () {
    expect(
      () => TypedMapItem.decode(
        legacy.mapTestItem('PLACE'),
        'PUBLIC_AUTHOR',
        legacy.mapTestBounds,
      ),
      throwsFormatException,
    );
  });

  test(
    'unselected v1 remains seven fields and null author with four positional args',
    () {
      final raw = legacy.mapTestView();
      expect(raw, hasLength(7));
      final public = TypedMapView.decode(
        raw,
        'owned-city',
        legacy.mapTestBounds,
        false,
      );
      final private = TypedMapView.decode(
        legacy.mapTestView(private: true),
        'owned-city',
        legacy.mapTestBounds,
        true,
      );
      expect(public.authorID, null);
      expect(private.authorID, null);
      expect(public.items, hasLength(5));
      expect(private.items.single.kind, 'OPPORTUNITY');
      expect(
        () => TypedMapView.decode(
          authorView(),
          'owned-city',
          legacy.mapTestBounds,
          false,
        ),
        throwsFormatException,
      );
      raw['authorId'] = authorID;
      expect(
        () =>
            TypedMapView.decode(raw, 'owned-city', legacy.mapTestBounds, false),
        throwsFormatException,
      );
    },
  );

  for (final author in [
    '',
    'not-a-uuid',
    '00000000-0000-0000-0000-000000000000',
    ' $authorID',
    'ABCDEF00-0000-4000-8000-000000000011',
  ]) {
    test('invalid opt-in author cannot issue HTTP [$author]', () async {
      var calls = 0;
      final api = MapLayersApi(
        client: MockClient((_) async {
          calls++;
          return frame(authorView());
        }),
        apiBaseUrl: 'http://owned.test',
      );
      await expectLater(
        api.read('owned-city', legacy.mapTestBounds, authorID: author),
        throwsFormatException,
      );
      expect(calls, 0);
      expect(
        () => decodeAuthor(authorView(), author: author),
        throwsFormatException,
      );
      api.dispose();
    });
  }

  test('author plus private or invalid bbox cannot issue request', () async {
    var calls = 0;
    final api = MapLayersApi(
      client: MockClient((_) async {
        calls++;
        return frame(authorView());
      }),
      apiBaseUrl: 'http://owned.test',
    );
    await expectLater(
      api.read(
        'owned-city',
        legacy.mapTestBounds,
        private: true,
        authorID: authorID,
        authorization: 'Bearer fixture',
      ),
      throwsFormatException,
    );
    expect(
      () => decodeAuthor(authorView(), private: true),
      throwsFormatException,
    );
    const invalid = MapBounds(west: 1, south: 2, east: 0, north: 3);
    await expectLater(
      api.read('owned-city', invalid, authorID: authorID),
      throwsFormatException,
    );
    expect(
      () => TypedMapView.decode(
        authorView(),
        'owned-city',
        invalid,
        false,
        authorID: authorID,
      ),
      throwsFormatException,
    );
    expect(calls, 0);
    api.dispose();
  });

  for (final signed in [false, true]) {
    test(
      'explicit author GET keeps original public route with exactly six params signed=$signed',
      () async {
        final api = MapLayersApi(
          client: MockClient((r) async {
            expect(r.method, 'GET');
            expect(r.url.path, '/v1/cities/owned-city/map-layers');
            expect(r.body, isEmpty);
            expect(r.url.queryParameters, {
              'west': '-2.2',
              'south': '57.1',
              'east': '-2.0',
              'north': '57.3',
              'scope': 'public-author',
              'authorId': authorID,
            });
            expect(
              r.headers['Authorization'],
              signed ? 'Bearer fixture' : null,
            );
            expect(
              r.headers.keys.any(
                (k) =>
                    k.toLowerCase().contains('workspace') ||
                    k.toLowerCase().contains('owner'),
              ),
              false,
            );
            return frame(authorView());
          }),
          apiBaseUrl: 'http://owned.test/',
        );
        final view = await api.read(
          'owned-city',
          legacy.mapTestBounds,
          authorID: authorID,
          authorization: signed ? 'Bearer fixture' : null,
        );
        expect(view.authorID, authorID);
        expect(view.items.single.kind, 'MOMENT');
        api.dispose();
      },
    );
  }

  for (final mode in [
    'otherAuthor',
    'legacy',
    'extraEnvelope',
    'http404',
    'http401',
    'http409',
    'http503',
  ]) {
    test(
      'API adapter decode fails closed for $mode without fallback',
      () async {
        var calls = 0;
        final raw = authorView();
        if (mode == 'otherAuthor') raw['authorId'] = otherAuthorID;
        final status = mode.startsWith('http')
            ? int.parse(mode.substring(4))
            : 200;
        final api = MapLayersApi(
          client: MockClient((_) async {
            calls++;
            if (mode == 'extraEnvelope')
              return http.Response(
                jsonEncode({'data': raw, 'owner': authorID}),
                200,
                headers: {'content-type': 'application/json; charset=utf-8'},
              );
            return frame(
              mode == 'legacy' ? legacy.mapTestView() : raw,
              status: status,
            );
          }),
          apiBaseUrl: 'http://owned.test',
        );
        await expectLater(
          api.read('owned-city', legacy.mapTestBounds, authorID: authorID),
          status == 200
              ? throwsFormatException
              : throwsA(
                  isA<MapLayersReadFailure>().having(
                    (e) => e.statusCode,
                    'status',
                    status,
                  ),
                ),
        );
        expect(calls, 1);
        api.dispose();
      },
    );
  }

  test('old API routes keep exactly four parameters without opt-in', () async {
    final paths = <String>[];
    final api = MapLayersApi(
      client: MockClient((r) async {
        paths.add(r.url.path);
        expect(r.url.queryParameters.keys.toSet(), {
          'west',
          'south',
          'east',
          'north',
        });
        final private = r.url.path.contains('/me/');
        return frame(legacy.mapTestView(private: private));
      }),
      apiBaseUrl: 'http://owned.test',
    );
    expect((await api.read('owned-city', legacy.mapTestBounds)).authorID, null);
    expect(
      (await api.read(
        'owned-city',
        legacy.mapTestBounds,
        private: true,
        authorization: 'Bearer fixture',
      )).authorID,
      null,
    );
    expect(paths, [
      '/v1/cities/owned-city/map-layers',
      '/v1/me/cities/owned-city/map-opportunities',
    ]);
    api.dispose();
  });
}
