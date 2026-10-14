import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:birdtie_client/src/workspace/activity_participation_operation.dart';
import 'package:birdtie_client/src/workspace/activity_participation_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'activity_participation_operation_contract_test.dart';

String legacyKey(String e, PendingParticipationOperation p) =>
    'birdtie.human-participation.pending.v1.${sha256.convert(utf8.encode('$e\n$participationOwner'))}.${p.operationID}';

class AliasFailureStorage extends FlutterSecureStorage {
  String? failedWriteKey, failedDeleteKey;
  final deletes = <String>[];
  @override
  Future<void> write({
    required String key,
    required String? value,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    if (key == failedWriteKey) throw StateError('write interrupted');
    await super.write(
      key: key,
      value: value,
      iOptions: iOptions,
      aOptions: aOptions,
      lOptions: lOptions,
      webOptions: webOptions,
      mOptions: mOptions,
      wOptions: wOptions,
    );
  }

  @override
  Future<void> delete({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    deletes.add(key);
    if (key == failedDeleteKey) throw StateError('clear interrupted');
    await super.delete(
      key: key,
      iOptions: iOptions,
      aOptions: aOptions,
      lOptions: lOptions,
      webOptions: webOptions,
      mOptions: mOptions,
      wOptions: wOptions,
    );
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  for (final secure in [false, true]) {
    test(
      'alias $secure new writes recover from either entry and clear both',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final ParticipationPendingStore store = secure
            ? SecureParticipationPendingStore()
            : MemoryParticipationPendingStore();
        await store.write(
          'http://api.test/',
          participationOwner,
          testParticipationPending,
        );
        expect(
          (await store.read(
            'http://api.test',
            participationOwner,
          )).single.operationID,
          participationOp,
        );
        await store.write(
          'http://api.test',
          participationOwner,
          testParticipationPending.stopping(),
        );
        expect(
          (await store.read(
            'http://api.test/',
            participationOwner,
          )).single.stopRequested,
          isTrue,
        );
        await store.delete(
          'http://api.test/',
          participationOwner,
          testParticipationPending.stopping(),
        );
        expect(
          await store.read('http://api.test', participationOwner),
          isEmpty,
        );
        expect(
          await store.read('http://api.test/', participationOwner),
          isEmpty,
        );
      },
    );
  }
  test(
    'legacy encrypted slash journal survives restart and merges stop without read migration',
    () async {
      final original = {
        legacyKey('http://api.test/', testParticipationPending): jsonEncode(
          testParticipationPending.toJson(),
        ),
      };
      FlutterSecureStorage.setMockInitialValues(original);
      final store = SecureParticipationPendingStore();
      expect(
        (await store.read(
          'http://api.test',
          participationOwner,
        )).single.operationID,
        participationOp,
      );
      expect(await const FlutterSecureStorage().readAll(), original);
      await store.write(
        'http://api.test',
        participationOwner,
        testParticipationPending.stopping(),
      );
      final restarted = SecureParticipationPendingStore();
      expect(
        (await restarted.read(
          'http://api.test/',
          participationOwner,
        )).single.stopRequested,
        isTrue,
      );
      await expectLater(
        restarted.write(
          'http://api.test/',
          participationOwner,
          testParticipationPending,
        ),
        throwsStateError,
      );
      await expectLater(
        restarted.delete(
          'http://api.test/',
          participationOwner,
          testParticipationPending,
        ),
        throwsStateError,
      );
      await restarted.delete(
        'http://api.test',
        participationOwner,
        testParticipationPending.stopping(),
      );
      expect(await const FlutterSecureStorage().readAll(), isEmpty);
    },
  );
  for (final bindingConflict in [false, true]) {
    test(
      'alias conflict $bindingConflict preserves all bytes and blocks read/write/delete',
      () async {
        final other = PendingParticipationOperation(
          operationID: bindingConflict
              ? participationOp
              : 'a1000000-0000-4000-8000-000000000088',
          activityID: participationActivity,
          operation: 'JOIN',
          requestDigest: bindingConflict ? 'b' * 64 : participationDigest,
        );
        final original = {
          legacyKey('http://api.test', testParticipationPending): jsonEncode(
            testParticipationPending.toJson(),
          ),
          legacyKey('http://api.test/', other): jsonEncode(other.toJson()),
        };
        FlutterSecureStorage.setMockInitialValues(original);
        final store = SecureParticipationPendingStore();
        await expectLater(
          store.read('http://api.test/', participationOwner),
          throwsFormatException,
        );
        await expectLater(
          store.write(
            'http://api.test',
            participationOwner,
            testParticipationPending.stopping(),
          ),
          throwsFormatException,
        );
        await expectLater(
          store.delete(
            'http://api.test',
            participationOwner,
            testParticipationPending,
          ),
          throwsFormatException,
        );
        expect(await const FlutterSecureStorage().readAll(), original);
      },
    );
  }
  test(
    'alias canonical write failure preserves legacy UNKNOWN byte for byte',
    () async {
      final original = {
        legacyKey('http://api.test/', testParticipationPending): jsonEncode(
          testParticipationPending.toJson(),
        ),
      };
      FlutterSecureStorage.setMockInitialValues(original);
      final storage = AliasFailureStorage()
        ..failedWriteKey = legacyKey(
          'http://api.test',
          testParticipationPending,
        );
      final store = SecureParticipationPendingStore(storage: storage);
      await expectLater(
        store.write(
          'http://api.test/',
          participationOwner,
          testParticipationPending.stopping(),
        ),
        throwsStateError,
      );
      expect(await storage.readAll(), original);
      expect(
        (await store.read(
          'http://api.test',
          participationOwner,
        )).single.operationID,
        participationOp,
      );
    },
  );
  for (final canonicalStopping in [false, true]) {
    test(
      'alias interrupted clear $canonicalStopping always retains stopping copy until terminal clear',
      () async {
        final canonical = legacyKey(
          'http://api.test',
          testParticipationPending,
        );
        final alias = legacyKey('http://api.test/', testParticipationPending);
        final stopping = testParticipationPending.stopping();
        final trueKey = canonicalStopping ? canonical : alias;
        final falseKey = canonicalStopping ? alias : canonical;
        FlutterSecureStorage.setMockInitialValues({
          trueKey: jsonEncode(stopping.toJson()),
          falseKey: jsonEncode(testParticipationPending.toJson()),
        });
        final storage = AliasFailureStorage()..failedDeleteKey = falseKey;
        final store = SecureParticipationPendingStore(storage: storage);
        await expectLater(
          store.delete('http://api.test/', participationOwner, stopping),
          throwsStateError,
        );
        expect(storage.deletes.first, falseKey);
        expect(
          (await store.read(
            'http://api.test',
            participationOwner,
          )).single.stopRequested,
          isTrue,
        );
        storage.failedDeleteKey = trueKey;
        await expectLater(
          store.delete('http://api.test', participationOwner, stopping),
          throwsStateError,
        );
        expect(
          (await store.read(
            'http://api.test/',
            participationOwner,
          )).single.stopRequested,
          isTrue,
        );
        expect((await storage.readAll()).keys, [trueKey]);
        storage.failedDeleteKey = null;
        await store.delete('http://api.test', participationOwner, stopping);
        expect(
          await store.read('http://api.test/', participationOwner),
          isEmpty,
        );
      },
    );
  }
  test(
    'alias independent secure instances serialize two keys for same activity',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final another = PendingParticipationOperation(
        operationID: 'a1000000-0000-4000-8000-000000000088',
        activityID: participationActivity,
        operation: 'JOIN',
        requestDigest: participationDigest,
      );
      final outcomes = await Future.wait(
        [
          ('http://api.test', testParticipationPending),
          ('http://api.test/', another),
        ].map((entry) async {
          try {
            await SecureParticipationPendingStore().write(
              entry.$1,
              participationOwner,
              entry.$2,
            );
            return true;
          } catch (_) {
            return false;
          }
        }),
      );
      expect(outcomes.where((x) => x), hasLength(1));
      expect(
        await SecureParticipationPendingStore().read(
          'http://api.test',
          participationOwner,
        ),
        hasLength(1),
      );
    },
  );
  test(
    'environment validates single slash without broad host or path normalization',
    () {
      expect(
        participationEnvironment('http://api.test/base/'),
        'http://api.test/base',
      );
      expect(
        participationEnvironment('https://API.TEST:443/base'),
        'https://API.TEST:443/base',
      );
      for (final invalid in [
        'http://api.test//',
        'api.test',
        'http://',
        'http://api.test/?x=1',
        'http://api.test/#a',
        'http://u:p@api.test',
      ]) {
        expect(() => participationEnvironment(invalid), throwsFormatException);
      }
    },
  );
  for (final secure in [false, true]) {
    test(
      'store $secure survives instance change and isolates owner/environment',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final ParticipationPendingStore store = secure
            ? SecureParticipationPendingStore()
            : MemoryParticipationPendingStore();
        await store.write(
          'http://api.test',
          participationOwner,
          testParticipationPending,
        );
        final restarted = secure ? SecureParticipationPendingStore() : store;
        expect(
          (await restarted.read(
            'http://api.test',
            participationOwner,
          )).single.toJson(),
          testParticipationPending.toJson(),
        );
        expect(
          await restarted.read('http://other.test', participationOwner),
          isEmpty,
        );
        expect(
          await restarted.read(
            'http://api.test',
            'a1000000-0000-4000-8000-000000000099',
          ),
          isEmpty,
        );
      },
    );
    test(
      'store $secure forbids rebind/second key/stop rollback and stale deletion',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final ParticipationPendingStore store = secure
            ? SecureParticipationPendingStore()
            : MemoryParticipationPendingStore();
        await store.write(
          'http://api.test',
          participationOwner,
          testParticipationPending,
        );
        final another = PendingParticipationOperation(
          operationID: 'a1000000-0000-4000-8000-000000000088',
          activityID: participationActivity,
          operation: 'JOIN',
          requestDigest: participationDigest,
        );
        await expectLater(
          store.write('http://api.test', participationOwner, another),
          throwsStateError,
        );
        await store.write(
          'http://api.test',
          participationOwner,
          testParticipationPending.stopping(),
        );
        await expectLater(
          store.write(
            'http://api.test',
            participationOwner,
            testParticipationPending,
          ),
          throwsStateError,
        );
        await expectLater(
          store.delete(
            'http://api.test',
            participationOwner,
            testParticipationPending,
          ),
          throwsStateError,
        );
        expect(
          (await store.read(
            'http://api.test',
            participationOwner,
          )).single.stopRequested,
          isTrue,
        );
      },
    );
  }
  test(
    'corrupt encrypted recovery metadata blocks new send rather than silently dropping it',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final store = SecureParticipationPendingStore();
      await store.write(
        'http://api.test',
        participationOwner,
        testParticipationPending,
      );
      const storage = FlutterSecureStorage();
      final all = await storage.readAll();
      final key = all.keys.single;
      await storage.write(
        key: key,
        value: jsonEncode({'token': 'private', 'operationId': participationOp}),
      );
      await expectLater(
        store.read('http://api.test', participationOwner),
        throwsFormatException,
      );
      await expectLater(
        store.write(
          'http://api.test',
          participationOwner,
          testParticipationPending,
        ),
        throwsFormatException,
      );
    },
  );
  test(
    'concurrent secure store writes cannot reserve two keys for same activity',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final store = SecureParticipationPendingStore();
      final another = PendingParticipationOperation(
        operationID: 'a1000000-0000-4000-8000-000000000088',
        activityID: participationActivity,
        operation: 'JOIN',
        requestDigest: participationDigest,
      );
      final outcomes = await Future.wait(
        [testParticipationPending, another].map((p) async {
          try {
            await store.write('http://api.test', participationOwner, p);
            return true;
          } catch (_) {
            return false;
          }
        }),
      );
      expect(outcomes.where((x) => x), hasLength(1));
      expect(
        await store.read('http://api.test', participationOwner),
        hasLength(1),
      );
    },
  );
}
