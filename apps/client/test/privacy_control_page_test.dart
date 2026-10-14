import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:birdtie_client/src/workspace/human_causal_pending_store.dart';
import 'package:birdtie_client/src/workspace/privacy_control_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'human_causal_control_test.dart';
import 'human_causal_pending_store_test.dart'
    show humanBlockInput, humanRemember;
import 'batch_a_visual_test.dart' show visualTheme;

Future<void> humanFonts() async {
  if (Platform.environment['BIRDTIE_B5_CAPTURE'] == null) return;
  for (final v in [
    ('BatchACJK', r'C:\Windows\Fonts\msyh.ttc'),
    (
      'MaterialIcons',
      r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
    ),
  ]) {
    await (FontLoader(v.$1)..addFont(
          Future.value(ByteData.sublistView(await File(v.$2).readAsBytes())),
        ))
        .load();
  }
}

const humanCaptureKey = Key('human-control-isolated-render');
Widget humanApp(Widget child, {ThemeData? theme}) => MaterialApp(
  debugShowCheckedModeBanner: false,
  theme: (theme ?? visualTheme()).copyWith(
    inputDecorationTheme: (theme ?? visualTheme()).inputDecorationTheme
        .copyWith(counterStyle: const TextStyle(fontFamily: 'BatchACJK')),
  ),
  builder: (context, child) => RepaintBoundary(
    key: humanCaptureKey,
    child: Column(
      children: [
        Expanded(child: child!),
        const Material(
          child: Padding(
            padding: EdgeInsets.all(6),
            child: Text(
              '隔离测试渲染 · 合成身份 · 非真实提交或授权',
              style: TextStyle(fontFamily: 'BatchACJK', fontSize: 11),
            ),
          ),
        ),
      ],
    ),
  ),
  home: child,
);
void humanViewport(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Future<void> humanCapture(WidgetTester t, String name) async {
  final path = Platform.environment['BIRDTIE_B5_CAPTURE'];
  if (path == null) return;
  await t.pumpAndSettle();
  await t.runAsync(() async {
    final im = await t
        .renderObject<RenderRepaintBoundary>(find.byKey(humanCaptureKey))
        .toImage(pixelRatio: 1);
    final bytes = await im.toByteData(format: ui.ImageByteFormat.png);
    final f = File('$path/$name.png');
    await f.parent.create(recursive: true);
    if (await f.exists()) throw StateError('Immutable capture exists');
    await f.writeAsBytes(bytes!.buffer.asUint8List());
    im.dispose();
  });
}

Widget privacyHarness(
  HumanControlFixture f,
  MemoryHumanCausalPendingStore store, {
  HumanCausalFamily family = HumanCausalFamily.block,
  ValueNotifier<int>? changes,
  String? Function()? owner,
  String target = humanTarget,
}) => humanApp(
  PrivacyControlPage(
    family: family,
    targetID: target,
    authorizationHeader: () => 'Bearer original',
    ownerID: owner ?? () => humanOwner,
    identityChanges: changes,
    client: f.client,
    apiBaseUrl: humanBase,
    pendingStore: store,
  ),
);
void main() {
  setUpAll(humanFonts);
  testWidgets(
    'BLOCK uses exact pair version and confirmation then one PUT normal390',
    (t) async {
      humanViewport(t);
      final f = HumanControlFixture(), store = MemoryHumanCausalPendingStore();
      await t.pumpWidget(privacyHarness(f, store));
      await humanSettle(t);
      expect(find.text('此账号当前未被屏蔽。'), findsOneWidget);
      expect(f.writes, isEmpty);
      await humanCapture(t, 'block-normal390');
      await humanTap(t, find.byKey(const Key('human-privacy-apply')));
      expect(find.textContaining('不会自动恢复好友'), findsWidgets);
      expect(f.writes, isEmpty);
      await humanTap(t, find.byKey(const Key('human-control-confirm')));
      expect(f.writes.length, 1);
      expect(f.blocked, true);
      expect(find.text('此账号当前已被屏蔽。'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      f.client.close();
    },
  );
  testWidgets(
    'expired own Grant remains revokable and success survives current403',
    (t) async {
      humanViewport(t);
      final f = HumanControlFixture()..denyAfterWrite = true;
      await t.pumpWidget(
        privacyHarness(
          f,
          MemoryHumanCausalPendingStore(),
          family: HumanCausalFamily.grantRevoke,
        ),
      );
      await humanSettle(t);
      expect(find.text('检查撤销授权'), findsOneWidget);
      await humanTap(t, find.byKey(const Key('human-privacy-apply')));
      expect(find.textContaining('已发生的读取不会被删除'), findsOneWidget);
      await humanCapture(t, 'grant-confirm-normal390');
      await humanTap(t, find.byKey(const Key('human-control-confirm')));
      expect(f.revoked, true);
      expect(f.writes.single.body, contains('"expectedVersion":4'));
      expect(find.textContaining('原操作已完成，当前资料暂不可读'), findsOneWidget);
      expect(find.text('检查撤销授权'), findsNothing);
      await t.pumpWidget(const SizedBox());
      f.client.close();
    },
  );
  testWidgets(
    'UNKNOWN original recovery is reachable with current state denied and no PUT',
    (t) async {
      humanViewport(t);
      final f = HumanControlFixture()..stateDenied = true,
          store = MemoryHumanCausalPendingStore();
      await humanRemember(
        store,
        humanBase,
        humanOwner,
        HumanCausalPending(humanBlockInput.selector),
      );
      f.receipts[humanOp] = humanReceiptWire(humanBlockInput);
      await t.pumpWidget(privacyHarness(f, store));
      await humanSettle(t);
      expect(find.byKey(const Key('human-control-recover')), findsOneWidget);
      expect(find.byKey(const Key('human-privacy-apply')), findsNothing);
      await humanCapture(t, 'privacy-unknown-normal390');
      await humanTap(t, find.byKey(const Key('human-control-recover')));
      expect(await store.read(humanBase, humanOwner), isEmpty);
      expect(find.textContaining('原操作已完成，当前资料暂不可读'), findsOneWidget);
      expect(f.writes, isEmpty);
      await t.pumpWidget(const SizedBox());
      f.client.close();
    },
  );
  testWidgets(
    'expiry hides old concrete confirmation and captured callback never PUT',
    (t) async {
      humanViewport(t);
      final f = HumanControlFixture()..previewSeconds = 1;
      await t.pumpWidget(privacyHarness(f, MemoryHumanCausalPendingStore()));
      await humanSettle(t);
      await humanTap(t, find.byKey(const Key('human-privacy-apply')));
      final old = t
          .widget<FilledButton>(find.byKey(const Key('human-control-confirm')))
          .onPressed!;
      await t.pump(const Duration(seconds: 2));
      await t.pumpAndSettle();
      expect(find.text('这次确认已失效'), findsOneWidget);
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.textContaining('原账号标识'),
        ),
        findsNothing,
      );
      old();
      await humanSettle(t);
      expect(f.writes, isEmpty);
      await humanCapture(t, 'privacy-expired-normal390');
      await humanTap(t, find.widgetWithText(TextButton, '取消'));
      await t.pumpWidget(const SizedBox());
      f.client.close();
    },
  );
  testWidgets('actor ABA permanently retires private source and old confirm', (
    t,
  ) async {
    final f = HumanControlFixture(), signal = ValueNotifier(0);
    String owner = humanOwner;
    await t.pumpWidget(
      privacyHarness(
        f,
        MemoryHumanCausalPendingStore(),
        changes: signal,
        owner: () => owner,
      ),
    );
    await humanSettle(t);
    await humanTap(t, find.byKey(const Key('human-privacy-apply')));
    final old = t
        .widget<FilledButton>(find.byKey(const Key('human-control-confirm')))
        .onPressed!;
    owner = humanTarget;
    signal.value++;
    owner = humanOwner;
    signal.value++;
    await t.pumpAndSettle();
    expect(find.text('这次确认已失效'), findsOneWidget);
    old();
    await humanSettle(t);
    expect(f.writes, isEmpty);
    expect(find.text('此账号当前未被屏蔽。'), findsNothing);
    await humanTap(t, find.widgetWithText(TextButton, '取消'));
    await t.pumpWidget(const SizedBox());
    signal.dispose();
    f.client.close();
  });
  testWidgets(
    'default dialog reverse survives target and Theme rebuild without stale notifier',
    (t) async {
      final f = HumanControlFixture(),
          target = ValueNotifier(humanTarget),
          store = MemoryHumanCausalPendingStore();
      final theme = visualTheme().copyWith(
        pageTransitionsTheme: const PageTransitionsTheme(
          builders: {
            TargetPlatform.android: FadeUpwardsPageTransitionsBuilder(),
          },
        ),
      );
      Widget app() => humanApp(
        ValueListenableBuilder<String>(
          valueListenable: target,
          builder: (_, id, __) => PrivacyControlPage(
            key: const Key('same-privacy'),
            family: HumanCausalFamily.block,
            targetID: id,
            authorizationHeader: () => 'Bearer original',
            ownerID: () => humanOwner,
            client: f.client,
            apiBaseUrl: humanBase,
            pendingStore: store,
          ),
        ),
        theme: target.value == humanTarget ? theme : ThemeData.dark(),
      );
      await t.pumpWidget(app());
      await humanSettle(t);
      await humanTap(t, find.byKey(const Key('human-privacy-apply')));
      final old = t
          .widget<FilledButton>(find.byKey(const Key('human-control-confirm')))
          .onPressed!;
      await t.tap(find.widgetWithText(TextButton, '取消'));
      await t.pump(const Duration(milliseconds: 20));
      expect(find.byType(AlertDialog), findsOneWidget);
      target.value = humanResult;
      await t.pumpWidget(app());
      old();
      await t.pump(const Duration(milliseconds: 700));
      await humanSettle(t);
      expect(t.takeException(), isNull);
      expect(f.writes, isEmpty);
      await t.pumpWidget(const SizedBox());
      target.dispose();
      f.client.close();
    },
  );
}
