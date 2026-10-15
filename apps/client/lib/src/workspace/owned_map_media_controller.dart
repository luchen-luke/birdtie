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

/// Bounded authenticated pixels for this current personal map only. No URL,
/// bearer, grant selector or original bytes are given to a map provider.
class OwnedMapMediaController extends ChangeNotifier {
  OwnedMapMediaController({
    required this.api,
    required this.current,
    required this.authorizationHeader,
    required this.now,
    this.maxItems = 16,
    this.maxBytes = 24 * 1024 * 1024,
  });
  final OwnedMapAPI api;
  final bool Function() current;
  final String? Function() authorizationHeader;
  final DateTime Function() now;
  final int maxItems, maxBytes;
  final _entries = <String, _OwnedMapPixels>{};
  final _queue = <String>[];
  final _keys = <String, String>{};
  int _epoch = 0, _running = 0, _reserved = 0;
  bool _closed = false, _notifyingLater = false;
  String _origin = '';
  String? _authorization;
  OwnedMapView? _latestView;
  double _latestRatio = 1;
  int get inFlight => _running;
  int get cachedItems => _entries.length;

  void sync(OwnedMapView view, {required double devicePixelRatio}) {
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
    _latestRatio = devicePixelRatio;
    // A successful new grant read permits retry of failed pixels. Ordinary
    // build/sync calls never retry a failed transport in a frame loop.
    if (renewed) {
      for (final entry in _entries.values.toList()) {
        if (entry.finished && entry.pixels == null) _retire(entry);
      }
    }
    final ratio = MapMarkerBitmap.pixelRatio(devicePixelRatio);
    final selected = <String, String>{};
    for (final pin in view.items) {
      if (selected.length >= maxItems) break;
      final cover = pin.cover;
      if (cover == null) continue;
      final key =
          'owned:$_epoch:${pin.mapID}:${pin.sourceVersion}:${cover.identity}:$ratio';
      selected[pin.mapID] = key;
      final old = _entries[key];
      if (old != null) {
        old.until = view.validUntil;
        continue;
      }
      final edge = (MapMarkerBitmap.pointCanvasSize * ratio).ceil();
      // Raw transfers are capped separately at two times 16MiB by the API;
      // reservations cover decoded pixels and both marker/native rasters.
      final reservation = 320 * 180 * 4 + edge * edge * 4 * 4;
      if (_reserved + reservation > maxBytes) {
        selected.remove(pin.mapID);
        continue;
      }
      final entry = _OwnedMapPixels(
        key,
        pin,
        cover,
        view.validUntil,
        ratio,
        _epoch,
        reservation,
      );
      _entries[key] = entry;
      _reserved += reservation;
      _queue.add(key);
    }
    _keys
      ..clear()
      ..addAll(selected);
    for (final entry in _entries.values.toList()) {
      if (_keys[entry.pin.mapID] != entry.key) _retire(entry);
    }
    _drain();
  }

  bool _current(_OwnedMapPixels entry) =>
      !_closed &&
      !entry.retired &&
      current() &&
      entry.epoch == _epoch &&
      _entries[entry.key] == entry &&
      _keys[entry.pin.mapID] == entry.key &&
      entry.until.isAfter(now()) &&
      authorizationHeader() == _authorization &&
      canonicalAuthOrigin(api.apiBaseUrl()) == _origin;

  MapEntity decorate(OwnedMapPin pin) {
    final entry = _entries[_keys[pin.mapID]];
    return pin.entity.withMediaPixels(
      entry != null &&
              _current(entry) &&
              entry.pin.sourceVersion == pin.sourceVersion &&
              entry.cover.identity == pin.cover?.identity
          ? entry.pixels
          : null,
    );
  }

  void _drain() {
    while (!_closed && _running < 2 && _queue.isNotEmpty) {
      final entry = _entries[_queue.removeAt(0)];
      if (entry == null || !_current(entry) || entry.running || entry.finished)
        continue;
      entry.running = true;
      ++_running;
      unawaited(_load(entry));
    }
  }

