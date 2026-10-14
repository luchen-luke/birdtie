import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/collection_copy_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'collection_copy_controller_test.dart'
    show copyBase, copyRef, copyOp, copySecond;
import 'personal_collection_controller_test.dart' show collectionOwner;

const copyStored = CollectionCopyPending(
  CollectionCopySelector(
    sourceRef: copyRef,
    operationID: copyOp,
    publicationRevision: 2,
    grantVersion: 1,
    orderedPlaceIDsHash:
        'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    inputHash:
        'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
  ),
);
const copyOther = CollectionCopyPending(
  CollectionCopySelector(
    sourceRef: copyRef,
    operationID: copySecond,
    publicationRevision: 2,
    grantVersion: 1,
    orderedPlaceIDsHash:
        'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    inputHash:
        'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
  ),
);

class CopyPausedStorage extends FlutterSecureStorage {
  final values = <String, String>{};
  Completer<void>? pauseRead, readEntered;
  @override
  Future<String?> read({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    final gate = pauseRead;
    if (gate != null) {
      pauseRead = null;
      readEntered?.complete();
      await gate.future;
    }
    return values[key];
  }

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
    if (value == null)
      values.remove(key);
    else
      values[key] = value;
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
    values.remove(key);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'C3 secure journal stores only original metadata and isolates origin and owner',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final store = SecureCollectionCopyPendingStore();
      await store.write(copyBase, collectionOwner, copyStored);
      final values = await const FlutterSecureStorage().readAll();
      final raw = values.values.single,
          m = jsonDecode(values.values.single) as Map<String, dynamic>;
      expect(m.keys.toSet(), {'origin', 'owner', 'pending'});
      expect((m['pending'] as Map)['selector'].keys.toSet(), {
        'sourceRef',
        'operationId',
        'publicationRevision',
        'grantVersion',
        'orderedPlaceIdsSha256',
        'inputSha256',
      });
      for (final forbidden in [
        'Bearer',
        'approval',
        'title',
        'summary',
        'note',
        'latitude',
        'longitude',
        'orderedPlaceIds"',
      ])
        expect(raw.contains(forbidden), isFalse);
      expect(
        (await SecureCollectionCopyPendingStore().read(
          copyBase,
          collectionOwner,
        ))!.same(copyStored),
        isTrue,
      );
      expect(
        await store.read('https://other.synthetic.test', collectionOwner),
        isNull,
      );
      expect(await store.read(copyBase, copySecond), isNull);
      expect(await const FlutterSecureStorage().readAll(), values);
    },
  );
  test(
    'C3 secure journal rejects another operation and stage downgrade, stale remove preserves bytes',
    () async {
      final storage = CopyPausedStorage(),
          a = SecureCollectionCopyPendingStore(storage: CopyPausedStorage());
      // The two instances below intentionally share the same backing service.
      final first = SecureCollectionCopyPendingStore(storage: storage),
          second = SecureCollectionCopyPendingStore(storage: storage);
      await first.write(copyBase, collectionOwner, copyStored);
      final original = Map<String, String>.of(storage.values);
      await expectLater(
        second.write(copyBase, collectionOwner, copyOther),
        throwsFormatException,
      );
      await expectLater(
        second.write(
          copyBase,
          collectionOwner,
          CollectionCopyPending(copyStored.selector, stage: 'PREPARED'),
        ),
        throwsFormatException,
      );
      await expectLater(
        second.remove(copyBase, collectionOwner, copyOther),
        throwsFormatException,
      );
      expect(storage.values, original);
      expect(await a.read(copyBase, collectionOwner), isNull);
    },
  );
  test(
    'C3 shared per-key storage lock serializes old remove before new owner-scoped operation',
    () async {
      final storage = CopyPausedStorage();
      final first = SecureCollectionCopyPendingStore(storage: storage),
          second = SecureCollectionCopyPendingStore(storage: storage);
      await first.write(copyBase, collectionOwner, copyStored);
      final gate = Completer<void>();
      storage.pauseRead = gate;
      storage.readEntered = Completer<void>();
      final remove = first.remove(copyBase, collectionOwner, copyStored);
      await storage.readEntered!.future;
      var finished = false;
      final write = second
          .write(copyBase, collectionOwner, copyOther)
          .then((_) => finished = true);
      await Future<void>.delayed(Duration.zero);
      expect(finished, isFalse);
      gate.complete();
      await remove;
      await write;
      expect(
        (await first.read(copyBase, collectionOwner))!.same(copyOther),
        isTrue,
      );
      await expectLater(
        first.remove(copyBase, collectionOwner, copyStored),
        throwsFormatException,
      );
      expect(
        (await second.read(copyBase, collectionOwner))!.same(copyOther),
        isTrue,
      );
    },
  );
  test(
    'C3 pending codec rejects body leakage, invalid closed selectors and malformed stage',
    () {
      for (final raw in [
        {...copyStored.toJson(), 'title': 'private'},
        {...copyStored.toJson(), 'stage': 'SENT'},
        {
          ...copyStored.toJson(),
          'selector': {...copyStored.selector.toJson(), 'title': 'private'},
        },
        {
          ...copyStored.toJson(),
          'selector': {
            ...copyStored.selector.toJson(),
            'publicationRevision': 2.0,
          },
        },
      ]) {
        expect(
          () => CollectionCopyPending.fromJson(raw),
          throwsFormatException,
        );
      }
    },
  );
  for (var zone = 1; zone <= 2; zone++) {
    testWidgets(
      'C3 default secure store completes in sequential widget zone $zone without reset',
      (t) async {
        if (zone == 1) FlutterSecureStorage.setMockInitialValues({});
        final store = SecureCollectionCopyPendingStore();
        await t.pumpWidget(
          const MaterialApp(home: Scaffold(body: Text('隔离本机恢复测试'))),
        );
        await t.runAsync(() async {
          await store.write(copyBase, collectionOwner, copyStored);
          expect(
            (await store.read(copyBase, collectionOwner))!.same(copyStored),
            isTrue,
          );
          await store.remove(copyBase, collectionOwner, copyStored);
          expect(await store.read(copyBase, collectionOwner), isNull);
        });
        await t.pumpAndSettle();
        expect(t.takeException(), isNull);
      },
    );
  }
}
