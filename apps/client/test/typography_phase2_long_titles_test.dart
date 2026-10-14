import 'dart:io';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/content/moment_context_choice.dart';
import 'package:birdtie_client/src/workspace/active_social_intent_card.dart';
import 'package:birdtie_client/src/workspace/active_social_intent_api.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';

import 'active_social_intent_api_test.dart'
    show nowEnvelope, nowItem, nowOptions, nowResponse, nowOwner;
import 'model_egress_page_test.dart' show egressAuth;

const _first = '阿伯丁 Aberdeen 的中文与 English 周末交流和餐厅探访，保留地点与活动完整名称供本人检查';
const _second = '第二个中文与 English 交流地点及活动安排，长名称也应完整换行并能重新选择';
const _location = '阿伯丁 Aberdeen 的完整中文与 English 区域说明，保留长地点名称和本人检查所需的信息';
const _firstID = '22222222-2222-4222-8222-222222222222';
const _secondID = '44444444-4444-4444-8444-444444444444';
const _boundary = Key('long-title-widget-fixture');
String? _font;

ThemeData _theme(Brightness brightness) {
  final original = birdtieTheme(brightness);
  if (_font == null) return original;
  final theme = original.copyWith(
    textTheme: original.textTheme.apply(fontFamily: _font),
  );
  final text = NowUiTokens.fromTheme(theme);
  return theme.copyWith(
    textButtonTheme: TextButtonThemeData(
      style: original.textButtonTheme.style?.copyWith(
        textStyle: WidgetStatePropertyAll(
          text.action.copyWith(fontFamily: _font),
        ),
      ),
    ),
    extensions: [
      ...theme.extensions.values.where((e) => e is! NowUiTokens),
      text,
    ],
  );
}

Widget _host(Widget body, Brightness brightness, double scale) => MaterialApp(
  theme: _theme(brightness),
  builder: (context, child) => RepaintBoundary(
    key: _boundary,
    child: Column(
      children: [
        Material(
          child: Text(
            'WIDGET FIXTURE / NOT PHONE / ${brightness.name} / ${(scale * 100).round()}%',
            style: const TextStyle(fontSize: 11),
          ),
        ),
        Expanded(
          child: MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: TextScaler.linear(scale)),
            child: child!,
          ),
        ),
      ],
    ),
  ),
  home: Scaffold(
    body: SingleChildScrollView(padding: const EdgeInsets.all(16), child: body),
  ),
);

void _view(WidgetTester tester) {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = const Size(320, 800);
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
}

Map<String, Object?> _metrics(WidgetTester tester, Finder finder) {
  final paragraph = tester.renderObject<RenderParagraph>(finder);
  final widget = tester.widget<Text>(finder);
  final painter = TextPainter(
    text: paragraph.text,
    textScaler: paragraph.textScaler,
    textDirection: paragraph.textDirection,
  )..layout(maxWidth: paragraph.size.width);
  final result = <String, Object?>{
    'size': paragraph.text.style?.fontSize,
    'weight': paragraph.text.style?.fontWeight,
    'height': paragraph.text.style?.height,
    'scaled': paragraph.textScaler.scale(paragraph.text.style!.fontSize!),
    'overflow': paragraph.overflow,
    'maxLines': widget.maxLines,
    'didExceed': paragraph.didExceedMaxLines,
    'renderHeight': paragraph.size.height,
    'naturalHeight': painter.height,
    'naturalLines': painter.computeLineMetrics().length,
  };
  painter.dispose();
  return result;
}

void _complete(
  Map<String, Object?> metric,
  double size,
  FontWeight weight,
  double scale,
) {
  expect(metric['size'], size);
  expect(metric['weight'], weight);
  expect(metric['height'], 1.5);
  expect(metric['scaled'], size * scale);
  expect(metric['overflow'], isNot(TextOverflow.ellipsis));
  expect(metric['maxLines'], isNull);
  expect(metric['didExceed'], false);
  expect(metric['naturalLines'], greaterThan(1));
  expect(
    metric['renderHeight'] as double,
    greaterThanOrEqualTo((metric['naturalHeight'] as double) - 1e-6),
  );
}

