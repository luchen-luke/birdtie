import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_media_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:http/http.dart' as http;

void main() {
  testWidgets(
    'owned Place and cluster covers never start map transfer or decode',
    (t) async {
      var calls = 0;
      final api = OwnedMapAPI(
        apiBaseUrl: () => 'https://isolated.invalid',
        client: MockClient((_) async {
          calls++;
          return http.Response('', 500);
        }),
      );
      final now = DateTime.now().toUtc();
      const cover = OwnedMapCover(
        kind: 'PLACE_IMAGE',
        id: 'fixture-image',
        revision: 1,
        visibilitySource: 'PUBLIC',
        mimeType: 'image/jpeg',
        width: 160,
        height: 90,
        byteCount: 100,
        sha256: 'fixture',
        sourceVersion: 'fixture',
        contentPath: '/fixture/content',
      );
      final pins = [
        for (final kind in ['PLACE_CONTENT', 'CLUSTER'])
          OwnedMapPin(
            placeID: 'fixture-place',
            name: '地点',
            categoryCode: 'cafe',
            cityID: null,
            latitude: 31,
            longitude: 121,
            visibilitySources: ['PUBLIC'],
            sourceVersion: kind,
            momentCount: 5,
            moments: const [],
            momentsTruncated: false,
            cover: cover,
            kind: kind,
            observedAt: now,
            validUntil: now.add(const Duration(seconds: 30)),
          ),
      ];
      final controller = OwnedMapMediaController(
        api: api,
        current: () => true,
        authorizationHeader: () => 'Bearer isolated',
        now: () => now,
      );
      controller.sync(
        OwnedMapView(
          viewerAccountID: 'fixture',
          cityID: '',
          bounds: const MapBounds(west: 120, south: 30, east: 122, north: 32),
          observedAt: now,
          validUntil: now.add(const Duration(seconds: 30)),
          sourceVersion: 'fixture',
          items: pins,
          truncated: false,
        ),
        devicePixelRatio: 3.25,
      );
      await t.pump();
      expect(calls, 0);
      expect(controller.decodes, 0);
      expect(controller.cachedItems, 0);
      for (final p in pins) expect(controller.decorate(p).mediaPixels, isNull);
      controller.dispose();
      api.dispose();
      await t.pump();
    },
  );
}
