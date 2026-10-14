import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/now_social_edge.dart';
import 'package:birdtie_client/src/workspace/now_social_edge_source.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const owner = '11111111-1111-4111-8111-111111111111';
String chatID(int n) =>
    '${n.toString().padLeft(8, '0')}-2222-4222-8222-222222222222';
Map<String, Object> chat(int n) => {
  'id': chatID(n),
  'otherAccountId': chatID(n + 100),
  'otherName': '联系人$n 中文长名称 English',
  'createdAt': '2026-10-11T12:00:00Z',
  'unreadCount': n == 5 ? 3 : 0,
  'lastMessagePreview': '不应在首层出现的消息正文$n',
};
http.Response page(List<Map<String, Object>> rows, {String? next}) =>
    http.Response.bytes(
      utf8.encode(
        jsonEncode({
          'data': {'items': rows, 'nextCursor': next, 'unreadCount': 3},
        }),
      ),
      200,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );

Future<void> mount(
  WidgetTester t,
  NowSocialEdgeSource source,
  ValueNotifier<int> identity, {
  String? Function()? account,
  double scale = 1,
  Widget? thread,
  ValueChanged<HumanConversation>? choose,
  ValueChanged<int>? unread,
}) async {
  await t.pumpWidget(
    MaterialApp(
      home: MediaQuery(
        data: const MediaQueryData(
          size: Size(390, 844),
        ).copyWith(textScaler: TextScaler.linear(scale)),
        child: Scaffold(
          body: Align(
            alignment: Alignment.topRight,
            child: SizedBox(
              height: 360,
              child: NowSocialEdge(
                open: true,
                source: source,
                identityChanges: identity,
                current: () => true,
                accountID: account ?? () => owner,
                workspaceID: () => null,
                onOpenAuthor: (_) {},
                onOpenConversation: (_) {},
                onChooseConversation: choose,
                onUnreadMessagesChanged: unread,
                onOpenManagement: () {},
                onSignIn: () {},
                onUsePersonal: () {},
                onFindPeople: () {},
                onScanCode: () {},
                onShowOwnCode: () {},
                conversationBody: thread,
                onOpenMessageRequests: () {},
              ),
            ),
          ),
        ),
      ),
    ),
  );
  await t.pumpAndSettle();
}

