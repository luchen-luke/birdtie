import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_memory_correction_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const correctionOwner = '82000000-0000-4000-8000-000000000001';
const correctionAgent = '82000000-0000-4000-8000-000000000002';
const correctionMemory = '82000000-0000-4000-8000-000000000003';
const correctionID = '82000000-0000-4000-8000-000000000004';
final correctionAt = DateTime.utc(2030, 1, 1);
String get correctionPlan => 'b' * 64;
Map<String, dynamic> correctionMemoryRaw({DateTime? at}) {
  final t = at ?? correctionAt;
  return {
    'schemaVersion': 'agent-memory-v1',
    'id': correctionMemory,
    'agentId': correctionAgent,
    'ownerType': 'PERSON',
    'ownerId': correctionOwner,
    'version': 1,
    'memoryType': 'PREFERENCE',
    'memoryKey': 'activity_category:hiking',
    'summary': '我偏好徒步活动',
    'structuredValue': {'activityCategory': 'hiking'},
    'confidence': 1,
    'sourceType': 'EXPLICIT',
    'visibility': 'PRIVATE',
    'status': 'ACTIVE',
    'validFrom': t.toIso8601String(),
    'validUntil': t.add(const Duration(days: 1)).toIso8601String(),
    'lastReinforcedAt': null,
    'createdAt': t.toIso8601String(),
    'updatedAt': t.toIso8601String(),
  };
}

Map<String, dynamic> correctionInputRaw({
  String id = correctionID,
  String action = 'REJECT',
}) => {
  'id': id,
  'targetKind': 'MEMORY',
  'targetId': correctionMemory,
  'expectedVersion': 1,
  'action': action,
  if (action == 'NEGATE') 'category': 'hiking',
  if (action == 'EDIT')
    'replacement': CorrectableMemory.read(
      correctionMemoryRaw(),
      correctionOwner,
    ).replacement('本人明确修订'),
};
Map<String, dynamic> correctionPreviewRaw({
  Map<String, dynamic>? input,
  DateTime? at,
  Map<String, dynamic>? memory,
}) {
  final t = at ?? correctionAt, x = input ?? correctionInputRaw();
  return {
    'schemaVersion': correctionSchema,
    'id': x['id'],
    'owner': {'type': 'PERSON', 'id': correctionOwner},
    'agentId': correctionAgent,
    'input': x,
    'memories': [memory ?? correctionMemoryRaw(at: t)],
    'affected': [
      {'kind': 'MEMORY', 'id': correctionMemory, 'version': 1},
    ],
    'planDigest': correctionPlan,
    'observedAt': t.toIso8601String(),
    'expiresAt': t.add(const Duration(minutes: 5)).toIso8601String(),
    'explanation': correctionExplanation,
    'modelAccess': false,
    if (x['action'] == 'NEGATE')
      'newMemoryValidUntil': t.add(const Duration(days: 365)).toIso8601String(),
  };
}

Map<String, dynamic> correctionReceiptRaw({
  Map<String, dynamic>? input,
  String state = 'COMMITTED',
  bool current = true,
  DateTime? at,
}) {
  final t = at ?? correctionAt, x = input ?? correctionInputRaw();
  return {
    'schemaVersion': correctionSchema,
    'id': x['id'],
    'owner': {'type': 'PERSON', 'id': correctionOwner},
    'agentId': correctionAgent,
    'target': {
      'kind': x['targetKind'],
      'id': x['targetId'],
      'version': x['expectedVersion'],
    },
    'action': x['action'],
    'planDigest': correctionPlan,
    'state': state,
    'currentResultMatches': state == 'COMMITTED' && current,
    'suppressionActive': state == 'COMMITTED' && x['action'] == 'NEGATE',
    'observedAt': t
        .add(Duration(minutes: state == 'EXPIRED' ? 6 : 0, seconds: 2))
        .toIso8601String(),
    'expiresAt': t.add(const Duration(minutes: 5)).toIso8601String(),
    'modelAccess': false,
    if (state == 'COMMITTED') 'resultMemoryId': correctionMemory,
    if (state == 'COMMITTED') 'resultMemoryVersion': 2,
    if (state == 'COMMITTED')
      'committedAt': t.add(const Duration(seconds: 1)).toIso8601String(),
  };
}

