import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

const _hint = '请先确认输入法候选字，再点击发送。';
const _composing = TextEditingValue(
  text: '阿伯丁地点',
  selection: TextSelection.collapsed(offset: 5),
  composing: TextRange(start: 3, end: 5),
);

Future<AgentWorkspaceController> _mount(
  WidgetTester tester,
  List<String> sent, {
  double maxHeight = 240,
}) async {
  final workspace = AgentWorkspaceController()
    ..task = const AgentTask(id: 'same-task', query: '地点', status: 'COMPLETED')
    ..selectedEntityId = 'place:same-place';
  addTearDown(workspace.dispose);
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Align(
          alignment: Alignment.bottomCenter,
          child: ConstrainedBox(
            constraints: BoxConstraints(maxHeight: maxHeight),
            child: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onSearchArea: () {},
              hasSearchArea: false,
            ),
          ),
        ),
      ),
    ),
  );
  await tester.showKeyboard(find.byType(TextField));
  tester.testTextInput.updateEditingValue(_composing);
  await tester.pump();
  return workspace;
}

TextField _field(WidgetTester tester) =>
    tester.widget<TextField>(find.byType(TextField));

void main() {
  testWidgets('组合期点击发送有可读反馈，保留原任务、焦点与编辑值', (tester) async {
    final sent = <String>[];
    final workspace = await _mount(tester, sent);
    final task = workspace.task, epoch = workspace.taskEpoch;
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(sent, isEmpty);
    expect(_field(tester).controller!.value, _composing);
    expect(_field(tester).focusNode!.hasFocus, isTrue);
    expect(workspace.task, same(task));
    expect(workspace.taskEpoch, epoch);
    expect(workspace.selectedEntityId, 'place:same-place');
    expect(find.text(_hint).hitTestable(), findsOneWidget);
    final live = tester.widget<Semantics>(
      find
          .ancestor(of: find.text(_hint), matching: find.byType(Semantics))
          .first,
    );
    expect(live.properties.liveRegion, isTrue);
    expect(tester.takeException(), isNull);
  });

  testWidgets('连点与候选改变不排队，确认候选后仍需本人明确发送一次', (tester) async {
    final sent = <String>[];
    await _mount(tester, sent);
    await tester.tap(find.byTooltip('发送需求'));
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(sent, isEmpty);
    tester.testTextInput.updateEditingValue(_composing.copyWith(text: '阿伯丁餐厅'));
    await tester.pump();
    expect(sent, isEmpty);
    tester.testTextInput.updateEditingValue(
      const TextEditingValue(
        text: '阿伯丁餐厅',
        selection: TextSelection.collapsed(offset: 5),
      ),
    );
    await tester.pump();
    expect(sent, isEmpty);
    expect(find.text(_hint), findsNothing);
    expect(_field(tester).controller!.text, '阿伯丁餐厅');
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(sent, ['阿伯丁餐厅']);
    expect(_field(tester).controller!.text, isEmpty);
    expect(tester.takeException(), isNull);
  });

  testWidgets('键盘发送也解释组合态，文字提交后键盘发送沿用同一路径', (tester) async {
    final sent = <String>[];
    await _mount(tester, sent);
    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.pump();
    expect(sent, isEmpty);
    expect(_field(tester).controller!.value, _composing);
    expect(find.text(_hint).hitTestable(), findsOneWidget);
    tester.testTextInput.updateEditingValue(
      _composing.copyWith(composing: TextRange.empty),
    );
    await tester.pump();
    expect(sent, isEmpty);
    expect(find.text(_hint), findsNothing);
    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.pump();
    expect(sent, ['阿伯丁地点']);
    expect(tester.takeException(), isNull);
  });

  testWidgets('面板仅80dp时组合态发送提示仍可见，不遮发送目标', (tester) async {
    final sent = <String>[];
    await _mount(tester, sent, maxHeight: 80);
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 250));
    expect(sent, isEmpty);
    expect(_field(tester).controller!.value, _composing);
    expect(find.text(_hint).hitTestable(), findsOneWidget);
    expect(find.byTooltip('发送需求').hitTestable(), findsOneWidget);
    expect(
      tester.getSize(find.byTooltip('发送需求')).height,
      greaterThanOrEqualTo(48),
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('程序填入已确认文字只生成可编辑草稿，不自动提交', (tester) async {
    final sent = <String>[];
    await _mount(tester, sent);
    final composer = tester.state<AgentComposerState>(
      find.byType(AgentComposer),
    );
    composer.fillDraft('语音转写或本人填写的中文草稿');
    await tester.pump();
    expect(sent, isEmpty);
    expect(_field(tester).controller!.text, '语音转写或本人填写的中文草稿');
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(sent, ['语音转写或本人填写的中文草稿']);
    expect(tester.takeException(), isNull);
  });
}
