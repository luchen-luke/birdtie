import 'dart:convert';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const reviewOwner = '11111111-1111-4111-8111-111111111111';
const reviewPeer = '22222222-2222-4222-8222-222222222222';
const reviewID = '33333333-3333-4333-8333-333333333333';

Map<String, dynamic> reviewRequest({String state = 'pending'}) => {
  'id': reviewID,
  'direction': 'incoming',
  'otherAccountId': reviewPeer,
  'otherName': '测试申请人',
  'note': '想一起参加公开活动',
  'scope': 'friend',
  'state': state,
  'conversationId': '',
  'policyDisposition': 'SCREEN',
  'screeningStatus': state == 'pending' ? 'PENDING_REVIEW' : '',
  'createdAt': '2026-10-06T08:00:00Z',
  'expiresAt': '2099-10-06T08:00:00Z',
};

http.Response reviewJSON(dynamic data, [int status = 200]) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      status,
      headers: {'Content-Type': 'application/json; charset=utf-8'},
    );

Future<BirdtieAuthController> reviewAuth(http.Client client) async {
  final auth = BirdtieAuthController(
    client: client,
    apiBaseUrl: 'https://original.fixture',
    sessionVault: MemorySessionVault()
      ..session = StoredSession(
        token: 'synthetic-review-session',
        method: 'dev_phone',
      ),
  );
  await auth.initialize();
  return auth;
}

http.Response reviewAuthResponse(http.Request r) => switch (r.url.path) {
  '/v1/me' => reviewJSON({'id': reviewOwner, 'accountType': 'person'}),
  '/v1/accounts/$reviewOwner/profile' => reviewJSON({'displayName': '测试本人'}),
  '/v1/auth/dev-phone/status' => reviewJSON({'enabled': true}),
  _ => reviewJSON({}, 404),
};

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  testWidgets('原Inbox申请操作打开同ID审阅，点击菜单不写', (t) async {
    var posts = 0, reads = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') posts++;
      if (r.url.path == '/v1/me/connection-requests') {
        reads++;
        return reviewJSON([reviewRequest()]);
      }
      if (r.url.path == '/v1/me/notifications' ||
          r.url.path == '/v1/me/ties' ||
          r.url.path == '/v1/me/conversations') {
        return reviewJSON([]);
      }
      return reviewAuthResponse(r);
    });
    final auth = await reviewAuth(client),
        source = RemoteInboxSource(
          authorizationHeader: () => auth.authorizationHeader,
          client: client,
          apiBaseUrl: 'https://original.fixture',
        );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(auth: auth, source: source),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.byTooltip('申请操作'));
    await t.pumpAndSettle();
    await t.tap(find.text('接受'));
    await t.pumpAndSettle();
    expect(find.text('审阅申请'), findsOneWidget);
    expect(find.text('测试申请人'), findsWidgets);
    expect(find.text('审阅接受'), findsOneWidget);
    expect(reads, 2);
    expect(posts, 0);
    await t.tap(find.text('审阅接受'));
    await t.pumpAndSettle();
    expect(find.text('确认接受这条申请'), findsOneWidget);
    expect(posts, 0);
    await t.tap(find.text('取消'));
    await t.pumpAndSettle();
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
  });
  for (final change in ['workspace', 'base', 'transport']) {
    testWidgets('原Inbox入口$change ABA使子路由批准和正文退役', (t) async {
      var posts = 0;
      http.Response response(http.Request r) {
        if (r.method == 'POST') posts++;
        if (r.url.path == '/v1/me/connection-requests') {
          return reviewJSON([reviewRequest()]);
        }
        if (r.url.path == '/v1/me/notifications' ||
            r.url.path == '/v1/me/ties' ||
            r.url.path == '/v1/me/conversations') {
          return reviewJSON([]);
        }
        return reviewAuthResponse(r);
      }

      final client = MockClient((r) async => response(r));
      final auth = await reviewAuth(client),
          workspace = ValueNotifier<String?>(null);
      final source = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://original.fixture',
      );
      final otherClient = MockClient((r) async => response(r));
      final otherSource = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: change == 'transport' ? otherClient : client,
        apiBaseUrl: change == 'base'
            ? 'https://another.fixture'
            : 'https://original.fixture',
      );
      // Same MaterialApp/Inbox key keeps the existing pushed route.
      Widget view(RemoteInboxSource value) => MaterialApp(
        home: Scaffold(
          body: InboxPanel(
            key: const ValueKey('same-inbox'),
            auth: auth,
            source: value,
            workspaceChanges: workspace,
            organizationWorkspaceID: () => workspace.value,
          ),
        ),
      );
      await t.pumpWidget(view(source));
      await t.pumpAndSettle();
      await t.tap(find.byTooltip('申请操作'));
      await t.pumpAndSettle();
      await t.tap(find.text('接受'));
      await t.pumpAndSettle();
      await t.tap(find.text('审阅接受'));
      await t.pumpAndSettle();
      if (change == 'workspace') {
        workspace.value = reviewPeer;
        workspace.value = null;
      } else {
        await t.pumpWidget(view(otherSource));
        await t.pumpAndSettle();
        await t.pumpWidget(view(source));
      }
      await t.pumpAndSettle();
      expect(find.text('确认接受'), findsNothing);
      expect(posts, 0);
      // Boundary replaces retired child; underlying current Inbox may show new list.
      expect(find.text('审阅接受'), findsNothing);
      expect(find.textContaining('请返回当前入口重新核实'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      workspace.dispose();
      source.dispose();
      otherSource.dispose();
      auth.dispose();
    });
  }
  testWidgets('原Inbox无普通通知仍显示同SCREEN申请待人工审阅', (t) async {
    var listReads = 0, writes = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') writes++;
      if (r.url.path == '/v1/me/connection-requests') {
        listReads++;
        expect(r.headers['Authorization'], 'Bearer synthetic-review-session');
        expect(r.url.host, 'original.fixture');
        return reviewJSON([reviewRequest()]);
      }
      if (r.url.path == '/v1/me/notifications' ||
          r.url.path == '/v1/me/ties' ||
          r.url.path == '/v1/me/conversations') {
        return reviewJSON([]);
      }
      return reviewAuthResponse(r);
    });
    final auth = await reviewAuth(client);
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'https://original.fixture',
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(auth: auth, source: source),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.textContaining('待人工审阅'), findsOneWidget);
    expect(find.textContaining('尚未进行 Agent 筛查'), findsOneWidget);
    expect(find.textContaining('有效期'), findsOneWidget);
    expect(listReads, 1);
    expect(writes, 0);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
  });
}
