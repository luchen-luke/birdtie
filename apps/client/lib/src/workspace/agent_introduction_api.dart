import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'model_egress_api.dart' show egressTime, egressIDValid;

const introductionCategories = <String>[
  'SAME_UNIVERSITY',
  'SHARED_COMMUNITY',
  'SHARED_ACTIVITY',
  'EXISTING_CONNECTION',
  'UNKNOWN_PERSON',
  'BUSINESS',
  'ORGANIZATION',
];
const introductionCategoryLabels = <String, String>{
  'SAME_UNIVERSITY': '同大学情境',
  'SHARED_COMMUNITY': '共同社群声明',
  'SHARED_ACTIVITY': '共同公开报名',
  'EXISTING_CONNECTION': '已有好友',
  'UNKNOWN_PERSON': '尚未建立关系的人',
  'BUSINESS': '商家',
  'ORGANIZATION': '组织',
};
Map<String, dynamic> _object(dynamic v) {
  if (v is! Map<String, dynamic>) throw const FormatException('记录结构无效');
  return v;
}

void _keys(
  Map<String, dynamic> m,
  Set<String> required, [
  Set<String> optional = const {},
]) {
  if (!m.keys.toSet().containsAll(required) ||
      !required.union(optional).containsAll(m.keys)) {
    throw const FormatException('记录字段无效');
  }
}

String _id(dynamic v) {
  if (!egressIDValid(v)) throw const FormatException('记录标识无效');
  return v as String;
}

String _text(dynamic v, {int max = 4096}) {
  if (v is! String || v.trim().isEmpty || v.length > max) {
    throw const FormatException('记录文本无效');
  }
  return v;
}

class IntroductionIntent {
  IntroductionIntent(Map<String, dynamic> v, String owner)
    : id = _id(v['id']),
      title = _text(v['title'], max: 320),
      expiry = egressTime(v['expiresAt']),
      eligible =
          v['type'] == 'FIND_COMPANION' &&
          v['status'] == 'ACTIVE' &&
          v['audience'] == 'PUBLIC' {
    if (_id(v['creatorAccountId']) != owner ||
        v['type'] != 'FIND_COMPANION' ||
        ![
          'DRAFT',
          'ACTIVE',
          'CANCELLED',
          'EXPIRED',
          'MATCHED',
          'CONVERTED',
        ].contains(v['status']) ||
        ![
          'PUBLIC',
          'PRIVATE',
          'FRIENDS',
          'LOCAL',
          'COMMUNITY',
          'INVITE_ONLY',
        ].contains(v['audience'])) {
      throw const FormatException('意图归属或状态无效');
    }
  }
  final String id, title;
  final DateTime expiry;
  final bool eligible;
  bool current(DateTime now) => eligible && expiry.isAfter(now);
}

class IntroductionPolicy {
  IntroductionPolicy(Map<String, dynamic> b, String owner)
    : ownerID = _id(b['ownerId']),
      agentID = _id(b['agentId']),
      observedAt = egressTime(b['observedAt']) {
    if (b['schemaVersion'] != 'agent-policy-settings-v1' ||
        b['ownerType'] != 'PERSON' ||
        ownerID != owner) {
      throw const FormatException('策略主体不符');
    }
    final s = _object(b['social']);
    _keys(
      s,
      {'family', 'configured', 'nativeRevision', 'status', 'settings'},
      {'validFrom', 'expiresAt', 'updatedAt'},
    );
    if (s['family'] != 'SOCIAL' ||
        s['configured'] is! bool ||
        s['nativeRevision'] is! int ||
        (s['nativeRevision'] as int) < 0 ||
        (s['nativeRevision'] as int) > 9223372036854775806) {
      throw const FormatException('策略版本无效');
    }
    revision = s['nativeRevision'] as int;
    configured = s['configured'] as bool;
    status = _text(s['status']);
    final settings = _object(s['settings']);
    _keys(settings, {'rules'});
    final rows = settings['rules'];
    if (rows is! List || rows.length != 7) {
      throw const FormatException('策略类别不完整');
    }
    final values = <String, String>{};
    for (final raw in rows) {
      final r = _object(raw);
      _keys(r, {'category', 'preference'});
      final k = r['category'], p = r['preference'];
      if (!introductionCategories.contains(k) ||
          !['DISABLED', 'REVIEW_REQUIRED'].contains(p) ||
          values.containsKey(k)) {
        throw const FormatException('策略类别或偏好无效');
      }
      values[k as String] = p as String;
    }
    rules = Map.unmodifiable(values);
    if (!configured) {
      expiry = null;
      if (revision != 0 ||
          status != 'UNCONFIGURED' ||
          s['validFrom'] != null ||
          s['expiresAt'] != null ||
          s['updatedAt'] != null ||
          rules.values.any((p) => p != 'DISABLED')) {
        throw const FormatException('未配置策略不一致');
      }
    } else {
      final from = egressTime(s['validFrom']),
          updated = egressTime(s['updatedAt']);
      expiry = egressTime(s['expiresAt']);
      if (revision < 1 ||
          from.isAfter(observedAt) ||
          updated != from ||
          !expiry!.isAfter(from) ||
          expiry!.difference(from) > const Duration(days: 30) ||
          status != (observedAt.isBefore(expiry!) ? 'ACTIVE' : 'EXPIRED')) {
        throw const FormatException('策略时间或状态无效');
      }
    }
  }
  final String ownerID, agentID;
  final DateTime observedAt;
  late final int revision;
  late final bool configured;
  late final String status;
  late final Map<String, String> rules;
  late final DateTime? expiry;
  bool get reviewEnabled =>
      status == 'ACTIVE' && rules['UNKNOWN_PERSON'] == 'REVIEW_REQUIRED';
}

