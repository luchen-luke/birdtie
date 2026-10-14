import 'dart:math' as math;
import 'dart:typed_data';

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:birdtie_client/src/workspace/now_workspace_layout.dart';
import 'package:birdtie_client/src/workspace/task_image_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

NowWorkspaceLayout _layout({
  double height = 844,
  double width = 390,
  double keyboard = 0,
  double? result,
  bool content = false,
  bool expanded = false,
  bool editing = false,
  double scale = 1,
}) => NowWorkspaceLayout.resolve(
  rootHeight: height,
  rootWidth: width,
  safeTop: 48,
  safeBottom: 24,
  keyboard: keyboard,
  chromeBottom: 104,
  composerHeight: 56,
  minimumComposerHeight: math.max(56, 24 * scale + 16),
  resultContentHeight: result,
  hasContent: content,
  expanded: expanded,
  editing: editing,
  textScale: scale,
);

// Foundation geometry only. This harness has one real Composer and does not
// replace the product/native MapWorkspace evidence collected by the root.
class _FoundationHost extends StatefulWidget {
  const _FoundationHost({
    required this.workspace,
    required this.scale,
    this.image,
    this.suggestions = const [],
    this.onSubmit,
  });
  final AgentWorkspaceController workspace;
  final double scale;
  final TaskImageDraftController? image;
  final List<String> suggestions;
  final ValueChanged<String>? onSubmit;
  @override
  State<_FoundationHost> createState() => _FoundationHostState();
}

class _FoundationHostState extends State<_FoundationHost> {
  double composerHeight = NowUiTokens.composerMinHeight;
  @override
  Widget build(BuildContext context) => MaterialApp(
    theme: birdtieTheme(Brightness.light),
    builder: (context, child) => MediaQuery(
      data: MediaQuery.of(
        context,
      ).copyWith(textScaler: TextScaler.linear(widget.scale)),
      child: child!,
    ),
    home: Scaffold(
      resizeToAvoidBottomInset: false,
      body: LayoutBuilder(
        builder: (context, bounds) => AnimatedBuilder(
          animation: widget.workspace,
          builder: (context, _) {
            final media = MediaQuery.of(context);
            final minimum = AgentComposer.minimumInputHeightOf(context);
            final layout = NowWorkspaceLayout.resolve(
              rootHeight: bounds.maxHeight,
              rootWidth: bounds.maxWidth,
              safeTop: media.viewPadding.top,
              safeBottom: media.viewPadding.bottom,
              keyboard: media.viewInsets.bottom,
              chromeBottom: media.viewPadding.top + 56,
              composerHeight: composerHeight,
              hasContent: false,
              expanded:
                  widget.workspace.sheetExtent == AgentSheetExtent.expanded,
              editing:
                  widget.workspace.inputFocused || media.viewInsets.bottom > 0,
              textScale: widget.scale,
              minimumComposerHeight: minimum,
            );
            return Stack(
              children: [
                Positioned.fill(child: Container(color: Colors.green)),
                Positioned(
                  left: 0,
                  right: 0,
                  bottom: layout.backgroundBottom,
                  height: layout.surfaceHeight,
                  child: ColoredBox(
                    key: const ValueKey('sheet-background'),
                    color: Theme.of(context).scaffoldBackgroundColor,
                    child: Padding(
                      padding: EdgeInsets.only(
                        bottom: layout.contentSafeBottom,
                      ),
                      child: Padding(
                        padding: EdgeInsets.symmetric(
                          horizontal: NowUiTokens.composerInsetHorizontal,
                          vertical: layout.surfaceInset,
                        ),
                        child: AgentComposer(
                          workspace: widget.workspace,
                          onSubmit: widget.onSubmit ?? (_) {},
                          onSearchArea: () {},
                          hasSearchArea: false,
                          embedded: true,
                          onPasteText: () {},
                          onDraftImage: widget.image == null ? null : () {},
                          imageDraft: widget.image,
                          suggestions: widget.suggestions,
                          onPreferredHeightChanged: (height) {
                            if (mounted &&
                                (height - composerHeight).abs() > .1) {
                              setState(() => composerHeight = height);
                            }
                          },
                        ),
                      ),
                    ),
                  ),
                ),
              ],
            );
          },
        ),
      ),
    ),
  );
}

void _configureView(WidgetTester tester, Size size, {double dpr = 1}) {
  tester.view.devicePixelRatio = dpr;
  tester.view.physicalSize = size * dpr;
  tester.view.viewPadding = FakeViewPadding(top: 48 * dpr, bottom: 24 * dpr);
  tester.view.padding = FakeViewPadding(top: 48 * dpr, bottom: 24 * dpr);
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  addTearDown(tester.view.resetViewInsets);
  addTearDown(tester.view.resetViewPadding);
  addTearDown(tester.view.resetPadding);
}

