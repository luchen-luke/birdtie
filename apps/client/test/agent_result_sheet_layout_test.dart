import 'dart:ui' show SemanticsAction;
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const longError =
    '暂时无法读取当前活动，请检查网络后重试；原来的结果和已选择的地点仍然保留。'
    '如果服务尚未恢复，可以先返回地图，并在连接恢复后继续本次任务。';
AgentWorkspaceController layoutWorkspace(
  AgentSheetExtent extent, {
  bool error = false,
}) => AgentWorkspaceController()
  ..task = const AgentTask(
    id: 'layout-current-task',
    query: '周末一起打羽毛球',
    intent: 'PERSONAL_RELATIONSHIP_CONTEXT',
    status: 'COMPLETED',
  )
  ..result = const AgentResult(
    entities: [],
    activities: [],
    places: [],
    note: '仅使用当前任务已有的可见结果，不读取新的私密资料。',
  )
  ..requestError = error ? longError : null
  ..selectEntity('place:stable-selected')
  ..setSheetExtent(extent);
Future<void> mountSheet(
  WidgetTester tester,
  AgentWorkspaceController workspace, {
  double width = 320,
  double height = 104,
  double scale = 3,
  double? available,
}) async {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = Size(width, 800);
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      home: MediaQuery(
        data: MediaQueryData(
          size: Size(width, 800),
          textScaler: TextScaler.linear(scale),
        ),
        child: Scaffold(
          body: Align(
            alignment: Alignment.bottomCenter,
            child: SizedBox(
              width: width,
              height: height,
              child: AgentResultsSheet(
                workspace: workspace,
                availableHeight: available ?? height,
                onSuggestion: (_) {},
                onRetry: () {},
              ),
            ),
          ),
        ),
      ),
    ),
  );
}

