import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:image_picker/image_picker.dart';
import 'package:video_player/video_player.dart';
import 'package:file_selector/file_selector.dart' as files;
import 'video_material_player_stub.dart'
    if (dart.library.io) 'video_material_player_io.dart'
    if (dart.library.html) 'video_material_player_web.dart';

const videoMaterialMaxBytes = 20 * 1024 * 1024;
const videoMaterialMaxDurationMS = 30000;
const videoMaterialPurpose = 'HUMAN_PRIVATE_VIDEO_MATERIAL';

enum VideoMaterialSource { gallery, file }

typedef VideoMaterialFileSelector =
    Future<XFile?> Function(List<files.XTypeGroup>);

abstract class VideoMaterialPreview {
  Duration get duration;
  int get width;
  int get height;
  bool get playing;
  Widget view();
  Future<void> toggle();
  Future<void> pause();
  Future<void> dispose();
}

class NativeVideoMaterialPreview implements VideoMaterialPreview {
  NativeVideoMaterialPreview(this.controller, this.path);
  final VideoPlayerController controller;
  final String path;
  bool _closed = false;
  @override
  Duration get duration => controller.value.duration;
  @override
  int get width => controller.value.size.width.round();
  @override
  int get height => controller.value.size.height.round();
  @override
  bool get playing => !_closed && controller.value.isPlaying;
  @override
  Widget view() => VideoPlayer(controller);
  @override
  Future<void> toggle() async {
    if (_closed) return;
    if (playing) {
      await controller.pause();
    } else {
      if (controller.value.position >= duration)
        await controller.seekTo(Duration.zero);
      await controller.play();
    }
  }

  @override
  Future<void> pause() async {
    if (!_closed) await controller.pause();
  }

  @override
  Future<void> dispose() async {
    if (_closed) return;
    _closed = true;
    try {
      await controller.dispose();
    } finally {
      await releaseVideoMaterialPath(path);
    }
  }
}

class VideoMaterialSelection {
  VideoMaterialSelection({required Uint8List bytes, required this.preview})
    : bytes = Uint8List.fromList(bytes),
      sha256Hex = sha256.convert(bytes).toString();
  final Uint8List bytes;
  final String sha256Hex;
  final VideoMaterialPreview preview;
  int get durationMS => preview.duration.inMilliseconds;
  Future<void> dispose() => preview.dispose();
}

// No camera, library enumeration, filename persistence or automatic upload.
// The picker preference does not enforce gallery duration: the actual player
// and container are checked before a selection can enter the current draft.
class VideoMaterialPicker {
  VideoMaterialPicker({
    ImagePicker? picker,
    VideoMaterialFileSelector? fileSelector,
  }) : _picker = picker ?? ImagePicker(),
       _fileSelector =
           fileSelector ??
           ((groups) => files.openFile(acceptedTypeGroups: groups));
  final ImagePicker _picker;
  final VideoMaterialFileSelector _fileSelector;
  bool supportsSource(VideoMaterialSource source) => supported;
  static const videoFileTypes = [
    files.XTypeGroup(
      label: 'MP4 视频',
      extensions: ['mp4'],
      mimeTypes: ['video/mp4'],
      uniformTypeIdentifiers: ['public.mpeg-4'],
    ),
  ];
  bool get supported =>
      kIsWeb ||
      const [
        TargetPlatform.android,
        TargetPlatform.iOS,
        TargetPlatform.macOS,
      ].contains(defaultTargetPlatform);
  Future<void> discardLostSelection() async {
    if (!kIsWeb && defaultTargetPlatform == TargetPlatform.android)
      await _picker.retrieveLostData();
  }

  Future<VideoMaterialSelection?> selectSource(
    VideoMaterialSource source,
  ) async {
    if (source == VideoMaterialSource.gallery) return select();
    if (!supported) throw const FormatException('此平台暂不支持视频预览，可选择图片文件。');
    final file = await _fileSelector(videoFileTypes);
    if (file == null) return null;
    final size = await file.length();
    if (size < 1 || size > videoMaterialMaxBytes)
      throw const FormatException('请选择不超过 20 MiB 的 MP4 视频。');
    final builder = BytesBuilder(copy: false);
    await for (final chunk in file.openRead()) {
      if (chunk.length > videoMaterialMaxBytes - builder.length)
        throw const FormatException('视频超过 20 MiB。');
      builder.add(chunk);
    }
    final bytes = Uint8List.fromList(builder.takeBytes());
    try {
      if (bytes.length != size) throw const FormatException('视频读取不完整，请重新选择。');
      validateLocalVideoMP4(bytes);
      return await openPrivateBytes(bytes);
    } finally {
      bytes.fillRange(0, bytes.length, 0);
    }
  }

