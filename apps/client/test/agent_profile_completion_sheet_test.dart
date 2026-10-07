import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/content/agent_profile_completion_sheet.dart';
import 'package:birdtie_client/src/content/agent_profile_completion_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_profile_completion_api_test.dart';
import 'agent_profile_completion_pending_store_test.dart'
    show completionPending;

Widget completionSheetHarness(
  SeedTestAuth auth,
  MockClient client,
  AgentProfileCompletionPendingStore store, {
  double scale = 1,
  double ime = 0,
}) => MaterialApp(
  theme: ThemeData(
    fontFamily:
        Platform.environment['BIRDTIE_PROFILE_COMPLETION_CJK_FONT'] == null
        ? null
        : 'BirdtieCompletionQA',
  ),
  builder: (context, child) => MediaQuery(
    data: MediaQuery.of(context).copyWith(
      textScaler: TextScaler.linear(scale),
      viewInsets: EdgeInsets.only(bottom: ime),
    ),
    child: child!,
  ),
  home: RepaintBoundary(
    key: const ValueKey('completion-render'),
    child: AgentProfileCompletionSheet(
      auth: auth,
      client: client,
      apiBaseUrl: 'http://local',
      pendingStore: store,
    ),
  ),
);

Future<void> completionCapture(WidgetTester t, String name) async {
  final root =
      Platform.environment['BIRDTIE_PROFILE_COMPLETION_SCREENSHOT_DIR'];
  if (root == null || root.isEmpty) return;
  final boundary = t.renderObject<RenderRepaintBoundary>(
    find.byKey(const ValueKey('completion-render')),
  );
  await t.runAsync(() async {
    final image = await boundary.toImage(pixelRatio: 2);
    try {
      final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
      if (bytes == null) throw StateError('render bytes unavailable');
      await Directory(root).create(recursive: true);
      await File('$root/$name.png').writeAsBytes(bytes.buffer.asUint8List());
    } finally {
      image.dispose();
    }
  });
}

Future<void> completionSheetTap(WidgetTester t, String text) async {
  final f = find.text(text).last;
  await t.ensureVisible(f);
  await t.tap(f);
  await t.pumpAndSettle();
}

Future<void> completionLoadCJKFont(WidgetTester t) async {
  final path = Platform.environment['BIRDTIE_PROFILE_COMPLETION_CJK_FONT'];
  if (path == null || path.isEmpty) return;
  await t.runAsync(() async {
    final bytes = await File(path).readAsBytes();
    final loader = FontLoader('BirdtieCompletionQA')
      ..addFont(Future.value(ByteData.view(bytes.buffer)));
    await loader.load();
  });
}

void main() {
  testWidgets('320宽 字号3 IME260 中文具体review/48dp/semantics/确认', (t) async {
    await completionLoadCJKFont(t);
    t.view.physicalSize = const Size(320, 820);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final auth = SeedTestAuth()..owner = completionOwner;
    addTearDown(auth.dispose);
    final store = MemoryAgentProfileCompletionPendingStore();
    var id = '', posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        if (r.url.path.endsWith('/accept')) {
          return completionResponse(completionReceipt(id: id));
        }
        id = jsonDecode(r.body)['previewId'];
        return completionResponse(completionPreview(id: id));
      }
      return completionResponse(completionSuggestions());
    });
    final semantics = t.ensureSemantics();
    await t.pumpWidget(
      completionSheetHarness(auth, client, store, scale: 3, ime: 260),
    );
    await t.pumpAndSettle();
    await completionSheetTap(t, '检查：我偏好徒步活动');
    expect(find.text('原值：未填写'), findsOneWidget);
    expect(find.text('保存后：我偏好徒步活动'), findsOneWidget);
    expect(find.textContaining('不会自动删除'), findsWidgets);
    await t.ensureVisible(find.text('保存后：我偏好徒步活动'));
    await t.pumpAndSettle();
    await completionCapture(t, 'review-320-font3-ime260');
    final button = find.widgetWithText(FilledButton, '确认补齐这一项');
    await t.ensureVisible(button);
    expect(t.getSize(button).height, greaterThanOrEqualTo(48));
    expect(t.getSemantics(button).label, contains('确认补齐这一项'));
    await t.tap(button);
    await t.pumpAndSettle();
    expect(posts, 2);
    expect(find.textContaining('已核实保存'), findsOneWidget);
    await t.ensureVisible(find.textContaining('已核实保存'));
    await t.pumpAndSettle();
    await completionCapture(t, 'saved-320-font3-ime260');
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    semantics.dispose();
  });
  testWidgets('迟到 Accept + 身份 ABA 不显示成功、不清原pending', (t) async {
    final auth = SeedTestAuth()..owner = completionOwner;
    addTearDown(auth.dispose);
    final store = MemoryAgentProfileCompletionPendingStore(),
        wait = Completer<http.Response>();
    var id = '', posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        if (r.url.path.endsWith('/accept')) return wait.future;
        id = jsonDecode(r.body)['previewId'];
        return completionResponse(completionPreview(id: id));
      }
      return completionResponse(completionSuggestions());
    });
    await t.pumpWidget(completionSheetHarness(auth, client, store));
    await t.pumpAndSettle();
    await completionSheetTap(t, '检查：我偏好徒步活动');
    await t.ensureVisible(find.text('确认补齐这一项'));
    await t.tap(find.text('确认补齐这一项'));
    await t.pump();
    auth.changeIdentity('Bearer peer', nextOwner: completionMemory);
    auth.changeIdentity('Bearer owner', nextOwner: completionOwner);
    wait.complete(completionResponse(completionReceipt(id: id)));
    await t.pumpAndSettle();
    expect(posts, 2);
    expect(find.textContaining('已核实保存'), findsNothing);
    expect(await store.read('http://local', completionOwner), isNotNull);
    expect(find.textContaining('身份或连接已变化'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('关闭重开未知只GET，无旧正文/确认按钮 新Session元数据', (t) async {
    final auth = SeedTestAuth()..owner = completionOwner;
    addTearDown(auth.dispose);
    final store = MemoryAgentProfileCompletionPendingStore();
    await store.write(
      'http://local',
      completionOwner,
      completionPending(phase: 'ACCEPT'),
    );
    var posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') posts++;
      return completionResponse(completionReceipt(state: 'PENDING'));
    });
    await t.pumpWidget(completionSheetHarness(auth, client, store));
    await t.pumpAndSettle();
    expect(find.text('确认补齐这一项'), findsNothing);
    expect(find.text('保存后：我偏好徒步活动'), findsNothing);
    expect(find.textContaining('旧审阅内容无法恢复'), findsOneWidget);
    await completionSheetTap(t, '核实原操作');
    expect(posts, 0);
    await t.pumpWidget(const SizedBox());
  });
}
