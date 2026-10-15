import 'dart:async';
import 'dart:convert';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/moment_interaction.dart';
import 'public_moment_api_test.dart' show publicMomentV3Data;
import 'entity_cover_test.dart' show coverSettle;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/workspace/moment_thread_api.dart';
import 'package:birdtie_client/src/workspace/moment_thread_controller.dart';
import 'package:birdtie_client/src/workspace/moment_thread_panel.dart';
import 'package:birdtie_client/src/workspace/moment_thread_pending_store.dart';
import 'package:birdtie_client/src/workspace/public_moment_media.dart';
import 'package:birdtie_client/src/workspace/entity_cover.dart';

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
    action: input['action'],
    threadID: input['threadId'],
    baseMomentID: input['baseMomentId'] as String? ?? '',
    expectedVersion: input['expectedVersion'],
    removedItems: (input['removedItems'] as List? ?? const [])
        .map((v) => Map<String, Object>.from(v))
        .toList(),
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
    if (requested.action == 'EDIT')
      'preservedMedia': [for (final _ in c.entries) null],
    if (c.entries.any((entry) => entry.locations != null))
      'locations': [for (final _ in c.entries) []],
    'contexts': [
      for (final entry in c.entries)
        {
          'cityId': entry.cityID,
          'cityName': entry.cityID.isEmpty ? '' : '阿伯丁',
          'placeId': entry.placeID,
          'placeName': '',
        },
    ],
  };
}

