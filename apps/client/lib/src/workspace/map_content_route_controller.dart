import 'dart:async';
import 'package:flutter/foundation.dart';
import 'map_content_route_geometry.dart';

typedef MapContentRouteLoader =
    Future<MapContentRouteGeometry> Function(
      MapContentRouteQuery query,
      bool Function() current,
      Future<void> abort,
    );

class MapContentRouteCandidate {
  const MapContentRouteCandidate(this.query, this.current);
  final MapContentRouteQuery query;
  final bool Function() current;
}

/// Finite, RAM-only geometry. Base content grants are never renewed here.
class MapContentRouteController extends ChangeNotifier {
  MapContentRouteController({
    required this.load,
    required this.now,
    this.maxItems = 24,
    this.timeout = const Duration(seconds: 22),
  });
  final MapContentRouteLoader load;
  final DateTime Function() now;
  final int maxItems;
  final Duration timeout;
  final _entries = <String, _RouteEntry>{};
  final _candidates = <String, MapContentRouteCandidate>{};
  final _queue = <String>[];
  Timer? _expiry;
  int _epoch = 0, _running = 0;
  bool _closed = false;
  MapContentRouteGeometry? geometry(MapContentRouteQuery query) {
    final value = _entries[query.key]?.value;
    return value?.availableAt(now()) == true ? value : null;
  }

  void sync(Iterable<MapContentRouteCandidate> candidates) {
    if (_closed) return;
    _candidates.clear();
    for (final c in candidates) {
      if (_candidates.length >= maxItems.clamp(0, 24)) break;
      if (c.query.valid && now().isBefore(c.query.validUntil) && c.current())
        _candidates.putIfAbsent(c.query.key, () => c);
    }
    for (final key in _entries.keys.toList()) {
      final e = _entries[key]!, c = _candidates[key];
      if (c == null || !e.live(now())) _remove(key);
    }
    for (final c in _candidates.values) {
      if (!_entries.containsKey(c.query.key)) {
        final e = _RouteEntry(c, _epoch);
        _entries[c.query.key] = e;
        _queue.add(c.query.key);
      }
    }
    _drain();
    _arm();
  }

  void _remove(String key) {
    final e = _entries.remove(key);
    if (e == null) return;
    e.retired = true;
    if (!e.abort.isCompleted) e.abort.complete();
    _queue.remove(key);
  }

  void _drain() {
    while (!_closed && _running < 2 && _queue.isNotEmpty) {
      final key = _queue.removeAt(0), entry = _entries[key];
      if (entry == null || !entry.live(now())) continue;
      _running++;
      entry.running = true;
      unawaited(_load(key, entry));
    }
  }

  Future<void> _load(String key, _RouteEntry entry) async {
    bool current() =>
        !_closed &&
        entry.epoch == _epoch &&
        identical(_entries[key], entry) &&
        entry.live(now());
    try {
      final value =
          await Future.any<MapContentRouteGeometry>([
            load(entry.candidate.query, current, entry.abort.future),
            entry.abort.future.then<MapContentRouteGeometry>(
              (_) => throw StateError('串文路线已取消'),
            ),
          ]).timeout(
            timeout,
            onTimeout: () {
              if (!entry.abort.isCompleted) entry.abort.complete();
              throw TimeoutException('串文路线读取超时');
            },
          );
      if (!current()) return;
      if (value.query.key != entry.candidate.query.key)
        throw const FormatException('串文路线目标不符');
      final otherPoints = _entries.values
          .where((e) => !identical(e, entry))
          .fold<int>(
            0,
            (n, e) =>
                n +
                (e.value?.segments.fold<int>(0, (a, s) => a + s.length) ?? 0),
          );
      if (otherPoints + value.segments.fold<int>(0, (n, s) => n + s.length) >
          24000)
        throw const FormatException('串文路线几何预算已满');
      var until = value.validUntil.isBefore(entry.candidate.query.validUntil)
          ? value.validUntil
          : entry.candidate.query.validUntil;
      if (value.status != 'AVAILABLE' &&
          until.isAfter(now().add(const Duration(seconds: 5))))
        until = now().add(const Duration(seconds: 5));
      if (!now().isBefore(until)) return;
      entry.until = until;
      entry.value = value.bind(until: until, current: current);
    } catch (_) {
      if (current())
        entry.until =
            now()
                .add(const Duration(seconds: 5))
                .isBefore(entry.candidate.query.validUntil)
            ? now().add(const Duration(seconds: 5))
            : entry.candidate.query.validUntil;
    } finally {
      entry.running = false;
      _running--;
      if (!_closed) {
        _arm();
        notifyListeners();
        _drain();
      }
    }
  }

  void _arm() {
    _expiry?.cancel();
    if (_closed) return;
    DateTime? deadline;
    for (final e in _entries.values) {
      final until = e.until ?? e.candidate.query.validUntil;
      if (deadline == null || until.isBefore(deadline)) deadline = until;
    }
    if (deadline != null)
      _expiry = Timer(
        deadline.difference(now()).isNegative
            ? Duration.zero
            : deadline.difference(now()),
        () {
          if (_closed) return;
          for (final key in _entries.keys.toList())
            if (!_entries[key]!.live(now())) _remove(key);
          _arm();
          notifyListeners();
        },
      );
  }

  void retire() {
    _epoch++;
    _expiry?.cancel();
    for (final key in _entries.keys.toList()) _remove(key);
    _candidates.clear();
    _queue.clear();
    if (!_closed) notifyListeners();
  }

  @override
  void dispose() {
    if (_closed) return;
    retire();
    _closed = true;
    super.dispose();
  }
}

class _RouteEntry {
  _RouteEntry(this.candidate, this.epoch);
  final MapContentRouteCandidate candidate;
  final int epoch;
  final abort = Completer<void>();
  bool retired = false, running = false;
  DateTime? until;
  MapContentRouteGeometry? value;
  bool live(DateTime at) =>
      !retired &&
      candidate.current() &&
      at.isBefore(candidate.query.validUntil) &&
      (until == null || at.isBefore(until!));
}