  Future<VideoMaterialSelection> openPrivateBytes(Uint8List bytes) async {
    if (!supported) throw const FormatException('此平台暂不支持视频预览。');
    validateLocalVideoMP4(bytes);
    final path = await createVideoMaterialMemoryPath(bytes);
    VideoPlayerController? player;
    try {
      player = createVideoMaterialPlayer(path);
      if (player == null) throw const FormatException('此平台暂不支持视频预览。');
      await player.initialize().timeout(const Duration(seconds: 12));
      final preview = NativeVideoMaterialPreview(player, path);
      if (preview.duration.inMilliseconds < 1 ||
          preview.duration.inMilliseconds > videoMaterialMaxDurationMS ||
          preview.width < 1 ||
          preview.height < 1 ||
          preview.width > 1920 ||
          preview.height > 1920 ||
          preview.width * preview.height > 1920 * 1080)
        throw const FormatException('视频实际内容超出播放范围。');
      await player.setLooping(false);
      await player.pause();
      return VideoMaterialSelection(bytes: bytes, preview: preview);
    } catch (_) {
      try {
        if (player != null) await player.dispose();
      } finally {
        await releaseVideoMaterialPath(path);
      }
      rethrow;
    }
  }

  Future<VideoMaterialSelection?> select() async {
    if (!supported) throw const FormatException('此平台暂不支持本机视频预览。');
    final file = await _picker.pickVideo(
      source: ImageSource.gallery,
      maxDuration: const Duration(seconds: 30),
    );
    if (file == null) return null;
    VideoPlayerController? player;
    try {
      final size = await file.length();
      if (size < 1 || size > videoMaterialMaxBytes)
        throw const FormatException('请选择不超过 20 MiB 的 MP4 视频。');
      final builder = BytesBuilder(copy: false);
      await for (final chunk in file.openRead()) {
        if (chunk.length > videoMaterialMaxBytes - builder.length)
          throw const FormatException('视频超过 20 MiB。');
        builder.add(chunk);
      }
      final bytes = builder.takeBytes();
      if (bytes.length != size) throw const FormatException('视频读取不完整，请重新选择。');
      validateLocalVideoMP4(bytes);
      player = createVideoMaterialPlayer(file.path);
      if (player == null) throw const FormatException('此平台暂不支持本机视频预览。');
      await player.initialize().timeout(const Duration(seconds: 12));
      final preview = NativeVideoMaterialPreview(player, file.path);
      final w = preview.width,
          h = preview.height,
          ms = preview.duration.inMilliseconds;
      if (ms < 1 ||
          ms > videoMaterialMaxDurationMS ||
          w < 1 ||
          h < 1 ||
          w > 1920 ||
          h > 1920 ||
          w * h > 1920 * 1080)
        throw const FormatException('请选择 30 秒以内、1080p 以内的 MP4/H.264 视频。');
      await player.setLooping(false);
      await player.pause();
      return VideoMaterialSelection(bytes: bytes, preview: preview);
    } catch (_) {
      if (player != null) await player.dispose();
      await releaseVideoMaterialPath(file.path);
      rethrow;
    }
  }
}

void validateLocalVideoMP4(Uint8List bytes) {
  const fail = FormatException('只支持完整的 MP4/H.264 视频，可选 AAC 音轨。');
  if (bytes.length < 24 || bytes.length > videoMaterialMaxBytes) throw fail;
  final data = ByteData.sublistView(bytes);
  var boxes = 0, videos = 0, audios = 0;
  var ftyp = false, moov = false, mdat = false;
  void walk(int start, int end, int depth) {
    if (depth > 8) throw fail;
    var offset = start;
    while (offset < end) {
      if (end - offset < 8 || ++boxes > 512) throw fail;
      var size = data.getUint32(offset);
      var header = 8;
      final kind = String.fromCharCodes(bytes.sublist(offset + 4, offset + 8));
      if (size == 1) {
        if (end - offset < 16) throw fail;
        size = data.getUint64(offset + 8);
        header = 16;
      }
      if (size == 0) size = end - offset;
      if (size < header || size > end - offset) throw fail;
      final next = offset + size;
      if (depth == 0) {
        if (kind == 'ftyp') {
          if (offset != 0 || size < 16) throw fail;
          final brand = String.fromCharCodes(
            bytes.sublist(offset + header, offset + header + 4),
          );
          if (!const [
            'isom',
            'iso2',
            'mp41',
            'mp42',
            'avc1',
            'M4V ',
          ].contains(brand))
            throw fail;
          ftyp = true;
        }
        if (kind == 'moov') moov = true;
        if (kind == 'mdat') mdat = true;
      }
      if (const ['moov', 'trak', 'mdia', 'minf', 'stbl'].contains(kind))
        walk(offset + header, next, depth + 1);
      if (kind == 'stsd') {
        final content = offset + header;
        if (next - content < 8) throw fail;
        final count = data.getUint32(content + 4);
        if (count < 1 || count > 2) throw fail;
        var sample = content + 8;
        for (var i = 0; i < count; i++) {
          if (next - sample < 8) throw fail;
          final entrySize = data.getUint32(sample);
          if (entrySize < 8 || entrySize > next - sample) throw fail;
          final codec = String.fromCharCodes(
            bytes.sublist(sample + 4, sample + 8),
          );
          if (codec == 'avc1' || codec == 'avc3') {
            if (entrySize < 86) throw fail;
            videos++;
          } else if (codec == 'mp4a') {
            if (entrySize < 36) throw fail;
            audios++;
          } else {
            throw fail;
          }
          sample += entrySize;
        }
        if (sample != next) throw fail;
      }
      offset = next;
    }
  }

  walk(0, bytes.length, 0);
  if (!ftyp || !moov || !mdat || videos != 1 || audios > 1) throw fail;
}
