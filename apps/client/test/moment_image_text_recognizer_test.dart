import 'dart:async';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/content/moment_image_text_recognizer.dart';
import 'moment_image_header_test.dart' show headerPNG;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const channel = MethodChannel('birdtie/local_image_text');
  final binding =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  final bytes = headerPNG(width: 4, height: 4);
  Map<String, Object?> response(MethodCall c) {
    final a = c.arguments as Map;
    return {
      'requestId': a['requestId'],
      'sha256': a['sha256'],
      'width': 4,
      'height': 4,
      'limited': false,
      'lines': [
        {
          'text': 'student@example.org',
          'rect': [0, 0, 2, 2],
        },
      ],
    };
  }

  tearDown(() => binding.setMockMethodCallHandler(channel, null));
  test('本机OCR适配器：显式调用才传当前字节无身份秘密并核实框', () async {
    var calls = 0;
    binding.setMockMethodCallHandler(channel, (c) async {
      calls++;
      expect(c.method, 'recognize');
      final a = c.arguments as Map;
      expect(a.keys.toSet(), {
        'requestId',
        'sha256',
        'bytes',
        'width',
        'height',
      });
      expect(a['bytes'], bytes);
      return response(c);
    });
    final x = MomentImageTextRecognizer(
      channel: channel,
      supported: () => true,
    );
    expect(calls, 0);
    final r = await x.recognize(bytes);
    expect(calls, 1);
    expect(r.limited, isFalse);
    expect(r.lines.single.region.right, .5);
    expect(r.lines.single.text, 'student@example.org');
  });
  test('本机OCR适配器：坏主体尺寸框文本及未知字段保守拒绝', () async {
    for (final mutate in <void Function(Map<String, Object?>)>[
      (r) => r['sha256'] = '0' * 64,
      (r) => r['requestId'] = 'other',
      (r) => r['width'] = 8,
      (r) => r['height'] = 3,
      (r) => r['grant'] = true,
      (r) => r['lines'] = [
        {
          'text': 'a@b.cn',
          'rect': [-1, 0, 2, 2],
        },
      ],
      (r) => r['lines'] = [
        {
          'text': 'a@b.cn',
          'rect': [0, 0, 5, 2],
        },
      ],
      (r) => r['lines'] = [
        {
          'text': 'x' * 1025,
          'rect': [0, 0, 2, 2],
        },
      ],
      (r) => r['lines'] = List.filled(65, {
        'text': 'x',
        'rect': [0, 0, 2, 2],
      }),
    ]) {
      binding.setMockMethodCallHandler(channel, (c) async {
        final r = response(c);
        mutate(r);
        return r;
      });
      await expectLater(
        MomentImageTextRecognizer(
          channel: channel,
          supported: () => true,
        ).recognize(bytes),
        throwsFormatException,
      );
    }
  });
  test('本机OCR适配器：缩放尺寸由原图片确定而非回执自称', () {
    expect(momentImageTextDimensions(4000, 3000), (
      width: 1000,
      height: 750,
      sample: 4,
    ));
    expect(momentImageTextDimensions(3201, 1601), (
      width: 801,
      height: 401,
      sample: 4,
    ));
    expect(momentImageTextDimensions(4, 4), (width: 4, height: 4, sample: 1));
  });
  test('本机OCR适配器：不支持及原生不可用保留人工出口', () async {
    var calls = 0;
    binding.setMockMethodCallHandler(channel, (c) async {
      calls++;
      throw PlatformException(code: 'UNAVAILABLE');
    });
    await expectLater(
      MomentImageTextRecognizer(supported: () => false).recognize(bytes),
      throwsA(isA<MomentImageTextFailure>()),
    );
    expect(calls, 0);
    await expectLater(
      MomentImageTextRecognizer(supported: () => true).recognize(bytes),
      throwsA(isA<MomentImageTextFailure>()),
    );
    expect(calls, 1);
  });
  test('本机OCR适配器：timeout取消但原调用结束前禁止再开工作', () async {
    final pending = Completer<Object?>();
    MethodCall? request;
    final methods = <String>[];
    binding.setMockMethodCallHandler(channel, (c) async {
      methods.add(c.method);
      if (c.method == 'cancel') {
        expect(
          (c.arguments as Map)['requestId'],
          (request!.arguments as Map)['requestId'],
        );
        return null;
      }
      request = c;
      return pending.future;
    });
    final x = MomentImageTextRecognizer(
      timeout: const Duration(milliseconds: 10),
      supported: () => true,
    );
    await expectLater(
      x.recognize(bytes),
      throwsA(isA<MomentImageTextFailure>()),
    );
    await Future<void>.delayed(const Duration(milliseconds: 1));
    await expectLater(
      MomentImageTextRecognizer(supported: () => true).recognize(bytes),
      throwsA(isA<MomentImageTextFailure>()),
    );
    expect(methods.where((m) => m == 'recognize').length, 1);
    expect(methods, contains('cancel'));
    pending.complete(response(request!));
    await Future<void>.delayed(const Duration(milliseconds: 1));
    binding.setMockMethodCallHandler(channel, (c) async => response(c));
    expect((await x.recognize(bytes)).lines.length, 1);
  });
}
