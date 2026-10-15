import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:birdtie_client/src/workspace/public_moment_media.dart';
import 'package:birdtie_client/src/workspace/public_moment_media_controller.dart';
import 'package:birdtie_client/src/workspace/public_moment_media_pending_store.dart';
import 'moment_image_header_test.dart' show headerJPEG;
import 'video_material_picker_test.dart' show syntheticVideoBytes;
import 'public_moment_media_controller_test.dart'
    show
        mediaOwner,
        mediaMoment,
        mediaPNG,
        MediaMemoryJournal,
        mediaOwnedRights;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'lost original stage response remains with its thread entry when another entry has the same server scope',
    () async {
      const entryA = '41000000-0000-4000-8000-000000000003';
      const entryB = '41000000-0000-4000-8000-000000000004';
      final bytes = syntheticVideoBytes(), journal = MediaMemoryJournal();
      final calls = <String>[];
      Map<String, Object?>? originalFact;
      final client = MockClient((request) async {
        calls.add('${request.method} ${request.url.path}');
        if (request.method == 'GET') {
          return http.Response(
            jsonEncode({'data': originalFact}),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        final input = jsonDecode(request.body) as Map<String, dynamic>,
            now = DateTime.now().toUtc();
        expect(input['localScope'], mediaOwner);
        expect(input.containsKey('entryId'), isFalse);
        originalFact = {
          'operationId': input['operationId'],
          'status': 'SUCCEEDED',
          'payloadDigest': humanControlTupleDigest([
            'moment-video-stage-v2',
            input['operationId'],
            mediaOwner,
            publicMomentVideoPurpose,
            'video/mp4',
            sha256.convert(base64Decode(input['contentBase64'])).toString(),
          ]),
          'committedAt': now.toIso8601String(),
          'material': {
            'schemaVersion': 'public-moment-media-stage-v2',
            'kind': 'VIDEO',
            'purpose': publicMomentVideoPurpose,
            'mediaId': mediaMoment,
            'mediaVersion': 1,
            'localScope': mediaOwner,
            'displaySha256': 'b' * 64,
            'displayBytes': 100,
            'width': 320,
            'height': 180,
            'thumbnailSha256': 'c' * 64,
            'thumbnailBytes': 100,
            'validationKind': publicMomentVideoValidation,
            'validationStatus': 'VALIDATED',
            'antivirusScan': 'NOT_RUN',
            'privacyRisk': 'UNKNOWN',
            'preparedAt': now.toIso8601String(),
            'retainUntil': now
                .add(const Duration(minutes: 10))
                .toIso8601String(),
            'video': {
              'mimeType': 'video/mp4',
              'durationMs': 1000,
              'codecVideo': 'h264',
              'codecAudio': 'none',
              'pixelFormat': 'yuv420p',
              'videoSha256': sha256.convert(bytes).toString(),
              'videoBytes': bytes.length,
              'videoPath': '/v1/me/moment-media-materials/$mediaMoment/1/video',
            },
          },
        };
        // The server has an original fact; only its response is lost.
        throw http.ClientException('synthetic response lost after stage fact');
      });
      PublicMomentMediaController entry(String id) =>
          PublicMomentMediaController(
            momentID: mediaMoment,
            expectedMomentRevision: 0,
            ownerID: () => mediaOwner,
            authorizationHeader: () => 'Bearer LOCAL',
            sourceCurrent: () => true,
            materialOnly: true,
            localScopeOverride: mediaOwner,
            journalEntryID: id,
            pendingStore: journal,
            client: client,
            apiBaseUrl: 'https://entry.test',
          );
      final a = entry(entryA);
      await a.load();
      final local = PublicMomentVideoSelection(
        bytes: bytes,
        durationMS: 1000,
        width: 320,
        height: 180,
      );
      a.addSelectedVideo(local);
      a.setRights(0, mediaOwnedRights());
      expect(await a.prepareMaterials(), isNull);
      expect(a.uncertain, isTrue);
      final operation = a.pending.single;
      expect(operation.targetID, mediaOwner);
      expect(operation.journalEntryID, entryA);
      expect(
        PublicMomentMediaPending.decode(operation.toJson()).matches(operation),
        isTrue,
      );
      expect(operation.toJson().containsKey('contentBase64'), isFalse);
      final b = entry(entryB);
      await b.load();
      expect(b.pending, isEmpty);
      await b.recover(operation);
      expect(b.images, isEmpty);
      expect(calls.length, 1);
      a.dispose();
      final restored = entry(entryA);
      await restored.load();
      expect(restored.pending.single.matches(operation), isTrue);
      await restored.recover(restored.pending.single);
      expect(restored.images.single.stage!.localScope, mediaOwner);
      expect(restored.images.single.stage!.mediaID, mediaMoment);
      expect(restored.images.single.rights, isNull);
      expect(restored.pending, isEmpty);
      expect(calls.length, 2);
      expect(
        calls.last,
        'GET /v1/me/moment-media-materials/operations/${operation.operationID}',
      );
      b.dispose();
      restored.dispose();
      local.bytes.fillRange(0, local.bytes.length, 0);
      client.close();
    },
  );
  test(
    'typed public video has its own purpose, exclusive material set, original preparation and private draft continuity',
    () async {
      final bytes = syntheticVideoBytes();
      final poster = headerJPEG(width: 320, height: 180);
      final journal = MediaMemoryJournal();
      var token = 'Bearer FIRST', source = true;
      final methods = <String>[];
      Map<String, dynamic>? stage;
      final client = MockClient((request) async {
        methods.add('${request.method} ${request.url.path}');
        if (request.method == 'GET' &&
            request.url.path.endsWith('/thumbnail')) {
          return http.Response.bytes(
            poster,
            200,
            headers: {'content-type': 'image/jpeg'},
          );
        }
        expect(
          request.url.path,
          startsWith('/v1/me/moment-media-video-materials/operations/'),
        );
        final input = jsonDecode(request.body) as Map<String, dynamic>;
        expect(input['purpose'], publicMomentVideoPurpose);
        expect(input['contentType'], 'video/mp4');
        expect(input.keys.toSet(), {
          'operationId',
          'localScope',
          'purpose',
          'contentType',
          'contentBase64',
        });
        final now = DateTime.now().toUtc();
        stage = {
          'schemaVersion': 'public-moment-media-stage-v2',
          'kind': 'VIDEO',
          'purpose': publicMomentVideoPurpose,
          'mediaId': mediaMoment,
          'mediaVersion': 1,
          'localScope': input['localScope'],
          'displaySha256': sha256.convert(poster).toString(),
          'displayBytes': poster.length,
          'width': 320,
          'height': 180,
          'thumbnailSha256': sha256.convert(poster).toString(),
          'thumbnailBytes': poster.length,
          'validationKind': publicMomentVideoValidation,
          'validationStatus': 'VALIDATED',
          'antivirusScan': 'NOT_RUN',
          'privacyRisk': 'UNKNOWN',
          'preparedAt': now.toIso8601String(),
          'retainUntil': now.add(const Duration(minutes: 10)).toIso8601String(),
          'video': {
            'mimeType': 'video/mp4',
            'durationMs': 1000,
            'codecVideo': 'h264',
            'codecAudio': 'none',
            'pixelFormat': 'yuv420p',
            'videoSha256': sha256.convert(bytes).toString(),
            'videoBytes': bytes.length,
            'videoPath': '/v1/me/moment-media-materials/$mediaMoment/1/video',
          },
        };
        return http.Response(
          jsonEncode({
            'data': {
              'operationId': input['operationId'],
              'status': 'SUCCEEDED',
              'payloadDigest': humanControlTupleDigest([
                'moment-video-stage-v2',
                input['operationId'],
                input['localScope'],
                publicMomentVideoPurpose,
                'video/mp4',
                sha256.convert(base64Decode(input['contentBase64'])).toString(),
              ]),
              'material': stage,
              'committedAt': now.toIso8601String(),
            },
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      PublicMomentMediaController controller({
        required bool Function() current,
        required int revision,
      }) => PublicMomentMediaController(
        momentID: mediaMoment,
        expectedMomentRevision: revision,
        ownerID: () => mediaOwner,
        authorizationHeader: () => token,
        sourceCurrent: current,
        materialOnly: true,
        localScopeOverride: mediaOwner,
        pendingStore: journal,
        client: client,
        apiBaseUrl: 'https://public-video.test',
      );
      final c = controller(current: () => source, revision: 2);
      await c.load();
      final local = PublicMomentVideoSelection(
        bytes: bytes,
        durationMS: 1000,
        width: 320,
        height: 180,
      );
      c.addSelectedVideo(local);
      c.setRights(0, mediaOwnedRights());
      final selected = c.selectionRevision;
      c.addSelectedBytes([mediaPNG]);
      expect(c.images.length, 1);
      expect(c.selectionRevision, selected);
      expect(c.error, contains('删除视频'));
      var changed = 0;
      c.addListener(() => changed++);
      token = 'Bearer RENEWED';
      c.observe();
      expect(changed, 1);
      expect(c.selectionRevision, selected);
      expect(c.images.single.bytes, bytes);
      // The old source has just changed parent revision. A detached local copy
      // must use its captured owner rather than call old.current and erase it.
      source = false;
      final next = controller(current: () => true, revision: 3);
      c.copyDraftTo(next);
      final copy = next.images.single.bytes!;
      expect(identical(copy, c.images.single.bytes), isFalse);
      c.dispose();
      expect(copy, bytes);
      await next.load();
      final prepared = await next.prepareMaterials();
      expect(prepared!.single.mediaID, mediaMoment);
      expect(next.images.single.stage!.isVideo, isTrue);
      expect(next.images.single.bytes, isNull);
      expect(next.images.single.poster, poster);
      expect(methods.length, 2);
      expect(next.pending, isEmpty);
      expect(
        await journal.read('https://public-video.test', mediaOwner),
        isEmpty,
      );
      expect(
        () => PublicMomentMediaStage.decode({
          ...stage!,
          'purpose': 'HUMAN_PRIVATE_VIDEO_MATERIAL',
        }),
        throwsFormatException,
      );
      expect(
        local.toPreparationJSON(mediaOwner, mediaMoment)['purpose'],
        publicMomentVideoPurpose,
      );
      final ownedPoster = next.images.single.poster!;
      next.dispose();
      expect(ownedPoster.every((b) => b == 0), isTrue);
      local.bytes.fillRange(0, local.bytes.length, 0);
      client.close();
    },
  );
}
