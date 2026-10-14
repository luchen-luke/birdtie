import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/now_social_edge_source.dart';
import 'package:birdtie_client/src/workspace/now_social_edge.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'inbox_panel_test.dart'
    show noticePage, pageNotice, notificationView;
import 'now_social_edge_test.dart' show originalWire, ownerID;

void main() {
  testWidgets('读取一条通知重新取得权威剩余计数并保留原列表', (t) async {
      final authClient = MockClient((r) async => http.Response(jsonEncode({'data': r.url.path == '/v1/me'
        ? {'id':'account-1','accountType':'person'} : r.url.path.endsWith('/profile')
        ? {'displayName':'隔离通知本人'} : {'enabled':true}}),200));
      addTearDown(authClient.close);
      final auth = BirdtieAuthController(client:authClient,apiBaseUrl:'https://fixture',
        sessionVault:MemorySessionVault()..session=const StoredSession(token:'fixture-token',method:'dev_phone',origin:'https://fixture'));
      await auth.initialize();
      expect(auth.signedIn,isTrue);
    addTearDown(auth.dispose);
    final first = pageNotice(1), second = pageNotice(2);
    var read = false;
    final limits = <String?>[];
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/read')) {
        read = true;
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': {...first, 'readAt': '2026-10-15T00:00:00Z'},
            }),
          ),
          200,
        );
      }
      limits.add(r.url.queryParameters['limit']);
      return noticePage(
        r.url.queryParameters['limit'] == '1' ? [second] : [first, second],
        unread: read ? 1 : 2,
      );
    });
    addTearDown(client.close);
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://fixture',
      client: client,
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
    await t.tap(find.text('本人合成通知1'));
    await t.pumpAndSettle();
    expect(counts.last, 1);
    expect(limits, ['3', '1']);
    expect(find.text('本人合成通知1'), findsOneWidget);
    expect(find.text('本人合成通知2'), findsOneWidget);
  });
  testWidgets('好友行管理菜单调用现有管理入口', (t) async {
    final identity = ValueNotifier<int>(0);
    addTearDown(identity.dispose);
    final client = MockClient((r) async => originalWire(r.url.path));
    addTearDown(client.close);
    final connection = ConnectionSource(
      authorizationHeader: () => 'Bearer fixture',
      apiBaseUrl: 'https://fixture',
      client: client,
    );
    addTearDown(connection.dispose);
    var opens = 0;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Align(
            alignment: Alignment.topRight,
            child: NowSocialEdge(
              open: true,
              source: NowSocialEdgeSource(connection),
              identityChanges: identity,
              current: () => true,
              accountID: () => ownerID,
              workspaceID: () => null,
              onOpenAuthor: (_) {},
              onOpenConversation: (_) {},
              onOpenManagement: () => opens++,
              onSignIn: () {},
              onUsePersonal: () {},
              initialSection: NowSocialSection.contacts,
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.byTooltip('好友操作'));
    await t.pumpAndSettle();
    await t.tap(find.text('好友管理'));
    await t.pumpAndSettle();
    expect(opens, 1);
  });
}
