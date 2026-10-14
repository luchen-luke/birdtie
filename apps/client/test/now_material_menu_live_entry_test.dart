import 'dart:async';

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_profile_page.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_profile_api_test.dart'
    show profileData, profileOwner, profileResponse;
import 'now_scope_recovery_test.dart';

Future<void> mountLiveMaterials(
  WidgetTester t,
  NowFixture f, {
  Brightness brightness = Brightness.light,
  double scale = 2,
  double keyboard = 0,
  http.Client? seedClient,
}) async {
  t.view.physicalSize = const Size(320, 720);
  t.view.devicePixelRatio = 1;
  await t.pumpWidget(
    MaterialApp(
      theme: birdtieTheme(brightness),
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(context).copyWith(
          textScaler: TextScaler.linear(scale),
          viewInsets: EdgeInsets.only(bottom: keyboard),
        ),
        child: child!,
      ),
      home: MapWorkspace(
        city: f.city,
        auth: f.auth,
        moments: f.moments,
        agentTaskSource: f.source,
        seedClient: seedClient ?? f.client,
        seedApiBaseUrl: f.base,
      ),
    ),
  );
  await t.pumpAndSettle();
}

// First real tap: no State helper and no pre-tap scroll or focus change.
Future<void> tapLiveMaterials(WidgetTester t) async {
  final button = find.byTooltip('添加材料');
  expect(button.hitTestable(), findsOneWidget);
  final size = t.getSize(button);
  expect(size.width, greaterThanOrEqualTo(48));
  expect(size.height, greaterThanOrEqualTo(48));
  await t.tap(button);
  await t.pumpAndSettle();
  expect(find.text('添加材料'), findsOneWidget);
}

Future<void> pasteLiveMaterials(WidgetTester t) async {
  // The menu entry was removed. Exercise the retained explicit native paste
  // operation, without claiming it is a reachable material-menu action.
  final state = t.state<AgentComposerState>(find.byType(AgentComposer));
  state.pauseEditing();
  unawaited(state.pasteTextFromClipboard());
  await t.pump();
  await t.pump(const Duration(milliseconds: 350));
}

