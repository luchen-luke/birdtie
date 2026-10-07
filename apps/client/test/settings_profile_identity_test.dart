import 'dart:async';

import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'public_intent_identity_test.dart';

Widget profileSettings(ProfileScenario s) => MaterialApp(
  home: ValueListenableBuilder<(http.Client, String)>(
    valueListenable: s.source,
    builder: (context, source, _) => Scaffold(
      body: SettingsPage(
        auth: s.auth,
        city: s.city,
        moments: s.moments,
        client: source.$1,
        apiBaseUrl: source.$2,
        workspaceChanges: s.workspace,
        organizationWorkspaceID: () => s.workspace.value,
      ),
    ),
  ),
);

Future<void> openSettingsProfile(WidgetTester t, ProfileScenario s) async {
  await t.pumpWidget(profileSettings(s));
  await t.pumpAndSettle();
  await t.scrollUntilVisible(
    find.text('个人资料与可见范围'),
    180,
    scrollable: find.byType(Scrollable).first,
  );
  await profileTap(t, '个人资料与可见范围');
  await t.pumpAndSettle();
  expect(find.byType(LegacyProfilePage), findsOneWidget);
}

Future<void> reachSettingsName(WidgetTester t) async {
  await t.scrollUntilVisible(
    profileField('显示名称'),
    180,
    scrollable: find.byType(Scrollable).last,
  );
  await t.ensureVisible(profileField('显示名称'));
  await t.pumpAndSettle();
  expect(profileField('显示名称').hitTestable(), findsOneWidget);
}

class ProfileLoginAuth extends SeedTestAuth {
  int loginTaps = 0;
  @override
  bool get available => true;
  @override
  Future<void> signIn() async {
    loginTaps++;
    label = '甲账号';
    changeIdentity(profileTokenA, nextOwner: profileOwnerA);
  }
}

