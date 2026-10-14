import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

Map<String, dynamic> notificationRow(int n) => {
  'id': '11111111-1111-4111-8111-${n.toString().padLeft(12, '0')}',
  'category': 'updates',
  'title': '本人合成通知$n',
  'detail': '合成测试内容',
  'resourceType': 'community',
  'resourceId': '22222222-2222-4222-8222-222222222222',
  'createdAt': '2026-10-12T00:00:00Z',
};

Map<String, dynamic> notificationPageData({
  List<Map<String, dynamic>>? items,
  String? cursor,
  int unread = 129,
}) => {
  'items': items ?? [notificationRow(1)],
  'nextCursor': cursor,
  'unreadCount': unread,
};

http.Response pageResponse({
  List<Map<String, dynamic>>? items,
  String? cursor,
  int unread = 129,
}) => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': notificationPageData(
        items: items,
        cursor: cursor,
        unread: unread,
      ),
    }),
  ),
  200,
);

void main() {
  test(
    'notification pages use bounded read-only endpoint and full unread total',
    () async {
      final requests = <http.Request>[];
      final source = RemoteInboxSource(
        authorizationHeader: () => 'Bearer synthetic-a',
        apiBaseUrl: 'https://fixture/',
        client: MockClient((request) async {
          requests.add(request);
          return pageResponse(cursor: 'YWJj');
        }),
      );
      addTearDown(source.dispose);
      final first = await source.loadNotificationsPage(limit: 3);
      final second = await source.loadNotificationsPage(
        limit: 20,
        cursor: 'ZGVm',
      );
      expect(first.items.single.title, '本人合成通知1');
      expect(first.unreadCount, 129);
      expect(second.nextCursor, 'YWJj');
      expect(requests.map((r) => r.method), everyElement('GET'));
      expect(requests.first.url.path, '/v1/me/inbox/page');
      expect(requests.first.url.queryParameters, {
        'view': 'notifications',
        'limit': '3',
      });
      expect(requests.last.url.queryParameters, {
        'view': 'notifications',
        'limit': '20',
        'cursor': 'ZGVm',
      });
      expect(requests.first.headers['Authorization'], 'Bearer synthetic-a');
    },
  );

  test(
    'page contract rejects mixed human messages, repeats and invalid totals',
    () {
      final row = notificationRow(1);
      final invalid = <Map<String, dynamic>>[
        {...notificationPageData(), 'extra': true},
        {...notificationPageData(), 'unreadCount': null},
        {...notificationPageData(), 'unreadCount': -1},
        {...notificationPageData(), 'unreadCount': 1.5},
        notificationPageData(items: [row, row]),
        notificationPageData(
          items: [
            {...row, 'resourceType': 'conversation_message'},
          ],
        ),
        notificationPageData(items: [], cursor: 'YWJj'),
        {...notificationPageData(), 'nextCursor': 1},
        notificationPageData(cursor: ''),
        notificationPageData(cursor: 'abc='),
        notificationPageData(cursor: 'https://external.invalid'),
        notificationPageData(cursor: 'A'),
        notificationPageData(cursor: 'AB'),
        notificationPageData(cursor: List.filled(1028, 'A').join()),
      ];
      for (final data in invalid) {
        expect(
          () => InboxNotificationPage.fromJson(data),
          throwsFormatException,
          reason: 'Invalid page must not become a notification list',
        );
      }
      expect(
        () => InboxNotificationPage.fromJson(
          notificationPageData(cursor: 'YWJj'),
          previousCursor: 'YWJj',
        ),
        throwsFormatException,
      );
      expect(
        () => InboxNotificationPage.fromJson(
          notificationPageData(items: [row, notificationRow(2)]),
          limit: 1,
        ),
        throwsFormatException,
      );
      final empty = InboxNotificationPage.fromJson(
        notificationPageData(items: [], unread: 0),
      );
      expect(empty.items, isEmpty);
      expect(empty.nextCursor, isNull);
      expect(empty.unreadCount, 0);
      final bounded = InboxNotificationPage.fromJson(
        notificationPageData(cursor: List.filled(1024, 'A').join()),
      );
      expect(bounded.nextCursor!.length, 1024);
      expect(
        () => bounded.items.add(InboxItem.fromJson(row)),
        throwsUnsupportedError,
      );
    },
  );

  test('invalid request cursor and limits make no HTTP request', () async {
    var reads = 0;
    final source = RemoteInboxSource(
      authorizationHeader: () => 'Bearer synthetic',
      client: MockClient((r) async {
        reads++;
        return pageResponse();
      }),
    );
    addTearDown(source.dispose);
    for (final limit in [0, 51]) {
      await expectLater(
        source.loadNotificationsPage(limit: limit),
        throwsFormatException,
      );
    }
    for (final cursor in [
      'A',
      'AB',
      'abc=',
      'http://external.invalid',
      List.filled(1028, 'A').join(),
    ]) {
      await expectLater(
        source.loadNotificationsPage(cursor: cursor),
        throwsFormatException,
      );
    }
    expect(reads, 0);
  });

  for (final retirement in ['account', 'aba', 'source', 'dispose']) {
    test(
      'late private page is discarded after $retirement retirement',
      () async {
        var token = 'Bearer synthetic-a', epoch = 0, active = true;
        final changes = ValueNotifier<int>(0),
            response = Completer<http.Response>();
        final source = RemoteInboxSource(
          authorizationHeader: () => token,
          identityEpoch: () => epoch,
          identityChanges: changes,
          isCurrent: () => active,
          client: MockClient((r) => response.future),
        );
        addTearDown(source.dispose);
        addTearDown(changes.dispose);
        final result = expectLater(
          source.loadNotificationsPage(),
          throwsStateError,
        );
        await Future<void>.delayed(Duration.zero);
        switch (retirement) {
          case 'account':
            token = 'Bearer synthetic-b';
          case 'aba':
            token = 'Bearer synthetic-b';
            changes.value++;
            token = 'Bearer synthetic-a';
            changes.value++;
            epoch++;
          case 'source':
            active = false;
          case 'dispose':
            source.dispose();
        }
        response.complete(pageResponse());
        await result;
      },
    );
  }

  for (final current in [true, false]) {
    test('only current 401 invokes shared recovery: $current', () async {
      var epoch = 0, recoveries = 0;
      final response = Completer<http.Response>();
      final source = RemoteInboxSource(
        authorizationHeader: () => 'Bearer synthetic-a',
        identityEpoch: () => epoch,
        onUnauthorized: (expected) async {
          expect(expected, 'Bearer synthetic-a');
          recoveries++;
        },
        client: MockClient((r) => response.future),
      );
      addTearDown(source.dispose);
      final result = expectLater(
        source.loadNotificationsPage(),
        current
            ? throwsA(
                isA<InboxReadFailure>().having(
                  (e) => e.statusCode,
                  'status',
                  401,
                ),
              )
            : throwsStateError,
      );
      await Future<void>.delayed(Duration.zero);
      if (!current) epoch++;
      response.complete(http.Response('{}', 401));
      await result;
      expect(recoveries, current ? 1 : 0);
    });
  }

  for (final status in [403, 404, 409, 503]) {
    test(
      'notification error $status is retained as typed failure without login',
      () async {
        var recoveries = 0;
        final source = RemoteInboxSource(
          authorizationHeader: () => 'Bearer synthetic',
          onUnauthorized: (_) async {
            recoveries++;
          },
          client: MockClient((r) async => http.Response('{}', status)),
        );
        addTearDown(source.dispose);
        await expectLater(
          source.loadNotificationsPage(),
          throwsA(
            isA<InboxReadFailure>().having(
              (e) => e.statusCode,
              'status',
              status,
            ),
          ),
        );
        expect(recoveries, 0);
      },
    );
  }

  test(
    'legacy full list and explicit read contracts remain reachable',
    () async {
      final requests = <http.Request>[];
      final row = notificationRow(1);
      final source = RemoteInboxSource(
        authorizationHeader: () => 'Bearer synthetic',
        client: MockClient((r) async {
          requests.add(r);
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': r.method == 'POST'
                    ? {...row, 'readAt': '2026-10-12T00:01:00Z'}
                    : [row],
              }),
            ),
            200,
          );
        }),
      );
      addTearDown(source.dispose);
      final rows = await source.load();
      expect(requests.single.url.path, '/v1/me/inbox');
      expect(requests.single.method, 'GET');
      final read = await source.markRead(rows.single.id);
      expect(read.readAt, isNotNull);
      expect(requests.last.method, 'POST');
      expect(requests.last.url.path, '/v1/me/inbox/${row['id']}/read');
    },
  );
  test(
    'Business claim decision uses distinct current private management target',
    () {
      final row = <String, dynamic>{
        'id': '11111111-1111-4111-8111-111111111111',
        'category': 'updates',
        'title': '商家经营权审核状态已更新',
        'detail': '请查看当前审核状态。',
        'resourceType': 'business_claim_review',
        'resourceId': '22222222-2222-4222-8222-222222222222',
        'targetBusinessId': '33333333-3333-4333-8333-333333333333',
        'semanticCategory': 'BUSINESS',
        'notificationRoute': 'NORMAL',
        'createdAt': '2026-10-04T04:00:00Z',
      };
      final item = InboxItem.fromJson(row);
      expect(item.targetBusinessID, row['targetBusinessId']);
      expect(item.targetBusinessID, isNot(item.resourceID));
      for (final bad in [
        {...row, 'targetBusinessId': null},
        {...row, 'targetBusinessId': 'invalid'},
        {...row, 'semanticCategory': 'ACTIVITY'},
        {...row, 'targetActivityId': row['targetBusinessId']},
        {...row, 'targetTaskId': row['targetBusinessId']},
        {...row, 'targetCommunityId': row['targetBusinessId']},
        {...row, 'targetConversationId': row['targetBusinessId']},
        {...row, 'resourceType': 'agent_task'},
        {...row, 'notificationRoute': 'SILENT'},
      ]) {
        expect(() => InboxItem.fromJson(bad), throwsFormatException);
      }
    },
  );
  test(
    'native Community and Task target IDs are distinct from decision IDs',
    () {
      final data = <String, dynamic>{
        'id': '11111111-1111-4111-8111-111111111111',
        'category': 'messages',
        'title': '新动态',
        'detail': '查看当前内容',
        'resourceType': 'community_message',
        'resourceId': '22222222-2222-4222-8222-222222222222',
        'createdAt': '2026-10-01T04:00:00Z',
        'targetCommunityId': '33333333-3333-4333-8333-333333333333',
      };
      final community = InboxItem.fromJson(data);
      expect(
        community.targetCommunityID,
        '33333333-3333-4333-8333-333333333333',
      );
      expect(community.targetCommunityID, isNot(community.resourceID));
      final task = InboxItem.fromJson({
        ...data,
        'resourceType': 'agent_task',
        'targetCommunityId': null,
        'targetTaskId': '44444444-4444-4444-8444-444444444444',
      });
      expect(task.targetTaskID, '44444444-4444-4444-8444-444444444444');
      for (final change in [
        {'resourceType': 'agent_task'},
        {'targetCommunityId': 'not-an-id'},
        {'targetTaskId': '44444444-4444-4444-8444-444444444444'},
        {'targetActivityId': '44444444-4444-4444-8444-444444444444'},
      ]) {
        expect(
          () => InboxItem.fromJson({...data, ...change}),
          throwsFormatException,
        );
      }
      final legacy = InboxItem.fromJson({...data, 'targetCommunityId': null});
      expect(legacy.targetCommunityID, isNull);
    },
  );
  test('activity notifications preserve the target activity and read time', () {
    final item = InboxItem.fromJson({
      'id': '11111111-1111-4111-8111-111111111111',
      'category': 'needs_attention',
      'title': '活动已取消',
      'detail': '请查看活动详情。',
      'resourceType': 'activity_cancelled',
      'resourceId': '22222222-2222-4222-8222-222222222222',
      'targetActivityId': '33333333-3333-4333-8333-333333333333',
      'targetConversationId': '44444444-4444-4444-8444-444444444444',
      'createdAt': '2026-10-01T04:00:00Z',
      'readAt': '2026-10-01T04:01:00Z',
    });

    expect(item.targetActivityID, '33333333-3333-4333-8333-333333333333');
    expect(item.targetConversationID, '44444444-4444-4444-8444-444444444444');
    expect(item.resourceType, 'activity_cancelled');
    expect(item.readAt, DateTime.utc(2026, 10, 1, 4, 1));
  });
}
