import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/collection_copy_controller.dart';
import 'package:birdtie_client/src/content/collection_copy_pending_store.dart';
import 'package:birdtie_client/src/content/collection_publication_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'personal_collection_controller_test.dart'
    show
        collectionReceipt,
        collectionResponse,
        collectionOwner,
        collectionID,
        collectionPlaceID;

const copyBase = 'https://synthetic-copy.test';
const copyRef = CollectionCopyReference('journey', collectionID);
const copySecond = '44444444-4444-4444-8444-444444444444';
const copyThird = '55555555-5555-4555-8555-555555555555';
const copyOp = '66666666-6666-4666-8666-666666666666';
const copyDestination = '77777777-7777-4777-8777-777777777777';
Map<String, dynamic> copyGrantWire({
  CollectionCopyReference ref = copyRef,
  int revision = 2,
  int grant = 1,
  List<String>? ids,
  int lease = 25,
}) {
  final list = ids ?? [collectionPlaceID, copySecond];
  return {
    'sourceRef': ref.toJson(),
    'publicationRevision': revision,
    'grantVersion': grant,
    'eligiblePlaceIds': list,
    'orderedPlaceIdsSha256': collectionCopyPlaceDigest(list),
    'expiresAt': DateTime.now()
        .toUtc()
        .add(Duration(seconds: lease))
        .toIso8601String(),
  };
}

CollectionCopyGrant copyGrant({
  int revision = 2,
  int version = 1,
  List<String>? ids,
  int lease = 25,
}) => CollectionCopyGrant.fromJson(
  copyGrantWire(revision: revision, grant: version, ids: ids, lease: lease),
  source: copyRef,
  publicationRevision: revision,
  sourceExpiresAt: DateTime.now().toUtc().add(const Duration(seconds: 60)),
  publicPlaceIDs: ids ?? [collectionPlaceID, copySecond],
);
CollectionCopyInput copyInput(
  Map<String, dynamic> body, {
  CollectionCopyReference ref = copyRef,
}) => CollectionCopyInput(
  sourceRef: ref,
  operationID: body['operationId'],
  publicationRevision: body['publicationRevision'],
  grantVersion: body['grantVersion'],
  orderedPlaceIDs: (body['orderedPlaceIds'] as List).cast<String>(),
  title: body['title'],
);
Map<String, dynamic> copyPreviewWire(
  CollectionCopyInput input, {
  int lease = 12,
}) => {
  'sourceRef': input.sourceRef.toJson(),
  'operationId': input.operationID,
  'publicationRevision': input.publicationRevision,
  'grantVersion': input.grantVersion,
  'orderedPlaceIdsSha256': input.placeDigest,
  'entryCount': input.orderedPlaceIDs.length,
  'title': input.title,
  'inputSha256': input.inputDigest,
  'approval': 'opaque-original-preview',
  'expiresAt': DateTime.now()
      .toUtc()
      .add(Duration(seconds: lease))
      .toIso8601String(),
};
Map<String, dynamic> copyReceiptWire(
  CollectionCopySelector s, {
  bool stopped = false,
  String destination = copyDestination,
}) => {
  'operationId': s.operationID,
  'status': stopped ? 'STOPPED' : 'SUCCEEDED',
  'resultRevision': stopped ? 0 : 1,
  'destinationRef': stopped
      ? null
      : {'kind': s.sourceRef.kind, 'id': destination},
  if (!stopped) ...{
    'sourceRef': s.sourceRef.toJson(),
    'publicationRevision': s.publicationRevision,
    'grantVersion': s.grantVersion,
    'orderedPlaceIdsSha256': s.orderedPlaceIDsHash,
  },
};
Map<String, dynamic> copyPrivateWire({
  String kind = 'journey',
  String id = copyDestination,
  int revision = 1,
  String title = '本人新标题',
  List<String>? ids,
}) => {
  ...collectionReceipt(kind: kind, id: id, revision: revision, title: title),
  'privatePlaceCopyRestricted': true,
  'entries': [
    for (final place in ids ?? [collectionPlaceID, copySecond])
      {
        'placeId': place,
        'note': '',
        'locationVisibility': 'hidden',
        'available': true,
        'placeName': '当前公开地点',
        'sourceExpiresAt': DateTime.now()
            .toUtc()
            .add(const Duration(seconds: 60))
            .toIso8601String(),
      },
  ],
};
Map<String, dynamic> copyPublicWire({
  bool allowed = true,
  String kind = 'journey',
}) => {
  'id': collectionID,
  'kind': kind,
  'cityId': 'aberdeen-gb',
  'title': 'AUTHOR_TITLE_DO_NOT_COPY',
  'summary': 'AUTHOR_BODY_DO_NOT_COPY',
  'revision': 2,
  'sourceExpiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(seconds: 60))
      .toIso8601String(),
  'entries': [
    for (final id in [collectionPlaceID, copySecond])
      {
        'placeId': id,
        'placeName': id == collectionPlaceID ? '合成海边地点' : '合成阅读地点',
        'note': 'AUTHOR_NOTE_DO_NOT_COPY',
        'locationVisibility': 'place',
        'latitude': 57.1,
        'longitude': -2.1,
      },
  ],
  if (allowed)
    'copyGrant': copyGrantWire(
      ref: CollectionCopyReference(kind, collectionID),
    ),
};

