import 'dart:typed_data';
import 'package:birdtie_client/src/content/moment_image_header.dart';
import 'package:flutter_test/flutter_test.dart';

Uint8List headerPNG({
  int width = 1,
  int height = 1,
  bool duplicate = false,
  bool animated = false,
}) {
  List<int> chunk(String type, List<int> data) => [
    ..._u32(data.length),
    ...type.codeUnits,
    ...data,
    0,
    0,
    0,
    0,
  ];
  final head = [..._u32(width), ..._u32(height), 8, 6, 0, 0, 0];
  return Uint8List.fromList([
    137,
    80,
    78,
    71,
    13,
    10,
    26,
    10,
    ...chunk('IHDR', head),
    if (duplicate)
      ...chunk('IHDR', [..._u32(9999), ..._u32(9999), 8, 6, 0, 0, 0]),
    if (animated) ...chunk('acTL', [0, 0, 0, 1, 0, 0, 0, 0]),
    ...chunk('IDAT', [1]),
    ...chunk('IEND', []),
  ]);
}

List<int> _u32(int n) => [
  (n >> 24) & 255,
  (n >> 16) & 255,
  (n >> 8) & 255,
  n & 255,
];
Uint8List headerJPEG({bool duplicate = false, int width = 1, int height = 1}) {
  List<int> frame(int w, int h) => [
    255,
    192,
    0,
    11,
    8,
    h >> 8,
    h & 255,
    w >> 8,
    w & 255,
    1,
    1,
    0x11,
    0,
  ];
  return Uint8List.fromList([
    255,
    216,
    ...frame(width, height),
    if (duplicate) ...frame(9999, 9999),
    255,
    218,
    0,
    8,
    1,
    1,
    0,
    0,
    63,
    0,
    3,
    255,
    217,
  ]);
}

void main() {
  test('本地头检查拒绝第二PNG IHDR避免只检查首尺寸', () {
    expect(
      () => MomentImageHeader.inspect(headerPNG(duplicate: true)),
      throwsFormatException,
    );
  });
  test('本地头检查拒绝第二JPEG SOF避免只检查首尺寸', () {
    expect(
      () => MomentImageHeader.inspect(headerJPEG(duplicate: true)),
      throwsFormatException,
    );
  });
  test('静态有界PNG JPEG头正常控制', () {
    final p = MomentImageHeader.inspect(headerPNG(width: 32, height: 16));
    expect(p.mime, 'image/png');
    expect(p.width, 32);
    final j = MomentImageHeader.inspect(headerJPEG(width: 20, height: 10));
    expect(j.height, 10);
    expect(j.mime, 'image/jpeg');
  });
  test('尺寸像素输入截断动画和非图片保守拒绝', () {
    for (final b in [
      headerPNG(width: 8193),
      headerPNG(width: 8192, height: 8192),
      headerPNG(animated: true),
      Uint8List(0),
      Uint8List(momentImageMaxBytes + 1),
      Uint8List.fromList([1, 2, 3]),
      headerJPEG(width: 8193),
      headerJPEG().sublist(0, 6),
    ]) {
      expect(() => MomentImageHeader.inspect(b), throwsFormatException);
    }
  });
}
