// Isolated device entry point. Never imported by lib/main.dart.
// Build explicitly with -t tool/probes/now_map_media_device_probe.dart.
// The fixture is synthetic lavfi testsrc2: H264 baseline, 160x90, 12fps, 2s,
// no audio. It is embedded here and never sent to a server or user account.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:birdtie_client/src/content/video_material_player_io.dart'
    as material;
import 'package:birdtie_client/src/content/video_material_sink_io.dart';
import 'package:birdtie_client/src/workspace/map_media_read_metrics.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:video_player/video_player.dart';

const _origin = 'https://map-device-probe.invalid';
const _authorization = 'Bearer isolated-local-fixture';
const _viewer = 'f1700000-0000-4000-8000-000000000001';
const _moment = 'f1700000-0000-4000-8000-000000000002';
const _media = 'f1700000-0000-4000-8000-000000000003';
const _fixtureBase64 =
    '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';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(
    MaterialApp(
      debugShowCheckedModeBanner: false,
      theme: ThemeData(colorSchemeSeed: const Color(0xFF356950)),
      home: const _DeviceProbe(),
    ),
  );
}

/// This client never delegates to an IO/network client. Any unexpected request
/// is rejected, so neither a production URL nor a real bearer can be used.
class _LocalFixtureClient extends http.BaseClient {
  _LocalFixtureClient(this._bytes, this._contentPath);
  final Uint8List _bytes;
  final String _contentPath;
  bool _closed = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    if (_closed ||
        request.method != 'GET' ||
        request.url.toString() != '$_origin$_contentPath' ||
        request.headers['Authorization'] != _authorization ||
        request.followRedirects ||
        request.headers.containsKey('Range')) {
      throw const FormatException('隔离验证拒绝非本地夹具请求');
    }
    Stream<List<int>> chunks() async* {
      for (var offset = 0; offset < _bytes.length; offset += 997) {
        if (_closed) throw const FormatException('隔离验证已撤销');
        yield Uint8List.fromList(
          _bytes.sublist(offset, math.min(offset + 997, _bytes.length)),
        );
      }
    }

    return http.StreamedResponse(
      chunks(),
      200,
      contentLength: _bytes.length,
      headers: const {
        'cache-control': 'private, no-store',
        'content-type': 'video/mp4',
        'x-content-type-options': 'nosniff',
      },
    );
  }

  @override
  void close() {
    _closed = true;
  }
}

class _DeviceProbe extends StatefulWidget {
  const _DeviceProbe();
  @override
  State<_DeviceProbe> createState() => _DeviceProbeState();
}

