import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/workspace/moment_thread_api.dart';
import 'package:birdtie_client/src/workspace/moment_thread_controller.dart';
import 'package:birdtie_client/src/workspace/moment_thread_panel.dart';
import 'package:birdtie_client/src/workspace/moment_thread_pending_store.dart';

const owner = '00000000-0000-4000-8000-000000000901';
const firstID = '00000000-0000-4000-8000-000000000902';
const secondID = '00000000-0000-4000-8000-000000000903';
const threadID = '00000000-0000-4000-8000-000000000904';
final source = List.filled(64, 'a').join();

MomentThreadJournal journal(Map<String, String> values) => MomentThreadJournal(
  readValue: (key) async => values[key],
  writeValue: (key, value) async {
    values[key] = value;
  },
);
http.Response response(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json'},
);
Map<String, Object?> preview(
  MomentThreadController c,
  Map<String, dynamic> input,
) {
  final requested = MomentThreadInput(
    operationID: input['operationId'],
    action: 'CREATE_PUBLISH',
    localScope: input['localScope'],
    entries: c.entries,
  );
  return {
    'operationId': requested.operationID,
    'action': requested.action,
    'payloadDigest': requested.digest,
    'observedAt': momentUTC(DateTime.now().toUtc()),
    'expiresAt': momentUTC(
      DateTime.now().toUtc().add(const Duration(seconds: 30)),
    ),
    'approval': 'hw1.concrete-preview-only',
    'input': input,
    'sourceVersion': source,
    'members': [],
    'materials': [for (final _ in c.entries) []],
    'contexts': [
      for (final entry in c.entries)
        {
          'cityId': entry.cityID,
          'cityName': '阿伯丁',
          'placeId': entry.placeID,
          'placeName': '',
        },
    ],
  };
}

Map<String, Object?> receipt(MomentThreadPending original) => {
  'schemaVersion': 'moment-thread-operation-v1',
  'operationId': original.operationID,
  'status': 'SUCCEEDED',
  'action': original.action,
  'threadId': threadID,
  'rootMomentId': firstID,
  'audience': 'PUBLIC',
  'expectedVersion': 0,
  'appliedVersion': 1,
  'payloadDigest': original.digest,
  'members': [
    for (var i = 0; i < 2; i++)
      {
        'momentId': i == 0 ? firstID : secondID,
        'status': 'published',
        'position': i,
        'appliedRevision': 2,
        'mediaSetVersion': 0,
        'media': [],
      },
  ],
  'committedAt': momentUTC(DateTime.now().toUtc()),
};

