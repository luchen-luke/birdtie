import 'dart:async';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'chat_entity_router_test.dart' show ChatTestAuth;
import 'package:birdtie_client/src/app/birdtie_app.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/entity_peek_card.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/active_social_intent_card.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:birdtie_client/src/workspace/social_intent_drafts.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Future<void> _openMoreTools(WidgetTester tester) async {
  await tester.tap(find.byTooltip('打开侧边栏'));
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(const Key('sidebar-account')));
  await tester.pumpAndSettle();
  await tester.tap(find.text('更多工具'));
  await tester.pumpAndSettle();
}

// The retained material helper has no Now attachment entry. This tests its
// legacy draft contract directly, without treating it as a connected UI action.
Future<void> _openLegacyMaterialTools(WidgetTester tester) async {
  final state = tester.state<AgentComposerState>(find.byType(AgentComposer));
  unawaited(state.showMaterialTools(state.context));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('真实Now联系留言320大字IME下检查与取消可达，零提交', (t) async {
    t.view.physicalSize = const Size(320, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    addTearDown(t.view.resetViewInsets);
    final auth = ChatTestAuth(), city = _ContactCity();
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      return http.Response('{"data":[]}', 200);
    });
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    final source = ConnectionSource(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    await t.pumpWidget(
      MaterialApp(
        builder: (c, w) => MediaQuery(
          data: MediaQuery.of(c).copyWith(textScaler: TextScaler.linear(3)),
          child: w!,
        ),
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          connectionSource: source,
        ),
      ),
    );
    await t.pumpAndSettle();
    final workspace = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
    workspace.task = const AgentTask(
      id: 'local-fixture',
      query: '联系人',
      status: 'COMPLETED',
    );
    workspace.result = const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '本人明确公开候选',
      people: [
        AgentPerson(
          accountID: '22222222-2222-4222-8222-222222222222',
          displayName: '合成伙伴',
          topic: '羽毛球',
          areaLabel: '大致区域',
        ),
      ],
    );
    workspace.setSheetExtent(AgentSheetExtent.expanded);
    await t.pumpAndSettle();
    // Invoke the actual result-sheet contact callback; the large-font sheet's
    // separate scroll mechanics are covered elsewhere, this isolates its real dialog.
    t.widget<AgentResultsSheet>(find.byType(AgentResultsSheet)).onContact!(
      workspace.result!.people.single,
    );
    await t.pumpAndSettle();
    t.view.viewInsets = const FakeViewPadding(bottom: 260);
    await t.pumpAndSettle();
    expect(t.takeException(), isNull);
    final cancel = find.widgetWithText(TextButton, '取消');
    final check = find.widgetWithText(FilledButton, '检查申请');
    expect(cancel.hitTestable(), findsOneWidget);
    expect(check.hitTestable(), findsOneWidget);
    expect(t.getSize(cancel).height, greaterThanOrEqualTo(48));
    expect(t.getSize(check).height, greaterThanOrEqualTo(48));
    await t.tap(cancel);
    await t.pumpAndSettle();
    expect(writes, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    city.dispose();
    moments.dispose();
    source.dispose();
    client.close();
  });
  testWidgets('工具入口亮暗主题语义前景清晰且48dp，不替换MapCanvas', (t) async {
    await t.pumpWidget(const BirdtieApp());
    final map = find.byType(MapCanvas).evaluate().single;
    for (final brightness in [Brightness.light, Brightness.dark]) {
      t.platformDispatcher.platformBrightnessTestValue = brightness;
      await t.pumpAndSettle();
      await _openMoreTools(t);
      await t.pumpAndSettle();
      expect(find.byTooltip('选择查询情境'), findsNothing);
      for (final title in ['我的社交意图', '地图图层']) {
        final finder = find.byTooltip(title);
        expect(finder, findsOneWidget);
        expect(t.getSize(finder).height, greaterThanOrEqualTo(48));
        expect(t.getSize(finder).width, greaterThanOrEqualTo(48));
        final button = t.widget<Icon>(
          find.descendant(of: finder, matching: find.byType(Icon)).first,
        );
        final scheme = Theme.of(t.element(finder)).colorScheme;
        expect(button.color, scheme.onSurface);
        final lum = [
          scheme.onSurface.computeLuminance(),
          scheme.surface.computeLuminance(),
        ]..sort();
        expect((lum[1] + 0.05) / (lum[0] + 0.05), greaterThanOrEqualTo(4.5));
      }
      await t.tap(find.byTooltip('关闭更多工具'));
      await t.pumpAndSettle();
      expect(identical(find.byType(MapCanvas).evaluate().single, map), isTrue);
    }
    t.platformDispatcher.clearPlatformBrightnessTestValue();
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('匿名窄屏意图入口有中文说明及48dp关闭且不退出Now', (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(const BirdtieApp());
    final canvas = find.byType(MapCanvas).evaluate().single;
    await _openMoreTools(tester);
    await tester.pumpAndSettle();
    final entry = find.byTooltip('我的社交意图');
    expect(tester.getSize(entry).height, greaterThanOrEqualTo(48));
    await tester.tap(entry);
    await tester.pumpAndSettle();
    final modalCard = find.byType(ActiveSocialIntentCard).last;
    expect(
      find.descendant(of: modalCard, matching: find.text('登录后可查看和管理自己的社交意图。')),
      findsOneWidget,
    );
    final close = find.descendant(of: modalCard, matching: find.text('关闭'));
    expect(close, findsOneWidget);
    final button = find.ancestor(of: close, matching: find.byType(TextButton));
    expect(tester.getSize(button).height, greaterThanOrEqualTo(48));
    await tester.tap(close);
    await tester.pumpAndSettle();
    expect(find.byType(ActiveSocialIntentCard), findsNothing);
    expect(find.text('登录后可查看和管理自己的社交意图。'), findsNothing);
    expect(find.text('关闭'), findsNothing);
    expect(identical(find.byType(MapCanvas).evaluate().single, canvas), isTrue);
    expect(find.byTooltip('打开侧边栏').hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('匿名私人意图不常驻首屏，真实独立入口不替换地图或任务', (tester) async {
    await tester.pumpWidget(const BirdtieApp());
    final canvas = find.byType(MapCanvas).evaluate().single;
    expect(find.byType(ActiveSocialIntentCard), findsNothing);
    await _openMoreTools(tester);
    await tester.pumpAndSettle();
    final entry = find.byTooltip('我的社交意图');
    expect(tester.getSize(entry).height, greaterThanOrEqualTo(48));
    await tester.tap(entry);
    await tester.pumpAndSettle();
    expect(find.byType(ActiveSocialIntentCard), findsOneWidget);
    expect(identical(find.byType(MapCanvas).evaluate().single, canvas), isTrue);
    Navigator.of(
      tester.element(find.byType(ActiveSocialIntentCard).last),
    ).pop();
    await tester.pumpAndSettle();
    expect(find.byType(ActiveSocialIntentCard), findsNothing);
    expect(identical(find.byType(MapCanvas).evaluate().single, canvas), isTrue);
    expect(tester.takeException(), isNull);
  });
  testWidgets('Now provides an explicit online query context without a city', (
    tester,
  ) async {
    await tester.pumpWidget(const BirdtieApp());
    await tester.tap(find.byKey(const Key('now-city-picker')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('now-scope-picker')), findsOneWidget);
    expect(find.text('选择城市与范围'), findsOneWidget);
    expect(find.text('登录后可选择本人声明的线上范围。'), findsOneWidget);
    expect(find.text('选择查询情境'), findsNothing);
    expect(find.text('选择查看情境'), findsNothing);
    expect(find.text('当前城市'), findsNothing);
    final online = find.byKey(const Key('now-scope-online'));
    await tester.ensureVisible(online);
    await tester.pumpAndSettle();
    expect(online.hitTestable(), findsOneWidget);
  });
  testWidgets('Area Pulse keeps category labels Chinese for seeded data', (
    tester,
  ) async {
    const bounds = MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.2);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AreaPulseStack(
            pulse: const NowPulse(
              cityID: 'aberdeen-gb',
              bounds: bounds,
              status: 'populated',
              total: 5,
              categories: [
                PulseCategory(code: 'social', count: 2),
                PulseCategory(code: 'new_category', count: 3),
              ],
              activities: [],
              truncated: false,
            ),
            bounds: bounds,
            loading: false,
            onSearch: (_) {},
          ),
        ),
      ),
    );
    expect(find.text('社交 · 2'), findsOneWidget);
    expect(find.text('其他活动 · 3'), findsOneWidget);
    expect(find.textContaining('new_category'), findsNothing);
  });

  testWidgets('twenty typing and focus cycles keep the same map canvas', (
    tester,
  ) async {
    await tester.pumpWidget(const BirdtieApp());
    final originalWidget = tester.widget<MapCanvas>(find.byType(MapCanvas));
    final originalElement = find.byType(MapCanvas).evaluate().single;
    for (var index = 0; index < 20; index++) {
      await tester.enterText(find.byType(TextField).first, '活动 ${index + 1}');
      await tester.pump();
      FocusManager.instance.primaryFocus?.unfocus();
      await tester.pump();
      expect(
        identical(
          tester.widget<MapCanvas>(find.byType(MapCanvas)),
          originalWidget,
        ),
        isTrue,
      );
      expect(
        identical(find.byType(MapCanvas).evaluate().single, originalElement),
        isTrue,
      );
    }
  });

  testWidgets('线上意图入口保留地图实例和当前 Agent 工作区', (tester) async {
    await tester.pumpWidget(const BirdtieApp());
    final originalMap = find.byType(MapCanvas).evaluate().single;
    expect(find.text('当前：未选城市'), findsOneWidget);
    await _openMoreTools(tester);
    await tester.pumpAndSettle();
    expect(find.byTooltip('打开意图草稿').hitTestable(), findsOneWidget);
    await tester.tap(find.byTooltip('打开意图草稿'));
    await tester.pumpAndSettle();
    expect(
      find.descendant(
        of: find.byType(SocialIntentDraftPage),
        matching: find.text('我的社交意图'),
      ),
      findsOneWidget,
    );
    final mode = find.byType(DropdownButtonFormField<String>);
    expect(
      tester.widget<DropdownButtonFormField<String>>(mode).initialValue,
      isNull,
      reason: '入口不代替用户批准参与方式',
    );
    await tester.tap(mode);
    await tester.pumpAndSettle();
    await tester.tap(find.text('线上').hitTestable());
    await tester.pumpAndSettle();
    expect(
      tester.widget<DropdownButtonFormField<String>>(mode).initialValue,
      'ONLINE',
    );
    expect(find.text('线上'), findsOneWidget);
    expect(find.text('大致区域'), findsNothing);
    expect(
      identical(
        find.byType(MapCanvas, skipOffstage: false).evaluate().single,
        originalMap,
      ),
      isTrue,
    );
    await tester.tap(find.byTooltip('返回本地地图'));
    await tester.pumpAndSettle();
    expect(find.text('你想做什么？'), findsOneWidget);
    expect(
      identical(find.byType(MapCanvas).evaluate().single, originalMap),
      isTrue,
    );
  });

  testWidgets('nearby overlay only intercepts its actual content height', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1220, 2656);
    tester.view.devicePixelRatio = 3.25;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(const BirdtieApp());
    await tester.pumpAndSettle();
    final scroll = find.byKey(const Key('now-native-context-scroll'));
    expect(scroll, findsOneWidget);
    final content = find
        .descendant(of: scroll, matching: find.byType(Column))
        .first;
    final viewport = tester.getRect(scroll);
    final body = tester.getRect(content);
    expect(viewport.height, closeTo(body.height, 1));
    expect(viewport.bottom, lessThan(400));
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'legacy material helper preserves quick-action drafts; Now exposes no attachment or fake mic',
    (tester) async {
      await tester.pumpWidget(const BirdtieApp());
      expect(
        tester.widget<MaterialApp>(find.byType(MaterialApp)).locale,
        const Locale('zh', 'CN'),
      );
      expect(find.byTooltip('打开快捷操作'), findsNothing);
      expect(find.byIcon(Icons.add_rounded), findsNothing);
      await _openLegacyMaterialTools(tester);
      await tester.pumpAndSettle();
      expect(find.text('找活动'), findsOneWidget);
      expect(find.text('找组织'), findsOneWidget);
      expect(find.text('找地点'), findsOneWidget);
      await tester.tap(find.text('找活动'));
      await tester.pumpAndSettle();
      expect(find.byType(TextField).first, findsOneWidget);
      expect(
        (tester.widget<TextField>(find.byType(TextField).first).controller!)
            .text,
        isEmpty,
      );
      expect(find.byIcon(Icons.mic_none_rounded), findsNothing);
      await _openLegacyMaterialTools(tester);
      await tester.pumpAndSettle();
      await tester.tap(find.text('找组织'));
      await tester.pumpAndSettle();
      expect(
        (tester.widget<TextField>(find.byType(TextField).first).controller!)
            .text,
        '找组织',
      );
    },
  );

  testWidgets(
    'map workspace replaces bottom navigation and accepts an intent',
    (tester) async {
      await tester.pumpWidget(const BirdtieApp());
      expect(find.byType(NavigationBar), findsNothing);
      expect(find.byTooltip('打开侧边栏'), findsOneWidget);
      expect(find.byTooltip('打开收件箱'), findsOneWidget);
      expect(find.text('你想做什么？'), findsOneWidget);
      expect(
        tester.widget<MapCanvas>(find.byType(MapCanvas)).city.selectedCity,
        isNull,
      );
      expect(find.byType(AreaPulseStack), findsNothing);
      expect(find.text('选择城市'), findsNothing);
      expect(
        find.byKey(const Key('now-city-picker')).hitTestable(),
        findsOneWidget,
      );

      await tester.enterText(
        find.byType(TextField).first,
        'Find someone to play badminton this weekend',
      );
      await tester.testTextInput.receiveAction(TextInputAction.send);
      await tester.pump();
      expect(find.byType(AgentResultsSheet), findsOneWidget);
      expect(find.byType(AreaPulseStack), findsNothing);
      await tester.pump(const Duration(milliseconds: 700));
      await tester.pumpAndSettle();
      expect(find.text('尚未搜索 · 需要选择城市'), findsOneWidget);
      expect(
        find.byKey(const Key('agent-sheet-choose-city')).hitTestable(),
        findsOneWidget,
      );
      expect(find.textContaining('0 个'), findsNothing);
      expect(find.text('未找到符合条件的公开内容'), findsNothing);
      final canvas = find.byType(MapCanvas).evaluate().single;
      final workspace = tester
          .widget<MapCanvas>(find.byType(MapCanvas))
          .workspace;
      final task = workspace.task, result = workspace.result;
      expect(workspace.sheetExtent, AgentSheetExtent.peek);
      expect(find.byType(AgentConversation), findsNothing);
      await tester.tap(find.byKey(const Key('agent-sheet-map-toggle')));
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      expect(find.byType(AgentConversation), findsOneWidget);

      await tester.tap(find.byTooltip('打开侧边栏'));
      await tester.pumpAndSettle();
      expect(find.text('最近对话'), findsOneWidget);
      expect(
        find.byKey(const Key('sidebar-new-conversation')).hitTestable(),
        findsOneWidget,
      );
      await tester.tap(find.byKey(const Key('sidebar-account')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('登录 / 账户'));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(BackButton));
      await tester.pumpAndSettle();
      expect(find.byType(AgentResultsSheet), findsOneWidget);
      expect(
        identical(find.byType(MapCanvas).evaluate().single, canvas),
        isTrue,
      );
      expect(identical(workspace.task, task), isTrue);
      expect(identical(workspace.result, result), isTrue);
      expect(find.byType(AgentConversation), findsOneWidget);
      expect(find.byTooltip('查看结果'), findsNothing);
      await tester.tap(find.byKey(const Key('agent-sheet-map-toggle')));
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.peek);
      expect(identical(workspace.task, task), isTrue);
      expect(identical(workspace.result, result), isTrue);
      expect(
        identical(find.byType(MapCanvas).evaluate().single, canvas),
        isTrue,
      );
      await tester.tap(find.byKey(const Key('agent-sheet-map-toggle')));
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      expect(find.byType(AgentConversation), findsOneWidget);
      expect(identical(workspace.task, task), isTrue);
      expect(identical(workspace.result, result), isTrue);
      expect(
        find.text('Find someone to play badminton this weekend'),
        findsWidgets,
      );

      await tester.tap(find.byTooltip('打开侧边栏'));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('sidebar-new-conversation')).hitTestable(),
        findsOneWidget,
      );
      await tester.tap(find.byKey(const Key('sidebar-new-conversation')));
      await tester.pumpAndSettle();
      expect(find.byType(AgentResultsSheet), findsNothing);
      expect(
        tester.widget<MapCanvas>(find.byType(MapCanvas)).city.selectedCity,
        isNull,
      );
      expect(find.byType(AreaPulseStack), findsNothing);
      expect(find.text('选择城市'), findsNothing);
      expect(
        find.byKey(const Key('now-city-picker')).hitTestable(),
        findsOneWidget,
      );

      await tester.tap(find.byTooltip('打开收件箱'));
      await tester.pumpAndSettle();
      expect(find.text('收件箱'), findsOneWidget);
      expect(find.textContaining('收件箱暂不可用'), findsOneWidget);
      expect(find.textContaining('Anna'), findsNothing);
    },
  );
  testWidgets(
    'IME inset is consumed once and restores same map task result selection',
    (tester) async {
      tester.view.devicePixelRatio = 1;
      tester.view.physicalSize = const Size(375.4, 640);
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.view.resetViewInsets);
      await tester.pumpWidget(const BirdtieApp());
      final mapFinder = find.byType(MapCanvas);
      final mapElement = mapFinder.evaluate().single;
      final map = tester.widget<MapCanvas>(mapFinder);
      final workspace = map.workspace;
      workspace.task = const AgentTask(
        id: 'current-keyboard-task',
        query: '羽毛球活动',
        status: 'COMPLETED',
      );
      workspace.result = const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '当前本地合成任务的已有结果',
      );
      workspace.selectEntity('place:stable-selected');
      workspace.setSheetExtent(AgentSheetExtent.medium);
      final task = workspace.task, result = workspace.result;
      await tester.pumpAndSettle();
      final header = tester.getRect(find.byType(TopControls));
      for (final inset in [
        0.0,
        260.0,
        450.0,
        472.0,
        540.0,
        610.0,
        700.0,
        260.0,
        0.0,
      ]) {
        tester.view.viewInsets = FakeViewPadding(bottom: inset);
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull);
        final sheet = tester.widget<AgentResultsSheet>(
          find.byType(AgentResultsSheet),
        );
        final composer = tester.getRect(find.byType(AgentComposer));
        final ornamentEnd = header.bottom + 8 + 48 + 8;
        expect(
          sheet.availableHeight,
          closeTo((composer.top - 12 - ornamentEnd).clamp(0.0, 640.0), .5),
        );
        expect(
          composer.bottom,
          lessThanOrEqualTo((640 - inset).clamp(0.0, 640.0)),
        );
        expect(identical(mapFinder.evaluate().single, mapElement), isTrue);
        expect(identical(tester.widget<MapCanvas>(mapFinder), map), isTrue);
        expect(identical(workspace.task, task), isTrue);
        expect(identical(workspace.result, result), isTrue);
        expect(workspace.selectedEntityId, 'place:stable-selected');
        if (inset == 472 || inset == 540) {
          final restore = find.byTooltip('收起键盘查看结果');
          expect(restore.hitTestable(), findsOneWidget);
          expect(tester.getSize(restore).height, greaterThanOrEqualTo(48));
          expect(
            tester.getBottomRight(restore).dy,
            lessThanOrEqualTo(640 - inset),
          );
          await tester.tap(restore);
          await tester.pumpAndSettle();
          expect(workspace.sheetExtent, AgentSheetExtent.medium);
        }
        if (640 - inset < 48) expect(find.byTooltip('收起键盘查看结果'), findsNothing);
      }
      expect(find.byTooltip('打开侧边栏'), findsOneWidget);
      expect(
        find.byKey(const Key('agent-sheet-handle')).hitTestable(),
        findsOneWidget,
      );
    },
  );
  testWidgets('已明确选择城市而API未配置保留真正错误与单一恢复，不伪装缺范围或空结果', (t) async {
    final city = _ContactCity(), auth = ChatTestAuth();
    var calls = 0;
    final client = MockClient((r) async {
      return http.Response('{"data":[]}', 200);
    });
    final queryClient = MockClient((r) async {
      calls++;
      return http.Response('{"data":[]}', 200);
    });
    final moments = PrivateMomentController(
      client: client,
      apiBaseUrl: '',
      authorizationHeader: () => auth.authorizationHeader,
    );
    final source = RemoteAgentTaskSource(
      cityID: () => city.selectedCity?.id,
      authorizationHeader: () => auth.authorizationHeader,
      client: queryClient,
      apiBaseUrl: '',
    );
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          agentTaskSource: source,
          seedClient: client,
          seedApiBaseUrl: '',
        ),
      ),
    );
    await t.pumpAndSettle();
    final element = find.byType(MapCanvas).evaluate().single;
    await t.enterText(find.byType(TextField).first, '给定范围的地点');
    await t.testTextInput.receiveAction(TextInputAction.send);
    await t.pumpAndSettle();
    final ws = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
    expect(ws.queryState, AgentQueryState.error);
    expect(ws.requestError, contains('尚未连接 Birdtie 服务'));
    expect(find.text('查询未完成'), findsOneWidget);
    expect(find.byKey(const Key('agent-sheet-retry')), findsOneWidget);
    expect(find.byKey(const Key('agent-sheet-choose-city')), findsNothing);
    expect(find.textContaining('0 个'), findsNothing);
    expect(identical(element, find.byType(MapCanvas).evaluate().single), true);
    expect(calls, 0);
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    city.dispose();
    auth.dispose();
    client.close();
    queryClient.close();
  });
}

class _ContactCity extends PublicCityController {
  @override
  PublicCity? get selectedCity => const PublicCity(
    id: 'aberdeen-gb',
    name: '合成城市',
    region: '测试',
    contentStatus: 'building',
    source: PublicSource(
      label: 'fixture',
      maintainer: 'test',
      freshness: 'test',
      updatedAt: null,
    ),
    map: null,
  );
}
