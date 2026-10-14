import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'agent_memory_candidate_api.dart'
    show candidateIDValid, candidateTime, candidateFreezeJson;

const agentRunStates = {
  'QUEUED': '等待处理',
  'RUNNING': '正在处理',
  'WAITING_CONFIRMATION': '等待明确许可',
  'RETRY_WAIT': '等待后续处理',
  'SUCCEEDED': '候选处理已完成',
  'FAILED': '处理失败',
  'CANCELLED': '已取消',
  'EXPIRED': '已到期',
};
bool agentRunVersion(dynamic v) => v is int && v > 0 && v <= 9007199254740991;
bool agentRunCursor(dynamic v) =>
    v is String &&
    v.isNotEmpty &&
    v.length <= 1024 &&
    RegExp(r'^[A-Za-z0-9_-]+$').hasMatch(v);
void _shape(
  Map<String, dynamic> m,
  Set<String> required, [
  Set<String> optional = const {},
]) {
  if (!m.keys.toSet().containsAll(required) ||
      m.keys.any((k) => !required.contains(k) && !optional.contains(k)))
    throw const FormatException('处理状态格式不符');
}

Map<String, dynamic> _map(dynamic v) => Map<String, dynamic>.from(v as Map);

class AgentRunRecord {
  AgentRunRecord._(this.raw);
  final Map<String, dynamic> raw;
  String get id => raw['id'];
  String get agentID => raw['agentId'];
  String get state => raw['state'];
  int get version => raw['version'];
  DateTime get observedAt => candidateTime(raw['observedAt']);
  DateTime get createdAt => candidateTime(raw['createdAt']);
  bool get terminal =>
      const ['SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED'].contains(state);
  factory AgentRunRecord.read(
    dynamic value,
    String owner, {
    String? id,
    String? agent,
  }) {
    final m = _map(value);
    _shape(
      m,
      {
        'schemaVersion',
        'id',
        'owner',
        'agentId',
        'source',
        'eventId',
        'logicalOperationId',
        'state',
        'checkpoint',
        'version',
        'attempt',
        'reason',
        'createdAt',
        'updatedAt',
        'deadline',
        'observedAt',
        'committed',
        'modelAccess',
        'memoryPromotionAllowed',
      },
      {
        'candidateId',
        'retentionGrantId',
        'generation',
        'recoveryRootId',
        'recoveryReason',
        'recoveryPreviousVersion',
      },
    );
    final o = _map(m['owner']), s = _map(m['source']);
    _shape(o, {'type', 'id'});
    _shape(s, {'type', 'id', 'owner', 'version'});
    final so = _map(s['owner']), sv = _map(s['version']);
    _shape(so, {'type', 'id'});
    _shape(sv, {'kind', 'revision'});
    if (m['schemaVersion'] != 'agent-enrichment-run-v1' ||
        o['type'] != 'PERSON' ||
        o['id'] != owner ||
        so['type'] != 'PERSON' ||
        so['id'] != owner ||
        s['type'] != 'MOMENT' ||
        sv['kind'] != 'REVISION' ||
        !agentRunVersion(sv['revision']) ||
        !agentRunVersion(m['version']) ||
        !agentRunStates.containsKey(m['state']) ||
        m['modelAccess'] != false ||
        m['memoryPromotionAllowed'] != false ||
        m['attempt'] is! int ||
        m['attempt'] < 0 ||
        m['attempt'] > 5 ||
        [
          'id',
          'agentId',
          'eventId',
          'logicalOperationId',
        ].any((k) => !candidateIDValid(m[k])) ||
        !candidateIDValid(s['id']) ||
        (id != null && m['id'] != id) ||
        (agent != null && m['agentId'] != agent))
      throw const FormatException('处理身份或状态不符');
    final reasons = {
      'QUEUED': {'QUEUED'},
      'RUNNING': {'DISPATCHING', 'RECONCILING'},
      'WAITING_CONFIRMATION': {'WAITING_APPROVAL'},
      'RETRY_WAIT': {'RETRY_BUSY', 'RETRY_UNAVAILABLE', 'RECONCILING'},
      'SUCCEEDED': {'COMMITTED'},
      'FAILED': {'AUTHORITY_CHANGED', 'ATTEMPTS_EXHAUSTED', 'INVALID_INPUT'},
      'CANCELLED': {'CANCELLED'},
      'EXPIRED': {'EXPIRED'},
    };
    if (!reasons[m['state']]!.contains(m['reason']) ||
        !const [
          'SCHEDULED',
          'VALIDATED',
          'DISPATCH_PENDING',
          'RECONCILE_EFFECT',
          'EFFECT_CONFIRMED',
        ].contains(m['checkpoint']) ||
        (m['state'] == 'SUCCEEDED' && m['checkpoint'] != 'EFFECT_CONFIRMED') ||
        (m['state'] == 'WAITING_CONFIRMATION' &&
            m['checkpoint'] != 'SCHEDULED') ||
        m['committed'] != (m['state'] == 'SUCCEEDED') ||
        (m['state'] == 'SUCCEEDED'
            ? !candidateIDValid(m['candidateId'])
            : m.containsKey('candidateId')))
      throw const FormatException('处理回执不符');
    if (m.containsKey('retentionGrantId') &&
            !candidateIDValid(m['retentionGrantId']) ||
        const [
              'QUEUED',
              'RUNNING',
              'RETRY_WAIT',
              'SUCCEEDED',
            ].contains(m['state']) &&
            !candidateIDValid(m['retentionGrantId']))
      throw const FormatException('处理引用不符');
    final created = candidateTime(m['createdAt']),
        updated = candidateTime(m['updatedAt']),
        deadline = candidateTime(m['deadline']),
        observed = candidateTime(m['observedAt']);
    if (updated.isBefore(created) ||
        created.isAfter(observed) ||
        updated.isAfter(observed) ||
        !deadline.isAfter(created) ||
        deadline.difference(created) > const Duration(minutes: 15))
      throw const FormatException('处理时间不符');
    final gen = m['generation'] ?? 0;
    if (gen is! int ||
        gen < 0 ||
        gen > 1 ||
        (gen == 0 &&
            [
              'recoveryRootId',
              'recoveryReason',
              'recoveryPreviousVersion',
            ].any(m.containsKey)) ||
        (gen == 1 &&
            (!candidateIDValid(m['recoveryRootId']) ||
                m['recoveryRootId'] == m['id'] ||
                m['recoveryReason'] != 'RETRY_TRANSIENT_NO_EFFECT' ||
                !agentRunVersion(m['recoveryPreviousVersion']) ||
                m['attempt'] > 1)))
      throw const FormatException('恢复引用不符');
    return AgentRunRecord._(candidateFreezeJson(m) as Map<String, dynamic>);
  }
}

