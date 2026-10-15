import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
import 'package:birdtie_client/src/workspace/public_moment_history_api.dart';
import 'package:birdtie_client/src/workspace/public_moment_history_controller.dart';
import 'package:birdtie_client/src/workspace/public_moment_history_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const owner = '11111111-1111-4111-8111-111111111111';
String id(int n) => '22222222-2222-4222-8222-${n.toString().padLeft(12, '0')}';
http.Response response(Object data, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);

Map<String, dynamic> publicPage(
  DateTime snapshot, {
  int start = 0,
  int count = 20,
  String scope = 'city',
  String? next = 'next-20',
}) {
  final observed = DateTime.now().toUtc(),
      until = DateTime.now().toUtc().add(const Duration(seconds: 50));
  return {
    'schemaVersion': 'public-moment-history-v1',
    'scope': scope,
    'cityId': 'aberdeen-gb',
    'authorId': '',
    'from': '',
    'to': '',
    'observedAt': observed.toIso8601String(),
    'snapshotAt': snapshot.toIso8601String(),
    'validUntil': until.toIso8601String(),
    'nextCursor': next,
    'refreshCursor': 'refresh-$start',
    'items': [
      for (var n = start; n < start + count; n++)
        {
          'schemaVersion': 'public-moment-v4',
          'id': id(n),
          'title': '合成公开动态 $n',
          'body': '合成正常中文正文',
          'cityId': 'aberdeen-gb',
          'cityName': '阿伯丁',
          'locationPrecision': 'city',
          'placeId': '',
          'placeName': '',
          'revision': 2,
          'publishedAt': snapshot
              .subtract(Duration(days: n + 1))
              .toIso8601String(),
          'authorId': null,
          'authorPublicProfile': null,
          'observedAt': observed.toIso8601String(),
          'validUntil': until.toIso8601String(),
          'photo': null,
        },
    ],
  };
}

