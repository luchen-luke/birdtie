import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

class PlaceHistoryMoment {
  const PlaceHistoryMoment(
    this.title,
    this.excerpt,
    this.publishedAt, {
    this.id,
    this.revision,
  });
  final String title, excerpt;
  final DateTime publishedAt;
  final String? id;
  final int? revision;
}

class PlaceHistoryPattern {
  const PlaceHistoryPattern(this.category, this.weekend, this.arrangements);
  final String category;
  final bool weekend;
  final int arrangements;
}

class PlaceHistorySummary {
  const PlaceHistorySummary({
    required this.placeID,
    required this.checkedAt,
    required this.count,
    required this.moments,
    required this.patterns,
    required this.facts,
    required this.sourceLabel,
    required this.confidence,
  });
  final String placeID;
  final DateTime checkedAt;
  final int count;
  final List<PlaceHistoryMoment> moments;
  final List<PlaceHistoryPattern> patterns;
  final Map<String, dynamic>? facts;
  final String? sourceLabel, confidence;
  static final _uuid = RegExp(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
  );
  static DateTime _date(dynamic v) {
    if (v is! String || !RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(v)) {
      throw const FormatException();
    }
    return DateTime.parse(v).toUtc();
  }

  static void _keys(Map<String, dynamic> d, Set<String> keys) {
    if (d.length != keys.length || d.keys.toSet().difference(keys).isNotEmpty) {
      throw const FormatException();
    }
  }

