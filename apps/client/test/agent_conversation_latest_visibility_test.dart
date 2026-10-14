import 'dart:async';
import 'dart:typed_data';
import 'dart:ui' show SemanticsAction;

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart' show ScrollDirection;
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

AgentResult _placeAnswer(String message) => AgentResult(
  entities: const [],
  activities: const [],
  places: List.generate(
    10,
    (i) => PublicPlace(
      id: 'fixture-place-$i',
      name: '布局测试地点 $i',
      categoryCode: 'restaurant',
      summary: '仅用于测试十张地点卡与解释的布局，不是真实检索证据。',
      addressLabel: '测试地址 $i',
      source: const PublicSource(
        label: '测试来源',
        maintainer: '测试维护者',
        freshness: 'unverified',
        updatedAt: null,
      ),
      location: const PublicPlaceLocation(
        coordinateSystem: 'wgs84',
        precision: 'point',
        latitude: 57.15,
        longitude: -2.09,
      ),
    ),
  ),
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
  Widget Function(int messageIndex)? attachmentBuilder,
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
                attachmentBuilder: attachmentBuilder,
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
  testWidgets('横向材料fling不退休纵向当前轮跟随，新回复仍进入解释', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(
      tester,
      workspace,
      attachmentBuilder: (index) => index == 15
          ? SizedBox(
              height: 120,
              child: ListView(
                key: const Key('test-horizontal-material-scroll'),
                scrollDirection: Axis.horizontal,
                primary: false,
                children: List.generate(
                  30,
                  (i) => SizedBox(width: 120, child: Text('横向测试行 $i')),
                ),
              ),
            )
          : const SizedBox.shrink(),
    );
    final pending = workspace.submit('中文当前轮横向材料', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    final mainScroll = find
        .descendant(
          of: find.byType(AgentConversation),
          matching: find.byType(Scrollable),
        )
        .first;
    final position = tester.state<ScrollableState>(mainScroll).position;
    await tester.fling(
      find.byKey(const Key('test-horizontal-material-scroll')),
      const Offset(-100, 0),
      900,
    );
    await _pendingFrames(tester);
    source.requests.single.complete(_answer('横向操作后当前解释可见。'));
    await pending;
    await tester.pumpAndSettle();
    expect(find.text('横向操作后当前解释可见。').hitTestable(), findsOneWidget);
    // The plain answer can be shorter than the loading row. Its visibility,
    // rather than an increase in scroll pixels, proves latest-turn following.
    expect(position.pixels, lessThanOrEqualTo(position.maxScrollExtent));
    expect(tester.takeException(), isNull);
  });

  testWidgets('真实fling长中文回复后折叠返回保持内外位置，不自动跟随当前解释', (tester) async {
    final workspace = _workspace(_PendingSource());
    addTearDown(workspace.dispose);
    workspace.conversation[workspace.conversation.length - 1] = AgentMessage(
      role: 'assistant',
      text:
          '当前长解释：\n${List.generate(10, (i) => '候选 $i：仅用于布局测试的名称与地址；营业时间待核实。').join('\n\n')}',
    );
    await _mount(tester, workspace);
    final currentText = workspace.conversation.last.text;
    final beforeLatestTop = tester.getTopLeft(find.text(currentText)).dy;
    await tester.fling(find.byType(ListView), const Offset(0, 180), 600);
    await tester.pumpAndSettle();
    final inner = _position(tester);
    final outer = tester
        .state<NestedScrollViewState>(find.byType(NestedScrollView))
        .outerController
        .position;
    final pixels = inner.pixels, outerPixels = outer.pixels;
    final latestTop = tester.getTopLeft(find.text(currentText)).dy;
    expect(latestTop, greaterThan(beforeLatestTop + 100));
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await tester.pumpAndSettle();
    workspace.selectEntity('place:readonly-selection', preserveViewport: true);
    workspace.setSheetExtent(AgentSheetExtent.expanded);
    await tester.pumpAndSettle();
    expect(_position(tester), same(inner));
    expect(inner.pixels, closeTo(pixels, 0.5));
    expect(outer.pixels, closeTo(outerPixels, 0.5));
    expect(
      tester.getTopLeft(find.text(currentText)).dy,
      closeTo(latestTop, 0.5),
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('fling读历史之后迟到ScrollEnd底部估值与metrics不重启自动跟随', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    await tester.fling(find.byType(ListView), const Offset(0, 350), 1200);
    await tester.pumpAndSettle();
    final position = _position(tester);
    final pixels = position.pixels;
    expect(position.extentAfter, greaterThan(100));
    final context = tester.element(find.byType(AgentConversation));
    // A nested idle/end or lazy extent estimate is not a new user request to
    // follow the latest answer. Its end metrics can precede updated layout.
    ScrollEndNotification(
      metrics: FixedScrollMetrics(
        minScrollExtent: 0,
        maxScrollExtent: 100,
        pixels: 100,
        viewportDimension: 400,
        axisDirection: AxisDirection.down,
        devicePixelRatio: 1,
      ),
      context: context,
    ).dispatch(context);
    ScrollMetricsNotification(
      metrics: position,
      context: context,
    ).dispatch(context);
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await tester.pumpAndSettle();
    workspace.setSheetExtent(AgentSheetExtent.expanded);
    await tester.pumpAndSettle();
    expect(position.pixels, closeTo(pixels, 0.5));
    expect(source.requests, isEmpty);
    expect(tester.takeException(), isNull);
  });

  testWidgets('消息内真实纵向Nested滚动后迟到回复不抢正在阅读的材料', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    final events = <int>[];
    addTearDown(workspace.dispose);
    await _mount(
      tester,
      workspace,
      attachmentBuilder: (index) => index == 15
          ? SizedBox(
              height: 200,
              child: NotificationListener<ScrollNotification>(
                onNotification: (event) {
                  if (event is UserScrollNotification &&
                      event.direction != ScrollDirection.idle) {
                    events.add(event.depth);
                  }
                  return false;
                },
                child: ListView(
                  key: const Key('test-nested-material-scroll'),
                  primary: false,
                  children: List.generate(
                    30,
                    (i) => SizedBox(height: 48, child: Text('仅测试的附件行 $i')),
                  ),
                ),
              ),
            )
          : const SizedBox.shrink(),
    );
    final rootDepths = <int>[];
    final mountedApp = tester.widget<MaterialApp>(find.byType(MaterialApp));
    await tester.pumpWidget(
      NotificationListener<ScrollNotification>(
        onNotification: (event) {
          if (event is UserScrollNotification &&
              event.direction != ScrollDirection.idle &&
              event.context?.findAncestorWidgetOfExactType<ListView>()?.key ==
                  const Key('test-nested-material-scroll')) {
            rootDepths.add(event.depth);
          }
          return false;
        },
        child: mountedApp,
      ),
    );
    await tester.pumpAndSettle();
    final pending = workspace.submit('阅读材料时的中文追问', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    final material = find.byKey(const Key('test-nested-material-scroll'));
    expect(material.hitTestable(), findsOneWidget);
    final mainScroll = find
        .descendant(
          of: find.byType(AgentConversation),
          matching: find.byType(Scrollable),
        )
        .first;
    final position = tester.state<ScrollableState>(mainScroll).position;
    final pixels = position.pixels;
    await tester.fling(material, const Offset(0, -100), 900);
    await _pendingFrames(tester);
    expect(events, isNotEmpty);
    expect(rootDepths, isNotEmpty);
    expect(rootDepths.every((depth) => depth > 1), isTrue);
    debugPrint('NATIVE_MANUAL_NESTED_TEST_ROOT_DEPTHS=$rootDepths');
    source.requests.single.complete(_answer('新回复不能把正在读的纵向材料拉走。'));
    await pending;
    await tester.pumpAndSettle();
    expect(
      position.pixels,
      closeTo(
        pixels.clamp(position.minScrollExtent, position.maxScrollExtent),
        0.5,
      ),
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('收起只隐藏消息的交互语义，协调顶栏与列表位置及State精确保留', (tester) async {
    final semantics = tester.ensureSemantics();
    try {
      final source = _PendingSource();
      final workspace = _workspace(source);
      addTearDown(workspace.dispose);
      await _mount(tester, workspace);
      await _readHistory(tester);
      final nested = tester.state<NestedScrollViewState>(
        find.byType(NestedScrollView),
      );
      final inner = _position(tester);
      final outer = nested.outerController.position;
      final innerPixels = inner.pixels, outerPixels = outer.pixels;
      workspace.setSheetExtent(AgentSheetExtent.peek);
      await tester.pumpAndSettle();
      expect(find.byType(AgentConversation), findsNothing);
      expect(
        find.byType(AgentConversation, skipOffstage: false),
        findsOneWidget,
      );
      expect(find.text('当前历史末尾：周末活动可继续查询。').hitTestable(), findsNothing);
      expect(find.bySemanticsLabel(RegExp('历史问题|历史回答|当前历史末尾')), findsNothing);
      expect(
        find.byKey(const Key('agent-sheet-expand-summary')).hitTestable(),
        findsOneWidget,
      );
      workspace.setSheetExtent(AgentSheetExtent.expanded);
      await tester.pumpAndSettle();
      expect(
        tester.state<NestedScrollViewState>(find.byType(NestedScrollView)),
        same(nested),
      );
      expect(_position(tester), same(inner));
      expect(nested.outerController.position, same(outer));
      expect(inner.pixels, closeTo(innerPixels, 0.5));
      expect(outer.pixels, closeTo(outerPixels, 0.5));
      expect(source.requests, isEmpty);
      expect(tester.takeException(), isNull);
    } finally {
      semantics.dispose();
    }
  });

  testWidgets('新Task退休与同ID恢复在一帧内发生，第一次恢复仍进入当前解释', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    await _readHistory(tester);
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await tester.pumpAndSettle();
    final previous = AgentTask(
      id: workspace.task!.id,
      query: workspace.task!.query,
      status: 'COMPLETED',
      cityID: 'aberdeen',
      messages: List<AgentMessage>.of(workspace.conversation),
    );
    // reopen calls newTask and publishes the original ID synchronously. Both
    // notifications can precede one build, so a key alone is not retirement.
    final reopened = workspace.reopen(previous, [], []);
    source.requests.single.complete(_answer('恢复读取的原结果，不是新检索。'));
    await reopened;
    workspace.showContent(AgentContentMode.conversation);
    await tester.pumpAndSettle();
    expect(find.text('当前历史末尾：周末活动可继续查询。').hitTestable(), findsOneWidget);
    expect(workspace.task!.id, previous.id);
    expect(workspace.conversation, orderedEquals(previous.messages));
    expect(source.requests, hasLength(1));
    expect(tester.takeException(), isNull);
  });

  testWidgets('收起再展开保留中文历史阅读位置与原任务，不跳到旧首页或当前底部', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final pending = workspace.submit('找十个晚餐地点', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    source.requests.single.complete(_placeAnswer('本轮十地点解释：名称和地址已列出。'));
    await pending;
    await tester.pumpAndSettle();
    expect(find.text('本轮十地点解释：名称和地址已列出。').hitTestable(), findsOneWidget);
    await _readHistory(tester);
    await _readHistory(tester);
    final pixels = _position(tester).pixels;
    expect(find.text('本轮十地点解释：名称和地址已列出。').hitTestable(), findsNothing);
    final task = workspace.task, result = workspace.result;
    final epoch = workspace.taskEpoch;
    final messages = List<AgentMessage>.of(workspace.conversation);
    await tester.tap(find.byKey(const Key('agent-sheet-map-toggle')));
    await tester.pumpAndSettle();
    expect(workspace.sheetExtent, AgentSheetExtent.peek);
    expect(find.text('本轮十地点解释：名称和地址已列出。').hitTestable(), findsNothing);
    workspace.selectEntity('place:fixture-place-4', preserveViewport: true);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('agent-sheet-map-toggle')));
    await tester.pumpAndSettle();
    expect(_position(tester).pixels, closeTo(pixels, 0.5));
    expect(find.text('本轮十地点解释：名称和地址已列出。').hitTestable(), findsNothing);
    expect(workspace.task, same(task));
    expect(workspace.result, same(result));
    expect(workspace.taskEpoch, epoch);
    expect(workspace.conversation, orderedEquals(messages));
    expect(workspace.selectedEntityId, 'place:fixture-place-4');
    expect(workspace.preserveReplyCamera, isTrue);
    expect(source.requests, hasLength(1));
    expect(tester.takeException(), isNull);
  });

  testWidgets('键盘大字历史收起时新回答到达，展开与输入焦点通知保留原阅读位置', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace, width: 320, textScale: 2, height: 400);
    final pending = workspace.submit('它们的营业时间呢？', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    await _readHistory(tester);
    final pixels = _position(tester).pixels;
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await _pendingFrames(tester);
    source.requests.single.complete(_placeAnswer('本轮时间解释：未核实的时间明确待核实。'));
    await pending;
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await tester.pumpAndSettle();
    workspace.beginTyping();
    await tester.pumpAndSettle();
    expect(_position(tester).pixels, closeTo(pixels, 0.5));
    expect(find.text('本轮时间解释：未核实的时间明确待核实。').hitTestable(), findsNothing);
    expect(workspace.inputFocused, isTrue);
    expect(source.requests, hasLength(1));
    expect(tester.takeException(), isNull);
  });

  testWidgets('第一次从收起恢复长多轮十地点回答，解释可见；地图选择后仍显示当前轮', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    final pending = workspace.submit('十个地点', [], [], cityID: 'aberdeen');
    source.requests.single.complete(_placeAnswer('最终十地点解释，不是历史单地点回答。'));
    await pending;
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await _mount(tester, workspace);
    await tester.tap(find.byKey(const Key('agent-sheet-expand-summary')));
    await tester.pumpAndSettle();
    expect(find.text('最终十地点解释，不是历史单地点回答。').hitTestable(), findsOneWidget);
    final pixels = _position(tester).pixels;
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await tester.pumpAndSettle();
    workspace.selectEntity('place:fixture-place-2', preserveViewport: true);
    workspace.setSheetExtent(AgentSheetExtent.medium);
    await tester.pumpAndSettle();
    expect(find.text('最终十地点解释，不是历史单地点回答。').hitTestable(), findsOneWidget);
    expect(_position(tester).pixels, closeTo(pixels, 0.5));
    expect(workspace.result!.places, hasLength(10));
    expect(workspace.selectedEntityId, 'place:fixture-place-2');
    expect(tester.takeException(), isNull);
  });

  testWidgets('失败后收起再展开仍先看到唯一失败恢复头，不伪称旧十地点成功', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final pending = workspace.submit('失败的当前追问', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    source.requests.single.completeError(
      const AgentRequestFailure('当前中文查询断开。', statusCode: 503),
    );
    await pending;
    workspace.showContent(AgentContentMode.conversation);
    await tester.pumpAndSettle();
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await tester.pumpAndSettle();
    workspace.setSheetExtent(AgentSheetExtent.expanded);
    await tester.pumpAndSettle();
    expect(find.text('查询未完成').hitTestable(), findsOneWidget);
    expect(find.text('当前中文查询断开。').hitTestable(), findsOneWidget);
    expect(find.text('重试').hitTestable(), findsOneWidget);
    expect(workspace.queryState, AgentQueryState.error);
    expect(tester.takeException(), isNull);
  });

  testWidgets('收起期间更换任务，不把旧历史阅读位置带入新任务', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    await _readHistory(tester);
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await tester.pumpAndSettle();
    workspace.newTask();
    final pending = workspace.submit('新任务的中文查询', [], [], cityID: 'aberdeen');
    source.requests.single.complete(_answer('新任务当前解释。'));
    await pending;
    workspace.showContent(AgentContentMode.conversation);
    await tester.pumpAndSettle();
    expect(find.text('新任务当前解释。').hitTestable(), findsOneWidget);
    expect(find.text('当前历史末尾：周末活动可继续查询。'), findsNothing);
    expect(workspace.task!.id, isNot('local-existing-conversation'));
    expect(tester.takeException(), isNull);
  });

  testWidgets('收起账号上下文ABA清除原任务，旧布局回调不恢复旧消息或位置', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    await _readHistory(tester);
    workspace.setSheetExtent(AgentSheetExtent.peek);
    // No frame between retirement notifications, as when a principal changes
    // and returns before Flutter paints. Existing taskEpoch still fences both.
    workspace.clearAccountContext();
    final older = workspace.submit('B账号请求', [], [], cityID: 'other-city');
    workspace.clearAccountContext();
    final current = workspace.submit('A账号的新请求', [], [], cityID: 'aberdeen');
    source.requests[1].complete(_answer('A账号当前解释，不恢复旧历史。'));
    await current;
    workspace.showContent(AgentContentMode.conversation);
    await tester.pumpAndSettle();
    source.requests[0].complete(_answer('B账号迟到解释不得复活。'));
    await older;
    await tester.pumpAndSettle();
    expect(find.text('A账号当前解释，不恢复旧历史。').hitTestable(), findsOneWidget);
    expect(find.text('B账号迟到解释不得复活。'), findsNothing);
    expect(find.text('当前历史末尾：周末活动可继续查询。'), findsNothing);
    expect(workspace.conversation.map((m) => m.text), [
      'A账号的新请求',
      'A账号当前解释，不恢复旧历史。',
    ]);
    expect(tester.takeException(), isNull);
  });

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

  testWidgets('十张地点卡后仍先看到本轮解释，连续追问不需手动翻到回答', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final first = workspace.submit('晚餐地点', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    source.requests[0].complete(_placeAnswer('本轮解释：找到十个候选地点。'));
    await first;
    await tester.pumpAndSettle();
    expect(find.text('本轮解释：找到十个候选地点。').hitTestable(), findsOneWidget);
    expect(workspace.result!.places, hasLength(10));
    final firstTaskID = workspace.task!.id;
    workspace.selectedEntityId = 'place:fixture-place-0';
    final second = workspace.submit('它的营业时间', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    expect(find.text('它的营业时间').hitTestable(), findsOneWidget);
    source.requests[1].complete(_placeAnswer('追问解释：该地点的时间需要按来源核实。'));
    await second;
    await tester.pumpAndSettle();
    expect(find.text('追问解释：该地点的时间需要按来源核实。').hitTestable(), findsOneWidget);
    expect(workspace.task!.id, firstTaskID);
    expect(workspace.selectedEntityId, 'place:fixture-place-0');
    expect(workspace.result!.places.first.id, 'fixture-place-0');
    expect(source.requests, hasLength(2));
    expect(tester.takeException(), isNull);
  });

  testWidgets('十张卡的待回复期间本人上翻，实际回答不抢历史阅读位置', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final pending = workspace.submit('中文追问时阅读历史', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    await _readHistory(tester);
    final pixels = _position(tester).pixels;
    source.requests.single.complete(_placeAnswer('十个候选保留，当前回答不抢历史。'));
    await pending;
    await tester.pumpAndSettle();
    expect(_position(tester).pixels, closeTo(pixels, 0.5));
    expect(workspace.conversation.last.text, '十个候选保留，当前回答不抢历史。');
    expect(find.text('十个候选保留，当前回答不抢历史。').hitTestable(), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('恢复十张卡的既有会话先看到最后解释，不需手动上翻卡片', (tester) async {
    final workspace = _workspace(_PendingSource())
      ..result = _placeAnswer('恢复结果的测试摘要');
    final task = workspace.task, result = workspace.result;
    final epoch = workspace.taskEpoch;
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    expect(find.text('当前历史末尾：周末活动可继续查询。').hitTestable(), findsOneWidget);
    expect(workspace.task, same(task));
    expect(workspace.result, same(result));
    expect(workspace.taskEpoch, epoch);
    expect(tester.takeException(), isNull);
  });

  testWidgets('当前轮附件高度迟到变化仍可看到解释，不更改任务与选中地点', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    final height = ValueNotifier<double>(48);
    addTearDown(workspace.dispose);
    addTearDown(height.dispose);
    await _mount(
      tester,
      workspace,
      attachmentBuilder: (index) => index == 15
          ? ValueListenableBuilder<double>(
              valueListenable: height,
              builder: (_, value, _) => SizedBox(height: value),
            )
          : const SizedBox.shrink(),
    );
    final pending = workspace.submit('带原消息图片的布局追问', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    source.requests.single.complete(_placeAnswer('图片与十卡之后的本轮解释。'));
    await pending;
    await tester.pumpAndSettle();
    workspace.selectedEntityId = 'place:fixture-place-0';
    final task = workspace.task, result = workspace.result;
    final epoch = workspace.taskEpoch;
    expect(find.text('图片与十卡之后的本轮解释。').hitTestable(), findsOneWidget);
    height.value = 420;
    await tester.pumpAndSettle();
    expect(find.text('图片与十卡之后的本轮解释。').hitTestable(), findsOneWidget);
    expect(workspace.task, same(task));
    expect(workspace.result, same(result));
    expect(workspace.taskEpoch, epoch);
    expect(workspace.selectedEntityId, 'place:fixture-place-0');
    expect(source.requests, hasLength(1));
    expect(tester.takeException(), isNull);
  });

  testWidgets('窄屏键盘面板内十卡回答的解释可见，读历史后扩大面板不强跳', (tester) async {
    final source = _PendingSource();
    final workspace = _workspace(source);
    addTearDown(workspace.dispose);
    await _mount(tester, workspace, width: 320, textScale: 2, height: 400);
    final pending = workspace.submit('它的营业时间', [], [], cityID: 'aberdeen');
    await _pendingFrames(tester);
    source.requests.single.complete(_placeAnswer('中文解释：时间待核实。'));
    await pending;
    await tester.pumpAndSettle();
    expect(find.text('中文解释：时间待核实。').hitTestable(), findsOneWidget);
    await _readHistory(tester);
    final pixels = _position(tester).pixels;
    final task = workspace.task, result = workspace.result;
    final epoch = workspace.taskEpoch;
    await _mount(tester, workspace, width: 320, textScale: 2, height: 520);
    expect(_position(tester).pixels, closeTo(pixels, 0.5));
    expect(workspace.task, same(task));
    expect(workspace.result, same(result));
    expect(workspace.taskEpoch, epoch);
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
