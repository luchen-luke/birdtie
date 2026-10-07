import 'dart:async';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/online_social_opportunity_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'online_social_opportunity_api_test.dart';

class OnlineDiscoveryAuth extends BirdtieAuthController {
  String? person = onlineOppOwner;
  @override
  String? get authorizationHeader => person == null ? null : 'Bearer fixed';
  @override
  String? get accountID => person;
  void use(String? value) {
    person = value;
    notifyListeners();
  }
}

class OnlineBorrowedClient extends MockClient {
  OnlineBorrowedClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

Future<void> onlineTap(WidgetTester t, String label) async {
  final f = find.text(label);
  await t.scrollUntilVisible(f, 100, scrollable: find.byType(Scrollable).first);
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f);
  await t.pumpAndSettle();
}

void main() {
  testWidgets('无城市GPS独立正常入口，中文先解释→原卡→fresh原Activity回调，取消不写', (t) async {
    final auth = OnlineDiscoveryAuth();
    final requests = <http.Request>[];
    String? opened;
    final client = OnlineBorrowedClient((r) async {
      requests.add(r);
      return onlineOppResponse(
        onlineOppWire(options: r.url.path.endsWith('/options')),
      );
    });
    await t.pumpWidget(
      MaterialApp(
        home: OnlineSocialOpportunityPage(
          auth: auth,
          client: client,
          initialIntentID: onlineOppIntent,
          onOpenActivity: (context, id) async {
            opened = id;
          },
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.textContaining('不需要定位或地图城市'), findsOneWidget);
    expect(find.textContaining('找到 1 条'), findsOneWidget);
    expect(find.text('异地线上羽毛球活动'), findsOneWidget);
    await onlineTap(t, '查看活动');
    expect(opened, onlineOppSource);
    expect(requests, hasLength(3));
    expect(requests.every((r) => r.method == 'GET'), true);
    await t.pumpWidget(const SizedBox());
    expect(client.closes, 0);
    auth.dispose();
    client.close();
  });
  testWidgets('320大字IME中文长源可滚动动作48dp，正文不裁掉', (t) async {
    t.view.physicalSize = const Size(320, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final auth = OnlineDiscoveryAuth();
    final client = MockClient(
      (r) async => onlineOppResponse(
        onlineOppWire(
          options: r.url.path.endsWith('/options'),
          title: '完整合成标题${'线上交流活动' * 20}',
        ),
      ),
    );
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context).copyWith(
            textScaler: const TextScaler.linear(3),
            viewInsets: const EdgeInsets.only(bottom: 260),
          ),
          child: child!,
        ),
        home: OnlineSocialOpportunityPage(
          auth: auth,
          client: client,
          initialIntentID: onlineOppIntent,
        ),
      ),
    );
    await t.pumpAndSettle();
    final f = find.text('查看活动');
    await t.scrollUntilVisible(
      f,
      150,
      scrollable: find.byType(Scrollable).first,
    );
    await t.ensureVisible(f);
    await t.pumpAndSettle();
    expect(f.hitTestable(), findsOneWidget);
    expect(
      t.getSize(find.ancestor(of: f, matching: find.byType(TextButton))).height,
      greaterThanOrEqualTo(48),
    );
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('同key端点重绑与A-B-A永久退役，不请求新身份/旧source和借用client零close', (t) async {
    final auth = OnlineDiscoveryAuth();
    final requests = <http.Request>[];
    final client = OnlineBorrowedClient((r) async {
      requests.add(r);
      return onlineOppResponse(
        onlineOppWire(
          options: r.url.path.endsWith('/options'),
          type: 'SOCIAL_INTENT',
        ),
      );
    });
    Future<void> show(String base) async {
      await t.pumpWidget(
        MaterialApp(
          home: OnlineSocialOpportunityPage(
            key: const ValueKey('same'),
            auth: auth,
            client: client,
            apiBaseUrl: base,
            initialIntentID: onlineOppIntent,
          ),
        ),
      );
      await t.pumpAndSettle();
    }

    await show('https://old.test');
    await onlineTap(t, '查看意图');
    expect(find.byType(AlertDialog), findsOneWidget);
    final before = requests.length;
    await show('https://new.test');
    await show('https://old.test');
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.textContaining('账号或连接已变化'), findsOneWidget);
    expect(requests.length, before);
    expect(client.closes, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('迟到旧GET与相同token账号ABA不能显示私密结果', (t) async {
    final auth = OnlineDiscoveryAuth();
    final late = Completer<http.Response>();
    var calls = 0;
    final client = MockClient((r) {
      calls++;
      return late.future;
    });
    await t.pumpWidget(
      MaterialApp(
        home: OnlineSocialOpportunityPage(auth: auth, client: client),
      ),
    );
    await t.pump();
    auth.use(onlineOppSource);
    auth.use(onlineOppOwner);
    late.complete(onlineOppResponse(onlineOppWire(options: true)));
    await t.pumpAndSettle();
    expect(find.text('线上羽毛球交流'), findsNothing);
    expect(calls, 1);
    expect(find.textContaining('账号或连接已变化'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('匿名中文提示不造登录按钮或网络', (t) async {
    final auth = OnlineDiscoveryAuth()..person = null;
    var calls = 0;
    final client = MockClient((r) async {
      calls++;
      return http.Response('{}', 403);
    });
    await t.pumpWidget(
      MaterialApp(
        home: OnlineSocialOpportunityPage(auth: auth, client: client),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('登录后可查看自己的线上机会。'), findsOneWidget);
    expect(calls, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('当前组织零请求；403明确重读；空来源不造活动', (t) async {
    final auth = OnlineDiscoveryAuth();
    var calls = 0;
    var denied = true;
    final client = MockClient((r) async {
      calls++;
      if (denied) return http.Response('{}', 403);
      final w = onlineOppWire(options: r.url.path.endsWith('/options'));
      w['items'] = [];
      return onlineOppResponse(w);
    });
    final workspace = ValueNotifier<String?>('organization');
    String? org() => workspace.value;
    await t.pumpWidget(
      MaterialApp(
        home: OnlineSocialOpportunityPage(
          auth: auth,
          client: client,
          organizationWorkspaceID: org,
          workspaceChanges: workspace,
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(calls, 0);
    expect(find.textContaining('请切回个人身份'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    workspace.value = null;
    await t.pumpWidget(
      MaterialApp(
        home: OnlineSocialOpportunityPage(
          auth: auth,
          client: client,
          initialIntentID: onlineOppIntent,
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.textContaining('无法读取当前'), findsOneWidget);
    denied = false;
    await onlineTap(t, '重新读取线上意图');
    expect(find.textContaining('没有找到符合当前意图'), findsOneWidget);
    expect(find.text('查看活动'), findsNothing);
    await t.pumpWidget(const SizedBox());
    workspace.dispose();
    auth.dispose();
    client.close();
  });
  testWidgets('好友明确操作才原Tie打开聊天，未知POST只一次且零消息RSVP邀请', (t) async {
    final auth = OnlineDiscoveryAuth();
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.method == 'POST') throw Exception('lost explicit chat receipt');
      return onlineOppResponse(
        onlineOppWire(
          options: r.url.path.endsWith('/options'),
          type: 'SOCIAL_INTENT',
          relation: 'FRIEND',
        ),
      );
    });
    await t.pumpWidget(
      MaterialApp(
        home: OnlineSocialOpportunityPage(
          auth: auth,
          client: client,
          initialIntentID: onlineOppIntent,
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(requests.every((r) => r.method == 'GET'), true);
    await onlineTap(t, '打开好友聊天');
    final posts = requests.where((r) => r.method == 'POST').toList();
    expect(posts, hasLength(1));
    expect(posts.single.url.path, contains(onlineOppTie));
    expect(posts.single.url.path, contains('conversation'));
    expect(
      requests.any(
        (r) =>
            r.url.path.endsWith('/messages') ||
            r.url.path.contains('participations') ||
            r.url.path.contains('invite'),
      ),
      false,
    );
    await t.pump(const Duration(seconds: 2));
    expect(requests.where((r) => r.method == 'POST'), hasLength(1));
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('具体来源有限期限结束后旧意图弹窗不可停留或操作', (t) async {
    final auth = OnlineDiscoveryAuth();
    final client = MockClient((r) async {
      final w = onlineOppWire(
        options: r.url.path.endsWith('/options'),
        type: 'SOCIAL_INTENT',
      );
      w['validUntil'] = DateTime.now()
          .toUtc()
          .add(const Duration(milliseconds: 900))
          .toIso8601String();
      return onlineOppResponse(w);
    });
    await t.pumpWidget(
      MaterialApp(
        home: OnlineSocialOpportunityPage(
          auth: auth,
          client: client,
          initialIntentID: onlineOppIntent,
        ),
      ),
    );
    await t.pumpAndSettle();
    await onlineTap(t, '查看意图');
    expect(find.byType(AlertDialog), findsOneWidget);
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 1000)),
    );
    await t.pump(const Duration(seconds: 2));
    await t.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
}
