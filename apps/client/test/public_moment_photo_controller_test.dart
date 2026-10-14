import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:birdtie_client/src/workspace/public_moment_photo.dart';
import 'package:birdtie_client/src/workspace/public_moment_photo_controller.dart';
import 'package:birdtie_client/src/workspace/public_moment_photo_pending_store.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_cover_test.dart' show coverJPEG, coverRights;
import 'public_moment_photo_test.dart';
import 'public_moment_photo_pending_store_test.dart' show photoPending;

class PhotoFixture {
  final changes = ValueNotifier(0);
  String owner = photoOwner, header = 'Bearer isolated-A';
  Object source = 0;
  String? workspace;
  bool visible = true,
      unknown = false,
      missingReceipt = false,
      unavailable = false,
      retired = false;
  int stateReads = 0,
      previews = 0,
      puts = 0,
      originalReads = 0,
      stops = 0,
      version = 0;
  Duration previewLease = const Duration(seconds: 24);
  String previewSource = 'a' * 64;
  Map<String, dynamic>? approved;
  Completer<http.Response>? slowPreview;
  Uint8List? derivative;
  final MemoryPublicMomentPhotoPendingStore store;
  late final MockClient client = MockClient(request);
  PhotoFixture({MemoryPublicMomentPhotoPendingStore? pendingStore})
    : store = pendingStore ?? MemoryPublicMomentPhotoPendingStore();
  Future<http.Response> request(http.Request r) async {
    expectSync(r.headers['Authorization'], header);
    expectSync(
      r.headers.containsKey('X-Birdtie-Organization-Workspace'),
      isFalse,
    );
    if (r.url.path.endsWith('/previews')) {
      previews++;
      approved = photoApproval(
        jsonDecode(r.body),
        lease: previewLease,
        derivative: derivative,
      );
      approved!['sourceVersion'] = previewSource;
      if (slowPreview != null) return slowPreview!.future;
      return photoWire(approved);
    }
    if (r.url.path.endsWith('/stop')) {
      stops++;
      expectSync(r.body, isEmpty);
      return photoWire(
        photoReceipt(
          approved ?? photoApproval(photoInput().body()),
          stop: true,
        ),
      );
    }
    if (r.url.path.contains('/operations/')) {
      if (r.method == 'PUT') {
        puts++;
        expectSync(
          (jsonDecode(r.body) as Map)['approval'],
          approved!['approval'],
        );
        expectSync(
          (jsonDecode(r.body) as Map)['sourceVersion'],
          approved!['sourceVersion'],
        );
        if (unknown) return photoWire(null, status: 500);
        version = (approved!['expectedPhotoVersion'] as int) + 1;
        return photoWire(photoReceipt(approved!));
      }
      originalReads++;
      if (missingReceipt) return photoWire(null, status: 404);
      version = (approved?['expectedPhotoVersion'] as int? ?? 0) + 1;
      return photoWire(
        photoReceipt(approved ?? photoApproval(photoInput().body())),
      );
    }
    if (r.url.path.endsWith('/public-photo')) {
      stateReads++;
      if (unavailable) return photoWire(null, status: 403);
      return photoWire({
        ...photoState(version: version, retired: retired),
        if (version > 0 && approved != null) 'photoId': approved!['photoId'],
      });
    }
    return photoWire(null, status: 404);
  }

  PublicMomentPhotoController controller({
    PublicMomentPhotoPendingStore? pendingStore,
  }) => PublicMomentPhotoController(
    momentID: photoMoment,
    api: PublicMomentPhotoAPI(
      client: client,
      baseUrl: photoBase,
      authorizationHeader: () => header,
    ),
    ownerID: () => owner,
    workspaceID: () => workspace,
    identityChanges: changes,
    sourceKey: () => source,
    sourceCurrent: () => visible,
    pendingStore: pendingStore ?? store,
  );
  void notify() => changes.value++;
  void dispose() => changes.dispose();
}

