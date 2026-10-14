import 'dart:convert';
import 'package:birdtie_client/src/workspace/public_moment_photo.dart';
import 'package:birdtie_client/src/workspace/public_moment_photo_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'public_moment_photo_test.dart';

PublicMomentPhotoPending photoPending() => PublicMomentPhotoPending.preview(
  PublicMomentPhotoPreview.decode(
    photoApproval(photoInput().body()),
    photoInput(),
    DateTime.now().toUtc().add(const Duration(seconds: 29)),
  ),
);
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'public photo cold journal only metadata and exact owner origin namespaces',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      const a = SecurePublicMomentPhotoPendingStore(),
          b = SecurePublicMomentPhotoPendingStore();
      final p = photoPending();
      await a.write(photoBase, photoOwner, p);
      expect((await b.read(photoBase, photoOwner)).single.toJson(), p.toJson());
      await b.write(photoBase, photoOwner, p.unknown);
      final raw = (await const FlutterSecureStorage().readAll()).values.single;
      expect((jsonDecode(raw) as Map).keys.toSet(), p.toJson().keys.toSet());
      expect(raw, isNot(contains('合成测试')));
      expect(raw, isNot(contains('contentBase64')));
      expect(raw, isNot(contains('approval')));
      expect(raw, isNot(contains('Bearer')));
      expect(raw, isNot(contains('thumbnailPath')));
      expect(await b.read('https://other.test', photoOwner), isEmpty);
      expect(await b.read(photoBase, photoMoment), isEmpty);
      await b.remove(photoBase, photoOwner, p.unknown);
      expect(await a.read(photoBase, photoOwner), isEmpty);
    },
  );
  for (final secure in [false, true])
    test(
      'repair original P prohibits missing record promotion secure=$secure',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final PublicMomentPhotoPendingStore a = secure
            ? const SecurePublicMomentPhotoPendingStore()
            : MemoryPublicMomentPhotoPendingStore();
        final p = photoPending();
        await a.write(photoBase, photoOwner, p);
        await a.remove(photoBase, photoOwner, p);
        await expectLater(
          a.write(photoBase, photoOwner, p.unknown),
          throwsStateError,
        );
        expect(await a.read(photoBase, photoOwner), isEmpty);
      },
    );
  test(
    'shared secure serial and exact stage prevent old removal after advanced selector',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      const a = SecurePublicMomentPhotoPendingStore(),
          b = SecurePublicMomentPhotoPendingStore();
      final p = photoPending();
      await a.write(photoBase, photoOwner, p);
      await b.write(photoBase, photoOwner, p.unknown);
      await expectLater(a.remove(photoBase, photoOwner, p), throwsStateError);
      await expectLater(b.write(photoBase, photoOwner, p), throwsStateError);
      expect((await b.read(photoBase, photoOwner)).single.stage, 'UNKNOWN');
      final bad = {...p.toJson(), 'photoId': photoMoment};
      expect(() => PublicMomentPhotoPending.decode(bad), throwsFormatException);
    },
  );
  testWidgets(
    'secure serial completed tail releases across first widget fake zone',
    (t) async {
      FlutterSecureStorage.setMockInitialValues({});
      const s = SecurePublicMomentPhotoPendingStore();
      await t.runAsync(() async {
        await s.write(photoBase, photoOwner, photoPending());
        expect((await s.read(photoBase, photoOwner)).length, 1);
      });
    },
  );
  testWidgets(
    'secure serial completed tail releases across second widget fake zone',
    (t) async {
      FlutterSecureStorage.setMockInitialValues({});
      const s = SecurePublicMomentPhotoPendingStore();
      await t.runAsync(() async {
        await s.write(photoBase, photoOwner, photoPending());
        expect((await s.read(photoBase, photoOwner)).length, 1);
      });
    },
  );
}
