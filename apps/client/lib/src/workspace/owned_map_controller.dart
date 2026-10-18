import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'map_content_coverage.dart';
import 'dart:math' as math;
import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'package:http/http.dart' as http;
import '../auth/session_vault.dart';
import '../content/video_material_sink_stub.dart'
    if (dart.library.io) '../content/video_material_sink_io.dart'
    if (dart.library.html) '../content/video_material_sink_web.dart'
    as video_sink;
import 'human_causal_control.dart';
import 'map_entities.dart';
import 'map_media_read_metrics.dart';
import 'owned_map_api.dart';
import 'owned_map_media_controller.dart';

typedef _MapQuery = (String, MapBounds, double, String?);

/// Settled callbacks are already debounced by the provider. This controller
/// coalesces identical reads and cancels superseded transport without another
/// timer. Cached grants remain bounded by the server's original deadline.
class OwnedMapController extends ChangeNotifier with WidgetsBindingObserver {
  OwnedMapController({
    required http.Client client,
    required this.apiBaseUrl,
    required this.ownerID,
    required this.authorizationHeader,
    required this.organizationWorkspaceID,
    required this.identityChanges,
    required this.refreshAllowed,
    DateTime Function()? now,
    this.maxCachedViews = 8,
    this.maxCachedPins = 320,
    this.maxCachedBytes = 2 * 1024 * 1024,
    this.progressiveBatchPages = 4,
    this.maxActivePins = 2048,
    this.maxActiveBytes = 16 * 1024 * 1024,
  }) : _now = now ?? (() => DateTime.now().toUtc()) {
    _api = OwnedMapAPI(
      client: client,
      apiBaseUrl: apiBaseUrl,
      onDenied: _accessDenied,
    );
    _media = OwnedMapMediaController(
      api: _api,
      current: () => current,
      authorizationHeader: authorizationHeader,
      now: _now,
    )..addListener(_notify);
    _identity = _readIdentity();
    identityChanges.addListener(_identityChanged);
    WidgetsBinding.instance.addObserver(this);
  }
  final String Function() apiBaseUrl;
  final String? Function() ownerID,
      authorizationHeader,
      organizationWorkspaceID;
  final Listenable identityChanges;
  final bool Function() refreshAllowed;
  final DateTime Function() _now;
  final int maxCachedViews, maxCachedPins, maxCachedBytes;
  final int progressiveBatchPages, maxActivePins, maxActiveBytes;
  // Current pagination grants are not evicted by the opportunistic view LRU.
  // The finite budget stops new reads visibly; it never samples loaded points.
  final _activePages = <Object, OwnedMapView>{};
  MapBounds? _visibleBounds;
  int? _navigationGeneration;
  bool _activeBudgetReached = false;
  String? _paginationError;
  late OwnedMapAPI _api;
  late OwnedMapMediaController _media;
  (String, String?, String?, String?)? _identity;
  _MapQuery? _query;
  OwnedMapView? _view;
  Timer? _renew, _expiry;
  int _epoch = 0, _request = 0, _permissionEpoch = 0;
  double _ratio = 1;
  bool _closed = false, _foreground = true, _leaseExpired = false;
  bool _visibilityAllowed = false, _loading = false, _notifyingLater = false;
  String? _error;
  Future<void>? _flight;
  Completer<void>? _abort;
  final _cache = <Object, OwnedMapView>{};
  OwnedMapView? _activeUncached;
  final _requestCursorByView = Expando<String>();
  final _extraPins = <String, (OwnedMapPin, OwnedMapPin, DateTime)>{};
  final _pinEpoch = Expando<int>();
  final _entityGrants =
      Expando<({int epoch, Object? pixels, MapEntityReadGrant grant})>();
  final _invalidPins = Expando<bool>();
  final _resourceRechecks = <String>{};
  Timer? _recovery;
  int _transientRetries = 0;
  final _momentGrants = Expando<_OwnedMapGrant>();
  final _detailGrants = Expando<_OwnedMapGrant>();
  final _threadGrants = Expando<_OwnedMapGrant>();
  final _retiredThreads = Expando<bool>();
  final _detailThreads = Expando<OwnedMapThread>();
  final _momentThreads = Expando<OwnedMapThread>();
  final _detailPlaceReads = Expando<_OwnedMapPlaceRead>();
  final _momentPlaceReads = Expando<_OwnedMapPlaceRead>();
  final _coverChecks = Expando<bool Function()>();
  final _cursors = <String, (_OwnedMapGrant, String)>{};
  // Continuations locate a page; they do not authorize its content. Every
  // use still needs a fresh parent grant and a server ACL/source check.
  final _clusterCursors = <String, (OwnedMapPin, int, String, DateTime)>{};
  final _usedPageCursors = <(String, String?)>{};
  // Compatibility accessors: base points no longer have replacement pages.
  int get mapPageNumber => 1;
  bool get hasPreviousMapPage => false;
  bool get hasNextMapPage => !_activeBudgetReached && _continuation != null;
  bool get mapContentIncomplete => current && _view!.truncated;
  int get loadedPointCount => items.length;
  int requests = 0, cancellations = 0, cacheHits = 0, responseBytes = 0;
  int get epoch => _epoch;
  int get permissionEpoch => _permissionEpoch;
  String? get cityID => _query?.$1;
  int get cachedViews => _cache.length;
  int get cachedPins => _cache.values.fold(0, (n, v) => n + v.items.length);
  int get cachedBytes => _cache.values.fold(0, (n, v) => n + v.byteCount);
  int get cachedMediaItems => _media.cachedItems;
  int get mediaRequests => _media.requests;
  int get mediaDecodes => _media.decodes;
  int get mediaCacheHits => _media.cacheHits;
  int get mediaInFlightPeak => _media.inFlightPeak;
  int get mediaCachedBytes => _media.cachedBytes;

  (String, String?, String?, String?) _readIdentity() {
    var origin = '';
    try {
      origin = canonicalAuthOrigin(apiBaseUrl());
    } on FormatException {}
    return (
      origin,
      ownerID(),
      authorizationHeader(),
      organizationWorkspaceID(),
    );
  }

  bool get _allowed {
    if (_closed || !_foreground || !refreshAllowed()) return false;
    final value = _readIdentity();
    return value == _identity &&
        value.$1.isNotEmpty &&
        humanControlID(value.$2) &&
        value.$3 != null &&
        value.$3!.isNotEmpty &&
        value.$4 == null;
  }

  bool get current =>
      _allowed &&
      !_leaseExpired &&
      _view != null &&
      _view!.validUntil.isAfter(_now());
  bool get loading => _allowed && _loading;
  String? get error => _allowed ? _error : null;
  List<OwnedMapPin> get items => current ? _view!.items : const [];
  List<MapEntity> get entities => current
      ? List.unmodifiable(items.map(_decorateEntity).whereType<MapEntity>())
      : const [];
  MapEntity? _decorateEntity(OwnedMapPin pin) {
    final until = pin.validUntil;
    if (until == null || !until.isAfter(_now()) || !sourceCurrent(pin))
      return null;
    final entity = _media.decorate(pin);
    final epoch = _epoch, identity = _identity;
    var issued = _entityGrants[pin];
    if (issued == null ||
        issued.epoch != epoch ||
        !identical(issued.pixels, entity.mediaPixels)) {
      issued = (
        epoch: epoch,
        pixels: entity.mediaPixels,
        grant: MapEntityReadGrant(
          source: entity,
          validUntil: until,
          copiedPixels: entity.mediaPixels,
          issuerScope: (this, identity, _permissionEpoch),
          observedAt: pin.observedAt,
          current: () =>
              epoch == _epoch && identity == _identity && sourceCurrent(pin),
        ),
      );
      _entityGrants[pin] = issued;
    }
    return entity.withMediaPixels(entity.mediaPixels, readGrant: issued.grant);
  }

