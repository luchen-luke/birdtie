import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/active_social_intent_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'model_egress_api_test.dart' show egressOwner;

const nowOwner = egressOwner,
    nowIntent = '22222222-2222-4222-8222-222222222222',
    nowAgent = '33333333-3333-4333-8333-333333333333';
String nowStamp(DateTime v) => v.toUtc().toIso8601String();
Map<String, dynamic> nowEnvelope() => {
  'schemaVersion': 'active-social-intents-v1',
  'owner': {'type': 'PERSON', 'id': nowOwner},
  'agentId': nowAgent,
  'observedAt': nowStamp(DateTime.now()),
  'modelAccess': false,
  'sendAllowed': false,
};
Map<String, dynamic> nowItem({String status = 'DRAFT', String? version}) => {
  'intent': {
    'id': nowIntent,
    'creatorAccountId': nowOwner,
    'type': 'FIND_ACTIVITY',
    'title': '周末羽毛球',
    'constraints': {'areaLabel': '合成城区', 'category': 'badminton'},
    'audience': 'PRIVATE',
    'modality': 'IN_PERSON',
    'status': status,
    'expiresAt': nowStamp(DateTime.now().add(const Duration(hours: 2))),
    'createdAt': '2026-01-01T12:00:00+08:00',
    'updatedAt': '2026-01-01T12:00:01+08:00',
  },
  'version': version ?? ('a' * 64),
  'sourceAvailable': true,
  'locationLabel': '合成城区',
  'audienceLabel': '仅自己',
};
Map<String, dynamic> nowList([Map<String, dynamic>? item]) => {
  ...nowEnvelope(),
  'items': [item ?? nowItem()],
  'limit': 100,
  'truncated': false,
};
Map<String, dynamic> nowOptions() => {
  ...nowEnvelope(),
  'cities': [
    {'id': 'local-city', 'label': '合成城市'},
  ],
  'places': [],
  'communities': [],
  'invitees': [],
  'limit': 100,
  'truncated': false,
};
Map<String, dynamic> nowPreview(
  Map<String, dynamic> before,
  String op, {
  Map<String, dynamic>? edit,
}) {
  final after = jsonDecode(jsonEncode(before)) as Map<String, dynamic>;
  after['intent']['status'] = switch (op) {
    'EDIT' => 'DRAFT',
    'ACTIVATE' => 'ACTIVE',
    _ => 'CANCELLED',
  };
  if (edit != null) after['intent'].addAll(edit);
  return {
    ...nowEnvelope(),
    'previewId': 'opaque-process-specific-preview-token',
    'operation': op,
    'before': before,
    'after': after,
    'expiresAt': nowStamp(DateTime.now().add(const Duration(seconds: 80))),
    'explanation': op == 'EDIT'
        ? '编辑后退回草稿，原有效意图停止发现；需要再次明确批准开启。'
        : '仅变更原意图；不清除对话和任务。',
  };
}

Map<String, dynamic> nowReceipt(Map<String, dynamic> preview) {
  final after =
      jsonDecode(jsonEncode(preview['after'])) as Map<String, dynamic>;
  after['version'] = 'b' * 64;
  after['intent']['updatedAt'] = nowStamp(DateTime.now());
  return {
    ...nowEnvelope(),
    'item': after,
    'operation': preview['operation'],
    'committed': true,
    'explanation': preview['explanation'],
  };
}

