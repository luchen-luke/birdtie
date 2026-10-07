import 'dart:convert';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_page.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'connection_request_review_entry_test.dart';
import 'connection_request_review_controller_test.dart' show decisionWire;

void main() {
  for (final visible in [true, false]) {
    testWidgets('申请决定页面恢复：关闭重开${visible ? 'pending' : 'LIMIT100缺项'}只核实不重提', (t) async {
      final store = MemoryConnectionReviewPendingStore();
      var posts = 0, missing = false;
      final client = MockClient((r) async {
        if (r.method == 'POST' && r.url.path.endsWith('/decision')) {
          posts++;
          return reviewJSON({'error': {'code': 'message_policy_unavailable'}}, 503);
        }
        if (r.url.path == '/v1/me/connection-requests') return reviewJSON(missing ? [] : [reviewRequest()]);
        return reviewAuthResponse(r);
      });
      final auth = await reviewAuth(client), source = ConnectionSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: client, apiBaseUrl: 'https://original.fixture',
      );
      Widget page() => MaterialApp(home: ConnectionRequestReviewPage(
        auth: auth, source: source, requestID: reviewID, pendingStore: store,
      ));
      await t.pumpWidget(page());
      await t.pumpAndSettle();
      await t.tap(find.text('审阅接受'));
      await t.pumpAndSettle();
      await t.tap(find.text('确认接受'));
      await t.pumpAndSettle();
      expect(posts, 1);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      missing = !visible;
      await t.pumpWidget(page());
      await t.pumpAndSettle();
      expect(find.textContaining('当前申请状态不是操作回执'), findsOneWidget);
      expect(find.textContaining('本机待核实操作'), findsOneWidget);
      expect(find.textContaining('本次操作回执：'), findsNothing);
      if (visible) expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, '审阅接受')).onPressed, null);
      await t.ensureVisible(find.text('核实当前申请'));
      await t.tap(find.text('核实当前申请'));
      await t.pumpAndSettle();
      expect(posts, 1);
      expect(store.values.length, 1);
      expect(t.takeException(), null);
      await t.pumpWidget(const SizedBox());
      source.dispose(); auth.dispose();
    });
  }
  for (final name in ['账号暂不可用', 'Unavailable account']) {
    testWidgets('原申请账号占位文案：$name', (t) async {
      // Synthetic DTO: Chinese system fallback, or an English user nickname.
      // No name string is translated or treated as authority by the page.
      final row = reviewRequest()..['otherName'] = name..['note'] = '';
      var posts = 0;
      final client = MockClient((r) async {
        if (r.method == 'POST') posts++;
        if (r.url.path == '/v1/me/connection-requests') {
          return reviewJSON([row]);
        }
        return reviewAuthResponse(r);
      });
      final auth = await reviewAuth(client);
      final source = ConnectionSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://original.fixture',
      );
      await t.pumpWidget(MaterialApp(home: ConnectionRequestReviewPage(
        pendingStore: MemoryConnectionReviewPendingStore(),
        auth: auth, source: source, requestID: reviewID,
      )));
      await t.pumpAndSettle();
      expect(find.text(name), findsOneWidget);
      expect(find.text('账号暂不可用'), name == '账号暂不可用' ? findsOneWidget : findsNothing);
      expect(find.text('Unavailable account'), name == 'Unavailable account' ? findsOneWidget : findsNothing);
      expect(find.textContaining('尚未进行 Agent 筛查'), findsOneWidget);
      expect(find.text('审阅接受'), findsOneWidget);
      expect(posts, 0);
      expect(t.takeException(), null);
      await t.pumpWidget(const SizedBox());
      source.dispose(); auth.dispose();
    });
  }
  for (final action in ['accept', 'decline']) {
    testWidgets('人工审阅$action明确具体后果及取消/确认，同ID不自动聊天', (t) async {
      var posts = 0;
      final client = MockClient((r) async {
        if (r.url.path == '/v1/me/connection-requests') {
          return reviewJSON([reviewRequest()]);
        }
        if (r.method == 'POST') {
          posts++;
          expect(r.url.path, '/v1/me/connection-requests/$reviewID/decision');
          expect(jsonDecode(r.body), {'action': action});
          return reviewJSON(decisionWire(reviewRequest(), action));
        }
        return reviewAuthResponse(r);
      });
      final auth = await reviewAuth(client),
          source = ConnectionSource(
            authorizationHeader: () => auth.authorizationHeader,
            client: client,
            apiBaseUrl: 'https://original.fixture',
          );
      await t.pumpWidget(
        MaterialApp(
          home: ConnectionRequestReviewPage(
            pendingStore: MemoryConnectionReviewPendingStore(),
            auth: auth,
            source: source,
            requestID: reviewID,
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(posts, 0);
      final label = action == 'accept' ? '接受' : '拒绝';
      await t.tap(find.text('审阅$label'));
      await t.pumpAndSettle();
      expect(find.text('确认$label这条申请'), findsOneWidget);
      expect(
        find.textContaining('不会自动打开聊天'),
        action == 'accept' ? findsOneWidget : findsNothing,
      );
      await t.tap(find.text('取消'));
      await t.pumpAndSettle();
      expect(posts, 0);
      await t.tap(find.text('审阅$label'));
      await t.pumpAndSettle();
      await t.tap(find.text('确认$label'));
      await t.pumpAndSettle();
      expect(posts, 1);
      expect(find.textContaining('本次操作回执'), findsWidgets);
      await t.pumpWidget(const SizedBox());
      source.dispose();
      auth.dispose();
    });
  }
  for (final dark in [false, true]) {
    testWidgets('窄屏大字号人工审阅可滚动，无溢出 $dark', (t) async {
      t.view.physicalSize = const Size(320, 600);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final row = reviewRequest()..['note'] = '人工审阅说明，需先核对具体后果。' * 50;
      var posts = 0;
      final client = MockClient((r) async {
        if (r.method == 'POST') posts++;
        if (r.url.path == '/v1/me/connection-requests') {
          return reviewJSON([row]);
        }
        return reviewAuthResponse(r);
      });
      final auth = await reviewAuth(client),
          source = ConnectionSource(
            authorizationHeader: () => auth.authorizationHeader,
            client: client,
            apiBaseUrl: 'https://original.fixture',
          );
      final semantics = t.ensureSemantics();
      await t.pumpWidget(
        MaterialApp(
          theme: dark ? ThemeData.dark() : ThemeData.light(),
          builder: (_, child) => MediaQuery(
            data: const MediaQueryData(textScaler: TextScaler.linear(2)),
            child: child!,
          ),
          home: ConnectionRequestReviewPage(
            pendingStore: MemoryConnectionReviewPendingStore(),
            auth: auth,
            source: source,
            requestID: reviewID,
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.scrollUntilVisible(
        find.text('审阅接受'),
        300,
        scrollable: find.byType(Scrollable).first,
        maxScrolls: 50,
      );
      await t.pumpAndSettle();
      await t.tap(find.text('审阅接受'));
      await t.pumpAndSettle();
      expect(find.text('确认接受'), findsOneWidget);
      expect(t.takeException(), null);
      await t.tap(find.text('取消'));
      await t.pumpAndSettle();
      expect(posts, 0);
      await t.pumpWidget(const SizedBox());
      semantics.dispose();
      source.dispose();
      auth.dispose();
    });
  }
  testWidgets('工作身份notify ABA移除批准和正文，不能继续确认', (t) async {
    var posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') posts++;
      if (r.url.path == '/v1/me/connection-requests') {
        return reviewJSON([reviewRequest()]);
      }
      return reviewAuthResponse(r);
    });
    final auth = await reviewAuth(client),
        source = ConnectionSource(
          authorizationHeader: () => auth.authorizationHeader,
          client: client,
          apiBaseUrl: 'https://original.fixture',
        );
    final workspace = ValueNotifier<String?>(null);
    await t.pumpWidget(
      MaterialApp(
        home: ConnectionRequestReviewPage(
          pendingStore: MemoryConnectionReviewPendingStore(),
          auth: auth,
          source: source,
          requestID: reviewID,
          workspaceChanges: workspace,
          organizationWorkspaceID: () => workspace.value,
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('审阅接受'));
    await t.pumpAndSettle();
    workspace.value = reviewPeer;
    workspace.value = null;
    await t.pumpAndSettle();
    expect(find.text('确认接受'), findsNothing);
    expect(find.text('测试申请人'), findsNothing);
    expect(find.textContaining('身份或来源已变化'), findsOneWidget);
    expect(posts, 0);
    await t.pumpWidget(const SizedBox());
    workspace.dispose();
    source.dispose();
    auth.dispose();
  });
  for (final change in ['source', 'transport', 'base', 'request']) {
    testWidgets('same-key $change替换即永久退役旧批准', (t) async {
      var posts = 0;
      http.Response response(http.Request r) {
        if (r.method == 'POST') posts++;
        if (r.url.path == '/v1/me/connection-requests') {
          return reviewJSON([reviewRequest()]);
        }
        return reviewAuthResponse(r);
      }

      final client = MockClient((r) async => response(r));
      final auth = await reviewAuth(client),
          sourceA = ConnectionSource(
            authorizationHeader: () => auth.authorizationHeader,
            client: client,
            apiBaseUrl: 'https://original.fixture',
          );
      final clientB = MockClient((r) async => response(r));
      final sourceB = ConnectionSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: change == 'transport' ? clientB : client,
        apiBaseUrl: change == 'base'
            ? 'https://another.fixture'
            : 'https://original.fixture',
      );
      Widget view(ConnectionSource s, String id) => MaterialApp(
        home: ConnectionRequestReviewPage(
          pendingStore: MemoryConnectionReviewPendingStore(),
          key: const ValueKey('same-review'),
          auth: auth,
          source: s,
          requestID: id,
        ),
      );
      await t.pumpWidget(view(sourceA, reviewID));
      await t.pumpAndSettle();
      await t.tap(find.text('审阅接受'));
      await t.pumpAndSettle();
      await t.pumpWidget(
        view(
          change == 'request' ? sourceA : sourceB,
          change == 'request' ? reviewPeer : reviewID,
        ),
      );
      await t.pumpAndSettle();
      await t.pumpWidget(view(sourceA, reviewID));
      await t.pumpAndSettle();
      expect(find.text('确认接受'), findsNothing);
      expect(find.text('测试申请人'), findsNothing);
      expect(find.textContaining('身份或来源已变化'), findsOneWidget);
      expect(posts, 0);
      await t.pumpWidget(const SizedBox());
      sourceA.dispose();
      sourceB.dispose();
      clientB.close();
      auth.dispose();
    });
  }
  testWidgets('UNKNOWN核实当前accepted仍无原回执、无重试提交', (t) async {
    var posts = 0;
    var row = reviewRequest();
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        row = reviewRequest(state: 'accepted');
        return reviewJSON({}, 503);
      }
      if (r.url.path == '/v1/me/connection-requests') return reviewJSON([row]);
      return reviewAuthResponse(r);
    });
    final auth = await reviewAuth(client),
        source = ConnectionSource(
          authorizationHeader: () => auth.authorizationHeader,
          client: client,
          apiBaseUrl: 'https://original.fixture',
        );
    await t.pumpWidget(
      MaterialApp(
        home: ConnectionRequestReviewPage(
          pendingStore: MemoryConnectionReviewPendingStore(),
          auth: auth,
          source: source,
          requestID: reviewID,
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('审阅接受'));
    await t.pumpAndSettle();
    await t.tap(find.text('确认接受'));
    await t.pumpAndSettle();
    expect(find.textContaining('结果未确认'), findsOneWidget);
    await t.tap(find.text('核实当前申请'));
    await t.pumpAndSettle();
    expect(find.textContaining('当前申请状态不是操作回执'), findsOneWidget);
    expect(find.textContaining('本次操作回执：'), findsNothing);
    expect(posts, 1);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
  });
}
