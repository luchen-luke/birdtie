import 'dart:async';

import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'public_intent_identity_test.dart';

// The shared identity transport predates the controlled avatar read. Keep its
// original profile/CAS and mutation fixtures, and supply an explicit empty avatar
// version for this real destination instead of a generic list response.
class SettingsProfileScenario extends ProfileScenario {
  @override
  Future<http.Response> reply(http.Request request) async {
    if (request.method == 'GET' && request.url.path == '/v1/me/avatar') {
      calls.add(request);
      expect(owners.containsKey(request.headers['Authorization']), true);
      expect(request.bodyBytes, isEmpty);
      return profileJson({'version': 0, 'avatar': null});
    }
    return super.reply(request);
  }
}

Future<void> settleSettingsProfile(WidgetTester t) async {
  // The real avatar API consumes a streamed body. Allow its real event loop
  // completion before waiting on the loading animation in FakeAsync.
  await t.pump();
  await t.runAsync(
    () => Future<void>.delayed(const Duration(milliseconds: 80)),
  );
  await t.pumpAndSettle();
}

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
  await settleSettingsProfile(t);
  await t.scrollUntilVisible(
    find.text('个人资料'),
    180,
    scrollable: find.byType(Scrollable).first,
  );
  await profileTap(t, '个人资料');
  await settleSettingsProfile(t);
  expect(find.byType(LegacyProfilePage), findsOneWidget);
}

Future<void> reachSettingsName(WidgetTester t) async {
  await t.scrollUntilVisible(
    profileField('显示名称'),
    180,
    scrollable: find.byType(Scrollable).last,
  );
  await t.ensureVisible(profileField('显示名称'));
  await settleSettingsProfile(t);
  expect(profileField('显示名称').hitTestable(), findsOneWidget);
}

Future<void> openNowAccountProfile(WidgetTester t, SeedTestAuth auth) async {
  final account = find.byKey(const Key('sidebar-account'));
  expect(account.hitTestable(), findsOneWidget);
  await t.tap(account);
  await settleSettingsProfile(t);
  await profileTap(t, auth.signedIn ? '个人资料与账户' : '登录 / 账户');
}

