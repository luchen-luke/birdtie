import 'dart:async';
import 'dart:typed_data';
import 'dart:ui' show SemanticsAction;

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart' show SemanticsNode;
import 'package:flutter_test/flutter_test.dart';

class _PendingSource extends AgentTaskSource {
  final requests = <Completer<AgentResult>>[];

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) {
    final pending = Completer<AgentResult>();
    requests.add(pending);
    return pending.future;
  }
}

AgentResult _answer(String message) => AgentResult(
  entities: const [],
  activities: const [],
  places: const [],
  note: message,
);

AgentWorkspaceController _workspace(_PendingSource source) {
  final workspace = AgentWorkspaceController(source: source)
    ..task = const AgentTask(
      id: 'local-existing-conversation',
      query: '周末羽毛球',
      status: 'COMPLETED',
      cityID: 'aberdeen',
    )
    ..result = _answer('上一轮公开查询完成。')
    ..state = AgentViewState.results;
  for (var turn = 0; turn < 7; turn++) {
    workspace.conversation.addAll([
      AgentMessage(role: 'user', text: '历史问题 $turn：查看公开活动'),
      AgentMessage(
        role: 'assistant',
        text: '历史回答 $turn：${'仅列出当前城市中已公开的信息，活动地点和参与条件请查看详情。' * 5}',
      ),
    ]);
  }
  workspace.conversation.add(
    const AgentMessage(role: 'assistant', text: '当前历史末尾：周末活动可继续查询。'),
  );
  workspace.showContent(AgentContentMode.conversation);
  return workspace;
}

