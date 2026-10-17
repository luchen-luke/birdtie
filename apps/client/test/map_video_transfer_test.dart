import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'package:birdtie_client/src/content/video_material_player_io.dart';
import 'package:birdtie_client/src/content/video_material_sink_io.dart';
import 'package:birdtie_client/src/workspace/map_media_read_metrics.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'content_map_controller_test.dart' as f;
import 'content_map_reader_test.dart' show ReaderFixture;

final payload = Uint8List.fromList([
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

class _VideoClient extends http.BaseClient {
  _VideoClient(this.body, {this.gate, this.onFirst, this.declaredLength});
  final Uint8List body;
  final Completer<void>? gate;
  final void Function()? onFirst;
  final int? declaredLength;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    expect(request.headers['Authorization'], 'Bearer fixture');
    expect(request.headers.containsKey('Range'), false);
    expect(request.followRedirects, false);
    Stream<List<int>> chunks() async* {
      yield body.sublist(0, 7);
      onFirst?.call();
      if (gate != null) await gate!.future;
      yield body.sublist(7, 17);
      yield body.sublist(17);
    }

    return http.StreamedResponse(
      chunks(),
      200,
      contentLength: declaredLength ?? body.length,
      headers: {
        'cache-control': 'private, no-store',
        'content-type': 'video/mp4',
        'x-content-type-options': 'nosniff',
      },
    );
  }
}

OwnedMapMomentDetail videoDetail({Uint8List? bytes, DateTime? now}) {
  final fixture = ReaderFixture()..video = true;
  if (now != null) fixture.now = now;
  final data = fixture.detail(1);
  data['media'] = [
    {
      ...fixture.media(1, isVideo: true),
      'byteCount': (bytes ?? payload).length,
      'sha256': sha256.convert(bytes ?? payload).toString(),
    },
  ];
  return OwnedMapMomentDetail.decode(
    data,
    ownerID: fixture.owner,
    now: fixture.now,
  );
}

class _ControlledVideoClient extends http.BaseClient {
  _ControlledVideoClient(this.stream, this.length);
  final Stream<List<int>> stream;
  final int length;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async =>
      http.StreamedResponse(
        stream,
        200,
        contentLength: length,
        headers: {
          'cache-control': 'private, no-store',
          'content-type': 'video/mp4',
          'x-content-type-options': 'nosniff',
        },
      );
}

class _RenewingVideoFixture extends ReaderFixture {
  _RenewingVideoFixture() {
    video = true;
  }
  Future<void> Function(int)? advance;
  int detailStatus = 200, detailReads = 0;
  String? changedDetailSource;
  bool cancelled = false, rejectFinal = false;
  bool metadataWaiting = false,
      metadataCancelled = false,
      bodyRequested = false;
  Completer<void>? metadataGate;
  int gateAtRead = 2;
  StreamController<List<int>>? controlledBody;
  @override
  Map<String, Object?> media(int n, {bool isVideo = false}) => {
    ...super.media(n, isVideo: isVideo),
    'byteCount': payload.length,
    'sha256': sha256.convert(payload).toString(),
  };
  @override
  Map<String, Object?> detail(int n) => {
    ...super.detail(n),
    if (changedDetailSource != null) 'sourceVersion': changedDetailSource,
  };
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    if (request.url.path == '/v1/me/map-content/moments/${f.id(1)}') {
      detailReads++;
      if (detailStatus != 200) {
        return http.StreamedResponse(
          Stream.value(<int>[]),
          detailStatus,
          headers: {'cache-control': 'no-store'},
        );
      }
      final gate = metadataGate;
      if (gate != null && detailReads == gateAtRead) {
        final response = await super.send(request);
        metadataWaiting = true;
        final abort = (request as http.AbortableRequest).abortTrigger;
        await Future.any<void>([
          gate.future,
          if (abort != null)
            abort.then<void>((_) {
              metadataCancelled = true;
              throw http.RequestAbortedException(request.url);
            }),
        ]);
        return response;
      }
    }
    if (!request.url.path.endsWith('/content') ||
        request.url.queryParameters['variant'] != null) {
      return super.send(request);
    }
    requests.add(request);
    bodyRequested = true;
    Stream<List<int>> chunks() async* {
      try {
        const ends = [4, 8, 12, 16, 20, 22, 24];
        var start = 0;
        for (var i = 0; i < ends.length; i++) {
          if (i > 0) await advance?.call(i);
          yield payload.sublist(start, ends[i]);
          start = ends[i];
        }
        if (rejectFinal) detailStatus = 403;
      } finally {
        cancelled = true;
      }
    }

    return http.StreamedResponse(
      controlledBody?.stream ?? chunks(),
      200,
      contentLength: payload.length,
      headers: {
        'cache-control': 'private, no-store',
        'content-type': 'video/mp4',
        'x-content-type-options': 'nosniff',
      },
    );
  }
}