Map<String, Object?> receipt(MomentThreadPending original) => {
  'schemaVersion': original.action == 'EDIT'
      ? 'moment-thread-operation-v2'
      : 'moment-thread-operation-v1',
  'operationId': original.operationID,
  'status': 'SUCCEEDED',
  'action': original.action,
  'threadId': threadID,
  'rootMomentId': firstID,
  'audience': 'PUBLIC',
  'expectedVersion': 0,
  'appliedVersion': 1,
  'payloadDigest': original.digest,
  if (original.action == 'EDIT') 'removedMembers': [],
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
  testWidgets('整串撤回批准经公开详情能力及祖先重建仍提交一次且身份变化退休', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final capability = Completer<http.Response>();
    final identity = ValueNotifier(owner);
    var puts = 0, threadReads = 0;
    late StateSetter rebuild;
    List<Map<String, Object?>> members(String status, int revision) => [
      for (var i = 0; i < 2; i++)
        {
          'momentId': i == 0 ? firstID : secondID,
          'status': status,
          'position': i,
          'appliedRevision': revision,
          'mediaSetVersion': 0,
          'media': [],
        },
    ];
    final client = MockClient((request) async {
      final path = request.url.path;
      if (path.endsWith('/public-photo')) return capability.future;
      if (path == '/v1/moments/$firstID') {
        return response({...publicMomentV3Data(photo: false), 'id': firstID});
      }
      if (path == '/v1/moments/$firstID/thread' ||
          path == '/v1/moment-threads/$threadID') {
        threadReads++;
        return response({
          'schemaVersion': 'moment-thread-view-v1',
          'threadId': threadID,
          'rootMomentId': firstID,
          'audience': 'PUBLIC',
          'version': 1,
          'canWithdraw': true,
          'sourceVersion': source,
          'observedAt': momentUTC(DateTime.now().toUtc()),
          'validUntil': momentUTC(
            DateTime.now().toUtc().add(const Duration(seconds: 30)),
          ),
          'members': [
            for (var i = 0; i < 2; i++)
              {
                'position': i,
                'momentId': i == 0 ? firstID : secondID,
                'revision': 2,
                'status': 'AVAILABLE',
              },
          ],
        });
      }
      if (path == '/v1/me/moment-threads/previews' || request.method == 'PUT') {
        final raw = jsonDecode(request.body) as Map<String, dynamic>;
        final input = MomentThreadInput(
          operationID: raw['operationId'],
          action: raw['action'],
          threadID: raw['threadId'],
          expectedVersion: raw['expectedVersion'],
        );
        expect(raw['localScope'], '');
        expect(raw['items'], isEmpty);
        if (request.method == 'PUT') {
          puts++;
          expect(raw['sourceVersion'], source);
          expect(raw['approval'], 'hw1.synthetic-withdraw');
          return response({
            'schemaVersion': 'moment-thread-operation-v1',
            'operationId': input.operationID,
            'status': 'SUCCEEDED',
            'action': 'WITHDRAW',
            'threadId': threadID,
            'rootMomentId': firstID,
            'audience': 'PUBLIC',
            'expectedVersion': 1,
            'appliedVersion': 2,
            'payloadDigest': input.digest,
            'members': members('withdrawn', 3),
            'committedAt': momentUTC(DateTime.now().toUtc()),
          });
        }
        return response({
          'operationId': input.operationID,
          'action': 'WITHDRAW',
          'payloadDigest': input.digest,
          'observedAt': momentUTC(DateTime.now().toUtc()),
          'expiresAt': momentUTC(
            DateTime.now().toUtc().add(const Duration(seconds: 30)),
          ),
          'approval': 'hw1.synthetic-withdraw',
          'input': raw,
          'sourceVersion': source,
          'members': members('published', 2),
          'materials': [],
          'contexts': [],
          'locations': [],
        });
      }
      return http.Response('{}', 404);
    });
    await t.pumpWidget(
      MaterialApp(
        home: StatefulBuilder(
          builder: (context, setState) {
            rebuild = setState;
            return PublicMomentPage(
              momentID: firstID,
              authorizationHeader: () => 'Bearer synthetic',
              ownerID: () => identity.value,
              identityChanges: identity,
              client: client,
              apiBaseUrl: 'https://birdtie.example',
            );
          },
        ),
      ),
    );
    await coverSettle(t);
    final api = t
        .widget<MomentThreadSidecar>(find.byType(MomentThreadSidecar))
        .api;
    await t.ensureVisible(find.text('预览撤回整串'));
    await t.tap(find.text('预览撤回整串'));
    await coverSettle(t);
    expect(find.text('确认撤回整串'), findsOneWidget);
    final control = t
        .widget<MomentThreadPanel>(find.byType(MomentThreadPanel))
        .controller;
    final approved = control.previewValue;
    final readsBefore = threadReads;
    capability.complete(http.Response('{}', 404));
    await coverSettle(t);
    rebuild(() {});
    await coverSettle(t);
    expect(
      identical(
        t.widget<MomentThreadSidecar>(find.byType(MomentThreadSidecar)).api,
        api,
      ),
      true,
    );
    expect(
      identical(
        t.widget<MomentThreadPanel>(find.byType(MomentThreadPanel)).controller,
        control,
      ),
      true,
    );
    expect(identical(control.previewValue, approved), true);
    expect(control.uncertain, false);
    expect(threadReads, readsBefore);
    await t.ensureVisible(find.text('确认撤回整串'));
    await t.tap(find.text('确认撤回整串'));
    await coverSettle(t);
    expect(puts, 1);
    expect(find.text('整串已撤回，不再公开显示。'), findsOneWidget);
    expect(find.text('中文公开正文'), findsNothing);
    expect(find.byType(MomentThreadSidecar), findsNothing);
    expect(find.byType(MomentInteractionPanel), findsNothing);
    expect(find.byType(Image), findsNothing);
    expect(find.byType(RawImage), findsNothing);
    expect(api.current(), false);
    expect(control.current, false);
    expect((await control.journal.load(owner, api.origin))?.pending, isNull);
    identity.value = secondID;
    await coverSettle(t);
    expect(api.current(), false);
    expect(puts, 1);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  test('真实Go字段顺序的无位置串文预览保留批准摘要并拒绝内容篡改', () async {
    final values = <String, String>{};
    var previews = 0, puts = 0;
    late MomentThreadController controller;
    Map<String, dynamic>? returned;
    final client = MockClient((request) async {
      if (request.method == 'PUT') puts++;
      expect(request.method, 'POST');
      expect(request.url.path, '/v1/me/moment-threads/previews');
      previews++;
      final submitted = jsonDecode(request.body) as Map<String, dynamic>;
      final items = submitted['items'] as List;
      expect(
        (items[0]['input'] as Map)['body'],
        'QA-device11-thread-first-20261015',
      );
      expect(
        (items[1]['input'] as Map)['body'],
        'QA-device11-thread-second-20261015',
      );
      final wire = preview(controller, submitted);
      wire['input'] = {
        'operationId': submitted['operationId'],
        'action': submitted['action'],
        'threadId': submitted['threadId'],
        'localScope': submitted['localScope'],
        'audience': submitted['audience'],
        'expectedVersion': submitted['expectedVersion'],
        'baseMomentId': submitted['baseMomentId'],
        'removedItems': submitted['removedItems'],
        'items': [
          for (final item in items)
            {
              'momentId': item['momentId'],
              'expectedRevision': item['expectedRevision'],
              'restore': item['restore'],
              // Match Go's MomentInput struct order, including explicit [].
              'input': {
                'locations': item['input']['locations'],
                'cityId': item['input']['cityId'],
                'placeId': item['input']['placeId'],
                'title': item['input']['title'],
                'body': item['input']['body'],
                'occurredAt': item['input']['occurredAt'],
                'timePrecision': item['input']['timePrecision'],
                'locationPrecision': item['input']['locationPrecision'],
              },
              'media': item['media'],
            },
        ],
      };
      wire['contexts'] = [
        for (final _ in items)
          {'cityId': '', 'cityName': '', 'placeId': '', 'placeName': ''},
      ];
      wire['locations'] = [for (final _ in items) []];
      returned = jsonDecode(jsonEncode(wire)) as Map<String, dynamic>;
      return response(wire);
    });
    final api = MomentThreadAPI(
      client: client,
      baseUrl: 'https://birdtie.example',
      authorizationHeader: () => 'Bearer synthetic',
      current: () => true,
    );
    final store = journal(values);
    controller = MomentThreadController(
      api: api,
      ownerID: owner,
      journal: store,
    );
    await controller.begin(initialText: 'QA-device11-thread-first-20261015\n');
    controller.addEntry();
    controller.selectEntry(1);
    controller.updateActiveText('QA-device11-thread-second-20261015\n');
    await controller.preview();
    final approved = controller.previewValue;
    expect(approved, isNotNull, reason: controller.notice);
    expect(controller.entries.first.text.endsWith('\n'), true);
    expect(
      approved!.input.entries.every((entry) => entry.cityID.isEmpty),
      true,
    );
    expect(
      approved.input.entries.every((entry) => entry.locations!.isEmpty),
      true,
    );
    expect(returned!['payloadDigest'], approved.input.digest);
    expect(
      momentHash(returned!['input']),
      isNot(momentHash(approved.input.toJson())),
    );
    expect((await store.load(owner, api.origin))!.pending!.stage, 'PREPARED');

    Map<String, dynamic> changed() =>
        jsonDecode(jsonEncode(returned)) as Map<String, dynamic>;
    final changedBody = changed();
    changedBody['input']['items'][0]['input']['body'] = '另一版正文';
    final changedOrder = changed();
    changedOrder['input']['items'] = (changedOrder['input']['items'] as List)
        .reversed
        .toList();
    final omittedLocations = changed();
    (omittedLocations['input']['items'][0]['input'] as Map).remove('locations');
    final extraField = changed();
    extraField['input']['items'][0]['input']['unapprovedField'] = true;
    for (final tampered in [
      changedBody,
      changedOrder,
      omittedLocations,
      extraField,
    ]) {
      expect(
        () => MomentThreadPreview.decode(tampered, approved.input),
        throwsFormatException,
      );
    }
    expect(previews, 1);
    expect(puts, 0);
    controller.dispose();
    client.close();
  });
  test('optional thread keeps a selected Place without a compulsory City', () {
    final place = MomentThreadDraftEntry(
      localID: firstID,
      cityID: '',
      placeID: owner,
      text: '作者选择的地点，与探索城市无关',
    );
    final input = MomentThreadInput(
      operationID: secondID,
      action: 'CREATE_PUBLISH',
      localScope: threadID,
      entries: [
        place,
        MomentThreadDraftEntry(localID: secondID, cityID: '', text: '无位置的第二条'),
      ],
    );
    final items = input.toJson()['items'] as List;
    expect(items.first['input'], {
      'cityId': '',
      'placeId': owner,
      'title': '',
      'body': place.text,
      'occurredAt': null,
      'timePrecision': 'unknown',
      'locationPrecision': 'place',
    });
    expect(items.last['input']['locationPrecision'], 'none');
    final restored = MomentThreadDraftEntry.decode(place.json);
    expect(restored.toItem()['input'], items.first['input']);
    expect(input.digest, matches(RegExp(r'^[0-9a-f]{64}$')));
  });

  test(
    'optional thread seals a media-only entry in its actual shared scope',
    () {
      final rights = EntityCoverRights(
        basis: 'OWNED',
        attribution: '',
        licenseReference: '',
        canRetain: true,
        canPublish: true,
        retentionUntil: DateTime.now().toUtc().add(const Duration(days: 1)),
      );
      final media = PublicMomentMediaSelection(owner, 1, rights);
      final entry = MomentThreadDraftEntry(
        localID: firstID,
        cityID: '',
        media: [media],
      );
      final second = MomentThreadDraftEntry(
        localID: secondID,
        cityID: '',
        text: '第二条正文',
      );
      final input = MomentThreadInput(
        operationID: secondID,
        action: 'CREATE_PUBLISH',
        localScope: threadID,
        entries: [entry, second],
      );
      final wire = input.toJson();
      final item = (wire['items'] as List).first;
      expect(wire['localScope'], threadID);
      expect(item['input']['title'], '');
      expect(item['input']['body'], '');
      expect(item['input']['cityId'], '');
      expect(item['input']['locationPrecision'], 'none');
      expect(item['media'], [media.toJson()]);
      final sealed = MomentControlInput(
        operationID: firstID,
        action: 'PUBLISH',
        momentID: firstID,
        expectedRevision: 1,
        input: entry.input,
        media: [media],
        localScope: threadID,
      );
      final secondInput = MomentControlInput(
        operationID: secondID,
        action: 'PUBLISH',
        momentID: secondID,
        expectedRevision: 1,
        input: second.input,
      );
      expect(
        input.digest,
        momentHash([
          'moment-thread-v1',
          secondID,
          'CREATE_PUBLISH',
          '',
          threadID,
          'PUBLIC',
          0,
          [
            [
              sealed.inputHash,
              [
                [owner, 1, rights.digest],
              ],
            ],
            [secondInput.inputHash, []],
          ],
        ]),
      );
      expect(() => entry.toItem(), throwsFormatException);
      expect(
        () => entry.copy(media: const []).toItem(localScope: threadID),
        throwsFormatException,
      );
      final changed = MomentThreadInput(
        operationID: input.operationID,
        action: input.action,
        localScope: input.localScope,
        entries: [
          entry.copy(media: [PublicMomentMediaSelection(owner, 2, rights)]),
          second,
        ],
      );
      expect(changed.digest, isNot(input.digest));
    },
  );

  test(
    'thread restore clears legacy rights and saves only derived media-free draft',
    () async {
      final values = <String, String>{}, store = journal(values);
      final media = PublicMomentMediaSelection(
        secondID,
        1,
        EntityCoverRights(
          basis: 'OWNED',
          attribution: '',
          licenseReference: '',
          canRetain: true,
          canPublish: true,
          retentionUntil: DateTime.now().toUtc().add(const Duration(days: 1)),
        ),
      );
      await store.saveDraft(
        owner,
        'https://thread-stage.test',
        MomentThreadJournalState(
          threadID,
          [
            MomentThreadDraftEntry(
              localID: firstID,
              cityID: '',
              text: '本人文字',
              media: [media],
            ),
            MomentThreadDraftEntry(localID: secondID, cityID: '', text: '第二条'),
          ],
          0,
          null,
        ),
      );
      var requests = 0;
      final c = MomentThreadController(
        ownerID: owner,
        journal: store,
        api: MomentThreadAPI(
          baseUrl: 'https://thread-stage.test',
          authorizationHeader: () => 'Bearer synthetic',
          current: () => true,
          client: MockClient((_) async {
            requests++;
            throw StateError('no HTTP expected');
          }),
        ),
      );
      expect(await c.restore(), isTrue);
      expect(c.activeText, '本人文字');
      expect(c.entries.every((entry) => entry.media.isEmpty), isTrue);
      c.setActiveMedia([media]);
      expect(c.entries.first.media.single.rights, same(media.rights));
      await Future<void>.delayed(Duration.zero);
      final reopened = (await store.load(owner, c.origin))!;
      expect(reopened.entries.every((entry) => entry.media.isEmpty), isTrue);
      expect(values.values.single, isNot(contains('rights')));
      expect(requests, 0);
      c.dispose();
    },
  );
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
      c.addEntry();
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
      c.addEntry();
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
      expect(restored.entries.map((entry) => entry.momentID), [
        firstID,
        secondID,
      ]);
      expect(restored.entries.every((entry) => entry.preserveMedia), isTrue);
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
      c.updateActiveText(
        List.filled(momentThreadJournalMaxBytes + 1, 'x').join(),
      );
      await c.saveLocal();
      expect(requests, 0);
      expect(c.entries.length, 1);
      expect(c.activeText.length, momentThreadJournalMaxBytes + 1);
      expect(c.unsavedDraft, isTrue);
      expect(c.notice, contains('尚未保存到本机'));
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
      other.addEntry();
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
