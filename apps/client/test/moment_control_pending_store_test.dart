import 'dart:convert';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
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
