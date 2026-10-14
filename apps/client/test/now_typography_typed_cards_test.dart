import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_entity_result_card.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

const _item = AgentResultItem(
  entity: AgentResultRef(type: 'place', id: 'long-place'),
  title: '阿伯丁 Aberdeen 面向朋友聚餐的完整长中文餐厅标题不应强制截断',
  summary: '公开餐厅资料 · Union Street 地址和开放信息需向餐厅核实',
  scope: 'AUTHORIZED_VIEW',
  detail: AgentResultRef(type: 'place', id: 'long-place'),
  share: AgentResultRef(type: 'place', id: 'long-place'),
  anchor: AgentResultAnchor(
    precision: 'point',
    latitude: 57.1497,
    longitude: -2.0943,
  ),
);

void _view(WidgetTester tester) {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = const Size(320, 780);
  addTearDown(tester.view.resetDevicePixelRatio);
  addTearDown(tester.view.resetPhysicalSize);
}

void main() {
  for (final scale in [1.0, 1.3, 2.0]) {
    testWidgets(
      'Now typed place card 320dp ${scale * 100}% uses reviewed roles and original actions',
      (tester) async {
        _view(tester);
        final workspace = AgentWorkspaceController()
          ..task = const AgentTask(
            id: 'now-type',
            query: '地点',
            status: 'COMPLETED',
          )
          ..result = AgentResult(
            entities: const [],
            activities: const [],
            places: const [],
            note: '以下是公开地点资料。',
            resultSet: AgentResultSet(
              id: 'now-type-set',
              status: 'ready',
              entities: [_item.entity],
              items: const [_item],
              generatedAt: DateTime.utc(2026),
            ),
          )
          ..setSheetExtent(AgentSheetExtent.expanded);
        addTearDown(workspace.dispose);
        AgentResultItem? opened, shared;
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
              body: AgentResultsSheet(
                workspace: workspace,
                availableHeight: 780,
                onOpenEntity: (item) => opened = item,
                onShareEntity: (item) => shared = item,
                onSuggestion: (_) {},
                onRetry: () {},
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        final card = find.byType(AgentEntityResultCard);
        final title = tester.widget<Text>(find.text(_item.title));
        expect(title.style!.fontSize, 16);
        expect(title.style!.fontWeight, FontWeight.w500);
        expect(title.style!.height, 1.5);
        expect(title.maxLines, isNull);
        expect(title.overflow, isNot(TextOverflow.ellipsis));
        final subtitle = tester.widget<Text>(
          find.text('地点 · ${_item.summary}'),
        );
        expect(subtitle.style!.fontSize, 13);
        expect(subtitle.style!.fontWeight, FontWeight.w400);
        expect(subtitle.style!.height, 1.5);
        final tokens = NowUiTokens.reviewedOf(tester.element(card));
        expect(title.style!.color, tokens.placeTitle.color);
        expect(subtitle.style!.color, tokens.listSubtitle.color);
        for (final label in ['地图查看', '分享给好友']) {
          final button = find.widgetWithText(TextButton, label);
          await tester.ensureVisible(button);
          await tester.pumpAndSettle();
          expect(button.hitTestable(), findsOneWidget);
          final style = tester
              .widget<TextButton>(button)
              .style!
              .textStyle!
              .resolve({});
          expect(style!.fontSize, 14);
          expect(style.fontWeight, FontWeight.w500);
          expect(tester.getSize(button).width, greaterThanOrEqualTo(48));
          expect(tester.getSize(button).height, greaterThanOrEqualTo(48));
          if (label == '分享给好友') {
            await tester.tap(button);
            expect(shared, same(_item));
          }
        }
        final tile = find.byKey(Key('agent-entity-${_item.entity.mapID}'));
        await tester.ensureVisible(tile);
        await tester.pumpAndSettle();
        expect(tester.getSize(tile).height, greaterThanOrEqualTo(48));
        await tester.tap(tile);
        expect(opened, same(_item));
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox.shrink());
      },
    );
  }

  testWidgets('Now material action body uses reviewed 15 and naturally wraps', (
    tester,
  ) async {
    _view(tester);
    final workspace = AgentWorkspaceController();
    addTearDown(workspace.dispose);
    await tester.pumpWidget(
      MaterialApp(
        theme: birdtieTheme(Brightness.light),
        home: Scaffold(
          body: Align(
            alignment: Alignment.bottomCenter,
            child: AgentComposer(
              workspace: workspace,
              onSubmit: (_) {},
              onSearchArea: () {},
              hasSearchArea: false,
              onTaskImage: () {},
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.byTooltip('添加材料'));
    await tester.pumpAndSettle();
    final copy = tester.widget<Text>(find.text('查看已保存会话图片'));
    expect(copy.style!.fontSize, 15);
    expect(copy.style!.fontWeight, FontWeight.w400);
    expect(copy.style!.height, 1.5);
    expect(copy.maxLines, isNull);
    expect(copy.overflow, isNot(TextOverflow.ellipsis));
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
    'explicit native paste feedback uses metadata 13 and material action body 15',
    (tester) async {
      _view(tester);
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async => null,
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          theme: birdtieTheme(Brightness.light),
          home: Scaffold(
            body: Align(
              alignment: Alignment.bottomCenter,
              child: AgentComposer(
                workspace: workspace,
                onSubmit: (_) {},
                onSearchArea: () {},
                hasSearchArea: false,
                onDraftImage: () {},
              ),
            ),
          ),
        ),
      );
      final composer = tester.state<AgentComposerState>(
        find.byType(AgentComposer),
      );
      await composer.pasteTextFromClipboard();
      final menuClosed = composer.showMaterialTools(composer.context);
      await tester.pumpAndSettle();
      final menu = find.byType(BottomSheet);
      final feedback = tester.widget<Text>(
        find.descendant(of: menu, matching: find.text('剪贴板没有可粘贴的文字或链接。')),
      );
      expect(feedback.style!.fontSize, 13);
      expect(feedback.style!.fontWeight, FontWeight.w400);
      expect(feedback.style!.height, 1.5);
      final description = tester.widget<Text>(
        find.descendant(of: menu, matching: find.text('图片')),
      );
      expect(description.style!.fontSize, 15);
      expect(description.style!.fontWeight, FontWeight.w400);
      expect(description.maxLines, isNull);
      expect(tester.takeException(), isNull);
      Navigator.of(tester.element(menu)).pop();
      await tester.pumpAndSettle();
      await menuClosed;
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
}
