import 'dart:typed_data';
import 'dart:io';
import 'package:flutter/services.dart';
import 'package:birdtie_client/src/workspace/local_visual_draft.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/workspace/agent_visual_page.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/task_image_controller.dart';
import 'agent_visual_controller_test.dart'
    show VisualFixture, visualOwner, visualSource, visualOldOperation;
import 'friend_image_test.dart' show imageFixtureBytes;
import 'batch_a_visual_test.dart' show visualTheme, capture, captureKey;

void visualLocalNormalView(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Future<void> visualLocalSettle(WidgetTester t) async {
  for (var i = 0; i < 3; i++) {
    await t.pump();
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 30)),
    );
  }
  await t.pumpAndSettle();
}

Widget visualLocalApp(
  VisualFixture f,
  Future<bool> Function(LocalVisualDraft) save, {
  ThemeData? theme,
}) => RepaintBoundary(
  key: captureKey,
  child: MaterialApp(
    debugShowCheckedModeBanner: false,
    theme: (theme ?? visualTheme()).copyWith(
      inputDecorationTheme: (theme ?? visualTheme()).inputDecorationTheme
          .copyWith(counterStyle: const TextStyle(fontFamily: 'BatchACJK')),
    ),
    builder: (context, child) => Column(
      children: [
        Expanded(child: child!),
        const ColoredBox(
          color: Colors.white,
          child: Text(
            '隔离测试渲染 · NOT_REAL_VISUAL · NOT_DEVICE',
            style: TextStyle(
              color: Colors.black,
              fontSize: 11,
              fontFamily: 'BatchACJK',
            ),
          ),
        ),
      ],
    ),
    home: AgentVisualPage(controller: f.controller, onSaveLocalDraft: save),
  ),
);
Future<void> visualLocalTap(WidgetTester t, Finder f) async {
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f.hitTestable());
  await visualLocalSettle(t);
}

Future<void> visualLocalApproveAnalysis(WidgetTester t, VisualFixture f) async {
  await t.runAsync(f.controller.prepare);
  await visualLocalSettle(t);
  final image = find.byType(Image);
  expect(image, findsOneWidget);
  await t.runAsync(
    () => precacheImage(t.widget<Image>(image).image, t.element(image)),
  );
  await t.pump();
  await t.pumpAndSettle();
  expect(
    t
        .widget<RawImage>(
          find.descendant(of: image, matching: find.byType(RawImage)),
        )
        .image,
    isNotNull,
  );
  await visualLocalTap(t, find.text('我已查看这次图片，批准将其用于上述分析'));
  await visualLocalTap(t, find.text('批准并分析'));
  expect(f.controller.report, isNotNull);
}

