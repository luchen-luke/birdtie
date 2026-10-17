import 'dart:convert';

import 'package:flutter/foundation.dart';

import 'native_map_failure.dart';

enum NativeMapDiagnosticProvider { mapbox, amap }

enum NativeMapDiagnosticFailureStage {
  provider,
  localStyle,
  initialization,
  renderControl,
}

/// Numeric wire codes are stable in profile logs; append new codes at the end.
enum NativeMapDiagnosticEvent {
  mounted,
  created,
  styleLoaded,
  mapLoaded,
  motionStarted,
  motionSettled,
  lifecycle,
  renderingState,
  frameSummary,
  resourceError,
  styleRetry,
  disposed,
  stylePrepared,
}

/// Profile-only, bounded diagnostics. No business IDs, coordinates, URLs,
/// credentials, text, media or wall-clock timestamps enter this collector.
/// A render completion is SDK evidence, not proof of screen presentation.
class NativeMapDiagnostics {
  NativeMapDiagnostics({
    required this.provider,
    this.enabled = kProfileMode,
    this.capacity = 64,
    this.eventLimit = 256,
    void Function(Map<String, int>)? onEvent,
    int Function()? elapsedMicros,
  }) : assert(capacity > 0 && capacity <= 256),
       assert(eventLimit > 0 && eventLimit <= 1024),
       _onEvent = onEvent ?? _printEvent,
       _elapsedMicros = elapsedMicros {
    if (enabled) {
      host = ++_nextHost;
      _clock.start();
    }
  }

  static int _nextHost = 0, _nextInstance = 0;
  static void _printEvent(Map<String, int> event) =>
      debugPrint('Birdtie native map diag ${jsonEncode(event)}');

  final NativeMapDiagnosticProvider provider;
  final bool enabled;
  final int capacity, eventLimit;
  final void Function(Map<String, int>) _onEvent;
  final int Function()? _elapsedMicros;
  final _clock = Stopwatch();
  final _events = <Map<String, int>>[];
  int host = 0, _instance = 0, _sequence = 0, _dropped = 0;
  int _creations = 0, _styles = 0, _loaded = 0, _motion = 0;
  int _preparedStyles = 0;
  int _cameraCallbacks = 0, _frames = 0, _fullFrames = 0;
  int _lastFrameMicros = -1, _lastFrameLogMicros = -1;
  int _maxFrameMicros = 0, _errors = 0, _lastErrorLogMicros = -1;
  int? _lastErrorCode;
  bool _moving = false, _disposed = false;

  List<Map<String, int>> get events => List.unmodifiable(_events);
  int get _now =>
      (_elapsedMicros?.call() ?? _clock.elapsedMicroseconds).clamp(0, 1 << 53);

  void mounted() => _emit(NativeMapDiagnosticEvent.mounted);

  void created() {
    if (!enabled || _disposed) return;
    _instance = ++_nextInstance;
    _creations++;
    _moving = false;
    _emit(NativeMapDiagnosticEvent.created);
  }

  void styleLoaded() {
    if (!enabled || _disposed) return;
    _styles++;
    _emit(NativeMapDiagnosticEvent.styleLoaded);
  }

  /// Local bytes only; kept distinct from the provider style-loaded callback.
  void stylePrepared() {
    if (!enabled || _disposed) return;
    _preparedStyles++;
    _emit(NativeMapDiagnosticEvent.stylePrepared);
  }

  void mapLoaded() {
    if (!enabled || _disposed) return;
    _loaded++;
    _emit(NativeMapDiagnosticEvent.mapLoaded);
  }

  void cameraMoving() {
    if (!enabled || _disposed) return;
    _cameraCallbacks++;
    if (_moving) return;
    _moving = true;
    _motion++;
    _emit(NativeMapDiagnosticEvent.motionStarted);
  }

  void cameraSettled() {
    if (!enabled || _disposed || !_moving) return;
    _moving = false;
    _emit(NativeMapDiagnosticEvent.motionSettled);
  }

