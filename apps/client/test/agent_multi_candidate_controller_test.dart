import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_api.dart';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_controller.dart';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_memory_candidate_api_test.dart';
import 'agent_multi_candidate_api_test.dart';
import 'agent_multi_candidate_pending_store_test.dart' show pendingMulti;

AgentMultiCandidateController multiController(
  MultiWireFixture f, {
  http.Client? client,
  AgentMultiCandidatePendingStore? store,
  String? Function()? auth,
  String? Function()? owner,
  String? Function()? workspace,
}) => AgentMultiCandidateController(
  api: f.api(client),
  pendingStore: store ?? MemoryAgentMultiCandidatePendingStore(),
  authorizationHeader: auth ?? () => 'Bearer synthetic',
  accountID: owner ?? () => candidateOwner,
  organizationWorkspaceID: workspace ?? () => null,
);
Future<void> approveTwoSources(AgentMultiCandidateController c) async {
  await c.load();
  c.edit(nextTask: multiTask);
  for (final id in [candidateMoment, candidateSaved]) {
    c.edit(toggleMoment: id);
    await c.prepareAnalysis(id);
    await c.approve();
  }
}

Future<void> approveCombination(AgentMultiCandidateController c) async {
  await approveTwoSources(c);
  await c.prepareMulti();
  await c.approve();
}

class RecoveryProbeStore extends MemoryAgentMultiCandidatePendingStore {
  bool failRead = false,
      failWrite = false,
      wrongReadback = false,
      failDelete = false;
  Completer<void>? pauseWrite, pauseDelete;
  final writeEntered = Completer<void>();
  @override
  Future<List<PendingMultiCandidateOperation>> read(String e, String o) async {
    if (failRead) throw StateError('synthetic secure read error');
    if (wrongReadback && values.isNotEmpty) return [];
    return super.read(e, o);
  }

  @override
  Future<void> write(
    String e,
    String o,
    PendingMultiCandidateOperation p,
  ) async {
    if (failWrite) throw StateError('synthetic secure write error');
    await super.write(e, o, p);
    if (!writeEntered.isCompleted) writeEntered.complete();
    if (pauseWrite != null) await pauseWrite!.future;
  }

  @override
  Future<void> delete(
    String e,
    String o,
    PendingMultiCandidateOperation p,
  ) async {
    if (failDelete) throw StateError('synthetic delete error');
    if (pauseDelete != null) await pauseDelete!.future;
    await super.delete(e, o, p);
  }
}

