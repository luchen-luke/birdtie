import 'dart:convert';
import 'agent_memory_candidate_api.dart';
import 'agent_multi_candidate_pending_store.dart';

const analysisPurposePath = '/v1/me/agent-enrichment-purpose';
const multiCandidatePath = '/v1/me/agent-multi-candidates';
const _analysisPurpose = 'MOMENT_LOCAL_ANALYSIS';
const _multiPurpose = 'STAGE_MEMORY_CANDIDATE_MULTI';
const _maxInteger = 9007199254740991;
final _digest = RegExp(r'^[0-9a-f]{64}$');

void _require(bool valid) {
  if (!valid) throw const FormatException('来源、版本或授权结果不匹配，请重新检查。');
}

Map<String, dynamic> _object(dynamic value) {
  _require(value is Map && value.keys.every((key) => key is String));
  return Map<String, dynamic>.from(value as Map);
}

bool _integer(dynamic value) =>
    value is int && value > 0 && value <= _maxInteger;
String _canonical(dynamic value) {
  if (value is Map) {
    final keys = value.keys.cast<String>().toList()..sort();
    return '{${keys.map((k) => '${jsonEncode(k)}:${_canonical(value[k])}').join(',')}}';
  }
  if (value is List) return '[${value.map(_canonical).join(',')}]';
  return jsonEncode(value);
}

bool _same(dynamic a, dynamic b) => _canonical(a) == _canonical(b);
bool _sameSelection(dynamic a, dynamic b, bool multi) {
  final x = _object(a), y = _object(b);
  final timeKey = multi ? 'retainUntil' : 'deadlineAt';
  x[timeKey] = candidateTime(x[timeKey]).toIso8601String();
  y[timeKey] = candidateTime(y[timeKey]).toIso8601String();
  return _same(x, y);
}

Map<String, dynamic> _frozen(Map<String, dynamic> value) =>
    candidateFreezeJson(value) as Map<String, dynamic>;

class AnalysisMomentChoice {
  AnalysisMomentChoice(this.raw);
  final Map<String, dynamic> raw;
  String get id => raw['id'] as String;
  String get title => raw['title'] as String;
  String get body => raw['body'] as String;
  int get revision => raw['revision'] as int;
  DateTime get updatedAt => candidateTime(raw['updatedAt']);
}

class AnalysisTaskChoice {
  AnalysisTaskChoice(this.raw);
  final Map<String, dynamic> raw;
  String get id => raw['id'] as String;
  String get query => raw['query'] as String;
  DateTime get updatedAt => candidateTime(raw['updatedAt']);
}

class CandidatePurposePreview {
  CandidatePurposePreview._(this.raw, this.multi);
  final Map<String, dynamic> raw;
  final bool multi;
  String get id => raw['id'] as String;
  String get state => raw['state'] as String;
  String get agentID => raw['agentId'] as String;
  String? get consumedGrantID => raw['consumedGrantId'] as String?;
  Map<String, dynamic> get selection =>
      raw['selection'] as Map<String, dynamic>;
  Map<String, dynamic>? get review => raw['review'] as Map<String, dynamic>?;
  DateTime get expiresAt => candidateTime(raw['expiresAt']);
  bool get current =>
      state == 'CURRENT_REVIEW' && expiresAt.isAfter(DateTime.now().toUtc());

