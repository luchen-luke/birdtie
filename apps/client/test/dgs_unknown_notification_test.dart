import 'dart:convert';

import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'inbox_panel_test.dart' as fixture;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'remote_inbox_source_test.dart' as rows;

Map<String, dynamic> unknown(int n) => {
  ...rows.notificationRow(n),
  'resourceType': 'future_event',
  'title': 'Untrusted future title',
  'detail': 'Unknown payload must not become a target grant',
  'targetActivityId': 'untrusted-destination',
  'actorAvatar': {'url': 'https://external.invalid/private'},
};

void main() {
  test(
    'unknown event preserves supported rows, pagination and server totals',
    () {
      final page = InboxNotificationPage.fromJson({
        ...rows.notificationPageData(
          items: [rows.notificationRow(1), unknown(2)],
          cursor: 'YWJj',
          unread: 31,
        ),
        'historicalUnreadCount': 7,
      });
      expect(page.items.first.supported, isTrue);
      final item = page.items.last;
      expect(item.supported, isFalse);
      expect(item.id, rows.notificationRow(2)['id']);
      expect(item.title, '暂不支持的通知');
      expect(item.targetActivityID, isNull);
      expect(item.actorAvatar, isNull);
      expect(page.nextCursor, 'YWJj');
      expect(page.unreadCount, 31);
      expect(page.historicalUnreadCount, 7);
      final onlyUnknown = InboxNotificationPage.fromJson(
        rows.notificationPageData(items: [unknown(2)], cursor: 'YWJj'),
      );
      expect(onlyUnknown.items.single.supported, isFalse);
      expect(onlyUnknown.nextCursor, 'YWJj');
    },
  );

  test(
    'unknown compatibility never weakens identity, delivery or duplicate rules',
    () {
      for (final row in [
        {...unknown(2), 'id': 'bad-id'},
        {...unknown(2), 'createdAt': '2026-10-16T00:00:00'},
        {...unknown(2), 'origin': 'LEGACY_HISTORY'},
        {...unknown(2), 'notificationRoute': 'PENDING'},
        {
          ...unknown(2),
          'notificationRoute': 'DIGEST',
          'notificationPriority': 0,
        },
      ]) {
        expect(
          () => InboxNotificationPage.fromJson(
            rows.notificationPageData(items: [rows.notificationRow(1), row]),
          ),
          throwsFormatException,
        );
      }
      expect(
        () => InboxNotificationPage.fromJson(
          rows.notificationPageData(items: [unknown(2), unknown(2)]),
        ),
        throwsFormatException,
      );
      expect(
        () => InboxNotificationPage.fromJson(
          rows.notificationPageData(
            items: [
              {...rows.notificationRow(1), 'targetTaskId': 'bad-known-target'},
              unknown(2),
            ],
          ),
        ),
        throwsFormatException,
      );
    },
  );

  testWidgets(
    'actual notification panel retains known row and cannot open unknown target',
    (tester) async {
      final auth = SeedTestAuth()
        ..owner = '11111111-1111-4111-8111-111111111111';
      addTearDown(auth.dispose);
      final requests = <http.Request>[];
      final source = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        apiBaseUrl: 'https://fixture.invalid',
        client: MockClient((request) async {
          requests.add(request);
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': rows.notificationPageData(
                  items: [rows.notificationRow(1), unknown(2)],
                  unread: 31,
                ),
              }),
            ),
            200,
          );
        }),
      );
      addTearDown(source.dispose);
      int? unread;
      await tester.pumpWidget(
        fixture.notificationView(
          auth,
          source,
          onUnreadChanged: (n) => unread = n,
          previewLimit: 3,
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('本人合成通知1'), findsOneWidget);
      expect(find.text('暂不支持的通知'), findsOneWidget);
      expect(find.text('Untrusted future title'), findsNothing);
      expect(unread, 31);
      final tile = tester.widget<ListTile>(
        find.ancestor(
          of: find.text('暂不支持的通知'),
          matching: find.byType(ListTile),
        ),
      );
      expect(tile.onTap, isNull);
      final before = requests.length;
      await tester.tap(find.text('暂不支持的通知'));
      await tester.pumpAndSettle();
      expect(requests.length, before);
      expect(
        requests.every(
          (r) => r.method == 'GET' && r.url.path == '/v1/me/inbox/page',
        ),
        isTrue,
      );
    },
  );
}
