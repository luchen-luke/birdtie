import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'content_map_controller_test.dart' as f;
import 'content_map_reader_test.dart'
    show ReaderFixture, mountReader, disposeReader;

class LazyFixture extends ReaderFixture {
  final urls = <Uri>[];
  bool current = true;
  void Function()? onDescriptor, onBytes;
  Map<String, Object?> Function(Map<String, Object?>)? changeDescriptor;

  @override
  Map<String, Object?> media(int n, {bool isVideo = false}) {
    final original = super.media(n, isVideo: isVideo);
    if (isVideo) return original;
    return {
      ...original,
      'width': 320,
      'height': 180,
      'byteCount': 1000000,
      'sha256': f.hash('original-$n'),
      'displayDescriptorPath':
          '/v1/me/map-content/moments/${f.id(1)}/media/${f.id(6000 + n)}/display-descriptor?sourceVersion=${original['sourceVersion']}',
    };
  }

  @override
  Map<String, Object?> detail(int n) => {
    ...super.detail(n),
    'media': [for (var i = 1; i <= 200; i++) media(i)],
  };

  Map<String, Object?> descriptor(int n) {
    final original = media(n);
    final source = sha256
        .convert(
          utf8.encode(
            jsonEncode([original['sourceVersion'], 'display1280-v1']),
          ),
        )
        .toString();
    return {
      'schemaVersion': 'map-content-media-display-v1',
      'viewerAccountId': owner,
      'momentId': f.id(1),
      'originalSourceVersion': original['sourceVersion'],
      ...lease(),
      'media': {
        ...super.media(n),
        'sourceVersion': source,
        'contentPath':
            '/v1/me/map-content/moments/${f.id(1)}/media/${original['id']}/content?sourceVersion=$source&variant=display',
      },
    };
  }

  late final api = OwnedMapAPI(
    apiBaseUrl: () => f.base,
    client: MockClient((request) async {
      urls.add(request.url);
      expect(request.headers['Authorization'], 'Bearer fixture');
      expect(request.followRedirects, false);
      final path = request.url.path;
      final isBytes = path.endsWith('/content');
      Object? value;
      if (path.endsWith('/display-descriptor')) {
        final id = path.split('/')[7];
        final n = int.parse(id.split('-').last) - 6000;
        value = descriptor(n);
        if (changeDescriptor != null) {
          value = changeDescriptor!(value as Map<String, Object?>);
        }
        onDescriptor?.call();
      } else if (isBytes) {
        onBytes?.call();
      } else if (path.endsWith('/moments') && path.contains('/places/')) {
        value = {
          'schemaVersion': 'map-content-place-moments-v1',
          'viewerAccountId': owner,
          'placeId': f.id(2001),
          'items': [detail(1)],
          'sourceVersion': f.hash('place-page'),
          'nextCursor': null,
          ...lease(),
        };
      } else {
        value = detail(1);
      }
      return http.Response.bytes(
        isBytes ? f.jpg : utf8.encode(jsonEncode({'data': value})),
        200,
        headers: {
          'cache-control': 'private, no-store',
          'content-type': isBytes ? 'image/jpeg' : 'application/json',
          'x-content-type-options': 'nosniff',
        },
      );
    }),
  );

  Future<OwnedMapMomentDetail> readDetail() => api.readMoment(
    OwnedMapMoment(
      f.id(1),
      1,
      'SELF_PRIVATE',
      '/v1/me/map-content/moments/${f.id(1)}',
    ),
    ownerID: owner,
    authorization: 'Bearer fixture',
    origin: f.base,
    current: () => current,
    now: () => now,
  );
  Future<Uint8List> bytes(OwnedMapMomentDetail d, int index) =>
      api.momentContent(
        d.media[index],
        detail: d,
        now: () => now,
        authorization: 'Bearer fixture',
        origin: f.base,
        current: () => current,
      );
}

class _PanelLazyFixture extends LazyFixture {
  DateTime? descriptorUntil;
  @override
  Map<String, Object?> descriptor(int n) => {
    ...super.descriptor(n),
    if (descriptorUntil != null)
      'validUntil': descriptorUntil!.toIso8601String(),
  };
  final descriptorGates = <int, Completer<void>>{
    1: Completer<void>(),
    2: Completer<void>(),
  };
  final handledDescriptors = <int>{};

