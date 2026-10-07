import 'dart:convert';
import 'dart:math';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import '../content/agent_profile_completion_api.dart'
    show completionMap, completionTime, completionDigest, completionVersion;
import 'agent_memory_candidate_api.dart'
    show
        candidateIDValid,
        candidateFreezeJson,
        HumanMemoryCandidate,
        candidateCategories;

DateTime correctionTime(dynamic raw) {
  final value = completionTime(raw);
  if (value.year < 1 || value.year > 9999) {
    throw const FormatException('期限超出有限范围');
  }
  return value;
}

const correctionSchema = 'agent-memory-correction-v1';
const correctionExplanation =
    '这是你本人的具体纠正。明确选择不偏好某类活动后，会停止以任何来源再次提出该类偏好；不代表概率，也不删除原动态。本人独立声明不因关联来源到期而删除。删除纠正记忆不会恢复旧推断或旧批准。';
const correctionNegativeStatements = {
  'badminton': '我不偏好羽毛球活动',
  'basketball': '我不偏好篮球活动',
  'football': '我不偏好足球活动',
  'sports': '我不偏好运动活动',
  'culture': '我不偏好文化活动',
  'hiking': '我不偏好徒步活动',
};
const correctionActions = {
  'EDIT': '修改',
  'DELETE': '删除',
  'REJECT': '拒绝保留',
  'NEGATE': '明确不偏好该类活动',
};
const _memoryTypes = {
  'IDENTITY',
  'PREFERENCE',
  'PLACE',
  'CITY',
  'ACTIVITY',
  'COMMUNITY',
  'ORGANIZATION',
  'RELATIONSHIP_CONTEXT',
  'HISTORY',
  'INTENT',
  'ROUTINE',
  'AVAILABILITY',
  'EXPERIENCE',
};
void _require(bool ok) {
  if (!ok) throw const FormatException('记忆纠正回执不符');
}

void _owner(Map<String, dynamic> m, String owner) {
  final o = completionMap(m['owner'], {'type', 'id'});
  _require(
    m['schemaVersion'] == correctionSchema &&
        o['type'] == 'PERSON' &&
        o['id'] == owner &&
        candidateIDValid(owner) &&
        candidateIDValid(m['agentId']) &&
        m['modelAccess'] == false,
  );
}

class CorrectableMemory {
  CorrectableMemory._(this.raw);
  factory CorrectableMemory.read(dynamic value, String owner) {
    final m = completionMap(value, {
      'schemaVersion',
      'id',
      'agentId',
      'ownerType',
      'ownerId',
      'version',
      'memoryType',
      'memoryKey',
      'summary',
      'structuredValue',
      'confidence',
      'sourceType',
      'visibility',
      'status',
      'validFrom',
      'validUntil',
      'lastReinforcedAt',
      'createdAt',
      'updatedAt',
    });
    _require(
      m['schemaVersion'] == 'agent-memory-v1' &&
          candidateIDValid(m['id']) &&
          candidateIDValid(m['agentId']) &&
          m['ownerType'] == 'PERSON' &&
          m['ownerId'] == owner &&
          candidateIDValid(owner) &&
          completionVersion(m['version']) &&
          _memoryTypes.contains(m['memoryType']) &&
          m['memoryKey'] is String &&
          RegExp(r'^[a-z0-9][a-z0-9._:-]{0,99}$').hasMatch(m['memoryKey']) &&
          m['summary'] is String &&
          utf8.encode(m['summary']).length <= 1200 &&
          m['structuredValue'] is Map &&
          utf8.encode(jsonEncode(m['structuredValue'])).length <= 8192 &&
          {'PRIVATE', 'AGENT_ONLY'}.contains(m['visibility']) &&
          {
            'ACTIVE',
            'EXPIRED',
            'DELETED',
            'PENDING_REVIEW',
          }.contains(m['status']) &&
          m['confidence'] is num &&
          (m['confidence'] as num).isFinite &&
          m['confidence'] >= 0 &&
          m['confidence'] <= 1,
    );
    final start = correctionTime(m['validFrom']),
        end = correctionTime(m['validUntil']),
        created = correctionTime(m['createdAt']),
        updated = correctionTime(m['updatedAt']);
    _require(
      end.isAfter(start) &&
          end.difference(start) <= const Duration(days: 365) &&
          !updated.isBefore(created),
    );
    if (m['sourceType'] == 'EXPLICIT') {
      _require(
        m['confidence'] == 1 &&
            m['lastReinforcedAt'] == null &&
            {'ACTIVE', 'EXPIRED', 'DELETED'}.contains(m['status']),
      );
    } else {
      _require(
        m['sourceType'] == 'INFERRED' &&
            {'PENDING_REVIEW', 'EXPIRED', 'DELETED'}.contains(m['status']),
      );
      if (m['lastReinforcedAt'] != null) {
        final reinforced = correctionTime(m['lastReinforcedAt']);
        _require(!reinforced.isBefore(created) && !reinforced.isAfter(updated));
      }
    }
    if (m['status'] == 'DELETED') {
      _require(
        m['summary'] == '' &&
            (m['structuredValue'] as Map).isEmpty &&
            m['lastReinforcedAt'] == null,
      );
    } else {
      _require((m['summary'] as String).trim().isNotEmpty);
    }
    return CorrectableMemory._(candidateFreezeJson(m));
  }
  final Map<String, dynamic> raw;
  String get id => raw['id'];
  String get agentID => raw['agentId'];
  String get summary => raw['summary'];
  String get status => raw['status'];
  String get sourceType => raw['sourceType'];
  int get version => raw['version'];
  DateTime get validUntil => correctionTime(raw['validUntil']);
  String? get category {
    final key = raw['memoryKey'] as String;
    if (raw['memoryType'] != 'PREFERENCE' ||
        !key.startsWith('activity_category:')) {
      return null;
    }
    final c = key.substring('activity_category:'.length);
    return candidateCategories.containsKey(c) ? c : null;
  }

