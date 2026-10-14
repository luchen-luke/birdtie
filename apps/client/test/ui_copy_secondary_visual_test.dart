import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/content/agent_seed_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_profile_page.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_page.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'package:birdtie_client/src/workspace/person_contexts_page.dart';
import 'package:birdtie_client/src/workspace/social_now_page.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_seed_controller_test.dart' show seedJson, seedResponse;
import 'agent_profile_api_test.dart'
    show profileOwner, profileData, profileResponse, detailRaw;
import 'agent_memory_correction_api_test.dart';
import 'social_now_controller_test.dart' show nowEmpty, nowOwner;

const _key = Key('copy-secondary-render');
const _at = '2099-01-01T00:00:00Z';
const _font = 'CopySecondaryFixtureCJK';
ThemeData secondaryTheme() {
  final base = birdtieTheme(Brightness.light);
  final theme = ThemeData(
    brightness: Brightness.light,
    colorScheme: base.colorScheme,
    fontFamily: _font,
    textTheme: base.textTheme.apply(fontFamily: _font),
    primaryTextTheme: base.primaryTextTheme.apply(fontFamily: _font),
  );
  final roles = NowUiTokens.fromTheme(theme);
  return theme.copyWith(
    extensions: [roles],
    appBarTheme: AppBarTheme(
      titleTextStyle: roles.sheetTitle,
      toolbarTextStyle: roles.body,
    ),
    listTileTheme: ListTileThemeData(
      titleTextStyle: roles.placeTitle,
      subtitleTextStyle: roles.listSubtitle,
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(textStyle: roles.action),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(textStyle: roles.action),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(textStyle: roles.action),
    ),
    chipTheme: ChipThemeData(
      labelStyle: roles.body,
      secondaryLabelStyle: roles.body,
    ),
    inputDecorationTheme: InputDecorationTheme(
      labelStyle: roles.body,
      floatingLabelStyle: roles.metadata,
      helperStyle: roles.metadata,
      hintStyle: roles.metadata,
    ),
  );
}

Future<void> secondaryMount(WidgetTester t, Widget page) async {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  await t.pumpWidget(
    RepaintBoundary(
      key: _key,
      child: MaterialApp(
        theme: secondaryTheme(),
        builder: (context, child) => Column(
          children: [
            Material(
              color: Theme.of(context).colorScheme.surfaceContainerHigh,
              child: const SizedBox(
                height: 28,
                child: Center(
                  child: Text(
                    '隔离测试渲染 · 非真人账号',
                    textDirection: TextDirection.ltr,
                  ),
                ),
              ),
            ),
            Expanded(child: child!),
          ],
        ),
        home: page,
      ),
    ),
  );
  await t.pump();
  await t.runAsync(
    () => Future<void>.delayed(const Duration(milliseconds: 30)),
  );
  await t.pump(const Duration(milliseconds: 100));
  await t.runAsync(
    () => Future<void>.delayed(const Duration(milliseconds: 30)),
  );
  await t.pump(const Duration(milliseconds: 100));
  expect(t.takeException(), isNull);
}

Future<void> secondaryCapture(WidgetTester t, String name) async {
  final dir = Platform.environment['BIRDTIE_SECONDARY_CAPTURE'];
  if (dir == null) return;
  final stage = Platform.environment['BIRDTIE_SECONDARY_STAGE'] ?? 'after';
  await t.runAsync(() async {
    final render = t.renderObject<RenderRepaintBoundary>(find.byKey(_key));
    final image = await render.toImage(pixelRatio: 1);
    try {
      final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
      final f = File('$dir/$stage-$name.png');
      await f.parent.create(recursive: true);
      await f.writeAsBytes(bytes!.buffer.asUint8List());
    } finally {
      image.dispose();
    }
  });
}

Future<void> secondaryTap(WidgetTester t, String label) async {
  final f = find.text(label);
  await t.scrollUntilVisible(
    f,
    160,
    maxScrolls: 60,
    scrollable: find.byType(Scrollable).first,
  );
  await t.ensureVisible(f);
  await t.tap(f);
  await t.pump();
  await t.runAsync(
    () => Future<void>.delayed(const Duration(milliseconds: 30)),
  );
  await t.pumpAndSettle();
}