  static CandidatePurposePreview read(
    dynamic value,
    String owner, {
    required bool multi,
    required Map<String, dynamic> selection,
    String? expectedID,
  }) {
    final m = _object(value);
    _purposeIdentity(m, owner, multi);
    _require(expectedID == null || m['id'] == expectedID);
    _validateSelection(m['selection'], multi);
    _require(_sameSelection(m['selection'], selection, multi));
    _require(
      m['explanation'] is String && (m['explanation'] as String).isNotEmpty,
    );
    final observed = candidateTime(m['observedAt']);
    final expires = candidateTime(m['expiresAt']);
    final deadline = candidateTime(
      selection[multi ? 'retainUntil' : 'deadlineAt'],
    );
    _require(!expires.isAfter(deadline));
    if (m['state'] == 'RECEIPT_ONLY') {
      _require(candidateIDValid(m['consumedGrantId']));
      if (multi) {
        _require(m['review'] == null);
      } else {
        final r = _object(m['review']);
        _require(
          (r['content'] as Map).isEmpty &&
              r['taskQuery'] == '' &&
              candidateTime(r['taskUpdatedAt']).year == 1,
        );
      }
    } else {
      _require(
        m['state'] == 'CURRENT_REVIEW' &&
            !m.containsKey('consumedGrantId') &&
            expires.isAfter(observed) &&
            expires.difference(observed) <= Duration(seconds: multi ? 90 : 300),
      );
      final r = _object(m['review']);
      if (multi) {
        _validateMultiReview(r, selection, observed, expires);
      } else {
        final content = _object(r['content']);
        final fields = (selection['fields'] as List).cast<String>();
        _require(
          r['taskQuery'] is String && (r['taskQuery'] as String).isNotEmpty,
        );
        _require(!candidateTime(r['taskUpdatedAt']).isAfter(observed));
        _require(
          content.length == fields.length &&
              fields.every((f) => content[f] is String),
        );
      }
    }
    return CandidatePurposePreview._(_frozen(m), multi);
  }
}

class CandidatePurposeGrant {
  CandidatePurposeGrant._(this.raw, this.multi);
  final Map<String, dynamic> raw;
  final bool multi;
  String get id => raw['id'] as String;
  String get previewID => raw['previewId'] as String;
  String get agentID => raw['agentId'] as String;
  int get revision => raw['revision'] as int;
  Map<String, dynamic> get selection =>
      raw['selection'] as Map<String, dynamic>;
  DateTime get expiresAt => candidateTime(raw['expiresAt']);
  bool get revoked => raw['revokedAt'] != null;
  bool get usable => !revoked && expiresAt.isAfter(DateTime.now().toUtc());
  static CandidatePurposeGrant read(
    dynamic value,
    String owner, {
    required bool multi,
    required Map<String, dynamic> selection,
    required String previewID,
    String? expectedID,
    String? agentID,
  }) {
    final m = _object(value);
    _purposeIdentity(m, owner, multi);
    _require(
      m['previewId'] == previewID &&
          candidateIDValid(previewID) &&
          (expectedID == null || m['id'] == expectedID) &&
          (agentID == null || m['agentId'] == agentID) &&
          _integer(m['revision']),
    );
    _validateSelection(m['selection'], multi);
    _require(_sameSelection(selection, m['selection'], multi));
    final created = candidateTime(m['createdAt']);
    final observed = candidateTime(m['observedAt']);
    final expires = candidateTime(m['expiresAt']);
    _require(
      !observed.isBefore(created) &&
          expires.isAfter(created) &&
          !expires.isAfter(
            candidateTime(selection[multi ? 'retainUntil' : 'deadlineAt']),
          ) &&
          (!multi ||
              expires.difference(created) <= const Duration(seconds: 90)),
    );
    if (m['revokedAt'] != null) {
      final revoked = candidateTime(m['revokedAt']);
      _require(!revoked.isBefore(created) && !revoked.isAfter(observed));
    }
    return CandidatePurposeGrant._(_frozen(m), multi);
  }
}

void _purposeIdentity(Map<String, dynamic> m, String owner, bool multi) {
  final principal = _object(m['owner']);
  _require(
    m['schemaVersion'] ==
            (multi
                ? 'agent-multi-candidate-v1'
                : 'agent-enrichment-purpose-v1') &&
        m['purpose'] == (multi ? _multiPurpose : _analysisPurpose) &&
        const ['PERSON', 'person'].contains(principal['type']) &&
        principal['id'] == owner &&
        candidateIDValid(owner) &&
        candidateIDValid(m['id']) &&
        candidateIDValid(m['agentId']) &&
        m['modelAccess'] == false &&
        m['explanation'] != '',
  );
  if (m.containsKey('explanation')) _require(m['explanation'] is String);
  if (multi) {
    _require(
      m['memoryPromotionAllowed'] == false &&
          m['candidateWriteAvailable'] == false,
    );
  } else {
    _require(m['candidateRetentionAllowed'] == false);
  }
}

