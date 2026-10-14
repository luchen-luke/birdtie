import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'package:birdtie_client/src/workspace/now_social_edge.dart';
import 'package:birdtie_client/src/workspace/now_social_edge_source.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:birdtie_client/src/workspace/now_glass_surface.dart';
import 'package:birdtie_client/src/workspace/person_avatar.dart';
import 'person_avatar_fixture.dart' show avatarMetadata;

const ownerID = '11111111-1111-4111-8111-111111111111';
const friendID = '22222222-2222-4222-8222-222222222222';
const tieID = '33333333-3333-4333-8333-333333333333';
const chatID = '44444444-4444-4444-8444-444444444444';
const followID = '55555555-5555-4555-8555-555555555555';
const organizationID = '66666666-6666-4666-8666-666666666666';

http.Response wire(List<Map<String, Object?>> rows, {int status = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': rows})),
      status,
      headers: {'Content-Type': 'application/json'},
    );

http.Response pageWire(
  List<Map<String, Object?>> rows, {
  int status = 200,
  String? nextCursor,
  int unreadCount = 0,
}) => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': {
        'items': rows,
        'nextCursor': nextCursor,
        'unreadCount': unreadCount,
      },
    }),
  ),
  status,
  headers: {'Content-Type': 'application/json'},
);

http.Response contactsPageWire(
  List<Map<String, Object?>> rows, {
  int status = 200,
  String? nextCursor,
}) => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': {'items': rows, 'nextCursor': nextCursor},
    }),
  ),
  status,
  headers: {'Content-Type': 'application/json'},
);

http.Response emptyWire(String path, {int status = 200}) =>
    path == '/v1/me/conversations/page'
    ? pageWire([], status: status)
    : contactsPageWire([], status: status);

http.Response originalWire(String path, {String name = '真实好友 River'}) =>
    switch (path) {
      '/v1/me/ties/page' => contactsPageWire([
        {
          'id': tieID,
          'otherAccountId': friendID,
          'otherName': name,
          'createdAt': '2026-10-10T11:00:00Z',
        },
      ]),
      '/v1/me/conversations/page' => pageWire([
        {
          'id': chatID,
          'otherAccountId': friendID,
          'otherName': name,
          'unreadCount': 7,
          'createdAt': '2026-10-10T11:00:00Z',
        },
      ], unreadCount: 7),
      '/v1/me/follows/page' => contactsPageWire([
        {
          'id': followID,
          'target': {'targetType': 'PERSON', 'targetId': friendID},
          'label': name,
          'createdAt': '2026-10-10T11:00:00Z',
        },
      ]),
      _ => throw StateError('Unexpected path $path'),
    };

class _Model {
  final identity = ValueNotifier<int>(0);
  final shape = ValueNotifier<int>(0);
  String? token = 'Bearer alpha', owner = ownerID, workspace;
  bool open = true, current = true, dark = false;
  double scale = 1;
  double? availableHeight;
  NowSocialSection? externalSection;
  void identityChanged() => identity.value++;
  void rebuild() => shape.value++;
  void dispose() {
    identity.dispose();
    shape.dispose();
  }
}

Widget harness(
  _Model model,
  NowSocialEdgeSource source, {
  ValueChanged<String>? onAuthor,
  ValueChanged<String>? onConversation,
  VoidCallback? onManagement,
  VoidCallback? onSignIn,
  VoidCallback? onPersonal,
  VoidCallback? onFindPeople,
  VoidCallback? onScanCode,
  VoidCallback? onShowOwnCode,
  bool Function(NowSocialSection)? onSectionChanging,
  ValueChanged<NowSocialSection>? onSectionChanged,
  NowSocialSection initialSection = NowSocialSection.messages,
  bool initialFollowedContacts = false,
  ValueChanged<bool>? onContactsFilterChanged,
  Widget? notificationBody,
  Widget? conversationBody,
  Widget? messageRequestsBody,
  VoidCallback? onRequests,
  VoidCallback? onCloseRequests,
  VoidCallback? onCloseConversation,
}) => ValueListenableBuilder<int>(
  valueListenable: model.shape,
  builder: (_, _, _) => MaterialApp(
    theme: model.dark ? ThemeData.dark() : ThemeData.light(),
    home: Builder(
      builder: (context) => MediaQuery(
        data: MediaQuery.of(
          context,
        ).copyWith(textScaler: TextScaler.linear(model.scale)),
        child: Scaffold(
          body: Stack(
            children: [
              const Positioned.fill(
                child: ColoredBox(color: Color(0xFFBCD9E5)),
              ),
              Positioned(
                left: 0,
                top: 0,
                child: Column(
                  children: [
                    IconButton(
                      key: const Key('harness-notifications'),
                      tooltip: '通知入口',
                      icon: const Icon(Icons.notifications_outlined),
                      onPressed: () {
                        if (onSectionChanging?.call(
                              NowSocialSection.notifications,
                            ) ==
                            false)
                          return;
                        model.externalSection = NowSocialSection.notifications;
                        onSectionChanged?.call(NowSocialSection.notifications);
                        model.rebuild();
                      },
                    ),
                    IconButton(
                      key: const Key('harness-contact'),
                      tooltip: '联系入口',
                      icon: const Icon(Icons.people_outline),
                      onPressed: () {
                        if (onSectionChanging?.call(
                              NowSocialSection.messages,
                            ) ==
                            false)
                          return;
                        model.externalSection = NowSocialSection.messages;
                        onSectionChanged?.call(NowSocialSection.messages);
                        model.rebuild();
                      },
                    ),
                  ],
                ),
              ),
              Positioned(
                top: 0,
                bottom: model.availableHeight == null ? 0 : null,
                height: model.availableHeight,
                right: 0,
                child: NowSocialEdge(
                  open: model.open,
                  source: source,
                  identityChanges: model.identity,
                  current: () => model.current,
                  accountID: () => model.owner,
                  workspaceID: () => model.workspace,
                  onOpenAuthor: onAuthor ?? (_) {},
                  onOpenConversation: onConversation ?? (_) {},
                  onOpenManagement: onManagement ?? () {},
                  onSignIn: onSignIn ?? () {},
                  onUsePersonal: onPersonal ?? () {},
                  onFindPeople: onFindPeople,
                  onScanCode: onScanCode,
                  onShowOwnCode: onShowOwnCode,
                  onSectionChanging: onSectionChanging,
                  onSectionChanged: onSectionChanged,
                  initialSection: model.externalSection ?? initialSection,
                  initialFollowedContacts: initialFollowedContacts,
                  onContactsFilterChanged: onContactsFilterChanged,
                  notificationBody: notificationBody,
                  conversationBody: conversationBody,
                  messageRequestsBody: messageRequestsBody,
                  onOpenMessageRequests: onRequests,
                  onCloseMessageRequests: onCloseRequests,
                  onCloseConversation: onCloseConversation,
                ),
              ),
            ],
          ),
        ),
      ),
    ),
  ),
);

