import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/collection_control.dart';
import 'package:birdtie_client/src/content/collection_control_pending_store.dart';
import 'package:birdtie_client/src/content/collection_create_pending_store.dart';
import 'collection_control_test.dart';

import 'package:birdtie_client/src/content/personal_collection_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const collectionOwner = '11111111-1111-4111-8111-111111111111';
const collectionID = '22222222-2222-4222-8222-222222222222';
const collectionPlaceID = '33333333-3333-4333-8333-333333333333';
Map<String, dynamic> collectionReceipt({
  String kind = 'journey',
  String id = collectionID,
  int revision = 1,
  String title = '周末路线',
  String status = 'draft',
  String owner = collectionOwner,
}) => {
  'id': id,
  'ownerAccountId': owner,
  'kind': kind,
  'cityId': 'aberdeen-gb',
  'title': title,
  'summary': '',
  'visibility': 'private',
  'status': status,
  'revision': revision,
  'entries': [
    {
      'placeId': collectionPlaceID,
      'placeName': '海边公园',
      'note': '',
      'locationVisibility': 'hidden',
      'available': true,
      'sourceExpiresAt': DateTime.now()
          .toUtc()
          .add(const Duration(minutes: 1))
          .toIso8601String(),
    },
  ],
};
http.Response collectionResponse(dynamic data, {int code = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      code,
      headers: {'Content-Type': 'application/json'},
    );
PersonalCollectionController collectionController(
  http.Client client, {
  String kind = 'journey',
  String? Function()? owner,
  String? Function()? token,
  Listenable? changes,
  bool Function()? current,
  String cityID = 'aberdeen-gb',
  String apiBaseUrl = 'https://synthetic.invalid',
  PersonalCollectionRetainedLocalDraft? retained,
  ValueChanged<PersonalCollectionRetainedLocalDraft>? onRetain,
  CollectionControlPendingStore? controlStore,
  CollectionCreatePendingStore? createStore,
}) => PersonalCollectionController(
  kind: kind,
  cityID: cityID,
  ownerID: owner ?? () => collectionOwner,
  authorizationHeader: token ?? () => 'Bearer owner',
  identityChanges: changes,
  sourceCurrent: current,
  client: client,
  apiBaseUrl: apiBaseUrl,
  retainedLocalDraft: retained,
  onRetainLocalDraft: onRetain,
  controlPendingStore: controlStore ?? MemoryCollectionControlPendingStore(),
  createPendingStore: createStore ?? MemoryCollectionCreatePendingStore(),
);
void seedCollectionDraft(PersonalCollectionController c) {
  c.setText(title: '周末路线');
  c.addPlace(const CollectionPlace(id: collectionPlaceID, name: '海边公园'));
}

Future<bool> saveCollectionUpdate(
  PersonalCollectionController c, {
  String action = 'UPDATE',
}) async {
  final p = await c.prepareControl(action);
  return p != null && await c.commitControl(p);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  collectionCreateRecoveryCases();
  collection147ControllerCases();
  test(
    'C3 withdrawn borrowed source permits only original selectors own text CAS without restoring metadata',
    () async {
      final bodies = <Map<String, dynamic>>[];
      var currentRevision = 1;
      Map<String, dynamic> record(
        int revision, {
        String title = '本人旧标题',
        String note = '',
      }) => {
        ...collectionReceipt(revision: revision, title: title),
        'privatePlaceCopyRestricted': true,
        'entries': [
          {
            'placeId': collectionPlaceID,
            'note': note,
            'locationVisibility': 'hidden',
            'available': false,
          },
        ],
      };
      var savedTitle = '本人旧标题', savedNote = '';
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') {
          final input = ccRequest(r), b = input.input!.toJson();
          bodies.add(b);
          currentRevision = 2;
          savedTitle = input.input!.title;
          savedNote = input.input!.entries.single.note;
          return ccResponse(ccReceipt(input));
        }
        return collectionResponse(
          record(currentRevision, title: savedTitle, note: savedNote),
        );
      });
      final c = collectionController(client);
      try {
        await c.openID(collectionID);
        c.setText(title: '本人新标题');
        c.editEntry(0, '本人私人备注');
        expect(await saveCollectionUpdate(c), isTrue);
        expect(c.lastControlReceipt!.expectedRevision, 1);
        expect(bodies.single['entries'], [
          {
            'placeId': collectionPlaceID,
            'note': '本人私人备注',
            'locationVisibility': 'hidden',
          },
        ]);
        expect(c.editing!.privatePlaceCopyRestricted, isTrue);
        expect(c.editing!.entries.single.available, isFalse);
        expect(c.editing!.entries.single.placeName, isEmpty);
        expect(c.editing!.entries.single.latitude, isNull);
        expect(c.editing!.entries.single.longitude, isNull);
        c.draftEntries = List.unmodifiable([
          ...c.draftEntries,
          const PersonalCollectionEntry(
            placeID: collectionOwner,
            available: false,
          ),
        ]);
        c.setText(title: '不能借复制许可新加失效ID');
        expect(await saveCollectionUpdate(c), isFalse);
        expect(bodies, hasLength(1));
      } finally {
        c.dispose();
        client.close();
      }
    },
  );
  test(
    'C3 private copy restriction is authoritative read only and survives local entry changes',
    () async {
      expect(
        PersonalCollection.fromJson(
          collectionReceipt(),
        ).privatePlaceCopyRestricted,
        isFalse,
      );
      for (final wrong in [null, 'true', 1]) {
        expect(
          () => PersonalCollection.fromJson({
            ...collectionReceipt(),
            'privatePlaceCopyRestricted': wrong,
          }),
          throwsFormatException,
        );
      }
      final bodies = <Map<String, dynamic>>[];
      var revision = 1;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') {
          final input = ccRequest(r);
          bodies.add(input.input!.toJson());
          revision = 2;
          return ccResponse(ccReceipt(input));
        }
        return collectionResponse({
          ...collectionReceipt(revision: revision),
          'privatePlaceCopyRestricted': true,
        });
      });
      final c = collectionController(client);
      try {
        expect(await c.openID(collectionID), isTrue);
        expect(c.editing!.privatePlaceCopyRestricted, isTrue);
        c.setText(title: '本人的改名');
        c.removeEntry(0);
        c.addPlace(
          const CollectionPlace(id: collectionPlaceID, name: '重新点入但尚未保存'),
        );
        expect(c.editing!.privatePlaceCopyRestricted, isTrue);
        await saveCollectionUpdate(c);
        expect(
          bodies.single.containsKey('privatePlaceCopyRestricted'),
          isFalse,
        );
        expect(c.editing!.privatePlaceCopyRestricted, isTrue);
      } finally {
        c.dispose();
        client.close();
      }
    },
  );
  test(
    'C3 direct destination open rejects a different actual private ID',
    () async {
      final client = MockClient(
        (r) async =>
            collectionResponse(collectionReceipt(id: collectionPlaceID)),
      );
      final c = collectionController(client);
      expect(await c.openID(collectionID), isFalse);
      expect(c.editing, isNull);
      c.dispose();
      client.close();
    },
  );
  test(
    'C3 ordinary Session renewal retains only same-owner copy title and source selector',
    () async {
      var token = 'Bearer first';
      final signal = ChangeNotifier();
      PersonalCollectionRetainedLocalDraft? retained;
      final client = MockClient((r) async => collectionResponse(null));
      final old = collectionController(
        client,
        token: () => token,
        changes: signal,
        onRetain: (d) => retained = d,
      );
      const local = PersonalCollectionCopyLocalDraft(
        sourceKind: 'theme_map',
        sourceID: collectionID,
        title: '本人的副本标题',
      );
      old.retainCopyLocalDraft(local);
      expect(old.title, isEmpty);
      expect(old.draftEntries, isEmpty);
      token = 'Bearer renewed';
      signal.notifyListeners();
      expect(old.current, isTrue);
      expect(old.copyLocalDrafts[local.key]!.title, '本人的副本标题');
      expect(retained!.ownerID, collectionOwner);
      expect(retained!.copyLocalDrafts[local.key]!.title, '本人的副本标题');
      final next = collectionController(
        client,
        token: () => token,
        retained: retained,
      );
      expect(next.copyLocalDrafts[local.key]!.title, '本人的副本标题');
      expect(next.current, isTrue);
      expect(next.editing, isNull);
      expect(next.draftEntries, isEmpty);
      expect(next.publicationPending, isNull);
      old.dispose();
      next.dispose();
      signal.dispose();
      client.close();
    },
  );
  test(
    'C3 copy title remains isolated by original owner, origin, city and kind',
    () {
      final client = MockClient((r) async => collectionResponse(null));
      final old = collectionController(client);
      const local = PersonalCollectionCopyLocalDraft(
        sourceKind: 'journey',
        sourceID: collectionID,
        title: '本人文字',
      );
      old.retainCopyLocalDraft(local);
      final saved = old.retainedLocalDraft!;
      old.dispose();
      for (final boundary in ['owner', 'origin', 'city', 'kind']) {
        final next = collectionController(
          client,
          retained: saved,
          owner: () => boundary == 'owner' ? collectionID : collectionOwner,
          apiBaseUrl: boundary == 'origin'
              ? 'https://other.invalid'
              : 'https://synthetic.invalid',
          cityID: boundary == 'city' ? 'other-city' : 'aberdeen-gb',
          kind: boundary == 'kind' ? 'theme_map' : 'journey',
        );
        expect(next.copyLocalDrafts, isEmpty);
        next.dispose();
      }
      client.close();
    },
  );
  test(
    'C3 clearing last local copy title replaces old host cache without replacing private creation draft',
    () {
      final client = MockClient((r) async => collectionResponse(null));
      PersonalCollectionRetainedLocalDraft? retained;
      final old = collectionController(client, onRetain: (d) => retained = d);
      const local = PersonalCollectionCopyLocalDraft(
        sourceKind: 'journey',
        sourceID: collectionID,
        title: '只属于本人',
      );
      old.retainCopyLocalDraft(local);
      expect(retained!.copyLocalDrafts, isNotEmpty);
      old.clearCopyLocalDraft(local.key);
      expect(retained!.copyLocalDrafts, isEmpty);
      expect(retained!.title, isEmpty);
      final next = collectionController(client, retained: retained);
      expect(next.copyLocalDrafts, isEmpty);
      expect(next.editing, isNull);
      old.dispose();
      next.dispose();
      client.close();
    },
  );
  test('切号前保留原owner文字与selectors，清除来源坐标标签和批准', () async {
    var owner = collectionOwner;
    final signal = ChangeNotifier();
    PersonalCollectionRetainedLocalDraft? retained;
    final client = MockClient(
      (r) async => collectionResponse(collectionReceipt(revision: 7)),
    );
    final c = collectionController(
      client,
      changes: signal,
      owner: () => owner,
      onRetain: (v) => retained = v,
    );
    try {
      await c.open(PersonalCollection.fromJson(collectionReceipt()));
      c.setText(title: '未保存文字', summary: '未发送说明');
      c.editEntry(0, '保留个人备注');
      owner = collectionID;
      signal.notifyListeners();
      expect(c.current, isFalse);
      expect(retained, isNotNull);
      expect(retained!.ownerID, collectionOwner);
      expect(retained!.title, '未保存文字');
      expect(retained!.editingID, collectionID);
      expect(retained!.editingRevision, 7);
      expect(retained!.entries.single.note, '保留个人备注');
      expect(retained!.entries.single.placeName, isEmpty);
      expect(retained!.entries.single.latitude, isNull);
      expect(retained!.entries.single.longitude, isNull);
      expect(retained!.entries.single.sourceExpiresAt, isNull);
      expect(retained!.entries.single.sourceCurrent, isFalse);
      expect(retained!.unknown, isFalse);
    } finally {
      c.dispose();
      client.close();
      signal.dispose();
    }
  });
  test('恢复草稿仅原owner/kind/city/transport，新来源不复用锚点', () async {
    final client = MockClient((r) async => collectionResponse(null));
    final c = collectionController(client);
    seedCollectionDraft(c);
    c.editEntry(0, '原城的备注');
    final draft = c.retainedLocalDraft!;
    c.dispose();
    final restored = collectionController(
      client,
      retained: draft,
      token: () => 'Bearer new-session',
    );
    try {
      expect(restored.title, '周末路线');
      expect(restored.draftEntries.single.note, '原城的备注');
      expect(restored.draftEntries.single.sourceCurrent, isFalse);
      expect(restored.draftEntries.single.placeName, isEmpty);
      expect(restored.current, isTrue);
    } finally {
      restored.dispose();
    }
    for (final boundary in ['owner', 'kind', 'city', 'transport']) {
      final wrong = collectionController(
        client,
        retained: draft,
        owner: () => boundary == 'owner' ? collectionID : collectionOwner,
        kind: boundary == 'kind' ? 'theme_map' : 'journey',
        cityID: boundary == 'city' ? 'other-city' : 'aberdeen-gb',
        apiBaseUrl: boundary == 'transport'
            ? 'https://other.invalid'
            : 'https://synthetic.invalid',
      );
      try {
        expect(wrong.title, isEmpty);
        expect(wrong.draftEntries, isEmpty);
        expect(wrong.editing, isNull);
        expect(wrong.error, isNotNull);
      } finally {
        wrong.dispose();
      }
    }
    client.close();
  });
  test('原来源在途create退休后UNKNOWN恢复只GET，不重发旧POST', () async {
    var source = true;
    final signal = ChangeNotifier();
    final reply = Completer<http.Response>();
    final postEntered = Completer<void>();
    PersonalCollectionRetainedLocalDraft? retained;
    var posts = 0;
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      if (r.method == 'POST') {
        posts++;
        postEntered.complete();
        return reply.future;
      }
      return collectionResponse(collectionReceipt());
    });
    final old = collectionController(
      client,
      current: () => source,
      changes: signal,
      onRetain: (v) => retained = v,
    );
    seedCollectionDraft(old);
    final submitted = old.save();
    await postEntered.future;
    source = false;
    signal.notifyListeners();
    expect(retained!.unknown, isTrue);
    expect(retained!.unknownPayload!['operationId'], isNotNull);
    reply.complete(collectionResponse(collectionReceipt(), code: 201));
    await submitted;
    old.dispose();
    signal.dispose();
    final restored = collectionController(
      client,
      retained: retained,
      token: () => 'Bearer new-source',
    );
    try {
      expect(restored.uncertain, isTrue);
      expect(await restored.save(), isFalse);
      expect(await restored.withdraw(), isFalse);
      expect(posts, 1);
      expect(await restored.recover(), isTrue);
      expect(posts, 1);
      expect(calls.last.method, 'GET');
      expect(
        calls.last.url.path.endsWith(
          '/operations/${retained!.unknownPayload!['operationId']}',
        ),
        isTrue,
      );
      expect(restored.editing!.id, collectionID);
      expect(restored.uncertain, isFalse);
    } finally {
      restored.dispose();
      client.close();
    }
  });
  test('恢复编辑原ID/revision先GET重验，保留修改稿再CAS', () async {
    final prepare = MockClient(
      (r) async => collectionResponse(collectionReceipt(revision: 7)),
    );
    final old = collectionController(prepare);
    await old.open(PersonalCollection.fromJson(collectionReceipt()));
    old.setText(title: '跨城切回的修改');
    old.editEntry(0, '本机修改备注');
    final retained = old.retainedLocalDraft!;
    old.dispose();
    prepare.close();
    final requests = <http.Request>[];
    var actualRevision = 7;
    final client = MockClient((r) async {
      requests.add(r);
      if (r.url.path.endsWith('/preview'))
        return ccResponse(ccPreview(ccRequest(r)));
      if (r.method == 'PUT') {
        final input = ccRequest(r);
        expectSync(input.expectedRevision, 7);
        expectSync(input.input!.title, '跨城切回的修改');
        expectSync(input.input!.entries.single.note, '本机修改备注');
        actualRevision = 8;
        return ccResponse(ccReceipt(input));
      }
      return collectionResponse(
        collectionReceipt(
          revision: actualRevision,
          title: actualRevision == 8 ? '跨城切回的修改' : '周末路线',
        ),
      );
    });
    final c = collectionController(client, retained: retained);
    try {
      expect(c.editing!.id, collectionID);
      expect(c.editing!.entries, isEmpty);
      expect(c.draftEntries.single.sourceCurrent, isFalse);
      expect(await saveCollectionUpdate(c), isTrue);
      expect(requests.map((r) => r.method), ['GET', 'POST', 'PUT', 'GET']);
      expect(
        requests
            .where((r) => r.method == 'GET')
            .every((r) => r.url.path.endsWith('/journey/$collectionID')),
        isTrue,
      );
      expect(c.editing!.revision, 8);
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('恢复编辑遇到新版本不覆盖，只保留本机稿且没有PUT', () async {
    final prepare = MockClient(
      (r) async => collectionResponse(collectionReceipt(revision: 7)),
    );
    final old = collectionController(prepare);
    await old.open(PersonalCollection.fromJson(collectionReceipt()));
    old.setText(title: '保留旧版的修改');
    final retained = old.retainedLocalDraft!;
    old.dispose();
    prepare.close();
    final methods = <String>[];
    final client = MockClient((r) async {
      methods.add(r.method);
      return collectionResponse(
        collectionReceipt(revision: 8, title: '别人最新版本'),
      );
    });
    final c = collectionController(client, retained: retained);
    try {
      expect(await saveCollectionUpdate(c), isFalse);
      expect(methods, ['GET']);
      expect(c.title, '保留旧版的修改');
      expect(c.editing!.revision, 7);
      expect(c.error, contains('版本已变化'));
    } finally {
      c.dispose();
      client.close();
    }
  });
  test(
    '147 cold unknown withdrawal restores original op and never DELETE or current-row guess',
    () async {
      final store = MemoryCollectionControlPendingStore(),
          calls = <http.Request>[];
      CollectionControlInput? submitted;
      final oldClient = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') {
          submitted = ccRequest(r);
          return ccResponse(null, status: 503);
        }
        return collectionResponse(collectionReceipt(revision: 7));
      });
      final old = collectionController(oldClient, controlStore: store);
      await old.openID(collectionID);
      expect(await saveCollectionUpdate(old, action: 'WITHDRAW'), isFalse);
      final retained = old.retainedLocalDraft!;
      old.dispose();
      oldClient.close();
      final client = MockClient((r) async {
        calls.add(r);
        expectSync(r.method, 'GET');
        expectSync(
          r.url.path,
          '/v1/me/personal-collection-controls/operations/' +
              submitted!.operationID,
        );
        return ccResponse(ccReceipt(submitted!));
      });
      final c = collectionController(
        client,
        retained: retained,
        controlStore: store,
      );
      try {
        await c.loadControlPending();
        expect(c.pendingWithdrawal, isTrue);
        expect(await c.withdraw(), isFalse);
        expect(await c.save(), isFalse);
        expect(await c.recover(), isTrue);
        expect(calls, hasLength(1));
        expect(c.editing, isNull);
        expect(c.uncertain, isFalse);
        expect(store.values, isEmpty);
      } finally {
        c.dispose();
        client.close();
      }
    },
  );
  for (final kind in ['journey', 'theme_map']) {
    test('个人$kind未知回执只读取原operation，不重复创建', () async {
      final requests = <Map<String, dynamic>>[];
      var posts = 0;
      final client = MockClient((r) async {
        if (r.method == 'POST') {
          requests.add(jsonDecode(r.body) as Map<String, dynamic>);
          ++posts;
          throw http.ClientException('lost receipt');
        }
        expect(r.method, 'GET');
        expect(
          r.url.path,
          endsWith('/operations/${requests.single['operationId']}'),
        );
        return collectionResponse(collectionReceipt(kind: kind));
      });
      final c = collectionController(client, kind: kind);
      addTearDown(c.dispose);
      addTearDown(client.close);
      seedCollectionDraft(c);
      expect(await c.save(), isFalse);
      expect(c.uncertain, isTrue);
      expect(c.title, '周末路线');
      expect(c.draftEntries, hasLength(1));
      c.setText(title: '不能覆盖在途版本');
      expect(c.title, '周末路线');
      expect(await c.save(), isFalse);
      expect(await c.recover(), isTrue);
      expect(posts, 1);
      expect(
        requests.first['operationId'],
        matches(RegExp(r'^[0-9a-f-]{36}$')),
      );
      expect(c.editing!.id, collectionID);
      expect(c.uncertain, isFalse);
      expect(c.dirty, isFalse);
    });
  }
  test('创建结果未知通过operation只读核实成功', () async {
    final reads = <http.Request>[];
    final client = MockClient((r) async {
      reads.add(r);
      return r.method == 'POST'
          ? collectionResponse(null, code: 503)
          : collectionResponse(collectionReceipt());
    });
    final c = collectionController(client);
    addTearDown(c.dispose);
    addTearDown(client.close);
    seedCollectionDraft(c);
    await c.save();
    expect(await c.recover(), isTrue);
    expect(reads.length, 2);
    expect(
      reads.last.url.path,
      startsWith('/v1/me/personal-collections/operations/'),
    );
    expect(reads.last.method, 'GET');
    expect(c.uncertain, isFalse);
  });
  test('保存单飞，账号ABA退休迟到回执与本机稿', () async {
    var owner = collectionOwner;
    final changes = ChangeNotifier(), pending = Completer<http.Response>();
    var calls = 0;
    final postEntered = Completer<void>();
    final client = MockClient((r) {
      calls++;
      postEntered.complete();
      return pending.future;
    });
    final c = collectionController(
      client,
      owner: () => owner,
      changes: changes,
    );
    addTearDown(c.dispose);
    addTearDown(client.close);
    addTearDown(changes.dispose);
    seedCollectionDraft(c);
    final first = c.save();
    expect(await c.save(), isFalse);
    await postEntered.future;
    expect(c.saving, isTrue);
    owner = collectionID;
    changes.notifyListeners();
    owner = collectionOwner;
    changes.notifyListeners();
    pending.complete(collectionResponse(collectionReceipt(), code: 201));
    expect(await first, isFalse);
    expect(calls, 1);
    expect(c.current, isFalse);
    expect(c.title, isEmpty);
    expect(c.items, isEmpty);
    expect(c.draftEntries, isEmpty);
  });
  test('来源改变退休迟到地点搜索，不借旧source标签', () async {
    var active = true;
    final changes = ChangeNotifier(), pending = Completer<http.Response>();
    final client = MockClient((r) => pending.future);
    final c = collectionController(
      client,
      changes: changes,
      current: () => active,
    );
    addTearDown(c.dispose);
    addTearDown(client.close);
    addTearDown(changes.dispose);
    final query = c.searchPlaces('park');
    active = false;
    changes.notifyListeners();
    pending.complete(
      collectionResponse([
        {'id': collectionPlaceID, 'name': '不能复活'},
      ]),
    );
    await query;
    expect(c.places, isEmpty);
    expect(c.current, isFalse);
  });
  test(
    '147 PUT specific current revision conflict remains UNKNOWN and keeps own text',
    () async {
      http.Request? write;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') {
          write = r;
          return ccResponse(null, status: 409);
        }
        return collectionResponse(collectionReceipt(revision: 7));
      });
      final c = collectionController(client);
      try {
        await c.openID(collectionID);
        c.setText(title: '保留修改');
        expect(c.dirty, true);
        expect(await saveCollectionUpdate(c), false);
        expect((jsonDecode(write!.body) as Map)['expectedRevision'], 7);
        expect(c.title, '保留修改');
        expect(c.editing!.revision, 7);
        expect(c.uncertain, true);
        expect(await c.save(), false);
        expect(await c.withdraw(), false);
        expect(c.controlPending.single.action, 'UPDATE');
      } finally {
        c.dispose();
        client.close();
      }
    },
  );
  test('分页续读失败保留已有页，重试去重并携当前身份', () async {
    var page = 0;
    final client = MockClient((r) async {
      expect(r.headers['Authorization'], 'Bearer owner');
      if (++page == 1)
        return collectionResponse({
          'items': [collectionReceipt()],
          'nextCursor': collectionID,
        });
      if (page == 2) throw http.ClientException('offline');
      expect(r.url.queryParameters['cursor'], collectionID);
      return collectionResponse({
        'items': [
          collectionReceipt(),
          collectionReceipt(id: collectionPlaceID),
        ],
      });
    });
    final c = collectionController(client);
    addTearDown(c.dispose);
    addTearDown(client.close);
    await c.load();
    await c.load(more: true);
    expect(c.items.length, 1);
    expect(c.nextCursor, collectionID);
    await c.load(more: true);
    expect(c.items.length, 2);
    expect(c.nextCursor, isNull);
  });
  test('回执owner不符不构成成功或可见数据', () async {
    final client = MockClient(
      (r) async =>
          collectionResponse(collectionReceipt(owner: collectionID), code: 201),
    );
    final c = collectionController(client);
    addTearDown(c.dispose);
    addTearDown(client.close);
    seedCollectionDraft(c);
    expect(await c.save(), isFalse);
    expect(c.editing, isNull);
    expect(c.items, isEmpty);
    expect(c.uncertain, isTrue);
  });
  test('已过期地点不可被当作当前地图锚点', () {
    final entry = PersonalCollectionEntry.fromJson({
      ...((collectionReceipt()['entries'] as List).first
          as Map<String, dynamic>),
      'sourceExpiresAt': DateTime.now()
          .toUtc()
          .subtract(const Duration(seconds: 1))
          .toIso8601String(),
    });
    expect(entry.sourceCurrent, isFalse);
  });
}