void main() {
  for (final reopened in [false, true]) {
    test(
      'CLOSED metadata ends approval unknown only after fixed native deadline reopened=$reopened',
      () async {
        final f = MultiWireFixture(),
            store = MemoryAgentMultiCandidatePendingStore();
        bool beforeCommit = true, closed = false;
        final client = MockClient((r) async {
          if (r.url.path.endsWith('/approve') && beforeCommit) {
            beforeCommit = false;
            throw http.ClientException('synthetic unknown');
          }
          if (closed && r.url.path.endsWith('/receipt')) {
            final p = f.previews[sourcePreviewID(1)]!,
                body = f.previewReceipt(p);
            final end = DateTime.parse(p['expiresAt'] as String);
            body['state'] = 'CLOSED_UNCONSUMED';
            body['observedAt'] = end.toIso8601String();
            body['validUntil'] = end
                .add(const Duration(seconds: 30))
                .toIso8601String();
            return candidateResponse(body);
          }
          if (closed && r.url.path.contains('/previews/')) {
            return http.Response('{}', 409);
          }
          return f.handle(r);
        });
        var c = multiController(f, client: client, store: store);
        await c.load();
        c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
        await c.prepareAnalysis(candidateMoment);
        await c.approve();
        if (reopened) {
          c.dispose();
          c = multiController(f, client: client, store: store);
          await c.load();
        }
        closed = true;
        final before = f.requests.length;
        await c.verifyUnknown();
        expect(c.unknown, isNull);
        expect(store.values, isEmpty);
        expect(c.analysisGrants, isEmpty);
        expect(c.preview, isNull);
        expect(c.retentionGrant, isNull);
        expect(c.notice, contains('当前没有批准记录'));
        expect(c.notice, isNot(contains('已回滚')));
        expect(
          f.requests.skip(before).every((r) => r['method'] == 'GET'),
          true,
        );
        c.dispose();
        client.close();
      },
    );
  }
  test(
    'OPEN source permission error cannot create original retry capability or clear the journal',
    () async {
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore();
      bool changed = false;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/approve')) {
          throw http.ClientException('synthetic unknown');
        }
        if (changed &&
            r.url.path.contains('/previews/') &&
            !r.url.path.endsWith('/receipt')) {
          return http.Response('{}', 403);
        }
        return f.handle(r);
      });
      final c = multiController(f, client: client, store: store);
      await c.load();
      c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await c.prepareAnalysis(candidateMoment);
      await c.approve();
      changed = true;
      await c.verifyUnknown();
      expect(c.error, isNotNull);
      expect(c.unknown, MultiCandidateUnknown.approval);
      expect(store.values.length, 1);
      expect(c.canApprove, false);
      final before = f.requests.length;
      await c.approve();
      await c.prepareMulti();
      c.edit(toggleMoment: candidateSaved);
      expect(f.requests.length, before);
      c.dispose();
      client.close();
    },
  );
  test(
    'reopened OPEN never recovers a retry even in the original Session',
    () async {
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore();
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/approve')) {
          throw http.ClientException('synthetic unknown');
        }
        return f.handle(r);
      });
      final old = multiController(f, client: client, store: store);
      await old.load();
      old.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await old.prepareAnalysis(candidateMoment);
      await old.approve();
      old.dispose();
      final c = multiController(f, client: client, store: store);
      await c.load();
      final before = f.requests.length;
      await c.verifyUnknown();
      expect(store.values.length, 1);
      expect(c.canApprove, false);
      expect(c.preview, isNull);
      await c.approve();
      expect(f.requests.skip(before).every((r) => r['method'] == 'GET'), true);
      c.dispose();
      client.close();
    },
  );

  test(
    'historical receipt closes same-page source-changed unknown without restoring a grant',
    () async {
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore();
      bool changed = false;
      final client = MockClient((r) async {
        if (changed && r.url.path.contains('/previews/')) {
          if (r.url.path.endsWith('/receipt')) {
            return candidateResponse(
              f.previewReceipt(f.previews[sourcePreviewID(1)]!),
            );
          }
          return http.Response('{}', 409);
        }
        return f.handle(r);
      });
      final c = multiController(f, client: client, store: store);
      await c.load();
      c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await c.prepareAnalysis(candidateMoment);
      f.loseApproval = true;
      await c.approve();
      changed = true;
      final before = f.requests.where((r) => r['method'] == 'POST').length;
      await c.verifyUnknown();
      expect(c.unknown, isNull);
      expect(store.values, isEmpty);
      expect(c.analysisGrants, isEmpty);
      expect(c.preview, isNull);
      expect(f.requests.where((r) => r['method'] == 'POST').length, before);
      c.dispose();
      client.close();
    },
  );
  test(
    'OPEN receipt retains same-page journal while allowing only explicit original retry',
    () async {
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore();
      bool loseBeforeCommit = true;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/approve') && loseBeforeCommit) {
          loseBeforeCommit = false;
          throw http.ClientException(
            'synthetic dispatch unknown before commit',
          );
        }
        return f.handle(r);
      });
      final c = multiController(f, client: client, store: store);
      await c.load();
      c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await c.prepareAnalysis(candidateMoment);
      final original = c.preview!;
      await c.approve();
      await c.verifyUnknown();
      expect(store.values.length, 1);
      expect(c.preview?.id, original.id);
      final before = f.requests.length;
      c.edit(toggleMoment: candidateSaved);
      await c.prepareAnalysis(candidateSaved);
      expect(f.requests.length, before);
      expect(c.preview?.id, original.id);
      await c.approve();
      expect(c.analysisGrants[candidateMoment]?.usable, true);
      expect(store.values, isEmpty);
      c.dispose();
      client.close();
    },
  );

  for (final phase in MultiCandidateUnknown.values) {
    test(
      'production default Secure controller reopens only original metadata with mocked platform storage',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final f = MultiWireFixture();
        AgentMultiCandidateController create() => AgentMultiCandidateController(
          api: f.api(),
          authorizationHeader: () => 'Bearer synthetic',
          accountID: () => candidateOwner,
          organizationWorkspaceID: () => null,
        );
        final c = create();
        expect(c.pendingStore, isA<SecureAgentMultiCandidatePendingStore>());
        await approveCombination(c);
        f.loseStage = true;
        await c.stage();
        c.dispose();
        final before = f.requests.length;
        final reopened = create();
        await reopened.load();
        expect(reopened.unknown, MultiCandidateUnknown.stage);
        await reopened.verifyUnknown();
        expect(reopened.retentionGrant, isNull);
        expect(reopened.preview, isNull);
        expect(reopened.analysisGrants, isEmpty);
        expect(
          f.requests.skip(before).every((r) => r['method'] == 'GET'),
          true,
        );
        reopened.dispose();
      },
    );
    for (final revoke in [false, true]) {
      test(
        'exact journal delete failure preserves same-page original GET retry revoke=$revoke',
        () async {
          final f = MultiWireFixture(),
              store = RecoveryProbeStore(),
              c = multiController(f, store: store);
          if (revoke) {
            await approveCombination(c);
            f.loseRevoke = true;
            await c.revoke(c.retentionGrant!);
          } else {
            await c.load();
            c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
            await c.prepareAnalysis(candidateMoment);
            f.loseApproval = true;
            await c.approve();
          }
          store.failDelete = true;
          final before = f.requests.length;
          await c.verifyUnknown();
          expect(c.unknown, isNotNull);
          expect(
            (await store.read('http://fixture', candidateOwner)).length,
            1,
          );
          if (!revoke) expect(c.preview, isNotNull);
          store.failDelete = false;
          await c.verifyUnknown();
          expect(c.unknown, isNull);
          expect(c.error, isNull);
          expect(
            f.requests.skip(before).every((r) => r['method'] == 'GET'),
            true,
          );
          expect(await store.read('http://fixture', candidateOwner), isEmpty);
          c.dispose();
        },
      );
    }
    test(
      'late GET after identity ABA cannot clear original journal or restore grant',
      () async {
        final f = MultiWireFixture(),
            store = MemoryAgentMultiCandidatePendingStore(),
            c = multiController(f, store: store);
        await approveCombination(c);
        f.loseStage = true;
        await c.stage();
        c.dispose();
        var token = 'Bearer A';
        final hold = Completer<http.Response>();
        final client = MockClient((r) => hold.future),
            r = multiController(
              f,
              store: store,
              client: client,
              auth: () => token,
            );
        await r.load();
        final check = r.verifyUnknown();
        await Future<void>.delayed(Duration.zero);
        token = 'Bearer B';
        r.synchronizeIdentity();
        token = 'Bearer A';
        r.synchronizeIdentity();
        hold.complete(candidateResponse(f.receipt()));
        await check;
        expect((await store.read('http://fixture', candidateOwner)).length, 1);
        expect(r.retentionGrant, isNull);
        expect(r.result, isNull);
        r.dispose();
        client.close();
      },
    );
    test('wrong-owner recovery HTTP response never clears pending', () async {
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore(),
          c = multiController(f, store: store);
      await approveCombination(c);
      f.loseStage = true;
      await c.stage();
      c.dispose();
      final client = MockClient(
        (r) async => candidateResponse({
          ...f.receipt(),
          'owner': {'type': 'PERSON', 'id': candidateAgent},
        }),
      );
      final r = multiController(f, store: store, client: client);
      await r.load();
      await r.verifyUnknown();
      expect(r.unknown, MultiCandidateUnknown.stage);
      expect(r.error, isNotNull);
      expect((await store.read('http://fixture', candidateOwner)).length, 1);
      r.dispose();
      client.close();
    });
    test(
      'reopen $phase reads original ID only and never restores a usable grant',
      () async {
        final f = MultiWireFixture(),
            store = MemoryAgentMultiCandidatePendingStore();
        final c = multiController(f, store: store);
        if (phase == MultiCandidateUnknown.approval) {
          await approveTwoSources(c);
          await c.prepareMulti();
          f.loseApproval = true;
          await c.approve();
        } else {
          await approveCombination(c);
          if (phase == MultiCandidateUnknown.stage) {
            f.loseStage = true;
            await c.stage();
          } else {
            f.loseRevoke = true;
            await c.revoke(c.retentionGrant!);
          }
        }
        expect(c.unknown, phase);
        final p = (await store.read('http://fixture', candidateOwner)).single;
        for (final private in [
          f.title,
          '想找附近的羽毛球活动',
          'Bearer synthetic',
          '整理我的羽毛球活动偏好',
          'sourceSelections',
          'content',
        ]) {
          expect(p.encoded.contains(private), false);
        }
        c.dispose();
        final before = f.requests.length;
        final reopened = multiController(
          f,
          store: store,
          auth: () => 'Bearer NEW SESSION',
        );
        await reopened.load();
        expect(reopened.unknown, phase);
        expect(f.requests.length, before);
        await reopened.verifyUnknown();
        expect(reopened.error, isNull);
        expect(reopened.unknown, isNull);
        expect(reopened.retentionGrant, isNull);
        expect(reopened.analysisGrants, isEmpty);
        expect(reopened.preview, isNull);
        expect(reopened.result, isNull);
        expect(reopened.canStage, false);
        await reopened.stage();
        await reopened.approve();
        expect(
          f.requests.skip(before).every((r) => r['method'] == 'GET'),
          true,
        );
        expect(await store.read('http://fixture', candidateOwner), isEmpty);
        reopened.dispose();
      },
    );
  }
  test(
    'reopen analysis approval uses original preview only without private content',
    () async {
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore(),
          c = multiController(
            f,
            store: MemoryAgentMultiCandidatePendingStore(),
          );
      c.dispose();
      final original = multiController(f, store: store);
      await original.load();
      original.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await original.prepareAnalysis(candidateMoment);
      f.loseApproval = true;
      await original.approve();
      original.dispose();
      final before = f.requests.length,
          reopened = multiController(f, store: store);
      await reopened.load();
      await reopened.verifyUnknown();
      expect(reopened.preview, isNull);
      expect(reopened.analysisGrants, isEmpty);
      expect(f.requests.skip(before).map((r) => r['path']), [
        '$analysisPurposePath/previews/${sourcePreviewID(1)}/receipt',
      ]);
      reopened.dispose();
    },
  );
  for (final failure in ['read', 'write', 'readback', 'corrupt']) {
    test(
      'secure $failure prevents mutations and never falls back to POST',
      () async {
        final f = MultiWireFixture(),
            store = RecoveryProbeStore(),
            c = multiController(f, store: store);
        await c.load();
        c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
        await c.prepareAnalysis(candidateMoment);
        final before = f.requests.length;
        if (failure == 'read') {
          store.failRead = true;
          c.synchronizeIdentity();
        }
        if (failure == 'write') store.failWrite = true;
        if (failure == 'readback') store.wrongReadback = true;
        if (failure == 'corrupt') {
          // Corruption in this owner namespace must block the atomic reserve.
          final probe = pendingMulti(phase: 'approval');
          await store.write('http://fixture', candidateOwner, probe);
          store.values[store.values.keys.single] = '{"query":"private-canary"}';
        }
        await c.approve();
        await c.prepareAnalysis(candidateSaved);
        await c.prepareMulti();
        await c.stage();
        expect(
          f.requests.skip(before).where((r) => r['method'] != 'GET'),
          isEmpty,
        );
        expect(c.recoveryBlocked, true);
        expect(c.error, isNotNull);
        c.dispose();
      },
    );
  }
  test(
    'two already prepared controllers atomic reserve blocks a different source approval',
    () async {
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore();
      final a = multiController(f, store: store),
          b = multiController(f, store: store);
      for (final c in [a, b]) {
        await c.load();
      }
      a.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      b.edit(nextTask: multiTask, toggleMoment: candidateSaved);
      await a.prepareAnalysis(candidateMoment);
      await b.prepareAnalysis(candidateSaved);
      f.loseApproval = true;
      await a.approve();
      final before = f.requests.length;
      await b.approve();
      expect(f.requests.length, before);
      expect(b.recoveryBlocked, true);
      expect(
        (await store.read('http://fixture', candidateOwner)).single.previewID,
        sourcePreviewID(1),
      );
      a.dispose();
      b.dispose();
    },
  );
  test(
    'storage wait across original expiry enters while valid and sends no approval',
    () async {
      final f = MultiWireFixture(),
          store = RecoveryProbeStore()..pauseWrite = Completer<void>();
      final client = MockClient((r) async {
        final response = await f.handle(r);
        if (r.url.path == '$analysisPurposePath/previews') {
          final envelope = jsonDecode(response.body) as Map<String, dynamic>;
          envelope['data']['expiresAt'] = DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 1))
              .toIso8601String();
          return candidateResponse(envelope['data']);
        }
        return response;
      });
      final c = multiController(f, store: store, client: client);
      await c.load();
      c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await c.prepareAnalysis(candidateMoment);
      expect(c.preview!.current, true);
      final before = f.requests.length;
      final write = c.approve();
      await store.writeEntered.future;
      expect(c.preview!.current, true);
      await Future<void>.delayed(const Duration(milliseconds: 1100));
      store.pauseWrite!.complete();
      await write;
      expect(f.requests.length, before);
      expect(c.unknown, MultiCandidateUnknown.approval);
      expect((await store.read('http://fixture', candidateOwner)).length, 1);
      c.dispose();
      client.close();
    },
  );
  test(
    'storage wait token ABA preserves original journal and sends no approval',
    () async {
      var token = 'Bearer A';
      final f = MultiWireFixture(),
          store = RecoveryProbeStore()..pauseWrite = Completer<void>();
      final c = multiController(f, store: store, auth: () => token);
      await c.load();
      c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await c.prepareAnalysis(candidateMoment);
      final before = f.requests.length, pending = c.approve();
      await store.writeEntered.future;
      token = 'Bearer B';
      c.synchronizeIdentity();
      token = 'Bearer A';
      c.synchronizeIdentity();
      store.pauseWrite!.complete();
      await pending;
      expect(f.requests.length, before);
      expect((await store.read('http://fixture', candidateOwner)).length, 1);
      expect(c.preview, isNull);
      c.dispose();
    },
  );
  test(
    'reopen NOT_STAGED before deadline remains blocked and never replays',
    () async {
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore(),
          c = multiController(f, store: store);
      await approveCombination(c);
      f.loseStage = true;
      await c.stage();
      c.dispose();
      f.staged = false;
      final r = multiController(f, store: store);
      await r.load();
      await r.verifyUnknown();
      expect(r.unknown, MultiCandidateUnknown.stage);
      expect(r.editable, false);
      final before = f.requests.length;
      await r.stage();
      await r.prepareMulti();
      r.edit(nextTask: multiTask);
      expect(f.requests.length, before);
      expect(r.retentionGrant, isNull);
      r.dispose();
    },
  );
  test(
    'expired denied original preview preserves UNKNOWN and never claims rollback',
    () async {
      final f = MultiWireFixture(),
          store = MemoryAgentMultiCandidatePendingStore(),
          c = multiController(f, store: store);
      await approveTwoSources(c);
      await c.prepareMulti();
      f.loseApproval = true;
      await c.approve();
      c.dispose();
      final client = MockClient(
        (r) async => r.url.path.contains('/previews/')
            ? http.Response('{}', 409)
            : f.handle(r),
      );
      final r = multiController(f, store: store, client: client);
      await r.load();
      await r.verifyUnknown();
      expect(r.unknown, MultiCandidateUnknown.approval);
      expect(r.error, isNotNull);
      expect(r.editable, false);
      expect((await store.read('http://fixture', candidateOwner)).length, 1);
      r.dispose();
      client.close();
    },
  );
  test(
    'unknown stage survives disposal and reopening without another POST',
    () async {
      final f = MultiWireFixture();
      final store = MemoryAgentMultiCandidatePendingStore();
      final c = multiController(f, store: store);
      await approveCombination(c);
      f.loseStage = true;
      await c.stage();
      expect(c.unknown, MultiCandidateUnknown.stage);
      c.dispose();
      final reopened = multiController(f, store: store);
      await reopened.load();
      expect(reopened.unknown, MultiCandidateUnknown.stage);
      reopened.dispose();
    },
  );

  test(
    'native follow-up task remains usable for concrete source approvals',
    () async {
      final f = MultiWireFixture()..currentQuery = '近一点的周末羽毛球呢？';
      final c = multiController(f);
      await approveCombination(c);
      expect(c.error, isNull);
      expect(c.canStage, true);
      expect(c.tasks.single.query, f.currentQuery);
      expect(c.analysisGrants.length, 2);
      await c.stage();
      expect(c.result?.staged, true);
      expect(c.result?.candidate?.status, 'CANDIDATE');
      c.dispose();
    },
  );
  test(
    'real route sequence stages only a candidate then leaves human Memory decision separate',
    () async {
      final f = MultiWireFixture(), c = multiController(f);
      await approveCombination(c);
      expect(c.canStage, true);
      expect(c.result, isNull);
      await c.stage();
      await c.stage();
      expect(c.result?.staged, true);
      expect(c.result?.candidate?.status, 'CANDIDATE');
      expect(
        f.requests
            .where((r) => r['path'] == '$multiCandidatePath/stage')
            .length,
        1,
      );
      expect(
        f.requests.where(
          (r) => (r['path'] as String).contains('agent-memory-candidates'),
        ),
        isEmpty,
      );
      expect(
        f.requests
            .where((r) => (r['path'] as String).endsWith('/approve'))
            .every((r) => r['body'] == ''),
        true,
      );
      c.dispose();
    },
  );
  test(
    'default only title is authorized and body requires an explicit choice',
    () async {
      final f = MultiWireFixture(), c = multiController(f);
      await c.load();
      c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await c.prepareAnalysis(candidateMoment);
      expect(c.preview!.selection['fields'], ['title']);
      c.edit(toggleBody: candidateMoment);
      expect(c.preview, isNull);
      await c.prepareAnalysis(candidateMoment);
      expect(c.preview!.selection['fields'], ['body', 'title']);
      expect(c.preview!.review!['content']['body'], f.moment(1)['body']);
      c.dispose();
    },
  );
  test(
    'source edit invalidates its old analysis approval and combo preview',
    () async {
      final f = MultiWireFixture(), c = multiController(f);
      await approveTwoSources(c);
      await c.prepareMulti();
      c.edit(toggleBody: candidateMoment);
      expect(c.preview, isNull);
      expect(c.analysisGrants[candidateMoment], isNull);
      expect(c.canPrepareMulti, false);
      expect(c.analysisGrants[candidateSaved]?.usable, true);
      c.dispose();
    },
  );
  test(
    'refresh retires concrete versions without silently extending or revoking grants',
    () async {
      final f = MultiWireFixture(), c = multiController(f);
      await approveCombination(c);
      await c.load();
      expect(c.retentionGrant, isNull);
      expect(c.analysisGrants, isEmpty);
      expect(c.selected, isEmpty);
      expect(f.requests.where((r) => r['method'] == 'DELETE'), isEmpty);
      c.dispose();
    },
  );
  test(
    'lost analysis approval reads recorded metadata by GET without restoring a grant',
    () async {
      final f = MultiWireFixture(), c = multiController(f);
      await c.load();
      c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await c.prepareAnalysis(candidateMoment);
      f.loseApproval = true;
      await c.approve();
      expect(c.unknown, MultiCandidateUnknown.approval);
      final before = f.requests.length;
      c.edit(toggleMoment: candidateSaved);
      await c.approve();
      expect(f.requests.length, before);
      expect(c.selected.containsKey(candidateSaved), false);
      await c.verifyUnknown();
      expect(c.unknown, isNull);
      expect(c.analysisGrants, isEmpty);
      expect(c.preview, isNull);
      expect(f.requests.skip(before).every((r) => r['method'] == 'GET'), true);
      expect(
        f.requests
            .where((r) => (r['path'] as String).endsWith('/approve'))
            .length,
        1,
      );
      c.dispose();
    },
  );
  test(
    'lost combo approval reconciles original consumed permit without new sources',
    () async {
      final f = MultiWireFixture(), c = multiController(f);
      await approveTwoSources(c);
      await c.prepareMulti();
      f.loseApproval = true;
      await c.approve();
      expect(c.unknown, MultiCandidateUnknown.approval);
      final before = f.requests.length;
      await c.verifyUnknown();
      expect(c.retentionGrant, isNull);
      expect(c.preview, isNull);
      expect(f.requests.skip(before).every((r) => r['method'] == 'GET'), true);
      expect(c.result, isNull);
      c.dispose();
    },
  );
  test(
    'lost stage result reconciles original grant receipt and never repeats POST',
    () async {
      final f = MultiWireFixture(), c = multiController(f);
      await approveCombination(c);
      f.loseStage = true;
      await c.stage();
      expect(c.unknown, MultiCandidateUnknown.stage);
      await c.stage();
      await c.verifyUnknown();
      expect(c.result?.staged, true);
      expect(c.unknown, isNull);
      await c.stage();
      expect(
        f.requests
            .where((r) => r['path'] == '$multiCandidatePath/stage')
            .length,
        1,
      );
      expect(
        f.requests.last,
        containsPair(
          'path',
          '$multiCandidatePath/grants/$multiGrantID/receipt',
        ),
      );
      c.dispose();
    },
  );
  test(
    'lost revoke reconciles versioned original permit and disables stage',
    () async {
      final f = MultiWireFixture(), c = multiController(f);
      await approveCombination(c);
      f.loseRevoke = true;
      await c.revoke(c.retentionGrant!);
      expect(c.unknown, MultiCandidateUnknown.revocation);
      await c.verifyUnknown();
      expect(c.retentionGrant!.revoked, true);
      expect(c.canStage, false);
      expect(f.requests.where((r) => r['method'] == 'DELETE').length, 1);
      expect(
        jsonDecode(
          f.requests.where((r) => r['method'] == 'DELETE').single['body']
              as String,
        ),
        {'expectedRevision': 1},
      );
      c.dispose();
    },
  );
  test(
    'personal OrgA personal ABA cannot restore an old source or preview',
    () async {
      String? workspace;
      final f = MultiWireFixture(),
          c = multiController(f, workspace: () => workspace);
      await approveCombination(c);
      workspace = 'orgA';
      c.synchronizeIdentity();
      workspace = null;
      c.synchronizeIdentity();
      expect(c.retentionGrant, isNull);
      expect(c.analysisGrants, isEmpty);
      expect(c.selected, isEmpty);
      final count = f.requests.length;
      await c.stage();
      expect(f.requests.length, count);
      c.dispose();
    },
  );
  test(
    'session token ABA clears pending approval even for the same account',
    () async {
      var token = 'Bearer A';
      final f = MultiWireFixture(), c = multiController(f, auth: () => token);
      await approveTwoSources(c);
      await c.prepareMulti();
      token = 'Bearer B';
      c.synchronizeIdentity();
      token = 'Bearer A';
      c.synchronizeIdentity();
      expect(c.preview, isNull);
      expect(c.analysisGrants, isEmpty);
      c.dispose();
    },
  );
  test(
    'late source preview cannot populate a newer personal identity',
    () async {
      final f = MultiWireFixture(), hold = Completer<http.Response>();
      var token = 'Bearer A';
      final client = MockClient(
        (r) async => r.method == 'POST' ? hold.future : f.handle(r),
      );
      final c = multiController(f, client: client, auth: () => token);
      await c.load();
      c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      final pending = c.prepareAnalysis(candidateMoment);
      await Future<void>.delayed(Duration.zero);
      final request = f.analysisSelection(1);
      token = 'Bearer B';
      c.synchronizeIdentity();
      hold.complete(candidateResponse(f.analysisPreview(1, request)));
      await pending;
      expect(c.preview, isNull);
      expect(c.tasks, isEmpty);
      expect(c.error, isNull);
      c.dispose();
      client.close();
    },
  );
  test('concurrent approve clicks result in one POST', () async {
    final f = MultiWireFixture(), gate = Completer<void>();
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/approve')) await gate.future;
      return f.handle(r);
    });
    final c = multiController(f, client: client);
    await c.load();
    c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
    await c.prepareAnalysis(candidateMoment);
    final first = c.approve();
    await c.approve();
    gate.complete();
    await first;
    expect(
      f.requests
          .where((r) => (r['path'] as String).endsWith('/approve'))
          .length,
      1,
    );
    c.dispose();
    client.close();
  });
  test(
    'default OFF unavailable preserves ordinary manual candidate path and makes no fallback write',
    () async {
      final f = MultiWireFixture()..rejectPreview = true,
          c = multiController(f);
      await c.load();
      c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
      await c.prepareAnalysis(candidateMoment);
      expect(c.preview, isNull);
      expect(c.error, isNotNull);
      expect(c.unknown, isNull);
      expect(c.selected.containsKey(candidateMoment), true);
      expect(
        f.requests.where(
          (r) =>
              (r['path'] as String).endsWith('/stage') ||
              (r['path'] as String).endsWith('/approve'),
        ),
        isEmpty,
      );
      c.dispose();
    },
  );
  test('dispose clears sensitive review and ignores delayed reply', () async {
    final f = MultiWireFixture(), hold = Completer<http.Response>();
    final client = MockClient(
      (r) async => r.method == 'POST' ? hold.future : f.handle(r),
    );
    final c = multiController(f, client: client);
    await c.load();
    c.edit(nextTask: multiTask, toggleMoment: candidateMoment);
    final pending = c.prepareAnalysis(candidateMoment);
    await Future<void>.delayed(Duration.zero);
    c.dispose();
    hold.complete(candidateResponse(f.analysisPreview(1)));
    await pending;
    expect(c.preview, isNull);
    expect(c.moments, isEmpty);
    client.close();
  });
}