void expectSocialViewport(WidgetTester tester, {double scale = 1}) {
  final edge = find.byKey(const Key('now-social-edge'));
  final compact = find
      .byKey(const Key('edge-recent-avatars'))
      .evaluate()
      .isNotEmpty;
  final width = tester.getSize(edge).width;
  expect(
    width,
    math.min(
      tester.view.physicalSize.width - 48,
      (compact ? NowUiTokens.socialRailWidth : NowUiTokens.socialPanelWidth) *
          scale.clamp(1, 1.5),
    ),
  );
  expect(width, greaterThanOrEqualTo(NowUiTokens.socialRailWidth));
  expect(
    tester.getSize(edge).height,
    lessThanOrEqualTo(NowUiTokens.socialPanelMaxHeight),
  );
  expect(
    find.descendant(of: edge, matching: find.byType(BackdropFilter)),
    findsOneWidget,
  );
  expect(find.byKey(const Key('now-social-edge-fade')), findsNothing);
  expect(
    find.descendant(of: edge, matching: find.byType(NowGlassSurface)),
    findsOneWidget,
  );
}

Future<void> selectSection(
  WidgetTester tester,
  NowSocialSection section,
) async {
  Future<void> tap(Key key) async {
    await tester.tap(find.byKey(key));
    await tester.pumpAndSettle();
  }

  if (section == NowSocialSection.notifications) {
    await tap(const Key('harness-notifications'));
  } else if (section == NowSocialSection.messages) {
    if (find.byKey(const Key('edge-back-messages')).evaluate().isNotEmpty) {
      await tap(const Key('edge-back-messages'));
    }
    if (find.byKey(const Key('edge-back-recent')).evaluate().isNotEmpty) {
      await tap(const Key('edge-back-recent'));
    } else if (find
        .byKey(const Key('edge-recent-avatars'))
        .evaluate()
        .isEmpty) {
      await tap(const Key('harness-contact'));
    }
  } else {
    if (find.byKey(const Key('edge-back-messages')).evaluate().isNotEmpty) {
      await tap(const Key('edge-back-messages'));
    }
    if (find.byKey(const Key('edge-filter-friends')).evaluate().isNotEmpty)
      return;
    if (find.byKey(const Key('edge-all-conversations')).evaluate().isEmpty &&
        find.byKey(const ValueKey('edge-tab-contacts')).evaluate().isEmpty) {
      await tap(const Key('harness-contact'));
    }
    if (find.byKey(const Key('edge-all-conversations')).evaluate().isNotEmpty) {
      await tap(const Key('edge-all-conversations'));
    }
    await tap(const ValueKey('edge-tab-contacts'));
  }
}

