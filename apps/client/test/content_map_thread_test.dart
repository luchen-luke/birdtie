import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/content/moment_image_header.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'entity_cover_test.dart' show coverJPEG;

const _owner = '11111111-1111-4111-8111-111111111111';
const _seed = '22222222-2222-4222-8222-222222222222';
const _sibling = '33333333-3333-4333-8333-333333333333';
const _thread = '44444444-4444-4444-8444-444444444444';
const _place = '55555555-5555-4555-8555-555555555555';
const _origin = 'https://map-thread.test';
final _source = 'a' * 64;

Map<String, Object?> _lease(DateTime now, [int seconds = 30]) => {
  'observedAt': now.toIso8601String(),
  'validUntil': now.add(Duration(seconds: seconds)).toIso8601String(),
};
Map<String, Object?> _detail(String id, DateTime now) => {
  'schemaVersion': 'map-content-moment-v1',
  'viewerAccountId': _owner,
  'id': id,
  'revision': 1,
  'visibilitySource': 'SELF_PRIVATE',
  'canEdit': true,
  'status': 'draft',
  'visibility': 'private',
  'title': '',
  'body': '隔离测试正文$id',
  'createdAt': now.subtract(const Duration(days: 1)).toIso8601String(),
  'locations': <Object>[],
  'media': <Object>[],
  'sourceVersion': 'b' * 64,
  ..._lease(now),
};
Map<String, Object?> _sequence(DateTime now, {String seed = _seed}) => {
  'schemaVersion': 'map-content-thread-v1',
  'viewerAccountId': _owner,
  'seedMomentId': seed,
  'seedRevision': 1,
  'seedVisibilitySource': 'SELF_PRIVATE',
  'status': 'AVAILABLE',
  'threadId': _thread,
  'entries': [
    {'position': 0, 'status': 'AVAILABLE', 'detail': _detail(_seed, now)},
    {'position': 1, 'status': 'UNAVAILABLE', 'detail': null},
    {'position': 2, 'status': 'AVAILABLE', 'detail': _detail(_sibling, now)},
  ],
  'sourceVersion': 'c' * 64,
  ..._lease(now, 5),
};
http.Response _json(Object? data) => http.Response(
  jsonEncode({'data': data}),
  200,
  headers: {'content-type': 'application/json', 'cache-control': 'no-store'},
);

class _GrantedController extends OwnedMapController {
  _GrantedController({
    required super.client,
    required super.identityChanges,
    required super.now,
    required super.ownerID,
  }) : super(
         apiBaseUrl: () => _origin,
         authorizationHeader: () => 'Bearer synthetic',
         organizationWorkspaceID: () => null,
         refreshAllowed: () => true,
       );
  bool permitted = true;
  @override
  bool sourceCurrent(OwnedMapPin pin) =>
      permitted && pin.sourceVersion == _source;
}

class _FeedbackPanelController extends OwnedMapController {
  _FeedbackPanelController(
    this.changes, {
    this.anchor = false,
    this.images = false,
  }) : super(
         client: MockClient((_) async => throw StateError('unexpected HTTP')),
         apiBaseUrl: () => _origin,
         ownerID: () => _owner,
         authorizationHeader: () => 'Bearer synthetic',
         organizationWorkspaceID: () => null,
         identityChanges: changes,
         refreshAllowed: () => true,
       );
  final ValueNotifier<int> changes;
  final bool anchor, images;
  final threadReads = <String>[];
  final jpeg = coverJPEG();
  late final pin = OwnedMapPin(
    placeID: anchor ? '' : _place,
    name: '隔离地点',
    categoryCode: 'park',
    cityID: null,
    latitude: 57.1,
    longitude: -2.1,
    visibilitySources: const ['SELF_PRIVATE'],
    sourceVersion: _source,
    momentCount: anchor ? 1 : 2,
    moments: [
      for (final id in anchor ? [_sibling] : [_seed, _sibling])
        OwnedMapMoment(id, 1, 'SELF_PRIVATE', '/v1/me/map-content/moments/$id'),
    ],
    momentsTruncated: false,
    cover: null,
    kind: anchor ? 'MOMENT_ANCHOR' : 'PLACE_CONTENT',
    markerID: anchor ? 'anchor:$_place' : 'place:$_place',
    anchorID: anchor ? _place : '',
    momentID: anchor ? _sibling : '',
    entryID: anchor ? _sibling : '',
  );
  Map<String, Object?> detailJSON(String id) {
    final now = DateTime.now().toUtc();
    final data = _detail(id, now);
    data['body'] = id == _seed
        ? (images ? List.filled(20, '很长的真实格式正文').join('\n') : '首段正文')
        : '目标第3段正文';
    if (images) {
      final header = MomentImageHeader.inspect(jpeg, storedDerivative: true);
      data['media'] = [
        {
          'kind': 'IMAGE',
          'id': '66666666-6666-4666-8666-666666666666',
          'revision': 1,
          'mimeType': header.mime,
          'width': header.width,
          'height': header.height,
          'byteCount': jpeg.length,
          'sha256': sha256.convert(jpeg).toString(),
          'sourceVersion': 'd' * 64,
          'contentPath':
              '/v1/me/map-content/moments/$id/media/66666666-6666-4666-8666-666666666666/content?sourceVersion=${'d' * 64}',
          'durationMs': null,
          'videoCodec': null,
          'audioCodec': null,
          'poster': null,
        },
      ];
    }
    return data;
  }

