import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';

import '../auth/session_vault.dart';
import 'map_entities.dart';
import 'map_marker_bitmap.dart';
import 'map_marker_visual.dart';
import 'owned_map_api.dart';

/// Pixels are cached independently of viewport leases. Only a fresh current
/// selector may activate cached pixels; neither URLs nor bearer reach the map.
class OwnedMapMediaController extends ChangeNotifier {
  OwnedMapMediaController({
    required this.api,
    required this.current,
    required this.authorizationHeader,
    required this.now,
    this.maxItems = 24,
    this.maxBytes = 24 * 1024 * 1024,
  });
  final OwnedMapAPI api;
  final bool Function() current;
  final String? Function() authorizationHeader;
  final DateTime Function() now;
  final int maxItems, maxBytes;
  final _entries = <String, _OwnedMapPixels>{};
  final _active = <String, _ActivePixels>{};
  final _queue = <String>[];
  int _epoch = 0, _running = 0, _reserved = 0;
  int requests = 0, decodes = 0, cacheHits = 0, inFlightPeak = 0;
  bool _closed = false, _notifyingLater = false;
  String _origin = '';
  String? _authorization;
  OwnedMapView? _latestView;
  int get inFlight => _running;
  int get cachedItems => _entries.length;
  int get cachedBytes => _reserved;

  void sync(
    OwnedMapView view, {
    required double devicePixelRatio,
    String? selectedMarkerID,
  }) {
    if (_closed) return;
    final origin = canonicalAuthOrigin(api.apiBaseUrl()),
        auth = authorizationHeader();
    if (origin != _origin || auth != _authorization) retire();
    _origin = origin;
    _authorization = auth;
    if (!current() || auth == null) {
      retire();
      return;
    }
    final renewed = !identical(_latestView, view);
    _latestView = view;
    final ratio = MapMarkerBitmap.pixelRatio(devicePixelRatio);
    final chosen = view.items.where((p) => p.cover != null).toList()
      ..sort(
        (a, b) =>
            (b.canonicalMarkerID == selectedMarkerID ? 1 : 0) -
            (a.canonicalMarkerID == selectedMarkerID ? 1 : 0),
      );
    final next = <String, _ActivePixels>{};
    for (final pin in chosen.take(math.min(12, maxItems))) {
      final cover = pin.cover!;
      // Media sourceVersion describes this asset and its permission facts,
      // not a viewport hash. A renewed camera lease does not redownload it.
      final key =
          '${cover.identity}:${cover.width}:${cover.height}:${pin.entity.kind.name}:${pin.categoryCode}:${pin.isCluster ? pin.momentCount : 0}:$ratio';
      var entry = _entries.remove(key);
      if (entry != null) _entries[key] = entry;
      if (entry != null && entry.finished && entry.image == null && renewed) {
        _retire(entry);
        entry = null;
      }
      if (entry == null) {
        final edge = (MapMarkerBitmap.pointCanvasSize * ratio).ceil();
        final reservation = 160 * 160 * 4 + edge * edge * 4 * 4;
        _makeRoom(reservation, next.keys.toSet());
        if (_entries.length >= maxItems || _reserved + reservation > maxBytes)
          continue;
        entry = _OwnedMapPixels(key, ratio, _epoch, reservation);
        _entries[key] = entry;
        _reserved += reservation;
      }
      final old = _active[pin.mapID];
      final active =
          old != null &&
              identical(old.pin, pin) &&
              old.entry == entry &&
              old.until == view.validUntil
          ? old
          : _ActivePixels(pin, cover, entry, view.validUntil);
      if (!identical(old, active) && entry.image != null) cacheHits++;
      next[pin.mapID] = active;
      if (!entry.running && !entry.finished && !_queue.contains(key))
        _queue.add(key);
    }
    _active
      ..clear()
      ..addAll(next);
    for (final entry in _entries.values.toList()) {
      if (_grant(entry) == null && entry.image == null) _retire(entry);
    }
    _drain();
  }

