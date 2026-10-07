import '../content/agent_profile_completion_api.dart'
    show completionMap, completionVersion;
import 'agent_memory_candidate_api.dart' show candidateIDValid;
import 'agent_memory_correction_api.dart'
    show CorrectableMemory, correctionTime;

const memoryDetailExplanation =
    '本人管理记忆；过期或已丢弃内容不在详情中返回。待审推断仅为预留形状，不代表已确认事实或模型读取许可。';

void _check(bool ok) {
  if (!ok) throw const FormatException('记忆来源详情不符');
}

bool _sameTime(dynamic value, DateTime expected) =>
    correctionTime(value) == expected;

/// A single current human read. Neither its score nor its empty conflict list
/// confers truth, model access, a correction approval or complete conflict data.
class MemoryFieldEvidenceDetail {
  MemoryFieldEvidenceDetail._({
    required this.id,
    required this.version,
    required this.status,
    required this.memory,
    required this.observedAt,
    required this.expiresAt,
    required this.evidenceAvailable,
  });
  final String id, status;
  final int version;
  final CorrectableMemory? memory;
  final DateTime observedAt, expiresAt;
  final bool evidenceAvailable;
  bool get inferred => memory?.sourceType == 'INFERRED';
  DateTime? get createdAt =>
      memory == null ? null : correctionTime(memory!.raw['createdAt']);
  DateTime? get updatedAt =>
      memory == null ? null : correctionTime(memory!.raw['updatedAt']);
  DateTime? get declaredAt => !evidenceAvailable || inferred ? null : updatedAt;
  DateTime? get validFrom =>
      memory == null ? null : correctionTime(memory!.raw['validFrom']);

