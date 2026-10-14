import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'moment_control_test.dart';

http.Response mcJSON(Object? data, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);
Map<String, Object> mcRecord({int revision = 1}) => {
  'id': mcTarget,
  'authorAccountId': mcOwner,
  'visibility': 'private',
  'cityId': 'aberdeen-gb',
  'title': '本人私人原文',
  'body': '本人正文',
  'timePrecision': 'unknown',
  'locationPrecision': 'city',
  'status': 'draft',
  'revision': revision,
};

class MomentFixture {
  final identity = ValueNotifier<(String?, String?)>((
    'Bearer synthetic',
    mcOwner,
  ));
  final MemoryMomentControlPendingStore store;
  MomentFixture({MemoryMomentControlPendingStore? pendingStore})
    : store = pendingStore ?? MemoryMomentControlPendingStore();
  int puts = 0,
      previews = 0,
      gets = 0,
      stops = 0,
      rawWrites = 0,
      listStatus = 200,
      previewStatus = 200,
      putStatus = 200;
  bool lost = false, missing = false;
  Map<String, Object?> record = mcRecord();
  final inputs = <String, MomentControlInput>{};
  final unexpectedRequests = <String>[];
  Completer<http.Response>? heldPreview, heldPUT, heldList;
  late final client = MockClient((r) async {
    expectSync(
      r.headers.containsKey('X-Birdtie-Organization-Workspace'),
      isFalse,
    );
    if (r.url.path == '/v1/me')
      return mcJSON({'id': identity.value.$2, 'accountType': 'person'});
    if (r.method == 'GET' && r.url.path == '/v1/me/intents') return mcJSON([]);
    if (r.method == 'GET' &&
        r.url.path == '/v1/accounts/${identity.value.$2}/profile') {
      return mcJSON({
        'accountId': identity.value.$2,
        'displayName': '隔离合成本人',
        'bio': '仅用于现有资料读取fixture',
        'visibility': 'private',
      });
    }
    if (r.url.path == '/v1/me/moments' && r.method == 'GET') {
      final held = heldList;
      heldList = null;
      return held?.future ?? Future.value(mcJSON([record], listStatus));
    }
    if (r.url.path == '/v1/me/moment-controls/previews') {
      previews++;
      if (previewStatus != 200) return mcJSON(null, previewStatus);
      final m = jsonDecode(r.body) as Map<String, dynamic>;
      final input = MomentControlInput(
        operationID: m['operationId'],
        action: m['action'],
        momentID: m['momentId'] ?? '',
        expectedRevision: m['expectedRevision'],
        input: m['input'] == null
            ? null
            : Map<String, Object?>.from(m['input']),
      );
      inputs[input.operationID] = input;
      return heldPreview?.future ??
          Future.value(
            mcJSON(
              mcPreview(
                input,
                preserved:
                    input.input?['placeId'] == mcTarget &&
                    input.input?['locationPrecision'] == 'city',
              ),
            ),
          );
    }
    if (r.url.path.startsWith('/v1/me/moment-controls/operations/')) {
      final stop = r.url.path.endsWith('/stop'), op = r.url.path.split('/')[5];
      final input = inputs[op];
      if (r.method == 'PUT') {
        puts++;
        expectSync(
          jsonDecode(r.body)['approval'],
          'opaque-actual-synthetic-token',
        );
        expectSync(
          (await store.read(
            'https://isolated.invalid',
            identity.value.$2!,
          )).single.stage,
          'UNKNOWN',
        );
        if (heldPUT != null) return heldPUT!.future;
        if (lost) throw http.ClientException('synthetic lost');
        if (putStatus != 200) return mcJSON(null, putStatus);
        if (input!.action != 'WITHDRAW') {
          record = {
            ...record,
            ...input.input!,
            'revision': input.expectedRevision + 1,
          };
          if (input.input!['timePrecision'] == 'unknown')
            record.remove('occurredAt');
          if (input.input!.containsKey('activityId'))
            record['activityIds'] = input.input!['activityId'] == ''
                ? <String>[]
                : [input.input!['activityId'] as String];
        }
        return mcJSON(mcReceipt(input));
      }
      if (stop) {
        stops++;
        expectSync(r.body, isEmpty);
        return input == null
            ? mcJSON({
                'operationId': op,
                'status': 'STOPPED',
                'appliedRevision': 0,
              })
            : mcJSON(mcReceipt(input, stop: true));
      }
      gets++;
      if (missing) return mcJSON(null, 404);
      return mcJSON(mcReceipt(input!));
    }
    unexpectedRequests.add('${r.method} ${r.url.path}');
    if (r.method != 'GET') rawWrites++;
    throw StateError('Forbidden ${r.method} ${r.url}');
  });
  late final controller = PrivateMomentController(
    authorizationHeader: () => identity.value.$1,
    ownerID: () => identity.value.$2,
    identityChanges: identity,
    client: client,
    apiBaseUrl: 'https://isolated.invalid',
    pendingStore: store,
  );
  Future<MomentControlPreview> prepare() async => (await controller
      .previewCreate(cityID: 'aberdeen-gb', title: '本人中文正文', body: '只是私人草稿'))!;
  void dispose() {
    controller.dispose();
    identity.dispose();
    client.close();
  }
}

class RemovedPreparedStore extends MemoryMomentControlPendingStore {
  @override
  Future<void> write(
    String origin,
    String owner,
    MomentControlPending p,
  ) async {
    await super.write(origin, owner, p);
    if (p.stage == 'PREPARED') await super.remove(origin, owner, p);
  }
}

