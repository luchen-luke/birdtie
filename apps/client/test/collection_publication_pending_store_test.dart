import 'dart:convert';
import 'dart:async';
import 'package:birdtie_client/src/content/collection_publication_pending_store.dart';
import 'package:birdtie_client/src/content/collection_publication_controller.dart';
import 'package:birdtie_client/src/content/personal_collection_controller.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'personal_collection_controller_test.dart'
    show
        collectionOwner,
        collectionID,
        collectionReceipt,
        collectionController,
        collectionResponse;
import 'collection_publication_controller_test.dart' show operationRecord;

CollectionPublicationPendingStore store({
  String owner = collectionOwner,
  String origin = 'https://synthetic.invalid',
}) => CollectionPublicationPendingStore(
  origin: origin,
  owner: owner,
  kind: 'journey',
  id: collectionID,
);

class DelayedStore extends CollectionPublicationPendingStore {
  DelayedStore()
    : super(
        origin: 'https://synthetic.invalid',
        owner: collectionOwner,
        kind: 'journey',
        id: collectionID,
      );
  final completed = Completer<void>();
  Map<String, dynamic>? saved;
  @override
  Future<void> save(Map<String, dynamic> value) async {
    await completed.future;
    saved = Map.of(value);
  }

  @override
  Future<Map<String, dynamic>?> load() async => saved;
  @override
  Future<void> clear() async {
    saved = null;
  }

