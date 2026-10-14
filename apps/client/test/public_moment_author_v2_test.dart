import 'dart:convert';
import 'dart:async';
import 'package:birdtie_client/src/workspace/public_moment_api.dart';
import 'package:birdtie_client/src/workspace/place_history_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:http/http.dart' as http;
import 'public_moment_api_test.dart'
    show publicMomentData, publicMomentV3Data, momentReply;
import 'place_history_controller_test.dart'
    show historyTestData, historyTestReply, historyTestPlace;
import 'entity_share_pending_store_test.dart' show shareTarget;

const publicAuthorTestID = '49100000-0000-4000-8000-000000000001';
Map<String, dynamic> publicMomentV2Data({bool hidden = false}) => {
  ...publicMomentData(),
  'schemaVersion': 'public-moment-v2',
  'authorId': hidden ? null : publicAuthorTestID,
  'authorPublicProfile': hidden
      ? null
      : {'accountId': publicAuthorTestID, 'displayName': '公开作者名字'},
};
Map<String, dynamic> historyV2Data({bool hidden = false}) {
  final d = jsonDecode(jsonEncode(historyTestData())) as Map<String, dynamic>;
  d['schemaVersion'] = 'place-social-history-v2';
  final row = (d['recentMoments'] as List).single as Map<String, dynamic>;
  row['authorId'] = hidden ? null : publicAuthorTestID;
  row['authorPublicProfile'] = hidden
      ? null
      : {'accountId': publicAuthorTestID, 'displayName': '公开作者名字'};
  return d;
}