class CreateFaultStore extends MemoryCollectionCreatePendingStore {
  bool readFails = false, writeFails = false, removeFails = false;
  bool writeNotRetained = false;
  Completer<void>? preparedGate, preparedEntered;
  @override
  Future<CollectionCreatePending?> read(
    String origin,
    String owner,
    String kind,
    String city,
  ) {
    if (readFails) throw StateError('isolated read failure');
    return super.read(origin, owner, kind, city);
  }

  @override
  Future<void> write(
    String origin,
    String owner,
    CollectionCreatePending p,
  ) async {
    if (writeFails) throw StateError('isolated write failure');
    if (writeNotRetained) return;
    if (p.stage == 'PREPARED' && preparedGate != null) {
      preparedEntered?.complete();
      await preparedGate!.future;
    }
    await super.write(origin, owner, p);
  }

  @override
  Future<void> remove(
    String origin,
    String owner,
    CollectionCreatePending p,
  ) async {
    if (removeFails) throw StateError('isolated remove failure');
    await super.remove(origin, owner, p);
  }
}

CollectionCreatePending collectionCreatePrepared() =>
    CollectionCreatePending.prepared(
      '44444444-4444-4444-8444-444444444444',
      CollectionControlBody.normalize({
        'kind': 'journey',
        'cityId': 'aberdeen-gb',
        'title': '周末路线',
        'summary': '',
        'entries': [
          {
            'placeId': collectionPlaceID,
            'note': '',
            'locationVisibility': 'hidden',
          },
        ],
      }),
    );