class _DeviceProbeState extends State<_DeviceProbe>
    with WidgetsBindingObserver {
  VideoPlayerController? _player;
  String? _path;
  Timer? _expiry;
  Future<void>? _flight;
  int _request = 0;
  bool _loading = false, _revoking = false, _active = false;
  bool? _deleted;
  String _status = '等待显式播放';
  int _fixtureBytes = 0;
  final _clock = Stopwatch()..start();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  void _log(String stage, {int? bytes, bool? deleted}) {
    // Fixed stage labels and numeric/boolean fields only. No paths or IDs.
    debugPrint(
      '[BirdtieMediaProbe] ${jsonEncode({'stage': stage, 'elapsedUs': _clock.elapsedMicroseconds, if (bytes != null) 'bytes': bytes, if (deleted != null) 'deleted': deleted})}',
    );
  }

  Future<void> _release(VideoPlayerController? player, String? path) async {
    try {
      await player?.dispose();
    } catch (_) {
      _log('disposeFailed');
    } finally {
      if (path != null) await material.releaseVideoMaterialPath(path);
    }
  }

  void _start() {
    if (_loading || _revoking || _player != null) return;
    final future = _play();
    _flight = future;
    unawaited(
      future.whenComplete(() {
        if (identical(_flight, future)) _flight = null;
      }),
    );
  }

  Future<void> _play() async {
    final request = ++_request;
    _clock.reset();
    _active = true;
    _deleted = null;
    setState(() {
      _loading = true;
      _status = '正在受控读取本地夹具';
    });
    _log('tap');
    final trace = MapMediaReadMetrics.begin(MapMediaReadKind.video);
    final bytes = base64Decode(_fixtureBase64);
    _fixtureBytes = bytes.length;
    final now = DateTime.now().toUtc();
    final until = now.add(const Duration(seconds: 30));
    final source = sha256.convert(bytes).toString();
    final detail = OwnedMapMomentDetail.decode(
      {
        'schemaVersion': 'map-content-moment-v1',
        'viewerAccountId': _viewer,
        'id': _moment,
        'revision': 1,
        'visibilitySource': 'SELF_PRIVATE',
        'canEdit': false,
        'status': 'published',
        'visibility': 'private',
        'title': '隔离合成视频',
        'body': '',
        'createdAt': now.toIso8601String(),
        'createdAtSource': 'UNKNOWN',
        'locations': <Object?>[],
        'observedAt': now.toIso8601String(),
        'validUntil': until.toIso8601String(),
        'sourceVersion': source,
        'media': [
          {
            'kind': 'VIDEO',
            'id': _media,
            'revision': 1,
            'mimeType': 'video/mp4',
            'width': 160,
            'height': 90,
            'byteCount': bytes.length,
            'sha256': source,
            'sourceVersion': source,
            'contentPath':
                '/v1/me/map-content/moments/$_moment/media/$_media/content?sourceVersion=$source',
            'durationMs': 2000,
            'videoCodec': 'h264',
            'audioCodec': 'none',
            'poster': null,
          },
        ],
      },
      ownerID: _viewer,
      now: now,
    );
    final client = _LocalFixtureClient(bytes, detail.media.single.contentPath);
    final api = OwnedMapAPI(client: client, apiBaseUrl: () => _origin);
    VideoMaterialSink? sink;
    VideoPlayerController? initializing;
    String? unpublishedPath;
    var handedOff = false;
    bool current() =>
        mounted &&
        _active &&
        request == _request &&
        DateTime.now().toUtc().isBefore(until);
    _expiry?.cancel();
    _expiry = Timer(until.difference(DateTime.now().toUtc()), () {
      unawaited(_revoke(expired: true));
    });
    try {
      trace.mark(MapMediaReadStage.detailReady);
      sink = await VideoMaterialSink.create(limit: bytes.length);
      await api.transferVideo(
        detail.media.single,
        detail: detail,
        now: () => DateTime.now().toUtc(),
        authorization: _authorization,
        origin: _origin,
        current: current,
        receive: sink.add,
        trace: trace,
      );
      if (!current()) return;
      unpublishedPath = await sink.finish();
      if (!current()) return;
      trace.mark(MapMediaReadStage.materialReady);
      initializing = material.createVideoMaterialPlayer(unpublishedPath);
      if (initializing == null) throw const FormatException('设备不支持原生播放器');
      await initializing.initialize();
      if (!current()) return;
      trace.mark(MapMediaReadStage.initialized);
      await initializing.setLooping(true);
      if (!current()) return;
      final playing = initializing;
      _player = playing;
      initializing = null;
      _path = unpublishedPath;
      unpublishedPath = null;
      handedOff = true;
      setState(() {
        _status = '播放器已初始化；等待真实画面截图';
      });
      // The widget must mount the real Texture. This boundary is not a claim
      // that its native surface has displayed a frame.
      await WidgetsBinding.instance.endOfFrame;
      if (!current()) return;
      await playing.play();
      if (!current()) return;
      trace.mark(MapMediaReadStage.playRequested);
      _log('playRequested', bytes: bytes.length);
      setState(() {
        _status = '已请求播放；以真实截图确认画面';
      });
    } catch (_) {
      trace.mark(MapMediaReadStage.failed);
      _log('failed', bytes: bytes.length);
      if (current())
        setState(() {
          _status = '隔离验证未能播放，可撤销后重试';
        });
    } finally {
      api.dispose();
      client.close();
      bytes.fillRange(0, bytes.length, 0);
      try {
        await _release(initializing, unpublishedPath);
      } finally {
        if (!handedOff && sink != null) await sink.discard();
      }
      if (mounted && request == _request) setState(() => _loading = false);
    }
  }

  Future<void> _revoke({bool expired = false}) async {
    if (_revoking) return;
    _active = false;
    ++_request;
    _expiry?.cancel();
    _revoking = true;
    final player = _player, path = _path, flight = _flight;
    _player = null;
    _path = null;
    if (mounted)
      setState(() {
        _status = '正在撤销并清理';
      });
    try {
      await _release(player, path);
    } finally {
      await flight;
    }
    final deleted = path == null || !await File(path).exists();
    _deleted = deleted;
    _loading = false;
    _revoking = false;
    _log(
      expired ? 'leaseExpiredAndCleaned' : 'revokedAndCleaned',
      deleted: deleted,
    );
    if (mounted)
      setState(() {
        _status = expired ? '隔离授权已到期，播放器和临时材料已释放' : '已撤销，播放器和临时材料已释放';
      });
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) unawaited(_revoke());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _active = false;
    ++_request;
    _expiry?.cancel();
    final player = _player, path = _path;
    _player = null;
    _path = null;
    unawaited(() async {
      try {
        await _release(player, path);
      } finally {
        await _flight;
      }
    }());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('隔离媒体设备验证')),
    body: SafeArea(
      child: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const Text(
            '仅本地合成视频',
            style: TextStyle(fontSize: 22, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 8),
          const Text('不连接生产，不读取账号或用户存储。点击后经真实受控传输、完整校验和原生播放器播放。'),
          const SizedBox(height: 20),
          AspectRatio(
            aspectRatio: 16 / 9,
            child: ColoredBox(
              color: Colors.black,
              child: _player == null
                  ? const Center(
                      child: Text(
                        '等待点击播放',
                        style: TextStyle(color: Colors.white),
                      ),
                    )
                  : VideoPlayer(_player!),
            ),
          ),
          const SizedBox(height: 12),
          Text(_status, key: const ValueKey('probe-status')),
          if (_player case final player?)
            ValueListenableBuilder<VideoPlayerValue>(
              valueListenable: player,
              builder: (_, value, __) => Text(
                '原生尺寸 ${value.size.width.toInt()}×${value.size.height.toInt()} · '
                '播放位置 ${value.position.inMilliseconds}ms · 正在播放 ${value.isPlaying ? "是" : "否"}',
                key: const ValueKey('probe-native-status'),
              ),
            ),
          if (_fixtureBytes > 0) Text('合成 H264 · $_fixtureBytes 字节 · 无音轨'),
          if (_deleted != null)
            Text(
              '临时文件已删除：${_deleted! ? "是" : "否"}',
              key: const ValueKey('probe-cleanup-status'),
            ),
          const SizedBox(height: 16),
          FilledButton(
            onPressed: _loading || _revoking || _player != null ? null : _start,
            child: const Text('显式播放本地视频'),
          ),
          const SizedBox(height: 8),
          OutlinedButton(
            onPressed: _revoking ? null : () => unawaited(_revoke()),
            child: const Text('撤销并删除临时材料'),
          ),
          const SizedBox(height: 12),
          const Text(
            '初始化完成和播放请求不等于首帧。真实画面以设备截图为证；截图时间只能给出首帧可见上界。',
            style: TextStyle(fontSize: 12),
          ),
        ],
      ),
    ),
  );
}
