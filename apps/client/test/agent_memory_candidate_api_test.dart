import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const candidateOwner = '11111111-1111-4111-8111-111111111111';
const candidateAgent = '22222222-2222-4222-8222-222222222222';
const candidateTarget = '33333333-3333-4333-8333-333333333333';
const candidateMoment = '44444444-4444-4444-8444-444444444444';
const candidateSaved = '55555555-5555-4555-8555-555555555555';
const candidateMemory = '66666666-6666-4666-8666-666666666666';
Map<String, dynamic> candidateWire({String status = 'CANDIDATE'}) {
  final pending = status == 'CANDIDATE' || status == 'ACTIVE';
  final now = DateTime.now().toUtc();
  return {
    'schemaVersion': 'agent-memory-candidate-v1',
    'id': candidateTarget,
    'agentId': candidateAgent,
    'owner': {'type': 'person', 'id': candidateOwner},
    'version': status == 'CANDIDATE' ? 1 : 2,
    'status': status,
    if (pending) 'predicate': 'ACTIVITY_CATEGORY',
    if (pending) 'category': 'badminton',
    if (pending) 'assessment': {'semantics': 'ORDINAL', 'level': 'LOW'},
    'sources': pending
        ? [
            for (final s in [
              ['MOMENT', candidateMoment],
              ['SAVED_PLACE', candidateSaved],
            ])
              {
                'selector': {'type': s[0], 'id': s[1]},
                'version': s[0] == 'MOMENT'
                    ? {'kind': 'REVISION', 'revision': 1}
                    : {'kind': 'CREATED_AT_DIGEST', 'token': 'c' * 64},
                'eventTime': now
                    .subtract(const Duration(minutes: 1))
                    .toIso8601String(),
                'fingerprint': 'a' * 64,
                'anchors': [],
              },
          ]
        : [],
    'validUntil': now.add(const Duration(hours: 1)).toIso8601String(),
    'createdAt': now.toIso8601String(),
    'updatedAt': now.toIso8601String(),
    if (status == 'ACTIVE') 'memoryId': candidateMemory,
    if (status == 'ACTIVE') 'memoryVersion': 1,
    'modelAccess': 'UNAVAILABLE',
  };
}

