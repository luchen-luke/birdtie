import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'batch_a_saved_before.dart' as before;

const owner = 'ba100000-0000-4000-8000-000000000001';
const captureKey = Key('batch-a-isolated-visual');
ThemeData visualTheme() {
  final base = birdtieTheme(Brightness.light);
  final themed = base.copyWith(
    textTheme: base.textTheme.apply(fontFamily: 'BatchACJK'),
    primaryTextTheme: base.primaryTextTheme.apply(fontFamily: 'BatchACJK'),
  );
  final roles = NowUiTokens.fromTheme(themed);
  ButtonStyle? actionStyle(ButtonStyle? style) => style?.copyWith(
    textStyle: WidgetStatePropertyAll(
      roles.action.copyWith(fontFamily: 'BatchACJK'),
    ),
  );
  return themed.copyWith(
    appBarTheme: themed.appBarTheme.copyWith(titleTextStyle: roles.pageTitle),
    listTileTheme: themed.listTileTheme.copyWith(
      titleTextStyle: roles.placeTitle,
      subtitleTextStyle: roles.listSubtitle,
    ),
    textButtonTheme: TextButtonThemeData(
      style: actionStyle(themed.textButtonTheme.style),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: actionStyle(themed.filledButtonTheme.style),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: actionStyle(themed.outlinedButtonTheme.style),
    ),
    extensions: [
      ...themed.extensions.values.where((e) => e is! NowUiTokens),
      roles,
    ],
  );
}

List<Map<String, dynamic>> rows() => [
  for (var i = 1; i <= 2; i++)
    {
      'id': 'ba100000-0000-4000-8000-00000000000${i + 1}',
      'kind': 'place',
      'targetId': 'ba200000-0000-4000-8000-00000000000$i',
      'title': i == 1 ? '合成海边步行地点 Aberdeen coast' : '合成公共阅读地点 City library',
      'summary': '隔离样例地点说明，用于核对同一数据的布局。',
      'cityId': 'aberdeen-gb',
      'cityName': '阿伯丁',
      'available': true,
      'note': i == 1 ? '合成本人备注，不能随集合公开' : '',
      'noteRevision': i == 1 ? 1 : 0,
      'latitude': 57.1 + i / 1000,
      'longitude': -2.1 - i / 1000,
      'sourceExpiresAt': DateTime.now()
          .toUtc()
          .add(const Duration(minutes: 1))
          .toIso8601String(),
    },
];
Future<void> capture(WidgetTester t, String name) async {
  final directory = Platform.environment['BIRDTIE_BATCH_A_CAPTURE'];
  if (directory == null) return;
  await t.runAsync(() async {
    final image = await t
        .renderObject<RenderRepaintBoundary>(find.byKey(captureKey))
        .toImage(pixelRatio: 1);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    final f = File('$directory/$name.png');
    await f.parent.create(recursive: true);
    if (await f.exists()) throw StateError('Immutable screenshot exists');
    await f.writeAsBytes(bytes!.buffer.asUint8List());
    image.dispose();
  });
}

void main() {
  setUpAll(() async {
    if (Platform.environment['BIRDTIE_BATCH_A_CAPTURE'] == null)
      throw StateError('Explicit isolated capture directory required');
    for (final f in [
      ('BatchACJK', r'C:\Windows\Fonts\msyh.ttc'),
      (
        'MaterialIcons',
        r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
      ),
    ]) {
      await (FontLoader(f.$1)..addFont(
            Future.value(ByteData.sublistView(await File(f.$2).readAsBytes())),
          ))
          .load();
    }
  });
  for (final old in [true, false]) {
    for (final state in ['normal', 'keyboard', 'empty', 'error']) {
      testWidgets(
        'batchA isolated visual ${old ? 'before' : 'after'} $state normal390',
        (t) async {
          FlutterSecureStorage.setMockInitialValues({});
          t.view.physicalSize = const Size(390, 844);
          t.view.devicePixelRatio = 1;
          addTearDown(t.view.resetPhysicalSize);
          addTearDown(t.view.resetDevicePixelRatio);
          final calls = <http.Request>[];
          final client = MockClient((r) async {
            calls.add(r);
            return http.Response.bytes(
              utf8.encode(
                jsonEncode({
                  'data': {
                    'items': state == 'empty' ? [] : rows(),
                    'nextCursor': null,
                  },
                }),
              ),
              state == 'error' ? 503 : 200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          });
          final dynamic controller = old
              ? before.SavedController(
                  client: client,
                  apiBaseUrl: 'https://batch-a-visual.test',
                  authorizationHeader: () => 'Bearer isolated',
                  ownerID: () => owner,
                )
              : SavedController(
                  client: client,
                  apiBaseUrl: 'https://batch-a-visual.test',
                  authorizationHeader: () => 'Bearer isolated',
                  ownerID: () => owner,
                );
          final Widget page = old
              ? before.SavedPage(saved: controller as before.SavedController)
              : SavedPage(
                  saved: controller as SavedController,
                  onAddToCollection: (_) {},
                  onViewMap: (_) {},
                );
          await t.pumpWidget(
            RepaintBoundary(
              key: captureKey,
              child: MaterialApp(
                theme: visualTheme(),
                debugShowCheckedModeBanner: false,
                builder: (context, child) => Column(
                  children: [
                    Material(
                      color: Theme.of(context).colorScheme.surfaceContainerHigh,
                      child: const SizedBox(
                        height: 28,
                        child: Center(child: Text('隔离视觉样例 · 非真实账号')),
                      ),
                    ),
                    Expanded(
                      child: MediaQuery(
                        data: MediaQuery.of(context).copyWith(
                          textScaler: TextScaler.noScaling,
                          viewInsets: state == 'keyboard'
                              ? const EdgeInsets.only(bottom: 300)
                              : EdgeInsets.zero,
                        ),
                        child: child!,
                      ),
                    ),
                  ],
                ),
                home: Scaffold(
                  appBar: AppBar(title: const Text('收藏')),
                  body: page,
                ),
              ),
            ),
          );
          await t.pumpAndSettle();
          expect(t.takeException(), isNull);
          expect(calls.where((r) => r.method != 'GET'), isEmpty);
          if (!old && state == 'error') {
            expect(find.text('尚未取得收藏结果'), findsOneWidget);
            expect(find.text('暂无收藏'), findsNothing);
            expect(find.textContaining('正在读取'), findsNothing);
            expect(find.text('重试读取'), findsOneWidget);
          }
          await capture(t, '${old ? 'before' : 'after'}-saved-$state');
          await t.pumpWidget(const SizedBox());
          await t.pumpAndSettle();
          controller.dispose();
          client.close();
        },
      );
    }
  }
}