void _validateSelection(dynamic value, bool multi) {
  final s = _object(value);
  if (multi) {
    _require(
      s.length == 2 &&
          s.keys.toSet().containsAll(['analysisGrantIds', 'retainUntil']),
    );
    final ids = (s['analysisGrantIds'] as List).cast<String>();
    _require(
      ids.length >= 2 &&
          ids.length <= 5 &&
          ids.toSet().length == ids.length &&
          ids.every(candidateIDValid),
    );
    _require(_same(ids, [...ids]..sort()));
    candidateTime(s['retainUntil']);
  } else {
    _require(
      s.length == 5 &&
          s.keys.toSet().containsAll([
            'taskId',
            'momentId',
            'momentRevision',
            'fields',
            'deadlineAt',
          ]) &&
          candidateIDValid(s['taskId']) &&
          candidateIDValid(s['momentId']) &&
          _integer(s['momentRevision']),
    );
    final fields = (s['fields'] as List).cast<String>();
    _require(
      fields.isNotEmpty &&
          fields.length <= 2 &&
          fields.toSet().length == fields.length &&
          fields.every((f) => f == 'title' || f == 'body') &&
          _same(fields, [...fields]..sort()),
    );
    candidateTime(s['deadlineAt']);
  }
}

void _validateMultiReview(
  Map<String, dynamic> r,
  Map<String, dynamic> selection,
  DateTime observed,
  DateTime expires,
) {
  _require(
    candidateIDValid(r['anchorEventId']) &&
        candidateIDValid(r['logicalOperationId']) &&
        candidateIDValid(r['taskId']) &&
        candidateTime(r['retainUntil']) == expires,
  );
  final sources = (r['sources'] as List).map(_object).toList();
  final members = (r['sourceSelections'] as List).map(_object).toList();
  final ids = (selection['analysisGrantIds'] as List).cast<String>();
  _require(
    sources.length == ids.length &&
        members.length == sources.length &&
        r['clusters'] is int &&
        r['clusters'] >= 2 &&
        r['clusters'] <= sources.length &&
        _same(r['anchorSource'], sources.first),
  );
  final momentIDs = <String>[], grantIDs = <String>[];
  for (var i = 0; i < sources.length; i++) {
    final source = sources[i], member = members[i];
    final selector = _object(source['selector']),
        version = _object(source['version']);
    _require(
      selector['type'] == 'MOMENT' &&
          candidateIDValid(selector['id']) &&
          version['kind'] == 'REVISION' &&
          _integer(version['revision']) &&
          !version.containsKey('token') &&
          _digest.hasMatch(source['fingerprint'] as String) &&
          !candidateTime(source['eventTime']).isAfter(observed) &&
          _same(source['anchors'], ['MOMENT:${selector['id']}']) &&
          _same(member['source'], source) &&
          candidateIDValid(member['analysisGrantId']) &&
          candidateIDValid(member['analysisPreviewId']),
    );
    final fields = (member['selectedFields'] as List).cast<String>();
    _require(
      fields.isNotEmpty &&
          fields.length <= 2 &&
          fields.toSet().length == fields.length &&
          fields.every((f) => f == 'title' || f == 'body'),
    );
    momentIDs.add(selector['id'] as String);
    grantIDs.add(member['analysisGrantId'] as String);
  }
  _require(
    momentIDs.toSet().length == momentIDs.length &&
        _same(momentIDs, [...momentIDs]..sort()) &&
        grantIDs.toSet().length == grantIDs.length &&
        _same([...grantIDs]..sort(), ids),
  );
  final proposal = _object(r['proposal']),
      assessment = _object(proposal['assessment']);
  _require(
    (proposal['algorithmVersion'] == 'moment-lexical-category-v1' ||
            proposal['algorithmVersion'] == 'moment-lexical-category-v2') &&
        (proposal['category'] != 'hiking' ||
            proposal['algorithmVersion'] == 'moment-lexical-category-v2') &&
        proposal['predicate'] == 'ACTIVITY_CATEGORY' &&
        candidateCategories.containsKey(proposal['category']) &&
        proposal['explanation'] is String &&
        (proposal['explanation'] as String).isNotEmpty &&
        assessment['semantics'] == 'ORDINAL' &&
        assessment['level'] == 'LOW' &&
        !assessment.containsKey('value'),
  );
}

