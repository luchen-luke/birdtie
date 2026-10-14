import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:birdtie_client/src/workspace/place_photo.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_media_controller.dart';
import 'package:birdtie_client/src/workspace/public_moment_photo.dart';
import 'entity_cover_test.dart' show coverJPEG;

const mapMediaOwner = '12345678-1234-1234-1234-123456789abc';
MapEntity mediaEntity(int n, {DateTime? until, String? digest}) {
  final id = '22345678-1234-1234-1234-${n.toString().padLeft(12, '0')}';
  final ref = PlacePhotoRef(id);
  return MapEntity(
    id: 'place:$id',
    kind: MapEntityKind.place,
    title: '隔离地点$n',
    subtitle: '合成样例',
    latitude: 57.2,
    longitude: -2.1,
    placeID: id,
    publicLocation: true,
    sourceVersion: 'specific-source-$n',
    placePhoto: EntityCoverMetadata(
      ref: ref,
      version: 1,
      path: '${ref.path}/1/content',
      byteCount: coverJPEG().length,
      digest: digest ?? sha256.convert(coverJPEG()).toString(),
      expiresAt:
          until ?? DateTime.now().toUtc().add(const Duration(seconds: 50)),
      attribution: '合成测试素材',
    ),
  );
}

/// Actual binary JPEGs remain isolated test geometry, never real POI media.
MapEntity distinctMediaEntity(
  int n,
  Uint8List bytes, {
  int version = 1,
  DateTime? until,
}) {
  final base = mediaEntity(n, until: until), ref = PlacePhotoRef(base.placeID);
  return MapEntity(
    id: base.id,
    kind: base.kind,
    title: base.title,
    subtitle: base.subtitle,
    latitude: base.latitude,
    longitude: base.longitude,
    placeID: base.placeID,
    publicLocation: true,
    sourceVersion: base.sourceVersion,
    placePhoto: EntityCoverMetadata(
      ref: ref,
      version: version,
      path: '${ref.path}/$version/content',
      byteCount: bytes.length,
      digest: sha256.convert(bytes).toString(),
      expiresAt: base.placePhoto!.expiresAt,
      attribution: '仅本机合成几何素材，不是真实地点照片',
    ),
  );
}

Future<List<int>> decodedCenter(ui.Image image) async {
  final raw = (await image.toByteData(format: ui.ImageByteFormat.rawRgba))!;
  final offset = ((image.height ~/ 2) * image.width + image.width ~/ 2) * 4;
  return raw.buffer
      .asUint8List(raw.offsetInBytes, raw.lengthInBytes)
      .sublist(offset, offset + 4);
}

void expectFixtureCenter(List<int> rgba, List<int> expected) {
  expect(rgba, hasLength(4));
  for (var i = 0; i < 3; i++) {
    expect(rgba[i], closeTo(expected[i], 6));
  }
  expect(rgba[3], 255);
}

void syncMedia(
  MapMediaController c,
  List<MapEntity> source, {
  String? owner = mapMediaOwner,
  String? token = 'Bearer isolated',
  Object generation = 1,
  double ratio = 1,
}) => c.sync(
  ownerID: owner,
  workspaceID: null,
  authorization: token,
  cityID: 'owned-city',
  sourceGeneration: generation,
  entities: source,
  devicePixelRatio: ratio,
);
Future<void> mediaWait(WidgetTester t, bool Function() done) async {
  for (var n = 0; n < 100 && !done(); n++) {
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 20)),
    );
    await t.pump();
  }
  expect(done(), isTrue);
}

