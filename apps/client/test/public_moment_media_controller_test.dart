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
  String key(String origin, String owner) => '$origin|$owner';
  @override
  Future<List<PublicMomentMediaPending>> read(String o, String a) async =>
      rows[key(o, a)] ?? [];
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
      expect(c.notice, contains('待随串文'));
      expect(
        (await journal.read(origin, mediaOwner)).single.targetID,
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
      controller.remove(0);
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
