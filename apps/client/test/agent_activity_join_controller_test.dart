import 'dart:async';

import 'package:birdtie_client/src/workspace/agent_activity_join_api.dart';
import 'package:birdtie_client/src/workspace/agent_activity_join_controller.dart';
import 'package:birdtie_client/src/workspace/agent_activity_join_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_activity_join_api_test.dart';
import 'agent_activity_join_pending_store_test.dart' show joinPending;

class JoinHarness {
  JoinHarness({AgentActivityJoinPendingStore? pendingStore})
    : store = pendingStore ?? MemoryAgentActivityJoinPendingStore() {
    client = MockClient((r) async {
      requests.add(r);
      if (intercept != null) {
        final response = await intercept!(r);
        if (response != null) return response;
      }
      if (r.url.path.endsWith('/previews')) {
        original = joinPreviewForRequest(r);
        return joinResponse(original, status: 201);
      }
      if (r.url.path.endsWith('/confirm'))
        return joinResponse({...original!, 'state': agentActivityJoinApproved});
      if (r.url.path.endsWith('/revoke'))
        return joinResponse({
          ...original ?? joinPreviewWire(),
          'state': 'CANCELLED',
          'review': null,
          'dispatchCommitted': revokeCommitted,
        });
      return joinResponse(joinReceiptWire(status: reconcileStatus));
    });
  }
  late final MockClient client;
  final AgentActivityJoinPendingStore store;
  final requests = <http.Request>[];
  Future<http.Response?> Function(http.Request)? intercept;
  Map<String, dynamic>? original;
  bool source = true, revokeCommitted = false;
  String? token = 'Bearer fixture', owner = joinOwner, organization;
  DateTime now = joinAt;
  Duration elapsed = Duration.zero;
  String reconcileStatus = 'SUCCEEDED';
  int calls(String suffix) =>
      requests.where((r) => r.url.path.endsWith('/$suffix')).length;
  AgentActivityJoinController controller({bool recoveryOnly = false}) =>
      AgentActivityJoinController(
        authorizationHeader: () => token,
        ownerID: () => owner,
        organizationWorkspaceID: () => organization,
        sourceCurrent: () => source,
        api: AgentActivityJoinAPI(client: client, apiBaseUrl: 'http://local'),
        selection: recoveryOnly ? null : joinSelection(),
        pendingStore: store,
        now: () => now,
        elapsed: () => elapsed,
      );
  void close() => client.close();
}

class ReadbackFailJoinStore extends MemoryAgentActivityJoinPendingStore {
  bool failAfterReserve = true, reserved = false;
  @override
  Future<bool> reserve(String e, String o, PendingAgentActivityJoin p) async {
    final created = await super.reserve(e, o, p);
    reserved = true;
    return created;
  }

  @override
  Future<PendingAgentActivityJoin?> read(String e, String o) async {
    if (reserved && failAfterReserve)
      throw StateError('synthetic storage reply lost');
    return super.read(e, o);
  }
}

class WaitJoinStore extends MemoryAgentActivityJoinPendingStore {
  final entered = Completer<void>(), release = Completer<void>();
  @override
  Future<bool> reserve(String e, String o, PendingAgentActivityJoin p) async {
    entered.complete();
    await release.future;
    return super.reserve(e, o, p);
  }
}

class ClearFailJoinStore extends MemoryAgentActivityJoinPendingStore {
  bool fail = true;
  @override
  Future<void> delete(String e, String o, PendingAgentActivityJoin p) async {
    if (fail) throw StateError('synthetic secure clear failure');
    return super.delete(e, o, p);
  }
}

