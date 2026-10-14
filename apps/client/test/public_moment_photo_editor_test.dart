import 'dart:io';
import 'dart:typed_data';
import 'package:birdtie_client/src/workspace/public_moment_photo_controller.dart';
import 'package:birdtie_client/src/workspace/public_moment_photo_editor.dart';
import 'package:birdtie_client/src/workspace/public_moment_photo_pending_store.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'entity_cover_test.dart' show coverJPEG, coverSettle;
import 'public_moment_photo_test.dart';
import 'public_moment_photo_controller_test.dart';
import 'public_moment_photo_pending_store_test.dart' show photoPending;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'moment_control_navigation_test.dart' show MomentNavCity;
import 'package:http/testing.dart';
import 'batch_a_visual_test.dart' show visualTheme, capture, captureKey;

void photoNormalView(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Widget photoEditorApp(
  PublicMomentPhotoController c, {
  Future<Uint8List?> Function()? picker,
  ThemeData? theme,
}) => RepaintBoundary(
  key: captureKey,
  child: MaterialApp(
    debugShowCheckedModeBanner: false,
    theme: theme ?? visualTheme(),
    home: Column(
      children: [
        Expanded(
          child: PublicMomentPhotoEditor(
            controller: c,
            momentTitle: '隔离样例动态 · 不代表真实发布',
            momentTitleRevision: 2,
            picker: picker,
          ),
        ),
        const ColoredBox(
          color: Colors.white,
          child: Text(
            '隔离测试渲染 · 合成身份 · 非真实提交/公开',
            textDirection: TextDirection.ltr,
            style: TextStyle(
              color: Colors.black,
              fontFamily: 'BatchACJK',
              fontSize: 11,
            ),
          ),
        ),
      ],
    ),
  ),
);
Future<void> photoTap(WidgetTester t, Finder f) async {
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f.hitTestable());
  await coverSettle(t);
}

Future<void> photoRights(WidgetTester t) async {
  await photoTap(t, find.byKey(const Key('public-photo-select')));
  await t.ensureVisible(find.byKey(const Key('public-photo-attribution')));
  await t.enterText(
    find.byKey(const Key('public-photo-attribution')),
    '合成测试图片 · 非真实发布',
  );
  await t.ensureVisible(find.byKey(const Key('public-photo-retention')));
  await t.enterText(
    find.byKey(const Key('public-photo-retention')),
    '2030-01-02',
  );
  t.testTextInput.hide();
  await t.pump();
  await photoTap(t, find.byKey(const Key('public-photo-retain')));
  await photoTap(t, find.byKey(const Key('public-photo-publish')));
}

Future<void> photoPaintedApproval(WidgetTester t) async {
  await photoTap(t, find.byKey(const Key('public-photo-preview')));
  final image = find.descendant(
    of: find.byType(AlertDialog),
    matching: find.byType(Image),
  );
  await photoDecodeVisible(t, image: image);
  expect(
    t
        .widget<FilledButton>(find.widgetWithText(FilledButton, '确认公开图片'))
        .onPressed,
    isNotNull,
  );
}

