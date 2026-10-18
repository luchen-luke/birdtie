import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:birdtie_client/src/city/amap_city_map_io.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

String _id(int n) => '17000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
String _hash(String v) => sha256.convert(utf8.encode(v)).toString();

void main() {
  testWidgets(
    'personal map grants keep source leases, page all moments, verify pixels, and retire identity ABA',
    (t) async {
      final jpeg = File(
        'test/fixtures/map-media/isolation-a.jpg',
      ).readAsBytesSync();
      final digest = sha256.convert(jpeg).toString();
      final markerJpeg = base64Decode(
        '/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgNDRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjL/wAARCABaAKADASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwDiKKKKk8sKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigD//2Q==',
      );
      final markerDigest = sha256.convert(markerJpeg).toString();
      final identity = ValueNotifier(0);
      var now = DateTime.now().toUtc();
      var owner = _id(9001), origin = 'https://owned-map.test';
      String? organization;
      var allowed = true, corrupt = false, source = _hash('first-source');
      const bounds = MapBounds(west: -2.3, south: 57, east: -1.9, north: 57.4);
      final calls = <http.Request>[];
      Completer<http.Response>? delayed;
      bool delayNextMap = false;
      DateTime? pageUntil;
      DateTime? heldMapUntil;
      Map<String, Object?> lease() => {
        'observedAt': now.toIso8601String(),
        'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
      };
      String scope(int n) =>
          n == 2 || n == 136 ? 'SELF_PRIVATE' : 'CIVU_PUBLIC_MOMENT';
      Map<String, Object?> selector(int n) => {
        'id': _id(n),
        'revision': 1,
        'visibilitySource': scope(n),
        'detailPath': '/v1/me/map-content/moments/${_id(n)}',
      };
      Map<String, Object?> media(int n) => {
        'kind': 'IMAGE',
        'id': _id(8001),
        'revision': 1,
        'mimeType': 'image/jpeg',
        'width': 320,
        'height': 180,
        'byteCount': jpeg.length,
        'sha256': digest,
        'sourceVersion': source,
        'contentPath':
            '/v1/me/map-content/moments/${_id(n)}/media/${_id(8001)}/content?sourceVersion=$source',
        'durationMs': null,
        'videoCodec': null,
        'audioCodec': null,
        'poster': null,
      };
      Map<String, Object?> detail(int n) => {
        'schemaVersion': 'map-content-moment-v1',
        'viewerAccountId': owner,
        'id': _id(n),
        'revision': 1,
        'visibilitySource': scope(n),
        'canEdit': n == 2,
        'status': 'draft',
        'visibility': 'private',
        'title': '',
        'body': '  原文 $n\n',
        'createdAt': '2026-01-01T00:00:00Z',
        'locations': [
          {'placeId': _id(5000), 'name': '合成地点', 'anchor': null},
        ],
        'media': n == 21 ? [media(n)] : [],
        'sourceVersion': _hash('detail-$n'),
        ...lease(),
      };
      Map<String, Object?> cover() => {
        'kind': 'REFERENCE_IMAGE',
        'id': _id(8001),
        'revision': 1,
        'visibilitySource': 'CIVU_PUBLIC_CATALOG',
        'mimeType': 'image/jpeg',
        'width': 160,
        'height': 90,
        'byteCount': markerJpeg.length,
        'sha256': markerDigest,
        'sourceVersion': source,
        'contentPath':
            '/v1/me/map-content/places/${_id(5000)}/images/${_id(8001)}/content?sourceVersion=$source&variant=marker',
      };
      Map<String, Object?> pin(int n) => {
        'kind': 'PLACE_CONTENT',
        'markerId': 'place:${_id(5000 + n)}',
        'entityId': _id(5000 + n),
        'anchorCount': 1,
        'placeId': _id(5000 + n),
        'name': '合成地点 $n',
        'categoryCode': 'culture.museum',
        'cityId': null,
        'anchor': {
          'coordinateSystem': 'wgs84',
          'precision': 'point',
          // Distinct real-shaped anchors: the server groups exact coincidences.
          'latitude': 57.2 + n / 10000,
          'longitude': -2.1,
        },
        'visibilitySources': n == 0
            ? ['CIVU_PUBLIC_CATALOG', 'CIVU_PUBLIC_MOMENT', 'SELF_PRIVATE']
            : ['CIVU_PUBLIC_MOMENT'],
        'sourceVersion': source,
        'momentCount': n == 0 ? 21 : 1,
        'moments': n == 0
            ? List.generate(20, (i) => selector(i + 1))
            : [selector(100 + n)],
        'momentsTruncated': n == 0,
        'cover': n == 0 ? cover() : null,
      };
      // Place covers remain readable in a detail, but PMAP renders only an
      // independent Moment's verified image on the map. Keep all pixel/ABA
      // assertions below on that real domain shape instead of restoring the
      // obsolete Place-cover transfer behavior.
      Map<String, Object?> momentPin() => {
        ...pin(35),
        'kind': 'MOMENT_ANCHOR',
        'markerId': 'anchor:${_id(7036)}',
        'anchorId': _id(7036),
        'entityId': _id(136),
        'momentId': _id(136),
        'entryId': _id(136),
        'placeId': '',
        'categoryCode': '',
        'visibilitySources': ['SELF_PRIVATE'],
        'moments': [selector(136)],
        'cover': {
          ...cover(),
          'kind': 'MOMENT_IMAGE',
          'visibilitySource': 'SELF_PRIVATE',
          'contentPath':
              '/v1/me/map-content/moments/${_id(136)}/media/${_id(8001)}/content?sourceVersion=$source&variant=marker',
        },
      };
      http.Response json(Object value) => http.Response(
        jsonEncode({'data': value}),
        200,
        headers: {
          'content-type': 'application/json',
          'cache-control': 'no-store',
        },
      );
      final client = MockClient((request) async {
        calls.add(request);
        expectSync(request.method, 'GET');
        expectSync(request.url.origin, origin);
        expectSync(request.headers['Authorization'], 'Bearer isolated-fixture');
        expectSync(request.followRedirects, isFalse);
        if (request.url.path == '/v1/me/map-content') {
          final response = json({
            'schemaVersion': 'map-content-v2',
            'zoom': double.parse(request.url.queryParameters['zoom']!),
            'filterScope': 'DEFAULT_CONTENT',
            'totalContentCount': 56,
            'totalAnchorCount': 36,
            'nextCursor': null,
            'scope': 'MAP_CONTENT',
            'viewerAccountId': owner,
            'cityId': 'aberdeen',
            'bounds': {
              for (final k in ['west', 'south', 'east', 'north'])
                k: double.parse(request.url.queryParameters[k]!),
            },
            'sourceVersion': source,
            'pins': List.generate(36, (n) => n == 35 ? momentPin() : pin(n)),
            'truncated': false,
            ...lease(),
          });
          if (delayNextMap) {
            delayNextMap = false;
            heldMapUntil = now.add(const Duration(seconds: 30));
            final wait = delayed!;
            await wait.future;
          }
          return response;
        }
        if (request.url.path ==
            '/v1/me/map-content/places/${_id(5000)}/moments') {
          final last = request.url.queryParameters['cursor'] == 'page-two';
          expectSync(request.url.queryParameters, {
            'mediaMode': 'lazy',
            if (last) 'cursor': 'page-two',
          });
          if (!last) pageUntil = now.add(const Duration(seconds: 30));
          final items = last
              ? [detail(21)]
              : List.generate(20, (i) => detail(i + 1));
          // A cursor keeps its original deadline. The server clamps each
          // returned detail to that page deadline, even on a later request.
          for (final item in items) {
            item['validUntil'] = pageUntil!.toIso8601String();
          }
          return json({
            'schemaVersion': 'map-content-place-moments-v1',
            'viewerAccountId': owner,
            'placeId': _id(5000),
            'items': items,
            'nextCursor': last ? null : 'page-two',
            'sourceVersion': _hash('page-source'),
            ...lease(),
            'validUntil': pageUntil!.toIso8601String(),
          });
        }
        if (request.url.path.endsWith('/content')) {
          final bytes = Uint8List.fromList(
            request.url.queryParameters['variant'] == 'marker'
                ? markerJpeg
                : jpeg,
          );
          if (corrupt) bytes[100] ^= 1;
          return http.Response.bytes(
            bytes,
            200,
            headers: {
              'content-type': 'image/jpeg',
              'cache-control': 'no-store',
              'x-content-type-options': 'nosniff',
              'content-length': bytes.length.toString(),
            },
          );
        }
        final number = int.parse(
          request.url.path.split('/').last.split('-').last,
        );
        return json(detail(number));
      });
      final controller = OwnedMapController(
        client: client,
        apiBaseUrl: () => origin,
        ownerID: () => owner,
        authorizationHeader: () => 'Bearer isolated-fixture',
        organizationWorkspaceID: () => organization,
        identityChanges: identity,
        refreshAllowed: () => allowed,
        now: () => now,
      );
      Future<void> drain(bool Function() done) async {
        for (var i = 0; i < 100 && !done(); i++) {
          await t.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 20)),
          );
          await t.pump();
        }
        expect(
          done(),
          isTrue,
          reason:
              'items=${controller.items.length}, current=${controller.current}, error=${controller.error}, requests=${calls.map((r) => r.url.path).join(",")}',
        );
      }

      final wire = OwnedMapAPI(client: client, apiBaseUrl: () => origin);
      final probe = await t.runAsync(
        () => wire.read(
          cityID: 'aberdeen',
          bounds: bounds,
          ownerID: owner,
          authorization: 'Bearer isolated-fixture',
          origin: origin,
          current: () => true,
          now: () => now,
        ),
      );
      expect(probe!.items, hasLength(36));
      final verified = await t.runAsync(
        () => wire.content(
          probe.items.first.cover!,
          authorization: 'Bearer isolated-fixture',
          origin: origin,
          current: () => true,
        ),
      );
      expect(verified!.length, markerJpeg.length);
      verified.fillRange(0, verified.length, 0);
      wire.dispose();
      MapEntity imageEntity() => controller.entities.singleWhere(
        (e) => e.kind == MapEntityKind.moment,
      );
      OwnedMapPin placePin() =>
          controller.items.singleWhere((p) => p.placeID == _id(5000));
      MapEntity placeEntity() =>
          controller.entities.singleWhere((e) => e.placeID == _id(5000));
      controller.sync(cityID: 'aberdeen', bounds: bounds);
      await drain(
        () =>
            controller.entities.length == 36 &&
            imageEntity().mediaPixels != null,
      );
      final initial = placePin(), pixels = imageEntity().mediaPixels!;
      final nativeCopy = imageEntity();
      expect(nativeCopy.readGrant!.authorizes(nativeCopy, now), isTrue);
      expect(placeEntity().kind, MapEntityKind.place);
      expect(placeEntity().mediaPixels, isNull);
      expect(
        calls.where(
          (r) =>
              r.url.path.contains('/places/') &&
              r.url.path.endsWith('/content'),
        ),
        hasLength(1),
        reason: 'Only the explicit detail-wire probe fetched a Place image',
      );
      expect(initial.cityID, isNull);
      expect(
        initial.visibilitySources,
        containsAll(['CIVU_PUBLIC_CATALOG', 'SELF_PRIVATE']),
      );
      expect(placeEntity().memberIDs, isEmpty);
      expect(placeEntity().categoryCode, 'culture.museum');
      expect(placeEntity().recentMoments!.labelAt(now), '21条动态');
      expect(pixels.current(), isTrue);
      expect(pixels.image.width, 36);
      expect(pixels.image.height, 20);
      expect(pixels.normalPNG, isNotEmpty);
      expect(pixels.selectedPNG, isNotEmpty);
      final raw = await t.runAsync(() => pixels.image.toByteData());
      expect(raw, isNotNull); // Actual canonical JPEG was decoded and painted.

      // The deployed authorized map request took more than six seconds. A
      // renewal must start early enough to survive that latency without
      // extending the old permission deadline or clearing the selected pin.
      delayed = Completer<http.Response>();
      delayNextMap = true;
      final oldUntil = controller.view!.validUntil;
      final renewEpoch = controller.epoch;
      now = now.add(const Duration(seconds: 15));
      await t.pump(const Duration(seconds: 15));
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 10)),
      );
      expect(controller.loading, isTrue);
      expect(nativeCopy.readGrant!.authorizes(nativeCopy, now), isTrue);
      expect(delayNextMap, isFalse);
      now = now.add(const Duration(seconds: 7));
      await t.pump(const Duration(seconds: 7));
      expect(controller.sourceCurrent(initial), isTrue);
      delayed.complete(http.Response('', 200));
      await drain(() => !controller.loading);
      expect(controller.epoch, renewEpoch);
      expect(controller.view!.validUntil.isAfter(oldUntil), isTrue);
      final renewedNative = imageEntity();
      expect(nativeCopy.readGrant!.authorizes(nativeCopy, now), isTrue);
      expect(nativeCopy.readGrant!.authorizesCopiedPixels(pixels, now), isTrue);
      expect(
        amapRequiresNativeRetirement(
          previousAuthority: 'isolated-same-actor',
          authority: 'isolated-same-actor',
          previous: [nativeCopy],
          next: {renewedNative.id: renewedNative},
          copiedMediaIDs: {nativeCopy.id},
          copiedAuxiliaryIDs: {},
          now: now,
        ),
        isFalse,
        reason: 'Real controller renewal must not clear its native photo',
      );
      now = now.add(const Duration(seconds: 9));
      await t.pump(const Duration(seconds: 9));
      expect(now.isAfter(oldUntil), isTrue);
      expect(controller.sourceCurrent(initial), isTrue);
      expect(pixels.current(), isFalse);
      expect(
        nativeCopy.readGrant!.authorizes(nativeCopy, now),
        isFalse,
        reason: 'A native copy cannot keep its original expired lease',
      );
      expect(imageEntity().mediaPixels!.current(), isTrue);
      expect(identical(imageEntity().mediaPixels!.image, pixels.image), isTrue);

      // The native reader can return up to 200 independently verified media
      // objects. This is separate from the editor's entry count of 20.
      Map<String, Object?> mediaAt(int index) => {
        ...media(21),
        'id': _id(8001 + index),
        'contentPath':
            '/v1/me/map-content/moments/${_id(21)}/media/${_id(8001 + index)}/content?sourceVersion=$source',
      };
      final maximumMedia = {
        ...detail(21),
        'media': List.generate(200, mediaAt),
      };
      expect(
        OwnedMapMomentDetail.decode(
          maximumMedia,
          ownerID: owner,
          now: now,
        ).media,
        hasLength(200),
      );
      expect(
        () => OwnedMapMomentDetail.decode(
          {...maximumMedia, 'media': List.generate(201, mediaAt)},
          ownerID: owner,
          now: now,
        ),
        throwsFormatException,
      );

      final page = await t.runAsync(() => controller.readPlaceMoments(initial));
      expect(page!.items, hasLength(20));
      expect(page.items.first.canEdit, isFalse);
      expect(page.items.first.visibility, 'private');
      expect(page.items.first.visibilitySource, 'CIVU_PUBLIC_MOMENT');
      expect(page.items[1].canEdit, isTrue);
      expect(page.items.first.body, '  原文 1\n');
      now = now.add(const Duration(seconds: 1));
      final next = await t.runAsync(
        () => controller.readPlaceMoments(initial, cursor: page.nextCursor),
      );
      final beyond = next!.items.single;
      expect(beyond.id, _id(21));
      expect(next.nextCursor, isNull);
      expect(next.validUntil, page.validUntil);
      expect(beyond.validUntil, page.validUntil);
      expect(controller.momentCurrent(beyond.selector), isTrue);
      final reread = await t.runAsync(
        () => controller.readMoment(beyond.selector),
      );
      expect(controller.detailCurrent(reread!), isTrue);
      final bytes = await t.runAsync(
        () => controller.momentContent(reread, reread.media.single),
      );
      expect(sha256.convert(bytes!).toString(), digest);
      bytes.fillRange(0, bytes.length, 0);
      await t.runAsync(
        () => expectLater(
          controller.readPlaceMoments(initial, cursor: 'unissued'),
          throwsFormatException,
        ),
      );
      corrupt = true;
      await t.runAsync(
        () => expectLater(
          controller.coverContent(initial.cover!, current: () => true),
          throwsFormatException,
        ),
      );
      corrupt = false;

      now = now.add(const Duration(seconds: 5));
      await t.runAsync(controller.refresh);
      expect(identical(initial, placePin()), isFalse);
      expect(controller.sourceCurrent(initial), isTrue);
      expect(pixels.current(), isFalse);
      expect(imageEntity().mediaPixels!.current(), isTrue);
      expect(identical(imageEntity().mediaPixels!.image, pixels.image), isTrue);
      expect(controller.detailCurrent(reread), isTrue);
      source = _hash('changed-source');
      await t.runAsync(controller.refresh);
      expect(controller.sourceCurrent(initial), isFalse);
      expect(controller.detailCurrent(reread), isFalse);
      expect(pixels.current(), isFalse);
      await drain(() => imageEntity().mediaPixels != null);
      final beforeABA = placePin(),
          beforeABAPixels = imageEntity().mediaPixels!;
      final oldEpoch = controller.epoch;
      delayed = Completer<http.Response>();
      delayNextMap = true;
      final pending = controller.refresh();
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 10)),
      );
      owner = _id(9002);
      organization = _id(9999);
      identity.value++;
      expect(controller.items, isEmpty);
      expect(beforeABAPixels.current(), isFalse);
      delayed.complete(http.Response('', 200));
      var pendingFinished = false;
      unawaited(pending.then((_) => pendingFinished = true));
      await drain(() => pendingFinished);
      expect(controller.items, isEmpty);
      owner = _id(9001);
      organization = null;
      identity.value++;
      await drain(() => controller.current);
      expect(controller.epoch, greaterThan(oldEpoch));
      expect(controller.sourceCurrent(beforeABA), isFalse);
      final preTransport = placePin();
      controller.replaceTransport(client: client);
      expect(controller.items, isEmpty);
      controller.sync(cityID: 'aberdeen', bounds: bounds);
      await drain(() => controller.current);
      expect(controller.sourceCurrent(preTransport), isFalse);
      final preOrigin = placePin();
      origin = 'https://new-owned-map.test';
      expect(controller.items, isEmpty);
      identity.value++;
      await drain(() => controller.current);
      expect(controller.sourceCurrent(preOrigin), isFalse);
      controller.updateRefreshVisibility();
      final beforeHidden = placePin();
      allowed = false;
      controller.updateRefreshVisibility();
      expect(controller.items, isEmpty);
      now = now.add(const Duration(seconds: 31));
      await t.pump(const Duration(seconds: 31));
      final beforeReturn = calls
          .where((r) => r.url.path == '/v1/me/map-content')
          .length;
      allowed = true;
      controller.updateRefreshVisibility();
      await drain(() => controller.current);
      expect(controller.cityID, 'aberdeen');
      expect(controller.sourceCurrent(beforeHidden), isFalse);
      expect(
        calls.where((r) => r.url.path == '/v1/me/map-content').length,
        beforeReturn + 1,
      );
      controller.updateRefreshVisibility();
      controller.updateRefreshVisibility();
      await t.pump();
      expect(
        calls.where((r) => r.url.path == '/v1/me/map-content').length,
        beforeReturn + 1,
      );
      final expires = placePin();
      delayed = Completer<http.Response>();
      delayNextMap = true;
      now = now.add(const Duration(seconds: 15));
      await t.pump(const Duration(seconds: 15));
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 10)),
      );
      expect(controller.loading, isTrue);
      now = now.add(const Duration(seconds: 16));
      await t.pump(const Duration(seconds: 16));
      expect(controller.current, isFalse);
      expect(controller.entities, isEmpty);
      expect(controller.sourceCurrent(expires), isFalse);
      expect(controller.view, isNull);
      expect(controller.items, isEmpty);
      expect(
        controller.loading,
        isTrue,
        reason:
            'The pending renewal survives, while every old source grant expires',
      );
      expect(
        controller.error,
        isNull,
        reason: 'An in-flight renewal is loading, not a completed read failure',
      );
      // The pending response carries its own original deadline. Let that
      // deadline pass too: an actually expired response must never resurrect
      // content, whereas a still-valid fresh renewal may legitimately return.
      now = now.add(const Duration(seconds: 16));
      await t.pump(const Duration(seconds: 16));
      delayed.complete(http.Response('', 200));
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 10)),
      );
      // The 12s HTTP timeout may already have requested a separate recovery.
      // Only that genuinely newer grant can restore the map, never the held
      // response whose own deadline has now passed.
      await drain(() => controller.current && !controller.loading);
      expect(
        controller.view!.observedAt.isAfter(expires.validUntil!),
        isTrue,
        reason: 'Recovery must be observed after the expired original grant',
      );
      expect(
        controller.view!.validUntil.isAfter(heldMapUntil!),
        isTrue,
        reason: 'The restored view must carry a newer server lease',
      );
      expect(controller.momentCurrent(beyond.selector), isFalse);
      final requestsBeforeClear = calls
          .where((r) => r.url.path == '/v1/me/map-content')
          .length;
      controller.retire(clearQuery: true);
      allowed = false;
      controller.updateRefreshVisibility();
      allowed = true;
      controller.updateRefreshVisibility();
      expect(controller.cityID, isNull);
      await t.pump();
      expect(
        calls.where((r) => r.url.path == '/v1/me/map-content').length,
        requestsBeforeClear,
      );
      expect(
        calls.every(
          (r) =>
              r.method == 'GET' && r.url.path.startsWith('/v1/me/map-content'),
        ),
        isTrue,
      );
      allowed = false;
      controller.dispose();
      identity.dispose();
      client.close();
      await t.pump();
    },
  );
}