  factory MemoryFieldEvidenceDetail.read(
    dynamic value, {
    required String owner,
    required String memoryID,
    required String agentID,
    required int version,
    required DateTime now,
  }) {
    final d = completionMap(
      value,
      {
        'schemaVersion',
        'owner',
        'agentId',
        'target',
        'memory',
        'observedAt',
        'expiresAt',
        'explanation',
        'modelAccess',
      },
      optional: {'fieldEvidenceSet'},
    );
    final o = completionMap(d['owner'], {'type', 'id'});
    final t = completionMap(d['target'], {'id', 'version', 'status'});
    _check(
      candidateIDValid(owner) &&
          candidateIDValid(memoryID) &&
          candidateIDValid(agentID) &&
          completionVersion(version) &&
          d['schemaVersion'] == 'agent-memory-detail-v1' &&
          o['type'] == 'PERSON' &&
          o['id'] == owner &&
          d['agentId'] == agentID &&
          t['id'] == memoryID &&
          t['version'] == version &&
          d['modelAccess'] == false &&
          d['explanation'] == memoryDetailExplanation,
    );
    final observed = correctionTime(d['observedAt']);
    final expires = correctionTime(d['expiresAt']);
    _check(
      !now.isBefore(observed) &&
          expires.isAfter(now) &&
          expires.difference(observed) <= const Duration(minutes: 2),
    );
    CorrectableMemory? memory;
    if ({'ACTIVE', 'PENDING_REVIEW'}.contains(t['status'])) {
      memory = CorrectableMemory.read(d['memory'], owner);
      _check(
        memory.id == memoryID &&
            memory.agentID == agentID &&
            memory.version == version &&
            memory.status == t['status'] &&
            !correctionTime(memory.raw['validFrom']).isAfter(observed) &&
            !correctionTime(memory.raw['createdAt']).isAfter(observed) &&
            !correctionTime(memory.raw['updatedAt']).isAfter(observed) &&
            memory.validUntil.isAfter(now) &&
            !expires.isAfter(memory.validUntil),
      );
    } else {
      _check(
        {'EXPIRED', 'DELETED'}.contains(t['status']) && d['memory'] == null,
      );
    }
    final available = d.containsKey('fieldEvidenceSet');
    if (available) {
      final s = completionMap(d['fieldEvidenceSet'], {
        'schemaVersion',
        'owner',
        'observedAt',
        'expiresAt',
        'scope',
        'claims',
        'conflicts',
        'modelAccess',
        'mediaAccess',
        'grantsAuthority',
      });
      final so = completionMap(s['owner'], {'type', 'id'});
      _check(
        s['schemaVersion'] == 'agent-field-evidence-v1' &&
            so['type'] == 'PERSON' &&
            so['id'] == owner &&
            _sameTime(s['observedAt'], observed) &&
            _sameTime(s['expiresAt'], expires) &&
            s['scope'] == 'HUMAN_MEMORY_DETAIL' &&
            s['grantsAuthority'] == false &&
            s['modelAccess'] == 'UNAVAILABLE' &&
            s['mediaAccess'] == 'UNAVAILABLE' &&
            s['claims'] is List &&
            (s['claims'] as List).length == (memory == null ? 0 : 1) &&
            s['conflicts'] is List &&
            (s['conflicts'] as List).isEmpty,
      );
      if (memory != null) {
        final c = completionMap(
          (s['claims'] as List).single,
          {
            'id',
            'itemKind',
            'itemId',
            'subjectKind',
            'subjectId',
            'field',
            'claimantKind',
            'source',
            'nature',
            'use',
            'observedAt',
            'collectedAt',
            'collectionEvent',
            'sourceUpdatedAt',
            'sourceCreatedAt',
            'validFrom',
            'validUntil',
            'captureTimeStatus',
            'validityStatus',
            'confidence',
          },
          optional: {'claimantId', 'declaredAt'},
        );
        final source = completionMap(c['source'], {
          'kind',
          'id',
          'version',
          'nativeTime',
        });
        final rev = completionMap(source['version'], {'kind', 'revision'});
        final score = completionMap(c['confidence'], {'semantics', 'value'});
        final inferred = memory.sourceType == 'INFERRED';
        final updated = correctionTime(memory.raw['updatedAt']);
        final use = inferred
            ? 'CANDIDATE_ONLY'
            : memory.raw['memoryType'] == 'IDENTITY'
            ? 'NO_IDENTITY_AUTHORITY'
            : {
                'PLACE',
                'CITY',
                'HISTORY',
                'EXPERIENCE',
              }.contains(memory.raw['memoryType'])
            ? 'NO_VISIT_OR_CURRENT_LOCATION_AUTHORITY'
            : 'DECLARATION_ONLY';
        _check(
          c['id'] == 'HUMAN_MEMORY_DETAIL:$memoryID:memory.summary' &&
              c['itemKind'] == 'memories' &&
              c['itemId'] == memoryID &&
              c['subjectKind'] == 'PERSON' &&
              c['subjectId'] == owner &&
              c['field'] == 'memory.summary' &&
              c['claimantKind'] ==
                  (inferred ? 'UNVERIFIED_INFERENCE' : 'PERSON_DECLARATION') &&
              c['nature'] ==
                  (inferred ? 'MODEL_INFERENCE' : 'USER_DECLARATION') &&
              c['use'] == use &&
              (inferred
                  ? !c.containsKey('claimantId') && !c.containsKey('declaredAt')
                  : c['claimantId'] == owner &&
                        _sameTime(c['declaredAt'], updated)) &&
              source['kind'] == 'HUMAN_MEMORY_DETAIL' &&
              source['id'] == memoryID &&
              rev['kind'] == 'REVISION' &&
              rev['revision'] == version &&
              _sameTime(source['nativeTime'], updated) &&
              _sameTime(c['sourceUpdatedAt'], updated) &&
              _sameTime(
                c['sourceCreatedAt'],
                correctionTime(memory.raw['createdAt']),
              ) &&
              _sameTime(
                c['validFrom'],
                correctionTime(memory.raw['validFrom']),
              ) &&
              _sameTime(c['validUntil'], memory.validUntil) &&
              _sameTime(c['observedAt'], observed) &&
              _sameTime(c['collectedAt'], observed) &&
              c['collectionEvent'] == 'CURRENT_NATIVE_READ' &&
              c['captureTimeStatus'] == 'UNKNOWN_NOT_COLLECTED' &&
              c['validityStatus'] == 'SOURCE_INTERVAL_KNOWN' &&
              score['semantics'] ==
                  (inferred ? 'UNCALIBRATED_SCORE' : 'DIRECT_DECLARATION') &&
              score['value'] == memory.raw['confidence'],
        );
      }
    }
    return MemoryFieldEvidenceDetail._(
      id: memoryID,
      version: version,
      status: t['status'],
      memory: memory,
      observedAt: observed,
      expiresAt: expires,
      evidenceAvailable: available,
    );
  }
}
