import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/journey_history_page.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _id = '16500000-0000-4000-8000-000000000001';
Map<String, dynamic> _wire({bool detail = false}) {
  final now = DateTime.now().toUtc();
  return {
    'schemaVersion': 'owned-journey-history-v1',
    'threads': [
      {
        'id': _id,
        'journeyId': '',
        'contextKind': 'DRAFT_CONTEXT',
        'summary': '合成原对话',
        'sourceStatus': 'active',
        'sourceContextRevision': 0,
        'createdAt': now.toIso8601String(),
        'updatedAt': now.toIso8601String(),
      },
    ],
    'messages': detail
        ? [
            {
              'id': '16500000-0000-4000-8000-000000000002',
              'role': 'ASSISTANT',
              'body': '合成原助手回复',
              'createdAt': now.toIso8601String(),
            },
          ]
        : [],
    'preferences': null,
    'observedAt': now.toIso8601String(),
    'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
    'sourceVersion': List.filled(64, 'a').join(),
    'nextCursor': null,
  };
}

void main() {
  test(
    'historical notifications keep separate unread and reject cached destinations',
    () {
      final now = DateTime.now().toUtc().toIso8601String();
      final item = {
        'id': _id,
        'category': 'updates',
        'title': '原通知记录',
        'detail': '历史事件',
        'resourceType': 'legacy_history',
        'resourceId': _id,
        'createdAt': now,
        'origin': 'LEGACY_HISTORY',
        'historicalType': 'follow',
      };
      final page = InboxNotificationPage.fromJson({
        'items': [item],
        'nextCursor': null,
        'unreadCount': 0,
        'historicalUnreadCount': 37,
      });
      expect(page.unreadCount, 0);
      expect(page.historicalUnreadCount, 37);
      expect(page.items.single.origin, 'LEGACY_HISTORY');
      expect(
        () => InboxItem.fromJson({...item, 'targetConversationId': _id}),
        throwsFormatException,
      );
      expect(
        () => JourneyHistoryData.read({
          ..._wire(detail: true),
          'messages': [
            {
              'id': _id,
              'role': 'AGENT',
              'body': '不伪造当前Agent',
              'createdAt': now,
            },
          ],
        }),
        throwsFormatException,
      );
    },
  );
  testWidgets(
    'historical assistant text retires on identity change and lease expiry',
    (tester) async {
      var token = 'Bearer synthetic';
      final changes = ValueNotifier(0);
      final client = MockClient(
        (request) async => http.Response.bytes(
          utf8.encode(
            jsonEncode({'data': _wire(detail: request.url.path.endsWith(_id))}),
          ),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: JourneyHistoryPage(
            client: client,
            apiBaseUrl: 'https://test.invalid',
            authorizationHeader: () => token,
            ownerID: () => _id,
            current: () => true,
            identityChanges: changes,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('合成原对话'), findsOneWidget);
      await tester.tap(find.text('合成原对话'));
      await tester.pumpAndSettle();
      expect(find.text('合成原助手回复'), findsOneWidget);
      expect(find.text('原助手'), findsOneWidget);
      await tester.pump(const Duration(seconds: 31));
      expect(find.text('合成原助手回复'), findsNothing);
      expect(find.textContaining('读取已过期'), findsOneWidget);
      await tester.tap(find.byTooltip('重新读取'));
      await tester.pumpAndSettle();
      expect(find.text('合成原助手回复'), findsOneWidget);
      token = 'Bearer another';
      changes.value++;
      await tester.pump();
      expect(find.text('合成原助手回复'), findsNothing);
      expect(find.textContaining('账号或入口已变化'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      changes.dispose();
    },
  );
  testWidgets(
    'late private history response is not accepted after owner switch',
    (tester) async {
      final reply = Completer<http.Response>();
      final started = Completer<void>();
      var token = 'Bearer first';
      final changes = ValueNotifier(0);
      final client = MockClient((request) {
        started.complete();
        return reply.future;
      });
      await tester.pumpWidget(
        MaterialApp(
          home: JourneyHistoryPage(
            client: client,
            apiBaseUrl: 'https://test.invalid',
            authorizationHeader: () => token,
            ownerID: () => _id,
            current: () => true,
            identityChanges: changes,
          ),
        ),
      );
      await started.future;
      token = 'Bearer second';
      changes.value++;
      reply.complete(
        http.Response.bytes(utf8.encode(jsonEncode({'data': _wire()})), 200),
      );
      await tester.pumpAndSettle();
      expect(find.text('合成原对话'), findsNothing);
      expect(find.textContaining('账号或入口已变化'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      changes.dispose();
    },
  );
}