  OwnedMapView? get view => current ? _view : null;
  OwnedMapPin? find(String mapID) {
    if (!_allowed) return null;
    final direct = items.where((p) => p.mapID == mapID).firstOrNull;
    if (direct != null && sourceCurrent(direct)) return direct;
    final extra = _extraPins[mapID];
    if (extra != null && sourceCurrent(extra.$1)) return extra.$1;
    // A camera change is not a withdrawal of the selected reader's lease.
    // Only the explicit selection can be recovered outside the current page.
    if (mapID != 'owned-map:${_query?.$4}') return null;
    final candidates =
        _leasedViews
            .expand((v) => v.items)
            .where((p) => p.mapID == mapID && sourceCurrent(p))
            .toList()
          ..sort(
            (a, b) => (a.observedAt ?? DateTime(0)).compareTo(
              b.observedAt ?? DateTime(0),
            ),
          );
    return candidates.lastOrNull;
  }

  bool sourceCurrent(OwnedMapPin pin) {
    if (!_allowed || _pinEpoch[pin] != _epoch || _invalidPins[pin] == true)
      return false;
    if (_leasedViews.any(
      (v) =>
          v.validUntil.isAfter(_now()) &&
          v.items.any(
            (p) => p.mapID == pin.mapID && p.sourceVersion == pin.sourceVersion,
          ),
    )) {
      return true;
    }
    final extra = _extraPins[pin.mapID];
    return extra != null &&
        extra.$1.sourceVersion == pin.sourceVersion &&
        extra.$3.isAfter(_now()) &&
        sourceCurrent(extra.$2);
  }

  void _registerPin(OwnedMapPin pin) {
    _pinEpoch[pin] = _epoch;
    for (final moment in pin.moments)
      _momentGrants[moment] = _OwnedMapGrant(pin, _epoch, pin.validUntil);
    if (pin.cover != null) _coverChecks[pin.cover!] = () => sourceCurrent(pin);
  }

  void updateRefreshVisibility() {
    if (_closed) return;
    final allowed = _allowed;
    if (allowed == _visibilityAllowed) return;
    _visibilityAllowed = allowed;
    if (!allowed) {
      _retireView(preserveMedia: true);
    } else if (_query != null) {
      unawaited(refresh());
    }
  }

  bool _grantCurrent(_OwnedMapGrant? grant) =>
      grant != null &&
      grant.epoch == _epoch &&
      sourceCurrent(grant.pin) &&
      (grant.until == null || grant.until!.isAfter(_now()));

  bool momentCurrent(OwnedMapMoment target) =>
      _grantCurrent(_momentGrants[target]) &&
      _placeReadCurrent(_momentPlaceReads[target]) &&
      (_momentThreads[target] == null ||
          threadCurrent(_momentThreads[target]!));

  bool detailCurrent(OwnedMapMomentDetail detail) =>
      detail.validUntil.isAfter(_now()) &&
      _grantCurrent(_detailGrants[detail]) &&
      _placeReadCurrent(_detailPlaceReads[detail]) &&
      (_detailThreads[detail] == null ||
          threadCurrent(_detailThreads[detail]!));

  bool threadCurrent(OwnedMapThread thread) =>
      _retiredThreads[thread] != true &&
      thread.validUntil.isAfter(_now()) &&
      _grantCurrent(_threadGrants[thread]);

  void _retireThread(OwnedMapThread thread) {
    _retiredThreads[thread] = true;
    _notify();
  }

  bool _placeReadCurrent(_OwnedMapPlaceRead? read) =>
      read == null || !read.retired && read.page.validUntil.isAfter(_now());

  void _retirePlaceRead(_OwnedMapPlaceRead read) {
    read.retired = true;
    _notify();
  }

  bool _samePlaceContent(OwnedMapPlacePage a, OwnedMapPlacePage b) {
    if (a.placeID != b.placeID ||
        a.sourceVersion != b.sourceVersion ||
        a.items.length != b.items.length)
      return false;
    for (var i = 0; i < a.items.length; i++) {
      final x = a.items[i], y = b.items[i];
      if (x.id != y.id ||
          x.revision != y.revision ||
          x.visibilitySource != y.visibilitySource ||
          x.sourceVersion != y.sourceVersion ||
          !listEquals(
            x.media.map((m) => m.identity).toList(),
            y.media.map((m) => m.identity).toList(),
          ))
        return false;
    }
    return true;
  }

  bool _sameThreadContent(OwnedMapThread a, OwnedMapThread b) {
    if (a.threadID != b.threadID || a.entries.length != b.entries.length)
      return false;
    for (var i = 0; i < a.entries.length; i++) {
      final x = a.entries[i], y = b.entries[i];
      if (x.position != y.position ||
          x.available != y.available ||
          x.detail?.id != y.detail?.id ||
          x.detail?.revision != y.detail?.revision ||
          x.detail?.sourceVersion != y.detail?.sourceVersion)
        return false;
    }
    return true;
  }

  OwnedMapThread? threadForDetail(OwnedMapMomentDetail detail) {
    final thread = _detailThreads[detail];
    return thread != null && threadCurrent(thread) && detailCurrent(detail)
        ? thread
        : null;
  }

  void _registerThread(OwnedMapThread thread, OwnedMapPin pin) {
    _threadGrants[thread] = _OwnedMapGrant(pin, _epoch, thread.validUntil);
    for (final entry in thread.entries) {
      if (entry.detail != null) {
        _registerDetail(entry.detail!, pin, thread: thread);
      }
    }
  }

  void _registerDetail(
    OwnedMapMomentDetail detail,
    OwnedMapPin pin, {
    OwnedMapThread? thread,
  }) {
    final until =
        thread != null && thread.validUntil.isBefore(detail.validUntil)
        ? thread.validUntil
        : detail.validUntil;
    final grant = _OwnedMapGrant(pin, _epoch, until);
    _detailGrants[detail] = grant;
    _momentGrants[detail.selector] = grant;
    if (thread != null) {
      _detailThreads[detail] = thread;
      _momentThreads[detail.selector] = thread;
    }
    for (final media in detail.media) {
      if (media.poster != null)
        _coverChecks[media.poster!] = () => detailCurrent(detail);
    }
  }

  Future<OwnedMapMomentDetail> readMoment(
    OwnedMapMoment target, {
    bool Function()? current,
    Future<void>? abortTrigger,
  }) async {
    final grant = _momentGrants[target], identity = _identity, epoch = _epoch;
    bool pending() =>
        epoch == _epoch &&
        identity == _identity &&
        (current?.call() ?? true) &&
        momentCurrent(target);
    if (!pending() || grant == null || identity == null)
      throw const FormatException('地图动态读取已失效');
    final originalThread = _momentThreads[target];
    if (originalThread != null) {
      OwnedMapThread? renewed;
      try {
        renewed = await _api.readThread(
          target.id,
          ownerID: identity.$2!,
          authorization: identity.$3!,
          origin: identity.$1,
          current: pending,
          now: _now,
          abortTrigger: abortTrigger,
        );
      } on OwnedMapReadFailure catch (failure) {
        if (failure.retiresGrant) _retireThread(originalThread);
        rethrow;
      }
      if (!pending() ||
          renewed == null ||
          !_sameThreadContent(originalThread, renewed)) {
        _retireThread(originalThread);
        throw const FormatException('地图串文已变化');
      }
      final selected = renewed.entries
          .map((entry) => entry.detail)
          .whereType<OwnedMapMomentDetail>()
          .where((detail) => detail.id == target.id)
          .firstOrNull;
      if (selected == null ||
          selected.revision != target.revision ||
          selected.visibilitySource != target.visibilitySource) {
        throw const FormatException('地图串文动态来源已变化');
      }
      // Renew the actual relation and its sibling grants together. A single
      // moment read has a different snapshot source and cannot renew a thread.
      _registerThread(renewed, grant.pin);
      return selected;
    }
    final originalPlace = _momentPlaceReads[target];
    if (originalPlace != null) {
      OwnedMapPlacePage renewed;
      try {
        // A place list's detail source includes its whole place snapshot. A
        // single-moment GET has a different source even with unchanged media.
        // Reauthorize the exact original page without guessing a thread.
        renewed = await _api.readPlaceMoments(
          originalPlace.page.placeID,
          cursor: originalPlace.cursor,
          ownerID: identity.$2!,
          authorization: identity.$3!,
          origin: identity.$1,
          current: pending,
          now: _now,
          abortTrigger: abortTrigger,
        );
      } on OwnedMapReadFailure catch (failure) {
        if (failure.retiresGrant) _retirePlaceRead(originalPlace);
        rethrow;
      } on FormatException {
        _retirePlaceRead(originalPlace);
        rethrow;
      }
      if (!pending() ||
          !_samePlaceContent(originalPlace.page, renewed) ||
          originalPlace.cursor != null &&
              renewed.validUntil.isAfter(originalPlace.page.validUntil)) {
        _retirePlaceRead(originalPlace);
        throw const FormatException('地点动态来源已变化，请重新读取');
      }
      final selected = renewed.items
          .where((detail) => detail.id == target.id)
          .singleOrNull;
      if (selected == null ||
          selected.revision != target.revision ||
          selected.visibilitySource != target.visibilitySource) {
        _retirePlaceRead(originalPlace);
        throw const FormatException('地点动态读取目标已变化');
      }
      _registerPlacePage(renewed, grant.pin, originalPlace.cursor);
      return selected;
    }
    final value = await _api.readMoment(
      target,
      ownerID: identity.$2!,
      authorization: identity.$3!,
      origin: identity.$1,
      current: pending,
      now: _now,
      abortTrigger: abortTrigger,
    );
    if (!pending()) throw const FormatException('地图动态读取已失效');
    _registerDetail(value, grant.pin, thread: _momentThreads[target]);
    return value;
  }

