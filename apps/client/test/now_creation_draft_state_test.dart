import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/now_creation_draft.dart';
import 'package:birdtie_client/src/workspace/now_creation_draft_state.dart';
import 'package:birdtie_client/src/workspace/typed_local_draft.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

import 'typed_local_draft_test.dart' show DraftMemoryStore;

const _origin = 'https://creation-material.test', _owner = 'person-a';
NowCreationDraft _draft({
  String city = 'alpha',
  String kind = 'journey',
  String body = '本人旅程正文\n保留原文 🌿',
}) => NowCreationDraft(
  kind: kind,
  cityID: city,
  cityName: '本人选择的城市',
  title: '本人标题',
  body: body,
  placeID: 'place-b',
  placeName: '第二个地点',
  startsAt: DateTime.utc(2026, 10, 17, 9),
  endsAt: DateTime.utc(2026, 10, 17, 11),
  orderedPlaceIDs: const ['place-a', 'place-b'],
);

class _FailingStore extends DraftMemoryStore {
  @override
  Future<void> save(
    String origin,
    String owner,
    List<TypedLocalDraft> drafts,
  ) => Future.error(StateError('synthetic local storage failure'));
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('本人材料通过原SecureStore恢复，标题正文UTC日期与地点顺序完整', () async {
    FlutterSecureStorage.setMockInitialValues({});
    final first = NowCreationDraftState()..configure(_origin, _owner);
    first.remember(_draft());
    await first.flush();
    expect(first.storageError, isNull);
    first.dispose();
    final next = NowCreationDraftState()..configure(_origin, _owner);
    final restored = await next.restore('alpha', 'journey');
    expect(restored!.toJson(), _draft().toJson());
    final rows = await SecureTypedLocalDraftStore().read(_origin, _owner);
    expect(rows.single.kind, 'journey');
    expect(rows.single.slotID, NowCreationDraftState.slot('alpha'));
    expect(rows.single.attachments, isEmpty);
    expect(rows.single.references, isEmpty);
    expect(rows.single.taskID, isNull);
    expect(rows.single.audience, 'private');
    next.dispose();
  });

  test('同账号不同origin城市与类型各自恢复，不覆盖原Now输入槽', () async {
    final store = DraftMemoryStore();
    await store.save(_origin, _owner, [
      TypedLocalDraft(
        owner: _owner,
        origin: _origin,
        kind: 'intent',
        text: '原Now未发送内容',
      ),
    ]);
    final state = NowCreationDraftState(store: store)
      ..configure(_origin, _owner);
    state.remember(_draft(body: '甲城旅程'));
    state.remember(_draft(city: 'beta', body: '乙城旅程'));
    state.remember(_draft(kind: 'theme_map', body: '甲城地图'));
    await state.flush();
    state.dispose();
    final restored = NowCreationDraftState(store: store)
      ..configure(_origin, _owner);
    expect((await restored.restore('alpha', 'journey'))!.body, '甲城旅程');
    expect((await restored.restore('beta', 'journey'))!.body, '乙城旅程');
    expect((await restored.restore('alpha', 'theme_map'))!.body, '甲城地图');
    expect(
      (await store.read(
        _origin,
        _owner,
      )).singleWhere((d) => d.kind == 'intent').text,
      '原Now未发送内容',
    );
    restored.configure('https://other-origin.test', _owner);
    expect(await restored.restore('alpha', 'journey'), isNull);
    restored.configure(_origin, _owner);
    expect((await restored.restore('alpha', 'journey'))!.body, '甲城旅程');
    restored.dispose();
  });

  test('迟到读取不能覆盖用户新编辑，也不能跨注销换人ABA复活旧草稿', () async {
    final store = DraftMemoryStore()
      ..pending = Completer<List<TypedLocalDraft>>();
    final state = NowCreationDraftState(store: store)
      ..configure(_origin, _owner);
    final read = state.restore('alpha', 'journey');
    state.remember(_draft(body: '读取期间本人新编辑'));
    store.pending!.complete([
      TypedLocalDraft(
        owner: _owner,
        origin: _origin,
        kind: 'journey',
        text: jsonEncode(_draft(body: '过时内容').toJson()),
        slotID: NowCreationDraftState.slot('alpha'),
      ),
    ]);
    expect((await read)!.body, '读取期间本人新编辑');
    store.pending = Completer<List<TypedLocalDraft>>();
    final late = state.restore('beta', 'journey');
    state.configure(_origin, 'person-b');
    state.configure(_origin, _owner);
    store.pending!.complete([
      TypedLocalDraft(
        owner: _owner,
        origin: _origin,
        kind: 'journey',
        text: jsonEncode(_draft(city: 'beta').toJson()),
        slotID: NowCreationDraftState.slot('beta'),
      ),
    ]);
    expect(await late, isNull);
    store.pending = null;
    expect(await state.restore('alpha', 'journey'), isNull);
    state.dispose();
  });

  test('认证恢复暂挂后续同一owner保留材料，真正注销清原namespace', () async {
    final store = DraftMemoryStore();
    final state = NowCreationDraftState(store: store)
      ..configure(_origin, _owner);
    state.remember(_draft());
    await state.flush();
    state.configure(_origin, null, rejectedOwner: _owner);
    expect(state.suspended, isTrue);
    expect(await state.restore('alpha', 'journey'), isNull);
    state.configure(_origin, _owner);
    expect((await state.restore('alpha', 'journey'))!.body, _draft().body);
    state.configure(_origin, null);
    await Future<void>.delayed(Duration.zero);
    expect(await store.read(_origin, _owner), isEmpty);
    state.dispose();
  });

  test('损坏或带批准字段的恢复材料拒绝，保存失败仍保留本人当前文字', () async {
    expect(
      () => NowCreationDraft.fromJson({..._draft().toJson(), 'approved': true}),
      throwsFormatException,
    );
    expect(
      () => NowCreationDraft.fromJson({
        ..._draft().toJson(),
        'orderedPlaceIds': ['place-a', 'place-a'],
      }),
      throwsFormatException,
    );
    final store = _FailingStore();
    final state = NowCreationDraftState(store: store)
      ..configure(_origin, _owner);
    state.remember(_draft());
    await state.flush();
    expect(state.storageError, contains('尚未保存'));
    expect((await state.restore('alpha', 'journey'))!.body, _draft().body);
    state.dispose();
  });
}