void collectionCreateRecoveryCases() {
  test(
    'create recovery true cold secure reconstruction uses exact original GET no retained object or replay',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        return r.method == 'POST'
            ? collectionResponse(null, code: 503)
            : collectionResponse(collectionReceipt());
      });
      addTearDown(client.close);
      final old = collectionController(
        client,
        createStore: const SecureCollectionCreatePendingStore(),
      );
      seedCollectionDraft(old);
      expect(await old.save(), false);
      final operation = jsonDecode(calls.single.body)['operationId'];
      old.dispose();
      // Nothing from the old controller, payload, or host cache enters this one.
      final next = collectionController(
        client,
        token: () => 'Bearer fresh-session',
        createStore: const SecureCollectionCreatePendingStore(),
      );
      addTearDown(next.dispose);
      await next.loadCreatePending();
      expect(next.createPending!.operationID, operation);
      expect(next.uncertain, true);
      expect(next.title, isEmpty);
      expect(await next.save(), false);
      expect(await next.recover(), true);
      expect(calls.map((r) => r.method).toList(), ['POST', 'GET']);
      expect(
        calls.last.url.path,
        '/v1/me/personal-collections/operations/$operation',
      );
      expect(calls.last.headers['Authorization'], 'Bearer fresh-session');
      expect(calls.last.followRedirects, false);
      expect(next.editing!.id, collectionID);
      expect(next.uncertain, false);
      expect(await const FlutterSecureStorage().readAll(), isEmpty);
    },
  );
  test(
    'create recovery await durable PREPARED before first producer and never repeat UNKNOWN',
    () async {
      final store = CreateFaultStore()
        ..preparedGate = Completer<void>()
        ..preparedEntered = Completer<void>();
      var posts = 0;
      final client = MockClient((r) async {
        posts++;
        return collectionResponse(null, code: 500);
      });
      final c = collectionController(client, createStore: store);
      addTearDown(c.dispose);
      addTearDown(client.close);
      seedCollectionDraft(c);
      final save = c.save();
      await store.preparedEntered!.future;
      expect(posts, 0);
      expect(c.saving, true);
      store.preparedGate!.complete();
      expect(await save, false);
      expect(posts, 1);
      expect(store.values.values.single.stage, 'UNKNOWN');
      expect(await c.save(), false);
      expect(posts, 1);
    },
  );
  for (final phase in ['read', 'write', 'readback']) {
    test('create recovery $phase storage failure aborts first send', () async {
      final store = CreateFaultStore()
        ..readFails = phase == 'read'
        ..writeFails = phase == 'write'
        ..writeNotRetained = phase == 'readback';
      var posts = 0;
      final client = MockClient((r) async {
        posts++;
        return collectionResponse(collectionReceipt(), code: 201);
      });
      final c = collectionController(client, createStore: store);
      addTearDown(c.dispose);
      addTearDown(client.close);
      seedCollectionDraft(c);
      expect(await c.save(), false);
      expect(posts, 0);
      expect(c.createReadFailed, true);
      expect(c.editable, false);
    });
  }
  test(
    'create recovery identity retirement during durable preparation never dispatches and preserves PREPARED',
    () async {
      final store = CreateFaultStore()
        ..preparedGate = Completer<void>()
        ..preparedEntered = Completer<void>();
      var active = true, posts = 0;
      final changes = ChangeNotifier();
      final client = MockClient((r) async {
        posts++;
        return collectionResponse(collectionReceipt(), code: 201);
      });
      final c = collectionController(
        client,
        createStore: store,
        current: () => active,
        changes: changes,
      );
      addTearDown(c.dispose);
      addTearDown(client.close);
      addTearDown(changes.dispose);
      seedCollectionDraft(c);
      final save = c.save();
      await store.preparedEntered!.future;
      active = false;
      changes.notifyListeners();
      store.preparedGate!.complete();
      expect(await save, false);
      expect(posts, 0);
      expect(store.values.values.single.stage, 'PREPARED');
    },
  );
  for (final mismatch in [
    '404',
    '403',
    '500',
    '302',
    'owner',
    'city',
    'kind',
    'digest',
    'revision',
    'public',
    'delete-failure',
  ]) {
    test(
      'create recovery $mismatch retains selector without false adoption or producer',
      () async {
        final store = CreateFaultStore()
          ..removeFails = mismatch == 'delete-failure';
        final p = collectionCreatePrepared();
        await store.write('https://synthetic.invalid', collectionOwner, p);
        final calls = <http.Request>[];
        final client = MockClient((r) async {
          calls.add(r);
          final v = collectionReceipt();
          if (mismatch == 'owner') v['ownerAccountId'] = collectionID;
          if (mismatch == 'city') v['cityId'] = 'other-city';
          if (mismatch == 'kind') v['kind'] = 'theme_map';
          if (mismatch == 'digest') v['title'] = '已被另一个端修改';
          if (mismatch == 'revision') v['revision'] = 2;
          if (mismatch == 'public') v['visibility'] = 'public';
          return collectionResponse(v, code: int.tryParse(mismatch) ?? 200);
        });
        final c = collectionController(client, createStore: store);
        addTearDown(c.dispose);
        addTearDown(client.close);
        expect(await c.recoverCreate(), false);
        expect(calls.single.method, 'GET');
        expect(c.editing, isNull);
        expect(c.uncertain, true);
        expect(store.values.values.single.same(p), true);
        expect(await c.save(), false);
        expect(calls, hasLength(1));
      },
    );
  }
  for (final boundary in ['owner', 'token', 'source']) {
    test(
      'create recovery late $boundary ABA read cannot adopt or delete original pending',
      () async {
        final store = MemoryCollectionCreatePendingStore();
        await store.write(
          'https://synthetic.invalid',
          collectionOwner,
          collectionCreatePrepared(),
        );
        var owner = collectionOwner, token = 'Bearer old', active = true;
        final changes = ChangeNotifier(),
            entered = Completer<void>(),
            reply = Completer<http.Response>();
        final client = MockClient((r) async {
          entered.complete();
          return reply.future;
        });
        final c = collectionController(
          client,
          createStore: store,
          owner: () => owner,
          token: () => token,
          current: () => active,
          changes: changes,
        );
        addTearDown(c.dispose);
        addTearDown(client.close);
        addTearDown(changes.dispose);
        final reading = c.recoverCreate();
        await entered.future;
        if (boundary == 'owner') owner = collectionID;
        if (boundary == 'token') token = 'Bearer different';
        if (boundary == 'source') active = false;
        changes.notifyListeners();
        owner = collectionOwner;
        token = 'Bearer old';
        active = true;
        changes.notifyListeners();
        reply.complete(collectionResponse(collectionReceipt()));
        expect(await reading, false);
        expect(c.editing, isNull);
        expect(store.values, hasLength(1));
      },
    );
  }
}