  Map<String, dynamic> replacement(String text) => {
    'expectedVersion': version,
    'memoryType': raw['memoryType'],
    'memoryKey': raw['memoryKey'],
    'summary': text.trim(),
    'structuredValue': raw['structuredValue'],
    'visibility': raw['visibility'],
    'validUntil': raw['validUntil'],
  };
}

Map<String, dynamic> correctionInput(dynamic raw) {
  final m = completionMap(
    raw,
    {'id', 'targetKind', 'targetId', 'expectedVersion', 'action'},
    optional: {'category', 'replacement'},
  );
  _require(
    candidateIDValid(m['id']) &&
        candidateIDValid(m['targetId']) &&
        completionVersion(m['expectedVersion']) &&
        {'MEMORY', 'CANDIDATE'}.contains(m['targetKind']) &&
        correctionActions.containsKey(m['action']),
  );
  final a = m['action'];
  _require(
    (a == 'NEGATE') == m.containsKey('category') &&
        (a == 'EDIT') == m.containsKey('replacement'),
  );
  if (a == 'EDIT' || a == 'DELETE') _require(m['targetKind'] == 'MEMORY');
  if (a == 'NEGATE') _require(candidateCategories.containsKey(m['category']));
  if (a == 'EDIT') {
    final x = completionMap(m['replacement'], {
      'expectedVersion',
      'memoryType',
      'memoryKey',
      'summary',
      'structuredValue',
      'visibility',
      'validUntil',
    });
    _require(
      x['expectedVersion'] == m['expectedVersion'] &&
          _memoryTypes.contains(x['memoryType']) &&
          x['memoryKey'] is String &&
          RegExp(r'^[a-z0-9][a-z0-9._:-]{0,99}$').hasMatch(x['memoryKey']) &&
          x['summary'] is String &&
          (x['summary'] as String).trim().isNotEmpty &&
          utf8.encode(x['summary']).length <= 1200 &&
          x['structuredValue'] is Map &&
          utf8.encode(jsonEncode(x['structuredValue'])).length <= 8192 &&
          {'PRIVATE', 'AGENT_ONLY'}.contains(x['visibility']),
    );
    correctionTime(x['validUntil']);
  }
  return candidateFreezeJson(m);
}

