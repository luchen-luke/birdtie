import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'batch_a_visual_test.dart' show visualTheme, capture, captureKey;
import 'moment_control_controller_test.dart';
import 'moment_control_test.dart';

class MomentNavAuth extends BirdtieAuthController {
  MomentNavAuth(this.f) {
    f.identity.addListener(notifyListeners);
  }
  final MomentFixture f;
  @override
  String? get accountID => f.identity.value.$2;
  @override
  String? get authorizationHeader => f.identity.value.$1;
  @override
  bool get signedIn => authorizationHeader != null;
  @override
  String get displayName => '隔离合成本人';
  @override
  String get loginMethod => 'dev_phone';
  @override
  void dispose() {
    f.identity.removeListener(notifyListeners);
    super.dispose();
  }
}

class MomentNavCity extends PublicCityController {
  static const value = PublicCity(
    id: 'aberdeen-gb',
    name: '阿伯丁',
    region: '英国',
    contentStatus: 'published',
    source: PublicSource(
      label: '隔离fixture',
      maintainer: '本地测试',
      freshness: 'unverified',
      updatedAt: null,
    ),
    map: null,
  );
  @override
  PublicCity get selectedCity => value;
  @override
  List<PublicCity> get cities => const [value];
}

Future<void> mcSettle(WidgetTester t) async {
  for (var i = 0; i < 3; i++) {
    await t.pump();
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 30)),
    );
  }
  await t.pumpAndSettle();
}

Future<void> mcMount(
  WidgetTester t,
  MomentFixture f,
  MomentNavAuth auth,
  MomentNavCity city,
) async {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  await f.controller.refresh();
  await t.pumpWidget(
    RepaintBoundary(
      key: captureKey,
      child: MaterialApp(
        debugShowCheckedModeBanner: false,
        theme: visualTheme().copyWith(
          inputDecorationTheme: visualTheme().inputDecorationTheme.copyWith(
            counterStyle: const TextStyle(fontFamily: 'BatchACJK'),
          ),
        ),
        home: Scaffold(
          body: Column(
            children: [
              const Text('隔离测试渲染 · 合成身份 · 非真实提交'),
              Expanded(
                child: LegacyProfilePage(
                  auth: auth,
                  city: city,
                  moments: f.controller,
                  client: f.client,
                  apiBaseUrl: 'https://isolated.invalid',
                ),
              ),
            ],
          ),
        ),
      ),
    ),
  );
  await mcSettle(t);
  await t.ensureVisible(find.text('编辑或撤回'));
  await t.tap(find.text('编辑或撤回'));
  await mcSettle(t);
}

Future<void> mcSavePreview(WidgetTester t) async {
  await t.enterText(find.byType(TextFormField).first, '本人待保存中文标题');
  await t.enterText(find.byType(TextFormField).last, '本人真实补充正文，只用于隔离fixture。');
  await t.ensureVisible(find.text('保存私人草稿'));
  await t.tap(find.text('保存私人草稿'));
  await mcSettle(t);
}

