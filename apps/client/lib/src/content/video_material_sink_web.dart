import 'dart:js_interop';
import 'dart:typed_data';
import 'package:web/web.dart' as web;

Future<void> releaseSinkVideoMaterial(String path) async {
  if (Uri.tryParse(path)?.scheme == 'blob') web.URL.revokeObjectURL(path);
}

class VideoMaterialSink {
  VideoMaterialSink._(this.limit);
  final int limit;
  int _bytes = 0;
  final _chunks = <Uint8List>[];
  String? _path;
  bool _finished = false;
  static Future<VideoMaterialSink> create({required int limit}) async {
    if (limit < 1 || limit > 64 * 1024 * 1024)
      throw const FormatException('视频大小不符');
    return VideoMaterialSink._(limit);
  }

  Future<void> add(Uint8List bytes) async {
    if (_finished || _bytes + bytes.length > limit)
      throw const FormatException('视频读取已失效或超限');
    _chunks.add(Uint8List.fromList(bytes));
    _bytes += bytes.length;
  }

  void _clear() {
    for (final bytes in _chunks) {
      bytes.fillRange(0, bytes.length, 0);
    }
    _chunks.clear();
  }

  Future<String> finish() async {
    if (_finished || _bytes != limit) throw const FormatException('视频大小不符');
    _finished = true;
    _path = web.URL.createObjectURL(
      web.Blob(
        <JSAny>[for (final chunk in _chunks) chunk.toJS].toJS,
        web.BlobPropertyBag(type: 'video/mp4'),
      ),
    );
    _clear();
    return _path!;
  }

  Future<void> discard() async {
    _finished = true;
    _clear();
    if (_path != null) await releaseSinkVideoMaterial(_path!);
    _path = null;
  }
}
