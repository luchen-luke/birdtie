import 'dart:async';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/content/private_moment_media_sheet.dart';
import 'package:birdtie_client/src/content/private_moment_media_controller.dart';
import 'package:birdtie_client/src/content/private_moment_media_pending_store.dart';
import 'package:birdtie_client/src/content/moment_image_text_recognizer.dart';
import 'package:birdtie_client/src/content/moment_image_text_rules.dart';
import 'private_moment_media_controller_test.dart'
    show MediaUnitFixture, syntheticPrivatePNG, imageMoment;

class LocalTextSpy extends MomentImageTextRecognizer {
  int calls = 0, cancels = 0;
  Completer<MomentImageTextResult>? pending;
  bool empty = false, unavailable = false;
  Uint8List? input;
  @override
  Future<MomentImageTextResult> recognize(Uint8List bytes) async {
    calls++;
    input = Uint8List.fromList(bytes);
    if (unavailable) {
      throw const MomentImageTextFailure('当前设备暂不支持本机文字检查；仍可手动遮挡或清除图片。');
    }
    return pending != null ? pending!.future : result();
  }

  MomentImageTextResult result() => MomentImageTextResult(
    empty
        ? []
        : const [
            MomentImageTextLine(
              'student@example.org',
              ui.Rect.fromLTRB(0, 0, .5, .5),
            ),
          ],
    limited: false,
  );
  @override
  Future<void> cancel() async {
    cancels++;
  }
}

MediaUnitFixture textFixture(Uint8List bytes, LocalTextSpy spy) {
  final x = MediaUnitFixture(bytes);
  x.controller.dispose();
  x.controller = PrivateMomentMediaController(
    momentID: imageMoment,
    momentRevision: 1,
    authorizationHeader: () => x.credentials.token,
    ownerID: () => x.credentials.owner,
    identityChanges: x.credentials,
    workspaceChanges: x.credentials,
    organizationWorkspaceID: () => x.credentials.workspace,
    sourceCurrent: () => x.credentials.source,
    apiBaseUrl: 'https://birdtie.example',
    client: x.client,
    picker: x.picker,
    clock: () => x.now,
    pendingStore: MemoryPrivateImagePendingStore(),
    textRecognizer: spy,
  );
  return x;
}

Future<void> advanceImage(WidgetTester t, bool Function() busy) async {
  for (var i = 0; i < 60 && busy(); i++) {
    await t.pump(const Duration(milliseconds: 16));
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 1)),
    );
  }
  await t.pumpAndSettle();
}

Future<void> reachTextControl(WidgetTester t, Finder target) async {
  await t.scrollUntilVisible(
    target,
    120,
    scrollable: find.byType(Scrollable).first,
  );
  await t.pumpAndSettle();
  expect(target.hitTestable(), findsOneWidget);
}

