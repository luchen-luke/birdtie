import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'map_layers_api_test.dart' as fixture;

Map<String, dynamic> mediaView(DateTime stamp, {int count = 241}) {
  final item = fixture.mapTestItem('PLACE');
  item['anchor']['placeId'] = fixture.mapTestID;
  item['placePhoto'] = {
    'entityType': 'place',
    'entityId': fixture.mapTestID,
    'version': 2,
    'thumbnailPath': '/v1/places/${fixture.mapTestID}/photos/2/content',
    'width': 320,
    'height': 180,
    'byteCount': 99,
    'sha256': List.filled(64, 'a').join(),
    'expiresAt': stamp.add(const Duration(seconds: 20)).toIso8601String(),
    'attribution': '隔离样例许可',
  };
  item['recentMoments'] = {
    'count': count,
    'checkedAt': stamp.toIso8601String(),
    'windowStart': stamp.subtract(const Duration(days: 30)).toIso8601String(),
    'validUntil': stamp.add(const Duration(seconds: 45)).toIso8601String(),
  };
  return {
    ...fixture.mapTestView(observedAt: stamp.toIso8601String()),
    'schemaVersion': 'typed-map-layers-v3',
    'items': [item],
  };
}

void main() {
  final stamp = DateTime.utc(2026, 10, 14, 12);
  TypedMapView decode(Map<String, dynamic> v, {DateTime? now}) =>
      TypedMapView.decode(
        v,
        'owned-city',
        fixture.mapTestBounds,
        false,
        now: now ?? stamp,
      );
  Map<String, dynamic> momentView() {
    final item = fixture.mapTestItem('MOMENT');
    item['momentPhoto'] = {
      'momentId': fixture.mapTestID,
      'momentRevision': 3,
      'photoId': fixture.mapTestPlace,
      'version': 2,
      'thumbnailPath':
          '/v1/moments/${fixture.mapTestID}/public-photo/3/${fixture.mapTestPlace}/2/content',
      'width': 320,
      'height': 180,
      'byteCount': 99,
      'sha256': List.filled(64, 'b').join(),
      'expiresAt': stamp.add(const Duration(seconds: 20)).toIso8601String(),
      'attribution': '仅本机隔离样例',
    };
    return {
      ...fixture.mapTestView(observedAt: stamp.toIso8601String()),
      'schemaVersion': 'typed-map-layers-v4',
      'items': [item],
    };
  }

  test(
    'v4 exact public Moment media retains original place anchor and author selector',
    () {
      final input = momentView(), before = decode(input);
      expect(before.items.single.momentPhoto!.momentID, fixture.mapTestID);
      expect(before.items.single.momentPhoto!.momentRevision, 3);
      expect(
        before.items.single.entity.momentPhoto!.photoID,
        fixture.mapTestPlace,
      );
      final expired = decode(
        input,
        now: stamp.add(const Duration(seconds: 21)),
      );
      expect(expired.items.single.momentPhoto, isNull);
      expect(expired.items.single.mapID, before.items.single.mapID);
      expect(expired.items.single.latitude, before.items.single.latitude);
      expect(expired.validUntil, before.validUntil);
      final author = {
        ...input,
        'scope': 'PUBLIC_AUTHOR',
        'authorId': fixture.mapTestPlace,
      };
      expect(
        TypedMapView.decode(
          author,
          'owned-city',
          fixture.mapTestBounds,
          false,
          authorID: fixture.mapTestPlace,
          now: stamp,
        ).items.single.momentPhoto,
        isNotNull,
      );
      input['validUntil'] = stamp
          .add(const Duration(seconds: 10))
          .toIso8601String();
      expect(decode(input).items.single.momentPhoto, isNotNull);
    },
  );
  for (final mode in [
    'v1',
    'v2',
    'v3',
    'wrong-moment',
    'wrong-revision-path',
    'wrong-photo-id',
    'foreign-url',
    'extra-private-field',
    'place-kind',
    'private-scope',
  ]) {
    test(
      'v4 public Moment photo rejects $mode without guessing another source',
      () {
        final v = momentView(),
            item = v['items'][0] as Map,
            photo = item['momentPhoto'] as Map;
        switch (mode) {
          case 'v1':
            v['schemaVersion'] = 'typed-map-layers-v1';
          case 'v2':
            v['schemaVersion'] = 'typed-map-layers-v2';
          case 'v3':
            v['schemaVersion'] = 'typed-map-layers-v3';
          case 'wrong-moment':
            photo['momentId'] = fixture.mapTestPlace;
          case 'wrong-revision-path':
            photo['momentRevision'] = 4;
          case 'wrong-photo-id':
            photo['photoId'] = fixture.mapTestID;
          case 'foreign-url':
            photo['thumbnailPath'] =
                'https://outside.invalid/private-original.jpg';
          case 'extra-private-field':
            photo['exif'] = {};
          case 'place-kind':
            item.addAll(fixture.mapTestItem('PLACE'));
          case 'private-scope':
            v['scope'] = 'SELF_PRIVATE';
        }
        expect(() => decode(v), throwsFormatException);
      },
    );
  }
  test('v3 exact Place photo plus full count above all excerpt limits', () {
    final v = decode(mediaView(stamp));
    expect(v.items.single.placePhoto!.ref.id, fixture.mapTestID);
    expect(v.items.single.recentMoments!.count, 241);
    expect(v.items.single.entity.id, 'place:${fixture.mapTestID}');
    expect(
      v.items.single.entity.recentMoments!.labelAt(stamp),
      '近30天 · 241条动态',
    );
  });
  test(
    'transport expiry independently removes annotations and keeps same point lease',
    () {
      final input = mediaView(stamp);
      final before = decode(input);
      final photoExpired = decode(
        input,
        now: stamp.add(const Duration(seconds: 25)),
      );
      expect(photoExpired.items.single.placePhoto, isNull);
      expect(photoExpired.items.single.recentMoments!.count, 241);
      final bothExpired = decode(
        input,
        now: stamp.add(const Duration(seconds: 50)),
      );
      expect(bothExpired.items.single.recentMoments, isNull);
      expect(bothExpired.validUntil, before.validUntil);
      expect(bothExpired.items.single.mapID, before.items.single.mapID);
      expect(bothExpired.items.single.latitude, before.items.single.latitude);
    },
  );
  test('zero has no label and missing remains unknown', () {
    expect(
      decode(
        mediaView(stamp, count: 0),
      ).items.single.recentMoments!.labelAt(stamp),
      isNull,
    );
    final missing = mediaView(stamp);
    (missing['items'][0] as Map).remove('recentMoments');
    expect(decode(missing).items.single.recentMoments, isNull);
  });
  test(
    'photo lease can outlast an independently shortened base and count remains bounded by base',
    () {
      final v = mediaView(stamp);
      v['validUntil'] = stamp
          .add(const Duration(seconds: 10))
          .toIso8601String();
      v['items'][0]['recentMoments']['validUntil'] = v['validUntil'];
      expect(decode(v).items.single.placePhoto, isNotNull);
      v['items'][0]['recentMoments']['validUntil'] = stamp
          .add(const Duration(seconds: 11))
          .toIso8601String();
      expect(() => decode(v), throwsFormatException);
    },
  );
  test(
    'checkedAt follows canonical PG snapshot frame and cannot precede observedAt',
    () {
      final v = mediaView(stamp);
      final checked = stamp.add(const Duration(milliseconds: 5));
      v['items'][0]['recentMoments']['checkedAt'] = checked.toIso8601String();
      v['items'][0]['recentMoments']['windowStart'] = checked
          .subtract(const Duration(days: 30))
          .toIso8601String();
      expect(decode(v).items.single.recentMoments!.checkedAt, checked);
      final prior = stamp.subtract(const Duration(milliseconds: 5));
      v['items'][0]['recentMoments']['checkedAt'] = prior.toIso8601String();
      v['items'][0]['recentMoments']['windowStart'] = prior
          .subtract(const Duration(days: 30))
          .toIso8601String();
      expect(() => decode(v), throwsFormatException);
    },
  );
  for (final mode in [
    'target',
    'path',
    'kind',
    'digest',
    'size',
    'lease',
    'extra',
    'window',
    'negative',
    'countLease',
    'legacy',
  ]) {
    test('v3 closed source rejects $mode rather than fetch guessed pixels', () {
      final v = mediaView(stamp), item = v['items'][0] as Map;
      final photo = item['placePhoto'] as Map,
          count = item['recentMoments'] as Map;
      switch (mode) {
        case 'target':
          photo['entityId'] = fixture.mapTestPlace;
        case 'path':
          photo['thumbnailPath'] = 'https://other.test/private.jpg';
        case 'kind':
          item.addAll(fixture.mapTestItem('MOMENT'));
        case 'digest':
          photo['sha256'] = 'bad';
        case 'size':
          photo['width'] = 321;
        case 'lease':
          photo['expiresAt'] = stamp
              .add(const Duration(seconds: 62))
              .toIso8601String();
        case 'extra':
          photo['url'] = 'https://other.test/private';
        case 'window':
          count['windowStart'] = stamp.toIso8601String();
        case 'negative':
          count['count'] = -1;
        case 'countLease':
          count['validUntil'] = stamp
              .add(const Duration(seconds: 61))
              .toIso8601String();
        case 'legacy':
          v['schemaVersion'] = 'typed-map-layers-v1';
      }
      expect(() => decode(v), throwsFormatException);
    });
  }
  test('v3 matches 300 rune names without changing canonical identifiers', () {
    final v = mediaView(stamp);
    v['items'][0]['title'] = List.filled(300, '场').join();
    v['items'][0]['anchor']['label'] = List.filled(300, '地').join();
    expect(decode(v).items.single.title.runes.length, 300);
    v['items'][0]['title'] += '地';
    expect(() => decode(v), throwsFormatException);
  });
}
