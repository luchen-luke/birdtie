import 'dart:async';

import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'now_scope_recovery_test.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  var reads = 0;
  Future<Map<String, dynamic>?> Function()? response;
  setUp(() {
    reads = 0;
    response = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.getData') {
            expect(call.arguments, Clipboard.kTextPlain);
            reads++;
            if (response != null) return await response!();
            return {'text': '链接 https://example.invalid/student'};
          }
          return null;
        });
  });
  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null);
  });
  testWidgets('legacy helper contract: control 原Now无加号且直接打开旧素材helper不读取剪贴板', (
    t,
  ) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await t.enterText(nowField(), '已有安全草稿');
    await openLegacyMaterialTools(t);
    expect(reads, 0);
    expect(f.source.queries, isEmpty);
    await nowBack(t);
    expect(t.widget<TextField>(nowField()).controller!.text, '已有安全草稿');
    await f.unmount(t);
  });
  testWidgets('legacy helper contract: 原Now显式粘贴文字链接保留旧稿且不发送或解释素材', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await t.enterText(nowField(), '已有安全草稿');
    await openLegacyMaterialTools(t);
    expect(find.text('添加素材'), findsOneWidget);
    expect(find.text('快捷任务'), findsOneWidget);
    await nowTap(t, find.text('粘贴文字/链接'));
    expect(reads, 1);
    expect(
      t.widget<TextField>(nowField()).controller!.text,
      '已有安全草稿\n链接 https://example.invalid/student',
    );
    expect(f.source.queries, isEmpty);
    expect(f.workspace(t).task, isNull);
    expect(f.workspace(t).conversation, isEmpty);
    await f.unmount(t);
  });
  testWidgets('legacy helper contract: 原ONLINE无加号，旧素材helper不覆盖已有草稿', (t) async {
    final workspace = AgentWorkspaceController();
    addTearDown(workspace.dispose);
    final sent = <String>[];
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: sent.add,
            onSearchArea: () {},
            hasSearchArea: false,
            onlineMode: true,
          ),
        ),
      ),
    );
    await t.enterText(find.byType(TextField), '线上未发草稿');
    await openLegacyMaterialTools(t);
    expect(find.text('添加素材'), findsOneWidget);
    expect(find.text('快捷任务'), findsOneWidget);
    expect(
      t.widget<TextField>(find.byType(TextField)).controller!.text,
      '线上未发草稿',
    );
    expect(reads, 0);
    expect(sent, isEmpty);
    await t.pumpWidget(const SizedBox());
  });

  for (final empty in [null, '', '   ']) {
    testWidgets('legacy helper contract: 显式粘贴空剪贴板 $empty 反馈且原稿不变', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      response = () async => empty == null ? null : {'text': empty};
      await f.mount(t);
      await t.enterText(nowField(), '原稿');
      await paste(t);
      expect(draft(t), '原稿');
      expect(find.text('剪贴板没有可粘贴的文字或链接。'), findsOneWidget);
      expect(reads, 1);
      expect(f.source.queries, isEmpty);
      await f.unmount(t);
    });
  }

  testWidgets('legacy helper contract: 系统拒绝读取有反馈且可再点击恢复不覆盖原稿', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    response = () async => throw PlatformException(code: 'denied');
    await f.mount(t);
    await t.enterText(nowField(), '原稿');
    await paste(t);
    expect(draft(t), '原稿');
    expect(find.text('暂时无法读取剪贴板。请允许系统读取或手动输入。'), findsOneWidget);
    response = () async => {'text': '可编辑素材'};
    await paste(t);
    expect(draft(t), '原稿\n可编辑素材');
    expect(reads, 2);
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });

  for (final text in ['x' * 241, '中' * 81]) {
    testWidgets('legacy helper contract: 过长素材按真实UTF8契约拒绝不截断 ${text.length}', (
      t,
    ) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      response = () async => {'text': text};
      await f.mount(t);
      await t.enterText(nowField(), '已有原稿');
      await paste(t);
      expect(draft(t), '已有原稿');
      expect(find.textContaining('文字较长，未粘贴且保留原草稿'), findsOneWidget);
      expect(f.source.queries, isEmpty);
      await f.unmount(t);
    });
  }

  testWidgets('legacy helper contract: 240UTF8字节短素材可编辑并仅明确发送后调用原query一次', (
    t,
  ) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    final text = '中' * 80;
    response = () async => {'text': text};
    await f.mount(t);
    await paste(t);
    expect(draft(t), text);
    expect(f.source.queries, isEmpty);
    await t.enterText(nowField(), '用户核对后的文字 https://example.invalid 明天地点未核验');
    await t.testTextInput.receiveAction(TextInputAction.send);
    await t.pumpAndSettle();
    expect(f.source.queries, ['用户核对后的文字 https://example.invalid 明天地点未核验']);
    expect(draft(t), isEmpty);
    await f.unmount(t);
  });

  testWidgets('legacy helper contract: 读取期间编辑与文本ABA不复活旧粘贴并允许重新点击', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    final pending = Completer<Map<String, dynamic>?>();
    response = () => pending.future;
    await f.mount(t);
    await t.enterText(nowField(), '原稿A');
    await paste(t);
    await nowTap(t, nowField());
    await t.enterText(nowField(), '编辑B');
    await t.enterText(nowField(), '原稿A');
    pending.complete({'text': '旧读取素材'});
    await t.pumpAndSettle();
    expect(draft(t), '原稿A');
    expect(find.text('输入已变化，未加入迟到的剪贴板内容。请重新点击粘贴。'), findsOneWidget);
    response = () async => {'text': '新明确粘贴'};
    await paste(t);
    expect(draft(t), '原稿A\n新明确粘贴');
    expect(reads, 2);
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });

  for (final boundary in [
    'taskABA',
    'cityABA',
    'identityABA',
    'transportABA',
  ]) {
    testWidgets('legacy helper contract: 原Map $boundary 拒绝迟到素材且不发送', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      final pending = Completer<Map<String, dynamic>?>();
      response = () => pending.future;
      await f.mount(t);
      await t.enterText(nowField(), '当前未发草稿');
      await paste(t);
      switch (boundary) {
        case 'taskABA':
          f.workspace(t).newTask();
          f.workspace(t).newTask();
        case 'cityABA':
          f.city.selectCity('beta');
          f.city.selectCity('alpha');
        case 'identityABA':
          f.auth.changeIdentity('Bearer B', nextOwner: 'person-B');
          f.auth.changeIdentity(null, nextOwner: 'person-A');
        case 'transportABA':
          f.base = 'http://second-fixture.test';
          await f.mount(t);
          f.base = 'http://now-fixture.test';
          await f.mount(t);
      }
      pending.complete({'text': '旧上下文素材不得进入'});
      await t.pumpAndSettle();
      expect(draft(t), isNot(contains('旧上下文素材不得进入')));
      expect(f.source.queries, isEmpty);
      expect(reads, 1);
      response = () async => {'text': '当前新素材'};
      await paste(t);
      expect(draft(t), contains('当前新素材'));
      expect(reads, 2);
      await f.unmount(t);
    });
  }

  testWidgets('legacy helper contract: 旧账号已粘贴私人素材在同token换owner时清除而新本人仍可粘贴', (
    t,
  ) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    f.auth.token = 'Bearer shared-synthetic-token';
    f.auth.owner = 'person-A';
    response = () async => {'text': 'A私人素材'};
    await f.mount(t);
    await paste(t);
    expect(draft(t), 'A私人素材');
    f.auth.changeIdentity(
      'Bearer shared-synthetic-token',
      nextOwner: 'person-B',
    );
    await t.pumpAndSettle();
    expect(draft(t), isEmpty);
    response = () async => {'text': 'B本人素材'};
    await paste(t);
    expect(draft(t), 'B本人素材');
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });

  testWidgets('legacy helper contract: 同账号昵称更新与普通layout通知不误退当前读取或已粘贴素材', (
    t,
  ) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    final pending = Completer<Map<String, dynamic>?>();
    response = () => pending.future;
    await f.mount(t);
    await paste(t);
    f.auth.updateProfileDisplayName('当前本人新昵称');
    f.workspace(t).setSheetExtent(AgentSheetExtent.expanded);
    await f.mount(t);
    pending.complete({'text': '当前有效素材'});
    await t.pumpAndSettle();
    expect(draft(t), '当前有效素材');
    f.workspace(t).setSheetExtent(AgentSheetExtent.peek);
    f.auth.updateProfileDisplayName('再次合法更新');
    await t.pumpAndSettle();
    expect(draft(t), '当前有效素材');
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });

  testWidgets('legacy helper contract: 旧scope错误与finally不得覆盖新读取状态或新素材', (
    t,
  ) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    final old = Completer<Map<String, dynamic>?>();
    final fresh = Completer<Map<String, dynamic>?>();
    response = () => old.future;
    await f.mount(t);
    await paste(t);
    f.city.selectCity('beta');
    await t.pumpAndSettle();
    response = () => fresh.future;
    await paste(t);
    expect(reads, 2);
    old.completeError(PlatformException(code: 'old-denied'));
    await t.pumpAndSettle();
    expect(find.text('暂时无法读取剪贴板。请允许系统读取或手动输入。'), findsNothing);
    await openLegacyMaterialTools(t);
    final tile = t.widget<ListTile>(
      find.ancestor(of: find.text('粘贴文字/链接'), matching: find.byType(ListTile)),
    );
    expect(tile.enabled, false);
    await nowBack(t);
    fresh.complete({'text': '新范围素材'});
    await t.pumpAndSettle();
    expect(draft(t), '新范围素材');
    expect(reads, 2);
    await f.unmount(t);
  });

  testWidgets('legacy helper contract: 已打开旧身份菜单不得在切换后触发Clipboard读取', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await openLegacyMaterialTools(t);
    f.auth.changeIdentity('Bearer B', nextOwner: 'person-B');
    await t.pumpAndSettle();
    await nowTap(t, find.text('粘贴文字/链接'));
    expect(reads, 0);
    expect(draft(t), isEmpty);
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });

  testWidgets('legacy helper contract: 粘贴后正常侧栏往返保留安全草稿且不自动IME', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await paste(t);
    final previous = draft(t);
    await nowTap(t, find.byTooltip('打开侧边栏'));
    await nowTap(t, find.byKey(const Key('sidebar-account')));
    await nowTap(t, find.text(f.auth.signedIn ? '个人资料与账户' : '登录 / 账户'));
    await nowBack(t);
    expect(draft(t), previous);
    expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
    expect(t.testTextInput.isVisible, false);
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });

  testWidgets('legacy helper contract: 粘贴后scope改变清除已编辑素材而非带入新任务', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await paste(t);
    await t.enterText(nowField(), '经过手动编辑的旧范围素材');
    f.city.selectCity('beta');
    await t.pumpAndSettle();
    expect(draft(t), isEmpty);
    f.city.selectCity('alpha');
    await t.pumpAndSettle();
    expect(draft(t), isEmpty);
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });

  for (final brightness in Brightness.values) {
    testWidgets(
      'legacy helper contract: 实际Map $brightness 大字IME素材反馈不遮挡粘贴重试与输入',
      (t) async {
        t.view.physicalSize = const Size(320, 720);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final f = NowFixture();
        addTearDown(f.dispose);
        await t.pumpWidget(
          MaterialApp(
            theme: birdtieTheme(brightness),
            builder: (context, child) => MediaQuery(
              data: MediaQuery.of(context).copyWith(
                textScaler: const TextScaler.linear(3),
                viewInsets: const EdgeInsets.only(bottom: 260),
              ),
              child: child!,
            ),
            home: MapWorkspace(
              city: f.city,
              auth: f.auth,
              moments: f.moments,
              agentTaskSource: f.source,
              seedClient: f.client,
              seedApiBaseUrl: f.base,
            ),
          ),
        );
        await t.pumpAndSettle();
        response = () async => throw PlatformException(code: 'denied');
        await paste(t);
        expect(find.byTooltip('打开快捷操作'), findsNothing);
        expect(find.byTooltip('关闭素材提示').hitTestable(), findsOneWidget);
        final composer = t.getRect(find.byType(AgentComposer));
        expect(composer.top, greaterThanOrEqualTo(0));
        expect(composer.bottom, closeTo(720 - 260 - 16, .5));
        response = () async => {'text': '短文字'};
        await paste(t);
        expect(draft(t), '短文字');
        expect(reads, 2);
        expect(t.takeException(), isNull);
        await f.unmount(t);
      },
    );
    testWidgets('legacy helper contract: 素材与原六快捷任务 $brightness 窄屏大字号滚动可达', (
      t,
    ) async {
      t.view.physicalSize = const Size(320, 720);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      final sent = <String>[];
      await t.pumpWidget(
        MaterialApp(
          theme: ThemeData(brightness: brightness),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(3)),
            child: child!,
          ),
          home: Scaffold(
            body: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onSearchArea: () {},
              hasSearchArea: false,
            ),
          ),
        ),
      );
      await openLegacyMaterialTools(t);
      for (final label in [
        '粘贴文字/链接',
        '找活动',
        '找组织',
        '找地点',
        '询问附近信息',
        '发布活动',
        '搜索当前地图区域',
      ]) {
        await t.ensureVisible(find.text(label));
        await t.pumpAndSettle();
        expect(find.text(label).hitTestable(), findsOneWidget);
      }
      expect(find.text('图片、语音、文件'), findsNothing);
      expect(find.textContaining('图片、语音、文件及长文暂未接入'), findsOneWidget);
      expect(reads, 0);
      expect(sent, isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    });
  }
  testWidgets('legacy helper contract: 真实组织workspace来回不得复活个人读取，返回个人仍可明确粘贴', (
    t,
  ) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await nowTap(t, find.byTooltip('打开侧边栏'));
    final organizations = t.widget<Sidebar>(find.byType(Sidebar)).organizations;
    await nowBack(t);
    final pending = Completer<Map<String, dynamic>?>();
    response = () => pending.future;
    await paste(t);
    organizations.select(
      const OrganizationWorkspace(
        id: 'synthetic-org',
        name: '合成组织',
        role: 'ADMIN',
        organizationType: 'CLUB',
      ),
    );
    organizations.select(null);
    pending.complete({'text': '旧个人素材'});
    await t.pumpAndSettle();
    expect(draft(t), isEmpty);
    response = () async => {'text': '新个人素材'};
    await paste(t);
    expect(draft(t), '新个人素材');
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });
  testWidgets('legacy helper contract: 同一真实task的合法结果到达不误退已粘贴素材，新task才清除', (
    t,
  ) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    final result = Completer<AgentResult>();
    f.source.pending = result;
    await f.mount(t);
    await t.enterText(nowField(), '找地点');
    await t.testTextInput.receiveAction(TextInputAction.send);
    await t.pump();
    final ws = f.workspace(t);
    final epoch = ws.taskEpoch;
    await openLegacyMaterialTools(t);
    await t.pump();
    await t.pump(const Duration(milliseconds: 300));
    await t.ensureVisible(find.text('粘贴文字/链接'));
    expect(find.text('粘贴文字/链接').hitTestable(), findsOneWidget);
    await t.tap(find.text('粘贴文字/链接'));
    await t.pump();
    await t.pump(const Duration(milliseconds: 300));
    await t.pump();
    expect(reads, 1);
    expect(draft(t), '链接 https://example.invalid/student');
    result.complete(f.source.response('找地点'));
    await t.pumpAndSettle();
    expect(ws.taskEpoch, epoch);
    expect(draft(t), '链接 https://example.invalid/student');
    expect(f.source.queries, ['找地点']);
    ws.newTask();
    await t.pumpAndSettle();
    expect(draft(t), isEmpty);
    await f.unmount(t);
  });
}

String draft(WidgetTester t) =>
    t.widget<TextField>(nowField()).controller!.text;

// This retained helper is not exposed as a Now attachment action. Keep its
// source, clipboard and draft guards covered without claiming UI integration.
Future<void> openLegacyMaterialTools(WidgetTester t) async {
  expect(find.byTooltip('打开快捷操作'), findsNothing);
  final state = t.state<AgentComposerState>(find.byType(AgentComposer));
  unawaited(state.showMaterialTools(state.context));
  await t.pump();
  await t.pump(const Duration(milliseconds: 300));
  await t.pump();
}

Future<void> paste(WidgetTester t) async {
  await openLegacyMaterialTools(t);
  await nowTap(t, find.text('粘贴文字/链接'));
}
