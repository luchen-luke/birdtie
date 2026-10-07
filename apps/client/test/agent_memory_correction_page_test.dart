import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/workspace/agent_memory_correction_page.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_memory_candidate_api_test.dart'
    show candidateWire, candidateTarget;
import 'agent_memory_correction_api_test.dart';
import 'agent_memory_correction_pending_store_test.dart';

Widget correctionPageHarness(
  SeedTestAuth auth,
  http.Client client,
  AgentMemoryCorrectionPendingStore s, {
  double scale = 1,
  double ime = 0,
  bool dark = false,
  ValueNotifier<String?>? workspace,
  String base = 'http://local',
}) => MaterialApp(
  theme: ThemeData(
    brightness: dark ? Brightness.dark : Brightness.light,
    fontFamily: Platform.environment['BIRDTIE_CORRECTION_CJK_FONT'] == null
        ? null
        : 'BirdtieCorrectionQA',
  ),
  builder: (context, child) => MediaQuery(
    data: MediaQuery.of(context).copyWith(
      textScaler: TextScaler.linear(scale),
      viewInsets: EdgeInsets.only(bottom: ime),
    ),
    child: child!,
  ),
  home: RepaintBoundary(
    key: const ValueKey('correction-render'),
    child: AgentMemoryCorrectionPage(
      key: const ValueKey('same-page'),
      auth: auth,
      client: client,
      apiBaseUrl: base,
      pendingStore: s,
      workspaceChanges: workspace,
      organizationWorkspaceID: workspace == null ? null : () => workspace.value,
    ),
  ),
);
Future<void> correctionCapture(WidgetTester t, String name) async {
  final root = Platform.environment['BIRDTIE_CORRECTION_SCREENSHOT_DIR'];
  if (root == null || root.isEmpty) return;
  final b = t.renderObject<RenderRepaintBoundary>(
    find.byKey(const ValueKey('correction-render')),
  );
  await t.runAsync(() async {
    final image = await b.toImage(pixelRatio: 2);
    try {
      final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
      await Directory(root).create(recursive: true);
      await File('$root/$name.png').writeAsBytes(bytes!.buffer.asUint8List());
    } finally {
      image.dispose();
    }
  });
}

Future<void> correctionFont(WidgetTester t) async {
  final icons = Platform.environment['BIRDTIE_CORRECTION_MATERIAL_FONT'];
  if (icons != null && icons.isNotEmpty) {
    await t.runAsync(() async {
      final bytes = await File(icons).readAsBytes();
      await (FontLoader(
        'MaterialIcons',
      )..addFont(Future.value(ByteData.view(bytes.buffer)))).load();
    });
  }
  final p = Platform.environment['BIRDTIE_CORRECTION_CJK_FONT'];
  if (p == null || p.isEmpty) return;
  await t.runAsync(() async {
    final bytes = await File(p).readAsBytes();
    await (FontLoader(
      'BirdtieCorrectionQA',
    )..addFont(Future.value(ByteData.view(bytes.buffer)))).load();
  });
}

Future<void> correctionSettle(WidgetTester t) async {
  await t.pump();
  await t.runAsync(
    () => Future<void>.delayed(const Duration(milliseconds: 30)),
  );
  await t.pump();
  await t.runAsync(
    () => Future<void>.delayed(const Duration(milliseconds: 30)),
  );
  await t.pumpAndSettle();
}

Future<void> correctionTap(WidgetTester t, String text) async {
  final all = find.text(text);
  if (all.evaluate().isEmpty) {
    t.state<ScrollableState>(find.byType(Scrollable).first).position.jumpTo(0);
    await t.pump();
    await t.scrollUntilVisible(
      all,
      160,
      maxScrolls: 80,
      scrollable: find.byType(Scrollable).first,
    );
  }
  final f = all.last;
  await t.ensureVisible(f);
  await correctionSettle(t);
  expect(
    t.getCenter(f).dy,
    lessThan(t.view.physicalSize.height / t.view.devicePixelRatio),
  );
  await t.tap(f);
  await correctionSettle(t);
}

