import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:birdtie_client/src/workspace/human_causal_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'human_causal_control_test.dart';
import 'collection_copy_pending_store_test.dart' show CopyPausedStorage;

const humanBlockInput = HumanBlockInput(
  operationID: humanOp,
  action: 'BLOCK',
  accountID: humanTarget,
  expectedVersion: 0,
);
Future<void> humanRemember(
  HumanCausalPendingStore store,
  String origin,
  String owner,
  HumanCausalPending pending,
) async {
  if (pending.stage == 'UNKNOWN')
    await store.write(
      origin,
      owner,
      HumanCausalPending(pending.selector, stage: 'PREPARED'),
    );
  await store.write(origin, owner, pending);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'UNKNOWN promotion requires the same preparation still owned under the key lock',
    () async {
      for (final store in <HumanCausalPendingStore>[
        MemoryHumanCausalPendingStore(),
        SecureHumanCausalPendingStore(storage: CopyPausedStorage()),
      ]) {
        final prepared = HumanCausalPending(
              humanBlockInput.selector,
              stage: 'PREPARED',
            ),
            unknown = HumanCausalPending(humanBlockInput.selector);
        await expectLater(
          store.write(humanBase, humanOwner, unknown),
          throwsFormatException,
        );
        await store.write(humanBase, humanOwner, prepared);
        await store.remove(humanBase, humanOwner, prepared);
        await expectLater(
          store.write(humanBase, humanOwner, unknown),
          throwsFormatException,
        );
        expect(await store.read(humanBase, humanOwner), isEmpty);
      }
    },
  );
  test(
    'shared secure owner key refuses concurrent unrelated operation and old-stage remove',
    () async {
      final storage = CopyPausedStorage(),
          a = SecureHumanCausalPendingStore(storage: storage),
          b = SecureHumanCausalPendingStore(storage: storage);
      final p = HumanCausalPending(humanBlockInput.selector, stage: 'PREPARED'),
          u = HumanCausalPending(humanBlockInput.selector);
      storage.pauseRead = Completer<void>();
      storage.readEntered = Completer<void>();
      final pause = storage.pauseRead!;
      final first = a.write(humanBase, humanOwner, p);
      await storage.readEntered!.future;
      final competing = b.write(
        humanBase,
        humanOwner,
        const HumanCausalPending(
          HumanCausalSelector(
            family: HumanCausalFamily.block,
            operationID: humanOtherOp,
            action: 'BLOCK',
            payloadDigest:
                '1111111111111111111111111111111111111111111111111111111111111111',
            targetID: humanTarget,
            expectedVersion: 0,
          ),
        ),
      );
      final rejected = expectLater(competing, throwsFormatException);
      pause.complete();
      await first;
      await rejected;
      await b.write(humanBase, humanOwner, u);
      await expectLater(
        a.remove(humanBase, humanOwner, p),
        throwsFormatException,
      );
      expect((await a.read(humanBase, humanOwner)).single.same(u), true);
      await b.remove(humanBase, humanOwner, u);
      expect(await a.read(humanBase, humanOwner), isEmpty);
    },
  );
  test(
    'multiple families and FAQ organizations remain independent with metadata only',
    () async {
      final storage = CopyPausedStorage(),
          store = SecureHumanCausalPendingStore(storage: storage);
      final p = HumanCausalPending(humanBlockInput.selector),
          report = HumanCausalPending(
            const HumanReportInput(
              operationID: humanOtherOp,
              targetType: 'general',
              targetID: '',
              reason: 'safety',
              details: '私人正文绝不落入本机原操作索引。',
            ).selector,
          );
      await humanRemember(store, humanBase, humanOwner, p);
      await humanRemember(store, humanBase, humanOwner, report);
      await humanRemember(store, humanBase, humanTarget, p);
      expect((await store.read(humanBase, humanOwner)).length, 2);
      final text = jsonEncode(storage.values);
      expect(text, isNot(contains('私人正文')));
      expect(text, isNot(contains('Bearer')));
      expect(text, isNot(contains('approval')));
      await store.remove(humanBase, humanOwner, p);
      expect((await store.read(humanBase, humanTarget)).single.same(p), true);
      expect(
        (await store.read(humanBase, humanOwner)).single.same(report),
        true,
      );
      expect(await store.read('https://other.fixture', humanOwner), isEmpty);
    },
  );
  for (final order in [1, 2])
    testWidgets(
      'default secure store sequential widget zone $order releases completed key tail',
      (t) async {
        FlutterSecureStorage.setMockInitialValues({});
        final store = SecureHumanCausalPendingStore();
        await t.runAsync(() async {
          await humanRemember(
            store,
            humanBase,
            humanOwner,
            HumanCausalPending(humanBlockInput.selector),
          );
          expect((await store.read(humanBase, humanOwner)).length, 1);
          await store.remove(
            humanBase,
            humanOwner,
            HumanCausalPending(humanBlockInput.selector),
          );
        });
      },
    );
  test(
    'corrupt or extra journal body is rejected without accepting selector',
    () {
      expect(
        () => HumanCausalPending.fromJson({
          'selector': humanBlockInput.selector.toJson(),
          'stage': 'UNKNOWN',
          'body': 'private',
        }),
        throwsFormatException,
      );
    },
  );
}
