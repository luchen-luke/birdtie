import 'dart:ui' show SemanticsFlag;

import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'now_scope_recovery_test.dart';

const _longCity = '这是一座用于窄屏与大字号核验的完整长城市名称';
Finder _key(String key) => find.byKey(Key(key));

Future<void> _mountTop(
  WidgetTester t, {
  required double width,
  double scale = 1,
  Brightness brightness = Brightness.light,
  String city = 'Aberdeen',
  VoidCallback? onSidebar,
  VoidCallback? onInbox,
  VoidCallback? onCity,
  ValueChanged<double>? onHeight,
}) async {
  t.view.physicalSize = Size(width, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  await t.pumpWidget(
    MaterialApp(
      theme: ThemeData(brightness: brightness),
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(context).copyWith(
          padding: const EdgeInsets.only(top: 24, bottom: 20),
          textScaler: TextScaler.linear(scale),
        ),
        child: child!,
      ),
      home: Scaffold(
        body: Align(
          alignment: Alignment.topCenter,
          child: TopControls(
            onSidebar: onSidebar ?? () {},
            onInbox: onInbox ?? () {},
            onTools: () {},
            onContext: onCity,
            contextLabel: city,
            onHeightChanged: onHeight,
          ),
        ),
      ),
    ),
  );
  await t.pumpAndSettle();
}

class _SidebarFixture {
  _SidebarFixture({required this.signedIn, this.restoring = false}) {
    organizations = OrganizationWorkspaceController(
      authorizationHeader: () => token,
      apiBaseUrl: '',
    );
  }
  final bool signedIn, restoring;
  final base = NowFixture();
  String? token;
  late final OrganizationWorkspaceController organizations;
  late final workspace = AgentWorkspaceController(source: base.source);
  final destinations = <SidebarDestination>[];
  int newCalls = 0, switches = 0;

