import 'dart:convert';
import 'dart:async';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/agent_task_detail_page.dart';
import 'package:birdtie_client/src/workspace/community_conversation_page.dart';
import 'package:birdtie_client/src/workspace/business_claim_notification_page.dart';
import 'business_claim_notification_page_test.dart' as business_fixture;
import 'agent_task_detail_page_test.dart'
    show taskDestinationData, taskDestinationOwner, taskDestinationID;

Future<BirdtieAuthController> signedInAuth({
  String accountID = 'account-1',
  String token = 'local-test',
}) async {
  final client = MockClient((request) async {
    switch (request.url.path) {
      case '/v1/me':
        return http.Response(
          jsonEncode({
            'data': {'id': accountID},
          }),
          200,
        );
      case final path when path == '/v1/accounts/$accountID/profile':
        return http.Response('{"data":{"displayName":"测试用户"}}', 200);
      case '/v1/auth/dev-phone/status':
        return http.Response('{"data":{"enabled":true}}', 200);
      default:
        return http.Response('{}', 404);
    }
  });
  final auth = BirdtieAuthController(
    client: client,
    apiBaseUrl: 'http://127.0.0.1:8080',
    sessionVault: MemorySessionVault()
      ..session = StoredSession(token: token, method: 'dev_phone'),
  );
  await auth.initialize();
  return auth;
}

Map<String, dynamic> pageNotice(int n, {String category = 'updates'}) => {
  'id': '11111111-1111-4111-8111-${n.toString().padLeft(12, '0')}',
  'category': category,
  'title': '本人合成通知$n',
  'detail': '本人合成测试说明',
  'resourceType': 'community',
  'resourceId': '22222222-2222-4222-8222-222222222222',
  'createdAt': '2026-10-12T00:00:00Z',
};

http.Response noticePage(
  List<Map<String, dynamic>> items, {
  String? cursor,
  int unread = 129,
}) => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': {'items': items, 'nextCursor': cursor, 'unreadCount': unread},
    }),
  ),
  200,
);

Widget notificationView(
  BirdtieAuthController auth,
  RemoteInboxSource source, {
  int? previewLimit,
  ValueChanged<int>? onUnreadChanged,
  VoidCallback? onUnreadInvalidated,
  ValueNotifier<String?>? workspace,
}) => MaterialApp(
  home: Scaffold(
    body: InboxPanel(
      key: const ValueKey('notification-page'),
      auth: auth,
      source: source,
      embedded: true,
      notificationsOnly: true,
      previewLimit: previewLimit,
      onUnreadChanged: onUnreadChanged,
      onUnreadInvalidated: onUnreadInvalidated,
      workspaceChanges: workspace,
      organizationWorkspaceID: workspace == null ? null : () => workspace.value,
    ),
  ),
);

