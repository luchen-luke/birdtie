import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;
import 'social_intent_draft_model.dart';

const socialIntentCreationSchema = 'social-intent-creations-v1';
String intentCreationHash(String value) =>
    sha256.convert(utf8.encode(value)).toString();
String intentCreationEnvironment(String base) {
  final u = Uri.parse(base);
  if (!u.hasScheme ||
      !{'https', 'http'}.contains(u.scheme) ||
      u.host.isEmpty ||
      u.userInfo.isNotEmpty ||
      u.hasQuery ||
      u.hasFragment) {
    throw const FormatException('意图服务地址无法核实');
  }
  return u.replace(path: u.path.replaceFirst(RegExp(r'/+$'), '')).toString();
}

String _trim(String v) => utf8
    .decode(utf8.encode(v))
    .replaceFirst(
      RegExp(
        r'^[\t-\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+',
      ),
      '',
    )
    .replaceFirst(
      RegExp(
        r'[\t-\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$',
      ),
      '',
    );
DateTime _time(Object? v) {
  if (v is! String) throw const FormatException('核实时间不符');
  final t = DateTime.tryParse(v);
  if (t == null ||
      t.year < 2000 ||
      t.year > 2200 ||
      !RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(v)) {
    throw const FormatException('核实时间不符');
  }
  return t.toUtc();
}

Map<String, dynamic> intentCreationDraft(
  Map<String, dynamic> input,
  String source,
) {
  const keys = {
    'type',
    'title',
    'audience',
    'modality',
    'constraints',
    'expiresAt',
    'contextId',
  };
  if (input.keys.any((k) => !keys.contains(k)) ||
      input['title'] is! String ||
      input['audience'] != 'PRIVATE' ||
      !socialIntentModes.contains(input['modality']) ||
      !{
        'FIND_ACTIVITY',
        'FIND_COMPANION',
        'ORGANIZE',
        'ASK_HELP',
        'OTHER',
      }.contains(input['type']) ||
      (source.isNotEmpty &&
          (!socialIntentIDValid(source) || input['type'] != 'FIND_ACTIVITY'))) {
    throw const FormatException('私人草稿内容不符');
  }
  final title = _trim(input['title'] as String),
      context = input['contextId'] ?? '';
  if (title.isEmpty ||
      title.runes.length > 160 ||
      (context != '' && !socialIntentIDValid(context))) {
    throw const FormatException('私人草稿内容不符');
  }
  final raw = input['constraints'];
  if (raw is! Map<String, dynamic> ||
      raw.keys.any(
        (k) => !{
          'category',
          'areaLabel',
          'onlinePlatform',
          'placeId',
          'minParticipants',
          'maxParticipants',
          'startsAt',
          'endsAt',
        }.contains(k),
      )) {
    throw const FormatException('私人草稿条件不符');
  }
  final c = <String, dynamic>{};
  for (final key in ['category', 'areaLabel', 'onlinePlatform', 'placeId']) {
    if (raw[key] == null) continue;
    if (raw[key] is! String) throw const FormatException('私人草稿条件不符');
    final value = _trim(raw[key] as String);
    if (value.runes.length > (key == 'areaLabel' ? 160 : 80) ||
        (key == 'placeId' && value.isNotEmpty && !socialIntentIDValid(value))) {
      throw const FormatException('私人草稿条件不符');
    }
    if (value.isNotEmpty) {
      c[key] = key == 'placeId' ? value.toLowerCase() : value;
    }
  }
  for (final key in ['minParticipants', 'maxParticipants']) {
    final value = raw[key];
    if (value == null) continue;
    if (value is! int || value < 0 || value > 100) {
      throw const FormatException('参与人数不符');
    }
    if (value != 0) c[key] = value;
  }
  if (c['minParticipants'] != null &&
      c['maxParticipants'] != null &&
      (c['minParticipants'] as int) > (c['maxParticipants'] as int)) {
    throw const FormatException('参与人数不符');
  }
  if ((raw['startsAt'] == null) != (raw['endsAt'] == null)) {
    throw const FormatException('活动时间不完整');
  }
  if (raw['startsAt'] != null) {
    final start = _time(raw['startsAt']), end = _time(raw['endsAt']);
    if (!end.isAfter(start) ||
        end.difference(start) > const Duration(days: 90)) {
      throw const FormatException('活动时间不符');
    }
    c['startsAt'] = start.toIso8601String();
    c['endsAt'] = end.toIso8601String();
  }
  final physical = c['areaLabel'] != null || c['placeId'] != null,
      mode = input['modality'];
  if ((mode == 'ONLINE' && physical) ||
      (mode == 'IN_PERSON' && (!physical || c['onlinePlatform'] != null)) ||
      (mode == 'HYBRID' && (!physical || c['onlinePlatform'] == null))) {
    throw const FormatException('参与方式与条件不符');
  }
  return {
    'type': input['type'],
    'title': title,
    'audience': 'PRIVATE',
    'modality': mode,
    'constraints': c,
    'expiresAt': _time(input['expiresAt']).toIso8601String(),
    if (context != '') 'contextId': (context as String).toLowerCase(),
  };
}

