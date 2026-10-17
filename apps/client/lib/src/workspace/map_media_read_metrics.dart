import 'dart:collection';
import 'dart:convert';
import 'package:flutter/foundation.dart';

enum MapMediaReadKind { detail, image, video }

enum MapMediaReadStage {
  detailReady,
  descriptorReady,
  headers,
  firstByte,
  downloaded,
  verified,
  materialReady,
  initialized,
  playRequested,
  imageVisible,
  failed,
}

/// Numeric, bounded diagnostics only: no resource IDs, URLs, identity, or text.
/// Player initialization/play requests are deliberately not called first frame.
class MapMediaReadMetrics {
  static final _recent = Queue<Map<String, Object>>();
  static int _serial = 0;
  static const capacity = 32;
  static List<Map<String, Object>> get recent => List.unmodifiable(_recent);
  static MapMediaReadTrace begin(MapMediaReadKind kind) =>
      MapMediaReadTrace._(++_serial, kind);
  static void clear() => _recent.clear();
  static void _record(Map<String, Object> value) {
    _recent.add(Map.unmodifiable(value));
    while (_recent.length > capacity) {
      _recent.removeFirst();
    }
    if (kProfileMode &&
        const bool.fromEnvironment(
          'BIRDTIE_MEDIA_TIMINGS',
          defaultValue: true,
        )) {
      debugPrint('[BirdtieMedia] ${jsonEncode(value)}');
    }
  }
}

class MapMediaReadTrace {
  MapMediaReadTrace._(this.operation, this.kind);
  final int operation;
  final MapMediaReadKind kind;
  final _clock = Stopwatch()..start();
  final _seen = <MapMediaReadStage>{};
  bool cacheHit = false;
  int requests = 0, bytes = 0;
  void mark(MapMediaReadStage stage) {
    if (!_seen.add(stage)) return;
    MapMediaReadMetrics._record({
      'operation': operation,
      'kind': kind.name,
      'stage': stage.name,
      'elapsedUs': _clock.elapsedMicroseconds,
      'bytes': bytes,
      'requests': requests,
      'cacheHit': cacheHit,
    });
  }
}
