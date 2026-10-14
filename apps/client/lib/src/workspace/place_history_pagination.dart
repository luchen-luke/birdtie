import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'place_history_controller.dart';
import 'public_moment_api.dart';

class PlaceHistoryPageChanged implements Exception {
  const PlaceHistoryPageChanged();
}

class PlaceHistoryPage {
  const PlaceHistoryPage({
    required this.placeID,
    required this.cityID,
    required this.withAuthor,
    required this.windowEnd,
    required this.observedAt,
    required this.validUntil,
    required this.sourceVersion,
    required this.count,
    required this.items,
    required this.nextCursor,
  });
  final String placeID, cityID, sourceVersion;
  final bool withAuthor;
  final DateTime windowEnd, observedAt, validUntil;
  final int count;
  final List<PlaceHistoryMoment> items;
  final String? nextCursor;
  static final _id = RegExp(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
  );
  static void _keys(Map<String, dynamic> m, Set<String> keys) {
    if (m.length != keys.length || m.keys.toSet().difference(keys).isNotEmpty)
      throw const FormatException();
  }

  static DateTime _stamp(dynamic raw) {
    if (raw is! String) throw const FormatException();
    final m = RegExp(
      r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,6}))?Z$',
    ).firstMatch(raw);
    if (m == null) throw const FormatException();
    final n = [for (var i = 1; i <= 6; i++) int.parse(m.group(i)!)];
    final us = int.parse((m.group(7) ?? '').padRight(6, '0'));
    final t = DateTime.utc(
      n[0],
      n[1],
      n[2],
      n[3],
      n[4],
      n[5],
      us ~/ 1000,
      us % 1000,
    );
    if (n[0] < 1 ||
        t.year != n[0] ||
        t.month != n[1] ||
        t.day != n[2] ||
        t.hour != n[3] ||
        t.minute != n[4] ||
        t.second != n[5])
      throw const FormatException();
    return t;
  }

  factory PlaceHistoryPage.decode(
    Map<String, dynamic> d,
    String expected,
    bool author,
  ) {
    _keys(d, {
      'schemaVersion',
      'placeId',
      'cityId',
      'authorMode',
      'windowDays',
      'windowStart',
      'windowEnd',
      'observedAt',
      'validUntil',
      'sourceVersion',
      'count',
      'items',
      'nextCursor',
    });
    if (d['schemaVersion'] != 'place-social-history-page-v1' ||
        d['placeId'] != expected ||
        !_id.hasMatch(expected) ||
        d['cityId'] is! String ||
        d['cityId'].isEmpty ||
        d['cityId'].length > 80 ||
        d['authorMode'] != (author ? 'PUBLIC_PROFILE' : 'NONE') ||
        d['windowDays'] != 30 ||
        d['sourceVersion'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(d['sourceVersion']) ||
        d['count'] is! int ||
        d['count'] < 0 ||
        d['items'] is! List)
      throw const FormatException();
    final end = _stamp(d['windowEnd']),
        start = _stamp(d['windowStart']),
        at = _stamp(d['observedAt']),
        until = _stamp(d['validUntil']);
    if (end.difference(start) != const Duration(days: 30) ||
        end.isAfter(at) ||
        !until.isAfter(at) ||
        until.difference(end) > const Duration(minutes: 2))
      throw const FormatException();
    final rows = d['items'] as List,
        items = <PlaceHistoryMoment>[],
        ids = <String>{};
    if (rows.length > 5 || rows.length > d['count'])
      throw const FormatException();
    for (final raw in rows) {
      final r = raw as Map<String, dynamic>;
      _keys(r, {
        'id',
        'title',
        'excerpt',
        'revision',
        'publishedAt',
        'authorId',
        'authorPublicProfile',
      });
      if (r['id'] is! String ||
          !_id.hasMatch(r['id']) ||
          !ids.add(r['id']) ||
          r['title'] is! String ||
          r['title'].isEmpty ||
          utf8.encode(r['title']).length > 160 ||
          r['excerpt'] is! String ||
          (r['excerpt'] as String).runes.length > 280 ||
          r['revision'] is! int ||
          r['revision'] < 2)
        throw const FormatException();
      final published = _stamp(r['publishedAt']),
          profile = PublicMomentAuthorProfile.fromPublicPair(r);
      if (published.isBefore(start) ||
          published.isAfter(end) ||
          (!author && profile != null))
        throw const FormatException();
      if (items.isNotEmpty) {
        final previous = items.last;
        if (published.isAfter(previous.publishedAt) ||
            (published == previous.publishedAt &&
                (r['id'] as String).compareTo(previous.id!) >= 0))
          throw const FormatException();
      }
      items.add(
        PlaceHistoryMoment(
          r['title'],
          r['excerpt'],
          published,
          id: r['id'],
          revision: r['revision'],
          authorPublicProfile: profile,
        ),
      );
    }
    final cursor = d['nextCursor'];
    if (cursor != null &&
        (cursor is! String ||
            cursor.isEmpty ||
            cursor.length > 1024 ||
            !RegExp(r'^[A-Za-z0-9_-]+$').hasMatch(cursor) ||
            items.length != 5))
      throw const FormatException();
    return PlaceHistoryPage(
      placeID: expected,
      cityID: d['cityId'],
      withAuthor: author,
      windowEnd: end,
      observedAt: at,
      validUntil: until,
      sourceVersion: d['sourceVersion'],
      count: d['count'],
      items: List.unmodifiable(items),
      nextCursor: cursor,
    );
  }
}

