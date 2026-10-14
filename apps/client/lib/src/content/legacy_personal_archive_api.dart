import 'dart:convert';
import 'dart:typed_data';
import 'package:http/http.dart' as http;
import '../auth/session_vault.dart';
import 'legacy_archive_api.dart';

const personalArchiveKinds = {'journey', 'theme_map', 'saved_place'};
Map<String, dynamic> _objectClosed(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      !keys.every(raw.containsKey)) {
    throw const FormatException('旧资料档案无法完整核对。');
  }
  return raw;
}

DateTime _time(Object? raw) {
  if (raw is! String || !RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(raw))
    throw const FormatException('旧资料日期不完整。');
  final t = DateTime.parse(raw).toUtc();
  if (t.year < 1 || t.year > 9999) throw const FormatException('旧资料日期不完整。');
  return t;
}

class LegacyPersonalSummary {
  const LegacyPersonalSummary({
    required this.id,
    required this.kind,
    required this.title,
    required this.deleted,
  });
  final String id, kind, title;
  final bool deleted;
  factory LegacyPersonalSummary.decode(Object? raw, {bool detail = false}) {
    final m = _objectClosed(raw, {
      'id',
      'kind',
      'sourceKey',
      'sourceRevision',
      'sourceSha256',
      'deleted',
      'sourceDeletedAt',
      'title',
      'importedAt',
      if (detail) 'payload',
    });
    final kind = m['kind'],
        key = m['sourceKey'],
        revision = m['sourceRevision'];
    if (!legacyArchiveID(m['id']) ||
        !personalArchiveKinds.contains(kind) ||
        key is! String ||
        (kind == 'saved_place'
            ? key.split(':').length != 2 ||
                  !key.split(':').every(legacyArchiveID) ||
                  revision != null
            : !legacyArchiveID(key) ||
                  revision is! int ||
                  revision < 1 ||
                  revision > legacyArchiveMaxVersion) ||
        m['sourceSha256'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(m['sourceSha256']) ||
        m['title'] is! String ||
        (m['title'] as String).contains('\u0000') ||
        m['deleted'] is! bool ||
        m['deleted'] == true &&
            (kind == 'saved_place' ||
                m['title'] != '' ||
                m['sourceDeletedAt'] == null ||
                detail && m['payload'] != null) ||
        m['deleted'] == false && m['sourceDeletedAt'] != null)
      throw const FormatException('旧资料归属或状态不符。');
    _time(m['importedAt']);
    if (m['sourceDeletedAt'] != null) _time(m['sourceDeletedAt']);
    return LegacyPersonalSummary(
      id: m['id'],
      kind: kind,
      title: m['title'],
      deleted: m['deleted'],
    );
  }
}

class LegacyPersonalDetail {
  const LegacyPersonalDetail(this.summary, this.payload);
  final LegacyPersonalSummary summary;
  final Map<String, dynamic>? payload;
  factory LegacyPersonalDetail.decode(Object? raw) {
    final s = LegacyPersonalSummary.decode(raw, detail: true);
    final m = raw as Map<String, dynamic>;
    if (s.deleted) return LegacyPersonalDetail(s, null);
    final keys = s.kind == 'journey'
        ? {
            'metadata',
            'stops',
            'arrangements',
            'notes',
            'members',
            'media',
            'publications',
            'publicationPlaces',
          }
        : s.kind == 'theme_map'
        ? {'metadata', 'entries', 'entrySources'}
        : {'sourcePlaceId', 'sourceCreatedAt', 'sources'};
    final p = _objectClosed(m['payload'], keys);
    if (s.kind == 'saved_place') {
      if (!legacyArchiveID(p['sourcePlaceId']))
        throw const FormatException('旧收藏来源不符。');
      _time(p['sourceCreatedAt']);
    } else if (p['metadata'] is! Map<String, dynamic>)
      throw const FormatException('缺少旧集合信息。');
    for (final key in keys.where(
      (k) => !{'metadata', 'sourcePlaceId', 'sourceCreatedAt'}.contains(k),
    )) {
      final rows = p[key];
      if (rows is! List ||
          rows.length > 100000 ||
          rows.any((v) => v is! Map<String, dynamic>))
        throw const FormatException('旧集合关系不完整。');
      if (key == 'media' && rows.any((v) => v['state'] != 'UNAVAILABLE'))
        throw const FormatException('旧媒体许可尚未核实。');
    }
    return LegacyPersonalDetail(s, Map.unmodifiable(p));
  }
}

class LegacyPersonalPage {
  const LegacyPersonalPage(this.items, this.next);
  final List<LegacyPersonalSummary> items;
  final String? next;
  factory LegacyPersonalPage.decode(
    Object? raw,
    String kind,
    String? previous,
  ) {
    if (raw is! Map<String, dynamic> ||
        raw.keys.any((k) => !{'items', 'nextCursor'}.contains(k)) ||
        raw['items'] is! List ||
        (raw['items'] as List).length > 20)
      throw const FormatException('旧资料分页不符。');
    final items = [
      for (final row in raw['items']) LegacyPersonalSummary.decode(row),
    ];
    final next = raw['nextCursor'];
    if (items.any((v) => v.kind != kind) ||
        items.map((v) => v.id).toSet().length != items.length ||
        next != null &&
            (next is! String ||
                next.isEmpty ||
                next.length > 1024 ||
                next == previous ||
                items.isEmpty))
      throw const FormatException('旧资料分页重复或来源已变化。');
    return LegacyPersonalPage(List.unmodifiable(items), next);
  }
}

class LegacyPersonalArchiveAPI {
  LegacyPersonalArchiveAPI({
    required this.authorizationHeader,
    required this.ownerID,
    required this.current,
    required String baseUrl,
    http.Client? client,
  }) : _client = client ?? http.Client(),
       _owns = client == null,
       origin = canonicalAuthOrigin(baseUrl);
  final String? Function() authorizationHeader, ownerID;
  final bool Function() current;
  final String origin;
  final http.Client _client;
  final bool _owns;
  bool _closed = false;
  Future<Object?> _get(String path, {Map<String, String>? query}) async {
    final owner = ownerID(), header = authorizationHeader();
    bool valid() =>
        !_closed &&
        current() &&
        owner == ownerID() &&
        header == authorizationHeader();
    if (!valid() || !legacyArchiveID(owner) || header == null)
      throw LegacyArchiveSourceChanged();
    final r = await _client
        .send(
          http.Request(
            'GET',
            Uri.parse('$origin$path').replace(queryParameters: query),
          )..headers['Authorization'] = header,
        )
        .timeout(const Duration(seconds: 12));
    if (!valid()) throw LegacyArchiveSourceChanged();
    if (r.statusCode != 200) throw LegacyArchiveFailure(r.statusCode);
    final b = BytesBuilder(copy: false);
    await for (final chunk in r.stream.timeout(const Duration(seconds: 12))) {
      if (!valid()) throw LegacyArchiveSourceChanged();
      if (chunk.length > legacyArchiveResponseMaxBytes - b.length)
        throw const FormatException('旧资料过大。');
      b.add(chunk);
    }
    if (!valid()) throw LegacyArchiveSourceChanged();
    return _closedRoot(legacyArchiveJSON(utf8.decode(b.takeBytes())))['data'];
  }

  Map<String, dynamic> _closedRoot(Object? v) => _objectClosed(v, {'data'});
  Future<LegacyPersonalPage> page(String kind, {String? cursor}) async {
    if (!personalArchiveKinds.contains(kind))
      throw const FormatException('旧资料类型不符。');
    return LegacyPersonalPage.decode(
      await _get(
        '/v1/me/migration-archive-records',
        query: {'kind': kind, 'cursor': ?cursor},
      ),
      kind,
      cursor,
    );
  }

  Future<LegacyPersonalDetail> detail(String id) async {
    if (!legacyArchiveID(id)) throw const FormatException('旧资料编号不符。');
    final v = LegacyPersonalDetail.decode(
      await _get('/v1/me/migration-archive-records/$id'),
    );
    if (v.summary.id != id) throw const FormatException('旧资料编号不符。');
    return v;
  }

  void dispose() {
    _closed = true;
    if (_owns) _client.close();
  }
}