http.Response nowResponse(Map<String, dynamic> v) => http.Response(
  jsonEncode({'data': v}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
void main() {
  test(
    'converted association is strict optional trio and not sent back as draft authority',
    () {
      final v =
          jsonDecode(jsonEncode(nowItem(status: 'CONVERTED')))
              as Map<String, dynamic>;
      v['intent'].addAll({
        'convertedActivityId': 'a1111111-1111-4111-8111-111111111111',
        'convertedParticipationId': 'b1111111-1111-4111-8111-111111111111',
        'convertedAt': v['intent']['updatedAt'],
      });
      final item = ActiveSocialIntent(v, nowOwner);
      expect(item.draft.keys.any((k) => k.startsWith('converted')), false);
      final legacy = nowItem(status: 'CONVERTED');
      expect(
        ActiveSocialIntent(
          legacy,
          nowOwner,
        ).intent.containsKey('convertedActivityId'),
        false,
      );
      for (final change in <void Function(Map<String, dynamic>)>[
        (m) => m['intent'].remove('convertedAt'),
        (m) => m['intent']['convertedParticipationId'] = 'bad',
        (m) => m['intent']['status'] = 'ACTIVE',
        (m) => m['intent']['convertedAt'] = '2999-01-01T00:00:00Z',
      ]) {
        final bad = jsonDecode(jsonEncode(v)) as Map<String, dynamic>;
        change(bad);
        expect(() => ActiveSocialIntent(bad, nowOwner), throwsFormatException);
      }
    },
  );

  test(
    'actual registered native edit and cancellation JSONB wire remains compatible',
    () async {
      final all = intentMap(
        jsonDecode(
          await File(
            '../../docs/testing/evidence/active-social-intent-2026-10-04/native-wire1.json',
          ).readAsString(),
        ),
      );
      final detail = intentMap(intentMap(all['DETAIL'])['data']),
          owner = intentMap(detail['owner'])['id'] as String;
      ActiveSocialIntent(intentMap(detail['item']), owner);
      ActiveIntentList(intentMap(intentMap(all['LIST'])['data']), owner);
      ActiveIntentOptions(intentMap(intentMap(all['OPTIONS'])['data']), owner);
      for (final pair in [
        ('EDIT_PREVIEW', 'EDIT_RECEIPT'),
        ('PREVIEW', 'RECEIPT'),
      ]) {
        final raw = intentMap(intentMap(all[pair.$1])['data']);
        final before = ActiveSocialIntent(intentMap(raw['before']), owner);
        final p = ActiveIntentPreview(raw, owner, before, raw['operation']);
        final receipt = ActiveIntentReceipt(
          intentMap(intentMap(all[pair.$2])['data']),
          p,
        );
        expect(receipt.item.id, before.id);
      }
    },
  );
  test('closed native wire identity offset and time window', () {
    final m = nowItem();
    m['intent']['constraints']['startsAt'] = '2030-01-01T18:00:00+08:00';
    m['intent']['constraints']['endsAt'] = '2030-01-01T19:00:00+08:00';
    final v = ActiveSocialIntent(m, nowOwner);
    expect(v.startsAt, DateTime.utc(2030, 1, 1, 10));
    expect(v.endsAt, DateTime.utc(2030, 1, 1, 11));
    expect(v.expiresAt, isNot(v.startsAt));
    for (final mutate in <void Function(Map<String, dynamic>)>[
      (m) => m['unexpected'] = true,
      (m) => m['intent']['creatorAccountId'] = nowAgent,
      (m) => m['intent']['constraints']['latitude'] = 57.0,
      (m) => m['intent']['constraints']['startsAt'] = '2030-02-30T18:00:00Z',
      (m) => m['intent']['constraints']['endsAt'] = '2030-01-01T09:00:00Z',
    ]) {
      final invalid = jsonDecode(jsonEncode(m)) as Map<String, dynamic>;
      mutate(invalid);
      expect(
        () => ActiveSocialIntent(invalid, nowOwner),
        throwsFormatException,
      );
    }
  });
  test('exact preview only approve token and matched receipt', () async {
    final item = ActiveSocialIntent(nowItem(), nowOwner);
    final wire = nowPreview(itemToWire(item), 'CANCEL');
    final requests = <http.Request>[];
    final api = ActiveSocialIntentAPI(
      client: MockClient((r) async {
        requests.add(r);
        return nowResponse(
          r.url.path.endsWith('/preview') ? wire : nowReceipt(wire),
        );
      }),
    );
    final p = await api.preview('Bearer A', nowOwner, item, 'CANCEL', null);
    final receipt = await api.approve('Bearer A', p);
    expect(receipt.item.id, nowIntent);
    expect(jsonDecode(requests.last.body), {'previewId': p.token});
    expect(requests.last.headers['Authorization'], 'Bearer A');
    for (final change in <void Function(Map<String, dynamic>)>[
      (v) => v['modelAccess'] = true,
      (v) => v['observedAt'] = nowStamp(p.expiresAt),
      (v) => v['item']['intent']['id'] = nowAgent,
      (v) => v['item']['intent']['title'] = '另一内容',
    ]) {
      final v = nowReceipt(wire);
      change(v);
      expect(() => ActiveIntentReceipt(v, p), throwsFormatException);
    }
    api.dispose();
  });
  test('bounded options reject inferred unknown keys', () {
    final v = nowOptions();
    v['places'] = [
      {'id': nowIntent, 'label': '公开地点'},
    ];
    expect(ActiveIntentOptions(v, nowOwner).choices['places'], hasLength(1));
    final malformed = jsonDecode(jsonEncode(v)) as Map<String, dynamic>;
    malformed['places'][0]['latitude'] = 57.1;
    expect(
      () => ActiveIntentOptions(malformed, nowOwner),
      throwsFormatException,
    );
  });
}

Map<String, dynamic> itemToWire(ActiveSocialIntent i) => {
  'intent': i.intent,
  'version': i.version,
  'sourceAvailable': i.sourceAvailable,
  'locationLabel': i.locationLabel,
  'audienceLabel': i.audienceLabel,
};