http.Response correctionResponse(dynamic value, {int status = 200}) =>
    http.Response(
      jsonEncode(value),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
void main() {
  test('真实注册HTTP原Memory data与bare preview完整闭集', () {
    final list = jsonDecode(correctionNativeMemoryWire)['data'] as List;
    final p = jsonDecode(correctionNativePreviewWire);
    final owner = p['owner']['id'] as String;
    expect(
      CorrectableMemory.read(list.single, owner).id,
      p['input']['targetId'],
    );
    expect(CorrectionPreview.read(p, owner).memories.single.raw, list.single);
  });
  test('真实注册bare preview拒新增data包装/错owner', () {
    final p = jsonDecode(correctionNativePreviewWire),
        owner = p['owner']['id'] as String;
    expect(
      () => CorrectionPreview.read({'data': p}, owner),
      throwsFormatException,
    );
    expect(
      () => CorrectionPreview.read(p, correctionOwner),
      throwsFormatException,
    );
  });
  test('真实confirm与新Session GET均仅历史metadata', () {
    for (final raw in [
      correctionNativeConfirmReceiptWire,
      correctionNativeSessionReceiptWire,
    ]) {
      final m = jsonDecode(raw);
      final r = CorrectionReceipt.read(m, m['owner']['id']);
      expect(r.state, 'COMMITTED');
      expect(m.containsKey('memories'), false);
      expect(m.containsKey('input'), false);
    }
  });
  test('真实wire传输GET无body，POST精确原input不包data', () async {
    final input = Map<String, dynamic>.from(
          jsonDecode(correctionNativeInputWire),
        ),
        seen = <http.Request>[];
    final p = jsonDecode(correctionNativePreviewWire),
        owner = p['owner']['id'] as String;
    final api = AgentMemoryCorrectionAPI(
      apiBaseUrl: 'http://native-fixture',
      client: MockClient((r) async {
        seen.add(r);
        return http.Response(
          r.url.path.endsWith('agent-memories')
              ? correctionNativeMemoryWire
              : r.url.path.endsWith('previews')
              ? correctionNativePreviewWire
              : correctionNativeSessionReceiptWire,
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }),
    );
    expect(
      (await api.memories('Bearer synthetic', owner)).single.id,
      input['targetId'],
    );
    expect(
      CorrectionPreview.read(
        await api.request('POST', 'previews', 'Bearer synthetic', body: input),
        owner,
      ).id,
      input['id'],
    );
    expect(
      CorrectionReceipt.read(
        await api.request('GET', input['id'], 'Bearer synthetic'),
        owner,
      ).state,
      'COMMITTED',
    );
    expect(
      seen
          .where((r) => r.method == 'GET')
          .every(
            (r) =>
                r.bodyBytes.isEmpty && !r.headers.containsKey('content-type'),
          ),
      true,
    );
    expect(jsonDecode(seen[1].body), input);
  });
  test('UTC归一后越出有限年界的来源时刻必须拒绝', () {
    final m = correctionMemoryRaw();
    m['validFrom'] = '0001-01-01T00:00:00+23:00';
    m['validUntil'] = '0001-01-02T00:00:00Z';
    m['createdAt'] = m['validFrom'];
    m['updatedAt'] = m['validUntil'];
    expect(
      () => CorrectableMemory.read(m, correctionOwner),
      throwsFormatException,
    );
  });
  test('原列表data与新纠正bare契约；所有GET真空body', () async {
    final seen = <http.Request>[];
    final api = AgentMemoryCorrectionAPI(
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        seen.add(r);
        return correctionResponse(
          r.url.path.endsWith('agent-memories')
              ? {
                  'data': [correctionMemoryRaw()],
                }
              : r.url.path.endsWith('agent-memory-candidates')
              ? {'data': []}
              : r.url.path.endsWith('previews')
              ? correctionPreviewRaw()
              : correctionReceiptRaw(),
        );
      }),
    );
    expect(
      (await api.memories('Bearer synthetic', correctionOwner)).single.id,
      correctionMemory,
    );
    expect(await api.candidates('Bearer synthetic', correctionOwner), isEmpty);
    expect(
      CorrectionPreview.read(
        await api.request(
          'POST',
          'previews',
          'Bearer synthetic',
          body: correctionInputRaw(),
        ),
        correctionOwner,
      ).memories.single.summary,
      '我偏好徒步活动',
    );
    await api.request(
      'POST',
      '$correctionID/confirm',
      'Bearer synthetic',
      body: {'planDigest': correctionPlan},
    );
    await api.request('GET', correctionID, 'Bearer synthetic');
    for (final r in seen.where((r) => r.method == 'GET')) {
      expect(r.bodyBytes, isEmpty);
      expect(r.headers.containsKey('content-type'), false);
      expect(r.url.query, isEmpty);
    }
    expect(seen[2].body, jsonEncode(correctionInputRaw()));
    expect(seen[3].body, jsonEncode({'planDigest': correctionPlan}));
  });
  test('method/path/body闭集在网络前拒绝', () async {
    var calls = 0;
    final api = AgentMemoryCorrectionAPI(
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        calls++;
        return correctionResponse({});
      }),
    );
    for (final action in [
      () => api.request('GET', correctionID, 'Bearer x', body: {}),
      () => api.request('DELETE', correctionID, 'Bearer x'),
      () => api.request(
        'POST',
        'previews',
        'Bearer x',
        body: {...correctionInputRaw(), 'ownerId': correctionOwner},
      ),
      () => api.request(
        'POST',
        '$correctionID/confirm',
        'Bearer x',
        body: {'planDigest': correctionPlan, 'approve': true},
      ),
      () => api.request('GET', '$correctionID?x=1', 'Bearer x'),
    ]) {
      await expectLater(action(), throwsFormatException);
    }
    expect(calls, 0);
  });
  for (final mutate in [
    'missing-memory',
    'extra-memory',
    'owner',
    'version',
    'model',
    'expired-shape',
    'nonfinite',
    'probability',
  ]) {
    test('具体preview拒绝 $mutate', () {
      final p = correctionPreviewRaw();
      switch (mutate) {
        case 'missing-memory':
          p['memories'] = [];
        case 'extra-memory':
          p['memories'] = [correctionMemoryRaw(), correctionMemoryRaw()];
        case 'owner':
          p['owner'] = {'type': 'PERSON', 'id': correctionMemory};
        case 'version':
          p['affected'] = [
            {'kind': 'MEMORY', 'id': correctionMemory, 'version': 2},
          ];
        case 'model':
          p['modelAccess'] = true;
        case 'expired-shape':
          p['expiresAt'] = p['observedAt'];
        case 'nonfinite':
          p['expiresAt'] = 'infinity';
        case 'probability':
          p['memories'] = [
            {
              ...correctionMemoryRaw(),
              'sourceType': 'INFERRED',
              'status': 'ACTIVE',
              'confidence': 0.99,
            },
          ];
      }
      expect(
        () => CorrectionPreview.read(p, correctionOwner),
        throwsFormatException,
      );
    });
  }
  test('同类仅闭集key，不能从summary猜；REJECT保留具体动作', () {
    final m = CorrectableMemory.read({
      ...correctionMemoryRaw(),
      'memoryKey': 'free.note',
      'summary': '我喜欢徒步',
    }, correctionOwner);
    expect(m.category, isNull);
    expect(correctionInput(correctionInputRaw())['action'], 'REJECT');
    expect(
      () => correctionInput({
        ...correctionInputRaw(action: 'NEGATE'),
        'category': 'guess',
      }),
      throwsFormatException,
    );
  });
  test('历史COMMITTED与当前是否一致分别表达', () {
    final r = CorrectionReceipt.read(
      correctionReceiptRaw(current: false),
      correctionOwner,
    );
    expect(r.state, 'COMMITTED');
    expect(r.currentMatches, false);
    expect(
      () => CorrectionReceipt.read({
        ...correctionReceiptRaw(),
        'resultMemoryVersion': 3,
      }, correctionOwner),
      throwsFormatException,
    );
    expect(
      () => CorrectionReceipt.read({
        ...correctionReceiptRaw(state: 'PENDING'),
        'resultMemoryId': correctionMemory,
      }, correctionOwner),
      throwsFormatException,
    );
  });
}

