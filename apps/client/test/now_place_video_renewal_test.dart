import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:birdtie_client/src/content/video_material_player_io.dart';
import 'package:birdtie_client/src/workspace/map_media_read_metrics.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'content_map_controller_test.dart' as f;

final _video = Uint8List.fromList([
  0,
  0,
  0,
  24,
  102,
  116,
  121,
  112,
  105,
  115,
  111,
  109,
  0,
  0,
  0,
  0,
  105,
  115,
  111,
  109,
  109,
  112,
  52,
  50,
]);
final _pinSource = f.hash('current-place-map-pin');
final _place = f.id(70);

class _Controller extends OwnedMapController {
  _Controller(_Fixture fixture, ValueNotifier<int> changes)
    : super(
        client: fixture.client,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => null,
        identityChanges: changes,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
  bool permitted = true;
  @override
  bool sourceCurrent(OwnedMapPin pin) =>
      permitted && pin.sourceVersion == _pinSource;
}

class _Fixture {
  DateTime now = DateTime.utc(2026, 10, 18);
  String owner = f.id(9000);
  bool paged = false;
  String mode = 'same';
  int status = 200;
  Completer<void>? wait, arrived;
  final requests = <Uri>[];
  late final client = MockClient((request) async {
    requests.add(request.url);
    expect(request.headers['Authorization'], 'Bearer fixture');
    final gate = wait;
    if (gate != null) {
      wait = null;
      arrived?.complete();
      await gate.future;
    }
    if (request.url.path.endsWith('/content')) {
      return http.Response.bytes(
        _video,
        200,
        headers: {
          'content-type': 'video/mp4',
          'cache-control': 'private, no-store',
          'x-content-type-options': 'nosniff',
        },
      );
    }
    if (status != 200) {
      return http.Response(
        jsonEncode({
          'error': {'code': 'not_found'},
        }),
        status,
        headers: {'content-type': 'application/json'},
      );
    }
    final second = request.url.queryParameters['cursor'] == 'original-page-2';
    final isPlace =
        request.url.path == '/v1/me/map-content/places/$_place/moments';
    return http.Response(
      jsonEncode({
        'data': isPlace
            ? page(second: second)
            : detail(1, source: f.hash('single-moment-snapshot')),
      }),
      200,
      headers: {
        'content-type': 'application/json',
        'cache-control': 'no-store',
      },
    );
  });
  final initial = DateTime.utc(2026, 10, 18);
  Map<String, Object?> lease({bool second = false}) => {
    'observedAt': now.toIso8601String(),
    'validUntil':
        (second && mode != 'cursor-extension'
                ? initial.add(const Duration(seconds: 30))
                : now.add(const Duration(seconds: 30)))
            .toIso8601String(),
  };
  Map<String, Object?> detail(int n, {String? source, bool second = false}) => {
    'schemaVersion': 'map-content-moment-v1',
    'viewerAccountId': owner,
    'id': f.id(n),
    'revision': 1,
    'visibilitySource': 'SELF_PRIVATE',
    'canEdit': true,
    'status': 'draft',
    'visibility': 'private',
    'title': '',
    'body': '隔离地点视频',
    'createdAt': '2026-01-01T00:00:00Z',
    'locations': <Object>[],
    'media': n == 1
        ? [
            {
              'kind': 'VIDEO',
              'id': f.id(6001),
              'revision': mode == 'media' ? 2 : 1,
              'mimeType': 'video/mp4',
              'width': 160,
              'height': 90,
              'byteCount': _video.length,
              'sha256': sha256.convert(_video).toString(),
              'sourceVersion': f.hash('video-source'),
              'contentPath':
                  '/v1/me/map-content/moments/${f.id(1)}/media/${f.id(6001)}/content?sourceVersion=${f.hash('video-source')}',
              'durationMs': 1000,
              'videoCodec': 'h264',
              'audioCodec': 'none',
              'poster': null,
            },
          ]
        : <Object>[],
    'sourceVersion': source ?? f.hash('place-snapshot-detail-$n'),
    ...lease(second: second),
  };
  Map<String, Object?> page({bool second = false}) => {
    'schemaVersion': 'map-content-place-moments-v1',
    'viewerAccountId': owner,
    'placeId': _place,
    'items': [
      if (paged && !second)
        detail(2)
      else if (mode != 'missing') ...[
        detail(1, second: second),
        if (!paged) detail(2),
      ],
    ],
    'nextCursor': paged && !second ? 'original-page-2' : null,
    'sourceVersion': f.hash(
      mode == 'source' ? 'changed-place-page' : 'place-page',
    ),
    ...lease(second: second),
  };
  OwnedMapPin pin() => OwnedMapPin(
    placeID: _place,
    name: '隔离地点',
    categoryCode: 'park',
    cityID: null,
    latitude: 57.1,
    longitude: -2.1,
    visibilitySources: const ['SELF_PRIVATE'],
    sourceVersion: _pinSource,
    momentCount: 2,
    moments: const [],
    momentsTruncated: false,
    cover: null,
    observedAt: now,
    validUntil: now.add(const Duration(seconds: 30)),
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'place video refresh keeps its original snapshot and reaches verified file transfer',
    () async {
      final fixture = _Fixture(), changes = ValueNotifier(0);
      final c = _Controller(fixture, changes);
      try {
        final page = await c.readPlaceMoments(fixture.pin());
        final old = page.items.first;
        fixture.now = fixture.now.add(const Duration(seconds: 2));
        final fresh = await c.readMoment(old.selector);
        expect(fresh.sourceVersion, old.sourceVersion);
        expect(fresh.media.single.identity, old.media.single.identity);
        expect(fresh.validUntil.isAfter(old.validUntil), true);
        expect(c.detailCurrent(fresh), true);
        expect(
          fixture.requests.every((u) => u.path.endsWith('/moments')),
          true,
        );
        expect(fixture.requests.last.queryParameters['mediaMode'], 'lazy');
        final path = await c.prepareVideo(
          fresh,
          fresh.media.single,
          current: () => c.detailCurrent(fresh),
          trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
        );
        try {
          expect(await File(path).readAsBytes(), _video);
        } finally {
          await releaseVideoMaterialPath(path);
        }
        fixture.now = fixture.initial.add(const Duration(seconds: 31));
        expect(c.detailCurrent(old), false);
        expect(c.detailCurrent(fresh), true);
      } finally {
        c.dispose();
        changes.dispose();
        fixture.client.close();
      }
    },
  );