void main() {
  testWidgets(
    'Place and public Moment with same UUID use distinct actual JPEG pixels; photo replacement and retirement preserve original anchors',
    (t) async {
      final files = (await t.runAsync(
        () => Future.wait([
          File('test/fixtures/map-media/isolation-a.jpg').readAsBytes(),
          File('test/fixtures/map-media/isolation-b.jpg').readAsBytes(),
        ]),
      ))!;
      final a = files[0],
          b = files[1],
          until = DateTime.now().toUtc().add(const Duration(seconds: 50));
      final place = distinctMediaEntity(1, a, until: until), id = place.placeID;
      MapEntity moment(
        Uint8List bytes, {
        int revision = 3,
        int version = 1,
        bool photo = true,
      }) => MapEntity(
        id: 'moment:$id',
        kind: MapEntityKind.moment,
        title: '本人明确公开的隔离记录',
        subtitle: '关联地点，不是拍摄坐标',
        latitude: place.latitude,
        longitude: place.longitude,
        placeID: place.placeID,
        sourceVersion: 'original-moment-$revision',
        momentPhoto: photo
            ? PublicMomentPhotoMetadata(
                momentID: id,
                momentRevision: revision,
                photoID: mapMediaOwner,
                version: version,
                path: publicMomentPhotoPath(
                  id,
                  revision,
                  mapMediaOwner,
                  version,
                ),
                byteCount: bytes.length,
                digest: sha256.convert(bytes).toString(),
                expiresAt: until,
                attribution: '仅几何测试素材',
              )
            : null,
      );
      final original = moment(b),
          replacement = moment(a, version: 2),
          requests = <String>[];
      final payloads = {
        place.placePhoto!.path: a,
        original.momentPhoto!.path: b,
        replacement.momentPhoto!.path: a,
      };
      final client = MockClient((r) async {
        requests.add(r.url.path);
        return http.Response.bytes(
          payloads[r.url.path]!,
          200,
          headers: {'content-type': 'image/jpeg', 'cache-control': 'no-store'},
        );
      });
      final controller = MapMediaController(
        client: client,
        apiBaseUrl: 'http://owned.invalid',
      );
      syncMedia(controller, [place, original]);
      await mediaWait(
        t,
        () =>
            controller.inFlight == 0 &&
            controller.decorate(original).mediaPixels != null,
      );
      final placePixels = controller.decorate(place).mediaPixels!,
          oldPixels = controller.decorate(original).mediaPixels!;
      final centers = (await t.runAsync(
        () => Future.wait([
          decodedCenter(placePixels.image),
          decodedCenter(oldPixels.image),
        ]),
      ))!;
      expectFixtureCenter(centers[0], [34, 118, 85]);
      expectFixtureCenter(centers[1], [197, 97, 37]);
      expect(placePixels.key, isNot(oldPixels.key));
      expect(
        sha256.convert(placePixels.normalPNG).toString(),
        isNot(sha256.convert(oldPixels.normalPNG).toString()),
      );
      expect(requests.toSet(), {
        place.placePhoto!.path,
        original.momentPhoto!.path,
      });
      syncMedia(controller, [place, replacement]);
      expect(oldPixels.current(), isFalse);
      expect(controller.decorate(replacement).mediaPixels, isNull);
      await mediaWait(
        t,
        () =>
            controller.inFlight == 0 &&
            controller.decorate(replacement).mediaPixels != null,
      );
      expectFixtureCenter(
        (await t.runAsync(
          () => decodedCenter(
            controller.decorate(replacement).mediaPixels!.image,
          ),
        ))!,
        [34, 118, 85],
      );
      expect(controller.decorate(place).mediaPixels!.key, placePixels.key);
      expect(requests.length, 3);
      expect(requests.last, replacement.momentPhoto!.path);
      final retired = moment(a, version: 2, photo: false),
          newPixels = controller.decorate(replacement).mediaPixels!;
      syncMedia(controller, [place, retired]);
      expect(newPixels.current(), isFalse);
      expect(controller.decorate(retired).mediaPixels, isNull);
      expect(retired.id, original.id);
      expect(retired.latitude, original.latitude);
      expect(retired.longitude, original.longitude);
      expect(controller.decorate(place).mediaPixels, isNotNull);
      expect(requests.length, 3);
      controller.dispose();
      await t.pump();
      client.close();
    },
  );
  testWidgets(
    'same-kind distinct real JPEG pixels stay isolated across media version without moving point',
    (t) async {
      final files = (await t.runAsync(
        () => Future.wait([
          File('test/fixtures/map-media/isolation-a.jpg').readAsBytes(),
          File('test/fixtures/map-media/isolation-b.jpg').readAsBytes(),
        ]),
      ))!;
      final a = files[0],
          b = files[1],
          until = DateTime.now().toUtc().add(const Duration(seconds: 50));
      final first = distinctMediaEntity(1, a, until: until),
          second = distinctMediaEntity(2, b, until: until);
      final requests = <String>[];
      final payloads = <String, Uint8List>{
        first.placePhoto!.path: a,
        second.placePhoto!.path: b,
      };
      final c = MapMediaController(
        apiBaseUrl: 'https://map-fixture.test',
        client: MockClient((r) async {
          requests.add(r.url.path);
          expect(r.method, 'GET');
          final bytes = payloads[r.url.path];
          expect(bytes, isNotNull);
          return http.Response.bytes(
            bytes!,
            200,
            headers: {'content-type': 'image/jpeg'},
          );
        }),
      );
      // Do not inject a decoder: bounded HTTP, JPEG/SHA verification and the
      // production instantiateImageCodec path must all produce these pixels.
      syncMedia(c, [first, second]);
      await mediaWait(
        t,
        () =>
            c.inFlight == 0 &&
            c.decorate(first).mediaPixels != null &&
            c.decorate(second).mediaPixels != null,
      );
      final firstPixels = c.decorate(first).mediaPixels!,
          secondPixels = c.decorate(second).mediaPixels!;
      final centers = (await t.runAsync(
        () => Future.wait([
          decodedCenter(firstPixels.image),
          decodedCenter(secondPixels.image),
        ]),
      ))!;
      expectFixtureCenter(centers[0], [34, 118, 86]);
      expectFixtureCenter(centers[1], [198, 97, 37]);
      expect(firstPixels.key, isNot(secondPixels.key));
      expect(first.placePhoto!.digest, isNot(second.placePhoto!.digest));
      expect(
        sha256.convert(firstPixels.normalPNG).toString(),
        isNot(sha256.convert(secondPixels.normalPNG).toString()),
      );
      expect(
        sha256.convert(firstPixels.selectedPNG).toString(),
        isNot(sha256.convert(secondPixels.selectedPNG).toString()),
      );
      expect(
        firstPixels.normalPNG,
        isNot(orderedEquals(secondPixels.normalPNG)),
      );
      expect(
        requests,
        unorderedEquals([first.placePhoto!.path, second.placePhoto!.path]),
      );
      final replacement = distinctMediaEntity(1, b, version: 2, until: until);
      payloads[replacement.placePhoto!.path] = b;
      syncMedia(c, [replacement, second]);
      expect(firstPixels.current(), isFalse);
      expect(secondPixels.current(), isTrue);
      expect(c.decorate(first).mediaPixels, isNull);
      await mediaWait(
        t,
        () => c.inFlight == 0 && c.decorate(replacement).mediaPixels != null,
      );
      final changed = c.decorate(replacement),
          changedPixels = changed.mediaPixels!;
      final changedCenter = (await t.runAsync(
        () => decodedCenter(changedPixels.image),
      ))!;
      expectFixtureCenter(changedCenter, [198, 97, 37]);
      expect(changedPixels.key, isNot(firstPixels.key));
      expect(
        sha256.convert(changedPixels.normalPNG).toString(),
        isNot(sha256.convert(firstPixels.normalPNG).toString()),
      );
      expect(changedPixels.normalPNG, orderedEquals(secondPixels.normalPNG));
      expect(c.decorate(second).mediaPixels, same(secondPixels));
      expect(
        (
          changed.id,
          changed.kind,
          changed.latitude,
          changed.longitude,
          changed.placeID,
          changed.sourceVersion,
        ),
        (
          first.id,
          first.kind,
          first.latitude,
          first.longitude,
          first.placeID,
          first.sourceVersion,
        ),
      );
      expect(requests.length, 3);
      expect(requests.last, replacement.placePhoto!.path);
      c.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'one bounded shared queue merges callers and never exceeds three GETs',
    (t) async {
      final replies = <Completer<http.Response>>[], requests = <http.Request>[];
      final c = MapMediaController(
        client: MockClient((r) {
          requests.add(r);
          final reply = Completer<http.Response>();
          replies.add(reply);
          return reply.future;
        }),
        apiBaseUrl: 'https://map-fixture.test',
      );
      final source = [for (var n = 1; n <= 5; n++) mediaEntity(n)];
      syncMedia(c, source);
      syncMedia(c, source);
      await mediaWait(t, () => requests.length == 3);
      expect(c.inFlight, 3);
      expect(c.reservedBytes, lessThanOrEqualTo(c.maxBytes));
      for (var n = 0; n < 5; n++) {
        await mediaWait(t, () => replies.length > n);
        replies[n].complete(http.Response.bytes(coverJPEG(), 200));
      }
      await mediaWait(t, () => c.inFlight == 0);
      expect(requests.length, 5);
      expect(c.decorate(source.first).mediaPixels!.current(), isTrue);
      syncMedia(c, source);
      await t.pump();
      expect(requests.length, 5);
      c.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'owner A B A permanently rejects late decoded pixels and keeps same point',
    (t) async {
      final held = Completer<ui.Image>();
      var decodes = 0;
      final source = [mediaEntity(1)];
      final c = MapMediaController(
        client: MockClient((_) async => http.Response.bytes(coverJPEG(), 200)),
        apiBaseUrl: 'https://map-fixture.test',
        decode: (_) {
          ++decodes;
          return held.future;
        },
      );
      syncMedia(c, source);
      await mediaWait(t, () => decodes == 1);
      syncMedia(
        c,
        const [],
        owner: '32345678-1234-1234-1234-123456789abc',
        generation: 2,
      );
      syncMedia(c, const [], generation: 3);
      final image = await t.runAsync(() async {
        final codec = await ui.instantiateImageCodec(coverJPEG());
        try {
          return (await codec.getNextFrame()).image;
        } finally {
          codec.dispose();
        }
      });
      held.complete(image!);
      await mediaWait(t, () => c.inFlight == 0);
      final samePoint = c.decorate(source.single);
      expect(samePoint.mediaPixels, isNull);
      expect(
        (samePoint.id, samePoint.latitude, samePoint.longitude),
        (source.single.id, source.single.latitude, source.single.longitude),
      );
      expect(c.reservedBytes, 0);
      c.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'bad JPEG digest falls back once and camera rebuild cannot retry',
    (t) async {
      var gets = 0;
      final c = MapMediaController(
        client: MockClient((_) async {
          ++gets;
          return http.Response.bytes(coverJPEG(), 200);
        }),
        apiBaseUrl: 'https://map-fixture.test',
      );
      final source = [mediaEntity(1, digest: 'a' * 64)];
      syncMedia(c, source);
      await mediaWait(t, () => gets == 1 && c.inFlight == 0);
      for (var i = 0; i < 10; i++) {
        syncMedia(c, source);
      }
      expect(gets, 1);
      expect(c.decorate(source.single).mediaPixels, isNull);
      expect(c.decorate(source.single).hasMapAnchor, isTrue);
      c.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'expiry clears pixels without clearing point, source or selection identity',
    (t) async {
      var now = DateTime.now().toUtc();
      final source = [
        mediaEntity(1, until: now.add(const Duration(seconds: 2))),
      ];
      final c = MapMediaController(
        client: MockClient((_) async => http.Response.bytes(coverJPEG(), 200)),
        apiBaseUrl: 'https://map-fixture.test',
        now: () => now,
      );
      syncMedia(c, source);
      await mediaWait(
        t,
        () => c.inFlight == 0 && c.decorate(source.single).mediaPixels != null,
      );
      final old = c.decorate(source.single), pixels = old.mediaPixels!;
      now = now.add(const Duration(seconds: 3));
      await t.pump(const Duration(seconds: 3));
      expect(pixels.current(), isFalse);
      final fresh = c.decorate(source.single);
      expect(fresh.mediaPixels, isNull);
      expect(fresh.id, old.id);
      expect(fresh.decorationKey, isNot(old.decorationKey));
      expect((fresh.latitude, fresh.longitude), (old.latitude, old.longitude));
      c.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'actual owner required for bearer, budget includes pending and transport retirement',
    (t) async {
      var gets = 0;
      final c = MapMediaController(
        client: MockClient((_) async {
          ++gets;
          return http.Response.bytes(coverJPEG(), 200);
        }),
        apiBaseUrl: 'https://map-fixture.test',
        maxItems: 2,
        maxBytes: 2 * 1024 * 1024,
      );
      final source = [for (var i = 1; i <= 20; i++) mediaEntity(i)];
      syncMedia(c, source, owner: null);
      await t.pump();
      expect(gets, 0);
      syncMedia(c, source);
      await mediaWait(t, () => c.inFlight == 0 && gets == 2);
      expect(c.cachedItems, 2);
      expect(c.reservedBytes, lessThanOrEqualTo(c.maxBytes));
      for (var i = 0; i < 10; i++) {
        syncMedia(c, source);
      }
      expect(gets, 2);
      final old = c.decorate(source.first).mediaPixels!;
      c.replaceTransport(
        client: MockClient((_) async => http.Response('', 404)),
        apiBaseUrl: 'https://new-fixture.test',
      );
      expect(old.current(), isFalse);
      expect(c.decorate(source.first).mediaPixels, isNull);
      c.dispose();
      await t.pump();
    },
  );
}