void main() {
  test(
    'explicit confirmed acknowledged fresh action uses original approval once and clears exact journal after native receipt',
    () async {
      final h = JoinHarness(), c = h.controller();
      await c.initialize();
      await c.prepare();
      expect(c.canConfirm, true);
      expect(h.calls('execute'), 0);
      await c.confirmAndExecute();
      await c.confirmAndExecute();
      expect(h.calls('confirm'), 1);
      expect(h.calls('execute'), 1);
      expect(c.receipt!.status, 'SUCCEEDED');
      expect(c.hasPending, false);
      expect(c.canPrepare, false);
      expect(c.canConfirm, false);
      expect(await h.store.read('http://local', joinOwner), null);
      c.dispose();
      h.close();
    },
  );
  test(
    'unknown confirm never proceeds to execute and original uncommitted stop clears only its approval',
    () async {
      final h = JoinHarness(), c = h.controller();
      h.intercept = (r) async {
        if (r.url.path.endsWith('/confirm'))
          throw http.ClientException('synthetic acknowledgement lost');
        return null;
      };
      await c.initialize();
      await c.prepare();
      await c.confirmAndExecute();
      expect(c.unknown, true);
      expect(c.pending!.phase, 'CONFIRM');
      expect(c.preview, null);
      expect(h.calls('execute'), 0);
      await c.confirmAndExecute();
      expect(h.calls('confirm'), 1);
      await c.stop();
      expect(h.calls('revoke'), 1);
      expect(h.calls('reconcile'), 0);
      expect(c.hasPending, false);
      expect(c.message, contains('未由这次批准提交'));
      c.dispose();
      h.close();
    },
  );
  test(
    'lost execute reply and even final 409 remain original UNKNOWN, only reconcile can produce causal success',
    () async {
      for (final status in [503, 409, 401]) {
        final h = JoinHarness(), c = h.controller();
        h.intercept = (r) async => r.url.path.endsWith('/execute')
            ? http.Response('{"error":"agent_activity_join_unknown"}', status)
            : null;
        await c.initialize();
        await c.prepare();
        await c.confirmAndExecute();
        expect(c.unknown, true);
        expect(c.pending!.phase, 'EXECUTE');
        expect(c.receipt, null);
        await c.confirmAndExecute();
        expect(h.calls('execute'), 1);
        await c.reconcile();
        expect(c.receipt!.approvalID, joinApproval);
        expect(c.receipt!.participationID, joinParticipation);
        expect(c.hasPending, false);
        expect(h.calls('execute'), 1);
        expect(h.calls('reconcile'), 1);
        c.dispose();
        h.close();
      }
    },
  );
  test(
    'cold selector journal restores no content or capability, and no-dispatch reconcile remains unknown until explicit fenced stop',
    () async {
      final h = JoinHarness();
      await h.store.reserve(
        'http://local',
        joinOwner,
        joinPending(phase: 'CONFIRM'),
      );
      h.intercept = (r) async => r.url.path.endsWith('/reconcile')
          ? http.Response('{"error":"agent_activity_join_unknown"}', 503)
          : null;
      final c = h.controller(recoveryOnly: true);
      await c.initialize();
      expect(c.hasPending, true);
      expect(c.preview, null);
      expect(h.requests, isEmpty);
      expect(c.canConfirm, false);
      expect(c.canPrepare, false);
      await c.reconcile();
      expect(c.hasPending, true);
      expect(c.unknown, true);
      expect(
        (await h.store.read('http://local', joinOwner))!.approvalID,
        joinApproval,
      );
      await c.stop();
      expect(c.hasPending, false);
      expect(h.calls('previews'), 0);
      expect(h.calls('confirm'), 0);
      expect(h.calls('execute'), 0);
      c.dispose();
      h.close();
    },
  );
  test(
    'committed revoke must reconcile independent original receipt and does not claim participation cancelled',
    () async {
      final h = JoinHarness();
      await h.store.reserve('http://local', joinOwner, joinPending());
      h.revokeCommitted = true;
      final c = h.controller(recoveryOnly: true);
      await c.initialize();
      await c.stop();
      expect(h.calls('revoke'), 1);
      expect(h.calls('reconcile'), 1);
      expect(c.receipt!.status, 'SUCCEEDED');
      expect(c.hasPending, false);
      expect(c.message, contains('查看当前状态'));
      expect(c.message, isNot(contains('已取消报名')));
      c.dispose();
      h.close();
    },
  );
  test(
    'source and task ABA permanently retire exact approval, stale preparation reply cannot restore it',
    () async {
      final h = JoinHarness(), c = h.controller();
      await c.initialize();
      await c.prepare();
      h.source = false;
      c.identityChanged();
      h.source = true;
      c.identityChanged();
      expect(c.preview, null);
      expect(c.canConfirm, false);
      expect(c.canPrepare, false);
      await c.confirmAndExecute();
      expect(h.calls('confirm'), 0);
      c.dispose();
      h.close();
      final delayed = JoinHarness(), late = delayed.controller();
      final entered = Completer<void>(), response = Completer<http.Response>();
      delayed.intercept = (r) async {
        if (r.url.path.endsWith('/previews')) {
          entered.complete();
          return response.future;
        }
        return null;
      };
      await late.initialize();
      final pending = late.prepare();
      await entered.future;
      delayed.source = false;
      late.identityChanged();
      delayed.source = true;
      late.identityChanged();
      response.complete(
        joinResponse(joinPreviewForRequest(delayed.requests.last), status: 201),
      );
      await pending;
      expect(late.preview, null);
      expect(late.canConfirm, false);
      expect(late.canPrepare, false);
      late.dispose();
      delayed.close();
    },
  );
  test(
    'owner Session and organization ABA clear old visible facts and preserve only original owner journal',
    () async {
      for (final boundary in ['owner', 'token', 'organization']) {
        final h = JoinHarness(), c = h.controller();
        h.intercept = (r) async =>
            r.url.path.endsWith('/execute') ? http.Response('{}', 503) : null;
        await c.initialize();
        await c.prepare();
        await c.confirmAndExecute();
        if (boundary == 'owner') {
          h.owner = joinOther;
          c.identityChanged();
          h.owner = joinOwner;
        }
        if (boundary == 'token') {
          h.token = 'Bearer another fixture';
          c.identityChanged();
          h.token = 'Bearer fixture';
        }
        if (boundary == 'organization') {
          h.organization = joinOther;
          c.identityChanged();
          h.organization = null;
        }
        c.identityChanged();
        expect(c.identityRetired, true);
        expect(c.preview, null);
        expect(c.receipt, null);
        expect(c.pending, null);
        expect(c.canConfirm, false);
        expect(c.canRecover, false);
        expect(
          (await h.store.read('http://local', joinOwner))!.approvalID,
          joinApproval,
        );
        expect(await h.store.read('http://local', joinOther), null);
        await c.reconcile();
        expect(h.calls('reconcile'), 0);
        c.dispose();
        h.close();
      }
    },
  );
  test(
    'monotonic deadline prevents wall-clock rollback and late native preview extending thirty-second approval',
    () async {
      final h = JoinHarness(), c = h.controller();
      await c.initialize();
      await c.prepare();
      h.now = joinAt.subtract(const Duration(days: 1));
      h.elapsed = const Duration(seconds: 31);
      c.expireReview();
      expect(c.preview, null);
      expect(c.canConfirm, false);
      await c.confirmAndExecute();
      expect(h.calls('confirm'), 0);
      c.dispose();
      h.close();
      final slow = JoinHarness(), late = slow.controller();
      slow.intercept = (r) async {
        if (r.url.path.endsWith('/previews')) {
          slow.elapsed = const Duration(seconds: 31);
          return joinResponse(joinPreviewForRequest(r), status: 201);
        }
        return null;
      };
      await late.initialize();
      await late.prepare();
      expect(late.canConfirm, false);
      expect(late.preview, null);
      late.dispose();
      slow.close();
    },
  );
  test(
    'source retirement during secure reservation preserves selectors but sends zero confirm or execute',
    () async {
      final store = WaitJoinStore(),
          h = JoinHarness(pendingStore: store),
          c = h.controller();
      await c.initialize();
      await c.prepare();
      final action = c.confirmAndExecute();
      await store.entered.future;
      h.source = false;
      c.identityChanged();
      h.source = true;
      c.identityChanged();
      store.release.complete();
      await action;
      expect(h.calls('confirm'), 0);
      expect(h.calls('execute'), 0);
      expect(c.preview, null);
      expect((await store.read('http://local', joinOwner))!.phase, 'CONFIRM');
      expect(c.hasPending, true);
      expect(c.canRecover, true);
      c.dispose();
      h.close();
    },
  );
  test(
    'secure readback failure never confirms and remains recoverable by the original selector',
    () async {
      final store = ReadbackFailJoinStore(),
          h = JoinHarness(pendingStore: store),
          c = h.controller();
      await c.initialize();
      await c.prepare();
      await c.confirmAndExecute();
      expect(c.unknown, true);
      expect(h.calls('confirm'), 0);
      expect(h.calls('execute'), 0);
      store.failAfterReserve = false;
      expect((await store.read('http://local', joinOwner))!.phase, 'CONFIRM');
      await c.stop();
      expect(c.hasPending, false);
      expect(h.calls('execute'), 0);
      c.dispose();
      h.close();
    },
  );
  test(
    'late execute acknowledgement after source retirement cannot restore old result or resend',
    () async {
      final h = JoinHarness(), c = h.controller();
      final entered = Completer<void>(), response = Completer<http.Response>();
      h.intercept = (r) async {
        if (r.url.path.endsWith('/execute')) {
          entered.complete();
          return response.future;
        }
        return null;
      };
      await c.initialize();
      await c.prepare();
      final action = c.confirmAndExecute();
      await entered.future;
      h.source = false;
      c.identityChanged();
      h.source = true;
      c.identityChanged();
      response.complete(joinResponse(joinReceiptWire()));
      await action;
      expect(c.preview, null);
      expect(c.receipt, null);
      expect(c.hasPending, true);
      expect(c.canConfirm, false);
      expect(h.calls('execute'), 1);
      await c.reconcile();
      expect(c.receipt!.status, 'SUCCEEDED');
      expect(c.hasPending, false);
      expect(h.calls('execute'), 1);
      c.dispose();
      h.close();
    },
  );
  test(
    'competing prepared controller adopts retained original selector and never confirms or executes its second approval',
    () async {
      final h = JoinHarness(), first = h.controller(), second = h.controller();
      await first.initialize();
      await first.prepare();
      await second.initialize();
      await second.prepare();
      final retained = joinPending(phase: 'CONFIRM');
      await h.store.reserve('http://local', joinOwner, retained);
      await second.confirmAndExecute();
      expect(second.pending!.same(retained), true);
      expect(second.canConfirm, false);
      expect(h.calls('confirm'), 0);
      expect(h.calls('execute'), 0);
      first.dispose();
      second.dispose();
      h.close();
    },
  );
  test(
    'confirmed receipt with failed local clear cannot permit duplicate action; explicit reread clears exact history',
    () async {
      final store = ClearFailJoinStore(),
          h = JoinHarness(pendingStore: store),
          c = h.controller();
      await c.initialize();
      await c.prepare();
      await c.confirmAndExecute();
      expect(c.receipt!.status, 'SUCCEEDED');
      expect(c.hasPending, true);
      expect(c.canConfirm, false);
      expect(c.canPrepare, false);
      store.fail = false;
      await c.reconcile();
      expect(c.hasPending, false);
      expect(h.calls('execute'), 1);
      expect(h.calls('reconcile'), 1);
      c.dispose();
      h.close();
    },
  );
  test(
    'default unavailable and unauthorized response cannot fallback into any registration or payment',
    () async {
      final h = JoinHarness(), c = h.controller();
      h.intercept = (_) async =>
          http.Response('{"error":"agent_activity_join_unavailable"}', 503);
      await c.initialize();
      await c.prepare();
      expect(c.preview, null);
      expect(c.canConfirm, false);
      expect(c.receipt, null);
      expect(h.calls('previews'), 1);
      expect(h.calls('confirm'), 0);
      expect(h.calls('execute'), 0);
      expect(
        h.requests.any(
          (r) => !r.url.path.startsWith('/v1/me/agent-activity-join/'),
        ),
        false,
      );
      c.dispose();
      h.close();
    },
  );
}