  @override
  Map<String, Object?> detail(int n) => {
    ...super.detail(n),
    'media': [media(1), media(2)],
  };

  List<Uri> get descriptorRequests => requests
      .map((r) => r.url)
      .where((u) => u.path.endsWith('/display-descriptor'))
      .toList();
  List<Uri> get byteRequests => requests
      .map((r) => r.url)
      .where((u) => u.path.endsWith('/content'))
      .toList();

  void releaseDescriptor(int n) {
    final gate = descriptorGates[n]!;
    if (!gate.isCompleted) gate.complete();
  }

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    final path = request.url.path;
    if (path.endsWith('/thread')) {
      return http.StreamedResponse(
        Stream.value(utf8.encode('404 page not found\n')),
        404,
        headers: {'content-type': 'text/plain'},
      );
    }
    if (!path.endsWith('/display-descriptor') && !path.endsWith('/content')) {
      return super.send(request);
    }
    requests.add(request);
    expect(request.headers['Authorization'], 'Bearer fixture');
    expect(request.followRedirects, false);
    final isDescriptor = path.endsWith('/display-descriptor');
    final n = int.parse(path.split('/')[7].split('-').last) - 6000;
    final body = isDescriptor
        ? utf8.encode(jsonEncode({'data': descriptor(n)}))
        : f.jpg;
    if (isDescriptor) {
      // Deliberately finish even when the page has retired: the real API must
      // reject a late descriptor before starting its subsequent bytes request.
      await descriptorGates[n]!.future;
      final response = StreamController<List<int>>();
      response.onListen = () {
        response.add(body);
        unawaited(response.close());
      };
      response.onCancel = () {
        handledDescriptors.add(n);
      };
      return http.StreamedResponse(
        response.stream,
        200,
        contentLength: body.length,
        headers: {
          'cache-control': 'private, no-store',
          'content-type': 'application/json',
          'x-content-type-options': 'nosniff',
        },
      );
    }
    return http.StreamedResponse(
      Stream.value(body),
      200,
      contentLength: body.length,
      headers: {
        'cache-control': 'private, no-store',
        'content-type': 'image/jpeg',
        'x-content-type-options': 'nosniff',
      },
    );
  }
}

class _PanelVideoRenewalFixture extends ReaderFixture {
  _PanelVideoRenewalFixture() {
    video = true;
  }

  final events = <String>[];
  final freshDetailGate = Completer<void>();
  final videoBytesGate = Completer<void>();
  bool shortInitialLease = false, holdVideoBytes = false;
  DateTime? initialDetailDeadline, freshDetailDeadline;

  @override
  Map<String, Object?> detail(int n) {
    final value = super.detail(n);
    if (!shortInitialLease) return value;
    final fresh = events.contains('fresh-detail');
    final deadline = now.add(Duration(seconds: fresh ? 30 : 2));
    if (fresh) {
      freshDetailDeadline = deadline;
    } else {
      initialDetailDeadline = deadline;
    }
    return {...value, 'validUntil': deadline.toIso8601String()};
  }

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    final uri = request.url;
    if (uri.path.endsWith('/thread')) {
      return http.StreamedResponse(
        Stream.value(utf8.encode('404 page not found\n')),
        404,
        headers: {'content-type': 'text/plain'},
      );
    }
    if (uri.path == '/v1/me/map-content/moments/${f.id(1)}') {
      final first = !events.contains('initial-detail');
      events.add(first ? 'initial-detail' : 'fresh-detail');
      if (!first) await freshDetailGate.future;
    } else if (uri.path.endsWith('/content') &&
        uri.queryParameters['variant'] != 'poster') {
      requests.add(request);
      events.add('video-bytes');
      expect(request.headers['Authorization'], 'Bearer fixture');
      expect(request.followRedirects, false);
      // This test stops at the real bytes request. A controlled server failure
      // avoids claiming platform player coverage from a synthetic MP4 fixture.
      if (holdVideoBytes) await videoBytesGate.future;
      return http.StreamedResponse(Stream.value(<int>[]), 500);
    }
    return super.send(request);
  }
}

class _NeighbourVideoFixture extends _PanelVideoRenewalFixture {
  _NeighbourVideoFixture() {
    shortInitialLease = true;
    holdVideoBytes = true;
  }