class PlaceHistoryPaginationController extends ChangeNotifier {
  PlaceHistoryPaginationController({
    required this.placeID,
    required this.authorizationHeader,
    required this.identityChanges,
    this.sourceCurrent,
    this.includePublicAuthor = false,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl {
    _token = authorizationHeader();
    identityChanges.addListener(_sourceChanged);
  }
  final String placeID, _base;
  final bool includePublicAuthor, _ownsClient;
  final Listenable identityChanges;
  final bool Function()? sourceCurrent;
  final String? Function() authorizationHeader;
  final http.Client _client;
  final _items = <PlaceHistoryMoment>[];
  int _serial = 0, _epoch = 0;
  bool _closed = false, _loading = false;
  String? _token, _error;
  PlaceHistoryPage? _last;
  Timer? _expiry;
  Stopwatch? _lease;
  Duration? _leaseDuration;
  List<PlaceHistoryMoment> get items {
    _sync();
    return List.unmodifiable(_items);
  }

  PlaceHistoryPage? get page {
    _sync();
    return _last;
  }

  bool get loading {
    _sync();
    return _loading;
  }

  bool get hasMore {
    _sync();
    return _last?.nextCursor != null;
  }

  String? get error {
    _sync();
    return _error;
  }

  String? _sync() {
    final t = authorizationHeader();
    if (t != _token) {
      _token = t;
      invalidate(notify: false);
    }
    return t;
  }

  void _sourceChanged() {
    // The Now boundary also notifies for harmless map loading and theme changes.
    // Only its current binding retires the pages. With no predicate, callers
    // must supply identity-only notifications, preserving the original contract.
    if (sourceCurrent == null || !sourceCurrent!()) invalidate();
  }

  void invalidate({bool notify = true}) {
    ++_serial;
    ++_epoch;
    _expiry?.cancel();
    _lease = null;
    _leaseDuration = null;
    _items.clear();
    _last = null;
    _error = null;
    _loading = false;
    if (!_closed && notify) notifyListeners();
  }

  Future<void> refresh() => _read(false);
  Future<void> loadMore() => _read(true);
  Future<void> _read(bool more) async {
    if (_closed) return;
    if (sourceCurrent?.call() == false) {
      invalidate();
      return;
    }
    final token = _sync(), prior = _last;
    if ((more && _loading) || (more && prior?.nextCursor == null)) return;
    final epoch = _epoch, serial = ++_serial;
    bool current() =>
        !_closed &&
        sourceCurrent?.call() != false &&
        _sync() == token &&
        epoch == _epoch &&
        serial == _serial;
    if (!more) {
      _expiry?.cancel();
      _lease = null;
      _leaseDuration = null;
      _items.clear();
      _last = null;
    }
    _loading = true;
    _error = null;
    notifyListeners();
    final elapsed = Stopwatch()..start();
    try {
      final query = {
        if (includePublicAuthor) 'include': 'public-author',
        if (more) 'cursor': prior!.nextCursor!,
      };
      final uri = Uri.parse(
        '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/places/${Uri.encodeComponent(placeID)}/social-history/moments',
      ).replace(queryParameters: query.isEmpty ? null : query);
      final response = await _client
          .get(uri, headers: {'Authorization': ?token})
          .timeout(const Duration(seconds: 12));
      if (response.statusCode == 409) throw const PlaceHistoryPageChanged();
      if (response.statusCode != 200) throw const FormatException();
      final raw = jsonDecode(utf8.decode(response.bodyBytes));
      if (raw is! Map<String, dynamic> ||
          raw.length != 1 ||
          raw['data'] is! Map<String, dynamic>)
        throw const FormatException();
      final next = PlaceHistoryPage.decode(
        raw['data'],
        placeID,
        includePublicAuthor,
      );
      var remaining =
          next.validUntil.difference(next.observedAt) - elapsed.elapsed;
      if (more && _lease != null && _leaseDuration != null) {
        final left = _leaseDuration! - _lease!.elapsed;
        if (left < remaining) remaining = left;
      }
      if (remaining <= Duration.zero) throw const PlaceHistoryPageChanged();
      if (more) {
        if (next.windowEnd != prior!.windowEnd ||
            next.validUntil != prior.validUntil ||
            next.sourceVersion != prior.sourceVersion ||
            next.count != prior.count)
          throw const PlaceHistoryPageChanged();
        final ids = _items.map((m) => m.id).toSet();
        for (final item in next.items) {
          if (ids.contains(item.id)) throw const FormatException();
          if (_items.isNotEmpty) {
            final last = _items.last;
            if (item.publishedAt.isAfter(last.publishedAt) ||
                (item.publishedAt == last.publishedAt &&
                    item.id!.compareTo(last.id!) >= 0))
              throw const FormatException();
          }
          ids.add(item.id);
        }
      }
      if (next.nextCursor == null &&
          (more ? _items.length : 0) + next.items.length != next.count)
        throw const FormatException();
      if (!current()) return;
      if (!more) {
        _lease = Stopwatch()..start();
        _leaseDuration = remaining;
      }
      _items.addAll(next.items);
      _last = next;
      _expiry?.cancel();
      // Every page carries the fixed original deadline. The timer may shorten
      // on conservative elapsed accounting, never lengthen that original lease.
      _expiry = Timer(remaining, () {
        if (!_closed && _sync() == token && _epoch == epoch) {
          invalidate();
          _error = '公开分享需要重新读取。';
          notifyListeners();
        }
      });
    } catch (e) {
      if (current()) {
        _expiry?.cancel();
        _items.clear();
        _last = null;
        _error = e is PlaceHistoryPageChanged
            ? '公开分享已更新，请重新读取。'
            : '公开分享暂不可用，请重试。';
      }
    } finally {
      if (current()) {
        _loading = false;
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    ++_serial;
    _expiry?.cancel();
    identityChanges.removeListener(_sourceChanged);
    _items.clear();
    _last = null;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
