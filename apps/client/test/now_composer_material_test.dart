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
  testWidgets('原Now材料菜单不读取剪贴板、不提交并保留原草稿', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await t.enterText(nowField(), '已有安全草稿');
    await openMaterialMenu(t);
    expect(reads, 0);
    expect(f.source.queries, isEmpty);
    await nowBack(t);
    expect(t.widget<TextField>(nowField()).controller!.text, '已有安全草稿');
    await f.unmount(t);
  });
  testWidgets('显式剪贴板辅助合同保留旧稿，不发送或解释素材', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await t.enterText(nowField(), '已有安全草稿');
    await paste(t);
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
  testWidgets('ONLINE当前创建菜单不覆盖已有草稿，不触发剪贴板或提交', (t) async {
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
            onCreateActivity: () {},
          ),
        ),
      ),
    );
    await t.enterText(find.byType(TextField), '线上未发草稿');
    await openMaterialMenu(t);
    expect(find.text('添加材料'), findsOneWidget);
    expect(find.text('创建草稿'), findsOneWidget);
    expect(find.text('活动'), findsOneWidget);
    expect(find.text('粘贴文字/链接'), findsNothing);
    expect(
      t.widget<TextField>(find.byType(TextField)).controller!.text,
      '线上未发草稿',
    );
    expect(reads, 0);
    expect(sent, isEmpty);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('真实加号按已有callback渐进展示，图片和创建都不读取剪贴板或发送', (t) async {
    final workspace = AgentWorkspaceController();
    addTearDown(workspace.dispose);
    final actions = <String>[];
    final sent = <String>[];
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: sent.add,
            onSearchArea: () {},
            hasSearchArea: false,
            onDraftImage: () => actions.add('image'),
            onCreateActivity: () => actions.add('activity'),
          ),
        ),
      ),
    );
    await t.enterText(find.byType(TextField), '核对前不发送的原稿');
    final plus = find.descendant(
      of: find.byType(AgentComposer),
      matching: find.byIcon(Icons.add),
    );
    await t.tap(plus);
    await t.pumpAndSettle();
    expect(find.text('添加材料'), findsOneWidget);
    expect(find.text('图片'), findsOneWidget);
    // Recording review replaces the obsolete nested creation menu with one
    // grouped sheet; the same authorized callbacks and draft guards remain.
    expect(find.text('创建草稿'), findsOneWidget);
    expect(find.text('活动'), findsOneWidget);
    expect(find.text('粘贴文字/链接'), findsNothing);
    expect(reads, 0);
    await nowTap(t, find.text('图片'));
    expect(actions, ['image']);
    expect(
      t.widget<TextField>(find.byType(TextField)).controller!.text,
      '核对前不发送的原稿',
    );
    await t.tap(plus);
    await t.pumpAndSettle();
    expect(find.text('活动'), findsOneWidget);
    expect(find.text('动态'), findsNothing);
    expect(find.text('收藏地点'), findsNothing);
    expect(find.text('图片'), findsOneWidget);
    await nowTap(t, find.text('活动'));
    expect(actions, ['image', 'activity']);
    expect(
      t.widget<TextField>(find.byType(TextField)).controller!.text,
      '核对前不发送的原稿',
    );
    expect(reads, 0);
    expect(sent, isEmpty);
    await t.pumpWidget(const SizedBox());
  });

  for (final empty in [null, '', '   ']) {
    testWidgets('显式剪贴板辅助合同: 显式粘贴空剪贴板 $empty 反馈且原稿不变', (t) async {
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

  testWidgets('显式剪贴板辅助合同: 系统拒绝读取有反馈且可再点击恢复不覆盖原稿', (t) async {
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
    testWidgets('显式剪贴板辅助合同: 过长素材按真实UTF8契约拒绝不截断 ${text.length}', (t) async {
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

  testWidgets('显式剪贴板辅助合同: 240UTF8字节短素材可编辑并仅明确发送后调用原query一次', (t) async {
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

  testWidgets('显式剪贴板辅助合同: 读取期间编辑与文本ABA不复活旧粘贴并允许重新点击', (t) async {
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
    testWidgets('显式剪贴板辅助合同: 原Map $boundary 拒绝迟到素材且不发送', (t) async {
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

  testWidgets('显式剪贴板辅助合同: 旧账号已粘贴私人素材在同token换owner时清除而新本人仍可粘贴', (t) async {
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

  testWidgets('显式剪贴板辅助合同: 同账号昵称更新与普通layout通知不误退当前读取或已粘贴素材', (t) async {
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

  testWidgets('显式剪贴板辅助合同: 旧scope错误与finally不得覆盖新读取状态或新素材', (t) async {
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
    // The explicit helper remains single-flight after an old finally returns;
    // there is no clipboard tile in the product material menu.
    await paste(t);
    expect(reads, 2);
    fresh.complete({'text': '新范围素材'});
    await t.pumpAndSettle();
    expect(draft(t), '新范围素材');
    expect(reads, 2);
    await f.unmount(t);
  });

  testWidgets('已打开材料菜单上下文ABA不得触发旧图片或创建回调', (t) async {
    final workspace = AgentWorkspaceController();
    final identity = ValueNotifier<String>('owner-A');
    addTearDown(workspace.dispose);
    addTearDown(identity.dispose);
    var images = 0, created = 0;
    final sent = <String>[];
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: sent.add,
            onSearchArea: () {},
            hasSearchArea: false,
            materialContext: () => identity.value,
            materialContextChanges: identity,
            onDraftImage: () => images++,
            onCreateMoment: () => created++,
          ),
        ),
      ),
    );
    await t.enterText(find.byType(TextField), '原安全草稿');
    await openMaterialMenu(t);
    identity.value = 'owner-B';
    identity.value = 'owner-A';
    await t.pumpAndSettle();
    await nowTap(t, find.text('动态'));
    expect(reads, 0);
    expect(created, 0);
    expect(images, 0);
    expect(sent, isEmpty);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('显式剪贴板辅助合同: 粘贴后正常侧栏往返保留安全草稿且不自动IME', (t) async {
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

  testWidgets('显式剪贴板辅助合同: 浏览城市保留本人编辑后的未发送草稿，不自动提交', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await paste(t);
    await t.enterText(nowField(), '经过手动编辑的旧范围素材');
    f.city.selectCity('beta');
    await t.pumpAndSettle();
    expect(draft(t), '经过手动编辑的旧范围素材');
    f.city.selectCity('alpha');
    await t.pumpAndSettle();
    expect(draft(t), '经过手动编辑的旧范围素材');
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });

  for (final brightness in Brightness.values) {
    testWidgets('显式剪贴板辅助合同: 实际Map $brightness 大字IME素材反馈不遮挡粘贴重试与输入', (t) async {
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
      expect(composer.bottom, closeTo(720 - 260 - 8, .5));
      response = () async => {'text': '短文字'};
      await paste(t);
      expect(draft(t), '短文字');
      expect(reads, 2);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
    testWidgets('材料和创建单层分组 $brightness 窄屏大字号可达，回调不提交或覆盖草稿', (t) async {
      t.view.physicalSize = const Size(320, 720);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      final sent = <String>[];
      final actions = <String>[];
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
              onDraftImage: () => actions.add('image'),
              onTaskImage: () => actions.add('saved-image'),
              onCreateMoment: () => actions.add('moment'),
              onCreateActivity: () => actions.add('activity'),
              onSavePlace: () => actions.add('save-place'),
            ),
          ),
        ),
      );
      await t.enterText(find.byType(TextField), '原未发送安全草稿');
      await openMaterialMenu(t);
      for (final label in ['图片', '查看已保存会话图片', '动态', '活动', '收藏地点']) {
        await t.ensureVisible(find.text(label));
        await t.pumpAndSettle();
        expect(find.text(label).hitTestable(), findsOneWidget);
      }
      expect(find.text('动态'), findsOneWidget);
      expect(find.text('活动'), findsOneWidget);
      expect(find.text('收藏地点'), findsOneWidget);
      expect(find.text('粘贴文字/链接'), findsNothing);
      expect(find.text('为私人记录添加图片'), findsNothing);
      expect(find.text('快捷任务'), findsNothing);
      expect(find.text('图片'), findsOneWidget);
      expect(find.text('创建草稿'), findsOneWidget);
      await nowTap(t, find.text('动态'));
      expect(actions, ['moment']);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        '原未发送安全草稿',
      );
      expect(reads, 0);
      expect(sent, isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    });
  }
  testWidgets('显式剪贴板辅助合同: 真实组织workspace来回不得复活个人读取，返回个人仍可明确粘贴', (t) async {
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
  testWidgets('显式剪贴板辅助合同: 待发送回包保留后来编辑，正常回包和新task均不丢未发稿', (t) async {
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
    await paste(t);
    expect(reads, 1);
    expect(draft(t), '找地点\n链接 https://example.invalid/student');
    result.complete(f.source.response('找地点'));
    await t.pumpAndSettle();
    expect(ws.taskEpoch, epoch);
    expect(draft(t), '找地点\n链接 https://example.invalid/student');
    expect(f.source.queries, ['找地点']);
    ws.newTask();
    await t.pumpAndSettle();
    expect(draft(t), '找地点\n链接 https://example.invalid/student');
    await f.unmount(t);
  });
}

String draft(WidgetTester t) =>
    t.widget<TextField>(nowField()).controller!.text;

Future<void> openMaterialMenu(WidgetTester t) async {
  final state = t.state<AgentComposerState>(find.byType(AgentComposer));
  unawaited(state.showNowMaterialMenu());
  await t.pump();
  await t.pump(const Duration(milliseconds: 300));
  await t.pump();
}

Future<void> paste(WidgetTester t) async {
  // An explicit helper contract, not a user-facing attachment menu action.
  final state = t.state<AgentComposerState>(find.byType(AgentComposer));
  unawaited(state.pasteTextFromClipboard());
  await t.pump();
  await t.pump(const Duration(milliseconds: 300));
  await t.pump();
}