class MultiCandidateReceipt {
  MultiCandidateReceipt._(this.raw, this.candidate);
  final Map<String, dynamic> raw;
  final HumanMemoryCandidate? candidate;
  String get state => raw['state'] as String;
  String? get candidateID => raw['candidateId'] as String?;
  bool get staged => state == 'CANDIDATE_STAGED';
  void requireApprovedReview(Map<String, dynamic> review) {
    if (!staged) return;
    _require(
      raw['eventId'] == review['anchorEventId'] &&
          raw['logicalOperationId'] == review['logicalOperationId'],
    );
    final c = candidate;
    if (c != null) {
      _require(
        c.category == review['proposal']['category'] &&
            _same(c.raw['sources'], review['sources']) &&
            !c.validUntil.isAfter(candidateTime(review['retainUntil'])),
      );
    }
  }

  static MultiCandidateReceipt read(
    dynamic value,
    String owner,
    CandidatePurposeGrant grant,
  ) {
    final m = _object(value), principal = _object(_object(value)['owner']);
    _require(
      m['schemaVersion'] == 'agent-multi-candidate-v1' &&
          m['retentionGrantId'] == grant.id &&
          const ['PERSON', 'person'].contains(principal['type']) &&
          principal['id'] == owner &&
          m['agentId'] == grant.agentID &&
          m['modelAccess'] == false &&
          m['memoryPromotionAllowed'] == false &&
          m['explanation'] is String &&
          (m['explanation'] as String).isNotEmpty,
    );
    candidateTime(m['observedAt']);
    HumanMemoryCandidate? c;
    if (m['state'] == 'NOT_STAGED') {
      _require(
        m['committed'] == false &&
            [
              'eventId',
              'logicalOperationId',
              'effectKey',
              'handlerVersion',
              'candidateId',
              'candidate',
            ].every((k) => !m.containsKey(k)),
      );
    } else {
      _require(
        m['state'] == 'CANDIDATE_STAGED' &&
            m['committed'] == true &&
            candidateIDValid(m['candidateId']) &&
            candidateIDValid(m['eventId']) &&
            candidateIDValid(m['logicalOperationId']) &&
            m['handlerVersion'] == 'mom-candidate-multi-v1' &&
            _digest.hasMatch(m['effectKey'] as String),
      );
      if (m['candidate'] != null) {
        c = HumanMemoryCandidate.read(m['candidate'], owner);
        _require(
          c.id == m['candidateId'] &&
              c.raw['agentId'] == grant.agentID &&
              c.status == 'CANDIDATE' &&
              c.raw['assessment']['semantics'] == 'ORDINAL' &&
              c.raw['assessment']['level'] == 'LOW' &&
              !c.raw['assessment'].containsKey('value') &&
              c.raw['sources'].length >= 2 &&
              c.raw['sources'].length <= 5 &&
              c.validUntil.isAfter(candidateTime(m['observedAt'])) &&
              !c.raw.containsKey('memoryId'),
        );
        final observed = candidateTime(m['observedAt']);
        final created = candidateTime(c.raw['createdAt']);
        final updated = candidateTime(c.raw['updatedAt']);
        _require(
          !created.isAfter(observed) &&
              !updated.isBefore(created) &&
              !updated.isAfter(observed),
        );
        for (final source in c.raw['sources'] as List) {
          final id = source['selector']['id'];
          _require(
            source['selector']['type'] == 'MOMENT' &&
                source['version']['kind'] == 'REVISION' &&
                _same(source['anchors'], ['MOMENT:$id']) &&
                !candidateTime(source['eventTime']).isAfter(created),
          );
        }
      }
    }
    return MultiCandidateReceipt._(_frozen(m), c);
  }
}

// Canonical association only. The server does not echo a decision receipt digest.
String multiSelectionDigest(Map<String, dynamic> selection, bool multi) {
  final m = Map<String, dynamic>.from(selection);
  final key = multi ? 'retainUntil' : 'deadlineAt';
  m[key] = candidateTime(m[key]).toIso8601String();
  return candidateRecoveryFingerprint(_canonical(m));
}

String multiReviewDigest(Map<String, dynamic>? review) =>
    candidateRecoveryFingerprint(_canonical(review));

class MultiCandidateRecoveryStatus {
  const MultiCandidateRecoveryStatus(this.state, this.resolved);
  final String state;
  final bool resolved;
}

