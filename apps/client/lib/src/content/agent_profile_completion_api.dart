import 'dart:convert';
import 'dart:math';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import '../workspace/agent_memory_candidate_api.dart' show candidateIDValid;

const profileCompletionSchema = 'agent-profile-memory-completion-v1';
const profileCompletionPurpose = 'HUMAN_PRIVATE_PROFILE_COMPLETION';
const profileCompletionField = 'preferredActivityTypes';
const profileCompletionStatements = <String, String>{
  'badminton': '我偏好羽毛球活动',
  'basketball': '我偏好篮球活动',
  'football': '我偏好足球活动',
  'sports': '我偏好运动活动',
  'culture': '我偏好文化活动',
  'hiking': '我偏好徒步活动',
};
bool completionVersion(dynamic v) => v is int && v > 0 && v < 9007199254740991;
bool completionDigest(dynamic v) =>
    v is String && RegExp(r'^[0-9a-f]{64}$').hasMatch(v);
DateTime completionTime(dynamic raw) {
  if (raw is! String) throw const FormatException('期限格式不符');
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|([+-])(\d{2}):(\d{2}))$',
  ).firstMatch(raw);
  if (m == null || m.end != raw.length) throw const FormatException('期限格式不符');
  final v = [for (var i = 1; i <= 6; i++) int.parse(m.group(i)!)];
  final t = DateTime.utc(v[0], v[1], v[2], v[3], v[4], v[5]);
  if (v[0] < 1 ||
      v[0] > 9999 ||
      t.year != v[0] ||
      t.month != v[1] ||
      t.day != v[2] ||
      t.hour != v[3] ||
      t.minute != v[4] ||
      t.second != v[5] ||
      (m.group(8) != 'Z' &&
          (int.parse(m.group(10)!) > 23 || int.parse(m.group(11)!) > 59))) {
    throw const FormatException('期限格式不符');
  }
  return DateTime.parse(raw).toUtc();
}

Map<String, dynamic> completionMap(
  dynamic v,
  Set<String> keys, {
  Set<String> optional = const {},
}) {
  if (v is! Map) throw const FormatException('回执形状不符');
  final m = Map<String, dynamic>.from(v);
  if (!m.keys.toSet().containsAll(keys) ||
      m.keys.any((k) => !keys.contains(k) && !optional.contains(k))) {
    throw const FormatException('回执字段不符');
  }
  return m;
}

void _owner(Map<String, dynamic> m, String owner) {
  final o = completionMap(m['owner'], {'type', 'id'});
  if (m['schemaVersion'] != profileCompletionSchema ||
      o['type'] != 'PERSON' ||
      o['id'] != owner ||
      !candidateIDValid(owner) ||
      !candidateIDValid(m['agentId']) ||
      m['modelAccess'] != false) {
    throw const FormatException('回执身份不符');
  }
}

class ProfileCompletionSource {
  ProfileCompletionSource._(
    this.id,
    this.version,
    this.category,
    this.value,
    this.validUntil,
  );
  factory ProfileCompletionSource.read(dynamic value, DateTime now) {
    final m = completionMap(value, {
      'memoryId',
      'memoryVersion',
      'category',
      'value',
      'memoryValidUntil',
    });
    final end = completionTime(m['memoryValidUntil']);
    if (!candidateIDValid(m['memoryId']) ||
        !completionVersion(m['memoryVersion']) ||
        !profileCompletionStatements.containsKey(m['category']) ||
        profileCompletionStatements[m['category']] != m['value'] ||
        !end.isAfter(now)) {
      throw const FormatException('来源不符或已失效');
    }
    return ProfileCompletionSource._(
      m['memoryId'],
      m['memoryVersion'],
      m['category'],
      m['value'],
      end,
    );
  }
  final String id, category, value;
  final int version;
  final DateTime validUntil;
}

class ProfileCompletionSuggestions {
  ProfileCompletionSuggestions._(
    this.agentID,
    this.profileVersion,
    this.alreadySet,
    this.sources,
  );
  factory ProfileCompletionSuggestions.read(dynamic raw, String owner) {
    final m = completionMap(raw, {
      'schemaVersion',
      'owner',
      'agentId',
      'profileVersion',
      'targetField',
      'state',
      'sources',
      'observedAt',
      'modelAccess',
    });
    _owner(m, owner);
    final at = completionTime(m['observedAt']);
    if (!completionVersion(m['profileVersion']) ||
        m['targetField'] != profileCompletionField ||
        !{'FIELD_EMPTY', 'FIELD_ALREADY_SET'}.contains(m['state']) ||
        m['sources'] is! List ||
        (m['sources'] as List).length > 6) {
      throw const FormatException('建议状态不符');
    }
    final sources = [
      for (final v in m['sources']) ProfileCompletionSource.read(v, at),
    ];
    if (sources.map((s) => s.id).toSet().length != sources.length ||
        sources.map((s) => s.category).toSet().length != sources.length ||
        (m['state'] == 'FIELD_ALREADY_SET' && sources.isNotEmpty)) {
      throw const FormatException('来源重复或目标已有值');
    }
    return ProfileCompletionSuggestions._(
      m['agentId'],
      m['profileVersion'],
      m['state'] == 'FIELD_ALREADY_SET',
      List.unmodifiable(sources),
    );
  }
  final String agentID;
  final int profileVersion;
  final bool alreadySet;
  final List<ProfileCompletionSource> sources;
}

