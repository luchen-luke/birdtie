import 'dart:async';
import 'dart:convert';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/content/moment_image_header.dart';
import 'package:birdtie_client/src/content/private_moment_media_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:birdtie_client/src/content/private_moment_media_pending_store.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'moment_image_header_test.dart' show headerPNG;

const imageOwner = '22222222-2222-4222-8222-222222222222';
const imageMoment = '11111111-1111-4111-8111-111111111111';
const imageID = '33333333-3333-4333-8333-333333333333';

class ImageCredentials extends ChangeNotifier {
  String? token = 'Bearer synthetic-a', owner = imageOwner, workspace;
  bool source = true;
  void event() {
    notifyListeners();
  }

  void actor(String value) {
    token = value;
    event();
  }
}

class TestMomentImagePicker extends MomentImagePicker {
  TestMomentImagePicker(this.bytes);
  Uint8List bytes;
  bool canceled = false;
  Completer<MomentImageSelection?>? pending;
  bool lost = false;
  int calls = 0;
  @override
  Future<MomentImageSelection?> select() async {
    calls++;
    return pending != null
        ? pending!.future
        : canceled
        ? null
        : MomentImageSelection(bytes);
  }

  @override
  Future<bool> discardLostSelection() async => lost;
}

Future<Uint8List> syntheticPrivatePNG() async {
  final rec = ui.PictureRecorder();
  final c = ui.Canvas(rec);
  c.drawRect(
    const ui.Rect.fromLTWH(0, 0, 4, 4),
    ui.Paint()..color = const ui.Color(0xffff0000),
  );
  final p = rec.endRecording();
  final image = await p.toImage(4, 4);
  p.dispose();
  try {
    final b = await image.toByteData(format: ui.ImageByteFormat.png);
    return Uint8List.fromList(
      b!.buffer.asUint8List(b.offsetInBytes, b.lengthInBytes),
    );
  } finally {
    image.dispose();
  }
}

class MediaUnitFixture {
  MediaUnitFixture(Uint8List bytes, {PrivateImagePendingStore? pendingStore}) {
    picker = TestMomentImagePicker(bytes);
    client = MockClient((r) async {
      requests.add(r);
      return custom == null ? route(r) : await custom!(r);
    });
    controller = PrivateMomentMediaController(
      momentID: imageMoment,
      momentRevision: 1,
      authorizationHeader: () => credentials.token,
      ownerID: () => credentials.owner,
      identityChanges: credentials,
      workspaceChanges: credentials,
      organizationWorkspaceID: () => credentials.workspace,
      sourceCurrent: () => credentials.source,
      apiBaseUrl: 'https://birdtie.example',
      client: client,
      picker: picker,
      clock: () => now,
      pendingStore: pendingStore ?? MemoryPrivateImagePendingStore(),
    );
  }
  final credentials = ImageCredentials();
  var now = DateTime.utc(2026, 10, 6);
  late TestMomentImagePicker picker;
  late MockClient client;
  late PrivateMomentMediaController controller;
  final requests = <http.Request>[];
  Map<String, dynamic>? metadata;
  Uint8List? stored;
  String state = 'preview';
  bool deleted = false;
  Future<http.Response> Function(http.Request)? custom;
  Map<String, dynamic> receipt({
    String? status,
    String? owner,
    int revision = 1,
  }) => {
    'id': imageID,
    'operationId': metadata!['operationId'],
    'momentId': imageMoment,
    'ownerAccountId': owner ?? imageOwner,
    'momentRevision': 1,
    'mimeType': 'image/png',
    'byteSize': metadata!['byteSize'],
    'inputSha256': metadata!['sha256'],
    'sha256': state == 'ready_private'
        ? sha256.convert(stored!).toString()
        : '',
    'pixelRisk': metadata!['pixelRisk'],
    'purpose': privateMomentImagePurpose,
    'status': status ?? state,
    'revision': revision,
    'previewExpiresAt': now.add(const Duration(minutes: 5)).toIso8601String(),
    'retainUntil': now.add(const Duration(days: 30)).toIso8601String(),
  };
  http.Response wire(Object data, [int status = 200]) => http.Response.bytes(
    utf8.encode(jsonEncode({'data': data})),
    status,
    headers: {'content-type': 'application/json'},
  );
  http.Response route(http.Request r) {
    if (r.method == 'POST') {
      metadata = jsonDecode(r.body) as Map<String, dynamic>;
      return wire(receipt(), 201);
    }
    if (r.method == 'PUT') {
      stored = Uint8List.fromList(r.bodyBytes);
      state = 'ready_private';
      return wire(receipt(revision: 2));
    }
    if (r.method == 'DELETE') {
      deleted = true;
      state = 'deleted';
      return http.Response('', 204);
    }
    if (r.url.path.endsWith('/private-images')) {
      return wire(state == 'ready_private' ? [receipt(revision: 2)] : []);
    }
    if (r.url.path.endsWith('/content')) {
      return http.Response.bytes(
        stored!,
        200,
        headers: {'content-type': 'image/png'},
      );
    }
    return wire(
      receipt(
        status: deleted ? 'deleted' : state,
        revision: state == 'preview'
            ? 1
            : state == 'deleted'
            ? 3
            : 2,
      ),
    );
  }