class _RepairInterleave extends MemoryPublicMomentPhotoPendingStore {
  bool intervene = false;
  @override
  Future<List<PublicMomentPhotoPending>> read(String o, String owner) async {
    final rows = await super.read(o, owner);
    if (intervene && rows.any((p) => p.stage == 'PREPARED')) {
      intervene = false;
      await super.remove(o, owner, rows.single);
    }
    return rows;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'SAVE source stamp change cannot rebase while DELETE private cleanup uses independent own stamp',
    () async {
      final f = PhotoFixture(), c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await c.load();
      c.select(coverJPEG());
      f.previewSource = 'b' * 64;
      expect(await c.prepare(action: 'SAVE', rights: coverRights()), isNull);
      expect(f.puts, 0);
      f.retired = true;
      f.version = 1;
      await c.load();
      expect(c.state?.photo, isNull);
      expect(c.state?.canSave, isFalse);
      expect(c.state?.canDelete, isTrue);
      final p = (await c.prepare(action: 'DELETE'))!;
      expect(p.sourceVersion, isNot(c.state!.sourceVersion));
      expect(p.derivative, isNull);
      expect(p.input.rights, isNull);
      await c.commit(p, c.epoch, painted: false);
      expect(f.puts, 1);
      expect(c.receipt?.status, 'SUCCEEDED');
    },
  );
  test(
    'only verified original success emits RAM same-owner origin media reread hint',
    () async {
      publicMomentPhotoChanges.value = null;
      final f = PhotoFixture()..unknown = true, c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      addTearDown(() => publicMomentPhotoChanges.value = null);
      await c.load();
      c.select(coverJPEG());
      final p = (await c.prepare(action: 'SAVE', rights: coverRights()))!;
      c.cancel(p);
      expect(publicMomentPhotoChanges.value, isNull);
      final next = (await c.prepare(action: 'SAVE', rights: coverRights()))!;
      await c.commit(next, c.epoch, painted: true);
      expect(publicMomentPhotoChanges.value, isNull);
      f.missingReceipt = true;
      await c.recover(c.pending.single);
      expect(publicMomentPhotoChanges.value, isNull);
      await c.recover(c.pending.single, stop: true);
      expect(publicMomentPhotoChanges.value, isNull);
      f.unknown = false;
      await c.load();
      c.select(coverJPEG());
      final success = (await c.prepare(action: 'SAVE', rights: coverRights()))!;
      await c.commit(success, c.epoch, painted: true);
      expect(publicMomentPhotoChanges.value?.ownerID, photoOwner);
      expect(publicMomentPhotoChanges.value?.origin, photoBase);
      expect(publicMomentPhotoChanges.value?.momentID, photoMoment);
    },
  );
  test(
    'preview is zero effect, independent new slot, actual derivative gate and one approved PUT',
    () async {
      final f = PhotoFixture(), c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await c.load();
      expect(c.localBytes, isNull);
      expect(c.canStart, isTrue);
      c.select(coverJPEG());
      final p = (await c.prepare(action: 'SAVE', rights: coverRights()))!;
      expect(p.derivative, coverJPEG());
      expect(p.input.photoID, isNot(p.input.operationID));
      expect(p.input.photoID, isNot(photoSlot));
      expect(f.puts, 0);
      await c.commit(p, c.epoch, painted: false);
      expect(f.puts, 0);
      await c.commit(p, c.epoch, painted: true);
      expect(f.puts, 1);
      expect(c.receipt?.succeeded, isTrue);
      expect(await f.store.read(photoBase, photoOwner), isEmpty);
      await c.commit(p, c.epoch, painted: true);
      expect(f.puts, 1);
    },
  );
  test(
    'unknown 404 never replays, restart restores only selector; stop fact independent current read 403',
    () async {
      final f = PhotoFixture()
        ..unknown = true
        ..missingReceipt = true;
      final c = f.controller();
      await c.load();
      c.select(coverJPEG());
      final p = (await c.prepare(action: 'SAVE', rights: coverRights()))!;
      await c.commit(p, c.epoch, painted: true);
      expect(c.canStart, isFalse);
      expect(c.pending.single.stage, 'UNKNOWN');
      c.dispose();
      final fresh = f.controller();
      addTearDown(fresh.dispose);
      addTearDown(f.dispose);
      await fresh.load();
      expect(fresh.localBytes, isNull);
      await fresh.recover(fresh.pending.single);
      expect(f.puts, 1);
      expect(fresh.pending.length, 1);
      expect(fresh.receipt, isNull);
      f.unavailable = true;
      await fresh.recover(fresh.pending.single, stop: true);
      expect(f.stops, 1);
      expect(f.puts, 1);
      expect(fresh.receipt?.status, 'STOPPED');
      expect(fresh.pending, isEmpty);
      expect(fresh.sourceUnavailable, isTrue);
      expect(fresh.canStart, isFalse);
    },
  );
  test(
    'success original immutable receipt survives current source unavailable without inferred image',
    () async {
      final f = PhotoFixture(), c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await c.load();
      c.select(coverJPEG());
      final p = (await c.prepare(action: 'SAVE', rights: coverRights()))!;
      f.unavailable = true;
      await c.commit(p, c.epoch, painted: true);
      expect(c.receipt?.status, 'SUCCEEDED');
      expect(c.state, isNull);
      expect(c.localBytes, isNull);
      expect(c.pending, isEmpty);
      expect(c.canStart, isFalse);
      expect(f.puts, 1);
    },
  );
  for (final axis in ['owner', 'target', 'scope'])
    test(
      'public photo $axis ABA retires late preview and captured approval permanently',
      () async {
        final f = PhotoFixture()..slowPreview = Completer<http.Response>(),
            c = f.controller();
        addTearDown(c.dispose);
        addTearDown(f.dispose);
        await c.load();
        c.select(coverJPEG());
        final future = c.prepare(action: 'SAVE', rights: coverRights());
        await Future<void>.delayed(Duration.zero);
        if (axis == 'owner') {
          f.owner = photoMoment;
          f.notify();
          f.owner = photoOwner;
          f.notify();
        } else if (axis == 'scope') {
          f.workspace = photoMoment;
          f.notify();
          f.workspace = null;
          f.notify();
        } else {
          f.source = 1;
          f.notify();
          f.source = 0;
          f.notify();
        }
        f.slowPreview!.complete(photoWire(f.approved));
        expect(await future, isNull);
        expect(c.current, isFalse);
        expect(c.localBytes, isNull);
        expect(f.puts, 0);
      },
    );
  test(
    'ordinary same owner renewal keeps local selection, retires old approval, rereads journal and allows new preview',
    () async {
      final f = PhotoFixture(), c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await c.load();
      c.select(coverJPEG());
      final old = (await c.prepare(action: 'SAVE', rights: coverRights()))!,
          e = c.epoch;
      f.header = 'Bearer isolated-renewed';
      f.notify();
      await Future<void>.delayed(const Duration(milliseconds: 30));
      expect(c.current, isTrue);
      expect(c.canStart, isTrue);
      expect(c.localBytes, coverJPEG());
      await c.commit(old, e, painted: true);
      expect(f.puts, 0);
      expect(await c.prepare(action: 'SAVE', rights: coverRights()), isNotNull);
      expect(f.previews, 2);
    },
  );
  test(
    'P repair interleave prevents U promotion and HTTP dispatch after another instance removes original',
    () async {
      final store = _RepairInterleave(),
          f = PhotoFixture(pendingStore: store),
          c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await c.load();
      c.select(coverJPEG());
      final p = (await c.prepare(action: 'SAVE', rights: coverRights()))!;
      store.intervene = true;
      await c.commit(p, c.epoch, painted: true);
      expect(f.puts, 0);
      expect(c.recoveryOperations, isNotEmpty);
      await c.repair(c.recoveryOperations.single);
      expect(c.recoveryOperations, isEmpty);
      expect(f.originalReads, 0);
    },
  );
  test(
    'lost public source preserves original tiny self history and no new authority',
    () async {
      final f = PhotoFixture()..unavailable = true;
      final p = photoPending().unknown;
      await f.store.write(photoBase, photoOwner, photoPending());
      await f.store.write(photoBase, photoOwner, p);
      f.approved = photoApproval(photoInput().body());
      final c = f.controller();
      addTearDown(c.dispose);
      addTearDown(f.dispose);
      await c.load();
      expect(c.pending.length, 1);
      expect(c.canStart, isFalse);
      await c.recover(c.pending.single);
      expect(c.receipt?.succeeded, isTrue);
      expect(c.pending, isEmpty);
      expect(c.state, isNull);
      expect(f.puts, 0);
      expect(f.originalReads, 1);
    },
  );
}
