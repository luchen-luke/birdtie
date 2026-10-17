import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'package:birdtie_client/src/content/video_material_player_io.dart';
import 'package:birdtie_client/src/content/video_material_sink_io.dart';
import 'package:birdtie_client/src/workspace/map_media_read_metrics.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
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

OwnedMapMomentDetail videoDetail() {
  final fixture = ReaderFixture()..video = true;
  final data = fixture.detail(1);
  data['media'] = [
    {
      ...fixture.media(1, isVideo: true),
      'byteCount': payload.length,
      'sha256': sha256.convert(payload).toString(),
    },
  ];
  return OwnedMapMomentDetail.decode(
    data,
    ownerID: fixture.owner,
    now: fixture.now,
  );
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
