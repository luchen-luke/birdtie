import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/app/birdtie_app.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_context_query_api_test.dart' show onlineIntentID;
import 'package:birdtie_client/src/workspace/now_context_query_api.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'now_context_query_api_test.dart' show onlineWire, onlineOwnerID, onlineTaskID;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_debug_panel.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'now_scope_recovery_test.dart';

void main() {
  testWidgets('资料路由绑定原本人正常打开返回保留Now安全草稿和任务', (t) async {
    final x = _PersonalRouteFixture();
    await x.mount(t);
    final w = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
    await nowSend(t, '正常本人公开查询');
    final task = w.task, result = w.result;
    await t.enterText(nowField(), '未发送安全草稿');
    await x.open(t, '个人资料');
    expect(find.text('A本人原记录'), findsOneWidget);
    expect(find.byType(LegacyProfilePage), findsOneWidget);
    await nowBack(t);
    expect(t.widget<TextField>(nowField()).controller!.text, '未发送安全草稿');
    expect(w.task, same(task));
    expect(w.result, same(result));
    expect(t.testTextInput.isVisible, isFalse);
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  for (final route in ['个人资料', '设置']) {
    for (final change in ['auth', 'moments', 'client', 'base']) {
      testWidgets('资料路由绑定实际侧栏$route $change replacement不保留旧本人来源', (t) async {
        final x = _PersonalRouteFixture();
        await x.mount(t);
        await x.open(t, route);
        if (route == '个人资料') {
          expect(find.text('A本人原记录'), findsOneWidget);
        } else {
          expect(find.byType(SettingsPage), findsOneWidget);
        }
        await x.replace(t, change);
        final home = t.widget<MapWorkspace>(
          find.byType(MapWorkspace, skipOffstage: false),
        );
        if (change == 'auth') expect(home.auth, same(x.other.auth));
        if (change == 'moments') {
          expect(home.moments, same(x.replacementMoments));
          expect(home.moments.ownerID!(), home.auth.accountID);
        }
        expect(find.text('A本人原记录'), findsNothing);
        expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
        expect(find.byType(LegacyProfilePage), findsNothing);
        expect(find.byType(SettingsPage), findsNothing);
        expect(t.takeException(), isNull);
        await x.close(t);
      });
    }
  }

  testWidgets('资料路由绑定同一auth首次登录保持当前本人页面可用', (t) async {
    final x = _PersonalRouteFixture();
    x.current.auth.changeIdentity(null, nextOwner: 'owner');
    await x.mount(t);
    await x.open(t, '个人资料');
    expect(find.text('A本人原记录'), findsNothing);
    x.current.auth.changeIdentity('Bearer owner', nextOwner: 'owner');
    await t.pumpAndSettle();
    expect(find.text('A本人原记录'), findsOneWidget);
    expect(find.byType(LegacyProfilePage), findsOneWidget);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsNothing);
    expect(t.takeException(), isNull);
    await x.close(t);
  });

  testWidgets('资料路由绑定同一auth昵称变化不退休也不重复私人GET', (t) async {
    final x = _PersonalRouteFixture();
    await x.mount(t);
    await x.open(t, '个人资料');
    final reads = x.current.reads.length;
    x.current.auth.updateProfileDisplayName('当前本人新昵称');
    await t.pumpAndSettle();
    expect(find.text('当前本人新昵称'), findsOneWidget);
    expect(find.text('A本人原记录'), findsOneWidget);
    expect(x.current.reads.length, reads);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsNothing);
    await x.close(t);
  });

  testWidgets('资料路由绑定仅moments替换保留Now task result选择模式extent草稿', (t) async {
    final x = _PersonalRouteFixture();
    await x.mount(t);
    final w = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
    await nowSend(t, '当前公开任务');
    w.selectEntity('place:fixture-selected-source-retention');
    w.setSheetExtent(AgentSheetExtent.expanded);
    w.showContent(AgentContentMode.conversation);
    await t.pumpAndSettle();
    final task = w.task, result = w.result;
    final state = w.state, mode = w.contentMode, extent = w.sheetExtent;
    final selected = w.selectedEntityId;
    await t.enterText(nowField(), '本人未发送安全草稿');
    await x.open(t, '个人资料');
    await x.replace(t, 'moments');
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(w.task, same(task));
    expect(w.result, same(result));
    expect(w.state, state);
    expect(w.contentMode, mode);
    expect(w.sheetExtent, extent);
    expect(w.selectedEntityId, selected);
    await nowBack(t);
    expect(t.widget<TextField>(nowField()).controller!.text, '本人未发送安全草稿');
    expect(t.testTextInput.isVisible, isFalse);
    expect(w.task, same(task));
    expect(w.result, same(result));
    expect(w.state, state);
    expect(w.contentMode, mode);
    expect(w.sheetExtent, extent);
    expect(w.selectedEntityId, selected);
    await x.open(t, '个人资料');
    expect(find.text('A当前替换来源记录'), findsOneWidget);
    expect(find.text('A本人原记录'), findsNothing);
    expect(t.takeException(), isNull);
    await x.close(t);
  });

  for (final change in ['auth', 'moments']) {
    testWidgets('资料路由绑定实际来源$change ABA不能复活旧route', (t) async {
      final x = _PersonalRouteFixture();
      await x.mount(t);
      await x.open(t, '个人资料');
      await x.replace(t, change);
      await x.replace(t, '');
      expect(find.text('A本人原记录'), findsNothing);
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
      expect(find.byType(LegacyProfilePage), findsNothing);
      await nowBack(t);
      await x.open(t, '个人资料');
      expect(find.text('A本人原记录'), findsOneWidget);
      expect(t.takeException(), isNull);
      await x.close(t);
    });
  }

  testWidgets('资料路由绑定替换auth后返回当前B入口能读本人B记录', (t) async {
    final x = _PersonalRouteFixture();
    await x.mount(t);
    await x.open(t, '个人资料');
    await x.replace(t, 'auth');
    await nowBack(t);
    await x.open(t, '个人资料');
    expect(find.text('B本人原记录'), findsOneWidget);
    expect(find.text('A本人原记录'), findsNothing);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsNothing);
    expect(t.takeException(), isNull);
    await x.close(t);
  });

  for (final outcome in ['success', 'network']) {
    testWidgets('资料路由绑定旧私人编辑器迟到$outcome不越过替换来源边界', (t) async {
      final x = _PersonalRouteFixture();
      await x.mount(t);
      await x.open(t, '个人资料');
      await nowTap(t, find.text('新建草稿'));
      await t.enterText(find.widgetWithText(TextFormField, '标题'), '旧来源本人草稿');
      await t.enterText(find.widgetWithText(TextFormField, '记录'), '旧来源私人正文');
      await t.testTextInput.receiveAction(TextInputAction.done);
      await t.pumpAndSettle();
      x.current.writeGate = Completer<void>();
      x.current.writeOutcome = outcome;
      await nowTap(t, find.text('保存私人草稿'));
      expect(x.current.writes.length, 1);
      await x.replace(t, 'moments');
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
      expect(find.text('保存私人草稿'), findsNothing);
      x.current.writeGate!.complete();
      await t.pumpAndSettle();
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
      expect(find.text('旧来源本人草稿'), findsNothing);
      expect(x.current.writes.length, 1);
      expect(x.replacementMoments.creationUncertain, isFalse);
      await nowBack(t);
      await x.open(t, '个人资料');
      expect(find.text('A当前替换来源记录'), findsOneWidget);
      expect(t.takeException(), isNull);
      await x.close(t);
    });
  }

  for (final values in [
    ('draft', 'private', '仅自己可见 · 草稿'),
    ('published', 'public', '公开可见 · 已发布'),
    ('draft', 'missing', '可见范围未确认 · 草稿'),
    ('unrecognized', 'private', '仅自己可见 · 状态未确认'),
    ('draft', 'public', '公开可见 · 草稿'),
  ]) {
    testWidgets('我的动态状态实际个人资料 ${values.$1}/${values.$2} 只显示真实范围和状态', (t) async {
      final auth = SeedTestAuth(), city = NowFixtureCity();
      final record = <String, dynamic>{
        'id': '11111111-1111-4111-8111-111111111111',
        'authorAccountId': 'owner',
        'cityId': 'alpha',
        'title': '当前本人记录',
        'body': '已核对服务记录',
        'timePrecision': 'unknown',
        'locationPrecision': 'city',
        'status': values.$1,
        'revision': 1,
        if (values.$2 != 'missing') 'visibility': values.$2,
      };
      final client = MockClient(
        (r) async =>
            _boundMomentReply(r.url.path == '/v1/me/moments' ? [record] : []),
      );
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
        ownerID: () => auth.accountID,
        identityChanges: auth,
        client: client,
        apiBaseUrl: 'http://moment-state-fixture.test',
      );
      await moments.refresh();
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: LegacyProfilePage(
              auth: auth,
              city: city,
              moments: moments,
              client: client,
              apiBaseUrl: 'http://moment-state-fixture.test',
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('当前本人记录'), findsOneWidget);
      expect(find.text('我的动态与草稿'), findsOneWidget);
      expect(find.textContaining(values.$3), findsOneWidget);
      if (values.$2 != 'private') {
        expect(find.textContaining('仅自己可见 · 草稿'), findsNothing);
      }
      if (values.$1 != 'draft' || values.$2 == 'public') {
        expect(find.text('编辑或撤回'), findsNothing);
      }
      if (values.$1 == 'draft' && values.$2 == 'missing') {
        await nowTap(t, find.text('编辑或撤回'));
        expect(find.textContaining('可见范围未确认 · 本页不会发布'), findsOneWidget);
        expect(find.textContaining('仅自己可见 · 本页不会发布'), findsNothing);
        await nowBack(t);
      }
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      moments.dispose();
      auth.dispose();
      city.dispose();
    });
  }
  testWidgets('私人身份绑定真实主App接线使用本人账号和身份通知', (t) async {
    await t.pumpWidget(const BirdtieApp());
    await t.pumpAndSettle();
    final page = t.widget<MapWorkspace>(find.byType(MapWorkspace));
    expect(page.moments.identityChanges, same(page.auth));
    expect(page.moments.ownerID, isNotNull);
    expect(page.moments.ownerID!(), page.auth.accountID);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  });
  test('私人身份绑定原本人权威作者读取及昵称变化无多余请求', () async {
    final x = _BoundMomentFixture();
    await x.moments.refresh();
    expect(x.moments.moments.single.authorAccountID, 'owner');
    final before = x.moments.moments.single;
    x.auth.updateProfileDisplayName('本人新昵称');
    await Future<void>.delayed(Duration.zero);
    expect(x.reads, hasLength(1));
    expect(x.moments.moments.single, same(before));
    expect(x.moments.error, isNull);
    x.close();
  });
  testWidgets('私人身份绑定个人资料同token新owner不能展示或改标签旧作者记录', (t) async {
    final x = _BoundMomentFixture();
    await x.moments.refresh();
    await x.mount(t);
    expect(find.text('A本人原记录'), findsOneWidget);
    x.auth.changeIdentity('Bearer owner', nextOwner: 'peer');
    await t.pumpAndSettle();
    expect(x.reads, hasLength(2));
    expect(x.reads.last.headers['Authorization'], 'Bearer owner');
    expect(x.moments.moments, isEmpty);
    expect(find.text('A本人原记录'), findsNothing);
    expect(find.text('编辑或撤回'), findsNothing);
    expect(find.text('无法读取私人草稿，请稍后重试。'), findsOneWidget);
    expect(find.text('还没有私人动态草稿。'), findsNothing);
    expect(find.text('还没有动态或草稿。'), findsNothing);
    expect(x.writes, isEmpty);
    expect(t.takeException(), isNull);
    await x.unmount(t);
  });
  for (final badAuthor in ['peer', 'missing']) {
    test('私人身份绑定GET $badAuthor作者不变成假empty或本人结果', () async {
      final x = _BoundMomentFixture()..authorOverride = badAuthor;
      await x.moments.refresh();
      expect(x.moments.moments, isEmpty);
      expect(x.moments.error, isNotNull);
      expect(x.reads, hasLength(1));
      expect(x.writes, isEmpty);
      x.close();
    });
  }
  test('私人身份绑定同帧账号会话ABA退休旧对象并只接受新当前作者读取', () async {
    final x = _BoundMomentFixture();
    await x.moments.refresh();
    final old = x.moments.moments.single;
    final b = Completer<void>(), a = Completer<void>();
    x.gates.addAll([b, a]);
    x.titles['owner'] = 'A本人重新核实记录';
    x.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
    x.auth.changeIdentity('Bearer owner', nextOwner: 'owner');
    expect(x.moments.moments, isEmpty);
    expect(x.moments.loading, isTrue);
    await Future<void>.delayed(Duration.zero);
    expect(x.reads, hasLength(3));
    a.complete();
    await Future<void>.delayed(Duration.zero);
    expect(x.moments.moments.single.title, 'A本人重新核实记录');
    expect(x.moments.moments.single, isNot(same(old)));
    b.complete();
    await Future<void>.delayed(Duration.zero);
    expect(x.moments.moments.single.authorAccountID, 'owner');
    expect(x.moments.moments.single.title, 'A本人重新核实记录');
    expect(x.moments.error, isNull);
    expect(await x.moments.withdraw(old), isFalse);
    expect(x.writes, isEmpty);
    x.close();
  });
  for (final outcome in ['success', '503', 'network']) {
    test('私人身份绑定迟到旧GET $outcome不覆盖新本人列表', () async {
      final gate = Completer<void>();
      final x = _BoundMomentFixture()..gates.add(gate);
      x.readOutcome = outcome;
      final oldRead = x.moments.refresh();
      await Future<void>.delayed(Duration.zero);
      x.readOutcome = 'success';
      x.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
      await Future<void>.delayed(Duration.zero);
      expect(x.moments.moments.single.title, 'B本人原记录');
      gate.complete();
      await oldRead;
      expect(x.moments.moments.single.authorAccountID, 'peer');
      expect(x.moments.moments.single.title, 'B本人原记录');
      expect(x.moments.error, isNull);
      expect(x.moments.loading, isFalse);
      x.close();
    });
  }
  for (final method in ['POST', 'PUT', 'DELETE']) {
    for (final outcome in ['success', 'network']) {
      test('私人身份绑定迟到旧$method $outcome不污染新列表或未知结果', () async {
        final x = _BoundMomentFixture();
        await x.moments.refresh();
        final old = x.moments.moments.single;
        x.writeGate = Completer<void>();
        x.writeOutcome = outcome;
        final request = switch (method) {
          'POST' => x.moments.create(cityID: 'alpha', title: 'A旧提交', body: ''),
          'PUT' => x.moments.update(
            moment: old,
            title: 'A旧修改',
            body: '',
            placeID: '',
          ),
          _ => x.moments.withdraw(old),
        };
        await Future<void>.delayed(Duration.zero);
        expect(x.writes, hasLength(1));
        expect(x.writes.single.headers['Authorization'], 'Bearer owner');
        x.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
        await Future<void>.delayed(Duration.zero);
        expect(x.moments.moments.single.title, 'B本人原记录');
        x.writeGate!.complete();
        expect(await request, isFalse);
        expect(x.moments.moments.single.authorAccountID, 'peer');
        expect(x.moments.error, isNull);
        expect(x.moments.creationUncertain, isFalse);
        expect(x.moments.saving, isFalse);
        expect(x.writes, hasLength(1));
        x.close();
      });
    }
  }
  for (final method in ['POST', 'PUT']) {
    for (final badAuthor in ['peer', 'missing']) {
      test('私人身份绑定$method $badAuthor回执不能冒称本人保存', () async {
        final x = _BoundMomentFixture();
        await x.moments.refresh();
        final old = x.moments.moments.single;
        x.authorOverride = badAuthor;
        final ok = method == 'POST'
            ? await x.moments.create(cityID: 'alpha', title: '本人主动保存', body: '')
            : await x.moments.update(
                moment: old,
                title: '本人主动修改',
                body: '',
                placeID: '',
              );
        expect(ok, isFalse);
        expect(x.moments.error, isNotNull);
        expect(x.moments.moments.single, same(old));
        expect(x.writes, hasLength(1));
        if (method == 'POST') {
          expect(x.moments.creationUncertain, isTrue);
          expect(
            await x.moments.create(cityID: 'alpha', title: '禁止盲重发', body: ''),
            isFalse,
          );
          expect(x.writes, hasLength(1));
        }
        x.close();
      });
    }
  }
  test('私人身份绑定当前本人仍可人工POST PUT DELETE无machine gate', () async {
    final x = _BoundMomentFixture();
    expect(
      await x.moments.create(cityID: 'alpha', title: '本人主动保存', body: ''),
      isTrue,
    );
    final moment = x.moments.moments.single;
    expect(
      await x.moments.update(
        moment: moment,
        title: '本人主动修改',
        body: '',
        placeID: '',
      ),
      isTrue,
    );
    expect(await x.moments.withdraw(x.moments.moments.single), isTrue);
    expect(x.writes.map((r) => r.method), ['POST', 'PUT', 'DELETE']);
    expect(
      x.writes.every((r) => r.url.path.startsWith('/v1/me/moments')),
      isTrue,
    );
    expect(x.writes.every((r) => !r.body.contains('authorAccountId')), isTrue);
    expect(x.moments.error, isNull);
    x.close();
  });
  test('私人身份绑定未确认account不能读取写入', () async {
    final x = _BoundMomentFixture();
    x.auth.owner = '';
    await x.moments.refresh();
    expect(x.reads, isEmpty);
    expect(x.moments.error, contains('身份尚未确认'));
    expect(
      await x.moments.create(cityID: 'alpha', title: '不得提交', body: ''),
      isFalse,
    );
    expect(x.writes, isEmpty);
    x.close();
  });
  test('私人身份绑定controller关闭后迟到读取不复活或notify', () async {
    final gate = Completer<void>();
    final x = _BoundMomentFixture()..gates.add(gate);
    final pending = x.moments.refresh();
    await Future<void>.delayed(Duration.zero);
    var notifications = 0;
    x.moments.addListener(() => notifications++);
    x.moments.dispose();
    gate.complete();
    await pending;
    x.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
    await Future<void>.delayed(Duration.zero);
    expect(notifications, 0);
    expect(x.reads, hasLength(1));
    x.auth.dispose();
    x.city.dispose();
  });
  for (final outcome in ['success', 'network']) {
    test('私人身份绑定替换旧controller后迟到POST $outcome不污染新controller', () async {
      final old = _BoundMomentFixture()
        ..writeGate = Completer<void>()
        ..writeOutcome = outcome;
      final pending = old.moments.create(
        cityID: 'alpha',
        title: '旧controller提交',
        body: '',
      );
      await Future<void>.delayed(Duration.zero);
      var notifications = 0;
      old.moments.addListener(() => notifications++);
      old.moments.dispose();
      final current = _BoundMomentFixture();
      current.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
      await Future<void>.delayed(Duration.zero);
      expect(current.moments.moments.single.authorAccountID, 'peer');
      old.writeGate!.complete();
      expect(await pending, isFalse);
      expect(notifications, 0);
      expect(current.moments.moments.single.authorAccountID, 'peer');
      expect(current.moments.creationUncertain, isFalse);
      expect(current.moments.error, isNull);
      expect(current.writes, isEmpty);
      old.auth.dispose();
      old.city.dispose();
      current.close();
    });
  }
  testWidgets('私人草稿实际资料入口当前本人保存保留正常 POST', (t) async {
    final x = _PrivateDraftFixture();
    await x.mount(t);
    await x.open(t);
    await x.save(t);
    expect(x.writes, hasLength(1));
    expect(x.writes.single.$1, 'POST');
    expect(x.writes.single.$2, 'Bearer owner');
    expect(x.writes.single.$3['title'], '原本人安全草稿');
    expect(find.text('新建动态'), findsNothing);
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  testWidgets('私人草稿当前本人昵称变化不退休合法草稿', (t) async {
    final x = _PrivateDraftFixture();
    await x.mount(t);
    await x.open(t);
    x.auth.updateProfileDisplayName('本人新昵称');
    await t.pumpAndSettle();
    await x.save(t);
    expect(x.writes, hasLength(1));
    expect(x.writes.single.$3['title'], '原本人安全草稿');
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  for (final change in ['token-B', 'token-ABA', 'owner-same-token']) {
    testWidgets('私人草稿实际资料入口 $change 不得复活旧草稿或提交旧内容', (t) async {
      final x = _PrivateDraftFixture();
      await x.mount(t);
      await x.open(t);
      x.auth.changeIdentity(
        change == 'owner-same-token' ? 'Bearer owner' : 'Bearer peer',
        nextOwner: 'peer',
      );
      // Two real notifications in the same frame must still retire A's editor.
      if (change == 'token-ABA') {
        x.auth.changeIdentity('Bearer owner', nextOwner: 'owner');
      }
      await t.pumpAndSettle();
      if (find.text('保存私人草稿').evaluate().isNotEmpty) await x.save(t);
      expect(x.writes, isEmpty, reason: '旧标题不得在变化后的账号/会话上写入');
      expect(find.text('账号已切换，请重新打开私人草稿。'), findsOneWidget);
      expect(find.widgetWithText(TextFormField, '标题'), findsNothing);
      expect(t.takeException(), isNull);
      await x.close(t);
    });
  }
  testWidgets('私人草稿退休后重新打开本人新草稿仍可正常保存', (t) async {
    final x = _PrivateDraftFixture();
    await x.mount(t);
    await x.open(t);
    x.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
    x.auth.changeIdentity('Bearer owner', nextOwner: 'owner');
    await t.pumpAndSettle();
    await nowTap(t, find.text('关闭'));
    await x.open(t);
    await x.save(t);
    expect(x.writes, hasLength(1));
    expect(x.writes.single.$2, 'Bearer owner');
    expect(find.text('新建动态'), findsNothing);
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  testWidgets('私人草稿原本人编辑和撤回保持真实 PUT DELETE 控制', (t) async {
    final x = _PrivateDraftFixture();
    await x.seedDraft();
    await x.mount(t);
    await nowTap(t, find.text('编辑或撤回'));
    await t.enterText(find.widgetWithText(TextFormField, '标题'), '本人主动修改');
    await t.testTextInput.receiveAction(TextInputAction.done);
    await t.pumpAndSettle();
    await x.save(t);
    expect(x.writes.single.$1, 'PUT');
    expect(x.writes.single.$3['revision'], 1);
    await nowTap(t, find.text('编辑或撤回'));
    await nowTap(t, find.text('撤回草稿'));
    await nowTap(t, find.text('保留'));
    expect(x.writes, hasLength(1));
    await nowTap(t, find.text('撤回草稿'));
    await nowTap(t, find.text('撤回'));
    expect(x.writes.map((w) => w.$1), ['PUT', 'DELETE']);
    expect(find.text('编辑动态'), findsNothing);
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  testWidgets('私人草稿撤回确认期间账号 ABA 不复用旧批准', (t) async {
    final x = _PrivateDraftFixture();
    await x.seedDraft();
    await x.mount(t);
    await nowTap(t, find.text('编辑或撤回'));
    await nowTap(t, find.text('撤回草稿'));
    x.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
    x.auth.changeIdentity('Bearer owner', nextOwner: 'owner');
    await t.pumpAndSettle();
    await nowTap(t, find.text('撤回'));
    expect(x.writes, isEmpty);
    expect(find.text('账号已切换，请重新打开私人草稿。'), findsOneWidget);
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  testWidgets('私人草稿迟到成功不能关闭已退休原编辑器', (t) async {
    final x = _PrivateDraftFixture()..writeGate = Completer<void>();
    await x.mount(t);
    await x.open(t);
    await x.save(t);
    expect(x.writes, hasLength(1));
    x.auth.changeIdentity('Bearer owner', nextOwner: 'peer');
    await t.pumpAndSettle();
    x.writeGate!.complete();
    await t.pumpAndSettle();
    expect(find.text('账号已切换，请重新打开私人草稿。'), findsOneWidget);
    expect(find.text('新建动态'), findsNothing);
    expect(x.writes, hasLength(1));
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  testWidgets('私人草稿未知结果检查迟到期间 ABA 不打开旧批准表单', (t) async {
    final x = _PrivateDraftFixture()..failWrite = true;
    await x.moments.create(cityID: 'alpha', title: '未知上次保存', body: '本人记录');
    expect(x.moments.creationUncertain, isTrue);
    x.failWrite = false;
    x.refreshGate = Completer<void>();
    await x.mount(t);
    await nowTap(t, find.text('新建草稿'));
    x.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
    x.auth.changeIdentity('Bearer owner', nextOwner: 'owner');
    await t.pumpAndSettle();
    x.refreshGate!.complete();
    await t.pumpAndSettle();
    expect(find.text('先检查上次保存结果'), findsNothing);
    expect(find.text('新建动态'), findsNothing);
    expect(x.writes, hasLength(1));
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  for (final repeatSame in [false, true]) {
    for (final outcome in ['success', '503', 'network']) {
      testWidgets(
        'ONLINE detail actual ${repeatSame ? 'same tile repeated' : 'A then B'} late $outcome cannot replace newer detail',
        (t) async {
          final x = _OnlineHistoryFixture('success');
          final gates = [Completer<void>(), Completer<void>()];
          final ids = <String>[];
          x.onlineTaskReply = () =>
              _onlineHistoryReply(_pairReadWire(onlineIntentID, both: true));
          x.onlineDetailRead = (id) async {
            final index = ids.length;
            ids.add(id);
            await gates[index].future;
            if (index == 0 && outcome == 'network') {
              throw http.ClientException('synthetic old network');
            }
            if (index == 0 && outcome == '503') return http.Response('{}', 503);
            return _onlineHistoryReply(_pairReadWire(id));
          };
          await x.mount(t);
          await x.reopen(t);
          final w = x.f.workspace(t);
          final task = w.task,
              result = w.result,
              mode = w.contentMode,
              extent = w.sheetExtent;
          await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
          await nowTap(
            t,
            find.byKey(
              Key(
                'online-intent-${repeatSame ? onlineIntentID : _secondOnlineID}',
              ),
            ),
          );
          expect(ids, [
            onlineIntentID,
            repeatSame ? onlineIntentID : _secondOnlineID,
          ]);
          expect(x.detailReads, 2);
          gates[1].complete();
          await t.pumpAndSettle();
          final title = repeatSame ? '一起线上阅读' : _secondOnlineTitle;
          expect(find.text('公开线上意图'), findsOneWidget);
          expect(find.text(title), findsOneWidget);
          final routeScaffolds = t
              .widgetList(find.byType(Scaffold, skipOffstage: false))
              .length;
          gates[0].complete();
          await t.pumpAndSettle();
          expect(
            t.widgetList(find.byType(Scaffold, skipOffstage: false)).length,
            routeScaffolds,
          );
          expect(find.text(title), findsOneWidget);
          expect(find.byType(SnackBar), findsNothing);
          expect(w.task, same(task));
          expect(w.result, same(result));
          expect(w.contentMode, mode);
          expect(w.sheetExtent, extent);
          expect(x.requests.every((r) => r.method == 'GET'), isTrue);
          expect(t.takeException(), isNull);
          await x.close(t);
        },
      );
    }
  }
  testWidgets(
    'ONLINE detail older A resolving while newer B pending cannot steal selected read route',
    (t) async {
      final x = _OnlineHistoryFixture('success');
      final gates = [Completer<void>(), Completer<void>()];
      final ids = <String>[];
      x.onlineTaskReply = () =>
          _onlineHistoryReply(_pairReadWire(onlineIntentID, both: true));
      x.onlineDetailRead = (id) async {
        final i = ids.length;
        ids.add(id);
        await gates[i].future;
        return _onlineHistoryReply(_pairReadWire(id));
      };
      await x.mount(t);
      await x.reopen(t);
      await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
      await nowTap(t, find.byKey(Key('online-intent-$_secondOnlineID')));
      gates[0].complete();
      await t.pumpAndSettle();
      expect(find.text('公开线上意图'), findsNothing);
      gates[1].complete();
      await t.pumpAndSettle();
      expect(find.text('公开线上意图'), findsOneWidget);
      expect(find.text(_secondOnlineTitle), findsOneWidget);
      expect(x.detailReads, 2);
      expect(t.takeException(), isNull);
      await x.close(t);
    },
  );
  testWidgets(
    'ONLINE detail current sequential human reads remain two original GET without clearing task',
    (t) async {
      final x = _OnlineHistoryFixture('success');
      x.onlineTaskReply = () =>
          _onlineHistoryReply(_pairReadWire(onlineIntentID, both: true));
      x.onlineDetailRead = (id) async => _onlineHistoryReply(_pairReadWire(id));
      await x.mount(t);
      await x.reopen(t);
      final w = x.f.workspace(t);
      final task = w.task, result = w.result;
      await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
      expect(find.text('公开线上意图'), findsOneWidget);
      await nowBack(t);
      await nowTap(t, find.byKey(Key('online-intent-$_secondOnlineID')));
      expect(find.text(_secondOnlineTitle), findsOneWidget);
      expect(x.detailReads, 2);
      expect(w.task, same(task));
      expect(w.result, same(result));
      expect(t.takeException(), isNull);
      await x.close(t);
    },
  );

  for (final status in ['401', '403']) {
    testWidgets(
      'ONLINE detail $status recovery keeps original result and allows fresh human GET',
      (t) async {
        final x = _OnlineHistoryFixture('success')..detailOutcome = status;
        await x.mount(t);
        await x.reopen(t);
        final w = x.f.workspace(t);
        final task = w.task, result = w.result;
        await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
        final expected = status == '401'
            ? '请先通过个人资料重新登录，再发起查询。'
            : '请先确认当前身份和访问权限；恢复权限后再发起查询。';
        expect(find.textContaining(expected), findsOneWidget);
        expect(w.task, same(task));
        expect(w.result, same(result));
        x.detailOutcome = 'success';
        await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
        expect(x.detailReads, 2);
        expect(find.text('公开线上意图'), findsOneWidget);
        expect(w.task, same(task));
        expect(w.result, same(result));
        expect(x.requests.every((r) => r.method == 'GET'), isTrue);
        expect(t.takeException(), isNull);
        await x.close(t);
      },
    );
  }
  testWidgets(
    'ONLINE detail same account label change preserves current legitimate source read',
    (t) async {
      final x = _OnlineHistoryFixture('success')..holdDetail = true;
      await x.mount(t);
      await x.reopen(t);
      await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
      x.f.auth.updateProfileDisplayName('本人新昵称');
      await t.pumpAndSettle();
      x.detailGate.complete();
      await t.pumpAndSettle();
      expect(x.detailReads, 1);
      expect(find.text('公开线上意图'), findsOneWidget);
      expect(t.takeException(), isNull);
      await x.close(t);
    },
  );
  for (final change in ['client', 'base']) {
    for (final outcome in ['success', '503', 'network']) {
      testWidgets(
        'ONLINE detail late $outcome after $change replacement cannot push old read route',
        (t) async {
          final x = _OnlineHistoryFixture('success')
            ..detailOutcome = outcome
            ..holdDetail = true;
          await x.mount(t);
          await x.reopen(t);
          await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
          final state = t.state(find.byType(MapWorkspace));
          await t.pumpWidget(
            x.page(
              transport: change == 'client' ? x.f.client : x.client,
              base: change == 'base'
                  ? 'https://replacement-fixture.test'
                  : x.f.base,
            ),
          );
          await t.pumpAndSettle();
          expect(t.state(find.byType(MapWorkspace)), same(state));
          final w = x.f.workspace(t);
          final task = w.task, result = w.result;
          x.detailGate.complete();
          await t.pumpAndSettle();
          expect(x.detailReads, 1);
          expect(find.text('公开线上意图'), findsNothing);
          expect(find.byType(SnackBar), findsNothing);
          expect(w.task, same(task));
          expect(w.result, same(result));
          expect(t.takeException(), isNull);
          await x.close(t);
        },
      );
    }
  }
  testWidgets(
    'ONLINE detail selected read stays read-only and existing identity boundary retires stale snapshot',
    (t) async {
      final x = _OnlineHistoryFixture('success');
      await x.mount(t);
      await x.reopen(t);
      await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
      expect(find.text('公开线上意图'), findsOneWidget);
      x.f.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
      await t.pumpAndSettle();
      expect(find.textContaining('未发送消息或邀请'), findsNothing);
      expect(x.requests.every((r) => r.method == 'GET'), isTrue);
      expect(t.takeException(), isNull);
      await x.close(t);
    },
  );

  testWidgets(
    'ONLINE detail current actual original result tile opens authoritative read-only route',
    (t) async {
      final x = _OnlineHistoryFixture('success');
      await x.mount(t);
      await x.reopen(t);
      await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
      expect(x.detailReads, 1);
      expect(find.text('公开线上意图'), findsOneWidget);
      expect(find.textContaining('未发送消息或邀请'), findsOneWidget);
      expect(x.requests.every((r) => r.method == 'GET'), isTrue);
      expect(t.takeException(), isNull);
      await x.close(t);
    },
  );
  for (final outcome in ['401', '403', '503', 'network']) {
    testWidgets(
      'ONLINE detail actual tile $outcome preserves typed recovery instead of source unavailable',
      (t) async {
        final x = _OnlineHistoryFixture('success')..detailOutcome = outcome;
        await x.mount(t);
        await x.reopen(t);
        await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
        final expected = switch (outcome) {
          '401' => '登录已失效，请重新登录。',
          '403' => '当前身份无法读取此情境。',
          'network' => '暂时无法读取线上结果，请重试。',
          _ => '线上查询暂时不可用。',
        };
        expect(find.textContaining(expected), findsOneWidget);
        expect(find.text('此意图已不可用或权限发生变化，请重新查询。'), findsNothing);
        expect(find.text('公开线上意图'), findsNothing);
        expect(x.detailReads, 1);
        expect(t.takeException(), isNull);
        await x.close(t);
      },
    );
  }
  for (final change in ['new', 'city', 'auth-ABA', 'workspace-ABA']) {
    for (final outcome in ['success', '503', 'network']) {
      testWidgets(
        'ONLINE detail late $outcome after $change cannot open obsolete route or message',
        (t) async {
          final x = _OnlineHistoryFixture('success')
            ..detailOutcome = outcome
            ..holdDetail = true;
          await x.mount(t);
          await x.reopen(t);
          await nowTap(t, find.byKey(Key('online-intent-$onlineIntentID')));
          expect(x.detailReads, 1);
          if (change == 'new') {
            await x.open(t);
            await nowTap(t, find.text('新建对话'));
          } else if (change == 'city') {
            x.f.city.selectCity('beta');
            await t.pumpAndSettle();
          } else if (change == 'auth-ABA') {
            x.f.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
            x.f.auth.changeIdentity(
              'Bearer synthetic-online-owner',
              nextOwner: onlineOwnerID,
            );
            await t.pumpAndSettle();
          } else {
            x.organizations!.select(
              const OrganizationWorkspace(
                id: 'synthetic-org',
                name: '合成组织',
                role: 'member',
                organizationType: 'student',
              ),
            );
            x.organizations!.select(null);
            await t.pumpAndSettle();
          }
          final w = x.f.workspace(t);
          final task = w.task, result = w.result;
          x.detailGate.complete();
          await t.pumpAndSettle();
          expect(find.text('公开线上意图'), findsNothing);
          expect(find.byType(SnackBar), findsNothing);
          expect(w.task, same(task));
          expect(w.result, same(result));
          expect(t.takeException(), isNull);
          await x.close(t);
        },
      );
    }
  }

  testWidgets(
    'ONLINE Recent current success normal control actual two GET and authoritative task',
    (t) async {
      final x = _OnlineHistoryFixture('success');
      await x.mount(t);
      await x.reopen(t);
      final w = x.f.workspace(t);
      expect(x.restores, 2);
      expect(w.task?.id, onlineTaskID);
      expect(w.task?.contextType, 'ONLINE');
      expect(w.result?.onlineIntents, hasLength(1));
      expect(find.text('此线上任务的情境或来源已不可用。'), findsNothing);
      expect(t.takeException(), isNull);
      await x.close(t);
    },
  );
  for (final outcome in ['401', '403', '503', 'network']) {
    testWidgets(
      'ONLINE Recent actual preflight $outcome retains typed human recovery',
      (t) async {
        final x = _OnlineHistoryFixture(outcome);
        await x.mount(t);
        await x.reopen(t);
        final expected = switch (outcome) {
          '401' => '登录已失效，请重新登录。',
          '403' => '当前身份无法读取此情境。',
          'network' => '暂时无法读取线上结果，请重试。',
          _ => '线上查询暂时不可用。',
        };
        expect(find.textContaining(expected), findsOneWidget);
        expect(find.text('此线上任务的情境或来源已不可用。'), findsNothing);
        expect(x.restores, 1);
        expect(x.f.workspace(t).result, isNull);
        expect(t.takeException(), isNull);
        await x.close(t);
      },
    );
  }
  for (final change in ['new', 'city', 'auth-ABA', 'workspace-ABA']) {
    for (final outcome in ['success', '503', 'network']) {
      testWidgets(
        'ONLINE Recent pending $outcome after $change cannot overwrite new view',
        (t) async {
          final x = _OnlineHistoryFixture(outcome, held: true);
          await x.mount(t);
          await x.reopen(t);
          if (change == 'new') {
            await x.open(t);
            await nowTap(t, find.text('新建对话'));
          } else if (change == 'city') {
            x.f.city.selectCity('beta');
            await t.pumpAndSettle();
          } else if (change == 'auth-ABA') {
            x.f.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
            x.f.auth.changeIdentity(
              'Bearer synthetic-online-owner',
              nextOwner: onlineOwnerID,
            );
            await t.pumpAndSettle();
          } else {
            x.organizations!.select(
              const OrganizationWorkspace(
                id: 'synthetic-org',
                name: '合成组织',
                role: 'member',
                organizationType: 'student',
              ),
            );
            x.organizations!.select(null);
            await t.pumpAndSettle();
          }
          final w = x.f.workspace(t);
          final task = w.task,
              result = w.result,
              mode = w.contentMode,
              extent = w.sheetExtent;
          x.gate.complete();
          await t.pumpAndSettle();
          expect(x.restores, 1);
          expect(w.task, same(task));
          expect(w.result, same(result));
          expect(w.contentMode, mode);
          expect(w.sheetExtent, extent);
          expect(find.text('此线上任务的情境或来源已不可用。'), findsNothing);
          expect(t.takeException(), isNull);
          await x.close(t);
        },
      );
    }
  }

  for (final status in ['401', '403']) {
    testWidgets(
      'ONLINE Recent $status after real human permission recovery permits current GET',
      (t) async {
        final x = _OnlineHistoryFixture(status);
        await x.mount(t);
        await x.reopen(t);
        final expected = status == '401'
            ? '请先通过个人资料重新登录，再发起查询。'
            : '请先确认当前身份和访问权限；恢复权限后再发起查询。';
        expect(find.textContaining(expected), findsOneWidget);
        x.outcome = 'success';
        await x.open(t);
        await x.reopen(t);
        expect(x.restores, 3);
        expect(x.f.workspace(t).task?.id, onlineTaskID);
        expect(x.f.workspace(t).result?.onlineIntents, hasLength(1));
        expect(x.requests.every((r) => r.method == 'GET'), isTrue);
        expect(t.takeException(), isNull);
        await x.close(t);
      },
    );
  }
  testWidgets(
    'ONLINE Recent invalid native DTO stays conservative without false results',
    (t) async {
      final x = _OnlineHistoryFixture('format');
      await x.mount(t);
      await x.reopen(t);
      expect(x.restores, 1);
      expect(x.f.workspace(t).task, isNull);
      expect(x.f.workspace(t).result, isNull);
      expect(find.text('此线上任务的情境或来源已不可用。'), findsOneWidget);
      expect(x.requests.every((r) => r.method == 'GET'), isTrue);
      expect(t.takeException(), isNull);
      await x.close(t);
    },
  );
  testWidgets(
    'ONLINE Recent same account label change does not retire legitimate current read',
    (t) async {
      final x = _OnlineHistoryFixture('success', held: true);
      await x.mount(t);
      await x.reopen(t);
      x.f.auth.updateProfileDisplayName('本人新昵称');
      await t.pumpAndSettle();
      x.gate.complete();
      await t.pumpAndSettle();
      expect(x.restores, 2);
      expect(x.f.workspace(t).task?.id, onlineTaskID);
      expect(x.f.workspace(t).result?.onlineIntents, hasLength(1));
      expect(t.takeException(), isNull);
      await x.close(t);
    },
  );
  for (final change in ['client', 'base']) {
    for (final outcome in ['success', '503', 'network']) {
      testWidgets(
        'ONLINE Recent late $outcome after $change replacement cannot use old transport view',
        (t) async {
          final x = _OnlineHistoryFixture(outcome, held: true);
          await x.mount(t);
          await x.reopen(t);
          final state = t.state(find.byType(MapWorkspace));
          await t.pumpWidget(
            x.page(
              transport: change == 'client' ? x.f.client : x.client,
              base: change == 'base'
                  ? 'https://replacement-fixture.test'
                  : x.f.base,
            ),
          );
          await t.pumpAndSettle();
          expect(t.state(find.byType(MapWorkspace)), same(state));
          final w = x.f.workspace(t);
          final task = w.task,
              result = w.result,
              extent = w.sheetExtent,
              mode = w.contentMode;
          x.gate.complete();
          await t.pumpAndSettle();
          expect(x.restores, 1);
          expect(w.task, same(task));
          expect(w.result, same(result));
          expect(w.sheetExtent, extent);
          expect(w.contentMode, mode);
          expect(find.byType(SnackBar), findsNothing);
          expect(x.requests.every((r) => r.method == 'GET'), isTrue);
          expect(t.takeException(), isNull);
          await x.close(t);
        },
      );
    }
  }

  for (final failure in ['503', 'network']) {
    testWidgets(
      'Recent list actual sidebar $failure has one 48dp real GET reread preserving safe draft/current task',
      (t) async {
        t.view.physicalSize = const Size(390, 844);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final f = NowFixture();
        f.auth.token = 'Bearer synthetic-owner';
        addTearDown(f.dispose);
        final requests = <http.Request>[];
        final source = RemoteAgentTaskSource(
          cityID: () => f.city.selectedCity?.id,
          authorizationHeader: () => f.auth.authorizationHeader,
          apiBaseUrl: 'https://recent-list-fixture.test',
          client: MockClient((r) async {
            requests.add(r);
            expect(r.method, 'GET');
            expect(r.url.path, '/v1/me/agent-tasks');
            if (requests.length <= 2) {
              if (failure == 'network') {
                throw http.ClientException('synthetic network');
              }
              return http.Response('{}', 503);
            }
            return http.Response(
              '{"data":[{"id":"fixture-original-history","query":"已读取的合成原对话","status":"COMPLETED"}]}',
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          }),
        );
        await t.pumpWidget(
          MaterialApp(
            home: MapWorkspace(
              city: f.city,
              auth: f.auth,
              moments: f.moments,
              agentTaskSource: source,
              seedClient: f.client,
              seedApiBaseUrl: f.base,
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(requests, hasLength(2));
        final w = f.workspace(t);
        const current = AgentTask(
          id: 'local-safe-current',
          query: '当前安全查询',
          status: 'COMPLETED',
          cityID: 'alpha',
        );
        const result = AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: '当前结果',
        );
        w.task = current;
        w.result = result;
        w.selectedEntityId = 'place:original';
        w.state = AgentViewState.results;
        w.notifyListeners();
        await t.enterText(nowField(), '未发送安全草稿');
        await t.pumpAndSettle();
        await nowTap(t, find.byTooltip('打开侧边栏'));
        final button = find.byKey(const Key('sidebar-retry-history'));
        await t.scrollUntilVisible(
          button,
          120,
          scrollable: find
              .descendant(
                of: find.byType(Drawer),
                matching: find.byType(Scrollable),
              )
              .first,
        );
        await t.pumpAndSettle();
        expect(button.hitTestable(), findsOneWidget);
        expect(t.getSize(button).height, greaterThanOrEqualTo(48));
        await nowTap(t, button);
        expect(requests, hasLength(3));
        expect(w.recent.single.id, 'fixture-original-history');
        expect(w.recentError, isNull);
        expect(w.recentFailure, isNull);
        expect(w.task, same(current));
        expect(w.result, same(result));
        expect(w.selectedEntityId, 'place:original');
        expect(find.text('最近对话读取失败'), findsNothing);
        expect(find.text('重新读取最近对话'), findsNothing);
        await nowBack(t);
        expect(t.widget<TextField>(nowField()).controller?.text, '未发送安全草稿');
        expect(t.takeException(), isNull);
        await f.unmount(t);
        source.dispose();
      },
    );
  }
  testWidgets(
    'Recent list actual sidebar loading is distinct from empty and completes to true empty',
    (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final f = NowFixture();
      f.auth.token = 'Bearer synthetic-owner';
      addTearDown(f.dispose);
      final held = Completer<http.Response>();
      final source = RemoteAgentTaskSource(
        cityID: () => null,
        authorizationHeader: () => f.auth.authorizationHeader,
        apiBaseUrl: 'https://recent-list-fixture.test',
        client: MockClient((r) async {
          expect(r.method, 'GET');
          return held.future;
        }),
      );
      await t.pumpWidget(
        MaterialApp(
          home: MapWorkspace(
            city: f.city,
            auth: f.auth,
            moments: f.moments,
            agentTaskSource: source,
            seedClient: f.client,
            seedApiBaseUrl: f.base,
          ),
        ),
      );
      await t.pumpAndSettle();
      await nowTap(t, find.byTooltip('打开侧边栏'));
      await t.scrollUntilVisible(
        find.text('正在读取最近对话…'),
        100,
        scrollable: find
            .descendant(
              of: find.byType(Drawer),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      expect(find.text('你开始的对话会显示在这里。'), findsNothing);
      expect(find.text('最近对话读取失败'), findsNothing);
      held.complete(http.Response('{"data":[]}', 200));
      await t.pumpAndSettle();
      expect(find.text('正在读取最近对话…'), findsNothing);
      expect(find.text('你开始的对话会显示在这里。'), findsOneWidget);
      expect(f.workspace(t).requestError, isNull);
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
    },
  );
  testWidgets(
    'Recent list guest preserves local history with zero authenticated GET and no failure UI',
    (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final f = NowFixture();
      addTearDown(f.dispose);
      var gets = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => null,
        authorizationHeader: () => null,
        apiBaseUrl: 'https://recent-list-fixture.test',
        client: MockClient((r) async {
          gets++;
          return http.Response('{}', 401);
        }),
      );
      await t.pumpWidget(
        MaterialApp(
          home: MapWorkspace(
            city: f.city,
            auth: f.auth,
            moments: f.moments,
            agentTaskSource: source,
            seedClient: f.client,
            seedApiBaseUrl: f.base,
          ),
        ),
      );
      await t.pumpAndSettle();
      final w = f.workspace(t);
      const local = AgentTask(
        id: 'local-guest-history',
        query: '访客本次对话',
        status: 'COMPLETED',
      );
      w.recent.add(local);
      await w.loadRecent();
      await t.pumpAndSettle();
      expect(gets, 0);
      expect(w.recent, [local]);
      await nowTap(t, find.byTooltip('打开侧边栏'));
      await t.scrollUntilVisible(
        find.text('访客本次对话'),
        100,
        scrollable: find
            .descendant(
              of: find.byType(Drawer),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      expect(find.text('访客本次对话').hitTestable(), findsOneWidget);
      expect(find.text('最近对话读取失败'), findsNothing);
      expect(find.text('你开始的对话会显示在这里。'), findsNothing);
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
    },
  );

  for (final mode in ['empty', '401', '403', 'network']) {
    testWidgets(
      'Recent list actual mounted Now sidebar $mode separates read failure from true empty',
      (t) async {
        t.view.physicalSize = const Size(390, 844);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final f = NowFixture();
        f.auth.token = 'Bearer synthetic-owner';
        addTearDown(f.dispose);
        final requests = <http.Request>[];
        final source = RemoteAgentTaskSource(
          cityID: () => f.city.selectedCity?.id,
          authorizationHeader: () => f.auth.authorizationHeader,
          apiBaseUrl: 'https://recent-list-fixture.test',
          client: MockClient((r) async {
            requests.add(r);
            expect(r.method, 'GET');
            expect(r.url.path, '/v1/me/agent-tasks');
            if (mode == 'empty') return http.Response('{"data":[]}', 200);
            if (mode == 'network') {
              throw http.ClientException('synthetic network');
            }
            return http.Response(
              jsonEncode({
                'error': {'code': mode == '401' ? 'unauthorized' : 'forbidden'},
              }),
              int.parse(mode),
            );
          }),
        );
        await t.pumpWidget(
          MaterialApp(
            home: MapWorkspace(
              city: f.city,
              auth: f.auth,
              moments: f.moments,
              agentTaskSource: source,
              seedClient: f.client,
              seedApiBaseUrl: f.base,
            ),
          ),
        );
        await t.pumpAndSettle();
        await nowTap(t, find.byTooltip('打开侧边栏'));
        final scroll = find
            .descendant(
              of: find.byType(Drawer),
              matching: find.byType(Scrollable),
            )
            .first;
        await t.scrollUntilVisible(
          find.text('最近的智能体对话'),
          100,
          scrollable: scroll,
        );
        await t.drag(scroll, const Offset(0, -160));
        await t.pumpAndSettle();
        final empty = find.text('你开始的对话会显示在这里。');
        debugPrintSynchronously((
          jsonEncode({
            'recentList': 'actual-mounted-Now-Sidebar',
            'mode': mode,
            'requests': requests.length,
            'emptyMounted': empty.evaluate().isNotEmpty,
          })).toString());
        expect(requests, hasLength(2));
        expect(empty, mode == 'empty' ? findsOneWidget : findsNothing);
        if (mode != 'empty') {
          final error = find.text('最近对话读取失败');
          await t.ensureVisible(error);
          await t.pumpAndSettle();
          expect(error.hitTestable(), findsOneWidget);
          if (mode == '401' || mode == '403') {
            expect(find.text('重新读取最近对话'), findsNothing);
            final recovery = find.text(
              mode == '401'
                  ? '请先通过个人资料重新登录，再读取最近对话。'
                  : '请先确认当前身份和访问权限；恢复权限后再读取最近对话。',
            );
            await t.ensureVisible(recovery);
            await t.pumpAndSettle();
            expect(recovery.hitTestable(), findsOneWidget);
          } else {
            expect(find.text('重新读取最近对话'), findsOneWidget);
          }
        }
        expect(f.workspace(t).requestError, isNull);
        expect(f.workspace(t).task, isNull);
        expect(t.takeException(), isNull);
        await f.unmount(t);
        source.dispose();
      },
    );
  }

  testWidgets('真实未选城市侧栏有就地选城路径', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    await nowTap(t, find.byTooltip('打开侧边栏'));
    final chooser = find.descendant(
      of: find.byType(Drawer),
      matching: find.text('选择城市'),
    );
    expect(chooser, findsOneWidget);
    await nowTap(t, chooser);
    await nowTap(t, find.text('甲验收城市'));
    expect(f.city.selectedCity?.id, 'alpha');
    await f.unmount(t);
  });
  testWidgets('真实匿名侧栏不声称已有本人智能体或组织身份', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await nowTap(t, find.byTooltip('打开侧边栏'));
    expect(find.text('未登录 · 公开浏览'), findsOneWidget);
    expect(find.text('个人智能体'), findsNothing);
    await f.unmount(t);
  });
  testWidgets('真实25项历史不会把设置与账号入口推离屏幕', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    f
        .workspace(t)
        .recent
        .addAll(
          List.generate(
            25,
            (i) => AgentTask(
              id: 'test-$i',
              query: '第$i个合成历史对话',
              status: 'COMPLETED',
            ),
          ),
        );
    f.workspace(t).notifyListeners();
    await t.pumpAndSettle();
    await nowTap(t, find.byTooltip('打开侧边栏'));
    expect(find.text('设置').hitTestable(), findsOneWidget);
    expect(find.text('个人资料').hitTestable(), findsOneWidget);
    expect(find.text('第24个合成历史对话'), findsNothing);
    await f.unmount(t);
  });
  testWidgets('真实个人资料无城市人格及规划假入口', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await nowTap(t, find.byTooltip('打开侧边栏'));
    await nowTap(t, find.text('个人资料'));
    expect(find.text('组织 / 城市工作区'), findsNothing);
    expect(find.text('规划中'), findsNothing);
    await f.unmount(t);
  });
  testWidgets('真实匿名设置诊断默认收起且保留可达入口', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await nowTap(t, find.byTooltip('打开侧边栏'));
    await nowTap(t, find.text('设置'));
    expect(find.text('运行诊断（开发版）'), findsOneWidget);
    expect(find.byType(AgentDebugPanel), findsNothing);
    await nowTap(t, find.text('运行诊断（开发版）'));
    expect(find.byType(AgentDebugPanel), findsOneWidget);
    expect(find.text('调试诊断'), findsOneWidget);
    await f.unmount(t);
  });
}
http.Response _onlineHistoryReply(Object data) => http.Response(
  jsonEncode({'data': data}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

class _OnlineHistoryFixture {
  _OnlineHistoryFixture(this.outcome, {this.held = false}) {
    f.auth.token = 'Bearer synthetic-online-owner';
    f.auth.owner = onlineOwnerID;
    client = MockClient((r) async {
      requests.add(r);
      expect(r.method, 'GET');
      if (r.url.path == '/v1/me/agent-tasks') {
        return _onlineHistoryReply([onlineWire()['task']]);
      }
      if (r.url.path == '/v1/me/now/online/tasks/$onlineTaskID' &&
          onlineTaskReply != null) {
        restores++;
        return onlineTaskReply!();
      }
      if (r.url.path.startsWith('/v1/me/now/online/intents/') &&
          onlineDetailRead != null) {
        detailReads++;
        return onlineDetailRead!(r.url.path.split('/').last);
      }
      if (r.url.path == '/v1/me/now/online/intents/$onlineIntentID') {
        detailReads++;
        if (holdDetail) await detailGate.future;
        if (detailOutcome == 'network') {
          throw http.ClientException('synthetic network');
        }
        if (detailOutcome != 'success') {
          return http.Response('{}', int.parse(detailOutcome));
        }
        return _onlineHistoryReply(onlineWire());
      }
      if (r.url.path == '/v1/me/now/online/tasks/$onlineTaskID') {
        restores++;
        if (held && restores == 1) await gate.future;
        if (outcome == 'network') {
          throw http.ClientException('synthetic network');
        }
        if (outcome == 'format') {
          return _onlineHistoryReply({'schema': 'invalid'});
        }
        if (outcome != 'success') {
          return http.Response('{}', int.parse(outcome));
        }
        return _onlineHistoryReply(onlineWire());
      }
      return _onlineHistoryReply([]);
    });
    api = NowContextQueryApi(
      authorizationHeader: () => f.auth.authorizationHeader,
      ownerID: () => f.auth.accountID,
      organizationWorkspaceID: () => organizations?.active?.id,
      client: client,
      apiBaseUrl: f.base,
    );
    source = RemoteAgentTaskSource(
      cityID: () => f.city.selectedCity?.id,
      authorizationHeader: () => f.auth.authorizationHeader,
      organizationWorkspaceID: () => organizations?.active?.id,
      onlineApi: api,
      client: client,
      apiBaseUrl: f.base,
    );
  }
  http.Response Function()? onlineTaskReply;
  Future<http.Response> Function(String id)? onlineDetailRead;
  String detailOutcome = 'success';
  bool holdDetail = false;
  int detailReads = 0;
  final detailGate = Completer<void>();
  String outcome;
  final bool held;
  final f = NowFixture();
  final gate = Completer<void>();
  final requests = <http.Request>[];
  int restores = 0;
  late final MockClient client;
  late final NowContextQueryApi api;
  late final RemoteAgentTaskSource source;
  OrganizationWorkspaceController? organizations;
  Widget page({http.Client? transport, String? base}) => MaterialApp(
    home: MapWorkspace(
      city: f.city,
      auth: f.auth,
      moments: f.moments,
      agentTaskSource: source,
      seedClient: transport ?? client,
      seedApiBaseUrl: base ?? f.base,
    ),
  );
  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await t.pumpWidget(page());
    await t.pumpAndSettle();
    await open(t);
    organizations = t.widget<Sidebar>(find.byType(Sidebar)).organizations;
  }

  Future<void> open(WidgetTester t) async {
    await nowTap(t, find.byTooltip('打开侧边栏'));
  }

  Future<void> reopen(WidgetTester t) async {
    final row = find.descendant(
      of: find.byType(Drawer),
      matching: find.text('阅读'),
    );
    await t.scrollUntilVisible(
      row,
      120,
      scrollable: find
          .descendant(
            of: find.byType(Drawer),
            matching: find.byType(Scrollable),
          )
          .first,
    );
    await nowTap(t, row);
    expect(
      restores,
      greaterThanOrEqualTo(held || outcome != 'success' ? 1 : 2),
    );
  }

  Future<void> close(WidgetTester t) async {
    await f.unmount(t);
    api.dispose();
    source.dispose();
    client.close();
    f.dispose();
  }
}

const _secondOnlineID = 'a1700000-0000-4000-8000-000000000005';
const _secondOnlineTitle = '另一个线上阅读意图';
Map<String, dynamic> _pairReadWire(String id, {bool both = false}) {
  final wire = onlineWire();
  final original = Map<String, dynamic>.from(
    (wire['items'] as List).single as Map,
  );
  final second = {
    ...original,
    'id': _secondOnlineID,
    'title': _secondOnlineTitle,
  };
  wire['items'] = both
      ? [original, second]
      : [id == _secondOnlineID ? second : original];
  return wire;
}

// Synthetic HTTP only; the original LegacyProfilePage opens the original
// private editor, whose real controller sends the observed HTTP request.
class _PrivateDraftFixture {
  final auth = SeedTestAuth();
  final city = NowFixtureCity();
  final writes = <(String, String?, Map<String, dynamic>)>[];
  late final MockClient client;
  late final PrivateMomentController moments;
  Map<String, dynamic>? saved;
  Completer<void>? writeGate;
  Completer<void>? refreshGate;
  bool failWrite = false;
  _PrivateDraftFixture() {
    client = MockClient((r) async {
      if (r.method == 'GET') {
        if (r.url.path == '/v1/me/moments') await refreshGate?.future;
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': r.url.path == '/v1/me/moments' && saved != null
                  ? [saved]
                  : [],
            }),
          ),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      final body = r.body.isEmpty
          ? <String, dynamic>{}
          : jsonDecode(r.body) as Map<String, dynamic>;
      writes.add((r.method, r.headers['Authorization'], body));
      await writeGate?.future;
      if (failWrite) throw http.ClientException('synthetic uncertain write');
      if (r.method == 'DELETE') {
        saved = null;
        return http.Response('', 204);
      }
      saved = {
        ...body,
        'id': '11111111-1111-4111-8111-111111111111',
        'status': 'draft',
        'revision': r.method == 'PUT' ? (body['revision'] as int) + 1 : 1,
        'activityIds': <String>[],
      };
      return http.Response.bytes(
        utf8.encode(jsonEncode({'data': saved})),
        r.method == 'POST' ? 201 : 200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'http://private-draft-fixture.test',
    );
  }
  Future<void> mount(WidgetTester t) async {
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: LegacyProfilePage(
            auth: auth,
            moments: moments,
            city: city,
            client: client,
            apiBaseUrl: 'http://private-draft-fixture.test',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  Future<void> open(WidgetTester t) async {
    await nowTap(t, find.text('新建草稿'));
    expect(find.text('新建动态'), findsOneWidget);
    await t.enterText(find.widgetWithText(TextFormField, '标题'), '原本人安全草稿');
    await t.enterText(find.widgetWithText(TextFormField, '记录'), '原本人私密记录');
    await t.pumpAndSettle();
  }

  Future<void> save(WidgetTester t) async {
    await nowTap(t, find.text('保存私人草稿'));
  }

  Future<void> seedDraft() async {
    await moments.create(cityID: 'alpha', title: '已保存本人草稿', body: '本人记录');
    writes.clear();
  }

  Future<void> close(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    moments.dispose();
    city.dispose();
    auth.dispose();
  }
}

class _BoundMomentFixture {
  final auth = SeedTestAuth();
  final city = NowFixtureCity();
  final reads = <http.Request>[];
  final writes = <http.Request>[];
  final gates = <Completer<void>>[];
  final titles = {'owner': 'A本人原记录', 'peer': 'B本人原记录'};
  late final MockClient client;
  late final PrivateMomentController moments;
  Completer<void>? writeGate;
  String? authorOverride;
  String readOutcome = 'success', writeOutcome = 'success';
  int revision = 1;
  final deletedAuthors = <String>{};
  _BoundMomentFixture() {
    client = MockClient((r) async {
      // Match the authenticated wire author, never a mutable UI owner label.
      final author = r.headers['Authorization'] == 'Bearer owner'
          ? 'owner'
          : 'peer';
      final override = authorOverride;
      final record = <String, dynamic>{
        'id': author == 'owner'
            ? '11111111-1111-4111-8111-111111111111'
            : '22222222-2222-4222-8222-222222222222',
        if (override != 'missing') 'authorAccountId': override ?? author,
        'cityId': 'alpha',
        'title': titles[author],
        'body': '仅本人记录',
        'timePrecision': 'unknown',
        'locationPrecision': 'city',
        'status': 'draft',
        'revision': revision,
      };
      if (r.method == 'GET') {
        reads.add(r);
        final outcome = readOutcome;
        final gate = gates.isEmpty ? null : gates.removeAt(0);
        await gate?.future;
        if (outcome == 'network') {
          throw http.ClientException('synthetic read network');
        }
        if (outcome == '503') return http.Response('{}', 503);
        return _boundMomentReply(
          deletedAuthors.contains(author) ? [] : [record],
        );
      }
      writes.add(r);
      final outcome = writeOutcome;
      await writeGate?.future;
      if (outcome == 'network') {
        throw http.ClientException('synthetic lost write');
      }
      if (r.method == 'DELETE') {
        deletedAuthors.add(author);
        return http.Response('', 204);
      }
      final input = jsonDecode(r.body) as Map<String, dynamic>;
      record['title'] = input['title'];
      if (r.method == 'PUT') record['revision'] = ++revision;
      return _boundMomentReply(record, r.method == 'POST' ? 201 : 200);
    });
    moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      ownerID: () => auth.accountID,
      identityChanges: auth,
      client: client,
      apiBaseUrl: 'http://bound-moment-fixture.test',
    );
  }
  Future<void> mount(WidgetTester t) async {
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: LegacyProfilePage(
            auth: auth,
            moments: moments,
            city: city,
            client: MockClient((r) async => _boundMomentReply([])),
            apiBaseUrl: 'http://bound-profile-fixture.test',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  Future<void> unmount(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    close();
  }

  void close() {
    moments.dispose();
    auth.dispose();
    city.dispose();
  }
}

http.Response _boundMomentReply(Object data, [int status = 200]) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );

class _PersonalRouteFixture {
  final current = _BoundMomentFixture();
  final other = _BoundMomentFixture();
  final client = MockClient((r) async => _boundMomentReply([]));
  final otherClient = MockClient((r) async => _boundMomentReply([]));
  late final replacementMoments = PrivateMomentController(
    authorizationHeader: () => current.auth.authorizationHeader,
    ownerID: () => current.auth.accountID,
    identityChanges: current.auth,
    client: MockClient(
      (r) async => _boundMomentReply([
        {
          'id': '33333333-3333-4333-8333-333333333333',
          'authorAccountId': 'owner',
          'cityId': 'alpha',
          'title': 'A当前替换来源记录',
          'body': '同账号新来源',
          'timePrecision': 'unknown',
          'locationPrecision': 'city',
          'visibility': 'private',
          'status': 'draft',
          'revision': 1,
        },
      ]),
    ),
    apiBaseUrl: 'http://replacement-moment.test',
  );
  late final source = NowFixtureSource(current.city);
  String change = '';
  Widget page() => MaterialApp(
    home: MapWorkspace(
      city: current.city,
      auth: change == 'auth' ? other.auth : current.auth,
      moments: change == 'auth'
          ? other.moments
          : change == 'moments'
          ? replacementMoments
          : current.moments,
      agentTaskSource: source,
      seedClient: change == 'client' ? otherClient : client,
      seedApiBaseUrl: change == 'base'
          ? 'http://new-personal-route.test'
          : 'http://personal-route.test',
    ),
  );
  Future<void> mount(WidgetTester t) async {
    await current.moments.refresh();
    await replacementMoments.refresh();
    other.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
    await t.pump();
    await t.pumpWidget(page());
    await t.pumpAndSettle();
  }

  Future<void> open(WidgetTester t, String route) async {
    await nowTap(t, find.byTooltip('打开侧边栏'));
    await nowTap(t, find.text(route));
  }

  Future<void> replace(WidgetTester t, String value) async {
    change = value;
    await t.pumpWidget(page());
    await t.pumpAndSettle();
  }

  Future<void> close(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    replacementMoments.dispose();
    current.close();
    other.close();
    client.close();
    otherClient.close();
  }
}
