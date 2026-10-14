import 'dart:async';
import 'dart:convert';
import 'dart:developer' as developer;
import 'dart:math' as math;
import 'dart:ui' show FramePhase, FrameTiming;

import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'package:crypto/crypto.dart' show sha256;

/// Closed names only: the probe never accepts conversation, location or owner
/// identifiers. Frame timings cover Flutter, not the native map GPU/present time.
enum NowPerfScene { startup, mapPan, socialToggle, keyboard, sustained }

enum NowPerfMilestone {
  mapCreated,
  mapLoaded,
  mapFirstInteraction,
  socialFirstPaint,
  keyboardVisible,
  imageReady,
  messageReceived,
}

enum NowPerfEnd { elapsed, manual, disposed }

typedef NowPerfTimingsListener = void Function(List<FrameTiming> timings);
typedef NowPerfTimingsRegistration =
    void Function(NowPerfTimingsListener listener);

/// Bounded, explicitly opted-in profile/release measurement. No scheduled
/// frames, persistent animation, network request, media or account state.
class NowPerfProbe {
  NowPerfProbe({
    required this.enabled,
    required this.onReport,
    this.drainDuration = const Duration(milliseconds: 1100),
    NowPerfTimingsRegistration? addTimingsListener,
    NowPerfTimingsRegistration? removeTimingsListener,
    int Function()? clockMicros,
  }) : _add = addTimingsListener ?? _addSchedulerListener,
       _remove = removeTimingsListener ?? _removeSchedulerListener,
       _clock = clockMicros ?? (() => developer.Timeline.now);

  factory NowPerfProbe.fromEnvironment({
    required ValueChanged<NowPerfReport> onReport,
  }) => NowPerfProbe(enabled: environmentEnabled, onReport: onReport);

  static const environmentEnabled = bool.fromEnvironment(
    'BIRDTIE_NOW_PERF_PROBE',
  );

  final bool enabled;
  final ValueChanged<NowPerfReport> onReport;
  final Duration drainDuration;
  final NowPerfTimingsRegistration _add, _remove;
  final int Function() _clock;
  final List<int> _ui = [], _raster = [], _total = [];
  final Map<NowPerfMilestone, int> _milestones = {};
  Timer? _windowTimer, _drainTimer;
  Completer<NowPerfReport?>? _completion;
  NowPerfScene? _scene;
  NowPerfEnd? _endReason;
  int? _startUs, _endUs;
  double _refreshRateHz = 60;
  bool _listening = false, _disposed = false;
  int _discarded = 0;
  // Hard bound even if an engine sends duplicate/unexpected batches.
  static const maxFrames = 72000;

  static void _addSchedulerListener(NowPerfTimingsListener listener) =>
      SchedulerBinding.instance.addTimingsCallback(listener);
  static void _removeSchedulerListener(NowPerfTimingsListener listener) =>
      SchedulerBinding.instance.removeTimingsCallback(listener);

  bool get active => _listening && _endUs == null;

  /// Use View.of(context).display.refreshRate; do not assume a 120 Hz panel
  /// renders the application at 120 Hz. Record actual present rate separately.
  bool beginWindow({
    required NowPerfScene scene,
    required double refreshRateHz,
    Duration duration = const Duration(seconds: 60),
  }) {
    if (!enabled || _disposed || _listening) return false;
    if (!refreshRateHz.isFinite ||
        refreshRateHz <= 0 ||
        duration <= Duration.zero ||
        duration > const Duration(minutes: 10) ||
        drainDuration.isNegative) {
      throw ArgumentError('Invalid performance measurement window.');
    }
    _ui.clear();
    _raster.clear();
    _total.clear();
    _milestones.clear();
    _discarded = 0;
    _scene = scene;
    _refreshRateHz = refreshRateHz;
    _startUs = _clock();
    _endUs = null;
    _endReason = null;
    _completion = Completer<NowPerfReport?>();
    _listening = true;
    _add(_onTimings);
    _windowTimer = Timer(
      duration,
      () => stopWindow(reason: NowPerfEnd.elapsed),
    );
    return true;
  }

  /// Explicit application milestone, first observation wins. This measures
  /// arrival/paint separately; it is not included in raster overhead.
  void mark(NowPerfMilestone milestone) {
    if (!active) return;
    _milestones.putIfAbsent(milestone, () => math.max(0, _clock() - _startUs!));
  }

  void _onTimings(List<FrameTiming> timings) {
    if (!_listening) return;
    for (final frame in timings) {
      final start = frame.timestampInMicroseconds(FramePhase.vsyncStart);
      // Timings arrive in batches. Exclude pre-window/post-window frames,
      // including frames received during the asynchronous drain period.
      if (start < _startUs! || (_endUs != null && start >= _endUs!)) continue;
      final ui = frame.buildDuration.inMicroseconds;
      final raster = frame.rasterDuration.inMicroseconds;
      final total = frame.totalSpan.inMicroseconds;
      if (ui < 0 || raster < 0 || total < 0 || _ui.length >= maxFrames) {
        _discarded++;
        continue;
      }
      _ui.add(ui);
      _raster.add(raster);
      _total.add(total);
    }
  }

  /// Wait only for pending engine timing batches, without generating frames.
  /// Flutter release can batch timings approximately once per second.
  Future<NowPerfReport?> stopWindow({NowPerfEnd reason = NowPerfEnd.manual}) {
    if (!_listening) return Future.value(null);
    if (_endUs == null) {
      _endUs = _clock();
      _endReason = reason;
      _windowTimer?.cancel();
      _drainTimer = Timer(drainDuration, _finish);
    }
    return _completion!.future;
  }

