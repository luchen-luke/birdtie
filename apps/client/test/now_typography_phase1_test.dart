import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _longQuestion = '想在阿伯丁 Aberdeen 找适合朋友一起吃饭的餐厅，地点名称和中文说明比较长时也需要完整阅读。';
const _longAnswer =
    '已经找到当前范围内的公开餐厅资料。你可以逐个查看名称、地址和地图位置，再按自己的偏好决定。'
    '营业时间与评分仍需向餐厅核实，OpenStreetMap 的资料保留原始来源。';

void _view(WidgetTester tester) {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = const Size(320, 780);
  addTearDown(tester.view.resetDevicePixelRatio);
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetViewInsets);
}

Widget _host({
  required AgentWorkspaceController workspace,
  required Brightness brightness,
  required double scale,
  required ValueChanged<String> onSubmit,
}) => MaterialApp(
  theme: birdtieTheme(brightness),
  builder: (context, child) => MediaQuery(
    data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(scale)),
    child: child!,
  ),
  home: Scaffold(
    body: Column(
      children: [
        Expanded(
          child: AgentConversation(
            workspace: workspace,
            onSuggestion: (_) {},
            onRetry: () {},
          ),
        ),
        Padding(
          padding: const EdgeInsets.all(16),
          child: AgentComposer(
            workspace: workspace,
            embedded: true,
            onSubmit: onSubmit,
            onSearchArea: () {},
            hasSearchArea: false,
          ),
        ),
      ],
    ),
  ),
);

void main() {
  for (final brightness in Brightness.values) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets(
        'Now $brightness ${scale * 100}% preserves long mixed copy and IME send',
        (tester) async {
          _view(tester);
          final workspace = AgentWorkspaceController()
            ..conversation.addAll(const [
              AgentMessage(role: 'user', text: _longQuestion),
              AgentMessage(role: 'assistant', text: _longAnswer),
            ]);
          addTearDown(workspace.dispose);
          final sent = <String>[];
          await tester.pumpWidget(
            _host(
              workspace: workspace,
              brightness: brightness,
              scale: scale,
              onSubmit: sent.add,
            ),
          );
          for (final copy in [_longQuestion, _longAnswer]) {
            final text = tester.widget<Text>(find.text(copy));
            expect(text.style!.fontSize, 15);
            expect(text.style!.fontWeight, FontWeight.w400);
            expect(text.style!.height, 1.5);
            expect(text.maxLines, isNull);
            expect(text.overflow, isNot(TextOverflow.ellipsis));
          }
          final fieldFinder = find.byType(TextField);
          final field = tester.widget<TextField>(fieldFinder);
          expect(field.style!.fontSize, 16);
          expect(field.style!.fontWeight, FontWeight.w400);
          expect(field.style!.height, 1.5);
          final context = tester.element(fieldFinder);
          expect(MediaQuery.textScalerOf(context).scale(16), 16 * scale);
          expect(
            field.decoration!.hintStyle!.color,
            NowUiTokens.reviewedOf(context).metadata.color,
          );
          final editBefore = tester.state<EditableTextState>(
            find.byType(EditableText),
          );
          await tester.enterText(fieldFinder, '阿伯丁 Aberdeen 餐厅');
          tester.view.viewInsets = const FakeViewPadding(bottom: 280);
          await tester.pumpAndSettle();
          final send = find.byTooltip('发送需求');
          expect(send.hitTestable(), findsOneWidget);
          final sendRect = tester.getRect(send);
          expect(sendRect.width, greaterThanOrEqualTo(48));
          expect(sendRect.height, greaterThanOrEqualTo(48));
          expect(sendRect.bottom, lessThanOrEqualTo(500));
          expect(
            tester.state<EditableTextState>(find.byType(EditableText)),
            same(editBefore),
          );
          expect(
            tester.widget<TextField>(fieldFinder).focusNode!.hasFocus,
            isTrue,
          );
          final keyboardTop = tester.getRect(fieldFinder).top;
          await tester.pump();
          expect(tester.getRect(fieldFinder).top, keyboardTop);
          expect(
            tester.widget<TextField>(fieldFinder).controller!.text,
            '阿伯丁 Aberdeen 餐厅',
          );
          expect(workspace.conversation.map((m) => m.text), [
            _longQuestion,
            _longAnswer,
          ]);
          expect(tester.takeException(), isNull);
          await tester.tap(send);
          await tester.pump();
          expect(sent, ['阿伯丁 Aberdeen 餐厅']);
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox.shrink());
        },
      );
    }
  }

  for (final scale in [1.0, 1.3, 2.0]) {
    testWidgets(
      'Now peek ${scale * 100}% header and actions use matching measured roles',
      (tester) async {
        _view(tester);
        final workspace = AgentWorkspaceController()
          ..task = const AgentTask(
            id: 'type-review',
            query: '地点',
            status: 'COMPLETED',
            intent: 'FIND_PLACE',
          )
          ..result = const AgentResult(
            entities: [],
            activities: [],
            places: [],
            note: _longAnswer,
          )
          ..setSheetExtent(AgentSheetExtent.peek);
        addTearDown(workspace.dispose);
        await tester.pumpWidget(
          MaterialApp(
            theme: birdtieTheme(Brightness.light),
            builder: (context, child) => MediaQuery(
              data: MediaQuery.of(
                context,
              ).copyWith(textScaler: TextScaler.linear(scale)),
              child: child!,
            ),
            home: Scaffold(
              body: Align(
                alignment: Alignment.bottomCenter,
                child: AgentResultsSheet(
                  workspace: workspace,
                  availableHeight: 780,
                  onSuggestion: (_) {},
                  onRetry: () {},
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        final title = tester.widget<Text>(find.text('找地点').hitTestable());
        expect(title.style!.fontSize, 18);
        expect(title.style!.fontWeight, FontWeight.w600);
        expect(title.maxLines, isNull);
        final caption = tester.widget<Text>(
          find.text('未找到符合条件的公开内容').hitTestable(),
        );
        expect(caption.style!.fontSize, 13);
        expect(caption.style!.fontWeight, FontWeight.w400);
        final expand = find.byKey(const Key('agent-sheet-expand-summary'));
        expect(expand.hitTestable(), findsOneWidget);
        expect(tester.getSize(expand).height, greaterThanOrEqualTo(48));
        await tester.tap(expand);
        await tester.pumpAndSettle();
        expect(workspace.sheetExtent, AgentSheetExtent.medium);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox.shrink());
      },
    );
  }
}