class ProfileLoginAuth extends SeedTestAuth {
  int loginTaps = 0;
  @override
  bool get available => true;
  @override
  Future<void> signIn({bool Function()? isCurrent}) async {
    if (isCurrent != null && !isCurrent()) return;
    loginTaps++;
    label = '甲账号';
    changeIdentity(profileTokenA, nextOwner: profileOwnerA);
  }
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  testWidgets('Settings本人原入口注入同一来源，真实保存且可返回', (t) async {
    final s = SettingsProfileScenario();
    addTearDown(s.dispose);
    await openSettingsProfile(t, s);
    await reachSettingsName(t);
    expect(profileName(t), '甲已保存资料');
    await t.enterText(profileField('显示名称'), profileOldDraft);
    await profileTap(t, '保存个人资料');
    await settleSettingsProfile(t);
    expect(s.mutations.single.url.host, 'profile.test');
    expect(s.mutations.single.headers['Authorization'], profileTokenA);
    expect(s.auth.label, profileOldDraft);
    await t.pageBack();
    await settleSettingsProfile(t);
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
      final s = SettingsProfileScenario();
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
      await settleSettingsProfile(t);
      if (mode.endsWith('aba')) {
        if (mode.startsWith('org')) {
          s.workspace.value = null;
        } else if (mode.startsWith('source')) {
          s.source.value = (s.client, 'http://profile.test');
        } else {
          s.toA();
        }
        await settleSettingsProfile(t);
      }
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
      expect(profileField('显示名称'), findsNothing);
      expect(find.text('保存个人资料'), findsNothing);
      expect(find.byType(LegacyProfilePage), findsNothing);
      expect(s.mutations, isEmpty);
      await t.pageBack();
      await settleSettingsProfile(t);
      expect(find.byType(SettingsPage), findsOneWidget);
      await s.unmount(t);
    });
  }

  testWidgets('真正Now侧栏匿名首次登录可编辑，后续切号不带旧草稿', (t) async {
    final s = SettingsProfileScenario();
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
    await settleSettingsProfile(t);
    final menu = find.byTooltip('打开侧边栏');
    expect(menu.hitTestable(), findsOneWidget);
    await t.tap(menu);
    await settleSettingsProfile(t);
    await openNowAccountProfile(t, s.auth);
    await settleSettingsProfile(t);
    expect(find.byType(LegacyProfilePage), findsOneWidget);
    expect(profileField('显示名称'), findsNothing);
    await profileTap(t, '登录');
    await settleSettingsProfile(t);
    expect(login.loginTaps, 1);
    await reachSettingsName(t);
    expect(profileName(t), '甲已保存资料');
    await t.enterText(profileField('显示名称'), profileOldDraft);
    await profileTap(t, '保存个人资料');
    await settleSettingsProfile(t);
    expect(s.mutations.single.headers['Authorization'], profileTokenA);
    await t.enterText(profileField('显示名称'), '甲另一个未保存草稿');
    s.toB();
    await settleSettingsProfile(t);
    expect(profileName(t), '乙已保存资料');
    s.toA();
    await settleSettingsProfile(t);
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
    final s = SettingsProfileScenario();
    addTearDown(s.dispose);
    s.workspace.value = profileOrg;
    await t.pumpWidget(profileSettings(s));
    await settleSettingsProfile(t);
    await t.scrollUntilVisible(find.text('个人资料'), 180);
    expect(
      t.widget<ListTile>(find.widgetWithText(ListTile, '个人资料')).onTap,
      isNull,
    );
    expect(find.text('组织工作身份 · 切换到个人身份管理个人设置'), findsOneWidget);
    expect(find.byType(LegacyProfilePage), findsNothing);
    expect(s.calls, isEmpty);
    await s.unmount(t);
  });

  testWidgets('Settings已发A保存迟到，切B后不更改B名字或成功文案', (t) async {
    final s = SettingsProfileScenario();
    addTearDown(s.dispose);
    await openSettingsProfile(t, s);
    await reachSettingsName(t);
    await t.enterText(profileField('显示名称'), profileOldDraft);
    final pending = Completer<http.Response>();
    s.nextMutation = pending;
    await profileTap(t, '保存个人资料');
    s.toB();
    await settleSettingsProfile(t);
    pending.complete(
      profileJson({'accountId': profileOwnerA, 'displayName': profileOldDraft}),
    );
    await settleSettingsProfile(t);
    expect(s.auth.label, '乙账号');
    expect(find.text('资料已设为私密，原公开意图已撤回。'), findsNothing);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(s.mutations.single.headers['Authorization'], profileTokenA);
    await s.unmount(t);
  });

  testWidgets('真正Now侧栏个人资料接同源transport和workspace，Org切换清旧稿', (t) async {
    final s = SettingsProfileScenario();
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
    await settleSettingsProfile(t);
    final entry = find.byTooltip('打开侧边栏');
    expect(entry.hitTestable(), findsOneWidget);
    await t.tap(entry);
    await settleSettingsProfile(t);
    final actualOrganizations = t
        .widget<Sidebar>(find.byType(Sidebar))
        .organizations;
    await openNowAccountProfile(t, s.auth);
    await settleSettingsProfile(t);
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
    await settleSettingsProfile(t);
    expect(profileField('显示名称'), findsNothing);
    expect(find.text('请切换到个人身份后管理个人资料和人员意图。'), findsOneWidget);
    expect(s.mutations, isEmpty);
    actualOrganizations.select(null);
    await settleSettingsProfile(t);
    await reachSettingsName(t);
    expect(profileName(t), '甲已保存资料');
    expect(
      s.calls
          .where((r) => r.url.path.endsWith('/profile'))
          .every((r) => r.url.host == 'profile.test'),
      true,
    );
    await profileTap(t, '保存个人资料');
    await settleSettingsProfile(t);
    expect(s.mutations.single.headers['Authorization'], profileTokenA);
    expect(s.mutations.single.body.contains(profileOldDraft), false);
    expect(t.takeException(), isNull);
    await s.unmount(t);
  });
}
