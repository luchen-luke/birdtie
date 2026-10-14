// Source-only review proposal. NOT_RUN; MockClient is not native HTTP evidence.
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _place = '22000000-0000-4000-8000-000000000001';
String _id(int n) =>
    '22000000-0000-4000-8000-${(100 - n).toString().padLeft(12, '0')}';
http.Response _reply(Map<String, dynamic> page) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': page})), 200);
Map<String, dynamic> _page(DateTime end, {bool next = false}) => {
  'schemaVersion': 'place-social-history-page-v1',
  'placeId': _place,
  'cityId': 'owned-test',
  'authorMode': 'PUBLIC_PROFILE',
  'windowDays': 30,
  'windowStart': end.subtract(const Duration(days: 30)).toIso8601String(),
  'windowEnd': end.toIso8601String(),
  'observedAt': end.toIso8601String(),
  'validUntil': end.add(const Duration(minutes: 2)).toIso8601String(),
  'sourceVersion': List.filled(64, 'a').join(),
  'count': 7,
  'nextCursor': next ? null : 'opaque_cursor',
  'items': [
    for (var n = next ? 6 : 1; n <= (next ? 7 : 5); n++)
      {
        'id': _id(n),
        'title': '公开动态 $n',
        'excerpt': '本人明确公开的测试节选 $n',
        'revision': 2,
        'publishedAt': end.subtract(Duration(days: n)).toIso8601String(),
        'authorId': null,
        'authorPublicProfile': null,
      },
  ],
};
Map<String, dynamic> _moment(DateTime end, int n) => {
  'schemaVersion': 'public-moment-v2',
  'id': _id(n),
  'title': '公开动态 $n',
  'body': '本人明确公开的完整测试正文 $n',
  'placeId': _place,
  'placeName': '测试地点',
  'cityId': 'owned-test',
  'revision': 2,
  'publishedAt': end.subtract(Duration(days: n)).toIso8601String(),
  'authorId': null,
  'authorPublicProfile': null,
};

void main() {
  testWidgets(
    'later page is reachable and opening item six never refreshes page one',
    (t) async {
      final end = DateTime.now().toUtc(),
          changes = ValueNotifier(true),
          opened = <String>[];
      final requests = <http.Request>[];
      final transport = MockClient((r) async {
        requests.add(r);
        expect(r.method, 'GET');
        if (r.url.path == '/v1/places/$_place/social-history/moments') {
          expect(r.url.queryParameters['include'], 'public-author');
          return _reply(
            _page(end, next: r.url.queryParameters.containsKey('cursor')),
          );
        }
        expect(r.url.path, '/v1/moments/${_id(6)}');
        return _reply(_moment(end, 6));
      });
      addTearDown(changes.dispose);
      addTearDown(transport.close);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: NowPlacePreview(
                placeID: _place,
                placeTitle: '测试地点',
                validUntil: end.add(const Duration(minutes: 2)),
                authorizationHeader: () => null,
                workspaceID: () => null,
                sourceChanges: changes,
                current: () => changes.value,
                revalidate: () async => changes.value,
                onClose: () {},
                onOpenPlace: () {},
                onOpenMoment: opened.add,
                apiBaseUrl: 'https://owned.test',
                client: transport,
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('最近 30 天 · 7 条公开分享'), findsOneWidget);
      await t.tap(find.text('看看公开动态'));
      await t.pumpAndSettle();
      await t.tap(find.byTooltip('下一组公开动态'));
      await t.pumpAndSettle();
      expect(find.text('公开动态 4'), findsOneWidget);
      await t.tap(find.byKey(const ValueKey('place-stories-load-more')));
      await t.pumpAndSettle();
      expect(find.text('公开动态 4'), findsOneWidget);
      expect(find.text('公开动态 1'), findsNothing);
      expect(find.text('公开动态 6'), findsOneWidget);
      await t.tap(find.text('公开动态 6'));
      await t.pumpAndSettle();
      expect(opened, [_id(6)]);
      final pages = requests
          .where((r) => r.url.path.endsWith('/social-history/moments'))
          .toList();
      expect(pages.length, 2);
      expect(pages.first.url.queryParameters.containsKey('cursor'), false);
      expect(pages.last.url.queryParameters['cursor'], 'opaque_cursor');
      expect(find.textContaining('摘要未提供下一页'), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox.shrink());
    },
  );
  testWidgets(
    'source ABA during continuation cannot recover stale preview stories',
    (t) async {
      final end = DateTime.now().toUtc(), changes = ValueNotifier(true);
      final pending = Completer<http.Response>();
      var reads = 0;
      final transport = MockClient((r) async {
        expect(r.method, 'GET');
        if (++reads == 1) return _reply(_page(end));
        return pending.future;
      });
      addTearDown(changes.dispose);
      addTearDown(transport.close);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: NowPlacePreview(
                placeID: _place,
                placeTitle: '测试地点',
                validUntil: end.add(const Duration(minutes: 2)),
                authorizationHeader: () => null,
                workspaceID: () => null,
                sourceChanges: changes,
                current: () => changes.value,
                revalidate: () async => changes.value,
                onClose: () {},
                onOpenPlace: () {},
                onOpenMoment: (_) => fail('retired preview opened a moment'),
                apiBaseUrl: 'https://owned.test',
                client: transport,
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.tap(find.text('看看公开动态'));
      await t.pumpAndSettle();
      await t.tap(find.byKey(const ValueKey('place-stories-load-more')));
      await t.pump();
      changes.value = false;
      changes.value = true;
      pending.complete(_reply(_page(end, next: true)));
      await t.pumpAndSettle();
      expect(find.text('请重新打开地点'), findsOneWidget);
      expect(find.text('公开动态 1'), findsNothing);
      expect(find.text('公开动态 6'), findsNothing);
      expect(reads, 2);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox.shrink());
    },
  );
}