  Future<void> local() async {
    await controller.select();
    controller.acknowledgeLocal(true);
  }

  Future<void> preview() async {
    await local();
    await controller.prepare();
  }

  void close() {
    controller.dispose();
    credentials.dispose();
    client.close();
  }
}

class JournalSpy extends MemoryPrivateImagePendingStore {
  bool readFails = false, writeFails = false, deleteFails = false;
  void Function()? duringWrite, duringRead;
  @override
  Future<List<PendingPrivateImageOperation>> read(
    String environment,
    String owner,
    String moment,
  ) async {
    if (readFails) throw StateError('synthetic read failure');
    final rows = await super.read(environment, owner, moment);
    duringRead?.call();
    return rows;
  }

  @override
  Future<void> write(String environment, PendingPrivateImageOperation v) async {
    await super.write(environment, v);
    duringWrite?.call();
    if (writeFails) throw StateError('synthetic write uncertain');
  }

  @override
  Future<void> delete(
    String environment,
    PendingPrivateImageOperation v,
  ) async {
    if (deleteFails) throw StateError('synthetic cleanup failure');
    await super.delete(environment, v);
  }
}

PendingPrivateImageOperation pendingReference(
  MediaUnitFixture x,
  String phase,
) {
  final ready = x.state == 'ready_private';
  final v = x.receipt(revision: ready ? 2 : 1);
  return PendingPrivateImageOperation(
    ownerID: imageOwner,
    momentID: imageMoment,
    operationID: v['operationId'],
    phase: phase,
    momentRevision: 1,
    mime: 'image/png',
    byteSize: v['byteSize'],
    inputHash: v['inputSha256'],
    pixelRisk: v['pixelRisk'],
    assetID: phase == 'preview' ? null : imageID,
    assetRevision: phase == 'preview'
        ? null
        : phase == 'save'
        ? 1
        : 2,
    derivativeHash: phase == 'delete' ? v['sha256'] : null,
    observedAt: x.now,
    deadlineAt: x.now.add(const Duration(minutes: 5)),
  );
}

void reopenForRecovery(MediaUnitFixture x, PrivateImagePendingStore store) {
  x.controller.dispose();
  x.controller = PrivateMomentMediaController(
    momentID: imageMoment,
    momentRevision: 2,
    authorizationHeader: () => x.credentials.token,
    ownerID: () => x.credentials.owner,
    identityChanges: x.credentials,
    workspaceChanges: x.credentials,
    organizationWorkspaceID: () => x.credentials.workspace,
    sourceCurrent: () => x.credentials.source,
    apiBaseUrl: 'https://birdtie.example',
    client: x.client,
    pendingStore: store,
    clock: () => x.now,
  );
}

