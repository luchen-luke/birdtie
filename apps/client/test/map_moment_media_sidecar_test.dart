import 'dart:convert';
import 'dart:io';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_media_controller.dart';
import 'package:birdtie_client/src/workspace/public_moment_media.dart';
import 'map_media_controller_test.dart' show syncMedia, mediaWait;

void main() {
  testWidgets(
    'first current ordered attachment decorates its real point and stale identity drops pixels',
    (t) async {
      final bytes = (await t.runAsync(
        () => File('test/fixtures/map-media/isolation-a.jpg').readAsBytes(),
      ))!;
      const moment = '22345678-1234-1234-1234-000000000001';
      const media = '32345678-1234-1234-1234-000000000001';
      final at = DateTime.now().toUtc(),
          until = DateTime.now().toUtc().add(const Duration(seconds: 50));
      final sha = sha256.convert(bytes).toString();
      final thumb = momentMediaPath(moment, 2, 1, media, 1, 'thumbnail');
      var metadataReads = 0, thumbnailReads = 0;
      final client = MockClient((request) async {
        if (request.url.path == '/v1/moments/$moment/public-media') {
          ++metadataReads;
          return http.Response(
            jsonEncode({
              'data': {
                'schemaVersion': 'public-moment-media-set-v1',
                'momentId': moment,
                'momentRevision': 2,
                'setVersion': 1,
                'status': 'READY',
                'sourceVersion': 'a' * 64,
                'observedAt': at.toIso8601String(),
                'validUntil': until.toIso8601String(),
                'canReplace': false,
                'canDelete': false,
                'attachments': [
                  {
                    'mediaId': media,
                    'mediaVersion': 1,
                    'position': 0,
                    'width': 320,
                    'height': 180,
                    'displayBytes': bytes.length,
                    'displaySha256': sha,
                    'thumbnailBytes': bytes.length,
                    'thumbnailSha256': sha,
                    'displayPath': momentMediaPath(
                      moment,
                      2,
                      1,
                      media,
                      1,
                      'display',
                    ),
                    'thumbnailPath': thumb,
                    'rightsDigest': 'b' * 64,
                    'attribution': 'Synthetic fixture',
                    'validationKind': publicMomentMediaValidation,
                    'validationStatus': 'VALIDATED',
                    'antivirusScan': 'NOT_RUN',
                    'privacyRisk': 'UNKNOWN',
                    'expiresAt': until.toIso8601String(),
                  },
                ],
              },
            }),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        if (request.url.path == thumb) {
          ++thumbnailReads;
          return http.Response.bytes(
            bytes,
            200,
            headers: {'content-type': 'image/jpeg'},
          );
        }
        return http.Response('', 404);
      });
      final c = MapMediaController(
        client: client,
        apiBaseUrl: 'https://unit.invalid',
      );
      const entity = MapEntity(
        id: 'moment:$moment',
        kind: MapEntityKind.moment,
        title: 'Synthetic publication',
        subtitle: 'Real place anchor',
        latitude: 57.2,
        longitude: -2.1,
        placeID: moment,
        publicLocation: true,
        sourceVersion: '2',
      );
      syncMedia(c, [entity]);
      await mediaWait(t, () => metadataReads == 1 && c.inFlight == 0);
      syncMedia(c, [entity]);
      await mediaWait(t, () => c.decorate(entity).mediaPixels != null);
      final decorated = c.decorate(entity);
      expect(decorated.latitude, entity.latitude);
      expect(decorated.longitude, entity.longitude);
      expect(thumbnailReads, 1);
      for (var n = 0; n < 4; n++) syncMedia(c, [entity]);
      expect(metadataReads, 1);
      expect(thumbnailReads, 1);
      syncMedia(c, [], owner: null, token: null, generation: 2);
      expect(c.decorate(entity).mediaPixels, isNull);
      await t.pump();
      c.dispose();
      await t.pump();
    },
  );
}
