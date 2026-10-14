import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'share_entity_to_chat_test.dart';
import 'entity_share_pending_store_test.dart';
import 'public_moment_api_test.dart' show publicMomentData;

const captureKey = Key('entity-share143-normal-widget-fixture');
Future<void> capture143(WidgetTester t, String name) async {
  final dir = Platform.environment['BIRDTIE_ENTITY_SHARE_SCREENSHOT_DIR'];
  if (dir == null) return;
  await t.pump();
  final boundary = t.renderObject<RenderRepaintBoundary>(
    find.byKey(captureKey),
  );
  await t.runAsync(() async {
    final image = await boundary.toImage(pixelRatio: 1),
        bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    final file = File('$dir/WIDGET_FIXTURE_NOT_DEVICE_390x844_$name.png');
    await file.parent.create(recursive: true);
    await file.writeAsBytes(bytes!.buffer.asUint8List());
    image.dispose();
  });
}

void view143(WidgetTester t) {
  t.view.devicePixelRatio = 1;
  t.view.physicalSize = const Size(390, 844);
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

void main() {
  setUpAll(() async {
    final path = Platform.environment['BIRDTIE_ENTITY_SHARE_FIXTURE_FONT'];
    if (path == null) return;
    final bytes = await File(path).readAsBytes();
    await (FontLoader(
      'EntityShareFixtureCJK',
    )..addFont(Future.value(ByteData.sublistView(bytes)))).load();
    final icons = await File(
      r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
    ).readAsBytes();
    await (FontLoader(
      'MaterialIcons',
    )..addFont(Future.value(ByteData.sublistView(icons)))).load();
  });
  testWidgets(
    '143 390 CJK full preview expiry removes old labels; UNKNOWN has only original recovery',
    (t) async {
      view143(t);
      final f = ShareFixture()
        ..title = '在阿伯丁 Aberdeen 与朋友讨论中文和 English 的当前公开动态，保留完整名称供本人检查'
        ..peerName = '本地合成收件人：王同学 Aberdeen 周末中文与 English 交流'
        ..previewSeconds = 2;
      final base = birdtieTheme(Brightness.light);
      final fontTheme = base.copyWith(
        textTheme: base.textTheme.apply(fontFamily: 'EntityShareFixtureCJK'),
        primaryTextTheme: base.primaryTextTheme.apply(
          fontFamily: 'EntityShareFixtureCJK',
        ),
      );
      ButtonStyle? fontButtons(ButtonStyle? style) => style?.copyWith(
        textStyle: WidgetStatePropertyAll(
          style.textStyle
              ?.resolve({})
              ?.copyWith(fontFamily: 'EntityShareFixtureCJK'),
        ),
      );
      final theme = fontTheme.copyWith(
        extensions: [NowUiTokens.fromTheme(fontTheme)],
        textButtonTheme: TextButtonThemeData(
          style: fontButtons(base.textButtonTheme.style),
        ),
        filledButtonTheme: FilledButtonThemeData(
          style: fontButtons(base.filledButtonTheme.style),
        ),
        outlinedButtonTheme: OutlinedButtonThemeData(
          style: fontButtons(base.outlinedButtonTheme.style),
        ),
        elevatedButtonTheme: ElevatedButtonThemeData(
          style: fontButtons(base.elevatedButtonTheme.style),
        ),
        listTileTheme: base.listTileTheme.copyWith(
          titleTextStyle: NowUiTokens.fromTheme(fontTheme).placeTitle,
          subtitleTextStyle: NowUiTokens.fromTheme(fontTheme).metadata,
        ),
      );
      await t.pumpWidget(
        f.harness(
          theme: theme,
          builder: (c, child) => RepaintBoundary(
            key: captureKey,
            child: Column(
              children: [
                const Material(
                  child: Text('WIDGET FIXTURE / NOT DEVICE / 390×844 / NORMAL'),
                ),
                Expanded(child: child!),
              ],
            ),
          ),
        ),
      );
      await f.open(t);
      await f.preview(t);
      await capture143(t, 'preview');
      expect(find.text(f.title), findsOneWidget);
      expect(t.takeException(), isNull);
      await t.pump(const Duration(seconds: 3));
      await t.pump();
      expect(find.text(f.title), findsNothing);
      expect(find.text('收件人：${f.peerName}'), findsNothing);
      expect(
        t
            .widget<FilledButton>(find.widgetWithText(FilledButton, '确认发送'))
            .onPressed,
        isNull,
      );
      await capture143(t, 'expired');
      await t.tap(find.text('取消'));
      await shareSettle(t);
      f.previewSeconds = 30;
      f.lost = true;
      await f.preview(t);
      await t.tap(find.text('确认发送'));
      await shareSettle(t);
      await capture143(t, 'unknown');
      expect(f.posted.length, 1);
      expect(find.text('检查并重试原分享'), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pump(const Duration(seconds: 31));
      f.dispose();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    '143 actual PublicMomentPage target A→B→A retires an already open share dialog',
    (t) async {
      FlutterSecureStorage.setMockInitialValues({});
      final f = ShareFixture(), target = ValueNotifier(shareTarget);
      final client = MockClient((r) async {
        if (r.url.path.startsWith('/v1/moments/'))
          return shareJSON(
            publicMomentData()..['id'] = r.url.path.split('/').last,
          );
        return f.handle(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder(
            valueListenable: target,
            builder: (c, id, _) => PublicMomentPage(
              momentID: id,
              authorizationHeader: () => f.identity.value,
              ownerID: () => f.actor.value,
              identityChanges: Listenable.merge([f.actor, f.identity]),
              apiBaseUrl: 'https://api.example',
              client: client,
            ),
          ),
        ),
      );
      await shareSettle(t);
      await t.tap(find.text('发给好友'));
      await shareSettle(t);
      await f.preview(t);
      target.value = sharePeer;
      await t.pump();
      target.value = shareTarget;
      await shareSettle(t);
      expect(find.text('收件人：本地合成好友'), findsNothing);
      expect(
        t
            .widget<FilledButton>(find.widgetWithText(FilledButton, '确认发送'))
            .onPressed,
        isNull,
      );
      expect(f.posted, isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pump(const Duration(seconds: 31));
      expect(t.takeException(), isNull);
      // Destroying the original Navigator must release the global flow too.
      await t.pumpWidget(f.harness());
      await f.open(t);
      expect(find.text('发给好友'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      await t.pump();
      target.dispose();
      client.close();
      f.dispose();
    },
  );
  for (final mode in ['header', 'listenable-aba', 'owner', 'workspace']) {
    testWidgets(
      '143 provider replacement $mode current frame retires old approval zero PUT',
      (t) async {
        FlutterSecureStorage.setMockInitialValues({});
        final f = ShareFixture(),
            oldEvents = ValueNotifier<String?>('Bearer A'),
            nextEvents = ValueNotifier<String?>('Bearer A');
        final header = ValueNotifier<String? Function()>(() => oldEvents.value);
        final owner = ValueNotifier<String? Function()>(() => shareOwner);
        final workspace = ValueNotifier<String? Function()>(() => null);
        final events = ValueNotifier<Listenable>(oldEvents);
        final client = MockClient((r) async {
          if (r.url.path.startsWith('/v1/moments/'))
            return shareJSON(publicMomentData());
          return f.handle(r);
        });
        await t.pumpWidget(
          MaterialApp(
            home: ListenableBuilder(
              listenable: Listenable.merge([header, owner, workspace, events]),
              builder: (c, _) => PublicMomentPage(
                momentID: shareTarget,
                authorizationHeader: header.value,
                ownerID: owner.value,
                workspaceID: workspace.value,
                identityChanges: events.value,
                apiBaseUrl: 'https://api.example',
                client: client,
              ),
            ),
          ),
        );
        await shareSettle(t);
        await t.tap(find.text('发给好友'));
        await shareSettle(t);
        await f.preview(t);
        final oldConfirm = t
            .widget<FilledButton>(find.widgetWithText(FilledButton, '确认发送'))
            .onPressed;
        expect(oldConfirm, isNotNull);
        if (mode == 'header') {
          header.value = () => 'Bearer B';
          await t.pump();
          header.value = () => 'Bearer A';
        } else if (mode == 'listenable-aba') {
          header.value = () => nextEvents.value;
          events.value = nextEvents;
          await t.pump();
          nextEvents.value = 'Bearer B';
          nextEvents.value = 'Bearer A';
        } else if (mode == 'owner') {
          owner.value = () => sharePeer;
          await t.pump();
          owner.value = () => shareOwner;
        } else {
          workspace.value = () => 'org-b';
          await t.pump();
          workspace.value = () => null;
        }
        // No fake clock advance: this requires the latest binding event, not the
        // dialog's 100ms fallback poll. The captured old callback is also retired.
        await t.pump();
        await t.pump();
        oldConfirm!();
        await t.pump();
        expect(find.text('收件人：本地合成好友'), findsNothing);
        expect(
          t
              .widget<FilledButton>(find.widgetWithText(FilledButton, '确认发送'))
              .onPressed,
          isNull,
        );
        expect(f.posted, isEmpty);
        expect(f.starts, 0);
        await t.pumpWidget(const SizedBox());
        await t.pump(const Duration(seconds: 31));
        expect(t.takeException(), isNull);
        client.close();
        header.dispose();
        owner.dispose();
        workspace.dispose();
        events.dispose();
        oldEvents.dispose();
        nextEvents.dispose();
        f.dispose();
      },
    );
  }
  testWidgets(
    '143 provider replacement normal renewal retains own UNKNOWN and original GET only',
    (t) async {
      FlutterSecureStorage.setMockInitialValues({});
      final f = ShareFixture()..lost = true;
      final oldEvents = ValueNotifier<String?>('Bearer A'),
          newEvents = ValueNotifier<String?>('Bearer renewed');
      final header = ValueNotifier<String? Function()>(() => oldEvents.value),
          events = ValueNotifier<Listenable>(oldEvents);
      final client = MockClient((r) async {
        if (r.url.path.startsWith('/v1/moments/'))
          return shareJSON(publicMomentData());
        return f.handle(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: ListenableBuilder(
            listenable: Listenable.merge([header, events]),
            builder: (c, _) => PublicMomentPage(
              momentID: shareTarget,
              authorizationHeader: header.value,
              ownerID: () => shareOwner,
              identityChanges: events.value,
              apiBaseUrl: 'https://api.example',
              client: client,
            ),
          ),
        ),
      );
      await shareSettle(t);
      await t.tap(find.text('发给好友'));
      await shareSettle(t);
      await f.preview(t);
      await t.tap(find.text('确认发送'));
      await shareSettle(t);
      expect(f.posted.length, 1);
      const store = SecureEntitySharePendingStore();
      final original = (await store.read(
        'https://api.example',
        shareOwner,
      )).single;
      expect(original.stage, 'UNKNOWN');
      header.value = () => newEvents.value;
      events.value = newEvents;
      await t.pump();
      await t.pump();
      expect(find.text('待核实的原分享'), findsOneWidget);
      expect(
        (await store.read('https://api.example', shareOwner)).single.toJson(),
        original.toJson(),
      );
      expect(await store.read('https://api.example', sharePeer), isEmpty);
      await t.tap(find.text('核实原结果'));
      await shareSettle(t);
      expect(f.posted.length, 1);
      expect(f.reads, 1);
      expect(find.text('已发送到私信。可在消息中找回。'), findsOneWidget);
      expect(await store.read('https://api.example', shareOwner), isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pump(const Duration(seconds: 31));
      expect(t.takeException(), isNull);
      header.dispose();
      events.dispose();
      oldEvents.dispose();
      newEvents.dispose();
      client.close();
      f.dispose();
    },
  );
}