  Future<OwnedMapThread?> readThread(
    OwnedMapPin pin,
    OwnedMapMomentDetail seed,
  ) async {
    final identity = _identity, epoch = _epoch;
    bool pending() =>
        epoch == _epoch &&
        identity == _identity &&
        identity != null &&
        sourceCurrent(pin) &&
        detailCurrent(seed) &&
        _detailGrants[seed]?.pin.mapID == pin.mapID &&
        _detailGrants[seed]?.pin.sourceVersion == pin.sourceVersion;
    if (!pending() || identity == null) {
      throw const FormatException('地图串文读取已失效');
    }
    final thread = await _api.readThread(
      seed.id,
      ownerID: identity.$2!,
      authorization: identity.$3!,
      origin: identity.$1,
      current: pending,
      now: _now,
    );
    if (!pending()) throw const FormatException('地图串文读取已失效');
    if (thread == null) return null;
    if (thread.seedRevision != seed.revision ||
        thread.seedVisibilitySource != seed.visibilitySource ||
        !thread.validUntil.isAfter(_now())) {
      throw const FormatException('地图串文入口已变化');
    }
    _registerThread(thread, pin);
    return thread;
  }

  Future<OwnedMapPlacePage> readPlaceMoments(
    OwnedMapPin pin, {
    String? cursor,
    String? placeID,
    OwnedMapMomentDetail? relationSource,
  }) async {
    final targetPlace = placeID ?? pin.placeID;
    bool relationCurrent() =>
        placeID == null ||
        relationSource != null &&
            detailCurrent(relationSource) &&
            relationSource.locations.any((p) => p.placeID == targetPlace);
    final identity = _identity, epoch = _epoch;
    final previous = cursor == null ? null : _cursors[cursor];
    bool pending() =>
        epoch == _epoch &&
        identity == _identity &&
        sourceCurrent(pin) &&
        relationCurrent() &&
        targetPlace.isNotEmpty &&
        (cursor == null ||
            previous != null &&
                _grantCurrent(previous.$1) &&
                previous.$1.pin.mapID == pin.mapID &&
                previous.$1.pin.sourceVersion == pin.sourceVersion);
    if (!pending() || identity == null)
      throw const FormatException('地点动态读取已失效，请重新读取');
    final value = await _api.readPlaceMoments(
      targetPlace,
      cursor: cursor,
      ownerID: identity.$2!,
      authorization: identity.$3!,
      origin: identity.$1,
      current: pending,
      now: _now,
    );
    if (!pending() || previous != null && value.sourceVersion != previous.$2) {
      throw const FormatException('地点动态来源已变化，请重新读取');
    }
    _registerPlacePage(value, pin, cursor);
    return value;
  }

  void _registerPlacePage(
    OwnedMapPlacePage value,
    OwnedMapPin pin,
    String? cursor,
  ) {
    final read = _OwnedMapPlaceRead(value, cursor);
    for (final detail in value.items) {
      _registerDetail(detail, pin);
      _detailPlaceReads[detail] = read;
      _momentPlaceReads[detail.selector] = read;
    }
    _cursors.removeWhere((_, v) => !_grantCurrent(v.$1));
    if (value.nextCursor != null) {
      if (_cursors.length >= 128) _cursors.remove(_cursors.keys.first);
      _cursors[value.nextCursor!] = (
        _OwnedMapGrant(pin, _epoch, value.validUntil),
        value.sourceVersion,
      );
    }
  }

  Future<Uint8List> momentContent(
    OwnedMapMomentDetail detail,
    OwnedMapMomentMedia media, {
    bool Function()? current,
  }) async {
    final identity = _identity, epoch = _epoch;
    bool pending() =>
        epoch == _epoch &&
        identity == _identity &&
        (current?.call() ?? true) &&
        detailCurrent(detail) &&
        detail.media.any((item) => identical(item, media));
    if (!pending() || identity == null)
      throw const FormatException('地图媒体读取已失效');
    return _api.momentContent(
      media,
      detail: detail,
      now: _now,
      authorization: identity.$3!,
      origin: identity.$1,
      current: pending,
    );
  }

  DateTime contentValidUntil(Uint8List bytes, OwnedMapMomentDetail detail) =>
      _api.contentValidUntil(bytes) ?? detail.validUntil;
  MapMediaReadTrace? contentTrace(Uint8List bytes) => _api.contentTrace(bytes);