void main() {
  testWidgets(
    'recent first level is four avatars, exact unread summary and all conversations reaches older pages',
    (t) async {
      final identity = ValueNotifier(0);
      addTearDown(identity.dispose);
      final paths = <String>[];
      HumanConversation? chosen;
      int? unread;
      final wire = MockClient((r) async {
        paths.add(r.url.toString());
        expect(r.url.path, '/v1/me/conversations/page');
        expect(r.url.queryParameters['limit'], '20');
        return r.url.queryParameters.containsKey('cursor')
            ? page([chat(21)])
            : page([for (var n = 1; n <= 20; n++) chat(n)], next: 'older_page');
      });
      final source = ConnectionSource(
        client: wire,
        apiBaseUrl: 'http://rail.test',
        authorizationHeader: () => 'Bearer fixture',
      );
      await mount(
        t,
        NowSocialEdgeSource(source),
        identity,
        choose: (v) => chosen = v,
        unread: (v) => unread = v,
      );
      expect(
        unread,
        3,
      ); // Includes an unread conversation outside the first four.
      expect(find.byKey(const Key('edge-recent-avatars')), findsOneWidget);
      expect(
        find.textContaining('消息正文'),
        findsNWidgets(4),
        reason: '2026-10-13录屏整改：近期联系人增加一行真实消息摘要。',
      );
      expect(find.byKey(const Key('edge-add-contact')), findsNothing);
      expect(find.byKey(const Key('edge-message-requests')), findsNothing);
      expect(
        find.byKey(ValueKey('edge-conversation-${chatID(5)}')),
        findsNothing,
      );
      await t.tap(find.byKey(const Key('edge-all-conversations')));
      await t.pumpAndSettle();
      expect(find.byKey(const Key('edge-tab-contacts')), findsOneWidget);
      expect(find.byKey(const Key('edge-message-requests')), findsOneWidget);
      await t.scrollUntilVisible(
        find.byKey(const Key('edge-more-conversations')),
        250,
        scrollable: find
            .descendant(
              of: find.byKey(const Key('now-social-edge')),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      await t.tap(find.byKey(const Key('edge-more-conversations')));
      await t.pumpAndSettle();
      await t.scrollUntilVisible(
        find.byKey(ValueKey('edge-conversation-${chatID(21)}')),
        100,
        scrollable: find
            .descendant(
              of: find.byKey(const Key('now-social-edge')),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      await t.tap(find.byKey(ValueKey('edge-conversation-${chatID(21)}')));
      expect(chosen?.id, chatID(21));
      expect(paths.length, 2);
      expect(find.byKey(const Key('now-social-edge-fade')), findsNothing);
      await t.pumpWidget(const SizedBox());
      source.dispose();
    },
  );

  testWidgets(
    'two labelled top entrances have separate actions and 48dp touch targets',
    (t) async {
      var contacts = 0, notifications = 0;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: TopControls(
              onSidebar: () {},
              onInbox: () => contacts++,
              onNotifications: () => notifications++,
              onTools: () {},
              contextLabel: 'Aberdeen',
              hasUnreadMessages: true,
            ),
          ),
        ),
      );
      expect(
        t.getSize(find.byKey(const Key('now-social-button'))),
        const Size(48, 48),
      );
      expect(
        t.getSize(find.byKey(const Key('now-notifications-button'))),
        const Size(48, 48),
      );
      await t.tap(find.byTooltip('联系'));
      await t.tap(find.byTooltip('通知'));
      expect(contacts, 1);
      expect(notifications, 1);
      expect(find.byTooltip('收件箱'), findsNothing);
    },
  );

  testWidgets(
    'guest issues no private page request and owns one short login guide',
    (t) async {
      final identity = ValueNotifier(0);
      addTearDown(identity.dispose);
      var requests = 0;
      final source = ConnectionSource(
        apiBaseUrl: 'http://rail.test',
        authorizationHeader: () => null,
        client: MockClient((_) async {
          requests++;
          return page([]);
        }),
      );
      await mount(
        t,
        NowSocialEdgeSource(source),
        identity,
        account: () => null,
        scale: 2,
      );
      expect(requests, 0);
      expect(find.text('登录，与朋友保持联系'), findsOneWidget);
      expect(find.text('登录'), findsOneWidget);
      expect(find.text('重试'), findsNothing);
      await t.pumpWidget(const SizedBox());
      source.dispose();
    },
  );

  testWidgets(
    'late page result cannot restore prior owner or disclose its names after ABA',
    (t) async {
      final identity = ValueNotifier(0);
      addTearDown(identity.dispose);
      final pending = Completer<http.Response>();
      var epoch = 0, calls = 0;
      String? account = owner;
      final source = ConnectionSource(
        apiBaseUrl: 'http://rail.test',
        authorizationHeader: () => 'Bearer fixture',
        client: MockClient((_) async {
          calls++;
          return calls == 1 ? pending.future : page([]);
        }),
      );
      // Mount without settle: the first authorised request deliberately remains pending.
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NowSocialEdge(
              open: true,
              source: NowSocialEdgeSource(source, identityEpoch: () => epoch),
              identityChanges: identity,
              current: () => true,
              accountID: () => account,
              workspaceID: () => null,
              onOpenAuthor: (_) {},
              onOpenConversation: (_) {},
              onOpenManagement: () {},
              onSignIn: () {},
              onUsePersonal: () {},
            ),
          ),
        ),
      );
      await t.pump();
      account = chatID(99);
      epoch++;
      identity.value++;
      account = owner;
      epoch++;
      identity.value++;
      pending.complete(page([chat(1)]));
      await t.pumpAndSettle();
      expect(find.textContaining('联系人1'), findsNothing);
      await t.pumpWidget(const SizedBox());
      source.dispose();
    },
  );
}