Future<Set<String>> temporaryReads() async => {
  await for (final entry in Directory.systemTemp.list())
    if (entry is Directory &&
        entry.path
            .split(Platform.pathSeparator)
            .last
            .startsWith('birdtie-video-read-'))
      entry.path,
};

void main() {
  test(
    'WAN sink cancellation waits for its in-flight write before close and cleanup',
    () async {
      final before = await temporaryReads();
      final sink = await VideoMaterialSink.create(limit: payload.length);
      final write = sink.add(payload);
      final discarded = sink.discard();
      await expectLater(write, throwsFormatException);
      await discarded;
      await sink.discard();
      expect(await temporaryReads(), before);
    },
  );
  test(
    'WAN sink finish racing discard never registers a late player path',
    () async {
      final before = await temporaryReads();
      final sink = await VideoMaterialSink.create(limit: payload.length);
      await sink.add(payload);
      final finish = sink.finish();
      final discarded = sink.discard();
      await expectLater(finish, throwsFormatException);
      await discarded;
      expect(await temporaryReads(), before);
    },
  );
  testWidgets(
    'WAN video fake-clock40s needs fresh exact grants while old30s expires',
    (t) async {
      var now = DateTime.utc(2026, 10, 16);
      final original = videoDetail(now: now);
      var active = original;
      var cancelled = false;
      final stream = StreamController<List<int>>(
        onCancel: () {
          cancelled = true;
        },
      );
      final api = OwnedMapAPI(
        client: _ControlledVideoClient(stream.stream, payload.length),
        apiBaseUrl: () => f.base,
      );
      final received = <int>[];
      var completed = false;
      Object? failure;
      final transfer = api
          .transferVideo(
            original.media.single,
            detail: original,
            now: () => now,
            authorization: 'Bearer fixture',
            origin: f.base,
            current: () => true,
            authorizedDetail: () => active,
            receive: (b) async {
              received.addAll(b);
            },
            trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
          )
          .then(
            (_) {
              completed = true;
            },
            onError: (Object e) {
              failure = e;
              completed = true;
            },
          );
      await t.pump();
      stream.add(payload.sublist(0, 4));
      await t.pump();
      for (var i = 1; i <= 5; i++) {
        now = now.add(const Duration(seconds: 8));
        active = videoDetail(now: now);
        await t.pump(const Duration(seconds: 8));
        final from = i == 1 ? 4 : 4 + (i - 1) * 4;
        stream.add(payload.sublist(from, i == 5 ? payload.length : from + 4));
        await t.pump();
      }
      unawaited(stream.close());
      await t.pump();
      await f.drain(t, () => completed);
      await transfer;
      expect(failure, isNull, reason: 'received=${received.length}; now=$now');
      expect(received, payload);
      expect(original.validUntil.isAfter(now), false);
      expect(active.validUntil.isAfter(now), true);
      expect(cancelled, true);
      api.dispose();
    },
  );

  testWidgets(
    'WAN video absolute110s cancels progressing stream before idle timeout',
    (t) async {
      var now = DateTime.utc(2026, 10, 16);
      final body = Uint8List.fromList([...payload, ...List.filled(176, 0)]);
      final original = videoDetail(bytes: body, now: now);
      var active = original;
      var cancelled = false, completed = false;
      Object? error;
      final stream = StreamController<List<int>>(
        onCancel: () {
          cancelled = true;
        },
      );
      final api = OwnedMapAPI(
        client: _ControlledVideoClient(stream.stream, body.length),
        apiBaseUrl: () => f.base,
      );
      final transfer = api
          .transferVideo(
            original.media.single,
            detail: original,
            now: () => now,
            authorization: 'Bearer fixture',
            origin: f.base,
            current: () => true,
            authorizedDetail: () => active,
            receive: (_) async {},
            trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
          )
          .then(
            (_) {
              completed = true;
            },
            onError: (Object e) {
              error = e;
              completed = true;
            },
          );
      await t.pump();
      stream.add(body.sublist(0, 12));
      await t.pump();
      for (var i = 0; i < 13; i++) {
        now = now.add(const Duration(seconds: 8));
        active = videoDetail(bytes: body, now: now);
        await t.pump(const Duration(seconds: 8));
        stream.add([body[12 + i]]);
        await t.pump();
      }
      await t.pump(const Duration(seconds: 7));
      await f.drain(t, () => completed);
      await transfer;
      expect(completed, true);
      expect(error, isA<TimeoutException>());
      expect(
        (error as TimeoutException).duration,
        const Duration(seconds: 110),
      );
      expect(cancelled, true);
      api.dispose();
      unawaited(stream.close());
    },
  );

  testWidgets(
    'WAN video enlarged transport budget cannot use the original grant after30s',
    (t) async {
      var now = DateTime.utc(2026, 10, 16);
      var completed = false;
      final detail = videoDetail(now: now);
      final stream = StreamController<List<int>>();
      final api = OwnedMapAPI(
        client: _ControlledVideoClient(stream.stream, payload.length),
        apiBaseUrl: () => f.base,
      );
      final received = <int>[];
      Object? error;
      final transfer = api
          .transferVideo(
            detail.media.single,
            detail: detail,
            now: () => now,
            authorization: 'Bearer fixture',
            origin: f.base,
            current: () => true,
            receive: (b) async {
              received.addAll(b);
            },
            trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
          )
          .then(
            (_) {
              completed = true;
            },
            onError: (Object e) {
              error = e;
              completed = true;
            },
          );
      await t.pump();
      stream.add(payload.sublist(0, 4));
      await t.pump();
      for (var i = 1; i <= 4; i++) {
        now = now.add(const Duration(seconds: 8));
        await t.pump(const Duration(seconds: 8));
        stream.add(payload.sublist(i * 4, i * 4 + 4));
        await t.pump();
      }
      await f.drain(t, () => completed);
      await transfer;
      expect(completed, true);
      expect(error, isA<FormatException>());
      expect(received.length, 16);
      api.dispose();
      unawaited(stream.close());
    },
  );

  testWidgets(
    'WAN video explicit cancellation interrupts empty body wait immediately',
    (t) async {
      final abort = Completer<void>();
      var cancelled = false, completed = false;
      final stream = StreamController<List<int>>(
        onCancel: () {
          cancelled = true;
        },
      );
      final api = OwnedMapAPI(
        client: _ControlledVideoClient(stream.stream, payload.length),
        apiBaseUrl: () => f.base,
      );
      final detail = videoDetail();
      final transfer = api
          .transferVideo(
            detail.media.single,
            detail: detail,
            now: () => DateTime.utc(2026, 10, 16),
            authorization: 'Bearer fixture',
            origin: f.base,
            current: () => true,
            abortTrigger: abort.future,
            receive: (_) async {},
            trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
          )
          .then(
            (_) {},
            onError: (Object e) {
              expect(e, isA<http.RequestAbortedException>());
              completed = true;
            },
          );
      await t.pump();
      abort.complete();
      await t.pump();
      await f.drain(t, () => completed);
      expect(completed, true);
      expect(cancelled, true);
      await transfer;
      api.dispose();
      unawaited(stream.close());
    },
  );

  testWidgets(
    'WAN controller42s renews checked authority and leaves old detail expired',
    (t) async {
      await t.runAsync(() async {
        final fixture = _RenewingVideoFixture();
        final identity = ValueNotifier(0);
        final c = OwnedMapController(
          client: fixture,
          apiBaseUrl: () => f.base,
          ownerID: () => fixture.owner,
          authorizationHeader: () => 'Bearer fixture',
          organizationWorkspaceID: () => fixture.organization,
          identityChanges: identity,
          refreshAllowed: () => true,
          now: () => fixture.now,
        );
        c.updateQuery('', f.bounds);
        await c.refresh();
        final original = await c.readMoment(c.items.single.moments.single);
        final accepted = <OwnedMapMomentDetail>[];
        fixture.advance = (_) async {
          fixture.now = fixture.now.add(const Duration(seconds: 7));
          await c.refresh();
        };
        final before = await temporaryReads();
        String? path;
        try {
          path = await c.prepareVideo(
            original,
            original.media.single,
            current: () => true,
            trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
            onFreshDetail: (fresh) async {
              accepted.add(fresh);
              return true;
            },
          );
          expect(await File(path).readAsBytes(), payload);
          expect(c.detailCurrent(original), false);
          expect(original.validUntil, DateTime.utc(2026, 10, 16, 0, 0, 30));
          expect(accepted.length, greaterThanOrEqualTo(3));
          expect(c.detailCurrent(accepted.last), true);
          expect(
            accepted.every((v) => v.sourceVersion == original.sourceVersion),
            true,
          );
          expect(fixture.detailReads, greaterThanOrEqualTo(4));
        } finally {
          if (path != null) await releaseVideoMaterialPath(path);
          c.dispose();
          identity.dispose();
          fixture.close();
        }
        expect(await temporaryReads(), before);
      });
    },
  );

  for (final boundary in [
    'metadata',
    '401',
    '403',
    'identity-ABA',
    'city',
    'transport',
    'cancel',
    'final403',
    'reject-visible-grant',
  ]) {
    testWidgets(
      'WAN controller $boundary aborts and discards unpublished material',
      (t) async {
        await t.runAsync(() async {
          final fixture = _RenewingVideoFixture();
          final identity = ValueNotifier(0);
          final abort = Completer<void>();
          final c = OwnedMapController(
            client: fixture,
            apiBaseUrl: () => f.base,
            ownerID: () => fixture.owner,
            authorizationHeader: () => 'Bearer fixture',
            organizationWorkspaceID: () => fixture.organization,
            identityChanges: identity,
            refreshAllowed: () => true,
            now: () => fixture.now,
          );
          c.updateQuery('', f.bounds);
          await c.refresh();
          final original = await c.readMoment(c.items.single.moments.single);
          fixture.rejectFinal = boundary == 'final403';
          fixture.advance = (i) async {
            if (i != 1 || boundary == 'final403') return;
            fixture.now = fixture.now.add(const Duration(seconds: 18));
            await c.refresh();
            switch (boundary) {
              case 'metadata':
                fixture.changedDetailSource = f.hash('changed-video');
              case '401':
                fixture.detailStatus = 401;
              case '403':
                fixture.detailStatus = 403;
              case 'identity-ABA':
                final old = fixture.owner;
                fixture.owner = f.id(9999);
                identity.value++;
                fixture.owner = old;
                identity.value++;
              case 'city':
                c.updateQuery(f.id(7000), f.bounds);
              case 'transport':
                c.replaceTransport(client: fixture);
              case 'cancel':
                abort.complete();
            }
          };
          final before = await temporaryReads();
          try {
            await expectLater(
              c.prepareVideo(
                original,
                original.media.single,
                current: () => true,
                trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
                abortTrigger: abort.future,
                onFreshDetail: (_) async => boundary != 'reject-visible-grant',
              ),
              throwsA(anything),
            );
            expect(fixture.cancelled, true);
          } finally {
            c.dispose();
            identity.dispose();
            fixture.close();
          }
          expect(await temporaryReads(), before);
        });
      },
    );
  }
  for (final stage in ['midstream-ack', 'final-ack', 'metadata']) {
    testWidgets(
      'WAN hanging $stage is cancelled without late publish or temp leak',
      (t) async {
        await t.runAsync(() async {
          final fixture = _RenewingVideoFixture();
          final identity = ValueNotifier(0), gate = Completer<bool>();
          final abort = Completer<void>();
          if (stage == 'metadata') fixture.metadataGate = Completer<void>();
          final c = OwnedMapController(
            client: fixture,
            apiBaseUrl: () => f.base,
            ownerID: () => fixture.owner,
            authorizationHeader: () => 'Bearer fixture',
            organizationWorkspaceID: () => fixture.organization,
            identityChanges: identity,
            refreshAllowed: () => true,
            now: () => fixture.now,
          );
          c.updateQuery('', f.bounds);
          await c.refresh();
          final detail = await c.readMoment(c.items.single.moments.single);
          if (stage != 'final-ack')
            fixture.advance = (i) async {
              if (i == 1) {
                fixture.now = fixture.now.add(const Duration(seconds: 18));
                await c.refresh();
              }
            };
          var waiting = false, published = false;
          final before = await temporaryReads();
          final transfer = c
              .prepareVideo(
                detail,
                detail.media.single,
                current: () => true,
                trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
                abortTrigger: abort.future,
                onFreshDetail: (_) {
                  waiting = true;
                  return gate.future;
                },
              )
              .then((path) async {
                published = true;
                await releaseVideoMaterialPath(path);
              }, onError: (Object _) {});
          for (
            var i = 0;
            i < 200 &&
                !(stage == 'metadata' ? fixture.metadataWaiting : waiting);
            i++
          ) {
            await Future<void>.delayed(const Duration(milliseconds: 5));
          }
          expect(stage == 'metadata' ? fixture.metadataWaiting : waiting, true);
          abort.complete();
          await transfer.timeout(const Duration(seconds: 2));
          expect(published, false);
          expect(fixture.cancelled, true);
          if (stage == 'metadata') expect(fixture.metadataCancelled, true);
          expect(await temporaryReads(), before);
          fixture.changedDetailSource = f.hash('late-change-must-not-adopt');
          if (!gate.isCompleted) gate.complete(true);
          fixture.metadataGate?.complete();
          await Future<void>.delayed(Duration.zero);
          expect(published, false);
          c.dispose();
          identity.dispose();
          fixture.close();
        });
      },
    );
  }

  testWidgets(
    'WAN final UI ack stays under the same absolute110s fake budget',
    (t) async {
      final fixture = _RenewingVideoFixture();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => fixture.organization,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      c.updateQuery('', f.bounds);
      await f.drain(t, () => c.current && !c.loading);
      final detail = (await t.runAsync(
        () => c.readMoment(c.items.single.moments.single),
      ))!;
      final before = await t.runAsync(temporaryReads);
      final gate = Completer<bool>();
      var waiting = false, completed = false, published = false;
      Object? failure;
      final job = c
          .prepareVideo(
            detail,
            detail.media.single,
            current: () => true,
            trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
            onFreshDetail: (_) {
              waiting = true;
              return gate.future;
            },
          )
          .then(
            (path) async {
              published = true;
              await releaseVideoMaterialPath(path);
              completed = true;
            },
            onError: (Object e) {
              failure = e;
              completed = true;
            },
          );
      await f.drain(t, () => waiting);
      await t.pump(const Duration(seconds: 110));
      await f.drain(t, () => completed);
      await job;
      expect(published, false);
      expect(failure, isA<TimeoutException>());
      expect(
        (failure as TimeoutException).duration,
        const Duration(seconds: 110),
      );
      expect(await t.runAsync(temporaryReads), before);
      gate.complete(true);
      await t.pump();
      expect(published, false);
      c.dispose();
      identity.dispose();
      fixture.close();
      await t.pump();
    },
  );

  testWidgets('WAN receive ack hang is released by absolute110s fake timer', (
    t,
  ) async {
    final stream = StreamController<List<int>>();
    final gate = Completer<void>();
    final api = OwnedMapAPI(
      client: _ControlledVideoClient(stream.stream, payload.length),
      apiBaseUrl: () => f.base,
    );
    final detail = videoDetail();
    var completed = false;
    Object? failure;
    final job = api
        .transferVideo(
          detail.media.single,
          detail: detail,
          now: () => DateTime.utc(2026, 10, 16),
          authorization: 'Bearer fixture',
          origin: f.base,
          current: () => true,
          receive: (_) => gate.future,
          trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
        )
        .then(
          (_) {
            completed = true;
          },
          onError: (Object e) {
            completed = true;
            failure = e;
          },
        );
    await t.pump();
    stream.add(payload.sublist(0, 4));
    await t.pump();
    await t.pump(const Duration(seconds: 110));
    await f.drain(t, () => completed);
    await job;
    expect(failure, isA<TimeoutException>());
    expect(
      (failure as TimeoutException).duration,
      const Duration(seconds: 110),
    );
    gate.complete();
    await t.pump();
    api.dispose();
    unawaited(stream.close());
  });

  testWidgets(
    'WAN final proof starts a new exact GET after verified even with earlier renewal',
    (t) async {
      final fixture = _RenewingVideoFixture()..metadataGate = Completer<void>();
      final identity = ValueNotifier(0);
      final body = StreamController<List<int>>();
      fixture.controlledBody = body;
      final c = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => fixture.organization,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      c.updateQuery('', f.bounds);
      await f.drain(t, () => c.current && !c.loading);
      final detail = (await t.runAsync(
        () => c.readMoment(c.items.single.moments.single),
      ))!;
      final before = await t.runAsync(temporaryReads);
      var completed = false, published = false;
      final trace = MapMediaReadMetrics.begin(MapMediaReadKind.video);
      final job = c
          .prepareVideo(
            detail,
            detail.media.single,
            current: () => true,
            trace: trace,
            onFreshDetail: (_) async => true,
          )
          .then(
            (path) async {
              published = true;
              await releaseVideoMaterialPath(path);
              completed = true;
            },
            onError: (Object _) {
              completed = true;
            },
          );
      await f.drain(t, () => fixture.bodyRequested);
      body.add(payload.sublist(0, 4));
      await t.pump();
      fixture.now = fixture.now.add(const Duration(seconds: 8));
      await t.pump(const Duration(seconds: 8));
      body.add(payload.sublist(4, 8));
      await t.pump();
      fixture.now = fixture.now.add(const Duration(seconds: 2));
      await t.pump(const Duration(seconds: 2));
      await f.drain(t, () => fixture.metadataWaiting);
      body.add(payload.sublist(8));
      unawaited(body.close());
      await t.pump();
      await f.drain(
        t,
        () => MapMediaReadMetrics.recent.any(
          (v) => v['operation'] == trace.operation && v['stage'] == 'verified',
        ),
      );
      fixture.changedDetailSource = f.hash('changed-after-verified');
      fixture.metadataGate!.complete();
      await f.drain(t, () => completed);
      await job;
      expect(published, false);
      expect(fixture.detailReads, 3);
      expect(await t.runAsync(temporaryReads), before);
      c.dispose();
      identity.dispose();
      fixture.close();
      await t.pump();
    },
  );

  test(
    'video overlimit chunk is rejected before sink write and short body is never published',
    () async {
      final before = await temporaryReads();
      for (final overflow in [true, false]) {
        final body = overflow
            ? Uint8List.fromList([...payload, 1, 2, 3, 4])
            : Uint8List.fromList(payload.sublist(0, 20));
        final client = _VideoClient(body, declaredLength: payload.length);
        final api = OwnedMapAPI(client: client, apiBaseUrl: () => f.base);
        final detail = videoDetail();
        final sink = await VideoMaterialSink.create(limit: payload.length);
        var received = 0, published = false;
        try {
          await expectLater(() async {
            await api.transferVideo(
              detail.media.single,
              detail: detail,
              now: () => DateTime.utc(2026, 10, 16),
              authorization: 'Bearer fixture',
              origin: f.base,
              current: () => true,
              trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
              receive: (bytes) async {
                received += bytes.length;
                await sink.add(bytes);
              },
            );
            await sink.finish();
            published = true;
          }, throwsFormatException);
          expect(published, false);
          expect(received, overflow ? 17 : 20);
        } finally {
          await sink.discard();
          api.dispose();
          client.close();
        }
      }
      expect(await temporaryReads(), before);
    },
  );
  test(
    'video chunks write during transfer but material publishes only after full SHA',
    () async {
      final gate = Completer<void>(), first = Completer<void>();
      final client = _VideoClient(
        payload,
        gate: gate,
        onFirst: () => first.complete(),
      );
      final api = OwnedMapAPI(client: client, apiBaseUrl: () => f.base);
      final detail = videoDetail();
      final sink = await VideoMaterialSink.create(limit: payload.length);
      final trace = MapMediaReadMetrics.begin(MapMediaReadKind.video);
      var completed = false;
      final received = <int>[];
      final transfer = api
          .transferVideo(
            detail.media.single,
            detail: detail,
            now: () => DateTime.utc(2026, 10, 16),
            authorization: 'Bearer fixture',
            origin: f.base,
            current: () => true,
            trace: trace,
            receive: (bytes) async {
              received.addAll(bytes);
              await sink.add(bytes);
            },
          )
          .then((_) => completed = true);
      await first.future;
      expect(received.length, 7);
      expect(completed, false);
      gate.complete();
      await transfer;
      expect(received, payload);
      final path = await sink.finish();
      expect(await File(path).readAsBytes(), payload);
      await releaseVideoMaterialPath(path);
      expect(await File(path).exists(), false);
      expect(
        MapMediaReadMetrics.recent
            .where((v) => v['operation'] == trace.operation)
            .map((v) => v['stage']),
        containsAllInOrder(['headers', 'firstByte', 'downloaded', 'verified']),
      );
      api.dispose();
      client.close();
    },
  );
  test(
    'video corrupt hash, midstream withdrawal and sink failure discard unpublished files',
    () async {
      final before = await temporaryReads();
      for (final failure in ['hash', 'withdrawal', 'write']) {
        var allowed = true;
        final body = Uint8List.fromList(payload);
        if (failure == 'hash') body[body.length - 1] ^= 1;
        final client = _VideoClient(
          body,
          onFirst: failure == 'withdrawal' ? () => allowed = false : null,
        );
        final api = OwnedMapAPI(client: client, apiBaseUrl: () => f.base);
        final detail = videoDetail();
        final sink = await VideoMaterialSink.create(limit: payload.length);
        var published = false;
        try {
          await expectLater(() async {
            await api.transferVideo(
              detail.media.single,
              detail: detail,
              now: () => DateTime.utc(2026, 10, 16),
              authorization: 'Bearer fixture',
              origin: f.base,
              current: () => allowed,
              trace: MapMediaReadMetrics.begin(MapMediaReadKind.video),
              receive: (bytes) async {
                if (failure == 'write')
                  throw const FileSystemException('isolated fixture failure');
                await sink.add(bytes);
              },
            );
            await sink.finish();
            published = true;
          }, throwsA(anything));
          expect(published, false);
        } finally {
          await sink.discard();
          api.dispose();
          client.close();
        }
      }
      expect(await temporaryReads(), before);
    },
  );
}
