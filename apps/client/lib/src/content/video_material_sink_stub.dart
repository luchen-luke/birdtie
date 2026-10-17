import 'dart:typed_data';

Future<void> releaseSinkVideoMaterial(String path) async {}

class VideoMaterialSink {
  static Future<VideoMaterialSink> create({required int limit}) async =>
      throw const FormatException('此平台暂不支持视频预览');
  Future<void> add(Uint8List bytes) async =>
      throw const FormatException('视频读取不可用');
  Future<String> finish() async => throw const FormatException('视频读取不可用');
  Future<void> discard() async {}
}
