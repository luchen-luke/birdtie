import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_memory_correction_api.dart';
import 'package:birdtie_client/src/workspace/agent_memory_field_evidence.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'agent_memory_correction_api_test.dart';

// Synthetic projection of the registered human detail contract, not native
// PostgreSQL, verified identities, photo provenance or complete conflict data.
Map<String, dynamic> memoryDetailWire({
  DateTime? at,
  Map<String, dynamic>? memory,
  String status = 'ACTIVE',
  bool metadata = true,
}) {
  final t = at ?? correctionAt;
  final m = memory ?? correctionMemoryRaw(at: t);
  final until = t.add(const Duration(minutes: 2)).toIso8601String();
  final inferred = m['sourceType'] == 'INFERRED';
  final use = inferred
      ? 'CANDIDATE_ONLY'
      : m['memoryType'] == 'IDENTITY'
      ? 'NO_IDENTITY_AUTHORITY'
      : {'PLACE', 'CITY', 'HISTORY', 'EXPERIENCE'}.contains(m['memoryType'])
      ? 'NO_VISIT_OR_CURRENT_LOCATION_AUTHORITY'
      : 'DECLARATION_ONLY';
  final withheld = {'DELETED', 'EXPIRED'}.contains(status);
  return {
    'schemaVersion': 'agent-memory-detail-v1',
    'owner': {'type': 'PERSON', 'id': correctionOwner},
    'agentId': correctionAgent,
    'target': {'id': m['id'], 'version': m['version'], 'status': status},
    'memory': withheld ? null : m,
    'observedAt': t.toIso8601String(),
    'expiresAt': until,
    'explanation': memoryDetailExplanation,
    'modelAccess': false,
    if (metadata)
      'fieldEvidenceSet': {
        'schemaVersion': 'agent-field-evidence-v1',
        'owner': {'type': 'PERSON', 'id': correctionOwner},
        'observedAt': t.toIso8601String(),
        'expiresAt': until,
        'scope': 'HUMAN_MEMORY_DETAIL',
        'conflicts': [],
        'modelAccess': 'UNAVAILABLE',
        'mediaAccess': 'UNAVAILABLE',
        'grantsAuthority': false,
        'claims': [
          if (!withheld)
            {
              'id': 'HUMAN_MEMORY_DETAIL:${m['id']}:memory.summary',
              'itemKind': 'memories',
              'itemId': m['id'],
              'subjectKind': 'PERSON',
              'subjectId': correctionOwner,
              'field': 'memory.summary',
              'claimantKind': inferred
                  ? 'UNVERIFIED_INFERENCE'
                  : 'PERSON_DECLARATION',
              if (!inferred) 'claimantId': correctionOwner,
              'source': {
                'kind': 'HUMAN_MEMORY_DETAIL',
                'id': m['id'],
                'version': {'kind': 'REVISION', 'revision': m['version']},
                'nativeTime': m['updatedAt'],
              },
              'nature': inferred ? 'MODEL_INFERENCE' : 'USER_DECLARATION',
              'use': use,
              'observedAt': t.toIso8601String(),
              'collectedAt': t.toIso8601String(),
              'collectionEvent': 'CURRENT_NATIVE_READ',
              'sourceUpdatedAt': m['updatedAt'],
              'sourceCreatedAt': m['createdAt'],
              if (!inferred) 'declaredAt': m['updatedAt'],
              'validFrom': m['validFrom'],
              'validUntil': m['validUntil'],
              'captureTimeStatus': 'UNKNOWN_NOT_COLLECTED',
              'validityStatus': 'SOURCE_INTERVAL_KNOWN',
              'confidence': {
                'semantics': inferred
                    ? 'UNCALIBRATED_SCORE'
                    : 'DIRECT_DECLARATION',
                'value': m['confidence'],
              },
            },
        ],
      },
  };
}

MemoryFieldEvidenceDetail readDetail(dynamic d, {DateTime? at}) =>
    MemoryFieldEvidenceDetail.read(
      d,
      owner: correctionOwner,
      memoryID: correctionMemory,
      agentID: correctionAgent,
      version: 1,
      now: at ?? correctionAt,
    );

Map<String, dynamic> detailClaim(Map<String, dynamic> w) =>
    (w['fieldEvidenceSet']['claims'] as List).single;