  Future<String> prepareVideo(
    OwnedMapMomentDetail detail,
    OwnedMapMomentMedia media, {
    required bool Function() current,
    required MapMediaReadTrace trace,
    Future<bool> Function(OwnedMapMomentDetail fresh)? onFreshDetail,
    Future<void>? abortTrigger,
  }) async {
    final identity = _identity, epoch = _epoch;
    var active = detail;
    var finished = false;
    Object? stopped;
    final abort = Completer<void>();
    Timer? renewalTimer, absoluteTimer;
    Future<void>? renewal;
    bool pending() =>
        !finished &&
        stopped == null &&
        epoch == _epoch &&
        identity == _identity &&
        current() &&
        detailCurrent(active) &&
        active.media.any((m) => m.identity == media.identity);
    if (!pending() || identity == null || !media.isVideo) {
      throw const FormatException('地图视频读取已失效');
    }
    void stop(Object reason) {
      if (finished || stopped != null) return;
      stopped = reason;
      renewalTimer?.cancel();
      if (!abort.isCompleted) abort.complete();
    }

    Future<T> guarded<T>(Future<T> job) => Future.any<T>([
      job,
      abort.future.then<T>(
        (_) => throw stopped ?? const FormatException('地图视频读取已失效'),
      ),
    ]);

    void changed() {
      if (!pending()) stop(const FormatException('地图视频读取已失效'));
    }

    late void Function() armRenewal;
    Future<void> renewAuthority() {
      if (renewal != null) return renewal!;
      final done = Completer<void>();
      renewal = done.future;
      unawaited(() async {
        try {
          if (!pending()) throw const FormatException('地图视频读取已失效');
          trace.requests++;
          // readMoment preserves the exact thread or original place+cursor
          // context. A cursor's original expiry is deliberately not extended.
          final fresh = await guarded(
            readMoment(
              active.selector,
              current: pending,
              abortTrigger: abort.future,
            ),
          );
          if (!pending()) throw const FormatException('地图视频读取已失效');
          if (fresh.id != detail.id ||
              fresh.revision != detail.revision ||
              fresh.visibilitySource != detail.visibilitySource ||
              fresh.sourceVersion != detail.sourceVersion ||
              !listEquals(
                fresh.media.map((m) => m.identity).toList(),
                detail.media.map((m) => m.identity).toList(),
              )) {
            _detailGrants[active] = null;
            _momentGrants[active.selector] = null;
            final thread = _detailThreads[active];
            if (thread != null) _retireThread(thread);
            final place = _detailPlaceReads[active];
            if (place != null) _retirePlaceRead(place);
            _notify();
            throw const FormatException('地图视频来源已变化');
          }
          // Replace only this transfer's authority reference. The old detail,
          // media/cache descriptors and their deadlines remain untouched.
          active = fresh;
          if (onFreshDetail != null && !await guarded(onFreshDetail(fresh))) {
            throw const FormatException('地图视频读取已失效');
          }
          if (!pending()) throw const FormatException('地图视频读取已失效');
          armRenewal();
          done.complete();
        } catch (error, stack) {
          stop(error);
          done.completeError(error, stack);
        } finally {
          if (identical(renewal, done.future)) renewal = null;
        }
      }());
      return done.future;
    }

    armRenewal = () {
      renewalTimer?.cancel();
      final remaining = active.validUntil.difference(_now());
      if (!pending() || remaining <= Duration.zero) {
        stop(const FormatException('地图视频读取已失效'));
        return;
      }
      final half = remaining ~/ 2;
      renewalTimer = Timer(
        half > const Duration(seconds: 10) ? const Duration(seconds: 10) : half,
        () => unawaited(renewAuthority().catchError((Object _) {})),
      );
    };
    final sink = await video_sink.VideoMaterialSink.create(
      limit: media.byteCount,
    );
    addListener(changed);
    abortTrigger?.then((_) => stop(const FormatException('视频读取已取消')));
    var handedOff = false;
    try {
      // One absolute budget includes headers, metadata renewals, UI adoption,
      // the complete transfer and final proof. No renewal restarts this timer.
      absoluteTimer = Timer(
        const Duration(seconds: 110),
        () =>
            stop(TimeoutException('视频读取总预算已用尽', const Duration(seconds: 110))),
      );
      if (!pending()) throw const FormatException('地图视频读取已失效');
      armRenewal();
      await _api.transferVideo(
        media,
        detail: detail,
        now: _now,
        authorization: identity.$3!,
        origin: identity.$1,
        current: pending,
        authorizedDetail: () => active,
        abortTrigger: abort.future,
        receive: (bytes) async {
          if (active.validUntil.difference(_now()) <=
              const Duration(seconds: 15)) {
            await renewAuthority();
          }
          if (!pending()) throw const FormatException('地图视频读取已失效');
          await sink.add(bytes);
        },
        trace: trace,
      );
      // Hash success alone never publishes a path. Recheck the same authority
      // once more after the final byte and before the sink can be handed off.
      renewalTimer?.cancel();
      final earlier = renewal;
      if (earlier != null) await guarded(earlier);
      renewalTimer?.cancel();
      await guarded(renewAuthority());
      if (!pending()) throw const FormatException('地图视频读取已失效');
      final path = await guarded(sink.finish());
      if (!pending()) throw const FormatException('地图视频读取已失效');
      trace.mark(MapMediaReadStage.materialReady);
      handedOff = true;
      return path;
    } finally {
      finished = true;
      renewalTimer?.cancel();
      absoluteTimer?.cancel();
      removeListener(changed);
      if (!abort.isCompleted) abort.complete();
      if (!handedOff) await sink.discard();
    }
  }

  Future<Uint8List> coverContent(
    OwnedMapCover cover, {
    required bool Function() current,
  }) async {
    final identity = _identity, epoch = _epoch, grant = _coverChecks[cover];
    bool pending() =>
        epoch == _epoch &&
        identity == _identity &&
        grant != null &&
        grant() &&
        current();
    if (!pending() || identity == null)
      throw const FormatException('地图图片读取已失效');
    return _api.content(
      cover,
      authorization: identity.$3!,
      origin: identity.$1,
      current: pending,
    );
  }

  Future<Uint8List> authorAvatarContent(OwnedMapMomentDetail detail) async {
    final identity = _identity, epoch = _epoch, avatar = detail.author?.avatar;
    bool pending() =>
        epoch == _epoch &&
        identity == _identity &&
        detailCurrent(detail) &&
        identical(detail.author?.avatar, avatar);
    if (!pending() || identity == null || avatar == null) {
      throw const FormatException('作者头像读取已失效');
    }
    return _api.avatarContent(
      avatar,
      authorization: identity.$3!,
      origin: identity.$1,
      current: pending,
    );
  }

  /// Small snapped prefetch cells bound incidental camera jitter; no city
  /// catalogue or external lookup participates. The query's exact bounds are
  /// also the permission-bound cache key and response decoder expectation.
  static MapBounds queryBounds(MapBounds bounds, double zoom) {
    if (!bounds.isValid) return bounds;
    final step = 360 / math.pow(2, zoom.floor().clamp(0, 22) + 5);
    double wrap(double v) => (v + 180) % 360 - 180;
    final west = (bounds.west / step).floor() * step;
    final end = bounds.west + bounds.longitudeSpan;
    final east = (end / step).ceil() * step;
    return MapBounds(
      west: east - west >= 360 ? -180 : wrap(west),
      east: east - west >= 360 ? 180 : wrap(east),
      south: ((bounds.south / step).floor() * step).clamp(-90, 90),
      north: ((bounds.north / step).ceil() * step).clamp(-90, 90),
    );
  }

  void updateQuery(
    String cityID,
    MapBounds bounds, {
    double zoom = 12,
    String? selectedMarkerID,
  }) {
    if (_closed) return;
    final identity = _readIdentity();
    if (identity != _identity) {
      retire();
      _identity = identity;
    }
    if (!bounds.isValid || !zoom.isFinite || zoom < 0 || zoom > 22) {
      retire(clearQuery: true);
      return;
    }
    var selected = selectedMarkerID;
    if (selected?.startsWith('owned-map:') == true)
      selected = selected!.substring(10);
    if (selected != null &&
        !RegExp(
          r'^((place|anchor):[0-9a-f-]{36}|cluster:[0-9a-f]{64})$',
        ).hasMatch(selected))
      selected = null;
    final query = (
      cityID,
      queryBounds(bounds, zoom),
      zoom.floorToDouble(),
      selected,
    );
    final previousSelected = _query?.$4;
    final changed = query != _query;
    if (changed) {
      if (_query != null && _query!.$1 != cityID) {
        _retireView(preserveMedia: true);
      }
      _cancelRequest();
      _paginationError = null;
      _activeBudgetReached = false;
      _query = query;
      // A committed new area can release previous offscreen page grants. A
      // passive camera movement never enters this path or evicts loaded IDs.
      _activePages.removeWhere(
        (_, page) =>
            !MapContentCoverage.intersects(page.bounds, query.$2) &&
            !page.items.any((p) => p.canonicalMarkerID == selected),
      );
    }
    if (!_allowed) {
      _retireView(preserveMedia: true);
      return;
    }
    _pruneCache();
    final missing = MapContentCoverage.missing(
      query.$2,
      _coverageViews.map((v) => v.bounds),
    );
    if (missing.isEmpty && changed) cacheHits++;
    for (final key in _cache.keys.toList()) {
      final value = _cache[key]!;
      if (value.cityID == query.$1 &&
          value.zoom == query.$3 &&
          MapContentCoverage.intersects(value.bounds, query.$2)) {
        _cache.remove(key);
        _cache[key] = value;
      }
    }
    _publishCoverage();
    if (missing.isNotEmpty && !_loading)
      unawaited(_loadRegions(missing, pad: true));
    else if (selected != null &&
        selected != previousSelected &&
        items.any((p) => p.canonicalMarkerID == selected && p.cover == null)) {
      unawaited(refresh());
    }
  }

  Iterable<OwnedMapView> get _leasedViews => [
    ...{..._cache.values, ..._activePages.values},
    if (_activeUncached != null) _activeUncached!,
  ].where((v) => v.cityID == _query?.$1 && v.validUntil.isAfter(_now()));

  // A geographic rectangle is only coverage for its issued zoom. Other zoom
  // pages remain bounded read grants, never evidence for the current viewport.
  Iterable<OwnedMapView> get _coverageViews =>
      _leasedViews.where((v) => v.zoom == _query?.$3);

  // One latest selected source may survive LRU pressure. It remains inside
  // the same byte/pin/view budgets and keeps its original server deadline.
  OwnedMapView? get _selectedCoverage {
    final selected = _query?.$4;
    if (selected == null) return null;
    final extra = _extraPins['owned-map:$selected'];
    final parentID = extra?.$2.canonicalMarkerID;
    OwnedMapView? latest;
    for (final value in _leasedViews) {
      if (value.items.any(
            (p) =>
                _invalidPins[p] != true &&
                (p.canonicalMarkerID == selected ||
                    p.canonicalMarkerID == parentID &&
                        p.sourceVersion == extra?.$2.sourceVersion),
          ) &&
          (latest == null || !value.observedAt.isBefore(latest.observedAt))) {
        latest = value;
      }
    }
    return latest;
  }

