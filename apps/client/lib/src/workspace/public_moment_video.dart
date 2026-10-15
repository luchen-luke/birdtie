import 'dart:async';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'public_moment_media.dart';
import 'public_moment_video_cache_stub.dart'
    if (dart.library.io) 'public_moment_video_cache_io.dart'
    if (dart.library.html) 'public_moment_video_cache_web.dart'
    as cache;

abstract class PublicMomentVideoHandle {
  bool get playing;
  int get width;
  int get height;
  int get durationMS;
  Widget view();
  Future<void> play();
  Future<void> pause();
  Future<void> dispose();
}

class PublicMomentVideoPlayer extends StatefulWidget {
  const PublicMomentVideoPlayer({
    super.key,
    required this.attachment,
    required this.api,
    required this.current,
    this.handleFactory,
    this.sourceVersion,
  });
  final PublicMomentMediaAttachment attachment;
  final PublicMomentMediaSetAPI api;
  final bool Function() current;
  final String? sourceVersion;
  final Future<PublicMomentVideoHandle> Function(Uint8List)? handleFactory;
  @override
  State<PublicMomentVideoPlayer> createState() =>
      _PublicMomentVideoPlayerState();
}

class _PublicMomentVideoPlayerState extends State<PublicMomentVideoPlayer>
    with WidgetsBindingObserver {
  PublicMomentVideoHandle? _handle;
  Uint8List? _poster;
  bool _busy = false, _retired = false;
  String? _error;
  int _epoch = 0;
  Timer? _lease;
  bool get _current =>
      !_retired &&
      mounted &&
      widget.current() &&
      widget.api.current() &&
      widget.attachment.expiresAt.isAfter(DateTime.now().toUtc());
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _arm();
  }

  void _arm() {
    _lease?.cancel();
    _lease = Timer(
      widget.attachment.expiresAt.difference(DateTime.now().toUtc()),
      () {
        if (mounted) {
          setState(_retire);
        }
      },
    );
    unawaited(_loadPoster());
  }

  Future<void> _loadPoster() async {
    final epoch = _epoch;
    try {
      final bytes = await widget.api.content(widget.attachment);
      if (!_current || epoch != _epoch) {
        bytes.fillRange(0, bytes.length, 0);
        return;
      }
      setState(() => _poster = bytes);
    } catch (_) {
      if (_current && epoch == _epoch) {
        setState(() => _error = '视频海报暂不可读取。');
      }
    }
  }

  void _retire() {
    _retired = true;
    _epoch++;
    _lease?.cancel();
    _poster?.fillRange(0, _poster!.length, 0);
    _poster = null;
    final handle = _handle;
    _handle = null;
    if (handle != null) {
      unawaited(handle.dispose());
    }
  }

  Future<void> _toggle() async {
    if (!_current || _busy) {
      return;
    }
    if (_handle?.playing == true) {
      await _handle!.pause();
      if (mounted) {
        setState(() {});
      }
      return;
    }
    final epoch = _epoch;
    setState(() {
      _busy = true;
      _error = null;
    });
    Uint8List? bytes;
    PublicMomentVideoHandle? created;
    try {
      // Every play rereads the current collection, including after a pause.
      final path = widget.attachment.displayPath.split('/');
      final currentSet = await widget.api.publicSet(
        path[3],
        expectedRevision: int.parse(path[5]),
      );
      if (!_current ||
          epoch != _epoch ||
          widget.sourceVersion != null &&
              currentSet.sourceVersion != widget.sourceVersion ||
          currentSet.attachments.length != 1 ||
          currentSet.attachments.single.video?['videoSha256'] !=
              widget.attachment.video?['videoSha256'] ||
          currentSet.attachments.single.mediaID != widget.attachment.mediaID ||
          currentSet.setVersion != int.parse(path[6])) {
        throw const FormatException('视频公开来源已变化');
      }
      if (_handle == null) {
        bytes = await widget.api.videoContent(widget.attachment);
        if (!_current || epoch != _epoch) {
          throw const FormatException('视频来源已到期');
        }
        created =
            await (widget.handleFactory ?? cache.createPublicMomentVideoHandle)(
              bytes,
            );
        if (!_current ||
            epoch != _epoch ||
            created.width != widget.attachment.width ||
            created.height != widget.attachment.height ||
            (created.durationMS - widget.attachment.video!['durationMs'] as int)
                    .abs() >
                120) {
          throw const FormatException('实际播放器资料与规范视频不符');
        }
        _handle = created;
        created = null;
      }
      await _handle!.play();
      if (!_current || epoch != _epoch) {
        _retire();
        return;
      }
    } catch (_) {
      await created?.dispose();
      if (_current && epoch == _epoch) {
        final old = _handle;
        _handle = null;
        await old?.dispose();
        _error = '当前视频暂不可播放，请重新读取。';
      }
    } finally {
      bytes?.fillRange(0, bytes.length, 0);
      if (mounted && epoch == _epoch) {
        setState(() => _busy = false);
      }
    }
  }

  @override
  void didUpdateWidget(covariant PublicMomentVideoPlayer old) {
    super.didUpdateWidget(old);
    if (old.api != widget.api ||
        old.attachment.video?['videoSha256'] !=
            widget.attachment.video?['videoSha256'] ||
        old.attachment.expiresAt != widget.attachment.expiresAt) {
      _retire();
      _retired = false;
      _arm();
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      unawaited(_handle?.pause() ?? Future<void>.value());
    }
  }

  @override
  void dispose() {
    _retire();
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_current) {
      _retire();
      return const Text('视频资料已到期，请重新读取。');
    }
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        AspectRatio(
          aspectRatio: widget.attachment.width / widget.attachment.height,
          child:
              _handle?.view() ??
              (_poster == null
                  ? const Center(child: Text('正在读取视频海报…'))
                  : Image.memory(
                      _poster!,
                      fit: BoxFit.contain,
                      semanticLabel: '规范视频海报',
                    )),
        ),
        Text(
          '${(widget.attachment.video!['durationMs'] as int) / 1000}秒 · ${widget.attachment.width}×${widget.attachment.height}',
        ),
        const Text('病毒扫描未运行 · 隐私风险未知'),
        if (_error != null) Text(_error!),
        TextButton(
          onPressed: _busy ? null : _toggle,
          child: Text(
            _busy
                ? '正在读取规范视频…'
                : _handle?.playing == true
                ? '暂停视频'
                : '播放视频',
          ),
        ),
      ],
    );
  }
}
