import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:birdtie_client/src/content/saved_collection_append.dart';
import 'personal_collection_controller_test.dart'
    show collectionReceipt, collectionResponse, collectionID;
import 'saved_search_test.dart';

class AppendJournal implements CollectionAppendJournal {
  Map<String, dynamic>? value;
  @override
  Future<Map<String, dynamic>?> read(String key) async => value;
  @override
  Future<Map<String, dynamic>> claim(
    String key,
    Map<String, dynamic> v,
  ) async => value ??= Map.unmodifiable(v);
  @override
  Future<void> clear(String key, String op) async {
    if (value?['operationId'] == op) value = null;
  }
}

class AppendFixture {
  AppendFixture({
    this.loseResponse = false,
    this.published = false,
    this.postStatus = 200,
    this.operationStatus = 200,
    this.stopped = false,
    this.publicationPending = false,
    this.badReceipt = false,
  }) {
    client = MockClient((r) async {
      calls.add(r);
      if (r.method == 'POST') {
        if (r.url.path.endsWith('/stop')) {
          stopBody = jsonDecode(r.body) as Map<String, dynamic>;
          return _result(
            r.url.pathSegments[r.url.pathSegments.length - 2],
            stop: stopped,
          );
        }
        body = jsonDecode(r.body) as Map<String, dynamic>;
        if (loseResponse) throw http.ClientException('lost');
        if (postStatus != 200) return http.Response('', postStatus);
        return _result(body!['operationId'] as String);
      }
      if (r.url.path.contains('/append-operations/'))
        return operationStatus == 200
            ? _result(r.url.pathSegments.last)
            : http.Response('', operationStatus);
      final wire = collectionReceipt(
        owner: searchOwner,
        status: published ? 'published' : 'draft',
      );
      if (published) wire['visibility'] = 'public';
      return r.url.queryParameters.containsKey('kind')
          ? collectionResponse({
              'items': r.url.queryParameters['kind'] == 'journey' ? [wire] : [],
              'nextCursor': null,
            })
          : collectionResponse(wire);
    });
    saved = searchController(client, owner: () => owner, token: () => token);
  }
  late MockClient client;
  late SavedController saved;
  final changes = ValueNotifier(0), journal = AppendJournal();
  final calls = <http.Request>[];
  bool loseResponse, published, current = true;
  final int postStatus, operationStatus;
  final bool stopped, publicationPending, badReceipt;
  String owner = searchOwner, token = 'Bearer A';
  Map<String, dynamic>? body;
  Map<String, dynamic>? stopBody;
  http.Response _result(String op, {bool stop = false}) => collectionResponse({
    'operationId': op,
    'kind': 'journey',
    'collectionId': badReceipt
        ? '44444444-4444-4444-8444-444444444444'
        : collectionID,
    'appliedRevision': stop ? 0 : 2,
    'status': stop ? 'STOPPED' : 'SUCCEEDED',
  });
  SavedCollectionAppendController controller({bool includeJourneys = true}) =>
      SavedCollectionAppendController(
        saved: saved,
        includeJourneys: includeJourneys,
        selected: [SavedItem.fromJson(searchRow(note: '私人备注不得入集合'))],
        ownerID: () => owner,
        sourceCurrent: () => current,
        identityChanges: changes,
        journal: journal,
        publicationRecoveryReader: (_, _) async => publicationPending
            ? {'operationId': '44444444-4444-4444-8444-444444444444'}
            : null,
      );
  Future<void> choose(SavedCollectionAppendController c) async {
    await c.load();
    await c.choose(c.collections['journey']!.items.single);
  }

