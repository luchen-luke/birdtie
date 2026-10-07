import 'dart:convert';
import 'package:birdtie_client/src/workspace/public_moment_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_share_pending_store_test.dart'
    show shareTarget, shareConversation;

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