Future<void> _mount(
  WidgetTester tester,
  AgentWorkspaceController workspace, {
  double width = 390,
  double textScale = 1,
  double height = 600,
}) async {
  tester.view.physicalSize = Size(width, 800);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      home: MediaQuery(
        data: MediaQueryData(
          size: Size(width, 800),
          textScaler: TextScaler.linear(textScale),
        ),
        child: Scaffold(
          body: Align(
            alignment: Alignment.bottomCenter,
            child: SizedBox(
              height: height,
              child: AgentResultsSheet(
                workspace: workspace,
                availableHeight: height,
                onSuggestion: (_) {},
                onRetry: () {},
              ),
            ),
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> _pendingFrames(WidgetTester tester) async {
  // Searching intentionally contains a live progress indicator. Wait only for
  // sheet/layout frames rather than trying to settle that repeating animation.
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 500));
  await tester.pump(const Duration(milliseconds: 16));
  await tester.pump(const Duration(milliseconds: 16));
}

ScrollPosition _position(WidgetTester tester) => tester
    .state<ScrollableState>(
      find.descendant(
        of: find.byType(ListView),
        matching: find.byType(Scrollable),
      ),
    )
    .position;

Future<void> _readHistory(WidgetTester tester) async {
  await tester.drag(find.byType(ListView), const Offset(0, 450));
  await _pendingFrames(tester);
  expect(_position(tester).extentAfter, greaterThan(100));
}

void main() {
  for (final status in [401, 403, 503, null]) {
    testWidgets('当前失败恢复优先可见 status=$status，不借旧成功摘要', (tester) async {
      final source = _PendingSource();
      final workspace = _workspace(source);
      addTearDown(workspace.dispose);
      await _mount(tester, workspace);
      final pending = workspace.submit('本轮未完成的查询', [], [], cityID: 'aberdeen');
      source.requests.single.completeError(
        AgentRequestFailure('本轮查询失败。', statusCode: status),
      );
      await pending;
      workspace.showContent(AgentContentMode.conversation);
      await tester.pumpAndSettle();
      expect(workspace.queryState, AgentQueryState.error);
      expect(find.text('查询未完成').hitTestable(), findsOneWidget);
      expect(find.text('本轮查询失败。').hitTestable(), findsOneWidget);
      if (status == 401 || status == 403) {
        expect(find.text('重试'), findsNothing);
        expect(
          find.text(workspace.requestFailure!.recoveryMessage!).hitTestable(),
          findsOneWidget,
        );
      } else {
        expect(find.text('重试').hitTestable(), findsOneWidget);
      }
      expect(find.text('0 个活动 · 0 个组织 · 0 个地点'), findsNothing);
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('缺城市恢复优先可见，原输入与唯一选择城市入口保留', (tester) async {
    final workspace = _workspace(_PendingSource());
    addTearDown(workspace.dispose);
    workspace.requireCity('保留我的原句');
    workspace.showContent(AgentContentMode.conversation);
    await _mount(tester, workspace);
    expect(find.text('尚未搜索 · 需要选择城市').hitTestable(), findsOneWidget);
    expect(find.text('选择城市继续'), findsOneWidget);
    expect(find.text('保留我的原句').hitTestable(), findsOneWidget);
    expect(find.text('重试'), findsNothing);
    expect(workspace.queryState, AgentQueryState.needsScope);
    expect(tester.takeException(), isNull);
  });

  testWidgets('当前不支持状态优先说明，不自动滚到旧成功轮次', (tester) async {
    final workspace = _workspace(_PendingSource())
      ..result = AgentResult(
        entities: const [],
        activities: const [],
        places: const [],
        note: '当前请求暂不可处理。',
        resultSet: AgentResultSet(
          id: 'unsupported-result',
          status: 'unsupported',
          entities: const [],
          generatedAt: DateTime.utc(2026, 10, 7),
        ),
      );
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    expect(workspace.queryState, AgentQueryState.unsupported);
    expect(find.text('当前请求暂不可处理').hitTestable(), findsOneWidget);
    expect(find.text('重试'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('待回复时阅读历史发生错误，当前恢复优先且不伪称旧结果成功', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final pending = workspace.submit('本轮读取', [], [], cityID: 'aberdeen');
    workspace.showContent(AgentContentMode.conversation);
    await _pendingFrames(tester);
    await _readHistory(tester);
    source.requests.single.completeError(const AgentRequestFailure('本次连接中断。'));
    await pending;
    await tester.pumpAndSettle();
    expect(find.text('查询未完成').hitTestable(), findsOneWidget);
    expect(find.text('本次连接中断。').hitTestable(), findsOneWidget);
    expect(find.text('重试').hitTestable(), findsOneWidget);
    expect(find.text('0 个活动 · 0 个组织 · 0 个地点'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('本人续问与当前回复可见，不必手动翻过旧对话', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final submitted = workspace.submit('近一点的呢？', [], [], cityID: 'aberdeen');
    // The existing controller owns sheet height. Reopening the conversation
    // during the pending turn must present the submitted message, not history.
    workspace.showContent(AgentContentMode.conversation);
    await _pendingFrames(tester);
    expect(find.text('近一点的呢？').hitTestable(), findsOneWidget);
    source.requests.single.complete(_answer('继续查找周末羽毛球，找到 2 个活动。'));
    await submitted;
    await tester.pumpAndSettle();
    expect(find.text('继续查找周末羽毛球，找到 2 个活动。').hitTestable(), findsOneWidget);
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    expect(workspace.contentMode, AgentContentMode.conversation);
    expect(tester.takeException(), isNull);
  });

  testWidgets('打开已有对话显示当前末尾，手动阅读历史不被焦点通知打断', (tester) async {
    final workspace = _workspace(_PendingSource());
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    expect(find.text('当前历史末尾：周末活动可继续查询。').hitTestable(), findsOneWidget);
    await _readHistory(tester);
    final pixels = _position(tester).pixels;
    workspace.beginTyping();
    await _pendingFrames(tester);
    expect(_position(tester).pixels, closeTo(pixels, 0.5));
    expect(workspace.inputFocused, isTrue);
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    expect(tester.takeException(), isNull);
  });

  testWidgets('等待本人回复时主动上翻，回复保留历史阅读位置', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final submitted = workspace.submit('近一点的呢？', [], [], cityID: 'aberdeen');
    workspace.showContent(AgentContentMode.conversation);
    await _pendingFrames(tester);
    expect(find.text('近一点的呢？').hitTestable(), findsOneWidget);
    await _readHistory(tester);
    final pixels = _position(tester).pixels;
    source.requests.single.complete(_answer('当前回答不抢走你的阅读位置。'));
    await submitted;
    await tester.pumpAndSettle();
    expect(_position(tester).pixels, closeTo(pixels, 0.5));
    expect(find.text('当前回答不抢走你的阅读位置。').hitTestable(), findsNothing);
    expect(workspace.conversation.last.text, '当前回答不抢走你的阅读位置。');
    expect(tester.takeException(), isNull);
  });

  testWidgets('从历史主动提交新问题重新跟随本人当前轮次', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    await _readHistory(tester);
    final submitted = workspace.submit('改成篮球呢？', [], [], cityID: 'aberdeen');
    workspace.showContent(AgentContentMode.conversation);
    await _pendingFrames(tester);
    expect(find.text('改成篮球呢？').hitTestable(), findsOneWidget);
    source.requests.single.complete(_answer('已按本轮条件查找篮球活动。'));
    await submitted;
    await tester.pumpAndSettle();
    expect(find.text('已按本轮条件查找篮球活动。').hitTestable(), findsOneWidget);
    expect(source.requests, hasLength(1));
    expect(tester.takeException(), isNull);
  });

  testWidgets('新请求先完成，旧请求晚到不复活旧回答或滚动到旧轮次', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final older = workspace.submit('旧请求', [], [], cityID: 'aberdeen');
    final newer = workspace.submit('当前请求', [], [], cityID: 'aberdeen');
    workspace.showContent(AgentContentMode.conversation);
    source.requests[1].complete(_answer('只有当前请求的结果。'));
    await newer;
    await tester.pumpAndSettle();
    expect(find.text('只有当前请求的结果。').hitTestable(), findsOneWidget);
    await _readHistory(tester);
    final pixels = _position(tester).pixels;
    source.requests[0].complete(_answer('旧请求不得覆盖。'));
    await older;
    await tester.pumpAndSettle();
    expect(find.text('旧请求不得覆盖。'), findsNothing);
    expect(workspace.conversation.any((m) => m.text == '旧请求不得覆盖。'), isFalse);
    expect(_position(tester).pixels, closeTo(pixels, 0.5));
    expect(source.requests, hasLength(2));
    expect(tester.takeException(), isNull);
  });

  testWidgets('换账号清除旧任务，迟到回复与旧滚动回调不可进入新对话', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final older = workspace.submit('旧账号请求', [], [], cityID: 'aberdeen');
    workspace.showContent(AgentContentMode.conversation);
    workspace.clearAccountContext();
    final newer = workspace.submit('新账号请求', [], [], cityID: 'new-city');
    workspace.showContent(AgentContentMode.conversation);
    source.requests[1].complete(_answer('新账号当前回答。'));
    await newer;
    await tester.pumpAndSettle();
    source.requests[0].complete(_answer('旧账号回答不能复活。'));
    await older;
    await tester.pumpAndSettle();
    expect(find.text('新账号当前回答。').hitTestable(), findsOneWidget);
    expect(find.text('旧账号回答不能复活。'), findsNothing);
    expect(workspace.task!.cityID, 'new-city');
    expect(workspace.conversation.map((m) => m.text), ['新账号请求', '新账号当前回答。']);
    expect(tester.takeException(), isNull);
  });

  testWidgets('范围退休后原请求回执不加入对话，手动阅读位置不变', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final pending = workspace.submit('旧范围查询', [], [], cityID: 'aberdeen');
    workspace.showContent(AgentContentMode.conversation);
    await _pendingFrames(tester);
    await _readHistory(tester);
    final pixels = _position(tester).pixels;
    workspace.retirePendingQueryForViewChange();
    await _pendingFrames(tester);
    source.requests.single.complete(_answer('旧范围不得显示为当前结果。'));
    await pending;
    await tester.pumpAndSettle();
    expect(
      workspace.conversation.any((m) => m.text == '旧范围不得显示为当前结果。'),
      isFalse,
    );
    expect(_position(tester).pixels, closeTo(pixels, 0.5));
    expect(workspace.requestError, contains('查看范围已变化'));
    expect(tester.takeException(), isNull);
  });

  testWidgets('对话展示通知不改变结果、选中实体、焦点与原任务高度', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source)
      ..selectedEntityId = 'place:stable'
      ..inputFocused = true;
    final result = workspace.result, task = workspace.task;
    final epoch = workspace.taskEpoch;
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    expect(workspace.result, same(result));
    expect(workspace.task, same(task));
    expect(workspace.taskEpoch, epoch);
    expect(workspace.selectedEntityId, 'place:stable');
    expect(workspace.inputFocused, isTrue);
    expect(source.requests, isEmpty);
    workspace.showContent(AgentContentMode.results);
    await tester.pumpAndSettle();
    // Map presentation changes height; explanation and cards stay in one flow.
    expect(find.byType(AgentConversation), findsOneWidget);
    expect(workspace.result, same(result));
    expect(workspace.task, same(task));
    expect(workspace.selectedEntityId, 'place:stable');
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    expect(tester.takeException(), isNull);
  });

  testWidgets('窄屏大字有限面板内当前回复可见，未改变高度口径', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace, width: 320, textScale: 2, height: 400);
    final pending = workspace.submit('近一点', [], [], cityID: 'aberdeen');
    workspace.showContent(AgentContentMode.conversation);
    source.requests.single.complete(_answer('找到 2 个活动。'));
    await pending;
    await tester.pumpAndSettle();
    expect(find.text('找到 2 个活动。').hitTestable(), findsOneWidget);
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    expect(tester.takeException(), isNull);
  });

  testWidgets('辅助技术向上滚动历史后当前回复也保留阅读位置', (tester) async {
    final semantics = tester.ensureSemantics();
    try {
      final source = _PendingSource();
      final workspace = _workspace(source);
      addTearDown(workspace.dispose);
      await _mount(tester, workspace);
      final pending = workspace.submit('等回复时阅读历史', [], [], cityID: 'aberdeen');
      workspace.showContent(AgentContentMode.conversation);
      await _pendingFrames(tester);
      SemanticsNode? target;
      void findInnerScroll(SemanticsNode node) {
        final data = node.getSemanticsData();
        if (data.hasAction(SemanticsAction.scrollToOffset) &&
            (data.scrollExtentMax ?? 0) > 1000) {
          target = node;
        }
        node.visitChildren((child) {
          findInnerScroll(child);
          return true;
        });
      }

      findInnerScroll(tester.getSemantics(find.byType(ListView)));
      expect(target, isNotNull);
      final beforeScroll = _position(tester).pixels;
      tester.binding.renderViews.single.owner!.semanticsOwner!.performAction(
        target!.id,
        SemanticsAction.scrollToOffset,
        Float64List.fromList([0, _position(tester).pixels - 450]),
      );
      expect(_position(tester).pixels, lessThan(beforeScroll - 100));
      await _pendingFrames(tester);
      expect(_position(tester).extentAfter, greaterThan(100));
      final pixels = _position(tester).pixels;
      source.requests.single.complete(_answer('不打断辅助技术阅读历史。'));
      await pending;
      await tester.pumpAndSettle();
      expect(_position(tester).pixels, closeTo(pixels, 0.5));
      expect(tester.takeException(), isNull);
    } finally {
      semantics.dispose();
    }
  });
}