  test(
    'second place page refresh uses exact original cursor without extending its deadline',
    () async {
      final fixture = _Fixture()..paged = true;
      final changes = ValueNotifier(0), c = _Controller(fixture, changes);
      try {
        final pin = fixture.pin();
        final first = await c.readPlaceMoments(pin);
        final second = await c.readPlaceMoments(pin, cursor: first.nextCursor);
        fixture.now = fixture.now.add(const Duration(seconds: 2));
        final fresh = await c.readMoment(second.items.single.selector);
        expect(
          fixture.requests.last.queryParameters['cursor'],
          'original-page-2',
        );
        expect(fresh.sourceVersion, second.items.single.sourceVersion);
        expect(fresh.validUntil, second.validUntil);
        fixture.now = fixture.initial.add(const Duration(seconds: 30));
        expect(c.detailCurrent(fresh), false);
        await expectLater(c.readMoment(fresh.selector), throwsFormatException);
      } finally {
        c.dispose();
        changes.dispose();
        fixture.client.close();
      }
    },
  );

  test(
    'a continuation response cannot extend the original cursor authority',
    () async {
      final fixture = _Fixture()..paged = true;
      final changes = ValueNotifier(0), c = _Controller(fixture, changes);
      try {
        final pin = fixture.pin();
        final first = await c.readPlaceMoments(pin);
        final second = await c.readPlaceMoments(pin, cursor: first.nextCursor);
        fixture.now = fixture.now.add(const Duration(seconds: 2));
        fixture.mode = 'cursor-extension';
        await expectLater(
          c.readMoment(second.items.single.selector),
          throwsFormatException,
        );
        expect(c.detailCurrent(second.items.single), false);
      } finally {
        c.dispose();
        changes.dispose();
        fixture.client.close();
      }
    },
  );

  for (final mode in ['source', 'missing', 'media', 'denied']) {
    test(
      'known $mode retires the original place snapshot and its media grants',
      () async {
        final fixture = _Fixture(), changes = ValueNotifier(0);
        final c = _Controller(fixture, changes);
        try {
          final page = await c.readPlaceMoments(fixture.pin());
          fixture.mode = mode;
          if (mode == 'denied') fixture.status = 404;
          await expectLater(
            c.readMoment(page.items.first.selector),
            throwsA(anything),
          );
          for (final old in page.items) {
            expect(c.detailCurrent(old), false);
            expect(c.momentCurrent(old.selector), false);
          }
          await expectLater(
            c.prepareVideo(
              page.items.first,
              page.items.first.media.single,
              current: () => c.detailCurrent(page.items.first),
              trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
            ),
            throwsFormatException,
          );
          expect(
            fixture.requests.any((u) => u.path.endsWith('/content')),
            false,
          );
        } finally {
          c.dispose();
          changes.dispose();
          fixture.client.close();
        }
      },
    );
  }

  for (final boundary in ['identity-A-B-A', 'source', 'expiry']) {
    test('late place renewal cannot survive $boundary', () async {
      final fixture = _Fixture(), changes = ValueNotifier(0);
      final c = _Controller(fixture, changes);
      try {
        final page = await c.readPlaceMoments(fixture.pin());
        final gate = fixture.wait = Completer<void>();
        final arrived = fixture.arrived = Completer<void>();
        final renewal = c.readMoment(page.items.first.selector);
        await arrived.future;
        if (boundary == 'identity-A-B-A') {
          final original = fixture.owner;
          fixture.owner = f.id(9001);
          changes.value++;
          fixture.owner = original;
          changes.value++;
        } else if (boundary == 'source') {
          c.permitted = false;
        } else {
          fixture.now = fixture.initial.add(const Duration(seconds: 31));
        }
        gate.complete();
        await expectLater(renewal, throwsFormatException);
        expect(c.detailCurrent(page.items.first), false);
        expect(fixture.requests.any((u) => u.path.endsWith('/content')), false);
      } finally {
        c.dispose();
        changes.dispose();
        fixture.client.close();
      }
    });
  }
}
