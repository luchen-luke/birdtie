import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/content/moment_publication_controller.dart';
import 'package:birdtie_client/src/content/moment_publication_sheet.dart';
import 'package:birdtie_client/src/content/private_moment_media_controller.dart';
import 'package:birdtie_client/src/content/private_moment_media_pending_store.dart';
import 'package:birdtie_client/src/content/private_moment_media_sheet.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:birdtie_client/src/workspace/now_private_moment_image_page.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:birdtie_client/src/workspace/social_intent_creation_pending_store.dart';
import 'package:birdtie_client/src/workspace/social_intent_drafts.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'entity_share_pending_store_test.dart'
    show shareOwner, sharePeer, shareTarget, shareConversation, shareOperation;
import 'moment_publication_controller_test.dart'
    show
        publicationTestPreview,
        publicationTestReply,
        publicationTestMoment,
        publicationTestPlace;
import 'now_private_moment_image_entry_test.dart' show EntryFixture, entryRow;
import 'private_moment_media_controller_test.dart' show TestMomentImagePicker;
import 'public_moment_api_test.dart' show publicMomentData;
import 'package:birdtie_client/src/workspace/entity_share_control.dart';
import 'entity_share_control_test.dart' show sharePreviewWire;
import 'share_entity_to_chat_test.dart' show shareSettle;

const _mixedTitle = '周末在阿伯丁 Aberdeen 参加中文与 English 交流，记录这次与朋友商量的详细想法和待确认安排';
const _mixedBody =
    '这里保留完整中文正文与 English source，长内容应自然换行并可以滚动阅读。具体的时间、地点、收件人和公开范围仍由本人检查与确认。';
const _boundary = Key('sharing-drafts-widget-fixture');
String? _fixtureFamily;

// This font is a test-only local display aid. It is never bundled or installed
// into the application, and the images below must not be called device evidence.
ThemeData _theme(Brightness brightness) {
  final original = birdtieTheme(brightness);
  final family = _fixtureFamily;
  if (family == null) return original;
  final themed = original.copyWith(
    textTheme: original.textTheme.apply(fontFamily: family),
  );
  final text = NowUiTokens.fromTheme(themed);
  ButtonStyle? withFont(ButtonStyle? style) => style?.copyWith(
    textStyle: WidgetStatePropertyAll(
      style.textStyle?.resolve({})?.copyWith(fontFamily: family),
    ),
  );
  return themed.copyWith(
    appBarTheme: original.appBarTheme.copyWith(titleTextStyle: text.pageTitle),
    listTileTheme: original.listTileTheme.copyWith(
      titleTextStyle: text.placeTitle,
      subtitleTextStyle: text.metadata,
    ),
    textButtonTheme: TextButtonThemeData(
      style: withFont(original.textButtonTheme.style),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: withFont(original.filledButtonTheme.style),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: withFont(original.outlinedButtonTheme.style),
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: withFont(original.elevatedButtonTheme.style),
    ),
    inputDecorationTheme: original.inputDecorationTheme.copyWith(
      hintStyle: original.inputDecorationTheme.hintStyle?.copyWith(
        fontFamily: family,
      ),
      helperStyle: text.metadata,
      errorStyle: original.inputDecorationTheme.errorStyle?.copyWith(
        fontFamily: family,
      ),
    ),
    extensions: [
      ...original.extensions.values.where(
        (extension) => extension is! NowUiTokens,
      ),
      text,
    ],
  );
}

