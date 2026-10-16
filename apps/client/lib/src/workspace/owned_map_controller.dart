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
  OwnedMapView? _activeUncached;
  final _requestCursorByView = Expando<String>();
  final _extraPins = <String, (OwnedMapPin, OwnedMapPin, DateTime)>{};
  final _pinEpoch = Expando<int>();
  final _momentGrants = Expando<_OwnedMapGrant>();
  final _detailGrants = Expando<_OwnedMapGrant>();
  final _coverChecks = Expando<bool Function()>();
  final _cursors = <String, (_OwnedMapGrant, String)>{};
  // Continuations locate a page; they do not authorize its content. Every
  // use still needs a fresh parent grant and a server ACL/source check.
  final _clusterCursors = <String, (OwnedMapPin, int, String, DateTime)>{};
  int _pageOffset = 0;
  final _usedPageCursors = <String>{};
  int get mapPageNumber => _pageOffset ~/ 40 + 1;
  bool get hasPreviousMapPage => _pageOffset > 0;
  bool get hasNextMapPage =>
      _visibleCachedPins().length > _pageOffset + 40 || _continuation != null;
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
    if (_coverageViews.any(
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
      _pageOffset = 0;
      _query = query;
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

  Iterable<OwnedMapView> get _coverageViews => [
    ..._cache.values,
    if (_activeUncached != null) _activeUncached!,
  ].where((v) => v.cityID == _query?.$1 && v.validUntil.isAfter(_now()));

  List<OwnedMapPin> _visibleCachedPins() {
    final query = _query;
    if (query == null) return [];
    final pins = <String, OwnedMapPin>{};
    final views = _coverageViews.toList()
      ..sort((a, b) => a.observedAt.compareTo(b.observedAt));
    for (final v in views) {
      for (final p in v.items) {
        if (query.$2.contains(p.latitude, p.longitude) ||
            p.canonicalMarkerID == query.$4)
          pins[p.mapID] = p;
      }
    }
    return pins.values.toList()..sort((a, b) => a.mapID.compareTo(b.mapID));
  }

  OwnedMapView? get _continuation => _coverageViews
      .where(
        (v) =>
            MapContentCoverage.intersects(v.bounds, _query!.$2) &&
            v.viewportPath.isNotEmpty &&
            v.nextCursor != null &&
            !_usedPageCursors.contains(v.nextCursor),
      )
      .firstOrNull;

  void _publishCoverage() {
    final query = _query;
    if (query == null || !_allowed) return;
    final views = _coverageViews
        .where(
          (v) =>
              MapContentCoverage.intersects(v.bounds, query.$2) ||
              v.items.any((p) => p.canonicalMarkerID == query.$4),
        )
        .toList();
    if (views.isEmpty) {
      _view = null;
      _renew?.cancel();
      _expiry?.cancel();
      _media.suspend();
      _notify();
      return;
    }
    final pins = _visibleCachedPins();
    if (_pageOffset >= pins.length)
      _pageOffset = math.max(0, (pins.length - 1) ~/ 40 * 40);
    final page = pins.skip(_pageOffset).take(40).toList();
    final selected = pins
        .where((p) => p.canonicalMarkerID == query.$4)
        .firstOrNull;
    if (selected != null && !page.contains(selected)) {
      if (page.length == 40) page.removeLast();
      page.add(selected);
    }
    final until = views
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
        truncated: pins.length > _pageOffset + 40 || next != null,
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
      ),
    );
  }

  void previousMapPage() {
    if (_pageOffset == 0) return;
    _pageOffset = math.max(0, _pageOffset - 40);
    _publishCoverage();
  }

  Future<void> nextMapPage() async {
    if (_loading) return;
    final query = _query, epoch = _epoch;
    if (_visibleCachedPins().length <= _pageOffset + 40) {
      final next = _continuation;
      if (next == null) return;
      await _loadRegions([next.bounds], continuation: next);
    }
    if (query != _query || epoch != _epoch || !_allowed) return;
    if (_visibleCachedPins().length > _pageOffset + 40) _pageOffset += 40;
    _publishCoverage();
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

  void _accessDenied() {
    retire();
    _error = '地图内容权限已变化，请重新读取。';
    _notify();
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
    final missing = MapContentCoverage.missing(
      _query!.$2,
      _coverageViews.map((v) => v.bounds),
    );
    if (missing.isNotEmpty) return _loadRegions(missing, pad: true);
    final renewals = _coverageViews
        .where((v) => MapContentCoverage.intersects(v.bounds, _query!.$2))
        .toList();
    return _loadRegions(
      renewals.map((v) => v.bounds).toList(),
      renewals: renewals,
    );
  }

  Future<void> _loadRegions(
    List<MapBounds> regions, {
    bool pad = false,
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
        final targets = pad
            ? MapContentCoverage.missing(
                MapContentCoverage.padded(query.$2),
                _coverageViews.map((v) => v.bounds),
              )
            : regions;
        final changedSources = <String>{};
        for (var targetIndex = 0; targetIndex < targets.length; targetIndex++) {
          final bounds = targets[targetIndex];
          final renewing = renewals?[targetIndex];
          if (renewing != null &&
              changedSources.contains(renewing.sourceVersion))
            continue;
          final requestCursor =
              continuation?.nextCursor ??
              (renewing == null ? null : _requestCursorByView[renewing]);
          final path =
              continuation?.viewportPath ??
              (requestCursor != null ? renewing?.viewportPath : null);
          if (!pending()) return;
          requests++;
          final value = await _api.read(
            cityID: query.$1,
            bounds: bounds,
            zoom: query.$3,
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
          responseBytes += value.byteCount;
          if (renewing != null &&
              value.sourceVersion == renewing.sourceVersion) {
            _cache.removeWhere((_, old) => identical(old, renewing));
            if (_usedPageCursors.contains(renewing.nextCursor) &&
                value.nextCursor != null) {
              _usedPageCursors.add(value.nextCursor!);
            }
          } else if (continuation == null) {
            if (renewing != null) changedSources.add(renewing.sourceVersion);
            // A new complete read supersedes old grants in that rectangle.
            // Touching edges of the incremental strips do not invalidate a
            // neighbouring cached region.
            _cache.removeWhere(
              (_, old) =>
                  old.cityID == value.cityID &&
                  _positiveOverlap(old.bounds, value.bounds),
            );
          } else {
            _usedPageCursors.add(continuation.nextCursor!);
            if (_usedPageCursors.length > 256)
              _usedPageCursors.remove(_usedPageCursors.first);
          }
          final key = (
            _cacheKey((query.$1, value.bounds, value.zoom, query.$4)),
            requestCursor ?? '',
          );
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
          for (final pin in value.items) _registerPin(pin);
          _pruneCache();
          _publishCoverage();
        }
      } catch (_) {
        if (pending()) _error = '地图内容暂不可读，请重试。';
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

  static bool _positiveOverlap(MapBounds a, MapBounds b) =>
      MapContentCoverage.split(a).any(
        (x) => MapContentCoverage.split(b).any(
          (y) =>
              math.min(x.east, y.east) - math.max(x.west, y.west) > 1e-9 &&
              math.min(x.north, y.north) - math.max(x.south, y.south) > 1e-9,
        ),
      );

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
    _activeUncached = null;
    _usedPageCursors.clear();
    _pageOffset = 0;
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