  void _finish() {
    if (!_listening) return;
    _windowTimer?.cancel();
    _drainTimer?.cancel();
    _remove(_onTimings);
    _listening = false;
    final report = NowPerfReport(
      scene: _scene!,
      reason: _endReason!,
      windowMicros: math.max(0, _endUs! - _startUs!),
      refreshRateHz: _refreshRateHz,
      uiMicros: _ui,
      rasterMicros: _raster,
      totalMicros: _total,
      milestoneMicros: _milestones,
      discardedFrames: _discarded,
    );
    _completion!.complete(report);
    onReport(report);
  }

  void dispose() {
    if (_disposed) return;
    _disposed = true;
    if (!_listening) return;
    _endUs ??= _clock();
    _endReason = NowPerfEnd.disposed;
    // A disposed host cannot keep a callback alive for the drain period.
    _finish();
  }
}

@immutable
class NowPerfReport {
  NowPerfReport({
    required this.scene,
    required this.reason,
    required this.windowMicros,
    required this.refreshRateHz,
    required List<int> uiMicros,
    required List<int> rasterMicros,
    required List<int> totalMicros,
    required Map<NowPerfMilestone, int> milestoneMicros,
    required this.discardedFrames,
  }) : _ui = List.unmodifiable(uiMicros),
       _raster = List.unmodifiable(rasterMicros),
       _total = List.unmodifiable(totalMicros),
       _milestones = Map.unmodifiable(milestoneMicros);

  final NowPerfScene scene;
  final NowPerfEnd reason;
  final int windowMicros, discardedFrames;
  final double refreshRateHz;
  final List<int> _ui, _raster, _total;
  final Map<NowPerfMilestone, int> _milestones;

  /// Pure formatting for an opted-in QA onReport callback. No logging, clock,
  /// timer, listener or network is added; disabled probes never call onReport.
  /// Reassembly must require current PID, BEGIN, every ordered unique chunk,
  /// END, matching total/ASCII length and SHA256 before parsing the JSON.
  List<String> toQaLogLines() {
    const prefix = 'BIRDTIE_NOW_PERF_V2';
    const payloadCharacters = 240;
    final payload = jsonEncode(toJson());
    // Closed enum/metric fields are ASCII. Refuse any future non-ASCII field
    // rather than accepting arbitrary user material or a byte/char mismatch.
    final bytes = ascii.encode(payload);
    final digest = sha256.convert(bytes).toString();
    final total = (payload.length / payloadCharacters).ceil();
    final lines = <String>[
      '$prefix BEGIN $digest $total ${bytes.length}',
      for (var index = 0; index < total; index++)
        '$prefix CHUNK $digest ${index + 1}/$total '
            '${payload.substring(index * payloadCharacters, math.min((index + 1) * payloadCharacters, payload.length))}',
      '$prefix END $digest $total ${bytes.length}',
    ];
    if (lines.any((line) => line.length > 400)) {
      throw StateError('QA performance transport line exceeds bound.');
    }
    return List.unmodifiable(lines);
  }

  Map<String, Object?> toJson() {
    final displayBudgetUs = 1000000 / refreshRateHz;
    const targetBudgetUs = 1000000 / 60;
    Map<String, Object?> metrics(List<int> raw) {
      final sorted = [...raw]..sort();
      double? percentile(double p) =>
          sorted.isEmpty ? null : sorted[(sorted.length * p).ceil() - 1] / 1000;
      final displaySlow = raw.where((us) => us > displayBudgetUs).length;
      final targetSlow = raw.where((us) => us > targetBudgetUs).length;
      return {
        'count': raw.length,
        'p50Ms': percentile(.50),
        'p95Ms': percentile(.95),
        'p99Ms': percentile(.99),
        'maxMs': sorted.isEmpty ? null : sorted.last / 1000,
        'overDisplayBudgetCount': displaySlow,
        'overDisplayBudgetRatio': raw.isEmpty ? null : displaySlow / raw.length,
        'over60HzBudgetCount': targetSlow,
        'over60HzBudgetRatio': raw.isEmpty ? null : targetSlow / raw.length,
      };
    }

    return {
      'schema': 'birdtie.now.perf.v1',
      'scene': scene.name,
      'endReason': reason.name,
      'buildMode': kReleaseMode
          ? 'release'
          : (kProfileMode ? 'profile' : 'debug'),
      'eligibleForReleaseComparison': kReleaseMode || kProfileMode,
      'measurementStatus': _ui.isEmpty ? 'NO_FRAMES' : 'SAMPLE_OBSERVED',
      'windowMs': windowMicros / 1000,
      'displayRefreshRateHz': refreshRateHz,
      'displayBudgetMs': displayBudgetUs / 1000,
      'target60HzBudgetMs': targetBudgetUs / 1000,
      'frameCount': _ui.length,
      'discardedFrames': discardedFrames,
      'uiBuild': metrics(_ui),
      'raster': metrics(_raster),
      'totalSpan': metrics(_total),
      'milestonesMs': {
        for (final entry in _milestones.entries)
          entry.key.name: entry.value / 1000,
      },
      'nativeMapPresentTimingsIncluded': false,
      'idleTimeConvertedToFrames': false,
      'absolutePerformancePass': false,
    };
  }
}