  Future<void> _load(_OwnedMapPixels entry) async {
    Uint8List? bytes, normal, selected;
    ui.Image? image;
    try {
      bytes = await api.content(
        entry.cover,
        authorization: _authorization!,
        origin: _origin,
        current: () => _current(entry),
      );
      if (!_current(entry)) return;
      final scale = math.min(
        1.0,
        math.min(320 / entry.cover.width, 180 / entry.cover.height),
      );
      final codec = await ui.instantiateImageCodec(
        bytes,
        targetWidth: math.max(1, (entry.cover.width * scale).round()),
        targetHeight: math.max(1, (entry.cover.height * scale).round()),
        allowUpscaling: false,
      );
      try {
        image = (await codec.getNextFrame()).image;
      } finally {
        codec.dispose();
      }
      if (!_current(entry)) return;
      if (image.width > 320 || image.height > 180)
        throw const FormatException('地图图片尺寸不符');
      normal = await MapMarkerBitmap.render(
        kind: MapEntityKind.place,
        categoryCode: entry.pin.categoryCode,
        selected: false,
        devicePixelRatio: entry.ratio,
        photo: image,
      );
      if (!_current(entry)) return;
      selected = await MapMarkerBitmap.render(
        kind: MapEntityKind.place,
        categoryCode: entry.pin.categoryCode,
        selected: true,
        devicePixelRatio: entry.ratio,
        photo: image,
      );
      if (!_current(entry)) return;
      final used =
          image.width * image.height * 4 +
          (normal.length + selected.length) * 4;
      if (used > entry.reservation) throw const FormatException('地图图片预算超限');
      _reserved -= entry.reservation - used;
      entry.reservation = used;
      entry.pixels = MapMediaPixels(
        key: entry.key,
        image: image,
        normalPNG: normal,
        selectedPNG: selected,
        devicePixelRatio: entry.ratio,
        current: () => _current(entry),
      );
      image = null;
      normal = null;
      selected = null;
    } catch (_) {
      // Failed decoration keeps its authorized geographic point. Retry waits
      // for a successful new metadata grant read, never a rendering frame.
    } finally {
      bytes?.fillRange(0, bytes.length, 0);
      normal?.fillRange(0, normal.length, 0);
      selected?.fillRange(0, selected.length, 0);
      image?.dispose();
      entry.running = false;
      entry.finished = true;
      --_running;
      if (entry.retired || entry.pixels == null) _release(entry);
      _notify();
      _drain();
    }
  }

  void _retire(_OwnedMapPixels entry) {
    entry.retired = true;
    _entries.remove(entry.key);
    _queue.remove(entry.key);
    if (!entry.running) _release(entry);
  }

  void _release(_OwnedMapPixels entry) {
    if (entry.released) return;
    entry.released = true;
    final pixels = entry.pixels;
    entry.pixels = null;
    void release() {
      pixels?.normalPNG.fillRange(0, pixels.normalPNG.length, 0);
      pixels?.selectedPNG.fillRange(0, pixels.selectedPNG.length, 0);
      pixels?.image.dispose();
      _reserved -= entry.reservation;
    }

    if (pixels == null) {
      release();
    } else {
      SchedulerBinding.instance.addPostFrameCallback((_) {
        release();
        final view = _latestView;
        if (!_closed && current() && view != null) {
          sync(view, devicePixelRatio: _latestRatio);
          _notify();
        }
      });
    }
  }

  void retire() {
    if (_closed) return;
    ++_epoch;
    _latestView = null;
    _keys.clear();
    _queue.clear();
    for (final entry in _entries.values.toList()) {
      _retire(entry);
    }
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

class _OwnedMapPixels {
  _OwnedMapPixels(
    this.key,
    this.pin,
    this.cover,
    this.until,
    this.ratio,
    this.epoch,
    this.reservation,
  );
  final String key;
  final OwnedMapPin pin;
  final OwnedMapCover cover;
  DateTime until;
  final double ratio;
  final int epoch;
  int reservation;
  bool running = false, finished = false, retired = false, released = false;
  MapMediaPixels? pixels;
}
