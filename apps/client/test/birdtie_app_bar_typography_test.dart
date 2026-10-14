import 'package:birdtie_client/src/app/birdtie_app_bar.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';

const _longTitle = '阿伯丁 Aberdeen 长中文社群标题';
const _titleKey = Key('app-bar-title');
const _backKey = Key('app-bar-back');
const _actionKey = Key('app-bar-action');
const _bodyKey = Key('body-action');

Future<void> _pump(
  WidgetTester tester, {
  Brightness brightness = Brightness.light,
  double scale = 1,
  String title = _longTitle,
  double keyboard = 0,
  TextDirection direction = TextDirection.ltr,
  VoidCallback? onBack,
  VoidCallback? onAction,
  VoidCallback? onBody,
}) async {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = const Size(320, 780);
  await tester.pumpWidget(
    MaterialApp(
      theme: birdtieTheme(brightness),
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(context).copyWith(
          textScaler: TextScaler.linear(scale),
          viewInsets: EdgeInsets.only(bottom: keyboard),
        ),
        child: Directionality(textDirection: direction, child: child!),
      ),
      home: Builder(
        builder: (context) => Scaffold(
          appBar: birdtieAppBar(
            context,
            title: Text(
              title,
              key: _titleKey,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
            leading: IconButton(
              key: _backKey,
              onPressed: onBack ?? () {},
              tooltip: '返回',
              icon: const Icon(Icons.arrow_back),
            ),
            actions: [
              IconButton(
                key: _actionKey,
                onPressed: onAction ?? () {},
                tooltip: '刷新',
                icon: const Icon(Icons.refresh),
              ),
            ],
          ),
          body: ListView(
            children: [
              const Text('正文自然滚动，导航栏保留原操作。'),
              FilledButton(
                key: _bodyKey,
                onPressed: onBody ?? () {},
                child: const Text('正文操作'),
              ),
              for (var i = 0; i < 30; i++) Text('长中文正文第 $i 段，混排 Birdtie。'),
            ],
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void _titleChecks(WidgetTester tester, String value, double scale) {
  final text = tester.widget<Text>(find.byKey(_titleKey));
  expect(text.data, value);
  expect(text.style!.fontSize, 20);
  expect(text.style!.fontWeight, FontWeight.w600);
  expect(text.style!.height, 1.4);
  expect(text.maxLines, isNull);
  expect(text.softWrap, isTrue);
  expect(text.overflow, TextOverflow.visible);
  final paragraph = tester.renderObject<RenderParagraph>(find.byKey(_titleKey));
  expect(paragraph.textScaler.scale(20), closeTo(20 * scale, .001));
  expect(
    MediaQuery.textScalerOf(tester.element(find.byKey(_titleKey))).scale(20),
    closeTo(20 * scale, .001),
  );
  expect(tester.getSize(find.byKey(_titleKey)).width, lessThanOrEqualTo(184));
  expect(tester.takeException(), isNull);
}

void main() {
  for (final brightness in Brightness.values) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets('${brightness.name} 320dp long mixed title ${scale * 100}%', (
        tester,
      ) async {
        addTearDown(tester.view.resetDevicePixelRatio);
        addTearDown(tester.view.resetPhysicalSize);
        var back = 0, action = 0;
        await _pump(
          tester,
          brightness: brightness,
          scale: scale,
          onBack: () => back++,
          onAction: () => action++,
        );
        _titleChecks(tester, _longTitle, scale);
        final bar = tester.widget<AppBar>(find.byType(AppBar));
        final titleHeight = tester.getSize(find.byKey(_titleKey)).height;
        expect(bar.toolbarHeight, greaterThanOrEqualTo(titleHeight + 16));
        expect(find.byType(Scrollable), findsOneWidget);
        expect(
          find.byKey(const Key('birdtie-app-bar-title-scroll')),
          findsNothing,
        );
        for (final key in [_backKey, _actionKey]) {
          expect(find.byKey(key).hitTestable(), findsOneWidget);
          final size = tester.getSize(find.byKey(key));
          expect(size.width, greaterThanOrEqualTo(48));
          expect(size.height, greaterThanOrEqualTo(48));
        }
        await tester.tap(find.byKey(_backKey));
        await tester.tap(find.byKey(_actionKey));
        expect(back, 1);
        expect(action, 1);
      });
    }
  }

  for (final keyboard in [0.0, 300.0]) {
    testWidgets(
      'very long 200% title scrolls to end, with keyboard $keyboard',
      (tester) async {
        addTearDown(tester.view.resetDevicePixelRatio);
        addTearDown(tester.view.resetPhysicalSize);
        final title = '${List.filled(6, _longTitle).join(' · ')} 这是标题末尾 END';
        var back = 0, action = 0, body = 0;
        await _pump(
          tester,
          scale: 2,
          title: title,
          keyboard: keyboard,
          onBack: () => back++,
          onAction: () => action++,
          onBody: () => body++,
        );
        _titleChecks(tester, title, 2);
        final bar = tester.widget<AppBar>(find.byType(AppBar));
        expect(bar.toolbarHeight, closeTo((780 - keyboard) * .4, .001));
        final viewport = find.byKey(const Key('birdtie-app-bar-title-scroll'));
        final scrollable = find.descendant(
          of: viewport,
          matching: find.byType(Scrollable),
        );
        final state = tester.state<ScrollableState>(scrollable);
        expect(state.position.maxScrollExtent, greaterThan(0));
        await tester.drag(viewport, const Offset(0, -6000));
        await tester.pumpAndSettle();
        expect(
          state.position.pixels,
          closeTo(state.position.maxScrollExtent, .001),
        );
        final paragraph = tester.renderObject<RenderParagraph>(
          find.byKey(_titleKey),
        );
        final end = paragraph
            .getBoxesForSelection(
              TextSelection(
                baseOffset: title.length - 3,
                extentOffset: title.length,
              ),
            )
            .last;
        final endPoint = paragraph.localToGlobal(
          Offset(end.left, end.bottom - 1),
        );
        expect(tester.getRect(viewport).contains(endPoint), isTrue);
        for (final key in [_backKey, _actionKey, _bodyKey]) {
          expect(find.byKey(key).hitTestable(), findsOneWidget);
          await tester.tap(find.byKey(key));
        }
        expect((back, action, body), (1, 1, 1));
        expect(tester.takeException(), isNull);
      },
    );
  }

  testWidgets('automatic BackButton preserves pop and popup action', (
    tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(320, 780);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);
    var selected = 0;
    await tester.pumpWidget(
      MaterialApp(
        theme: birdtieTheme(Brightness.dark),
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (context) => Scaffold(
                    appBar: birdtieAppBar(
                      context,
                      title: const Text(_longTitle),
                      actions: [
                        PopupMenuButton<String>(
                          tooltip: '原安全操作',
                          onSelected: (_) => selected++,
                          itemBuilder: (_) => const [
                            PopupMenuItem(value: 'check', child: Text('原菜单项')),
                          ],
                        ),
                      ],
                    ),
                    body: const Text('原详情内容'),
                  ),
                ),
              ),
              child: const Text('打开'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('打开'));
    await tester.pumpAndSettle();
    expect(find.byType(BackButton).hitTestable(), findsOneWidget);
    expect(
      tester.getSize(find.byType(BackButton)).shortestSide,
      greaterThanOrEqualTo(48),
    );
    await tester.tap(find.byTooltip('原安全操作'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('原菜单项'));
    await tester.pumpAndSettle();
    expect(selected, 1);
    await tester.tap(find.byType(BackButton));
    await tester.pumpAndSettle();
    expect(find.text('打开'), findsOneWidget);
  });

  testWidgets('200% RTL preserves title and actions', (tester) async {
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);
    await _pump(tester, scale: 2, direction: TextDirection.rtl);
    _titleChecks(tester, _longTitle, 2);
    expect(find.byKey(_backKey).hitTestable(), findsOneWidget);
    expect(find.byKey(_actionKey).hitTestable(), findsOneWidget);
  });
}
