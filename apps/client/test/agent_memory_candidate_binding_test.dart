import 'package:birdtie_client/src/workspace/agent_multi_candidate_pending_store.dart';
import 'package:birdtie_client/src/workspace/agent_candidate_pending_store.dart';
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_memory_candidate_api_test.dart';

late MemoryAgentCandidatePendingStore bindingPendingStore;

const retiredCandidate = '页面连接已变更，请返回后重新打开。';

class CountingCandidateClient extends MockClient {
  CountingCandidateClient(super.handler);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

class CandidateChanges extends ChangeNotifier {
  bool get listening => hasListeners;
  void change() => notifyListeners();
}

Future<(BirdtieAuthController, http.Client)> bindingAuth(String token) async {
  final client = MockClient(
    (r) async => candidateResponse(switch (r.url.path) {
      '/v1/me' => {'id': candidateOwner},
      '/v1/auth/dev-phone/status' => {'enabled': true},
      _ => {'displayName': '本人合成测试'},
    }),
  );
  final auth = BirdtieAuthController(
    client: client,
    apiBaseUrl: 'http://auth-fixture',
    sessionVault: MemorySessionVault()
      ..session = StoredSession(token: token, method: 'dev_phone'),
  );
  await auth.initialize();
  return (auth, client);
}

Widget candidateFrame(
  BirdtieAuthController auth,
  http.Client client,
  String base, {
  Listenable? changes,
  String? Function()? workspace,
  Key key = const ValueKey('candidate-stable'),
}) => MaterialApp(
  home: AgentMemoryCandidatePage(
    multiPendingStore: _testMultiStore,
    pendingStore: bindingPendingStore,
    key: key,
    auth: auth,
    client: client,
    apiBaseUrl: base,
    workspaceChanges: changes,
    organizationWorkspaceID: workspace,
  ),
);

Future<void> showCandidatePreview(WidgetTester t) async {
  await t.scrollUntilVisible(find.text('检查这项候选'), 220);
  await t.ensureVisible(find.text('检查这项候选'));
  await t.pumpAndSettle();
  await t.tap(find.text('检查这项候选'));
  await t.pumpAndSettle();
  await t.drag(find.byType(ListView), const Offset(0, 500));
  await t.pumpAndSettle();
  expect(find.text('具体版本预览'), findsOneWidget);
}

late AgentMultiCandidatePendingStore _testMultiStore;
void main() {
  setUp(() => _testMultiStore = MemoryAgentMultiCandidatePendingStore());
  testWidgets(
    'multi store replacement permanently retires original human page A B A',
    (t) async {
      final (auth, authClient) = await bindingAuth('token-a');
      final client = MockClient(
        (r) async => candidateResponse([candidateWire()]),
      );
      final a = MemoryAgentMultiCandidatePendingStore(),
          b = MemoryAgentMultiCandidatePendingStore();
      Widget frame(AgentMultiCandidatePendingStore store) => MaterialApp(
        home: AgentMemoryCandidatePage(
          key: const ValueKey('multi-store-binding'),
          auth: auth,
          client: client,
          apiBaseUrl: 'http://fixture',
          pendingStore: bindingPendingStore,
          multiPendingStore: store,
        ),
      );
      await t.pumpWidget(frame(a));
      await t.pumpAndSettle();
      await t.pumpWidget(frame(b));
      await t.pumpAndSettle();
      expect(find.text(retiredCandidate), findsOneWidget);
      await t.pumpWidget(frame(a));
      await t.pumpAndSettle();
      expect(find.text(retiredCandidate), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
      authClient.close();
    },
  );
  testWidgets(
    'pending store A B A permanently retires old concrete preview with zero accept',
    (t) async {
      final (auth, authClient) = await bindingAuth('token-a');
      final original = MemoryAgentCandidatePendingStore();
      final other = MemoryAgentCandidatePendingStore();
      var posts = 0;
      final c = MockClient((r) async {
        if (r.method == 'GET') {
          return candidateResponse([candidateWire()]);
        }
        if (r.url.path.endsWith('/preview')) {
          return candidateResponse(
            previewWire(
              (jsonDecode(r.body) as Map)['previewId'],
              candidateWire(),
            ),
          );
        }
        posts++;
        return candidateResponse(candidateWire(status: 'ACTIVE'));
      });
      Widget frame(AgentCandidatePendingStore store) => MaterialApp(
        home: AgentMemoryCandidatePage(
          multiPendingStore: _testMultiStore,
          key: const ValueKey('store-binding'),
          auth: auth,
          client: c,
          apiBaseUrl: 'http://fixture',
          pendingStore: store,
        ),
      );
      await t.pumpWidget(frame(original));
      await t.pumpAndSettle();
      await showCandidatePreview(t);
      await t.pumpWidget(frame(other));
      await t.pumpAndSettle();
      await t.pumpWidget(frame(original));
      await t.pumpAndSettle();
      expect(find.text(retiredCandidate), findsOneWidget);
      expect(find.text('确认保存这项声明'), findsNothing);
      expect(posts, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      authClient.close();
      c.close();
    },
  );

  setUp(() {
    bindingPendingStore = MemoryAgentCandidatePendingStore();
  });
  testWidgets('normal initial GET preview and exact approval still work', (
    t,
  ) async {
    final (a, ac) = await bindingAuth('token-a');
    String? preview;
    var gets = 0, accepts = 0;
    final c = MockClient((r) async {
      expect(r.headers['authorization'], 'Bearer token-a');
      expect(r.url.host, 'old-fixture');
      if (r.method == 'GET') {
        gets++;
        return candidateResponse([candidateWire()]);
      }
      final body = jsonDecode(r.body) as Map;
      if (r.url.path.endsWith('/preview')) {
        preview = body['previewId'] as String;
        return candidateResponse(previewWire(preview!, candidateWire()));
      }
      expect(r.url.path.endsWith('/$candidateTarget/accept'), isTrue);
      expect(body, {'previewId': preview});
      accepts++;
      return candidateResponse(candidateWire(status: 'ACTIVE'));
    });
    await t.pumpWidget(candidateFrame(a, c, 'http://old-fixture'));
    await t.pumpAndSettle();
    await showCandidatePreview(t);
    // A harmless rebuild must not retire a concrete, still-current approval.
    await t.pumpWidget(candidateFrame(a, c, 'http://old-fixture'));
    await t.pumpAndSettle();
    expect(find.text('具体版本预览'), findsOneWidget);
    await t.scrollUntilVisible(find.text('确认保存这项声明'), 180);
    await t.ensureVisible(find.text('确认保存这项声明'));
    await t.pumpAndSettle();
    await t.tap(find.text('确认保存这项声明'));
    await t.pumpAndSettle();
    expect(gets, 1);
    expect(accepts, 1);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    a.dispose();
    ac.close();
    c.close();
  });

  testWidgets(
    'base A B A is permanently retired and explicit new State reads fresh',
    (t) async {
      final (a, ac) = await bindingAuth('token-a');
      final requests = <http.Request>[];
      final c = CountingCandidateClient((r) async {
        requests.add(r);
        return candidateResponse([candidateWire()]);
      });
      await t.pumpWidget(candidateFrame(a, c, 'http://old-fixture'));
      await t.pumpAndSettle();
      await t.pumpWidget(candidateFrame(a, c, 'http://new-fixture'));
      await t.pumpAndSettle();
      await t.pumpWidget(candidateFrame(a, c, 'http://old-fixture'));
      await t.pumpAndSettle();
      expect(find.text(retiredCandidate), findsOneWidget);
      expect(requests.length, 1);
      expect(c.closes, 0);
      await t.pumpWidget(
        candidateFrame(
          a,
          c,
          'http://new-fixture',
          key: const ValueKey('fresh'),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text(retiredCandidate), findsNothing);
      expect(requests.length, 2);
      expect(requests.last.url.host, 'new-fixture');
      expect(requests.every((r) => r.method == 'GET'), isTrue);
      await t.pumpWidget(const SizedBox());
      expect(c.closes, 0);
      a.dispose();
      ac.close();
      c.close();
    },
  );

  for (final changed in ['client', 'listenable', 'getter']) {
    testWidgets(
      '$changed replacement retires old listeners and borrowed transport',
      (t) async {
        final (a, ac) = await bindingAuth('token-a');
        var oldCalls = 0, newCalls = 0;
        final old = CountingCandidateClient((r) async {
          oldCalls++;
          return candidateResponse([candidateWire()]);
        });
        final next = CountingCandidateClient((r) async {
          newCalls++;
          return candidateResponse([candidateWire()]);
        });
        final changes = CandidateChanges(), nextChanges = CandidateChanges();
        String? getter() => null;
        String? nextGetter() => null;
        await t.pumpWidget(
          candidateFrame(
            a,
            old,
            'http://old-fixture',
            changes: changes,
            workspace: getter,
          ),
        );
        await t.pumpAndSettle();
        expect(changes.listening, isTrue);
        await t.pumpWidget(
          candidateFrame(
            a,
            changed == 'client' ? next : old,
            'http://old-fixture',
            changes: changed == 'listenable' ? nextChanges : changes,
            workspace: changed == 'getter' ? nextGetter : getter,
          ),
        );
        await t.pumpAndSettle();
        expect(find.text(retiredCandidate), findsOneWidget);
        expect(changes.listening, isFalse);
        expect(nextChanges.listening, isFalse);
        changes.change();
        nextChanges.change();
        await t.pumpAndSettle();
        expect(oldCalls, 1);
        expect(newCalls, 0);
        expect(old.closes, 0);
        expect(next.closes, 0);
        await t.pumpWidget(const SizedBox());
        expect(old.closes, 0);
        expect(next.closes, 0);
        a.dispose();
        ac.close();
        old.close();
        next.close();
        changes.dispose();
        nextChanges.dispose();
      },
    );
  }

  for (final phase in ['GET', 'preview', 'accept', 'unknownAccept']) {
    testWidgets(
      'late $phase after endpoint retirement cannot restore approval or retry',
      (t) async {
        final (a, ac) = await bindingAuth('token-a');
        final pending = Completer<http.Response>();
        var accepts = 0, gets = 0;
        String? opaque;
        final c = MockClient((r) async {
          if (r.method == 'GET') {
            gets++;
            return phase == 'GET'
                ? pending.future
                : candidateResponse([candidateWire()]);
          }
          if (r.url.path.endsWith('/preview')) {
            opaque = (jsonDecode(r.body) as Map)['previewId'] as String;
            return phase == 'preview'
                ? pending.future
                : candidateResponse(previewWire(opaque!, candidateWire()));
          }
          accepts++;
          expect(jsonDecode(r.body), {'previewId': opaque});
          return pending.future;
        });
        await t.pumpWidget(candidateFrame(a, c, 'http://old-fixture'));
        if (phase != 'GET') {
          await t.pumpAndSettle();
          if (phase == 'preview') {
            await t.scrollUntilVisible(find.text('检查这项候选'), 220);
            await t.ensureVisible(find.text('检查这项候选'));
            await t.pumpAndSettle();
            await t.tap(find.text('检查这项候选'));
            await t.pump();
          } else {
            await showCandidatePreview(t);
            await t.scrollUntilVisible(find.text('确认保存这项声明'), 180);
            await t.ensureVisible(find.text('确认保存这项声明'));
            await t.pumpAndSettle();
            await t.tap(find.text('确认保存这项声明'));
            await t.pump();
          }
        }
        await t.pumpWidget(candidateFrame(a, c, 'http://new-fixture'));
        await t.pumpAndSettle();
        if (phase == 'unknownAccept') {
          pending.completeError(Exception('lost native response'));
        } else {
          pending.complete(
            candidateResponse(
              phase == 'GET'
                  ? [candidateWire()]
                  : phase == 'preview'
                  ? previewWire(opaque!, candidateWire())
                  : candidateWire(status: 'ACTIVE'),
            ),
          );
        }
        await t.pumpAndSettle();
        expect(find.text(retiredCandidate), findsOneWidget);
        expect(find.text('具体版本预览'), findsNothing);
        expect(find.text('确认保存这项声明'), findsNothing);
        expect(gets, 1);
        expect(accepts, phase == 'accept' || phase == 'unknownAccept' ? 1 : 0);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        a.dispose();
        ac.close();
        c.close();
      },
    );
  }

  testWidgets(
    '320 width large text retired route has reachable semantic return',
    (t) async {
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final semantics = t.ensureSemantics();
      final (a, ac) = await bindingAuth('token-a');
      var posts = 0;
      final c = MockClient((r) async {
        if (r.method != 'GET') {
          posts++;
        }
        return candidateResponse([candidateWire()]);
      });
      final base = ValueNotifier('http://old-fixture');
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (context) => ValueListenableBuilder<String>(
                      valueListenable: base,
                      builder: (context, value, child) =>
                          AgentMemoryCandidatePage(
                            multiPendingStore: _testMultiStore,
                            pendingStore: bindingPendingStore,
                            key: const ValueKey('route'),
                            auth: a,
                            client: c,
                            apiBaseUrl: value,
                          ),
                    ),
                  ),
                ),
                child: const Text('原入口'),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('原入口'));
      await t.pumpAndSettle();
      base.value = 'http://new-fixture';
      await t.pumpAndSettle();
      await t.scrollUntilVisible(find.text('返回'), 180);
      await t.ensureVisible(find.text('返回'));
      await t.pumpAndSettle();
      final button = find.ancestor(
        of: find.text('返回'),
        matching: find.byType(FilledButton),
      );
      expect(t.getSize(button).height, greaterThanOrEqualTo(48));
      expect(t.getSize(button).width, greaterThanOrEqualTo(48));
      expect(t.getSemantics(button).label, contains('返回'));
      expect(t.takeException(), isNull);
      await t.tap(button);
      await t.pumpAndSettle();
      expect(find.text('原入口'), findsOneWidget);
      expect(posts, 0);
      await t.pumpWidget(const SizedBox());
      base.dispose();
      a.dispose();
      ac.close();
      c.close();
      semantics.dispose();
    },
  );
  testWidgets(
    'same key new session never sends its token to the old endpoint',
    (t) async {
      final (a, ac) = await bindingAuth('token-a');
      final (b, bc) = await bindingAuth('token-b');
      final oldRequests = <http.Request>[], newRequests = <http.Request>[];
      final old = MockClient((r) async {
        oldRequests.add(r);
        return candidateResponse([candidateWire()]);
      });
      final next = MockClient((r) async {
        newRequests.add(r);
        return candidateResponse([candidateWire()]);
      });
      await t.pumpWidget(candidateFrame(a, old, 'http://old-fixture'));
      await t.pumpAndSettle();
      await t.pumpWidget(candidateFrame(b, next, 'http://new-fixture'));
      await t.pumpAndSettle();
      expect(
        oldRequests.where(
          (r) => r.headers['authorization'] == 'Bearer token-b',
        ),
        isEmpty,
        reason: 'a new session must not reach the retired API transport',
      );
      expect(find.text(retiredCandidate), findsOneWidget);
      expect(newRequests, isEmpty);
      await t.pumpWidget(const SizedBox());
      a.dispose();
      b.dispose();
      ac.close();
      bc.close();
      old.close();
      next.close();
    },
  );

  testWidgets('same identity base change removes old concrete approval', (
    t,
  ) async {
    final (a, ac) = await bindingAuth('token-a');
    var accepts = 0;
    final c = MockClient((r) async {
      if (r.method == 'GET') {
        return candidateResponse([candidateWire()]);
      }
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
    await t.pumpWidget(candidateFrame(a, c, 'http://old-fixture'));
    await t.pumpAndSettle();
    await showCandidatePreview(t);
    await t.pumpWidget(candidateFrame(a, c, 'http://new-fixture'));
    await t.pumpAndSettle();
    expect(find.text('具体版本预览'), findsNothing);
    expect(find.text('确认保存这项声明'), findsNothing);
    expect(find.text(retiredCandidate), findsOneWidget);
    expect(accepts, 0);
    await t.pumpWidget(const SizedBox());
    a.dispose();
    ac.close();
    c.close();
  });
}
