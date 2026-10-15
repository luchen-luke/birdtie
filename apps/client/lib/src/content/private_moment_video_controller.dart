import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:video_player/video_player.dart';
import 'moment_media_context_api.dart';
import 'video_material_player_stub.dart'
    if (dart.library.io) 'video_material_player_io.dart'
    if (dart.library.js_interop) 'video_material_player_web.dart';

final _videoID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
final _videoSHA = RegExp(r'^[0-9a-f]{64}$');

class PrivateMomentVideo {
  PrivateMomentVideo.fromJson(Map<String, dynamic> v)
    : id = v['id'] as String,
      momentID = v['momentId'] as String,
      ownerID = v['ownerAccountId'] as String,
      status = v['status'] as String,
      revision = v['revision'] as int,
      position = v['position'] as int,
      bytes = v['byteSize'] as int,
      digest = v['sha256'] as String,
      durationMS = v['durationMs'] as int,
      width = v['width'] as int,
      height = v['height'] as int {
    if (v['schemaVersion'] != 'private-moment-video-v1' ||
        v['origin'] != 'LEGACY_PRIVATE_IMPORT' ||
        v['mimeType'] != 'video/mp4' ||
        v['antivirusScan'] != 'NOT_RUN' ||
        v['privacyRisk'] != 'UNKNOWN' ||
        ![id, momentID, ownerID].every(_videoID.hasMatch) ||
        !const {'ready_private', 'deleted'}.contains(status) ||
        revision < 1 ||
        position < 0 ||
        bytes < 1 ||
        bytes > 64 * 1024 * 1024 ||
        !_videoSHA.hasMatch(digest) ||
        durationMS < 1 ||
        durationMS > 120000 ||
        width < 1 ||
        height < 1 ||
        width * height > 16777216) {
      throw const FormatException('私人视频回执不完整');
    }
  }
  final String id, momentID, ownerID, status, digest;
  final int revision, position, bytes, durationMS, width, height;
}