void main() {
  test('单条来源：明确声明只读时间与未知拍摄时间，不授予权限', () {
    final d = readDetail(memoryDetailWire());
    expect(d.inferred, isFalse);
    expect(d.declaredAt, correctionAt);
    expect(d.createdAt, correctionAt);
    expect(d.memory!.summary, '我偏好徒步活动');
    expect(d.evidenceAvailable, isTrue);
  });
  test('单条来源：待审推断无本人声明时间，不把confidence当概率', () {
    final m = correctionMemoryRaw()
      ..['sourceType'] = 'INFERRED'
      ..['status'] = 'PENDING_REVIEW'
      ..['confidence'] = .4;
    final d = readDetail(memoryDetailWire(memory: m, status: 'PENDING_REVIEW'));
    expect(d.inferred, isTrue);
    expect(d.declaredAt, isNull);
  });
  for (final status in ['EXPIRED', 'DELETED']) {
    test('单条来源：$status仅当前墓碑，无旧正文或声明', () {
      final d = readDetail(memoryDetailWire(status: status));
      expect(d.status, status);
      expect(d.memory, isNull);
      expect(d.createdAt, isNull);
      expect(d.declaredAt, isNull);
    });
  }
  test('单条来源：旧详情缺metadata明确不可用，不补全冲突', () {
    final d = readDetail(memoryDetailWire(metadata: false));
    expect(d.evidenceAvailable, isFalse);
    expect(d.declaredAt, isNull);
  });
  for (final type in ['IDENTITY', 'PLACE', 'CITY', 'HISTORY', 'EXPERIENCE']) {
    test('单条来源：$type声明不授身份或当前位置到访权限', () {
      final m = correctionMemoryRaw()..['memoryType'] = type;
      final w = memoryDetailWire(memory: m);
      expect(readDetail(w).memory!.raw['memoryType'], type);
      detailClaim(w)['use'] = 'DECLARATION_ONLY';
      expect(() => readDetail(w), throwsFormatException);
    });
  }
  test('单条来源：推断不得虚构声明人或声明时刻', () {
    final m = correctionMemoryRaw()
      ..['sourceType'] = 'INFERRED'
      ..['status'] = 'PENDING_REVIEW'
      ..['confidence'] = .4;
    final w = memoryDetailWire(memory: m, status: 'PENDING_REVIEW');
    detailClaim(w)['declaredAt'] = correctionAt.toIso8601String();
    expect(() => readDetail(w), throwsFormatException);
    detailClaim(w).remove('declaredAt');
    detailClaim(w)['claimantId'] = correctionOwner;
    expect(() => readDetail(w), throwsFormatException);
  });
  final corrupt = <String, void Function(Map<String, dynamic>)>{
    'owner': (w) => w['owner']['id'] = correctionID,
    'agent': (w) => w['agentId'] = correctionID,
    'targetID': (w) => w['target']['id'] = correctionID,
    'targetVersion': (w) => w['target']['version'] = 2,
    'targetStatus': (w) => w['target']['status'] = 'PENDING_REVIEW',
    'bodyOwner': (w) => w['memory']['ownerId'] = correctionID,
    'model': (w) => w['modelAccess'] = true,
    'futureObserved': (w) => w['observedAt'] = correctionAt
        .add(const Duration(seconds: 1))
        .toIso8601String(),
    'expired': (w) => w['expiresAt'] = correctionAt.toIso8601String(),
    'longLease': (w) => w['expiresAt'] = correctionAt
        .add(const Duration(minutes: 3))
        .toIso8601String(),
    'setOwner': (w) => w['fieldEvidenceSet']['owner']['id'] = correctionID,
    'scope': (w) => w['fieldEvidenceSet']['scope'] = 'COMPLETE',
    'authority': (w) => w['fieldEvidenceSet']['grantsAuthority'] = true,
    'media': (w) => w['fieldEvidenceSet']['mediaAccess'] = 'READY',
    'conflict': (w) => w['fieldEvidenceSet']['conflicts'] = [{}],
    'twoClaims': (w) => w['fieldEvidenceSet']['claims'].add(
      jsonDecode(jsonEncode(detailClaim(w))),
    ),
    'item': (w) => detailClaim(w)['itemId'] = correctionID,
    'subject': (w) => detailClaim(w)['subjectId'] = correctionID,
    'field': (w) => detailClaim(w)['field'] = 'identity',
    'sourceID': (w) => detailClaim(w)['source']['id'] = correctionID,
    'sourceRevision': (w) =>
        detailClaim(w)['source']['version']['revision'] = 2,
    'sourceToken': (w) =>
        detailClaim(w)['source']['version']['token'] = 'not-native',
    'sourceTime': (w) => detailClaim(w)['source']['nativeTime'] = correctionAt
        .subtract(const Duration(seconds: 1))
        .toIso8601String(),
    'declaredTime': (w) => detailClaim(w)['declaredAt'] = correctionAt
        .subtract(const Duration(seconds: 1))
        .toIso8601String(),
    'capturedAt': (w) =>
        detailClaim(w)['capturedAt'] = correctionAt.toIso8601String(),
    'captureStatus': (w) => detailClaim(w)['captureTimeStatus'] = 'KNOWN',
    'use': (w) => detailClaim(w)['use'] = 'CURRENT_LOCATION',
    'confidenceProbability': (w) =>
        detailClaim(w)['confidence']['semantics'] = 'CALIBRATED_PROBABILITY',
    'createdTime': (w) => detailClaim(w)['sourceCreatedAt'] = correctionAt
        .subtract(const Duration(days: 1))
        .toIso8601String(),
    'collectedTime': (w) => detailClaim(w)['collectedAt'] = correctionAt
        .subtract(const Duration(seconds: 1))
        .toIso8601String(),
  };
  for (final entry in corrupt.entries) {
    test('单条来源：拒绝${entry.key}错配', () {
      final w = memoryDetailWire();
      entry.value(w);
      expect(() => readDetail(w), throwsFormatException);
    });
  }
  test('单条来源：原传输GET同ID详情，裸列表和operation路径不混用', () async {
    final paths = <String>[];
    final api = AgentMemoryCorrectionAPI(
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        expect(r.method, 'GET');
        expect(r.headers['Authorization'], 'Bearer synthetic');
        expect(r.body, isEmpty);
        paths.add(r.url.path);
        return correctionResponse({
          'data': r.url.path.endsWith(correctionMemory)
              ? memoryDetailWire()
              : [correctionMemoryRaw()],
        });
      }),
    );
    final list = await api.memories('Bearer synthetic', correctionOwner);
    expect(list.single.summary, '我偏好徒步活动');
    final raw = await api.request(
      'GET',
      'memories/$correctionMemory',
      'Bearer synthetic',
    );
    expect(readDetail(raw).id, list.single.id);
    expect(paths, [
      '/v1/me/agent-memories',
      '/v1/me/agent-memories/$correctionMemory',
    ]);
    expect(
      () =>
          api.request('POST', 'memories/$correctionMemory', 'Bearer synthetic'),
      throwsFormatException,
    );
  });
}
