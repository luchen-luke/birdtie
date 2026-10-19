import 'dart:async';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_media_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:http/http.dart' as http;
import 'entity_cover_test.dart' show coverJPEG;

class _ObservedMapAPI extends OwnedMapAPI {
  _ObservedMapAPI({required super.client, required super.apiBaseUrl});
  final failures = <Object>[];
  @override
  Future<Uint8List> content(
    OwnedMapCover cover, {
    required String authorization,
    required String origin,
    required bool Function() current,
    Future<void>? abortTrigger,
  }) async {
    try {
      return await super.content(
        cover,
        authorization: authorization,
        origin: origin,
        current: current,
        abortTrigger: abortTrigger,
      );
    } catch (error) {
      failures.add(error);
      rethrow;
    }
  }
}

void main() {
  testWidgets(
    '24 distinct media slots share anchors while preserving each original lease',
    (t) async {
      var clock = DateTime.now().toUtc();
      var permitted = true;
      final jpeg = coverJPEG();
      final digest = sha256.convert(jpeg).toString();
      final pending = <String, Completer<http.Response>>{};
      final paths = <String>[];
      var authMismatch = 0, duplicateTransfer = 0;
      final api = _ObservedMapAPI(
        apiBaseUrl: () => 'https://isolated.invalid',
        client: MockClient((request) {
          final path = request.url.path;
          // Record here; Flutter assertions run outside the guarded pump.
          if (pending.containsKey(path)) duplicateTransfer++;
          if (request.headers['authorization'] != 'Bearer synthetic-density83')
            authMismatch++;
          paths.add(path);
          final hold = Completer<http.Response>();
          pending[path] = hold;
          return hold.future;
        }),
      );
      final controller = OwnedMapMediaController(
        api: api,
        current: () => permitted,
        authorizationHeader: () => 'Bearer synthetic-density83',
        now: () => clock,
      );
      final until = clock.add(const Duration(seconds: 45));
      OwnedMapPin pin(String id, int asset, {DateTime? deadline}) =>
          OwnedMapPin(
            placeID: '',
            markerID: 'anchor:$id',
            anchorID: id,
            kind: 'MOMENT_ANCHOR',
            momentID: 'moment-$asset',
            name: '合成媒体$asset',
            categoryCode: 'moment',
            cityID: '',
            latitude: 31.2,
            longitude: 121.4,
            visibilitySources: const ['PUBLIC'],
            sourceVersion: 'source-$id',
            momentCount: 1,
            moments: const [],
            momentsTruncated: false,
            observedAt: clock,
            validUntil: deadline ?? until,
            cover: OwnedMapCover(
              kind: 'MOMENT_MEDIA',
              id: 'asset-$asset',
              revision: 2,
              visibilitySource: 'PUBLIC',
              mimeType: 'image/jpeg',
              width: 320,
              height: 180,
              byteCount: jpeg.length,
              sha256: digest,
              sourceVersion: 'asset-source-$asset',
              contentPath: '/fixture/asset/$asset',
            ),
          );
      // 36 authorized anchors share one image. They must not spend 36 slots
      // and exclude the other 23 allowed image keys, including the selection.
      final shared = [
        for (var i = 0; i < 36; i++)
          pin(
            'a-${i.toString().padLeft(2, '0')}',
            0,
            deadline: i == 0 ? clock.add(const Duration(seconds: 5)) : until,
          ),
      ];
      final selected = pin('z-selected', 24);
      final pins = [
        ...shared,
        for (var i = 1; i < 24; i++)
          pin('b-${i.toString().padLeft(2, '0')}', i),
        selected,
      ];
      OwnedMapView view(List<OwnedMapPin> items) => OwnedMapView(
        viewerAccountID: 'synthetic',
        cityID: '',
        bounds: const MapBounds(west: 120, south: 30, east: 122, north: 32),
        observedAt: clock,
        validUntil: until,
        sourceVersion: 'synthetic-view',
        items: items,
        truncated: false,
      );
      try {
        controller.sync(
          view(pins),
          devicePixelRatio: 1,
          selectedMarkerID: selected.canonicalMarkerID,
        );
        await t.pump();
        expect(api.failures, isEmpty);
        expect(authMismatch, 0);
        expect(duplicateTransfer, 0);
        expect(
          paths.length,
          4,
          reason: 'The real HTTP client must hold four header futures',
        );
        expect(controller.inFlight, 4);
        for (var round = 0; round < 120; round++) {
          final batch = pending.values.toList();
          pending.clear();
          for (final response in batch) {
            response.complete(
              http.Response.bytes(
                Uint8List.fromList(jpeg),
                200,
                headers: {
                  'content-type': 'image/jpeg',
                  'cache-control': 'no-store',
                  'x-content-type-options': 'nosniff',
                  'content-length': jpeg.length.toString(),
                },
              ),
            );
          }
          await t.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)),
          );
          await t.pump(const Duration(milliseconds: 10));
          if (controller.inFlight == 0 && pending.isEmpty) break;
        }
        expect(controller.inFlight, 0);
        expect(api.failures, isEmpty);
        expect(authMismatch, 0);
        expect(duplicateTransfer, 0);
        expect(controller.requests, 24);
        expect(paths.toSet(), hasLength(24));
        expect(paths, contains('/fixture/asset/24'));
        expect(controller.cachedItems, 24);
        expect(controller.cachedBytes, lessThanOrEqualTo(24 * 1024 * 1024));
        expect(controller.inFlightPeak, 4);
        expect(
          shared.every((p) => controller.decorate(p).mediaPixels != null),
          isTrue,
        );
        final short = controller.decorate(shared.first).mediaPixels!;
        final long = controller.decorate(shared[1]).mediaPixels!;
        expect(identical(short.normalPNG, long.normalPNG), isTrue);
        expect(short.validUntil, shared.first.validUntil);
        expect(long.validUntil, until);
        expect(controller.decorate(selected).mediaPixels, isNotNull);
        clock = clock.add(const Duration(seconds: 6));
        expect(
          short.current(),
          isFalse,
          reason:
              'Another current anchor of the same image cannot extend this original pin lease',
        );
        expect(controller.decorate(shared.first).mediaPixels, isNull);
        expect(long.current(), isTrue);
        controller.sync(
          view(pins.reversed.toList()),
          devicePixelRatio: 1,
          selectedMarkerID: selected.canonicalMarkerID,
        );
        await t.pump();
        expect(
          controller.requests,
          24,
          reason:
              'Reordering and the removal of one expired shared grant reuse current verified pixels',
        );
        expect(controller.cachedItems, 24);
        expect(short.current(), isFalse);
        expect(controller.decorate(shared[1]).mediaPixels, isNotNull);
        permitted = false;
        controller.sync(view(pins), devicePixelRatio: 1);
        expect(long.current(), isFalse);
        expect(controller.cachedItems, 0);
        expect(controller.decorate(selected).mediaPixels, isNull);
        expect(controller.requests, 24);
      } finally {
        controller.dispose();
        for (final hold in pending.values) {
          if (!hold.isCompleted) hold.complete(http.Response('', 503));
        }
        api.dispose();
        await t.pump();
      }
    },
  );
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