void main() {
  setUpAll(() async {
    if (Platform.environment['BIRDTIE_BATCH_A_CAPTURE'] != null) {
      for (final f in [
        ('BatchACJK', r'C:\Windows\Fonts\msyh.ttc'),
        (
          'MaterialIcons',
          r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
        ),
      ]) {
        await (FontLoader(f.$1)..addFont(
              Future.value(
                ByteData.sublistView(await File(f.$2).readAsBytes()),
              ),
            ))
            .load();
      }
    }
  });
  for (final lost in [false, true])
    testWidgets(
      'original person profile public-photo entry and cold history recovery lostList=$lost',
      (t) async {
        photoNormalView(t);
        FlutterSecureStorage.setMockInitialValues({});
        final auth = SeedTestAuth()
              ..owner = photoOwner
              ..token = 'Bearer isolated-A',
            city = MomentNavCity(),
            f = PhotoFixture()..unavailable = lost;
        addTearDown(auth.dispose);
        addTearDown(city.dispose);
        addTearDown(f.dispose);
        if (lost) {
          const store = SecurePublicMomentPhotoPendingStore();
          final p = photoPending();
          await t.runAsync(() async {
            await store.write(photoBase, photoOwner, p);
            await store.write(photoBase, photoOwner, p.unknown);
          });
          f.approved = photoApproval(photoInput().body());
        }
        final client = MockClient((r) async {
          if (r.url.path.contains('/public-photo')) return f.request(r);
          if (r.url.path == '/v1/me/moments')
            return photoWire([
              {
                'id': photoMoment,
                'authorAccountId': photoOwner,
                'cityId': 'aberdeen-gb',
                'placeId': photoSlot,
                'title': '本人公开动态合成样例',
                'body': '正文不进入图片审批',
                'timePrecision': 'unknown',
                'locationPrecision': 'city',
                'visibility': 'public',
                'status': 'published',
                'revision': 2,
                'activityIds': [],
              },
            ], status: lost ? 403 : 200);
          return photoWire([]);
        });
        final moments = PrivateMomentController(
          authorizationHeader: () => auth.authorizationHeader,
          ownerID: () => auth.accountID,
          identityChanges: auth,
          client: client,
          apiBaseUrl: photoBase,
          pendingStore: MemoryMomentControlPendingStore(),
        );
        addTearDown(moments.dispose);
        await t.runAsync(moments.refresh);
        await t.pumpWidget(
          MaterialApp(
            debugShowCheckedModeBanner: false,
            theme: visualTheme(),
            home: Scaffold(
              body: LegacyProfilePage(
                auth: auth,
                moments: moments,
                city: city,
                client: client,
                apiBaseUrl: photoBase,
              ),
            ),
          ),
        );
        await coverSettle(t);
        if (lost) {
          expect(find.text('本人公开动态合成样例'), findsNothing);
          await photoTap(t, find.text('核实原图片操作 1'));
          await photoTap(t, find.text('核实原结果'));
          expect(f.originalReads, 1);
          expect(find.textContaining('原公开图片操作已完成'), findsOneWidget);
          expect(find.textContaining('当前图片不可读取'), findsOneWidget);
          expect(
            await const SecurePublicMomentPhotoPendingStore().read(
              photoBase,
              photoOwner,
            ),
            isEmpty,
          );
        } else {
          await photoTap(t, find.text('管理本人公开图片'));
          expect(find.text('公开动态图片'), findsOneWidget);
          expect(find.text('重新选择本机图片'), findsOneWidget);
          expect(find.byType(Image), findsNothing);
        }
        expect(f.previews, 0);
        expect(f.puts, 0);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
      },
    );
  testWidgets(
    'public photo normal390 explicit reselect real JPEG confirmation cancel has zero effect',
    (t) async {
      photoNormalView(t);
      final f = PhotoFixture(), c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      var picks = 0;
      await t.pumpWidget(
        photoEditorApp(
          c,
          picker: () async {
            picks++;
            return coverJPEG();
          },
        ),
      );
      await coverSettle(t);
      expect(picks, 0);
      expect(find.byType(Image), findsNothing);
      await photoRights(t);
      expect(picks, 1);
      await capture(t, 'moment-public-photo-editor-normal');
      await photoPaintedApproval(t);
      await capture(t, 'moment-public-photo-confirm');
      expect(f.puts, 0);
      await photoTap(t, find.widgetWithText(TextButton, '取消'));
      expect(f.puts, 0);
      expect(c.localBytes, isNotNull);
      expect(c.pending, isEmpty);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'public photo UPDATE unknown disabled new writes reachable original GET and bodyless STOP',
    (t) async {
      photoNormalView(t);
      final f = PhotoFixture()
        ..version = 1
        ..unknown = true
        ..missingReceipt = true;
      final c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await t.pumpWidget(photoEditorApp(c, picker: () async => coverJPEG()));
      await coverSettle(t);
      await photoRights(t);
      final old = t
          .widget<FilledButton>(find.byKey(const Key('public-photo-preview')))
          .onPressed!;
      await photoPaintedApproval(t);
      await photoTap(t, find.widgetWithText(FilledButton, '确认公开图片'));
      expect(f.puts, 1);
      await t.ensureVisible(find.byKey(const Key('public-photo-preview')));
      await t.pump();
      expect(
        t
            .widget<FilledButton>(find.byKey(const Key('public-photo-preview')))
            .onPressed,
        isNull,
      );
      await t.ensureVisible(find.text('检查移除当前公开图片'));
      await t.pump();
      expect(
        t
            .widget<TextButton>(find.widgetWithText(TextButton, '检查移除当前公开图片'))
            .onPressed,
        isNull,
      );
      old();
      await coverSettle(t);
      expect(f.previews, 1);
      expect(f.puts, 1);
      await photoTap(t, find.text('核实原结果'));
      expect(f.originalReads, 1);
      expect(f.puts, 1);
      await t.ensureVisible(find.text('停止原操作'));
      Scrollable.of(t.element(find.text('核实原结果'))).position.jumpTo(0);
      await t.pump();
      expect(find.textContaining('原图片结果仍待核实').hitTestable(), findsOneWidget);
      expect(find.text('核实原结果').hitTestable(), findsOneWidget);
      expect(find.text('停止原操作').hitTestable(), findsOneWidget);
      await capture(t, 'moment-public-photo-unknown');
      await photoTap(t, find.widgetWithText(TextButton, '停止原操作'));
      await photoTap(t, find.widgetWithText(FilledButton, '停止原操作'));
      expect(f.stops, 1);
      expect(f.puts, 1);
      expect(c.receipt?.status, 'STOPPED');
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'public photo expired approval hides normalized pixels and old callback cannot PUT',
    (t) async {
      photoNormalView(t);
      final f = PhotoFixture()..previewLease = const Duration(seconds: 3),
          c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await t.pumpWidget(photoEditorApp(c, picker: () async => coverJPEG()));
      await coverSettle(t);
      await photoRights(t);
      await photoPaintedApproval(t);
      final old = t
          .widget<FilledButton>(find.widgetWithText(FilledButton, '确认公开图片'))
          .onPressed!;
      await t.pump(const Duration(seconds: 4));
      await t.pump();
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(Image),
        ),
        findsNothing,
      );
      expect(
        t
            .widget<FilledButton>(find.widgetWithText(FilledButton, '确认公开图片'))
            .onPressed,
        isNull,
      );
      old();
      await t.pump();
      expect(f.puts, 0);
      await capture(t, 'moment-public-photo-confirm-expired');
      await photoTap(t, find.widgetWithText(TextButton, '取消'));
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'public photo same-owner renewal preserves rights local selection; old approval zero PUT new preview valid',
    (t) async {
      photoNormalView(t);
      final f = PhotoFixture(), c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await t.pumpWidget(photoEditorApp(c, picker: () async => coverJPEG()));
      await coverSettle(t);
      await photoRights(t);
      await photoPaintedApproval(t);
      final old = t
          .widget<FilledButton>(find.widgetWithText(FilledButton, '确认公开图片'))
          .onPressed!;
      f.header = 'Bearer renewed-isolated';
      f.notify();
      await coverSettle(t);
      old();
      await t.pump();
      expect(f.puts, 0);
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(Image),
        ),
        findsNothing,
      );
      await photoTap(t, find.widgetWithText(TextButton, '取消'));
      await t.ensureVisible(find.byKey(const Key('public-photo-preview')));
      await t.pump();
      expect(
        t
            .widget<FilledButton>(find.byKey(const Key('public-photo-preview')))
            .onPressed,
        isNotNull,
      );
      expect(
        t
            .widget<TextField>(
              find.byKey(const Key('public-photo-attribution')),
            )
            .controller!
            .text,
        '合成测试图片 · 非真实发布',
      );
      await photoPaintedApproval(t);
      expect(f.previews, 2);
      expect(f.puts, 0);
      await photoTap(t, find.widgetWithText(TextButton, '取消'));
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'public photo actual decode failure disables confirmation rather than placeholder approved',
    (t) async {
      photoNormalView(t);
      final bad = coverJPEG();
      for (var i = 0; i < bad.length - 4; i++) {
        if (bad[i] == 255 && bad[i + 1] == 192) {
          bad[i + 4] = 255;
          break;
        }
      }
      final f = PhotoFixture()..derivative = bad, c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await t.pumpWidget(photoEditorApp(c, picker: () async => coverJPEG()));
      await coverSettle(t);
      await photoRights(t);
      await photoTap(t, find.byKey(const Key('public-photo-preview')));
      final image = find.descendant(
        of: find.byType(AlertDialog),
        matching: find.byType(Image),
      );
      expect(image, findsOneWidget);
      await t.runAsync(
        () => precacheImage(
          t.widget<Image>(image).image,
          t.element(image),
          onError: (_, __) {},
        ),
      );
      await t.pump();
      await t.pumpAndSettle();
      expect(
        t
            .widget<FilledButton>(find.widgetWithText(FilledButton, '确认公开图片'))
            .onPressed,
        isNull,
      );
      expect(f.puts, 0);
      await photoTap(t, find.widgetWithText(TextButton, '取消'));
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'public photo reverse animation retains notifier until owned dialog unmount and target ABA is retired',
    (t) async {
      photoNormalView(t);
      final first = PhotoFixture(),
          second = PhotoFixture(),
          old = first.controller(),
          fresh = second.controller();
      addTearDown(old.dispose);
      addTearDown(fresh.dispose);
      addTearDown(first.dispose);
      addTearDown(second.dispose);
      var controller = old;
      late StateSetter rebuild;
      await t.pumpWidget(
        StatefulBuilder(
          builder: (context, set) {
            rebuild = set;
            return photoEditorApp(controller, picker: () async => coverJPEG());
          },
        ),
      );
      await coverSettle(t);
      await photoRights(t);
      await photoPaintedApproval(t);
      final confirm = t
          .widget<FilledButton>(find.widgetWithText(FilledButton, '确认公开图片'))
          .onPressed!;
      await t.tap(find.widgetWithText(TextButton, '取消'));
      await t.pump(const Duration(milliseconds: 10));
      expect(find.byType(AlertDialog), findsOneWidget);
      rebuild(() => controller = fresh);
      await t.pump();
      first.source = 1;
      first.notify();
      first.source = 0;
      first.notify();
      confirm();
      await coverSettle(t);
      expect(t.takeException(), isNull);
      expect(first.puts, 0);
      expect(second.puts, 0);
      await t.pumpWidget(const SizedBox());
    },
  );
}
