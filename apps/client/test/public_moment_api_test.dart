import 'dart:convert';
import 'package:birdtie_client/src/workspace/public_moment_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_share_pending_store_test.dart'
    show shareTarget, shareConversation;
import 'public_moment_photo_test.dart' show photoMetadata;

Map<String, dynamic> publicMomentV3Data({
  bool photo = true,
  int revision = 2,
  Duration lease = const Duration(seconds: 50),
}) {
  final at = DateTime.now().toUtc();
  return {
    ...publicMomentData(),
    'schemaVersion': 'public-moment-v3',
    'revision': revision,
    'authorId': null,
    'authorPublicProfile': null,
    'observedAt': at.toIso8601String(),
    'validUntil': at.add(lease).toIso8601String(),
    'photo': photo
        ? photoMetadata(id: shareTarget, revision: revision, at: at)
        : null,
  };
}

Map<String, dynamic> publicMomentData({String title = '本人明确公开的合成动态'}) => {
  'schemaVersion': 'public-moment-v1',
  'id': shareTarget,
  'title': title,
  'body': '中文公开正文',
  'placeId': shareConversation,
  'placeName': '本地合成公开地点',
  'cityId': 'aberdeen-gb',
  'revision': 2,
  'publishedAt': '2026-10-03T08:00:00.123456Z',
};
http.Response momentReply(Map<String, dynamic> data, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);
void main() {
  test(
    'media opt-in public v3 explicit parent current JPEG nullable author no private attachments',
    () async {
      final api = PublicMomentApi(
        authorizationHeader: () => null,
        apiBaseUrl: 'https://api.example',
        client: MockClient((r) async {
          expectSync(r.headers['X-Birdtie-Public-Moment-Media'], 'photo-v1');
          expectSync(r.url.hasQuery, isFalse);
          return momentReply(publicMomentV3Data());
        }),
      );
      addTearDown(api.dispose);
      final value = await api.read(shareTarget);
      expect(value.photo?.momentRevision, value.revision);
      expect(value.photo?.momentID, shareTarget);
      expect(value.authorID, isNull);
      expect(value.current, isTrue);
      for (final change in <Map<String, dynamic>>[
        {
          'photo': {...photoMetadata(id: shareTarget), 'momentRevision': 3},
        },
        {
          'photo': {
            ...photoMetadata(id: shareTarget),
            'privateImageId': shareConversation,
          },
        },
        {'authorId': shareTarget},
        {
          'validUntil': DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 61))
              .toIso8601String(),
        },
      ]) {
        expect(
          () => PublicMoment.fromJson({
            ...publicMomentV3Data(),
            ...change,
          }, shareTarget),
          throwsFormatException,
        );
      }
      expect(
        PublicMoment.fromJson(
          publicMomentV3Data(photo: false),
          shareTarget,
        ).photo,
        isNull,
      );
    },
  );
  test('公共单动态是九字段稳定详情，不含私人元数据', () async {
    final api = PublicMomentApi(
      authorizationHeader: () => null,
      apiBaseUrl: 'https://api.example',
      client: MockClient((r) async {
        expect(r.url.path, '/v1/moments/$shareTarget');
        expect(r.url.hasQuery, isFalse);
        expect(r.headers.containsKey('Authorization'), isFalse);
        return momentReply(publicMomentData());
      }),
    );
    final value = await api.read(shareTarget);
    expect(value.id, shareTarget);
    expect(value.placeID, shareConversation);
    expect(value.revision, 2);
    expect(value.publishedAt.microsecond, 456);
    api.dispose();
  });
  test('失效会话不降为匿名重试', () async {
    var calls = 0;
    final api = PublicMomentApi(
      authorizationHeader: () => 'Bearer expired',
      apiBaseUrl: 'https://api.example',
      client: MockClient((r) async {
        calls++;
        expect(r.headers['Authorization'], 'Bearer expired');
        return http.Response('{}', 401);
      }),
    );
    await expectLater(api.read(shareTarget), throwsStateError);
    expect(calls, 1);
  });
  for (final mode in [
    'private',
    'wrong-id',
    'bad-place',
    'bad-date',
    'revision',
    'long-title',
  ]) {
    test('公共动态闭合拒绝$mode', () {
      final value = publicMomentData();
      switch (mode) {
        case 'private':
          value['authorAccountId'] = shareTarget;
        case 'wrong-id':
          value['id'] = shareConversation;
        case 'bad-place':
          value['placeId'] = 'private-address';
        case 'bad-date':
          value['publishedAt'] = '2026-02-31T08:00:00Z';
        case 'revision':
          value['revision'] = 1;
        case 'long-title':
          value['title'] = List.filled(60, '中').join();
      }
      expect(
        () => PublicMoment.fromJson(value, shareTarget),
        throwsFormatException,
      );
    });
  }
}