void main() {
  setUpAll(() async {
    if (Platform.environment['BIRDTIE_BATCH_A_CAPTURE'] == null) return;
    for (final font in [
      ('BatchACJK', r'C:\Windows\Fonts\msyh.ttc'),
      (
        'MaterialIcons',
        r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
      ),
    ]) {
      await (FontLoader(font.$1)..addFont(
            Future.value(
              ByteData.sublistView(await File(font.$2).readAsBytes()),
            ),
          ))
          .load();
    }
  });
  testWidgets(
    'Moment normal390 exact private preview cancel then one confirmed PUT',
    (t) async {
      final f = MomentFixture(),
          auth = MomentNavAuth(f),
          city = MomentNavCity();
      addTearDown(city.dispose);
      addTearDown(auth.dispose);
      addTearDown(f.dispose);
      await mcMount(t, f, auth, city);
      await mcSavePreview(t);
      expect(f.puts, 0);
      expect(find.text('检查私人草稿'), findsOneWidget);
      expect(find.text('本人待保存中文标题'), findsWidgets);
      await capture(t, 'moment-preview-normal390');
      await t.tap(find.text('取消').last);
      await mcSettle(t);
      expect(f.puts, 0);
      await t.ensureVisible(find.text('保存私人草稿'));
      await t.tap(find.text('保存私人草稿'));
      await mcSettle(t);
      await t.tap(find.text('确认保存'));
      await mcSettle(t);
      expect(f.puts, 1);
      expect(f.rawWrites, 0);
      expect(f.unexpectedRequests, isEmpty);
      expect(f.controller.moments.single.title, '本人待保存中文标题');
      expect(f.store.values, isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'Moment original unknown remains recoverable after current list403',
    (t) async {
      final f = MomentFixture()..lost = true,
          auth = MomentNavAuth(f),
          city = MomentNavCity();
      addTearDown(city.dispose);
      addTearDown(auth.dispose);
      addTearDown(f.dispose);
      await mcMount(t, f, auth, city);
      final oldSave = t
          .widget<FilledButton>(
            find.ancestor(
              of: find.text('保存私人草稿'),
              matching: find.byType(FilledButton),
            ),
          )
          .onPressed!;
      await mcSavePreview(t);
      await t.tap(find.text('确认保存'));
      await mcSettle(t);
      expect(f.puts, 1);
      expect(f.store.values.length, 1);
      expect(f.controller.pendingOperations.single.action, 'UPDATE');
      final originalPreviews = f.previews, originalPUTs = f.puts;
      oldSave();
      await mcSettle(t);
      expect(
        f.previews,
        originalPreviews,
        reason: 'captured pre-UNKNOWN save cannot request a new approval',
      );
      expect(f.puts, originalPUTs);
      expect(
        t
            .widget<FilledButton>(
              find.ancestor(
                of: find.text('保存私人草稿'),
                matching: find.byType(FilledButton),
              ),
            )
            .onPressed,
        isNull,
      );
      expect(
        t
            .widget<TextButton>(
              find.ancestor(
                of: find.text('撤回草稿'),
                matching: find.byType(TextButton),
              ),
            )
            .onPressed,
        isNull,
      );
      final recovery = find.descendant(
        of: find.byType(BottomSheet),
        matching: find.text('核实原结果'),
      );
      await t.ensureVisible(recovery);
      await t.pumpAndSettle();
      expect(recovery.hitTestable(), findsOneWidget);
      expect(find.text('停止原操作').hitTestable(), findsOneWidget);
      expect(find.textContaining('保存结果待核实').hitTestable(), findsOneWidget);
      await capture(t, 'moment-unknown-normal390');
      f.listStatus = 403;
      await f.controller.refresh();
      await mcSettle(t);
      await t.ensureVisible(find.text('核实原结果').last);
      await t.tap(find.text('核实原结果').last);
      await mcSettle(t);
      expect(f.controller.operationReceipt!.succeeded, isTrue);
      expect(f.controller.moments, isEmpty);
      expect(f.store.values, isEmpty);
      expect(f.puts, 1);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'Moment expiry hides confirmation body and captured callback cannot PUT',
    (t) async {
      final f = MomentFixture(),
          auth = MomentNavAuth(f),
          city = MomentNavCity();
      addTearDown(city.dispose);
      addTearDown(auth.dispose);
      addTearDown(f.dispose);
      await mcMount(t, f, auth, city);
      await mcSavePreview(t);
      final old = t
          .widget<FilledButton>(
            find.ancestor(
              of: find.text('确认保存'),
              matching: find.byType(FilledButton),
            ),
          )
          .onPressed!;
      await t.pump(const Duration(seconds: 31));
      await t.pump();
      expect(find.text('这次确认已失效，原资料与批准已隐藏。请关闭后重新检查，不会执行旧批准。'), findsOneWidget);
      old();
      await t.pump();
      expect(f.puts, 0);
      await capture(t, 'moment-expired-normal390');
      await t.tap(find.text('取消').last);
      await mcSettle(t);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'Moment same owner renewal keeps typed body but retires concrete approval',
    (t) async {
      final f = MomentFixture(),
          auth = MomentNavAuth(f),
          city = MomentNavCity();
      addTearDown(city.dispose);
      addTearDown(auth.dispose);
      addTearDown(f.dispose);
      await mcMount(t, f, auth, city);
      await mcSavePreview(t);
      final old = t
          .widget<FilledButton>(
            find.ancestor(
              of: find.text('确认保存'),
              matching: find.byType(FilledButton),
            ),
          )
          .onPressed!;
      f.identity.value = ('Bearer renewed', mcOwner);
      old();
      await mcSettle(t);
      expect(f.puts, 0);
      await t.tap(find.text('取消').last);
      await mcSettle(t);
      expect(find.byType(TextFormField), findsWidgets);
      expect(
        t
            .widget<TextFormField>(find.byType(TextFormField).first)
            .controller!
            .text,
        '本人待保存中文标题',
      );
      expect(
        t
            .widget<FilledButton>(
              find.ancestor(
                of: find.text('保存私人草稿'),
                matching: find.byType(FilledButton),
              ),
            )
            .onPressed,
        isNotNull,
      );
      final prior = f.previews;
      await t.ensureVisible(find.text('保存私人草稿'));
      await t.tap(find.text('保存私人草稿'));
      await mcSettle(t);
      expect(f.previews, prior + 1);
      expect(find.text('检查私人草稿'), findsOneWidget);
      expect(
        t
            .widget<FilledButton>(
              find.ancestor(
                of: find.text('确认保存'),
                matching: find.byType(FilledButton),
              ),
            )
            .onPressed,
        isNotNull,
      );
      expect(
        f.puts,
        0,
        reason:
            'normal renewal permits a new explicit preview, never sends automatically',
      );
      await t.tap(find.text('取消').last);
      await mcSettle(t);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'Moment actor ABA during reverse dialog animation retires old callback',
    (t) async {
      final f = MomentFixture(),
          auth = MomentNavAuth(f),
          city = MomentNavCity();
      addTearDown(city.dispose);
      addTearDown(auth.dispose);
      addTearDown(f.dispose);
      await mcMount(t, f, auth, city);
      await mcSavePreview(t);
      final old = t
          .widget<FilledButton>(
            find.ancestor(
              of: find.text('确认保存'),
              matching: find.byType(FilledButton),
            ),
          )
          .onPressed!;
      await t.tap(find.text('取消').last);
      await t.pump();
      expect(find.byType(AlertDialog), findsOneWidget);
      f.identity.value = ('Bearer B', mcOp);
      f.identity.value = ('Bearer A again', mcOwner);
      old();
      await mcSettle(t);
      expect(f.puts, 0);
      expect(find.text('账号已切换，请重新打开私人草稿。'), findsOneWidget);
      expect(find.byType(TextFormField), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
  );
}
