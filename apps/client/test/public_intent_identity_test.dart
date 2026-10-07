import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/public_intent_section.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;

const profileOwnerA = '81000000-0000-4000-8000-000000000001';
const profileOwnerB = '81000000-0000-4000-8000-000000000002';
const profileOrg = '81000000-0000-4000-8000-000000000003';
const profileTokenA = 'Bearer SYNTHETIC-A';
const profileTokenB = 'Bearer SYNTHETIC-B';
const profileOldDraft = '甲的未保存私人资料';

http.Response profileJson(Object data, {int status = 200}) => http.Response(
  jsonEncode({'data': data}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

Finder profileField(String label) => find.byWidgetPredicate(
  (w) => w is TextField && w.decoration?.labelText == label,
);
String profileName(WidgetTester t) =>
    t.widget<TextField>(profileField('显示名称')).controller!.text;

Future<void> profileTap(WidgetTester t, String label) async {
  final target = find.text(label);
  await t.ensureVisible(target);
  await t.pumpAndSettle();
  expect(target.hitTestable(), findsOneWidget);
  await t.tap(target);
  await t.pump();
}

class ProfileCity extends PublicCityController {
  static const fixture = PublicCity(
    id: 'profile-synthetic-city',
    name: '合成验收城市',
    region: '仅测试',
    timeZone: 'UTC',
    contentStatus: 'test',
    source: PublicSource(
      label: '合成组件测试',
      maintainer: 'fixture',
      freshness: 'synthetic',
      updatedAt: null,
    ),
    map: null,
  );
  @override
  PublicCity get selectedCity => fixture;
}

class ProfileTrackingClient extends MockClient {
  ProfileTrackingClient(super.handler);
  bool closed = false;
  @override
  void close() {
    closed = true;
    super.close();
  }
}

/// Synthetic HTTP only; all state/actions are the real public profile widget.
class ProfileScenario {
  ProfileScenario() {
    auth = SeedTestAuth()
      ..owner = profileOwnerA
      ..token = profileTokenA
      ..label = '甲账号';
    client = ProfileTrackingClient(reply);
    source = ValueNotifier<(http.Client, String)>((
      client,
      'http://profile.test',
    ));
    moments = PrivateMomentController(
      client: client,
      apiBaseUrl: source.value.$2,
      authorizationHeader: () => auth.authorizationHeader,
    );
  }
  late SeedTestAuth auth;
  final city = ProfileCity();
  late ProfileTrackingClient client;
  late PrivateMomentController moments;
  late ValueNotifier<(http.Client, String)> source;
  final workspace = ValueNotifier<String?>(null);
  final calls = <http.Request>[];
  final owners = {profileTokenA: profileOwnerA, profileTokenB: profileOwnerB};
  final profiles = <String, Map<String, dynamic>>{
    profileOwnerA: {
      'accountId': profileOwnerA,
      'displayName': '甲已保存资料',
      'bio': '甲合成简介',
      'visibility': 'private',
    },
    profileOwnerB: {
      'accountId': profileOwnerB,
      'displayName': '乙已保存资料',
      'bio': '乙合成简介',
      'visibility': 'private',
    },
  };
  final intents = <String, List<Map<String, dynamic>>>{
    profileOwnerA: [],
    profileOwnerB: [],
  };
  Completer<http.Response>? nextMe, nextProfile, nextMutation;
  int profileFailures = 0;

  Future<http.Response> reply(http.Request r) async {
    calls.add(r);
    final owner = owners[r.headers['Authorization']]!;
    if (r.method == 'GET' && r.url.path == '/v1/me') {
      final hold = nextMe;
      nextMe = null;
      return hold == null ? profileJson({'id': owner}) : hold.future;
    }
    if (r.method == 'GET' && r.url.path.endsWith('/profile')) {
      final hold = nextProfile;
      nextProfile = null;
      if (hold != null) return hold.future;
      if (profileFailures > 0) {
        profileFailures--;
        return http.Response('', 503);
      }
      return profileJson(profiles[owner]!);
    }
    if (r.method == 'GET' && r.url.path == '/v1/me/intents') {
      return profileJson(intents[owner]!);
    }
    if (r.method == 'GET') return profileJson([]);
    final hold = nextMutation;
    nextMutation = null;
    if (hold != null) return hold.future;
    if (r.method == 'PUT' && r.url.path == '/v1/me/profile') {
      profiles[owner]!.addAll(jsonDecode(r.body) as Map<String, dynamic>);
      return profileJson(profiles[owner]!);
    }
    if (r.url.path.endsWith('/withdraw')) {
      intents[owner]!.removeWhere(
        (i) => r.url.path.contains(i['id'] as String),
      );
      return http.Response('', 204);
    }
    final body = jsonDecode(r.body) as Map<String, dynamic>;
    final intent = {'id': 'synthetic-intent', 'state': 'active', ...body};
    intents[owner]!.add(intent);
    return profileJson(intent, status: 201);
  }

  List<http.Request> get mutations =>
      calls.where((r) => r.method != 'GET').toList();
  void toB() {
    auth.label = '乙账号';
    auth.changeIdentity(profileTokenB, nextOwner: profileOwnerB);
  }

  void toA() {
    auth.label = '甲账号';
    auth.changeIdentity(profileTokenA, nextOwner: profileOwnerA);
  }

  Widget direct({bool injectClient = true}) => MaterialApp(
    home: ValueListenableBuilder<(http.Client, String)>(
      valueListenable: source,
      builder: (context, config, _) => Scaffold(
        body: SingleChildScrollView(
          child: PublicIntentSection(
            key: const ValueKey('actual-profile'),
            auth: auth,
            city: city,
            client: injectClient ? config.$1 : null,
            apiBaseUrl: config.$2,
            workspaceChanges: workspace,
            organizationWorkspaceID: () => workspace.value,
          ),
        ),
      ),
    ),
  );
  Future<void> mount(WidgetTester t) async {
    await t.pumpWidget(direct());
    await t.pumpAndSettle();
  }

  Future<void> unmount(WidgetTester t) async {
    await t.pumpWidget(const SizedBox.shrink());
    await t.pumpAndSettle();
  }

  void dispose() {
    auth.dispose();
    city.dispose();
    moments.dispose();
    workspace.dispose();
    source.dispose();
    client.close();
  }
}

Future<void> profileIntentDraft(WidgetTester t) async {
  for (final item in [
    ('想和人一起做什么？', '合成周末羽毛球'),
    ('粗略区域，例如 Aberdeen 市中心', '合成市中心'),
  ]) {
    final f = profileField(item.$1);
    await t.ensureVisible(f);
    await t.enterText(f, item.$2);
  }
  await profileTap(t, '我确认公开此意图');
  await t.pumpAndSettle();
}

void main() {
  testWidgets('本人真实保存/重复刷新/同账号昵称通知不清草稿', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    await s.mount(t);
    await t.enterText(profileField('显示名称'), profileOldDraft);
    s.auth.updateProfileDisplayName('同账号仅名称通知');
    await t.pumpAndSettle();
    expect(profileName(t), profileOldDraft);
    await profileTap(t, '保存个人资料');
    await t.pumpAndSettle();
    expect(s.mutations.single.headers['Authorization'], profileTokenA);
    expect(jsonDecode(s.mutations.single.body)['displayName'], profileOldDraft);
    expect(s.auth.label, profileOldDraft);
    expect(profileName(t), profileOldDraft);
    await profileTap(t, '保存个人资料');
    await t.pumpAndSettle();
    expect(s.mutations.length, 2);
    await s.unmount(t);
    expect(
      s.client.closed,
      false,
      reason: 'borrowed transport remains owned by caller',
    );
  });

  for (final mode in [
    'account-ab',
    'account-aba',
    'token-ab',
    'token-aba',
    'org',
    'org-aba',
  ]) {
    testWidgets('实际个人资料 $mode 清旧草稿，当前新本人仍可合法保存', (t) async {
      final s = ProfileScenario();
      addTearDown(s.dispose);
      await s.mount(t);
      await t.enterText(profileField('显示名称'), profileOldDraft);
      await t.enterText(profileField('简介（可选）'), '甲旧私密简介草稿');
      if (mode.startsWith('token')) {
        s.owners[profileTokenB] = profileOwnerA;
        s.auth.changeIdentity(profileTokenB, nextOwner: profileOwnerA);
      } else if (mode.startsWith('org')) {
        s.workspace.value = profileOrg;
      } else {
        s.toB();
      }
      await t.pumpAndSettle();
      if (mode.endsWith('aba')) {
        if (mode.startsWith('org')) {
          s.workspace.value = null;
        } else {
          s.toA();
        }
        await t.pumpAndSettle();
      }
      if (mode == 'org') {
        expect(profileField('显示名称'), findsNothing);
        expect(find.text('保存个人资料'), findsNothing);
        expect(s.mutations, isEmpty);
      } else {
        expect(profileName(t), isNot(profileOldDraft));
        expect(
          t.widget<TextField>(profileField('简介（可选）')).controller!.text,
          isNot('甲旧私密简介草稿'),
        );
        await profileTap(t, '保存个人资料');
        await t.pumpAndSettle();
        final write = s.mutations.single;
        expect(write.headers['Authorization'], s.auth.authorizationHeader);
        expect(jsonDecode(write.body)['displayName'], isNot(profileOldDraft));
        expect(write.headers['X-Birdtie-Organization-Workspace'], isNull);
      }
      await s.unmount(t);
    });
  }

  for (final aba in [false, true]) {
    testWidgets('原 A 迟到 GET ${aba ? 'ABA' : 'A→B'} 不能显示旧私密正文', (t) async {
      final s = ProfileScenario();
      addTearDown(s.dispose);
      final hold = Completer<http.Response>();
      s.nextProfile = hold;
      await t.pumpWidget(s.direct());
      await t.pump();
      await t.pump();
      expect(
        s.calls.any((r) => r.url.path == '/v1/accounts/$profileOwnerA/profile'),
        true,
      );
      s.toB();
      await t.pumpAndSettle();
      if (aba) {
        s.toA();
        await t.pumpAndSettle();
      }
      hold.complete(
        profileJson({
          ...s.profiles[profileOwnerA]!,
          'displayName': profileOldDraft,
        }),
      );
      await t.pumpAndSettle();
      expect(profileName(t), isNot(profileOldDraft));
      expect(s.mutations, isEmpty);
      await s.unmount(t);
    });
  }

  for (final aba in [false, true]) {
    testWidgets('原 A 迟到 PUT ${aba ? 'ABA' : 'A→B'} 不改后继名字/成功/忙碌状态', (t) async {
      final s = ProfileScenario();
      addTearDown(s.dispose);
      await s.mount(t);
      await t.enterText(profileField('显示名称'), profileOldDraft);
      final old = Completer<http.Response>();
      s.nextMutation = old;
      await profileTap(t, '保存个人资料');
      s.toB();
      await t.pumpAndSettle();
      if (aba) {
        s.toA();
        await t.pumpAndSettle();
      }
      final label = s.auth.label;
      final next = Completer<http.Response>();
      s.nextMutation = next;
      await profileTap(t, '保存个人资料');
      old.complete(
        profileJson({
          'accountId': profileOwnerA,
          'displayName': profileOldDraft,
        }),
      );
      await t.pumpAndSettle();
      expect(s.auth.label, label);
      expect(find.text('资料已设为私密，原公开意图已撤回。'), findsNothing);
      expect(
        t
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, '保存个人资料'),
            )
            .onPressed,
        isNull,
      );
      expect(s.mutations.first.headers['Authorization'], profileTokenA);
      expect(
        jsonDecode(s.mutations.first.body)['displayName'],
        profileOldDraft,
      );
      expect(
        jsonDecode(s.mutations.last.body)['displayName'],
        isNot(profileOldDraft),
      );
      next.complete(profileJson(s.profiles[s.auth.accountID]!));
      await t.pumpAndSettle();
      expect(find.text('资料已设为私密，原公开意图已撤回。'), findsOneWidget);
      await s.unmount(t);
    });
  }

  testWidgets('匿名首次登录正常读取，退出后隐藏；新会话可再次读取', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    s.auth.changeIdentity(null, nextOwner: profileOwnerA);
    await s.mount(t);
    expect(profileField('显示名称'), findsNothing);
    expect(s.calls, isEmpty);
    s.toA();
    await t.pumpAndSettle();
    expect(profileName(t), '甲已保存资料');
    await t.enterText(profileField('显示名称'), profileOldDraft);
    s.auth.changeIdentity(null, nextOwner: profileOwnerA);
    await t.pumpAndSettle();
    expect(profileField('显示名称'), findsNothing);
    s.toA();
    await t.pumpAndSettle();
    expect(profileName(t), '甲已保存资料');
    expect(s.mutations, isEmpty);
    await s.unmount(t);
  });

  testWidgets('真实读取失败可重试，不能拿空资料提交', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    s.profileFailures = 1;
    await s.mount(t);
    expect(find.text('读取失败，重试'), findsOneWidget);
    expect(
      t
          .widget<OutlinedButton>(find.widgetWithText(OutlinedButton, '保存个人资料'))
          .onPressed,
      isNull,
    );
    expect(s.mutations, isEmpty);
    await profileTap(t, '读取失败，重试');
    await t.pumpAndSettle();
    expect(profileName(t), '甲已保存资料');
    await profileTap(t, '保存个人资料');
    await t.pumpAndSettle();
    expect(s.mutations.length, 1);
    await s.unmount(t);
  });

  testWidgets('source/client A→B→A 原迟到读取失效且不关闭借用 client', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    final hold = Completer<http.Response>();
    s.nextProfile = hold;
    await t.pumpWidget(s.direct());
    await t.pump();
    await t.pump();
    final clientB = ProfileTrackingClient(s.reply);
    addTearDown(clientB.close);
    s.source.value = (clientB, 'http://profile-b.test');
    await t.pumpAndSettle();
    s.source.value = (s.client, 'http://profile.test');
    await t.pumpAndSettle();
    hold.complete(
      profileJson({
        ...s.profiles[profileOwnerA]!,
        'displayName': profileOldDraft,
      }),
    );
    await t.pumpAndSettle();
    expect(profileName(t), isNot(profileOldDraft));
    expect(s.calls.any((r) => r.url.host == 'profile-b.test'), true);
    expect(s.client.closed, false);
    expect(clientB.closed, false);
    await s.unmount(t);
    expect(s.client.closed, false);
    expect(clientB.closed, false);
  });

  testWidgets('组件自建 client 在销毁时关闭', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    final owned = ProfileTrackingClient(s.reply);
    await http.runWithClient(() async {
      await t.pumpWidget(s.direct(injectClient: false));
      await t.pumpAndSettle();
      expect(profileName(t), '甲已保存资料');
      await s.unmount(t);
      expect(owned.closed, true);
      expect(s.client.closed, false);
    }, () => owned);
  });

  testWidgets('当前本人真实确认公开和撤回保留动作与有效结果', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    s.profiles[profileOwnerA]!['visibility'] = 'public';
    await s.mount(t);
    await profileIntentDraft(t);
    await profileTap(t, '公开意图');
    await t.pumpAndSettle();
    final publish = s.mutations.single;
    expect(publish.url.path, '/v1/cities/profile-synthetic-city/intents');
    expect(publish.headers['Authorization'], profileTokenA);
    expect(jsonDecode(publish.body)['confirmed'], true);
    expect(find.text('意图已公开；在有效期内可出现在用户搜索结果中。'), findsOneWidget);
    await profileTap(t, '撤回');
    await t.pumpAndSettle();
    expect(
      s.mutations.last.url.path,
      '/v1/me/intents/synthetic-intent/withdraw',
    );
    expect(s.mutations.last.headers['Authorization'], profileTokenA);
    expect(find.text('撤回'), findsNothing);
    await s.unmount(t);
  });

  for (final action in ['publish', 'withdraw']) {
    for (final error in [false, true]) {
      testWidgets('原 $action 迟到 ${error ? 'error' : 'success'} 不改变后继状态', (
        t,
      ) async {
        final s = ProfileScenario();
        addTearDown(s.dispose);
        s.profiles[profileOwnerA]!['visibility'] = 'public';
        if (action == 'withdraw') {
          s.intents[profileOwnerA]!.add({
            'id': 'old-A-intent',
            'topic': '甲合成意图',
            'state': 'active',
          });
        }
        await s.mount(t);
        if (action == 'publish') await profileIntentDraft(t);
        final hold = Completer<http.Response>();
        s.nextMutation = hold;
        await profileTap(t, action == 'publish' ? '公开意图' : '撤回');
        s.toB();
        await t.pumpAndSettle();
        hold.complete(
          error
              ? http.Response('', 500)
              : action == 'withdraw'
              ? http.Response('', 204)
              : profileJson({'id': 'old-A-result'}, status: 201),
        );
        await t.pumpAndSettle();
        expect(profileName(t), '乙已保存资料');
        expect(s.auth.label, '乙账号');
        expect(find.text('意图已公开；在有效期内可出现在用户搜索结果中。'), findsNothing);
        expect(find.text('撤回失败，请重试。'), findsNothing);
        expect(find.text('提交失败。请确认资料已保存为公开，或稍后重试。'), findsNothing);
        expect(s.mutations.length, 1);
        expect(s.mutations.single.headers['Authorization'], profileTokenA);
        await s.unmount(t);
      });
    }
  }
}