class FailedReadbackStore extends MemoryMomentControlPendingStore {
  int reads = 0;
  @override
  Future<List<MomentControlPending>> read(String origin, String owner) async {
    if (++reads == 2) throw StateError('isolated durable readback failed');
    return super.read(origin, owner);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'renewal observed first by an editor getter still refreshes the authoritative owned version',
    () async {
      final f = MomentFixture();
      addTearDown(f.dispose);
      f.identity.addListener(() {
        f.controller.identityEpoch;
      });
      await f.controller.refresh();
      final p = await f.prepare();
      f.identity.value = ('Bearer renewed', mcOwner);
      for (var i = 0; i < 4; i++) {
        await Future<void>.delayed(Duration.zero);
      }
      expect(f.controller.moments.single.authorAccountID, mcOwner);
      expect(f.controller.canStartOperation, isTrue);
      expect(f.controller.previewCurrent(p), isFalse);
      final fresh = await f.controller.previewUpdate(
        moment: f.controller.moments.single,
        title: '本人续期后文字',
        body: '未丢失',
        placeID: '',
      );
      expect(fresh, isNotNull);
      expect(f.puts, 0);
    },
  );
  test(
    'preview has no effect, raw SDK cannot bypass approval, exact PUT only once',
    () async {
      final f = MomentFixture();
      addTearDown(f.dispose);
      expect(
        await f.controller.create(
          cityID: 'aberdeen-gb',
          title: 'raw',
          body: '',
        ),
        isFalse,
      );
      expect(f.rawWrites, 0);
      final p = await f.prepare();
      expect(f.puts, 0);
      expect(await f.controller.commit(p), isTrue);
      expect(await f.controller.commit(p), isFalse);
      expect(f.puts, 1);
      expect(f.store.values, isEmpty);
      expect(f.controller.operationReceipt!.succeeded, isTrue);
    },
  );
  test(
    'unknown restart GET404 cannot replay; bodyless STOP settles original op',
    () async {
      final f = MomentFixture()..lost = true;
      addTearDown(f.dispose);
      final p = await f.prepare();
      expect(await f.controller.commit(p), isFalse);
      final pending = f.store.values.values.single;
      f.missing = true;
      await f.controller.recoverOperation(pending);
      expect(f.controller.operationError, contains('不等于'));
      expect(
        await f.controller.previewCreate(
          cityID: 'aberdeen-gb',
          title: 'new',
          body: '',
        ),
        isNull,
      );
      expect(f.puts, 1);
      await f.controller.recoverOperation(pending, stop: true);
      expect(f.stops, 1);
      expect(f.store.values, isEmpty);
      expect(f.controller.operationReceipt!.status, 'STOPPED');
    },
  );
  test(
    'success current list403 preserves original tiny fact and clears exact journal',
    () async {
      final f = MomentFixture();
      addTearDown(f.dispose);
      final p = await f.prepare();
      f.listStatus = 403;
      expect(await f.controller.commit(p), isTrue);
      expect(f.controller.moments, isEmpty);
      expect(f.controller.operationReceipt!.succeeded, isTrue);
      expect(f.store.values, isEmpty);
      expect(f.puts, 1);
    },
  );
  test(
    'same owner renewal retires approval but preserves original recovery record',
    () async {
      final f = MomentFixture()..lost = true;
      addTearDown(f.dispose);
      final p = await f.prepare();
      await f.controller.commit(p);
      final old = f.store.values.values.single;
      f.identity.value = ('Bearer renewed', mcOwner);
      await Future<void>.delayed(Duration.zero);
      await f.controller.loadOperations();
      expect(
        f.controller.pendingOperations.single.operationID,
        old.operationID,
      );
      expect(f.puts, 1);
      expect(await f.controller.commit(p), isFalse);
    },
  );
  test('actor ABA and late preview cannot restore old approval', () async {
    final f = MomentFixture();
    addTearDown(f.dispose);
    f.heldPreview = Completer<http.Response>();
    final future = f.controller.previewCreate(
      cityID: 'aberdeen-gb',
      title: 'old',
      body: '',
    );
    while (f.inputs.isEmpty) {
      await Future<void>.delayed(Duration.zero);
    }
    final input = f.inputs.values.single;
    f.identity.value = ('Bearer B', mcOp);
    f.identity.value = ('Bearer A-again', mcOwner);
    f.heldPreview!.complete(mcJSON(mcPreview(input)));
    expect(await future, isNull);
    expect(f.controller.operationPreview, isNull);
    expect(f.puts, 0);
  });
  test('removed PREPARED cannot turn into dispatched UNKNOWN', () async {
    final f = MomentFixture(pendingStore: RemovedPreparedStore());
    addTearDown(f.dispose);
    final p = await f.prepare();
    expect(await f.controller.commit(p), isFalse);
    expect(f.puts, 0);
    expect(f.store.values, isEmpty);
    expect(f.controller.operationError, contains('尚未提交'));
  });
  test(
    'failed PREPARED readback is a local unsent preparation, never a fake receipt404 loop',
    () async {
      final f = MomentFixture(pendingStore: FailedReadbackStore());
      addTearDown(f.dispose);
      final p = await f.prepare();
      expect(await f.controller.commit(p), isFalse);
      expect(f.puts, 0);
      final original = f.store.values.values.single;
      expect(original.stage, 'PREPARED');
      await f.controller.repairPrepared(original);
      expect(f.store.values, isEmpty);
      expect(f.gets, 0);
      expect(f.puts, 0);
    },
  );
}
