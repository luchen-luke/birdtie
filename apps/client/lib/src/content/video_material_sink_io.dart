import 'dart:io';
import 'dart:typed_data';

final _owned = <String, Directory>{};
Future<void> releaseSinkVideoMaterial(String path) async {
  final dir = _owned.remove(path);
  if (dir != null && await dir.exists()) await dir.delete(recursive: true);
}

/// Unpublished temporary material. Only finish, after the caller's full hash
/// verification, makes its path available to the player ownership registry.
class VideoMaterialSink {
  VideoMaterialSink._(this._dir, this._file, this._output, this.limit);
  final Directory _dir;
  final File _file;
  RandomAccessFile? _output;
  final int limit;
  int _bytes = 0;
  bool _finished = false;
  static Future<VideoMaterialSink> create({required int limit}) async {
    if (limit < 1 || limit > 64 * 1024 * 1024) {
      throw const FormatException('视频大小不符');
    }
    final dir = await Directory.systemTemp.createTemp('birdtie-video-read-');
    try {
      final file = File('${dir.path}${Platform.pathSeparator}video.mp4');
      return VideoMaterialSink._(
        dir,
        file,
        await file.open(mode: FileMode.write),
        limit,
      );
    } catch (_) {
      await dir.delete(recursive: true);
      rethrow;
    }
  }

  Future<void> add(Uint8List bytes) async {
    if (_finished || _output == null || _bytes + bytes.length > limit) {
      throw const FormatException('视频读取已失效或超限');
    }
    await _output!.writeFrom(bytes);
    _bytes += bytes.length;
  }

  Future<String> finish() async {
    if (_finished || _output == null || _bytes != limit) {
      throw const FormatException('视频大小不符');
    }
    await _output!.close();
    _output = null;
    _finished = true;
    _owned[_file.path] = _dir;
    return _file.path;
  }

  Future<void> discard() async {
    await _output?.close();
    _output = null;
    _owned.remove(_file.path);
    if (await _dir.exists()) await _dir.delete(recursive: true);
  }
}