// Exact owned native httpapi.New JSON from root-wire-export011-38mx; no token.
const correctionNativeMemoryWire =
    r'''{"data":[{"schemaVersion":"agent-memory-v1","id":"546726c0-71a7-41ce-8545-110e300b0319","agentId":"8511aa4d-b7cb-43dc-95c1-7b77806bca00","ownerType":"PERSON","ownerId":"b22be1b3-b486-4434-b11e-a02db0a5f4f6","version":1,"memoryType":"PREFERENCE","memoryKey":"registered.correction","summary":"合成本人原声明","structuredValue":{"native":true},"confidence":1,"sourceType":"EXPLICIT","visibility":"PRIVATE","status":"ACTIVE","validFrom":"2026-10-06T00:10:11.285278+08:00","validUntil":"2026-10-06T01:10:11.276901+08:00","lastReinforcedAt":null,"createdAt":"2026-10-06T00:10:11.285278+08:00","updatedAt":"2026-10-06T00:10:11.285278+08:00"}]}
''';
const correctionNativePreviewWire =
    r'''{"schemaVersion":"agent-memory-correction-v1","id":"4b7cc457-45a5-4f46-a4a4-3f8fe46b162f","owner":{"type":"PERSON","id":"b22be1b3-b486-4434-b11e-a02db0a5f4f6"},"agentId":"8511aa4d-b7cb-43dc-95c1-7b77806bca00","input":{"id":"4b7cc457-45a5-4f46-a4a4-3f8fe46b162f","targetKind":"MEMORY","targetId":"546726c0-71a7-41ce-8545-110e300b0319","expectedVersion":1,"action":"REJECT"},"memories":[{"schemaVersion":"agent-memory-v1","id":"546726c0-71a7-41ce-8545-110e300b0319","agentId":"8511aa4d-b7cb-43dc-95c1-7b77806bca00","ownerType":"PERSON","ownerId":"b22be1b3-b486-4434-b11e-a02db0a5f4f6","version":1,"memoryType":"PREFERENCE","memoryKey":"registered.correction","summary":"合成本人原声明","structuredValue":{"native":true},"confidence":1,"sourceType":"EXPLICIT","visibility":"PRIVATE","status":"ACTIVE","validFrom":"2026-10-06T00:10:11.285278+08:00","validUntil":"2026-10-06T01:10:11.276901+08:00","lastReinforcedAt":null,"createdAt":"2026-10-06T00:10:11.285278+08:00","updatedAt":"2026-10-06T00:10:11.285278+08:00"}],"affected":[{"kind":"MEMORY","id":"546726c0-71a7-41ce-8545-110e300b0319","version":1}],"planDigest":"f74aaa33d1fc63d4e81942a56931eb832d4ce30f6edf194b0280b556bee36e62","observedAt":"2026-10-05T16:10:11.323556Z","expiresAt":"2026-10-05T16:15:11.323556Z","explanation":"这是你本人的具体纠正。明确选择不偏好某类活动后，会停止以任何来源再次提出该类偏好；不代表概率，也不删除原动态。本人独立声明不因关联来源到期而删除。删除纠正记忆不会恢复旧推断或旧批准。","modelAccess":false}
''';
const correctionNativeConfirmReceiptWire =
    r'''{"schemaVersion":"agent-memory-correction-v1","id":"4b7cc457-45a5-4f46-a4a4-3f8fe46b162f","owner":{"type":"PERSON","id":"b22be1b3-b486-4434-b11e-a02db0a5f4f6"},"agentId":"8511aa4d-b7cb-43dc-95c1-7b77806bca00","target":{"kind":"MEMORY","id":"546726c0-71a7-41ce-8545-110e300b0319","version":1},"action":"REJECT","planDigest":"f74aaa33d1fc63d4e81942a56931eb832d4ce30f6edf194b0280b556bee36e62","state":"COMMITTED","resultMemoryId":"546726c0-71a7-41ce-8545-110e300b0319","resultMemoryVersion":2,"committedAt":"2026-10-06T00:10:11.348844+08:00","currentResultMatches":true,"suppressionActive":false,"observedAt":"2026-10-06T00:10:11.359448+08:00","expiresAt":"2026-10-06T00:15:11.323556+08:00","modelAccess":false}
''';
const correctionNativeSessionReceiptWire =
    r'''{"schemaVersion":"agent-memory-correction-v1","id":"4b7cc457-45a5-4f46-a4a4-3f8fe46b162f","owner":{"type":"PERSON","id":"b22be1b3-b486-4434-b11e-a02db0a5f4f6"},"agentId":"8511aa4d-b7cb-43dc-95c1-7b77806bca00","target":{"kind":"MEMORY","id":"546726c0-71a7-41ce-8545-110e300b0319","version":1},"action":"REJECT","planDigest":"f74aaa33d1fc63d4e81942a56931eb832d4ce30f6edf194b0280b556bee36e62","state":"COMMITTED","resultMemoryId":"546726c0-71a7-41ce-8545-110e300b0319","resultMemoryVersion":2,"committedAt":"2026-10-06T00:10:11.348844+08:00","currentResultMatches":true,"suppressionActive":false,"observedAt":"2026-10-06T00:10:11.371076+08:00","expiresAt":"2026-10-06T00:15:11.323556+08:00","modelAccess":false}
''';
const correctionNativeInputWire =
    r'''{"id":"4b7cc457-45a5-4f46-a4a4-3f8fe46b162f","targetKind":"MEMORY","targetId":"546726c0-71a7-41ce-8545-110e300b0319","expectedVersion":1,"action":"REJECT"}''';
