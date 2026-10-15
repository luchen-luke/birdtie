import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:birdtie_client/src/workspace/public_moment_media.dart';
import 'package:birdtie_client/src/workspace/public_moment_media_controller.dart';
import 'package:birdtie_client/src/workspace/public_moment_media_pending_store.dart';

const mediaOwner = '41000000-0000-4000-8000-000000000001';
const mediaMoment = '41000000-0000-4000-8000-000000000002';
final mediaPNG = base64Decode(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a1X8AAAAASUVORK5CYII=',
);
Map<String, Object> mediaEmptySet({bool own = true}) {
  final now = DateTime.now().toUtc();
  return {
    'schemaVersion': 'public-moment-media-set-v1',
    'momentId': mediaMoment,
    'momentRevision': 2,
    'setVersion': 0,
    'status': 'EMPTY',
    'attachments': [],
    'sourceVersion': 'a' * 64,
    'observedAt': now.toIso8601String(),
    'validUntil': now.add(const Duration(seconds: 60)).toIso8601String(),
    'canReplace': own,
    'canDelete': false,
  };
}

class MediaMemoryJournal implements PublicMomentMediaPendingStore {
  final rows = <String, List<PublicMomentMediaPending>>{};
  bool failReads = false;
  String key(String origin, String owner) => '$origin|$owner';
  @override
  Future<List<PublicMomentMediaPending>> read(String o, String a) async {
    if (failReads) throw StateError('synthetic journal read failure');
    return rows[key(o, a)] ?? [];
  }

  @override
  Future<void> write(String o, String a, PublicMomentMediaPending p) async {
    rows[key(o, a)] = [...rows[key(o, a)] ?? [], p];
  }

  @override
  Future<void> remove(String o, String a, PublicMomentMediaPending p) async {
    rows[key(o, a)] = (rows[key(o, a)] ?? [])
        .where((r) => r.operationID != p.operationID)
        .toList();
  }

  @override
  Future<void> saveStagedSelection(
    String o,
    String a,
    String scope,
    String entry,
    List<PublicMomentMediaPending> expected,
    List<PublicMomentMediaPending> selection,
  ) async {
    rows[key(o, a)] = replaceStagedMediaSelection(
      rows[key(o, a)] ?? [],
      scope,
      entry,
      expected,
      selection,
    );
  }
}

