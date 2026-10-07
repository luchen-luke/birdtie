import 'dart:convert';

/// These are the native human management rules, not a model prompt surface.
const businessKnowledgeQuestions = [
  '商家介绍',
  '商家简介',
  '营业时间',
  '官方网站',
  '场地适用场景',
  '场地预约链接',
];
bool businessKnowledgeNeedsPlace(String query) =>
    query == '场地适用场景' || query == '场地预约链接';
bool _id(dynamic v) =>
    v is String &&
    RegExp(
      r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
    ).hasMatch(v) &&
    v != '00000000-0000-0000-0000-000000000000';
Never _bad() => throw const FormatException('商家回答格式无效');
Map<String, dynamic> _shape(dynamic v, Set<String> keys) {
  if (v is! Map<String, dynamic> ||
      v.keys.toSet().difference(keys).isNotEmpty ||
      keys.difference(v.keys.toSet()).isNotEmpty) {
    _bad();
  }
  return v;
}

DateTime businessKnowledgeStamp(dynamic raw) {
  if (raw is! String) _bad();
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|([+-])(\d{2}):(\d{2}))$',
  ).firstMatch(raw);
  if (m == null || m.end != raw.length) _bad();
  final v = [for (var i = 1; i <= 6; i++) int.parse(m.group(i)!)];
  final micros = int.parse((m.group(7) ?? '').padRight(6, '0').substring(0, 6));
  final t = DateTime.utc(v[0], v[1], v[2], v[3], v[4], v[5], 0, micros);
  if (v[0] < 1 ||
      v[0] > 9999 ||
      t.year != v[0] ||
      t.month != v[1] ||
      t.day != v[2] ||
      t.hour != v[3] ||
      t.minute != v[4] ||
      t.second != v[5]) {
    _bad();
  }
  var offset = 0;
  if (m.group(8) != 'Z') {
    final hours = int.parse(m.group(10)!);
    final minutes = int.parse(m.group(11)!);
    if (hours > 23 || minutes > 59) _bad();
    offset = (hours * 60 + minutes) * (m.group(9) == '-' ? -1 : 1);
  }
  final utc = t.subtract(Duration(minutes: offset));
  if (utc.year < 1 || utc.year > 9999) _bad();
  return utc;
}

class BusinessKnowledgeSource {
  const BusinessKnowledgeSource._(
    this.type,
    this.id,
    this.version,
    this.validUntil,
  );
  final String type, id;
  final int version;
  final DateTime validUntil;
}

class BusinessKnowledgeAnswer {
  const BusinessKnowledgeAnswer._(
    this.businessID,
    this.status,
    this.text,
    this.sourceVersion,
    this.sources,
  );
  final String businessID, status, text, sourceVersion;
  final List<BusinessKnowledgeSource> sources;
  bool currentAt(DateTime at) =>
      sources.every((s) => s.validUntil.isAfter(at.toUtc()));
  factory BusinessKnowledgeAnswer.read(
    dynamic raw, {
    required String businessID,
    required String query,
    required String placeID,
    required DateTime now,
  }) {
    final m = _shape(raw, {
      'businessId',
      'mode',
      'status',
      'answer',
      'sourceVersion',
      'sources',
      'agentStatus',
      'modelStatus',
      'tools',
    });
    if (!_id(businessID) ||
        m['businessId'] != businessID ||
        !businessKnowledgeQuestions.contains(query) ||
        (businessKnowledgeNeedsPlace(query) ? !_id(placeID) : placeID != '') ||
        m['mode'] != 'HUMAN_VERIFIED_RULES' ||
        m['agentStatus'] != 'unavailable' ||
        m['modelStatus'] != 'unavailable' ||
        m['tools'] is! List ||
        (m['tools'] as List).isNotEmpty ||
        !const {'known', 'unknown'}.contains(m['status']) ||
        m['answer'] is! String ||
        (m['answer'] as String).trim().isEmpty ||
        utf8.encode(m['answer'] as String).length > 32768 ||
        m['sourceVersion'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(m['sourceVersion'] as String) ||
        m['sources'] is! List ||
        (m['sources'] as List).length > 1) {
      _bad();
    }
    final sources = <BusinessKnowledgeSource>[];
    for (final item in m['sources'] as List) {
      final s = _shape(item, {'type', 'id', 'version', 'validUntil'});
      final venue = businessKnowledgeNeedsPlace(query);
      if (s['type'] != (venue ? 'business_venue' : 'business_profile') ||
          s['id'] != (venue ? placeID : businessID) ||
          !_id(s['id']) ||
          s['version'] is! int ||
          s['version'] < 1 ||
          s['version'] > 9007199254740990) {
        _bad();
      }
      final until = businessKnowledgeStamp(s['validUntil']);
      if (!until.isAfter(now.toUtc())) _bad();
      sources.add(
        BusinessKnowledgeSource._(s['type'], s['id'], s['version'], until),
      );
    }
    if ((m['status'] == 'known') != sources.isNotEmpty) _bad();
    return BusinessKnowledgeAnswer._(
      businessID,
      m['status'],
      m['answer'],
      m['sourceVersion'],
      List.unmodifiable(sources),
    );
  }
}