void main() {
  setUpAll(() async {
    if (Platform.environment['BIRDTIE_SECONDARY_CAPTURE'] == null) return;
    for (final f in [
      (_font, r'C:\Windows\Fonts\msyh.ttc'),
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
  testWidgets('isolated secondary pages same sample normal font before after', (
    t,
  ) async {
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    FlutterSecureStorage.setMockInitialValues({});
    final auth = SeedTestAuth()..owner = profileOwner;
    final profile = MockClient((r) async {
      var value = profileData(r.url.path);
      if (r.url.path.endsWith('/$correctionMemory'))
        value = detailRaw(at: DateTime.parse(_at));
      return profileResponse(value);
    });
    await secondaryMount(
      t,
      AgentProfilePage(auth: auth, client: profile, apiBaseUrl: 'http://local'),
    );
    await secondaryCapture(t, 'profile-first');
    await secondaryTap(t, '我偏好徒步活动');
    await secondaryCapture(t, 'profile-detail');
    final supplement = find.text('检查原补充内容');
    if (supplement.evaluate().isNotEmpty) {
      await secondaryTap(t, '检查原补充内容');
      await secondaryCapture(t, 'profile-fields');
    } else
      await secondaryCapture(t, 'profile-fields');
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    profile.close();

    final correction = MockClient(
      (r) async => r.method == 'POST'
          ? correctionResponse(
              correctionPreviewRaw(
                input: Map<String, dynamic>.from(jsonDecode(r.body)),
                at: DateTime.parse(_at),
              ),
            )
          : correctionReadResponse(r, {
              'data': r.url.path.endsWith('agent-memories/page')
                  ? [correctionMemoryRaw(at: DateTime.parse(_at))]
                  : [],
            }),
    );
    await secondaryMount(
      t,
      AgentMemoryCorrectionPage(
        auth: auth,
        client: correction,
        apiBaseUrl: 'http://local',
        pendingStore: MemoryAgentMemoryCorrectionPendingStore(),
      ),
    );
    await secondaryCapture(t, 'memory-list');
    await secondaryTap(t, '删除');
    final check = find.text('检查具体纠正版本').evaluate().isNotEmpty
        ? '检查具体纠正版本'
        : '检查修改';
    await secondaryTap(t, check);
    await secondaryCapture(t, 'memory-delete-review');
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    correction.close();

    final seed = MockClient((r) async => seedResponse(seedJson()));
    await secondaryMount(
      t,
      AgentSeedSheet(auth: auth, client: seed, apiBaseUrl: 'http://local'),
    );
    await secondaryCapture(t, 'seed-first');
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    seed.close();
    final contexts = MockClient(
      (r) async => http.Response.bytes(
        utf8.encode(
          jsonEncode({
            'data': r.url.path == '/v1/cities'
                ? [
                    {'id': 'sample-city', 'name': '阿伯丁'},
                  ]
                : [],
          }),
        ),
        200,
      ),
    );
    await secondaryMount(
      t,
      PersonContextsPage(
        auth: auth,
        client: contexts,
        apiBaseUrl: 'http://local',
      ),
    );
    await secondaryCapture(t, 'contexts-empty');
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    contexts.close();
    auth.owner = nowOwner;
    final social = MockClient((r) async => nowEmpty(r));
    await secondaryMount(
      t,
      SocialNowPage(auth: auth, client: social, apiBaseUrl: 'http://local'),
    );
    await secondaryCapture(t, 'social-empty');
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    social.close();
    final place = MockClient(
      (r) async => r.url.path.endsWith('/venue')
          ? http.Response('{}', 404)
          : http.Response.bytes(
              utf8.encode(
                jsonEncode({
                  'data': r.url.path.endsWith('/activities')
                      ? []
                      : {
                          'id': correctionMemory,
                          'name': '样例图书馆',
                          'summary': '可查看地址和相关活动。',
                          'addressLabel': '样例街道 10 号',
                          'source': {'label': '隔离测试资料'},
                          'location': {'precision': 'none'},
                        },
                }),
              ),
              200,
            ),
    );
    await secondaryMount(
      t,
      Scaffold(
        body: PlaceDetailSheet(
          placeID: correctionMemory,
          authorizationHeader: () => null,
          onOpenActivity: (_) {},
          client: place,
          apiBaseUrl: 'http://local',
        ),
      ),
    );
    await secondaryCapture(t, 'place-no-venue');
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    place.close();
    auth.dispose();
  });
}
