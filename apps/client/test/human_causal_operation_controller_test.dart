import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:birdtie_client/src/workspace/human_causal_pending_store.dart';
import 'package:birdtie_client/src/workspace/human_causal_operation_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'human_causal_control_test.dart';
import 'human_causal_pending_store_test.dart'
    show humanBlockInput, humanRemember;

class HumanFaultStore extends MemoryHumanCausalPendingStore {
  int reads = 0, writes = 0;
  int? failRead;
  Completer<void>? holdWrite, entered;
  @override
  Future<List<HumanCausalPending>> read(String o, String p) async {
    reads++;
    if (reads == failRead) throw StateError('injected readback failure');
    return super.read(o, p);
  }

  @override
  Future<void> write(String o, String p, HumanCausalPending v) async {
    writes++;
    final gate = holdWrite;
    holdWrite = null;
    if (gate != null) {
      entered?.complete();
      await gate.future;
    }
    await super.write(o, p, v);
  }
}

class HumanPromotionStore extends MemoryHumanCausalPendingStore {
  final entered = Completer<void>(), resume = Completer<void>();
  @override
  Future<void> write(String origin, String owner, HumanCausalPending p) async {
    if (p.stage == 'UNKNOWN') {
      entered.complete();
      await resume.future;
    }
    await super.write(origin, owner, p);
  }
}

