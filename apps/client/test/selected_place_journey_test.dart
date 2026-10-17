import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/collection_control_pending_store.dart';
import 'package:birdtie_client/src/content/collection_create_pending_store.dart';
import 'package:birdtie_client/src/content/journey_detail.dart';
import 'package:birdtie_client/src/content/personal_collection_controller.dart';
import 'package:birdtie_client/src/content/selected_place_journey.dart';
import 'package:birdtie_client/src/workspace/selected_place_followup.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'collection_control_test.dart' show ccRequest, ccPreview, ccReceipt;

const journeyOwner = '18000000-0000-4000-8000-000000000001';
const journeyPlace = '18000000-0000-4000-8000-000000000002';
const journeyID = '18000000-0000-4000-8000-000000000003';
const journeyEntry = '18000000-0000-4000-8000-000000000004';
const newJourneyID = '18000000-0000-4000-8000-000000000005';
const journeyCity = 'aberdeen-gb';

http.Response journeyResponse(Object? data, [int status = 200]) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      status,
      headers: {
        'content-type': 'application/json; charset=utf-8',
        'cache-control': 'no-store',
      },
    );

Map<String, dynamic> journeyRecord({
  String id = journeyID,
  String city = journeyCity,
  String status = 'draft',
  int revision = 4,
  JourneyDetails? details,
  bool flat = false,
}) => {
  'id': id,
  'ownerAccountId': journeyOwner,
  'kind': 'journey',
  'cityId': city,
  'title': '合成本人旅程',
  'summary': '保留原说明',
  'visibility': status == 'published' ? 'public' : 'private',
  'status': status,
  'revision': revision,
  'entries': <Object>[],
  if (!flat) 'journeyDetails': (details ?? JourneyDetails.empty()).toInput(),
};

/// Synthetic HTTP only; it deliberately fails on every unrelated domain call.
class JourneyHarness {
  JourneyHarness() {
    records[journeyID] = journeyRecord();
    client = MockClient((r) async {
      requests.add('${r.method} ${r.url.path}');
      if (r.method == 'GET' && r.url.path == '/v1/places/$journeyPlace') {
        if (placeGate != null) return placeGate!.future;
        return journeyResponse({
          'id': journeyPlace,
          'cityId': placeCity,
          'name': '合成当前地点',
        }, placeStatus);
      }
      if (r.method == 'GET' && r.url.path == '/v1/me/personal-collections') {
        return journeyResponse({
          'items': records.values.toList(),
          'nextCursor': null,
        });
      }
      if (r.method == 'GET' &&
          r.url.path.startsWith('/v1/me/personal-collections/operations/')) {
        return journeyResponse(records[newJourneyID]);
      }
      if (r.method == 'GET' &&
          r.url.path.startsWith('/v1/me/personal-collections/journey/')) {
        final record = records[r.url.pathSegments.last];
        if (record == null) return journeyResponse(null, 404);
        return journeyResponse(
          verificationWrong && creates + puts > 0
              ? {...record, 'revision': (record['revision'] as int) + 1}
              : record,
        );
      }
      if (r.method == 'POST' && r.url.path == '/v1/me/personal-collections') {
        creates++;
        final input = jsonDecode(r.body) as Map<String, dynamic>;
        final record = {
          ...input,
          'id': newJourneyID,
          'ownerAccountId': journeyOwner,
          'status': 'draft',
          'visibility': 'private',
          'revision': 1,
        };
        records[newJourneyID] = record;
        return journeyResponse(record, createUnknown ? 500 : 201);
      }
      if (r.method == 'POST' && r.url.path.endsWith('/preview')) {
        previews++;
        return journeyResponse(ccPreview(ccRequest(r)));
      }
      if (r.method == 'PUT' &&
          r.url.path.startsWith(
            '/v1/me/personal-collection-controls/operations/',
          )) {
        puts++;
        final input = ccRequest(r);
        final before = records[input.collectionID]!;
        records[input.collectionID] = {
          ...before,
          ...input.input!.toJson(),
          'revision': input.expectedRevision + 1,
        };
        return journeyResponse(ccReceipt(input));
      }
      throw StateError('Unexpected synthetic path: ${r.method} ${r.url.path}');
    });
    controller = SelectedPlaceJourneyController(
      selection: SelectedPlaceFollowup(
        placeID: journeyPlace,
        cityID: journeyCity,
        taskID: 'native-task',
        current: () => selected,
      ),
      day: 2,
      ownerID: () => owner,
      authorizationHeader: () => token,
      sourceCurrent: () => taskCurrent,
      identityChanges: changes,
      client: client,
      apiBaseUrl: 'https://synthetic.invalid',
      controlPendingStore: MemoryCollectionControlPendingStore(),
      createPendingStore: createStore,
      publicationRecoveryReader: (_) async => null,
    );
  }
  final changes = ValueNotifier(0);
  final createStore = MemoryCollectionCreatePendingStore();
  String? owner = journeyOwner, token = 'Bearer synthetic';
  bool selected = true, taskCurrent = true, verificationWrong = false;
  bool createUnknown = false;
  String placeCity = journeyCity;
  int placeStatus = 200, creates = 0, puts = 0, previews = 0;
  Completer<http.Response>? placeGate;
  final requests = <String>[];
  final records = <String, Map<String, dynamic>>{};
  late final MockClient client;
  late final SelectedPlaceJourneyController controller;
  void dispose() {
    controller.dispose();
    client.close();
    changes.dispose();
  }
}

