import 'package:flutter_test/flutter_test.dart';
import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/content/moment_media_context_api.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/moment_thread_api.dart';
import 'package:birdtie_client/src/workspace/moment_thread_controller.dart';
import 'package:birdtie_client/src/workspace/moment_thread_pending_store.dart';

const mid = '10000000-0000-4000-8000-000000000001';
const second = '10000000-0000-4000-8000-000000000002';
const tid = '10000000-0000-4000-8000-000000000003';
const aid = '10000000-0000-4000-8000-000000000004';
String get hash => List.filled(64, 'a').join();
Map<String, dynamic> contextData() => {
  'schemaVersion': 'owned-moment-media-context-v1',
  'momentId': mid,
  'momentRevision': 3,
  'sourceVersion': hash,
  'observedAt': DateTime.now().toUtc().toIso8601String(),
  'validUntil': DateTime.now()
      .toUtc()
      .add(const Duration(seconds: 25))
      .toIso8601String(),
  'items': [
    {
      'id': tid,
      'assetKind': 'IMAGE',
      'assetId': aid,
      'revision': 1,
      'sourcePosition': 0,
      'capturedDate': '2024-04-12',
      'originalMapEnabled': true,
      'coordinateStatus': 'UNSPECIFIED',
      'sourceLatitude': 51.2,
      'sourceLongitude': -1.2,
      'locations': [],
    },
  ],
};
PrivateMoment moment(String id) => PrivateMoment(
  id: id,
  cityID: '',
  placeID: '',
  title: '原题目',
  body: '原内容',
  timePrecision: 'unknown',
  locationPrecision: 'none',
  status: 'draft',
  revision: 3,
  activityIDs: const [],
  communityID: '',
  organizationID: '',
  visibility: 'private',
);

void main() {
  test(
    'original coordinates and date remain private metadata without map authority',
    () {
      final data = contextData();
      final value = MomentMediaContexts.decode(data, mid, expectedRevision: 3);
      expect(value.items.single.coordinateStatus, 'UNSPECIFIED');
      expect(value.items.single.capturedDate, '2024-04-12');
      expect(value.items.single.latitude, 51.2);
      expect(
        () => MomentMediaContexts.decode(data, second),
        throwsFormatException,
      );
      (data['items'] as List).first['coordinateStatus'] = 'WGS84';
      expect(
        () => MomentMediaContexts.decode(data, mid),
        throwsFormatException,
      );
    },
  );
  test(
    'publishing an existing thread retains original IDs, versions and title',
    () {
      final entries = [
        MomentThreadDraftEntry.owned(moment(mid), locations: const []),
        MomentThreadDraftEntry.owned(moment(second), locations: const []),
      ];
      final input = MomentThreadInput(
        operationID: aid,
        action: 'PUBLISH',
        threadID: tid,
        expectedVersion: 1,
        localScope: tid,
        entries: entries,
      );
      final wire = input.toJson();
      expect((wire['items'] as List).first['momentId'], mid);
      expect((wire['items'] as List).first['expectedRevision'], 3);
      expect((wire['items'] as List).first['input']['title'], '原题目');
      expect(input.digest, hasLength(64));
      final copied = MomentThreadDraftEntry.decode(
        entries.first.json,
      ).copy(text: '修改后的内容');
      expect(copied.momentID, mid);
      expect(copied.input['title'], '原题目');
      expect(copied.input['body'], '修改后的内容');
      expect(
        () => MomentThreadInput(
          operationID: aid,
          action: 'PUBLISH',
          threadID: tid,
          expectedVersion: 1,
          localScope: tid,
          entries: [entries.first, entries.first],
        ).validate(),
        throwsFormatException,
      );
    },
  );
  test(
    'private original thread draft restores same members and retires on identity change',
    () async {
      final storage = <String, String>{};
      bool current = true;
      final client = MockClient((_) async => http.Response('', 404));
      final c = MomentThreadController(
        api: MomentThreadAPI(
          client: client,
          baseUrl: 'https://example.test',
          authorizationHeader: () => current ? 'Bearer synthetic' : null,
          current: () => current,
        ),
        ownerID: aid,
        journal: MomentThreadJournal(
          readValue: (key) async => storage[key],
          writeValue: (key, value) async {
            storage[key] = value;
          },
        ),
      );
      final view = OwnedMomentThread(
        tid,
        mid,
        'PRIVATE',
        'draft',
        1,
        [
          {
            'position': 0,
            'momentId': mid,
            'revision': 3,
            'status': 'AVAILABLE',
          },
          {
            'position': 1,
            'momentId': second,
            'revision': 3,
            'status': 'AVAILABLE',
          },
        ],
        const [],
        const [],
        hash,
        DateTime.now().toUtc().add(const Duration(seconds: 25)),
      );
      expect(
        await c.beginOwned(view, [
          MomentThreadDraftEntry.owned(moment(mid)),
          MomentThreadDraftEntry.owned(moment(second)),
        ]),
        isTrue,
      );
      c.updateActiveText('修改后的内容');
      await Future<void>.delayed(Duration.zero);
      c.moveEntry(0, 1);
      expect(c.entries.first.momentID, mid);
      c.removeEntry(0);
      expect(c.entries.length, 2);
      final restored = MomentThreadJournalState.decode(
        jsonDecode(storage.values.single),
      );
      expect(restored.threadID, tid);
      expect(restored.entries.first.text, '修改后的内容');
      expect(restored.entries.first.momentID, mid);
      current = false;
      c.retire();
      expect(c.entries, isEmpty);
      c.dispose();
      client.close();
    },
  );
}