  OwnedMapMomentDetail detail(String id) => OwnedMapMomentDetail.decode(
    detailJSON(id),
    ownerID: _owner,
    now: DateTime.now().toUtc(),
  );
  @override
  bool get current => true;
  @override
  OwnedMapPin? find(String id) => id == pin.mapID ? pin : null;
  @override
  bool sourceCurrent(OwnedMapPin value) => value.mapID == pin.mapID;
  @override
  bool detailCurrent(OwnedMapMomentDetail value) =>
      value.validUntil.isAfter(DateTime.now().toUtc());
  @override
  bool threadCurrent(OwnedMapThread value) => value.current;
  @override
  Future<OwnedMapMomentDetail> readMoment(OwnedMapMoment value) async =>
      detail(value.id);
  @override
  Future<OwnedMapPlacePage> readPlaceMoments(
    OwnedMapPin value, {
    String? cursor,
  }) async {
    final now = DateTime.now().toUtc();
    return OwnedMapPlacePage(
      placeID: _place,
      items: [detail(_seed), detail(_sibling)],
      nextCursor: null,
      observedAt: now,
      validUntil: now.add(const Duration(seconds: 30)),
      sourceVersion: _source,
    );
  }

  @override
  Future<OwnedMapThread?> readThread(
    OwnedMapPin value,
    OwnedMapMomentDetail seed,
  ) async {
    threadReads.add(seed.id);
    final now = DateTime.now().toUtc(),
        data = _sequence(DateTime.now().toUtc(), seed: seed.id);
    for (final entry in data['entries'] as List) {
      if (entry['detail'] != null) {
        entry['detail'] = detailJSON(entry['detail']['id'] as String);
      }
    }
    return OwnedMapThread.decode(
      data,
      ownerID: _owner,
      seedMomentID: seed.id,
      now: now,
    );
  }

  @override
  Future<Uint8List> momentContent(
    OwnedMapMomentDetail detail,
    OwnedMapMomentMedia media, {
    bool Function()? current,
  }) async {
    if (current?.call() == false)
      throw const FormatException('retired fixture');
    return Uint8List.fromList(jpeg);
  }
}