  Future<void> mount(WidgetTester t, {double scale = 1}) async {
    t.view.physicalSize = const Size(320, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context).copyWith(
            padding: const EdgeInsets.only(top: 24, bottom: 20),
            textScaler: TextScaler.linear(scale),
          ),
          child: child!,
        ),
        home: Scaffold(
          body: Sidebar(
            workspace: workspace,
            city: base.city,
            onCitySelected: (_) {},
            onNew: () => newCalls++,
            onDestination: destinations.add,
            onRecent: (_) {},
            organizations: organizations,
            onCreateOrganization: () {},
            onViewInvitations: () {},
            onWorkspaceSelected: () => switches++,
            signedIn: signedIn,
            sessionRestoring: restoring,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  void dispose() {
    workspace.dispose();
    organizations.dispose();
    base.dispose();
  }
}

void main() {
  for (final sample in [
    (390.0, 1.0, Brightness.light, 'Aberdeen'),
    (320.0, 2.0, Brightness.light, _longCity),
    (320.0, 3.2, Brightness.dark, _longCity),
    (840.0, 1.0, Brightness.dark, 'Aberdeen'),
  ]) {
    testWidgets('扁平胶囊${sample.$1}/${sample.$2}/${sample.$3}保持对称与完整点击区', (
      t,
    ) async {
      double? measuredHeight;
      await _mountTop(
        t,
        width: sample.$1,
        scale: sample.$2,
        brightness: sample.$3,
        city: sample.$4,
        onCity: () {},
        onHeight: (h) => measuredHeight = h,
      );
      final left = t.getRect(_key('now-sidebar-button')),
          city = t.getRect(_key('now-city-picker')),
          right = t.getRect(_key('now-inbox-button'));
      final leftPaint = t.getRect(_key('now-sidebar-capsule')),
          cityPaint = t.getRect(_key('now-city-capsule')),
          rightPaint = t.getRect(_key('now-inbox-capsule'));
      expect(left.size, right.size);
      expect(left.width, greaterThanOrEqualTo(64));
      expect(left.height, greaterThanOrEqualTo(48));
      expect(city.height, left.height);
      expect(city.center.dx, closeTo(sample.$1 / 2, .01));
      expect(left.center.dy, city.center.dy);
      expect(right.center.dy, city.center.dy);
      expect(city.left - left.right, closeTo(8, .01));
      expect(right.left - city.right, closeTo(8, .01));
      expect(leftPaint.width, greaterThan(leftPaint.height));
      expect(leftPaint.height, cityPaint.height);
      expect(leftPaint.height, rightPaint.height);
      expect(leftPaint.center, left.center);
      expect(cityPaint.center, city.center);
      if (sample.$2 == 1) {
        expect(leftPaint.size, const Size(64, 32));
        expect(left.size, const Size(64, 48));
      }
      final sideMaterial = t.widget<Material>(
        find.descendant(
          of: _key('now-sidebar-capsule'),
          matching: find.byType(Material),
        ),
      );
      expect(sideMaterial.shape, isA<StadiumBorder>());
      final path = sideMaterial.shape!.getOuterPath(
        Offset.zero & leftPaint.size,
      );
      expect(
        path.contains(Offset(leftPaint.width / 2 - 6, .5)),
        true,
        reason: '上下存在平直段，未退回圆形',
      );
      final centralInk = t.widget<Ink>(_key('now-city-capsule'));
      expect(
        (centralInk.decoration! as ShapeDecoration).shape,
        isA<StadiumBorder>(),
      );
      expect(find.byTooltip('选择城市与范围：${sample.$4}'), findsOneWidget);
      expect(find.text('当前：${sample.$4}'), findsOneWidget);
      final text = t.widget<Text>(find.text('当前：${sample.$4}'));
      expect(text.maxLines, 1);
      expect(text.overflow, TextOverflow.ellipsis);
      expect(measuredHeight, closeTo(24 + 10 + left.height, .01));
      expect(t.takeException(), isNull);
    });
  }

  testWidgets('视觉外的48dp区域仍真实可点，保留键盘焦点和无障碍动作', (t) async {
    var sidebar = 0, inbox = 0, cities = 0;
    final semantics = t.ensureSemantics();
    await _mountTop(
      t,
      width: 390,
      onSidebar: () => sidebar++,
      onInbox: () => inbox++,
      onCity: () => cities++,
    );
    for (final control in [
      'now-sidebar-button',
      'now-city-picker',
      'now-inbox-button',
    ]) {
      final rect = t.getRect(_key(control));
      await t.tapAt(Offset(rect.center.dx, rect.bottom - 1));
      await t.pumpAndSettle();
    }
    expect([sidebar, cities, inbox], [1, 1, 1]);
    final sidebarButton = find.descendant(
      of: _key('now-sidebar-button'),
      matching: find.byType(IconButton),
    );
    final data = t.getSemantics(sidebarButton).getSemanticsData();
    expect(data.hasFlag(SemanticsFlag.isButton), true);
    expect(find.byTooltip('打开侧边栏'), findsOneWidget);
    expect(find.byTooltip('打开收件箱'), findsOneWidget);
    expect(t.widget<IconButton>(sidebarButton).onPressed, isNotNull);
    Focus.of(
      t.element(
        find.descendant(
          of: sidebarButton,
          matching: find.byIcon(Icons.menu_rounded),
        ),
      ),
    ).requestFocus();
    await t.pumpAndSettle();
    await t.sendKeyEvent(LogicalKeyboardKey.enter);
    await t.pumpAndSettle();
    expect(sidebar, 2, reason: '复用IconButton的键盘激活动作');
    semantics.dispose();
  });

  testWidgets('没有city callback时保留原禁用语义，不用胶囊伪造选择动作', (t) async {
    await _mountTop(t, width: 320, city: '未选城市');
    expect(t.widget<InkWell>(_key('now-city-picker')).onTap, isNull);
    expect(_key('now-city-picker'), findsOneWidget);
    expect(t.takeException(), isNull);
  });

  for (final sample in [
    (false, false, '登录 / 账户 · 未登录'),
    (true, false, '账户 · 个人身份'),
    (true, true, '正在恢复账户'),
  ]) {
    testWidgets('账户只显示图标且${sample.$3}保留48dp和权限提示', (t) async {
      final f = _SidebarFixture(signedIn: sample.$1, restoring: sample.$2);
      addTearDown(f.dispose);
      if (sample.$1) f.token = 'Bearer synthetic-owner';
      final semantics = t.ensureSemantics();
      await f.mount(t, scale: 2);
      final account = _key('sidebar-account');
      expect(
        find.descendant(of: account, matching: find.byType(Text)),
        findsNothing,
      );
      expect(
        find.descendant(
          of: account,
          matching: find.byIcon(Icons.account_circle_outlined),
        ),
        findsOneWidget,
      );
      expect(t.getSize(account), const Size(48, 48));
      expect(account.hitTestable(), findsOneWidget);
      expect(find.byTooltip(sample.$3), findsOneWidget);
      expect(
        t
            .getSemantics(account)
            .getSemanticsData()
            .hasFlag(SemanticsFlag.isButton),
        true,
      );
      await t.tap(account);
      await t.pumpAndSettle();
      final popup = t.widget<PopupMenuButton<String>>(account);
      final profile = popup
          .itemBuilder(t.element(account))
          .whereType<PopupMenuItem<String>>()
          .singleWhere((i) => i.value == 'profile');
      expect(profile.enabled, !sample.$2);
      if (!sample.$1 || sample.$2) {
        expect(find.text('创建组织'), findsNothing);
        expect(find.text('我的组织邀请'), findsNothing);
      }
      await t.tap(find.text('设置'));
      await t.pumpAndSettle();
      expect(f.destinations, [SidebarDestination.settings]);
      expect(t.takeException(), isNull);
      semantics.dispose();
    });
  }

  testWidgets('图标账户保留实际组织身份及仅管理者切换，迟到菜单不借旧权限', (t) async {
    final f = _SidebarFixture(signedIn: true)..token = 'Bearer synthetic-owner';
    addTearDown(f.dispose);
    const owner = OrganizationWorkspace(
      id: 'synthetic-owner-org',
      name: '合成所有者组织',
      role: 'owner',
      organizationType: 'student',
    );
    const member = OrganizationWorkspace(
      id: 'synthetic-member-org',
      name: '合成成员组织',
      role: 'member',
      organizationType: 'student',
    );
    f.organizations.organizations = [owner, member];
    f.organizations.active = owner;
    await f.mount(t);
    expect(find.byTooltip('账户 · 合成所有者组织'), findsOneWidget);
    await t.tap(_key('sidebar-account'));
    await t.pumpAndSettle();
    expect(find.text('以 合成所有者组织 身份使用 · 所有者'), findsOneWidget);
    expect(find.textContaining('以 合成成员组织'), findsNothing);
    f.token = null;
    await t.tap(find.text('以个人身份使用'));
    await t.pumpAndSettle();
    expect(f.organizations.active, same(owner));
    expect(f.switches, 0);
  });

  testWidgets('长Recent滚动不带走底部图标账户与新建对话', (t) async {
    final f = _SidebarFixture(signedIn: false);
    addTearDown(f.dispose);
    f.workspace.recent.addAll(
      List.generate(
        50,
        (i) => AgentTask(
          id: 'synthetic-$i',
          query: '第$i条很长的合成历史对话名称，用于核验底部固定入口',
          status: 'COMPLETED',
        ),
      ),
    );
    await f.mount(t, scale: 2);
    final account = _key('sidebar-account'),
        create = _key('sidebar-new-conversation');
    final accountBefore = t.getRect(account), createBefore = t.getRect(create);
    await t.fling(
      find.byKey(const PageStorageKey('sidebar-navigation-history')),
      const Offset(0, -400),
      1000,
    );
    await t.pumpAndSettle();
    expect(t.getRect(account), accountBefore);
    expect(t.getRect(create), createBefore);
    expect(accountBefore.height, greaterThanOrEqualTo(48));
    expect(createBefore.height, greaterThanOrEqualTo(48));
    expect(accountBefore.bottom, lessThanOrEqualTo(640 - 20));
    expect(account.hitTestable(), findsOneWidget);
    expect(create.hitTestable(), findsOneWidget);
    await t.tap(create);
    await t.pumpAndSettle();
    expect(f.newCalls, 1);
    expect(t.takeException(), isNull);
  });

  testWidgets('真实MapWorkspace复用测量与唯一city入口，开关侧栏保留Task草稿与地图', (t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    t.view.padding = const FakeViewPadding(top: 24, bottom: 20);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    addTearDown(t.view.resetPadding);
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final map = find.byType(MapCanvas).evaluate().single;
    final publicMap = find.byType(PublicCityMapView).evaluate().single;
    final header = t.getRect(find.byType(TopControls));
    expect(t.getSize(_key('now-sidebar-capsule')), const Size(64, 32));
    expect(
      t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).ornamentTop,
      closeTo(header.bottom + 8, .01),
    );
    await nowSend(t, '保留合成原任务');
    final w = f.workspace(t), task = w.task, result = w.result;
    await t.enterText(nowField(), '未发送的原草稿');
    await nowTap(t, find.byTooltip('打开侧边栏'));
    expect(
      find.descendant(of: _key('sidebar-account'), matching: find.byType(Text)),
      findsNothing,
    );
    expect(_key('now-city-picker'), findsOneWidget);
    expect(
      find.descendant(
        of: find.byType(Sidebar),
        matching: _key('now-city-picker'),
      ),
      findsNothing,
    );
    await nowBack(t);
    expect(w.task, same(task));
    expect(w.result, same(result));
    expect(t.widget<TextField>(nowField()).controller!.text, '未发送的原草稿');
    expect(find.byType(MapCanvas).evaluate().single, same(map));
    expect(find.byType(PublicCityMapView).evaluate().single, same(publicMap));
    expect(f.source.queries, ['保留合成原任务']);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });
}
