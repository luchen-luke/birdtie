import 'dart:convert';
import 'package:birdtie_client/src/workspace/active_social_intent_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'active_social_intent_api_test.dart';
import 'model_egress_page_test.dart' show egressAuth;
import 'intent_activity_conversion_api_test.dart';

Future<void> nowTap(WidgetTester t, String text) async {
  final f = find.text(text);
  if (find.byType(AlertDialog).evaluate().isEmpty) {
    await t.scrollUntilVisible(
      f,
      100,
      scrollable: find.byType(Scrollable).first,
    );
  }
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f);
  await t.pumpAndSettle();
}

class NowBorrowedClient extends MockClient {
  NowBorrowedClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

void main() {
  testWidgets(
    'original FIND_ACTIVITY entry opens real current joined selector no implicit POST',
    (t) async {
      final auth = await egressAuth();
      final paths = <String>[];
      final item = nowItem(status: 'ACTIVE');
      item['intent']['type'] = 'FIND_ACTIVITY';
      final c = MockClient((r) async {
        paths.add('${r.method} ${r.url.path}');
        if (r.url.path.endsWith('/activity-conversion')) {
          return conversionResponse(conversionList());
        }
        return nowResponse(
          r.url.path.endsWith('/options') ? nowOptions() : nowList(item),
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: ActiveSocialIntentPage(
            auth: auth,
            client: c,
            initialIntentID: nowIntent,
          ),
        ),
      );
      await t.pumpAndSettle();
      await nowTap(t, '从已报名活动完成这条意图');
      expect(find.text('关联已报名活动'), findsOneWidget);
      expect(find.text('检查关联此活动'), findsOneWidget);
      expect(paths.every((p) => p.startsWith('GET ')), true);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );

  testWidgets('Chinese exact review cancellation and back have zero approve', (
    t,
  ) async {
    final auth = await egressAuth();
    var approvals = 0;
    final source = nowItem();
    final c = MockClient((r) async {
      if (r.url.path.endsWith('/preview')) {
        return nowResponse(nowPreview(source, jsonDecode(r.body)['operation']));
      }
      if (r.url.path.endsWith('/approve')) approvals++;
      return nowResponse(
        r.url.path.endsWith('/options') ? nowOptions() : nowList(source),
      );
    });
    await t.pumpWidget(
      MaterialApp(
        home: ActiveSocialIntentPage(
          auth: auth,
          client: c,
          initialIntentID: nowIntent,
        ),
      ),
    );
    await t.pumpAndSettle();
    await nowTap(t, '检查取消');
    expect(find.text('检查这次具体意图'), findsOneWidget);
    expect(find.text('受众：仅自己'), findsWidgets);
    expect(find.textContaining('寻找截止：'), findsWidgets);
    await nowTap(t, '取消');
    expect(approvals, 0);
    await nowTap(t, '检查取消');
    await t.binding.handlePopRoute();
    await t.pumpAndSettle();
    expect(approvals, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
  });
  testWidgets(
    'editor modifies same ID returns DRAFT needs separate activation',
    (t) async {
      final auth = await egressAuth();
      var source = nowItem(status: 'ACTIVE'), preview = <String, dynamic>{};
      final writes = <Map<String, dynamic>>[];
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          final b = jsonDecode(r.body) as Map<String, dynamic>;
          writes.add(b);
          preview = nowPreview(source, b['operation'], edit: b['edit']);
          return nowResponse(preview);
        }
        if (r.url.path.endsWith('/approve')) {
          final value = nowReceipt(preview);
          source = value['item'];
          return nowResponse(value);
        }
        return nowResponse(
          r.url.path.endsWith('/options') ? nowOptions() : nowList(source),
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: ActiveSocialIntentPage(
            auth: auth,
            client: c,
            initialIntentID: nowIntent,
          ),
        ),
      );
      await t.pumpAndSettle();
      await nowTap(t, '编辑并检查');
      expect(find.textContaining('原有效意图会停止发现'), findsOneWidget);
      await t.enterText(find.widgetWithText(TextField, '你想做什么'), '新的周末羽毛球');
      await nowTap(t, '检查具体修改');
      expect(writes.single['operation'], 'EDIT');
      expect(find.text('操作后：草稿'), findsOneWidget);
      await nowTap(t, '批准此具体版本');
      expect(source['intent']['id'], nowIntent);
      expect(source['intent']['status'], 'DRAFT');
      expect(writes, hasLength(1));
      await nowTap(t, '检查并开启');
      expect(writes.last['operation'], 'ACTIVATE');
      await nowTap(t, '取消');
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'same-key base and borrowed client rebind retires concrete approval',
    (t) async {
      final auth = await egressAuth();
      final hosts = <String>[];
      var approvals = 0;
      final source = nowItem();
      Future<http.Response> reply(http.Request r) async {
        hosts.add(r.url.host);
        if (r.url.path.endsWith('/preview')) {
          return nowResponse(nowPreview(source, 'CANCEL'));
        }
        if (r.url.path.endsWith('/approve')) approvals++;
        return nowResponse(
          r.url.path.endsWith('/options') ? nowOptions() : nowList(source),
        );
      }

      final a = NowBorrowedClient(reply), b = NowBorrowedClient(reply);
      final workspace = ValueNotifier<String?>(null);
      String? getter() => workspace.value;
      Widget page(http.Client c, String base) => MaterialApp(
        home: ActiveSocialIntentPage(
          key: const ValueKey('same'),
          auth: auth,
          client: c,
          apiBaseUrl: base,
          workspaceChanges: workspace,
          organizationWorkspaceID: getter,
          initialIntentID: nowIntent,
        ),
      );
      await t.pumpWidget(page(a, 'http://a.test'));
      await t.pumpAndSettle();
      await nowTap(t, '检查取消');
      await t.pumpWidget(page(b, 'http://b.test'));
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
      expect(find.byType(AlertDialog), findsNothing);
      expect(hosts.last, 'b.test');
      expect(approvals, 0);
      expect(a.closes, 0);
      await t.pumpWidget(const SizedBox());
      expect(b.closes, 0);
      auth.dispose();
      workspace.dispose();
    },
  );
  testWidgets('workspace ABA during parent build permanently retires preview', (
    t,
  ) async {
    final auth = await egressAuth();
    final workspace = ValueNotifier<String?>(null),
        rebuild = ValueNotifier<int>(0);
    var approvals = 0;
    final source = nowItem();
    final c = MockClient((r) async {
      if (r.url.path.endsWith('/preview')) {
        return nowResponse(nowPreview(source, 'CANCEL'));
      }
      if (r.url.path.endsWith('/approve')) approvals++;
      return nowResponse(
        r.url.path.endsWith('/options') ? nowOptions() : nowList(source),
      );
    });
    String? getter() => workspace.value;
    await t.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<int>(
          valueListenable: rebuild,
          builder: (ctx, v, child) {
            if (v == 1) {
              workspace.value = nowIntent;
              workspace.value = null;
            }
            return ActiveSocialIntentPage(
              auth: auth,
              client: c,
              workspaceChanges: workspace,
              organizationWorkspaceID: getter,
              initialIntentID: nowIntent,
            );
          },
        ),
      ),
    );
    await t.pumpAndSettle();
    await nowTap(t, '检查取消');
    rebuild.value = 1;
    await t.pumpAndSettle();
    expect(t.takeException(), isNull);
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.textContaining('请在当前个人账号打开'), findsOneWidget);
    expect(approvals, 0);
    await t.pumpWidget(const SizedBox());
    workspace.dispose();
    rebuild.dispose();
    auth.dispose();
  });
  testWidgets(
    'reset local only and small screen large text editor no clipping exception',
    (t) async {
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final auth = await egressAuth();
      var posts = 0;
      final c = MockClient((r) async {
        if (r.method == 'POST') posts++;
        return nowResponse(
          r.url.path.endsWith('/options') ? nowOptions() : nowList(),
        );
      });
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: ActiveSocialIntentPage(
            auth: auth,
            client: c,
            initialIntentID: nowIntent,
          ),
        ),
      );
      await t.pumpAndSettle();
      await nowTap(t, '编辑并检查');
      expect(t.takeException(), isNull);
      final title = find.widgetWithText(TextField, '你想做什么');
      await t.ensureVisible(title);
      await t.showKeyboard(title);
      t.view.viewInsets = const FakeViewPadding(bottom: 220);
      addTearDown(t.view.resetViewInsets);
      await t.pumpAndSettle();
      await t.enterText(title, '小屏键盘中的本人草稿');
      expect(t.takeException(), isNull);
      final cancel = find.widgetWithText(TextButton, '取消编辑');
      expect(t.getRect(cancel).bottom, lessThanOrEqualTo(640));
      await nowTap(t, '取消编辑');
      t.view.resetViewInsets();
      await t.pumpAndSettle();
      await nowTap(t, '重置本地选择（不取消意图）');
      expect(posts, 0);
      expect(find.textContaining('服务器意图未取消'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
}
