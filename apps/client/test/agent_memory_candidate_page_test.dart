import 'package:birdtie_client/src/workspace/agent_multi_candidate_pending_store.dart';
import 'package:birdtie_client/src/workspace/agent_candidate_pending_store.dart';
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_page.dart';
import 'package:flutter/material.dart';
import 'agent_candidate_pending_store_test.dart' show pendingAcceptance;
import 'agent_memory_candidate_controller_test.dart' show FailingCandidateStore;
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_memory_candidate_api_test.dart';

Future<BirdtieAuthController> candidateAuth() async {
  final c = MockClient(
    (r) async => candidateResponse(switch (r.url.path) {
      '/v1/me' => {'id': candidateOwner},
      '/v1/auth/dev-phone/status' => {'enabled': true},
      _ => {'displayName': '本人测试'},
    }),
  );
  final a = BirdtieAuthController(
    client: c,
    apiBaseUrl: 'http://fixture',
    sessionVault: MemorySessionVault()
      ..session = const StoredSession(token: 'synthetic', method: 'dev_phone'),
  );
  await a.initialize();
  return a;
}

late AgentMultiCandidatePendingStore _testMultiStore;
void main() {
  setUp(() => _testMultiStore = MemoryAgentMultiCandidatePendingStore());
  for (final storageFailure in [false, true]) {
    testWidgets(
      '320 font3 IME260 reopened human recovery safe controls storageFailure=$storageFailure',
      (t) async {
        t.view.physicalSize = const Size(320, 740);
        t.view.devicePixelRatio = 1;
        t.view.viewInsets = const FakeViewPadding(bottom: 260);
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        addTearDown(t.view.resetViewInsets);
        final auth = await candidateAuth(), store = FailingCandidateStore();
        var posts = 0;
        if (storageFailure) {
          store.failRead = true;
        } else {
          await store.write(
            'http://fixture',
            candidateOwner,
            pendingAcceptance(),
          );
        }
        final c = MockClient((r) async {
          if (r.method != 'GET') posts++;
          return candidateResponse(
            r.url.path == '/v1/me/agent-memory-candidates'
                ? [candidateWire()]
                : candidateWire(),
          );
        });
        await t.pumpWidget(
          MaterialApp(
            home: Builder(
              builder: (context) => MediaQuery(
                data: MediaQuery.of(
                  context,
                ).copyWith(textScaler: TextScaler.linear(3)),
                child: AgentMemoryCandidatePage(
                  multiPendingStore: _testMultiStore,
                  auth: auth,
                  client: c,
                  apiBaseUrl: 'http://fixture',
                  pendingStore: store,
                ),
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(find.text('具体版本预览'), findsNothing);
        expect(find.text('确认保存这项声明'), findsNothing);
        final verify = find.text('核实原候选结果');
        await t.scrollUntilVisible(verify, 200);
        await t.ensureVisible(verify);
        await t.pumpAndSettle();
        final target = find.ancestor(
          of: verify,
          matching: find.byType(FilledButton),
        );
        expect(t.getSize(target).height, greaterThanOrEqualTo(48));
        await t.tap(verify);
        await t.pumpAndSettle();
        expect(posts, 0);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        auth.dispose();
        c.close();
      },
    );
  }

  testWidgets(
    'workspace change removes concrete review and old approval action',
    (t) async {
      final a = await candidateAuth();
      final workspace = ValueNotifier<String?>(null);
      var accepts = 0;
      final c = MockClient((r) async {
        if (r.method == 'GET') return candidateResponse([candidateWire()]);
        if (r.url.path.endsWith('/preview')) {
          return candidateResponse(
            previewWire(
              (jsonDecode(r.body) as Map)['previewId'] as String,
              candidateWire(),
            ),
          );
        }
        accepts++;
        return candidateResponse(candidateWire(status: 'ACTIVE'));
      });
      await t.pumpWidget(
        MaterialApp(
          home: AgentMemoryCandidatePage(
            multiPendingStore: _testMultiStore,
            pendingStore: MemoryAgentCandidatePendingStore(),
            auth: a,
            client: c,
            apiBaseUrl: 'http://fixture',
            workspaceChanges: workspace,
            organizationWorkspaceID: () => workspace.value,
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.scrollUntilVisible(find.text('检查这项候选'), 200);
      await t.ensureVisible(find.text('检查这项候选'));
      await t.pumpAndSettle();
      await t.tap(find.text('检查这项候选'));
      await t.pumpAndSettle();
      await t.drag(find.byType(ListView), const Offset(0, 500));
      await t.pumpAndSettle();
      expect(find.text('具体版本预览'), findsOneWidget);
      workspace.value = 'organization';
      await t.pumpAndSettle();
      expect(find.text('具体版本预览'), findsNothing);
      expect(find.text('确认保存这项声明'), findsNothing);
      expect(find.textContaining('退出组织工作台'), findsOneWidget);
      expect(accepts, 0);
      await t.pumpWidget(const SizedBox());
      a.dispose();
      workspace.dispose();
      c.close();
    },
  );
  testWidgets('Chinese concrete version review cancel is no write', (t) async {
    final a = await candidateAuth();
    var accepts = 0;
    final c = MockClient((r) async {
      if (r.method == 'GET') return candidateResponse([candidateWire()]);
      if (r.url.path.endsWith('/preview')) {
        return candidateResponse(
          previewWire(
            (jsonDecode(r.body) as Map)['previewId'] as String,
            candidateWire(),
          ),
        );
      }
      accepts++;
      return candidateResponse(candidateWire(status: 'ACTIVE'));
    });
    await t.pumpWidget(
      MaterialApp(
        home: AgentMemoryCandidatePage(
          multiPendingStore: _testMultiStore,
          pendingStore: MemoryAgentCandidatePendingStore(),
          auth: a,
          client: c,
          apiBaseUrl: 'http://fixture',
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(find.text('检查这项候选'), 240);
    await t.ensureVisible(find.text('检查这项候选'));
    await t.pumpAndSettle();
    await t.tap(find.text('检查这项候选'));
    await t.pumpAndSettle();
    await t.drag(find.byType(ListView), const Offset(0, 500));
    await t.pumpAndSettle();
    expect(
      find.text('具体版本预览'),
      findsOneWidget,
      reason: t
          .widgetList<Text>(find.byType(Text))
          .map((e) => e.data)
          .join('\n'),
    );
    await t.scrollUntilVisible(find.text('取消预览'), 220);
    await t.ensureVisible(find.text('取消预览'));
    await t.pumpAndSettle();
    await t.tap(find.text('取消预览'));
    await t.pumpAndSettle();
    expect(accepts, 0);
    expect(find.text('具体版本预览'), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    a.dispose();
    c.close();
  });
  testWidgets('360dp and 1.6 text scale scroll access without overflow', (
    t,
  ) async {
    t.view.physicalSize = const Size(360, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final a = await candidateAuth();
    final c = MockClient((_) async => candidateResponse([]));
    final semantics = t.ensureSemantics();
    await t.pumpWidget(
      MaterialApp(
        builder: (ctx, child) => MediaQuery(
          data: MediaQuery.of(
            ctx,
          ).copyWith(textScaler: const TextScaler.linear(1.6)),
          child: child!,
        ),
        home: AgentMemoryCandidatePage(
          multiPendingStore: _testMultiStore,
          pendingStore: MemoryAgentCandidatePendingStore(),
          auth: a,
          client: c,
          apiBaseUrl: 'http://fixture',
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(find.text('保存待确认候选'), 180);
    expect(t.takeException(), isNull);
    expect(find.text('保存待确认候选'), findsOneWidget);
    semantics.dispose();
    await t.pumpWidget(const SizedBox());
    a.dispose();
    c.close();
  });
  testWidgets('default OFF explains unavailable without fake success', (
    t,
  ) async {
    final a = await candidateAuth();
    final c = MockClient((_) async => http.Response('{}', 503));
    await t.pumpWidget(
      MaterialApp(
        home: AgentMemoryCandidatePage(
          multiPendingStore: _testMultiStore,
          pendingStore: MemoryAgentCandidatePendingStore(),
          auth: a,
          client: c,
          apiBaseUrl: 'http://fixture',
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.textContaining('未启用'), findsOneWidget);
    expect(find.text('已保存为本人明确声明的私密记忆。'), findsNothing);
    await t.pumpWidget(const SizedBox());
    a.dispose();
    c.close();
  });
}