  factory PlaceHistorySummary.fromJson(
    Map<String, dynamic> d,
    String expected,
  ) {
    _keys(d, {
      'schemaVersion',
      'placeId',
      'cityId',
      'windowDays',
      'windowStart',
      'checkedAt',
      'recentMomentCount',
      'recentMoments',
      'activityPatterns',
      'suitability',
    });
    if (d['schemaVersion'] != 'place-social-history-v1' ||
        d['placeId'] != expected ||
        !_uuid.hasMatch(expected) ||
        d['cityId'] is! String ||
        d['cityId'].isEmpty ||
        d['windowDays'] != 30 ||
        d['recentMomentCount'] is! int ||
        d['recentMomentCount'] < 0) {
      throw const FormatException();
    }
    final now = _date(d['checkedAt']), start = _date(d['windowStart']);
    if (now.difference(start) != const Duration(days: 30)) {
      throw const FormatException();
    }
    final rows = d['recentMoments'] as List<dynamic>,
        buckets = d['activityPatterns'] as List<dynamic>;
    if (rows.length > 5 ||
        rows.length > d['recentMomentCount'] ||
        buckets.length > 200) {
      throw const FormatException();
    }
    final ids = <String>{}, moments = <PlaceHistoryMoment>[];
    for (final row in rows) {
      final r = row as Map<String, dynamic>;
      _keys(r, {'id', 'title', 'excerpt', 'revision', 'publishedAt'});
      if (r['id'] is! String ||
          !_uuid.hasMatch(r['id']) ||
          !ids.add(r['id']) ||
          r['title'] is! String ||
          r['title'].isEmpty ||
          utf8.encode(r['title']).length > 160 ||
          r['excerpt'] is! String ||
          (r['excerpt'] as String).runes.length > 280 ||
          r['revision'] is! int ||
          r['revision'] < 2) {
        throw const FormatException();
      }
      final at = _date(r['publishedAt']);
      if (at.isBefore(start) ||
          at.isAfter(now) ||
          (moments.isNotEmpty && at.isAfter(moments.last.publishedAt))) {
        throw const FormatException();
      }
      moments.add(
        PlaceHistoryMoment(
          r['title'],
          r['excerpt'],
          at,
          id: r['id'],
          revision: r['revision'],
        ),
      );
    }
    final patterns = <PlaceHistoryPattern>[], seen = <String>{};
    for (final raw in buckets) {
      final r = raw as Map<String, dynamic>;
      _keys(r, {'category', 'dayKind', 'arrangements'});
      if (r['category'] is! String ||
          (r['category'] as String).length > 80 ||
          !{'WEEKEND', 'WEEKDAY'}.contains(r['dayKind']) ||
          r['arrangements'] is! int ||
          r['arrangements'] < 1 ||
          r['arrangements'] > 1000000 ||
          !seen.add('${r['category']}:${r['dayKind']}')) {
        throw const FormatException();
      }
      patterns.add(
        PlaceHistoryPattern(
          r['category'],
          r['dayKind'] == 'WEEKEND',
          r['arrangements'],
        ),
      );
    }
    Map<String, dynamic>? facts;
    String? label, confidence;
    if (d['suitability'] != null) {
      final p = d['suitability'] as Map<String, dynamic>;
      _keys(p, {
        'schemaVersion',
        'placeId',
        'cityId',
        'version',
        'facts',
        'source',
        'confidence',
        'checkedAt',
      });
      if (p['schemaVersion'] != 'place-semantic-v1' ||
          p['placeId'] != expected ||
          p['cityId'] != d['cityId'] ||
          p['version'] is! int ||
          p['version'] < 1 ||
          _date(p['checkedAt']) != now) {
        throw const FormatException();
      }
      final source = p['source'] as Map<String, dynamic>,
          assessment = p['confidence'] as Map<String, dynamic>;
      _keys(source, {'label', 'url', 'observedAt', 'reviewedAt', 'expiresAt'});
      _keys(assessment, {'kind', 'level'});
      final url = Uri.parse(source['url'] as String),
          observed = _date(source['observedAt']),
          reviewed = _date(source['reviewedAt']),
          expiry = _date(source['expiresAt']);
      if (source['label'] is! String ||
          source['label'].isEmpty ||
          url.scheme != 'https' ||
          url.host.isEmpty ||
          url.userInfo.isNotEmpty ||
          url.fragment.isNotEmpty ||
          observed.isAfter(reviewed) ||
          reviewed.isAfter(now) ||
          !expiry.isAfter(now) ||
          assessment['kind'] != 'EDITOR_ASSESSMENT_UNCALIBRATED' ||
          !{'LOW', 'MEDIUM', 'HIGH'}.contains(assessment['level'])) {
        throw const FormatException();
      }
      facts = p['facts'] as Map<String, dynamic>;
      _keys(facts, {
        'vibe',
        'good_for',
        'price',
        'accessibility',
        'group_size',
        'reservation',
        'suitability',
      });
      for (final k in ['vibe', 'good_for', 'suitability']) {
        final values = facts[k];
        if (values != null &&
            (values is! List ||
                values.length > 16 ||
                values.toSet().length != values.length ||
                values.any(
                  (v) =>
                      v is! String ||
                      !RegExp(r'^[a-z][a-z0-9_]{1,39}$').hasMatch(v),
                ))) {
          throw const FormatException();
        }
      }
      if (!facts.values.any((v) => v != null && (v is! List || v.isNotEmpty))) {
        throw const FormatException();
      }
      if (facts['price'] != null) {
        final v = facts['price'] as Map<String, dynamic>;
        _keys(v, {'currency', 'minMinor', 'maxMinor', 'unit'});
        if (v['currency'] is! String ||
            !RegExp(r'^[A-Z]{3}$').hasMatch(v['currency']) ||
            v['minMinor'] is! int ||
            v['maxMinor'] is! int ||
            v['minMinor'] < 0 ||
            v['maxMinor'] < v['minMinor'] ||
            v['maxMinor'] > 100000000 ||
            !{'per_person', 'per_visit', 'per_hour'}.contains(v['unit'])) {
          throw const FormatException();
        }
      }
      if (facts['accessibility'] != null) {
        final v = facts['accessibility'] as Map<String, dynamic>;
        _keys(v, {'stepFree', 'accessibleToilet'});
        if (!{'yes', 'no', 'unknown'}.contains(v['stepFree']) ||
            !{'yes', 'no', 'unknown'}.contains(v['accessibleToilet']) ||
            (v['stepFree'] == 'unknown' &&
                v['accessibleToilet'] == 'unknown')) {
          throw const FormatException();
        }
      }
      if (facts['group_size'] != null) {
        final v = facts['group_size'] as Map<String, dynamic>;
        _keys(v, {'min', 'max'});
        if (v['min'] is! int ||
            v['max'] is! int ||
            v['min'] < 1 ||
            v['max'] < v['min'] ||
            v['max'] > 1000) {
          throw const FormatException();
        }
      }
      if (facts['reservation'] != null) {
        final v = facts['reservation'] as Map<String, dynamic>;
        if (v.keys.toSet().difference({'support', 'url'}).isNotEmpty ||
            !{'none', 'contact', 'external_url'}.contains(v['support'])) {
          throw const FormatException();
        }
        if (v['support'] == 'external_url') {
          final link = Uri.parse(v['url'] as String);
          if (link.scheme != 'https' ||
              link.host.isEmpty ||
              link.userInfo.isNotEmpty ||
              link.fragment.isNotEmpty) {
            throw const FormatException();
          }
        } else if (v['url'] != null) {
          throw const FormatException();
        }
      }
      label = source['label'];
      confidence = assessment['level'];
      facts = Map.unmodifiable(facts);
    }
    return PlaceHistorySummary(
      placeID: expected,
      checkedAt: now,
      count: d['recentMomentCount'],
      moments: List.unmodifiable(moments),
      patterns: List.unmodifiable(patterns),
      facts: facts,
      sourceLabel: label,
      confidence: confidence,
    );
  }
}