  void _makeRoom(int reservation, Set<String> keepIDs) {
    for (final entry in _entries.values.toList()) {
      if (_entries.length < maxItems && _reserved + reservation <= maxBytes)
        break;
      if (_active.values.any(
        (g) => g.entry == entry && keepIDs.contains(g.pin.mapID),
      ))
        continue;
      if (entry.running) continue;
      _retire(entry);
    }
  }

  _ActivePixels? _grant(_OwnedMapPixels entry) =>
      _active.values.where((g) => g.entry == entry && _current(g)).firstOrNull;
  bool _current(_ActivePixels grant) =>
      !_closed &&
      !grant.entry.retired &&
      current() &&
      grant.entry.epoch == _epoch &&
      identical(_entries[grant.entry.key], grant.entry) &&
      identical(_active[grant.pin.mapID], grant) &&
      grant.until.isAfter(now()) &&
      authorizationHeader() == _authorization &&
      canonicalAuthOrigin(api.apiBaseUrl()) == _origin;

  MapEntity decorate(OwnedMapPin pin) {
    final grant = _active[pin.mapID];
    if (grant == null ||
        !_current(grant) ||
        grant.pin.sourceVersion != pin.sourceVersion ||
        grant.cover.identity != pin.cover?.identity ||
        grant.entry.image == null)
      return pin.entity;
    final entry = grant.entry;
    grant.pixels ??= MapMediaPixels(
      key: '${entry.key}:${pin.sourceVersion}',
      image: entry.image!,
      normalPNG: entry.normal!,
      selectedPNG: entry.selected!,
      devicePixelRatio: entry.ratio,
      current: () => _current(grant),
      validUntil:
          pin.validUntil != null && pin.validUntil!.isBefore(grant.until)
          ? pin.validUntil
          : grant.until,
    );
    return pin.entity.withMediaPixels(grant.pixels);
  }

  void _drain() {
    while (!_closed && _running < 4 && _queue.isNotEmpty) {
      final entry = _entries[_queue.removeAt(0)];
      if (entry == null ||
          _grant(entry) == null ||
          entry.running ||
          entry.finished)
        continue;
      entry.running = true;
      entry.abort = Completer<void>();
      _running++;
      inFlightPeak = math.max(inFlightPeak, _running);
      unawaited(_load(entry));
    }
  }

  Future<void> _load(_OwnedMapPixels entry) async {
    Uint8List? bytes, normal, selected;
    ui.Image? image;
    final grant = _grant(entry)!;
    bool allowed() => _grant(entry) != null && !entry.retired;
    try {
      requests++;
      bytes = await api.content(
        grant.cover,
        authorization: _authorization!,
        origin: _origin,
        current: allowed,
        abortTrigger: entry.abort!.future,
      );
      if (!allowed()) return;
      final target = math.min(160, (36 * entry.ratio).ceil());
      final scale = math.min(
        1.0,
        math.min(target / grant.cover.width, target / grant.cover.height),
      );
      decodes++;
      final codec = await ui.instantiateImageCodec(
        bytes,
        targetWidth: math.max(1, (grant.cover.width * scale).round()),
        targetHeight: math.max(1, (grant.cover.height * scale).round()),
        allowUpscaling: false,
      );
      try {
        image = (await codec.getNextFrame()).image;
      } finally {
        codec.dispose();
      }
      if (!allowed()) return;
      if (image.width > 160 || image.height > 160)
        throw const FormatException('地图图片尺寸不符');
      normal = await MapMarkerBitmap.render(
        kind: grant.pin.entity.kind,
        count: grant.pin.isCluster ? grant.pin.momentCount : 0,
        categoryCode: grant.pin.categoryCode,
        selected: false,
        devicePixelRatio: entry.ratio,
        photo: image,
      );
      if (!allowed()) return;
      selected = await MapMarkerBitmap.render(
        kind: grant.pin.entity.kind,
        count: grant.pin.isCluster ? grant.pin.momentCount : 0,
        categoryCode: grant.pin.categoryCode,
        selected: true,
        devicePixelRatio: entry.ratio,
        photo: image,
      );
      if (!allowed()) return;
      final used =
          image.width * image.height * 4 +
          (normal.length + selected.length) * 4;
      if (used > entry.reservation) throw const FormatException('地图图片预算超限');
      _reserved -= entry.reservation - used;
      entry.reservation = used;
      entry.image = image;
      entry.normal = normal;
      entry.selected = selected;
      image = null;
      normal = null;
      selected = null;
    } catch (_) {
      // The geographic anchor survives failed decoration. Only a fresh grant
      // retries; repeated build/sync calls do not create a transport loop.
    } finally {
      bytes?.fillRange(0, bytes.length, 0);
      normal?.fillRange(0, normal.length, 0);
      selected?.fillRange(0, selected.length, 0);
      image?.dispose();
      entry.running = false;
      entry.finished = true;
      if (entry.abort != null && !entry.abort!.isCompleted)
        entry.abort!.complete();
      entry.abort = null;
      _running--;
      if (entry.retired) _release(entry);
      _notify();
      _drain();
    }
  }

