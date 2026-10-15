import 'dart:typed_data';
import 'package:flutter/widgets.dart';
import 'package:video_player/video_player.dart';
import '../content/video_material_player_web.dart' as local;
import 'public_moment_video.dart';

class _PublicVideoHandle extends PublicMomentVideoHandle {
  _PublicVideoHandle(this.controller, this.path);
  final VideoPlayerController controller;
  final String path;
  bool _disposed = false;
  @override
  bool get playing => !_disposed && controller.value.isPlaying;
  @override
  int get width => controller.value.size.width.round();
  @override
  int get height => controller.value.size.height.round();
  @override
  int get durationMS => controller.value.duration.inMilliseconds;
  @override
  Widget view() => VideoPlayer(controller);
  @override
  Future<void> play() async {
    if (!_disposed) {
      if (controller.value.position >= controller.value.duration) {
        await controller.seekTo(Duration.zero);
      }
      await controller.play();
    }
  }

  @override
  Future<void> pause() async {
    if (!_disposed) {
      await controller.pause();
    }
  }

  @override
  Future<void> dispose() async {
    if (_disposed) {
      return;
    }
    _disposed = true;
    try {
      await controller.pause();
      await controller.dispose();
    } finally {
      await local.releaseVideoMaterialPath(path);
    }
  }
}

Future<PublicMomentVideoHandle> createPublicMomentVideoHandle(
  Uint8List bytes,
) async {
  final path = await local.createVideoMaterialMemoryPath(bytes);
  VideoPlayerController? controller;
  try {
    controller = local.createVideoMaterialPlayer(path);
    if (controller == null) {
      throw const FormatException('此平台暂不支持视频播放');
    }
    await controller.initialize();
    await controller.setLooping(false);
    return _PublicVideoHandle(controller, path);
  } catch (_) {
    await controller?.dispose();
    await local.releaseVideoMaterialPath(path);
    rethrow;
  }
}