Object? _sort(Object? value) => value is Map<String, dynamic>
    ? {for (final key in value.keys.toList()..sort()) key: _sort(value[key])}
    : value is List
    ? value.map(_sort).toList()
    : value;
String intentCreationDigest(
  String owner,
  String source,
  Map<String, dynamic> draft,
) {
  if (!socialIntentIDValid(owner)) throw const FormatException('本人账号不符');
  final n = intentCreationDraft(draft, source),
      c = Map<String, dynamic>.from(n['constraints'] as Map);
  for (final key in ['startsAt', 'endsAt']) {
    if (c[key] != null) {
      c['${key}Micros'] = _time(
        c.remove(key),
      ).microsecondsSinceEpoch.toString();
    }
  }
  final content = _sort({
    'ownerAccountId': owner.toLowerCase(),
    'sourceTaskId': source.toLowerCase(),
    'draft': {
      'type': n['type'],
      'title': n['title'],
      'audience': n['audience'],
      'modality': n['modality'],
      'contextId': n['contextId'] ?? '',
      'expiresAtMicros': _time(
        n['expiresAt'],
      ).microsecondsSinceEpoch.toString(),
      'constraints': c,
    },
  });
  // Go's canonical JSON uses literal HTML characters but escapes these two
  // JavaScript separators. Keep the exact same UTF-8 digest in both clients.
  return intentCreationHash(
    jsonEncode(
      content,
    ).replaceAll('\u2028', r'\u2028').replaceAll('\u2029', r'\u2029'),
  );
}

class SocialIntentCreationReceipt {
  SocialIntentCreationReceipt._(this.data);
  final Map<String, dynamic> data;
  String get ownerID => data['ownerAccountId'] as String;
  String get id => (data['intentId'] ?? data['priorIntentId']) as String;
  String get status => data['status'] as String;
  bool get committed => status == 'COMMITTED';
  String? get reason => data['reason'] as String?;
  Map<String, dynamic>? get intent => data['intent'] as Map<String, dynamic>?;
  factory SocialIntentCreationReceipt.decode(
    String body, {
    required String owner,
    required String operation,
    required String digest,
    required String source,
  }) {
    final envelope = jsonDecode(body);
    if (envelope is! Map<String, dynamic> ||
        envelope['data'] is! Map<String, dynamic>) {
      throw const FormatException('无法核实原保存回执');
    }
    final v = envelope['data'] as Map<String, dynamic>;
    if (v['schemaVersion'] != socialIntentCreationSchema ||
        v['ownerAccountId'] != owner ||
        v['operationId'] != operation ||
        v['requestDigest'] != digest ||
        (v['sourceTaskId'] ?? '') != source) {
      throw const FormatException('原操作、内容或归属无法核实');
    }
    _time(v['recordedAt']);
    final committed = v['status'] == 'COMMITTED',
        prior =
            v['status'] == 'NO_EFFECT' &&
            v['reason'] == 'SOURCE_ALREADY_EXISTS';
    if (committed || prior) {
      final id = v[committed ? 'intentId' : 'priorIntentId'],
          item = v['intent'];
      if (!socialIntentIDValid(id) ||
          item is! Map<String, dynamic> ||
          item['id'] != id ||
          item['creatorAccountId'] != owner ||
          item['title'] is! String ||
          !socialIntentModes.contains(item['modality']) ||
          !{
            'PRIVATE',
            'FRIENDS',
            'PUBLIC',
            'LOCAL',
            'COMMUNITY',
            'INVITE_ONLY',
          }.contains(item['audience']) ||
          !{
            'DRAFT',
            'ACTIVE',
            'MATCHED',
            'CONVERTED',
            'EXPIRED',
            'CANCELLED',
          }.contains(item['status']) ||
          (committed && (v['priorIntentId'] != null || v['reason'] != null)) ||
          (prior && (source.isEmpty || v['intentId'] != null))) {
        throw const FormatException('保存对象或当前状态无法核实');
      }
      _time(item['expiresAt']);
      _time(item['createdAt']);
      _time(item['updatedAt']);
    } else if (v['status'] != 'NO_EFFECT' ||
        !{
          'EXPIRED',
          'SOURCE_UNAVAILABLE',
          'SOURCE_CHANGED',
        }.contains(v['reason']) ||
        v['intentId'] != null ||
        v['priorIntentId'] != null ||
        v['intent'] != null) {
      throw const FormatException('保存结果无法核实');
    }
    return SocialIntentCreationReceipt._(Map.unmodifiable(v));
  }
}

