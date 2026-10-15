import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:file_selector/file_selector.dart' as files;
import 'package:image_picker/image_picker.dart';
import 'moment_image_header.dart';

class MomentImageSelection {
  MomentImageSelection(this.bytes) : header = MomentImageHeader.inspect(bytes);
  final Uint8List bytes;
  final MomentImageHeader header;
}

enum MomentImageSource { gallery, camera, file }

typedef MomentImageFileSelector =
    Future<XFile?> Function(List<files.XTypeGroup>);

// One explicit system selection. No broad library scan,
// filename upload or background read. Lost process results never attach to a
// new owner: the user must select again in the current bound editor.
class MomentImagePicker {
  MomentImagePicker({
    ImagePicker? picker,
    MomentImageFileSelector? fileSelector,
  }) : _picker = picker ?? ImagePicker(),
       _fileSelector =
           fileSelector ??
           ((groups) => files.openFile(acceptedTypeGroups: groups));
  final ImagePicker _picker;
  final MomentImageFileSelector _fileSelector;
  bool supportsSource(MomentImageSource source) =>
      source != MomentImageSource.camera ||
      !kIsWeb &&
          const [
            TargetPlatform.android,
            TargetPlatform.iOS,
          ].contains(defaultTargetPlatform);
  bool get cameraSupported => supportsSource(MomentImageSource.camera);
  bool get fileSupported => supportsSource(MomentImageSource.file);
  static const imageFileTypes = [
    files.XTypeGroup(
      label: 'JPEG/PNG 图片',
      extensions: ['jpg', 'jpeg', 'png'],
      mimeTypes: ['image/jpeg', 'image/png'],
      uniformTypeIdentifiers: ['public.jpeg', 'public.png'],
    ),
  ];
  Future<bool> discardLostSelection() async {
    if (kIsWeb || defaultTargetPlatform != TargetPlatform.android) return false;
    final response = await _picker.retrieveLostData();
    return !response.isEmpty;
  }

  // Keep this gallery signature for existing callers and injected fixtures.
  Future<MomentImageSelection?> select() => _selectImage(ImageSource.gallery);

  /// One explicit multi-selection, read with the same bounded file checks.
  /// No lost-selection recovery is used across an identity change.
  Future<List<MomentImageSelection>> selectMany({int limit = 9}) async {
    if (limit < 1 || limit > 9) throw const FormatException('最多选择 9 张图片');
    try {
      final chosen = await _picker.pickMultiImage(
        limit: limit,
        requestFullMetadata: false,
      );
      if (chosen.length > limit) throw const FormatException('最多选择 9 张图片');
      final result = <MomentImageSelection>[];
      for (final file in chosen) {
        final selection = await _read(file);
        if (selection != null) result.add(selection);
      }
      return result;
    } on PlatformException {
      throw const FormatException('无法打开照片，请检查权限后重试。');
    }
  }

  Future<MomentImageSelection?> selectSource(MomentImageSource source) async {
    if (source == MomentImageSource.gallery) return select();
    if (!supportsSource(source))
      throw const FormatException('此平台暂不支持拍照，可从照片或文件选择图片。');
    if (source == MomentImageSource.camera)
      return _selectImage(ImageSource.camera);
    try {
      return await _read(await _fileSelector(imageFileTypes));
    } on PlatformException {
      throw const FormatException('无法打开图片文件，请重新选择或改从照片选择。');
    }
  }

  Future<MomentImageSelection?> _selectImage(ImageSource source) async {
    try {
      return await _read(
        await _picker.pickImage(source: source, requestFullMetadata: false),
      );
    } on PlatformException catch (e) {
      final denied =
          e.code.toLowerCase().contains('denied') ||
          e.code.toLowerCase().contains('restricted');
      throw FormatException(
        denied ? '未允许使用相机或照片，请检查权限或改从文件选择。' : '图片选择暂不可用，请重试或改从文件选择。',
      );
    }
  }

  Future<MomentImageSelection?> _read(XFile? file) async {
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