EntityCoverRights mediaOwnedRights() => EntityCoverRights(
  basis: 'OWNED',
  attribution: '',
  licenseReference: '',
  retentionUntil: DateTime.now().toUtc().add(const Duration(days: 30)),
  canRetain: true,
  canPublish: true,
);
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'thread material preparation has no published parent or SET call and isolates opaque scope',
    () async {
      final journal = MediaMemoryJournal();
      const origin = 'https://media.test';
      await journal.write(
        origin,
        mediaOwner,
        PublicMomentMediaPending(
          kind: 'STAGE',
          operationID: mediaMoment,
          targetID: '41000000-0000-4000-8000-000000000008',
          digest: 'a' * 64,
          momentRevision: 0,
          setVersion: 0,
          action: 'PREPARE',
          media: const [],
        ),
      );
      final methods = <String>[];
      final client = MockClient((request) async {
        methods.add('${request.method} ${request.url.path}');
        final input = jsonDecode(request.body) as Map<String, dynamic>,
            now = DateTime.now().toUtc();
        return http.Response(
          jsonEncode({
            'data': {
              'operationId': input['operationId'],
              'status': 'SUCCEEDED',
              'payloadDigest': humanControlTupleDigest([
                'moment-media-stage-v1',
                input['operationId'],
                input['localScope'],
                input['contentType'],
                sha256.convert(base64Decode(input['contentBase64'])).toString(),
              ]),
              'committedAt': now.toIso8601String(),
              'material': {
                'mediaId': mediaMoment,
                'mediaVersion': 1,
                'localScope': input['localScope'],
                'displaySha256': 'b' * 64,
                'displayBytes': 100,
                'width': 1,
                'height': 1,
                'thumbnailSha256': 'c' * 64,
                'thumbnailBytes': 100,
                'validationKind': publicMomentMediaValidation,
                'validationStatus': 'VALIDATED',
                'antivirusScan': 'NOT_RUN',
                'privacyRisk': 'UNKNOWN',
                'preparedAt': now.toIso8601String(),
                'retainUntil': now
                    .add(const Duration(minutes: 10))
                    .toIso8601String(),
              },
            },
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      final c = PublicMomentMediaController(
        momentID: mediaOwner,
        expectedMomentRevision: 0,
        ownerID: () => mediaOwner,
        authorizationHeader: () => 'Bearer LOCAL',
        sourceCurrent: () => true,
        client: client,
        pendingStore: journal,
        apiBaseUrl: origin,
        materialOnly: true,
        localScopeOverride: mediaOwner,
      );
      await c.load();
      expect(methods, isEmpty);
      expect(c.pending, isEmpty);
      c.addSelectedBytes([mediaPNG]);
      c.setRights(0, mediaOwnedRights());
      final materials = await c.prepareMaterials();
      expect(materials!.single.mediaID, mediaMoment);
      expect(methods.length, 1);
      expect(
        methods.single,
        startsWith('PUT /v1/me/moment-media-materials/operations/'),
      );
      expect(c.state, isNull);
      expect(c.images.single.bytes, isNull);
      expect(c.notice, contains('待随动态'));
      expect(
        (await journal.read(
          origin,
          mediaOwner,
        )).where((p) => !p.staged).single.targetID,
        '41000000-0000-4000-8000-000000000008',
      );
      c.dispose();
      client.close();
    },
  );
  test(
    'nine local selections require each explicit rights declaration and retire owned RAM',
    () async {
      var owner = mediaOwner, header = 'Bearer FIRST';
      final client = MockClient(
        (r) async => http.Response(
          jsonEncode({'data': mediaEmptySet()}),
          200,
          headers: {'content-type': 'application/json'},
        ),
      );
      final controller = PublicMomentMediaController(
        momentID: mediaMoment,
        expectedMomentRevision: 2,
        ownerID: () => owner,
        authorizationHeader: () => header,
        sourceCurrent: () => true,
        client: client,
        pendingStore: MediaMemoryJournal(),
        apiBaseUrl: 'https://media.test',
      );
      await controller.load();
      controller.addSelectedBytes(
        List.generate(9, (_) => Uint8List.fromList(mediaPNG)),
      );
      expect(controller.images.length, 9);
      expect(controller.canPreview, isFalse);
      for (var i = 0; i < 9; i++) controller.setRights(i, mediaOwnedRights());
      expect(controller.canPreview, isTrue);
      final owned = controller.images.first.bytes!;
      await controller.remove(0);
      expect(owned.every((b) => b == 0), isTrue);
      expect(mediaPNG.any((b) => b != 0), isTrue);
      final retained = controller.images.first.bytes!;
      header = 'Bearer RENEWED';
      expect(controller.current, isTrue);
      expect(retained.any((b) => b != 0), isTrue);
      owner = '41000000-0000-4000-8000-000000000009';
      expect(controller.current, isFalse);
      expect(retained.every((b) => b == 0), isTrue);
      expect(controller.images, isEmpty);
      controller.dispose();
      client.close();
    },
  );
  test(
    'lost stage result persists selectors only and never blindly repeats upload',
    () async {
      var puts = 0;
      final journal = MediaMemoryJournal();
      final client = MockClient((r) async {
        if (r.method == 'PUT') {
          puts++;
          return http.Response(
            '{}',
            503,
            headers: {'content-type': 'application/json'},
          );
        }
        return http.Response(
          jsonEncode({'data': mediaEmptySet()}),
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      final c = PublicMomentMediaController(
        momentID: mediaMoment,
        expectedMomentRevision: 2,
        ownerID: () => mediaOwner,
        authorizationHeader: () => 'Bearer LOCAL',
        sourceCurrent: () => true,
        client: client,
        pendingStore: journal,
        apiBaseUrl: 'https://media.test',
      );
      await c.load();
      c.addSelectedBytes([mediaPNG]);
      c.setRights(0, mediaOwnedRights());
      await c.prepare();
      expect(puts, 1);
      expect(c.uncertain, isTrue);
      expect(c.images.single.bytes!.any((b) => b != 0), isTrue);
      await c.prepare();
      expect(puts, 1);
      final pending = journal.rows.values.single.single;
      final serialized = jsonEncode(pending.toJson());
      expect(serialized, contains('moment-media-journal-v1'));
      for (final forbidden in [
        'contentBase64',
        'approval',
        'authorization',
        'rights',
      ]) {
        expect(serialized, isNot(contains(forbidden)));
      }
      expect(pending.kind, 'STAGE');
      c.dispose();
      client.close();
    },
  );
  test(
    'secure journal is origin owner separated and rejects arbitrary approval fields',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      const store = SecurePublicMomentMediaPendingStore();
      final p = PublicMomentMediaPending(
        kind: 'STAGE',
        operationID: mediaMoment,
        targetID: mediaOwner,
        digest: 'a' * 64,
        momentRevision: 0,
        setVersion: 0,
        action: 'PREPARE',
        media: const [],
      );
      await store.write('https://media.test', mediaOwner, p);
      expect(
        (await store.read('https://media.test', mediaOwner)).single.matches(p),
        isTrue,
      );
      expect(await store.read('https://other.test', mediaOwner), isEmpty);
      expect(
        () => PublicMomentMediaPending.decode({
          ...p.toJson(),
          'approval': 'fake',
        }),
        throwsFormatException,
      );
    },
  );
  test(
    'successful staged selection survives reopening in saved order without rights or upload replay',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      const journal = SecurePublicMomentMediaPendingStore(),
          origin = 'https://stage.test';
      const entryA = '41000000-0000-4000-8000-000000000010',
          entryB = '41000000-0000-4000-8000-000000000011';
      final receipts = <String, Map<String, Object>>{};
      var puts = 0, gets = 0;
      final client = MockClient((request) async {
        final op = request.url.pathSegments.last;
        if (request.method == 'PUT') {
          puts++;
          final input = jsonDecode(request.body) as Map<String, dynamic>;
          receipts[op] = stageFixture(
            op,
            input['localScope'],
            humanControlTupleDigest([
              'moment-media-stage-v1',
              op,
              input['localScope'],
              input['contentType'],
              sha256.convert(base64Decode(input['contentBase64'])).toString(),
            ]),
          );
        } else {
          gets++;
        }
        return http.Response(
          jsonEncode({'data': receipts[op]}),
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      PublicMomentMediaController open(String entry) =>
          PublicMomentMediaController(
            momentID: entry,
            expectedMomentRevision: 0,
            materialOnly: true,
            localScopeOverride: mediaMoment,
            journalEntryID: entry,
            ownerID: () => mediaOwner,
            authorizationHeader: () => 'Bearer synthetic',
            sourceCurrent: () => true,
            client: client,
            pendingStore: journal,
            apiBaseUrl: origin,
          );
      final first = open(entryA);
      await first.load();
      first.addSelectedBytes([mediaPNG, mediaPNG]);
      first.setRights(0, mediaOwnedRights());
      first.setRights(1, mediaOwnedRights());
      final selected = (await first.prepareMaterials())!;
      expect(first.pending, isEmpty);
      await first.reorder(0, 1);
      final orderedIDs = selected.reversed.map((p) => p.mediaID).toList();
      final other = open(entryB);
      await other.load();
      other.addSelectedBytes([mediaPNG]);
      other.setRights(0, mediaOwnedRights());
      final otherID = (await other.prepareMaterials())!.single.mediaID;
      first.dispose();
      other.dispose();
      expect(puts, 3);
      final raw = (await const FlutterSecureStorage().readAll()).values.single;
      expect(raw, isNot(contains('rights')));
      expect(raw, isNot(contains('contentBase64')));
      final restored = open(entryA);
      await restored.load();
      expect(restored.images.map((p) => p.stage!.mediaID), orderedIDs);
      expect(
        restored.images.every((p) => p.rights == null && p.bytes == null),
        isTrue,
      );
      expect(restored.canPreview, isFalse);
      expect(restored.pending, isEmpty);
      expect(restored.hasUnresolvedStagedMaterials, isFalse);
      restored.setRights(0, mediaOwnedRights());
      restored.setRights(1, mediaOwnedRights());
      expect(
        (await restored.prepareMaterials())!.map((p) => p.mediaID),
        orderedIDs,
      );
      expect(puts, 3);
      await restored.remove(0);
      restored.dispose();
      final again = open(entryA);
      await again.load();
      expect(again.images.single.stage!.mediaID, orderedIDs.last);
      final isolated = open(entryB);
      await isolated.load();
      expect(isolated.images.single.stage!.mediaID, otherID);
      expect(gets, 4);
      final publishedOperations = again.stagedOperationIDs;
      again.addSelectedBytes([mediaPNG]);
      again.setRights(0, mediaOwnedRights());
      again.setRights(1, mediaOwnedRights());
      await again.prepareMaterials();
      final newDraftOperation = again.stagedOperationIDs.last;
      expect(puts, 4);
      again.dispose();
      expect(
        await again.releaseStagedAfterPublication(
          operationIDs: publishedOperations,
        ),
        isTrue,
      );
      expect(
        (await journal.read(
          origin,
          mediaOwner,
        )).where((p) => p.journalEntryID == entryB).single.journalEntryID,
        entryB,
      );
      expect(
        (await journal.read(
          origin,
          mediaOwner,
        )).where((p) => p.journalEntryID == entryA).single.operationID,
        newDraftOperation,
      );
      isolated.dispose();
      client.close();
    },
  );
  test(
    'expired successful stages remain visible until explicit removal',
    () async {
      final journal = MediaMemoryJournal();
      final reference = PublicMomentMediaPending(
        kind: 'STAGE',
        operationID: mediaOwner,
        targetID: mediaMoment,
        digest: 'd' * 64,
        momentRevision: 0,
        setVersion: 0,
        action: 'PREPARE',
        media: const [],
      );
      const origin = 'https://expired-stage.test';
      await journal.write(origin, mediaOwner, reference);
      await journal.saveStagedSelection(
        origin,
        mediaOwner,
        mediaMoment,
        '',
        [],
        [reference.asStaged(0)],
      );
      var reads = 0;
      final client = MockClient((request) async {
        expect(request.method, 'GET');
        reads++;
        return http.Response(
          jsonEncode({
            'data': stageFixture(
              mediaOwner,
              mediaMoment,
              reference.digest,
              preparedAt: DateTime.now().toUtc().subtract(
                const Duration(minutes: 11),
              ),
            ),
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      final c = PublicMomentMediaController(
        momentID: mediaMoment,
        expectedMomentRevision: 1,
        materialOnly: true,
        localScopeOverride: mediaMoment,
        ownerID: () => mediaOwner,
        authorizationHeader: () => 'Bearer synthetic',
        sourceCurrent: () => true,
        client: client,
        pendingStore: journal,
        apiBaseUrl: origin,
      );
      await c.load();
      expect(reads, 1);
      expect(c.images.single.stage!.current, isFalse);
      expect(c.hasUnresolvedStagedMaterials, isTrue);
      journal.failReads = true;
      await c.load();
      expect(c.journalReady, isFalse);
      expect(c.hasUnresolvedStagedMaterials, isTrue);
      journal.failReads = false;
      await c.load();
      c.setRights(0, mediaOwnedRights());
      expect(await c.prepareMaterials(), isNull);
      expect(c.images.length, 1);
      await c.remove(0);
      expect(c.images, isEmpty);
      expect(c.hasUnresolvedStagedMaterials, isFalse);
      expect(await journal.read(origin, mediaOwner), isEmpty);
      c.dispose();
      client.close();
    },
  );
  test(
    'streamed content refuses oversized body before accepting pixels',
    () async {
      final now = DateTime.now().toUtc(), bytes = Uint8List.fromList([1, 2, 3]);
      final image = PublicMomentMediaAttachment.decode(
        {
          'mediaId': mediaOwner,
          'mediaVersion': 1,
          'position': 0,
          'width': 1,
          'height': 1,
          'displayBytes': 3,
          'displaySha256': sha256.convert(bytes).toString(),
          'thumbnailBytes': 3,
          'thumbnailSha256': sha256.convert(bytes).toString(),
          'displayPath': momentMediaPath(
            mediaMoment,
            2,
            1,
            mediaOwner,
            1,
            'display',
          ),
          'thumbnailPath': momentMediaPath(
            mediaMoment,
            2,
            1,
            mediaOwner,
            1,
            'thumbnail',
          ),
          'rightsDigest': 'b' * 64,
          'attribution': '',
          'validationKind': publicMomentMediaValidation,
          'validationStatus': 'VALIDATED',
          'antivirusScan': 'NOT_RUN',
          'privacyRisk': 'UNKNOWN',
          'expiresAt': now.add(const Duration(seconds: 30)).toIso8601String(),
        },
        mediaMoment,
        2,
        1,
        0,
        now.add(const Duration(seconds: 30)),
      );
      final client = MockClient(
        (r) async => http.Response.bytes(
          [1, 2, 3, 4],
          200,
          headers: {'content-type': 'image/jpeg'},
        ),
      );
      final api = PublicMomentMediaSetAPI(
        client: client,
        apiBaseUrl: 'https://media.test',
      );
      await expectLater(api.content(image), throwsFormatException);
      api.dispose();
      client.close();
    },
  );
}

Map<String, Object> stageFixture(
  String op,
  String scope,
  String digest, {
  DateTime? preparedAt,
}) {
  final now = preparedAt ?? DateTime.now().toUtc();
  return {
    'operationId': op,
    'status': 'SUCCEEDED',
    'payloadDigest': digest,
    'committedAt': now.toIso8601String(),
    'material': {
      'mediaId': op,
      'mediaVersion': 1,
      'localScope': scope,
      'displaySha256': 'b' * 64,
      'displayBytes': 100,
      'width': 1,
      'height': 1,
      'thumbnailSha256': 'c' * 64,
      'thumbnailBytes': 100,
      'validationKind': publicMomentMediaValidation,
      'validationStatus': 'VALIDATED',
      'antivirusScan': 'NOT_RUN',
      'privacyRisk': 'UNKNOWN',
      'preparedAt': now.toIso8601String(),
      'retainUntil': now.add(const Duration(minutes: 10)).toIso8601String(),
    },
  };
}
