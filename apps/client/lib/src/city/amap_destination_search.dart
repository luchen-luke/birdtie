import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'amap_session_consent.dart';
import 'browse_destination.dart';
import 'destination_search_service.dart';
import 'map_coordinate_boundary.dart';

/// Android SDK service shares the map's installed Manifest Key. Neither Key nor
/// Web-service credentials cross the bridge. Results live only in this session.
class AMapDestinationSearchService implements DestinationSearchService {
  static const channel = MethodChannel('birdtie/destination_search');
  static int _sequence = 0;
  final _pending = <String>{};
  bool _closed = false;
  static bool get supported =>
      !kIsWeb && defaultTargetPlatform == TargetPlatform.android;
  static bool get enabled => supported && AMapSessionConsent.accepted;

  @override
  Future<DestinationSearchResult> search(
    String query, {
    Future<void>? abortTrigger,
  }) async {
    if (_closed || !enabled) {
      return const DestinationSearchResult(
        destinations: [],
        domesticUnavailable: true,
      );
    }
    final id = 'destination-${++_sequence}';
    _pending.add(id);
    var cancelled = false;
    var finished = false;
    void cancel() {
      if (finished) return;
      cancelled = true;
      unawaited(
        channel
            .invokeMethod<void>('cancel', {'requestId': id})
            .catchError((Object _) {}),
      );
    }

    if (abortTrigger != null) unawaited(abortTrigger.then((_) => cancel()));
    try {
      final raw = await channel
          .invokeMapMethod<String, dynamic>('search', {
            'requestId': id,
            'query': query.trim(),
            'consent': true,
          })
          .timeout(const Duration(seconds: 14));
      if (_closed || cancelled || !_pending.contains(id))
        throw const FormatException('retired');
      if (raw == null ||
          raw['items'] is! List ||
          (raw['items'] as List).length > 5 ||
          raw['attribution'] != '高德地图') {
        throw const FormatException('invalid destination result');
      }
      final items = <BrowseDestination>[];
      for (final row in raw['items'] as List) {
        if (row is! Map ||
            row['coordinateSystem'] != 'gcj02' ||
            row['countryCode'] != 'CN' ||
            row['latitude'] is! num ||
            row['longitude'] is! num ||
            row['id'] is! String ||
            row['name'] is! String ||
            row['region'] is! String ||
            !const {
              'country',
              'region',
              'place',
              'district',
              'locality',
            }.contains(row['featureType'])) {
          throw const FormatException('invalid destination');
        }
        final point = MapCoordinateBoundary.fromAmap(
          (row['latitude'] as num).toDouble(),
          (row['longitude'] as num).toDouble(),
        );
        final destination = BrowseDestination(
          provider: 'amap',
          externalID: row['id'],
          name: row['name'],
          countryCode: 'CN',
          region: row['region'],
          latitude: point.latitude,
          longitude: point.longitude,
          featureType: row['featureType'],
          attribution: '高德地图',
          storageMode: DestinationStorageMode.temporary,
        );
        if (!destination.valid)
          throw const FormatException('invalid destination');
        items.add(destination);
      }
      return DestinationSearchResult(
        destinations: List.unmodifiable(items),
        attribution: '高德地图',
      );
    } catch (_) {
      if (!finished) cancel();
      throw const DestinationSearchFailure(
        '国内目的地搜索失败，请重试。',
        retryAfter: Duration(seconds: 2),
      );
    } finally {
      finished = true;
      _pending.remove(id);
    }
  }

  @override
  void dispose() {
    _closed = true;
    for (final id in _pending) {
      unawaited(
        channel
            .invokeMethod<void>('cancel', {'requestId': id})
            .catchError((Object _) {}),
      );
    }
    _pending.clear();
  }
}