void main() {
  testWidgets('恢复记录读取失败不打开图库也不产生HTTP且不会假空列表', (t) async {
    await t.runAsync(() async {
      final store = JournalSpy()..readFails = true;
      final x = MediaUnitFixture(
        await syntheticPrivatePNG(),
        pendingStore: store,
      );
      await x.controller.load();
      await x.controller.select();
      expect(x.requests, isEmpty);
      expect(x.picker.calls, 0);
      expect(x.controller.loaded, isFalse);
      expect(x.controller.recoveryBlocked, isTrue);
      expect(x.controller.canChangeLocal, isFalse);
      store.readFails = false;
      await x.controller.load();
      expect(x.requests.single.method, 'GET');
      expect(x.controller.loaded, isTrue);
      x.close();
    });
  });
  for (final phase in ['preview', 'save', 'delete']) {
    testWidgets('写前journal失败或等待时身份ABA退役零HTTP/$phase', (t) async {
      await t.runAsync(() async {
        for (final mode in ['storage', 'owner', 'token', 'org', 'source']) {
          final store = JournalSpy();
          final x = MediaUnitFixture(
            await syntheticPrivatePNG(),
            pendingStore: store,
          );
          if (phase == 'preview') {
            await x.local();
          } else {
            await x.preview();
            if (phase == 'delete') {
              await x.controller.saveReviewed(confirmed: true);
            }
          }
          final before = x.requests.length;
          if (mode == 'storage') {
            store.writeFails = true;
          } else {
            store.duringWrite = () {
              if (mode == 'owner') {
                x.credentials.owner = '44444444-4444-4444-8444-444444444444';
                x.credentials.event();
                x.credentials.owner = imageOwner;
              }
              if (mode == 'token') {
                x.credentials.actor('Bearer synthetic-b');
                x.credentials.actor('Bearer synthetic-a');
              }
              if (mode == 'org') {
                x.credentials.workspace = 'synthetic-org';
                x.credentials.event();
                x.credentials.workspace = null;
              }
              if (mode == 'source') {
                x.credentials.source = false;
                x.credentials.event();
                x.credentials.source = true;
              }
              x.credentials.event();
            };
          }
          if (phase == 'preview') await x.controller.prepare();
          if (phase == 'save') await x.controller.saveReviewed(confirmed: true);
          if (phase == 'delete') {
            await x.controller.remove(
              x.controller.images.single,
              confirmed: true,
            );
          }
          expect(x.requests.length, before, reason: '$phase/$mode old wire');
          if (mode == 'storage') {
            expect(x.controller.recoveryBlocked, isTrue);
          } else {
            expect(x.controller.retired, isTrue);
            expect(x.controller.localBytes, isNull);
          }
          x.close();
        }
      });
    });
  }
  testWidgets('确认回执后本机清理失败保持待核实且不能重复写', (t) async {
    await t.runAsync(() async {
      final store = JournalSpy();
      final x = MediaUnitFixture(
        await syntheticPrivatePNG(),
        pendingStore: store,
      );
      await x.preview();
      store.deleteFails = true;
      await x.controller.saveReviewed(confirmed: true);
      final count = x.requests.length;
      expect(x.controller.unknown, isTrue);
      expect(x.controller.canSave, isFalse);
      await x.controller.saveReviewed(confirmed: true);
      expect(x.requests.length, count);
      expect(x.controller.recoveryBlocked, isTrue);
      store.deleteFails = false;
      await x.controller.reconcile();
      expect(x.requests.last.method, 'GET');
      expect(x.controller.unknown, isFalse);
      expect(store.values, isEmpty);
      expect(x.controller.images.single.id, imageID);
      x.close();
    });
  });
  for (final phase in ['preview', 'save', 'delete']) {
    testWidgets('重开各phase仅GET且错误回执不作NO_EFFECT不恢复照片或批准/$phase', (t) async {
      await t.runAsync(() async {
        for (final code in [404, 401, 403, 409, 503]) {
          final store = MemoryPrivateImagePendingStore();
          final x = MediaUnitFixture(
            await syntheticPrivatePNG(),
            pendingStore: store,
          );
          await x.preview();
          if (phase == 'delete') {
            await x.controller.saveReviewed(confirmed: true);
          }
          final pending = pendingReference(x, phase);
          await store.write('https://birdtie.example', pending);
          reopenForRecovery(x, store);
          x.requests.clear();
          await x.controller.load();
          expect(x.requests, isEmpty);
          x.custom = (r) async => http.Response('', code);
          await x.controller.reconcile();
          expect(
            x.controller.unknown,
            isTrue,
            reason: '$phase/$code remains unknown',
          );
          expect(store.values, isNotEmpty);
          expect(x.requests.single.method, 'GET');
          expect(
            x.requests.single.url.path,
            endsWith('/private-image-operations/${pending.operationID}'),
          );
          expect(x.controller.requiresReopen, [401, 403, 409].contains(code));
          expect(x.controller.localBytes, isNull);
          expect(x.controller.localReviewed, isFalse);
          expect(x.controller.canSave, isFalse);
          x.close();
        }
      });
    });
  }
  for (final phase in ['preview', 'save', 'delete']) {
    testWidgets('重开原operation实际核实对应phase且不恢复照片确认/$phase', (t) async {
      await t.runAsync(() async {
        final store = MemoryPrivateImagePendingStore();
        final x = MediaUnitFixture(
          await syntheticPrivatePNG(),
          pendingStore: store,
        );
        await x.preview();
        if (phase != 'preview') {
          await x.controller.saveReviewed(confirmed: true);
        }
        final pending = pendingReference(x, phase);
        await store.write('https://birdtie.example', pending);
        if (phase == 'delete') {
          x.deleted = true;
          x.state = 'deleted';
        }
        final before = x.now;
        if (phase != 'preview') x.now = x.now.add(const Duration(minutes: 10));
        final row =
            x.receipt(
                status: phase == 'delete' ? 'deleted' : x.state,
                revision: phase == 'delete'
                    ? 3
                    : phase == 'save'
                    ? 2
                    : 1,
              )
              ..['previewExpiresAt'] = before
                  .add(const Duration(minutes: 5))
                  .toIso8601String()
              ..['retainUntil'] = before
                  .add(const Duration(days: 30))
                  .toIso8601String();
        if (phase != 'preview') {
          x.credentials.actor('Bearer synthetic-new-session');
        }
        reopenForRecovery(x, store);
        x.custom = (r) async => x.wire(row);
        x.requests.clear();
        await x.controller.load();
        expect(x.requests, isEmpty);
        await x.controller.reconcile();
        expect(x.requests.single.method, 'GET');
        expect(
          x.requests.single.headers['Authorization'],
          phase == 'preview'
              ? 'Bearer synthetic-a'
              : 'Bearer synthetic-new-session',
        );
        expect(x.controller.unknown, isFalse);
        expect(store.values, isEmpty);
        expect(x.controller.localBytes, isNull);
        expect(x.controller.localReviewed, isFalse);
        expect(x.controller.canSave, isFalse);
        if (phase == 'save') {
          expect(x.controller.images.single.momentRevision, 1);
        } else {
          expect(x.controller.images, isEmpty);
        }
        if (phase == 'preview') expect(x.controller.preview, isNull);
        x.close();
      });
    });
  }
  testWidgets('恢复GET具体绑定不符或预览实际过期保留journal禁止复用批准', (t) async {
    await t.runAsync(() async {
      for (final mismatch in [
        'owner',
        'moment',
        'operation',
        'asset',
        'revision',
        'mime',
        'size',
        'hash',
        'risk',
        'purpose',
        'expired',
      ]) {
        final store = MemoryPrivateImagePendingStore();
        final x = MediaUnitFixture(
          await syntheticPrivatePNG(),
          pendingStore: store,
        );
        await x.preview();
        final p = pendingReference(
          x,
          mismatch == 'expired' ? 'preview' : 'save',
        );
        await store.write('https://birdtie.example', p);
        final row = x.receipt();
        switch (mismatch) {
          case 'owner':
            row['ownerAccountId'] = '44444444-4444-4444-8444-444444444444';
          case 'moment':
            row['momentId'] = '44444444-4444-4444-8444-444444444444';
          case 'operation':
            row['operationId'] = '44444444-4444-4444-8444-444444444444';
          case 'asset':
            row['id'] = '44444444-4444-4444-8444-444444444444';
          case 'revision':
            row['momentRevision'] = 2;
          case 'mime':
            row['mimeType'] = 'image/jpeg';
          case 'size':
            row['byteSize'] = (row['byteSize'] as int) + 1;
          case 'hash':
            row['inputSha256'] = List.filled(64, 'a').join();
          case 'risk':
            row['pixelRisk'] = 'USER_MASKED';
          case 'purpose':
            row['purpose'] = 'MODEL';
          case 'expired':
            row['previewExpiresAt'] = x.now
                .subtract(const Duration(seconds: 1))
                .toIso8601String();
        }
        reopenForRecovery(x, store);
        x.requests.clear();
        x.custom = (r) async => x.wire(row);
        await x.controller.load();
        await x.controller.reconcile();
        expect(x.requests.single.method, 'GET');
        expect(x.controller.unknown, isTrue, reason: mismatch);
        expect(store.values, isNotEmpty);
        expect(x.controller.images, isEmpty);
        expect(x.controller.error, isNotNull);
        expect(x.controller.canSave, isFalse);
        x.close();
      }
    });
  });
  testWidgets('读取恢复记录或GET核实等待期间身份ABA不串用晚回执', (t) async {
    await t.runAsync(() async {
      for (final waiting in ['journal', 'wire']) {
        final store = JournalSpy();
        final x = MediaUnitFixture(
          await syntheticPrivatePNG(),
          pendingStore: store,
        );
        await x.preview();
        await x.controller.saveReviewed(confirmed: true);
        final p = pendingReference(x, 'save');
        await store.write('https://birdtie.example', p);
        reopenForRecovery(x, store);
        x.requests.clear();
        if (waiting == 'journal') {
          store.duringRead = () {
            x.credentials.actor('Bearer synthetic-b');
            x.credentials.actor('Bearer synthetic-a');
          };
        }
        await x.controller.load();
        if (waiting == 'wire') {
          x.custom = (r) async {
            x.credentials.actor('Bearer synthetic-b');
            x.credentials.actor('Bearer synthetic-a');
            return x.route(r);
          };
          await x.controller.reconcile();
          expect(x.requests.single.method, 'GET');
        } else {
          expect(x.requests, isEmpty);
        }
        expect(x.controller.retired, isTrue);
        expect(x.controller.images, isEmpty);
        expect(x.controller.localBytes, isNull);
        expect(store.values, isNotEmpty);
        x.close();
      }
    });
  });
  testWidgets('三类真实写响应丢失关闭重开保持写前journal并只GET核实', (t) async {
    await t.runAsync(() async {
      for (final phase in ['preview', 'save', 'delete']) {
        final store = MemoryPrivateImagePendingStore();
        final x = MediaUnitFixture(
          await syntheticPrivatePNG(),
          pendingStore: store,
        );
        if (phase == 'preview') {
          await x.local();
        } else {
          await x.preview();
          if (phase == 'delete') {
            await x.controller.saveReviewed(confirmed: true);
          }
        }
        x.custom = (r) async {
          if (r.method ==
              {'preview': 'POST', 'save': 'PUT', 'delete': 'DELETE'}[phase]) {
            x.route(r);
            return http.Response('', 503);
          }
          return x.route(r);
        };
        if (phase == 'preview') await x.controller.prepare();
        if (phase == 'save') await x.controller.saveReviewed(confirmed: true);
        if (phase == 'delete') {
          await x.controller.remove(
            x.controller.images.single,
            confirmed: true,
          );
        }
        expect(x.controller.unknown, isTrue);
        expect(store.values.values.single.phase, phase);
        final op = store.values.values.single.operationID;
        reopenForRecovery(x, store);
        x.requests.clear();
        await x.controller.load();
        expect(x.requests, isEmpty);
        await x.controller.reconcile();
        expect(x.requests.single.method, 'GET');
        expect(
          x.requests.single.url.path,
          endsWith('/private-image-operations/$op'),
        );
        expect(x.controller.unknown, isFalse);
        expect(store.values, isEmpty);
        expect(x.controller.localBytes, isNull);
        expect(x.controller.localReviewed, isFalse);
        expect(x.controller.canSave, isFalse);
        x.close();
      }
    });
  });
  testWidgets('重开原操作GET网络失败不丢journal不自动重发', (t) async {
    await t.runAsync(() async {
      final store = MemoryPrivateImagePendingStore();
      final x = MediaUnitFixture(
        await syntheticPrivatePNG(),
        pendingStore: store,
      );
      await x.preview();
      await store.write('https://birdtie.example', pendingReference(x, 'save'));
      reopenForRecovery(x, store);
      x.requests.clear();
      await x.controller.load();
      x.custom = (r) async =>
          throw http.ClientException('synthetic-network-failure');
      await x.controller.reconcile();
      expect(x.requests.single.method, 'GET');
      expect(x.controller.unknown, isTrue);
      expect(store.values, isNotEmpty);
      expect(x.controller.error, isNotNull);
      expect(x.controller.canSave, isFalse);
      x.close();
    });
  });
  testWidgets('关闭重开未知保存必须保留原operation并仅GET核实不能借列表当结果', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    await t.runAsync(() async {
      final x = MediaUnitFixture(
        await syntheticPrivatePNG(),
        pendingStore: const SecurePrivateImagePendingStore(),
      );
      await x.preview();
      x.custom = (r) async {
        if (r.method == 'PUT') {
          x.route(r);
          return http.Response('', 503);
        }
        return x.route(r);
      };
      await x.controller.saveReviewed(confirmed: true);
      expect(x.controller.unknown, isTrue);
      final operation = x.metadata!['operationId'];
      x.controller.dispose();
      x.controller = PrivateMomentMediaController(
        momentID: imageMoment,
        momentRevision:
            2, // Current private text edit is not old upload approval.
        authorizationHeader: () => x.credentials.token,
        ownerID: () => x.credentials.owner,
        identityChanges: x.credentials,
        workspaceChanges: x.credentials,
        organizationWorkspaceID: () => x.credentials.workspace,
        sourceCurrent: () => x.credentials.source,
        apiBaseUrl: 'https://birdtie.example',
        client: x.client,
        clock: () => x.now,
      );
      x.requests.clear();
      await x.controller.load();
      expect(
        x.controller.unknown,
        isTrue,
        reason: 'closing cannot forget the original unknown operation',
      );
      expect(
        x.requests,
        isEmpty,
        reason: 'current list cannot substitute for exact operation recovery',
      );
      await x.controller.reconcile();
      expect(x.requests.single.method, 'GET');
      expect(
        x.requests.single.url.path,
        endsWith('/private-image-operations/$operation'),
      );
      expect(x.controller.images.single.id, imageID);
      expect(x.controller.unknown, isFalse);
      expect(x.controller.localBytes, isNull);
      expect(x.controller.localReviewed, isFalse);
      expect(x.controller.canSave, isFalse);
      x.close();
    });
  });
  for (final action in ['load', 'delete', 'select']) {
    testWidgets('忙碌同步通知中身份或source变化时零旧HTTP或gallery副作用/$action', (t) async {
      await t.runAsync(() async {
        for (final change in ['owner', 'token', 'org', 'source']) {
          final x = MediaUnitFixture(await syntheticPrivatePNG());
          if (action == 'delete') {
            await x.preview();
            await x.controller.saveReviewed(confirmed: true);
          }
          final requestCount = x.requests.length, pickerCount = x.picker.calls;
          x.controller.addListener(() {
            if (!x.controller.busy) return;
            if (change == 'owner') {
              x.credentials.owner = '44444444-4444-4444-8444-444444444444';
            }
            if (change == 'token') x.credentials.token = 'Bearer synthetic-b';
            if (change == 'org') {
              x.credentials.workspace = 'organization-synthetic';
            }
            if (change == 'source') x.credentials.source = false;
          });
          if (action == 'load') await x.controller.load();
          if (action == 'delete') {
            await x.controller.remove(
              x.controller.images.single,
              confirmed: true,
            );
          }
          if (action == 'select') await x.controller.select();
          expect(
            x.requests.length,
            requestCount,
            reason: '$action/$change must not send stale request',
          );
          expect(
            x.picker.calls,
            pickerCount,
            reason: '$action/$change must not open stale gallery',
          );
          expect(x.controller.retired, isTrue);
          x.close();
        }
      });
    });
  }
  testWidgets('未知保存GET回执必须匹配已经明确确认的预览ID', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.preview();
      x.custom = (r) async {
        if (r.method == 'PUT') {
          x.route(r);
          return http.Response('', 503);
        }
        final row = x.receipt(revision: 2)
          ..['id'] = '44444444-4444-4444-8444-444444444444';
        return x.wire(row);
      };
      await x.controller.saveReviewed(confirmed: true);
      await x.controller.reconcile();
      expect(x.controller.unknown, isTrue);
      expect(x.controller.images, isEmpty);
      expect(x.requests.where((r) => r.method == 'PUT').length, 1);
      x.close();
    });
  });
  testWidgets('本人衍生图读路径接受10至16MiB结构有界头且拒绝超限', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.preview();
      // Header-only structural fixture; this does not claim real pixel decoding.
      final head = headerPNG();
      final large = Uint8List(momentImageMaxBytes + 128);
      large.setRange(0, 33, head);
      ByteData.sublistView(large).setUint32(33, large.length - 57);
      large.setRange(37, 41, 'IDAT'.codeUnits);
      large.setRange(
        large.length - 12,
        large.length,
        head.sublist(head.length - 12),
      );
      x.stored = large;
      x.state = 'ready_private';
      await x.controller.load();
      await x.controller.read(x.controller.images.single);
      expect(x.controller.displayedBytes?.length, large.length);
      x.stored = Uint8List(16 * 1024 * 1024 + 1);
      x.controller.displayedBytes = null;
      await x.controller.load();
      await x.controller.read(x.controller.images.single);
      expect(x.controller.displayedBytes, isNull);
      expect(x.controller.error, isNotNull);
      x.close();
    });
  });
  testWidgets('保存401403409必须重新打开当前记录不能沿用旧确认重复PUT', (t) async {
    await t.runAsync(() async {
      for (final code in [401, 403, 409]) {
        final x = MediaUnitFixture(await syntheticPrivatePNG());
        await x.preview();
        x.custom = (r) async =>
            r.method == 'PUT' ? http.Response('', code) : x.route(r);
        await x.controller.saveReviewed(confirmed: true);
        expect(x.controller.canSave, isFalse);
        await x.controller.saveReviewed(confirmed: true);
        expect(x.requests.where((r) => r.method == 'PUT').length, 1);
        x.close();
      }
    });
  });
  testWidgets('提交408不明确状态仅GET核实不重复提交', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.preview();
      x.custom = (r) async =>
          r.method == 'PUT' ? http.Response('', 408) : x.route(r);
      await x.controller.saveReviewed(confirmed: true);
      expect(x.controller.unknown, isTrue);
      await x.controller.saveReviewed(confirmed: true);
      expect(x.requests.where((r) => r.method == 'PUT').length, 1);
      x.close();
    });
  });
  testWidgets('图片先实际本机重编码及确认再POST PUT，正常本人读删除', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.controller.load();
      await x.local();
      expect(x.requests.map((r) => r.method), ['GET']);
      expect(x.controller.localBytes, isNotNull);
      expect(x.controller.pixelRisk, 'UNKNOWN');
      await x.controller.prepare();
      await x.controller.saveReviewed(confirmed: false);
      expect(x.requests.where((r) => r.method == 'PUT'), isEmpty);
      await x.controller.saveReviewed(confirmed: true);
      expect(x.controller.images.length, 1);
      expect(x.controller.message, contains('未发送给 AI'));
      final image = x.controller.images.single;
      await x.controller.read(image);
      expect(x.controller.displayedBytes, x.stored);
      await x.controller.remove(image, confirmed: false);
      expect(x.requests.where((r) => r.method == 'DELETE'), isEmpty);
      await x.controller.remove(image, confirmed: true);
      expect(x.controller.images, isEmpty);
      expect(x.deleted, isTrue);
      expect(
        x.requests
            .where((r) => r.method == 'PUT')
            .single
            .headers['X-Birdtie-Private-Image-Confirmation'],
        imageID,
      );
      expect(x.metadata!.keys.toSet(), {
        'operationId',
        'momentRevision',
        'mimeType',
        'byteSize',
        'sha256',
        'pixelRisk',
        'purpose',
      });
      x.close();
    });
  });
  testWidgets('本地实色遮挡改变真实像素而不宣称SAFE或调用API', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.local();
      final before = x.controller.localBytes;
      await x.controller.mask(const ui.Rect.fromLTWH(0, 0, .5, .5));
      expect(x.controller.pixelRisk, 'USER_MASKED');
      expect(x.controller.localReviewed, isFalse);
      expect(x.requests, isEmpty);
      expect(x.controller.localBytes, isNot(before));
      final codec = await ui.instantiateImageCodec(x.controller.localBytes!);
      final frame = await codec.getNextFrame();
      codec.dispose();
      final raw = await frame.image.toByteData(
        format: ui.ImageByteFormat.rawRgba,
      );
      expect(raw!.buffer.asUint8List().sublist(0, 4), [0, 0, 0, 255]);
      expect(raw.buffer.asUint8List().sublist(60, 64), [255, 0, 0, 255]);
      frame.image.dispose();
      x.close();
    });
  });
  testWidgets('取消选择与不处理均零上传且不覆盖已有本机图片', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.local();
      final b = x.controller.localBytes;
      x.picker.canceled = true;
      await x.controller.select();
      expect(x.controller.localBytes, b);
      x.controller.discard();
      expect(x.controller.localBytes, isNull);
      expect(x.requests, isEmpty);
      x.close();
    });
  });
  testWidgets('选择器同帧账号和工作身份ABA晚回永久退休', (t) async {
    await t.runAsync(() async {
      for (final workspace in [false, true]) {
        final x = MediaUnitFixture(await syntheticPrivatePNG());
        x.picker.pending = Completer();
        final f = x.controller.select();
        if (workspace) {
          x.credentials.workspace = 'org';
          x.credentials.event();
          x.credentials.workspace = null;
          x.credentials.event();
        } else {
          x.credentials.actor('Bearer synthetic-b');
          x.credentials.actor('Bearer synthetic-a');
        }
        x.picker.pending!.complete(MomentImageSelection(x.picker.bytes));
        await f;
        expect(x.controller.retired, isTrue);
        expect(x.controller.localBytes, isNull);
        expect(x.requests, isEmpty);
        x.close();
      }
    });
  });
  testWidgets('昵称通知保留本人预览，token ABA与source变化拒绝旧保存', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.preview();
      x.credentials.event();
      expect(x.controller.retired, isFalse);
      expect(x.controller.canSave, isTrue);
      x.credentials.actor('Bearer synthetic-b');
      x.credentials.actor('Bearer synthetic-a');
      await x.controller.saveReviewed(confirmed: true);
      expect(x.requests.where((r) => r.method == 'PUT'), isEmpty);
      expect(x.controller.localBytes, isNull);
      x.close();
      final y = MediaUnitFixture(await syntheticPrivatePNG());
      await y.preview();
      y.credentials.source = false;
      y.credentials.event();
      await y.controller.saveReviewed(confirmed: true);
      expect(y.requests.where((r) => r.method == 'PUT'), isEmpty);
      y.close();
    });
  });
  testWidgets('丢失minted预览ID只按本人operationGET核实，未重POST', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      x.custom = (r) async {
        final v = x.route(r);
        return r.method == 'POST' ? http.Response('{broken', 201) : v;
      };
      await x.preview();
      expect(x.controller.unknown, isTrue);
      expect(x.controller.preview, isNull);
      await x.controller.prepare();
      expect(x.requests.where((r) => r.method == 'POST').length, 1);
      await x.controller.reconcile();
      expect(x.controller.unknown, isFalse);
      expect(x.controller.preview?.status, 'preview');
      expect(
        x.requests.last.url.path,
        endsWith('/private-image-operations/${x.metadata!['operationId']}'),
      );
      await x.controller.saveReviewed(confirmed: true);
      expect(x.controller.images.length, 1);
      x.close();
    });
  });
  testWidgets('503未知预览与late HTTP不能污染新身份，未知保存仅GET对账', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.preview();
      var fail = true;
      x.custom = (r) async {
        if (r.method == 'PUT') {
          if (fail) {
            fail = false;
            throw http.ClientException('synthetic connection lost');
          }
        }
        return x.route(r);
      };
      await x.controller.saveReviewed(confirmed: true);
      expect(x.controller.unknown, isTrue);
      await x.controller.saveReviewed(confirmed: true);
      await x.controller.reconcile();
      expect(x.controller.unknown, isTrue);
      expect(x.requests.where((r) => r.method == 'PUT').length, 1);
      x.stored = x.controller.localBytes;
      x.state = 'ready_private';
      await x.controller.reconcile();
      expect(x.controller.unknown, isFalse);
      expect(x.controller.images.length, 1);
      expect(x.requests.where((r) => r.method == 'PUT').length, 1);
      x.close();
      final y = MediaUnitFixture(await syntheticPrivatePNG());
      final wait = Completer<http.Response>();
      y.custom = (r) async => wait.future;
      final f = y.controller.load();
      y.credentials.actor('Bearer synthetic-b');
      wait.complete(y.wire([], 503));
      await f;
      expect(y.controller.images, isEmpty);
      expect(y.controller.retired, isTrue);
      expect(y.controller.error, contains('身份'));
      y.close();
    });
  });
  testWidgets('UNKNOWN DELETE通过真实墓碑metadata核实且不重复删除', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.preview();
      await x.controller.saveReviewed(confirmed: true);
      final image = x.controller.images.single;
      x.custom = (r) async {
        final v = x.route(r);
        if (r.method == 'DELETE') {
          throw http.ClientException('synthetic ack lost');
        }
        return v;
      };
      await x.controller.remove(image, confirmed: true);
      expect(x.controller.unknown, isTrue);
      await x.controller.remove(image, confirmed: true);
      await x.controller.reconcile();
      expect(x.controller.images, isEmpty);
      expect(x.controller.unknown, isFalse);
      expect(x.requests.where((r) => r.method == 'DELETE').length, 1);
      x.close();
    });
  });
  testWidgets('列表权限与不可用不是空成功，错owner回执拒绝，late读无污染', (t) async {
    await t.runAsync(() async {
      for (final code in [401, 403, 503]) {
        final x = MediaUnitFixture(await syntheticPrivatePNG());
        x.custom = (r) async => http.Response('', code);
        await x.controller.load();
        expect(x.controller.loaded, isFalse);
        expect(x.controller.error, isNotNull);
        expect(x.controller.images, isEmpty);
        x.close();
      }
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      x.custom = (r) async {
        x.route(r);
        return x.wire(
          x.receipt(owner: '44444444-4444-4444-8444-444444444444'),
          201,
        );
      };
      await x.preview();
      expect(x.controller.unknown, isTrue);
      expect(x.controller.preview, isNull);
      x.close();
      final late = MediaUnitFixture(await syntheticPrivatePNG());
      await late.preview();
      await late.controller.saveReviewed(confirmed: true);
      final pending = Completer<http.Response>();
      late.custom = (r) async => pending.future;
      final reading = late.controller.read(late.controller.images.single);
      late.credentials.actor('Bearer synthetic-b');
      pending.complete(
        http.Response.bytes(
          late.stored!,
          200,
          headers: {'content-type': 'image/png'},
        ),
      );
      await reading;
      expect(late.controller.displayedBytes, isNull);
      expect(late.controller.images, isEmpty);
      expect(late.controller.retired, isTrue);
      late.close();
    });
  });
  testWidgets('过期确认与已更换列表版本不接受旧删除批准', (t) async {
    await t.runAsync(() async {
      final expired = MediaUnitFixture(await syntheticPrivatePNG());
      await expired.preview();
      expired.now = expired.now.add(const Duration(minutes: 6));
      expect(expired.controller.canSave, isFalse);
      await expired.controller.saveReviewed(confirmed: true);
      expect(expired.requests.where((r) => r.method == 'PUT'), isEmpty);
      expired.close();
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      await x.preview();
      await x.controller.saveReviewed(confirmed: true);
      final old = x.controller.images.single;
      x.controller.images = [
        PrivateMomentImageReceipt.fromJson(x.receipt(revision: 3)),
      ];
      await x.controller.remove(old, confirmed: true);
      expect(x.requests.where((r) => r.method == 'DELETE'), isEmpty);
      x.close();
    });
  });
  testWidgets('dispose与初始匿名不得恢复私人图片，不关闭借用transport', (t) async {
    await t.runAsync(() async {
      final x = MediaUnitFixture(await syntheticPrivatePNG());
      x.picker.pending = Completer();
      final f = x.controller.select();
      x.controller.dispose();
      x.picker.pending!.complete(MomentImageSelection(x.picker.bytes));
      await f;
      expect(x.controller.localBytes, isNull);
      expect(x.requests, isEmpty);
      x.custom = (r) async => http.Response('', 200);
      expect(
        (await x.client.get(
          Uri.parse('https://birdtie.example/borrowed'),
        )).statusCode,
        200,
      );
      final requestCount = x.requests.length;
      final anonymous = PrivateMomentMediaController(
        momentID: imageMoment,
        momentRevision: 1,
        authorizationHeader: () => null,
        ownerID: () => null,
        identityChanges: x.credentials,
        sourceCurrent: () => true,
        apiBaseUrl: 'https://birdtie.example',
        client: x.client,
        picker: x.picker,
      );
      await anonymous.select();
      await anonymous.load();
      expect(anonymous.retired, isTrue);
      expect(anonymous.localBytes, isNull);
      expect(x.requests.length, requestCount);
      anonymous.dispose();
      x.credentials.dispose();
      x.client.close();
    });
  });
}