class CopyReadBackFault extends MemoryCollectionCopyPendingStore {
  bool fail = false;
  @override
  Future<void> write(
    String origin,
    String owner,
    CollectionCopyPending p,
  ) async {
    await super.write(origin, owner, p);
    if (p.stage == 'UNKNOWN') fail = true;
  }

  @override
  Future<CollectionCopyPending?> read(String origin, String owner) async {
    if (fail) {
      fail = false;
      throw StateError('injected readback');
    }
    return super.read(origin, owner);
  }
}

CollectionCopyController copyController(
  http.Client client,
  CollectionCopyPendingStore store, {
  CollectionCopyGrant? Function()? grant,
  String? Function()? owner,
  String? Function()? header,
  Listenable? changes,
  bool Function()? current,
}) => CollectionCopyController(
  api: CollectionCopyApi(
    client: client,
    baseUrl: copyBase,
    authorizationHeader: header ?? () => 'Bearer receiver',
  ),
  ownerID: owner ?? () => collectionOwner,
  sourceCurrent: current ?? () => true,
  sourceRef: copyRef,
  grant: grant,
  identityChanges: changes,
  pendingStore: store,
);
void main() {
  test(
    'C3 grant is absent by default, only public place IDs and exact order are eligible',
    () {
      final noGrant = PublicCollectionRecord.fromJson(
        copyPublicWire(allowed: false),
        const CollectionShareReference('journey', collectionID),
      );
      expect(noGrant.copyGrant, isNull);
      final raw = copyPublicWire();
      final parsed = PublicCollectionRecord.fromJson(
        raw,
        const CollectionShareReference('journey', collectionID),
      );
      expect(parsed.copyGrant!.eligiblePlaceIDs, [
        collectionPlaceID,
        copySecond,
      ]);
      for (final wrong in [
        {
          ...copyGrantWire(),
          'sourceRef': {'kind': 'theme_map', 'id': collectionID},
        },
        {...copyGrantWire(), 'publicationRevision': 3},
        {...copyGrantWire(), 'grantVersion': 1.0},
        {...copyGrantWire(), 'grantVersion': 9007199254740991},
        copyGrantWire(ids: [collectionPlaceID]),
        copyGrantWire(ids: []),
        {
          ...copyGrantWire(),
          'eligiblePlaceIds': [copySecond, collectionPlaceID],
        },
        {...copyGrantWire(), 'orderedPlaceIdsSha256': 'a' * 64},
        {...copyGrantWire(), 'note': 'private canary'},
      ])
        expect(
          () => CollectionCopyGrant.fromJson(
            wrong,
            source: copyRef,
            publicationRevision: 2,
            sourceExpiresAt: DateTime.now().toUtc().add(
              const Duration(seconds: 60),
            ),
            publicPlaceIDs: [collectionPlaceID, copySecond],
          ),
          throwsFormatException,
        );
      raw['entries'] = [
        {'note': 'hidden author note', 'locationVisibility': 'hidden'},
        {'note': 'city author note', 'locationVisibility': 'city'},
      ];
      expect(
        () => PublicCollectionRecord.fromJson(
          raw,
          const CollectionShareReference('journey', collectionID),
        ),
        throwsFormatException,
      );
    },
  );
  test(
    'C3 ordered subsequence and closed request never contain source text notes or coordinates',
    () {
      final input = CollectionCopyInput(
        sourceRef: copyRef,
        operationID: copyOp,
        publicationRevision: 2,
        grantVersion: 1,
        orderedPlaceIDs: [collectionPlaceID, copyThird],
        title: '本人标题',
      );
      expect(
        collectionCopySubsequence(input.orderedPlaceIDs, [
          collectionPlaceID,
          copySecond,
          copyThird,
        ]),
        isTrue,
      );
      expect(
        collectionCopySubsequence(
          [copyThird, collectionPlaceID],
          [collectionPlaceID, copySecond, copyThird],
        ),
        isFalse,
      );
      expect(input.toJson().keys.toSet(), {
        'operationId',
        'publicationRevision',
        'grantVersion',
        'orderedPlaceIds',
        'orderedPlaceIdsSha256',
        'title',
      });
      expect(input.selector.toJson().containsKey('title'), isFalse);
      expect(input.selector.toJson().containsKey('orderedPlaceIds'), isFalse);
      for (final forbidden in [
        'summary',
        'note',
        'latitude',
        'longitude',
        'authorTitle',
      ])
        expect(input.toJson().containsKey(forbidden), isFalse);
    },
  );
  test(
    'C3 preview exact selector input digest title count and TTL are required',
    () {
      final input = CollectionCopyInput(
        sourceRef: copyRef,
        operationID: copyOp,
        publicationRevision: 2,
        grantVersion: 1,
        orderedPlaceIDs: [collectionPlaceID],
        title: '本人标题',
      );
      final m = copyPreviewWire(input),
          expiry = DateTime.now().toUtc().add(const Duration(seconds: 25));
      expect(
        CollectionCopyPreview.fromJson(m, input, expiry).input.title,
        '本人标题',
      );
      for (final wrong in [
        {...m, 'entryCount': 2},
        {...m, 'title': 'AUTHOR_TITLE_DO_NOT_COPY'},
        {...m, 'inputSha256': 'a' * 64},
        {
          ...m,
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 40))
              .toIso8601String(),
        },
        {...m, 'grantVersion': 1.0},
        {...m, 'unknown': true},
      ])
        expect(
          () => CollectionCopyPreview.fromJson(wrong, input, expiry),
          throwsFormatException,
        );
    },
  );
  test(
    'C3 receipt new server target and original grant fields; STOP exact tiny shape',
    () {
      final input = CollectionCopyInput(
            sourceRef: copyRef,
            operationID: copyOp,
            publicationRevision: 2,
            grantVersion: 1,
            orderedPlaceIDs: [collectionPlaceID],
            title: '本人标题',
          ),
          s = input.selector;
      expect(
        CollectionCopyReceipt.fromJson(
          copyReceiptWire(s),
          s,
        ).destinationRef!.id,
        copyDestination,
      );
      for (final wrong in [
        {
          ...copyReceiptWire(s),
          'destinationRef': {'kind': 'journey', 'id': collectionID},
        },
        {
          ...copyReceiptWire(s),
          'destinationRef': {'kind': 'journey', 'id': copyOp},
        },
        {...copyReceiptWire(s), 'publicationRevision': 3},
        {...copyReceiptWire(s), 'resultRevision': 2},
      ])
        expect(
          () => CollectionCopyReceipt.fromJson(wrong, s),
          throwsFormatException,
        );
      expect(
        CollectionCopyReceipt.fromJson(
          copyReceiptWire(s, stopped: true),
          s,
        ).status,
        'STOPPED',
      );
      expect(
        () => CollectionCopyReceipt.fromJson({
          ...copyReceiptWire(s, stopped: true),
          'sourceRef': copyRef.toJson(),
        }, s),
        throwsFormatException,
      );
    },
  );
  test(
    'C3 one COPY PUT follows durable preparation, reads independent private target, no ordinary create',
    () async {
      final requests = <http.Request>[];
      final store = MemoryCollectionCopyPendingStore(), g = copyGrant();
      final client = MockClient((r) async {
        requests.add(r);
        expectSync(r.headers['Authorization'], 'Bearer receiver');
        expectSync(
          r.headers.keys.where((k) => k.toLowerCase().contains('workspace')),
          isEmpty,
        );
        if (r.url.path.endsWith('/preview'))
          return collectionResponse(
            copyPreviewWire(copyInput(jsonDecode(r.body))),
          );
        if (r.method == 'PUT') {
          expectSync(store.values.values.single.stage, 'UNKNOWN');
          return collectionResponse(
            copyReceiptWire(copyInput(jsonDecode(r.body)).selector),
          );
        }
        if (r.url.path.endsWith(copyDestination))
          return collectionResponse(copyPrivateWire());
        throw StateError('wrong path ${r.url}');
      });
      addTearDown(client.close);
      final c = copyController(client, store, grant: () => g);
      addTearDown(c.dispose);
      await c.restore();
      c.setTitle('本人新标题');
      final p = (await c.prepare())!;
      expect(requests.length, 1);
      expect((await c.submit(p))!.status, 'SUCCEEDED');
      expect(c.destination!.id, copyDestination);
      expect(c.destination!.revision, 1);
      expect(requests.map((r) => r.method), ['POST', 'PUT', 'GET']);
      expect(
        requests[1].url.path,
        '/v1/public-collections/journey/$collectionID/copy',
      );
      expect(c.canPreview, isFalse);
      expect(store.values, isEmpty);
      expect(c.destination!.summary, isEmpty);
    },
  );
  test(
    'C3 non200401 unknown restart only original GET404 then bodyless STOP, never rePUT',
    () async {
      var puts = 0, gets = 0, stops = 0;
      final store = MemoryCollectionCopyPendingStore(), g = copyGrant();
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return collectionResponse(
            copyPreviewWire(copyInput(jsonDecode(r.body))),
          );
        if (r.method == 'PUT') {
          puts++;
          return collectionResponse(null, code: 401);
        }
        if (r.url.path.endsWith('/stop')) {
          stops++;
          expectSync(r.body, isEmpty);
          return collectionResponse(
            copyReceiptWire(store.values.values.single.selector, stopped: true),
          );
        }
        gets++;
        return collectionResponse(null, code: 404);
      });
      addTearDown(client.close);
      final first = copyController(client, store, grant: () => g);
      await first.restore();
      first.setTitle('OWN_DRAFT_NOT_DURABLE');
      final p = (await first.prepare())!;
      await first.submit(p);
      expect(puts, 1);
      final raw = jsonEncode(store.values.values.single.toJson());
      for (final canary in [
        'OWN_DRAFT_NOT_DURABLE',
        'AUTHOR_NOTE',
        'Bearer',
        'approval',
        'latitude',
        'longitude',
        '"orderedPlaceIds":',
      ])
        expect(raw.contains(canary), isFalse);
      first.dispose();
      final next = copyController(client, store);
      addTearDown(next.dispose);
      await next.restore();
      expect(next.title, isEmpty);
      expect(next.canPreview, isFalse);
      expect(await next.recover(), isNull);
      expect(gets, 1);
      expect(puts, 1);
      expect((await next.recover(stop: true))!.status, 'STOPPED');
      expect(stops, 1);
      expect(puts, 1);
      expect(store.values, isEmpty);
    },
  );
  test(
    'C3 definitive success remains known when current private GET403, retry only reads destination',
    () async {
      final store = MemoryCollectionCopyPendingStore(), g = copyGrant();
      var puts = 0, reads = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return collectionResponse(
            copyPreviewWire(copyInput(jsonDecode(r.body))),
          );
        if (r.method == 'PUT') {
          puts++;
          return collectionResponse(
            copyReceiptWire(copyInput(jsonDecode(r.body)).selector),
          );
        }
        reads++;
        return collectionResponse(null, code: 403);
      });
      addTearDown(client.close);
      final c = copyController(client, store, grant: () => g);
      addTearDown(c.dispose);
      await c.restore();
      c.setTitle('本人标题');
      final p = (await c.prepare())!;
      expect((await c.submit(p))!.status, 'SUCCEEDED');
      expect(c.receipt!.destinationRef!.id, copyDestination);
      expect(c.destination, isNull);
      expect(c.pending, isNull);
      expect(c.canPreview, isFalse);
      await c.loadDestination();
      expect(puts, 1);
      expect(reads, 2);
      expect(c.receipt!.status, 'SUCCEEDED');
    },
  );
  test(
    'C3 source and grant revision ABA permanently retires original preview but keeps own title',
    () async {
      final changes = ChangeNotifier();
      var g = copyGrant(), puts = 0;
      final client = MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return collectionResponse(
          copyPreviewWire(copyInput(jsonDecode(r.body))),
        );
      });
      addTearDown(client.close);
      final c = copyController(
        client,
        MemoryCollectionCopyPendingStore(),
        grant: () => g,
        changes: changes,
      );
      addTearDown(c.dispose);
      addTearDown(changes.dispose);
      await c.restore();
      c.setTitle('本人的标题');
      final p = (await c.prepare())!;
      g = copyGrant(revision: 3, version: 2);
      changes.notifyListeners();
      g = copyGrant();
      changes.notifyListeners();
      expect(c.previewCurrent(p), isFalse);
      expect(c.title, '本人的标题');
      expect(c.selected, isEmpty);
      expect(await c.submit(p), isNull);
      expect(puts, 0);
    },
  );
  test(
    'C3 late actor ABA and same opaque header cannot restore private preview or execute',
    () async {
      final changes = ChangeNotifier(), delayed = Completer<http.Response>();
      final entered = Completer<void>();
      var owner = collectionOwner;
      CollectionCopyInput? input;
      final client = MockClient((r) {
        input = copyInput(jsonDecode(r.body));
        entered.complete();
        return delayed.future;
      });
      addTearDown(client.close);
      final g = copyGrant(),
          c = copyController(
            client,
            MemoryCollectionCopyPendingStore(),
            grant: () => g,
            owner: () => owner,
            changes: changes,
          );
      addTearDown(c.dispose);
      addTearDown(changes.dispose);
      await c.restore();
      c.setTitle('旧本人标题');
      final future = c.prepare();
      await entered.future;
      owner = copyDestination;
      changes.notifyListeners();
      owner = collectionOwner;
      changes.notifyListeners();
      delayed.complete(collectionResponse(copyPreviewWire(input!)));
      expect(await future, isNull);
      expect(c.current, isFalse);
      expect(c.title, isEmpty);
      expect(c.pending, isNull);
    },
  );
  test(
    'C3 normal receiver renewal retains own selection and title but never reuses approval',
    () async {
      final changes = ChangeNotifier();
      var header = 'Bearer first';
      final g = copyGrant();
      final client = MockClient(
        (r) async =>
            collectionResponse(copyPreviewWire(copyInput(jsonDecode(r.body)))),
      );
      addTearDown(client.close);
      final c = copyController(
        client,
        MemoryCollectionCopyPendingStore(),
        grant: () => g,
        header: () => header,
        changes: changes,
      );
      addTearDown(c.dispose);
      addTearDown(changes.dispose);
      await c.restore();
      c.setTitle('本人草稿');
      c.selectPlace(copySecond, false);
      final p = (await c.prepare())!;
      header = 'Bearer renewed';
      changes.notifyListeners();
      expect(c.current, isTrue);
      expect(c.title, '本人草稿');
      expect(c.selected, [collectionPlaceID]);
      expect(c.previewCurrent(p), isFalse);
      expect(await c.submit(p), isNull);
      expect(c.storageReady, isFalse);
    },
  );
  test(
    'C3 pre-PUT storage readback failure is locally repairable, not nonexistent server receipt',
    () async {
      final store = CopyReadBackFault(), g = copyGrant();
      var puts = 0;
      final client = MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return collectionResponse(
          copyPreviewWire(copyInput(jsonDecode(r.body))),
        );
      });
      addTearDown(client.close);
      final c = copyController(client, store, grant: () => g);
      addTearDown(c.dispose);
      await c.restore();
      c.setTitle('本人标题');
      final p = (await c.prepare())!;
      await c.submit(p);
      expect(puts, 0);
      expect(c.locallyPrepared, isTrue);
      expect(await c.recover(), isNull);
      await c.repairPrepared();
      expect(store.values, isEmpty);
      expect(c.canPreview, isTrue);
      expect(puts, 0);
    },
  );
  test(
    'C3 own UTF8 title limit blocks preview yet retains overlimit local title for correction',
    () async {
      final g = copyGrant();
      var requests = 0;
      final client = MockClient((r) async {
        requests++;
        return collectionResponse(null);
      });
      addTearDown(client.close);
      final c = copyController(
        client,
        MemoryCollectionCopyPendingStore(),
        grant: () => g,
      );
      addTearDown(c.dispose);
      await c.restore();
      c.setTitle('中' * 54);
      expect(await c.prepare(), isNull);
      expect(requests, 0);
      expect(c.title, '中' * 54);
      expect(c.error, contains('160'));
    },
  );
}