void main() {
  testWidgets('本机文字检查：原私人图片页选图后有实际主动入口', (t) async {
    final x = MediaUnitFixture((await t.runAsync(syntheticPrivatePNG))!);
    await t.pumpWidget(
      MaterialApp(
        home: PrivateMomentMediaSheet(
          controller: x.controller,
          momentTitle: '本人私人记录',
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('选择一张图片'));
    await advanceImage(t, () => x.controller.busy);
    expect(x.controller.hasLocalImage, isTrue);
    expect(x.requests.where((r) => r.method != 'GET'), isEmpty);
    expect(find.text('检查图片中的文字（本机）'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    x.close();
  });
  testWidgets('本机文字检查：真实页主动检查选择确认后才改变真实像素', (t) async {
    t.view.physicalSize = const Size(320, 720);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final spy = LocalTextSpy(),
        x = textFixture((await t.runAsync(syntheticPrivatePNG))!, spy);
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(2)),
          child: child!,
        ),
        home: PrivateMomentMediaSheet(
          controller: x.controller,
          momentTitle: '本人私人记录',
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('选择一张图片'));
    await advanceImage(t, () => x.controller.busy);
    final before = x.controller.localBytes!;
    expect(spy.calls, 0);
    final check = find.text('检查图片中的文字（本机）');
    await reachTextControl(t, check);
    await t.tap(check);
    await t.pumpAndSettle();
    expect(spy.calls, 1);
    expect(spy.input, before);
    expect(x.controller.pixelRisk, 'UNKNOWN');
    expect(x.controller.localBytes, same(before));
    expect(x.controller.localReviewed, isFalse);
    final hint = find.byKey(const ValueKey('private-image-text-hint-0'));
    await reachTextControl(t, hint);
    await t.tap(hint);
    await t.pump();
    final mask = find.text('检查并遮挡所选文字');
    await reachTextControl(t, mask);
    await t.tap(mask);
    await t.pumpAndSettle();
    expect(find.text('遮挡所选文字区域？'), findsOneWidget);
    await t.tap(find.text('返回检查'));
    await t.pumpAndSettle();
    expect(x.controller.localBytes, same(before));
    await reachTextControl(t, mask);
    await t.tap(mask);
    await t.pumpAndSettle();
    await t.tap(find.text('确认遮挡所选区域'));
    await advanceImage(t, () => x.controller.busy);
    expect(x.controller.pixelRisk, 'USER_MASKED');
    expect(x.controller.localReviewed, isFalse);
    expect(x.controller.textHints, isEmpty);
    await t.runAsync(() async {
      final codec = await ui.instantiateImageCodec(x.controller.localBytes!);
      final frame = await codec.getNextFrame();
      codec.dispose();
      final raw = await frame.image.toByteData(
        format: ui.ImageByteFormat.rawRgba,
      );
      expect(raw!.buffer.asUint8List().sublist(0, 4), [0, 0, 0, 255]);
      expect(raw.buffer.asUint8List().sublist(60, 64), [255, 0, 0, 255]);
      frame.image.dispose();
    });
    expect(x.requests.where((r) => r.method != 'GET'), isEmpty);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    x.close();
  });
  testWidgets('本机文字检查：空或不可用不称安全也不自动提交', (t) async {
    await t.runAsync(() async {
      final spy = LocalTextSpy()..empty = true,
          x = textFixture(await syntheticPrivatePNG(), spy);
      await x.local();
      await x.controller.checkLocalText();
      expect(x.controller.textHints, isEmpty);
      expect(x.controller.textMessage, contains('不表示图片安全'));
      expect(x.controller.localReviewed, isFalse);
      expect(x.requests, isEmpty);
      spy.unavailable = true;
      final before = x.controller.localBytes;
      await x.controller.checkLocalText();
      expect(x.controller.textMessage, contains('仍可手动遮挡'));
      expect(x.controller.localBytes, same(before));
      expect(x.controller.canChangeLocal, isTrue);
      expect(x.controller.pixelRisk, 'UNKNOWN');
      x.close();
    });
  });
  testWidgets('本机文字检查：忙碌通知身份退休时零识别，ABA迟到结果丢弃', (t) async {
    await t.runAsync(() async {
      final spy = LocalTextSpy(),
          x = textFixture(await syntheticPrivatePNG(), spy);
      await x.local();
      x.controller.addListener(() {
        if (x.controller.textChecking) {
          x.credentials.source = false;
          x.credentials.event();
        }
      });
      await x.controller.checkLocalText();
      expect(spy.calls, 0);
      expect(x.controller.retired, isTrue);
      x.close();
      final late = LocalTextSpy()..pending = Completer(),
          y = textFixture(await syntheticPrivatePNG(), late);
      await y.local();
      final check = y.controller.checkLocalText();
      expect(late.calls, 1);
      y.credentials.actor('Bearer synthetic-b');
      y.credentials.actor('Bearer synthetic-a');
      late.pending!.complete(late.result());
      await check;
      expect(y.controller.retired, isTrue);
      expect(y.controller.textHints, isEmpty);
      expect(y.controller.localBytes, isNull);
      expect(y.requests, isEmpty);
      y.close();
    });
  });
  testWidgets('本机文字检查：重新选图清除取消与关闭不能收取旧结果', (t) async {
    await t.runAsync(() async {
      for (final action in ['select', 'discard', 'cancel', 'close']) {
        final spy = LocalTextSpy()..pending = Completer(),
            x = textFixture(await syntheticPrivatePNG(), spy);
        await x.local();
        final check = x.controller.checkLocalText();
        expect(x.controller.canPreview, isFalse);
        switch (action) {
          case 'select':
            await x.controller.select();
          case 'discard':
            x.controller.discard();
          case 'cancel':
            x.controller.cancelLocalText();
          case 'close':
            x.controller.retire();
        }
        spy.pending!.complete(spy.result());
        await check;
        expect(x.controller.textHints, isEmpty);
        expect(x.controller.textChecking, isFalse);
        expect(x.requests, isEmpty);
        expect(spy.cancels, greaterThan(0));
        x.close();
      }
    });
  });
  testWidgets('本机文字检查：确认框等待期间来源退休不得遮挡新图片', (t) async {
    final spy = LocalTextSpy(),
        x = textFixture((await t.runAsync(syntheticPrivatePNG))!, spy);
    await t.runAsync(x.local);
    await x.controller.checkLocalText();
    x.controller.selectTextHint(0, true);
    await t.pumpWidget(
      MaterialApp(
        home: PrivateMomentMediaSheet(
          controller: x.controller,
          momentTitle: '本人私人记录',
        ),
      ),
    );
    await t.pumpAndSettle();
    // Initial list GET intentionally invalidates an earlier local check; recheck current image.
    await x.controller.checkLocalText();
    x.controller.selectTextHint(0, true);
    await t.pump();
    final button = find.text('检查并遮挡所选文字');
    await reachTextControl(t, button);
    await t.tap(button);
    await t.pumpAndSettle();
    x.credentials.source = false;
    x.credentials.event();
    await t.pump();
    await t.tap(find.text('确认遮挡所选区域'));
    await t.pumpAndSettle();
    expect(x.controller.retired, isTrue);
    expect(x.controller.localBytes, isNull);
    expect(x.requests.where((r) => r.method != 'GET'), isEmpty);
    await t.pumpWidget(const SizedBox());
    x.close();
  });
}