class SocialIntentCreationApiFailure implements Exception {
  const SocialIntentCreationApiFailure(this.status);
  final int status;
}

/// Owner/source correlation for a legacy draft. It deliberately has no
/// creation operation/digest and cannot confirm a later request was saved.
class SocialIntentTaskDraftReceipt {
  SocialIntentTaskDraftReceipt._(this.data);
  final Map<String, dynamic> data;
  String get id => data['intentId'] as String;
  factory SocialIntentTaskDraftReceipt.decode(
    String body, {
    required String owner,
    required String source,
  }) {
    final envelope = jsonDecode(body);
    if (envelope is! Map<String, dynamic> ||
        envelope['data'] is! Map<String, dynamic>) {
      throw const FormatException('原来源对象无法核实');
    }
    final v = envelope['data'] as Map<String, dynamic>, item = v['intent'];
    if (v['schemaVersion'] != socialIntentCreationSchema ||
        v['ownerAccountId'] != owner ||
        v['sourceTaskId'] != source ||
        !socialIntentIDValid(v['intentId']) ||
        item is! Map<String, dynamic> ||
        item['id'] != v['intentId'] ||
        item['creatorAccountId'] != owner ||
        v.containsKey('operationId') ||
        v.containsKey('requestDigest')) {
      throw const FormatException('原来源对象归属无法核实');
    }
    return SocialIntentTaskDraftReceipt._(Map.unmodifiable(v));
  }
}

class SocialIntentCreationAPI {
  SocialIntentCreationAPI({
    required this.client,
    required String base,
    required String token,
  }) : base = intentCreationEnvironment(base),
       headers = Map.unmodifiable({
         'Authorization': token,
         'Content-Type': 'application/json',
       });
  final http.Client client;
  final String base;
  final Map<String, String> headers;
  Future<SocialIntentTaskDraftReceipt?> readTask({
    required String owner,
    required String source,
  }) async {
    if (!socialIntentIDValid(owner) || !socialIntentIDValid(source)) {
      throw const FormatException('原来源对象无法核实');
    }
    final r = await client
        .get(
          Uri.parse('$base/v1/me/agent-tasks/$source/social-intent-draft'),
          headers: headers,
        )
        .timeout(const Duration(seconds: 12));
    if (r.statusCode == 404) return null;
    if (r.statusCode != 200) throw SocialIntentCreationApiFailure(r.statusCode);
    return SocialIntentTaskDraftReceipt.decode(
      utf8.decode(r.bodyBytes),
      owner: owner,
      source: source,
    );
  }

  Future<SocialIntentCreationReceipt?> read({
    required String owner,
    required String operation,
    required String digest,
    required String source,
  }) async {
    final r = await client
        .get(
          Uri.parse('$base/v1/me/social-intent-creations/$operation'),
          headers: headers,
        )
        .timeout(const Duration(seconds: 12));
    if (r.statusCode == 404) return null;
    if (r.statusCode != 200) throw SocialIntentCreationApiFailure(r.statusCode);
    return SocialIntentCreationReceipt.decode(
      utf8.decode(r.bodyBytes),
      owner: owner,
      operation: operation,
      digest: digest,
      source: source,
    );
  }

  Future<SocialIntentCreationReceipt> create({
    required String owner,
    required String operation,
    required String digest,
    required String source,
    required Map<String, dynamic> draft,
  }) async {
    final captured = {...draft, 'operationId': operation};
    final body = jsonEncode(
      source.isEmpty ? captured : {'confirmed': true, 'draft': captured},
    );
    final path = source.isEmpty
        ? '/v1/me/social-intents'
        : '/v1/me/agent-tasks/$source/social-intent-drafts';
    final r = await client
        .post(Uri.parse('$base$path'), headers: headers, body: body)
        .timeout(const Duration(seconds: 12));
    if (!{200, 201, 409}.contains(r.statusCode)) {
      throw SocialIntentCreationApiFailure(r.statusCode);
    }
    final result = SocialIntentCreationReceipt.decode(
      utf8.decode(r.bodyBytes),
      owner: owner,
      operation: operation,
      digest: digest,
      source: source,
    );
    if (r.statusCode == 201) {
      final item = result.intent;
      if (!result.committed ||
          item == null ||
          item['audience'] != 'PRIVATE' ||
          item['status'] != 'DRAFT') {
        throw const FormatException('首次保存对象无法核实');
      }
      final echo = {
        for (final k in [
          'type',
          'title',
          'audience',
          'modality',
          'constraints',
          'expiresAt',
          'contextId',
        ])
          if (item[k] != null) k: item[k],
      };
      if (intentCreationDigest(owner, source, echo) != digest) {
        throw const FormatException('首次保存内容无法核实');
      }
    }
    if (r.statusCode == 409 && result.reason != 'SOURCE_ALREADY_EXISTS') {
      throw const FormatException('原来源回执无法核实');
    }
    return result;
  }
}