Map<String, dynamic> ownPage(DateTime snapshot, int start, int count) => {
  'schemaVersion': 'owner-moment-history-v1',
  'status': 'active',
  'observedAt': DateTime.now().toUtc().toIso8601String(),
  'snapshotAt': snapshot.toIso8601String(),
  'validUntil': DateTime.now()
      .toUtc()
      .add(const Duration(seconds: 50))
      .toIso8601String(),
  'nextCursor': start + count < 105 ? 'next-${start + count}' : null,
  'refreshCursor': 'refresh-$start',
  'items': [
    for (var n = start; n < start + count; n++)
      {
        'id': id(n),
        'authorAccountId': owner,
        'visibility': 'private',
        'cityId': 'aberdeen-gb',
        'title': '合成本人草稿 $n',
        'body': '本人正文',
        'timePrecision': 'unknown',
        'locationPrecision': 'city',
        'status': 'draft',
        'revision': 1,
        'createdAt': snapshot
            .subtract(Duration(seconds: n + 1))
            .toIso8601String(),
        'updatedAt': snapshot
            .subtract(Duration(seconds: n + 1))
            .toIso8601String(),
      },
  ],
};

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'owner history reaches 105 without changing the old list endpoint',
    () async {
      final snapshot = DateTime.now().toUtc(), paths = <String>[];
      final client = MockClient((r) async {
        paths.add(r.url.path);
        final cursor = r.url.queryParameters['cursor'];
        final start = cursor == null ? 0 : int.parse(cursor.split('-').last);
        return response(ownPage(snapshot, start, (105 - start).clamp(0, 20)));
      });
      final controller = PrivateMomentController(
        authorizationHeader: () => 'Bearer synthetic',
        ownerID: () => owner,
        client: client,
        apiBaseUrl: 'https://unit.invalid',
        pendingStore: MemoryMomentControlPendingStore(),
      );
      addTearDown(controller.dispose);
      await controller.enableHistory();
      while (controller.canLoadMoreHistory) {
        await controller.loadMoreHistory();
      }
      expect(controller.historyError, isNull);
      expect(controller.historyMoments.length, 105);
      expect(controller.historyMoments.map((m) => m.id).toSet().length, 105);
      expect(paths.every((p) => p == '/v1/me/moments/history'), isTrue);
    },
  );

  test('history status filtering keeps the current Now draft cache', () async {
    final snapshot = DateTime.now().toUtc();
    final client = MockClient((r) async {
      if (r.url.path == '/v1/me/moments')
        return response(ownPage(snapshot, 0, 1)['items']);
      final filtered = r.url.queryParameters['status'] == 'published';
      final page = ownPage(snapshot, 0, filtered ? 0 : 1);
      page['status'] = filtered ? 'published' : 'active';
      page['nextCursor'] = null;
      return response(page);
    });
    final controller = PrivateMomentController(
      authorizationHeader: () => 'Bearer synthetic',
      ownerID: () => owner,
      client: client,
      apiBaseUrl: 'https://unit.invalid',
      pendingStore: MemoryMomentControlPendingStore(),
    );
    addTearDown(controller.dispose);
    await controller.refresh();
    await controller.enableHistory();
    expect(controller.moments.single.id, id(0));
    expect(controller.historyMoments.length, 1);
    await controller.setHistoryStatus('published');
    expect(controller.historyError, isNull);
    expect(controller.historyMoments, isEmpty);
    expect(controller.moments.single.id, id(0));
    expect(controller.currentOwnedVersion(controller.moments.single), isTrue);
  });

  test(
    'public query switch retires a late city page and current relation cursor',
    () async {
      final changes = ValueNotifier(0),
          delayed = Completer<http.Response>(),
          refreshing = Completer<http.Response>(),
          snapshot = DateTime.now().toUtc();
      addTearDown(changes.dispose);
      var reads = 0;
      final client = MockClient((r) async {
        reads++;
        if (reads == 1) return delayed.future;
        if (reads == 3) return refreshing.future;
        expect(r.url.queryParameters['scope'], 'following');
        return response(
          publicPage(snapshot, scope: 'following', count: 1, next: null),
        );
      });
      final controller = PublicMomentHistoryController(
        api: PublicMomentHistoryApi(
          authorizationHeader: () => 'Bearer synthetic',
          client: client,
          apiBaseUrl: 'https://unit.invalid',
        ),
        query: const PublicMomentHistoryQuery(
          scope: 'city',
          cityID: 'aberdeen-gb',
        ),
        ownerID: () => owner,
        identityChanges: changes,
      );
      addTearDown(controller.dispose);
      final first = controller.refresh();
      await Future<void>.delayed(Duration.zero);
      await controller.select(
        const PublicMomentHistoryQuery(
          scope: 'following',
          cityID: 'aberdeen-gb',
        ),
      );
      delayed.complete(response(publicPage(snapshot)));
      await first;
      expect(controller.query.scope, 'following');
      expect(controller.items.length, 1);
      expect(controller.nextCursor, isNull);
      final refresh = controller.refresh();
      await Future<void>.delayed(Duration.zero);
      expect(controller.loading, isTrue);
      expect(controller.items.length, 1);
      refreshing.complete(
        response(
          publicPage(snapshot, scope: 'following', count: 1, next: null),
        ),
      );
      await refresh;
      expect(controller.items.length, 1);
    },
  );

  test('public expired or foreign wire cannot become a cached list', () {
    final snapshot = DateTime.now().toUtc();
    final raw = publicPage(snapshot, count: 1, next: null);
    raw['cityId'] = 'foreign-city';
    expect(
      () => PublicMomentHistoryPageData.decode(
        raw,
        const PublicMomentHistoryQuery(scope: 'city', cityID: 'aberdeen-gb'),
      ),
      throwsFormatException,
    );
  });

  testWidgets(
    'secondary city browsing renders honest rows and changes relationship scope',
    (tester) async {
      final snapshot = DateTime.now().toUtc(), requests = <String>[];
      final client = MockClient((r) async {
        final scope = r.url.queryParameters['scope']!;
        requests.add(scope);
        return response(
          publicPage(snapshot, scope: scope, count: 1, next: null),
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: PublicMomentHistoryPage(
            query: const PublicMomentHistoryQuery(
              scope: 'city',
              cityID: 'aberdeen-gb',
            ),
            cityName: '阿伯丁',
            authorizationHeader: () => 'Bearer synthetic',
            ownerID: () => owner,
            client: client,
            apiBaseUrl: 'https://unit.invalid',
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('城市动态'), findsOneWidget);
      expect(find.text('合成公开动态 0'), findsOneWidget);
      await tester.tap(find.text('全部公开动态'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('我的好友').last);
      await tester.pumpAndSettle();
      expect(requests, ['city', 'friends']);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