class IntroductionCandidate {
  IntroductionCandidate(
    Map<String, dynamic> v,
    String source,
    DateTime observed,
    DateTime end,
  ) : sourceID = _id(v['sourceIntentId']),
      intentID = _id(v['candidateIntentId']),
      accountID = _id(v['accountId']),
      name = _text(v['displayName'], max: 320),
      expiry = egressTime(v['expiresAt']) {
    _keys(v, {
      'sourceIntentId',
      'candidateIntentId',
      'accountId',
      'displayName',
      'basis',
      'sourceBinding',
      'expiresAt',
    });
    if (sourceID != source ||
        intentID == source ||
        !observed.isBefore(expiry) ||
        expiry.isAfter(end) ||
        v['sourceBinding'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(v['sourceBinding'])) {
      throw const FormatException('候选来源或期限无效');
    }
    final raw = v['basis'];
    if (raw is! List || raw.isEmpty || raw.length > 4) {
      throw const FormatException('公开依据无效');
    }
    final result = <String, String>{};
    for (final item in raw) {
      final b = _object(item);
      _keys(b, {'kind', 'explanation'});
      final k = b['kind'];
      if (![
            'SHARED_INTEREST',
            'SHARED_CITY',
            'SHARED_COMMUNITY',
            'SHARED_ACTIVITY',
          ].contains(k) ||
          result.containsKey(k)) {
        throw const FormatException('公开依据类别无效');
      }
      result[k as String] = _text(b['explanation']);
    }
    if (!result.containsKey('SHARED_INTEREST')) {
      throw const FormatException('明确意图依据缺失');
    }
    basis = Map.unmodifiable(result);
  }
  final String sourceID, intentID, accountID, name;
  final DateTime expiry;
  late final Map<String, String> basis;
}

class IntroductionResult {
  IntroductionResult(Map<String, dynamic> v, String source, String owner)
    : sourceID = _id(v['sourceIntentId']),
      observedAt = egressTime(v['observedAt']),
      expiry = egressTime(v['expiresAt']),
      explanation = _text(v['explanation']) {
    _keys(v, {
      'schemaVersion',
      'mode',
      'sourceIntentId',
      'sourceStatus',
      'candidates',
      'truncated',
      'observedAt',
      'expiresAt',
      'explanation',
      'modelAccess',
      'sendAllowed',
      'memoryPromotionAllowed',
    });
    if (sourceID != source ||
        v['schemaVersion'] != 'human-introduction-suggestions-v1' ||
        v['mode'] != 'HUMAN_REVIEW_ONLY' ||
        v['modelAccess'] != false ||
        v['sendAllowed'] != false ||
        v['memoryPromotionAllowed'] != false ||
        v['truncated'] is! bool ||
        !observedAt.isBefore(expiry)) {
      throw const FormatException('建议边界无效');
    }
    truncated = v['truncated'] as bool;
    final statuses = _object(v['sourceStatus']);
    _keys(statuses, {
      'SHARED_INTEREST',
      'SHARED_CITY',
      'SHARED_COMMUNITY',
      'SHARED_ACTIVITY',
    });
    activityAvailable =
        statuses['SHARED_ACTIVITY'] == 'PUBLIC_REGISTRATIONS_ONLY';
    if (![
          'UNAVAILABLE',
          'PUBLIC_REGISTRATIONS_ONLY',
        ].contains(statuses['SHARED_ACTIVITY']) ||
        [
          'SHARED_INTEREST',
          'SHARED_CITY',
          'SHARED_COMMUNITY',
        ].any((k) => statuses[k] != 'PUBLIC_DECLARATIONS_ONLY')) {
      throw const FormatException('来源可用性不符');
    }
    final rows = v['candidates'];
    if (rows is! List || rows.length > 50) {
      throw const FormatException('候选数量无效');
    }
    final seen = <String>{};
    candidates = List.unmodifiable(
      rows.map((r) {
        final c = IntroductionCandidate(_object(r), source, observedAt, expiry);
        if (c.basis.containsKey('SHARED_ACTIVITY') && !activityAvailable) {
          throw const FormatException('公开报名来源不可用');
        }
        if (c.accountID == owner || !seen.add(c.accountID)) {
          throw const FormatException('候选主体无效');
        }
        return c;
      }),
    );
  }
  final String sourceID, explanation;
  final DateTime observedAt, expiry;
  late final bool truncated, activityAvailable;
  late final List<IntroductionCandidate> candidates;
}

class IntroductionFailure implements Exception {
  IntroductionFailure(this.status);
  final int status;
}

class AgentIntroductionAPI {
  AgentIntroductionAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _ownsClient = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _ownsClient;
  final String _base;
  Future<dynamic> _call(
    String token,
    String path, {
    Map<String, dynamic>? body,
    String? source,
  }) async {
    if (token.isEmpty || _base.isEmpty) throw const FormatException('请登录并连接服务');
    var u = Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/$path');
    if (source != null) {
      u = u.replace(queryParameters: {'sourceIntentId': _id(source)});
    }
    final headers = {
      'Authorization': token,
      if (body != null) 'Content-Type': 'application/json',
    };
    final response =
        await (body == null
                ? _client.get(u, headers: headers)
                : _client.put(u, headers: headers, body: jsonEncode(body)))
            .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw IntroductionFailure(response.statusCode);
    }
    final e = _object(jsonDecode(utf8.decode(response.bodyBytes)));
    if (!e.containsKey('data')) throw const FormatException('记录缺失');
    return e['data'];
  }