  bool _selectedFrom(OwnedMapView value) => identical(value, _selectedCoverage);

  List<OwnedMapPin> _visibleCachedPins() {
    final query = _query;
    if (query == null) return [];
    final pins = <String, OwnedMapPin>{};
    final views = _coverageViews.toList()
      ..sort((a, b) => a.observedAt.compareTo(b.observedAt));
    for (final v in views) {
      for (final p in v.items) {
        if (_invalidPins[p] != true) {
          pins[p.mapID] = p;
        }
      }
    }
    // A new zoom needs its own coverage, but a pending read is not evidence
    // that already loaded objects disappeared. Keep a bounded display baseline
    // under each original source lease. Only the same stable identity can
    // replace its old representation; coincident independent objects survive.
    final currentIDs = pins.keys.toSet();
    final baseline = _leasedViews.where((v) => v.zoom != query.$3).toList()
      ..sort((a, b) => a.observedAt.compareTo(b.observedAt));
    final baselinePins = <String, OwnedMapPin>{};
    for (final v in baseline) {
      for (final p in v.items) {
        final id = p.mapID;
        if (!sourceCurrent(p) ||
            p.validUntil?.isAfter(_now()) != true ||
            currentIDs.contains(id) ||
            pins.containsKey(p.mapID))
          continue;
        // A last pagination page alone cannot prove absence in its rectangle.
        // Only a complete single-page read supplies that negative evidence.
        final absent = views.any(
          (read) =>
              !read.observedAt.isBefore(v.observedAt) &&
              _requestCursorByView[read] == null &&
              read.nextCursor == null &&
              !read.truncated &&
              read.bounds.contains(p.latitude, p.longitude),
        );
        if (!absent) baselinePins[id] = p;
      }
    }
    for (final p in baselinePins.values) pins.putIfAbsent(p.mapID, () => p);
    // Preserve a selected reader under its own original identity and lease.
    final selectedView = _selectedCoverage;
    if (selectedView != null && selectedView.zoom != query.$3) {
      for (final p in selectedView.items) {
        if (sourceCurrent(p) &&
            p.canonicalMarkerID == query.$4 &&
            !pins.containsKey(p.mapID)) {
          pins.putIfAbsent(p.mapID, () => p);
        }
      }
    }
    return pins.values.toList()..sort(OwnedMapPin.newestFirst);
  }

  OwnedMapView? get _continuation {
    final candidates = _coverageViews
        .where((v) => MapContentCoverage.intersects(v.bounds, _query!.$2))
        .toList();
    final roots =
        candidates.where((v) => _requestCursorByView[v] == null).toList()
          ..sort((a, b) => b.observedAt.compareTo(a.observedAt));
    final regions = <(MapBounds, double)>{};
    for (final root in roots) {
      // Older snapshots are finite display baselines. Only the latest root
      // for this exact region can authorize a continuation, regardless of LRU
      // ordering or which obsolete tail still has an unused cursor.
      if (!regions.add((root.bounds, root.zoom))) continue;
      final chain = candidates
          .where(
            (v) =>
                v.bounds == root.bounds &&
                v.zoom == root.zoom &&
                v.sourceVersion == root.sourceVersion,
          )
          .toList();
      final seen = <String>{};
      var page = root;
      while (true) {
        final next = page.nextCursor;
        if (next == null || !seen.add(next)) break;
        final child = chain
            .where((v) => _requestCursorByView[v] == next)
            .firstOrNull;
        if (child == null) {
          // A live parent may reauthorize an expired missing child. A used
          // cursor alone proves neither a still-readable child nor coverage.
          if (page.viewportPath.isNotEmpty) return page;
          break;
        }
        page = child;
      }
    }
    return null;
  }

  List<OwnedMapView>? _completePageChain(OwnedMapView page) {
    final candidates = _leasedViews
        .where(
          (v) =>
              v.bounds == page.bounds &&
              v.zoom == page.zoom &&
              v.sourceVersion == page.sourceVersion,
        )
        .toList();
    final root = candidates
        .where((v) => _requestCursorByView[v] == null)
        .firstOrNull;
    if (root == null) return null;
    final chain = <OwnedMapView>[], seenCursors = <String>{};
    var current = root;
    while (true) {
      if (current.rejectedItemCount > 0 ||
          current.totalPointCount != root.totalPointCount)
        return null;
      chain.add(current);
      final next = current.nextCursor;
      if (next == null) {
        if (current.truncated) return null;
        break;
      }
      if (!seenCursors.add(next)) return null;
      final child = candidates
          .where((v) => _requestCursorByView[v] == next)
          .firstOrNull;
      if (child == null) return null;
      current = child;
    }
    // The connected root-to-tail proof also checks distinct point cardinality.
    // Neither a terminal page nor a transport/label budget implies completion.
    final ids = {
      for (final v in chain)
        for (final p in v.items)
          if (_invalidPins[p] != true) p.mapID,
    };
    return ids.length == root.totalPointCount ? chain : null;
  }

  bool get _hasUnfinishedPageChain {
    final seen = <(MapBounds, double, String)>{};
    for (final page in _coverageViews) {
      if (MapContentCoverage.intersects(page.bounds, _query!.$2) &&
          seen.add((page.bounds, page.zoom, page.sourceVersion)) &&
          _completePageChain(page) == null)
        return true;
    }
    return false;
  }

  void _retireAbsentFromCompleteChain(OwnedMapView page) {
    final chain = _completePageChain(page);
    if (chain == null) return;
    final ids = {
      for (final v in chain)
        for (final p in v.items) p.mapID,
    };
    final earliest = chain
        .map((v) => v.observedAt)
        .reduce((a, b) => a.isBefore(b) ? a : b);
    for (final old in _leasedViews) {
      if (old.sourceVersion == page.sourceVersion ||
          old.zoom != page.zoom ||
          old.observedAt.isAfter(earliest))
        continue;
      for (final pin in old.items) {
        if (page.bounds.contains(pin.latitude, pin.longitude) &&
            !ids.contains(pin.mapID)) {
          _invalidPins[pin] = true;
        }
      }
    }
    bool superseded(Object _, OwnedMapView old) =>
        old.sourceVersion != page.sourceVersion &&
        old.zoom == page.zoom &&
        !old.observedAt.isAfter(earliest) &&
        MapContentCoverage.missing(old.bounds, [page.bounds]).isEmpty;
    _cache.removeWhere(superseded);
    _activePages.removeWhere(superseded);
    if (_activeUncached != null && superseded('', _activeUncached!))
      _activeUncached = null;
  }

  void _publishCoverage() {
    final query = _query;
    if (query == null || !_allowed) return;
    final views = _coverageViews
        .where(
          (v) =>
              MapContentCoverage.intersects(v.bounds, query.$2) ||
              _selectedFrom(v),
        )
        .toList();
    final pins = _visibleCachedPins();
    for (final baseline in _leasedViews) {
      if (!views.contains(baseline) &&
          pins.any((pin) => baseline.items.any((p) => identical(pin, p))))
        views.add(baseline);
    }
    final selectedView = _selectedCoverage;
    if (selectedView != null && !views.contains(selectedView)) {
      views.add(selectedView);
    }
    if (views.isEmpty) {
      _view = null;
      _renew?.cancel();
      _expiry?.cancel();
      _media.suspend();
      _notify();
      return;
    }
    final page = pins;
    final contributors = views
        .where(
          (v) => page.any(
            (pin) => v.items.any((candidate) => identical(pin, candidate)),
          ),
        )
        .toList();
    // Only displayed/readable page entries receive this composed deadline.
    // An obsolete overlapping rectangle that contributed no selected pin must
    // not truncate a fresh pixel grant. Every included pin keeps its own lease.
    final until = (contributors.isEmpty ? views : contributors)
        .map((v) => v.validUntil)
        .reduce((a, b) => a.isBefore(b) ? a : b);
    final observed = views
        .map((v) => v.observedAt)
        .reduce((a, b) => a.isAfter(b) ? a : b);
    final versions = views.map((v) => v.sourceVersion).toSet().toList()..sort();
    final next = _continuation;
    _apply(
      OwnedMapView(
        viewerAccountID: _identity!.$2!,
        cityID: query.$1,
        bounds: query.$2,
        observedAt: observed,
        validUntil: until,
        sourceVersion: sha256
            .convert(utf8.encode(versions.join(':')))
            .toString(),
        items: List.unmodifiable(page),
        zoom: query.$3,
        truncated:
            next != null ||
            _paginationError != null ||
            _activeBudgetReached ||
            _hasUnfinishedPageChain,
        // These composed counts describe loaded objects only. Regional server
        // totals cannot be added across overlapping, paginated coverage.
        totalContentCount: pins
            .expand((p) => p.moments)
            .map((m) => m.id)
            .toSet()
            .length,
        totalAnchorCount: pins.fold(0, (n, p) => n + p.anchorCount),
        totalPointCount: pins.length,
        nextCursor: next?.nextCursor,
        viewportPath: next?.viewportPath ?? '',
        rejectedItemCount: views.fold(0, (n, v) => n + v.rejectedItemCount),
      ),
    );
  }

