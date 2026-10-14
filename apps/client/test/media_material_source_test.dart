import 'dart:async';
import 'dart:typed_data';
import 'package:birdtie_client/src/content/moment_image_header.dart';
import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/content/private_moment_media_controller.dart';
import 'package:birdtie_client/src/content/private_moment_media_pending_store.dart';
import 'package:birdtie_client/src/content/video_material_controller.dart';
import 'package:birdtie_client/src/content/video_material_page.dart';
import 'package:birdtie_client/src/content/video_material_picker.dart';
import 'package:birdtie_client/src/content/video_material_pending_store.dart';
import 'package:birdtie_client/src/workspace/local_image_draft.dart';
import 'package:birdtie_client/src/workspace/task_image_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image_picker/image_picker.dart';
import 'package:http/testing.dart';
import 'package:http/http.dart' as http;
import 'moment_image_picker_test.dart'
    show ImagePickerUnitSpy, OversizedImageFile;
import 'private_moment_media_controller_test.dart'
    show TestMomentImagePicker, syntheticPrivatePNG, imageOwner, imageMoment;
import 'video_material_picker_test.dart'
    show syntheticVideoBytes, FakeVideoPreview, FakeVideoPicker;
import 'video_material_controller_test.dart'
    show videoCapabilities, videoResponse, videoOwner;

// Real picker/codec/controller boundaries with injected local system selectors;
// no device-camera, browser chooser, native video decoder or provider claim.
class SourceImagePicker extends TestMomentImagePicker {
  SourceImagePicker(super.bytes);
  MomentImageSource? seen;
  @override
  bool supportsSource(MomentImageSource source) => true;
  @override
  Future<MomentImageSelection?> selectSource(MomentImageSource source) {
    seen = source;
    return select();
  }
}

class SourceVideoPicker extends VideoMaterialPicker {
  SourceVideoPicker({required super.fileSelector, this.available = true});
  final bool available;
  int decoded = 0;
  @override
  bool get supported => available;
  @override
  Future<void> discardLostSelection() async {}
  @override
  Future<VideoMaterialSelection> openPrivateBytes(Uint8List bytes) async {
    decoded++;
    return VideoMaterialSelection(bytes: bytes, preview: FakeVideoPreview());
  }
}

class ShortReadFile extends XFile {
  ShortReadFile(this.bytes, this.declared) : super('never-persist-this-name');
  final Uint8List bytes;
  final int declared;
  @override
  Future<int> length() async => declared;
  @override
  Stream<Uint8List> openRead([int? start, int? end]) => Stream.value(bytes);
}