Widget _host(Widget home, Brightness brightness, double scale) => MaterialApp(
  theme: _theme(brightness),
  builder: (context, child) => RepaintBoundary(
    key: _boundary,
    child: Column(
      children: [
        Material(
          color: Theme.of(context).colorScheme.surface,
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
  home: home,
);

void _view(WidgetTester tester) {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = const Size(320, 800);
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  addTearDown(tester.view.resetViewInsets);
}

void _role(
  WidgetTester tester,
  Finder finder,
  double size,
  FontWeight weight,
  double scale,
) {
  final text = tester.widget<Text>(finder);
  expect(text.style!.fontSize, size);
  expect(text.style!.fontWeight, weight);
  expect(text.maxLines, isNull);
  expect(text.overflow, isNot(TextOverflow.ellipsis));
  expect(
    MediaQuery.textScalerOf(tester.element(finder)).scale(size),
    size * scale,
  );
  // Verify the painted text as well as its inherited context. The shared app
  // bar must restore the full scaler rather than just retain unscaled tokens.
  final paragraph = tester.renderObject<RenderParagraph>(finder);
  expect(paragraph.textScaler.scale(size), size * scale);
  expect(paragraph.didExceedMaxLines, isFalse);
  expect(paragraph.overflow, isNot(TextOverflow.ellipsis));
}

Future<void> _reachable(WidgetTester tester, Finder finder) async {
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  expect(finder.hitTestable(), findsOneWidget);
  final rect = tester.getRect(finder);
  // Scrolling fractional line metrics can leave 48 as 47.99999999999994.
  const epsilon = 1e-6;
  expect(rect.width, greaterThanOrEqualTo(48 - epsilon));
  expect(rect.height, greaterThanOrEqualTo(48 - epsilon));
  expect(rect.top, greaterThanOrEqualTo(-epsilon));
  expect(
    rect.bottom,
    lessThanOrEqualTo(800 - tester.view.viewInsets.bottom + epsilon),
  );
}

Future<void> _capture(
  WidgetTester tester,
  String name,
  Brightness brightness,
  double scale,
) async {
  final path =
      Platform.environment['BIRDTIE_TYPOGRAPHY_SHARING_SCREENSHOT_DIR'];
  if (path == null ||
      !(brightness == Brightness.light && scale == 1 ||
          brightness == Brightness.dark && scale == 2)) {
    return;
  }
  final boundary = tester.renderObject<RenderRepaintBoundary>(
    find.byKey(_boundary),
  );
  await tester.runAsync(() async {
    final image = await boundary.toImage(pixelRatio: 1);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    final output = File(
      '$path/WIDGET_FIXTURE_NOT_PHONE_${brightness.name}_${(scale * 100).round()}_$name.png',
    );
    await output.parent.create(recursive: true);
    await output.writeAsBytes(bytes!.buffer.asUint8List());
    image.dispose();
  });
}

http.Response _wire(Object data) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), 200);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    final fontPath = Platform.environment['BIRDTIE_TYPOGRAPHY_FIXTURE_FONT'];
    if (fontPath == null) return;
    final loader = FontLoader('BirdtieTypographyFixtureCJK')
      ..addFont(
        Future.value(ByteData.sublistView(File(fontPath).readAsBytesSync())),
      );
    await loader.load();
    _fixtureFamily = 'BirdtieTypographyFixtureCJK';
    await (FontLoader(
      'MaterialIcons',
    )..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'))).load();
  });
  for (final brightness in Brightness.values) {
    for (final scale in [1.0, 1.3, 2.0]) {
      final sample = '${brightness.name} ${(scale * 100).round()}%';
      testWidgets('公开确认 $sample 320长中英标题正文自然换行与取消无写入', (tester) async {
        _view(tester);
        var writes = 0;
        final client = MockClient((request) async {
          if (request.method != 'GET') writes++;
          return publicationTestReply(
            publicationTestPreview()..addAll({
              'title': _mixedTitle,
              'body': _mixedBody,
              'placeName': _mixedTitle,
            }),
          );
        });
        final controller = MomentPublicationController(
          momentID: publicationTestMoment,
          placeID: publicationTestPlace,
          authorizationHeader: () => 'Bearer fixture',
          client: client,
          apiBaseUrl: 'https://fixture.test',
        );
        addTearDown(controller.dispose);
        addTearDown(client.close);
        await tester.pumpWidget(
          _host(
            Scaffold(body: MomentPublicationSheet(controller: controller)),
            brightness,
            scale,
          ),
        );
        await tester.pumpAndSettle();
        _role(tester, find.text('确认公开这条记录'), 18, FontWeight.w600, scale);
        _role(tester, find.text(_mixedTitle), 16, FontWeight.w500, scale);
        final body = DefaultTextStyle.of(
          tester.element(find.text(_mixedBody)),
        ).style;
        expect(body.fontSize, 15);
        expect(body.fontWeight, FontWeight.w400);
        expect(body.height, 1.5);
        expect(tester.takeException(), isNull);
        await _capture(tester, 'publication_initial', brightness, scale);
        await _reachable(tester, find.byType(CheckboxListTile));
        await tester.tap(find.byType(CheckboxListTile));
        await tester.pumpAndSettle();
        await _reachable(tester, find.widgetWithText(FilledButton, '确认公开'));
        expect(
          tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
          isNotNull,
        );
        await _reachable(tester, find.widgetWithText(TextButton, '取消'));
        await _capture(tester, 'publication_cancel', brightness, scale);
        await tester.tap(find.text('取消'));
        await tester.pumpAndSettle();
        expect(writes, 0);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      });

      testWidgets('私人图片 $sample 320长标题与移除确认可滚动取消无写入', (tester) async {
        _view(tester);
        var writes = 0;
        final identity = ValueNotifier(0);
        final client = MockClient((request) async {
          if (request.method != 'GET') writes++;
          return _wire([
            {
              'id': shareTarget,
              'operationId': shareOperation,
              'momentId': publicationTestMoment,
              'ownerAccountId': shareOwner,
              'momentRevision': 1,
              'mimeType': 'image/png',
              'byteSize': 10,
              'inputSha256': 'a' * 64,
              'sha256': 'b' * 64,
              'pixelRisk': 'UNKNOWN',
              'purpose': privateMomentImagePurpose,
              'status': 'ready_private',
              'revision': 2,
              'previewExpiresAt': '2026-10-10T09:00:00Z',
              'retainUntil': '2026-11-09T09:00:00Z',
            },
          ]);
        });
        final controller = PrivateMomentMediaController(
          momentID: publicationTestMoment,
          momentRevision: 1,
          authorizationHeader: () => 'Bearer fixture',
          ownerID: () => shareOwner,
          identityChanges: identity,
          sourceCurrent: () => true,
          client: client,
          apiBaseUrl: 'https://fixture.test',
          picker: TestMomentImagePicker(Uint8List(0)),
          pendingStore: MemoryPrivateImagePendingStore(),
        );
        await tester.pumpWidget(
          _host(
            PrivateMomentMediaSheet(
              controller: controller,
              momentTitle: _mixedTitle,
            ),
            brightness,
            scale,
          ),
        );
        await tester.pumpAndSettle();
        _role(
          tester,
          find.descendant(of: find.byType(AppBar), matching: find.text('私人图片')),
          20,
          FontWeight.w600,
          scale,
        );
        _role(tester, find.text(_mixedTitle), 16, FontWeight.w500, scale);
        _role(tester, find.text('已保存的私人图片'), 18, FontWeight.w600, scale);
        await _reachable(tester, find.widgetWithText(OutlinedButton, '选择一张图片'));
        await _capture(tester, 'private_image_initial', brightness, scale);
        await _reachable(tester, find.byTooltip('移除私人图片'));
        await tester.tap(find.byTooltip('移除私人图片'));
        await tester.pumpAndSettle();
        _role(tester, find.text('移除私人图片？'), 18, FontWeight.w600, scale);
        expect(
          tester.widget<AlertDialog>(find.byType(AlertDialog)).scrollable,
          isTrue,
        );
        await _reachable(tester, find.widgetWithText(FilledButton, '确认移除'));
        await _capture(
          tester,
          'private_image_remove_confirmation',
          brightness,
          scale,
        );
        await tester.tap(find.text('取消'));
        await tester.pumpAndSettle();
        expect(writes, 0);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        controller.dispose();
        identity.dispose();
        client.close();
      });

      testWidgets('社交创作草稿 $sample 320长中英输入键盘与主要保存可达', (tester) async {
        _view(tester);
        var writes = 0;
        final client = MockClient((request) async {
          if (request.method != 'GET') writes++;
          return _wire([]);
        });
        await tester.pumpWidget(
          _host(
            SocialIntentDraftPage(
              authorizationHeader: () => 'Bearer fixture',
              ownerID: () => shareOwner,
              client: client,
              apiBaseUrl: 'https://fixture.test',
              pendingStore: MemorySocialIntentCreationPendingStore(),
              suggestedTitle: _mixedTitle,
              contextNote: _mixedBody,
            ),
            brightness,
            scale,
          ),
        );
        await tester.pumpAndSettle();
        _role(tester, find.text('我的社交意图'), 20, FontWeight.w600, scale);
        _role(tester, find.text('先记下想做的事'), 18, FontWeight.w600, scale);
        final fieldFinder = find.byKey(const Key('intent-draft-title'));
        final field = tester.widget<TextField>(fieldFinder);
        expect(field.style!.fontSize, 16);
        expect(field.style!.fontWeight, FontWeight.w400);
        expect(field.style!.height, 1.5);
        expect(
          MediaQuery.textScalerOf(tester.element(fieldFinder)).scale(16),
          scale * 16,
        );
        await tester.ensureVisible(fieldFinder);
        await tester.tap(fieldFinder);
        await tester.pumpAndSettle();
        await _capture(tester, 'draft_initial', brightness, scale);
        final state = tester.state<EditableTextState>(
          find.descendant(of: fieldFinder, matching: find.byType(EditableText)),
        );
        tester.view.viewInsets = const FakeViewPadding(bottom: 260);
        await tester.pumpAndSettle();
        expect(
          tester.state<EditableTextState>(
            find.descendant(
              of: fieldFinder,
              matching: find.byType(EditableText),
            ),
          ),
          same(state),
        );
        expect(field.controller!.text, _mixedTitle);
        await _reachable(tester, find.widgetWithText(FilledButton, '保存私人草稿'));
        await tester.tap(find.text('保存私人草稿'));
        await tester.pumpAndSettle();
        expect(find.text('请选择参与方式，不会根据城市推断线上或线下。'), findsOneWidget);
        await _reachable(tester, find.widgetWithText(FilledButton, '保存私人草稿'));
        await _capture(
          tester,
          'draft_keyboard_save_reachable',
          brightness,
          scale,
        );
        expect(writes, 0);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        client.close();
      });

      testWidgets('私人记录选择 $sample 320长中英列表与错误重试可达', (tester) async {
        _view(tester);
        final fixture = EntryFixture()
          ..rows = [entryRow()..['title'] = _mixedTitle];
        // Match the production entry's merged changes, including the read
        // controller; auth alone does not notify when its async load completes.
        final changes = Listenable.merge([fixture.auth, fixture.moments]);
        await tester.pumpWidget(
          _host(
            NowPrivateMomentImagePage(
              auth: fixture.auth,
              moments: fixture.moments,
              identityChanges: changes,
              current: () => true,
              organizationWorkspaceID: () => null,
            ),
            brightness,
            scale,
          ),
        );
        await tester.pumpAndSettle();
        _role(tester, find.text('选择私人记录'), 20, FontWeight.w600, scale);
        await _reachable(tester, find.widgetWithText(ListTile, _mixedTitle));
        _role(tester, find.text(_mixedTitle), 16, FontWeight.w500, scale);
        expect(
          tester.getSize(find.text(_mixedTitle)).height,
          greaterThan(16 * scale),
        );
        await _capture(tester, 'private_moment_chooser', brightness, scale);
        fixture.status = 503;
        await fixture.moments.refresh();
        await tester.pumpAndSettle();
        await _reachable(tester, find.widgetWithText(FilledButton, '重新读取私人记录'));
        await _capture(
          tester,
          'private_moment_chooser_error',
          brightness,
          scale,
        );
        expect(
          fixture.requests.every((request) => request.method == 'GET'),
          isTrue,
        );
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        fixture.dispose();
        fixture.client.close();
      });

      testWidgets('分享预览 $sample 320长中英目标收件人完整且取消无写入', (tester) async {
        _view(tester);
        var writes = 0;
        final identity = ValueNotifier(0);
        final client = MockClient((request) async {
          if (request.method == 'PUT') writes++;
          if (request.url.path ==
              '/v1/me/entity-shares/CONVERSATION/$shareConversation/previews') {
            final body = jsonDecode(request.body) as Map<String, dynamic>;
            return _wire(
              sharePreviewWire(
                EntityShareInput(
                  operationID: body['operationId'],
                  recipient: const EntityShareRecipient(
                    'CONVERSATION',
                    shareConversation,
                  ),
                  type: 'moment',
                  entityID: shareTarget,
                ),
                title: _mixedTitle,
              ),
            );
          }
          switch (request.url.path) {
            case '/v1/me':
              return _wire({'id': shareOwner, 'accountType': 'person'});
            case '/v1/me/conversations':
              return _wire([
                {
                  'id': shareConversation,
                  'otherAccountId': sharePeer,
                  'otherName': _mixedTitle,
                },
              ]);
            case '/v1/me/ties':
              return _wire([]);
            case '/v1/moments/$shareTarget':
              return _wire(publicMomentData()..['title'] = _mixedTitle);
          }
          throw StateError('Unexpected fixture route ${request.url.path}');
        });
        await tester.pumpWidget(
          _host(
            Scaffold(
              body: Builder(
                builder: (context) => FilledButton(
                  onPressed: () => shareEntityToChat(
                    context,
                    authorizationHeader: () => 'Bearer fixture',
                    identityChanges: identity,
                    type: 'moment',
                    id: shareTarget,
                    apiBaseUrl: 'https://fixture.test',
                    client: client,
                    pendingStore: MemoryEntitySharePendingStore(),
                  ),
                  child: const Text('分享入口'),
                ),
              ),
            ),
            brightness,
            scale,
          ),
        );
        await tester.tap(find.text('分享入口'));
        await shareSettle(tester);
        _role(tester, find.text('发给好友'), 18, FontWeight.w600, scale);
        await _reachable(tester, find.widgetWithText(ListTile, _mixedTitle));
        await _capture(tester, 'share_recipient_preview', brightness, scale);
        await tester.tap(find.text(_mixedTitle));
        await shareSettle(tester);
        _role(tester, find.text('检查分享'), 18, FontWeight.w600, scale);
        _role(tester, find.text(_mixedTitle).last, 16, FontWeight.w500, scale);
        await _reachable(tester, find.widgetWithText(FilledButton, '确认发送'));
        await _capture(tester, 'share_confirmation', brightness, scale);
        await tester.tap(find.text('取消'));
        await shareSettle(tester);
        await _reachable(tester, find.widgetWithText(TextButton, '关闭'));
        await tester.tap(find.text('关闭'));
        await shareSettle(tester);
        expect(writes, 0);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        client.close();
        identity.dispose();
      });
    }
  }
}