String liveDraft(WidgetTester t) =>
    t.widget<TextField>(nowField()).controller!.text;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  tearDown(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null),
  );
  for (final scale in [1.0, 2.0]) {
    testWidgets('real settings agent route has one header and retires $scale', (
      t,
    ) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      f.auth.changeIdentity('Bearer owner', nextOwner: profileOwner);
      final reads = <http.Request>[];
      final client = MockClient((r) async {
        reads.add(r);
        return profileResponse(profileData(r.url.path));
      });
      addTearDown(client.close);
      await mountLiveMaterials(t, f, scale: scale, seedClient: client);
      await t.enterText(nowField(), '保留 Now 普通草稿');
      await nowTap(t, find.byTooltip('打开侧边栏'));
      await nowTap(t, find.byKey(const Key('sidebar-account')));
      await nowTap(t, find.text('设置'));
      expect(find.byType(SettingsPage), findsOneWidget);
      expect(find.byType(AppBar), findsOneWidget);
      final entry = find.text('我的智能体');
      await t.ensureVisible(entry);
      final tile = find.ancestor(of: entry, matching: find.byType(ListTile));
      expect(t.getSize(tile).height, greaterThanOrEqualTo(48));
      await nowTap(t, entry);
      expect(find.byType(AgentProfilePage), findsOneWidget);
      expect(find.byType(AppBar), findsOneWidget);
      expect(find.text('设置'), findsNothing);
      expect(find.textContaining('本人填写的合成补充'), findsOneWidget);
      expect(
        MediaQuery.textScalerOf(t.element(find.text('我的智能体'))).scale(20),
        closeTo(20 * scale, .01),
      );
      final back = find.byType(BackButton).hitTestable();
      expect(back, findsOneWidget);
      expect(t.getSize(back).width, greaterThanOrEqualTo(48));
      expect(t.getSize(back).height, greaterThanOrEqualTo(48));
      await nowTap(t, back);
      expect(find.byType(AgentProfilePage), findsNothing);
      expect(find.byType(SettingsPage), findsOneWidget);
      expect(find.byType(AppBar), findsOneWidget);
      expect(find.text('设置'), findsOneWidget);
      await nowTap(t, find.byType(BackButton).hitTestable());
      expect(find.byType(SettingsPage), findsNothing);
      expect(liveDraft(t), '保留 Now 普通草稿');
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, isFalse);
      expect(t.testTextInput.isVisible, isFalse);
      expect(find.byType(MapCanvas, skipOffstage: false), findsOneWidget);
      await nowTap(t, find.byTooltip('打开侧边栏'));
      await nowTap(t, find.byKey(const Key('sidebar-account')));
      await nowTap(t, find.text('设置'));
      await nowTap(t, find.text('我的智能体'));
      expect(find.textContaining('本人填写的合成补充'), findsOneWidget);
      const privateProfilePaths = {
        '/v1/me/agent-private-profile',
        '/v1/me/agent-memories',
        '/v1/me/agent-policies',
        '/v1/me/community-interests',
        '/v1/me/activity-participation-disclosures',
      };
      int profileReads() =>
          reads.where((r) => privateProfilePaths.contains(r.url.path)).length;
      final beforeRevokeReads = profileReads();
      f.auth.changeIdentity(null);
      await t.pump();
      f.auth.changeIdentity('Bearer owner', nextOwner: profileOwner);
      await t.pumpAndSettle();
      expect(find.textContaining('本人填写的合成补充'), findsNothing);
      expect(find.byType(AgentProfilePage), findsNothing);
      expect(find.byType(SettingsPage), findsNothing);
      expect(find.byType(AppBar), findsOneWidget);
      expect(find.textContaining('工作身份或来源已变化'), findsOneWidget);
      expect(profileReads(), beforeRevokeReads);
      expect(reads.every((r) => r.method == 'GET'), isTrue);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  final cases = [
    for (final brightness in Brightness.values)
      for (final mode in ['history', 'hidden-ime', 'keyboard'])
        (brightness: brightness, mode: mode, scale: 2.0),
    (brightness: Brightness.light, mode: 'history', scale: 1.0),
  ];
  for (final c in cases) {
    testWidgets('single tap real expanded Now ${c.brightness} ${c.mode} '
        '${c.scale} opens material menu', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      await mountLiveMaterials(t, f, brightness: c.brightness, scale: c.scale);
      await nowSend(t, '有没有适合周末见面的餐厅 Aberdeen');
      final workspace = f.workspace(t);
      workspace.setSheetExtent(AgentSheetExtent.expanded);
      await t.pumpAndSettle();
      final map = t.element(find.byType(MapCanvas)), task = workspace.task;
      if (c.mode != 'history') {
        await t.enterText(nowField(), '未发送的中文与 English 草稿');
        await t.pumpAndSettle();
        expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, isTrue);
        if (c.mode == 'hidden-ime') {
          t.testTextInput.hide();
          await t.pumpAndSettle();
          expect(t.testTextInput.isVisible, isFalse);
          expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, isTrue);
        } else {
          await mountLiveMaterials(
            t,
            f,
            brightness: c.brightness,
            scale: c.scale,
            keyboard: 260,
          );
          expect(
            t.getRect(find.byType(AgentComposer)).bottom,
            closeTo(720 - 260 - 12, .5),
          );
        }
      }
      final beforeDraft = liveDraft(t);
      await tapLiveMaterials(t);
      // Recording review: creation is a group in this one sheet, not a second
      // route. Keep the same single-tap/draft/source/IME assertions below.
      expect(find.text('选择材料'), findsOneWidget);
      expect(find.text('创建草稿'), findsOneWidget);
      expect(find.text('粘贴文字/链接'), findsNothing);
      expect(find.text('为私人记录添加图片'), findsNothing);
      expect(f.source.queries, ['有没有适合周末见面的餐厅 Aberdeen']);
      expect(find.byType(TextField, skipOffstage: false), findsOneWidget);
      expect(find.byType(MapCanvas, skipOffstage: false), findsOneWidget);
      await nowBack(t);
      expect(liveDraft(t), beforeDraft);
      expect(t.element(find.byType(MapCanvas)), same(map));
      expect(workspace.task, same(task));
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, isFalse);
      expect(t.testTextInput.isVisible, isFalse);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }

  testWidgets(
    'real menu uses available current-draft actions and no old private-record route',
    (t) async {
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      var selected = 0, oldPrivateSelections = 0;
      final sent = <String>[];
      await t.pumpWidget(
        MaterialApp(
          theme: birdtieTheme(Brightness.light),
          home: Scaffold(
            body: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onSearchArea: () {},
              hasSearchArea: false,
              onPrivateMomentImage: () => oldPrivateSelections++,
              onDraftImage: () => selected++,
            ),
          ),
        ),
      );
      await t.enterText(nowField(), '独立 Now 草稿');
      await tapLiveMaterials(t);
      expect(find.text('为私人记录添加图片'), findsNothing);
      expect(find.text('图片'), findsOneWidget);
      expect(find.text('粘贴文字/链接'), findsNothing);
      expect(find.text('会话图片（PNG/JPEG）'), findsNothing);
      expect(find.text('快捷任务'), findsNothing);
      expect(selected, 0);
      await t.tap(find.text('图片'));
      await t.pumpAndSettle();
      expect(selected, 1);
      expect(oldPrivateSelections, 0);
      expect(liveDraft(t), '独立 Now 草稿');
      expect(sent, isEmpty);
      await t.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'explicit native paste city ABA unlocks fresh read and rejects old finally',
    (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final old = Completer<Map<String, dynamic>?>(),
          fresh = Completer<Map<String, dynamic>?>();
      var reads = 0;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, (call) async {
            if (call.method != 'Clipboard.getData') return null;
            reads++;
            return reads == 1 ? old.future : fresh.future;
          });
      await mountLiveMaterials(t, f);
      await t.enterText(nowField(), '保留普通未发送草稿');
      await pasteLiveMaterials(t);
      expect(reads, 1);
      f.city.selectCity('beta');
      f.city.selectCity('alpha');
      await t.pumpAndSettle();
      expect(liveDraft(t), '保留普通未发送草稿');
      await pasteLiveMaterials(t);
      expect(reads, 2);
      old.completeError(PlatformException(code: 'old-denied'));
      await t.pumpAndSettle();
      expect(find.textContaining('暂时无法读取剪贴板'), findsNothing);
      await pasteLiveMaterials(t);
      expect(
        reads,
        2,
        reason: 'A retired finally cannot unlock an in-flight fresh read.',
      );
      fresh.complete({'text': '当前范围新文字'});
      await t.pumpAndSettle();
      expect(liveDraft(t), '保留普通未发送草稿\n当前范围新文字');
      expect(f.source.queries, isEmpty);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    },
  );

  testWidgets(
    'explicit send retires pending native paste and permits fresh read',
    (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final old = Completer<Map<String, dynamic>?>();
      var reads = 0;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, (call) async {
            if (call.method != 'Clipboard.getData') return null;
            reads++;
            return reads == 1 ? old.future : {'text': '下一条材料'};
          });
      await mountLiveMaterials(t, f, scale: 1);
      await t.enterText(nowField(), '明确发送已有文字');
      await pasteLiveMaterials(t);
      await t.tap(find.byTooltip('发送需求'));
      await t.pumpAndSettle();
      expect(f.source.queries, ['明确发送已有文字']);
      await pasteLiveMaterials(t);
      expect(reads, 2);
      expect(liveDraft(t), '下一条材料');
      old.complete({'text': '旧剪贴板不得复活'});
      await t.pumpAndSettle();
      expect(liveDraft(t), '下一条材料');
      expect(t.takeException(), isNull);
      await f.unmount(t);
    },
  );

  testWidgets(
    'current menu stays open during explicit native paste without reopening IME',
    (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final pending = Completer<Map<String, dynamic>?>();
      var reads = 0;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, (call) async {
            if (call.method != 'Clipboard.getData') return null;
            reads++;
            return pending.future;
          });
      await mountLiveMaterials(t, f);
      await t.enterText(nowField(), '原中文草稿');
      await pasteLiveMaterials(t);
      await tapLiveMaterials(t);
      expect(find.text('粘贴文字/链接'), findsNothing);
      await pasteLiveMaterials(t);
      expect(reads, 1);
      pending.complete({'text': '本人明确粘贴的短文字'});
      await t.pumpAndSettle();
      expect(find.text('添加材料'), findsOneWidget);
      final title = t.widget<Text>(find.text('添加材料'));
      expect(title.style!.fontSize, 18);
      final copy = t.widget<Text>(find.text('创建草稿'));
      expect(copy.style!.fontSize, 16);
      expect(copy.style!.fontWeight, FontWeight.w500);
      expect(copy.style!.height, 1.5);
      final action = t.widget<Text>(find.text('动态'));
      expect(action.style!.fontSize, 14);
      expect(action.style!.fontWeight, FontWeight.w500);
      expect(t.testTextInput.isVisible, isFalse);
      await nowBack(t);
      expect(liveDraft(t), '原中文草稿\n本人明确粘贴的短文字');
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, isFalse);
      expect(t.testTextInput.isVisible, isFalse);
      expect(f.source.queries, isEmpty);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    },
  );

  testWidgets(
    'retiring a request in the same conversation preserves valid human paste',
    (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final pending = Completer<Map<String, dynamic>?>();
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            SystemChannels.platform,
            (call) async =>
                call.method == 'Clipboard.getData' ? pending.future : null,
          );
      await mountLiveMaterials(t, f, scale: 1);
      await t.enterText(nowField(), '本人普通未发送文字');
      await pasteLiveMaterials(t);
      final workspace = f.workspace(t);
      final conversationBoundary = workspace.taskIdentityEpoch;
      workspace.retirePendingQueryForViewChange();
      await t.pumpAndSettle();
      expect(workspace.taskIdentityEpoch, conversationBoundary);
      pending.complete({'text': '仍属于当前对话的材料'});
      await t.pumpAndSettle();
      expect(liveDraft(t), '本人普通未发送文字\n仍属于当前对话的材料');
      expect(f.source.queries, isEmpty);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    },
  );
}