  void previousMapPage() {}

  Future<void> nextMapPage() async {
    if (_loading || _activeBudgetReached) return;
    final next = _continuation;
    if (next == null) return;
    _paginationError = null;
    await _loadRegions([next.bounds], continuation: next);
  }

  /// A gesture updates projection and the media budget, not the search scope.
  /// A new explicit navigation starts from the renderer's real settled bounds.
  void observeViewport({
    required String cityID,
    required MapBounds bounds,
    required int navigationGeneration,
    double devicePixelRatio = 1,
    double zoom = 12,
    String? selectedMarkerID,
  }) {
    if (!bounds.isValid) return;
    _visibleBounds = bounds;
    _ratio = devicePixelRatio;
    if (_query == null ||
        _query!.$1 != cityID ||
        _navigationGeneration != navigationGeneration) {
      _navigationGeneration = navigationGeneration;
      sync(
        cityID: cityID,
        bounds: bounds,
        zoom: zoom,
        devicePixelRatio: devicePixelRatio,
        selectedMarkerID: selectedMarkerID,
      );
    } else if (current) {
      _media.sync(
        _view!,
        devicePixelRatio: _ratio,
        selectedMarkerID: selectedMarkerID,
        visibleBounds: bounds,
      );
    }
  }

  void sync({
    required String cityID,
    required MapBounds bounds,
    double devicePixelRatio = 1,
    double zoom = 12,
    String? selectedMarkerID,
  }) {
    _ratio = devicePixelRatio;
    updateQuery(cityID, bounds, zoom: zoom, selectedMarkerID: selectedMarkerID);
  }

  Object _cacheKey(_MapQuery query) => (
    _identity?.$1,
    _identity?.$2,
    _identity?.$4,
    _permissionEpoch,
    'DEFAULT_CONTENT',
    query,
  );

  void _pruneCache() {
    _cache.removeWhere((_, v) => !v.validUntil.isAfter(_now()));
    _activePages.removeWhere(
      (_, v) => !v.validUntil.isAfter(_now()) || v.cityID != _query?.$1,
    );
    final selected = _selectedCoverage;
    while (_cache.isNotEmpty &&
        (cachedViews > maxCachedViews ||
            cachedPins > maxCachedPins ||
            cachedBytes > maxCachedBytes)) {
      final victim = _cache.keys
          .where((key) => !identical(_cache[key], selected))
          .firstOrNull;
      _cache.remove(victim ?? _cache.keys.first);
    }
  }

  void _apply(OwnedMapView value) {
    _renew?.cancel();
    _expiry?.cancel();
    _view = value;
    _leaseExpired = false;
    _error =
        _paginationError ??
        (value.rejectedItemCount > 0 ? '部分地图内容暂不可读，请重试。' : null);
    for (final pin in value.items) _registerPin(pin);
    _extraPins.removeWhere(
      (_, extra) => !sourceCurrent(extra.$2) || !extra.$3.isAfter(_now()),
    );
    _media.sync(
      value,
      devicePixelRatio: _ratio,
      selectedMarkerID: _query?.$4,
      visibleBounds: _visibleBounds,
    );
    final remaining = value.validUntil.difference(_now());
    _expiry = Timer(remaining, () {
      if (_closed || !identical(_view, value)) return;
      _pruneCache();
      if (_activeUncached?.validUntil.isAfter(_now()) == false)
        _activeUncached = null;
      // Expire only the old grants. A newer strip and an in-flight renewal
      // must survive while the now-uncovered area is fetched immediately.
      _publishCoverage();
      if (_allowed && !_loading) unawaited(refresh());
    });
    if (remaining > const Duration(seconds: 2)) {
      _renew = Timer(remaining ~/ 2, () {
        if (_closed || !identical(_view, value)) return;
        if (_allowed) {
          unawaited(refresh());
        } else {
          _retireView(preserveMedia: true);
        }
      });
    }
    _notify();
  }

  void replaceTransport({required http.Client client}) {
    if (_closed) return;
    retire();
    _api.replaceTransport(client: client);
    _identity = _readIdentity();
  }

  void _identityChanged() {
    if (_closed) return;
    retire();
    _identity = _readIdentity();
    if (_allowed && _query != null) unawaited(refresh());
  }

  void _accessDenied(OwnedMapReadFailure failure) {
    if (failure.resourceScoped) {
      final resource = '${failure.resourceKind}:${failure.resourceID}';
      final invalidated = <String>{};
      bool affected(OwnedMapPin pin) => failure.resourceKind == 'places'
          ? pin.placeID == failure.resourceID
          : pin.momentID == failure.resourceID ||
                pin.moments.any((m) => m.id == failure.resourceID) ||
                pin.cover?.contentPath.startsWith(
                      '/v1/me/map-content/moments/${failure.resourceID}/',
                    ) ==
                    true;
      OwnedMapView withoutDenied(OwnedMapView value) {
        final retained = <OwnedMapPin>[];
        for (final pin in value.items) {
          if (affected(pin)) {
            _invalidPins[pin] = true;
            invalidated.add(pin.mapID);
          } else {
            retained.add(pin);
          }
        }
        if (retained.length == value.items.length) return value;
        final clean = OwnedMapView(
          viewerAccountID: value.viewerAccountID,
          cityID: value.cityID,
          bounds: value.bounds,
          observedAt: value.observedAt,
          validUntil: value.validUntil,
          sourceVersion: value.sourceVersion,
          items: List.unmodifiable(retained),
          truncated: value.truncated,
          zoom: value.zoom,
          filterScope: value.filterScope,
          totalContentCount: value.totalContentCount,
          totalAnchorCount: value.totalAnchorCount,
          totalPointCount: value.totalPointCount,
          nextCursor: value.nextCursor,
          viewportPath: value.viewportPath,
          byteCount: value.byteCount,
          rejectedItemCount: value.rejectedItemCount,
        );
        final cursor = _requestCursorByView[value];
        if (cursor != null) _requestCursorByView[clean] = cursor;
        return clean;
      }

      for (final key in _cache.keys.toList()) {
        _cache[key] = withoutDenied(_cache[key]!);
      }
      for (final key in _activePages.keys.toList()) {
        _activePages[key] = withoutDenied(_activePages[key]!);
      }
      if (_activeUncached != null)
        _activeUncached = withoutDenied(_activeUncached!);
      _extraPins.removeWhere((_, value) {
        if (!affected(value.$1) && !affected(value.$2)) return false;
        _invalidPins[value.$1] = true;
        invalidated.add(value.$1.mapID);
        return true;
      });
      _media.retirePins(invalidated);
      _publishCoverage();
      _error = '部分地图内容已变化，正在重新读取。';
      // One coalesced revalidation per resource and identity. An unavailable
      // image cannot loop forever or invalidate unrelated authorized content.
      if (_resourceRechecks.length < 128 && _resourceRechecks.add(resource)) {
        _scheduleRecovery();
      } else {
        _error = '部分地图内容暂不可读，请重试。';
      }
      _notify();
      return;
    }
    retire();
    _error = '地图内容权限已变化，请重新读取。';
    _notify();
  }

