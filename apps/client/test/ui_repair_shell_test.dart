import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _owner = OrganizationWorkspace(
  id: 'org-owner',
  name: '已授权组织',
  role: 'owner',
  organizationType: 'club',
);
const _member = OrganizationWorkspace(
  id: 'org-member',
  name: '普通成员组织',
  role: 'member',
  organizationType: 'club',
);

Future<void> _sidebar(
  WidgetTester tester,
  OrganizationWorkspaceController organizations, {
  bool signedIn = true,
  List<AgentTask> recent = const [],
  ValueChanged<SidebarDestination>? onDestination,
  VoidCallback? onNew,
  VoidCallback? onWorkspaceSelected,
  VoidCallback? onTools,
  double textScale = 1,
}) async {
  final workspace = AgentWorkspaceController()..recent.addAll(recent);
  final city = PublicCityController();
  addTearDown(workspace.dispose);
  addTearDown(city.dispose);
  await tester.pumpWidget(
    MaterialApp(
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(
          context,
        ).copyWith(textScaler: TextScaler.linear(textScale)),
        child: child!,
      ),
      home: Scaffold(
        body: Sidebar(
          workspace: workspace,
          city: city,
          onCitySelected: (_) {},
          onNew: onNew ?? () {},
          onDestination: onDestination ?? (_) {},
          onRecent: (_) {},
          organizations: organizations,
          onCreateOrganization: () {},
          onViewInvitations: () {},
          onChooseCity: () {},
          signedIn: signedIn,
          onWorkspaceSelected: onWorkspaceSelected,
          onTools: onTools,
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('320窄屏200%字号长历史底栏左右动作可达且执行原回调', (t) async {
    t.view.physicalSize = const Size(320, 480);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final organizations = OrganizationWorkspaceController(
      authorizationHeader: () => 'Bearer current',
    );
    addTearDown(organizations.dispose);
    var created = 0;
    final destinations = <SidebarDestination>[];
    await _sidebar(
      t,
      organizations,
      textScale: 2,
      onNew: () => created++,
      onDestination: destinations.add,
      recent: [
        for (var i = 0; i < 50; i++)
          AgentTask(
            id: 'scaled-$i',
            query: '很长的历史对话标题 $i',
            status: 'COMPLETED',
          ),
      ],
    );
    final newChat = find.byKey(const Key('sidebar-new-conversation'));
    final account = find.byKey(const Key('sidebar-account'));
    expect(newChat.hitTestable(), findsOneWidget);
    expect(account.hitTestable(), findsOneWidget);
    expect(t.getSize(newChat).height, greaterThanOrEqualTo(48));
    expect(t.getSize(account).height, greaterThanOrEqualTo(48));
    expect(t.getSize(newChat).width, greaterThanOrEqualTo(48));
    expect(t.getSize(account).width, greaterThanOrEqualTo(48));
    expect(t.getRect(newChat).right, lessThan(t.getRect(account).left));
    expect(t.getCenter(newChat).dy, closeTo(t.getCenter(account).dy, .1));
    final newCenter = t.getCenter(newChat);
    final accountCenter = t.getCenter(account);
    await t.drag(
      find.byKey(const PageStorageKey('sidebar-navigation-history')),
      const Offset(0, -1200),
    );
    await t.pumpAndSettle();
    expect(t.getCenter(newChat), newCenter);
    expect(t.getCenter(account), accountCenter);
    expect(newChat.hitTestable(), findsOneWidget);
    expect(account.hitTestable(), findsOneWidget);
    await t.tap(newChat);
    expect(created, 1);
    expect(t.takeException(), isNull);
    await t.tap(account);
    await t.pumpAndSettle();
    expect(find.text('个人资料与账户').hitTestable(), findsOneWidget);
    await t.tap(find.text('个人资料与账户'));
    await t.pumpAndSettle();
    expect(destinations, [SidebarDestination.profile]);
    expect(t.takeException(), isNull);
  });
  testWidgets('长历史滚动保持底部新建和账户可点，侧栏无重复城市', (t) async {
    t.view.physicalSize = const Size(360, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final organizations = OrganizationWorkspaceController(
      authorizationHeader: () => 'Bearer current',
    );
    addTearDown(organizations.dispose);
    await _sidebar(
      t,
      organizations,
      recent: [
        for (var i = 0; i < 50; i++)
          AgentTask(id: 'local-$i', query: '历史会话 $i', status: 'COMPLETED'),
      ],
    );
    final newChat = find.byKey(const Key('sidebar-new-conversation'));
    final account = find.byKey(const Key('sidebar-account'));
    final newPosition = t.getCenter(newChat);
    final accountPosition = t.getCenter(account);
    expect(newChat.hitTestable(), findsOneWidget);
    expect(account.hitTestable(), findsOneWidget);
    expect(newPosition.dy, closeTo(accountPosition.dy, .1));
    expect(t.getRect(newChat).right, lessThan(t.getRect(account).left));
    await t.drag(
      find.byKey(const PageStorageKey('sidebar-navigation-history')),
      const Offset(0, -1200),
    );
    await t.pumpAndSettle();
    expect(t.getCenter(newChat), newPosition);
    expect(t.getCenter(account), accountPosition);
    expect(find.text('新建对话'), findsOneWidget);
    expect(find.byKey(const Key('sidebar-city-picker')), findsNothing);
    expect(t.takeException(), isNull);
  });

  testWidgets('游客账户提供现有登录路径，无管理和组织选择', (t) async {
    final destinations = <SidebarDestination>[];
    final organizations = OrganizationWorkspaceController(
      authorizationHeader: () => null,
    )..organizations = [_owner];
    addTearDown(organizations.dispose);
    await _sidebar(
      t,
      organizations,
      signedIn: false,
      onDestination: destinations.add,
    );
    expect(find.text('组织活动管理'), findsNothing);
    expect(find.text('我的内容'), findsNothing);
    await t.tap(find.byKey(const Key('sidebar-account')));
    await t.pumpAndSettle();
    expect(find.text('选择本次工作身份'), findsNothing);
    expect(find.text('创建组织'), findsNothing);
    await t.tap(find.text('登录 / 账户'));
    await t.pumpAndSettle();
    expect(destinations, [SidebarDestination.profile]);
  });

  testWidgets('普通成员不展示管理或组织身份选择', (t) async {
    final organizations = OrganizationWorkspaceController(
      authorizationHeader: () => 'Bearer member',
    )..organizations = [_member];
    addTearDown(organizations.dispose);
    await _sidebar(t, organizations);
    expect(find.text('组织活动管理'), findsNothing);
    expect(find.text('商家工作台'), findsNothing);
    await t.tap(find.byKey(const Key('sidebar-account')));
    await t.pumpAndSettle();
    expect(find.text('选择本次工作身份'), findsNothing);
    expect(find.textContaining('普通成员组织'), findsNothing);
  });

  testWidgets('管理者明确选择授权组织，迟到撤权后旧菜单不切换', (t) async {
    var selected = 0;
    final organizations = OrganizationWorkspaceController(
      authorizationHeader: () => 'Bearer manager',
    )..organizations = [_owner, _member];
    addTearDown(organizations.dispose);
    await _sidebar(t, organizations, onWorkspaceSelected: () => selected++);
    await t.tap(find.byKey(const Key('sidebar-account')));
    await t.pumpAndSettle();
    expect(find.textContaining('普通成员组织'), findsNothing);
    await t.tap(find.text('以 已授权组织 身份使用 · 所有者'));
    await t.pumpAndSettle();
    expect(organizations.active, _owner);
    expect(selected, 1);
    await t.tap(find.byKey(const Key('sidebar-account')));
    await t.pumpAndSettle();
    // The open popup may retain its row, but its callback checks the live list.
    organizations.clear();
    await t.tap(find.text('以 已授权组织 身份使用 · 所有者'));
    await t.pumpAndSettle();
    expect(organizations.active, isNull);
    expect(selected, 1);
  });

  testWidgets('原工具入口移到账户且执行原回调', (t) async {
    var opened = 0;
    final organizations = OrganizationWorkspaceController(
      authorizationHeader: () => null,
    );
    addTearDown(organizations.dispose);
    await _sidebar(t, organizations, signedIn: false, onTools: () => opened++);
    await t.tap(find.byKey(const Key('sidebar-account')));
    await t.pumpAndSettle();
    await t.tap(find.text('更多工具'));
    await t.pumpAndSettle();
    expect(opened, 1);
  });

  for (final width in [320.0, 600.0]) {
    testWidgets('顶栏在宽度 $width 对称且城市按钮居中', (t) async {
      t.view.physicalSize = Size(width, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: TopControls(
              onSidebar: () {},
              onInbox: () {},
              onTools: () {},
              contextLabel: '名字很长的城市 Aberdeen',
              onContext: () {},
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      final cityCenter = t.getCenter(find.byKey(const Key('now-city-picker')));
      expect(cityCenter.dx, closeTo(width / 2, .1));
      expect(
        t.getSize(find.byTooltip('打开侧边栏')).width,
        t.getSize(find.byTooltip('打开收件箱')).width,
      );
      expect(find.byTooltip('打开更多工具'), findsNothing);
      expect(t.takeException(), isNull);
    });
  }

  for (final status in [401, 403]) {
    test('组织读取 $status 撤销旧列表和活动工作身份', () async {
      final c =
          OrganizationWorkspaceController(
              authorizationHeader: () => 'Bearer old',
              apiBaseUrl: 'https://api.test',
              client: MockClient((_) async => http.Response('{}', status)),
            )
            ..organizations = [_owner]
            ..active = _owner;
      addTearDown(c.dispose);
      await c.load();
      expect(c.organizations, isEmpty);
      expect(c.active, isNull);
      expect(c.loading, isFalse);
    });
  }

  test('旧账号组织响应不得复活新账号列表', () async {
    var token = 'Bearer old';
    final pending = Completer<http.Response>();
    final c =
        OrganizationWorkspaceController(
            authorizationHeader: () => token,
            apiBaseUrl: 'https://api.test',
            client: MockClient(
              (r) async => r.headers['Authorization'] == 'Bearer old'
                  ? pending.future
                  : http.Response(jsonEncode({'data': []}), 200),
            ),
          )
          ..organizations = [_owner]
          ..active = _owner;
    addTearDown(c.dispose);
    final oldRead = c.load();
    token = 'Bearer new';
    c.clear();
    await c.load();
    pending.complete(
      http.Response(
        jsonEncode({
          'data': [
            {
              'id': _owner.id,
              'name': _owner.name,
              'role': 'owner',
              'organizationType': 'club',
            },
          ],
        }),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    await oldRead;
    expect(c.organizations, isEmpty);
    expect(c.active, isNull);
    expect(c.loading, isFalse);
  });

  test('组织角色降为成员后退回个人身份', () async {
    final c =
        OrganizationWorkspaceController(
            authorizationHeader: () => 'Bearer current',
            apiBaseUrl: 'https://api.test',
            client: MockClient(
              (_) async => http.Response(
                jsonEncode({
                  'data': [
                    {
                      'id': _owner.id,
                      'name': _owner.name,
                      'role': 'member',
                      'organizationType': 'club',
                    },
                  ],
                }),
                200,
                headers: {'content-type': 'application/json; charset=utf-8'},
              ),
            ),
          )
          ..organizations = [_owner]
          ..active = _owner;
    addTearDown(c.dispose);
    await c.load();
    expect(c.organizations.single.role, 'member');
    expect(c.active, isNull);
  });
}
