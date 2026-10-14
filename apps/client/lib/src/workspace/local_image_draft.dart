import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';

import '../content/moment_image_header.dart';
import '../content/moment_image_picker.dart';

/// A reading/editing view only: no identity grant, HTTP or upload approval.
abstract interface class ComposerImageDraftView implements Listenable {
  bool get current;
  bool get busy;
  bool get canSelect;
  Uint8List? get localBytes;
  double get localAspectRatio;
  String? get error;
  void removeLocal();
}

void rejectAnimatedLocalImage(Uint8List bytes) {
  if (bytes.length < 8 || bytes[0] != 137 || bytes[1] != 80) return;
  final data = ByteData.sublistView(bytes);
  for (var offset = 8; offset + 12 <= bytes.length;) {
    final size = data.getUint32(offset);
    if (size > bytes.length - offset - 12) {
      throw const FormatException('PNG 图片块不完整');
    }
    final type = String.fromCharCodes(bytes.sublist(offset + 4, offset + 8));
    if (const {'acTL', 'fcTL', 'fdAT'}.contains(type)) {
      throw const FormatException('仅支持静态 JPEG 或 PNG 图片');
    }
    offset += size + 12;
  }
}

Future<ui.Image> decodeLocalImagePixels(MomentImageSelection selected) async {
  rejectAnimatedLocalImage(selected.bytes);
  final codec = await ui.instantiateImageCodec(selected.bytes);
  ui.Image? pixels;
  try {
    if (codec.frameCount != 1) throw const FormatException('仅支持静态图片');
    pixels = (await codec.getNextFrame()).image;
    if (pixels.width > momentImageMaxDimension ||
        pixels.height > momentImageMaxDimension ||
        pixels.width * pixels.height > momentImageMaxPixels) {
      throw const FormatException('图片尺寸超过处理范围');
    }
    final result = pixels;
    pixels = null;
    return result;
  } finally {
    pixels?.dispose();
    codec.dispose();
  }
}

/// Produces only the displayed pixel version; source metadata is not copied.
Future<Uint8List> renderLocalImagePixels(
  ui.Image pixels,
  List<ui.Rect> masks,
) async {
  final ownedPixels = pixels.clone();
  final recorder = ui.PictureRecorder();
  ui.Picture? picture;
  ui.Image? rendered;
  try {
    final canvas = ui.Canvas(recorder);
    canvas.drawImage(ownedPixels, ui.Offset.zero, ui.Paint());
    for (final rect in masks) {
      canvas.drawRect(
        ui.Rect.fromLTRB(
          rect.left * ownedPixels.width,
          rect.top * ownedPixels.height,
          rect.right * ownedPixels.width,
          rect.bottom * ownedPixels.height,
        ),
        ui.Paint()..color = const ui.Color(0xff000000),
      );
    }
    picture = recorder.endRecording();
    rendered = await picture.toImage(ownedPixels.width, ownedPixels.height);
    final data = await rendered.toByteData(format: ui.ImageByteFormat.png);
    if (data == null || data.lengthInBytes > momentImageMaxBytes) {
      throw const FormatException('处理后图片超过 10 MiB');
    }
    final bytes = Uint8List.fromList(
      data.buffer.asUint8List(data.offsetInBytes, data.lengthInBytes),
    );
    if (MomentImageHeader.inspect(bytes).mime != 'image/png') {
      throw const FormatException('本机图片处理格式不符');
    }
    return bytes;
  } finally {
    if (recorder.isRecording) recorder.endRecording().dispose();
    rendered?.dispose();
    picture?.dispose();
    ownedPixels.dispose();
  }
}

/// Detached local pixels. This object contains no approval or recipient grant.
class LocalImageRetainedDraft {
  LocalImageRetainedDraft._(this._bytes, this._pixels, this._masks);
  final Uint8List _bytes;
  final ui.Image _pixels;
  final List<ui.Rect> _masks;
  bool _disposed = false;
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _bytes.fillRange(0, _bytes.length, 0);
    _pixels.dispose();
  }
}