  void dispose() {
    saved.dispose();
    client.close();
    changes.dispose();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test(
    'paused Journey is absent from picker; original uncertain operation remains recoverable',
    () async {
      final f = AppendFixture(loseResponse: true), original = f.controller();
      await f.choose(original);
      expect(await original.submit(), isFalse);
      final selector = Map.of(f.journal.value!);
      original.dispose();
      final hidden = f.controller(includeJourneys: false);
      expect(hidden.collections.keys, ['theme_map']);
      await hidden.load();
      expect(hidden.uncertain, isTrue);
      expect(f.journal.value, selector);
      expect(await hidden.recover(), isTrue);
      expect(f.calls.where((r) => r.method == 'POST'), hasLength(1));
      hidden.dispose();
      f.dispose();
    },
  );

  test('collectionAppend preview取消零写，去重且不复制Saved私人note', () async {
    final f = AppendFixture(), c = f.controller();
    await f.choose(c);
    expect(c.additions.single.targetId, searchPlace);
    c.back();
    expect(f.calls.where((r) => r.method != 'GET'), isEmpty);
    expect(f.journal.value, isNull);
    c.dispose();
    f.dispose();
  });
  test('collectionAppend 一次CAS原operation提交，hidden位置与空备注', () async {
    final f = AppendFixture(), c = f.controller();
    await f.choose(c);
    expect(await c.submit(), true);
    expect(await c.submit(), false);
    expect(f.calls.where((r) => r.method == 'POST'), hasLength(1));
    expect(f.body!['expectedRevision'], 1);
    expect(f.body!['cityId'], 'aberdeen-gb');
    expect(f.body!['entries'], [
      {'placeId': searchPlace, 'note': '', 'locationVisibility': 'hidden'},
    ]);
    expect(f.body!.containsKey('kind'), false);
    expect(c.succeeded, true);
    expect(f.journal.value, isNull);
    c.dispose();
    f.dispose();
  });
  test('collectionAppend 丢响应仅核原回执，重启也不POST重发', () async {
    final f = AppendFixture(loseResponse: true), first = f.controller();
    await f.choose(first);
    expect(await first.submit(), false);
    final operation = f.journal.value!['operationId'];
    expect(first.uncertain, true);
    first.dispose();
    final restored = f.controller();
    await restored.load();
    expect(restored.uncertain, true);
    expect(await restored.submit(), false);
    expect(await restored.recover(), true);
    expect(f.calls.where((r) => r.method == 'POST'), hasLength(1));
    expect(
      f.calls.last.url.path.endsWith('/append-operations/$operation'),
      true,
    );
    restored.dispose();
    f.dispose();
  });
  test('collectionAppend 已有pending抢占另一选择，零新POST', () async {
    final f = AppendFixture(), c = f.controller();
    await f.choose(c);
    f.journal.value = {
      'operationId': '44444444-4444-4444-8444-444444444444',
      'kind': 'journey',
      'collectionId': collectionID,
      'cityId': 'aberdeen-gb',
      'expectedRevision': 1,
    };
    expect(await c.submit(), false);
    expect(c.uncertain, true);
    expect(f.calls.where((r) => r.method == 'POST'), isEmpty);
    c.dispose();
    f.dispose();
  });
  test('collectionAppend 主体ABA永久退休且不借新批准', () async {
    final f = AppendFixture(), c = f.controller();
    await f.choose(c);
    f.owner = searchPlace;
    f.changes.value++;
    f.owner = searchOwner;
    f.changes.value++;
    expect(c.current, false);
    expect(await c.submit(), false);
    expect(f.calls.where((r) => r.method == 'POST'), isEmpty);
    c.dispose();
    f.dispose();
  });
  test('collectionAppend 普通同主体认证续期保留具体版本选择', () async {
    final f = AppendFixture(), c = f.controller();
    await f.choose(c);
    f.token = 'Bearer renewed';
    f.changes.value++;
    expect(c.current, true);
    expect(c.target?.id, collectionID);
    expect(await c.submit(), true);
    expect(f.calls.last.headers['Authorization'], 'Bearer renewed');
    c.dispose();
    f.dispose();
  });
  test('collectionAppend 已公开集合不被当可编辑草稿', () async {
    final f = AppendFixture(published: true), c = f.controller();
    await f.choose(c);
    expect(c.target, isNull);
    expect(await c.submit(), false);
    expect(f.calls.where((r) => r.method == 'POST'), isEmpty);
    c.dispose();
    f.dispose();
  });
  test('collectionAppend 原公开UNKNOWN阻止选择，不绕过原发布门槛', () async {
    final f = AppendFixture(publicationPending: true), c = f.controller();
    await f.choose(c);
    expect(c.target, isNull);
    expect(c.error, contains('公开操作待核实'));
    expect(await c.submit(), false);
    expect(f.calls.where((r) => r.method == 'POST'), isEmpty);
    c.dispose();
    f.dispose();
  });
  test('collectionAppend 提交后401仍保留原journal，结束读取原成功不移除', () async {
    final f = AppendFixture(postStatus: 401), c = f.controller();
    await f.choose(c);
    expect(await c.submit(), false);
    final op = f.journal.value!['operationId'];
    expect(c.uncertain, true);
    expect(await c.stop(), true);
    expect(c.succeeded, true);
    expect(f.calls.last.url.path.endsWith('/$op/stop'), true);
    expect(f.stopBody, {'cityId': 'aberdeen-gb', 'expectedRevision': 1});
    expect(f.journal.value, isNull);
    expect(
      f.calls.where((r) => r.url.path.endsWith('/append-places')),
      hasLength(1),
    );
    c.dispose();
    f.dispose();
  });
  test('collectionAppend 原GET404不判失败，STOPPED解除原阻塞并刷新选择', () async {
    final f = AppendFixture(
          loseResponse: true,
          operationStatus: 404,
          stopped: true,
        ),
        c = f.controller();
    await f.choose(c);
    expect(await c.submit(), false);
    c.dispose();
    final restored = f.controller();
    await restored.load();
    expect(restored.recoveryTitle, '周末路线');
    expect(await restored.recover(), false);
    expect(restored.uncertain, true);
    expect(f.journal.value, isNotNull);
    expect(await restored.stop(), true);
    expect(restored.succeeded, false);
    expect(restored.uncertain, false);
    expect(restored.collections['journey']!.items, hasLength(1));
    expect(restored.notice, contains('原操作已结束'));
    expect(
      f.calls.where((r) => r.url.path.endsWith('/append-places')),
      hasLength(1),
    );
    restored.dispose();
    f.dispose();
  });
  test('collectionAppend 不同目标的成功回执不能清除原UNKNOWN', () async {
    final f = AppendFixture(badReceipt: true), c = f.controller();
    await f.choose(c);
    expect(await c.submit(), false);
    expect(await c.recover(), false);
    expect(c.uncertain, true);
    expect(f.journal.value, isNotNull);
    c.dispose();
    f.dispose();
  });
  testWidgets('collectionAppend 正常窄屏预览需要明确勾选，取消无写', (tester) async {
    final f = AppendFixture(), c = f.controller();
    await tester.binding.setSurfaceSize(const Size(390, 844));
    await tester.pumpWidget(
      MaterialApp(home: SavedCollectionAppendPage(controller: c)),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('周末路线'));
    await tester.pumpAndSettle();
    final button = tester.widget<FilledButton>(
      find.widgetWithText(FilledButton, '确认加入'),
    );
    expect(button.onPressed, isNull);
    expect(find.textContaining('不复制私人收藏备注'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.tap(find.text('返回选择'));
    await tester.pumpAndSettle();
    expect(f.calls.where((r) => r.method == 'POST'), isEmpty);
    await tester.pumpWidget(const SizedBox());
    c.dispose();
    f.dispose();
    await tester.binding.setSurfaceSize(null);
  });
}
