import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import '../workspace/map_entities.dart';
import 'browse_destination.dart';
import 'amap_destination_search.dart';

class DestinationSearchResult {
  const DestinationSearchResult({
    required this.destinations,
    this.domesticUnavailable = false,
    this.attribution = '',
    this.partialError,
  });
  final List<BrowseDestination> destinations;
  final bool domesticUnavailable;
  final String attribution;
  final String? partialError;
}

class DestinationSearchFailure implements Exception {
  const DestinationSearchFailure(this.message, {this.retryAfter});
  final String message;
  final Duration? retryAfter;
}

abstract interface class DestinationSearchService {
  Future<DestinationSearchResult> search(
    String query, {
    Future<void>? abortTrigger,
  });
  void dispose();
}

/// Search routing is independent of the currently displayed city and provider.
/// Domestic requests require the same explicit native SDK consent as the map.
class CompositeDestinationSearchService implements DestinationSearchService {
  CompositeDestinationSearchService({
    DestinationSearchService? overseas,
    DestinationSearchService? domestic,
    bool Function()? domesticEnabled,
  }) : _overseas = overseas ?? MapboxDestinationSearchService(),
       _domestic = domestic ?? AMapDestinationSearchService(),
       _ownsOverseas = overseas == null,
       _ownsDomestic = domestic == null,
       _domesticEnabled =
           domesticEnabled ?? (() => AMapDestinationSearchService.enabled);

  final DestinationSearchService _overseas, _domestic;
  final bool _ownsOverseas, _ownsDomestic;
  final bool Function() _domesticEnabled;
  final _retryAt = <DestinationSearchService, DateTime>{};
  final _failures = <DestinationSearchService, DestinationSearchFailure>{};
  bool _closed = false;

  @override
  Future<DestinationSearchResult> search(
    String query, {
    Future<void>? abortTrigger,
  }) async {
    if (_closed) throw const DestinationSearchFailure('目的地搜索已关闭。');
    final useDomestic = _domesticEnabled();
    var cancelled = false;
    if (abortTrigger != null)
      unawaited(abortTrigger.then((_) => cancelled = true));
    Future<(DestinationSearchResult?, DestinationSearchFailure?)> read(
      DestinationSearchService source,
    ) async {
      if (_retryAt[source]?.isAfter(DateTime.now()) == true) {
        return (null, _failures[source]);
      }
      try {
        final result = await source.search(query, abortTrigger: abortTrigger);
        _retryAt.remove(source);
        _failures.remove(source);
        return (result, null);
      } catch (error) {
        final failure = error is DestinationSearchFailure
            ? error
            : const DestinationSearchFailure(
                '目的地搜索失败，请重试。',
                retryAfter: Duration(seconds: 2),
              );
        if (!cancelled && !_closed && failure.retryAfter != null) {
          _retryAt[source] = DateTime.now().add(failure.retryAfter!);
          _failures[source] = failure;
        }
        return (null, failure);
      }
    }

    final results = await Future.wait([
      read(_overseas),
      if (useDomestic) read(_domestic),
    ]);
    if (_closed) throw const DestinationSearchFailure('目的地搜索已关闭。');
    final overseas = results[0].$1, overseasError = results[0].$2;
    final domestic = useDomestic ? results[1].$1 : null;
    final domesticError = useDomestic ? results[1].$2 : null;
    if (overseas == null && domestic == null) throw overseasError!;
    final destinations = <BrowseDestination>[];
    final seen = <String>{};
    for (final item in [
      ...?overseas?.destinations,
      ...?domestic?.destinations,
    ]) {
      if (seen.add(item.browseKey)) destinations.add(item);
    }
    return DestinationSearchResult(
      destinations: List.unmodifiable(destinations),
      domesticUnavailable: useDomestic
          ? domesticError != null || (domestic?.domesticUnavailable ?? false)
          : overseas?.domesticUnavailable ?? false,
      attribution: {
        if (overseas?.attribution.isNotEmpty == true) overseas!.attribution,
        if (domestic?.attribution.isNotEmpty == true) domestic!.attribution,
      }.join('\n'),
      partialError: overseasError != null
          ? '部分目的地暂未能读取，请重试。'
          : domesticError != null
          ? '国内目的地暂未能读取，请重试。'
          : overseas?.partialError ?? domestic?.partialError,
    );
  }

  @override
  void dispose() {
    _closed = true;
    _retryAt.clear();
    _failures.clear();
    if (_ownsOverseas) _overseas.dispose();
    if (_ownsDomestic) _domestic.dispose();
  }
}

/// Temporary geocoding is used only for the active interaction. No result cache,
/// logging, persistence, catalog insertion or cross-provider map reuse exists.
class MapboxDestinationSearchService implements DestinationSearchService {
  MapboxDestinationSearchService({http.Client? client, String? accessToken})
    : _client = client ?? http.Client(),
      _ownsClient = client == null,
      _accessToken = accessToken ?? BirdtieEnvironment.mapboxMobilePublicToken;

  final http.Client _client;
  final bool _ownsClient;
  final String _accessToken;
  bool _closed = false;