class CorrectionPreview {
  CorrectionPreview._(this.raw, this.memories);
  factory CorrectionPreview.read(dynamic raw, String owner) {
    final m = completionMap(
      raw,
      {
        'schemaVersion',
        'id',
        'owner',
        'agentId',
        'input',
        'memories',
        'affected',
        'planDigest',
        'observedAt',
        'expiresAt',
        'explanation',
        'modelAccess',
      },
      optional: {'newMemoryValidUntil'},
    );
    _owner(m, owner);
    final input = correctionInput(m['input']);
    final at = correctionTime(m['observedAt']),
        end = correctionTime(m['expiresAt']);
    _require(
      m['id'] == input['id'] &&
          completionDigest(m['planDigest']) &&
          end.isAfter(at) &&
          end.difference(at) <= const Duration(minutes: 5) &&
          m['explanation'] == correctionExplanation &&
          m['memories'] is List &&
          m['affected'] is List &&
          (m['memories'] as List).length <= 100 &&
          (m['affected'] as List).length <= 100,
    );
    final affected = <String, int>{};
    for (final v in m['affected']) {
      final t = completionMap(v, {'kind', 'id', 'version'});
      _require(
        {'MEMORY', 'CANDIDATE'}.contains(t['kind']) &&
            candidateIDValid(t['id']) &&
            completionVersion(t['version']),
      );
      final key = '${t['kind']}:${t['id']}';
      _require(!affected.containsKey(key));
      affected[key] = t['version'];
    }
    _require(
      affected['${input['targetKind']}:${input['targetId']}'] ==
          input['expectedVersion'],
    );
    final memories = [
      for (final v in m['memories']) CorrectableMemory.read(v, owner),
    ];
    final seen = <String>{};
    for (final memory in memories) {
      _require(
        memory.agentID == m['agentId'] &&
            affected['MEMORY:${memory.id}'] == memory.version &&
            seen.add(memory.id),
      );
    }
    for (final key in affected.keys.where((k) => k.startsWith('MEMORY:'))) {
      _require(seen.contains(key.substring(7)));
    }
    if (input['action'] == 'NEGATE') {
      _require(
        m.containsKey('newMemoryValidUntil') &&
            correctionTime(
              m['newMemoryValidUntil'],
            ).isAtSameMomentAs(at.add(const Duration(days: 365))),
      );
    } else {
      _require(!m.containsKey('newMemoryValidUntil'));
    }
    return CorrectionPreview._(
      candidateFreezeJson(m),
      List.unmodifiable(memories),
    );
  }
  final Map<String, dynamic> raw;
  final List<CorrectableMemory> memories;
  String get id => raw['id'];
  String get agentID => raw['agentId'];
  String get digest => raw['planDigest'];
  Map<String, dynamic> get input => raw['input'];
  DateTime get expiresAt => correctionTime(raw['expiresAt']);
}

class CorrectionReceipt {
  CorrectionReceipt._(this.raw);
  factory CorrectionReceipt.read(dynamic raw, String owner) {
    final m = completionMap(
      raw,
      {
        'schemaVersion',
        'id',
        'owner',
        'agentId',
        'target',
        'action',
        'planDigest',
        'state',
        'currentResultMatches',
        'suppressionActive',
        'observedAt',
        'expiresAt',
        'modelAccess',
      },
      optional: {'resultMemoryId', 'resultMemoryVersion', 'committedAt'},
    );
    _owner(m, owner);
    final t = completionMap(m['target'], {'kind', 'id', 'version'});
    _require(
      candidateIDValid(m['id']) &&
          candidateIDValid(t['id']) &&
          completionVersion(t['version']) &&
          {'MEMORY', 'CANDIDATE'}.contains(t['kind']) &&
          correctionActions.containsKey(m['action']) &&
          completionDigest(m['planDigest']) &&
          {'PENDING', 'EXPIRED', 'COMMITTED'}.contains(m['state']) &&
          m['currentResultMatches'] is bool &&
          m['suppressionActive'] is bool &&
          (!(m['suppressionActive'] as bool) || m['action'] == 'NEGATE'),
    );
    final at = correctionTime(m['observedAt']),
        end = correctionTime(m['expiresAt']);
    if (m['action'] == 'EDIT' || m['action'] == 'DELETE') {
      _require(t['kind'] == 'MEMORY');
    }
    if (m['state'] == 'COMMITTED') {
      final commit = correctionTime(m['committedAt']);
      _require(!commit.isAfter(at) && commit.isBefore(end));
      if (t['kind'] == 'CANDIDATE' && m['action'] == 'REJECT') {
        _require(
          !m.containsKey('resultMemoryId') &&
              !m.containsKey('resultMemoryVersion') &&
              m['currentResultMatches'] == false,
        );
      } else {
        _require(
          candidateIDValid(m['resultMemoryId']) &&
              completionVersion(m['resultMemoryVersion']),
        );
        if (m['action'] != 'NEGATE') {
          _require(
            m['resultMemoryId'] == t['id'] &&
                m['resultMemoryVersion'] == t['version'] + 1,
          );
        }
      }
    } else {
      _require(
        !m.containsKey('committedAt') &&
            !m.containsKey('resultMemoryId') &&
            !m.containsKey('resultMemoryVersion') &&
            m['currentResultMatches'] == false &&
            m['suppressionActive'] == false &&
            (m['state'] == 'PENDING') == end.isAfter(at),
      );
    }
    return CorrectionReceipt._(candidateFreezeJson(m));
  }
  final Map<String, dynamic> raw;
  String get id => raw['id'];
  String get agentID => raw['agentId'];
  String get digest => raw['planDigest'];
  String get state => raw['state'];
  bool get currentMatches => raw['currentResultMatches'];
  DateTime get expiresAt => correctionTime(raw['expiresAt']);
}