void main() {
  test(
    'v3 media keeps public author opt-in independent nullable pair and photo parent revision',
    () async {
      final api = PublicMomentApi(
        authorizationHeader: () => null,
        apiBaseUrl: 'https://api.test',
        client: MockClient((r) async {
          expectSync(r.headers['X-Birdtie-Public-Moment-Media'], 'photo-v1');
          expectSync(r.url.queryParameters, {'include': 'public-author'});
          return momentReply({
            ...publicMomentV3Data(),
            'authorId': publicAuthorTestID,
            'authorPublicProfile': {
              'accountId': publicAuthorTestID,
              'displayName': '当前公开作者',
            },
          });
        }),
      );
      addTearDown(api.dispose);
      final value = await api.read(shareTarget, includePublicAuthor: true);
      expect(value.photo, isNotNull);
      expect(value.authorID, publicAuthorTestID);
      final hidden = PublicMoment.fromJson(
        publicMomentV3Data(),
        shareTarget,
        includePublicAuthor: true,
      );
      expect(hidden.photo, isNotNull);
      expect(hidden.authorPublicProfile, isNull);
      expect(
        () => PublicMoment.fromJson(
          {...publicMomentV3Data(), 'authorId': publicAuthorTestID},
          shareTarget,
          includePublicAuthor: true,
        ),
        throwsFormatException,
      );
    },
  );
  test('v2显式失效会话不回退匿名或v1请求', () async {
    var reads = 0;
    final api = PublicMomentApi(
      authorizationHeader: () => 'Bearer expired',
      apiBaseUrl: 'https://api.test',
      client: MockClient((r) async {
        reads++;
        expect(r.headers['Authorization'], 'Bearer expired');
        expect(r.url.queryParameters, {'include': 'public-author'});
        return http.Response('{}', 401);
      }),
    );
    addTearDown(api.dispose);
    await expectLater(
      api.read(shareTarget, includePublicAuthor: true),
      throwsStateError,
    );
    expect(reads, 1);
  });
  test('v2地点历史主体变化退役旧公开作者，迟到响应不覆盖当前隐藏作者', () async {
    var token = 'Bearer one', reads = 0;
    final old = Completer<http.Response>();
    final c = PlaceHistoryController(
      placeID: historyTestPlace,
      authorizationHeader: () => token,
      includePublicAuthor: true,
      apiBaseUrl: 'https://api.test',
      client: MockClient((r) async {
        expect(r.url.queryParameters, {'include': 'public-author'});
        if (++reads == 1) return old.future;
        return historyTestReply(historyV2Data(hidden: true));
      }),
    );
    addTearDown(c.dispose);
    final pending = c.refresh();
    token = 'Bearer two';
    expect(c.summary, isNull);
    await c.refresh();
    expect(c.summary!.moments.single.authorPublicProfile, isNull);
    old.complete(historyTestReply(historyV2Data()));
    await pending;
    expect(c.summary!.moments.single.authorID, isNull);
    expect(reads, 2);
  });
  test('单Moment作者opt-in请求与严格v2成对nullable读取', () async {
    final api = PublicMomentApi(
      authorizationHeader: () => null,
      apiBaseUrl: 'https://api.test',
      client: MockClient((r) async {
        expect(r.url.queryParameters, {'include': 'public-author'});
        return momentReply(publicMomentV2Data());
      }),
    );
    addTearDown(api.dispose);
    final value = await api.read(shareTarget, includePublicAuthor: true);
    expect(value.authorID, publicAuthorTestID);
    expect(value.authorPublicProfile!.accountID, publicAuthorTestID);
    expect(value.authorPublicProfile!.displayName, '公开作者名字');
    final hidden = PublicMoment.fromJson(
      publicMomentV2Data(hidden: true),
      shareTarget,
      includePublicAuthor: true,
    );
    expect(hidden.authorID, isNull);
    expect(hidden.authorPublicProfile, isNull);
  });
  test('v1与v2不能互相回退或忽略新增字段', () {
    expect(
      () => PublicMoment.fromJson(publicMomentV2Data(), shareTarget),
      throwsFormatException,
    );
    expect(
      () => PublicMoment.fromJson(
        publicMomentData(),
        shareTarget,
        includePublicAuthor: true,
      ),
      throwsFormatException,
    );
    expect(
      () => PlaceHistorySummary.fromJson(historyV2Data(), historyTestPlace),
      throwsFormatException,
    );
    expect(
      () => PlaceHistorySummary.fromJson(
        historyTestData(),
        historyTestPlace,
        includePublicAuthor: true,
      ),
      throwsFormatException,
    );
  });
  for (final mode in [
    'missing-id',
    'missing-profile',
    'id-only',
    'profile-only',
    'wrong-id',
    'bad-id',
    'blank-name',
    'private-field',
    'long-name',
    'wrong-type',
  ]) {
    test('单Moment与历史作者严格拒绝$mode', () {
      void mutate(Map<String, dynamic> d) {
        switch (mode) {
          case 'missing-id':
            d.remove('authorId');
          case 'missing-profile':
            d.remove('authorPublicProfile');
          case 'id-only':
            d['authorPublicProfile'] = null;
          case 'profile-only':
            d['authorId'] = null;
          case 'wrong-id':
            (d['authorPublicProfile'] as Map)['accountId'] = shareTarget;
          case 'bad-id':
            d['authorId'] = 'private-address';
          case 'blank-name':
            (d['authorPublicProfile'] as Map)['displayName'] = '  ';
          case 'private-field':
            (d['authorPublicProfile'] as Map)['memory'] = 'PRIVATE';
          case 'long-name':
            (d['authorPublicProfile'] as Map)['displayName'] = List.filled(
              641,
              'x',
            ).join();
          case 'wrong-type':
            d['authorPublicProfile'] = 'PRIVATE';
        }
      }

      final moment = publicMomentV2Data();
      mutate(moment);
      expect(
        () => PublicMoment.fromJson(
          moment,
          shareTarget,
          includePublicAuthor: true,
        ),
        throwsFormatException,
      );
      final history = historyV2Data();
      mutate((history['recentMoments'] as List).single as Map<String, dynamic>);
      expect(
        () => PlaceHistorySummary.fromJson(
          history,
          historyTestPlace,
          includePublicAuthor: true,
        ),
        throwsFormatException,
      );
    });
  }
  test('地点历史outer十字段不变，v2作者不扩大其他字段', () async {
    final c = PlaceHistoryController(
      placeID: historyTestPlace,
      authorizationHeader: () => null,
      includePublicAuthor: true,
      apiBaseUrl: 'https://api.test',
      client: MockClient((r) async {
        expect(r.url.path, '/v1/places/$historyTestPlace/social-history');
        expect(r.url.queryParameters, {'include': 'public-author'});
        return historyTestReply(historyV2Data());
      }),
    );
    addTearDown(c.dispose);
    await c.refresh();
    expect(c.summary!.moments.single.authorID, publicAuthorTestID);
    expect(
      c.summary!.moments.single.authorPublicProfile!.displayName,
      '公开作者名字',
    );
    expect(historyV2Data().length, 10);
    final hidden = PlaceHistorySummary.fromJson(
      historyV2Data(hidden: true),
      historyTestPlace,
      includePublicAuthor: true,
    );
    expect(hidden.moments.single.authorID, isNull);
    expect(hidden.moments.single.authorPublicProfile, isNull);
    final private = historyV2Data()..['privateCount'] = 9;
    expect(
      () => PlaceHistorySummary.fromJson(
        private,
        historyTestPlace,
        includePublicAuthor: true,
      ),
      throwsFormatException,
    );
    expect(
      utf8.encode(jsonEncode(hidden.moments.single.authorPublicProfile)),
      isNotEmpty,
    );
  });
}
