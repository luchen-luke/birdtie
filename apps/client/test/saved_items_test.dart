import 'dart:async';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'entity_action_contract_test.dart' show actionWire;
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'community_avatar_test.dart' show communityAvatarMetadata;
import 'person_avatar_fixture.dart' show avatarOwner, avatarPeer;

void main() {
  test('communityProjection 收藏原group目标匹配且备注保留受控头像，隐源不带metadata', () {
    final raw = {
      ...savedTestRow(savedNoteID, 'group', avatarOwner),
      'communityAvatar': communityAvatarMetadata(),
    };
    final item = SavedItem.fromJson(raw);
    expect(item.communityAvatar?.communityID, avatarOwner);
    final next = item.withNote('本人完整备注', 3);
    expect(next.communityAvatar?.same(item.communityAvatar!), true);
    expect(next.targetId, avatarOwner);
    for (final invalid in [
      {...raw, 'targetId': avatarPeer},
      {...raw, 'kind': 'place'},
      {...raw, 'available': false},
      {
        ...raw,
        'communityAvatar': {
          ...communityAvatarMetadata(),
          'thumbnailPath': 'https://legacy.invalid/external.jpg',
        },
      },
    ]) {
      expect(() => SavedItem.fromJson(invalid), throwsFormatException);
    }
    expect(
      SavedItem.fromJson({
        ...savedTestRow(savedNoteID, 'group', avatarOwner),
        'avatarUrl': 'https://legacy.invalid/unused.jpg',
      }).communityAvatar,
      isNull,
    );
  });
  test('准备写入被另实例UNKNOWN抢先占用，本机检查不能删除其已发送selector', () async {
    final store = _RacingNoteStore();
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.url.path.endsWith('/note'))
        return _savedJSON({'savedId': savedNoteID, 'note': '', 'revision': 0});
      if (r.url.path.contains('/operations/')) return http.Response('', 404);
      return _savedJSON({
        'items': [savedTestRow(savedNoteID, 'place', savedTargetID)],
        'nextCursor': null,
      });
    });
    final c = savedNoteController(client, store);
    await c.load(paged: true);
    final item = c.items.single;
    await c.readNote(item);
    c.setNoteDraft(item.id, '本实例尚未发送的草稿');
    expect(await c.saveNote(item), false);
    expect(c.noteState(item.id)!.notSubmitted, true);
    await c.readNote(item, refresh: true);
    expect(store.values.values.single.operationID, savedOperationID);
    expect(c.noteState(item.id)!.pending!.operationID, savedOperationID);
    expect(c.noteState(item.id)!.uncertain, true);
    expect(c.noteState(item.id)!.draft, '本实例尚未发送的草稿');
    expect(await c.recoverNote(item), false);
    expect(
      requests.last.url.path.endsWith('/operations/$savedOperationID'),
      true,
    );
    expect(requests.where((r) => r.method != 'GET'), isEmpty);
    expect(store.values, hasLength(1));
    c.dispose();
    client.close();
  });
  test('已知第21项不被首批分页丢失，权威UNSAVE未知收藏读原target再移除，零SAVE', () async {
    final wire = actionWire(ref: const EntityActionRef('place', savedTargetID));
    for (final dynamic action in wire['actions'] as List) {
      if (action['kind'] == 'JOIN') action['state'] = 'UNAVAILABLE';
    }
    (wire['actions'] as List).firstWhere(
      (a) => a['kind'] == 'SAVE',
    )['operation'] = 'UNSAVE';
    final view = EntityActionView.decode(
      wire,
      const EntityActionRef('place', savedTargetID),
    );
    final target = savedTestRow(savedNoteID, 'place', savedTargetID);
    final first = List.generate(
      20,
      (i) => savedTestRow('known-$i', 'place', 'target-$i'),
    );
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.method == 'DELETE') return http.Response('', 204);
      if (r.url.path.contains('/target/')) return _savedJSON(target);
      if (r.url.path.endsWith('/page'))
        return _savedJSON({'items': first, 'nextCursor': savedNoteID});
      return _savedJSON([...first, target]);
    });
    final c = SavedController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://saved.test',
    );
    await c.load();
    await c.load(paged: true);
    expect(c.items, hasLength(20));
    expect(c.contains('place', savedTargetID), true);
    await c.toggle(
      'place',
      savedTargetID,
      approved: view.action(EntityActionKind.save).reviewed(view),
    );
    expect(requests.where((r) => r.url.path.contains('/target/')), isEmpty);
    c.clear();
    await c.load(paged: true);
    expect(c.contains('place', savedTargetID), false);
    await c.toggle(
      'place',
      savedTargetID,
      approved: view.action(EntityActionKind.save).reviewed(view),
    );
    expect(
      requests.where((r) => r.url.path.contains('/target/')),
      hasLength(1),
    );
    expect(
      requests
          .where((r) => r.method == 'DELETE')
          .map((r) => r.url.path)
          .toList(),
      ['/v1/me/saved/$savedNoteID', '/v1/me/saved/$savedNoteID'],
    );
    expect(requests.where((r) => r.method == 'POST'), isEmpty);
    c.dispose();
    client.close();
  });
  test('409失效分页退休原cursor，保留列表并可明确重读首批', () async {
    var initial = true;
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.url.queryParameters['cursor'] != null)
        return http.Response('{"error":{"code":"saved_page_changed"}}', 409);
      return _savedJSON({
        'items': [
          savedTestRow(initial ? 'first' : 'new', 'place', savedTargetID),
        ],
        'nextCursor': initial ? savedNoteID : null,
      });
    });
    final c = SavedController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://saved.test',
    );
    await c.load(paged: true);
    await c.load(more: true);
    expect(c.pageChanged, true);
    expect(c.hasMore, false);
    expect(c.items.single.id, 'first');
    await c.load(more: true);
    expect(requests, hasLength(2));
    initial = false;
    await c.load();
    expect(c.pageChanged, false);
    expect(c.items.single.id, 'new');
    expect(requests.last.url.queryParameters, isEmpty);
    c.dispose();
    client.close();
  });
  test('secure journal只存selector，分隔owner/origin，跨实例挂起读写串行不覆盖未知op', () async {
    final storage = _HangingSecureStorage();
    final a = SecureSavedNotePendingStore(storage: storage),
        b = SecureSavedNotePendingStore(storage: storage);
    const old = SavedNotePending(
      savedID: savedNoteID,
      operationID: savedOperationID,
      expectedRevision: 2,
    );
    const newer = SavedNotePending(
      savedID: savedNoteID,
      operationID: savedTargetID,
      expectedRevision: 3,
    );
    final hold = Completer<void>();
    storage.holdNextRead = hold;
    final oldWrite = a.write('https://a.test', savedOwnerID, old);
    final newWrite = b.write('https://a.test', savedOwnerID, newer);
    final refused = expectLater(newWrite, throwsFormatException);
    await Future<void>.delayed(Duration.zero);
    expect(storage.writes, 0);
    hold.complete();
    await oldWrite;
    await refused;
    expect(
      (await b.read('https://a.test', savedOwnerID, savedNoteID))!.same(old),
      true,
    );
    await b.write('https://b.test', savedOwnerID, newer);
    await b.write('https://a.test', savedTargetID, newer);
    expect(storage.values, hasLength(3));
    for (final value in storage.values.values)
      expect((jsonDecode(value) as Map).keys.toSet(), {
        'savedId',
        'operationId',
        'expectedRevision',
        'stage',
      });
    final deleteHold = Completer<void>();
    storage.holdNextRead = deleteHold;
    final oldDelete = a.remove('https://a.test', savedOwnerID, old);
    final replacement = b.write('https://a.test', savedOwnerID, newer);
    await Future<void>.delayed(Duration.zero);
    expect(storage.writes, 3);
    deleteHold.complete();
    await oldDelete;
    await replacement;
    expect(
      (await a.read('https://a.test', savedOwnerID, savedNoteID))!.same(newer),
      true,
    );
    await expectLater(
      a.remove('https://a.test', savedOwnerID, old),
      throwsFormatException,
    );
    expect(
      (await a.read('https://a.test', savedOwnerID, savedNoteID))!.same(newer),
      true,
    );
    expect(
      (await a.read('https://a.test', savedTargetID, savedNoteID))!.same(newer),
      true,
    );
  });
  for (final stage in ['PREPARED', 'UNKNOWN']) {
    test('$stage持久化读回失败确定未发送，本机核对保留草稿且零后端receipt读取', () async {
      final store = MemorySavedNoteStore()..failReadStage = stage;
      final requests = <http.Request>[];
      final client = MockClient((r) async {
        requests.add(r);
        if (r.method == 'PUT') {
          final body = jsonDecode(r.body) as Map;
          return _savedJSON({
            'savedId': savedNoteID,
            'operationId': body['operationId'],
            'revision': 1,
            'status': 'SUCCEEDED',
          });
        }
        if (r.url.path.endsWith('/note'))
          return _savedJSON({
            'savedId': savedNoteID,
            'note': '',
            'revision': requests.any((r) => r.method == 'PUT') ? 1 : 0,
          });
        return _savedJSON({
          'items': [savedTestRow(savedNoteID, 'place', savedTargetID)],
          'nextCursor': null,
        });
      });
      final c = savedNoteController(client, store);
      await c.load(paged: true);
      final item = c.items.single;
      await c.readNote(item);
      c.setNoteDraft(item.id, '未发送的本人草稿');
      expect(await c.saveNote(item), false);
      expect(c.noteState(item.id)!.notSubmitted, true);
      expect(c.noteState(item.id)!.uncertain, false);
      expect(await c.recoverNote(item), false);
      expect(
        requests.where(
          (r) => r.method == 'PUT' || r.url.path.contains('/operations/'),
        ),
        isEmpty,
      );
      await c.readNote(item, refresh: true);
      expect(c.noteState(item.id)!.draft, '未发送的本人草稿');
      expect(store.values, isEmpty);
      expect(c.noteState(item.id)!.editable, true);
      expect(await c.saveNote(item), true);
      expect(requests.where((r) => r.method == 'PUT'), hasLength(1));
      c.dispose();
      client.close();
    });
  }
  test('冷启动PREPARED只核对本机准备记录，不恢复正文也不发送或读后端operation', () async {
    final store = MemorySavedNoteStore();
    await store.write(
      'https://saved.test',
      savedOwnerID,
      const SavedNotePending(
        savedID: savedNoteID,
        operationID: savedOperationID,
        expectedRevision: 0,
        stage: 'PREPARED',
      ),
    );
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.url.path.endsWith('/note'))
        return _savedJSON({
          'savedId': savedNoteID,
          'note': '当前已保存备注',
          'revision': 0,
        });
      return _savedJSON({
        'items': [savedTestRow(savedNoteID, 'place', savedTargetID)],
        'nextCursor': null,
      });
    });
    final c = savedNoteController(client, store);
    await c.load(paged: true);
    final item = c.items.single;
    expect(c.noteState(item.id)!.notSubmitted, true);
    expect(c.noteState(item.id)!.draft, '');
    await c.readNote(item);
    expect(store.values, isEmpty);
    expect(c.noteState(item.id)!.draft, '当前已保存备注');
    expect(
      requests.where(
        (r) => r.method != 'GET' || r.url.path.contains('/operations/'),
      ),
      isEmpty,
    );
    c.dispose();
    client.close();
  });
  test('分页读取 20+ 项、重复去重、失败保留并原游标重试', () async {
    var fail = false;
    final reads = <String?>[];
    final client = MockClient((r) async {
      expect(r.url.path, '/v1/me/saved/page');
      reads.add(r.url.queryParameters['cursor']);
      if (fail) return http.Response('', 503);
      return _savedJSON({
        'items': r.url.queryParameters['cursor'] == null
            ? List.generate(
                20,
                (i) => savedTestRow('bookmark-$i', 'place', 'place-$i'),
              )
            : [
                savedTestRow('bookmark-19', 'place', 'place-19'),
                savedTestRow('bookmark-20', 'group', 'group-20'),
              ],
        'nextCursor': r.url.queryParameters['cursor'] == null
            ? savedNoteID
            : null,
      });
    });
    final c = SavedController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://saved.test',
    );
    await c.load(paged: true);
    expect(c.items, hasLength(20));
    fail = true;
    await c.load(more: true);
    expect(c.failed, true);
    expect(c.items, hasLength(20));
    expect(c.nextCursor, savedNoteID);
    fail = false;
    await c.load(more: true);
    expect(c.items, hasLength(21));
    expect(c.hasMore, false);
    expect(reads, [null, savedNoteID, savedNoteID]);
    c.dispose();
    client.close();
  });
  test('迟到分页在账号 A B A 清空后不恢复上一页', () async {
    final held = Completer<http.Response>();
    var token = 'Bearer A';
    final client = MockClient((r) async => held.future);
    final c = SavedController(
      authorizationHeader: () => token,
      client: client,
      apiBaseUrl: 'https://saved.test',
    );
    final loading = c.load(paged: true);
    token = 'Bearer B';
    c.clear();
    token = 'Bearer A';
    c.clear();
    held.complete(
      _savedJSON({
        'items': [savedTestRow(savedNoteID, 'place', savedTargetID)],
        'nextCursor': null,
      }),
    );
    await loading;
    expect(c.items, isEmpty);
    expect(c.hasMore, false);
    expect(c.loading, false);
    c.dispose();
    client.close();
  });
  test('本人备注先写回读恢复selector，CAS保存和清空均消费精确回执', () async {
    final store = MemorySavedNoteStore();
    var current = '原本人备注', revision = 4;
    final writes = <http.Request>[];
    final client = MockClient((r) async {
      if (r.method == 'PUT') {
        final body = jsonDecode(r.body) as Map<String, dynamic>;
        final pending = store.values.values.single;
        expect(pending.operationID, body['operationId']);
        expect(store.readsAfterWrite, greaterThan(0));
        expect(body['expectedRevision'], revision);
        current = body['note'];
        revision++;
        writes.add(r);
        return _savedJSON({
          'savedId': savedNoteID,
          'operationId': body['operationId'],
          'revision': revision,
          'status': 'SUCCEEDED',
        });
      }
      if (r.url.path.endsWith('/note'))
        return _savedJSON({
          'savedId': savedNoteID,
          'note': current,
          'revision': revision,
        });
      return _savedJSON({
        'items': [savedTestRow(savedNoteID, 'place', savedTargetID)],
        'nextCursor': null,
      });
    });
    final c = savedNoteController(client, store);
    await c.load(paged: true);
    final item = c.items.single;
    await c.readNote(item);
    c.setNoteDraft(item.id, '新备注');
    expect(await c.saveNote(item), true);
    expect(c.items.single.note, '新备注');
    expect(c.items.single.noteRevision, 5);
    c.setNoteDraft(item.id, '');
    expect(await c.saveNote(item), true);
    expect(c.items.single.note, '');
    expect(c.items.single.noteRevision, 6);
    expect(writes, hasLength(2));
    expect(store.values, isEmpty);
    c.dispose();
    client.close();
  });
  test('CAS冲突保留草稿，人工读取当前备注可检查，UTF8超限零PUT', () async {
    final store = MemorySavedNoteStore();
    var revision = 2, note = '旧版本', puts = 0;
    final client = MockClient((r) async {
      if (r.method == 'PUT') {
        puts++;
        return http.Response('', 409);
      }
      if (r.url.path.endsWith('/note'))
        return _savedJSON({
          'savedId': savedNoteID,
          'note': note,
          'revision': revision,
        });
      return _savedJSON({
        'items': [savedTestRow(savedNoteID, 'place', savedTargetID)],
        'nextCursor': null,
      });
    });
    final c = savedNoteController(client, store);
    await c.load(paged: true);
    final item = c.items.single;
    await c.readNote(item);
    c.setNoteDraft(item.id, '我保留的草稿');
    expect(await c.saveNote(item), false);
    expect(c.noteState(item.id)!.draft, '我保留的草稿');
    expect(store.values, isEmpty);
    revision = 3;
    note = '其他端修改';
    await c.readNote(item, refresh: true);
    expect(c.noteState(item.id)!.draft, '我保留的草稿');
    expect(c.noteState(item.id)!.currentNote, '其他端修改');
    c.setNoteDraft(item.id, '汉' * 667);
    expect(await c.saveNote(item), false);
    expect(puts, 1);
    c.dispose();
    client.close();
  });
  test('UNKNOWN保留同op禁止再PUT，404保留，重启只恢复selector核实原回执再读当前', () async {
    final store = MemorySavedNoteStore();
    final requests = <http.Request>[];
    var receiptReady = false;
    final client = MockClient((r) async {
      requests.add(r);
      if (r.method == 'PUT') throw TimeoutException('synthetic lost response');
      if (r.url.path.contains('/operations/')) {
        if (!receiptReady) return http.Response('', 404);
        final p = store.values.values.single;
        return _savedJSON({
          'savedId': savedNoteID,
          'operationId': p.operationID,
          'revision': 3,
          'status': 'SUCCEEDED',
        });
      }
      if (r.url.path.endsWith('/note'))
        return _savedJSON({
          'savedId': savedNoteID,
          'note': receiptReady ? '当前端的权威备注' : '原备注',
          'revision': receiptReady ? 4 : 2,
        });
      return _savedJSON({
        'items': [savedTestRow(savedNoteID, 'place', savedTargetID)],
        'nextCursor': null,
      });
    });
    var c = savedNoteController(client, store);
    await c.load(paged: true);
    var item = c.items.single;
    await c.readNote(item);
    c.setNoteDraft(item.id, '本人待核实草稿');
    expect(await c.saveNote(item), false);
    final original = c.noteState(item.id)!.pending!;
    expect(c.noteState(item.id)!.draft, '本人待核实草稿');
    expect(await c.saveNote(item), false);
    expect(await c.recoverNote(item), false);
    expect(c.noteState(item.id)!.pending!.same(original), true);
    c.dispose();
    c = savedNoteController(client, store);
    await c.load(paged: true);
    item = c.items.single;
    expect(c.noteState(item.id)!.draft, '');
    expect(c.noteState(item.id)!.pending!.same(original), true);
    receiptReady = true;
    expect(await c.recoverNote(item), true);
    expect(c.items.single.note, '当前端的权威备注');
    expect(c.items.single.noteRevision, 4);
    expect(store.values, isEmpty);
    expect(requests.where((r) => r.method == 'PUT'), hasLength(1));
    final recovery = requests
        .where((r) => r.url.path.contains('/operations/'))
        .last;
    expect(recovery.url.path.endsWith('/${original.operationID}'), true);
    c.dispose();
    client.close();
  });
  test('selector写回读失败不发送，凭证不写入secure恢复数据', () async {
    final store = MemorySavedNoteStore()..failWrite = true;
    var puts = 0;
    final client = MockClient((r) async {
      if (r.method == 'PUT') {
        puts++;
        return http.Response('', 500);
      }
      if (r.url.path.endsWith('/note'))
        return _savedJSON({'savedId': savedNoteID, 'note': '', 'revision': 0});
      return _savedJSON({
        'items': [savedTestRow(savedNoteID, 'place', savedTargetID)],
        'nextCursor': null,
      });
    });
    final c = savedNoteController(client, store);
    await c.load(paged: true);
    final item = c.items.single;
    await c.readNote(item);
    c.setNoteDraft(item.id, 'private note');
    expect(await c.saveNote(item), false);
    expect(puts, 0);
    expect(c.noteState(item.id)!.draft, 'private note');
    final selector = SavedNotePending(
      savedID: savedNoteID,
      operationID: savedOperationID,
      expectedRevision: 0,
    );
    expect(selector.toJson().keys.toSet(), {
      'savedId',
      'operationId',
      'expectedRevision',
      'stage',
    });
    c.dispose();
    client.close();
  });
  for (final phase in ['read', 'write', 'recovery']) {
    test('本人备注 $phase 迟到跨账号 A B A 退休，不删除旧本人journal或关闭借用client', () async {
      final store = MemorySavedNoteStore();
      final held = Completer<http.Response>();
      var token = 'Bearer A', owner = savedOwnerID, closes = 0, puts = 0;
      final client = _ClosingClient((r) async {
        if (r.method == 'PUT') {
          puts++;
          return held.future;
        }
        if (r.url.path.contains('/operations/')) return held.future;
        if (r.url.path.endsWith('/note')) {
          if (phase == 'read') return held.future;
          return _savedJSON({
            'savedId': savedNoteID,
            'note': '',
            'revision': 0,
          });
        }
        return _savedJSON({
          'items': [savedTestRow(savedNoteID, 'place', savedTargetID)],
          'nextCursor': null,
        });
      }, () => closes++);
      final c = SavedController(
        authorizationHeader: () => token,
        ownerID: () => owner,
        client: client,
        apiBaseUrl: 'https://saved.test',
        notePendingStore: store,
      );
      if (phase == 'recovery')
        await store.write(
          'https://saved.test',
          owner,
          const SavedNotePending(
            savedID: savedNoteID,
            operationID: savedOperationID,
            expectedRevision: 0,
          ),
        );
      await c.load(paged: true);
      final item = c.items.single;
      Future<Object?> request;
      if (phase == 'read')
        request = c.readNote(item);
      else if (phase == 'write') {
        await c.readNote(item);
        c.setNoteDraft(item.id, 'old private');
        request = c.saveNote(item);
      } else
        request = c.recoverNote(item);
      await Future<void>.delayed(Duration.zero);
      token = 'Bearer B';
      owner = savedTargetID;
      c.clear();
      token = 'Bearer A';
      owner = savedOwnerID;
      c.clear();
      held.complete(
        _savedJSON(
          phase == 'read'
              ? {'savedId': savedNoteID, 'note': 'retired body', 'revision': 1}
              : {
                  'savedId': savedNoteID,
                  'operationId': store.values.values.single.operationID,
                  'revision': 1,
                  'status': 'SUCCEEDED',
                },
        ),
      );
      await request;
      expect(c.items, isEmpty);
      expect(c.noteState(item.id), isNull);
      if (phase != 'read') expect(store.values, hasLength(1));
      expect(puts, phase == 'write' ? 1 : 0);
      c.dispose();
      expect(closes, 0);
      client.close();
      expect(closes, 1);
    });
  }
  test('收藏明确当前条件传入原writer，私人不可见收藏仍可清理', () async {
    const id = '22e9cd18-babb-4359-9d1e-53593bedff47';
    final wire = actionWire(ref: const EntityActionRef('place', id));
    wire['actions'][3]['state'] = 'UNAVAILABLE';
    final view = EntityActionView.decode(
      wire,
      const EntityActionRef('place', id),
    );
    var present = false;
    final seen = <http.Request>[];
    final client = MockClient((r) async {
      seen.add(r);
      if (r.method == 'GET') {
        return http.Response(
          jsonEncode({
            'data': present
                ? [
                    {
                      'id': 'original-save',
                      'kind': 'place',
                      'targetId': id,
                      'title': '',
                      'summary': '',
                      'cityId': '',
                      'available': false,
                    },
                  ]
                : [],
          }),
          200,
        );
      }
      if (r.method == 'POST') {
        present = true;
        return http.Response('{"data":{"id":"original-save"}}', 201);
      }
      present = false;
      return http.Response('', 204);
    });
    final controller = SavedController(
      authorizationHeader: () => 'Bearer a',
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    await controller.toggle(
      'place',
      id,
      approved: view.action(EntityActionKind.save).reviewed(view),
    );
    final post = seen.singleWhere((r) => r.method == 'POST');
    expect(post.headers['X-Birdtie-Action-Version'], view.sourceVersion);
    expect(
      post.headers['X-Birdtie-Action-Until'],
      view.validUntil.toIso8601String(),
    );
    expect(post.headers['X-Birdtie-Action-Operation'], 'SAVE');
    // Ownership-only unavailable-item cleanup intentionally preserves legacy path.
    await controller.toggle('place', id);
    final deleted = seen.singleWhere((r) => r.method == 'DELETE');
    expect(deleted.url.path, '/v1/me/saved/original-save');
    expect(deleted.headers.containsKey('X-Birdtie-Action-Version'), isFalse);
    controller.dispose();
    client.close();
  });
  for (final mutation in [false, true]) {
    test('收藏${mutation ? '写' : '读'}迟到在本人A-B-A清空后不复活或重发，借用client不关闭', () async {
      final pending = Completer<http.Response>();
      var token = 'Bearer A', calls = 0, closes = 0;
      final client = _ClosingClient((r) async {
        calls++;
        return pending.future;
      }, () => closes++);
      final controller = SavedController(
        authorizationHeader: () => token,
        client: client,
        apiBaseUrl: 'https://api.test',
      );
      final result = mutation
          ? controller.toggle('place', 'target')
          : controller.load();
      await Future<void>.delayed(Duration.zero);
      token = 'Bearer B';
      controller.clear();
      token = 'Bearer A';
      controller.clear();
      pending.complete(
        mutation
            ? http.Response('{"data":{"id":"old"}}', 201)
            : http.Response(
                '{"data":[{"id":"old","kind":"place","targetId":"target","title":"private","summary":"old","available":true}]}',
                200,
              ),
      );
      await result;
      expect(calls, 1);
      expect(controller.items, isEmpty);
      expect(controller.busy, isEmpty);
      controller.dispose();
      expect(closes, 0);
      client.close();
      expect(closes, 1);
    });
  }
  test('Saved loads, adds and removes an owner bookmark', () async {
    var present = false;
    String? authorization = 'Bearer test-session';
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], authorization);
      if (request.method == 'GET') {
        return http.Response(
          jsonEncode({
            'data': present
                ? [
                    {
                      'id': 'saved-1',
                      'kind': 'place',
                      'targetId': 'place-1',
                      'title': 'Town House',
                      'summary': 'Public place',
                      'cityId': 'aberdeen-gb',
                      'available': true,
                    },
                  ]
                : [],
          }),
          200,
        );
      }
      if (request.method == 'POST') {
        expect(jsonDecode(request.body), {
          'kind': 'place',
          'targetId': 'place-1',
        });
        present = true;
        return http.Response('{"data":{"id":"saved-1"}}', 201);
      }
      if (request.method == 'DELETE') {
        expect(request.url.path, '/v1/me/saved/saved-1');
        present = false;
        return http.Response('', 204);
      }
      return http.Response('', 404);
    });
    final saved = SavedController(
      authorizationHeader: () => authorization,
      client: client,
      apiBaseUrl: 'http://localhost:8080',
    );
    await saved.load();
    expect(saved.items, isEmpty);
    await saved.toggle('place', 'place-1');
    expect(saved.contains('place', 'place-1'), isTrue);
    await saved.toggle('place', 'place-1');
    expect(saved.items, isEmpty);
    authorization = null;
    saved.clear();
    expect(saved.items, isEmpty);
    saved.dispose();
  });
}