  @override
  Future<void> clearIfOperation(String operationID) async {
    if (saved?['operationId'] == operationID) saved = null;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test('原selector不能被新操作覆盖，过期exact-clear不会删掉新selector', () async {
    final a = store(), b = store();
    final old = <String, dynamic>{
      'id': collectionID,
      'kind': 'journey',
      'operationId': collectionID,
      'revision': 1,
      'action': 'PUBLISH',
    };
    final next = {...old, 'operationId': collectionOwner};
    await a.save(old);
    await expectLater(b.save(next), throwsStateError);
    expect((await b.load())!['operationId'], collectionID);
    await a.clearIfOperation(collectionID);
    await b.save(next);
    await a.clearIfOperation(collectionID);
    expect((await b.load())!['operationId'], collectionOwner);
  });
  test('并发母页失去reserve后保留原持久selector且零重发', () async {
    var posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        throw http.ClientException('lost');
      }
      return collectionResponse(collectionReceipt());
    });
    final a = collectionController(client), b = collectionController(client);
    await a.open(PersonalCollection.fromJson(collectionReceipt()));
    await b.open(PersonalCollection.fromJson(collectionReceipt()));
    CollectionPublicationController controller(
      PersonalCollectionController s,
    ) => CollectionPublicationController(
      source: s,
      authorizationHeader: s.authorizationHeader,
      ownerID: s.ownerID,
      client: client,
      apiBaseUrl: s.apiBaseUrl,
      pendingStore: store(),
    );
    final pa = controller(a), pb = controller(b);
    try {
      await pa.restorePending();
      await pb.restorePending();
      expect(await pa.publish(), false);
      final original = a.publicationPending!['operationId'];
      expect(await pb.publish(), false);
      expect(posts, 1);
      expect(b.publicationPending!['operationId'], original);
      expect(b.editable, false);
      expect((await store().load())!['operationId'], original);
    } finally {
      pa.dispose();
      pb.dispose();
      a.dispose();
      b.dispose();
      client.close();
    }
  });
  test('未知提交记录可跨控制器重建，恢复只有原操作GET', () async {
    var writes = 0;
    Map<String, dynamic>? pending;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        writes++;
        throw http.ClientException('lost');
      }
      if (r.url.path.contains('publication-operations'))
        return collectionResponse(operationRecord(pending!));
      return collectionResponse(collectionReceipt());
    });
    final first = collectionController(client);
    await first.open(PersonalCollection.fromJson(collectionReceipt()));
    final p = CollectionPublicationController(
      source: first,
      authorizationHeader: first.authorizationHeader,
      ownerID: first.ownerID,
      client: client,
      apiBaseUrl: first.apiBaseUrl,
      pendingStore: store(),
    );
    expect(p.canPublish, false);
    await p.restorePending();
    expect(p.canPublish, true);
    expect(await p.publish(), false);
    pending = await store().load();
    expect(pending!.containsKey('Authorization'), false);
    expect(pending.containsKey('entries'), false);
    p.dispose();
    first.dispose();
    final second = collectionController(client);
    await second.open(PersonalCollection.fromJson(collectionReceipt()));
    final next = CollectionPublicationController(
      source: second,
      authorizationHeader: second.authorizationHeader,
      ownerID: second.ownerID,
      client: client,
      apiBaseUrl: second.apiBaseUrl,
      pendingStore: store(),
    );
    await next.restorePending();
    expect(next.uncertain, true);
    expect(await next.publish(), false);
    expect(await next.recover(), true);
    expect(writes, 1);
    await Future<void>.delayed(Duration.zero);
    expect(await store().load(), null);
    next.dispose();
    second.dispose();
    client.close();
  });
  test('恢复selector按原owner和origin隔离，不保存Session或内容', () async {
    final s = store();
    await s.save({
      'id': collectionID,
      'kind': 'journey',
      'operationId': collectionID,
      'revision': 1,
      'action': 'PUBLISH',
    });
    expect(await store(owner: collectionID).load(), null);
    expect(await store(origin: 'https://other.invalid').load(), null);
    final raw = await const FlutterSecureStorage().read(key: s.key);
    expect((jsonDecode(raw!) as Map).keys.toSet(), {
      'origin',
      'owner',
      'kind',
      'id',
      'operationId',
      'revision',
      'action',
    });
  });

  test('普通母页冷启动打开原对象即退休新写，不必先打开公开页', () async {
    final s = store();
    await s.save({
      'id': collectionID,
      'kind': 'journey',
      'operationId': collectionID,
      'revision': 1,
      'action': 'PUBLISH',
    });
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      return collectionResponse(collectionReceipt());
    });
    final c = PersonalCollectionController(
      kind: 'journey',
      cityID: 'aberdeen-gb',
      authorizationHeader: () => 'Bearer owner',
      ownerID: () => collectionOwner,
      client: client,
      apiBaseUrl: s.origin,
      publicationRecoveryReader: (_) => s.load(),
    );
    await c.open(PersonalCollection.fromJson(collectionReceipt()));
    expect(c.editable, false);
    c.setText(title: '不得修改');
    expect(c.title, '周末路线');
    expect(await c.save(), false);
    expect(await c.withdraw(), false);
    c.newDraft();
    expect(c.editing!.id, collectionID);
    expect(writes, 0);
    c.dispose();
    client.close();
  });

  test('保存恢复selector途中返回，明确0HTTP则清理同操作而非永久UNKNOWN', () async {
    final s = DelayedStore();
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      return collectionResponse(collectionReceipt());
    });
    final c = collectionController(client);
    await c.open(PersonalCollection.fromJson(collectionReceipt()));
    final p = CollectionPublicationController(
      source: c,
      authorizationHeader: c.authorizationHeader,
      ownerID: c.ownerID,
      client: client,
      apiBaseUrl: c.apiBaseUrl,
      pendingStore: s,
    );
    await p.restorePending();
    final result = p.publish();
    expect(c.editable, false);
    p.dispose();
    s.completed.complete();
    expect(await result, false);
    expect(writes, 0);
    expect(s.saved, null);
    expect(c.publicationPending, null);
    expect(c.editable, true);
    c.dispose();
    client.close();
  });
  test('损坏或外加授权字段的本机记录不能恢复成批准', () async {
    final s = store();
    await const FlutterSecureStorage().write(
      key: s.key,
      value: jsonEncode({
        'origin': s.origin,
        'owner': s.owner,
        'kind': s.kind,
        'id': s.id,
        'operationId': collectionID,
        'revision': 1,
        'action': 'PUBLISH',
        'approved': true,
      }),
    );
    expect(s.load(), throwsFormatException);
  });
}
