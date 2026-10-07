import 'dart:convert';
import 'dart:math';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

const candidateCategories = {
  'badminton': '羽毛球',
  'basketball': '篮球',
  'football': '足球',
  'sports': '运动',
  'culture': '文化活动',
  'hiking': '徒步',
};
const candidateStatusLabels = {
  'CANDIDATE': '待确认',
  'ACTIVE': '已声明',
  'REJECTED': '已拒绝',
  'SUPERSEDED': '已被替代',
  'EXPIRED': '已失效',
};
final _id = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
bool candidateIDValid(dynamic v) =>
    v is String &&
    _id.hasMatch(v) &&
    v != '00000000-0000-0000-0000-000000000000';
dynamic candidateFreezeJson(dynamic value) {
  if (value is Map) {
    return Map<String, dynamic>.unmodifiable(
      value.map((k, v) => MapEntry(k as String, candidateFreezeJson(v))),
    );
  }
  if (value is List) {
    return List<dynamic>.unmodifiable(value.map(candidateFreezeJson));
  }
  return value;
}

DateTime candidateTime(dynamic v) {
  if (v is! String || !RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(v)) {
    throw const FormatException('期限无效');
  }
  final t = DateTime.parse(v).toUtc();
  if (t.year < 1 || t.year > 9999) throw const FormatException('期限无效');
  return t;
}

class CandidateSourceChoice {
  const CandidateSourceChoice(this.type, this.id, this.title, this.detail);
  final String type, id, title, detail;
  String get key => '$type:$id';
  Map<String, String> get selector => {'type': type, 'id': id};
}

class HumanMemoryCandidate {
  HumanMemoryCandidate(this.raw);
  final Map<String, dynamic> raw;
  String get id => raw['id'] as String;
  int get version => raw['version'] as int;
  String get status => raw['status'] as String;
  String? get category => raw['category'] as String?;
  DateTime get validUntil => candidateTime(raw['validUntil']);
  String? get memoryID => raw['memoryId'] as String?;
  factory HumanMemoryCandidate.read(dynamic value, String owner) {
    final m = Map<String, dynamic>.from(value as Map);
    final o = m['owner'] as Map;
    final v = m['version'];
    if (m['schemaVersion'] != 'agent-memory-candidate-v1' ||
        !candidateIDValid(m['id']) ||
        !candidateIDValid(m['agentId']) ||
        !const ['PERSON', 'person'].contains(o['type']) ||
        o['id'] != owner ||
        v is! int ||
        v < 1 ||
        v > 9007199254740991 ||
        !candidateStatusLabels.containsKey(m['status']) ||
        m['modelAccess'] != 'UNAVAILABLE') {
      throw const FormatException('候选身份或状态无效');
    }
    candidateTime(m['validUntil']);
    candidateTime(m['createdAt']);
    candidateTime(m['updatedAt']);
    final sources = m['sources'] as List;
    if (sources.length > 20) throw const FormatException('来源过多');
    if (m['status'] == 'CANDIDATE' || m['status'] == 'ACTIVE') {
      if (m['predicate'] != 'ACTIVITY_CATEGORY' ||
          !candidateCategories.containsKey(m['category']) ||
          sources.isEmpty) {
        throw const FormatException('候选内容无效');
      }
      for (final s in sources) {
        final sel = (s as Map)['selector'] as Map;
        if (!const [
              'MOMENT',
              'ACTIVITY_PARTICIPATION',
              'SAVED_PLACE',
            ].contains(sel['type']) ||
            !candidateIDValid(sel['id'])) {
          throw const FormatException('来源无效');
        }
        final version = s['version'] as Map;
        final expectedKind = {
          'MOMENT': 'REVISION',
          'ACTIVITY_PARTICIPATION': 'UPDATED_AT_DIGEST',
          'SAVED_PLACE': 'CREATED_AT_DIGEST',
        }[sel['type']];
        if (version['kind'] != expectedKind ||
            (expectedKind == 'REVISION'
                ? version['revision'] is! int ||
                      (version['revision'] as int) < 1 ||
                      (version['revision'] as int) > 9007199254740991
                : version['token'] is! String ||
                      !RegExp(
                        r'^[0-9a-f]{64}$',
                      ).hasMatch(version['token'] as String))) {
          throw const FormatException('来源具体版本无效');
        }
        candidateTime(s['eventTime']);
        if (s['fingerprint'] is! String ||
            !RegExp(r'^[0-9a-f]{64}$').hasMatch(s['fingerprint'] as String)) {
          throw const FormatException('来源快照无效');
        }
      }
      if (sources
              .map((s) => jsonEncode((s as Map)['selector']))
              .toSet()
              .length !=
          sources.length) {
        throw const FormatException('来源重复');
      }
    } else if (m.containsKey('category') ||
        m.containsKey('predicate') ||
        m.containsKey('assessment') ||
        m.containsKey('memoryVersion') ||
        sources.isNotEmpty ||
        m.containsKey('memoryId')) {
      throw const FormatException('失效内容未清除');
    }
    if (m['status'] == 'ACTIVE' &&
        (!candidateIDValid(m['memoryId']) ||
            m['memoryVersion'] is! int ||
            (m['memoryVersion'] as int) < 1)) {
      throw const FormatException('声明结果无效');
    }
    return HumanMemoryCandidate(candidateFreezeJson(m) as Map<String, dynamic>);
  }
}