void _hit48(WidgetTester tester, Finder finder) {
  final size = tester.getSize(finder);
  expect(size.width, greaterThanOrEqualTo(48 - 1e-6));
  expect(size.height, greaterThanOrEqualTo(48 - 1e-6));
}

Future<void> _capture(
  WidgetTester tester,
  String name,
  Brightness brightness,
  double scale,
) async {
  final output =
      Platform.environment['BIRDTIE_TYPOGRAPHY_LONG_TITLE_SCREENSHOTS'];
  if (output == null ||
      !(brightness == Brightness.light && scale == 1 ||
          brightness == Brightness.dark && scale == 2)) {
    return;
  }
  await tester.runAsync(() async {
    final image = await tester
        .renderObject<RenderRepaintBoundary>(find.byKey(_boundary))
        .toImage(pixelRatio: 1);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    final path = File(
      '$output/WIDGET_FIXTURE_NOT_PHONE_${brightness.name}_${(scale * 100).round()}_$name.png',
    );
    await path.parent.create(recursive: true);
    await path.writeAsBytes(bytes!.buffer.asUint8List());
    image.dispose();
  });
}

Finder _menuItem(String title) => find.ancestor(
  of: find.text(title).last,
  matching: find.byType(DropdownMenuItem<String>),
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    final file = Platform.environment['BIRDTIE_TYPOGRAPHY_FIXTURE_FONT'];
    if (file == null) return;
    await (FontLoader('BirdtieLongTitleFixtureCJK')..addFont(
          Future.value(ByteData.sublistView(File(file).readAsBytesSync())),
        ))
        .load();
    _font = 'BirdtieLongTitleFixtureCJK';
    await (FontLoader(
      'MaterialIcons',
    )..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'))).load();
  });
  for (final brightness in Brightness.values) {
    for (final scale in [1.0, 1.3, 2.0]) {
      final name =
          '${brightness.name} ${(scale * 100).round()}% 320 ChineseEnglish';
      testWidgets('草稿关联 $name 完整selected/menu、48与原选择回调', (tester) async {
        _view(tester);
        var selected = _firstID;
        final callbacks = <String>[];
        await tester.pumpWidget(
          _host(
            StatefulBuilder(
              builder: (context, setState) => MomentContextChoice(
                label: '地点',
                value: selected,
                available: const {_firstID: _first, _secondID: _second},
                onChanged: (value) => setState(() {
                  selected = value;
                  callbacks.add(value);
                }),
              ),
            ),
            brightness,
            scale,
          ),
        );
        await tester.pumpAndSettle();
        final dropdown = find.byType(DropdownButton<String>);
        final before = _metrics(tester, find.text(_first));
        final longHeight = tester.getSize(dropdown).height;
        await _capture(tester, 'context_selected_first', brightness, scale);
        await tester.ensureVisible(dropdown);
        await tester.tap(dropdown);
        await tester.pumpAndSettle();
        final menuFirst = _metrics(tester, find.text(_first).last);
        await _capture(tester, 'context_open_menu', brightness, scale);
        await tester.ensureVisible(find.text(_second).last);
        final menuSecond = _metrics(tester, find.text(_second).last);
        final menuSize = tester.getSize(_menuItem(_second));
        await tester.tap(find.text(_second).last);
        await tester.pumpAndSettle();
        final after = _metrics(tester, find.text(_second));
        await _capture(tester, 'context_selected_second', brightness, scale);
        await tester.ensureVisible(dropdown);
        await tester.tap(dropdown);
        await tester.pumpAndSettle();
        await tester.ensureVisible(find.text('不关联').last);
        await tester.tap(find.text('不关联').last);
        await tester.pumpAndSettle();
        await _capture(tester, 'context_cleared_short', brightness, scale);
        expect(selected, '');
        expect(callbacks, [_secondID, '']);
        expect(menuSize.height, greaterThanOrEqualTo(48 - 1e-6));
        expect(menuSize.width, greaterThanOrEqualTo(48 - 1e-6));
        _hit48(tester, dropdown);
        expect(tester.getSize(dropdown).height, lessThan(longHeight));
        for (final metric in [before, menuFirst, menuSecond, after]) {
          _complete(metric, 15, FontWeight.w400, scale);
        }
        expect(
          tester.widget<DropdownButton<String>>(dropdown).itemHeight,
          isNull,
        );
        expect(tester.takeException(), isNull);
      });
      testWidgets('意图卡片 $name 完整selected/menu、48及选择不提交', (tester) async {
        _view(tester);
        final auth = await egressAuth();
        final methods = <String>[];
        Map<String, dynamic> row(String id, String title) {
          final item = nowItem();
          final intent = item['intent'] as Map<String, dynamic>;
          intent['id'] = id;
          intent['title'] = title;
          item['locationLabel'] = _location;
          return item;
        }

        final list = {
          ...nowEnvelope(),
          'items': [row(_firstID, _first), row(_secondID, _second)],
          'limit': 100,
          'truncated': false,
        };
        ActiveIntentList(list, nowOwner);

        final client = MockClient((request) async {
          methods.add(request.method);
          return nowResponse(
            request.url.path.endsWith('/options') ? nowOptions() : list,
          );
        });
        addTearDown(() async {
          await tester.pumpWidget(const SizedBox());
          auth.dispose();
          client.close();
        });
        await tester.pumpWidget(
          _host(
            ActiveSocialIntentCard(auth: auth, client: client, onClose: () {}),
            brightness,
            scale,
          ),
        );
        await tester.pumpAndSettle();
        final dropdown = find.byType(DropdownButton<String>);
        final emptyHeight = tester.getSize(dropdown).height;
        await tester.tap(dropdown);
        await tester.pumpAndSettle();
        final menuFirst = _metrics(tester, find.text(_first).last);
        await _capture(tester, 'intent_open_menu', brightness, scale);
        await tester.ensureVisible(find.text(_first).last);
        final menuSize = tester.getSize(_menuItem(_first));
        await tester.tap(find.text(_first).last);
        await tester.pumpAndSettle();
        final selected = _metrics(tester, find.text(_first));
        final selectedHeight = tester.getSize(dropdown).height;
        final location = _metrics(tester, find.text(_location));
        await _capture(tester, 'intent_selected_first', brightness, scale);
        await tester.ensureVisible(dropdown);
        await tester.tap(dropdown);
        await tester.pumpAndSettle();
        await tester.ensureVisible(find.text(_second).last);
        final menuSecond = _metrics(tester, find.text(_second).last);
        await tester.tap(find.text(_second).last);
        await tester.pumpAndSettle();
        final second = _metrics(tester, find.text(_second));
        await _capture(tester, 'intent_selected_second', brightness, scale);
        await tester.ensureVisible(find.text('重置本地选择'));
        final reset = find.widgetWithText(TextButton, '重置本地选择');
        final resetSize = tester.getSize(reset);
        await tester.tap(reset);
        await tester.pumpAndSettle();
        await _capture(tester, 'intent_reset', brightness, scale);
        expect(find.text('选择本轮意图'), findsOneWidget);
        expect(find.text(_location), findsNothing);
        expect(methods, isNotEmpty);
        expect(methods.every((method) => method == 'GET'), isTrue);
        expect(emptyHeight, lessThan(selectedHeight));
        expect(tester.getSize(dropdown).height, closeTo(emptyHeight, 1e-6));
        expect(menuSize.height, greaterThanOrEqualTo(48 - 1e-6));
        expect(menuSize.width, greaterThanOrEqualTo(48 - 1e-6));
        expect(resetSize.height, greaterThanOrEqualTo(48 - 1e-6));
        _hit48(tester, dropdown);
        for (final metric in [menuFirst, selected, menuSecond, second]) {
          _complete(metric, 16, FontWeight.w500, scale);
        }
        _complete(location, 15, FontWeight.w400, scale);
        expect(
          tester.widget<DropdownButton<String>>(dropdown).itemHeight,
          isNull,
        );
        expect(tester.takeException(), isNull);
      });
    }
  }
}
