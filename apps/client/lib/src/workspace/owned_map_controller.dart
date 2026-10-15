import 'dart:async';
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

/// The personal Now map is a read projection, separate from Agent results and
/// public map layers. Identity changes retire data before any later response.
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
  }) : _now = now ?? (() => DateTime.now().toUtc()) {
    _api = OwnedMapAPI(client: client, apiBaseUrl: apiBaseUrl);
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
  late OwnedMapAPI _api;
  late OwnedMapMediaController _media;
  (String, String?, String?, String?)? _identity;
  (String, MapBounds)? _query;
  OwnedMapView? _view;
  Timer? _renew, _expiry;
  int _epoch = 0, _request = 0;
  double _ratio = 1;
  bool _closed = false, _foreground = true, _leaseExpired = false;
  bool _visibilityAllowed = false;
  bool _loading = false, _notifyingLater = false;
  String? _error;
  final _pinEpoch = Expando<int>();
  final _momentGrants = Expando<_OwnedMapGrant>();
  final _detailGrants = Expando<_OwnedMapGrant>();
  final _coverChecks = Expando<bool Function()>();
  final _cursors = <String, (_OwnedMapGrant, String)>{};
  int get epoch => _epoch;
  String? get cityID => _query?.$1;

  (String, String?, String?, String?) _readIdentity() {
    var origin = '';
    try {
      origin = canonicalAuthOrigin(apiBaseUrl());
    } on FormatException {
      // An invalid or absent origin never owns an authenticated read.
    }
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
    for (final item in items) {
      if (item.mapID == mapID) return item;
    }
    return null;
  }

  bool sourceCurrent(OwnedMapPin item) =>
      current &&
      _pinEpoch[item] == _epoch &&
      _view!.items.any(
        (value) =>
            value.mapID == item.mapID &&
            value.sourceVersion == item.sourceVersion,
      );

  /// Route visibility is independent of metadata renewal. A route returning
  /// after the old lease expired gets one new read of its chosen area; builds
  /// while visible never become a retry loop after a failed response.
  void updateRefreshVisibility() {
    if (_closed) return;
    final allowed = _allowed;
    if (allowed == _visibilityAllowed) return;
    _visibilityAllowed = allowed;
    if (!allowed) {
      retire();
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

  /// Camera rendering can call sync repeatedly. The same query does not reload
  /// or reset selection. The workspace decides when a new area is requested.
  void sync({
    required String cityID,
    required MapBounds bounds,
    double devicePixelRatio = 1,
  }) {
    if (_closed) return;
    final identity = _readIdentity();
    if (identity != _identity) {
      retire();
      _identity = identity;
    }
    if (cityID.isEmpty || !bounds.isValid) {
      retire();
      _query = null;
      return;
    }
    final query = (cityID, bounds);
    if (query != _query) {
      retire();
      _query = query;
    }
    _ratio = devicePixelRatio;
    if (!_allowed) {
      retire();
      return;
    }
    if (current) {
      _media.sync(_view!, devicePixelRatio: _ratio);
    } else if (!_loading) {
      unawaited(refresh());
    }
  }

  void replaceTransport({required http.Client client}) {
    if (_closed) return;
    retire();
    _api.replaceTransport(client: client);
    _identity = _readIdentity();
  }

  void _identityChanged() {
    if (_closed) return;
    // Even an equal tuple can accompany a permission epoch change. Never keep
    // an old proof across an observed account/workspace boundary or ABA.
    retire();
    _identity = _readIdentity();
    if (_allowed && _query != null) unawaited(refresh());
  }

  Future<void> refresh() async {
    if (!_allowed || _query == null || _loading) return;
    final epoch = _epoch, request = ++_request;
    final identity = _identity!, query = _query!;
    bool pending() =>
        _allowed &&
        epoch == _epoch &&
        request == _request &&
        identity == _identity &&
        query == _query;
    _loading = true;
    _error = null;
    _notify();
    try {
      final value = await _api.read(
        cityID: query.$1,
        bounds: query.$2,
        ownerID: identity.$2!,
        authorization: identity.$3!,
        origin: identity.$1,
        current: pending,
        now: _now,
      );
      if (!pending() || !value.validUntil.isAfter(_now())) return;
      _renew?.cancel();
      _expiry?.cancel();
      _view = value;
      _leaseExpired = false;
      for (final pin in value.items) {
        _pinEpoch[pin] = epoch;
        for (final moment in pin.moments) {
          _momentGrants[moment] = _OwnedMapGrant(pin, epoch, null);
        }
        if (pin.cover != null)
          _coverChecks[pin.cover!] = () => sourceCurrent(pin);
      }
      _media.sync(value, devicePixelRatio: _ratio);
      final remaining = value.validUntil.difference(_now());
      _expiry = Timer(remaining, () {
        if (_closed || !identical(_view, value)) return;
        retire();
        _error = '地图内容读取已到期，请重新读取。';
        _notify();
      });
      // Renew only the already chosen area while its Now surface is visible.
      // No map movement, selection, publication or Agent request is produced.
      // Start halfway through the remaining lease. A real authorized map read
      // can take longer than five seconds; waiting until its last five seconds
      // made expiry retire that still-pending renewal. The original deadline
      // still clears old content if the fresh read fails or arrives too late.
      if (remaining > const Duration(seconds: 2)) {
        _renew = Timer(remaining ~/ 2, () {
          if (_closed || !identical(_view, value)) return;
          if (_allowed) {
            unawaited(refresh());
          } else {
            retire();
          }
        });
      }
    } catch (_) {
      if (pending()) _error = '地图内容暂不可读，请重新读取。';
    } finally {
      if (pending()) {
        _loading = false;
        _notify();
      }
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    retire();
    if (_allowed && _query != null) unawaited(refresh());
  }

  void retire({bool clearQuery = false}) {
    if (_closed) return;
    ++_epoch;
    ++_request;
    if (clearQuery) _query = null;
    _renew?.cancel();
    _expiry?.cancel();
    _view = null;
    _leaseExpired = true;
    _loading = false;
    _error = null;
    _cursors.clear();
    _media.retire();
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
