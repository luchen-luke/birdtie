import 'dart:async';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'moment_image_header.dart';
import 'moment_image_text_rules.dart';

class MomentImageTextResult {
  const MomentImageTextResult(this.lines, {required this.limited});
  final List<MomentImageTextLine> lines;
  final bool limited;
}

class MomentImageTextFailure implements Exception {
  const MomentImageTextFailure(this.message);
  final String message;
}

({int width, int height, int sample}) momentImageTextDimensions(
  int width,
  int height,
) {
  var sample = 1;
  int scaled(int n) => (n + sample - 1) ~/ sample;
  while (scaled(width) > 1600 ||
      scaled(height) > 1600 ||
      scaled(width) * scaled(height) > 1000000) {
    sample *= 2;
  }
  return (width: scaled(width), height: scaled(height), sample: sample);
}

/// Concrete Android channel. Other platforms keep manual masking available.
class MomentImageTextRecognizer {
  MomentImageTextRecognizer({
    MethodChannel? channel,
    bool Function()? supported,
    this.timeout = const Duration(seconds: 35),
  }) : _channel = channel ?? const MethodChannel('birdtie/local_image_text'),
       _supported =
           supported ??
           (() => !kIsWeb && defaultTargetPlatform == TargetPlatform.android);
  final MethodChannel _channel;
  final bool Function() _supported;
  final Duration timeout;
  static int _sequence = 0;
  static bool _inFlight = false;
  String? _request;

  Future<MomentImageTextResult> recognize(Uint8List bytes) async {
    if (!_supported()) {
      throw const MomentImageTextFailure('当前设备暂不支持本机文字检查；仍可手动遮挡或清除图片。');
    }
    if (_inFlight) throw const MomentImageTextFailure('上一次文字检查尚未结束，请稍后再试。');
    final header = MomentImageHeader.inspect(bytes);
    final hash = sha256.convert(bytes).toString();
    final id = 'local-text-${++_sequence}';
    _request = id;
    _inFlight = true;
    // A timeout does not release this gate. Only the actual native reply does.
    final original = _channel.invokeMethod<Object?>('recognize', {
      'requestId': id,
      'sha256': hash,
      'bytes': Uint8List.fromList(bytes),
      'width': header.width,
      'height': header.height,
    });
    final settled = original.whenComplete(() {
      _inFlight = false;
      if (_request == id) _request = null;
    });
    try {
      final value = await settled.timeout(timeout);
      if (value is! Map ||
          value.length != 6 ||
          value['requestId'] != id ||
          value['sha256'] != hash ||
          value['width'] is! int ||
          value['height'] is! int ||
          value['limited'] is! bool ||
          value['lines'] is! List) {
        throw const FormatException('本机文字结果不完整');
      }
      final w = value['width'] as int, h = value['height'] as int;
      final expected = momentImageTextDimensions(header.width, header.height);
      final raw = value['lines'] as List;
      if (w != expected.width || h != expected.height || raw.length > 64) {
        throw const FormatException('本机文字结果超过范围');
      }
      var size = 0;
      final lines = <MomentImageTextLine>[];
      for (final row in raw) {
        if (row is! Map ||
            row.length != 2 ||
            row['text'] is! String ||
            row['rect'] is! List) {
          throw const FormatException('文字位置无效');
        }
        final text = row['text'] as String, rect = row['rect'] as List;
        size += text.length;
        if (text.length > 1024 ||
            size > 8192 ||
            rect.length != 4 ||
            rect.any((n) => n is! int)) {
          throw const FormatException('文字结果超过范围');
        }
        final x = rect.cast<int>();
        if (x[0] < 0 ||
            x[1] < 0 ||
            x[2] > w ||
            x[3] > h ||
            x[2] <= x[0] ||
            x[3] <= x[1]) {
          throw const FormatException('文字位置超出当前图片');
        }
        lines.add(
          MomentImageTextLine(
            text,
            Rect.fromLTRB(x[0] / w, x[1] / h, x[2] / w, x[3] / h),
          ),
        );
      }
      return MomentImageTextResult(
        List.unmodifiable(lines),
        limited: value['limited'] as bool,
      );
    } on TimeoutException {
      unawaited(cancel());
      throw const MomentImageTextFailure('文字检查用时过长，正在停止；仍可手动遮挡，旧检查结束前不能再检查。');
    } on MissingPluginException {
      throw const MomentImageTextFailure('当前设备暂不支持本机文字检查；仍可手动遮挡或清除图片。');
    } on PlatformException catch (e) {
      throw MomentImageTextFailure(switch (e.code) {
        'BUSY' => '上一次文字检查尚未结束，请稍后再试。',
        'CANCELLED' => '文字检查已停止，未上传图片。',
        'LIMIT' => '图片或文字超过本机检查范围；请手动遮挡或清除图片。',
        _ => '本机文字检查暂未完成；请重试或手动遮挡。',
      });
    }
  }

  Future<void> cancel() async {
    final id = _request;
    if (id == null) return;
    try {
      await _channel.invokeMethod<void>('cancel', {'requestId': id});
    } catch (_) {
      /* Native single-flight still gates unfinished work. */
    }
  }
}