void _expectOneCompleteLine(WidgetTester tester) {
  final field = tester.widget<TextField>(find.byType(TextField));
  final editable = tester
      .state<EditableTextState>(find.byType(EditableText))
      .renderEditable;
  final padding = field.decoration!.contentPadding!.resolve(TextDirection.ltr);
  expect(field.style!.fontSize, 16);
  expect(field.style!.height, 24 / 16);
  expect(field.decoration!.hintStyle!.height, field.style!.height);
  expect(
    editable.size.height,
    greaterThanOrEqualTo(editable.preferredLineHeight - .01),
  );
  expect(
    tester.getSize(find.byType(TextField)).height,
    greaterThanOrEqualTo(editable.preferredLineHeight + padding.vertical - .01),
  );
  expect(find.byType(TextField), findsOneWidget);
  expect(find.byType(FittedBox), findsNothing);
}

void main() {
  for (final keyboard in [0.0, 10.0, 320.0]) {
    test(
      'background and content consume bottom obstruction once ($keyboard)',
      () {
        final layout = _layout(keyboard: keyboard, editing: keyboard > 0);
        expect(layout.backgroundBottom, keyboard);
        expect(layout.bottom, keyboard);
        expect(layout.contentSafeBottom, math.max(0, 24 - keyboard));
        expect(
          layout.surfaceHeight,
          layout.contentHeight + layout.contentSafeBottom,
        );
        expect(844 - layout.backgroundBottom, 844 - keyboard);
        expect(
          844 - layout.backgroundBottom - layout.contentSafeBottom,
          844 - math.max(keyboard, 24),
        );
      },
    );
  }

  test('empty compact ignores stale expanded flag and measured result', () {
    final compact = _layout(result: 900, expanded: true);
    expect(compact.contentHeight, 56 + 2 * 8);
    expect(compact.surfaceHeight, 56 + 2 * 8 + 24);
    expect(compact.surfaceInset, 8);
  });

  test(
    'measured short result follows natural height without chat placeholder',
    () {
      final empty = _layout(content: true, result: 0);
      final short = _layout(content: true, result: 42);
      final larger = _layout(content: true, result: 108);
      expect(empty.contentHeight, 56 + 2 * 12);
      expect(short.contentHeight - empty.contentHeight, 42);
      expect(larger.contentHeight - short.contentHeight, 66);
    },
  );

  test(
    'unknown and long result use safe ceiling, expanded uses available height',
    () {
      final pending = _layout(content: true);
      final long = _layout(content: true, result: 2000);
      final expanded = _layout(content: true, result: 42, expanded: true);
      expect(pending.contentHeight, pending.maxContentHeight);
      expect(long.contentHeight, long.maxContentHeight);
      expect(long.surfaceHeight, closeTo((844 - 48 - 24) * .4, .001));
      expect(expanded.contentHeight, expanded.availableHeight);
      expect(expanded.contentHeight, greaterThan(long.contentHeight));
    },
  );

  test(
    'large text and landscape do not receive hard portrait reading ceiling',
    () {
      final large = _layout(content: true, result: 440, scale: 2);
      final landscape = _layout(
        height: 390,
        width: 844,
        content: true,
        result: 200,
      );
      expect(large.contentHeight, 64 + 24 + 440);
      expect(large.maxContentHeight, large.availableHeight);
      expect(landscape.maxContentHeight, landscape.availableHeight);
      expect(landscape.contentHeight, landscape.availableHeight);
    },
  );

  for (final scale in [1.0, 1.3, 2.0]) {
    for (final lines in [1, 2, 3, 4]) {
      testWidgets('natural preference unlocks $lines full lines at $scale', (
        tester,
      ) async {
        _configureView(tester, const Size(390, 844));
        final workspace = AgentWorkspaceController();
        final sent = <String>[];
        await tester.pumpWidget(
          _FoundationHost(
            workspace: workspace,
            scale: scale,
            onSubmit: sent.add,
          ),
        );
        await tester.pumpAndSettle();
        final composer = tester.state<AgentComposerState>(
          find.byType(AgentComposer),
        );
        final draft = List.filled(lines, '原草稿').join('\n');
        composer.fillDraft(draft);
        await tester.pumpAndSettle();
        final editable = tester
            .state<EditableTextState>(find.byType(EditableText))
            .renderEditable;
        final natural = math.max(
          56.0,
          editable.preferredLineHeight * lines + 16,
        );
        expect(
          tester.getSize(find.byType(AgentComposer)).height,
          closeTo(natural, 1),
        );
        expect(
          (editable.offset as ScrollPosition).maxScrollExtent,
          lessThanOrEqualTo(1),
        );
        _expectOneCompleteLine(tester);
        expect(composer.draftText, draft);
        await tester.tap(find.byTooltip('发送需求'));
        await tester.pumpAndSettle();
        expect(sent, [draft]);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        workspace.dispose();
      });
    }
  }

  testWidgets(
    'preferred measurement is independent of a currently clamped input',
    (tester) async {
      _configureView(tester, const Size(390, 844));
      final workspace = AgentWorkspaceController();
      double? actual, preferred;
      await tester.pumpWidget(
        MaterialApp(
          theme: birdtieTheme(Brightness.light),
          home: MediaQuery(
            data: const MediaQueryData(textScaler: TextScaler.linear(2)),
            child: Scaffold(
              body: Center(
                child: SizedBox(
                  width: 358,
                  height: 64,
                  child: AgentComposer(
                    workspace: workspace,
                    onSubmit: (_) {},
                    onSearchArea: () {},
                    hasSearchArea: false,
                    embedded: true,
                    onPasteText: () {},
                    onHeightChanged: (height) => actual = height,
                    onPreferredHeightChanged: (height) => preferred = height,
                  ),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      tester
          .state<AgentComposerState>(find.byType(AgentComposer))
          .fillDraft('第一行\n第二行\n第三行\n第四行');
      await tester.pumpAndSettle();
      expect(actual, 64);
      expect(preferred, 4 * 48 + 16);
      _expectOneCompleteLine(tester);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      workspace.dispose();
    },
  );

  testWidgets(
    'scaled suggestions measure their own line and yield during editing',
    (tester) async {
      _configureView(tester, const Size(320, 568));
      final workspace = AgentWorkspaceController();
      await tester.pumpWidget(
        _FoundationHost(
          workspace: workspace,
          scale: 2,
          suggestions: const ['普通任务'],
        ),
      );
      await tester.pumpAndSettle();
      final chip = find.byType(ActionChip);
      expect(chip, findsOneWidget);
      expect(tester.getSize(chip).height, greaterThanOrEqualTo(48));
      expect(tester.widget<Text>(find.text('普通任务')).style!.height, 1.5);
      final composer = tester.state<AgentComposerState>(
        find.byType(AgentComposer),
      );
      composer.fillDraft('新草稿');
      await tester.pumpAndSettle();
      expect(find.byType(ActionChip), findsNothing);
      _expectOneCompleteLine(tester);
      expect(composer.draftText, '新草稿');
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      workspace.dispose();
    },
  );

  testWidgets(
    'local image row respects scaled action text and yields to short IME',
    (tester) async {
      const dpr = 3.25;
      _configureView(tester, const Size(2656 / dpr, 1220 / dpr), dpr: dpr);
      tester.view.viewPadding = const FakeViewPadding(top: 150, bottom: 78);
      tester.view.padding = const FakeViewPadding(top: 150, bottom: 78);
      final workspace = AgentWorkspaceController();
      final boundary = ValueNotifier(false);
      // Local state fixture only; no upload, decode, external authentication or
      // native image flow is claimed by this layout regression.
      final image = TaskImageDraftController(
        authorizationHeader: () => 'Bearer local-test-fixture',
        ownerID: () => '11111111-1111-4111-8111-111111111111',
        cityID: () => 'fixture-city',
        sourceCurrent: () => true,
        sourceChanges: boundary,
      )..localBytes = Uint8List.fromList([137, 80, 78, 71]);
      await tester.pumpWidget(
        _FoundationHost(workspace: workspace, scale: 2, image: image),
      );
      await tester.pumpAndSettle();
      final composer = tester.state<AgentComposerState>(
        find.byType(AgentComposer),
      );
      composer.fillDraft('原材料草稿');
      await tester.pumpAndSettle();
      final label = find.text('图片待检查 · 仅本机');
      expect(label, findsOneWidget);
      final text = tester.widget<Text>(label);
      expect(text.style!.height, 1.5);
      expect(tester.getSize(label).height, greaterThanOrEqualTo(40));
      tester.view.viewInsets = const FakeViewPadding(bottom: 1220 - 379);
      await tester.pumpAndSettle();
      expect(label, findsNothing);
      _expectOneCompleteLine(tester);
      expect(composer.draftText, '原材料草稿');
      expect(image.current, isTrue);
      expect(image.localBytes, isNotNull);
      expect(find.byTooltip('发送需求').hitTestable(), findsOneWidget);
      tester.view.viewInsets = const FakeViewPadding();
      await tester.pumpAndSettle();
      expect(label, findsOneWidget);
      expect(
        tester.state<AgentComposerState>(find.byType(AgentComposer)),
        same(composer),
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      image.dispose();
      boundary.dispose();
      workspace.dispose();
    },
  );

  for (final size in [
    const Size(320, 568),
    const Size(390, 844),
    const Size(430, 932),
  ]) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets(
        'one scaled composer and full-width background $size × $scale',
        (tester) async {
          _configureView(tester, size);
          final workspace = AgentWorkspaceController();
          await tester.pumpWidget(
            _FoundationHost(workspace: workspace, scale: scale),
          );
          await tester.pumpAndSettle();
          final composer = tester.state<AgentComposerState>(
            find.byType(AgentComposer),
          );
          composer.fillDraft('保留中英文 Address 14');
          await tester.pumpAndSettle();
          _expectOneCompleteLine(tester);
          final background = tester.getRect(
            find.byKey(const ValueKey('sheet-background')),
          );
          expect(background.left, 0);
          expect(background.right, size.width);
          expect(background.bottom, size.height);
          expect(tester.getRect(find.byType(AgentComposer)).left, 16);
          expect(
            tester.getRect(find.byType(AgentComposer)).right,
            size.width - 16,
          );
          for (var cycle = 0; cycle < 2; cycle++) {
            tester.view.viewInsets = FakeViewPadding(bottom: size.height * .42);
            await tester.pumpAndSettle();
            _expectOneCompleteLine(tester);
            expect(
              tester
                  .getRect(find.byKey(const ValueKey('sheet-background')))
                  .bottom,
              closeTo(size.height * .58, .001),
            );
            expect(composer.draftText, '保留中英文 Address 14');
            expect(
              tester.state<AgentComposerState>(find.byType(AgentComposer)),
              same(composer),
            );
            final send = find.byTooltip('发送需求');
            expect(send.hitTestable(), findsOneWidget);
            expect(tester.getSize(send).height, greaterThanOrEqualTo(48));
            expect(tester.takeException(), isNull);
            tester.view.viewInsets = const FakeViewPadding();
            await tester.pumpAndSettle();
            expect(
              tester
                  .getRect(find.byKey(const ValueKey('sheet-background')))
                  .bottom,
              size.height,
            );
          }
          await tester.pumpWidget(const SizedBox());
          workspace.dispose();
        },
      );
    }
  }

  for (final scale in [1.3, 2.0]) {
    testWidgets(
      'short physical landscape IME keeps full line and scrolls at $scale',
      (tester) async {
        const dpr = 3.25;
        _configureView(tester, const Size(2656 / dpr, 1220 / dpr), dpr: dpr);
        tester.view.viewPadding = const FakeViewPadding(top: 150, bottom: 78);
        tester.view.padding = const FakeViewPadding(top: 150, bottom: 78);
        final workspace = AgentWorkspaceController();
        await tester.pumpWidget(
          _FoundationHost(workspace: workspace, scale: scale),
        );
        await tester.pumpAndSettle();
        final composer = tester.state<AgentComposerState>(
          find.byType(AgentComposer),
        );
        composer.fillDraft('保留第一行\n第二行\n第三行\n第四行');
        await tester.pumpAndSettle();
        for (var cycle = 0; cycle < 2; cycle++) {
          tester.view.viewInsets = const FakeViewPadding(bottom: 1220 - 379);
          await tester.pumpAndSettle();
          _expectOneCompleteLine(tester);
          final editable = tester
              .state<EditableTextState>(find.byType(EditableText))
              .renderEditable;
          expect(
            (editable.offset as ScrollPosition).maxScrollExtent,
            greaterThan(0),
          );
          final send = find.byTooltip('发送需求');
          expect(send.hitTestable(), findsOneWidget);
          expect(tester.getSize(send).height, greaterThanOrEqualTo(48));
          expect(tester.getBottomRight(send).dy, lessThanOrEqualTo(379 / dpr));
          expect(composer.draftText, '保留第一行\n第二行\n第三行\n第四行');
          expect(
            tester.state<AgentComposerState>(find.byType(AgentComposer)),
            same(composer),
          );
          expect(tester.takeException(), isNull);
          tester.view.viewInsets = const FakeViewPadding();
          await tester.pumpAndSettle();
        }
        await tester.pumpWidget(const SizedBox());
        workspace.dispose();
      },
    );
  }
}