  @override
  Future<DestinationSearchResult> search(
    String query, {
    Future<void>? abortTrigger,
  }) async {
    final value = query.trim();
    if (_closed) throw const DestinationSearchFailure('目的地搜索已关闭。');
    if (value.isEmpty ||
        value.length > 256 ||
        value.contains(';') ||
        value.runes.any((r) => r < 32 || r == 127) ||
        value.split(RegExp(r'\s+')).length > 20) {
      throw const DestinationSearchFailure('请输入简短的城市或目的地名称。');
    }
    if (!_accessToken.startsWith('pk.')) {
      throw const DestinationSearchFailure('目的地搜索暂不可用，请稍后重试。');
    }
    final uri = Uri.https('api.mapbox.com', '/search/geocode/v6/forward', {
      'q': value,
      'types': 'country,region,district,place,locality',
      'language': 'zh-Hans,en',
      'limit': '5',
      'autocomplete': 'false',
      'permanent': 'false',
      'access_token': _accessToken,
    });
    final request = http.AbortableRequest(
      'GET',
      uri,
      abortTrigger: abortTrigger,
    )..followRedirects = false;
    try {
      final response = await _client
          .send(request)
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        await response.stream.listen((_) {}).cancel();
        throw DestinationSearchFailure(
          response.statusCode == 429 ? '搜索较频繁，请稍后重试。' : '目的地搜索失败，请重试。',
          retryAfter: response.statusCode == 429
              ? const Duration(seconds: 30)
              : const Duration(seconds: 2),
        );
      }
      final reader = StreamIterator(response.stream);
      final bytes = <int>[];
      final deadline = Stopwatch()..start();
      try {
        while (await reader.moveNext().timeout(const Duration(seconds: 8))) {
          if (_closed ||
              deadline.elapsed > const Duration(seconds: 15) ||
              bytes.length + reader.current.length > 128 * 1024) {
            throw const FormatException('destination response unavailable');
          }
          bytes.addAll(reader.current);
        }
        if (_closed) throw const FormatException('destination source retired');
        return _decode(jsonDecode(utf8.decode(bytes)));
      } finally {
        bytes.fillRange(0, bytes.length, 0);
        await reader.cancel();
      }
    } on DestinationSearchFailure {
      rethrow;
    } catch (_) {
      // Transport exceptions may contain token-bearing URLs. Never surface them.
      throw const DestinationSearchFailure(
        '目的地搜索失败，请重试。',
        retryAfter: Duration(seconds: 2),
      );
    }
  }

  DestinationSearchResult _decode(dynamic raw) {
    if (raw is! Map ||
        raw['type'] != 'FeatureCollection' ||
        raw['features'] is! List ||
        (raw['features'] as List).length > 10 ||
        raw['attribution'] is! String ||
        (raw['attribution'] as String).trim().isEmpty) {
      throw const FormatException('destination envelope unavailable');
    }
    final attribution = raw['attribution'] as String;
    final items = <BrowseDestination>[];
    final seen = <String>{};
    var rejected = 0, domestic = false;
    for (final feature in raw['features'] as List) {
      try {
        if (feature is! Map || feature['type'] != 'Feature') {
          throw const FormatException('destination feature unavailable');
        }
        final props = feature['properties'];
        final geometry = feature['geometry'];
        if (props is! Map ||
            geometry is! Map ||
            geometry['type'] != 'Point' ||
            geometry['coordinates'] is! List) {
          throw const FormatException('destination geometry unavailable');
        }
        final coordinates = geometry['coordinates'] as List;
        final context = props['context'];
        final country = context is Map ? context['country'] : null;
        final region = context is Map ? context['region'] : null;
        final countryCode = country is Map && country['country_code'] is String
            ? (country['country_code'] as String).toUpperCase()
            : '';
        final id = props['mapbox_id'] ?? feature['id'];
        final name = props['name'];
        final kind = props['feature_type'];
        if (coordinates.length != 2 ||
            coordinates[0] is! num ||
            coordinates[1] is! num ||
            id is! String ||
            id.length > 512 ||
            name is! String ||
            name.length > 160 ||
            !const {
              'country',
              'region',
              'district',
              'place',
              'locality',
            }.contains(kind)) {
          throw const FormatException('destination identity unavailable');
        }
        MapBounds? bounds;
        final bbox = props['bbox'] ?? feature['bbox'];
        if (bbox is List && bbox.length == 4 && bbox.every((v) => v is num)) {
          final candidate = MapBounds(
            west: (bbox[0] as num).toDouble(),
            south: (bbox[1] as num).toDouble(),
            east: (bbox[2] as num).toDouble(),
            north: (bbox[3] as num).toDouble(),
          );
          if (candidate.isValid) bounds = candidate;
        }
        final destination = BrowseDestination(
          provider: 'mapbox',
          externalID: id,
          name: name,
          countryCode: countryCode,
          region: [
            if (region is Map && region['name'] is String)
              region['name'] as String,
            if (country is Map && country['name'] is String)
              country['name'] as String,
          ].join(' · '),
          latitude: (coordinates[1] as num).toDouble(),
          longitude: (coordinates[0] as num).toDouble(),
          bounds: bounds,
          featureType: kind as String,
          attribution: attribution,
          storageMode: DestinationStorageMode.temporary,
        );
        if (!destination.valid)
          throw const FormatException('invalid destination');
        // Mapbox geocoding is licensed for use with Mapbox maps only. Mainland
        // rendering is AMap; a separate licensed search source is required.
        if (countryCode == 'CN') {
          domestic = true;
          continue;
        }
        if (seen.add(destination.browseKey)) items.add(destination);
      } on FormatException {
        rejected++;
      }
    }
    if (items.isEmpty && rejected > 0 && !domestic) {
      throw const FormatException('destination results unavailable');
    }
    return DestinationSearchResult(
      destinations: List.unmodifiable(items),
      domesticUnavailable: domestic,
      attribution: attribution,
    );
  }

  @override
  void dispose() {
    _closed = true;
    if (_ownsClient) _client.close();
  }
}