  @override
  Map<String, Object?> page(Uri uri) {
    final value = super.page(uri);
    final pin = (value['pins'] as List).single as Map<String, Object?>;
    return {
      ...value,
      'totalContentCount': 2,
      'pins': [
        {
          ...pin,
          'markerId': 'place:${f.id(2001)}',
          'kind': 'PLACE_CONTENT',
          'entityId': f.id(2001),
          'placeId': f.id(2001),
          'momentId': '',
          'anchorId': '',
          'entryId': '',
          'threadId': '',
          'momentCount': 2,
          'moments': [selector(2), selector(1)],
        },
      ],
    };
  }

  @override
  Map<String, Object?> detail(int n) {
    final value = super.detail(n);
    return n == 2
        ? {...value, 'title': '', 'body': '即将过期的前一条', 'media': <Object?>[]}
        : value;
  }

  http.StreamedResponse _jsonResponse(Object value) {
    final body = utf8.encode(jsonEncode({'data': value}));
    return http.StreamedResponse(
      Stream.value(body),
      200,
      contentLength: body.length,
      headers: {
        'cache-control': 'private, no-store',
        'content-type': 'application/json',
        'x-content-type-options': 'nosniff',
      },
    );
  }

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    final path = request.url.path;
    if (path == '/v1/me/map-content/places/${f.id(2001)}/moments' ||
        path == '/v1/me/map-content/moments/${f.id(1)}') {
      requests.add(request);
      expect(request.headers['Authorization'], 'Bearer fixture');
      expect(request.followRedirects, false);
      if (path.contains('/places/')) {
        events.add('place-page');
        return _jsonResponse({
          'schemaVersion': 'map-content-place-moments-v1',
          'viewerAccountId': owner,
          'placeId': f.id(2001),
          'items': [detail(2), detail(1)],
          'sourceVersion': f.hash('neighbour-place-page'),
          'nextCursor': null,
          ...lease(),
        });
      }
      events.add('fresh-detail');
      await freshDetailGate.future;
      return _jsonResponse(detail(1));
    }
    return super.send(request);
  }
}

Future<(OwnedMapController, ValueNotifier<int>)> _mountLazyPanel(
  WidgetTester t,
  _PanelLazyFixture fixture,
) async {
  final identity = ValueNotifier(0);
  final controller = OwnedMapController(
    client: fixture,
    apiBaseUrl: () => f.base,
    ownerID: () => fixture.owner,
    authorizationHeader: () => 'Bearer fixture',
    organizationWorkspaceID: () => fixture.organization,
    identityChanges: identity,
    refreshAllowed: () => true,
    now: () => fixture.now,
  );
  controller.updateQuery('', f.bounds);
  await f.drain(t, () => controller.current);
  final pin = controller.items.single, epoch = controller.permissionEpoch;
  var open = true;
  await t.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: StatefulBuilder(
          builder: (context, setState) => Align(
            alignment: Alignment.topCenter,
            child: SizedBox(
              width: 360,
              height: 640,
              child: open
                  ? NowMapContentPanel(
                      controller: controller,
                      pin: pin,
                      initialExpanded: true,
                      current: () =>
                          controller.permissionEpoch == epoch &&
                          controller.find(pin.mapID)?.sourceVersion ==
                              pin.sourceVersion,
                      changes: controller,
                      onClose: () => setState(() => open = false),
                      onExpand: () {},
                      onEdit: (_, _) {},
                    )
                  : const SizedBox(key: ValueKey('closed-map-content-panel')),
            ),
          ),
        ),
      ),
    ),
  );
  await f.drain(t, () => fixture.descriptorRequests.isNotEmpty);
  return (controller, identity);
}