class AgentMultiCandidateAPI {
  AgentMultiCandidateAPI(this.transport);
  final AgentMemoryCandidateAPI transport;
  String get recoveryEnvironment => transport.recoveryEnvironment;
  Future<MultiCandidateRecoveryStatus> reconcile(
    String auth,
    String owner,
    PendingMultiCandidateOperation entry,
  ) async {
    final d = entry.data, multi = d['multi'] as bool;
    _require(d['ownerId'] == owner);
    final base = multi ? multiCandidatePath : analysisPurposePath;
    final phase = d['phase'];
    final path = phase == 'approval'
        ? '$base/previews/${d['previewId']}/receipt'
        : phase == 'stage'
        ? '$multiCandidatePath/grants/${d['grantId']}/receipt'
        : '$base/grants/${d['grantId']}';
    final m = _object(await transport.request('GET', path, auth));
    if (phase == 'approval') {
      const required = {
        'schemaVersion',
        'previewId',
        'purpose',
        'owner',
        'agentId',
        'state',
        'previewObservedAt',
        'previewExpiresAt',
        'observedAt',
        'validUntil',
        'modelAccess',
        'candidateWriteAvailable',
        'memoryPromotionAllowed',
      };
      final recorded = m['state'] == 'APPROVAL_RECORDED';
      _require(
        m.keys.toSet().length == required.length + (recorded ? 1 : 0) &&
            required.every(m.containsKey) &&
            m.keys.every(
              (k) => required.contains(k) || recorded && k == 'consumedGrantId',
            ),
      );
      final principal = _object(m['owner']);
      _require(
        principal.length == 2 &&
            const ['PERSON', 'person'].contains(principal['type']) &&
            principal['id'] == owner &&
            m['schemaVersion'] ==
                (multi
                    ? 'agent-multi-candidate-preview-receipt-v1'
                    : 'agent-enrichment-purpose-preview-receipt-v1') &&
            m['purpose'] == (multi ? _multiPurpose : _analysisPurpose) &&
            m['previewId'] == d['previewId'] &&
            m['agentId'] == d['agentId'] &&
            m['modelAccess'] == false &&
            m['candidateWriteAvailable'] == false &&
            m['memoryPromotionAllowed'] == false,
      );
      final initial = candidateTime(m['previewObservedAt']),
          end = candidateTime(m['previewExpiresAt']),
          at = candidateTime(m['observedAt']),
          until = candidateTime(m['validUntil']);
      _require(
        end == entry.expiresAt &&
            end.isAfter(initial) &&
            end.difference(initial) <= const Duration(minutes: 5) &&
            !at.isBefore(initial) &&
            until.isAfter(at) &&
            until.difference(at) <= const Duration(seconds: 30),
      );
      if (recorded) {
        _require(candidateIDValid(m['consumedGrantId']));
        return const MultiCandidateRecoveryStatus('APPROVAL_RECORDED', true);
      }
      _require(
        m['state'] ==
            (at.isBefore(end) ? 'OPEN_UNCONSUMED' : 'CLOSED_UNCONSUMED'),
      );
      return MultiCandidateRecoveryStatus(
        m['state'] as String,
        !at.isBefore(end),
      );
    }
    if (phase == 'stage') {
      final principal = _object(m['owner']);
      _require(
        m['schemaVersion'] == 'agent-multi-candidate-v1' &&
            const ['PERSON', 'person'].contains(principal['type']) &&
            principal['id'] == owner &&
            m['agentId'] == d['agentId'] &&
            m['retentionGrantId'] == d['grantId'] &&
            m['modelAccess'] == false &&
            m['memoryPromotionAllowed'] == false &&
            m['explanation'] is String,
      );
      final observed = candidateTime(m['observedAt']);
      if (m['state'] == 'CANDIDATE_STAGED') {
        _require(
          m['committed'] == true &&
              candidateIDValid(m['candidateId']) &&
              m['eventId'] == d['anchorEventId'] &&
              m['logicalOperationId'] == d['logicalOperationId'] &&
              m['handlerVersion'] == 'mom-candidate-multi-v1' &&
              m['effectKey'] is String &&
              _digest.hasMatch(m['effectKey']),
        );
        return const MultiCandidateRecoveryStatus('CANDIDATE_STAGED', true);
      }
      _require(
        m['state'] == 'NOT_STAGED' &&
            m['committed'] == false &&
            [
              'eventId',
              'logicalOperationId',
              'effectKey',
              'handlerVersion',
              'candidateId',
              'candidate',
            ].every((k) => !m.containsKey(k)),
      );
      return MultiCandidateRecoveryStatus(
        'NOT_STAGED',
        !observed.isBefore(entry.expiresAt),
      );
    }
    _purposeIdentity(m, owner, multi);
    _require(m['agentId'] == d['agentId']);
    _validateSelection(m['selection'], multi);
    _require(
      multiSelectionDigest(_object(m['selection']), multi) ==
              d['selectionDigest'] &&
          !candidateTime(m['expiresAt']).isAfter(entry.expiresAt),
    );
    final observed = candidateTime(m['observedAt']);
    _require(
      m['id'] == d['grantId'] &&
          m['previewId'] == d['previewId'] &&
          _integer(m['revision']),
    );
    final created = candidateTime(m['createdAt']);
    _require(
      !observed.isBefore(created) &&
          candidateTime(m['expiresAt']).isAfter(created),
    );
    if (m['revokedAt'] != null) {
      final revoked = candidateTime(m['revokedAt']);
      _require(
        !revoked.isBefore(created) &&
            !revoked.isAfter(observed) &&
            m['revision'] == d['grantRevision'] + 1,
      );
      return const MultiCandidateRecoveryStatus('REVOKED', true);
    }
    _require(m['revision'] == d['grantRevision']);
    return MultiCandidateRecoveryStatus(
      'NOT_REVOKED',
      !observed.isBefore(entry.expiresAt),
    );
  }

