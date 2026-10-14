import 'dart:typed_data';
import 'package:birdtie_client/src/content/video_material_picker.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

Uint8List syntheticVideoBytes({String codec = 'avc1'}) {
  Uint8List box(String name, List<int> content) {
    final bytes = Uint8List(8 + content.length);
    ByteData.sublistView(bytes).setUint32(0, bytes.length);
    bytes.setRange(4, 8, name.codeUnits);
    bytes.setRange(8, bytes.length, content);
    return bytes;
  }

  final entry = box(codec, List.filled(78, 0));
  final header = Uint8List(8);
  ByteData.sublistView(header).setUint32(4, 1);
  final stsd = box('stsd', [...header, ...entry]);
  return Uint8List.fromList([
    ...box('ftyp', 'isom0000'.codeUnits),
    ...box('moov', box('trak', box('mdia', box('minf', box('stbl', stsd))))),
    ...box('mdat', [1, 2, 3, 4]),
  ]);
}

class FakeVideoPreview implements VideoMaterialPreview {
  FakeVideoPreview({this.ms = 1000, this.w = 640, this.h = 360});
  final int ms, w, h;
  bool closed = false, isPlaying = false;
  @override
  Duration get duration => Duration(milliseconds: ms);
  @override
  int get width => w;
  @override
  int get height => h;
  @override
  bool get playing => isPlaying;
  @override
  Widget view() => const SizedBox(key: Key('synthetic-video-preview'));
  @override
  Future<void> toggle() async {
    isPlaying = !isPlaying;
  }

  @override
  Future<void> pause() async {
    isPlaying = false;
  }

  @override
  Future<void> dispose() async {
    closed = true;
    isPlaying = false;
  }
}

class FakeVideoPicker extends VideoMaterialPicker {
  VideoMaterialSelection? next;
  Future<VideoMaterialSelection?>? delayed;
  @override
  bool get supported => true;
  @override
  Future<void> discardLostSelection() async {}
  @override
  Future<VideoMaterialSelection?> select() async =>
      delayed == null ? next : await delayed;
  @override
  Future<VideoMaterialSelection> openPrivateBytes(Uint8List bytes) async =>
      VideoMaterialSelection(bytes: bytes, preview: FakeVideoPreview());
}

void main() {
  test('本机视频必须具有完整MP4与H264样本描述', () {
    expect(() => validateLocalVideoMP4(syntheticVideoBytes()), returnsNormally);
    expect(
      () => validateLocalVideoMP4(syntheticVideoBytes(codec: 'hvc1')),
      throwsFormatException,
    );
    final b = syntheticVideoBytes();
    expect(
      () => validateLocalVideoMP4(Uint8List.sublistView(b, 0, b.length - 1)),
      throwsFormatException,
    );
    expect(
      () => validateLocalVideoMP4(Uint8List.fromList([1, 2, 3])),
      throwsFormatException,
    );
  });
  test('本机签名检查拒绝超额字节与伪造box长度', () {
    expect(
      () => validateLocalVideoMP4(Uint8List(videoMaterialMaxBytes + 1)),
      throwsFormatException,
    );
    final b = syntheticVideoBytes();
    ByteData.sublistView(b).setUint32(0, 0xffffffff);
    expect(() => validateLocalVideoMP4(b), throwsFormatException);
  });
}
