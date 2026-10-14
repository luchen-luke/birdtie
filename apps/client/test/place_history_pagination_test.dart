import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/place_history_pagination.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const place = '22222222-2222-4222-8222-222222222222';
Map<String, dynamic> page(
  int offset, {
  int length = 5,
  String? cursor = 'opaque_cursor',
  String? source,
}) {
  final now = DateTime.now().toUtc();
  return {
    'schemaVersion': 'place-social-history-page-v1',
    'placeId': place,
    'cityId': 'test',
    'authorMode': 'PUBLIC_PROFILE',
    'windowDays': 30,
    'windowStart': now.subtract(const Duration(days: 30)).toIso8601String(),
    'windowEnd': now.toIso8601String(),
    'observedAt': now.toIso8601String(),
    'validUntil': now.add(const Duration(minutes: 2)).toIso8601String(),
    'sourceVersion': source ?? List.filled(64, 'a').join(),
    'count': 7,
    'nextCursor': cursor,
    'items': [
      for (var i = 0; i < length; i++)
        {
          'id':
              '11111111-1111-4111-8111-${(99 - offset - i).toString().padLeft(12, '0')}',
          'title': '公开分享',
          'excerpt': '原公开节选',
          'revision': 2,
          'publishedAt': now
              .subtract(Duration(days: offset + i + 1))
              .toIso8601String(),
          'authorId': null,
          'authorPublicProfile': null,
        },
    ],
  };
}