  void dispose() => transport.dispose();
  Future<(List<AnalysisTaskChoice>, List<AnalysisMomentChoice>)> choices(
    String auth,
    String owner,
  ) async {
    final results = await Future.wait([
      transport.request('GET', '/v1/me/agent-tasks', auth),
      transport.request('GET', '/v1/me/moments', auth),
    ]);
    final tasks = <AnalysisTaskChoice>[], moments = <AnalysisMomentChoice>[];
    for (final item in (results[0] as List).take(50)) {
      final m = _object(item);
      if (m['principalType'] != 'person') continue;
      _require(
        m['principalId'] == owner &&
            m['actingUserId'] == owner &&
            candidateIDValid(m['id']),
      );
      if (m['status'] != 'ACTIVE' || m['contextType'] != 'CITY') continue;
      final filters = m['filters'] == null
          ? const <String, dynamic>{}
          : _object(m['filters']);
      final currentQuery = filters['currentQuery'];
      _require(currentQuery == null || currentQuery is String);
      // Match the original native analysis resolver's currentQuery fallback.
      // Keep the exact current text; do not silently replace malformed input.
      final query = currentQuery == null || currentQuery == ''
          ? m['query']
          : currentQuery;
      _require(
        query is String &&
            query.trim().isNotEmpty &&
            utf8.encode(query).length <= 240 &&
            candidateIDValid(m['contextId']),
      );
      candidateTime(m['updatedAt']);
      tasks.add(
        AnalysisTaskChoice(
          _frozen({'id': m['id'], 'query': query, 'updatedAt': m['updatedAt']}),
        ),
      );
    }
    final now = DateTime.now().toUtc();
    for (final item in (results[1] as List).take(50)) {
      final m = _object(item);
      _require(m['authorAccountId'] == owner && candidateIDValid(m['id']));
      if (m['status'] != 'draft' ||
          m['visibility'] != 'private' ||
          (m['activityIds'] as List).isNotEmpty ||
          (m['communityId'] ?? '') != '' ||
          (m['organizationId'] ?? '') != '') {
        continue;
      }
      _require(
        _integer(m['revision']) && m['title'] is String && m['body'] is String,
      );
      final updated = candidateTime(m['updatedAt']);
      if (updated.isAfter(now) ||
          !updated.add(const Duration(minutes: 15)).isAfter(now)) {
        continue;
      }
      moments.add(
        AnalysisMomentChoice(
          _frozen({
            'id': m['id'],
            'title': m['title'],
            'body': m['body'],
            'revision': m['revision'],
            'updatedAt': m['updatedAt'],
          }),
        ),
      );
    }
    _require(
      tasks.map((t) => t.id).toSet().length == tasks.length &&
          moments.map((m) => m.id).toSet().length == moments.length,
    );
    return (
      List<AnalysisTaskChoice>.unmodifiable(tasks),
      List<AnalysisMomentChoice>.unmodifiable(moments),
    );
  }