void main() {
  for (final extent in [
    AgentSheetExtent.peek,
    AgentSheetExtent.medium,
    AgentSheetExtent.expanded,
  ]) {
    testWidgets('面板模式语义 对话${extent.name}不朗读为结果模式', (t) async {
      final semantics = t.ensureSemantics();
      try {
        final w = layoutWorkspace(extent)
          ..contentMode = AgentContentMode.conversation;
        addTearDown(w.dispose);
        final task = w.task, result = w.result;
        await mountSheet(t, w, height: 500, scale: 1);
        await t.pumpAndSettle();
        final handle = find.byKey(const Key('agent-sheet-handle'));
        final suffix = extent == AgentSheetExtent.peek
            ? '已收起'
            : extent == AgentSheetExtent.medium
            ? '处于中间高度'
            : '已展开';
        expect(
          t.getSemantics(handle),
          matchesSemantics(
            label: '对话面板$suffix',
            hint: '点击切换面板高度，上滑展开，下滑收起',
            isButton: true,
            hasTapAction: true,
            hasScrollUpAction: true,
            hasScrollDownAction: true,
          ),
        );
        expect(handle.hitTestable(), findsOneWidget);
        expect(t.getSize(handle).height, greaterThanOrEqualTo(48));
        expect(w.contentMode, AgentContentMode.conversation);
        expect(w.sheetExtent, extent);
        expect(w.task, same(task));
        expect(w.result, same(result));
        expect(w.selectedEntityId, 'place:stable-selected');
        expect(t.takeException(), isNull);
      } finally {
        semantics.dispose();
      }
    });
  }
  for (final phase in ['ready', 'error', 'searching']) {
    testWidgets('面板模式语义 结果中间高度$phase不宣告查询成功', (t) async {
      final semantics = t.ensureSemantics();
      try {
        final w = layoutWorkspace(
          AgentSheetExtent.medium,
          error: phase == 'error',
        );
        if (phase == 'searching') w.state = AgentViewState.searching;
        addTearDown(w.dispose);
        final task = w.task, result = w.result, error = w.requestError;
        await mountSheet(t, w, height: 500, scale: 1);
        if (phase == 'searching') {
          await t.pump(const Duration(milliseconds: 260));
        } else {
          await t.pumpAndSettle();
        }
        final handle = find.byKey(const Key('agent-sheet-handle'));
        expect(
          t.getSemantics(handle),
          matchesSemantics(
            label: '结果面板处于中间高度',
            hint: '点击切换面板高度，上滑展开，下滑收起',
            isButton: true,
            hasTapAction: true,
            hasScrollUpAction: true,
            hasScrollDownAction: true,
          ),
        );
        expect(
          w.queryState,
          phase == 'searching'
              ? AgentQueryState.loading
              : phase == 'error'
              ? AgentQueryState.error
              : AgentQueryState.success,
        );
        expect(handle.hitTestable(), findsOneWidget);
        expect(t.getSize(handle).height, greaterThanOrEqualTo(48));
        expect(w.task, same(task));
        expect(w.result, same(result));
        expect(w.requestError, error);
        expect(w.contentMode, AgentContentMode.results);
        expect(w.sheetExtent, AgentSheetExtent.medium);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox.shrink());
      } finally {
        semantics.dispose();
      }
    });
  }
  for (final extent in [AgentSheetExtent.peek, AgentSheetExtent.expanded]) {
    testWidgets('面板模式语义 结果${extent.name}保留原正确中文标签', (t) async {
      final semantics = t.ensureSemantics();
      try {
        final w = layoutWorkspace(extent);
        addTearDown(w.dispose);
        await mountSheet(t, w, height: 500, scale: 1);
        await t.pumpAndSettle();
        final handle = find.byKey(const Key('agent-sheet-handle'));
        expect(
          t.getSemantics(handle),
          matchesSemantics(
            label: extent == AgentSheetExtent.peek ? '结果面板已收起' : '结果面板已展开',
            hint: '点击切换面板高度，上滑展开，下滑收起',
            isButton: true,
            hasTapAction: true,
            hasScrollUpAction: true,
            hasScrollDownAction: true,
          ),
        );
        expect(t.getSize(handle).height, greaterThanOrEqualTo(48));
        expect(t.takeException(), isNull);
      } finally {
        semantics.dispose();
      }
    });
  }
  for (final mode in AgentContentMode.values) {
    testWidgets('面板模式语义 ${mode.name}隐藏或不足48高度无隐形句柄', (t) async {
      final semantics = t.ensureSemantics();
      try {
        final w = layoutWorkspace(AgentSheetExtent.hidden)..contentMode = mode;
        addTearDown(w.dispose);
        final task = w.task, result = w.result;
        for (final height in [500.0, 0.0, 20.0, 47.0]) {
          w.setSheetExtent(
            height == 500 ? AgentSheetExtent.hidden : AgentSheetExtent.expanded,
          );
          await mountSheet(t, w, height: height, scale: 1);
          await t.pumpAndSettle();
          expect(find.byKey(const Key('agent-sheet-handle')), findsNothing);
          expect(find.bySemanticsLabel(RegExp('^(对话|结果)面板')), findsNothing);
          expect(find.byType(TextButton), findsNothing);
          expect(w.contentMode, mode);
          expect(w.task, same(task));
          expect(w.result, same(result));
          expect(t.takeException(), isNull);
        }
      } finally {
        semantics.dispose();
      }
    });
  }
  testWidgets('面板模式语义 地图对话切换及句柄语义动作只更改高度', (t) async {
    final semantics = t.ensureSemantics();
    try {
      final w = layoutWorkspace(AgentSheetExtent.medium)
        ..contentMode = AgentContentMode.conversation;
      addTearDown(w.dispose);
      final task = w.task, result = w.result;
      await mountSheet(t, w, height: 500, scale: 1);
      await t.pumpAndSettle();
      final mode = find.byKey(const Key('agent-sheet-map-toggle'));
      expect(mode.hitTestable(), findsOneWidget);
      await t.tap(mode);
      await t.pumpAndSettle();
      expect(w.sheetExtent, AgentSheetExtent.peek);
      expect(w.contentMode, AgentContentMode.conversation);
      expect(find.byTooltip('展开对话').hitTestable(), findsOneWidget);
      await t.tap(mode);
      await t.pumpAndSettle();
      expect(w.contentMode, AgentContentMode.conversation);
      expect(w.sheetExtent, AgentSheetExtent.expanded);
      final handle = find.byKey(const Key('agent-sheet-handle'));
      expect(t.getSemantics(handle).label, '对话面板已展开');
      final owner = t.binding.renderViews.single.owner!.semanticsOwner!;
      owner.performAction(
        t.getSemantics(handle).id,
        SemanticsAction.scrollDown,
      );
      await t.pumpAndSettle();
      expect(w.sheetExtent, AgentSheetExtent.medium);
      expect(t.getSemantics(handle).label, '对话面板处于中间高度');
      owner.performAction(
        t.getSemantics(handle).id,
        SemanticsAction.scrollDown,
      );
      await t.pumpAndSettle();
      expect(w.sheetExtent, AgentSheetExtent.peek);
      expect(t.getSemantics(handle).label, '对话面板已收起');
      owner.performAction(t.getSemantics(handle).id, SemanticsAction.scrollUp);
      await t.pumpAndSettle();
      expect(w.sheetExtent, AgentSheetExtent.medium);
      owner.performAction(t.getSemantics(handle).id, SemanticsAction.tap);
      await t.pumpAndSettle();
      expect(w.sheetExtent, AgentSheetExtent.peek);
      expect(w.contentMode, AgentContentMode.conversation);
      expect(w.task, same(task));
      expect(w.result, same(result));
      expect(w.selectedEntityId, 'place:stable-selected');
      // Map and conversation use the same stream. Returning from the map does
      // not select a separate result body or replace the task selection.
      expect(mode.hitTestable(), findsOneWidget);
      await t.tap(mode);
      await t.pumpAndSettle();
      expect(w.contentMode, AgentContentMode.conversation);
      expect(w.sheetExtent, AgentSheetExtent.expanded);
      expect(find.byType(AgentConversation), findsOneWidget);
      expect(t.getSemantics(handle).label, '对话面板已展开');
      expect(w.task, same(task));
      expect(w.result, same(result));
      expect(w.selectedEntityId, 'place:stable-selected');
      expect(t.takeException(), isNull);
    } finally {
      semantics.dispose();
    }
  });
  testWidgets(
    'historical constrained 375.4 by 104 peek supports enlarged text',
    (tester) async {
      final workspace = layoutWorkspace(AgentSheetExtent.peek);
      addTearDown(workspace.dispose);
      await mountSheet(
        tester,
        workspace,
        width: 375.4,
        height: 104,
        scale: 1.8,
        available: 700,
      );
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(
        tester.getSize(find.byKey(const Key('agent-sheet-handle'))).height,
        greaterThanOrEqualTo(48),
      );
    },
  );
  for (final height in [90.0, 104.0, 180.0]) {
    for (final extent in [
      AgentSheetExtent.peek,
      AgentSheetExtent.medium,
      AgentSheetExtent.expanded,
    ]) {
      testWidgets('320 width font3 bounded $height $extent and long error', (
        tester,
      ) async {
        final workspace = layoutWorkspace(extent, error: true);
        addTearDown(workspace.dispose);
        final task = workspace.task, result = workspace.result;
        await mountSheet(tester, workspace, height: height);
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull);
        expect(
          tester.getSize(find.byType(AgentResultsSheet)).height,
          lessThanOrEqualTo(height),
        );
        expect(
          tester.getSize(find.byKey(const Key('agent-sheet-handle'))).height,
          greaterThanOrEqualTo(48),
        );
        expect(identical(workspace.task, task), isTrue);
        expect(identical(workspace.result, result), isTrue);
        expect(workspace.selectedEntityId, 'place:stable-selected');
      });
    }
  }
  testWidgets(
    'font3 accurate query error and long error are scroll reachable with retry',
    (tester) async {
      final workspace = layoutWorkspace(AgentSheetExtent.medium, error: true);
      addTearDown(workspace.dispose);
      var retries = 0;
      await tester.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(textScaler: TextScaler.linear(3)),
            child: Scaffold(
              body: Align(
                alignment: Alignment.bottomCenter,
                child: SizedBox(
                  width: 320,
                  height: 180,
                  child: AgentResultsSheet(
                    workspace: workspace,
                    availableHeight: 180,
                    onSuggestion: (_) {},
                    onRetry: () => retries++,
                  ),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      final status = tester.widget<Text>(find.text('查询未完成'));
      expect(status.maxLines, isNull);
      expect(find.textContaining('0 个'), findsNothing);
      final scroll = find
          .descendant(
            of: find.byType(AgentResultsSheet),
            matching: find.byType(Scrollable),
          )
          .first;
      final retry = find.byKey(const Key('agent-sheet-retry'));
      for (var i = 0; i < 30 && retry.hitTestable().evaluate().isEmpty; i++) {
        await tester.drag(scroll, const Offset(0, -60));
        await tester.pumpAndSettle();
      }
      expect(retry.hitTestable(), findsOneWidget);
      await tester.tap(retry);
      expect(retries, 1);
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'detent and hide animation has no overflow at zero 100 and 260 ms',
    (tester) async {
      final workspace = layoutWorkspace(AgentSheetExtent.peek, error: true);
      addTearDown(workspace.dispose);
      await mountSheet(tester, workspace, height: 180);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      for (final extent in [
        AgentSheetExtent.expanded,
        AgentSheetExtent.medium,
        AgentSheetExtent.hidden,
        AgentSheetExtent.peek,
      ]) {
        workspace.setSheetExtent(extent);
        await tester.pump();
        expect(tester.takeException(), isNull);
        await tester.pump(const Duration(milliseconds: 100));
        expect(tester.takeException(), isNull);
        await tester.pump(const Duration(milliseconds: 160));
        expect(tester.takeException(), isNull);
        if (extent == AgentSheetExtent.hidden) {
          expect(find.byKey(const Key('agent-sheet-handle')), findsNothing);
          expect(find.byKey(const Key('agent-sheet-map-toggle')), findsNothing);
        }
      }
    },
  );
  testWidgets(
    'scroll content does not replace drag handle or detach stable task',
    (tester) async {
      final workspace = layoutWorkspace(AgentSheetExtent.peek);
      addTearDown(workspace.dispose);
      await mountSheet(
        tester,
        workspace,
        height: 500,
        scale: 1,
        available: 700,
      );
      await tester.pumpAndSettle();
      final task = workspace.task;
      final handle = find.byKey(const Key('agent-sheet-handle'));
      await tester.drag(handle, const Offset(0, -80));
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.medium);
      await tester.drag(handle, const Offset(0, -80));
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      await tester.drag(handle, const Offset(0, 80));
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.medium);
      expect(identical(workspace.task, task), isTrue);
      expect(workspace.selectedEntityId, 'place:stable-selected');
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'less than 48 actual parent height leaves no hidden interactive subtree',
    (tester) async {
      final workspace = layoutWorkspace(AgentSheetExtent.expanded);
      addTearDown(workspace.dispose);
      for (final height in [0.0, 20.0, 47.0]) {
        await mountSheet(tester, workspace, height: height, available: 700);
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull);
        expect(find.byKey(const Key('agent-sheet-handle')), findsNothing);
        expect(find.byKey(const Key('agent-sheet-map-toggle')), findsNothing);
        expect(find.byType(TextButton), findsNothing);
        expect(find.byType(IconButton), findsNothing);
      }
    },
  );
  testWidgets('expanded searching is scrollable and wraps at font3', (
    tester,
  ) async {
    final workspace = layoutWorkspace(AgentSheetExtent.expanded)
      ..state = AgentViewState.searching;
    addTearDown(workspace.dispose);
    await mountSheet(tester, workspace, height: 180);
    await tester.pump();
    expect(tester.takeException(), isNull);
    final scroll = find
        .descendant(
          of: find.byType(AgentConversation),
          matching: find.byType(Scrollable),
        )
        .first;
    final searching = find.text('正在查找当前公开信息…');
    for (var i = 0; i < 12 && searching.hitTestable().evaluate().isEmpty; i++) {
      await tester.drag(scroll, const Offset(0, -40));
      await tester.pump(const Duration(milliseconds: 260));
      expect(tester.takeException(), isNull);
    }
    expect(searching.hitTestable(), findsOneWidget);
    expect(find.textContaining('0 个'), findsNothing);
  });
  testWidgets(
    'font3 successful relationship source label stays complete and scroll reachable',
    (tester) async {
      final workspace = layoutWorkspace(AgentSheetExtent.medium);
      addTearDown(workspace.dispose);
      final task = workspace.task, result = workspace.result;
      await mountSheet(tester, workspace, height: 180);
      await tester.pumpAndSettle();
      final source = find.text('本人授权 · 最近 30 天的互动记录');
      final scroll = find
          .descendant(
            of: find.byType(AgentResultsSheet),
            matching: find.byType(Scrollable),
          )
          .first;
      for (var i = 0; i < 30 && source.hitTestable().evaluate().isEmpty; i++) {
        // The unified message stream follows the latest response. Scroll back
        // to the source header instead of targeting the former result body.
        await tester.drag(scroll, const Offset(0, 60));
        await tester.pumpAndSettle();
      }
      expect(source.hitTestable(), findsOneWidget);
      expect(tester.widget<Text>(source).maxLines, isNull);
      expect(identical(workspace.task, task), true);
      expect(identical(workspace.result, result), true);
      expect(workspace.selectedEntityId, 'place:stable-selected');
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'Chinese handle exposes tap semantics at 48dp without text scaling shrink',
    (tester) async {
      final semantics = tester.ensureSemantics();
      final workspace = layoutWorkspace(AgentSheetExtent.peek);
      addTearDown(workspace.dispose);
      await mountSheet(tester, workspace, height: 104, scale: 3);
      await tester.pumpAndSettle();
      final handle = find.byKey(const Key('agent-sheet-handle'));
      expect(tester.getSize(handle).height, greaterThanOrEqualTo(48));
      expect(
        tester.getSemantics(handle),
        matchesSemantics(
          label: '结果面板已收起',
          hint: '点击切换面板高度，上滑展开，下滑收起',
          isButton: true,
          hasTapAction: true,
          hasScrollUpAction: true,
          hasScrollDownAction: true,
        ),
      );
      final title = tester.widget<Text>(find.text('我的关系信号'));
      expect(title.style?.fontSize, 16);
      await tester.tap(handle);
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.medium);
      semantics.dispose();
      expect(tester.takeException(), isNull);
    },
  );
}