Uint8List animatedControl(Uint8List png) {
  final control = Uint8List(20);
  ByteData.sublistView(control).setUint32(0, 8);
  control.setRange(4, 8, 'acTL'.codeUnits);
  ByteData.sublistView(control).setUint32(8, 2);
  return Uint8List.fromList([...png.take(33), ...control, ...png.skip(33)]);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'source gallery preserves the original select fixture and no file dialog',
    () async {
      final picker = TestMomentImagePicker(await syntheticPrivatePNG());
      final selected = await picker.selectSource(MomentImageSource.gallery);
      expect(selected!.header.mime, 'image/png');
      expect(picker.calls, 1);
    },
  );
  for (final platform in [TargetPlatform.android, TargetPlatform.iOS]) {
    test(
      'native $platform capture uses one explicit image gesture without full metadata',
      () async {
        debugDefaultTargetPlatformOverride = platform;
        addTearDown(() => debugDefaultTargetPlatformOverride = null);
        final spy = ImagePickerUnitSpy()
          ..result = XFile.fromData(await syntheticPrivatePNG());
        var files = 0;
        final picker = MomentImagePicker(
          picker: spy,
          fileSelector: (_) async {
            files++;
            return null;
          },
        );
        expect(picker.cameraSupported, true);
        expect(
          (await picker.selectSource(MomentImageSource.camera))!.header.mime,
          'image/png',
        );
        expect(spy.seenSource, ImageSource.camera);
        expect(spy.fullMetadata, false);
        expect(spy.calls, 1);
        expect(files, 0);
      },
    );
  }
  for (final platform in [
    TargetPlatform.windows,
    TargetPlatform.linux,
    TargetPlatform.macOS,
  ]) {
    test(
      '$platform does not expose a fake camera or fall back to gallery',
      () async {
        debugDefaultTargetPlatformOverride = platform;
        addTearDown(() => debugDefaultTargetPlatformOverride = null);
        final spy = ImagePickerUnitSpy();
        var files = 0;
        final picker = MomentImagePicker(
          picker: spy,
          fileSelector: (_) async {
            files++;
            return null;
          },
        );
        expect(picker.cameraSupported, false);
        expect(picker.fileSupported, true);
        await expectLater(
          picker.selectSource(MomentImageSource.camera),
          throwsFormatException,
        );
        expect(spy.calls, 0);
        expect(files, 0);
      },
    );
  }
  test(
    'single image file filters cover mobile desktop Web but pixels determine the type',
    () async {
      var calls = 0;
      final picker = MomentImagePicker(
        fileSelector: (groups) async {
          calls++;
          expect(groups, hasLength(1));
          expect(groups.single.extensions, ['jpg', 'jpeg', 'png']);
          expect(groups.single.mimeTypes, ['image/jpeg', 'image/png']);
          expect(groups.single.uniformTypeIdentifiers, [
            'public.jpeg',
            'public.png',
          ]);
          return XFile.fromData(
            await syntheticPrivatePNG(),
            mimeType: 'application/pdf',
            name: 'untrusted-name.pdf',
          );
        },
      );
      expect(
        (await picker.selectSource(MomentImageSource.file))!.header.mime,
        'image/png',
      );
      expect(calls, 1);
      final bad = MomentImagePicker(
        fileSelector: (_) async => XFile.fromData(
          Uint8List.fromList('%PDF'.codeUnits),
          mimeType: 'image/png',
        ),
      );
      await expectLater(
        bad.selectSource(MomentImageSource.file),
        throwsFormatException,
      );
    },
  );
  test(
    'file cancel, pre-read byte cap and changed read length remain closed',
    () async {
      final large = OversizedImageFile();
      final picker = MomentImagePicker(fileSelector: (_) async => large);
      await expectLater(
        picker.selectSource(MomentImageSource.file),
        throwsFormatException,
      );
      expect(large.opened, false);
      final raw = await syntheticPrivatePNG();
      final short = MomentImagePicker(
        fileSelector: (_) async => ShortReadFile(raw, raw.length + 1),
      );
      await expectLater(
        short.selectSource(MomentImageSource.file),
        throwsFormatException,
      );
      expect(
        await MomentImagePicker(
          fileSelector: (_) async => null,
        ).selectSource(MomentImageSource.file),
        isNull,
      );
    },
  );
  test(
    'file dialog permission failure is readable and does not select bytes',
    () async {
      final picker = MomentImagePicker(
        fileSelector: (_) async =>
            throw PlatformException(code: 'access_denied'),
      );
      await expectLater(
        picker.selectSource(MomentImageSource.file),
        throwsA(isA<FormatException>()),
      );
    },
  );
  for (final source in [MomentImageSource.camera, MomentImageSource.file]) {
    test(
      'local $source late recipient ABA selection cannot attach or restore pixels',
      () async {
        final picker = SourceImagePicker(await syntheticPrivatePNG())
          ..pending = Completer<MomentImageSelection?>();
        var recipient = 'a';
        final draft = LocalImageDraftController(
          isCurrent: () => true,
          scope: () => recipient,
          picker: picker,
        );
        addTearDown(draft.dispose);
        final selecting = draft.select(
          gestureCurrent: () => recipient == 'a',
          source: source,
        );
        await Future<void>.delayed(Duration.zero);
        expect(picker.seen, source);
        recipient = 'b';
        draft.retireSelection();
        recipient = 'a';
        picker.pending!.complete(MomentImageSelection(picker.bytes));
        await selecting;
        expect(draft.localBytes, isNull);
        expect(draft.busy, false);
      },
    );
  }
  test(
    'canceled file selection preserves the original local image without creating approval',
    () async {
      final picker = SourceImagePicker(await syntheticPrivatePNG());
      final draft = LocalImageDraftController(
        isCurrent: () => true,
        scope: () => 'original-recipient',
        picker: picker,
      );
      addTearDown(draft.dispose);
      await draft.select(gestureCurrent: () => true);
      final original = draft.localBytes;
      picker.canceled = true;
      await draft.select(
        gestureCurrent: () => true,
        source: MomentImageSource.file,
      );
      expect(draft.localBytes, same(original));
      expect(draft.hasPixels, true);
      expect(picker.seen, MomentImageSource.file);
    },
  );
  test(
    'Task file pixels stay local before any Task and require an explicit local review',
    () async {
      final changes = ChangeNotifier(),
          picker = SourceImagePicker(await syntheticPrivatePNG());
      final draft = TaskImageDraftController(
        authorizationHeader: () => 'Bearer synthetic',
        ownerID: () => imageOwner,
        cityID: () => 'original-city',
        sourceCurrent: () => true,
        sourceChanges: changes,
        picker: picker,
      );
      try {
        await draft.select(source: MomentImageSource.file);
        expect(picker.seen, MomentImageSource.file);
        expect(draft.localBytes, isNotNull);
        expect(MomentImageHeader.inspect(draft.localBytes!).mime, 'image/png');
        expect(draft.reviewed, false);
        expect(draft.canUse, false);
        expect(draft.snapshot(), isNull);
        draft.setReviewed(true);
        expect(draft.canUse, true);
      } finally {
        draft.dispose();
        changes.dispose();
      }
    },
  );
  test(
    'private Moment file rejects APNG before first-frame decoding with zero HTTP',
    () async {
      final changes = ChangeNotifier();
      var requests = 0;
      final client = MockClient((r) async {
        requests++;
        return http.Response('{}', 500);
      });
      final picker = SourceImagePicker(
        animatedControl(await syntheticPrivatePNG()),
      );
      final c = PrivateMomentMediaController(
        momentID: imageMoment,
        momentRevision: 1,
        authorizationHeader: () => 'Bearer synthetic',
        ownerID: () => imageOwner,
        identityChanges: changes,
        sourceCurrent: () => true,
        apiBaseUrl: 'http://fixture',
        client: client,
        picker: picker,
        pendingStore: MemoryPrivateImagePendingStore(),
      );
      try {
        await c.select(source: MomentImageSource.file);
        expect(picker.seen, MomentImageSource.file);
        expect(c.hasLocalImage, false);
        expect(c.error, contains('静态'));
        expect(c.preview, isNull);
        expect(requests, 0);
      } finally {
        c.dispose();
        changes.dispose();
        client.close();
      }
    },
  );
  test(
    'MP4 file uses original bounded container check and existing private-byte preview',
    () async {
      final raw = syntheticVideoBytes();
      var files = 0;
      final picker = SourceVideoPicker(
        fileSelector: (groups) async {
          files++;
          expect(groups.single.extensions, ['mp4']);
          expect(groups.single.mimeTypes, ['video/mp4']);
          expect(groups.single.uniformTypeIdentifiers, ['public.mpeg-4']);
          return XFile.fromData(raw, name: 'untrusted-private-name.mp4');
        },
      );
      final selected = (await picker.selectSource(VideoMaterialSource.file))!;
      try {
        expect(selected.bytes, orderedEquals(raw));
        expect(selected.durationMS, 1000);
        expect(picker.decoded, 1);
        expect(files, 1);
      } finally {
        await selected.dispose();
      }
      final wrong = SourceVideoPicker(
        fileSelector: (_) async =>
            XFile.fromData(await syntheticPrivatePNG(), mimeType: 'video/mp4'),
      );
      await expectLater(
        wrong.selectSource(VideoMaterialSource.file),
        throwsFormatException,
      );
      expect(wrong.decoded, 0);
    },
  );
  test(
    'unsupported video platform cannot open a file dialog or fabricate a preview',
    () async {
      var files = 0;
      final picker = SourceVideoPicker(
        available: false,
        fileSelector: (_) async {
          files++;
          return null;
        },
      );
      await expectLater(
        picker.selectSource(VideoMaterialSource.file),
        throwsFormatException,
      );
      expect(files, 0);
      expect(picker.decoded, 0);
      final gallery = FakeVideoPicker()
        ..next = VideoMaterialSelection(
          bytes: syntheticVideoBytes(),
          preview: FakeVideoPreview(),
        );
      final selected = await gallery.selectSource(VideoMaterialSource.gallery);
      expect(selected, same(gallery.next));
      await selected!.dispose();
    },
  );
  testWidgets(
    'existing video page imports one MP4 file while provider OFF prevents upload',
    (t) async {
      final methods = <String>[];
      final client = MockClient((r) async {
        methods.add(r.method);
        return r.url.path.endsWith('capabilities')
            ? videoResponse(videoCapabilities(available: false))
            : videoResponse({'items': []});
      });
      var files = 0;
      final picker = SourceVideoPicker(
        fileSelector: (_) async {
          files++;
          return XFile.fromData(syntheticVideoBytes());
        },
      );
      final changes = ChangeNotifier();
      final c = VideoMaterialController(
        authorizationHeader: () => 'Bearer synthetic',
        ownerID: () => videoOwner,
        sourceCurrent: () => true,
        identityChanges: changes,
        client: client,
        apiBaseUrl: 'http://fixture',
        picker: picker,
        pendingStore: MemoryVideoMaterialPendingStore(),
      );
      try {
        await t.pumpWidget(MaterialApp(home: VideoMaterialPage(controller: c)));
        await t.pumpAndSettle();
        final button = find.byKey(const Key('video-select-file'));
        await t.ensureVisible(button);
        await t.pumpAndSettle();
        await t.tap(button);
        await t.pumpAndSettle();
        expect(files, 1);
        expect(c.selection, isNotNull);
        expect(c.canSave, false);
        expect(c.active, isNull);
        expect(c.uncertain, false);
        expect(methods.every((v) => v == 'GET'), true);
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox());
        c.dispose();
        changes.dispose();
        client.close();
      }
    },
  );
}