const savedNoteID = '10000000-0000-4000-8000-000000000001';
const savedTargetID = '10000000-0000-4000-8000-000000000002';
const savedOwnerID = '10000000-0000-4000-8000-000000000003';
const savedOperationID = '10000000-0000-4000-8000-000000000004';
Map<String, Object> savedTestRow(
  String id,
  String kind,
  String target, {
  String city = 'alpha',
  bool available = true,
  String? title,
}) => {
  'id': id,
  'kind': kind,
  'targetId': target,
  'title': title ?? '$kind $target',
  'summary': '原实体摘要',
  'cityId': city,
  'available': available,
  'note': '',
  'noteRevision': 0,
};
http.Response _savedJSON(Object? data) => http.Response(
  jsonEncode({'data': data}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
SavedController savedNoteController(
  http.Client client,
  SavedNotePendingStore store,
) => SavedController(
  authorizationHeader: () => 'Bearer A',
  ownerID: () => savedOwnerID,
  client: client,
  apiBaseUrl: 'https://saved.test',
  notePendingStore: store,
);

class MemorySavedNoteStore implements SavedNotePendingStore {
  final values = <String, SavedNotePending>{};
  bool failWrite = false;
  String? failReadStage;
  int readsAfterWrite = 0;
  String _key(String origin, String owner, String id) => '$origin|$owner|$id';
  @override
  Future<SavedNotePending?> read(
    String origin,
    String owner,
    String savedID,
  ) async {
    if (values.isNotEmpty) readsAfterWrite++;
    if (values[_key(origin, owner, savedID)]?.stage == failReadStage &&
        failReadStage != null) {
      failReadStage = null;
      throw StateError('synthetic readback failure');
    }
    return values[_key(origin, owner, savedID)];
  }

  @override
  Future<void> write(
    String origin,
    String owner,
    SavedNotePending pending,
  ) async {
    if (failWrite) throw StateError('synthetic secure storage failure');
    values[_key(origin, owner, pending.savedID)] = pending;
  }

  @override
  Future<void> remove(
    String origin,
    String owner,
    SavedNotePending pending,
  ) async {
    final key = _key(origin, owner, pending.savedID);
    if (values[key]?.same(pending) != true) throw StateError('wrong journal');
    values.remove(key);
  }
}

class _RacingNoteStore extends MemorySavedNoteStore {
  @override
  Future<void> write(
    String origin,
    String owner,
    SavedNotePending pending,
  ) async {
    await super.write(
      origin,
      owner,
      const SavedNotePending(
        savedID: savedNoteID,
        operationID: savedOperationID,
        expectedRevision: 0,
      ),
    );
    throw const FormatException('another instance already sent its operation');
  }
}

class _HangingSecureStorage extends FlutterSecureStorage {
  final values = <String, String>{};
  Completer<void>? holdNextRead;
  int writes = 0;
  @override
  Future<String?> read({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    final hold = holdNextRead;
    holdNextRead = null;
    if (hold != null) await hold.future;
    return values[key];
  }

  @override
  Future<void> write({
    required String key,
    required String? value,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    writes++;
    if (value == null)
      values.remove(key);
    else
      values[key] = value;
  }

  @override
  Future<void> delete({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    values.remove(key);
  }
}

class _ClosingClient extends MockClient {
  _ClosingClient(super.fn, this.closed);
  final void Function() closed;
  @override
  void close() {
    closed();
    super.close();
  }
}
