import 'package:birdtie_client/src/workspace/entity_share_control.dart';
import 'package:birdtie_client/src/workspace/entity_share_control_controller.dart';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'share_entity_to_chat_test.dart';
import 'entity_share_control_test.dart';
import 'entity_share_pending_store_test.dart';

class FaultShareStore extends MemoryEntitySharePendingStore {
  String? failStage;
  bool repairAtPreparedRead = false;
  @override
  Future<void> write(String env, String owner, PendingEntityShare p) async {
    if (p.stage == failStage)
      throw StateError('synthetic durable write failure');
    await super.write(env, owner, p);
  }

  @override
  Future<List<PendingEntityShare>> read(String env, String owner) async {
    final records = await super.read(env, owner);
    if (repairAtPreparedRead && records.any((p) => p.stage == 'PREPARED')) {
      repairAtPreparedRead = false;
      // Another live page repairs P after A reads it; A may not promote an empty key.
      await remove(env, owner, records.single);
    }
    return records;
  }
}

class CleanupFailShareStore extends MemoryEntitySharePendingStore {
  @override
  Future<void> remove(String env, String owner, PendingEntityShare p) async =>
      throw StateError('synthetic exact cleanup failure');
}

EntityShareControlController control(ShareFixture f) =>
    EntityShareControlController(
      api: EntityShareControlAPI(
        authorizationHeader: () => f.identity.value,
        client: f.client,
        apiBaseUrl: 'https://api.example',
      ),
      store: f.store,
      ownerID: () => f.actor.value,
      identityChanges: f.actor,
      sourceKey: () => f.source.value,
    );
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  for (final stage in ['PREPARED', 'UNKNOWN']) {
    test(
      '143 durable $stage write failure means zero PUT and preserves repair path',
      () async {
        final store = FaultShareStore()..failStage = stage;
        final f = ShareFixture(pendingStore: store);
        final actual = control(f);
        await actual.load();
        final p = await actual.prepare(
          controlInput,
          sharePeer,
          () async => true,
        );
        await actual.submit(p!, actual.epoch, () async => true);
        expect(f.posted, isEmpty);
        expect(actual.error, contains('尚未发起提交'));
        if (stage == 'UNKNOWN') {
          expect(store.values.values.single.stage, 'PREPARED');
          await actual.repair(store.values.values.single);
          expect(store.values, isEmpty);
        }
        actual.dispose();
        f.dispose();
      },
    );
  }
  test(
    '143 second page removes P before UNKNOWN: original A cannot dispatch',
    () async {
      final f = ShareFixture(pendingStore: FaultShareStore());
      final active = f.store as FaultShareStore, c = control(f);
      await c.load();
      final p = await c.prepare(controlInput, sharePeer, () async => true);
      active.repairAtPreparedRead = true;
      await c.submit(p!, c.epoch, () async => true);
      expect(f.posted, isEmpty);
      expect(active.values, isEmpty);
      expect(c.error, contains('尚未发起提交'));
      c.dispose();
      f.dispose();
    },
  );
  test(
    '143 actual recipient changes after approval produces no PUT and recoverable original metadata',
    () async {
      final f = ShareFixture(), actual = control(f);
      await actual.load();
      final p = await actual.prepare(controlInput, sharePeer, () async => true);
      await actual.submit(p!, actual.epoch, () async => false);
      expect(f.posted, isEmpty);
      expect(f.store.values.values.single.stage, 'UNKNOWN');
      await actual.repair(f.store.values.values.single);
      expect(f.store.values, isEmpty);
      actual.dispose();
      f.dispose();
    },
  );
  test(
    '143 after lost response source becomes invisible; original receipt still resolves',
    () async {
      final f = ShareFixture()..lost = true;
      final actual = control(f);
      await actual.load();
      final p = await actual.prepare(controlInput, sharePeer, () async => true);
      await actual.submit(p!, actual.epoch, () async => true);
      final original = f.store.values.values.single;
      f.withdrawn = true;
      await actual.recover(original);
      expect(actual.receipt?.succeeded, true);
      expect(f.posted.length, 1);
      expect(f.store.values, isEmpty);
      actual.dispose();
      f.dispose();
    },
  );
  test(
    '143 immutable success stays known if durable exact cleanup fails',
    () async {
      final f = ShareFixture(pendingStore: CleanupFailShareStore()),
          c = control(f);
      await c.load();
      final p = await c.prepare(controlInput, sharePeer, () async => true);
      await c.submit(p!, c.epoch, () async => true);
      expect(c.receipt?.succeeded, true);
      expect(c.notice, contains('已发送'));
      expect(f.posted.length, 1);
      expect(f.store.values.length, 1);
      expect(c.error, contains('原结果已核实'));
      c.dispose();
      f.dispose();
    },
  );
}
