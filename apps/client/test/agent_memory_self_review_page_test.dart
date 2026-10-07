import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'dart:async';
import 'package:http/http.dart' as http;
import 'package:birdtie_client/src/workspace/agent_memory_correction_page.dart';
import 'package:birdtie_client/src/workspace/agent_memory_self_review.dart'
    show selfReviewNotice;
import 'agent_memory_self_review_api_test.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_memory_correction_api_test.dart';
import 'agent_memory_correction_page_test.dart'
    show correctionPageHarness, correctionSettle, correctionTap;

void main() {
  testWidgets('同体审阅原入口：当前明确记忆可实际点击核对活动偏好', (t) async {
    final auth = SeedTestAuth()..owner = correctionOwner;
    addTearDown(auth.dispose);
    addTearDown(() async => t.pumpWidget(const SizedBox()));
    final at = DateTime.now().toUtc();
    var readPosts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST' && r.url.path.endsWith('/self-review')) {
        readPosts++;
      }
      return correctionResponse({
        'data': r.url.path.endsWith('/agent-memories')
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
    expect(find.text('我偏好徒步活动'), findsOneWidget);
    await correctionTap(t, '核对活动偏好与这条记忆');
    expect(readPosts, 1);
  });
  for (final dark in [false, true]) {
    testWidgets('同体审阅页面：原wire双方声明待确认，320大字${dark ? '深' : '浅'}色与48dp保留更正', (
      t,
    ) async {
      t.view.physicalSize = const Size(320, 720);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final auth = SeedTestAuth()..owner = reviewOwner;
      addTearDown(auth.dispose);
      addTearDown(() async => t.pumpWidget(const SizedBox()));
      final client = ReviewSendClient(
        (r) async => r.url.path.endsWith('/self-review')
            ? reviewWireResponse()
            : reviewResponse(
                r.url.path.endsWith('/agent-memories')
                    ? [reviewMemoryRow()]
                    : [],
              ),
      );
      await t.pumpWidget(reviewPageHarness(auth, client, dark: dark, scale: 2));
      await correctionSettle(t);
      final entry = find.widgetWithText(TextButton, '核对活动偏好与这条记忆');
      await t.ensureVisible(entry);
      await correctionSettle(t);
      expect(t.getSize(entry).height, greaterThanOrEqualTo(48));
      await correctionTap(t, '核对活动偏好与这条记忆');
      expect(find.text('资料中的活动偏好：羽毛球'), findsOneWidget);
      expect(find.text('这条记忆的声明：我不偏好羽毛球活动'), findsOneWidget);
      expect(find.text('声明不一致：羽毛球 · 待本人确认'), findsOneWidget);
      expect(find.text('本次未提供置信描述；不会补成分值或概率。'), findsOneWidget);
      expect(find.textContaining('拍摄时间未采集。更新时间'), findsOneWidget);
      final note = find.text(selfReviewNotice);
      await t.ensureVisible(note);
      await correctionSettle(t);
      expect(note.hitTestable(), findsOneWidget);
      await correctionTap(t, '修改这条记忆');
      expect(find.byType(TextField), findsOneWidget);
      expect(find.widgetWithText(FilledButton, '检查具体纠正版本'), findsOneWidget);
      expect(client.requests.where((r) => r.method == 'POST').length, 1);
      expect(t.takeException(), isNull);
    });
  }
  for (final change in [
    'authABA',
    'client',
    'base',
    'clock',
    'workspace',
    'close',
  ]) {
    testWidgets('同体审阅页面：$change实际source退役后迟到wire不显示或写入', (t) async {
      final auth = SeedTestAuth()..owner = reviewOwner;
      addTearDown(auth.dispose);
      addTearDown(() async => t.pumpWidget(const SizedBox()));
      final gate = Completer<http.Response>();
      final client = ReviewSendClient(
        (r) => r.url.path.endsWith('/self-review')
            ? gate.future
            : Future.value(
                reviewResponse(
                  r.url.path.endsWith('/agent-memories')
                      ? [reviewMemoryRow()]
                      : [],
                ),
              ),
      );
      final workspace = ValueNotifier<String?>(null);
      addTearDown(workspace.dispose);
      String? binding() => workspace.value;
      await t.pumpWidget(
        reviewPageHarness(auth, client, changes: workspace, workspace: binding),
      );
      await correctionSettle(t);
      final button = find.widgetWithText(TextButton, '核对活动偏好与这条记忆');
      await t.ensureVisible(button);
      await t.tap(button);
      await t.pump();
      expect(client.requests.where((r) => r.method == 'POST'), hasLength(1));
      switch (change) {
        case 'authABA':
          auth.changeIdentity(
            'Bearer peer',
            nextOwner: '40000000-0000-4000-8000-000000000099',
          );
          auth.changeIdentity('Bearer owner', nextOwner: reviewOwner);
          break;
        case 'client':
          await t.pumpWidget(
            reviewPageHarness(
              auth,
              ReviewSendClient((r) async => reviewWireResponse()),
              changes: workspace,
              workspace: binding,
            ),
          );
          break;
        case 'base':
          await t.pumpWidget(
            reviewPageHarness(
              auth,
              client,
              base: 'http://other',
              changes: workspace,
              workspace: binding,
            ),
          );
          break;
        case 'clock':
          await t.pumpWidget(
            reviewPageHarness(
              auth,
              client,
              now: () => reviewClock(),
              changes: workspace,
              workspace: binding,
            ),
          );
          break;
        case 'workspace':
          workspace.value = 'org';
          workspace.value = null;
          break;
        case 'close':
          await t.pumpWidget(const SizedBox());
          break;
      }
      gate.complete(reviewWireResponse());
      await correctionSettle(t);
      expect(find.text('一起核对这两份声明'), findsNothing);
      expect(find.textContaining('声明不一致：'), findsNothing);
      expect(client.requests.where((r) => r.method == 'POST'), hasLength(1));
      expect(t.takeException(), isNull);
    });
  }
  testWidgets('同体审阅页面：原snapshot到期只关闭说明，不动记忆或更正入口', (t) async {
    final auth = SeedTestAuth()..owner = reviewOwner;
    addTearDown(auth.dispose);
    addTearDown(() async => t.pumpWidget(const SizedBox()));
    var now = reviewClock(), ticks = Duration.zero;
    final client = ReviewSendClient(
      (r) async => r.url.path.endsWith('/self-review')
          ? reviewWireResponse()
          : reviewResponse(
              r.url.path.endsWith('/agent-memories') ? [reviewMemoryRow()] : [],
            ),
    );
    await t.pumpWidget(
      reviewPageHarness(auth, client, now: () => now, elapsed: () => ticks),
    );
    await correctionSettle(t);
    await correctionTap(t, '核对活动偏好与这条记忆');
    expect(find.text('一起核对这两份声明'), findsOneWidget);
    ticks = const Duration(minutes: 2);
    now = now.subtract(const Duration(hours: 1));
    await t.pump(const Duration(seconds: 1));
    expect(find.text('一起核对这两份声明'), findsNothing);
    expect(find.textContaining('同体来源审阅已到期'), findsOneWidget);
    await correctionTap(t, '修改这条记忆');
    expect(find.byType(TextField), findsOneWidget);
    expect(client.requests.where((r) => r.method == 'POST'), hasLength(1));
  });
}

Widget reviewPageHarness(
  SeedTestAuth auth,
  http.Client client, {
  double scale = 1,
  bool dark = false,
  String base = 'http://local',
  DateTime Function()? now,
  Duration Function()? elapsed,
  Listenable? changes,
  String? Function()? workspace,
}) => MaterialApp(
  theme: ThemeData(brightness: dark ? Brightness.dark : Brightness.light),
  builder: (context, child) => MediaQuery(
    data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(scale)),
    child: child!,
  ),
  home: AgentMemoryCorrectionPage(
    key: const ValueKey('same-page'),
    auth: auth,
    client: client,
    apiBaseUrl: base,
    now: now ?? reviewClock,
    elapsed: elapsed,
    workspaceChanges: changes,
    organizationWorkspaceID: workspace,
    pendingStore: sharedReviewStore,
  ),
);
final sharedReviewStore = MemoryAgentMemoryCorrectionPendingStore();
