import 'dart:async';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/workspace/friend_image.dart';
import 'package:birdtie_client/src/workspace/local_image_draft.dart';
import 'package:flutter_test/flutter_test.dart';

import 'private_moment_media_controller_test.dart' show syntheticPrivatePNG, TestMomentImagePicker;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('gallery and masking remain local, original pixels become a static PNG draft', () async {
    final picker = TestMomentImagePicker(await syntheticPrivatePNG());
    final draft = LocalImageDraftController(isCurrent: () => true, scope: () => 'recipient-a', picker: picker);
    addTearDown(draft.dispose);
    await draft.select(gestureCurrent: () => true);
    expect(picker.calls, 1);
    expect(draft.hasPixels, isTrue);
    expect(draft.width, 4); expect(draft.height, 4);
    final original = FriendImageDraft(draft.localBytes!, width: 4, height: 4);
    await draft.mask(const ui.Rect.fromLTRB(0, 0, .5, .5));
    expect(draft.masked, isTrue);
    expect(FriendImageDraft(draft.localBytes!, width: 4, height: 4,
        pixelRisk: 'USER_MASKED').inputSha256, isNot(original.inputSha256));
    expect(draft.localBytes, same(draft.localBytes));
  });

  test('recipient A-B-A invalidates a late native gallery result', () async {
    final picker = TestMomentImagePicker(await syntheticPrivatePNG())
      ..pending = Completer<MomentImageSelection?>();
    var target = 'a';
    final draft = LocalImageDraftController(isCurrent: () => true, scope: () => target, picker: picker);
    addTearDown(draft.dispose);
    final selecting = draft.select(gestureCurrent: () => target == 'a');
    await Future<void>.delayed(Duration.zero);
    target = 'b'; draft.retireSelection(); target = 'a';
    picker.pending!.complete(MomentImageSelection(picker.bytes));
    await selecting;
    expect(draft.localBytes, isNull); expect(draft.busy, isFalse);
  });

  test('retained draft has no grant, explicit restore clones pixels and removal wipes old view', () async {
    final draft = LocalImageDraftController(isCurrent: () => true, scope: () => 'owner-a',
        picker: TestMomentImagePicker(await syntheticPrivatePNG()));
    addTearDown(draft.dispose);
    await draft.select(gestureCurrent: () => true);
    final retained = draft.retain()!, oldView = draft.localBytes!;
    draft.discard();
    expect(oldView.every((b) => b == 0), isTrue);
    expect(draft.restore(retained), isTrue);
    retained.dispose();
    expect(draft.hasPixels, isTrue);
    draft.discard();
    expect(draft.restore(retained), isFalse);
  });

  test('APNG animation control chunks are rejected before local decoding', () {
    final bytes = Uint8List.fromList([137, 80, 78, 71, 13, 10, 26, 10,
      0, 0, 0, 0, 97, 99, 84, 76, 0, 0, 0, 0]);
    expect(() => rejectAnimatedLocalImage(bytes), throwsFormatException);
  });

  test('retired in-flight mask preserves the prior pixels and prior risk flag', () async {
    var epoch = 0;
    final draft = LocalImageDraftController(isCurrent: () => true, scope: () => epoch,
        picker: TestMomentImagePicker(await syntheticPrivatePNG()));
    addTearDown(draft.dispose);
    await draft.select(gestureCurrent: () => true);
    final bytes = Uint8List.fromList(draft.localBytes!);
    final masking = draft.mask(const ui.Rect.fromLTRB(0, 0, .5, .5));
    epoch++; draft.retireSelection();
    await masking;
    expect(draft.localBytes, orderedEquals(bytes));
    expect(draft.masked, isFalse);
  });
}