  void _scheduleRecovery() {
    if (!_allowed || _query == null || _recovery?.isActive == true) return;
    final epoch = _epoch, identity = _identity, query = _query;
    bool current() =>
        _allowed && epoch == _epoch && identity == _identity && query == _query;
    _recovery = Timer(const Duration(seconds: 2), () {
      if (!current()) return;
      final pending = _flight;
      if (pending != null) {
        unawaited(
          pending.then((_) {
            if (current()) unawaited(refresh());
          }),
        );
      } else {
        unawaited(refresh());
      }
    });
  }

  void _cancelRequest() {
    ++_request;
    final abort = _abort;
    if (abort != null && !abort.isCompleted) {
      abort.complete();
      cancellations++;
    }
    _abort = null;
    _flight = null;
    _loading = false;
  }

  Future<void> refresh() {
    if (_flight != null) return _flight!;
    if (!_allowed || _query == null) return Future<void>.value();
    _paginationError = null;
    _activeBudgetReached = false;
    final missing = MapContentCoverage.missing(
      _query!.$2,
      _coverageViews.map((v) => v.bounds),
    );
    if (missing.isNotEmpty)
      return _loadRegions(missing, pad: true, renewSelected: true);
    final selected = _selectedCoverage;
    final renewals = _coverageViews
        .where(
          (v) =>
              identical(v, selected) ||
              MapContentCoverage.intersects(v.bounds, _query!.$2),
        )
        .toList();
    // Renew only old-zoom reads that still contribute a displayed baseline.
    // They retain their issued zoom and never become current-zoom coverage.
    final displayed = _visibleCachedPins();
    for (final baseline in _leasedViews) {
      if (!renewals.contains(baseline) &&
          displayed.any((pin) => baseline.items.any((p) => identical(pin, p))))
        renewals.add(baseline);
    }
    if (selected != null && !renewals.contains(selected))
      renewals.add(selected);
    // Renew linked pages in source order, independent of view-LRU ordering.
    // A root can rotate the next signed token before its child is read.
    final unordered = List<OwnedMapView>.of(renewals);
    renewals.clear();
    while (unordered.isNotEmpty) {
      final index = unordered.indexWhere((v) {
        final cursor = _requestCursorByView[v];
        return cursor == null ||
            !unordered.any(
              (parent) =>
                  !identical(parent, v) &&
                  parent.bounds == v.bounds &&
                  parent.zoom == v.zoom &&
                  parent.sourceVersion == v.sourceVersion &&
                  parent.nextCursor == cursor,
            );
      });
      if (index < 0) {
        renewals.addAll(unordered);
        break;
      }
      renewals.add(unordered.removeAt(index));
    }
    return _loadRegions(
      renewals.map((v) => v.bounds).toList(),
      renewals: renewals,
    );
  }

  Future<void> _loadRegions(
    List<MapBounds> regions, {
    bool pad = false,
    bool renewSelected = false,
    OwnedMapView? continuation,
    List<OwnedMapView>? renewals,
  }) {
    if (_flight != null) return _flight!;
    if (!_allowed || _query == null) return Future<void>.value();
    final epoch = _epoch,
        request = ++_request,
        identity = _identity!,
        query = _query!;
    final abort = _abort = Completer<void>();
    _loading = true;
    _error = null;
    _notify();
    final done = Completer<void>();
    _flight = done.future;
    unawaited(() async {
      bool pending() =>
          _allowed &&
          epoch == _epoch &&
          request == _request &&
          identity == _identity &&
          query == _query;
      try {
        // Padding is itself subtracted from valid coverage: a pan fetches new
        // strips, never the already authorized centre of the previous view.
        final selectedRenewal = renewSelected ? _selectedCoverage : null;
        final uncovered = pad
            ? MapContentCoverage.missing(
                MapContentCoverage.padded(query.$2),
                _coverageViews.map((v) => v.bounds),
              )
            : regions;
        final targets =
            <({MapBounds bounds, OwnedMapView? renewal, OwnedMapView? after})>[
              if (selectedRenewal != null)
                (
                  bounds: selectedRenewal.bounds,
                  renewal: selectedRenewal,
                  after: null,
                ),
              for (var i = 0; i < uncovered.length; i++)
                (
                  bounds: uncovered[i],
                  renewal: renewals?[i],
                  after: continuation,
                ),
            ];
        var extraPages = 0;
        final batchCursors = <String>{};
        final changedSources = <String>{};
        for (var targetIndex = 0; targetIndex < targets.length; targetIndex++) {
          final target = targets[targetIndex];
          final bounds = target.bounds,
              renewing = target.renewal,
              after = target.after;
          if (renewing != null &&
              changedSources.contains(renewing.sourceVersion))
            continue;
          final requestCursor =
              after?.nextCursor ??
              (renewing == null ? null : _requestCursorByView[renewing]);
          final path =
              after?.viewportPath ??
              (requestCursor != null ? renewing?.viewportPath : null);
          if (!pending()) return;
          if (after != null && !batchCursors.add(requestCursor!)) {
            _paginationError = _error = '部分位置尚未加载，请重试。';
            break;
          }
          requests++;
          final value = await _api.read(
            cityID: query.$1,
            bounds: bounds,
            zoom: renewing?.zoom ?? query.$3,
            selectedMarkerID: query.$4,
            abortTrigger: abort.future,
            ownerID: identity.$2!,
            authorization: identity.$3!,
            origin: identity.$1,
            current: pending,
            now: _now,
            viewportPath: path,
            cursor: requestCursor,
          );
          if (!pending() || !value.validUntil.isAfter(_now())) return;
          _transientRetries = 0;
          responseBytes += value.byteCount;
          if (after != null &&
              (value.sourceVersion != after.sourceVersion ||
                  value.bounds != after.bounds ||
                  value.zoom != after.zoom)) {
            _paginationError = _error = '地图内容已变化，请重新读取。';
            break;
          }
          if (value.items.isEmpty && value.nextCursor != null) {
            _paginationError = _error = '部分位置尚未加载，请重试。';
            break;
          }
          final key = (
            _cacheKey((query.$1, value.bounds, value.zoom, query.$4)),
            requestCursor ?? '',
          );
          final retainPage = requestCursor != null || value.nextCursor != null;
          final activeOthers = _activePages.entries
              .where((e) => e.key != key && !identical(e.value, renewing))
              .map((e) => e.value)
              .toList();
          final retainedIDs = {
            for (final v in activeOthers)
              for (final pin in v.items) pin.mapID,
            for (final pin in _visibleCachedPins()) pin.mapID,
            for (final pin in value.items) pin.mapID,
          };
          if (retainPage &&
              (retainedIDs.length > maxActivePins ||
                  activeOthers.fold(0, (n, v) => n + v.byteCount) +
                          value.byteCount >
                      maxActiveBytes)) {
            _activeBudgetReached = true;
            _paginationError = _error = '已加载的位置已保留，请缩小范围后继续搜索。';
            break;
          }
          // A newer authority response retires known changed/withdrawn pins
          // even if another still-leased rectangle also contains the old pin.
          final incoming = {for (final pin in value.items) pin.mapID: pin};
          final completeRead =
              requestCursor == null &&
              value.nextCursor == null &&
              !value.truncated;
          for (final oldView in _leasedViews) {
            for (final old in oldView.items) {
              final fresh = incoming[old.mapID];
              if (fresh != null && fresh.sourceVersion != old.sourceVersion ||
                  oldView.zoom == value.zoom &&
                      !value.observedAt.isBefore(oldView.observedAt) &&
                      completeRead &&
                      value.bounds.contains(old.latitude, old.longitude) &&
                      fresh == null) {
                _invalidPins[old] = true;
              }
            }
          }
          final selectedCoverage = _selectedCoverage;
          final selectedPin = selectedCoverage?.items
              .where((p) => p.canonicalMarkerID == query.$4)
              .firstOrNull;
          if (renewing != null &&
              value.sourceVersion == renewing.sourceVersion) {
            _cache.removeWhere((_, old) => identical(old, renewing));
            _activePages.removeWhere((_, old) => identical(old, renewing));
            if (renewing.nextCursor != _requestCursorByView[renewing] &&
                _usedPageCursors.contains((
                  renewing.sourceVersion,
                  renewing.nextCursor,
                )) &&
                value.nextCursor != null) {
              // Same snapshot and same page position may issue a renewed
              // signed next token. Preserve only an already proven child link;
              // never extend that child's original read grant or pixel lease.
              for (final child in _leasedViews) {
                if (child.sourceVersion == renewing.sourceVersion &&
                    child.bounds == renewing.bounds &&
                    child.zoom == renewing.zoom &&
                    _requestCursorByView[child] == renewing.nextCursor) {
                  _requestCursorByView[child] = value.nextCursor;
                }
              }
              _usedPageCursors.add((value.sourceVersion, value.nextCursor));
            }
          } else if (after == null) {
            if (renewing != null) changedSources.add(renewing.sourceVersion);
            // A new complete read supersedes old grants in that rectangle.
            // Touching edges of the incremental strips do not invalidate a
            // neighbouring cached region.
            bool superseded(Object _, OwnedMapView old) =>
                old.cityID == value.cityID &&
                old.zoom == value.zoom &&
                !value.observedAt.isBefore(old.observedAt) &&
                completeRead &&
                MapContentCoverage.missing(old.bounds, [
                  value.bounds,
                ]).isEmpty &&
                !(identical(old, selectedCoverage) &&
                    selectedPin != null &&
                    !value.bounds.contains(
                      selectedPin.latitude,
                      selectedPin.longitude,
                    ));
            _cache.removeWhere(superseded);
            _activePages.removeWhere(superseded);
          } else {
            _usedPageCursors.add((after.sourceVersion, after.nextCursor));
            if (_usedPageCursors.length > 256)
              _usedPageCursors.remove(_usedPageCursors.first);
          }
          if (requestCursor != null)
            _requestCursorByView[value] = requestCursor;
          while (_usedPageCursors.length > 256)
            _usedPageCursors.remove(_usedPageCursors.first);
          if (value.byteCount > maxCachedBytes ||
              value.items.length > maxCachedPins) {
            _activeUncached = value;
          } else {
            _activeUncached = null;
            _cache[key] = value;
          }
          if (retainPage) _activePages[key] = value;
          for (final pin in value.items) _registerPin(pin);
          _pruneCache();
          _retireAbsentFromCompleteChain(value);
          if (value.nextCursor == null &&
              !value.truncated &&
              _completePageChain(value) == null) {
            _paginationError = '部分位置尚未加载，请重新读取。';
          }
          _publishCoverage();
          final nextCursor = value.nextCursor;
          if (nextCursor != null && batchCursors.contains(nextCursor)) {
            _paginationError = _error = '部分位置尚未加载，请重试。';
            break;
          }
          if (nextCursor != null &&
              value.items.isNotEmpty &&
              !_usedPageCursors.contains((value.sourceVersion, nextCursor)) &&
              extraPages < math.max(0, progressiveBatchPages - 1)) {
            extraPages++;
            targets.add((bounds: value.bounds, renewal: null, after: value));
          }
        }
      } catch (failure) {
        if (pending()) {
          if (failure is FormatException) {
            // A malformed authority envelope is not a transient transport
            // outage. Do not retain old grants under unverified metadata.
            retire();
            _error = '地图内容校验失败，请重新读取。';
            _notify();
            return;
          }
          _error = '地图内容暂不可读，请重试。';
          if (continuation != null || _continuation != null)
            _paginationError = _error;
          if (_transientRetries < 1 &&
              (failure is TimeoutException ||
                  failure is http.ClientException ||
                  failure is OwnedMapReadFailure && failure.retryable)) {
            _transientRetries++;
            _scheduleRecovery();
          }
        }
      } finally {
        if (!abort.isCompleted) abort.complete();
        if (pending()) {
          _loading = false;
          _flight = null;
          _abort = null;
          _notify();
        }
        done.complete();
      }
    }());
    return done.future;
  }

