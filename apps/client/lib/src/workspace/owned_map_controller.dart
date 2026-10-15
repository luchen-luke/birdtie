import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'package:http/http.dart' as http;
import '../auth/session_vault.dart';
import 'human_causal_control.dart';
import 'map_entities.dart';
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
  }) : _now = now ?? (() => DateTime.now().toUtc()) {
    _api = OwnedMapAPI(
      client: client,
      apiBaseUrl: apiBaseUrl,
      onDenied: (_) => _accessDenied(),
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
  final _extraPins = <String, (OwnedMapPin, OwnedMapPin, DateTime)>{};
  final _pinEpoch = Expando<int>();
  final _momentGrants = Expando<_OwnedMapGrant>();
  final _detailGrants = Expando<_OwnedMapGrant>();
  final _coverChecks = Expando<bool Function()>();
  final _cursors = <String, (_OwnedMapGrant, String)>{};
  // Continuations locate a page; they do not authorize its content. Every
  // use still needs a fresh parent grant and a server ACL/source check.
  final _clusterCursors = <String, (OwnedMapPin, int, String, DateTime)>{};
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
  List<MapEntity> get entities =>
      current ? List.unmodifiable(items.map(_media.decorate)) : const [];
  OwnedMapView? get view => current ? _view : null;
  OwnedMapPin? find(String mapID) {
    if (!current) return null;
    final direct = items.where((p) => p.mapID == mapID).firstOrNull;
    if (direct != null) return direct;
    final extra = _extraPins[mapID];
    return extra != null && sourceCurrent(extra.$1) ? extra.$1 : null;
  }

  bool sourceCurrent(OwnedMapPin pin) {
    if (!current || _pinEpoch[pin] != _epoch) return false;
    if (_view!.items.any(
      (p) => p.mapID == pin.mapID && p.sourceVersion == pin.sourceVersion,
    ))
      return true;
    final extra = _extraPins[pin.mapID];
    return extra != null &&
        extra.$1.sourceVersion == pin.sourceVersion &&
        extra.$3.isAfter(_now()) &&
        _view!.items.any(
          (p) =>
              p.mapID == extra.$2.mapID &&
              p.sourceVersion == extra.$2.sourceVersion,
        );
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
      _grantCurrent(_momentGrants[target]);

  bool detailCurrent(OwnedMapMomentDetail detail) =>
      detail.validUntil.isAfter(_now()) && _grantCurrent(_detailGrants[detail]);

  void _registerDetail(OwnedMapMomentDetail detail, OwnedMapPin pin) {
    final grant = _OwnedMapGrant(pin, _epoch, detail.validUntil);
    _detailGrants[detail] = grant;
    _momentGrants[detail.selector] = grant;
    for (final media in detail.media) {
      if (media.poster != null)
        _coverChecks[media.poster!] = () => detailCurrent(detail);
    }
  }

  Future<OwnedMapMomentDetail> readMoment(OwnedMapMoment target) async {
    final grant = _momentGrants[target], identity = _identity, epoch = _epoch;
    bool pending() =>
        epoch == _epoch && identity == _identity && momentCurrent(target);
    if (!pending() || grant == null || identity == null)
      throw const FormatException('地图动态读取已失效');
    final value = await _api.readMoment(
      target,
      ownerID: identity.$2!,
      authorization: identity.$3!,
      origin: identity.$1,
      current: pending,
      now: _now,
    );
    if (!pending()) throw const FormatException('地图动态读取已失效');
    _registerDetail(value, grant.pin);
    return value;
  }

  Future<OwnedMapPlacePage> readPlaceMoments(
    OwnedMapPin pin, {
    String? cursor,
  }) async {
    final identity = _identity, epoch = _epoch;
    final previous = cursor == null ? null : _cursors[cursor];
    bool pending() =>
        epoch == _epoch &&
        identity == _identity &&
        sourceCurrent(pin) &&
        (cursor == null ||
            previous != null &&
                _grantCurrent(previous.$1) &&
                previous.$1.pin.mapID == pin.mapID &&
                previous.$1.pin.sourceVersion == pin.sourceVersion);
    if (!pending() || identity == null)
      throw const FormatException('地点动态读取已失效，请重新读取');
    final value = await _api.readPlaceMoments(
      pin.placeID,
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
    for (final detail in value.items) {
      _registerDetail(detail, pin);
    }
    _cursors.removeWhere((_, v) => !_grantCurrent(v.$1));
    if (value.nextCursor != null) {
      if (_cursors.length >= 128) _cursors.remove(_cursors.keys.first);
      _cursors[value.nextCursor!] = (
        _OwnedMapGrant(pin, epoch, value.validUntil),
        value.sourceVersion,
      );
    }
    return value;
  }

  Future<Uint8List> momentContent(
    OwnedMapMomentDetail detail,
    OwnedMapMomentMedia media,
  ) async {
    final identity = _identity, epoch = _epoch;
    bool pending() =>
        epoch == _epoch &&
        identity == _identity &&
        detailCurrent(detail) &&
        detail.media.any((item) => identical(item, media));
    if (!pending() || identity == null)
      throw const FormatException('地图媒体读取已失效');
    return _api.momentContent(
      media,
      authorization: identity.$3!,
      origin: identity.$1,
      current: pending,
    );
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
    final changed = query != _query;
    if (changed) {
      _retireView(preserveMedia: true);
      _query = query;
    }
    if (!_allowed) {
      _retireView(preserveMedia: true);
      return;
    }
    if (current) {
      _media.sync(_view!, devicePixelRatio: _ratio, selectedMarkerID: selected);
      return;
    }
    if (_loading) return;
    _pruneCache();
    final key = _cacheKey(query), cached = _cache.remove(_cacheKey(query));
    if (cached != null) {
      _cache[key] = cached;
      cacheHits++;
      _apply(cached);
    } else {
      unawaited(refresh());
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
    while (_cache.isNotEmpty &&
        (cachedViews > maxCachedViews ||
            cachedPins > maxCachedPins ||
            cachedBytes > maxCachedBytes)) {
      _cache.remove(_cache.keys.first);
    }
  }

  void _apply(OwnedMapView value) {
    _renew?.cancel();
    _expiry?.cancel();
    _view = value;
    _leaseExpired = false;
    _error = null;
    for (final pin in value.items) _registerPin(pin);
    _extraPins.removeWhere(
      (_, extra) => !sourceCurrent(extra.$2) || !extra.$3.isAfter(_now()),
    );
    _media.sync(value, devicePixelRatio: _ratio, selectedMarkerID: _query?.$4);
    final remaining = value.validUntil.difference(_now());
    _expiry = Timer(remaining, () {
      if (_closed || !identical(_view, value)) return;
      _retireView(preserveMedia: true);
      _pruneCache();
      _error = '地图内容读取已到期，请重新读取。';
      _notify();
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

  void _accessDenied() {
    retire();
    _error = '地图内容权限已变化，请重新读取。';
    _notify();
  }

  Future<void> refresh() {
    if (_flight != null) return _flight!;
    if (!_allowed || _query == null) return Future<void>.value();
    final epoch = _epoch,
        request = ++_request,
        identity = _identity!,
        query = _query!;
    final abort = _abort = Completer<void>();
    _loading = true;
    _error = null;
    requests++;
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
        final value = await _api.read(
          cityID: query.$1,
          bounds: query.$2,
          zoom: query.$3,
          selectedMarkerID: query.$4,
          abortTrigger: abort.future,
          ownerID: identity.$2!,
          authorization: identity.$3!,
          origin: identity.$1,
          current: pending,
          now: _now,
        );
        if (!pending() || !value.validUntil.isAfter(_now())) return;
        responseBytes += value.byteCount;
        final key = _cacheKey(query);
        _cache.remove(key);
        _cache[key] = value;
        _pruneCache();
        _apply(value);
      } catch (_) {
        if (pending()) {
          _error = '地图内容暂不可读，请重试。';
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
        query == _query &&
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
    _view = null;
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
    _clusterCursors.clear();
    _cache.clear();
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
