import 'package:birdtie_client/src/workspace/agent_candidate_pending_store.dart';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_section.dart';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_pending_store.dart';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter/semantics.dart';
import 'agent_candidate_pending_store_test.dart' show pendingAcceptance;
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/testing.dart';
import 'package:http/http.dart' as http;
import 'agent_memory_candidate_api_test.dart';
import 'agent_memory_candidate_page_test.dart';
import 'agent_multi_candidate_api_test.dart';
import 'agent_multi_candidate_controller_test.dart'
    show multiController, approveCombination;

Future<void> multiTap(WidgetTester t, Finder target) async {
  await t.ensureVisible(target);
  await t.pumpAndSettle();
  await t.tap(target);
  await t.pumpAndSettle();
}

Widget multiHarness(
  Widget child, {
  double scale = 1,
  Brightness brightness = Brightness.light,
}) => MaterialApp(
  theme: ThemeData(brightness: brightness),
  home: MediaQuery(
    data: MediaQueryData(textScaler: TextScaler.linear(scale)),
    child: Scaffold(
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(20),
          child: child,
        ),
      ),
    ),
  ),
);

late AgentMultiCandidatePendingStore _testMultiStore;
void main() {
  testWidgets(
    'same-page OPEN at 320 font3 IME260 keeps journal and only original human retry',
    (t) async {
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final auth = await candidateAuth(),
          f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore();
      var approvals = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/approve') && ++approvals == 1) {
          throw http.ClientException('synthetic dispatch unknown');
        }
        return f.handle(r);
      });
      final semantics = t.ensureSemantics();
      await t.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(
              size: Size(320, 640),
              viewInsets: EdgeInsets.only(bottom: 260),
              textScaler: TextScaler.linear(3),
            ),
            child: Scaffold(
              body: SingleChildScrollView(
                child: AgentMultiCandidateSection(
                  auth: auth,
                  client: client,
                  apiBaseUrl: 'http://fixture',
                  pendingStore: store,
                ),
              ),
            ),
          ),
        ),
      );
      await multiTap(t, find.text('选择来源和已有任务'));
      await multiTap(t, find.byKey(const ValueKey('multi-task-$multiTask')));
      await multiTap(
        t,
        find.byKey(const ValueKey('multi-source-$candidateMoment')),
      );
      await multiTap(
        t,
        find.byKey(const ValueKey('multi-review-$candidateMoment')),
      );
      await multiTap(t, find.text('允许这次本地分析'));
      expect(store.values.length, 1);
      await multiTap(t, find.text('核实原授权或提交结果'));
      expect(store.values.length, 1);
      expect(approvals, 1);
      final source = t.widget<CheckboxListTile>(
        find.byKey(const ValueKey('multi-source-$candidateSaved')),
      );
      expect(source.onChanged, isNull);
      final retry = find.widgetWithText(FilledButton, '允许这次本地分析');
      await t.ensureVisible(retry);
      await t.pumpAndSettle();
      expect(t.getSize(retry).height, greaterThanOrEqualTo(48));
      expect(
        t.getSemantics(retry).getSemanticsData().hasAction(SemanticsAction.tap),
        true,
      );
      await multiTap(t, retry);
      expect(approvals, 2);
      expect(store.values, isEmpty);
      expect(
        f.requests
            .where(
              (r) =>
                  r['method'] == 'POST' &&
                  (r['path'] as String).endsWith('/approve'),
            )
            .length,
        1,
      );
      expect(
        f.requests.any(
          (r) => (r['path'] as String).endsWith(
            '/previews/${sourcePreviewID(1)}/receipt',
          ),
        ),
        true,
      );
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      semantics.dispose();
      auth.dispose();
      client.close();
    },
  );

  setUp(() => _testMultiStore = MemoryAgentMultiCandidatePendingStore());
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  testWidgets(
    'reopened UNKNOWN at 320 font3 IME260 keeps new writes disabled and exposes only original GET',
    (t) async {
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore();
      final original = multiController(f, store: store);
      await approveCombination(original);
      f.loseStage = true;
      await original.stage();
      original.dispose();
      f.staged = false;
      final before = f.requests.length, auth = await candidateAuth();
      final client = MockClient(f.handle);
      final semantics = t.ensureSemantics();
      await t.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(
              size: Size(320, 640),
              viewInsets: EdgeInsets.only(bottom: 260),
              textScaler: TextScaler.linear(3),
            ),
            child: Scaffold(
              resizeToAvoidBottomInset: true,
              body: SingleChildScrollView(
                child: AgentMultiCandidateSection(
                  auth: auth,
                  client: client,
                  apiBaseUrl: 'http://fixture',
                  pendingStore: store,
                ),
              ),
            ),
          ),
        ),
      );
      await multiTap(t, find.text('选择来源和已有任务'));
      expect(find.textContaining('发现原操作的本机核实记录'), findsOneWidget);
      final check = find.widgetWithText(FilledButton, '核实原授权或提交结果');
      await t.ensureVisible(check);
      await t.pumpAndSettle();
      expect(t.getSize(check).height, greaterThanOrEqualTo(48));
      expect(
        t.getSemantics(check).getSemanticsData().hasAction(SemanticsAction.tap),
        true,
      );
      await multiTap(t, find.text('核实原授权或提交结果'));
      final newWrite = find.widgetWithText(OutlinedButton, '检查组合候选');
      await t.ensureVisible(newWrite);
      await t.pumpAndSettle();
      expect(t.widget<OutlinedButton>(newWrite).onPressed, isNull);
      expect(
        t
            .getSemantics(newWrite)
            .getSemanticsData()
            .hasAction(SemanticsAction.tap),
        false,
      );
      await t.sendKeyEvent(LogicalKeyboardKey.tab);
      await t.sendKeyEvent(LogicalKeyboardKey.enter);
      await t.pumpAndSettle();
      expect(f.requests.skip(before).every((r) => r['method'] == 'GET'), true);
      expect((await store.read('http://fixture', candidateOwner)).length, 1);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      semantics.dispose();
      auth.dispose();
      client.close();
    },
  );
  testWidgets(
    'pending human receipt blocks multi pointer focus and semantics without destroying section',
    (t) async {
      final auth = await candidateAuth(),
          f = MultiWireFixture(),
          store = MemoryAgentCandidatePendingStore();
      final pending = pendingAcceptance(
        expires: DateTime.now().toUtc().add(const Duration(seconds: 2)),
      );
      await store.write('http://fixture', candidateOwner, pending);
      final c = MockClient((r) async {
        if (r.url.path == '/v1/me/agent-memory-candidates') {
          return candidateResponse([candidateWire()]);
        }
        if (r.url.path == '/v1/me/agent-memory-candidates/$candidateTarget') {
          return candidateResponse(candidateWire());
        }
        return f.handle(r);
      });
      final semantics = t.ensureSemantics();
      await t.pumpWidget(
        MaterialApp(
          home: AgentMemoryCandidatePage(
            multiPendingStore: _testMultiStore,
            auth: auth,
            client: c,
            apiBaseUrl: 'http://fixture',
            pendingStore: store,
          ),
        ),
      );
      await t.pumpAndSettle();
      final refreshAction = t
          .widget<OutlinedButton>(find.widgetWithText(OutlinedButton, '刷新候选'))
          .onPressed!;
      final section = find.byKey(
        const ValueKey('human-multi-candidate-section'),
      );
      final button = find.text('选择来源和已有任务');
      await t.scrollUntilVisible(button, 200);
      await t.ensureVisible(button);
      await t.pumpAndSettle();
      final original = t.element(section);
      final boundary = find.byKey(
        const ValueKey('candidate-multi-recovery-boundary'),
      );
      expect(
        t
            .getSemantics(boundary)
            .getSemanticsData()
            .hasAction(SemanticsAction.tap),
        false,
      );
      expect(
        find
            .descendant(of: boundary, matching: find.byType(ExcludeSemantics))
            .evaluate()
            .single
            .widget,
        isA<ExcludeSemantics>().having((e) => e.excluding, 'excluded', true),
      );
      await t.tap(button, warnIfMissed: false);
      await t.pumpAndSettle();
      final node = Focus.of(
        t.element(
          find
              .ancestor(of: button, matching: find.byType(OutlinedButton))
              .first,
        ),
      );
      node.requestFocus();
      await t.pump();
      expect(node.hasFocus, false);
      await t.sendKeyEvent(LogicalKeyboardKey.enter);
      await t.pumpAndSettle();
      expect(f.requests, isEmpty);
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 2100)),
      );
      // Invoke the real refresh callback without moving the lazy list away from the Section.
      refreshAction();
      await t.pumpAndSettle();
      expect(identical(original, t.element(section)), true);
      expect(
        t.getSemantics(boundary).getSemanticsData().label,
        contains('多来源整理'),
      );
      await t.ensureVisible(button);
      await t.tap(button);
      await t.pumpAndSettle();
      expect(f.requests, isNotEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      c.close();
      semantics.dispose();
    },
  );

  testWidgets(
    'original human page record refresh preserves the multi section identity and result',
    (t) async {
      final auth = await candidateAuth(), f = MultiWireFixture();
      var candidateReads = 0;
      final transport = MockClient((r) async {
        if (r.url.path == '/v1/me/agent-memory-candidates') {
          candidateReads++;
          return candidateResponse(f.staged ? [f.receipt()['candidate']] : []);
        }
        return f.handle(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: AgentMemoryCandidatePage(
            multiPendingStore: _testMultiStore,
            pendingStore: MemoryAgentCandidatePendingStore(),
            auth: auth,
            client: transport,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.scrollUntilVisible(find.text('选择来源和已有任务'), 240);
      await multiTap(t, find.text('选择来源和已有任务'));
      await multiTap(t, find.byKey(const ValueKey('multi-task-$multiTask')));
      for (final id in [candidateMoment, candidateSaved]) {
        await multiTap(t, find.byKey(ValueKey('multi-source-$id')));
        await multiTap(t, find.byKey(ValueKey('multi-review-$id')));
        await multiTap(t, find.text('允许这次本地分析'));
      }
      await multiTap(t, find.text('检查组合候选'));
      await multiTap(t, find.text('批准这项候选的保留'));
      await multiTap(t, find.text('提交为待确认候选'));
      expect(find.textContaining('候选已进入“我的候选记录”'), findsOneWidget);
      expect(find.text('选择来源和已有任务'), findsNothing);
      expect(candidateReads, 2);
      expect(
        f.requests
            .where((r) => (r['path'] as String).endsWith('/stage'))
            .length,
        1,
      );
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      transport.close();
    },
  );
  for (final brightness in Brightness.values) {
    testWidgets(
      'Chinese 320px 2x $brightness concrete analysis review remains readable and cancellable',
      (t) async {
        t.view.physicalSize = const Size(320, 740);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final auth = await candidateAuth(),
            f = MultiWireFixture()..title = '超长真实来源标题羽毛球活动与同学周末安排' * 6;
        final client = MockClient(f.handle);
        await t.pumpWidget(
          multiHarness(
            AgentMultiCandidateSection(
              pendingStore: MemoryAgentMultiCandidatePendingStore(),
              auth: auth,
              client: client,
              apiBaseUrl: 'http://fixture',
            ),
            scale: 2,
            brightness: brightness,
          ),
        );
        await multiTap(t, find.text('选择来源和已有任务'));
        await multiTap(t, find.byKey(const ValueKey('multi-task-$multiTask')));
        await multiTap(
          t,
          find.byKey(const ValueKey('multi-source-$candidateMoment')),
        );
        await multiTap(
          t,
          find.byKey(const ValueKey('multi-review-$candidateMoment')),
        );
        await t.ensureVisible(find.text('允许这次本地分析'));
        await t.pumpAndSettle();
        expect(find.text('本次来源分析预览'), findsOneWidget);
        expect(find.text('标题：${f.title}'), findsOneWidget);
        expect(find.text('动态具体版本：修订 1'), findsOneWidget);
        expect(t.takeException(), isNull);
        await multiTap(t, find.text('取消本次预览'));
        expect(find.text('允许这次本地分析'), findsNothing);
        expect(
          f.requests.where((r) => (r['path'] as String).endsWith('/approve')),
          isEmpty,
        );
        await t.pumpWidget(const SizedBox());
        auth.dispose();
        client.close();
      },
    );
  }
  for (final hiking in [false, true]) {
    testWidgets(
      'two explicit source approvals, combo approve and stage update original candidate list only once hiking=$hiking',
      (t) async {
        final auth = await candidateAuth(),
            f = MultiWireFixture(),
            client = MockClient(MultiWireFixture().handle);
        // Use the one fixture's stable task/source timestamps for every response.
        client.close();
        final transport = MockClient(f.handle);
        if (hiking) {
          f.category = 'hiking';
          f.algorithm = 'moment-lexical-category-v2';
          f.title = '本人的徒步记录';
        }
        var refreshes = 0;
        await t.pumpWidget(
          multiHarness(
            AgentMultiCandidateSection(
              pendingStore: MemoryAgentMultiCandidatePendingStore(),
              auth: auth,
              client: transport,
              apiBaseUrl: 'http://fixture',
              onCandidateChanged: () => refreshes++,
            ),
          ),
        );
        await multiTap(t, find.text('选择来源和已有任务'));
        await multiTap(t, find.byKey(const ValueKey('multi-task-$multiTask')));
        for (final id in [candidateMoment, candidateSaved]) {
          await multiTap(t, find.byKey(ValueKey('multi-source-$id')));
          await multiTap(t, find.byKey(ValueKey('multi-review-$id')));
          await multiTap(t, find.text('允许这次本地分析'));
        }
        await multiTap(t, find.text('检查组合候选'));
        expect(find.text('组合候选预览'), findsOneWidget);
        expect(find.text('建议活动类别：${hiking ? '徒步' : '羽毛球'}'), findsOneWidget);
        await multiTap(t, find.text('批准这项候选的保留'));
        expect(refreshes, 0);
        await multiTap(t, find.text('提交为待确认候选'));
        expect(refreshes, 1);
        expect(find.textContaining('候选已进入“我的候选记录”'), findsOneWidget);
        expect(
          f.requests.where(
            (r) => (r['path'] as String).contains('agent-memory-candidates'),
          ),
          isEmpty,
        );
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        auth.dispose();
        transport.close();
      },
    );
  }
  testWidgets(
    'workspace ABA drops review and returns to an unapproved personal frame',
    (t) async {
      final auth = await candidateAuth(),
          f = MultiWireFixture(),
          work = ValueNotifier<String?>(null),
          client = MockClient(MultiWireFixture().handle);
      client.close();
      final transport = MockClient(f.handle);
      String? workspace() => work.value;
      await t.pumpWidget(
        multiHarness(
          AgentMultiCandidateSection(
            pendingStore: MemoryAgentMultiCandidatePendingStore(),
            auth: auth,
            client: transport,
            apiBaseUrl: 'http://fixture',
            workspaceChanges: work,
            organizationWorkspaceID: workspace,
          ),
        ),
      );
      await multiTap(t, find.text('选择来源和已有任务'));
      await multiTap(t, find.byKey(const ValueKey('multi-task-$multiTask')));
      await multiTap(
        t,
        find.byKey(const ValueKey('multi-source-$candidateMoment')),
      );
      await multiTap(
        t,
        find.byKey(const ValueKey('multi-review-$candidateMoment')),
      );
      work.value = 'orgA';
      await t.pumpAndSettle();
      expect(find.text('允许这次本地分析'), findsNothing);
      work.value = null;
      await t.pumpAndSettle();
      expect(find.text('本次来源分析预览'), findsNothing);
      expect(
        f.requests.where((r) => (r['path'] as String).endsWith('/approve')),
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      work.dispose();
      transport.close();
    },
  );
  for (final change in [
    'endpoint',
    'client',
    'workspaceGetter',
    'auth',
    'pendingStore',
  ]) {
    testWidgets('changing $change permanently retires section even on A B A', (
      t,
    ) async {
      final auth = await candidateAuth(),
          otherAuth = await candidateAuth(),
          f = MultiWireFixture();
      final client = MockClient(f.handle), otherClient = MockClient(f.handle);
      final store = MemoryAgentMultiCandidatePendingStore(),
          otherStore = MemoryAgentMultiCandidatePendingStore();
      String? workspaceA() => null;
      String? workspaceB() => null;
      const key = ValueKey('single-section-frame');
      Widget frame(bool alternate) => multiHarness(
        AgentMultiCandidateSection(
          pendingStore: change == 'pendingStore' && alternate
              ? otherStore
              : store,
          key: key,
          auth: change == 'auth' && alternate ? otherAuth : auth,
          client: change == 'client' && alternate ? otherClient : client,
          apiBaseUrl: change == 'endpoint' && alternate
              ? 'http://other'
              : 'http://fixture',
          organizationWorkspaceID: change == 'workspaceGetter' && alternate
              ? workspaceB
              : workspaceA,
        ),
      );
      await t.pumpWidget(frame(false));
      await multiTap(t, find.text('选择来源和已有任务'));
      final count = f.requests.length;
      await t.pumpWidget(frame(true));
      await t.pumpAndSettle();
      expect(find.textContaining('来源页面连接已变更'), findsOneWidget);
      await t.pumpWidget(frame(false));
      await t.pumpAndSettle();
      expect(find.text('选择来源和已有任务'), findsNothing);
      expect(f.requests.length, count);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      otherAuth.dispose();
      client.close();
      otherClient.close();
    });
  }
  testWidgets(
    'permission unavailable is explained without generating a fake candidate',
    (t) async {
      final auth = await candidateAuth(),
          f = MultiWireFixture()..rejectPreview = true,
          client = MockClient(MultiWireFixture().handle);
      client.close();
      final transport = MockClient(f.handle);
      await t.pumpWidget(
        multiHarness(
          AgentMultiCandidateSection(
            pendingStore: MemoryAgentMultiCandidatePendingStore(),
            auth: auth,
            client: transport,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await multiTap(t, find.text('选择来源和已有任务'));
      await multiTap(t, find.byKey(const ValueKey('multi-task-$multiTask')));
      await multiTap(
        t,
        find.byKey(const ValueKey('multi-source-$candidateMoment')),
      );
      await multiTap(
        t,
        find.byKey(const ValueKey('multi-review-$candidateMoment')),
      );
      expect(find.textContaining('服务暂不可用'), findsOneWidget);
      expect(find.text('允许这次本地分析'), findsNothing);
      expect(
        f.requests.where((r) => (r['path'] as String).endsWith('/stage')),
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      transport.close();
    },
  );
}