http.Response reply(Map<String, dynamic> d) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': d})), 200);
void main() {
  test('anonymous non-author first page has no empty forced query', () async {
    final changes = ValueNotifier(0),
        wire = page(0, length: 2, cursor: null)
          ..['authorMode'] = 'NONE'
          ..['count'] = 2;
    final c = PlaceHistoryPaginationController(
      placeID: place,
      authorizationHeader: () => null,
      identityChanges: changes,
      apiBaseUrl: 'https://owned.test',
      client: MockClient((r) async {
        expect(r.url.hasQuery, false);
        expect(r.headers.containsKey('Authorization'), false);
        return reply(wire);
      }),
    );
    addTearDown(c.dispose);
    addTearDown(changes.dispose);
    await c.refresh();
    expect(c.items.length, 2);
    expect(c.error, isNull);
  });
  test('append original page cursor without forging media or author', () async {
    final changes = ValueNotifier(0), first = page(0);
    var calls = 0;
    final next = page(5, length: 2, cursor: null);
    for (final key in ['windowStart', 'windowEnd', 'validUntil']) {
      next[key] = first[key];
    }
    final c = PlaceHistoryPaginationController(
      placeID: place,
      authorizationHeader: () => null,
      identityChanges: changes,
      includePublicAuthor: true,
      apiBaseUrl: 'https://owned.test',
      client: MockClient((r) async {
        calls++;
        if (calls == 1) {
          expect(r.url.queryParameters, {'include': 'public-author'});
          return reply(first);
        }
        expect(r.url.queryParameters['cursor'], 'opaque_cursor');
        return reply(next);
      }),
    );
    addTearDown(c.dispose);
    addTearDown(changes.dispose);
    await c.refresh();
    expect(c.items.length, 5);
    await c.loadMore();
    expect(c.items.length, 7);
    expect(c.hasMore, false);
    expect(c.items.every((m) => m.authorPublicProfile == null), true);
  });
  test(
    'same token identity A B A notifications retire late continuation',
    () async {
      final changes = ValueNotifier('A'),
          first = page(0),
          pending = Completer<http.Response>();
      var calls = 0;
      final c = PlaceHistoryPaginationController(
        placeID: place,
        authorizationHeader: () => 'Bearer same',
        identityChanges: changes,
        includePublicAuthor: true,
        apiBaseUrl: 'https://owned.test',
        client: MockClient((_) async {
          if (++calls == 1) return reply(first);
          return pending.future;
        }),
      );
      addTearDown(c.dispose);
      addTearDown(changes.dispose);
      await c.refresh();
      final more = c.loadMore();
      changes.value = 'B';
      changes.value = 'A';
      final next = page(5, length: 2, cursor: null);
      for (final key in ['windowStart', 'windowEnd', 'validUntil']) {
        next[key] = first[key];
      }
      pending.complete(reply(next));
      await more;
      expect(c.items, isEmpty);
      expect(c.page, isNull);
    },
  );
  test(
    '409 or mixed source clears old pages and requires GET restart',
    () async {
      for (final mode in ['409', 'source']) {
        final changes = ValueNotifier(0), first = page(0);
        var calls = 0;
        final next = page(
          5,
          length: 2,
          cursor: null,
          source: List.filled(64, 'b').join(),
        );
        for (final key in ['windowStart', 'windowEnd', 'validUntil']) {
          next[key] = first[key];
        }
        final c = PlaceHistoryPaginationController(
          placeID: place,
          authorizationHeader: () => null,
          identityChanges: changes,
          includePublicAuthor: true,
          apiBaseUrl: 'https://owned.test',
          client: MockClient((r) async {
            expect(r.method, 'GET');
            if (++calls == 1) return reply(first);
            return mode == '409' ? http.Response('{}', 409) : reply(next);
          }),
        );
        await c.refresh();
        await c.loadMore();
        expect(c.items, isEmpty);
        expect(c.error, contains('重新读取'));
        c.dispose();
        changes.dispose();
      }
    },
  );
  testWidgets('pending continuation cannot disable original expiry timer', (
    tester,
  ) async {
    final changes = ValueNotifier(0),
        first = page(0),
        pending = Completer<http.Response>();
    var calls = 0;
    final at = DateTime.parse(first['observedAt']);
    first['validUntil'] = at.add(const Duration(seconds: 1)).toIso8601String();
    final c = PlaceHistoryPaginationController(
      placeID: place,
      authorizationHeader: () => null,
      identityChanges: changes,
      includePublicAuthor: true,
      apiBaseUrl: 'https://owned.test',
      client: MockClient((_) async {
        if (++calls == 1) return reply(first);
        return pending.future;
      }),
    );
    addTearDown(c.dispose);
    addTearDown(changes.dispose);
    await c.refresh();
    expect(c.items.length, 5);
    final more = c.loadMore();
    await tester.pump(const Duration(seconds: 2));
    expect(c.items, isEmpty);
    expect(c.page, isNull);
    final next = page(5, length: 2, cursor: null);
    for (final key in ['windowStart', 'windowEnd', 'validUntil']) {
      next[key] = first[key];
    }
    pending.complete(reply(next));
    await more;
    expect(c.items, isEmpty);
  });
  test('closed DTO refuses private fields or private author promotion', () {
    final private = page(0)..['privateImages'] = [];
    expect(
      () => PlaceHistoryPage.decode(private, place, true),
      throwsFormatException,
    );
    final bad = page(0);
    (bad['items'] as List).first['authorId'] =
        '11111111-1111-4111-8111-111111111111';
    expect(
      () => PlaceHistoryPage.decode(bad, place, true),
      throwsFormatException,
    );
  });
  test(
    'harmless boundary notifications preserve original loaded generation',
    () async {
      final changes = ValueNotifier(0), wire = page(0);
      var live = true;
      final c = PlaceHistoryPaginationController(
        placeID: place,
        authorizationHeader: () => null,
        identityChanges: changes,
        sourceCurrent: () => live,
        includePublicAuthor: true,
        apiBaseUrl: 'https://owned.test',
        client: MockClient((_) async => reply(wire)),
      );
      addTearDown(c.dispose);
      addTearDown(changes.dispose);
      await c.refresh();
      final generation = c.page;
      changes.value++;
      expect(c.page, same(generation));
      expect(c.items.length, 5);
      live = false;
      changes.value++;
      live = true;
      changes.value++;
      expect(c.items, isEmpty);
      expect(c.page, isNull);
    },
  );
  test(
    'token synchronization getters do not notify inside a caller build',
    () async {
      final changes = ValueNotifier(0), wire = page(0);
      String? token;
      var notifications = 0;
      final c = PlaceHistoryPaginationController(
        placeID: place,
        authorizationHeader: () => token,
        identityChanges: changes,
        includePublicAuthor: true,
        apiBaseUrl: 'https://owned.test',
        client: MockClient((_) async => reply(wire)),
      );
      addTearDown(c.dispose);
      addTearDown(changes.dispose);
      c.addListener(() => notifications++);
      await c.refresh();
      notifications = 0;
      token = 'Bearer changed';
      expect(c.items, isEmpty);
      expect(c.page, isNull);
      expect(notifications, 0);
    },
  );
}
