import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'place_history_controller_test.dart' show historyTestData;

const _id = '22222222-2222-4222-8222-222222222222';
const _momentID = '11111111-1111-4111-8111-111111111111';
const _name = '阿伯丁 Aberdeen 与朋友一起探索完整中文名称';
const _body =
    '这是用于组件验收的合成中文正文，混排 Union Street 和 Birdtie。文字应该自然换行，时间与来源帮助理解内容，不能把公开分享当作已核验到访。';
const _captureKey = ValueKey('public-panels-typography-capture');

http.Response _json(Object data, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);

MockClient _source() => MockClient((request) async {
  if (request.url.path.contains('/accounts/')) {
    return _json({
      'accountId': _id,
      'displayName': _name,
      'bio': _body,
      'visibility': 'public',
    });
  }
  if (request.url.path.contains('/moments/')) {
    return _json({
      'schemaVersion': 'public-moment-v1',
      'id': _momentID,
      'title': _name,
      'body': _body,
      'placeId': _id,
      'placeName': _name,
      'cityId': 'aberdeen-gb',
      'revision': 2,
      'publishedAt': '2026-10-03T08:00:00.123456Z',
    });
  }
  if (request.url.path.endsWith('/venue')) return _json({}, 404);
  if (request.url.path.endsWith('/activities') ||
      request.url.path == '/v1/me/moments') {
    return _json([]);
  }
  if (request.url.path.endsWith('/social-history')) {
    return _json(historyTestData());
  }
  return _json({
    'id': _id,
    'name': _name,
    'summary': _body,
    'addressLabel': 'Aberdeen Union Street 123 · 合成公开地址',
    'source': {'label': '组件验收合成公开资料', 'freshness': 'unverified'},
    'location': {'precision': 'none'},
  });
});

Future<void> _font(WidgetTester tester) async {
  if (Platform.environment['BIRDTIE_PUBLIC_PANELS_CAPTURE_DIR'] == null) return;
  await tester.runAsync(() async {
    await (FontLoader('PublicPanelsFixtureCJK')..addFont(
          File(
            'C:/Windows/Fonts/msyh.ttc',
          ).readAsBytes().then(ByteData.sublistView),
        ))
        .load();
    if (Platform.environment['BIRDTIE_PUBLIC_PANELS_LOAD_ICONS'] == '1') {
      await (FontLoader('MaterialIcons')..addFont(
            File(
              'D:/DevTools/flutter/bin/cache/artifacts/material_fonts/materialicons-regular.otf',
            ).readAsBytes().then(ByteData.sublistView),
          ))
          .load();
    }
  });
}

Future<void> _capture(WidgetTester tester, String name) async {
  final directory = Platform.environment['BIRDTIE_PUBLIC_PANELS_CAPTURE_DIR'];
  if (directory == null) return;
  final boundary = tester.renderObject<RenderRepaintBoundary>(
    find.byKey(_captureKey),
  );
  await tester.runAsync(() async {
    final image = await boundary.toImage(pixelRatio: 2);
    try {
      final pixels = await image.toByteData(format: ui.ImageByteFormat.png);
      if (pixels == null) throw StateError('capture pixels unavailable');
      await Directory(directory).create(recursive: true);
      await File(
        '$directory/$name.png',
      ).writeAsBytes(pixels.buffer.asUint8List());
    } finally {
      image.dispose();
    }
  });
}