/// Local codec state. Its domain owner supplies the current scope; changing a
/// visible target retires selection, while its previously saved pixels may stay
/// in that original owner's private container.
class LocalImageDraftController extends ChangeNotifier
    implements ComposerImageDraftView {
  LocalImageDraftController({
    required this.isCurrent,
    required this.scope,
    MomentImagePicker? picker,
  }) : _picker = picker ?? MomentImagePicker();

  final bool Function() isCurrent;
  final Object? Function() scope;
  final MomentImagePicker _picker;
  bool supportsSource(MomentImageSource source) =>
      _picker.supportsSource(source);
  bool canSelectSource(MomentImageSource source) =>
      canSelect && supportsSource(source);
  bool _disposed = false;
  @override
  bool busy = false;
  @override
  String? error;
  int _serial = 0, _revision = 0;
  ui.Image? _pixels;
  Uint8List? _bytes;
  Uint8List? _view;
  final _masks = <ui.Rect>[];
  int get revision => _revision;
  bool get masked => _masks.isNotEmpty;
  int? get width => current ? _pixels?.width : null;
  int? get height => current ? _pixels?.height : null;
  @override
  bool get current => !_disposed && isCurrent();
  @override
  bool get canSelect => current && !busy;
  @override
  Uint8List? get localBytes => current ? _view : null;
  @override
  double get localAspectRatio =>
      _pixels == null ? 1 : _pixels!.width / _pixels!.height;
  bool get hasPixels => current && _bytes != null && _pixels != null && !busy;

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _clear() {
    ++_revision;
    _bytes?.fillRange(0, _bytes!.length, 0);
    _bytes = null;
    _view = null;
    _pixels?.dispose();
    _pixels = null;
    _masks.clear();
  }

  Future<void> select({
    required bool Function() gestureCurrent,
    MomentImageSource source = MomentImageSource.gallery,
  }) async {
    if (!canSelect || !gestureCurrent()) return;
    final serial = ++_serial, revision = _revision, binding = scope();
    bool valid() =>
        current &&
        serial == _serial &&
        revision == _revision &&
        binding == scope() &&
        gestureCurrent();
    busy = true;
    error = null;
    _notify();
    ui.Image? decoded;
    Uint8List? bytes;
    try {
      final lost = await _picker.discardLostSelection();
      if (!valid()) return;
      if (lost) error = '上次图片选择已失效，请在当前会话重新选择。';
      final selected = await _picker.selectSource(source);
      if (!valid() || selected == null) return;
      decoded = await decodeLocalImagePixels(selected);
      if (!valid()) return;
      bytes = await renderLocalImagePixels(decoded, const []);
      if (!valid()) return;
      _clear();
      _pixels = decoded;
      decoded = null;
      _bytes = bytes;
      _view = bytes.asUnmodifiableView();
      bytes = null;
    } catch (e) {
      if (valid()) error = e is FormatException ? e.message : '图片未能处理，请重新选择。';
    } finally {
      if (bytes != null) bytes.fillRange(0, bytes.length, 0);
      decoded?.dispose();
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> mask(ui.Rect rectangle) async {
    if (!canSelect ||
        _pixels == null ||
        ![
          rectangle.left,
          rectangle.top,
          rectangle.right,
          rectangle.bottom,
        ].every((value) => value.isFinite))
      return;
    final rect = rectangle.intersect(const ui.Rect.fromLTRB(0, 0, 1, 1));
    if (rect.isEmpty) return;
    final serial = ++_serial, binding = scope(), pixels = _pixels!.clone();
    final masks = [..._masks, rect];
    busy = true;
    ++_revision;
    _notify();
    Uint8List? bytes;
    try {
      bytes = await renderLocalImagePixels(pixels, masks);
      if (current && serial == _serial && binding == scope()) {
        _bytes?.fillRange(0, _bytes!.length, 0);
        _bytes = bytes;
        _view = bytes.asUnmodifiableView();
        _masks
          ..clear()
          ..addAll(masks);
        bytes = null;
      }
    } catch (_) {
      if (current && serial == _serial) {
        _clear();
        error = '遮盖未完成，请重新选择图片。';
      }
    } finally {
      pixels.dispose();
      if (bytes != null) bytes.fillRange(0, bytes.length, 0);
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
      }
    }
  }

  /// Stops this explicit selection only, without erasing the original draft.
  void retireSelection() {
    if (_disposed) return;
    ++_serial;
    busy = false;
    _notify();
  }

  @override
  void removeLocal() {
    if (!canSelect) return;
    discard();
  }

  void discard() {
    if (_disposed) return;
    ++_serial;
    busy = false;
    _clear();
    // Local removal does not alter the owning domain's history/permission error.
    error = null;
    _notify();
  }

  LocalImageRetainedDraft? retain() =>
      _disposed || busy || _bytes == null || _pixels == null
      ? null
      : LocalImageRetainedDraft._(
          Uint8List.fromList(_bytes!),
          _pixels!.clone(),
          List.of(_masks),
        );

  bool restore(LocalImageRetainedDraft retained) {
    if (!canSelect || retained._disposed) return false;
    ++_serial;
    _clear();
    _bytes = Uint8List.fromList(retained._bytes);
    _view = _bytes!.asUnmodifiableView();
    _pixels = retained._pixels.clone();
    _masks.addAll(retained._masks);
    _notify();
    return true;
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    ++_serial;
    _clear();
    super.dispose();
  }
}