class PlaceHistoryController extends ChangeNotifier {
  PlaceHistoryController({
    required this.placeID,
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final String placeID, _base;
  final String? Function() authorizationHeader;
  final http.Client _client;
  final bool _ownsClient;
  bool _closed = false, _loading = false;
  int _serial = 0, _epoch = 0;
  String? _actor, _error;
  PlaceHistorySummary? _summary;
  String? _sync() {
    final token = authorizationHeader();
    if (token != _actor) {
      _actor = token;
      _serial++;
      _epoch++;
      _summary = null;
      _loading = false;
      _error = null;
    }
    return token;
  }

  PlaceHistorySummary? get summary {
    _sync();
    return _summary;
  }

  bool get loading {
    _sync();
    return _loading;
  }

  String? get error {
    _sync();
    return _error;
  }

  void invalidate({bool notify = true}) {
    _serial++;
    _epoch++;
    _summary = null;
    _loading = false;
    _error = null;
    if (!_closed && notify) notifyListeners();
  }

  bool _current(String? t, int e, int r) =>
      !_closed && _sync() == t && e == _epoch && r == _serial;
  Future<void> refresh() async {
    if (_closed) return;
    final token = _sync(), epoch = _epoch, serial = ++_serial;
    _summary = null;
    _error = null;
    _loading = true;
    notifyListeners();
    try {
      final response = await _client
          .get(
            Uri.parse(
              '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/places/${Uri.encodeComponent(placeID)}/social-history',
            ),
            headers: token == null ? {} : {'Authorization': token},
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw const FormatException();
      final raw = jsonDecode(utf8.decode(response.bodyBytes));
      if (raw is! Map<String, dynamic> ||
          raw.length != 1 ||
          raw['data'] is! Map<String, dynamic>) {
        throw const FormatException();
      }
      final next = PlaceHistorySummary.fromJson(raw['data'], placeID);
      if (_current(token, epoch, serial)) _summary = next;
    } catch (_) {
      if (_current(token, epoch, serial)) _error = '公开摘要暂不可用，请重试。';
    } finally {
      if (_current(token, epoch, serial)) {
        _loading = false;
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    _serial++;
    _summary = null;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