  Future<CandidatePurposePreview> preview(
    String auth,
    String owner,
    Map<String, dynamic> selection, {
    required bool multi,
  }) async {
    _validateSelection(selection, multi);
    return CandidatePurposePreview.read(
      await transport.request(
        'POST',
        '${multi ? multiCandidatePath : analysisPurposePath}/previews',
        auth,
        selection,
      ),
      owner,
      multi: multi,
      selection: selection,
    );
  }

  Future<CandidatePurposePreview> readPreview(
    String auth,
    String owner,
    CandidatePurposePreview p,
  ) async {
    final result = CandidatePurposePreview.read(
      await transport.request(
        'GET',
        '${p.multi ? multiCandidatePath : analysisPurposePath}/previews/${p.id}',
        auth,
      ),
      owner,
      multi: p.multi,
      selection: p.selection,
      expectedID: p.id,
    );
    _require(
      result.agentID == p.agentID &&
          (result.state != 'CURRENT_REVIEW' || _same(result.review, p.review)),
    );
    return result;
  }

  Future<CandidatePurposeGrant> approve(
    String auth,
    String owner,
    CandidatePurposePreview p,
  ) async {
    final result = CandidatePurposeGrant.read(
      await transport.request(
        'POST',
        '${p.multi ? multiCandidatePath : analysisPurposePath}/previews/${p.id}/approve',
        auth,
      ),
      owner,
      multi: p.multi,
      selection: p.selection,
      previewID: p.id,
      agentID: p.agentID,
    );
    _require(!result.expiresAt.isAfter(p.expiresAt));
    return result;
  }

  Future<CandidatePurposeGrant> readGrant(
    String auth,
    String owner,
    CandidatePurposeGrant g,
  ) async => CandidatePurposeGrant.read(
    await transport.request(
      'GET',
      '${g.multi ? multiCandidatePath : analysisPurposePath}/grants/${g.id}',
      auth,
    ),
    owner,
    multi: g.multi,
    selection: g.selection,
    previewID: g.previewID,
    agentID: g.agentID,
    expectedID: g.id,
  );
  Future<CandidatePurposeGrant> grantAfterApproval(
    String auth,
    String owner,
    CandidatePurposePreview p,
    String id,
  ) async {
    final result = CandidatePurposeGrant.read(
      await transport.request(
        'GET',
        '${p.multi ? multiCandidatePath : analysisPurposePath}/grants/$id',
        auth,
      ),
      owner,
      multi: p.multi,
      selection: p.selection,
      previewID: p.id,
      expectedID: id,
      agentID: p.agentID,
    );
    _require(!result.expiresAt.isAfter(p.expiresAt));
    return result;
  }

  Future<CandidatePurposeGrant> revoke(
    String auth,
    String owner,
    CandidatePurposeGrant g,
  ) async {
    final result = CandidatePurposeGrant.read(
      await transport.request(
        'DELETE',
        '${g.multi ? multiCandidatePath : analysisPurposePath}/grants/${g.id}',
        auth,
        {'expectedRevision': g.revision},
      ),
      owner,
      multi: g.multi,
      selection: g.selection,
      previewID: g.previewID,
      expectedID: g.id,
      agentID: g.agentID,
    );
    _require(result.revoked && result.revision == g.revision + 1);
    return result;
  }

  Future<MultiCandidateReceipt> stage(
    String auth,
    String owner,
    CandidatePurposeGrant g,
  ) async {
    _require(g.multi && g.usable);
    final result = MultiCandidateReceipt.read(
      await transport.request('POST', '$multiCandidatePath/stage', auth, {
        'retentionGrantId': g.id,
      }),
      owner,
      g,
    );
    _require(result.staged);
    return result;
  }

  Future<MultiCandidateReceipt> receipt(
    String auth,
    String owner,
    CandidatePurposeGrant g,
  ) async => MultiCandidateReceipt.read(
    await transport.request(
      'GET',
      '$multiCandidatePath/grants/${g.id}/receipt',
      auth,
    ),
    owner,
    g,
  );
}