void main() {
  testWidgets('media04 fullscreen handoff joins the pending image request', (
    t,
  ) async {
    final fixture = _PanelLazyFixture()..now = DateTime.now().toUtc();
    final (controller, identity) = await _mountLazyPanel(t, fixture);
    try {
      expect(fixture.descriptorRequests.length, 1);
      await t.tap(find.byType(PageView));
      await t.pump();
      expect(find.byTooltip('返回动态'), findsOneWidget);
      fixture.releaseDescriptor(1);
      await f.drain(
        t,
        () => find
            .descendant(of: find.byType(PageView), matching: find.byType(Image))
            .evaluate()
            .isNotEmpty,
      );
      expect(fixture.descriptorRequests.length, 1);
      expect(fixture.byteRequests.length, 1);
      await t.tap(find.byTooltip('返回动态'));
      await t.pump();
      await f.drain(
        t,
        () => find
            .descendant(of: find.byType(PageView), matching: find.byType(Image))
            .evaluate()
            .isNotEmpty,
      );
      expect(fixture.descriptorRequests.length, 1);
      expect(fixture.byteRequests.length, 1);
      expect(t.takeException(), isNull);
    } finally {
      fixture.releaseDescriptor(1);
      fixture.releaseDescriptor(2);
      await disposeReader(t, controller, identity);
    }
  });
  test(
    'media04 display bytes retain the shorter descriptor deadline',
    () async {
      final x = LazyFixture();
      addTearDown(x.api.dispose);
      final until = x.now.add(const Duration(seconds: 2));
      x.changeDescriptor = (value) => {
        ...value,
        'validUntil': until.toIso8601String(),
      };
      final detail = await x.readDetail();
      final bytes = await x.bytes(detail, 0);
      expect(x.api.contentValidUntil(bytes), until);
      expect(until.isBefore(detail.validUntil), true);
      bytes.fillRange(0, bytes.length, 0);
    },
  );
  testWidgets(
    'media04 cached image retires at descriptor expiry before detail expiry',
    (t) async {
      final fixture = _PanelLazyFixture()..now = DateTime.now().toUtc();
      final until = fixture.now.add(const Duration(milliseconds: 700));
      fixture.changeDescriptor = (value) => {
        ...value,
        'validUntil': until.toIso8601String(),
      };
      // The real transport below uses descriptor(), so bind its narrow lease there.
      fixture.descriptorUntil = until;
      final (controller, identity) = await _mountLazyPanel(t, fixture);
      try {
        fixture.releaseDescriptor(1);
        await f.drain(
          t,
          () => find
              .descendant(
                of: find.byType(PageView),
                matching: find.byType(Image),
              )
              .evaluate()
              .isNotEmpty,
        );
        await t.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 750)),
        );
        await t.pump(const Duration(seconds: 1));
        expect(controller.current, true);
        expect(
          find.descendant(
            of: find.byType(PageView),
            matching: find.byType(Image),
          ),
          findsNothing,
        );
        fixture.now = DateTime.now().toUtc();
        fixture.descriptorUntil = null;
        await t.tap(find.byType(PageView));
        await t.pump();
        await f.drain(t, () => fixture.descriptorRequests.length == 2);
        await f.drain(t, () => fixture.byteRequests.length == 2);
        expect(t.takeException(), isNull);
      } finally {
        fixture.releaseDescriptor(1);
        fixture.releaseDescriptor(2);
        await disposeReader(t, controller, identity);
      }
    },
  );
  testWidgets(
    'panel page change retires a pending descriptor before old bytes start',
    (t) async {
      final fixture = _PanelLazyFixture()..now = DateTime.now().toUtc();
      final (controller, identity) = await _mountLazyPanel(t, fixture);
      try {
        expect(
          fixture.descriptorRequests.single.path,
          endsWith('/${f.id(6001)}/display-descriptor'),
        );
        expect(fixture.byteRequests, isEmpty);
        await t.tap(find.byTooltip('下一张'));
        await t.pump();
        await t.pump(const Duration(milliseconds: 250));
        await f.drain(t, () => fixture.descriptorRequests.length == 2);
        expect(find.text('媒体 2 / 2'), findsOneWidget);
        expect(controller.current, isTrue);

        fixture.releaseDescriptor(2);
        await f.drain(
          t,
          () => find
              .byWidgetPredicate(
                (widget) => widget is RawImage && widget.image != null,
              )
              .evaluate()
              .isNotEmpty,
        );
        expect(
          fixture.byteRequests.single.path,
          endsWith('/${f.id(6002)}/content'),
        );
        expect(
          fixture.byteRequests.single.queryParameters['variant'],
          'display',
        );

        fixture.releaseDescriptor(1);
        await f.drain(t, () => fixture.handledDescriptors.contains(1));
        await t.pump();
        expect(fixture.byteRequests.length, 1);
        expect(
          fixture.byteRequests.any(
            (u) => u.path.endsWith('/${f.id(6001)}/content'),
          ),
          isFalse,
        );
        expect(find.text('媒体 2 / 2'), findsOneWidget);
        expect(t.takeException(), isNull);
      } finally {
        fixture.releaseDescriptor(1);
        fixture.releaseDescriptor(2);
        await disposeReader(t, controller, identity);
      }
    },
  );

  testWidgets(
    'closing the real panel retires its pending descriptor before bytes start',
    (t) async {
      final fixture = _PanelLazyFixture()..now = DateTime.now().toUtc();
      final (controller, identity) = await _mountLazyPanel(t, fixture);
      try {
        expect(fixture.descriptorRequests.length, 1);
        expect(fixture.byteRequests, isEmpty);
        await t.tap(find.byTooltip('返回地图'));
        await t.pump();
        expect(find.byType(NowMapContentPanel), findsNothing);
        expect(
          find.byKey(const ValueKey('closed-map-content-panel')),
          findsOneWidget,
        );
        // The owner and map grant remain valid; only the panel was closed.
        expect(controller.current, isTrue);

        fixture.releaseDescriptor(1);
        await f.drain(t, () => fixture.handledDescriptors.contains(1));
        await t.pump();
        expect(fixture.byteRequests, isEmpty);
        expect(controller.current, isTrue);
        expect(t.takeException(), isNull);
      } finally {
        fixture.releaseDescriptor(1);
        fixture.releaseDescriptor(2);
        await disposeReader(t, controller, identity);
      }
    },
  );

  testWidgets(
    'explicit panel video play refreshes detail before requesting video bytes',
    (t) async {
      final fixture = _PanelVideoRenewalFixture()..now = DateTime.now().toUtc();
      final (controller, identity) = await mountReader(t, fixture);
      try {
        expect(fixture.events, ['initial-detail']);
        await t.tap(find.byTooltip('播放视频'));
        await f.drain(t, () => fixture.events.contains('fresh-detail'));
        expect(fixture.events, ['initial-detail', 'fresh-detail']);
        expect(
          fixture.requests.where(
            (r) =>
                r.url.path.endsWith('/content') &&
                r.url.queryParameters['variant'] != 'poster',
          ),
          isEmpty,
        );

        fixture.freshDetailGate.complete();
        await f.drain(t, () => fixture.events.contains('video-bytes'));
        await f.drain(t, () => find.byTooltip('重试播放').evaluate().isNotEmpty);
        expect(fixture.events, [
          'initial-detail',
          'fresh-detail',
          'video-bytes',
        ]);
        expect(
          fixture.requests
              .where(
                (r) => r.url.path == '/v1/me/map-content/moments/${f.id(1)}',
              )
              .length,
          2,
        );
        expect(
          fixture.requests
              .where(
                (r) =>
                    r.url.path.endsWith('/content') &&
                    r.url.queryParameters['variant'] != 'poster',
              )
              .single
              .url
              .path,
          endsWith('/${f.id(6001)}/content'),
        );
        expect(t.takeException(), isNull);
      } finally {
        if (!fixture.freshDetailGate.isCompleted) {
          fixture.freshDetailGate.complete();
        }
        await disposeReader(t, controller, identity);
      }
    },
  );

  testWidgets(
    'fresh video detail survives the original panel deadline during transfer',
    (t) async {
      final fixture = _PanelVideoRenewalFixture()
        ..now = DateTime.now().toUtc()
        ..shortInitialLease = true
        ..holdVideoBytes = true;
      final (controller, identity) = await mountReader(t, fixture);
      try {
        final initialDeadline = fixture.initialDetailDeadline!;
        expect(
          initialDeadline.difference(fixture.now),
          const Duration(seconds: 2),
        );
        expect(fixture.events, ['initial-detail']);
        await t.tap(find.byTooltip('播放视频'));
        await f.drain(t, () => fixture.events.contains('fresh-detail'));
        fixture.freshDetailGate.complete();
        await f.drain(t, () => fixture.events.contains('video-bytes'));
        final freshDeadline = fixture.freshDetailDeadline!;
        expect(
          freshDeadline.difference(fixture.now),
          const Duration(seconds: 30),
        );

        // Advance the injected API/controller clock and the widget timer clock
        // together. pump does not replace production DateTime.now(); the real
        // initial deadline was <= 2 seconds away when its timers were armed.
        fixture.now = fixture.now.add(const Duration(seconds: 3));
        await t.pump(const Duration(seconds: 3));
        await t.pump();
        expect(fixture.now.isAfter(initialDeadline), isTrue);
        expect(fixture.now.isBefore(freshDeadline), isTrue);
        expect(controller.current, isTrue);
        expect(find.text('真实内容只出现一次'), findsOneWidget);
        expect(find.text('1:05'), findsOneWidget);
        expect(
          find.byKey(ValueKey('moment-media-frame:${f.id(1)}:false')),
          findsOneWidget,
        );
        expect(find.byTooltip('重试播放'), findsNothing);
        expect(fixture.events, [
          'initial-detail',
          'fresh-detail',
          'video-bytes',
        ]);

        fixture.videoBytesGate.complete();
        await f.drain(t, () => find.byTooltip('重试播放').evaluate().isNotEmpty);
        expect(find.text('真实内容只出现一次'), findsOneWidget);
        expect(find.text('1:05'), findsOneWidget);
        expect(fixture.events, [
          'initial-detail',
          'fresh-detail',
          'video-bytes',
        ]);
        expect(t.takeException(), isNull);
      } finally {
        if (!fixture.freshDetailGate.isCompleted) {
          fixture.freshDetailGate.complete();
        }
        if (!fixture.videoBytesGate.isCompleted) {
          fixture.videoBytesGate.complete();
        }
        await disposeReader(t, controller, identity);
      }
    },
  );

  testWidgets(
    'expiring an earlier entry preserves the shifted video state and request',
    (t) async {
      final fixture = _NeighbourVideoFixture()..now = DateTime.now().toUtc();
      final (controller, identity) = await mountReader(t, fixture);
      try {
        expect(find.text('即将过期的前一条'), findsOneWidget);
        expect(find.text('真实内容只出现一次'), findsOneWidget);
        expect(fixture.events, ['place-page']);
        final media = OwnedMapMomentMedia.decode(
          fixture.media(1, isVideo: true),
          f.id(1),
          'SELF_PRIVATE',
        );
        final video = find.byKey(ValueKey(media.identity));
        final initialState = t.state(video);
        await t.ensureVisible(find.byTooltip('播放视频'));
        await t.pump();
        await t.tap(find.byTooltip('播放视频'));
        await f.drain(t, () => fixture.events.contains('fresh-detail'));
        fixture.freshDetailGate.complete();
        await f.drain(t, () => fixture.events.contains('video-bytes'));
        expect(identical(t.state(video), initialState), isTrue);

        fixture.now = fixture.now.add(const Duration(seconds: 3));
        await t.pump(const Duration(seconds: 3));
        await t.pump();
        expect(fixture.now.isAfter(fixture.initialDetailDeadline!), isTrue);
        expect(fixture.now.isBefore(fixture.freshDetailDeadline!), isTrue);
        expect(find.text('即将过期的前一条'), findsNothing);
        expect(find.text('真实内容只出现一次'), findsOneWidget);
        expect(find.text('1:05'), findsOneWidget);
        expect(
          identical(t.state(video), initialState),
          isTrue,
          reason:
              'Removing the preceding row must move the existing video '
              'element instead of disposing the in-flight playback state.',
        );
        expect(fixture.events, ['place-page', 'fresh-detail', 'video-bytes']);

        fixture.videoBytesGate.complete();
        await f.drain(t, () => find.byTooltip('重试播放').evaluate().isNotEmpty);
        expect(identical(t.state(video), initialState), isTrue);
        expect(fixture.events, ['place-page', 'fresh-detail', 'video-bytes']);
        expect(controller.current, isTrue);
        expect(t.takeException(), isNull);
      } finally {
        if (!fixture.freshDetailGate.isCompleted) {
          fixture.freshDetailGate.complete();
        }
        if (!fixture.videoBytesGate.isCompleted) {
          fixture.videoBytesGate.complete();
        }
        await disposeReader(t, controller, identity);
      }
    },
  );

  test(
    '200-image detail and place page fetch only the selected display bytes',
    () async {
      final x = LazyFixture();
      addTearDown(x.api.dispose);
      final detail = await x.readDetail();
      expect(detail.media.length, 200);
      expect(x.urls.length, 1);
      expect(x.urls.single.queryParameters, {'mediaMode': 'lazy'});
      expect(await x.bytes(detail, 0), f.jpg);
      expect(x.urls.length, 3);
      expect(x.urls[1].path, endsWith('/${f.id(6001)}/display-descriptor'));
      expect(x.urls[2].queryParameters['variant'], 'display');
      expect(await x.bytes(detail, 199), f.jpg);
      expect(x.urls.length, 5);
      expect(x.urls[3].path, endsWith('/${f.id(6200)}/display-descriptor'));
      final page = await x.api.readPlaceMoments(
        f.id(2001),
        cursor: 'next',
        ownerID: x.owner,
        authorization: 'Bearer fixture',
        origin: f.base,
        current: () => true,
        now: () => x.now,
      );
      expect(page.items.single.media.length, 200);
      expect(x.urls.last.queryParameters, {
        'mediaMode': 'lazy',
        'cursor': 'next',
      });
      expect(x.urls.length, 6);
    },
  );

  test('lazy selector rejects another target, arbitrary route and video', () {
    final x = LazyFixture();
    final original = x.media(1);
    OwnedMapMomentMedia decode(Map<String, Object?> value) =>
        OwnedMapMomentMedia.decode(value, f.id(1), 'SELF_PRIVATE');
    expect(decode(original).displayDescriptorPath, isNotNull);
    for (final bad in <Map<String, Object?>>[
      {'displayDescriptorPath': ''},
      {'displayDescriptorPath': 3},
      {
        'displayDescriptorPath':
            'https://other.test${original['displayDescriptorPath']}',
      },
      {'displayDescriptorPath': '${original['displayDescriptorPath']}&extra=1'},
      {
        'displayDescriptorPath':
            '${original['displayDescriptorPath']}#fragment',
      },
      {
        'displayDescriptorPath': (original['displayDescriptorPath'] as String)
            .replaceFirst(f.id(1), f.id(2)),
      },
      {
        'displayDescriptorPath': (original['displayDescriptorPath'] as String)
            .replaceFirst(f.hash('reader-1'), f.hash('wrong')),
      },
      {
        ...x.media(1, isVideo: true),
        'displayDescriptorPath': original['displayDescriptorPath'],
      },
    ]) {
      expect(() => decode({...original, ...bad}), throwsFormatException);
    }
  });

  test(
    'display response binds owner moment original source media and revision',
    () async {
      for (final change in <Map<String, Object?>>[
        {'viewerAccountId': f.id(9)},
        {'momentId': f.id(2)},
        {'originalSourceVersion': f.hash('wrong')},
        {'schemaVersion': 'unknown'},
        {'validUntil': '2025-01-01T00:00:00Z'},
        {'extra': true},
      ]) {
        final x = LazyFixture()..changeDescriptor = (v) => {...v, ...change};
        addTearDown(x.api.dispose);
        final detail = await x.readDetail();
        await expectLater(x.bytes(detail, 0), throwsFormatException);
        expect(x.urls.where((u) => u.path.endsWith('/content')), isEmpty);
      }
      for (final change in <Map<String, Object?>>[
        {'revision': 2},
        {'id': f.id(6002)},
        {'sourceVersion': f.hash('wrong')},
        {'byteCount': 262145},
        {'width': 161, 'height': 80},
        {'displayDescriptorPath': '/recursive'},
      ]) {
        final x = LazyFixture()
          ..changeDescriptor = (v) => {
            ...v,
            'media': {...v['media'] as Map<String, Object?>, ...change},
          };
        addTearDown(x.api.dispose);
        final detail = await x.readDetail();
        await expectLater(x.bytes(detail, 0), throwsFormatException);
        expect(x.urls.where((u) => u.path.endsWith('/content')), isEmpty);
      }
    },
  );

  test(
    'retirement during metadata prevents bytes and shorter descriptor lease wins',
    () async {
      final x = LazyFixture();
      addTearDown(x.api.dispose);
      final detail = await x.readDetail();
      x.onDescriptor = () => x.current = false;
      await expectLater(x.bytes(detail, 0), throwsFormatException);
      expect(x.urls.length, 2);
      final y = LazyFixture();
      addTearDown(y.api.dispose);
      final d = await y.readDetail();
      y.changeDescriptor = (v) => {
        ...v,
        'validUntil': y.now.add(const Duration(seconds: 2)).toIso8601String(),
      };
      y.onBytes = () => y.now = y.now.add(const Duration(seconds: 3));
      await expectLater(y.bytes(d, 0), throwsFormatException);
      expect(d.validUntil.isAfter(y.now), isTrue);
    },
  );
}