class _Picker extends MomentImagePicker {
  _Picker(this.bytes);
  final Uint8List bytes;
  @override
  Future<bool> discardLostSelection() async => false;
  @override
  Future<MomentImageSelection?> select() async => MomentImageSelection(bytes);
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
  testWidgets(
    'local visual empty question editable own text saves only after explicit check with zero HTTP',
    (t) async {
      visualLocalNormalView(t);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final bytes = (await t.runAsync(imageFixtureBytes))!,
          f = VisualFixture(bytes)..now = DateTime.now().toUtc();
      addTearDown(f.dispose);
      final saved = <LocalVisualDraft>[];
      await t.pumpWidget(
        visualLocalApp(f, (d) async {
          saved.add(d);
          return true;
        }),
      );
      await visualLocalSettle(t);
      expect(
        t
            .widget<OutlinedButton>(
              find.byKey(const Key('local-visual-save-question')),
            )
            .onPressed,
        isNull,
      );
      await t.enterText(
        find.byKey(const Key('local-visual-question')),
        '本人未分析的图片问题，稍后再核实',
      );
      t.testTextInput.hide();
      await t.pump();
      await visualLocalTap(
        t,
        find.byKey(const Key('local-visual-save-question')),
      );
      expect(f.calls, isEmpty);
      expect(saved, isEmpty);
      await capture(t, 'local-visual-own-question-confirm');
      await visualLocalTap(t, find.text('取消'));
      expect(saved, isEmpty);
      await visualLocalTap(
        t,
        find.byKey(const Key('local-visual-save-question')),
      );
      await visualLocalTap(t, find.byKey(const Key('local-visual-confirm')));
      expect(saved.single.text, '本人未分析的图片问题，稍后再核实');
      expect(saved.single.status, LocalVisualDraftStatus.ownQuestion);
      expect(saved.single.reportProvenance, isNull);
      expect(f.calls, isEmpty);
      expect(f.controller.report, isNull);
      expect(f.controller.userText, isEmpty);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'local visual verified current report separately editable retains unknown provenance with no extra POST',
    (t) async {
      visualLocalNormalView(t);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final bytes = (await t.runAsync(imageFixtureBytes))!,
          f = VisualFixture(bytes, text: '本人的原问题')
            ..now = DateTime.now().toUtc();
      addTearDown(f.dispose);
      final saved = <LocalVisualDraft>[];
      await t.pumpWidget(
        visualLocalApp(f, (d) async {
          saved.add(d);
          return true;
        }),
      );
      await visualLocalApproveAnalysis(t, f);
      final count = f.calls.length;
      await t.ensureVisible(find.byKey(const Key('local-visual-report-note')));
      await t.enterText(
        find.byKey(const Key('local-visual-report-note')),
        '本人改写的观察摘录，仍需核实',
      );
      t.testTextInput.hide();
      await t.pump();
      await visualLocalTap(
        t,
        find.byKey(const Key('local-visual-save-report')),
      );
      await capture(t, 'local-visual-unverified-report-confirm');
      expect(saved, isEmpty);
      await visualLocalTap(t, find.byKey(const Key('local-visual-confirm')));
      expect(saved.single.status, LocalVisualDraftStatus.unverifiedReportNote);
      expect(saved.single.text, contains('本人改写的观察摘录'));
      expect(saved.single.text, contains('不是地点事实、到访证据或记忆权威'));
      expect(saved.single.text, contains('拍摄地点未知'));
      expect(saved.single.text, contains('拍摄时间未知'));
      expect(saved.single.text, isNot(contains('本人的原问题')));
      expect(
        saved.single.reportProvenance?.operationID,
        f.dispatched!['operationId'],
      );
      expect(f.calls.length, count);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'local visual ordinary renewal preserves own edit retires old model and local confirmations without auto request',
    (t) async {
      visualLocalNormalView(t);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final bytes = (await t.runAsync(imageFixtureBytes))!,
          f = VisualFixture(bytes, text: '原问题')..now = DateTime.now().toUtc();
      addTearDown(f.dispose);
      final saved = <LocalVisualDraft>[];
      await t.pumpWidget(
        visualLocalApp(f, (d) async {
          saved.add(d);
          return true;
        }),
      );
      await visualLocalSettle(t);
      await t.enterText(
        find.byKey(const Key('local-visual-question')),
        '续期后继续编辑的本人问题',
      );
      t.testTextInput.hide();
      await t.pump();
      await visualLocalTap(
        t,
        find.byKey(const Key('local-visual-save-question')),
      );
      final old = t
          .widget<FilledButton>(find.byKey(const Key('local-visual-confirm')))
          .onPressed!;
      f.token = 'Bearer renewed-synthetic';
      f.changes.value++;
      await visualLocalSettle(t);
      old();
      await t.pump();
      expect(saved, isEmpty);
      expect(f.calls, isEmpty);
      expect(find.byKey(const Key('local-visual-preview')), findsNothing);
      await visualLocalTap(t, find.text('取消'));
      expect(
        t
            .widget<TextField>(find.byKey(const Key('local-visual-question')))
            .controller!
            .text,
        '续期后继续编辑的本人问题',
      );
      expect(f.controller.current, isTrue);
      expect(f.controller.localPixels, isNotNull);
      await visualLocalTap(
        t,
        find.byKey(const Key('local-visual-save-question')),
      );
      await visualLocalTap(t, find.byKey(const Key('local-visual-confirm')));
      expect(saved.single.text, '续期后继续编辑的本人问题');
      expect(f.calls, isEmpty);
      await t.runAsync(f.controller.prepare);
      await visualLocalSettle(t);
      final image = find.byType(Image);
      await t.runAsync(
        () => precacheImage(t.widget<Image>(image).image, t.element(image)),
      );
      await t.pumpAndSettle();
      expect(
        t
            .widget<RawImage>(
              find.descendant(of: image, matching: find.byType(RawImage)),
            )
            .image,
        isNotNull,
      );
      await visualLocalTap(t, find.text('我已查看这次图片，批准将其用于上述分析'));
      final nativeOld = t
          .widget<FilledButton>(find.widgetWithText(FilledButton, '批准并分析'))
          .onPressed!;
      final before = f.calls.length;
      f.token = 'Bearer second-renewed-synthetic';
      f.changes.value++;
      await visualLocalSettle(t);
      nativeOld();
      await visualLocalSettle(t);
      expect(f.calls.length, before);
      expect(f.calls.where((r) => r.url.path.endsWith('/analyze')), isEmpty);
      expect(f.controller.localPixels, isNotNull);
      expect(
        t
            .widget<TextField>(find.byKey(const Key('local-visual-question')))
            .controller!
            .text,
        '续期后继续编辑的本人问题',
      );
      await t.pumpWidget(const SizedBox());
    },
  );
  for (final axis in ['owner', 'selection'])
    testWidgets(
      'local visual $axis ABA masks old private text and captured confirmation has zero callback',
      (t) async {
        t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
        final bytes = (await t.runAsync(imageFixtureBytes))!,
            f = VisualFixture(
              bytes,
              text: 'PRIVATE_VISUAL_QUESTION_MUST_RETIRE',
            )..now = DateTime.now().toUtc();
        addTearDown(f.dispose);
        var saves = 0;
        await t.pumpWidget(
          visualLocalApp(f, (d) async {
            saves++;
            return true;
          }),
        );
        await visualLocalSettle(t);
        await visualLocalTap(
          t,
          find.byKey(const Key('local-visual-save-question')),
        );
        final old = t
            .widget<FilledButton>(find.byKey(const Key('local-visual-confirm')))
            .onPressed!;
        if (axis == 'owner') {
          f.owner = null;
          f.changes.value++;
          f.owner = visualOwner;
          f.changes.value++;
        } else {
          f.draftCurrent = false;
          f.changes.value++;
          f.draftCurrent = true;
          f.changes.value++;
        }
        await visualLocalSettle(t);
        expect(find.text('PRIVATE_VISUAL_QUESTION_MUST_RETIRE'), findsNothing);
        expect(find.byKey(const Key('local-visual-preview')), findsNothing);
        old();
        await t.pump();
        expect(saves, 0);
        expect(f.calls, isEmpty);
        await visualLocalTap(t, find.text('取消'));
        await t.pumpWidget(const SizedBox());
      },
    );
  testWidgets(
    'local visual cold CITY journal original GET preserves history but cannot pair current pixels or copy',
    (t) async {
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final bytes = (await t.runAsync(imageFixtureBytes))!,
          f = VisualFixture(bytes, text: '当前重新选图的问题')
            ..now = DateTime.now().toUtc();
      addTearDown(f.dispose);
      f.pending.value = visualOldOperation;
      final saved = <LocalVisualDraft>[];
      await t.runAsync(f.controller.prepare);
      await t.runAsync(f.controller.recover);
      expect(f.calls.single.method, 'GET');
      expect(f.controller.report, isNotNull);
      expect(f.controller.localReportProvenance, isNull);
      await t.pumpWidget(
        visualLocalApp(f, (d) async {
          saved.add(d);
          return true;
        }),
      );
      await visualLocalSettle(t);
      expect(find.byType(Image), findsNothing);
      expect(find.text('原操作历史结果（未绑定当前选图）'), findsOneWidget);
      expect(find.byKey(const Key('local-visual-save-report')), findsNothing);
      await visualLocalTap(
        t,
        find.byKey(const Key('local-visual-save-question')),
      );
      await visualLocalTap(t, find.byKey(const Key('local-visual-confirm')));
      expect(saved.single.text, '当前重新选图的问题');
      expect(saved.single.reportProvenance, isNull);
      expect(f.calls.length, 1);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'local visual failed local store keeps edited text without analysis or false saved state',
    (t) async {
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final bytes = (await t.runAsync(imageFixtureBytes))!,
          f = VisualFixture(bytes, text: '本人问题')..now = DateTime.now().toUtc();
      addTearDown(f.dispose);
      await t.pumpWidget(visualLocalApp(f, (d) async => false));
      await visualLocalSettle(t);
      await t.enterText(
        find.byKey(const Key('local-visual-question')),
        '失败时仍保留的本人文字',
      );
      t.testTextInput.hide();
      await t.pump();
      await visualLocalTap(
        t,
        find.byKey(const Key('local-visual-save-question')),
      );
      await visualLocalTap(t, find.byKey(const Key('local-visual-confirm')));
      expect(
        t
            .widget<TextField>(find.byKey(const Key('local-visual-question')))
            .controller!
            .text,
        '失败时仍保留的本人文字',
      );
      expect(find.textContaining('本机保存未完成'), findsOneWidget);
      expect(f.calls, isEmpty);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'local visual parent route retirement while reverse dialog mounted releases timers and notifiers safely',
    (t) async {
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final bytes = (await t.runAsync(imageFixtureBytes))!,
          f = VisualFixture(bytes, text: 'PRIVATE_REVERSE_LOCAL_VISUAL')
            ..now = DateTime.now().toUtc();
      addTearDown(f.dispose);
      late StateSetter rebuild;
      var dark = false, saves = 0;
      Future<bool> save(LocalVisualDraft d) async {
        saves++;
        return true;
      }

      await t.pumpWidget(
        StatefulBuilder(
          builder: (context, set) {
            rebuild = set;
            return visualLocalApp(
              f,
              save,
              theme: dark ? ThemeData.dark() : visualTheme(),
            );
          },
        ),
      );
      await visualLocalSettle(t);
      await visualLocalTap(
        t,
        find.byKey(const Key('local-visual-save-question')),
      );
      final old = t
          .widget<FilledButton>(find.byKey(const Key('local-visual-confirm')))
          .onPressed!;
      await t.tap(find.text('取消'));
      await t.pump(const Duration(milliseconds: 10));
      expect(find.byType(AlertDialog), findsOneWidget);
      f.controller.dispose();
      rebuild(() => dark = true);
      await t.pump();
      old();
      await visualLocalSettle(t);
      expect(t.takeException(), isNull);
      expect(saves, 0);
      expect(f.calls, isEmpty);
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'pure image arrow opens analysis, creates no text Task and preserves material',
    (t) async {
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final bytes = (await t.runAsync(imageFixtureBytes))!;
      final changes = ValueNotifier<int>(0),
          workspace = AgentWorkspaceController();
      final draft = TaskImageDraftController(
        authorizationHeader: () => 'Bearer synthetic',
        ownerID: () => visualOwner,
        cityID: () => visualSource,
        sourceCurrent: () => true,
        sourceChanges: changes,
        picker: _Picker(bytes),
      );
      addTearDown(draft.dispose);
      addTearDown(changes.dispose);
      addTearDown(workspace.dispose);
      await t.runAsync(draft.select);
      final sent = <String>[], analysis = <String>[];
      await t.pumpWidget(
        MaterialApp(
          theme: birdtieTheme(Brightness.light),
          home: Scaffold(
            body: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onAnalyzeImage: analysis.add,
              onSearchArea: () {},
              hasSearchArea: false,
              imageDraft: draft,
            ),
          ),
        ),
      );
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 30)),
      );
      await t.pumpAndSettle();
      await t.tap(find.byTooltip('发送需求'));
      await t.pump();
      expect(analysis, ['']);
      expect(sent, isEmpty);
      expect(workspace.task, isNull);
      expect(draft.localBytes, isNotNull);
      await t.enterText(find.byType(TextField), '请读图片上的字');
      await t.tap(find.byTooltip('发送需求'));
      await t.pump();
      expect(analysis, ['', '请读图片上的字']);
      expect(sent, isEmpty);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        '请读图片上的字',
      );
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets('video-only material entry is reachable and preserves text', (
    t,
  ) async {
    final workspace = AgentWorkspaceController();
    addTearDown(workspace.dispose);
    var opened = 0;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: (_) {},
            onSearchArea: () {},
            hasSearchArea: false,
            onVideoMaterial: () => opened++,
          ),
        ),
      ),
    );
    await t.enterText(find.byType(TextField), '未发送的文字');
    await t.tap(find.byTooltip('添加材料'));
    await t.pumpAndSettle();
    await t.tap(find.text('视频'));
    await t.pumpAndSettle();
    expect(opened, 1);
    expect(
      t.widget<TextField>(find.byType(TextField)).controller!.text,
      '未发送的文字',
    );
    await t.pumpWidget(const SizedBox());
  });
  testWidgets(
    'small screen large text missing provider keeps draft and explains boundary',
    (t) async {
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final bytes = (await t.runAsync(imageFixtureBytes))!;
      final f = VisualFixture(bytes, text: '保留图片问题')..unavailable = true;
      addTearDown(f.dispose);
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: TextScaler.linear(2)),
            child: child!,
          ),
          home: AgentVisualPage(controller: f.controller),
        ),
      );
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 30)),
      );
      await t.pumpAndSettle();
      final button = find.text('准备分析预览');
      await t.ensureVisible(button);
      await t.tap(button);
      await t.runAsync(() async {
        for (var i = 0; i < 50 && f.controller.busy; i++) {
          await Future<void>.delayed(const Duration(milliseconds: 10));
        }
      });
      await t.pumpAndSettle();
      expect(f.controller.report, isNull);
      expect(f.controller.localPixels, isNotNull);
      expect(find.text('保留图片问题'), findsOneWidget);
      expect(find.textContaining('尚未配置'), findsOneWidget);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
  );
}