void main() {
  testWidgets(
    'notifications preview and all use real pages in server order with recoverable more',
    (t) async {
      final auth = await signedInAuth();
      addTearDown(auth.dispose);
      final requests = <http.Request>[];
      var moreReads = 0;
      final source = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        apiBaseUrl: 'https://fixture',
        client: MockClient((r) async {
          requests.add(r);
          expect(r.method, 'GET');
          expect(r.url.path, '/v1/me/inbox/page');
          if (r.url.queryParameters['limit'] == '3') {
            return noticePage([pageNotice(1)], cursor: 'YWJj');
          }
          if (r.url.queryParameters['cursor'] == null) {
            return noticePage(
              [pageNotice(2), pageNotice(1, category: 'needs_attention')],
              cursor: 'ZGVm',
              unread: 140,
            );
          }
          expect(r.url.queryParameters['cursor'], 'ZGVm');
          if (++moreReads == 1) return http.Response('{}', 503);
          return noticePage([pageNotice(3)], unread: 141);
        }),
      );
      addTearDown(source.dispose);
      final counts = <int>[];
      await t.pumpWidget(
        notificationView(
          auth,
          source,
          previewLimit: 3,
          onUnreadChanged: counts.add,
        ),
      );
      await t.pumpAndSettle();
      expect(counts, [129]);
      await t.tap(find.byKey(const Key('notifications-show-all')));
      await t.pumpAndSettle();
      expect(requests.last.url.queryParameters, {
        'view': 'notifications',
        'limit': '20',
      });
      expect(
        t.getTopLeft(find.text('本人合成通知2')).dy,
        lessThan(t.getTopLeft(find.text('本人合成通知1')).dy),
      );
      expect(counts, [129, 140]);
      await t.tap(find.byKey(const Key('notifications-load-more')));
      await t.pumpAndSettle();
      expect(find.text('本人合成通知2'), findsOneWidget);
      expect(find.text('本人合成通知1'), findsOneWidget);
      expect(find.text('重试加载更多'), findsOneWidget);
      expect(counts, [129, 140]);
      await t.tap(find.byKey(const Key('notifications-load-more')));
      await t.pumpAndSettle();
      expect(find.text('本人合成通知3'), findsOneWidget);
      expect(find.byKey(const Key('notifications-load-more')), findsNothing);
      expect(requests.last.url.queryParameters, {
        'view': 'notifications',
        'limit': '20',
        'cursor': 'ZGVm',
      });
      expect(counts, [129, 140, 141]);
      expect(requests.where((r) => r.method != 'GET'), isEmpty);
    },
  );

  for (final status in [401, 403, 404, 409]) {
    testWidgets(
      'notification page $status retires old rows and unknown badge without empty or login',
      (t) async {
        final auth = await signedInAuth();
        addTearDown(auth.dispose);
        final requests = <http.Request>[];
        var fail = false, invalidated = 0;
        final counts = <int>[];
        final source = RemoteInboxSource(
          authorizationHeader: () => auth.authorizationHeader,
          client: MockClient((r) async {
            requests.add(r);
            return fail
                ? http.Response('{}', status)
                : noticePage([pageNotice(1)], cursor: 'YWJj');
          }),
        );
        addTearDown(source.dispose);
        await t.pumpWidget(
          notificationView(
            auth,
            source,
            onUnreadChanged: counts.add,
            onUnreadInvalidated: () => invalidated++,
          ),
        );
        await t.pumpAndSettle();
        expect(find.text('本人合成通知1'), findsOneWidget);
        expect(counts, [129]);
        fail = true;
        await t.tap(find.byKey(const Key('notifications-load-more')));
        await t.pumpAndSettle();
        expect(find.text('本人合成通知1'), findsNothing);
        expect(find.text('暂无通知'), findsNothing);
        expect(find.text('登录'), findsNothing);
        expect(find.byKey(const Key('notifications-load-more')), findsNothing);
        expect(counts, [129]);
        expect(invalidated, 1);
        final message = switch (status) {
          401 => '登录已失效，请重新登录。',
          403 => '暂无权限查看通知。',
          404 => '这些通知当前不可访问，请刷新核对。',
          _ => '通知内容已变化，请刷新查看。',
        };
        expect(find.text(message), findsOneWidget);
        if (status == 409) {
          fail = false;
          await t.tap(find.text('重试'));
          await t.pumpAndSettle();
          expect(find.text('本人合成通知1'), findsOneWidget);
          expect(
            requests.last.url.queryParameters.containsKey('cursor'),
            isFalse,
          );
          expect(counts, [129, 129]);
        }
        expect(requests.where((r) => r.method != 'GET'), isEmpty);
      },
    );
  }

  for (final failure in ['offline', 'service']) {
    testWidgets(
      'notification refresh $failure preserves current rows and actual unread until retry',
      (t) async {
        final auth = await signedInAuth();
        addTearDown(auth.dispose);
        var fail = false, invalidated = 0;
        final counts = <int>[];
        final source = RemoteInboxSource(
          authorizationHeader: () => auth.authorizationHeader,
          client: MockClient((r) async {
            if (fail && failure == 'offline') {
              throw http.ClientException('Synthetic offline');
            }
            return fail
                ? http.Response('{}', 503)
                : noticePage([pageNotice(1)]);
          }),
        );
        addTearDown(source.dispose);
        await t.pumpWidget(
          notificationView(
            auth,
            source,
            onUnreadChanged: counts.add,
            onUnreadInvalidated: () => invalidated++,
          ),
        );
        await t.pumpAndSettle();
        fail = true;
        unawaited(
          t.state<RefreshIndicatorState>(find.byType(RefreshIndicator)).show(),
        );
        await t.pumpAndSettle();
        expect(find.text('本人合成通知1'), findsOneWidget);
        expect(find.text('暂无通知'), findsNothing);
        expect(find.text('重试'), findsOneWidget);
        expect(counts, [129]);
        expect(invalidated, 0);
        fail = false;
        await t.tap(find.text('重试'));
        await t.pumpAndSettle();
        expect(find.text('重试'), findsNothing);
        expect(find.text('本人合成通知1'), findsOneWidget);
        expect(counts, [129, 129]);
      },
    );
  }

  testWidgets(
    'verified empty notification page gives one empty and real zero without read writes',
    (t) async {
      final auth = await signedInAuth();
      addTearDown(auth.dispose);
      final requests = <http.Request>[];
      final source = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: MockClient((r) async {
          requests.add(r);
          return noticePage([], unread: 0);
        }),
      );
      addTearDown(source.dispose);
      final counts = <int>[];
      await t.pumpWidget(
        notificationView(auth, source, onUnreadChanged: counts.add),
      );
      await t.pumpAndSettle();
      expect(find.text('暂无通知'), findsOneWidget);
      expect(find.text('刷新'), findsOneWidget);
      expect(find.text('重试'), findsNothing);
      expect(counts, [0]);
      expect(requests.single.method, 'GET');
      expect(requests.single.url.path, '/v1/me/inbox/page');
    },
  );

  testWidgets(
    'notification account ABA discards pending old-owner page and keeps new canonical page',
    (t) async {
      final authA = await signedInAuth(
            accountID: 'account-a',
            token: 'session-a',
          ),
          authB = await signedInAuth(
            accountID: 'account-b',
            token: 'session-b',
          );
      addTearDown(authA.dispose);
      addTearDown(authB.dispose);
      final pending = Completer<http.Response>();
      var aReads = 0;
      final sourceA = RemoteInboxSource(
        authorizationHeader: () => authA.authorizationHeader,
        client: MockClient(
          (r) async =>
              ++aReads == 1 ? pending.future : noticePage([pageNotice(3)]),
        ),
      );
      final sourceB = RemoteInboxSource(
        authorizationHeader: () => authB.authorizationHeader,
        client: MockClient((r) async => noticePage([pageNotice(2)])),
      );
      addTearDown(sourceA.dispose);
      addTearDown(sourceB.dispose);
      final counts = <int>[];
      await t.pumpWidget(
        notificationView(authA, sourceA, onUnreadChanged: counts.add),
      );
      await t.pump();
      await t.pumpWidget(
        notificationView(authB, sourceB, onUnreadChanged: counts.add),
      );
      await t.pumpAndSettle();
      expect(find.text('本人合成通知2'), findsOneWidget);
      await t.pumpWidget(
        notificationView(authA, sourceA, onUnreadChanged: counts.add),
      );
      await t.pumpAndSettle();
      expect(find.text('本人合成通知2'), findsNothing);
      expect(find.text('本人合成通知3'), findsOneWidget);
      pending.complete(noticePage([pageNotice(1)], unread: 999));
      await t.pumpAndSettle();
      expect(find.text('本人合成通知1'), findsNothing);
      expect(find.text('本人合成通知3'), findsOneWidget);
      expect(counts, [129, 129]);
      expect(aReads, 2);
    },
  );

  testWidgets(
    'notification workspace ABA retires cursor before late more returns',
    (t) async {
      final auth = await signedInAuth();
      addTearDown(auth.dispose);
      final workspace = ValueNotifier<String?>(null),
          pending = Completer<http.Response>();
      addTearDown(workspace.dispose);
      final requests = <http.Request>[];
      var reads = 0;
      final source = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: MockClient((r) async {
          requests.add(r);
          if (r.url.queryParameters['cursor'] != null) return pending.future;
          return ++reads == 1
              ? noticePage([pageNotice(1)], cursor: 'YWJj')
              : noticePage([pageNotice(3)], unread: 3);
        }),
      );
      addTearDown(source.dispose);
      final counts = <int>[];
      var invalidated = 0;
      await t.pumpWidget(
        notificationView(
          auth,
          source,
          workspace: workspace,
          onUnreadChanged: counts.add,
          onUnreadInvalidated: () => invalidated++,
        ),
      );
      await t.pumpAndSettle();
      await t.tap(find.byKey(const Key('notifications-load-more')));
      await t.pump();
      workspace.value = 'org';
      workspace.value = null;
      await t.pumpAndSettle();
      expect(find.text('本人合成通知1'), findsNothing);
      expect(find.text('本人合成通知3'), findsOneWidget);
      pending.complete(noticePage([pageNotice(2)], unread: 999));
      await t.pumpAndSettle();
      expect(find.text('本人合成通知2'), findsNothing);
      expect(counts, [129, 3]);
      expect(invalidated, 2);
      expect(requests.last.url.queryParameters.containsKey('cursor'), isFalse);
    },
  );
  testWidgets(
    'notification page preview uses server three and independent unread total',
    (t) async {
      final auth = await signedInAuth();
      addTearDown(auth.dispose);
      final requests = <http.Request>[];
      final source = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        apiBaseUrl: 'https://fixture',
        client: MockClient((request) async {
          requests.add(request);
          if (request.url.path != '/v1/me/inbox/page') {
            return http.Response('{}', 503);
          }
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': {
                  'items': [
                    {
                      'id': '11111111-1111-4111-8111-111111111111',
                      'category': 'updates',
                      'title': '本人合成通知',
                      'detail': '合成测试说明',
                      'resourceType': 'community',
                      'resourceId': '22222222-2222-4222-8222-222222222222',
                      'createdAt': '2026-10-12T00:00:00Z',
                    },
                  ],
                  'nextCursor': 'YWJj',
                  'unreadCount': 129,
                },
              }),
            ),
            200,
          );
        }),
      );
      addTearDown(source.dispose);
      final counts = <int>[];
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: InboxPanel(
              auth: auth,
              source: source,
              embedded: true,
              notificationsOnly: true,
              previewLimit: 3,
              onUnreadChanged: counts.add,
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(requests.single.url.path, '/v1/me/inbox/page');
      expect(requests.single.url.queryParameters, {
        'view': 'notifications',
        'limit': '3',
      });
      expect(find.text('本人合成通知'), findsOneWidget);
      expect(counts.last, 129);
      expect(find.byKey(const Key('notifications-show-all')), findsOneWidget);
      expect(requests.where((r) => r.method == 'POST'), isEmpty);
    },
  );
  for (final replaceSource in [true, false]) {
    testWidgets(
      'same-key identity replaces or rejects original source: $replaceSource',
      (t) async {
        final authA = await signedInAuth(
          accountID: 'account-a',
          token: 'session-a',
        );
        final authB = await signedInAuth(
          accountID: 'account-b',
          token: 'session-b',
        );
        var readsA = 0, readsB = 0;
        RemoteInboxSource source(
          BirdtieAuthController auth,
          String title,
          void Function() read,
        ) => RemoteInboxSource(
          authorizationHeader: () => auth.authorizationHeader,
          apiBaseUrl: 'https://fixture',
          client: MockClient((request) async {
            if (request.url.path == '/v1/me/inbox') read();
            return http.Response.bytes(
              utf8.encode(
                jsonEncode({
                  'data': [
                    {
                      'id': '11111111-1111-4111-8111-111111111111',
                      'category': 'updates',
                      'title': title,
                      'detail': '当前账号',
                      'resourceType': 'community',
                      'resourceId': '22222222-2222-4222-8222-222222222222',
                      'createdAt': '2026-10-01T04:00:00Z',
                    },
                  ],
                }),
              ),
              200,
            );
          }),
        );
        final sourceA = source(authA, '甲的私人动态', () => readsA++);
        final sourceB = source(authB, '乙的私人动态', () => readsB++);
        Widget view(BirdtieAuthController auth, RemoteInboxSource source) =>
            MaterialApp(
              home: Scaffold(
                body: InboxPanel(
                  key: const ValueKey('same-inbox'),
                  auth: auth,
                  source: source,
                ),
              ),
            );
        await t.pumpWidget(view(authA, sourceA));
        await t.pumpAndSettle();
        expect(find.text('甲的私人动态'), findsOneWidget);
        await t.pumpWidget(view(authB, replaceSource ? sourceB : sourceA));
        await t.pumpAndSettle();
        expect(find.text('甲的私人动态'), findsNothing);
        expect(
          find.text('乙的私人动态'),
          replaceSource ? findsOneWidget : findsNothing,
        );
        expect(readsA, 1);
        expect(readsB, replaceSource ? 1 : 0);
        await t.pumpWidget(const SizedBox());
        sourceA.dispose();
        sourceB.dispose();
        authA.dispose();
        authB.dispose();
      },
    );
  }
  for (final kind in [
    'agent_task',
    'community_message',
    'business_claim_review',
  ]) {
    for (final mode in ['current', 'denied', 'workspace-aba']) {
      testWidgets('typed original notification opens current $kind: $mode', (
        t,
      ) async {
        final auth = await signedInAuth(accountID: taskDestinationOwner);
        final workspace = ValueNotifier<String?>(null),
            waiting = Completer<http.Response>();
        var postReads = 0, destinationReads = 0;
        final item = <String, dynamic>{
          'id': '55555555-5555-4555-8555-555555555555',
          'category': 'messages',
          'title': '原对象动态',
          'detail': '查看当前内容',
          'resourceType': kind,
          'resourceId': '66666666-6666-4666-8666-666666666666',
          'createdAt': '2026-10-01T04:00:00Z',
          if (kind == 'agent_task') 'targetTaskId': taskDestinationID,
          if (kind == 'community_message')
            'targetCommunityId': taskDestinationID,
          if (kind == 'business_claim_review') ...{
            'targetBusinessId': taskDestinationID,
            'semanticCategory': 'BUSINESS',
          },
        };
        http.Response reply(Object data, [int code = 200]) =>
            http.Response.bytes(utf8.encode(jsonEncode({'data': data})), code);
        final client = MockClient((r) async {
          if (r.url.path == '/v1/me/inbox') return reply([item]);
          if (r.url.path.endsWith('/read')) {
            postReads++;
            if (mode == 'denied') return http.Response('{}', 404);
            if (mode == 'workspace-aba') return waiting.future;
            return reply({...item, 'readAt': '2026-10-01T04:01:00Z'});
          }
          expect(r.method, 'GET');

          if (r.url.path == '/v1/me/businesses/$taskDestinationID/console') {
            destinationReads++;
            return business_fixture.reply(id: taskDestinationID);
          }
          if (r.url.path == '/v1/me/agent-tasks/$taskDestinationID') {
            destinationReads++;
            return reply(taskDestinationData());
          }
          if (r.url.path == '/v1/communities/$taskDestinationID') {
            destinationReads++;
            return reply({
              'id': taskDestinationID,
              'name': '原社群',
              'visibility': 'public',
              'joinPolicy': 'open',
              'status': 'active',
              'memberCount': 2,
            });
          }
          if (r.url.path.endsWith('/messages')) {
            return reply({'messages': [], 'hasMore': false});
          }
          if (r.url.path.endsWith('/conversation')) {
            return reply({
              'viewerAccountId': taskDestinationOwner,
              'joined': true,
              'canJoin': true,
              'canSend': true,
              'moderator': false,
            });
          }
          return http.Response('{}', 404);
        });
        final source = RemoteInboxSource(
          authorizationHeader: () => auth.authorizationHeader,
          client: client,
          apiBaseUrl: 'https://fixture',
        );
        expect(auth.signedIn, isTrue);
        expect((await source.load()).length, 1);
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: InboxPanel(
                auth: auth,
                source: source,
                workspaceChanges: workspace,
                organizationWorkspaceID: () => workspace.value,
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        await t.scrollUntilVisible(find.text('原对象动态'), 150);
        await t.tap(find.text('原对象动态'));
        await t.pump();
        if (mode == 'workspace-aba') {
          workspace.value = 'org';
          workspace.value = null;
          waiting.complete(reply({...item, 'readAt': '2026-10-01T04:01:00Z'}));
        }
        await t.pumpAndSettle();
        expect(postReads, 1);
        expect(
          find.byType(AgentTaskDetailPage),
          kind == 'agent_task' && mode == 'current'
              ? findsOneWidget
              : findsNothing,
        );
        expect(
          find.byType(CommunityConversationPage),
          kind == 'community_message' && mode == 'current'
              ? findsOneWidget
              : findsNothing,
        );
        expect(destinationReads, mode == 'current' ? greaterThan(0) : 0);
        expect(
          find.byType(BusinessClaimNotificationPage),
          kind == 'business_claim_review' && mode == 'current'
              ? findsOneWidget
              : findsNothing,
        );
        await t.pumpWidget(const SizedBox());
        source.dispose();
        auth.dispose();
        workspace.dispose();
      });
    }
  }
  for (final mode in ['failed', 'current-target', 'workspace-retired']) {
    testWidgets('already-read notification requires current read: $mode', (
      tester,
    ) async {
      final auth = await signedInAuth();
      final workspace = ValueNotifier<String?>(null);
      final pending = Completer<http.Response>();
      var posts = 0;
      const oldTarget = '33333333-3333-4333-8333-333333333333';
      const newTarget = '44444444-4444-4444-8444-444444444444';
      final item = <String, dynamic>{
        'id': '11111111-1111-4111-8111-111111111111',
        'category': 'updates',
        'title': '与你的明确意图匹配',
        'detail': '查看当前活动。',
        'resourceType': 'opportunity_available',
        'resourceId': '22222222-2222-4222-8222-222222222222',
        'targetActivityId': oldTarget,
        'createdAt': '2026-10-01T04:00:00Z',
        'readAt': '2026-10-01T04:01:00Z',
      };
      final client = MockClient((r) async {
        if (r.method == 'GET') {
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': [item],
              }),
            ),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        posts++;
        if (mode == 'workspace-retired') return pending.future;
        if (mode == 'failed') return http.Response('{}', 404);
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': {...item, 'targetActivityId': newTarget},
            }),
          ),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      final source = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'http://fixture',
      );
      String? opened;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: InboxPanel(
              auth: auth,
              source: source,
              workspaceChanges: workspace,
              organizationWorkspaceID: () => workspace.value,
              onOpenActivity: (id) => opened = id,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('与你的明确意图匹配'), 200);
      await tester.tap(find.text('与你的明确意图匹配'));
      await tester.pump();
      if (mode == 'workspace-retired') {
        workspace.value = 'organization';
        await tester.pump();
        pending.complete(
          http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': {...item, 'targetActivityId': newTarget},
              }),
            ),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          ),
        );
      }
      await tester.pumpAndSettle();
      expect(posts, 1);
      expect(opened, mode == 'current-target' ? newTarget : isNull);
      if (mode == 'failed') {
        expect(find.text('当前通知暂不可读取，请刷新后重试。'), findsOneWidget);
      }
      await tester.pumpWidget(const SizedBox.shrink());
      source.dispose();
      auth.dispose();
      workspace.dispose();
    });
  }
  testWidgets('Inbox uses an honest empty and retry state', (tester) async {
    final auth = await signedInAuth();
    var fail = false;
    final client = MockClient(
      (request) async => http.Response(
        fail ? '{"error":{"code":"offline"}}' : '{"data":[]}',
        fail ? 503 : 200,
      ),
    );
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'http://127.0.0.1:8080',
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(auth: auth, source: source),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('暂无通知'), findsOneWidget);
    expect(find.textContaining('Anna'), findsNothing);
    fail = true;
    await tester.drag(find.byType(ListView).first, const Offset(0, 500));
    await tester.pumpAndSettle();
    expect(find.textContaining('收件箱暂不可用'), findsOneWidget);
    fail = false;
    await tester.tap(find.textContaining('收件箱暂不可用'));
    await tester.pumpAndSettle();
    expect(find.text('暂无通知'), findsOneWidget);
    source.dispose();
    auth.dispose();
  });

  testWidgets('message item marks read and opens its real conversation', (
    tester,
  ) async {
    final auth = await signedInAuth();
    final item = {
      'id': '11111111-1111-4111-8111-111111111111',
      'category': 'messages',
      'title': '收到新消息',
      'detail': '打开对话查看消息。',
      'resourceType': 'conversation_message',
      'resourceId': '22222222-2222-4222-8222-222222222222',
      'targetConversationId': '33333333-3333-4333-8333-333333333333',
      'createdAt': '2026-10-01T04:00:00Z',
    };
    var markedRead = false;
    final client = MockClient((request) async {
      if (request.method == 'GET') {
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': [item],
            }),
          ),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      if (request.method == 'POST' && request.url.path.endsWith('/read')) {
        markedRead = true;
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': {...item, 'readAt': '2026-10-01T04:01:00Z'},
            }),
          ),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      return http.Response('{}', 404);
    });
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'http://127.0.0.1:8080',
    );
    String? opened;
    expect((await source.load()).length, 1);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(
            auth: auth,
            source: source,
            onOpenConversation: (id) => opened = id,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('收到新消息'), 200);
    await tester.tap(find.text('收到新消息'));
    await tester.pumpAndSettle();
    expect(markedRead, isTrue);
    expect(opened, '33333333-3333-4333-8333-333333333333');
    source.dispose();
    auth.dispose();
  });
}