class AgentRunPageData {
  AgentRunPageData._(this.agentID, this.items, this.nextCursor, this.expiresAt);
  final String agentID;
  final List<AgentRunRecord> items;
  final String? nextCursor;
  final DateTime expiresAt;
  factory AgentRunPageData.read(dynamic value, String owner) {
    final m = _map(value);
    _shape(m, {
      'schema',
      'owner',
      'agentId',
      'items',
      'nextCursor',
      'observedAt',
      'expiresAt',
      'modelAccess',
    });
    final o = _map(m['owner']);
    _shape(o, {'type', 'id'});
    if (m['schema'] != 'agent-run-page-v1' ||
        o['type'] != 'PERSON' ||
        o['id'] != owner ||
        !candidateIDValid(m['agentId']) ||
        m['modelAccess'] != false ||
        m['items'] is! List ||
        (m['items'] as List).length > 20 ||
        (m['nextCursor'] != null && !agentRunCursor(m['nextCursor'])))
      throw const FormatException('处理列表不符');
    final observed = candidateTime(m['observedAt']),
        expires = candidateTime(m['expiresAt']),
        now = DateTime.now().toUtc();
    if (!expires.isAfter(observed) ||
        expires.difference(observed) > const Duration(minutes: 2) ||
        !expires.isAfter(now) ||
        observed.isAfter(now.add(const Duration(seconds: 1))))
      throw const FormatException('处理列表已到期');
    final rows = [
      for (final r in m['items'] as List)
        AgentRunRecord.read(r, owner, agent: m['agentId']),
    ];
    if (rows.map((r) => r.id).toSet().length != rows.length ||
        rows.any((r) => r.observedAt.isAfter(observed)) ||
        (m['nextCursor'] != null && rows.length != 20))
      throw const FormatException('处理列表不符');
    return AgentRunPageData._(
      m['agentId'],
      List.unmodifiable(rows),
      m['nextCursor'],
      expires,
    );
  }
}

class AgentRunFailure implements Exception {
  const AgentRunFailure(this.status);
  final int status;
}

class AgentRunAPI {
  AgentRunAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owns = client == null,
      base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owns;
  final String base;
  Uri _uri(String suffix, [String? cursor]) => Uri.parse(
    '${base.replaceFirst(RegExp(r'/$'), '')}/v1/me/agent-runs$suffix',
  ).replace(queryParameters: cursor == null ? null : {'cursor': cursor});
  Future<dynamic> _response(Future<http.Response> request) async {
    final r = await request.timeout(const Duration(seconds: 12));
    if (r.statusCode != 200) throw AgentRunFailure(r.statusCode);
    if (r.bodyBytes.length > 65536 ||
        !(r.headers['content-type'] ?? '').toLowerCase().startsWith(
          'application/json',
        ))
      throw const FormatException('处理回执不符');
    return jsonDecode(utf8.decode(r.bodyBytes));
  }

  Future<AgentRunPageData> page(
    String token,
    String owner, {
    String? cursor,
  }) async => AgentRunPageData.read(
    await _response(
      _client.get(_uri('/page', cursor), headers: {'Authorization': token}),
    ),
    owner,
  );
  Future<AgentRunRecord> read(
    String token,
    String owner,
    String id, {
    String? agent,
  }) async {
    if (!candidateIDValid(id)) throw const FormatException('处理对象不符');
    return AgentRunRecord.read(
      await _response(
        _client.get(_uri('/$id'), headers: {'Authorization': token}),
      ),
      owner,
      id: id,
      agent: agent,
    );
  }

  Future<AgentRunRecord> cancel(
    String token,
    String owner,
    AgentRunRecord record,
  ) async => AgentRunRecord.read(
    await _response(
      _client.post(
        _uri('/${record.id}/cancel'),
        headers: {'Authorization': token, 'Content-Type': 'application/json'},
        body: jsonEncode({'expectedVersion': record.version}),
      ),
    ),
    owner,
    id: record.id,
    agent: record.agentID,
  );
  void dispose() {
    if (_owns) _client.close();
  }
}