  /// [state] is the AppLifecycleState enum index, never a map/user identity.
  void lifecycle(int state) =>
      _emit(NativeMapDiagnosticEvent.lifecycle, {'state': state.clamp(0, 4)});

  /// Called only after the existing native controls acknowledge success.
  void renderingState({required bool? paused, required int fps}) =>
      _emit(NativeMapDiagnosticEvent.renderingState, {
        'paused': paused == null ? -1 : (paused ? 1 : 0),
        'fps': fps.clamp(0, 120),
      });

  void renderFrame({
    required bool fullyRendered,
    required int durationMicros,
    required bool needsRepaint,
  }) {
    if (!enabled || _disposed) return;
    final now = _now;
    _frames++;
    if (fullyRendered) _fullFrames++;
    final duration = durationMicros.clamp(0, 60000000);
    if (duration > _maxFrameMicros) _maxFrameMicros = duration;
    _lastFrameMicros = now;
    // Count every callback, but serialize at most one frame summary per second.
    if (_lastFrameLogMicros >= 0 && now - _lastFrameLogMicros < 1000000) {
      return;
    }
    _lastFrameLogMicros = now;
    _emit(NativeMapDiagnosticEvent.frameSummary, {
      'frameUs': duration,
      'full': fullyRendered ? 1 : 0,
      'repaint': needsRepaint ? 1 : 0,
    });
  }

  void resourceError({
    required NativeMapResource resource,
    required NativeMapFailureReason reason,
    int? httpStatus,
    NativeMapDiagnosticFailureStage stage =
        NativeMapDiagnosticFailureStage.provider,
  }) {
    if (!enabled || _disposed) return;
    _errors++;
    final status = httpStatus != null && httpStatus >= 100 && httpStatus <= 599
        ? httpStatus
        : 0;
    final code =
        stage.index * 100000 +
        resource.index * 10000 +
        reason.index * 1000 +
        status;
    final now = _now;
    if (_lastErrorCode == code && now - _lastErrorLogMicros < 1000000) return;
    _lastErrorCode = code;
    _lastErrorLogMicros = now;
    _emit(NativeMapDiagnosticEvent.resourceError, {
      'resource': resource.index,
      'reason': reason.index,
      'status': status,
      'stage': stage.index,
    });
  }

  void styleRetry() => _emit(NativeMapDiagnosticEvent.styleRetry);

  void dispose() {
    if (!enabled || _disposed) return;
    _emit(NativeMapDiagnosticEvent.disposed, const {}, true);
    _disposed = true;
    _clock.stop();
  }

  void _emit(
    NativeMapDiagnosticEvent event, [
    Map<String, int> detail = const {},
    bool finalEvent = false,
  ]) {
    if (!enabled || _disposed) return;
    if (_sequence >= eventLimit && !finalEvent) {
      _dropped++;
      return;
    }
    final now = _now;
    final snapshot = Map<String, int>.unmodifiable({
      'v': 1,
      'provider': provider.index,
      'host': host,
      'instance': _instance,
      'event': event.index,
      'seq': ++_sequence,
      'us': now,
      'motion': _motion,
      'moving': _moving ? 1 : 0,
      'cameraCallbacks': _cameraCallbacks,
      'creations': _creations,
      'styles': _styles,
      'preparedStyles': _preparedStyles,
      'loaded': _loaded,
      'frames': _frames,
      'fullFrames': _fullFrames,
      'maxFrameUs': _maxFrameMicros,
      'lastFrameAgeUs': _lastFrameMicros < 0 ? -1 : now - _lastFrameMicros,
      'errors': _errors,
      'dropped': _dropped,
      ...detail,
    });
    if (_events.length == capacity) _events.removeAt(0);
    _events.add(snapshot);
    try {
      _onEvent(snapshot);
    } catch (_) {
      // Diagnostics must not change rendering or authorization behavior.
    }
  }
}