void main() {
  testWidgets('Settings本人原入口注入同一来源，真实保存且可返回', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    await openSettingsProfile(t, s);
    await reachSettingsName(t);
    expect(profileName(t), '甲已保存资料');
    await t.enterText(profileField('显示名称'), profileOldDraft);
    await profileTap(t, '保存个人资料');
    await t.pumpAndSettle();
    expect(s.mutations.single.url.host, 'profile.test');
    expect(s.mutations.single.headers['Authorization'], profileTokenA);
    expect(s.auth.label, profileOldDraft);
    await t.pageBack();
    await t.pumpAndSettle();
    expect(find.byType(SettingsPage), findsOneWidget);
    expect(find.byType(LegacyProfilePage), findsNothing);
    await s.unmount(t);
    expect(s.client.closed, false);
  });

  for (final mode in [
    'account',
    'account-aba',
    'token-aba',
    'org',
    'org-aba',
    'source-aba',
  ]) {
    testWidgets('Settings真实旧个人目的地 $mode 永久退休，零旧稿提交', (t) async {
      final s = ProfileScenario();
      addTearDown(s.dispose);
      await openSettingsProfile(t, s);
      await reachSettingsName(t);
      await t.enterText(profileField('显示名称'), profileOldDraft);
      if (mode.startsWith('source')) {
        s.source.value = (s.client, 'http://profile-b.test');
      } else if (mode.startsWith('org')) {
        s.workspace.value = profileOrg;
      } else if (mode.startsWith('token')) {
        s.owners[profileTokenB] = profileOwnerA;
        s.auth.changeIdentity(profileTokenB, nextOwner: profileOwnerA);
      } else {
        s.toB();
      }
      await t.pumpAndSettle();
      if (mode.endsWith('aba')) {
        if (mode.startsWith('org')) {
          s.workspace.value = null;
        } else if (mode.startsWith('source')) {
          s.source.value = (s.client, 'http://profile.test');
        } else {
          s.toA();
        }
        await t.pumpAndSettle();
      }
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
      expect(profileField('显示名称'), findsNothing);
      expect(find.text('保存个人资料'), findsNothing);
      expect(find.byType(LegacyProfilePage), findsNothing);
      expect(s.mutations, isEmpty);
      await t.pageBack();
      await t.pumpAndSettle();
      expect(find.byType(SettingsPage), findsOneWidget);
      await s.unmount(t);
    });
  }

  testWidgets('真正Now侧栏匿名首次登录可编辑，后续切号不带旧草稿', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    s.auth.dispose();
    final login = ProfileLoginAuth()
      ..token = null
      ..owner = profileOwnerA;
    s.auth = login;
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          auth: s.auth,
          city: s.city,
          moments: s.moments,
          seedClient: s.client,
          seedApiBaseUrl: 'http://profile.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    final menu = find.byTooltip('打开侧边栏');
    expect(menu.hitTestable(), findsOneWidget);
    await t.tap(menu);
    await t.pumpAndSettle();
    await profileTap(t, '个人资料');
    await t.pumpAndSettle();
    expect(find.byType(LegacyProfilePage), findsOneWidget);
    expect(profileField('显示名称'), findsNothing);
    await profileTap(t, '登录');
    await t.pumpAndSettle();
    expect(login.loginTaps, 1);
    await reachSettingsName(t);
    expect(profileName(t), '甲已保存资料');
    await t.enterText(profileField('显示名称'), profileOldDraft);
    await profileTap(t, '保存个人资料');
    await t.pumpAndSettle();
    expect(s.mutations.single.headers['Authorization'], profileTokenA);
    await t.enterText(profileField('显示名称'), '甲另一个未保存草稿');
    s.toB();
    await t.pumpAndSettle();
    expect(profileName(t), '乙已保存资料');
    s.toA();
    await t.pumpAndSettle();
    expect(
      profileName(t),
      profileOldDraft,
      reason: 'current A authoritative saved data remains readable',
    );
    expect(profileName(t), isNot('甲另一个未保存草稿'));
    expect(s.mutations.length, 1);
    await s.unmount(t);
  });

  testWidgets('Settings组织入口说明个人边界，不读取本人资料或写入', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    s.workspace.value = profileOrg;
    await t.pumpWidget(profileSettings(s));
    await t.pumpAndSettle();
    await t.scrollUntilVisible(find.text('个人资料与可见范围'), 180);
    await profileTap(t, '个人资料与可见范围');
    await t.pumpAndSettle();
    expect(find.text('请切换到个人身份后管理个人资料。'), findsOneWidget);
    expect(find.byType(LegacyProfilePage), findsNothing);
    expect(s.calls, isEmpty);
    await s.unmount(t);
  });

  testWidgets('Settings已发A保存迟到，切B后不更改B名字或成功文案', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    await openSettingsProfile(t, s);
    await reachSettingsName(t);
    await t.enterText(profileField('显示名称'), profileOldDraft);
    final pending = Completer<http.Response>();
    s.nextMutation = pending;
    await profileTap(t, '保存个人资料');
    s.toB();
    await t.pumpAndSettle();
    pending.complete(
      profileJson({'accountId': profileOwnerA, 'displayName': profileOldDraft}),
    );
    await t.pumpAndSettle();
    expect(s.auth.label, '乙账号');
    expect(find.text('资料已设为私密，原公开意图已撤回。'), findsNothing);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(s.mutations.single.headers['Authorization'], profileTokenA);
    await s.unmount(t);
  });

  testWidgets('真正Now侧栏个人资料接同源transport和workspace，Org切换清旧稿', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          auth: s.auth,
          city: s.city,
          moments: s.moments,
          seedClient: s.client,
          seedApiBaseUrl: 'http://profile.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    final entry = find.byTooltip('打开侧边栏');
    expect(entry.hitTestable(), findsOneWidget);
    await t.tap(entry);
    await t.pumpAndSettle();
    final actualOrganizations = t
        .widget<Sidebar>(find.byType(Sidebar))
        .organizations;
    await profileTap(t, '个人资料');
    await t.pumpAndSettle();
    expect(find.byType(LegacyProfilePage), findsOneWidget);
    await reachSettingsName(t);
    expect(profileName(t), '甲已保存资料');
    await t.enterText(profileField('显示名称'), profileOldDraft);
    actualOrganizations.select(
      const OrganizationWorkspace(
        id: profileOrg,
        name: '合成组织',
        role: 'owner',
        organizationType: 'student',
      ),
    );
    await t.pumpAndSettle();
    expect(profileField('显示名称'), findsNothing);
    expect(find.text('请切换到个人身份后管理个人资料和人员意图。'), findsOneWidget);
    expect(s.mutations, isEmpty);
    actualOrganizations.select(null);
    await t.pumpAndSettle();
    await reachSettingsName(t);
    expect(profileName(t), '甲已保存资料');
    expect(
      s.calls
          .where((r) => r.url.path.endsWith('/profile'))
          .every((r) => r.url.host == 'profile.test'),
      true,
    );
    await profileTap(t, '保存个人资料');
    await t.pumpAndSettle();
    expect(s.mutations.single.headers['Authorization'], profileTokenA);
    expect(s.mutations.single.body.contains(profileOldDraft), false);
    expect(t.takeException(), isNull);
    await s.unmount(t);
  });
}
