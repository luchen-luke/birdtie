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
  Future<void>? _writing, _closing, _discarding;
  final int limit;
  int _bytes = 0;
  bool _finished = false, _discarded = false;
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
    if (_finished ||
        _closing != null ||
        _writing != null ||
        _output == null ||
        _bytes + bytes.length > limit) {
      throw const FormatException('视频读取已失效或超限');
    }
    final write = _writing = _output!.writeFrom(bytes).then<void>((_) {});
    try {
      await write;
      if (_discarded) throw const FormatException('视频读取已取消');
      _bytes += bytes.length;
    } finally {
      if (identical(_writing, write)) _writing = null;
    }
  }

  Future<void> _close() => _closing ??= () async {
    // RandomAccessFile permits only one unfinished asynchronous operation.
    // Cancellation retires immediately, but close must follow the owned write.
    final write = _writing;
    if (write != null) {
      try {
        await write;
      } catch (_) {
        /* Still close and remove failed material. */
      }
    }
    final output = _output;
    _output = null;
    if (output != null) await output.close();
  }();

  Future<String> finish() async {
    if (_finished || _output == null || _bytes != limit) {
      throw const FormatException('视频大小不符');
    }
    _finished = true;
    await _close();
    if (_discarded) throw const FormatException('视频读取已取消');
    _owned[_file.path] = _dir;
    return _file.path;
  }

  Future<void> discard() => _discarding ??= () async {
    _discarded = true;
    _finished = true;
    try {
      await _close();
    } finally {
      _owned.remove(_file.path);
      if (await _dir.exists()) await _dir.delete(recursive: true);
    }
  }();
}
