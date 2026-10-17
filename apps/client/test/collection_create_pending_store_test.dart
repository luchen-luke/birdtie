import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/collection_control.dart';
import 'package:birdtie_client/src/content/collection_create_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'collection_copy_pending_store_test.dart' show CopyPausedStorage;

const createOrigin = 'https://synthetic.invalid';
const createOwner = '11111111-1111-4111-8111-111111111111';
const createOperation = '44444444-4444-4444-8444-444444444444';
CollectionCreatePending createStored() => CollectionCreatePending.prepared(
  createOperation,
  CollectionControlBody.normalize({
    'kind': 'journey',
    'cityId': 'aberdeen-gb',
    'title': '仅本人的草稿',
    'summary': '正文不可入journal',
    'entries': [
      {
        'placeId': '33333333-3333-4333-8333-333333333333',
        'note': '私人备注',
        'locationVisibility': 'hidden',
      },
    ],
  }),
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'create secure restart journal has only selectors and exact origin owner kind city namespace',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      const first = SecureCollectionCreatePendingStore(),
          next = SecureCollectionCreatePendingStore();
      final p = createStored();
      await first.write(createOrigin, createOwner, p);
      await first.write(createOrigin, createOwner, p.unknown);
      final raw = (await const FlutterSecureStorage().readAll()).values.single;
      final m = jsonDecode(raw) as Map;
      expect(m.keys.toSet(), {'origin', 'owner', 'workspace', 'pending'});
      expect((m['pending'] as Map).keys.toSet(), {
        'schemaVersion',
        'operationId',
        'kind',
        'cityId',
        'payloadDigest',
        'stage',
      });
      for (final forbidden in [
        'title',
        'summary',
        'note',
        'journeyDetails',
        'input',
        'Bearer',
        'token',
        'grant',
        'latitude',
        'approval',
        '私人备注',
      ]) {
        expect(raw, isNot(contains(forbidden)));
      }
      expect(
        (await next.read(
          createOrigin,
          createOwner,
          'journey',
          'aberdeen-gb',
        ))!.same(p),
        true,
      );
      for (final context in [
        ['https://other.invalid', createOwner, 'journey', 'aberdeen-gb'],
        [createOrigin, createOperation, 'journey', 'aberdeen-gb'],
        [createOrigin, createOwner, 'theme_map', 'aberdeen-gb'],
        [createOrigin, createOwner, 'journey', 'other-city'],
      ]) {
        expect(
          await next.read(context[0], context[1], context[2], context[3]),
          isNull,
        );
      }
      expect(
        () => next.read(
          createOrigin + '/path',
          createOwner,
          'journey',
          'aberdeen-gb',
        ),
        throwsFormatException,
      );
      await next.remove(createOrigin, createOwner, p.unknown);
      expect(
        await next.read(createOrigin, createOwner, 'journey', 'aberdeen-gb'),
        isNull,
      );
    },
  );
  for (final secure in [false, true]) {
    test(
      'create immutable operation digest stage and CAS removal secure=$secure',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final CollectionCreatePendingStore s = secure
            ? const SecureCollectionCreatePendingStore()
            : MemoryCollectionCreatePendingStore();
        final p = createStored();
        await expectLater(
          s.write(createOrigin, createOwner, p.unknown),
          throwsStateError,
        );
        await s.write(createOrigin, createOwner, p);
        await expectLater(
          s.write(
            createOrigin,
            createOwner,
            CollectionCreatePending.decode({
              ...p.toJson(),
              'operationId': createOwner,
            }),
          ),
          throwsStateError,
        );
        await expectLater(
          s.write(
            createOrigin,
            createOwner,
            CollectionCreatePending.decode({
              ...p.toJson(),
              'payloadDigest': 'b' * 64,
            }),
          ),
          throwsStateError,
        );
        await s.write(createOrigin, createOwner, p.unknown);
        await expectLater(
          s.write(createOrigin, createOwner, p),
          throwsStateError,
        );
        await expectLater(
          s.remove(createOrigin, createOwner, p),
          throwsStateError,
        );
        expect(
          (await s.read(
            createOrigin,
            createOwner,
            'journey',
            'aberdeen-gb',
          ))!.stage,
          'UNKNOWN',
        );
        await s.remove(createOrigin, createOwner, p.unknown);
        await expectLater(
          s.write(createOrigin, createOwner, p.unknown),
          throwsStateError,
        );
      },
    );
  }
  test(
    'create secure cross-instance lock retains promoted UNKNOWN against stale PREPARED removal',
    () async {
      final storage = CopyPausedStorage(),
          a = SecureCollectionCreatePendingStore(storage: storage),
          b = SecureCollectionCreatePendingStore(storage: storage),
          p = createStored();
      await a.write(createOrigin, createOwner, p);
      final release = Completer<void>();
      storage.pauseRead = release;
      storage.readEntered = Completer<void>();
      final promotion = a.write(createOrigin, createOwner, p.unknown);
      await storage.readEntered!.future;
      final stale = expectLater(
        b.remove(createOrigin, createOwner, p),
        throwsStateError,
      );
      release.complete();
      await promotion;
      await stale;
      expect(
        (await b.read(
          createOrigin,
          createOwner,
          'journey',
          'aberdeen-gb',
        ))!.stage,
        'UNKNOWN',
      );
    },
  );
  test(
    'create closed journal rejects payload credentials invalid schema and stage',
    () {
      final p = createStored();
      for (final patch in <Map<String, Object>>[
        {'title': 'private'},
        {'token': 'secret'},
        {'schemaVersion': 'other'},
        {'stage': 'SUCCEEDED'},
        {'kind': 'plan'},
        {'cityId': 'wrong\ncity'},
        {'payloadDigest': 'a'},
      ]) {
        expect(
          () => CollectionCreatePending.decode({...p.toJson(), ...patch}),
          throwsFormatException,
        );
      }
    },
  );
}
