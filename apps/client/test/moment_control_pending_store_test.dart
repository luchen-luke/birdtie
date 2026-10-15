import 'dart:convert';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
import 'package:birdtie_client/src/workspace/public_moment_media.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'moment_control_test.dart';

MomentControlPending mcPending() => MomentControlPending.preview(
  MomentControlPreview.fromJson(
    mcPreview(
      MomentControlInput(
        operationID: mcOp,
        action: 'CREATE',
        expectedRevision: 0,
        input: mcInput(),
      ),
    ),
    MomentControlInput(
      operationID: mcOp,
      action: 'CREATE',
      expectedRevision: 0,
      input: mcInput(),
    ),
  ),
);

/// Called by the single media-publication regression, without rerunning the
/// existing pending/controller suites or retaining private draft payloads.
Future<void> verifyDurableMediaPendingVariants(
  List<PublicMomentMediaSelection> ordered,
) async {
  const origin = 'https://isolated.invalid';
  const store = SecureMomentControlPendingStore();
  for (final action in ['PUBLISH', 'UPDATE_PUBLIC']) {
    String? omittedDigest;
    for (final media in <List<PublicMomentMediaSelection>?>[
      null,
      [],
      ordered,
    ]) {
      final input = MomentControlInput(
        operationID: mcOp,
        action: action,
        momentID: mcTarget,
        expectedRevision: action == 'PUBLISH' ? 1 : 2,
        input: mcInput(),
        media: media,
        localScope: media?.isNotEmpty == true ? mcOp : '',
      );
      final now = DateTime.now().toUtc();
      final pending = MomentControlPending.preview(
        MomentControlPreview(
          input: input,
          sourceVersion: 'a' * 64,
          approval: 'never-journal-approval',
          observedAt: now,
          expiresAt: now.add(const Duration(seconds: 30)),
          preservedPlaceUnavailable: false,
        ),
      );
      await store.write(origin, mcOwner, pending);
      final prepared = (await const SecureMomentControlPendingStore().read(
        origin,
        mcOwner,
      )).single;
      expect(prepared.stage, 'PREPARED');
      expect(prepared.payloadDigest, input.digest);
      expect(prepared.same(pending), isTrue);
      if (media == null) {
        omittedDigest = input.digest;
        expect(prepared.toJson().containsKey('mediaSelectors'), isFalse);
        expect(prepared.toJson().containsKey('localScope'), isFalse);
      } else {
        expect(input.digest, isNot(omittedDigest));
        expect(
          prepared.mediaSelectors!.map((v) => v.mediaID).toList(),
          media.map((v) => v.mediaID).toList(),
        );
        if (media.isNotEmpty) {
          final altered =
              jsonDecode(jsonEncode(prepared.toJson())) as Map<String, dynamic>;
          (altered['mediaSelectors'] as List).first['mediaVersion'] = 999;
          expect(
            () => MomentControlPending.fromJson(altered),
            throwsFormatException,
          );
          final reordered =
              jsonDecode(jsonEncode(prepared.toJson())) as Map<String, dynamic>;
          reordered['mediaSelectors'] = (reordered['mediaSelectors'] as List)
              .reversed
              .toList();
          expect(
            () => MomentControlPending.fromJson(reordered),
            throwsFormatException,
          );
        }
      }
      await store.write(origin, mcOwner, prepared.unknown);
      final restored = (await const SecureMomentControlPendingStore().read(
        origin,
        mcOwner,
      )).single;
      expect(restored.stage, 'UNKNOWN');
      expect(restored.same(pending), isTrue);
      final raw = (await const FlutterSecureStorage().readAll()).values.single;
      for (final forbidden in [
        '本人记录',
        'approval',
        'licenseReference',
        'canPublish',
        'contentBase64',
        'Bearer',
      ]) {
        expect(raw, isNot(contains(forbidden)));
      }
      expect(await store.read('https://other.invalid', mcOwner), isEmpty);
      expect(await store.read(origin, mcTarget), isEmpty);
      await store.remove(origin, mcOwner, restored);
    }
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'durable readback restores metadata only and isolates origin and owner',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      const a = SecureMomentControlPendingStore(),
          b = SecureMomentControlPendingStore();
      final p = mcPending();
      await a.write('https://isolated.invalid', mcOwner, p);
      expect(
        (await b.read('https://isolated.invalid', mcOwner)).single.toJson(),
        p.toJson(),
      );
      await b.write('https://isolated.invalid', mcOwner, p.unknown);
      final raw = await const FlutterSecureStorage().readAll();
      expect(raw.values.single, isNot(contains('本人记录')));
      expect(raw.values.single, isNot(contains('approval')));
      expect(jsonDecode(raw.values.single)['stage'], 'UNKNOWN');
      expect(await b.read('https://other.invalid', mcOwner), isEmpty);
      expect(await b.read('https://isolated.invalid', mcTarget), isEmpty);
      await b.remove('https://isolated.invalid', mcOwner, p.unknown);
      expect(await a.read('https://isolated.invalid', mcOwner), isEmpty);
    },
  );
  for (final secure in [false, true])
    test(
      'PREPARED repair prevents old UNKNOWN promotion secure=$secure',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final MomentControlPendingStore store = secure
            ? const SecureMomentControlPendingStore()
            : MemoryMomentControlPendingStore();
        final p = mcPending();
        await store.write('https://isolated.invalid', mcOwner, p);
        await store.remove('https://isolated.invalid', mcOwner, p);
        await expectLater(
          store.write('https://isolated.invalid', mcOwner, p.unknown),
          throwsStateError,
        );
        expect(await store.read('https://isolated.invalid', mcOwner), isEmpty);
      },
    );
  test(
    'full selector and stage comparison refuses deletion of an advanced operation',
    () async {
      final store = MemoryMomentControlPendingStore(), p = mcPending();
      await store.write('https://isolated.invalid', mcOwner, p);
      await store.write('https://isolated.invalid', mcOwner, p.unknown);
      await expectLater(
        store.remove('https://isolated.invalid', mcOwner, p),
        throwsStateError,
      );
      expect(
        (await store.read('https://isolated.invalid', mcOwner)).single.stage,
        'UNKNOWN',
      );
    },
  );
}