void main() {
  testWidgets(
    'thread panel edits selected entries through the existing composer and preserves order',
    (tester) async {
      final values = <String, String>{};
      final c = MomentThreadController(
        api: MomentThreadAPI(
          client: MockClient(
            (_) async => throw StateError('no requests expected'),
          ),
          baseUrl: 'https://birdtie.example',
          authorizationHeader: () => 'Bearer synthetic',
          current: () => true,
        ),
        ownerID: owner,
        journal: journal(values),
      );
      await c.begin(cityID: 'aberdeen-gb', initialText: '第一条正文');
      c.selectEntry(1);
      c.updateActiveText('第二条独立正文');
      c.selectEntry(0);
      var composer = '';
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SizedBox(
              height: 500,
              child: MomentThreadPanel(
                controller: c,
                onActiveText: (text) => composer = text,
              ),
            ),
          ),
        ),
      );
      expect(find.byType(TextField), findsNothing);
      await tester.tap(find.text('第2条'));
      await tester.pump();
      expect(composer, '第二条独立正文');
      final before = MomentThreadInput(
        operationID: newMomentOperationID(),
        action: 'CREATE_PUBLISH',
        localScope: c.localScope,
        entries: c.entries,
      );
      final beforeDigest = before.digest;
      c.moveEntry(1, 0);
      expect(c.entries.first.text, '第二条独立正文');
      expect(c.activeIndex, 0);
      final reordered = MomentThreadInput(
        operationID: before.operationID,
        action: 'CREATE_PUBLISH',
        localScope: c.localScope,
        entries: c.entries,
      );
      expect(reordered.digest, isNot(beforeDigest));
      await tester.pumpWidget(const SizedBox());
      c.dispose();
    },
  );

  testWidgets(
    'lost atomic publish response recovers the original thread and child IDs without PUT replay',
    (tester) async {
      final values = <String, String>{};
      final store = journal(values);
      var puts = 0, reads = 0;
      late MomentThreadController c;
      MomentThreadPending? original;
      final api = MomentThreadAPI(
        baseUrl: 'https://birdtie.example',
        authorizationHeader: () => 'Bearer synthetic',
        current: () => true,
        client: MockClient((request) async {
          if (request.method == 'POST') {
            return response(preview(c, jsonDecode(request.body)));
          }
          if (request.method == 'PUT') {
            puts++;
            original = c.pending;
            throw http.ClientException('synthetic response loss');
          }
          reads++;
          return response(receipt(original!));
        }),
      );
      c = MomentThreadController(api: api, ownerID: owner, journal: store);
      await c.begin(cityID: 'aberdeen-gb', initialText: '第一条');
      c.selectEntry(1);
      c.updateActiveText('第二条');
      await c.preview();
      expect(c.previewValue, isNotNull);
      await c.cancelPreview();
      expect(puts, 0);
      await c.preview();
      await c.confirm();
      expect(puts, 1);
      expect(c.uncertain, isTrue);
      final stored = values.values.single;
      expect(stored.contains('hw1.'), isFalse);
      expect(stored.contains('Bearer'), isFalse);
      expect((await store.load(owner, api.origin))!.pending!.stage, 'UNKNOWN');
      c.dispose();
      final restored = MomentThreadController(
        api: api,
        ownerID: owner,
        journal: store,
      );
      await restored.restore();
      expect(restored.uncertain, isTrue);
      await restored.recover();
      expect(reads, 1);
      expect(puts, 1);
      expect(restored.receipt!.threadID, threadID);
      expect(restored.receipt!.members.map((entry) => entry.momentID), [
        firstID,
        secondID,
      ]);
      expect(restored.entries, isEmpty);
      expect((await store.load(owner, api.origin))!.pending, isNull);
      restored.dispose();
    },
  );

  testWidgets(
    'draft storage cap keeps RAM text and retired approval sends zero writes',
    (tester) async {
      final values = <String, String>{};
      var requests = 0;
      final api = MomentThreadAPI(
        client: MockClient((_) async {
          requests++;
          throw StateError('should stay local');
        }),
        baseUrl: 'https://birdtie.example',
        authorizationHeader: () => 'Bearer synthetic',
        current: () => true,
      );
      final c = MomentThreadController(
        api: api,
        ownerID: owner,
        journal: journal(values),
      );
      await c.begin(cityID: 'aberdeen-gb');
      for (var i = 0; i < 20; i++) {
        if (i >= c.entries.length) {
          c.addEntry();
        }
        c.selectEntry(i);
        c.updateActiveText(List.filled(1000, 'x').join());
      }
      await c.preview();
      expect(requests, 0);
      expect(c.entries.length, 20);
      expect(c.activeText.length, 1000);
      expect(c.notice, contains('16KiB'));
      c.dispose();
      var valid = true, puts = 0;
      late MomentThreadController other;
      final otherAPI = MomentThreadAPI(
        client: MockClient((request) async {
          if (request.method == 'PUT') {
            puts++;
          }
          return response(preview(other, jsonDecode(request.body)));
        }),
        baseUrl: 'https://other-birdtie.example',
        authorizationHeader: () => 'Bearer synthetic',
        current: () => valid,
      );
      other = MomentThreadController(
        api: otherAPI,
        ownerID: owner,
        journal: journal(<String, String>{}),
      );
      await other.begin(cityID: 'aberdeen-gb', initialText: '第一条');
      other.selectEntry(1);
      other.updateActiveText('第二条');
      await other.preview();
      expect(other.previewValue, isNotNull);
      final captured = other.confirm;
      valid = false;
      other.retire();
      await captured();
      expect(puts, 0);
      expect(other.entries, isEmpty);
      other.dispose();
    },
  );
}