  Future<bool> consent(String token) async {
    final d = _object(await _call(token, 'new-people/consent'));
    _keys(d, {'enabled'});
    if (d['enabled'] is! bool) throw const FormatException('找朋友设置无效');
    return d['enabled'] as bool;
  }

  Future<List<IntroductionIntent>> intents(String token, String owner) async {
    final rows = await _call(token, 'new-people/intents');
    if (rows is! List || rows.length > 100) {
      throw const FormatException('意图列表无效');
    }
    final seen = <String>{};
    return List.unmodifiable(
      rows.map((r) {
        final i = IntroductionIntent(_object(r), owner);
        if (!seen.add(i.id)) throw const FormatException('意图重复');
        return i;
      }),
    );
  }

  Future<IntroductionPolicy> policy(String token, String owner) async =>
      IntroductionPolicy(_object(await _call(token, 'agent-policies')), owner);
  Future<IntroductionResult> suggestions(
    String token,
    String owner,
    String source,
  ) async => IntroductionResult(
    _object(await _call(token, 'agent-introductions', source: source)),
    source,
    owner,
  );
  Future<IntroductionPolicy> saveSocial(
    String token,
    IntroductionPolicy original,
    Map<String, String> rules,
    DateTime expiry,
  ) async {
    if (rules.length != 7 ||
        !rules.keys.toSet().containsAll(introductionCategories) ||
        rules.values.any((v) => !['DISABLED', 'REVIEW_REQUIRED'].contains(v)) ||
        introductionCategories
            .where(
              (k) => ![
                'UNKNOWN_PERSON',
                'SHARED_COMMUNITY',
                'SHARED_ACTIVITY',
              ].contains(k),
            )
            .any((k) => rules[k] != original.rules[k])) {
      throw const FormatException('只能修改本次展示的社交偏好');
    }
    final saved = IntroductionPolicy(
      _object(
        await _call(
          token,
          'agent-policies/social',
          body: {
            'expectedVersion': original.revision,
            'settings': {
              'rules': [
                for (final k in introductionCategories)
                  {'category': k, 'preference': rules[k]},
              ],
            },
            'expiresAt': expiry.toUtc().toIso8601String(),
          },
        ),
      ),
      original.ownerID,
    );
    if (saved.agentID != original.agentID ||
        saved.revision != original.revision + 1 ||
        saved.status != 'ACTIVE' ||
        saved.expiry != expiry ||
        introductionCategories.any((k) => saved.rules[k] != rules[k])) {
      throw const FormatException('保存结果尚未核实');
    }
    return saved;
  }

  void dispose() {
    if (_ownsClient) _client.close();
  }
}