class ProfileCompletionPreview {
  ProfileCompletionPreview._(
    this.id,
    this.agentID,
    this.source,
    this.profileVersion,
    this.digest,
    this.expiresAt,
    this.explanation,
  );
  factory ProfileCompletionPreview.read(dynamic raw, String owner) {
    final m = completionMap(raw, {
      'schemaVersion',
      'id',
      'purpose',
      'owner',
      'agentId',
      'source',
      'expectedProfileVersion',
      'targetField',
      'before',
      'after',
      'planDigest',
      'observedAt',
      'expiresAt',
      'explanation',
      'modelAccess',
    });
    _owner(m, owner);
    final at = completionTime(m['observedAt']),
        end = completionTime(m['expiresAt']);
    final source = ProfileCompletionSource.read(m['source'], at);
    if (!candidateIDValid(m['id']) ||
        m['purpose'] != profileCompletionPurpose ||
        !completionVersion(m['expectedProfileVersion']) ||
        m['targetField'] != profileCompletionField ||
        !completionDigest(m['planDigest']) ||
        m['before'] is! List ||
        (m['before'] as List).isNotEmpty ||
        m['after'] is! List ||
        (m['after'] as List).length != 1 ||
        m['after'][0] != source.value ||
        !end.isAfter(at) ||
        end.isAfter(source.validUntil) ||
        end.difference(at) > const Duration(minutes: 5) ||
        m['explanation'] is! String ||
        (m['explanation'] as String).isEmpty ||
        (m['explanation'] as String).runes.length > 1000) {
      throw const FormatException('具体审阅版本不符');
    }
    return ProfileCompletionPreview._(
      m['id'],
      m['agentId'],
      source,
      m['expectedProfileVersion'],
      m['planDigest'],
      end,
      m['explanation'],
    );
  }
  final String id, agentID, digest, explanation;
  final ProfileCompletionSource source;
  final int profileVersion;
  final DateTime expiresAt;
}

class ProfileCompletionReceipt {
  ProfileCompletionReceipt._(
    this.id,
    this.agentID,
    this.memoryID,
    this.memoryVersion,
    this.profileVersion,
    this.digest,
    this.state,
    this.currentMatches,
    this.expiresAt,
  );
  factory ProfileCompletionReceipt.read(dynamic raw, String owner) {
    final m = completionMap(
      raw,
      {
        'schemaVersion',
        'id',
        'purpose',
        'owner',
        'agentId',
        'memoryId',
        'memoryVersion',
        'expectedProfileVersion',
        'planDigest',
        'state',
        'currentProfileMatches',
        'observedAt',
        'expiresAt',
        'modelAccess',
      },
      optional: {'resultProfileVersion', 'committedAt'},
    );
    _owner(m, owner);
    final at = completionTime(m['observedAt']),
        end = completionTime(m['expiresAt']);
    if (!candidateIDValid(m['id']) ||
        !candidateIDValid(m['memoryId']) ||
        m['purpose'] != profileCompletionPurpose ||
        !completionVersion(m['memoryVersion']) ||
        !completionVersion(m['expectedProfileVersion']) ||
        !completionDigest(m['planDigest']) ||
        m['currentProfileMatches'] is! bool ||
        !{'PENDING', 'EXPIRED', 'COMMITTED'}.contains(m['state'])) {
      throw const FormatException('核实回执不符');
    }
    if (m['state'] == 'COMMITTED') {
      final committed = completionTime(m['committedAt']);
      if (!completionVersion(m['resultProfileVersion']) ||
          m['resultProfileVersion'] != m['expectedProfileVersion'] + 1 ||
          !committed.isBefore(end) ||
          committed.isAfter(at)) {
        throw const FormatException('保存版本不符');
      }
    } else if (m.containsKey('resultProfileVersion') ||
        m.containsKey('committedAt') ||
        m['currentProfileMatches'] != false ||
        (m['state'] == 'PENDING') != end.isAfter(at)) {
      throw const FormatException('核实状态不符');
    }
    return ProfileCompletionReceipt._(
      m['id'],
      m['agentId'],
      m['memoryId'],
      m['memoryVersion'],
      m['expectedProfileVersion'],
      m['planDigest'],
      m['state'],
      m['currentProfileMatches'],
      end,
    );
  }
  final String id, agentID, memoryID, digest, state;
  final int memoryVersion, profileVersion;
  final bool currentMatches;
  final DateTime expiresAt;
}

class ProfileCompletionHTTPError implements Exception {
  const ProfileCompletionHTTPError(this.status);
  final int status;
}

class AgentProfileCompletionAPI {
  AgentProfileCompletionAPI({http.Client? client, String? apiBaseUrl})
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
    if (!{'GET', 'POST'}.contains(method) ||
        (method == 'GET' && body != null) ||
        token.isEmpty) {
      throw const FormatException('请求范围不符');
    }
    final uri = Uri.parse(
      '${base.replaceAll(RegExp(r'/$'), '')}/v1/me/agent-profile-completion/$suffix',
    );
    final req = http.Request(method, uri)..headers['Authorization'] = token;
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final response = await client.send(req);
    final bytes = <int>[];
    await for (final part in response.stream) {
      bytes.addAll(part);
      if (bytes.length > 65536) throw const FormatException('回执过大');
    }
    if (response.statusCode != 200) {
      throw ProfileCompletionHTTPError(response.statusCode);
    }
    return jsonDecode(utf8.decode(bytes));
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