class CorrectionHTTPError implements Exception {
  const CorrectionHTTPError(this.status);
  final int status;
}

class AgentMemoryCorrectionAPI {
  AgentMemoryCorrectionAPI({http.Client? client, String? apiBaseUrl})
    : client = client ?? http.Client(),
      ownsClient = client == null,
      base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client client;
  final bool ownsClient;
  final String base;
  String get environment => Uri.parse(base).normalizePath().toString();
  Future<dynamic> request(
    String method,
    String suffix,
    String token, {
    Map<String, dynamic>? body,
  }) async {
    String path;
    bool envelope = false;
    if (method == 'GET' && {'memories', 'candidates'}.contains(suffix)) {
      path = suffix == 'memories'
          ? '/v1/me/agent-memories'
          : '/v1/me/agent-memory-candidates';
      envelope = true;
    } else if (method == 'GET' &&
        suffix.startsWith('memories/') &&
        suffix.split('/').length == 2 &&
        candidateIDValid(suffix.split('/').last)) {
      path = '/v1/me/agent-memories/${suffix.split('/').last}';
      envelope = true;
    } else if (method == 'POST' && suffix == 'self-review') {
      final m = completionMap(body, {'profileFields', 'memoryIds', 'policyFamilies'});
      _require(jsonEncode(m['profileFields']) == '["preferredActivityTypes"]' &&
          m['memoryIds'] is List && (m['memoryIds'] as List).length == 1 &&
          candidateIDValid((m['memoryIds'] as List).single) &&
          m['policyFamilies'] is List && (m['policyFamilies'] as List).isEmpty);
      path = '/v1/me/agent-context/self-review';
      envelope = true;
    } else if (method == 'POST' && suffix == 'previews') {
      path = '/v1/me/agent-memory-corrections/previews';
      correctionInput(body);
    } else if (method == 'GET' && candidateIDValid(suffix)) {
      path = '/v1/me/agent-memory-corrections/$suffix';
    } else if (method == 'POST' &&
        suffix.endsWith('/confirm') &&
        candidateIDValid(suffix.split('/').first) &&
        suffix.split('/').length == 2) {
      path = '/v1/me/agent-memory-corrections/$suffix';
      final m = completionMap(body, {'planDigest'});
      _require(completionDigest(m['planDigest']));
    } else {
      throw const FormatException('请求范围不符');
    }
    _require(token.isNotEmpty && (method != 'GET' || body == null));
    final uri = Uri.parse('${base.replaceAll(RegExp(r'/$'), '')}$path');
    _require(
      uri.host.isNotEmpty &&
          {'http', 'https'}.contains(uri.scheme) &&
          uri.query.isEmpty &&
          uri.fragment.isEmpty,
    );
    final req = http.Request(method, uri)..headers['Authorization'] = token;
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final response = await client
        .send(req)
        .timeout(const Duration(seconds: 12));
    final bytes = <int>[];
    await for (final part in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      bytes.addAll(part);
      if (bytes.length > 2 * 1024 * 1024) throw const FormatException('回执过大');
    }
    if (response.statusCode != 200) {
      throw CorrectionHTTPError(response.statusCode);
    }
    final raw = jsonDecode(utf8.decode(bytes));
    if (envelope) return completionMap(raw, {'data'})['data'];
    return raw;
  }

  Future<List<CorrectableMemory>> memories(String token, String owner) async {
    final raw = await request('GET', 'memories', token);
    _require(raw is List && raw.length <= 1000);
    final result = [for (final v in raw) CorrectableMemory.read(v, owner)];
    _require(
      result.map((m) => m.id).toSet().length == result.length &&
          result.map((m) => m.agentID).toSet().length <= 1,
    );
    return List.unmodifiable(result);
  }

  Future<List<HumanMemoryCandidate>> candidates(
    String token,
    String owner,
  ) async {
    final raw = await request('GET', 'candidates', token);
    _require(raw is List && raw.length <= 50);
    final list = [for (final v in raw) HumanMemoryCandidate.read(v, owner)];
    _require(
      list.map((c) => c.id).toSet().length == list.length &&
          list.map((c) => c.raw['agentId']).toSet().length <= 1,
    );
    return List.unmodifiable(list);
  }

  String newID() {
    final r = Random.secure(),
        b = List<int>.generate(16, (_) => r.nextInt(256));
    b[6] = (b[6] & 15) | 64;
    b[8] = (b[8] & 63) | 128;
    final h = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-${h.substring(16, 20)}-${h.substring(20)}';
  }

  void dispose() {
    if (ownsClient) client.close();
  }
}
