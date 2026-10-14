import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/route_draft.dart';
import 'package:birdtie_client/src/workspace/typed_local_draft.dart';
import 'package:birdtie_client/src/workspace/now_composer_draft_state.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'saved_search_test.dart'
    show searchController, searchPage, searchRow, searchOwner, searchPlace;
import 'typed_local_draft_test.dart' show DraftMemoryStore;

const routeOrigin = 'https://saved.test';
void main() {
  test(
    'selected stable day survives reorder restart and source expiry without auto map or input-ID matching',
    () async {
      var requests = 0;
      final client = MockClient((_) async {
            requests++;
            return searchPage([]);
          }),
          saved = searchController(client),
          boundary = ValueNotifier(0),
          store = DraftMemoryStore();
      saved.items = [SavedItem.fromJson(searchRow(point: true))];
      final initial = TypedLocalDraft(
        owner: searchOwner,
        origin: routeOrigin,
        kind: 'intent',
        text: '',
        taskID: 'original-task',
        references: const [
          LocalDraftReference(kind: 'person', id: 'original-context'),
        ],
        attachments: const [
          LocalDraftAttachment(
            id: 'historical',
            kind: 'image',
            state: 'unknown',
          ),
        ],
      );
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: store,
        initial: initial.converted('route'),
      );
      await c.restore();
      c.edit(
        LocalRouteValue.importText(
          jsonEncode({
            'days': [
              {},
              {
                'stops': [
                  {'name': '输入名称', 'placeId': searchPlace},
                ],
              },
            ],
          }),
        ),
      );
      expect(c.points(), isEmpty);
      expect(
        c.envelope.references.where((r) => r.purpose == 'route_stop'),
        isEmpty,
      );
      c.selectDay(1);
      final dayID = c.value.currentDayID;
      c.add(searchPlace);
      expect(c.points()!.single.targetId, searchPlace);
      c.edit(c.value.edit(days: c.value.days.reversed.toList()));
      expect(c.selectedDay, 0);
      expect(c.value.currentDayID, dayID);
      await c.flush();
      final revision = c.envelope.revision, id = c.envelope.id;
      c.dispose();
      final restored = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: store,
      );
      await restored.restore();
      expect(restored.value.currentDayID, dayID);
      expect(restored.envelope.id, id);
      expect(restored.envelope.revision, revision);
      expect(restored.envelope.taskID, 'original-task');
      expect(restored.envelope.attachments.single.state, 'unknown');
      saved.items = const [];
      boundary.value++;
      expect(restored.points(), isNull);
      expect(restored.value.currentDayID, dayID);
      expect(requests, 0);
      restored.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  test(
    'route typed conversion retains reference context and historical attachment across edits and restart',
    () async {
      final store = DraftMemoryStore(), boundary = ValueNotifier(0);
      var requests = 0;
      final client = MockClient((_) async {
        requests++;
        return searchPage([]);
      });
      final saved = searchController(client);
      final initial = TypedLocalDraft(
        owner: searchOwner,
        origin: routeOrigin,
        kind: 'message',
        text: '本人行程文字',
        conversationID: 'actual-conversation',
        recipientID: 'actual-recipient',
        audience: 'recipient',
        references: const [
          LocalDraftReference(
            kind: 'person',
            id: 'referenced-person',
            mode: 'snapshot',
            version: 'v1',
            snapshotText: '本人注释',
          ),
        ],
        attachments: const [
          LocalDraftAttachment(
            id: 'original-upload',
            kind: 'image',
            state: 'unknown',
          ),
        ],
      );
      final converted = initial.converted('route');
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: store,
        initial: converted,
      );
      await c.restore();
      expect(c.envelope.id, initial.id);
      expect(c.envelope.recipientID, isNull);
      expect(c.envelope.references.single.snapshotText, '本人注释');
      c.edit(c.value.edit(label: '明确修改后的行程'));
      await c.flush();
      final revision = c.envelope.revision;
      c.dispose();
      final restored = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: store,
      );
      await restored.restore();
      expect(restored.envelope.id, initial.id);
      expect(restored.envelope.revision, revision);
      expect(restored.value.label, '明确修改后的行程');
      expect(restored.envelope.references.single.id, 'referenced-person');
      expect(restored.envelope.attachments.single.state, 'unknown');
      expect(requests, 0);
      restored.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  for (final useStored in [false, true]) {
    test(
      'route typed conflicting conversion waits for explicit ${useStored ? 'restore old' : 'keep new'} choice without silently replacing material',
      () async {
        final store = DraftMemoryStore(), boundary = ValueNotifier(0);
        final client = MockClient((_) async => searchPage([])),
            saved = searchController(client);
        final old = TypedLocalDraft(
          owner: searchOwner,
          origin: routeOrigin,
          kind: 'route',
          slotID: 'route:aberdeen-gb',
          text: jsonEncode(LocalRouteValue(label: '已保存原计划').toJson()),
        );
        await store.save(routeOrigin, searchOwner, [old]);
        final next = TypedLocalDraft(
          owner: searchOwner,
          origin: routeOrigin,
          kind: 'intent',
          text: '新的本人文字',
        ).converted('route');
        final c = RouteDraftController(
          origin: routeOrigin,
          owner: searchOwner,
          cityID: 'aberdeen-gb',
          saved: saved,
          boundaryChanges: boundary,
          sourceCurrent: () => true,
          store: store,
          initial: next,
        );
        await c.restore();
        expect(c.restoreChoicePending, true);
        c.edit(c.value.edit(label: '不能在选择前覆盖'));
        await c.flush();
        expect((await store.read(routeOrigin, searchOwner)).single.id, old.id);
        c.chooseRestoredDraft(useStored);
        await c.flush();
        expect(c.restoreChoicePending, false);
        expect(c.envelope.id, useStored ? old.id : next.id);
        expect(
          (await store.read(routeOrigin, searchOwner)).single.id,
          useStored ? old.id : next.id,
        );
        c.dispose();
        saved.dispose();
        client.close();
        boundary.dispose();
      },
    );
  }
  test(
    'route old pending first read preserves edit and requires explicit choice before saving',
    () async {
      final store = DraftMemoryStore()
        ..pending = Completer<List<TypedLocalDraft>>();
      final client = MockClient((_) async => searchPage([])),
          saved = searchController(client),
          boundary = ValueNotifier(0);
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: store,
      );
      final restoring = c.restore();
      c.edit(c.value.edit(label: '新编辑已保存'));
      await c.flush();
      expect(store.rows, isEmpty);
      final revision = c.envelope.revision, version = c.envelope.contentVersion;
      store.pending!.complete([
        TypedLocalDraft(
          owner: searchOwner,
          origin: routeOrigin,
          kind: 'route',
          slotID: 'route:aberdeen-gb',
          text: jsonEncode(LocalRouteValue(label: '迟到的旧快照').toJson()),
        ),
      ]);
      await restoring;
      expect(c.value.label, '新编辑已保存');
      expect(c.envelope.revision, revision);
      expect(c.envelope.contentVersion, version);
      expect(c.restoreChoicePending, true);
      expect(store.rows, isEmpty);
      c.chooseRestoredDraft(false);
      await c.flush();
      expect(store.rows.values.single.single.text, contains('新编辑已保存'));
      c.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  test(
    'route typed delayed existing namespace cannot be overwritten by pre-read edit or auto-save',
    () async {
      final store = DraftMemoryStore(), boundary = ValueNotifier(0);
      final client = MockClient((_) async => searchPage([])),
          saved = searchController(client);
      final old = TypedLocalDraft(
        owner: searchOwner,
        origin: routeOrigin,
        kind: 'route',
        slotID: 'route:aberdeen-gb',
        text: jsonEncode(LocalRouteValue(label: '已保存原计划').toJson()),
      );
      await store.save(routeOrigin, searchOwner, [old]);
      store.pending = Completer<List<TypedLocalDraft>>();
      final incoming = TypedLocalDraft(
        owner: searchOwner,
        origin: routeOrigin,
        kind: 'intent',
        text: '新的本人转换文字',
        references: const [
          LocalDraftReference(kind: 'organization', id: 'own-context'),
        ],
      ).converted('route');
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: store,
        initial: incoming,
      );
      final reading = c.restore();
      c.edit(c.value.edit(label: '首读期间本人的新编辑'));
      final ownVersion = c.envelope.contentVersion;
      await c.flush();
      expect(c.localNamespaceReadComplete, false);
      expect(store.rows.values.single.single.id, old.id);
      expect(store.rows.values.single.single.text, old.text);
      store.pending!.complete([old]);
      await reading;
      expect(c.restoreChoicePending, true);
      expect(c.value.label, '首读期间本人的新编辑');
      expect(c.envelope.contentVersion, ownVersion);
      await c.flush();
      expect(store.rows.values.single.single.id, old.id);
      c.chooseRestoredDraft(false);
      await c.flush();
      expect(store.rows.values.single.single.id, incoming.id);
      expect(
        store.rows.values.single.single.references.single.id,
        'own-context',
      );
      c.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  test(
    'route rejected same-owner session retains only private material; explicit account replacement cannot persist it',
    () async {
      final store = DraftMemoryStore(),
          client = MockClient((_) async => searchPage([])),
          saved = searchController(client),
          boundary = ValueNotifier(0);
      var source = true, ownerCurrent = true;
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => source,
        localOwnerCurrent: () => ownerCurrent,
        store: store,
      );
      c.edit(c.value.edit(label: '续期前的私人编辑'));
      source = false;
      boundary.value++;
      await Future<void>.value();
      await c.flush();
      expect(c.current, false);
      expect(store.rows.values.single.single.text, contains('续期前的私人编辑'));
      ownerCurrent = false;
      store.rows.clear();
      await c.flush();
      expect(store.rows, isEmpty);
      c.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  test(
    'route import preserves supplied instructions as unresolved text and never fabricates places or geometry',
    () {
      const raw = '第1天\n酒店名字未知\n发信、订票、公开发布\nDay 2\n地址未核对';
      final v = LocalRouteValue.importText(raw);
      expect(v.days.map((d) => d.label), ['第1天', 'Day 2']);
      expect(v.days.first.text, contains('发信、订票、公开发布'));
      expect(v.days.every((d) => d.places.isEmpty), true);
      expect(v.sourceText, raw);
      expect(v.sourceDigest, hasLength(64));
      final copy = LocalRouteValue.fromJson(v.toJson());
      expect(copy.days.first.id, v.days.first.id);
      for (final key in ['geometry', 'travelTime', 'execute', 'approval']) {
        expect(
          () => LocalRouteValue.fromJson({...v.toJson(), key: true}),
          throwsFormatException,
        );
      }
      expect(
        () => LocalRouteValue.importText(
          List.generate(15, (i) => 'Day ${i + 1}\n未核对').join('\n'),
        ),
        throwsFormatException,
      );
      expect(LocalRouteValue.importText('第1天\n第2天').days, hasLength(2));
    },
  );
  test(
    'route slots actual secure merge preserve Now text and other city; external conversion retains same local ID',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final store = SecureTypedLocalDraftStore();
      final now = TypedLocalDraft(
        owner: searchOwner,
        origin: routeOrigin,
        kind: 'intent',
        text: '原Now输入',
      );
      final a = TypedLocalDraft(
        owner: searchOwner,
        origin: routeOrigin,
        kind: 'route',
        slotID: 'route:aberdeen-gb',
        text: jsonEncode(LocalRouteValue().toJson()),
      );
      final b = TypedLocalDraft(
        owner: searchOwner,
        origin: routeOrigin,
        kind: 'route',
        slotID: 'route:other',
        text: jsonEncode(LocalRouteValue().toJson()),
      );
      await store.save(routeOrigin, searchOwner, [now, a, b]);
      final external = a.edit(
        kind: 'external_plan',
        text: jsonEncode(LocalRouteValue.importText('Day 1\n待选择地点').toJson()),
      );
      await store.save(routeOrigin, searchOwner, [external]);
      final rows = await store.read(routeOrigin, searchOwner);
      expect(rows, hasLength(3));
      expect(rows.singleWhere((r) => r.slotID == 'route:aberdeen-gb').id, a.id);
      expect(
        rows.singleWhere((r) => r.slotID == 'route:aberdeen-gb').kind,
        'external_plan',
      );
      final composer = NowComposerDraftState(store: store)
        ..observeOwner(searchOwner);
      composer.configurePersistence(routeOrigin);
      await composer.restore();
      expect(composer.value.text, '原Now输入');
      expect(composer.envelope!.id, now.id);
      composer.dispose();
      final old = now.toJson()..remove('slotId');
      expect(TypedLocalDraft.fromJson(old).slotID, 'primary');
    },
  );
  test(
    'route ordered current selectors, explicit start, unavailable source and private note boundaries',
    () async {
      final client = MockClient((_) async => searchPage([])),
          saved = searchController(client),
          boundary = ValueNotifier(0);
      const second = '22222222-2222-4222-8222-222222222223';
      saved.items = [
        SavedItem.fromJson(searchRow(point: true, note: '不能复制的本人备注')),
        SavedItem.fromJson({
          ...searchRow(point: true, title: '第二地点'),
          'id': '33333333-3333-4333-8333-333333333334',
          'targetId': second,
        }),
      ];
      var source = true;
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => source,
        store: DraftMemoryStore(),
      );
      c.add(searchPlace);
      c.add(second);
      c.add(searchPlace);
      expect(c.value.days.single.places, [searchPlace, second]);
      c.move(1, -1);
      expect(c.value.days.single.places, [second, searchPlace]);
      c.edit(c.value.edit(startPlaceID: searchPlace, mode: 'transit'));
      expect(c.points()!.map((p) => p.targetId), [searchPlace, second]);
      expect(c.envelope.text, isNot(contains('不能复制的本人备注')));
      expect(c.envelope.references.map((r) => r.id), [searchPlace, second]);
      saved.failed = true;
      boundary.value++;
      expect(c.points(), isNull);
      saved.failed = false;
      boundary.value++;
      expect(c.points(), isNotNull);
      saved.items = [saved.items.first];
      boundary.value++;
      expect(c.points(), isNull);
      source = false;
      boundary.value++;
      source = true;
      boundary.value++;
      expect(c.current, false);
      c.add(second);
      expect(c.value.days.single.places, [second, searchPlace]);
      c.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  test(
    'route restart restores ID/day/order without persisted coordinates or old source authority',
    () async {
      final store = DraftMemoryStore(),
          client = MockClient((_) async => searchPage([])),
          saved = searchController(client),
          boundary = ValueNotifier(0);
      saved.items = [SavedItem.fromJson(searchRow(point: true))];
      RouteDraftController make() => RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => true,
        store: store,
      );
      final first = make();
      first.edit(LocalRouteValue.importText('Day 1\n只作草稿\nDay 2\n尚未匹配地点'));
      first.add(searchPlace);
      await first.flush();
      final id = first.envelope.id, dayID = first.value.days.first.id;
      first.dispose();
      saved.items = const [];
      final restored = make();
      await restored.restore();
      expect(restored.envelope.id, id);
      expect(restored.value.days.first.id, dayID);
      expect(restored.value.days.first.places, [searchPlace]);
      expect(restored.points(), isNull);
      final json = restored.envelope.toJson();
      expect(jsonEncode(json), isNot(contains('latitude')));
      expect(jsonEncode(json), isNot(contains('Bearer')));
      restored.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
  test(
    'route late restore cannot erase edit, resurrect actor ABA or carry wrong origin',
    () async {
      final store = DraftMemoryStore()
        ..pending = Completer<List<TypedLocalDraft>>();
      final client = MockClient((_) async => searchPage([])),
          saved = searchController(client),
          boundary = ValueNotifier(0);
      var source = true;
      final c = RouteDraftController(
        origin: routeOrigin,
        owner: searchOwner,
        cityID: 'aberdeen-gb',
        saved: saved,
        boundaryChanges: boundary,
        sourceCurrent: () => source,
        store: store,
      );
      final pending = c.restore();
      c.edit(c.value.edit(label: '当前编辑'));
      store.pending!.complete([
        TypedLocalDraft(
          owner: searchOwner,
          origin: routeOrigin,
          kind: 'route',
          slotID: 'route:aberdeen-gb',
          text: jsonEncode(LocalRouteValue(label: '旧盘内容').toJson()),
        ),
      ]);
      await pending;
      expect(c.value.label, '当前编辑');
      source = false;
      boundary.value++;
      source = true;
      boundary.value++;
      await c.flush();
      expect(c.current, false);
      expect(store.rows, isEmpty);
      expect(
        () => RouteDraftController(
          origin: routeOrigin,
          owner: searchOwner,
          cityID: 'aberdeen-gb',
          saved: saved,
          boundaryChanges: boundary,
          sourceCurrent: () => true,
          initial: TypedLocalDraft(
            owner: 'other',
            origin: routeOrigin,
            kind: 'route',
            text: '旧帐号',
          ),
        ),
        throwsArgumentError,
      );
      c.dispose();
      saved.dispose();
      client.close();
      boundary.dispose();
    },
  );
}
