import 'dart:convert';
import 'package:birdtie_client/src/workspace/message_request_policy_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'message_request_policy_controller_test.dart'
    show policyData, policyWire, policyOwner, policyPeer, policyClock;

class _Fixture {
  final auth = SeedTestAuth()..owner = policyOwner;
  final workspace = ValueNotifier<String?>(null);
  final requests = <http.Request>[];
  late final MockClient client = MockClient((r) async {
    requests.add(r);
    if (r.method == 'GET') {
      return policyWire(
        policyData(version: version, incoming: incoming, expires: expiry),
      );
    }
    if (unknown) return http.Response('{}', 500);
    final b = jsonDecode(r.body) as Map<String, dynamic>;
    version++;
    incoming = b['incomingRequests'] as String;
    expiry = DateTime.parse(b['expiresAt'] as String);
    return policyWire(
      policyData(version: version, incoming: incoming, expires: expiry),
    );
  });
  late final MockClient replacement = MockClient((r) async {
    requests.add(r);
    return policyWire(policyData(version: 4));
  });
  int version = 0;
  String incoming = 'REQUEST';
  DateTime? expiry;
  bool unknown = false, replace = false;
  String base = 'http://policy-page.test';
  String? workspaceID() => workspace.value;
  Widget page({double scale = 1, bool dark = false}) => MaterialApp(
    theme: ThemeData(brightness: dark ? Brightness.dark : Brightness.light),
    builder: (context, child) => MediaQuery(
      data: MediaQuery.of(
        context,
      ).copyWith(textScaler: TextScaler.linear(scale)),
      child: child!,
    ),
    home: MessageRequestPolicyPage(
      auth: auth,
      client: replace ? replacement : client,
      apiBaseUrl: base,
      workspaceChanges: workspace,
      organizationWorkspaceID: workspaceID,
      now: policyClock,
    ),
  );
  void dispose() {
    auth.dispose();
    workspace.dispose();
    client.close();
    replacement.close();
  }

  Future<void> tap(WidgetTester t, String s) async {
    await t.scrollUntilVisible(
      find.text(s),
      180,
      maxScrolls: 35,
      scrollable: find.byType(Scrollable).first,
    );
    await t.pumpAndSettle();
    await t.tap(find.text(s));
    await t.pumpAndSettle();
  }

  Future<void> preview(WidgetTester t) async {
    await tap(t, '1 天');
    await tap(t, '检查并保存设置');
    expect(find.text('确认消息请求设置'), findsOneWidget);
  }

  List<http.Request> get puts =>
      requests.where((r) => r.method == 'PUT').toList();
}

void main() {
  testWidgets('未设置读取不保存，SCREEN真实中文预览可返回编辑并明确确认', (t) async {
    final f = _Fixture();
    addTearDown(f.dispose);
    await t.pumpWidget(f.page());
    await t.pumpAndSettle();
    expect(find.text('尚未设置：沿用原普通请求流程。'), findsOneWidget);
    expect(f.puts, isEmpty);
    await f.tap(t, '先留待人工审阅');
    await f.preview(t);
    expect(find.textContaining('不发送普通申请通知'), findsWidgets);
    expect(find.textContaining('当前设置版本：0'), findsOneWidget);
    await t.tap(find.text('返回修改'));
    await t.pumpAndSettle();
    expect(f.puts, isEmpty);
    await f.tap(t, '检查并保存设置');
    await t.tap(find.text('确认保存'));
    await t.pumpAndSettle();
    expect(f.puts.length, 1);
    expect(jsonDecode(f.puts.single.body)['incomingRequests'], 'SCREEN');
    expect(find.text('消息请求设置已保存。'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('未知保存只重新读取，当前设置相同不伪造上次成功', (t) async {
    final f = _Fixture()..unknown = true;
    addTearDown(f.dispose);
    await t.pumpWidget(f.page());
    await t.pumpAndSettle();
    await f.preview(t);
    await t.tap(find.text('确认保存'));
    await t.pumpAndSettle();
    expect(f.puts.length, 1);
    await f.tap(t, '只重新读取当前设置');
    expect(f.puts.length, 1);
    expect(find.textContaining('不能确认上次保存是否成功'), findsOneWidget);
    expect(find.text('消息请求设置已保存。'), findsNothing);
    await t.pumpWidget(const SizedBox());
  });
  for (final mode in ['owner', 'token', 'org', 'base', 'transport']) {
    testWidgets('预览后 $mode ABA 真实退役，旧确认零PUT', (t) async {
      final f = _Fixture();
      addTearDown(f.dispose);
      await t.pumpWidget(f.page());
      await t.pumpAndSettle();
      await f.preview(t);
      if (mode == 'owner') {
        f.auth.changeIdentity('Bearer peer', nextOwner: policyPeer);
      } else if (mode == 'token') {
        f.auth.changeIdentity('Bearer changed', nextOwner: policyOwner);
      } else if (mode == 'org') {
        f.workspace.value = policyPeer;
      } else if (mode == 'base') {
        f.base = 'http://policy-other.test';
        await t.pumpWidget(f.page());
      } else {
        f.replace = true;
        await t.pumpWidget(f.page());
      }
      await t.pumpAndSettle();
      f.auth.changeIdentity('Bearer owner', nextOwner: policyOwner);
      f.workspace.value = null;
      f.base = 'http://policy-page.test';
      f.replace = false;
      await t.pumpWidget(f.page());
      await t.pumpAndSettle();
      expect(find.text('工作身份或来源已变化，请返回设置重新打开。'), findsOneWidget);
      expect(find.text('确认保存'), findsNothing);
      expect(find.text('检查并保存设置'), findsNothing);
      expect(f.puts, isEmpty);
      await t.pumpWidget(const SizedBox());
    });
  }
  for (final dark in [false, true]) {
    testWidgets('320宽大字号主题$dark控件可滚动，确认内容可读', (t) async {
      t.view.physicalSize = const Size(320, 650);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final f = _Fixture();
      addTearDown(f.dispose);
      final semantics = t.ensureSemantics();
      await t.pumpWidget(f.page(scale: 2, dark: dark));
      await t.pumpAndSettle();
      await f.preview(t);
      expect(t.takeException(), isNull);
      expect(find.text('确认保存').hitTestable(), findsOneWidget);
      final node = t
          .getSemantics(find.widgetWithText(FilledButton, '确认保存'))
          .getSemanticsData();
      expect(node.label, contains('确认保存'));
      expect(node.flagsCollection.isButton, isTrue);
      await t.tap(find.text('返回修改'));
      await t.pumpAndSettle();
      expect(f.puts, isEmpty);
      await t.pumpWidget(const SizedBox());
      semantics.dispose();
    });
  }
}