void main() {
  testWidgets('future server短lease迟到不首次展示Memoryreview正文', (t) async {
    final auth = SeedTestAuth()..owner = correctionOwner;
    addTearDown(auth.dispose);
    final at = DateTime.now().toUtc(),
        store = MemoryAgentMemoryCorrectionPendingStore(),
        response = Completer<http.Response>();
    Map<String, dynamic>? input;
    var posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        input = Map<String, dynamic>.from(jsonDecode(r.body));
        return response.future;
      }
      return correctionResponse({
        'data': r.url.path.endsWith('agent-memories')
            ? [correctionMemoryRaw(at: at)]
            : [],
      });
    });
    await t.pumpWidget(correctionPageHarness(auth, client, store));
    await correctionSettle(t);
    await correctionTap(t, '拒绝保留');
    final button = find.widgetWithText(FilledButton, '检查具体纠正版本');
    await t.ensureVisible(button);
    await correctionSettle(t);
    await t.tap(button);
    await t.pump();
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 100)),
    );
    final server = at.add(const Duration(hours: 1));
    response.complete(
      correctionResponse({
        ...correctionPreviewRaw(
          input: input,
          at: server,
          memory: correctionMemoryRaw(at: at),
        ),
        'expiresAt': server
            .add(const Duration(milliseconds: 30))
            .toIso8601String(),
      }),
    );
    await correctionSettle(t);
    expect(posts, 1);
    expect(find.text('检查这次具体纠正'), findsNothing);
    expect(find.text('确认此版本的纠正'), findsNothing);
    expect(find.text('核实原纠正结果'), findsOneWidget);
    await correctionCapture(t, 'delayed-review-metadata-only');
    await t.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('候选REJECT明确REJECTED及每项affected kind/id/version无伪正文', (t) async {
    await correctionFont(t);
    final auth = SeedTestAuth()..owner = correctionOwner;
    addTearDown(auth.dispose);
    final at = DateTime.now().toUtc();
    var posts = 0;
    Map<String, dynamic>? input;
    final candidate = {
      ...candidateWire(),
      'owner': {'type': 'PERSON', 'id': correctionOwner},
      'agentId': correctionAgent,
      'category': 'hiking',
    };
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        if (r.url.path.endsWith('previews')) {
          input = Map<String, dynamic>.from(jsonDecode(r.body));
          return correctionResponse({
            ...correctionPreviewRaw(input: input, at: at),
            'memories': [],
            'affected': [
              {'kind': 'CANDIDATE', 'id': candidateTarget, 'version': 1},
            ],
          });
        }
        final receipt =
            correctionReceiptRaw(input: input, at: at, current: false)
              ..remove('resultMemoryId')
              ..remove('resultMemoryVersion');
        return correctionResponse(receipt);
      }
      return correctionResponse({
        'data': r.url.path.endsWith('agent-memory-candidates')
            ? [candidate]
            : [],
      });
    });
    await t.pumpWidget(
      correctionPageHarness(
        auth,
        client,
        MemoryAgentMemoryCorrectionPendingStore(),
      ),
    );
    await correctionSettle(t);
    await correctionTap(t, '拒绝这条候选');
    await correctionTap(t, '检查具体纠正版本');
    expect(find.textContaining('使用原 REJECTED'), findsOneWidget);
    expect(find.textContaining('使用原 DELETED'), findsNothing);
    await correctionTap(t, '检查全部受影响对象（1 项）');
    expect(find.textContaining('候选 · 版本 1'), findsOneWidget);
    expect(find.textContaining(candidateTarget), findsOneWidget);
    await correctionCapture(t, 'candidate-reject-affected-review');
    await correctionTap(t, '确认此版本的纠正');
    expect(posts, 2);
    await t.pumpWidget(const SizedBox());
    client.close();
  });

  for (final dark in [false, true]) {
    for (final scale in [1.0, 3.0]) {
      testWidgets(
        '中文widget ${dark ? 'dark' : 'light'} 320 字号$scale IME260具体版本与48dp',
        (t) async {
          await correctionFont(t);
          t.view.physicalSize = const Size(320, 820);
          t.view.devicePixelRatio = 1;
          addTearDown(t.view.resetPhysicalSize);
          addTearDown(t.view.resetDevicePixelRatio);
          final auth = SeedTestAuth()..owner = correctionOwner;
          addTearDown(auth.dispose);
          final at = DateTime.now().toUtc(),
              store = MemoryAgentMemoryCorrectionPendingStore();
          var posts = 0;
          Map<String, dynamic>? input;
          final client = MockClient((r) async {
            if (r.method == 'POST') {
              posts++;
              if (r.url.path.endsWith('previews')) {
                input = Map<String, dynamic>.from(jsonDecode(r.body));
                return correctionResponse(
                  correctionPreviewRaw(input: input, at: at),
                );
              }
              return correctionResponse(
                correctionReceiptRaw(input: input, at: at),
              );
            }
            return correctionResponse({
              'data': r.url.path.endsWith('agent-memories')
                  ? [correctionMemoryRaw(at: at)]
                  : [],
            });
          });
          await t.pumpWidget(
            correctionPageHarness(
              auth,
              client,
              store,
              scale: scale,
              ime: 260,
              dark: dark,
            ),
          );
          await correctionSettle(t);
          expect(t.takeException(), isNull);
          await correctionCapture(
            t,
            'list-${dark ? 'dark' : 'light'}-font$scale',
          );
          await correctionTap(t, '修改这条记忆');
          await t.scrollUntilVisible(
            find.byType(TextField),
            160,
            maxScrolls: 80,
            scrollable: find.byType(Scrollable).first,
          );
          await correctionSettle(t);
          await t.enterText(find.byType(TextField), '本人明确修订');
          await correctionTap(t, '检查具体纠正版本');
          expect(posts, 1);
          expect(find.text('检查这次具体纠正'), findsOneWidget);
          expect(find.textContaining('原记忆有效至：'), findsOneWidget);
          expect(find.textContaining('本次确认截止：'), findsOneWidget);
          expect(find.text('我偏好徒步活动'), findsWidgets);
          expect(find.text('本人明确修订'), findsOneWidget);
          await correctionCapture(
            t,
            'review-${dark ? 'dark' : 'light'}-font$scale',
          );
          final semantics = t.ensureSemantics();
          final button = find.widgetWithText(FilledButton, '确认此版本的纠正');
          await t.ensureVisible(button);
          expect(t.getSize(button).height, greaterThanOrEqualTo(48));
          expect(
            t.getSemantics(button).getSemanticsData().label,
            contains('确认此版本'),
          );
          await correctionTap(t, '确认此版本的纠正');
          expect(posts, 2);
          expect(
            t
                .state<ScrollableState>(find.byType(Scrollable).first)
                .position
                .pixels,
            0,
          );
          expect(find.textContaining('原纠正已完成'), findsOneWidget);
          expect(find.text('检查具体纠正版本'), findsNothing);
          expect(t.takeException(), isNull);
          await correctionCapture(
            t,
            'result-${dark ? 'dark' : 'light'}-font$scale',
          );
          await t.pumpWidget(const SizedBox());
          semantics.dispose();
          client.close();
        },
      );
    }
  }
  testWidgets('NEGATE类别默认空 必须具体本人选择', (t) async {
    final auth = SeedTestAuth()..owner = correctionOwner;
    addTearDown(auth.dispose);
    var posts = 0;
    final at = DateTime.now().toUtc();
    final client = MockClient((r) async {
      if (r.method == 'POST') posts++;
      return correctionResponse({
        'data': r.url.path.endsWith('agent-memories')
            ? [correctionMemoryRaw(at: at)]
            : [],
      });
    });
    await t.pumpWidget(
      correctionPageHarness(
        auth,
        client,
        MemoryAgentMemoryCorrectionPendingStore(),
      ),
    );
    await correctionSettle(t);
    await correctionTap(t, '纠正活动偏好');
    final b = t.widget<FilledButton>(
      find.widgetWithText(FilledButton, '检查具体纠正版本'),
    );
    expect(b.onPressed, isNull);
    expect(find.textContaining('持续被抑制'), findsWidgets);
    expect(posts, 0);
    await t.pumpWidget(const SizedBox());
    client.close();
  });
  testWidgets('UNKNOWN关闭重开新session 仅GET原ID无旧正文批准', (t) async {
    final auth = SeedTestAuth()..owner = correctionOwner;
    addTearDown(auth.dispose);
    final store = MemoryAgentMemoryCorrectionPendingStore(),
        calls = <http.Request>[];
    await store.write(
      'http://local',
      correctionOwner,
      correctionPending(phase: 'CONFIRM'),
    );
    final client = MockClient((r) async {
      calls.add(r);
      return correctionResponse(correctionReceiptRaw(state: 'PENDING'));
    });
    await t.pumpWidget(correctionPageHarness(auth, client, store));
    await correctionSettle(t);
    expect(find.text('确认此版本的纠正'), findsNothing);
    expect(find.textContaining('旧审阅内容无法恢复'), findsOneWidget);
    await correctionCapture(t, 'unknown-metadata-only');
    await t.pumpWidget(const SizedBox());
    auth.token = 'Bearer next';
    await t.pumpWidget(correctionPageHarness(auth, client, store));
    await correctionSettle(t);
    expect(
      calls.every(
        (r) =>
            r.method == 'GET' &&
            r.url.path.endsWith(correctionID) &&
            r.bodyBytes.isEmpty,
      ),
      true,
    );
    expect(find.text('确认此版本的纠正'), findsNothing);
    await t.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('真实预览deadline后清正文 只留原GET引用不造CLOSED', (t) async {
    final auth = SeedTestAuth()..owner = correctionOwner;
    addTearDown(auth.dispose);
    final at = DateTime.now().toUtc(),
        store = MemoryAgentMemoryCorrectionPendingStore();
    var posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        return correctionResponse({
          ...correctionPreviewRaw(
            input: Map<String, dynamic>.from(jsonDecode(r.body)),
            at: at,
          ),
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(milliseconds: 300))
              .toIso8601String(),
        });
      }
      return correctionResponse({
        'data': r.url.path.endsWith('agent-memories')
            ? [correctionMemoryRaw(at: at)]
            : [],
      });
    });
    await t.pumpWidget(correctionPageHarness(auth, client, store));
    await correctionSettle(t);
    await correctionTap(t, '拒绝保留');
    await correctionTap(t, '检查具体纠正版本');
    expect(find.text('检查这次具体纠正'), findsOneWidget);
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 450)),
    );
    await t.pump(const Duration(seconds: 1));
    await correctionSettle(t);
    expect(find.text('检查这次具体纠正'), findsNothing);
    expect(find.text('确认此版本的纠正'), findsNothing);
    expect(find.text('核实原纠正结果'), findsOneWidget);
    expect(posts, 1);
    expect(await store.read('http://local', correctionOwner), isNotNull);
    await correctionCapture(t, 'expired-review-metadata-only');
    await t.pumpWidget(const SizedBox());
    client.close();
  });
  for (final mode in [
    'base',
    'client',
    'store',
    'getter',
    'workspace-during-build',
    'auth-during-build',
  ]) {
    for (final stage in ['draft', 'preview']) {
      testWidgets('隔离same-key $mode $stage A-B-A同步退休0Confirm', (t) async {
        final auth = SeedTestAuth()..owner = correctionOwner;
        addTearDown(auth.dispose);
        final signal = ValueNotifier<int>(0), ws = ValueNotifier<String?>(null);
        addTearDown(signal.dispose);
        addTearDown(ws.dispose);
        final original = MemoryAgentMemoryCorrectionPendingStore(),
            other = MemoryAgentMemoryCorrectionPendingStore();
        var calls = 0, confirms = 0;
        final at = DateTime.now().toUtc();
        MockClient make() => MockClient((r) async {
          calls++;
          if (r.method == 'POST') {
            if (r.url.path.endsWith('confirm')) confirms++;
            return correctionResponse(
              correctionPreviewRaw(
                input: Map<String, dynamic>.from(jsonDecode(r.body)),
                at: at,
              ),
            );
          }
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw(at: at)]
                : [],
          });
        });
        final first = make(), second = make();
        String? stableGetter() => ws.value;
        String? nextGetter() => ws.value;
        await t.pumpWidget(
          MaterialApp(
            home: ValueListenableBuilder<int>(
              valueListenable: signal,
              builder: (context, v, _) {
                if (mode == 'workspace-during-build') {
                  ws.value = v == 1 ? correctionAgent : null;
                }
                if (mode == 'auth-during-build' && v > 0) {
                  auth.changeIdentity(
                    v == 1 ? 'Bearer changed' : 'Bearer owner',
                    nextOwner: correctionOwner,
                  );
                }
                return AgentMemoryCorrectionPage(
                  key: const ValueKey('same-page'),
                  auth: auth,
                  client: mode == 'client' && v == 1 ? second : first,
                  apiBaseUrl: mode == 'base' && v == 1
                      ? 'http://other'
                      : 'http://local',
                  pendingStore: mode == 'store' && v == 1 ? other : original,
                  workspaceChanges: ws,
                  organizationWorkspaceID: mode == 'getter' && v == 1
                      ? nextGetter
                      : stableGetter,
                );
              },
            ),
          ),
        );
        await correctionSettle(t);
        await correctionTap(t, '修改这条记忆');
        await t.enterText(find.byType(TextField), '私密具体版本');
        if (stage == 'preview') {
          await correctionTap(t, '检查具体纠正版本');
          expect(find.text('确认此版本的纠正'), findsOneWidget);
        }
        final count = calls;
        signal.value = 1;
        await correctionSettle(t);
        expect(t.takeException(), isNull);
        signal.value = 2;
        await correctionSettle(t);
        expect(t.takeException(), isNull);
        expect(calls, count);
        expect(confirms, 0);
        expect(find.textContaining('身份或连接已变化'), findsOneWidget);
        expect(find.text('私密具体版本'), findsNothing);
        expect(find.text('确认此版本的纠正'), findsNothing);
        expect(find.byType(TextField), findsNothing);
        await t.pumpWidget(const SizedBox());
        first.close();
        second.close();
      });
    }
  }
}
