import 'dart:convert';
import 'package:birdtie_client/src/content/journey_detail.dart';
import 'package:birdtie_client/src/content/collection_control.dart';
import 'package:birdtie_client/src/content/collection_control_pending_store.dart';
import 'package:birdtie_client/src/content/personal_collection_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const owner = '15800000-0000-4000-8000-000000000300';
Map<String, dynamic> entry(
  String id,
  String type,
  int position,
  Map<String, dynamic> fields,
) => {
  'id': id,
  'entryType': type,
  'position': position,
  for (final k in journeyTextFields) k: '',
  for (final k in journeyDayFields) k: null,
  ...fields,
};
JourneyDetails fixture() => JourneyDetails.decode({
  'schemaVersion': journeyDetailsSchema,
  'startDate': '2026-10-15',
  'endDate': '',
  'dateLabel': '两天 · 自由行',
  'days': 2,
  'itinerary': [
    entry('15800000-0000-4000-8000-000000000001', 'STOP', 1, {
      'title': '城市 · 一',
    }),
    entry('15800000-0000-4000-8000-000000000002', 'ARRANGEMENT', 2, {
      'title': '无地点安排',
      'note': '先逛街\n再休息',
      'kind': 'custom',
      'dayIndex': 1,
      'stopId': '15800000-0000-4000-8000-000000000001',
      'sourceLabel': '手动整理',
      'sourceText': '保留原始说明',
    }),
  ],
});
void main() {
  test('Journey Unicode schedule matches real Go 158 and 147 digests', () {
    final d = fixture();
    expect(
      d.digest,
      '18ad141cc09e8c1f1e09948ecdfed4de76e7c92901a1c5a5dadf474b6f127aaa',
    );
    final wire = {
      ...d.toInput(),
      'sourceVersion': 'a' * 64,
      'itinerary': [
        for (final e in d.itinerary)
          Map<String, dynamic>.fromEntries(
            e.toInput().entries.toList().reversed,
          ),
      ],
    };
    expect(
      jsonEncode(JourneyDetails.decode(wire).toInput()),
      jsonEncode(d.toInput()),
    );
    expect(
      () => JourneyDetails.decode(wire, input: true),
      throwsFormatException,
    );
    final input = CollectionControlInput(
      operationID: '15800000-0000-4000-8000-000000000100',
      action: 'UPDATE',
      kind: 'journey',
      collectionID: '15800000-0000-4000-8000-000000000200',
      expectedRevision: 3,
      input: CollectionControlBody.normalize({
        'kind': 'journey',
        'cityId': '',
        'title': '',
        'summary': '旅程备注',
        'entries': [],
        'journeyDetails': d.toInput(),
      }),
    );
    expect(
      input.payloadDigest,
      'c4a544d9835305d9a781602585aefccc71041994e629b40db841ae030de34d78',
    );
    expect(
      () => d.changed(itinerary: [d.itinerary.last]),
      throwsFormatException,
    );
    expect(
      d.changed(itinerary: d.itinerary.reversed.toList()).digest,
      isNot(d.digest),
    );
  });
  test(
    'No-city Journey save and draft recovery preserve all detail material',
    () async {
      Map<String, dynamic>? submitted;
      final client = MockClient((r) async {
        expect(r.method, 'POST');
        submitted = jsonDecode(r.body);
        return http.Response(
          jsonEncode({
            'data': {
              ...submitted!,
              'id': '15800000-0000-4000-8000-000000000200',
              'ownerAccountId': owner,
              'revision': 1,
              'status': 'draft',
              'visibility': 'private',
            },
          }),
          201,
          headers: {'content-type': 'application/json'},
        );
      });
      final c = PersonalCollectionController(
        kind: 'journey',
        cityID: '',
        authorizationHeader: () => 'Bearer synthetic',
        ownerID: () => owner,
        client: client,
        apiBaseUrl: 'https://synthetic.invalid',
        controlPendingStore: MemoryCollectionControlPendingStore(),
      );
      c.setJourneyDetails(fixture());
      expect(await c.save(), isTrue);
      expect(submitted!['entries'], isEmpty);
      expect(submitted!['cityId'], '');
      expect(c.editing!.journeyDetails!.digest, fixture().digest);
      c.setJourneyDetails(fixture().changed(dateLabel: '本机未保存改动'));
      final retained = c.retainedLocalDraft!;
      c.dispose();
      final restored = PersonalCollectionController(
        kind: 'journey',
        cityID: '',
        authorizationHeader: () => 'Bearer refreshed',
        ownerID: () => owner,
        client: client,
        apiBaseUrl: 'https://synthetic.invalid',
        retainedLocalDraft: retained,
        controlPendingStore: MemoryCollectionControlPendingStore(),
      );
      expect(restored.draftJourneyDetails!.dateLabel, '本机未保存改动');
      expect(
        restored.draftJourneyDetails!.itinerary.last.text('sourceText'),
        '保留原始说明',
      );
      restored.dispose();
      client.close();
    },
  );
}
