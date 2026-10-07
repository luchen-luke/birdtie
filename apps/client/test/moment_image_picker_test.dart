import 'dart:typed_data';
import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/content/moment_image_header.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image_picker/image_picker.dart';
import 'moment_image_header_test.dart' show headerPNG;

class ImagePickerUnitSpy extends ImagePicker {
  XFile? result;
  ImageSource? seenSource;
  bool? fullMetadata;
  int calls = 0;
  @override
  Future<XFile?> pickImage({
    required ImageSource source,
    double? maxWidth,
    double? maxHeight,
    int? imageQuality,
    CameraDevice preferredCameraDevice = CameraDevice.rear,
    bool requestFullMetadata = true,
  }) async {
    seenSource = source;
    fullMetadata = requestFullMetadata;
    calls++;
    return result;
  }
}

class OversizedImageFile extends XFile {
  OversizedImageFile() : super('synthetic-not-real-file');
  bool opened = false;
  @override
  Future<int> length() async => momentImageMaxBytes + 1;
  @override
  Stream<Uint8List> openRead([int? start, int? end]) {
    opened = true;
    return const Stream.empty();
  }
}

void main() {
  test('实际gallery适配器只选所选单图且不索取完整metadata', () async {
    final s = ImagePickerUnitSpy()
      ..result = XFile.fromData(headerPNG(), mimeType: 'image/png');
    final p = MomentImagePicker(picker: s);
    final image = await p.select();
    expect(image!.header.mime, 'image/png');
    expect(s.seenSource, ImageSource.gallery);
    expect(s.fullMetadata, isFalse);
    expect(s.calls, 1);
  });
  test('系统取消返回null不读或上传，大小在openRead之前检查', () async {
    final s = ImagePickerUnitSpy();
    final p = MomentImagePicker(picker: s);
    expect(await p.select(), isNull);
    final large = OversizedImageFile();
    s.result = large;
    await expectLater(p.select(), throwsFormatException);
    expect(large.opened, isFalse);
  });
  test('文件声明MIME不代替实际图片头及读长检查', () async {
    final s = ImagePickerUnitSpy()
      ..result = XFile.fromData(
        Uint8List.fromList([1, 2, 3]),
        mimeType: 'image/png',
      );
    await expectLater(
      MomentImagePicker(picker: s).select(),
      throwsFormatException,
    );
  });
}