class ControlFailReadbackStore extends MemoryCollectionControlPendingStore {
  bool failAfterPrepared = true;
  @override
  Future<List<CollectionControlPending>> read(
    String origin,
    String owner,
  ) async {
    if (failAfterPrepared && values.values.any((p) => p.stage == 'PREPARED')) {
      failAfterPrepared = false;
      throw StateError('synthetic disk read failure');
    }
    return super.read(origin, owner);
  }
}

void collection147ControllerCases() {
  for (final status in [401, 404, 409, 503]) {
    test(
      '147 submitted UPDATE HTTP $status keeps original op UNKNOWN no second producer',
      () async {
        final calls = <http.Request>[];
        final client = MockClient((r) async {
          calls.add(r);
          if (r.url.path.endsWith('/preview'))
            return ccResponse(ccPreview(ccRequest(r)));
          if (r.method == 'PUT') return ccResponse(null, status: status);
          if (r.url.path.startsWith(
            '/v1/me/personal-collection-controls/operations/',
          ))
            return ccResponse(null, status: 404);
          return collectionResponse(collectionReceipt(revision: 7));
        });
        final c = collectionController(client);
        try {
          await c.openID(collectionID);
          c.setText(title: '修改稿仍保留');
          final p = await c.prepareControl('UPDATE');
          expect(p, isNotNull);
          expect(await c.commitControl(p!), false);
          final original = c.controlPending.single;
          expect(await c.commitControl(p), false);
          expect(await c.prepareControl('UPDATE'), isNull);
          expect(await c.recoverControl(original), false);
          expect(c.controlPending.single.same(original), true);
          expect(c.title, '修改稿仍保留');
          expect(calls.where((r) => r.method == 'PUT'), hasLength(1));
          expect(
            calls.where((r) => r.url.path.endsWith('/preview')),
            hasLength(1),
          );
          expect(calls.where((r) => r.method == 'DELETE'), isEmpty);
        } finally {
          c.dispose();
          client.close();
        }
      },
    );
  }
  test(
    '147 original SUCCEEDED clears owned journal even current target GET403 remains unavailable',
    () async {
      var committed = false;
      final store = MemoryCollectionControlPendingStore();
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') {
          committed = true;
          return ccResponse(ccReceipt(ccRequest(r)));
        }
        if (committed) return ccResponse(null, status: 403);
        return collectionResponse(collectionReceipt(revision: 7));
      });
      final c = collectionController(client, controlStore: store);
      try {
        await c.openID(collectionID);
        c.setText(title: '本人已确认修改');
        expect(await saveCollectionUpdate(c), true);
        expect(c.lastControlReceipt!.succeeded, true);
        expect(store.values, isEmpty);
        expect(c.uncertain, false);
        expect(c.controlCurrentUnread, true);
        expect(c.editing!.id, collectionID);
        expect(c.editing!.revision, 8);
        expect(c.items, isEmpty);
        expect(c.draftEntries.single.placeName, isEmpty);
        expect(c.draftEntries.single.latitude, isNull);
        expect(c.draftEntries.single.sourceCurrent, false);
        expect(c.title, '本人已确认修改');
        expect(await c.prepareControl('UPDATE'), isNull);
      } finally {
        c.dispose();
        client.close();
      }
    },
  );
  test(
    '147 cold original STOP is bodyless retains own text and permits no automatic rebase',
    () async {
      final store = MemoryCollectionControlPendingStore();
      CollectionControlInput? input;
      final first = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') {
          input = ccRequest(r);
          throw http.ClientException('lost synthetic receipt');
        }
        return collectionResponse(collectionReceipt(revision: 7));
      });
      final old = collectionController(first, controlStore: store);
      await old.openID(collectionID);
      old.setText(title: '本人未确认稿');
      expect(await saveCollectionUpdate(old), false);
      final retained = old.retainedLocalDraft;
      old.dispose();
      first.close();
      final calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        expectSync(r.url.hasQuery, false);
        expectSync(r.bodyBytes, isEmpty);
        return ccResponse(ccReceipt(input!, stopped: true));
      });
      final c = collectionController(
        client,
        controlStore: store,
        retained: retained,
      );
      try {
        await c.loadControlPending();
        final original = c.controlPending.single;
        expect(await c.recoverControl(original, stop: true), true);
        expect(c.lastControlReceipt!.succeeded, false);
        expect(c.title, '本人未确认稿');
        expect(store.values, isEmpty);
        expect(calls, hasLength(1));
        expect(calls.single.method, 'POST');
        expect(
          calls.single.url.path.endsWith(
            '/operations/' + input!.operationID + '/stop',
          ),
          true,
        );
        expect(c.publicationVersionCurrent, false);
      } finally {
        c.dispose();
        client.close();
      }
    },
  );
  test(
    '147 PREPARED write then failed readback has no PUT and exact local repair unlocks preparation',
    () async {
      final store = ControlFailReadbackStore(), calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        return collectionResponse(collectionReceipt());
      });
      final c = collectionController(client, controlStore: store);
      try {
        await c.openID(collectionID);
        c.setText(title: '本机稿');
        final p = await c.prepareControl('UPDATE');
        expect(p, isNotNull);
        expect(await c.commitControl(p!), false);
        await c.loadControlPending();
        final pending = c.controlPending.single;
        expect(pending.stage, 'PREPARED');
        expect(calls.where((r) => r.method == 'PUT'), isEmpty);
        expect(await c.repairControlPrepared(pending), true);
        expect(store.values, isEmpty);
        expect(c.title, '本机稿');
        expect(await c.prepareControl('UPDATE'), isNotNull);
        expect(
          calls.where((r) => r.url.path.contains('/operations/')),
          isEmpty,
        );
      } finally {
        c.dispose();
        client.close();
      }
    },
  );
  test(
    '147 same owner renewal preserves text rejects old approval then new concrete preview works',
    () async {
      var header = 'Bearer old';
      final changes = ChangeNotifier(), calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        return collectionResponse(collectionReceipt(revision: 7));
      });
      final c = collectionController(
        client,
        token: () => header,
        changes: changes,
      );
      try {
        await c.openID(collectionID);
        c.setText(title: '续期保留本人稿');
        final old = await c.prepareControl('UPDATE');
        expect(old, isNotNull);
        header = 'Bearer renewed';
        changes.notifyListeners();
        await c.loadControlPending();
        expect(c.current, true);
        expect(c.title, '续期保留本人稿');
        expect(await c.commitControl(old!), false);
        final fresh = await c.prepareControl('UPDATE');
        expect(fresh, isNotNull);
        expect(fresh!.input.operationID, isNot(old.input.operationID));
        expect(calls.last.headers['authorization'], 'Bearer renewed');
        expect(calls.where((r) => r.method == 'PUT'), isEmpty);
      } finally {
        c.dispose();
        changes.dispose();
        client.close();
      }
    },
  );
  for (final axis in ['owner', 'source']) {
    test(
      '147 $axis ABA old confirmation cannot PUT or erase original owner journal',
      () async {
        var owner = collectionOwner, current = true;
        final changes = ChangeNotifier(),
            store = MemoryCollectionControlPendingStore();
        final calls = <http.Request>[];
        final client = MockClient((r) async {
          calls.add(r);
          if (r.url.path.endsWith('/preview'))
            return ccResponse(ccPreview(ccRequest(r)));
          return collectionResponse(collectionReceipt(revision: 7));
        });
        final c = collectionController(
          client,
          owner: () => owner,
          current: () => current,
          changes: changes,
          controlStore: store,
        );
        try {
          await c.openID(collectionID);
          c.setText(title: '旧来源本人稿');
          final old = await c.prepareControl('UPDATE');
          expect(old, isNotNull);
          if (axis == 'owner')
            owner = collectionID;
          else
            current = false;
          changes.notifyListeners();
          owner = collectionOwner;
          current = true;
          changes.notifyListeners();
          expect(c.current, false);
          expect(c.title, isEmpty);
          expect(await c.commitControl(old!), false);
          expect(calls.where((r) => r.method == 'PUT'), isEmpty);
        } finally {
          c.dispose();
          changes.dispose();
          client.close();
        }
      },
    );
  }
  test(
    '147 target ID ABA makes old approved draft unusable despite same object returning',
    () async {
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        return collectionResponse(
          collectionReceipt(
            id: r.url.path.endsWith(collectionPlaceID)
                ? collectionPlaceID
                : collectionID,
          ),
        );
      });
      final c = collectionController(client);
      try {
        await c.openID(collectionID);
        final old = await c.prepareControl('WITHDRAW');
        expect(old, isNotNull);
        await c.openID(collectionPlaceID);
        await c.openID(collectionID);
        expect(await c.commitControl(old!), false);
        expect(c.editing!.id, collectionID);
      } finally {
        c.dispose();
        client.close();
      }
    },
  );
}
