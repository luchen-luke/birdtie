import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'package:image_picker/image_picker.dart';
import 'moment_image_header.dart';

class MomentImageSelection {
  MomentImageSelection(this.bytes) : header = MomentImageHeader.inspect(bytes);
  final Uint8List bytes;
  final MomentImageHeader header;
}

// One explicit system-gallery selection. No camera/video, broad library scan,
// filename upload or background read. Lost process results never attach to a
// new owner: the user must select again in the current bound editor.
class MomentImagePicker {
  MomentImagePicker({ImagePicker? picker}) : _picker = picker ?? ImagePicker();
  final ImagePicker _picker;
  Future<bool> discardLostSelection() async {
    if (kIsWeb || defaultTargetPlatform != TargetPlatform.android) return false;
    final response = await _picker.retrieveLostData();
    return !response.isEmpty;
  }

  Future<MomentImageSelection?> select() async {
    final file = await _picker.pickImage(
      source: ImageSource.gallery,
      requestFullMetadata: false,
    );
    if (file == null) return null;
    final size = await file.length();
    if (size < 1 || size > momentImageMaxBytes) {
      throw const FormatException('图片超过 10 MiB 或为空');
    }
    // XFile's bounded header length check precedes reading/decoding the image.
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in file.openRead()) {
      if (chunk.length > momentImageMaxBytes - bytes.length) {
        throw const FormatException('图片超过 10 MiB');
      }
      bytes.add(chunk);
    }
    final raw = bytes.takeBytes();
    if (raw.length != size) throw const FormatException('图片读取不完整');
    return MomentImageSelection(raw);
  }
}