  void _retire(_OwnedMapPixels entry) {
    entry.retired = true;
    _entries.remove(entry.key);
    _queue.remove(entry.key);
    if (entry.abort != null && !entry.abort!.isCompleted)
      entry.abort!.complete();
    if (!entry.running) _release(entry);
  }

  void _release(_OwnedMapPixels entry) {
    if (entry.released) return;
    entry.released = true;
    final image = entry.image, normal = entry.normal, selected = entry.selected;
    entry.image = null;
    entry.normal = null;
    entry.selected = null;
    // Accounting frees the reservation immediately; actual UI image disposal
    // waits for the frame which removes invalid wrappers from the renderer.
    _reserved -= entry.reservation;
    void release() {
      normal?.fillRange(0, normal.length, 0);
      selected?.fillRange(0, selected.length, 0);
      image?.dispose();
    }

    if (image == null) {
      release();
    } else {
      SchedulerBinding.instance.addPostFrameCallback((_) => release());
    }
  }

  void suspend() {
    if (_closed) return;
    _latestView = null;
    _active.clear();
    _queue.clear();
    for (final entry in _entries.values.toList()) {
      if (entry.image == null) _retire(entry);
    }
    _notify();
  }

  /// A resource-level denial retires its active pixels, including shared cache
  /// entries, while independent geographic grants stay on the map.
  void retirePins(Set<String> mapIDs) {
    final entries = _active.entries
        .where((entry) => mapIDs.contains(entry.key))
        .map((entry) => entry.value.entry)
        .toSet();
    _active.removeWhere((_, grant) => entries.contains(grant.entry));
    for (final entry in entries) _retire(entry);
  }

  void retire() {
    if (_closed) return;
    _epoch++;
    _latestView = null;
    _active.clear();
    _queue.clear();
    for (final entry in _entries.values.toList()) _retire(entry);
    _notify();
  }

  void _notify() {
    if (_closed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_notifyingLater) return;
      _notifyingLater = true;
      SchedulerBinding.instance.addPostFrameCallback((_) {
        _notifyingLater = false;
        if (!_closed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    retire();
    _closed = true;
    super.dispose();
  }
}

class _ActivePixels {
  _ActivePixels(this.pin, this.cover, this.entry, this.until);
  final OwnedMapPin pin;
  final OwnedMapCover cover;
  final _OwnedMapPixels entry;
  final DateTime until;
  MapMediaPixels? pixels;
}

class _OwnedMapPixels {
  _OwnedMapPixels(this.key, this.ratio, this.epoch, this.reservation);
  final String key;
  final double ratio;
  final int epoch;
  int reservation;
  bool running = false, finished = false, retired = false, released = false;
  Completer<void>? abort;
  ui.Image? image;
  Uint8List? normal, selected;
}
