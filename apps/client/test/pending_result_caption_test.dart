import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Future<void> showResult(
  WidgetTester tester, {
  String status = 'ACTIVE',
  List<AgentMessage> messages = const [
    AgentMessage(role: 'user', text: 'list restaurant names and addresses'),
  ],
  String? resultTaskID = 'original-task',
  AgentSheetExtent extent = AgentSheetExtent.peek,
  AgentViewState state = AgentViewState.results,
  String? error,
}) async {
  final workspace = AgentWorkspaceController()
    ..task = AgentTask(
      id: 'original-task',
      query: '找地点',
      status: status,
      intent: 'FIND_PLACE',
      messages: messages,
    )
    ..result = AgentResult(
      entities: const [],
      activities: const [],
      places: const [],
      taskID: resultTaskID,
      note: '这轮查询尚未完成；尚无可用结果。',
    )
    ..state = state
    ..requestError = error
    ..setSheetExtent(extent);
  addTearDown(workspace.dispose);
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: AgentResultsSheet(
          workspace: workspace,
          availableHeight: 600,
          onSuggestion: (_) {},
          onRetry: () {},
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
  expect(tester.takeException(), isNull);
}

void main() {
  testWidgets('ACTIVE末条用户请求的空结果不冒称已搜索无匹配', (t) async {
    await showResult(t);
    expect(find.text('查询尚未完成 · 暂无结果'), findsOneWidget);
    expect(find.text('未找到符合条件的公开内容'), findsNothing);
  });

  testWidgets('展开同一未完成轮次的消息与空结果提示一致', (t) async {
    await showResult(t, extent: AgentSheetExtent.expanded);
    expect(find.text('查询尚未完成 · 暂无结果'), findsOneWidget);
    expect(find.text('这轮查询尚未完成；尚无可用结果。'), findsOneWidget);
    expect(find.text('这轮查询尚未完成；暂无可展示的结果。'), findsNothing);
    expect(find.text('当前没有可展示的结果。'), findsNothing);
  });

  for (final status in ['COMPLETED', 'CANCELLED', 'NEEDS_CLARIFICATION']) {
    testWidgets('$status空集保持原空结果语义', (t) async {
      await showResult(t, status: status);
      expect(find.text('未找到符合条件的公开内容'), findsOneWidget);
      expect(find.text('查询尚未完成 · 暂无结果'), findsNothing);
    });
  }

  testWidgets('ACTIVE已有助手完成回答不因旧用户消息改成未完成', (t) async {
    await showResult(
      t,
      messages: const [
        AgentMessage(role: 'user', text: '找地点'),
        AgentMessage(role: 'assistant', text: '没有符合条件的地点。'),
      ],
    );
    expect(find.text('未找到符合条件的公开内容'), findsOneWidget);
    expect(find.text('查询尚未完成 · 暂无结果'), findsNothing);
  });

  testWidgets('无消息及错误Task结果不得推断当前用户轮次未完成', (t) async {
    await showResult(t, messages: const []);
    expect(find.text('未找到符合条件的公开内容'), findsOneWidget);
    await showResult(t, resultTaskID: 'another-task');
    expect(find.text('查询尚未完成 · 暂无结果'), findsNothing);
  });

  testWidgets('无结果TaskID的原本地空轮次仍可正确解释未完成', (t) async {
    await showResult(t, resultTaskID: null);
    expect(find.text('查询尚未完成 · 暂无结果'), findsOneWidget);
  });

  testWidgets('当前错误保持错误状态和原输入，不被空集提示覆盖', (t) async {
    await showResult(t, error: '服务暂不可用');
    expect(find.text('查询未完成'), findsOneWidget);
    expect(find.text('服务暂不可用'), findsOneWidget);
    expect(find.text('查询尚未完成 · 暂无结果'), findsNothing);
  });
}