void main() {
  testWidgets(
    'normal typed non-person follows use entity icons without private person avatar or false row action',
    (tester) async {
      final model = _Model();
      final requests = <http.Request>[];
      final privateAvatar = avatarMetadata(owner: ownerID);
      final targets = [
        (
          type: 'COMMUNITY',
          target: friendID,
          id: followID,
          label: '合成社群',
          icon: Icons.groups_outlined,
        ),
        (
          type: 'ORGANIZATION',
          target: organizationID,
          id: tieID,
          label: '合成机构',
          icon: Icons.account_balance_outlined,
        ),
        (
          type: 'BUSINESS',
          target: chatID,
          id: chatID,
          label: '合成商家',
          icon: Icons.storefront_outlined,
        ),
      ];
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((request) async {
          requests.add(request);
          expect(request.method, 'GET');
          expect(request.url.path, '/v1/me/follows/page');
          expect(request.bodyBytes, isEmpty);
          expect(request.headers['Authorization'], model.token);
          return contactsPageWire([
            for (final entry in targets)
              {
                'id': entry.id,
                'target': {'targetType': entry.type, 'targetId': entry.target},
                'label': entry.label,
                'createdAt': '2026-10-10T11:00:00Z',
                // A private person's metadata must never supply a non-person icon.
                'avatar': privateAvatar,
              },
          ]);
        }),
      );
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      await tester.pumpWidget(
        harness(
          model,
          NowSocialEdgeSource(api),
          initialSection: NowSocialSection.contacts,
          initialFollowedContacts: true,
        ),
      );
      await tester.pumpAndSettle();
      expect(model.scale, 1);
      expect(requests, hasLength(1));
      for (final entry in targets) {
        final row = find.byKey(
          ValueKey('edge-follow-${entry.type}-${entry.target}'),
        );
        expect(row, findsOneWidget);
        expect(
          find.descendant(of: row, matching: find.byIcon(entry.icon)),
          findsOneWidget,
        );
        expect(
          find.descendant(of: row, matching: find.byType(PersonAvatar)),
          findsNothing,
        );
        expect(
          tester.widget<ListTile>(row).onTap,
          isNull,
          reason:
              'The real harness has no target-detail callback; an entity icon cannot invent an action.',
        );
      }
      expect(find.byType(PersonAvatar), findsNothing);
      expect(requests.where((r) => r.url.path.contains('/avatar/')), isEmpty);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets('guest login remains reachable at 2x font in 96 dp viewport', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final model = _Model()
      ..scale = 2
      ..availableHeight = 96
      ..owner = null
      ..token = null;
    var reads = 0, logins = 0;
    final api = ConnectionSource(
      authorizationHeader: () => model.token,
      apiBaseUrl: 'https://edge.fixture',
      client: MockClient((r) async {
        ++reads;
        return emptyWire(r.url.path);
      }),
    );
    addTearDown(model.dispose);
    addTearDown(api.dispose);
    await tester.pumpWidget(
      harness(model, NowSocialEdgeSource(api), onSignIn: () => ++logins),
    );
    await tester.pumpAndSettle();
    expect(tester.getSize(find.byKey(const Key('now-social-edge'))).height, 96);
    await tester.drag(
      find.byType(SingleChildScrollView),
      const Offset(0, -200),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('edge-sign-in')));
    expect(logins, 1);
    expect(reads, 0);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets(
    'compact network failure shows the whole recovery action before scrolling',
    (tester) async {
      tester.view.physicalSize = const Size(320, 640);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final model = _Model()..availableHeight = 360;
      var reads = 0;
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((request) async {
          reads++;
          if (reads == 1) throw http.ClientException('synthetic offline');
          return emptyWire(request.url.path);
        }),
      );
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      await tester.pumpWidget(harness(model, NowSocialEdgeSource(api)));
      await tester.pumpAndSettle();
      final retry = find.byKey(const Key('edge-retry'));
      final label = find.descendant(of: retry, matching: find.text('重试'));
      final edge = find.byKey(const Key('now-social-edge'));
      final viewport = find.byType(SingleChildScrollView);
      final scrollable = find.byType(Scrollable);
      void expectFullyVisible(Finder child) {
        final visible = tester.getRect(viewport);
        final rect = tester.getRect(child);
        expect(rect.left, greaterThanOrEqualTo(visible.left - .001));
        expect(rect.right, lessThanOrEqualTo(visible.right + .001));
        expect(rect.top, greaterThanOrEqualTo(visible.top - .001));
        expect(rect.bottom, lessThanOrEqualTo(visible.bottom + .001));
      }

      expect(viewport, findsOneWidget);
      expect(
        scrollable,
        findsOneWidget,
        reason: 'No fixed inner error viewport clips the recovery action',
      );
      expect(
        tester.state<ScrollableState>(scrollable).position.maxScrollExtent,
        closeTo(0, .001),
      );
      expect(tester.getSize(edge).height, lessThan(360));
      expect(tester.getSize(retry).height, greaterThanOrEqualTo(48));
      expectFullyVisible(find.text('连接中断，请重试。'));
      expectFullyVisible(retry);
      expectFullyVisible(label);
      expectFullyVisible(find.byKey(const Key('edge-all-conversations')));
      expect(reads, 1);

      model.scale = 2;
      model.availableHeight = 176;
      model.rebuild();
      await tester.pumpAndSettle();
      expect(tester.getSize(edge).height, lessThanOrEqualTo(176));
      expect(viewport, findsOneWidget);
      expect(scrollable, findsOneWidget);
      expect(
        tester.state<ScrollableState>(scrollable).position.maxScrollExtent,
        greaterThan(0),
      );
      await tester.scrollUntilVisible(
        retry,
        80,
        scrollable: scrollable,
        maxScrolls: 8,
      );
      await tester.pumpAndSettle();
      expect(tester.getSize(retry).height, greaterThanOrEqualTo(48));
      expectFullyVisible(retry);
      expectFullyVisible(label);
      expect(retry.hitTestable(), findsOneWidget);
      expect(
        reads,
        1,
        reason: 'Changing the viewport does not repeat a private read',
      );
      await tester.tap(retry);
      await tester.pumpAndSettle();
      expect(reads, 2);
      expect(find.text('暂无会话'), findsOneWidget);
      expect(find.byKey(const Key('edge-retry')), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final status in [0, 200, 401, 403, 503]) {
    testWidgets(
      'short keyboard viewport at 2x font status $status scrolls one status safely',
      (tester) async {
        tester.view.physicalSize = const Size(320, 640);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final model = _Model()
          ..scale = 2
          ..availableHeight = 176;
        var actions = 0;
        final api = ConnectionSource(
          authorizationHeader: () => model.token,
          apiBaseUrl: 'https://edge.fixture',
          client: MockClient((r) async {
            if (status == 0) throw http.ClientException('synthetic offline');
            return emptyWire(r.url.path, status: status);
          }),
        );
        addTearDown(model.dispose);
        addTearDown(api.dispose);
        await tester.pumpWidget(
          harness(
            model,
            NowSocialEdgeSource(api),
            onFindPeople: () => ++actions,
            onSignIn: () => ++actions,
          ),
        );
        await tester.pumpAndSettle();
        expect(
          tester.getSize(find.byKey(const Key('now-social-edge'))).height,
          lessThanOrEqualTo(176),
        );
        expect(
          find.byType(SingleChildScrollView),
          status == 200 ? findsNothing : findsOneWidget,
        );
        if (status == 200) {
          expect(find.text('暂无会话'), findsOneWidget);
          await selectSection(tester, NowSocialSection.contacts);
          await tester.drag(
            find.byType(SingleChildScrollView),
            const Offset(0, -200),
          );
          await tester.pumpAndSettle();
          await tester.tap(find.text('搜索用户'));
          expect(actions, 1);
        } else if (status == 401) {
          await tester.drag(
            find.byType(SingleChildScrollView),
            const Offset(0, -200),
          );
          await tester.pumpAndSettle();
          await tester.tap(find.byKey(const Key('edge-retry')));
          expect(actions, 1);
        }
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  for (final condition in [
    (width: 390.0, scale: 1.0),
    (width: 376.0, scale: 1.3),
    (width: 320.0, scale: 2.0),
  ]) {
    testWidgets(
      'bounded one category, long name and preview ${condition.width}/${condition.scale}',
      (tester) async {
        tester.view.physicalSize = Size(condition.width, 800);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final model = _Model()..scale = condition.scale;
        final paths = <String>[];
        final api = ConnectionSource(
          authorizationHeader: () => model.token,
          apiBaseUrl: 'https://edge.fixture',
          client: MockClient((request) async {
            paths.add(request.url.path);
            return originalWire(request.url.path, name: '长中文朋友 English 名字混排');
          }),
        );
        addTearDown(model.dispose);
        addTearDown(api.dispose);
        await tester.pumpWidget(
          harness(model, NowSocialEdgeSource(api), onFindPeople: () {}),
        );
        await tester.pumpAndSettle();
        expect(paths, ['/v1/me/conversations/page']);
        expectSocialViewport(tester, scale: condition.scale);
        final name = tester.widget<Text>(find.text('长中文朋友 English 名字混排'));
        expect(name.maxLines, 1);
        expect(name.overflow, TextOverflow.ellipsis);
        expect(
          find.byKey(const ValueKey('edge-friend-$friendID')),
          findsNothing,
        );
        expect(find.byKey(const Key('edge-management')), findsNothing);
        expect(find.text('联系'), findsNothing, reason: '删除首层冗余视觉标题');
        await selectSection(tester, NowSocialSection.contacts);
        expect(paths, ['/v1/me/conversations/page', '/v1/me/ties/page']);
        expect(
          find.byKey(const ValueKey('edge-conversation-$chatID')),
          findsNothing,
        );
        expect(find.text('联系人'), findsOneWidget);
        expect(
          tester.getSize(find.byKey(const Key('edge-add-contact'))).height,
          greaterThanOrEqualTo(48),
        );
        await tester.tap(find.byKey(const ValueKey('edge-filter-follows')));
        await tester.pumpAndSettle();
        expect(paths.last, '/v1/me/follows/page');
        expect(
          find.byKey(const ValueKey('edge-friend-$friendID')),
          findsNothing,
        );
        expect(
          find.byKey(const ValueKey('edge-follow-PERSON-$friendID')),
          findsOneWidget,
        );
        expectSocialViewport(tester, scale: condition.scale);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  for (final personal in [false, true]) {
    for (final scale in [1.0, 2.0]) {
      testWidgets(
        personal
            ? 'organization no private reads $scale'
            : 'guest short single login $scale',
        (tester) async {
          tester.view.physicalSize = const Size(320, 640);
          tester.view.devicePixelRatio = 1;
          addTearDown(tester.view.resetPhysicalSize);
          addTearDown(tester.view.resetDevicePixelRatio);
          final model = _Model()..scale = scale;
          if (personal) {
            model.workspace = organizationID;
          } else {
            model.token = null;
            model.owner = null;
          }
          var requests = 0, actions = 0;
          final api = ConnectionSource(
            authorizationHeader: () => model.token,
            apiBaseUrl: 'https://edge.fixture',
            client: MockClient((r) async {
              ++requests;
              return originalWire(r.url.path);
            }),
          );
          addTearDown(model.dispose);
          addTearDown(api.dispose);
          await tester.pumpWidget(
            harness(
              model,
              NowSocialEdgeSource(api),
              onSignIn: () => ++actions,
              onPersonal: () => ++actions,
            ),
          );
          await tester.pumpAndSettle();
          expect(requests, 0);
          expect(find.byKey(const ValueKey('edge-tab-contacts')), findsNothing);
          expect(find.byType(CircularProgressIndicator), findsNothing);
          expect(find.text('重试'), findsNothing);
          expect(find.text('0'), findsNothing);
          if (!personal) {
            expect(find.text('登录，与朋友保持联系'), findsOneWidget);
            expect(find.text('登录'), findsOneWidget);
            expect(
              tester.getSize(find.byKey(const Key('now-social-edge'))).height,
              lessThan(200),
            );
          }
          final action = find.byKey(
            Key(personal ? 'edge-use-personal' : 'edge-sign-in'),
          );
          expect(tester.getSize(action).height, greaterThanOrEqualTo(48));
          await tester.tap(action);
          expect(actions, 1);
          expect(requests, 0);
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox());
        },
      );
    }
  }

  testWidgets(
    'same owner relogin restores contacts filter; account B resets and old callbacks retire',
    (tester) async {
      final model = _Model(), paths = <String>[], authors = <String>[];
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((r) async {
          paths.add(r.url.path);
          return originalWire(r.url.path);
        }),
      );
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      await tester.pumpWidget(
        harness(model, NowSocialEdgeSource(api), onAuthor: authors.add),
      );
      await tester.pumpAndSettle();
      await selectSection(tester, NowSocialSection.contacts);
      await tester.tap(find.byKey(const ValueKey('edge-filter-follows')));
      await tester.pumpAndSettle();
      final saved = tester
          .widget<ListTile>(
            find.byKey(const ValueKey('edge-follow-PERSON-$friendID')),
          )
          .onTap!;
      model.token = null;
      model.owner = null;
      model.identityChanged();
      await tester.pumpAndSettle();
      expect(find.text('登录，与朋友保持联系'), findsOneWidget);
      expect(find.text('真实好友 River'), findsNothing);
      final before = paths.length;
      model.token = 'Bearer new-alpha';
      model.owner = ownerID;
      model.identityChanged();
      await tester.pumpAndSettle();
      expect(paths.length, before + 1);
      expect(paths.last, '/v1/me/follows/page');
      expect(find.text('联系人'), findsOneWidget);
      saved();
      expect(authors, isEmpty);
      model.token = 'Bearer beta';
      model.owner = friendID;
      model.identityChanged();
      await tester.pumpAndSettle();
      expect(paths.last, '/v1/me/conversations/page');
      expect(find.text('联系'), findsNothing, reason: '删除首层冗余视觉标题');
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final savedSection in [
    NowSocialSection.contacts,
    NowSocialSection.notifications,
  ]) {
    testWidgets(
      'remount restores ${savedSection.name} and followed filter; new owner resets messages',
      (tester) async {
        final model = _Model(), paths = <String>[];
        var section = NowSocialSection.messages, followed = false;
        final sections = <NowSocialSection>[], filters = <bool>[];
        final api = ConnectionSource(
          authorizationHeader: () => model.token,
          apiBaseUrl: 'https://edge.fixture',
          client: MockClient((request) async {
            paths.add(request.url.path);
            return originalWire(request.url.path);
          }),
        );
        addTearDown(model.dispose);
        addTearDown(api.dispose);
        final source = NowSocialEdgeSource(api);
        Widget mount() => harness(
          model,
          source,
          initialSection: section,
          initialFollowedContacts: followed,
          onSectionChanged: (value) {
            section = value;
            sections.add(value);
          },
          onContactsFilterChanged: (value) {
            followed = value;
            filters.add(value);
          },
          notificationBody: const Text('独立通知内容'),
        );
        await tester.pumpWidget(mount());
        await tester.pumpAndSettle();
        await selectSection(tester, NowSocialSection.contacts);
        await tester.tap(find.byKey(const ValueKey('edge-filter-follows')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const ValueKey('edge-filter-follows')));
        await tester.pumpAndSettle();
        expect(filters, [true]);
        if (savedSection == NowSocialSection.notifications) {
          await selectSection(tester, savedSection);
        }
        final readsBeforeUnmount = paths.length;
        final changesBeforeUnmount = sections.length;
        await tester.pumpWidget(const SizedBox());
        model.token = null;
        model.owner = null;
        await tester.pumpWidget(mount());
        await tester.pumpAndSettle();
        expect(find.text('登录，与朋友保持联系'), findsOneWidget);
        expect(paths, hasLength(readsBeforeUnmount));
        model.token = 'Bearer alpha-restored';
        model.owner = ownerID;
        model.identityChanged();
        await tester.pumpAndSettle();
        expect(sections, hasLength(changesBeforeUnmount));
        expect(filters, [true]);
        if (savedSection == NowSocialSection.contacts) {
          expect(find.text('联系人'), findsOneWidget);
          expect(paths, hasLength(readsBeforeUnmount + 1));
          expect(paths.last, '/v1/me/follows/page');
          expect(
            find.byKey(const ValueKey('edge-follow-PERSON-$friendID')),
            findsOneWidget,
          );
        } else {
          expect(find.text('通知'), findsOneWidget);
          expect(find.text('独立通知内容'), findsOneWidget);
          expect(paths, hasLength(readsBeforeUnmount));
          await selectSection(tester, NowSocialSection.contacts);
          expect(paths.last, '/v1/me/follows/page');
        }
        model.token = 'Bearer beta';
        model.owner = friendID;
        model.identityChanged();
        await tester.pumpAndSettle();
        expect(find.text('联系'), findsNothing, reason: '删除首层冗余视觉标题');
        expect(paths.last, '/v1/me/conversations/page');
        await selectSection(tester, NowSocialSection.contacts);
        expect(paths.last, '/v1/me/ties/page');
        expect(
          find.byKey(const ValueKey('edge-friend-$friendID')),
          findsOneWidget,
        );
        await tester.tap(find.byKey(const ValueKey('edge-filter-follows')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const ValueKey('edge-filter-friends')));
        await tester.pumpAndSettle();
        expect(filters, [true, true, false]);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets(
    'ABA retires pending reads, late 401 cannot reauthenticate returned owner',
    (tester) async {
      final model = _Model(), pending = Completer<http.Response>();
      var reads = 0, reauth = 0;
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((r) async {
          ++reads;
          return reads == 1 ? pending.future : emptyWire(r.url.path);
        }),
      );
      final source = NowSocialEdgeSource(
        api,
        identityEpoch: () => model.identity.value,
        onUnauthorized: (_) async {
          ++reauth;
        },
      );
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      await tester.pumpWidget(harness(model, source));
      await tester.pump();
      model.token = 'Bearer beta';
      model.owner = friendID;
      model.identityChanged();
      model.token = 'Bearer alpha';
      model.owner = ownerID;
      model.identityChanged();
      pending.complete(wire([], status: 401));
      await tester.pumpAndSettle();
      expect(reauth, 0);
      expect(reads, 3);
      expect(find.text('暂无会话'), findsOneWidget);
      expect(find.text('登录'), findsNothing);
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final status in [401, 403, 503]) {
    testWidgets('private read HTTP $status has distinct recovery', (
      tester,
    ) async {
      final model = _Model()..scale = 2;
      var reads = 0, login = 0;
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((r) async {
          ++reads;
          return reads == 1
              ? emptyWire(r.url.path, status: status)
              : emptyWire(r.url.path);
        }),
      );
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      await tester.pumpWidget(
        harness(model, NowSocialEdgeSource(api), onSignIn: () => ++login),
      );
      await tester.pumpAndSettle();
      expect(find.text('还没有消息'), findsNothing);
      expect(
        find.text(const NowSocialReadFailure(401).message),
        status == 401 ? findsOneWidget : findsNothing,
      );
      if (status == 403) {
        expect(find.text('当前身份没有访问权限。'), findsOneWidget);
        expect(find.text('登录'), findsNothing);
        expect(find.text('重试'), findsNothing);
      } else {
        final action = find.byKey(const Key('edge-retry'));
        await tester.ensureVisible(action);
        await tester.pumpAndSettle();
        expect(action.hitTestable(), findsOneWidget);
        await tester.tap(action);
        await tester.pumpAndSettle();
        expect(login, status == 401 ? 1 : 0);
        expect(reads, status == 401 ? 1 : 2);
      }
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('offline is retry, successful empty offers relevant action', (
    tester,
  ) async {
    final model = _Model();
    var reads = 0;
    final api = ConnectionSource(
      authorizationHeader: () => model.token,
      apiBaseUrl: 'https://edge.fixture',
      client: MockClient((r) async {
        if (++reads == 1) throw http.ClientException('synthetic offline');
        return emptyWire(r.url.path);
      }),
    );
    addTearDown(model.dispose);
    addTearDown(api.dispose);
    await tester.pumpWidget(
      harness(model, NowSocialEdgeSource(api), onFindPeople: () {}),
    );
    await tester.pumpAndSettle();
    expect(find.text('连接中断，请重试。'), findsOneWidget);
    expect(find.text('登录'), findsNothing);
    final retry = find.byKey(const Key('edge-retry'));
    await tester.ensureVisible(retry);
    await tester.pumpAndSettle();
    expect(retry.hitTestable(), findsOneWidget);
    await tester.tap(retry);
    await tester.pumpAndSettle();
    expect(find.text('暂无会话'), findsOneWidget);
    await tester.tap(find.byKey(const Key('edge-all-conversations')));
    await tester.pumpAndSettle();
    expect(find.text('还没有消息'), findsOneWidget);
    await tester.tap(find.text('查看联系人'));
    await tester.pumpAndSettle();
    expect(find.text('还没有好友'), findsOneWidget);
    expect(find.text('搜索用户'), findsOneWidget);
    expect(find.text('还没有消息'), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets(
    'theme and text changes preserve selection/read; collapse and reopen refresh same section',
    (tester) async {
      final model = _Model();
      var reads = 0;
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((r) async {
          ++reads;
          return originalWire(r.url.path);
        }),
      );
      final source = NowSocialEdgeSource(api);
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      await tester.pumpWidget(harness(model, source));
      await tester.pumpAndSettle();
      await selectSection(tester, NowSocialSection.contacts);
      expect(reads, 2);
      model.dark = true;
      model.scale = 2;
      model.rebuild();
      await tester.pumpAndSettle();
      expect(reads, 2);
      expect(find.text('联系人'), findsOneWidget);
      model.open = false;
      model.rebuild();
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('now-social-edge')), findsNothing);
      model.open = true;
      model.rebuild();
      await tester.pumpAndSettle();
      expect(reads, 3);
      expect(find.text('联系人'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final condition in [
    (width: 390.0, scale: 1.0, height: 360.0),
    (width: 320.0, scale: 1.0, height: 360.0),
    (width: 320.0, scale: 2.0, height: 176.0),
  ]) {
    testWidgets(
      'recent nicknames remain scrollable and full-list previews have reading room ${condition.width}/${condition.scale}',
      (tester) async {
        tester.view.physicalSize = Size(condition.width, 800);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final semantics = tester.ensureSemantics();
        try {
          final model = _Model()
            ..scale = condition.scale
            ..availableHeight = condition.height;
          final opened = <String>[];
          final paths = <String>[];
          const names = [
            'Now20新空账号',
            '小林',
            'River',
            '很长的联系人名字 English 完整名字',
            '第五位联系人',
          ];
          const previews = [
            '明天下午见',
            '稍后联系',
            'See you soon.',
            '这个预览应该保持一行并省略无法显示的尾部内容。',
            '不应出现在近期首屏',
          ];
          String id(int i) =>
              '${i.toString().padLeft(8, '0')}-4444-4444-8444-444444444444';
          final api = ConnectionSource(
            authorizationHeader: () => model.token,
            apiBaseUrl: 'https://edge.fixture',
            client: MockClient((request) async {
              paths.add(request.url.path);
              return pageWire([
                for (var i = 0; i < names.length; i++)
                  {
                    'id': id(i),
                    'otherAccountId': friendID,
                    'otherName': names[i],
                    'lastMessagePreview': previews[i],
                    'unreadCount': i == 0 ? 7 : 0,
                    'createdAt': '2026-10-10T11:00:00Z',
                  },
              ], unreadCount: 7);
            }),
          );
          addTearDown(model.dispose);
          addTearDown(api.dispose);
          await tester.pumpWidget(
            harness(
              model,
              NowSocialEdgeSource(api),
              onConversation: opened.add,
            ),
          );
          await tester.pumpAndSettle();
          expectSocialViewport(tester, scale: condition.scale);
          final edge = find.byKey(const Key('now-social-edge'));
          final first = find.byKey(ValueKey('edge-conversation-${id(0)}'));
          expect(
            tester.getSize(edge).height,
            lessThanOrEqualTo(condition.height),
          );
          expect(tester.getSize(first).height, greaterThanOrEqualTo(48));
          expect(find.text(names[4]), findsNothing);
          if (condition.scale == 1) {
            for (final text in [names[0], names[1], names[2]]) {
              final paragraph = tester.renderObject<RenderParagraph>(
                find.text(text),
              );
              expect(paragraph.didExceedMaxLines, isFalse, reason: text);
            }
            expect(
              tester
                  .renderObject<RenderParagraph>(find.text(names[3]))
                  .didExceedMaxLines,
              isTrue,
            );
          }
          final name = tester.widget<Text>(find.text(names[0]));
          expect(name.maxLines, 1);
          expect(name.overflow, TextOverflow.ellipsis);
          expect(
            find.text(previews.first),
            findsOneWidget,
            reason: '近期联系人展示原服务的一行摘要',
          );
          expect(
            find.text(previews.last),
            findsNothing,
            reason: '第五码仍只从完整会话列表读取',
          );
          expect(
            tester
                .getSemantics(
                  find.byKey(ValueKey('edge-recent-semantics-${id(0)}')),
                )
                .label,
            '${names[0]}，7 条未读消息',
          );
          expect(
            tester
                .widget<Badge>(
                  find.byKey(ValueKey('edge-recent-unread-${id(0)}')),
                )
                .isLabelVisible,
            isTrue,
          );
          expect(
            tester
                .widget<Badge>(
                  find.byKey(ValueKey('edge-recent-unread-${id(1)}')),
                )
                .isLabelVisible,
            isFalse,
          );
          final scrollable = find.descendant(
            of: find.byKey(const Key('edge-recent-list')),
            matching: find.byType(Scrollable),
          );
          if (condition.scale > 1) {
            expect(
              tester
                  .state<ScrollableState>(scrollable)
                  .position
                  .maxScrollExtent,
              greaterThan(0),
            );
          }
          final last = find.byKey(ValueKey('edge-conversation-${id(3)}'));
          // The fourth row may not be built yet in a short, large-text viewport.
          await tester.scrollUntilVisible(
            last,
            100,
            scrollable: scrollable,
            maxScrolls: 8,
          );
          await tester.pumpAndSettle();
          expect(last.hitTestable(), findsOneWidget);
          expect(tester.getRect(edge).contains(tester.getCenter(last)), isTrue);
          await tester.tap(last);
          expect(opened, [id(3)]);
          expect(paths, ['/v1/me/conversations/page']);
          await tester.scrollUntilVisible(
            find.byKey(const Key('edge-all-conversations')),
            100,
            scrollable: scrollable,
            maxScrolls: 8,
          );
          await tester.pumpAndSettle();
          expect(
            tester
                .getSize(find.byKey(const Key('edge-all-conversations')))
                .height,
            greaterThanOrEqualTo(48),
          );
          await tester.tap(find.byKey(const Key('edge-all-conversations')));
          await tester.pumpAndSettle();
          expect(find.byKey(const Key('edge-recent-avatars')), findsNothing);
          final preview = tester.widget<Text>(find.text(previews[0]));
          expect(preview.maxLines, 1);
          expect(preview.overflow, TextOverflow.ellipsis);
          if (condition.scale == 1) {
            expect(
              tester
                  .renderObject<RenderParagraph>(find.text(previews[0]))
                  .didExceedMaxLines,
              isFalse,
              reason: previews[0],
            );
          }
          expect(paths, ['/v1/me/conversations/page']);
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox());
        } finally {
          semantics.dispose();
        }
      },
    );
  }

  testWidgets(
    'four recent avatars lead to paginated history; notifications replace one viewport',
    (tester) async {
      final model = _Model();
      var reads = 0;
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((r) async {
          ++reads;
          return pageWire(
            [
              if (r.url.queryParameters['cursor'] != null)
                {
                  'id': '00000020-4444-4444-8444-444444444444',
                  'otherAccountId': friendID,
                  'otherName': '消息联系人 20',
                  'unreadCount': 0,
                  'createdAt': '2026-10-10T11:00:00Z',
                },
              if (r.url.queryParameters['cursor'] == null)
                for (var i = 0; i < 20; i++)
                  {
                    'id':
                        '${i.toString().padLeft(8, '0')}-4444-4444-8444-444444444444',
                    'otherAccountId': friendID,
                    'otherName': '消息联系人 $i',
                    'unreadCount': i,
                    'lastMessagePreview': '预览只有一行，原文可以在对话中阅读。',
                    'createdAt': '2026-10-10T11:00:00Z',
                  },
            ],
            nextCursor: r.url.queryParameters['cursor'] == null
                ? 'next-page-2'
                : null,
            unreadCount: 190,
          );
        }),
      );
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      final notifications = ListView(
        children: [for (var i = 0; i < 30; ++i) Text('事项 $i')],
      );
      await tester.pumpWidget(
        harness(
          model,
          NowSocialEdgeSource(api),
          notificationBody: notifications,
        ),
      );
      await tester.pumpAndSettle();
      expectSocialViewport(tester);
      expect(find.byType(CircleAvatar), findsNWidgets(4));
      expect(find.text('消息联系人 4'), findsNothing);
      expect(find.text('预览只有一行，原文可以在对话中阅读。'), findsNWidgets(4));
      expect(find.byKey(const ValueKey('edge-tab-contacts')), findsNothing);
      expect(find.byKey(const Key('edge-message-requests')), findsNothing);
      await tester.tap(find.byKey(const Key('edge-all-conversations')));
      await tester.pumpAndSettle();
      expect(reads, 1, reason: '管理层复用已读取分页，不再为展开请求同一页');
      expect(find.text('预览只有一行，原文可以在对话中阅读。'), findsWidgets);
      expectSocialViewport(tester);
      final list = find.byType(ListView);
      expect(list, findsOneWidget);
      await tester.drag(list, const Offset(0, -1800));
      await tester.pumpAndSettle();
      expect(find.text('消息联系人 19').hitTestable(), findsOneWidget);
      await tester.ensureVisible(
        find.byKey(const Key('edge-more-conversations')),
      );
      await tester.tap(find.byKey(const Key('edge-more-conversations')));
      await tester.pumpAndSettle();
      await tester.drag(list, const Offset(0, -400));
      await tester.pumpAndSettle();
      expect(find.text('消息联系人 20').hitTestable(), findsOneWidget);
      expect(reads, 2);
      await selectSection(tester, NowSocialSection.notifications);
      expect(reads, 2);
      expect(find.text('通知'), findsOneWidget);
      expect(find.byType(ListView), findsOneWidget);
      expect(find.byType(SingleChildScrollView), findsNothing);
      expect(find.text('消息联系人 20'), findsNothing);
      await tester.drag(find.byType(ListView), const Offset(0, -800));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'contact menu direct actions, a stale open menu cannot act for next account',
    (tester) async {
      final model = _Model();
      var searches = 0, scans = 0, codes = 0;
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((r) async => emptyWire(r.url.path)),
      );
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      await tester.pumpWidget(
        harness(
          model,
          NowSocialEdgeSource(api),
          onFindPeople: () => ++searches,
          onScanCode: () => ++scans,
          onShowOwnCode: () => ++codes,
        ),
      );
      await tester.pumpAndSettle();
      await selectSection(tester, NowSocialSection.contacts);
      for (final action in ['搜索用户', '扫一扫', '我的二维码']) {
        await tester.tap(find.byKey(const Key('edge-add-contact')));
        await tester.pumpAndSettle();
        await tester.tap(find.text(action).last);
        await tester.pumpAndSettle();
      }
      expect((searches, scans, codes), (1, 1, 1));
      await tester.tap(find.byKey(const Key('edge-add-contact')));
      await tester.pumpAndSettle();
      model.owner = friendID;
      model.token = 'Bearer beta';
      model.identityChanged();
      await tester.pumpAndSettle();
      await tester.tap(find.text('扫一扫'));
      await tester.pumpAndSettle();
      expect(scans, 1);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'section guard preserves active recording; conversation body selects messages without extra editor',
    (tester) async {
      final model = _Model();
      var recording = true, changes = 0, closed = 0;
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((r) async => originalWire(r.url.path)),
      );
      final source = NowSocialEdgeSource(api);
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      Widget mount({Widget? body}) => harness(
        model,
        source,
        onSectionChanging: (_) => !recording,
        onSectionChanged: (_) => ++changes,
        conversationBody: body,
        onCloseConversation: () => ++closed,
      );
      await tester.pumpWidget(mount());
      await tester.pumpAndSettle();
      await selectSection(tester, NowSocialSection.contacts);
      expect(find.text('全部会话'), findsOneWidget);
      expect(changes, 0);
      recording = false;
      await selectSection(tester, NowSocialSection.contacts);
      expect(changes, 1);
      await tester.pumpWidget(
        mount(body: ListView(children: const [Text('当前朋友的消息')])),
      );
      await tester.pumpAndSettle();
      expect(find.text('会话'), findsOneWidget);
      expect(find.text('当前朋友的消息'), findsOneWidget);
      expect(find.byType(TextField), findsNothing);
      expect(find.byType(ListView), findsOneWidget);
      await tester.tap(find.byKey(const Key('edge-back-messages')));
      expect(closed, 1);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets('message requests replace recent messages in same viewport', (
    tester,
  ) async {
    final model = _Model();
    var requests = 0, closes = 0;
    final api = ConnectionSource(
      authorizationHeader: () => model.token,
      apiBaseUrl: 'https://edge.fixture',
      client: MockClient((r) async => originalWire(r.url.path)),
    );
    final source = NowSocialEdgeSource(api);
    addTearDown(model.dispose);
    addTearDown(api.dispose);
    await tester.pumpWidget(
      harness(model, source, onRequests: () => ++requests),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('edge-message-requests')), findsNothing);
    await tester.tap(find.byKey(const Key('edge-all-conversations')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('edge-message-requests')));
    expect(requests, 1);
    await tester.pumpWidget(
      harness(
        model,
        source,
        onRequests: () => ++requests,
        messageRequestsBody: ListView(children: const [Text('来自陌生人的消息请求')]),
        onCloseRequests: () => ++closes,
      ),
    );
    await tester.pumpAndSettle();
    expect(
      find.byKey(const ValueKey('edge-conversation-$chatID')),
      findsNothing,
    );
    expect(find.text('来自陌生人的消息请求'), findsOneWidget);
    await tester.tap(find.byKey(const Key('edge-back-messages')));
    expect(closes, 1);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets(
    'return from conversation refreshes one page and full-list message previews once',
    (tester) async {
      final model = _Model();
      final paths = <String>[];
      final api = ConnectionSource(
        authorizationHeader: () => model.token,
        apiBaseUrl: 'https://edge.fixture',
        client: MockClient((request) async {
          paths.add(request.url.path);
          return pageWire([
            {
              'id': chatID,
              'otherAccountId': friendID,
              'otherName': '最近联系的人',
              'unreadCount': paths.length == 1 ? 7 : 0,
              'lastMessagePreview': paths.length == 1 ? '原消息预览' : '更新后的消息预览',
              'createdAt': '2026-10-10T11:00:00Z',
            },
          ], unreadCount: paths.length == 1 ? 7 : 0);
        }),
      );
      addTearDown(model.dispose);
      addTearDown(api.dispose);
      final source = NowSocialEdgeSource(api);
      Widget mount({Widget? body}) => harness(
        model,
        source,
        conversationBody: body,
        notificationBody: const Text('单独维护通知'),
      );
      await tester.pumpWidget(mount());
      await tester.pumpAndSettle();
      expect(find.text('原消息预览'), findsOneWidget);
      expect(find.text('最近联系的人'), findsOneWidget);
      await tester.tap(find.byKey(const Key('edge-all-conversations')));
      await tester.pumpAndSettle();
      expect(find.text('原消息预览'), findsOneWidget);
      await tester.pumpWidget(mount(body: const Text('真人历史消息')));
      await tester.pumpAndSettle();
      expect(paths, ['/v1/me/conversations/page']);
      await tester.pumpWidget(mount());
      await tester.pumpAndSettle();
      expect(paths, ['/v1/me/conversations/page', '/v1/me/conversations/page']);
      expect(find.text('更新后的消息预览'), findsOneWidget);
      expect(find.text('7'), findsNothing);
      model.dark = true;
      model.rebuild();
      await tester.pumpAndSettle();
      expect(paths, hasLength(2));
      await selectSection(tester, NowSocialSection.notifications);
      expect(find.text('单独维护通知'), findsOneWidget);
      expect(paths, hasLength(2));
      await tester.pumpWidget(const SizedBox());
    },
  );

  test(
    'follows original wire rejects malformed target and negative conversation count',
    () async {
      final client = MockClient(
        (r) async => r.url.path == '/v1/me/follows'
            ? wire([
                {
                  'id': followID,
                  'target': {'targetType': 'PERSON', 'targetId': 'wrong'},
                  'label': '来源标签',
                  'createdAt': '2026-10-10T11:00:00Z',
                },
              ])
            : wire([
                {
                  'id': chatID,
                  'otherAccountId': friendID,
                  'otherName': '成员',
                  'unreadCount': -1,
                },
              ]),
      );
      final api = ConnectionSource(
        authorizationHeader: () => 'Bearer alpha',
        apiBaseUrl: 'https://edge.fixture',
        client: client,
      );
      addTearDown(api.dispose);
      final source = NowSocialEdgeSource(api);
      await expectLater(source.follows(), throwsFormatException);
      await expectLater(source.conversations(), throwsFormatException);
    },
  );
}
