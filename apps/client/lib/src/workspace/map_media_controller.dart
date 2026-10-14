import 'dart:async';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'entity_cover.dart';
import 'map_entities.dart';
import 'map_marker_bitmap.dart';
import 'map_marker_visual.dart';
import 'public_moment_photo.dart';

typedef MapMediaDecode = Future<ui.Image> Function(Uint8List bytes);

/// Shared bounded RAM cache for the map, current chooser and native bridge.
/// A photo failure retires its decoration, never the authorized point.
class MapMediaController extends ChangeNotifier {
  MapMediaController({
    http.Client? client,
    String? apiBaseUrl,
    DateTime Function()? now,
    MapMediaDecode? decode,
    this.maxItems = 24,
    this.maxBytes = 16 * 1024 * 1024,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       baseUrl = (apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).replaceFirst(
         RegExp(r'/$'),
         '',
       ),
       _now = now ?? (() => DateTime.now().toUtc()),
       _decode = decode ?? _decodeJPEG;
  http.Client _client;
  bool _ownsClient;
  String baseUrl;
  final DateTime Function() _now;
  final MapMediaDecode _decode;
  final int maxItems, maxBytes;
  final _entries = <String, _MapMediaEntry>{};
  final _queue = <String>[];
  final _photoClocks = <String, _MapAnnotationClock>{};
  final _countClocks = <String, _MapAnnotationClock>{};
  Map<String, String> _currentKeys = const {};
  Object? _namespace;
  String? _authorization;
  int _epoch = 0, _inFlight = 0, _reserved = 0;
  bool _closed = false, _notificationPending = false;
  Timer? _expiry;
  List<MapEntity> _source = const [];
  int get inFlight => _inFlight;
  int get cachedItems => _entries.length;
  int get reservedBytes => _reserved;
  int revision = 0;

  void replaceTransport({
    required http.Client client,
    required String apiBaseUrl,
  }) {
    final base = apiBaseUrl.replaceFirst(RegExp(r'/$'), '');
    if (_closed || (identical(client, _client) && baseUrl == base)) return;
    retire();
    if (_ownsClient) _client.close();
    _client = client;
    _ownsClient = false;
    baseUrl = base;
    _namespace = null;
  }

  static Future<ui.Image> _decodeJPEG(Uint8List bytes) async {
    final codec = await ui.instantiateImageCodec(
      bytes,
      targetWidth: 320,
      targetHeight: 180,
    );
    try {
      return (await codec.getNextFrame()).image;
    } finally {
      codec.dispose();
    }
  }

  /// Synchronous binding before rendering. The caller supplies actual identity,
  /// never an owner inferred from a bearer token. No point polling is scheduled.
  void sync({
    required String? ownerID,
    required String? workspaceID,
    required String? authorization,
    required String cityID,
    required Object sourceGeneration,
    required Iterable<MapEntity> entities,
    required double devicePixelRatio,
  }) {
    if (_closed) return;
    final namespace = (baseUrl, ownerID, workspaceID, cityID, sourceGeneration);
    if (_namespace != namespace) {
      _namespace = namespace;
      ++_epoch;
      _photoClocks.clear();
      _countClocks.clear();
      for (final entry in _entries.values.toList()) {
        _retire(entry);
      }
      _queue.clear();
    }
    _authorization = authorization;
    _source = List.unmodifiable(entities.take(300));
    final ratio = MapMarkerBitmap.pixelRatio(devicePixelRatio);
    final keys = <String, String>{};
    var selectedBudget = 0;
    final currentPhotoClocks = <String>{};
    final currentCounts = <String>{};
    for (final entity in _source) {
      final count = entity.recentMoments;
      if (entity.kind != MapEntityKind.place || count == null) continue;
      final key = '${entity.id}:${count.sourceKey}';
      currentCounts.add(key);
      _countClocks.putIfAbsent(
        key,
        () => _MapAnnotationClock(count.validUntil.difference(_now())),
      );
    }
    _countClocks.removeWhere((key, _) => !currentCounts.contains(key));
    // Signed-in pixels require a separately known current person namespace.
    final mayRead =
        authorization == null || (ownerID != null && entityCoverID(ownerID));
    for (final e in _source) {
      final p = _MapSourcePhoto.of(e);
      if (!mayRead || e.precision != 'point' || !e.hasMapAnchor || p == null)
        continue;
      final key =
          '$_epoch:$baseUrl:$ownerID:$workspaceID:$cityID:${e.id}:${e.sourceVersion}:${p.identity}:${p.byteCount}:${p.expiresAt.microsecondsSinceEpoch}:$ratio:${e.kind.name}:normal+selected';
      final clockKey =
          '$_epoch:${e.id}:${e.sourceVersion}:${p.identity}:${p.expiresAt.microsecondsSinceEpoch}';
      currentPhotoClocks.add(clockKey);
      final clock = _photoClocks.putIfAbsent(
        clockKey,
        () => _MapAnnotationClock(p.expiresAt.difference(_now())),
      );
      if (clock.expired || !p.expiresAt.isAfter(_now())) continue;
      // Account for input, decoded JPEG and both transient native rasters/PNG.
      // Reservations survive a retired pending decode until it is released.
      final canvasBytes = (MapMarkerBitmap.pointCanvasSize * ratio).ceil();
      final reservation =
          entityCoverMaxBytes +
          320 * 180 * 4 +
          canvasBytes * canvasBytes * 4 * 4;
      if (keys.length >= maxItems || selectedBudget + reservation > maxBytes)
        continue;
      selectedBudget += reservation;
      keys[e.id] = key;
      final old = _entries.remove(key);
      if (old != null) {
        _entries[key] = old;
        continue;
      }
      while (_entries.length >= maxItems ||
          _reserved + reservation > maxBytes) {
        final victims = _entries.values
            .where((x) => !x.running && !keys.containsValue(x.key))
            .toList();
        if (victims.isEmpty) break;
        _retire(victims.first);
      }
      if (_entries.length >= maxItems || _reserved + reservation > maxBytes)
        continue;
      final entry = _MapMediaEntry(
        key,
        e.id,
        p,
        ratio,
        _epoch,
        reservation,
        clock,
        e.sourceVersion,
        e.kind,
      );
      _reserved += reservation;
      _entries[key] = entry;
      _queue.add(key);
    }
    _currentKeys = Map.unmodifiable(keys);
    _photoClocks.removeWhere((key, _) => !currentPhotoClocks.contains(key));
    for (final entry in _entries.values.toList()) {
      if (_currentKeys[entry.entityID] != entry.key) _retire(entry);
    }
    _scheduleExpiry();
    _drain();
  }

  bool _current(_MapMediaEntry entry) =>
      !_closed &&
      !entry.retired &&
      entry.epoch == _epoch &&
      _entries[entry.key] == entry &&
      _currentKeys[entry.entityID] == entry.key &&
      !entry.clock.expired &&
      entry.photo.expiresAt.isAfter(_now());

  MapEntity decorate(MapEntity entity) {
    final entry = _entries[_currentKeys[entity.id]];
    final source = _MapSourcePhoto.of(entity);
    final matches =
        entry != null &&
        source != null &&
        source.sameSource(entry.photo) &&
        source.expiresAt == entry.photo.expiresAt &&
        entity.kind == entry.kind &&
        entity.sourceVersion == entry.sourceVersion;
    return entity.withMediaPixels(
      matches && _current(entry) ? entry.pixels : null,
      now: _now(),
      clearRecent:
          _countClocks['${entity.id}:${entity.recentMoments?.sourceKey}']
              ?.expired ==
          true,
    );
  }

  void _drain() {
    while (!_closed && _inFlight < 3 && _queue.isNotEmpty) {
      final entry = _entries[_queue.removeAt(0)];
      if (entry == null || !_current(entry) || entry.running || entry.finished)
        continue;
      entry.running = true;
      ++_inFlight;
      unawaited(_load(entry));
    }
  }

  Future<void> _load(_MapMediaEntry entry) async {
    ui.Image? image;
    try {
      final token = _authorization;
      final bytes = entry.photo.moment != null
          ? await PublicMomentPhotoAPI(
              client: _client,
              baseUrl: baseUrl,
              authorizationHeader: () => _authorization,
              current: () => _current(entry) && _authorization == token,
            ).content(entry.photo.moment!)
          : await EntityCoverAPI(
              client: _client,
              baseUrl: baseUrl,
              authorizationHeader: () => _authorization,
              current: () => _current(entry) && _authorization == token,
            ).content(entry.photo.cover!);
      if (!_current(entry) || token != _authorization) return;
      image = await _decode(bytes);
      if (!_current(entry) || token != _authorization) return;
      if (image.width != 320 || image.height != 180)
        throw const FormatException('地图照片尺寸不符');
      final normal = await MapMarkerBitmap.render(
        kind: entry.kind,
        selected: false,
        devicePixelRatio: entry.ratio,
        photo: image,
      );
      if (!_current(entry) || token != _authorization) return;
      final selected = await MapMarkerBitmap.render(
        kind: entry.kind,
        selected: true,
        devicePixelRatio: entry.ratio,
        photo: image,
      );
      if (!_current(entry) || token != _authorization) return;
      final used =
          image.width * image.height * 4 +
          (normal.length + selected.length) * 4;
      if (used > entry.reservation) throw const FormatException('地图照片内存预算不符');
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
    } catch (_) {
      // This source/version fails once. An unrelated camera frame cannot retry.
    } finally {
      image?.dispose();
      entry.running = false;
      entry.finished = true;
      if (entry.pixels == null && !entry.released) {
        _reserved -= entry.reservation;
        entry.reservation = 0;
      }
      --_inFlight;
      if (entry.retired) _release(entry);
      _notify();
      _drain();
    }
  }

  void _scheduleExpiry() {
    _expiry?.cancel();
    Duration? remaining;
    for (final e in _source) {
      for (final pair in [
        (_MapSourcePhoto.of(e)?.expiresAt, _entries[_currentKeys[e.id]]?.clock),
        (
          e.recentMoments?.validUntil,
          _countClocks['${e.id}:${e.recentMoments?.sourceKey}'],
        ),
      ]) {
        final end = pair.$1, clock = pair.$2;
        if (end == null || !end.isAfter(_now())) continue;
        var duration = end.difference(_now());
        if (clock?.expired == true) continue;
        if (clock != null && clock.remaining < duration)
          duration = clock.remaining;
        if (remaining == null || duration < remaining) remaining = duration;
      }
    }
    if (remaining == null) return;
    _expiry = Timer(remaining > Duration.zero ? remaining : Duration.zero, () {
      if (_closed) return;
      for (final entry in _entries.values.toList()) {
        if (entry.clock.expired || !entry.photo.expiresAt.isAfter(_now())) {
          _retire(entry);
        }
      }
      _notify();
      _scheduleExpiry();
    });
  }

  void _retire(_MapMediaEntry entry) {
    if (entry.retired) return;
    entry.retired = true;
    _entries.remove(entry.key);
    _queue.remove(entry.key);
    if (!entry.running) _release(entry);
  }

  void _release(_MapMediaEntry entry) {
    if (entry.released) return;
    entry.released = true;
    final image = entry.pixels?.image;
    entry.pixels = null;
    if (image == null) {
      _reserved -= entry.reservation;
    } else {
      // Old RawImage elements leave the render tree before disposing handles.
      SchedulerBinding.instance.addPostFrameCallback((_) {
        image.dispose();
        _reserved -= entry.reservation;
      });
    }
  }

  void _notify() {
    if (_closed) return;
    ++revision;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_notificationPending) return;
      _notificationPending = true;
      SchedulerBinding.instance.addPostFrameCallback((_) {
        _notificationPending = false;
        if (!_closed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  void retire() {
    ++_epoch;
    _expiry?.cancel();
    _currentKeys = const {};
    _source = const [];
    _countClocks.clear();
    _photoClocks.clear();
    _queue.clear();
    for (final entry in _entries.values.toList()) {
      _retire(entry);
    }
    _notify();
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    retire();
    if (_ownsClient) _client.close();
    super.dispose();
  }
}

class _MapAnnotationClock {
  _MapAnnotationClock(Duration duration)
    : duration = duration < const Duration(seconds: 60)
          ? duration
          : const Duration(seconds: 60),
      watch = Stopwatch()..start();
  final Duration duration;
  final Stopwatch watch;
  bool get expired => watch.elapsed >= duration;
  Duration get remaining => duration - watch.elapsed;
}

class _MapMediaEntry {
  _MapMediaEntry(
    this.key,
    this.entityID,
    this.photo,
    this.ratio,
    this.epoch,
    this.reservation,
    this.clock,
    this.sourceVersion,
    this.kind,
  );
  final String key, entityID;
  final String sourceVersion;
  final _MapSourcePhoto photo;
  final MapEntityKind kind;
  final double ratio;
  final int epoch;
  int reservation;
  final _MapAnnotationClock clock;
  bool running = false, finished = false, retired = false, released = false;
  MapMediaPixels? pixels;
}

/// The two real content readers keep their independent closed wire contracts.
/// This transient union shares only map memory/lease budgets and pixel work.
class _MapSourcePhoto {
  const _MapSourcePhoto({this.cover, this.moment});
  final EntityCoverMetadata? cover;
  final PublicMomentPhotoMetadata? moment;
  static _MapSourcePhoto? of(MapEntity e) {
    if (e.kind == MapEntityKind.place &&
        e.momentPhoto == null &&
        e.placePhoto != null &&
        e.placePhoto!.ref.type == 'place' &&
        e.id == 'place:${e.placePhoto!.ref.id}')
      return _MapSourcePhoto(cover: e.placePhoto);
    if (e.kind == MapEntityKind.moment &&
        e.placePhoto == null &&
        e.momentPhoto != null &&
        e.id == 'moment:${e.momentPhoto!.momentID}')
      return _MapSourcePhoto(moment: e.momentPhoto);
    return null;
  }

  DateTime get expiresAt => moment?.expiresAt ?? cover!.expiresAt;
  int get byteCount => moment?.byteCount ?? cover!.byteCount;
  String get identity => moment != null
      ? '${moment!.momentRevision}:${moment!.photoID}:${moment!.version}:${moment!.digest}'
      : '${cover!.ref.type}:${cover!.ref.id}:${cover!.version}:${cover!.digest}';
  bool sameSource(_MapSourcePhoto other) => moment != null
      ? other.moment != null && moment!.sameSource(other.moment!)
      : other.cover != null && cover!.sameSource(other.cover!);
}