HumanCausalOperationController humanController(
  HumanControlFixture f,
  HumanCausalPendingStore store, {
  HumanCausalFamily family = HumanCausalFamily.block,
  ValueNotifier<int>? changes,
  String? Function()? owner,
  String? Function()? header,
  Object? Function()? target,
  bool Function()? current,
}) => HumanCausalOperationController(
  api: HumanCausalApi(
    client: f.client,
    baseUrl: humanBase,
    authorizationHeader: header ?? () => 'Bearer original',
  ),
  family: family,
  organizationID: family == HumanCausalFamily.faq ? humanOrg : '',
  targetID:
      const {
        HumanCausalFamily.block,
        HumanCausalFamily.grantRevoke,
      }.contains(family)
      ? humanTarget
      : '',
  ownerID: owner ?? () => humanOwner,
  identityChanges: changes,
  targetKey: target,
  sourceCurrent: current,
  pendingStore: store,
);
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'another page repairs PREPARED before promotion; original controller cannot PUT',
    () async {
      final f = HumanControlFixture(),
          store = HumanPromotionStore(),
          a = humanController(f, store),
          b = humanController(f, store);
      addTearDown(a.dispose);
      addTearDown(b.dispose);
      addTearDown(f.client.close);
      await a.initialize();
      final p = (await a.prepare(humanBlockInput))!;
      final submitting = a.submit(p);
      await store.entered.future;
      await b.initialize();
      expect(b.locallyPrepared, true);
      await b.repairPrepared();
      expect(await store.read(humanBase, humanOwner), isEmpty);
      store.resume.complete();
      await submitting;
      expect(f.writes, isEmpty);
      expect(a.error, contains('尚未发送'));
      expect(a.pending, isNull);
    },
  );
  for (final family in HumanCausalFamily.values)
    test(
      '${family.wire} one preview, one original PUT, authoritative receipt before current read denial',
      () async {
        final f = HumanControlFixture()..denyAfterWrite = true,
            store = MemoryHumanCausalPendingStore(),
            c = humanController(f, store, family: family);
        addTearDown(c.dispose);
        addTearDown(f.client.close);
        await c.initialize();
        expect(c.canStart, true);
        final HumanCausalInput i = switch (family) {
          HumanCausalFamily.faq => const HumanFAQInput(
            organizationID: humanOrg,
            operationID: humanOp,
            action: 'CREATE',
            expectedVersion: 0,
            question: '如何报名？',
            answer: '请在活动详情核对报名。',
            published: false,
          ),
          HumanCausalFamily.context => const HumanContextInput(
            operationID: humanOp,
            action: 'DECLARE',
            expectedVersion: 0,
            relation: 'current',
            contextType: 'CITY',
            sourceKey: 'aberdeen-gb',
          ),
          HumanCausalFamily.report => const HumanReportInput(
            operationID: humanOp,
            targetType: 'general',
            targetID: '',
            reason: 'safety',
            details: '这是真实问题的足够长说明。',
          ),
          HumanCausalFamily.grantRevoke => const HumanGrantInput(
            operationID: humanOp,
            grantID: humanTarget,
            expectedVersion: 4,
          ),
          HumanCausalFamily.block => humanBlockInput,
        };
        final p = await c.prepare(i);
        expect(p, isNotNull);
        final result = await c.submit(p!);
        expect(result?.status, 'SUCCEEDED');
        expect(c.receipt?.status, 'SUCCEEDED');
        expect(c.pending, isNull);
        expect(c.snapshot, isNull);
        expect(c.canStart, false);
        expect(await store.read(humanBase, humanOwner), isEmpty);
        expect(f.writes.length, 1);
        expect(
          f.calls
              .where(
                (r) => r.method == 'POST' && r.url.path.endsWith('/previews'),
              )
              .length,
          1,
        );
      },
    );
  test(
    '401/404 UNKNOWN never resubmits, explicit bodyless stop certifies late zero effect',
    () async {
      final f = HumanControlFixture()
            ..putStatus = 401
            ..receipt404 = true,
          store = MemoryHumanCausalPendingStore(),
          c = humanController(f, store);
      addTearDown(c.dispose);
      addTearDown(f.client.close);
      await c.initialize();
      final p = (await c.prepare(humanBlockInput))!;
      expect(await c.submit(p), isNull);
      expect(c.pending?.stage, 'UNKNOWN');
      await c.recover();
      await c.recover();
      expect(f.writes.length, 1);
      f.receipts.clear();
      final r = await c.recover(stop: true);
      expect(r?.status, 'STOPPED');
      expect(c.pending, isNull);
      expect(
        f.calls.lastWhere((r) => r.url.path.endsWith('/stop')).bodyBytes,
        isEmpty,
      );
      expect(f.writes.length, 1);
    },
  );
  test(
    'restart restores metadata, historical success after lost target/source is final',
    () async {
      final f = HumanControlFixture()..putStatus = 503,
          store = MemoryHumanCausalPendingStore();
      var c = humanController(f, store);
      await c.initialize();
      await c.submit((await c.prepare(humanBlockInput))!);
      c.dispose();
      f.stateDenied = true;
      c = humanController(f, store);
      addTearDown(c.dispose);
      addTearDown(f.client.close);
      await c.initialize();
      expect(c.pending?.selector.operationID, humanOp);
      expect(c.snapshot, isNull);
      await c.recover();
      expect(c.receipt?.status, 'SUCCEEDED');
      expect(c.pending, isNull);
      expect(c.canStart, false);
      expect(f.writes.length, 1);
      expect(jsonEncode(store.values.toString()), isNot(contains('Bearer')));
    },
  );
  test(
    'write then failed readback is known not submitted and safely repaired',
    () async {
      final f = HumanControlFixture(),
          store = HumanFaultStore()..failRead = 2,
          c = humanController(f, store);
      addTearDown(c.dispose);
      addTearDown(f.client.close);
      await c.initialize();
      await c.submit((await c.prepare(humanBlockInput))!);
      expect(f.writes, isEmpty);
      expect(c.locallyPrepared, true);
      expect(c.error, contains('尚未发送'));
      await c.repairPrepared();
      expect(c.pending, isNull);
      expect(await store.read(humanBase, humanOwner), isEmpty);
      expect(
        f.calls.where((r) => r.url.path.contains('/operations/')),
        isEmpty,
      );
    },
  );
  test(
    'actor A B A during storage await never sends nor removes other owner pending',
    () async {
      final f = HumanControlFixture(),
          store = HumanFaultStore(),
          signal = ValueNotifier(0);
      String owner = humanOwner;
      final c = humanController(f, store, changes: signal, owner: () => owner);
      addTearDown(c.dispose);
      addTearDown(signal.dispose);
      addTearDown(f.client.close);
      await c.initialize();
      final p = (await c.prepare(humanBlockInput))!;
      store.entered = Completer<void>();
      final gate = Completer<void>();
      store.holdWrite = gate;
      final work = c.submit(p);
      await store.entered!.future;
      owner = humanTarget;
      signal.value++;
      owner = humanOwner;
      signal.value++;
      gate.complete();
      await work;
      expect(c.current, false);
      expect(f.writes, isEmpty);
      expect(
        (await store.read(humanBase, humanOwner)).single.stage,
        'PREPARED',
      );
    },
  );
  test(
    'same owner credential renewal preserves logical controller but kills old approval',
    () async {
      final f = HumanControlFixture(),
          store = MemoryHumanCausalPendingStore(),
          signal = ValueNotifier(0);
      String token = 'Bearer original';
      final c = humanController(f, store, changes: signal, header: () => token);
      addTearDown(c.dispose);
      addTearDown(signal.dispose);
      addTearDown(f.client.close);
      await c.initialize();
      final p = (await c.prepare(humanBlockInput))!;
      token = 'Bearer renewed';
      signal.value++;
      expect(c.current, true);
      expect(c.previewCurrent(p), false);
      expect(await c.submit(p), isNull);
      await c.initialize();
      expect(c.canStart, true);
      expect(f.writes, isEmpty);
    },
  );
  test(
    'late readonly preview and original target ABA cannot replace current snapshot',
    () async {
      final f = HumanControlFixture(),
          store = MemoryHumanCausalPendingStore(),
          signal = ValueNotifier(0);
      String target = 'A';
      final gate = Completer<void>(), entered = Completer<void>();
      f.intercept = (r) async {
        if (r.url.path.endsWith('/previews')) {
          entered.complete();
          await gate.future;
        }
        return null;
      };
      final c = humanController(
        f,
        store,
        changes: signal,
        target: () => target,
      );
      addTearDown(c.dispose);
      addTearDown(signal.dispose);
      addTearDown(f.client.close);
      await c.initialize();
      final work = c.prepare(humanBlockInput);
      await entered.future;
      target = 'B';
      signal.value++;
      target = 'A';
      signal.value++;
      gate.complete();
      expect(await work, isNull);
      expect(c.current, false);
      expect(c.preview, isNull);
      expect(f.writes, isEmpty);
    },
  );
  test(
    'conflicting other-instance UNKNOWN is never erased as local not-sent operation',
    () async {
      final f = HumanControlFixture(),
          store = MemoryHumanCausalPendingStore(),
          c = humanController(f, store);
      addTearDown(c.dispose);
      addTearDown(f.client.close);
      await c.initialize();
      final p = (await c.prepare(humanBlockInput))!;
      const other = HumanBlockInput(
        operationID: humanOtherOp,
        action: 'BLOCK',
        accountID: humanTarget,
        expectedVersion: 0,
      );
      await humanRemember(
        store,
        humanBase,
        humanOwner,
        HumanCausalPending(other.selector),
      );
      await c.submit(p);
      expect(c.locallyPrepared, false);
      await c.repairPrepared();
      expect(c.pending?.selector.operationID, humanOtherOp);
      expect((await store.read(humanBase, humanOwner)).single.stage, 'UNKNOWN');
      expect(f.writes, isEmpty);
    },
  );
}