Widget _feedbackPanel(_FeedbackPanelController c, {bool expanded = true}) =>
    MaterialApp(
      home: Scaffold(
        body: Align(
          alignment: Alignment.topLeft,
          child: SizedBox(
            width: 358,
            height: expanded ? 600 : 288,
            child: NowMapContentPanel(
              controller: c,
              pin: c.pin,
              current: () => true,
              changes: c.changes,
              onClose: () {},
              onExpand: () {},
              onEdit: (_, __) {},
              initialExpanded: expanded,
            ),
          ),
        ),
      ),
    );

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'thread decoder preserves hidden positions and rejects stale or smuggled content',
    () {
      final now = DateTime.now().toUtc();
      OwnedMapThread decode(Object data) => OwnedMapThread.decode(
        data,
        ownerID: _owner,
        seedMomentID: _seed,
        now: now,
      );
      final view = decode(_sequence(now));
      expect(view.entries.map((e) => e.position), [0, 1, 2]);
      expect(view.entries[1].available, isFalse);
      expect(view.entries[2].detail!.id, _sibling);
      for (final edit in <void Function(Map<String, Object?>)>[
        (v) => (v['entries'] as List)[1]['detail'] = _detail(_sibling, now),
        (v) => (v['entries'] as List)[1]['position'] = 3,
        (v) => (v['entries'] as List)[2]['detail'] = _detail(_seed, now),
        (v) => v['seedRevision'] = 2,
        (v) => v['viewerAccountId'] = _sibling,
        (v) => v['validUntil'] = now
            .subtract(const Duration(seconds: 1))
            .toIso8601String(),
      ]) {
        final value = _sequence(now);
        edit(value);
        expect(() => decode(value), throwsFormatException);
      }
    },
  );

  test(
    'only exact old mux 404 is compatible; domain missing seed retires grant',
    () async {
      final now = DateTime.now().toUtc();
      var mode = 'route', denied = 0;
      final api = OwnedMapAPI(
        client: MockClient(
          (_) async => switch (mode) {
            'route' => http.Response(
              '404 page not found\n',
              404,
              headers: {'content-type': 'text/plain; charset=utf-8'},
            ),
            'domain' => http.Response(
              jsonEncode({
                'error': {'code': 'not_found'},
              }),
              404,
              headers: {'content-type': 'application/json'},
            ),
            _ => http.Response(
              'something else',
              404,
              headers: {'content-type': 'text/plain'},
            ),
          },
        ),
        apiBaseUrl: () => _origin,
        onDenied: (_) => denied++,
      );
      Future<OwnedMapThread?> read() => api.readThread(
        _seed,
        ownerID: _owner,
        authorization: 'Bearer synthetic',
        origin: _origin,
        current: () => true,
        now: () => now,
      );
      expect(await read(), isNull);
      expect(denied, 0);
      mode = 'domain';
      await expectLater(read(), throwsA(isA<OwnedMapReadFailure>()));
      expect(denied, 1);
      mode = 'unknown';
      await expectLater(read(), throwsA(isA<OwnedMapReadFailure>()));
      expect(denied, 2);
      api.dispose();
    },
  );

  test(
    'thread grants reauthorize sibling playback, renew together and expire or retire on identity',
    () async {
      var now = DateTime.now().toUtc();
      var owner = _owner;
      final changes = ValueNotifier(0);
      Completer<http.Response>? delayed;
      var changedSibling = false;
      var threadReads = 0;
      final client = MockClient((request) async {
        if (request.url.path.endsWith('/thread')) {
          threadReads++;
          final data = _sequence(now, seed: request.url.pathSegments[4]);
          if (changedSibling) {
            (data['entries'] as List)[0] = {
              'position': 0,
              'status': 'UNAVAILABLE',
              'detail': null,
            };
          }
          if (delayed != null) return delayed!.future;
          return _json(data);
        }
        if (request.url.path == '/v1/me/map-content/places/$_place/moments') {
          return _json({
            'schemaVersion': 'map-content-place-moments-v1',
            'viewerAccountId': _owner,
            'placeId': _place,
            'items': [_detail(_seed, now)],
            'nextCursor': null,
            'sourceVersion': _source,
            ..._lease(now),
          });
        }
        throw StateError('Unexpected request ${request.url.path}');
      });
      final c = _GrantedController(
        client: client,
        identityChanges: changes,
        now: () => now,
        ownerID: () => owner,
      );
      final pin = OwnedMapPin(
        placeID: _place,
        name: '隔离地点',
        categoryCode: 'park',
        cityID: null,
        latitude: 57.1,
        longitude: -2.1,
        visibilitySources: const ['SELF_PRIVATE'],
        sourceVersion: _source,
        momentCount: 1,
        moments: const [],
        momentsTruncated: false,
        cover: null,
        observedAt: now,
        validUntil: now.add(const Duration(seconds: 30)),
      );
      final seed = (await c.readPlaceMoments(pin)).items.single;
      final thread = (await c.readThread(pin, seed))!;
      final sibling = thread.entries[2].detail!;
      expect(c.detailCurrent(sibling), isTrue);
      expect(c.momentCurrent(sibling.selector), isTrue);
      now = now.add(const Duration(seconds: 2));
      final fresh = await c.readMoment(sibling.selector);
      final renewed = c.threadForDetail(fresh)!;
      expect(threadReads, 2);
      expect(fresh.sourceVersion, sibling.sourceVersion);
      expect(renewed.validUntil.isAfter(thread.validUntil), isTrue);
      expect(c.detailCurrent(fresh), isTrue);
      now = now.add(const Duration(seconds: 4));
      expect(c.threadCurrent(thread), isFalse);
      expect(c.detailCurrent(sibling), isFalse);
      expect(c.detailCurrent(fresh), isTrue);
      changedSibling = true;
      await expectLater(c.readMoment(fresh.selector), throwsFormatException);
      expect(c.threadCurrent(renewed), isFalse);
      expect(c.detailCurrent(fresh), isFalse);
      changedSibling = false;
      final currentThread = (await c.readThread(pin, seed))!;
      final currentSibling = currentThread.entries[2].detail!;
      delayed = Completer<http.Response>();
      final stale = c.readMoment(currentSibling.selector);
      await Future<void>.delayed(Duration.zero);
      owner = _sibling;
      changes.value++;
      delayed!.complete(_json(_sequence(now, seed: _sibling)));
      await expectLater(stale, throwsFormatException);
      expect(c.detailCurrent(fresh), isFalse);
      expect(c.threadCurrent(renewed), isFalse);
      expect(c.threadCurrent(currentThread), isFalse);
      c.dispose();
      changes.dispose();
    },
  );

  testWidgets(
    'multiple place entries open their own authoritative thread at the selected position and return',
    (t) async {
      final c = _FeedbackPanelController(ValueNotifier(0));
      await t.pumpWidget(_feedbackPanel(c));
      await t.pump(const Duration(milliseconds: 100));
      expect(c.threadReads, isEmpty);
      final action = find.byKey(const ValueKey('read-thread:$_sibling'));
      await t.ensureVisible(action);
      await t.tap(action);
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(c.threadReads, [_sibling]);
      expect(find.text('串文 · 3段'), findsOneWidget);
      expect(find.text('目标第3段正文').hitTestable(), findsOneWidget);
      expect(find.text('首段正文').hitTestable(), findsNothing);
      await t.drag(find.byType(CustomScrollView), const Offset(0, 350));
      await t.pump(const Duration(milliseconds: 100));
      expect(find.text('第2段暂不可查看'), findsOneWidget);
      await t.tap(find.byTooltip('返回地点动态'));
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(find.text('串文 · 3段'), findsNothing);
      expect(
        find.byKey(const ValueKey('read-thread:$_sibling')),
        findsOneWidget,
      );
      await t.pumpWidget(const SizedBox());
      c.dispose();
      c.changes.dispose();
    },
  );

  testWidgets('a third-entry map anchor starts at that authoritative segment', (
    t,
  ) async {
    final c = _FeedbackPanelController(ValueNotifier(0), anchor: true);
    await t.pumpWidget(_feedbackPanel(c));
    await t.pump(const Duration(milliseconds: 100));
    expect(c.threadReads, [_sibling]);
    expect(find.text('目标第3段正文').hitTestable(), findsOneWidget);
    expect(find.text('首段正文').hitTestable(), findsNothing);
    await t.pumpWidget(const SizedBox());
    c.dispose();
    c.changes.dispose();
  });

  testWidgets(
    '288dp compact preview keeps media first and its long body inside the action budget',
    (t) async {
      final c = _FeedbackPanelController(ValueNotifier(0), images: true);
      await t.pumpWidget(_feedbackPanel(c, expanded: false));
      await t.pump(const Duration(milliseconds: 100));
      final frame = t.getRect(
        find.byKey(const ValueKey('moment-media-frame:$_seed:false')),
      );
      final body = t.getRect(
        find.byKey(const ValueKey('moment-preview-body:$_seed')),
      );
      final action = t.getRect(find.text('查看此地点的全部2条动态'));
      expect(frame.height, 144);
      expect(frame.top, lessThan(body.top));
      expect(body.bottom, lessThanOrEqualTo(action.top));
      expect(action.bottom, lessThanOrEqualTo(288));
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      c.changes.dispose();
    },
  );
}