class HumanCandidatePreview {
  HumanCandidatePreview(this.id, this.review, this.candidate);
  final String id;
  final Map<String, dynamic> review;
  final HumanMemoryCandidate candidate;
  DateTime get expiresAt => candidateTime(review['expiresAt']);
  DateTime get memoryValidUntil => candidateTime(review['memoryValidUntil']);
  String get statement => review['statement'] as String;
  factory HumanCandidatePreview.read(
    dynamic value,
    String owner,
    HumanMemoryCandidate selected,
    String expectedID,
  ) {
    final m = value as Map;
    final r = Map<String, dynamic>.from(m['review'] as Map);
    final c = HumanMemoryCandidate.read(r['candidate'], owner);
    final expected =
        '我偏好${{'badminton': '羽毛球', 'basketball': '篮球', 'football': '足球', 'sports': '运动', 'culture': '文化', 'hiking': '徒步'}[c.category]}活动';
    if (m['previewId'] != expectedID ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(expectedID) ||
        c.id != selected.id ||
        c.version != selected.version ||
        c.category != selected.category ||
        c.raw['agentId'] != selected.raw['agentId'] ||
        c.status != 'CANDIDATE' ||
        r['purpose'] != 'HUMAN_EXPLICIT_DECLARATION' ||
        r['statement'] != expected ||
        !candidateIDValid(r['targetMemoryId']) ||
        r['expectedMemoryVersion'] != 0 ||
        r['previousMemory'] != null ||
        r['clusters'] is! int ||
        (r['clusters'] as int) < 2 ||
        r['planDigest'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(r['planDigest'] as String)) {
      throw const FormatException('批准版本无效');
    }
    final p = HumanCandidatePreview(
      expectedID,
      candidateFreezeJson(r) as Map<String, dynamic>,
      c,
    );
    if (!p.expiresAt.isAfter(DateTime.now()) ||
        p.expiresAt.isAfter(p.memoryValidUntil) ||
        p.expiresAt.isAfter(c.validUntil)) {
      throw const FormatException('预览已失效');
    }
    return p;
  }
}

class CandidateAPIError implements Exception {
  const CandidateAPIError(this.status);
  final int status;
  String get message => switch (status) {
    401 => '登录已失效，请重新登录。',
    403 => '当前身份或来源不可用，请重新检查。',
    404 => '候选已不可用。',
    409 => '版本、来源或批准已失效，请核实当前结果。',
    503 => '人工记忆候选未启用或服务暂不可用；不会自动写入。',
    _ => '请求未完成，请重试或核实结果。',
  };
}

class AgentMemoryCandidateAPI {
  AgentMemoryCandidateAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owns = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owns;
  final String _base;
  String get recoveryEnvironment => _base.replaceFirst(RegExp(r'/$'), '');
  static const path = '/v1/me/agent-memory-candidates';
  void dispose() {
    if (_owns) _client.close();
  }

  static String previewKey() {
    final r = Random.secure();
    return List.generate(
      32,
      (_) => r.nextInt(256).toRadixString(16).padLeft(2, '0'),
    ).join();
  }