/// Each read is bound to the captured person, Session and current Moment.
/// Playback bytes have a short local lifetime and never become a public URL.
class PrivateMomentVideoController extends ChangeNotifier {
  PrivateMomentVideoController({
    required this.momentID,
    required this.authorizationHeader,
    required this.ownerID,
    required this.sourceCurrent,
    required this.apiBaseUrl,
    required this.identityChanges,
    http.Client? client,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _token = authorizationHeader(),
       _owner = ownerID() {
    identityChanges.addListener(_boundary);
  }
  final String momentID, apiBaseUrl;
  final String? Function() authorizationHeader, ownerID;
  final bool Function() sourceCurrent;
  final Listenable identityChanges;
  final http.Client _client;
  final bool _ownsClient;
  MomentMediaContextAPI get mediaContextAPI => MomentMediaContextAPI(
    client: _client,
    baseUrl: apiBaseUrl,
    authorizationHeader: authorizationHeader,
    current: () => current,
  );
  final String? _token, _owner;
  bool _disposed = false, _retired = false;
  int _epoch = 0;
  bool loading = false, busy = false, uncertain = false;
  String? error;
  List<PrivateMomentVideo> items = const [];
  VideoPlayerController? player;
  String? playingID, _path;
  Timer? _playLease;
  StreamIterator<List<int>>? _reader;
  BytesBuilder? _partial;
  List<List<int>>? _chunks;
  void _cancelDownload() {
    final reader = _reader;
    _reader = null;
    if (reader != null) unawaited(reader.cancel());
    final chunks = _chunks;
    _chunks = null;
    if (chunks != null)
      for (final chunk in chunks) {
        chunk.fillRange(0, chunk.length, 0);
      }
    final partial = _partial;
    _partial = null;
    if (partial != null) {
      final bytes = partial.takeBytes();
      bytes.fillRange(0, bytes.length, 0);
    }
  }

  bool get current =>
      !_disposed &&
      !_retired &&
      _token != null &&
      _owner != null &&
      _videoID.hasMatch(momentID) &&
      authorizationHeader() == _token &&
      ownerID() == _owner &&
      sourceCurrent();
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _boundary() {
    if (current || _retired || _disposed) return;
    _retired = true;
    ++_epoch;
    items = const [];
    busy = loading = false;
    _cancelDownload();
    error = '账号或记录已变化，请重新打开。';
    unawaited(clearPlayback());
    _notify();
  }

  bool _live(int epoch) => current && epoch == _epoch;
  Uri _uri([String suffix = '']) => Uri.parse(
    '${apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}/v1/me/moments/$momentID/videos$suffix',
  );
  Map<String, String> get _headers => {'Authorization': _token!};
  PrivateMomentVideo _parse(dynamic value) {
    final v = PrivateMomentVideo.fromJson(value as Map<String, dynamic>);
    if (v.ownerID != _owner || v.momentID != momentID) {
      throw const FormatException('私人视频来源已变化');
    }
    return v;
  }

  Future<void> load() async {
    if (!current || loading || busy) return;
    final epoch = ++_epoch;
    loading = true;
    error = null;
    await clearPlayback();
    _notify();
    try {
      final r = await _client
          .get(_uri(), headers: _headers)
          .timeout(const Duration(seconds: 20));
      if (!_live(epoch)) return;
      if (r.statusCode != 200) throw const FormatException();
      final rows =
          (jsonDecode(utf8.decode(r.bodyBytes))['data']['items'] as List);
      if (rows.length > 12) throw const FormatException();
      final parsed = rows.map(_parse).toList();
      if (parsed.map((v) => v.id).toSet().length != parsed.length)
        throw const FormatException();
      items = List.unmodifiable(
        parsed.where((v) => v.status == 'ready_private'),
      );
      uncertain = false;
    } catch (_) {
      if (_live(epoch)) {
        items = const [];
        error = '视频暂时无法读取，请重试。';
      }
    } finally {
      if (_live(epoch)) {
        loading = false;
        _notify();
      }
    }
  }

  Future<Uint8List?> readContent(PrivateMomentVideo v) async {
    if (!current || v.status != 'ready_private' || !items.contains(v))
      return null;
    final epoch = _epoch;
    _cancelDownload();
    final partial = BytesBuilder(copy: false);
    _partial = partial;
    final chunks = <List<int>>[];
    _chunks = chunks;
    StreamIterator<List<int>>? reader;
    Uint8List? data;
    var accepted = false, headersExpired = false;
    try {
      final request = http.Request('GET', _uri('/${v.id}/content'))
        ..headers.addAll(_headers);
      final pending = _client.send(request).then((response) {
        if (headersExpired || !_live(epoch))
          unawaited(response.stream.listen((_) {}).cancel());
        return response;
      });
      final response = await pending.timeout(
        const Duration(seconds: 30),
        onTimeout: () {
          headersExpired = true;
          throw TimeoutException('视频读取超时');
        },
      );
      if (!_live(epoch)) return null;
      if (response.statusCode != 200 ||
          response.contentLength != null && response.contentLength != v.bytes ||
          !(response.headers['content-type'] ?? '').startsWith('video/mp4')) {
        await response.stream.listen((_) {}).cancel();
        return null;
      }
      reader = StreamIterator(response.stream);
      _reader = reader;
      final deadline = DateTime.now().add(const Duration(minutes: 3));
      while (true) {
        final remaining = deadline.difference(DateTime.now());
        if (remaining <= Duration.zero) throw TimeoutException('视频读取超时');
        if (!await reader.moveNext().timeout(
          remaining > const Duration(seconds: 30)
              ? const Duration(seconds: 30)
              : remaining,
        ))
          break;
        if (!_live(epoch) || partial.length + reader.current.length > v.bytes)
          return null;
        chunks.add(reader.current);
        partial.add(reader.current);
      }
      data = partial.takeBytes();
      if (!_live(epoch) ||
          data.length != v.bytes ||
          sha256.convert(data).toString() != v.digest)
        return null;
      accepted = true;
      return data;
    } catch (_) {
      return null;
    } finally {
      if (identical(_reader, reader)) _reader = null;
      await reader?.cancel();
      if (identical(_partial, partial)) _partial = null;
      if (identical(_chunks, chunks)) _chunks = null;
      for (final chunk in chunks) {
        if (!accepted || !identical(chunk, data))
          chunk.fillRange(0, chunk.length, 0);
      }
      final tail = partial.takeBytes();
      tail.fillRange(0, tail.length, 0);
      if (!accepted && data != null) data.fillRange(0, data.length, 0);
    }
  }

  Future<void> play(PrivateMomentVideo v) async {
    if (!current || loading || busy || uncertain) return;
    busy = true;
    error = null;
    final epoch = _epoch;
    _notify();
    String? path;
    VideoPlayerController? candidate;
    try {
      await clearPlayback();
      final data = await readContent(v);
      if (data == null) throw const FormatException();
      try {
        if (!_live(epoch)) return;
        path = await createVideoMaterialMemoryPath(data);
      } finally {
        data.fillRange(0, data.length, 0);
      }
      if (!_live(epoch)) return;
      candidate = createVideoMaterialPlayer(path);
      if (candidate == null) throw const FormatException();
      await candidate.initialize();
      if (!_live(epoch)) return;
      player = candidate;
      candidate = null;
      _path = path;
      path = null;
      playingID = v.id;
      // No automatic playback. Retire bytes after a bounded viewing period.
      _playLease = Timer(const Duration(minutes: 3), () {
        unawaited(clearPlayback());
      });
    } catch (_) {
      if (_live(epoch)) error = '视频暂时无法播放，请重试。';
    } finally {
      await candidate?.dispose();
      if (path != null) await releaseVideoMaterialPath(path);
      if (_live(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> remove(PrivateMomentVideo v) async {
    if (!current || busy || loading || uncertain || !items.contains(v)) return;
    final epoch = _epoch;
    busy = true;
    error = null;
    _notify();
    await clearPlayback();
    try {
      final r = await _client
          .delete(_uri('/${v.id}?revision=${v.revision}'), headers: _headers)
          .timeout(const Duration(seconds: 20));
      if (!_live(epoch)) return;
      if (r.statusCode != 200) throw const FormatException();
      final result = _parse(jsonDecode(utf8.decode(r.bodyBytes))['data']);
      if (result.id != v.id ||
          result.status != 'deleted' ||
          result.revision != v.revision + 1)
        throw const FormatException();
      items = List.unmodifiable(items.where((e) => e.id != v.id));
    } catch (_) {
      if (_live(epoch)) {
        uncertain = true;
        items = const [];
        error = '移除结果尚未确认，请重新读取；不会自动重发。';
      }
    } finally {
      if (_live(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> clearPlayback() async {
    _playLease?.cancel();
    _playLease = null;
    final old = player, path = _path;
    player = null;
    _path = null;
    playingID = null;
    _notify();
    try {
      await old?.dispose();
    } finally {
      if (path != null) await releaseVideoMaterialPath(path);
    }
  }

  void suspend() {
    ++_epoch;
    _cancelDownload();
    busy = loading = false;
    items = const [];
    unawaited(clearPlayback());
    _notify();
  }

  @override
  void dispose() {
    _disposed = true;
    ++_epoch;
    _cancelDownload();
    items = const [];
    identityChanges.removeListener(_boundary);
    unawaited(clearPlayback());
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