void main() {
  final before = Platform.environment['BIRDTIE_PUBLIC_PANELS_BEFORE'] == '1';
  for (final page in ['moment', 'person', 'place']) {
    for (final brightness in Brightness.values) {
      for (final scale in [1.0, 1.3, 2.0]) {
        testWidgets(
          '$page ${brightness.name} ${scale * 100}% long mixed public content',
          (tester) async {
            tester.view.devicePixelRatio = 1;
            tester.view.physicalSize = const Size(320, 780);
            addTearDown(tester.view.resetDevicePixelRatio);
            addTearDown(tester.view.resetPhysicalSize);
            await _font(tester);
            final source = _source();
            addTearDown(source.close);
            Widget child;
            var opened = 0;
            switch (page) {
              case 'moment':
                child = PublicMomentPage(
                  momentID: _momentID,
                  authorizationHeader: () => null,
                  client: source,
                  apiBaseUrl: 'http://fixture.test',
                  onOpenPlace: (_) => opened++,
                );
              case 'person':
                child = PublicPersonPage(
                  accountID: _id,
                  authorizationHeader: () => null,
                  client: source,
                  apiBaseUrl: 'http://fixture.test',
                  onOpenPublicMoments: () async {
                    opened++;
                  },
                );
              default:
                child = Scaffold(
                  body: PlaceDetailSheet(
                    placeID: _id,
                    authorizationHeader: () => null,
                    client: source,
                    apiBaseUrl: 'http://fixture.test',
                    onOpenActivity: (_) {},
                  ),
                );
            }
            var theme = birdtieTheme(brightness);
            if (Platform.environment['BIRDTIE_PUBLIC_PANELS_CAPTURE_DIR'] !=
                null) {
              TextStyle fixtureFont(TextStyle style) =>
                  style.copyWith(fontFamily: 'PublicPanelsFixtureCJK');
              final tokens = NowUiTokens.fromTheme(theme);
              theme = theme.copyWith(
                textTheme: theme.textTheme.apply(
                  fontFamily: 'PublicPanelsFixtureCJK',
                ),
                extensions: [
                  ...theme.extensions.values.where(
                    (extension) => extension is! NowUiTokens,
                  ),
                  tokens.copyWith(
                    sheetTitle: fixtureFont(tokens.sheetTitle),
                    placeTitle: fixtureFont(tokens.placeTitle),
                    chrome: fixtureFont(tokens.chrome),
                    body: fixtureFont(tokens.body),
                    user: fixtureFont(tokens.user),
                    composer: fixtureFont(tokens.composer),
                    address: fixtureFont(tokens.address),
                    action: fixtureFont(tokens.action),
                    listSubtitle: fixtureFont(tokens.listSubtitle),
                    metadata: fixtureFont(tokens.metadata),
                  ),
                ],
              );
            }
            await tester.pumpWidget(
              MaterialApp(
                theme: theme,
                builder: (context, child) => MediaQuery(
                  data: MediaQuery.of(
                    context,
                  ).copyWith(textScaler: TextScaler.linear(scale)),
                  child: child!,
                ),
                home: RepaintBoundary(key: _captureKey, child: child),
              ),
            );
            await tester.pumpAndSettle();
            expect(find.text(_name), findsOneWidget);
            expect(find.text(_body), findsOneWidget);
            final title = tester.widget<Text>(find.text(_name));
            expect(title.maxLines, isNull);
            expect(title.overflow, isNot(TextOverflow.ellipsis));
            if (!before) {
              final tokens = NowUiTokens.of(tester.element(find.text(_name)));
              expect(title.style!.fontSize, page == 'place' ? 18 : 20);
              expect(title.style!.fontWeight, FontWeight.w600);
              final body = tester.widget<Text>(find.text(_body));
              expect(body.style!.fontSize, 15);
              expect(body.style!.fontWeight, FontWeight.w400);
              expect(body.style!.height, 1.5);
              expect(body.style!.color, tokens.body.color);
              if (page != 'place') {
                final navigationTitle = find.descendant(
                  of: find.byType(AppBar),
                  matching: find.byType(Text),
                );
                expect(navigationTitle, findsOneWidget);
                final navigationText = tester.widget<Text>(navigationTitle);
                expect(navigationText.style!.fontSize, 20);
                expect(navigationText.style!.fontWeight, FontWeight.w600);
                expect(navigationText.maxLines, isNull);
                expect(navigationText.softWrap, isTrue);
                expect(navigationText.overflow, TextOverflow.visible);
                final paragraph = tester.renderObject<RenderParagraph>(
                  navigationTitle,
                );
                expect(
                  paragraph.textScaler.scale(20),
                  closeTo(20 * scale, .001),
                );
              }
            }
            expect(tester.takeException(), isNull);
            await _capture(
              tester,
              '$page-${brightness.name}-${(scale * 100).round()}-top',
            );
            if (page != 'place') {
              final label = page == 'moment' ? '查看地点' : '当前范围的公开动态';
              final button = find.widgetWithText(TextButton, label);
              await tester.scrollUntilVisible(
                button,
                120,
                scrollable: find.byType(Scrollable).first,
              );
              await tester.pumpAndSettle();
              expect(button.hitTestable(), findsOneWidget);
              expect(tester.getSize(button).height, greaterThanOrEqualTo(48));
              await tester.tap(button);
              expect(opened, 1);
            } else {
              final heading = find.text('举办条件');
              await tester.scrollUntilVisible(
                heading,
                120,
                scrollable: find.byType(Scrollable).first,
              );
              await tester.pumpAndSettle();
              expect(heading.hitTestable(), findsOneWidget);
            }
            expect(tester.takeException(), isNull);
            await tester.pumpWidget(const SizedBox.shrink());
          },
        );
      }
    }
  }
}