Future<void> journeyDrain() async {
  for (var i = 0; i < 12; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

class _SelectedMapReplySource extends AgentTaskSource {
  String? replyPlace = journeyPlace;
  AgentResult reply(String query) {
    final ref = AgentResultRef(type: 'place', id: replyPlace ?? journeyPlace);
    return AgentResult(
      entities: const [],
      activities: const [],
      places: const [],
      note: '合成第2天待选目标',
      task: AgentTask(
        id: journeyID,
        query: query,
        status: 'COMPLETED',
        cityID: journeyCity,
        intent: 'FIND_PLACE',
        messages: [
          AgentMessage(role: 'user', text: query),
          const AgentMessage(role: 'assistant', text: '合成第2天待选目标'),
        ],
      ),
      resultSet: AgentResultSet(
        id: 'synthetic-current-selected',
        status: 'ready',
        entities: const [],
        generatedAt: DateTime.now().toUtc(),
        items: [
          if (replyPlace != null)
            AgentResultItem(
              entity: ref,
              detail: ref,
              title: '合成当前地点',
              summary: '',
              scope: 'AUTHORIZED_VIEW',
            ),
        ],
      ),
    );
  }

  @override
  Future<AgentResult> continueSelectedPlace(
    AgentTask? task,
    String query,
    SelectedPlaceFollowup selected,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => reply(query);
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => reply(query);
}

void main() {
  test(
    'create recovery cold selected adapter reads original Journey without old selector grant or false current-selection receipt',
    () async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await h.controller.load();
      await h.controller.choose();
      h.createUnknown = true;
      expect(await h.controller.collections.save(), false);
      final original = h.records[newJourneyID]!;
      h.controller.dispose();
      h.placeStatus = 403;
      h.requests.clear();
      // A new context has no old draft, entry ID, Task or public Place grant.
      final next = SelectedPlaceJourneyController(
        selection: SelectedPlaceFollowup(
          placeID: journeyEntry,
          cityID: journeyCity,
          taskID: 'fresh-task',
          current: () => true,
        ),
        day: 2,
        ownerID: () => h.owner,
        authorizationHeader: () => h.token,
        sourceCurrent: () => true,
        identityChanges: h.changes,
        client: h.client,
        apiBaseUrl: 'https://synthetic.invalid',
        controlPendingStore: MemoryCollectionControlPendingStore(),
        createPendingStore: h.createStore,
        publicationRecoveryReader: (_) async => null,
      );
      addTearDown(next.dispose);
      await next.load();
      expect(next.recoveringOriginal, true);
      expect(next.editing, true);
      expect(h.requests, isEmpty);
      expect(await next.collections.save(), false);
      expect(await next.collections.recoverCreate(), true);
      expect(
        h.requests.single,
        startsWith('GET /v1/me/personal-collections/operations/'),
      );
      final entry = next.collections.editing!.journeyDetails!.itinerary.single;
      expect(entry.text('placeId'), journeyPlace);
      expect(entry.day('dayIndex'), 2);
      expect(next.collections.editing!.id, original['id']);
      expect(next.receipt, isNull);
      expect(h.creates, 1);
    },
  );
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'explicit current owned-map Place alias survives native day-two reply and can enter Journey selection',
    () async {
      final source = _SelectedMapReplySource();
      final workspace = AgentWorkspaceController(source: source);
      addTearDown(workspace.dispose);
      const mapID = 'owned-map:place:$journeyPlace';
      workspace.selectEntity(mapID);
      final selected = SelectedPlaceFollowup(
        placeID: journeyPlace,
        cityID: journeyCity,
        taskID: null,
        current: () => workspace.selectedEntityId == mapID,
      );
      await workspace.submit(
        '把这个加入第二天',
        [],
        [],
        cityID: journeyCity,
        selectedPlace: selected,
      );
      expect(workspace.requestError, isNull);
      expect(workspace.selectedEntityId, mapID);
      expect(selected.current, isTrue);
      final h = JourneyHarness();
      addTearDown(h.dispose);
      final journey = SelectedPlaceJourneyController(
        selection: selected,
        day: 2,
        ownerID: () => journeyOwner,
        authorizationHeader: () => h.token,
        sourceCurrent: () => workspace.task?.id == journeyID,
        identityChanges: workspace,
        client: h.client,
        apiBaseUrl: 'https://synthetic.invalid',
        controlPendingStore: MemoryCollectionControlPendingStore(),
        createPendingStore: MemoryCollectionCreatePendingStore(),
        publicationRecoveryReader: (_) async => null,
      );
      addTearDown(journey.dispose);
      await journey.load();
      expect(journey.targets, hasLength(1));
      expect(await journey.choose(), isTrue);
      expect(h.creates + h.puts, 0);
    },
  );
  for (final mode in ['new-search', 'missing-place', 'different-place']) {
    test(
      'owned-map alias is retired for $mode rather than broadening the selected exception',
      () async {
        final source = _SelectedMapReplySource();
        if (mode == 'missing-place') source.replyPlace = null;
        if (mode == 'different-place') source.replyPlace = newJourneyID;
        final workspace = AgentWorkspaceController(source: source);
        addTearDown(workspace.dispose);
        const mapID = 'owned-map:place:$journeyPlace';
        workspace.selectEntity(mapID);
        final selected = SelectedPlaceFollowup(
          placeID: journeyPlace,
          cityID: journeyCity,
          taskID: null,
          current: () => workspace.selectedEntityId == mapID,
        );
        await workspace.submit(
          mode == 'new-search' ? '找新的地点' : '把这个加入第二天',
          [],
          [],
          cityID: journeyCity,
          selectedPlace: mode == 'new-search' ? null : selected,
        );
        expect(workspace.selectedEntityId, isNull);
        expect(selected.current, isFalse);
      },
    );
  }
  test(
    'day-two parser and staging preserve exact native identity and original material',
    () {
      expect(selectedPlaceFollowupDay('把这个加入第二天？'), 2);
      expect(selectedPlaceFollowupDay('add this to day 2'), 2);
      expect(selectedPlaceFollowupDay('明天'), isNull);
      final original = JourneyDetails.empty().changed(dateLabel: '本人日期未定');
      final next = selectedPlaceJourneyDraft(
        original: original,
        entryID: journeyEntry,
        placeID: journeyPlace,
        cityID: journeyCity,
        name: '真实重读名称',
        day: 2,
      );
      expect(original.itinerary, isEmpty);
      expect(next.dateLabel, original.dateLabel);
      expect(next.days, 2);
      expect(next.itinerary.single.day('dayIndex'), 2);
      expect(next.itinerary.single.text('placeId'), journeyPlace);
      expect(next.itinerary.single.text('cityId'), journeyCity);
      expect(next.itinerary.single.text('sourceText'), isEmpty);
      expect(next.itinerary.single.toInput().keys, isNot(contains('latitude')));
      expect(
        () => selectedPlaceJourneyDraft(
          original: next,
          entryID: newJourneyID,
          placeID: journeyPlace,
          cityID: journeyCity,
          name: '真实重读名称',
          day: 2,
        ),
        throwsFormatException,
      );
    },
  );

  test(
    'new Journey only stages until existing save then independently reads exact day/place/revision',
    () async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await h.controller.load();
      expect(await h.controller.choose(), isTrue);
      expect(h.creates, 0);
      expect(h.controller.receipt, isNull);
      expect(await h.controller.collections.save(), isTrue);
      await journeyDrain();
      expect(h.creates, 1);
      expect(h.puts, 0);
      expect(h.controller.receipt?.id, newJourneyID);
      expect(h.controller.receipt?.revision, 1);
      expect(
        h.requests,
        contains('GET /v1/me/personal-collections/journey/$newJourneyID'),
      );
      final value = PersonalCollection.fromJson(h.records[newJourneyID]!);
      expect(value.entries, isEmpty);
      expect(
        value.journeyDetails!.itinerary.single.text('placeId'),
        journeyPlace,
      );
      expect(value.journeyDetails!.itinerary.single.day('dayIndex'), 2);
    },
  );

  test(
    'existing rich Journey uses fresh revision and concrete preview/commit while preserving original entries',
    () async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await h.controller.load();
      final target = h.controller.targets.single;
      h.records[journeyID] = journeyRecord(
        revision: 7,
        details: JourneyDetails.empty().changed(dateLabel: '原日期文字'),
      );
      expect(await h.controller.choose(target), isTrue);
      expect(h.controller.collections.editing!.revision, 7);
      expect(h.creates + h.puts, 0);
      final preview = await h.controller.collections.prepareControl('UPDATE');
      expect(preview, isNotNull);
      expect(preview!.input.expectedRevision, 7);
      expect(preview.input.input!.journeyDetails!.dateLabel, '原日期文字');
      expect(h.controller.receipt, isNull);
      expect(await h.controller.collections.commitControl(preview), isTrue);
      await journeyDrain();
      expect(h.previews, 1);
      expect(h.puts, 1);
      expect(h.creates, 0);
      expect(h.controller.receipt?.revision, 8);
      expect(
        h.requests.where((r) => r.endsWith('/journey/$journeyID')).length,
        3,
      );
    },
  );

  test(
    'published flat and other-city targets never become day-two destinations',
    () async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      h.records[journeyID] = journeyRecord(flat: true);
      h.records[newJourneyID] = journeyRecord(
        id: newJourneyID,
        city: 'qingdao-cn',
      );
      h.records[journeyEntry] = journeyRecord(
        id: journeyEntry,
        flat: true,
        status: 'published',
      );
      await h.controller.load();
      expect(h.controller.targets, isEmpty);
      expect(
        await h.controller.choose(
          PersonalCollection.fromJson(h.records[journeyID]!),
        ),
        isFalse,
      );
      expect(h.creates + h.puts, 0);
    },
  );

  test(
    'changed target schema and wrong-city source fail without implicit conversion or writes',
    () async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await h.controller.load();
      final target = h.controller.targets.single;
      h.records[journeyID] = journeyRecord(flat: true);
      expect(await h.controller.choose(target), isFalse);
      expect(h.controller.editing, isFalse);
      h.placeCity = 'qingdao-cn';
      expect(await h.controller.choose(), isFalse);
      expect(h.creates + h.puts, 0);
    },
  );

  for (final change in ['token', 'task', 'selection']) {
    // Identity/selection retirement remains distinct from a read lease timeout.
    test('late native read cannot survive $change A-B-A retirement', () async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      h.placeGate = Completer<http.Response>();
      final reading = h.controller.load();
      if (change == 'token') h.token = 'Bearer replacement';
      if (change == 'task') h.taskCurrent = false;
      if (change == 'selection') h.selected = false;
      h.changes.value++;
      h.token = 'Bearer synthetic';
      h.taskCurrent = h.selected = true;
      h.changes.value++;
      h.placeGate!.complete(
        journeyResponse({
          'id': journeyPlace,
          'cityId': journeyCity,
          'name': '迟到名称',
        }),
      );
      await reading;
      expect(h.controller.current, isFalse);
      expect(h.controller.placeName, isNull);
      expect(await h.controller.choose(), isFalse);
      expect(h.creates + h.puts, 0);
    });
  }

  test(
    'an explicit independent public Place denial retires the intent without using the old selection grant',
    () async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await h.controller.load();
      h.placeStatus = 403;
      expect(await h.controller.choose(), isFalse);
      expect(h.controller.current, isFalse);
      expect(h.controller.placeName, isNull);
      expect(h.creates + h.puts, 0);
    },
  );

  test(
    'a newer or mismatched readback is never claimed as the approved day-two save or retried',
    () async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await h.controller.load();
      await h.controller.choose();
      h.verificationWrong = true;
      expect(await h.controller.collections.save(), isTrue);
      await journeyDrain();
      expect(h.controller.receipt, isNull);
      expect(h.controller.error, contains('不会自动重发'));
      await h.controller.verifySaved();
      expect(h.creates, 1);
    },
  );
  test(
    'original uncertain create survives identity retirement as selectors and is only read on recovery',
    () async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await h.controller.load();
      await h.controller.choose();
      h.createUnknown = true;
      expect(await h.controller.collections.save(), isFalse);
      final retained = h.controller.collections.retainedLocalDraft!;
      expect(retained.unknown, isTrue);
      expect(retained.unknownPayload!['operationId'], isNotEmpty);
      h.owner = null;
      h.changes.value++;
      h.controller.dispose();
      h.owner = journeyOwner;
      final resumed = SelectedPlaceJourneyController(
        selection: SelectedPlaceFollowup(
          placeID: journeyPlace,
          cityID: journeyCity,
          taskID: 'new-current-task',
          current: () => true,
        ),
        day: 2,
        ownerID: () => h.owner,
        authorizationHeader: () => h.token,
        sourceCurrent: () => true,
        identityChanges: h.changes,
        client: h.client,
        apiBaseUrl: 'https://synthetic.invalid',
        retainedLocalDraft: retained,
        controlPendingStore: MemoryCollectionControlPendingStore(),
        createPendingStore: MemoryCollectionCreatePendingStore(),
        publicationRecoveryReader: (_) async => null,
      );
      addTearDown(resumed.dispose);
      expect(resumed.recoveringOriginal, isTrue);
      expect(await resumed.collections.recover(), isTrue);
      expect(h.creates, 1);
      expect(resumed.receipt, isNull);
      await resumed.finishOriginalRecovery();
      expect(resumed.editing, isFalse);
      expect(resumed.recoveringOriginal, isFalse);
      expect(resumed.targets.length, 2);
    },
  );
}
