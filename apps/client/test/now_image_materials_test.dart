import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/workspace/now_image_materials.dart';

class _Picker extends MomentImagePicker {
  _Picker(this.result);
  final Future<List<MomentImageSelection>> result;
  @override
  Future<List<MomentImageSelection>> selectMany({int limit = 9}) => result;
}

void main() {
  testWidgets(
    'late multi-selection cannot cross a retired creation, and its source bytes are erased',
    (tester) async {
      final bytes = await tester.runAsync(
        () => File('test/fixtures/map-media/isolation-a.jpg').readAsBytes(),
      );
      final source = Uint8List.fromList(bytes!);
      final pending = Completer<List<MomentImageSelection>>();
      final basket = NowImageMaterials();
      final a = Object(), b = Object();
      var current = a;
      final selecting = basket.select(
        scope: a,
        current: () => current == a,
        picker: _Picker(pending.future),
      );
      current = b;
      basket.bind(b);
      pending.complete([MomentImageSelection(source)]);
      await selecting;
      expect(basket.images, isEmpty);
      expect(source.every((byte) => byte == 0), isTrue);
      expect(basket.busy, isFalse);
      basket.dispose();
    },
  );

  testWidgets(
    'selected image order is explicit, immutable to callers, and removed pixels are erased',
    (tester) async {
      final bytes = await tester.runAsync(
        () => File('test/fixtures/map-media/isolation-a.jpg').readAsBytes(),
      );
      final selected = List.generate(
        3,
        (_) => MomentImageSelection(Uint8List.fromList(bytes!)),
      );
      final basket = NowImageMaterials(), scope = Object();
      await tester.runAsync(
        () => basket.select(
          scope: scope,
          current: () => true,
          picker: _Picker(Future.value(selected)),
        ),
      );
      expect(basket.images, hasLength(3));
      final first = basket.images.first;
      expect(() => first[0] = 0, throwsUnsupportedError);
      basket.move(0, 1);
      basket.remove(1);
      expect(first.every((byte) => byte == 0), isTrue);
      final retained = basket.images.first;
      basket.dispose();
      expect(retained.every((byte) => byte == 0), isTrue);
    },
  );
}