  Future<dynamic> request(
    String method,
    String path,
    String auth, [
    Map<String, dynamic>? body,
  ]) async {
    if (!const ['GET', 'POST', 'DELETE'].contains(method) ||
        (method == 'GET' && body != null)) {
      throw const FormatException('请求方式或正文无效');
    }
    final u = Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path');
    if (_base.isEmpty ||
        !u.hasAuthority ||
        !const ['http', 'https'].contains(u.scheme)) {
      throw const FormatException('接口未配置');
    }
    final h = {
      'Authorization': auth,
      'Accept': 'application/json',
      if (body != null) 'Content-Type': 'application/json',
    };
    final encoded = body == null ? null : jsonEncode(body);
    final response = await (switch (method) {
      'GET' => _client.get(u, headers: h),
      'POST' => _client.post(u, headers: h, body: encoded),
      'DELETE' => _client.delete(u, headers: h, body: encoded),
      _ => throw const FormatException('请求方式无效'),
    }).timeout(const Duration(seconds: 20));
    if (response.statusCode != 200) {
      throw CandidateAPIError(response.statusCode);
    }
    if (response.bodyBytes.length > 1024 * 1024) {
      throw const FormatException('响应过大');
    }
    final decoded = jsonDecode(utf8.decode(response.bodyBytes)) as Map;
    // These original human purpose ports return bare DTOs. This only selects
    // their wire shape; each caller still validates the exact current DTO.
    if (_barePurposeRoute(method, path) && !decoded.containsKey('data')) {
      return decoded;
    }
    return decoded['data'];
  }

  static bool _barePurposeRoute(String method, String path) {
    for (final prefix in const [
      '/v1/me/agent-enrichment-purpose',
      '/v1/me/agent-multi-candidates',
    ]) {
      if (method == 'POST' && path == '$prefix/previews') return true;
      final preview = RegExp('^$prefix/previews/[A-Za-z0-9-]+');
      if (method == 'GET' && RegExp('${preview.pattern}\$').hasMatch(path)) {
        return true;
      }
      if (method == 'GET' &&
          RegExp('${preview.pattern}/receipt\$').hasMatch(path)) {
        return true;
      }
      if (method == 'POST' &&
          RegExp('${preview.pattern}/approve\$').hasMatch(path)) {
        return true;
      }
      if (const ['GET', 'DELETE'].contains(method) &&
          RegExp('^$prefix/grants/[A-Za-z0-9-]+\$').hasMatch(path)) {
        return true;
      }
    }
    return method == 'POST' && path == '/v1/me/agent-multi-candidates/stage' ||
        method == 'GET' &&
            RegExp(
              r'^/v1/me/agent-multi-candidates/grants/[A-Za-z0-9-]+/receipt$',
            ).hasMatch(path);
  }

  Future<List<HumanMemoryCandidate>> list(String auth, String owner) async {
    final v = await request('GET', path, auth) as List;
    if (v.length > 50) throw const FormatException('候选列表过长');
    final r = v.map((e) => HumanMemoryCandidate.read(e, owner)).toList();
    if (r.map((e) => e.id).toSet().length != r.length ||
        r.map((e) => e.raw['agentId']).toSet().length > 1) {
      throw const FormatException('候选列表身份冲突');
    }
    return r;
  }

  Future<HumanMemoryCandidate> read(
    String auth,
    String owner,
    String id,
  ) async {
    final r = HumanMemoryCandidate.read(
      await request('GET', '$path/$id', auth),
      owner,
    );
    if (r.id != id) throw const FormatException('目标不匹配');
    return r;
  }

  Future<List<CandidateSourceChoice>> sources(String auth, String owner) async {
    final all = await Future.wait(
      [
        '/v1/me/moments',
        '/v1/me/participations',
        '/v1/me/saved',
      ].map((p) => request('GET', p, auth)),
    );
    final out = <CandidateSourceChoice>[];
    for (var i = 0; i < 3; i++) {
      for (final raw in (all[i] as List).take(50)) {
        final m = raw as Map;
        if (!candidateIDValid(m['id'])) throw const FormatException('来源标识无效');
        final eligible = switch (i) {
          0 => m['authorAccountId'] == owner && m['status'] != 'withdrawn',
          1 => m['status'] == 'going' && m['available'] == true,
          _ => m['kind'] == 'place' && m['available'] == true,
        };
        if (!eligible) continue;
        out.add(
          CandidateSourceChoice(
            ['MOMENT', 'ACTIVITY_PARTICIPATION', 'SAVED_PLACE'][i],
            m['id'] as String,
            m['title'] is String && (m['title'] as String).isNotEmpty
                ? m['title'] as String
                : ['我的动态', '我的报名', '保存的地点'][i],
            ['自己记录的动态；不证明偏好', '已报名；不等于实际到场', '已保存；不等于实际到访'][i],
          ),
        );
      }
    }
    if (out.map((e) => e.key).toSet().length != out.length) {
      throw const FormatException('来源重复');
    }
    return out;
  }
}