  Future<OwnedMapView> readClusterMembers(
    OwnedMapPin pin, {
    String? cursor,
  }) async {
    final identity = _identity, epoch = _epoch, query = _query;
    final previous = cursor == null ? null : _clusterCursors[cursor];
    bool pending() =>
        epoch == _epoch &&
        identity == _identity &&
        pin.isCluster &&
        sourceCurrent(pin) &&
        (cursor == null ||
            previous != null &&
                previous.$2 == _permissionEpoch &&
                previous.$4.isAfter(_now()) &&
                previous.$1.mapID == pin.mapID &&
                previous.$1.sourceVersion == pin.sourceVersion);
    if (!pending() || identity == null || query == null)
      throw const FormatException('聚合内容读取已失效');
    if (cursor != null && previous != null) {
      // Keep a bounded current-window starting position while renewed pages
      // receive new signed continuations. Active positions are LRU, not FIFO.
      _clusterCursors.remove(cursor);
      _clusterCursors[cursor] = previous;
    }
    final value = await _api.read(
      cityID: query.$1,
      bounds: query.$2,
      zoom: query.$3,
      cluster: pin,
      cursor: cursor,
      ownerID: identity.$2!,
      authorization: identity.$3!,
      origin: identity.$1,
      current: pending,
      now: _now,
    );
    final source = Uri.parse(pin.membersPath).queryParameters['sourceVersion'];
    if (!pending() ||
        value.sourceVersion != source ||
        value.items.any((p) => p.isCluster) ||
        previous != null && previous.$3 != value.sourceVersion)
      throw const FormatException('聚合内容来源已变化');
    for (final child in value.items) {
      _extraPins.remove(child.mapID);
      _extraPins[child.mapID] = (child, pin, value.validUntil);
      _registerPin(child);
    }
    while (_extraPins.length > 320) _extraPins.remove(_extraPins.keys.first);
    _clusterCursors.removeWhere(
      (_, value) => value.$2 != _permissionEpoch || !value.$4.isAfter(_now()),
    );
    if (value.nextCursor != null) {
      if (_clusterCursors.length >= 32)
        _clusterCursors.remove(_clusterCursors.keys.first);
      _clusterCursors[value.nextCursor!] = (
        pin,
        _permissionEpoch,
        value.sourceVersion,
        _now().add(const Duration(minutes: 30)),
      );
    }
    return value;
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    _retireView(preserveMedia: true);
    if (_allowed && _query != null) unawaited(refresh());
  }

  void _retireView({required bool preserveMedia}) {
    if (_closed) return;
    ++_epoch;
    ++_request;
    final abort = _abort;
    if (abort != null && !abort.isCompleted) {
      abort.complete();
      cancellations++;
    }
    _abort = null;
    _flight = null;
    _renew?.cancel();
    _expiry?.cancel();
    _recovery?.cancel();
    _view = null;
    _activePages.clear();
    _paginationError = null;
    _activeBudgetReached = false;
    _leaseExpired = true;
    _loading = false;
    _error = null;
    _cursors.clear();
    _clusterCursors.removeWhere(
      (_, v) => v.$2 != _permissionEpoch || !v.$4.isAfter(_now()),
    );
    _extraPins.clear();
    if (preserveMedia) {
      _media.suspend();
    } else {
      _media.retire();
    }
    _notify();
  }

  void retire({bool clearQuery = false}) {
    if (_closed) return;
    _permissionEpoch++;
    _resourceRechecks.clear();
    _transientRetries = 0;
    _clusterCursors.clear();
    _cache.clear();
    _activePages.clear();
    _activeUncached = null;
    _paginationError = null;
    _activeBudgetReached = false;
    _usedPageCursors.clear();
    if (clearQuery) _query = null;
    _retireView(preserveMedia: false);
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
    identityChanges.removeListener(_identityChanged);
    WidgetsBinding.instance.removeObserver(this);
    _media.removeListener(_notify);
    _media.dispose();
    _api.dispose();
    super.dispose();
  }
}

class _OwnedMapGrant {
  const _OwnedMapGrant(this.pin, this.epoch, this.until);
  final OwnedMapPin pin;
  final int epoch;
  final DateTime? until;
}

class _OwnedMapPlaceRead {
  _OwnedMapPlaceRead(this.page, this.cursor);
  final OwnedMapPlacePage page;
  final String? cursor;
  bool retired = false;
}