Map<String, dynamic> previewWire(String id, Map<String, dynamic> c) => {
  'previewId': id,
  'review': {
    'candidate': c,
    'statement': '我偏好羽毛球活动',
    'targetMemoryId': candidateMemory,
    'expectedMemoryVersion': 0,
    'clusters': 2,
    'planDigest': 'b' * 64,
    'purpose': 'HUMAN_EXPLICIT_DECLARATION',
    'memoryValidUntil': DateTime.now()
        .toUtc()
        .add(const Duration(days: 1))
        .toIso8601String(),
    'expiresAt': DateTime.now()
        .toUtc()
        .add(const Duration(minutes: 4))
        .toIso8601String(),
  },
};
http.Response candidateResponse(dynamic value) => http.Response(
  jsonEncode({'data': value}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
// Exact historical native1 registered receipt; metadata parsing is not authority.
const historicalHikingReceiptWire = r'''
{"schemaVersion":"agent-multi-candidate-v1","state":"CANDIDATE_STAGED","retentionGrantId":"44538a0e-3b2f-4afa-89c6-117797617462","owner":{"type":"PERSON","id":"533fcfb3-0f17-4dc4-a4dc-eebf942852ff"},"agentId":"e4fb42bb-21b4-431d-a4ef-d1910948208f","eventId":"71b21904-86e5-8cbe-a0ff-0ba9f84079ce","logicalOperationId":"518a971e-707f-8180-ba9b-a4bcc7979aff","effectKey":"93b5aabfe3a244b5497b8d99c54bb7daddd2c572ef8e9f351cc5869f2e3bee68","handlerVersion":"mom-candidate-multi-v1","candidateId":"669939c0-76d6-4ec6-afb6-0ea18db2285b","candidate":{"schemaVersion":"agent-memory-candidate-v1","id":"669939c0-76d6-4ec6-afb6-0ea18db2285b","agentId":"e4fb42bb-21b4-431d-a4ef-d1910948208f","owner":{"type":"PERSON","id":"533fcfb3-0f17-4dc4-a4dc-eebf942852ff"},"version":1,"status":"CANDIDATE","predicate":"ACTIVITY_CATEGORY","category":"hiking","assessment":{"semantics":"ORDINAL","level":"LOW"},"sources":[{"selector":{"type":"MOMENT","id":"450a4682-f0e2-4bda-b0be-cf568c01f2dc"},"version":{"kind":"REVISION","revision":3},"eventTime":"2026-10-05T10:02:23.470708Z","fingerprint":"26342b4b40e035fef1203c4ea0e5152dbe2c54d5004905be7cf7c139d974211b","anchors":["MOMENT:450a4682-f0e2-4bda-b0be-cf568c01f2dc"]},{"selector":{"type":"MOMENT","id":"90bbd776-e3d0-4831-80a5-e06d3b621e5b"},"version":{"kind":"REVISION","revision":1},"eventTime":"2026-10-05T10:02:23.506418Z","fingerprint":"4ff1706c2507b32d40521ec94b3c7befdefe90c4b083c9c4de75c3d10f38d4e2","anchors":["MOMENT:90bbd776-e3d0-4831-80a5-e06d3b621e5b"]}],"validUntil":"2026-10-05T10:02:54.054454Z","createdAt":"2026-10-05T10:02:24.054454Z","updatedAt":"2026-10-05T10:02:24.060584Z","modelAccess":"UNAVAILABLE"},"observedAt":"2026-10-05T10:02:24.730707Z","committed":true,"modelAccess":false,"memoryPromotionAllowed":false,"explanation":"已在原生事务中保留一项私密待核验候选；关键词不证明偏好或到场，不调用模型，不写正式记忆。"}
''';

void main() {
  test(
    'recovery environment is the actual transport prefix with no journal URL body',
    () {
      final a = AgentMemoryCandidateAPI(
        client: MockClient((r) async => candidateResponse([])),
        apiBaseUrl: 'http://fixture/',
      );
      final b = AgentMemoryCandidateAPI(
        client: MockClient((r) async => candidateResponse([])),
        apiBaseUrl: 'http://fixture',
      );
      expect(a.recoveryEnvironment, b.recoveryEnvironment);
      expect(a.recoveryEnvironment, 'http://fixture');
      a.dispose();
      b.dispose();
    },
  );

  test(
    'actual canonical PERSON candidate retains native owner and pending status',
    () {
      final receipt =
          jsonDecode(historicalHikingReceiptWire) as Map<String, dynamic>;
      final raw = receipt['candidate'];
      final owner = receipt['owner']['id'] as String;
      final c = HumanMemoryCandidate.read(raw, owner);
      expect(c.category, 'hiking');
      expect(c.status, 'CANDIDATE');
      for (final wrong in [
        'ORGANIZATION',
        'organization',
        'BUSINESS',
        'Person',
        'pErSoN',
        ' PERSON',
        'PERSON ',
        null,
        1,
      ]) {
        final bad = jsonDecode(jsonEncode(raw));
        bad['owner']['type'] = wrong;
        expect(
          () => HumanMemoryCandidate.read(bad, owner),
          throwsFormatException,
        );
      }
    },
  );

  test(
    'hiking candidate has Chinese label and human statement without probability',
    () {
      final wire = candidateWire()..['category'] = 'hiking';
      final c = HumanMemoryCandidate.read(wire, candidateOwner);
      expect(c.category, 'hiking');
      expect(candidateCategories['hiking'], '徒步');
      expect(wire['assessment']['value'], isNull);
    },
  );
  test('five lifecycle states are typed and no fake probability', () {
    for (final s in candidateStatusLabels.keys) {
      final c = HumanMemoryCandidate.read(
        candidateWire(status: s),
        candidateOwner,
      );
      expect(c.status, s);
    }
    expect(candidateWire()['assessment']['value'], isNull);
  });
  for (final mutation in [
    'owner',
    'agent',
    'version',
    'model',
    'status',
    'sources',
    'terminalPayload',
    'activeMissingMemory',
  ]) {
    test('reject invalid $mutation', () {
      final m = candidateWire();
      switch (mutation) {
        case 'owner':
          m['owner'] = {'type': 'person', 'id': candidateAgent};
        case 'agent':
          m['agentId'] = 'bad';
        case 'version':
          m['version'] = 0;
        case 'model':
          m['modelAccess'] = 'ON';
        case 'status':
          m['status'] = 'PENDING';
        case 'sources':
          m['sources'] = [];
        case 'terminalPayload':
          m['status'] = 'EXPIRED';
        case 'activeMissingMemory':
          m['status'] = 'ACTIVE';
      }
      expect(
        () => HumanMemoryCandidate.read(m, candidateOwner),
        throwsA(anything),
      );
    });
  }
  test('opaque preview validates exact identity, version and expiry', () {
    final c = HumanMemoryCandidate.read(candidateWire(), candidateOwner);
    final k = 'a' * 64;
    expect(
      HumanCandidatePreview.read(
        previewWire(k, c.raw),
        candidateOwner,
        c,
        k,
      ).statement,
      '我偏好羽毛球活动',
    );
    for (final name in ['key', 'version', 'statement', 'expiry', 'purpose']) {
      final p = previewWire(k, Map.from(c.raw));
      final r = p['review'] as Map;
      switch (name) {
        case 'key':
          p['previewId'] = 'b' * 64;
        case 'version':
          (r['candidate'] as Map)['version'] = 2;
        case 'statement':
          r['statement'] = '虚构事实';
        case 'expiry':
          r['expiresAt'] = DateTime.now()
              .toUtc()
              .subtract(const Duration(seconds: 1))
              .toIso8601String();
        case 'purpose':
          r['purpose'] = 'MODEL_READ';
      }
      expect(
        () => HumanCandidatePreview.read(p, candidateOwner, c, k),
        throwsA(anything),
      );
    }
  });
  test(
    'source picker uses actual native record IDs rather than entity targets',
    () async {
      final client = MockClient(
        (r) async => candidateResponse(switch (r.url.path) {
          '/v1/me/moments' => [
            {
              'id': candidateMoment,
              'authorAccountId': candidateOwner,
              'status': 'draft',
              'title': '我的真实动态',
            },
          ],
          '/v1/me/participations' => [
            {
              'id': candidateTarget,
              'activityId': candidateMemory,
              'status': 'going',
              'available': true,
              'title': '本人报名',
            },
          ],
          _ => [
            {
              'id': candidateSaved,
              'targetId': candidateMemory,
              'kind': 'place',
              'available': true,
              'title': '本人保存',
            },
          ],
        }),
      );
      final a = AgentMemoryCandidateAPI(
        client: client,
        apiBaseUrl: 'http://fixture',
      );
      final c = await a.sources('Bearer t', candidateOwner);
      expect(c.map((e) => e.id), [
        candidateMoment,
        candidateTarget,
        candidateSaved,
      ]);
      expect(c[1].detail, contains('不等于实际到场'));
      client.close();
    },
  );
  test('defaultOFF 503 Chinese honest error', () async {
    final c = MockClient((_) async => http.Response('{}', 503));
    final a = AgentMemoryCandidateAPI(client: c, apiBaseUrl: 'http://fixture');
    await expectLater(
      a.list('Bearer t', candidateOwner),
      throwsA(
        isA<CandidateAPIError>().having(
          (e) => e.message,
          'message',
          contains('未启用'),
        ),
      ),
    );
    c.close();
  });
}
