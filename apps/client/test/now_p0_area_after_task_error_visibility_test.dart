import 'dart:async';

import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _bounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.3);
const _oldFailure = AgentRequestFailure('旧任务访问被拒绝。', statusCode: 403);
const _partialSummary = '部分内容暂时无法获取，已显示本次查到的结果。';

AgentWorkspaceController _failedHistory() {
  final workspace = AgentWorkspaceController()
    ..task = const AgentTask(
      id: 'retained-task-with-403',
      query: 'Glenhouse 旧查询',
      status: 'ACTIVE',
      cityID: 'aberdeen-gb',
    )
    ..result = const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '旧查询已有回答。',
    )
    ..state = AgentViewState.conversation
    ..requestFailure = _oldFailure
    ..requestError = _oldFailure.userMessage
    ..selectedEntityId = 'place:retained-selection';
  for (var turn = 0; turn < 8; turn++) {
    workspace.conversation.addAll([
      AgentMessage(role: 'user', text: '历史问题 $turn'),
      AgentMessage(
        role: 'assistant',
        text: '历史回答 $turn：${'仅使用本轮授权来源，请查看内容详情。' * 10}',
      ),
    ]);
  }
  workspace.showContent(AgentContentMode.conversation);
  return workspace;
}

AgentAreaRead _partialRead() => AgentAreaRead(
  publicView: TypedMapView(
    cityID: 'aberdeen-gb',
    scope: 'PUBLIC',
    observedAt: DateTime.utc(2026, 10, 10),
    validUntil: DateTime.utc(2026, 10, 10, 0, 2),
    truncated: false,
    items: List.generate(
      13,
      (index) => TypedMapItem(
        kind: 'PLACE',
        id: 'fixture-public-place-$index',
        title: '区域测试地点 $index',
        detailType: 'PLACE',
        detailID: 'fixture-public-place-$index',
        placeID: 'fixture-public-place-$index',
        anchorLabel: '仅用于滚动可见性单元测试',
        latitude: 57.15,
        longitude: -2.1,
        sourceVersion: 'fixture-version-$index',
      ),
    ),
  ),
  unavailableKinds: const {'OPPORTUNITY'},
);

Future<void> _mount(
  WidgetTester tester,
  AgentWorkspaceController workspace,
) async {
  tester.view.physicalSize = const Size(390, 800);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Align(
          alignment: Alignment.bottomCenter,
          child: SizedBox(
            height: 350,
            child: AgentResultsSheet(
              workspace: workspace,
              embedded: true,
              availableHeight: 350,
              onSuggestion: (_) {},
              onRetry: () {},
            ),
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> _loadingFrames(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 300));
  for (var frame = 0; frame < 4; frame++) {
    await tester.pump(const Duration(milliseconds: 16));
  }
}

ScrollPosition _listPosition(WidgetTester tester) => tester
    .state<ScrollableState>(
      find.descendant(
        of: find.byType(ListView),
        matching: find.byType(Scrollable),
      ),
    )
    .position;

void main() {
  testWidgets('新区域 partial 正文实际可见，不被保留的旧任务403拉回顶部', (tester) async {
    final workspace = _failedHistory();
    addTearDown(workspace.dispose);
    final oldTask = workspace.task;
    final oldResult = workspace.result;
    final history = List<AgentMessage>.of(workspace.conversation);
    final epoch = workspace.taskEpoch;
    await _mount(tester, workspace);
    expect(find.text(_oldFailure.userMessage).hitTestable(), findsOneWidget);

    final response = Completer<AgentAreaRead>();
    final command = workspace.executeMapArea(
      cityID: 'aberdeen-gb',
      bounds: _bounds,
      kinds: const {'PLACE', 'OPPORTUNITY'},
      reader: () => response.future,
    );
    await _loadingFrames(tester);
    expect(find.text('正在搜索这片地图区域…').hitTestable(), findsOneWidget);
    response.complete(_partialRead());
    await command;
    await tester.pumpAndSettle();

    expect(workspace.areaOperation!.status, AgentAreaStatus.partial);
    expect(workspace.areaOperation!.items, hasLength(13));
    expect(find.text(_partialSummary).hitTestable(), findsOneWidget);
    expect(find.text(_oldFailure.userMessage), findsNothing);
    expect(workspace.task, same(oldTask));
    expect(workspace.result, same(oldResult));
    expect(workspace.taskEpoch, epoch);
    expect(workspace.requestFailure, same(_oldFailure));
    expect(workspace.requestError, _oldFailure.userMessage);
    expect(workspace.queryState, AgentQueryState.error);
    expect(workspace.selectedEntityId, 'place:retained-selection');
    expect(workspace.conversation, orderedEquals(history));
    expect(
      workspace.conversation.where((message) => message.role == 'user'),
      hasLength(8),
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('新区域请求后主动阅读历史，partial完成和重排保持用户滚动选择', (tester) async {
    final workspace = _failedHistory();
    addTearDown(workspace.dispose);
    await _mount(tester, workspace);
    final response = Completer<AgentAreaRead>();
    final command = workspace.executeMapArea(
      cityID: 'aberdeen-gb',
      bounds: _bounds,
      kinds: const {'PLACE', 'OPPORTUNITY'},
      reader: () => response.future,
    );
    await _loadingFrames(tester);
    expect(find.text('正在搜索这片地图区域…').hitTestable(), findsOneWidget);
    await tester.drag(find.byType(ListView), const Offset(0, 900));
    await _loadingFrames(tester);
    final position = _listPosition(tester);
    final readingOffset = position.pixels;
    expect(position.extentAfter, greaterThan(300));
    expect(find.text('正在搜索这片地图区域…').hitTestable(), findsNothing);

    response.complete(_partialRead());
    await command;
    await tester.pumpAndSettle();
    expect(position.pixels, closeTo(readingOffset, 1));
    expect(find.text(_partialSummary).hitTestable(), findsNothing);
    workspace.setSheetExtent(AgentSheetExtent.medium);
    await tester.pumpAndSettle();
    expect(_listPosition(tester), same(position));
    expect(position.pixels, closeTo(readingOffset, 1));
    expect(workspace.requestError, _oldFailure.userMessage);
    expect(tester.takeException(), isNull);
  });
}
