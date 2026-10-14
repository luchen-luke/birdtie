import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'public_moment_api_test.dart' show momentReply, publicMomentData;
import 'public_moment_author_v2_test.dart'
    show publicMomentV2Data, publicAuthorTestID;
import 'entity_share_pending_store_test.dart'
    show shareTarget, shareConversation;

Widget embeddedHost(Widget body, {double scale = 1}) => MaterialApp(
  home: Scaffold(
    appBar: AppBar(title: const Text('原Now面板')),
    body: MediaQuery(
      data: MediaQueryData(
        size: const Size(320, 740),
        textScaler: TextScaler.linear(scale),
      ),
      child: body,
    ),
  ),
);
http.Response profileReply({String name = '当前公开作者'}) => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': {
        'accountId': publicAuthorTestID,
        'displayName': name,
        'visibility': 'public',
        'bio': '本人公开简介',
      },
    }),
  ),
  200,
);

void main() {
  testWidgets('同key作者callback打开与移除明确切换v2和旧v1读取', (t) async {
    final enabled = ValueNotifier(false);
    final modes = <String>[];
    final client = MockClient((r) async {
      modes.add(r.url.query);
      return momentReply(
        r.url.hasQuery ? publicMomentV2Data() : publicMomentData(),
      );
    });
    await t.pumpWidget(
      embeddedHost(
        ValueListenableBuilder<bool>(
          valueListenable: enabled,
          builder: (_, value, _) => PublicMomentPage(
            key: const ValueKey('same-moment'),
            embedded: true,
            momentID: shareTarget,
            authorizationHeader: () => null,
            apiBaseUrl: 'https://api.test',
            client: client,
            onOpenAuthor: value ? (_) {} : null,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('查看作者主页'), findsNothing);
    enabled.value = true;
    await t.pumpAndSettle();
    expect(find.text('公开作者名字'), findsOneWidget);
    enabled.value = false;
    await t.pumpAndSettle();
    expect(find.text('查看作者主页'), findsNothing);
    expect(modes, ['', 'include=public-author', '']);
    await t.pumpWidget(const SizedBox());
    enabled.dispose();
  });
  testWidgets('嵌入动态无第二Scaffold顶栏且可信作者与地点复用回调', (t) async {
    String? author, place;
    final client = MockClient((r) async {
      expect(r.url.queryParameters, {'include': 'public-author'});
      return momentReply(publicMomentV2Data());
    });
    await t.pumpWidget(
      embeddedHost(
        PublicMomentPage(
          embedded: true,
          momentID: shareTarget,
          authorizationHeader: () => null,
          apiBaseUrl: 'https://api.test',
          client: client,
          onOpenAuthor: (id) => author = id,
          onOpenPlace: (id) => place = id,
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.byType(Scaffold), findsOneWidget);
    expect(find.byType(AppBar), findsOneWidget);
    expect(find.text('公开动态'), findsNothing);
    await t.tap(find.text('公开作者名字'));
    expect(author, publicAuthorTestID);
    await t.ensureVisible(find.text('查看地点'));
    await t.tap(find.text('查看地点'));
    expect(place, shareConversation);
    expect(t.takeException(), isNull);
  });
  testWidgets('隐藏作者双null不显示或推断作者入口，动态仍可读取', (t) async {
    var calls = 0;
    await t.pumpWidget(
      embeddedHost(
        PublicMomentPage(
          embedded: true,
          momentID: shareTarget,
          authorizationHeader: () => null,
          apiBaseUrl: 'https://api.test',
          client: MockClient(
            (_) async => momentReply(publicMomentV2Data(hidden: true)),
          ),
          onOpenAuthor: (_) => calls++,
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('本人明确公开的合成动态'), findsOneWidget);
    expect(find.text('查看作者主页'), findsNothing);
    expect(find.byType(ListTile), findsNothing);
    expect(calls, 0);
  });
  testWidgets('错误作者配对不能从作者ID单独打开主页', (t) async {
    var calls = 0;
    final invalid = publicMomentV2Data()..['authorPublicProfile'] = null;
    await t.pumpWidget(
      embeddedHost(
        PublicMomentPage(
          embedded: true,
          momentID: shareTarget,
          authorizationHeader: () => null,
          apiBaseUrl: 'https://api.test',
          client: MockClient((_) async => momentReply(invalid)),
          onOpenAuthor: (_) => calls++,
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('重新核实'), findsOneWidget);
    expect(find.text('查看作者主页'), findsNothing);
    expect(calls, 0);
  });
  for (final mode in ['account', 'workspace']) {
    testWidgets('嵌入作者入口$mode ABA不复活旧响应', (t) async {
      final identity = ValueNotifier<String?>('Bearer A'),
          workspace = ValueNotifier<String?>(null);
      final changes = Listenable.merge([identity, workspace]);
      final old = Completer<http.Response>();
      var reads = 0;
      String? opened;
      final client = MockClient((_) async {
        if (++reads == 1) return old.future;
        final next = publicMomentV2Data();
        next['title'] = '当前动态';
        return momentReply(next);
      });
      await t.pumpWidget(
        embeddedHost(
          PublicMomentPage(
            embedded: true,
            momentID: shareTarget,
            authorizationHeader: () => identity.value,
            workspaceID: () => workspace.value,
            identityChanges: changes,
            apiBaseUrl: 'https://api.test',
            client: client,
            onOpenAuthor: (id) => opened = id,
          ),
        ),
      );
      if (mode == 'account') {
        identity.value = 'Bearer B';
        identity.value = 'Bearer A';
      } else {
        workspace.value = 'UNIT org';
        workspace.value = null;
      }
      await t.pumpAndSettle();
      final stale = publicMomentV2Data();
      stale['title'] = '旧身份动态';
      stale['authorPublicProfile'] = {
        'accountId': publicAuthorTestID,
        'displayName': '旧身份作者',
      };
      old.complete(momentReply(stale));
      await t.pumpAndSettle();
      expect(find.text('旧身份动态'), findsNothing);
      expect(find.text('旧身份作者'), findsNothing);
      expect(find.text('当前动态'), findsOneWidget);
      await t.tap(find.text('公开作者名字'));
      expect(opened, publicAuthorTestID);
      await t.pumpWidget(const SizedBox());
      identity.dispose();
      workspace.dispose();
    });
  }
  testWidgets('点击时身份已变但尚无通知，作者入口先退役旧内容', (t) async {
    var token = 'Bearer A', opens = 0;
    await t.pumpWidget(
      embeddedHost(
        PublicMomentPage(
          embedded: true,
          momentID: shareTarget,
          authorizationHeader: () => token,
          apiBaseUrl: 'https://api.test',
          client: MockClient((_) async => momentReply(publicMomentV2Data())),
          onOpenAuthor: (_) => opens++,
        ),
      ),
    );
    await t.pumpAndSettle();
    token = 'Bearer B';
    await t.tap(find.text('公开作者名字'));
    expect(opens, 0);
    await t.pumpAndSettle();
    await t.tap(find.text('公开作者名字'));
    expect(opens, 1);
  });
  testWidgets('320dp双倍字号长作者与动态仍滚动可点', (t) async {
    t.view.physicalSize = const Size(320, 740);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final data = publicMomentV2Data();
    data['title'] = List.filled(40, '长动态标题').join();
    data['authorPublicProfile'] = {
      'accountId': publicAuthorTestID,
      'displayName': List.filled(30, '公开作者').join(),
    };
    String? opened;
    // Existing title byte limit is deliberately retained for real decoding.
    data['title'] = List.filled(10, '长动态标题').join();
    await t.pumpWidget(
      embeddedHost(
        PublicMomentPage(
          embedded: true,
          momentID: shareTarget,
          authorizationHeader: () => null,
          apiBaseUrl: 'https://api.test',
          client: MockClient((_) async => momentReply(data)),
          onOpenAuthor: (id) => opened = id,
        ),
        scale: 2,
      ),
    );
    await t.pumpAndSettle();
    await t.ensureVisible(find.text('查看作者主页'));
    await t.pumpAndSettle();
    await t.tap(find.text('查看作者主页'));
    expect(opened, publicAuthorTestID);
    expect(t.takeException(), isNull);
  });
  for (final embedded in [false, true]) {
    testWidgets('个人详情embedded=$embedded复用原公开profile读取与页面正文', (t) async {
      var profileReads = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/profile')) {
          profileReads++;
          expect(r.url.path, '/v1/accounts/$publicAuthorTestID/profile');
          return profileReply();
        }
        return http.Response('{}', 404);
      });
      final page = PublicPersonPage(
        embedded: embedded,
        accountID: publicAuthorTestID,
        authorizationHeader: () => null,
        apiBaseUrl: 'https://api.test',
        client: client,
      );
      await t.pumpWidget(
        embedded ? embeddedHost(page) : MaterialApp(home: page),
      );
      await t.pumpAndSettle();
      expect(profileReads, 1);
      expect(find.text('当前公开作者'), findsOneWidget);
      expect(find.text('本人公开简介'), findsOneWidget);
      expect(find.byType(Scaffold), findsOneWidget);
      expect(find.byType(AppBar), findsOneWidget);
      expect(find.text('主办人'), embedded ? findsNothing : findsOneWidget);
      expect(t.takeException(), isNull);
    });
  }
  testWidgets('嵌入作者profile保留身份ABA旧读取退役', (t) async {
    final identity = ValueNotifier<String?>('Bearer A');
    final old = Completer<http.Response>();
    var reads = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/profile')) {
        if (++reads == 1) return old.future;
        return profileReply();
      }
      return http.Response('{}', 404);
    });
    await t.pumpWidget(
      embeddedHost(
        PublicPersonPage(
          embedded: true,
          accountID: publicAuthorTestID,
          authorizationHeader: () => identity.value,
          identityChanges: identity,
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    identity.value = 'Bearer B';
    identity.value = 'Bearer A';
    await t.pumpAndSettle();
    old.complete(profileReply(name: '旧身份作者'));
    await t.pumpAndSettle();
    expect(find.text('旧身份作者'), findsNothing);
    expect(find.text('当前公开作者'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
  });
}
