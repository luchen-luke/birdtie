import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/collection_control.dart';
import 'package:birdtie_client/src/content/collection_control_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'collection_control_test.dart';
import 'collection_copy_pending_store_test.dart' show CopyPausedStorage;

const ccStoreOrigin = 'https://control-synthetic.test';
const ccStoreOwner = '50000000-0000-4000-8000-000000000005';
CollectionControlPending ccPending() => CollectionControlPending.preview(
  CollectionControlPreview.decode(ccPreview(ccInput()), ccInput()),
);

class ControlPausedStorage extends CopyPausedStorage {
  @override
  Future<Map<String, String>> readAll({
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async => Map.from(values);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    '147 secure journal only original selector and personal origin owner namespace',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      const store = SecureCollectionControlPendingStore();
      final p = ccPending();
      await store.write(ccStoreOrigin, ccStoreOwner, p);
      await store.write(ccStoreOrigin, ccStoreOwner, p.unknown);
      final all = await const FlutterSecureStorage().readAll(),
          raw = all.values.single;
      final m = jsonDecode(raw) as Map;
      expect(m.keys.toSet(), {'origin', 'owner', 'workspace', 'pending'});
      expect(m['workspace'], 'personal');
      expect((m['pending'] as Map).keys.toSet(), {
        'schemaVersion',
        'operationId',
        'action',
        'kind',
        'collectionId',
        'expectedRevision',
        'payloadDigest',
        'stage',
      });
      for (final forbidden in [
        'title',
        'summary',
        'note',
        'input',
        'approval',
        'Bearer',
        'latitude',
        'longitude',
        '私人备注',
      ]) {
        expect(raw, isNot(contains(forbidden)));
      }
      expect(
        (await store.read(ccStoreOrigin, ccStoreOwner)).single.same(p),
        true,
      );
      expect(
        await store.read('https://other-synthetic.test', ccStoreOwner),
        isEmpty,
      );
      expect(await store.read(ccStoreOrigin, ccTarget), isEmpty);
      await expectLater(
        store.read(ccStoreOrigin + '/path', ccStoreOwner),
        throwsFormatException,
      );
      await store.remove(ccStoreOrigin, ccStoreOwner, p.unknown);
      expect(await store.read(ccStoreOrigin, ccStoreOwner), isEmpty);
    },
  );
  for (final secure in [false, true]) {
    test(
      '147 repaired PREPARED cannot be resurrected as UNKNOWN secure=$secure',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final CollectionControlPendingStore store = secure
            ? const SecureCollectionControlPendingStore()
            : MemoryCollectionControlPendingStore();
        final p = ccPending();
        await expectLater(
          store.write(ccStoreOrigin, ccStoreOwner, p.unknown),
          throwsStateError,
        );
        await store.write(ccStoreOrigin, ccStoreOwner, p);
        await store.remove(ccStoreOrigin, ccStoreOwner, p);
        await expectLater(
          store.write(ccStoreOrigin, ccStoreOwner, p.unknown),
          throwsStateError,
        );
        expect(await store.read(ccStoreOrigin, ccStoreOwner), isEmpty);
      },
    );
  }
  test(
    '147 secure cross instance key lock prevents paused old PREPARED removal from deleting UNKNOWN',
    () async {
      final storage = ControlPausedStorage(),
          a = SecureCollectionControlPendingStore(storage: storage),
          b = SecureCollectionControlPendingStore(storage: storage),
          p = ccPending();
      await a.write(ccStoreOrigin, ccStoreOwner, p);
      storage.pauseRead = Completer<void>();
      storage.readEntered = Completer<void>();
      final release = storage.pauseRead!;
      final promotion = a.write(ccStoreOrigin, ccStoreOwner, p.unknown);
      await storage.readEntered!.future;
      final deletion = b.remove(ccStoreOrigin, ccStoreOwner, p);
      final rejected = expectLater(deletion, throwsStateError);
      release.complete();
      await promotion;
      await rejected;
      expect(
        (await b.read(ccStoreOrigin, ccStoreOwner)).single.stage,
        'UNKNOWN',
      );
    },
  );
  test(
    '147 same operation cannot replace digest target or regress stage',
    () async {
      final store = MemoryCollectionControlPendingStore(), p = ccPending();
      await store.write(ccStoreOrigin, ccStoreOwner, p);
      final other = CollectionControlPending.decode({
        ...p.toJson(),
        'payloadDigest': 'b' * 64,
      });
      await expectLater(
        store.write(ccStoreOrigin, ccStoreOwner, other),
        throwsStateError,
      );
      await store.write(ccStoreOrigin, ccStoreOwner, p.unknown);
      await expectLater(
        store.write(ccStoreOrigin, ccStoreOwner, p),
        throwsStateError,
      );
      await expectLater(
        store.remove(ccStoreOrigin, ccStoreOwner, other),
        throwsStateError,
      );
      expect(
        (await store.read(ccStoreOrigin, ccStoreOwner)).single.payloadDigest,
        p.payloadDigest,
      );
    },
  );
  test(
    '147 closed journal rejects credential body and malformed action schema or MAX update',
    () {
      final p = ccPending();
      for (final patch in <Map<String, Object>>[
        {'token': 'secret'},
        {'title': 'private'},
        {'schemaVersion': 'other'},
        {'stage': 'SUCCEEDED'},
        {'action': 'CREATE'},
        {'expectedRevision': collectionControlMaxRevision},
      ]) {
        expect(
          () => CollectionControlPending.decode({...p.toJson(), ...patch}),
          throwsFormatException,
        );
      }
    },
  );
  for (var i = 1; i <= 2; i++) {
    testWidgets(
      '147 default secure sequential widget zone $i retains no completed lock Future',
      (t) async {
        FlutterSecureStorage.setMockInitialValues({});
        const store = SecureCollectionControlPendingStore();
        final p = ccPending();
        await t.runAsync(() async {
          await store.write(ccStoreOrigin, ccStoreOwner, p);
          await store.write(ccStoreOrigin, ccStoreOwner, p.unknown);
          expect(
            (await store.read(ccStoreOrigin, ccStoreOwner)).single.stage,
            'UNKNOWN',
          );
          await store.remove(ccStoreOrigin, ccStoreOwner, p.unknown);
        });
        expect(t.takeException(), isNull);
      },
    );
  }
}
