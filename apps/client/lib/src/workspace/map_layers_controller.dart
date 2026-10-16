import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import 'map_entities.dart';
import 'map_layers_api.dart';

/// One map and card projection. Identity epochs permanently retire older reads.
class MapLayersController extends ChangeNotifier {
  MapLayersController({
    required this._api,
    required this.authorizationHeader,
    required this.organizationWorkspaceID,
    this.refreshAllowed,
  });
  MapLayersApi _api;
  final String? Function() authorizationHeader, organizationWorkspaceID;

  /// Only the visible, foreground Now route opts into renewing reads.
  final bool Function()? refreshAllowed;
  final Set<String> visible = {
    ...mapLayerKinds.where((e) => e != 'OPPORTUNITY'),
  };
  TypedMapView? publicView, privateView;
  TypedMapView? authorView;
  String? authorID, authorCityID, authorError;
  MapBounds? authorBounds;
  bool authorLoading = false;
  int _authorRequest = 0;
  Timer? _authorExpiry, _authorRefresh;
  String? cityID, error;
  MapBounds? bounds;
  final Set<String> _unavailableKinds = {};

  /// Failed requested sources are unavailable, never authoritative empty data.
  Set<String> get unavailableKinds => Set.unmodifiable(_unavailableKinds);
  bool get partial => publicView != null && _unavailableKinds.isNotEmpty;
  bool loading = false, _disposed = false;
  int _epoch = 0, _request = 0;
  Timer? _expiry;
  Timer? _refresh;
  Future<void>? _baseFlight;
  Future<bool>? _authorFlight;
  Object? _baseFlightKey, _authorFlightKey;
  int _baseFlightSerial = 0, _authorFlightSerial = 0;
  int _visibilityGeneration = 0;
  bool? _foreground;
  bool _baseAutomatic = false, _authorAutomatic = false;
  bool _baseDue = false, _authorDue = false;
  bool _mediaBaseDue = false, _mediaAuthorDue = false;
  int _baseRetries = 0, _authorRetries = 0;

  bool get _canRefresh => !_disposed && refreshAllowed?.call() == true;

  void _evidence(
    String event, {
    TypedMapView? view,
    Duration? duration,
    bool author = false,
  }) {
    if (!const bool.fromEnvironment('BIRDTIE_NOW_CAMERA_EVIDENCE')) return;
    debugPrint(
      'BIRDTIE_MAP_LEASE_EVIDENCE ${jsonEncode({'event': event, 'receivedAt': DateTime.now().toUtc().toIso8601String(), if (view != null) 'observedAt': view.observedAt.toIso8601String(), if (view != null) 'validUntil': view.validUntil.toIso8601String(), if (duration != null) 'durationUs': duration.inMicroseconds, 'epoch': _epoch, 'serial': author ? _authorRequest : _request, 'authorScope': author, 'count': view?.items.length ?? 0, 'allowed': _canRefresh})}',
    );
  }

  /// Called by the host on route/lifecycle transitions. A skipped timer is a
  /// pending read, never an extension of the source's original permission.
  void updateRefreshVisibility() {
    if (_disposed || refreshAllowed == null) return;
    final active = _canRefresh;
    if (_foreground == active) return;
    _foreground = active;
    ++_visibilityGeneration;
    _evidence('visibility');
    if (!active) {
      if (_baseAutomatic && loading) {
        ++_request;
        loading = false;
        _baseDue = true;
      }
      if (_authorAutomatic && authorLoading) {
        ++_authorRequest;
        authorLoading = false;
        _authorDue = true;
      }
      // Keep the deadline and one-shot due timers. They may retire data and
      // mark a missed read in the background, but cannot start background HTTP.
      return;
    }
    if (_baseDue || (publicView == null && error != null)) {
      _renewBase();
    }
    if (_authorDue || (authorView == null && authorError != null)) {
      _renewAuthor();
    }
  }

  void _renewBase() {
    if (!_canRefresh || cityID == null || bounds == null) {
      _baseDue = true;
      return;
    }
    if (loading) return;
    _mediaBaseDue = false;
    _baseDue = false;
    unawaited(_startBase(cityID!, bounds!, automatic: true));
  }

  void _renewAuthor() {
    if (!_canRefresh ||
        authorID == null ||
        authorBounds == null ||
        authorCityID != cityID) {
      _authorDue = authorID != null;
      return;
    }
    if (authorLoading) return;
    _mediaAuthorDue = false;
    _authorDue = false;
    unawaited(
      _startAuthor(authorCityID!, authorBounds!, authorID!, automatic: true),
    );
  }

  /// A verified local photo operation only requests another current read. It
  /// never changes the base item, extends its lease or supplies image authority.
  void requestMediaRefresh() {
    if (_disposed) return;
    _mediaBaseDue = true;
    _baseDue = true;
    if (authorID != null) {
      _mediaAuthorDue = true;
      _authorDue = true;
    }
    _renewBase();
    if (authorID != null) _renewAuthor();
  }

  Duration _remaining(TypedMapView view, Stopwatch requestClock) {
    final relative =
        view.validUntil.difference(view.observedAt) - requestClock.elapsed;
    final absolute = view.validUntil.difference(DateTime.now().toUtc());
    return relative < absolute ? relative : absolute;
  }

  Duration _mediaRefreshRemaining(TypedMapView view, Duration baseRemaining) {
    var remaining = baseRemaining;
    final now = DateTime.now().toUtc();
    for (final item in view.items) {
      for (final deadline in [
        item.placePhoto?.expiresAt,
        item.momentPhoto?.expiresAt,
        item.recentMoments?.validUntil,
      ]) {
        if (deadline == null) continue;
        final absolute = deadline.difference(now);
        final ceiling = deadline.difference(view.observedAt);
        final relative = absolute < ceiling ? absolute : ceiling;
        if (relative > Duration.zero && relative < remaining)
          remaining = relative;
      }
    }
    return remaining;
  }

  void _scheduleRefresh(
    Duration remaining,
    bool Function() current, {
    bool author = false,
  }) {
    if (refreshAllowed == null || remaining <= Duration.zero) return;
    final lead = Duration(
      microseconds: (remaining.inMicroseconds ~/ 4).clamp(
        0,
        const Duration(seconds: 10).inMicroseconds,
      ),
    );
    final minimum = const Duration(seconds: 2);
    final untilRefresh = remaining - lead < minimum
        ? minimum
        : remaining - lead;
    final timer = Timer(untilRefresh, () {
      if (!current()) return;
      if (author) {
        _authorDue = true;
        _renewAuthor();
      } else {
        _baseDue = true;
        _renewBase();
      }
    });
    if (author) {
      _authorRefresh = timer;
    } else {
      _refresh = timer;
    }
  }

  bool _retryable(Object failure) =>
      failure is TimeoutException ||
      failure is http.ClientException ||
      (failure is MapLayersReadFailure && failure.retryable);

  // A transient automatic GET receives at most one delayed recovery attempt.
  // Permission and malformed data failures never become a polling loop.
  void _scheduleRetry(bool Function() current, {bool author = false}) {
    if (refreshAllowed == null ||
        !_canRefresh ||
        (author ? _authorRetries : _baseRetries) >= 1) {
      return;
    }
    if (author) {
      ++_authorRetries;
    } else {
      ++_baseRetries;
    }
    _evidence('retry', duration: const Duration(seconds: 2), author: author);
    final timer = Timer(const Duration(seconds: 2), () {
      if (!current()) return;
      if (author) {
        _authorDue = true;
        _renewAuthor();
      } else {
        _baseDue = true;
        _renewBase();
      }
    });
    if (author) {
      _authorRefresh = timer;
    } else {
      _refresh = timer;
    }
  }

  bool _notificationPending = false;
  void _notify() {
    if (_disposed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_notificationPending) return;
      _notificationPending = true;
      SchedulerBinding.instance.addPostFrameCallback((_) {
        _notificationPending = false;
        if (!_disposed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  int get epoch => _epoch;

  /// A refresh may replace the DTO without changing its authorized content.
  /// The original lease still expires; callers must retain identity and bounds
  /// guards because a TypedMapView does not carry an authorization epoch/range.
  bool retainsSnapshot(TypedMapView expected) {
    if (_disposed) return false;
    final current = switch (expected.scope) {
      'PUBLIC' => publicView,
      'SELF_PRIVATE' => privateView,
      'PUBLIC_AUTHOR' => authorView,
      _ => null,
    };
    final now = DateTime.now().toUtc();
    if (current == null ||
        current.cityID != expected.cityID ||
        current.scope != expected.scope ||
        current.authorID != expected.authorID ||
        current.truncated != expected.truncated ||
        !now.isBefore(expected.validUntil) ||
        !now.isBefore(current.validUntil) ||
        current.items.length != expected.items.length) {
      return false;
    }
    final byID = {for (final item in current.items) item.mapID: item};
    if (byID.length != current.items.length) return false;
    final seen = <String>{};
    for (final item in expected.items) {
      final latest = byID[item.mapID];
      if (!seen.add(item.mapID) ||
          latest == null ||
          item.kind != latest.kind ||
          item.id != latest.id ||
          item.pinIdentity != latest.pinIdentity ||
          item.title != latest.title ||
          item.detailType != latest.detailType ||
          item.detailID != latest.detailID ||
          item.placeID != latest.placeID ||
          item.anchorLabel != latest.anchorLabel ||
          item.latitude != latest.latitude ||
          item.longitude != latest.longitude ||
          item.sourceVersion != latest.sourceVersion) {
        return false;
      }
    }
    return true;
  }

  List<TypedMapItem> get baseItems => List.unmodifiable([
    ...?publicView?.items.where((i) => visible.contains(i.kind)),
    ...?privateView?.items.where((i) => visible.contains(i.kind)),
  ]);
  // A selected author scope remains empty on failure/expiry. It must never
  // substitute the underlying city or private opportunity collection.
  List<TypedMapItem> get items => authorID == null
      ? baseItems
      : authorView?.authorID == authorID && authorView?.cityID == authorCityID
      ? authorView!.items
      : const [];
  TypedMapItem? findBase(String id) {
    for (final item in baseItems) {
      if (item.mapID == id) return item;
    }
    return null;
  }

  TypedMapItem? find(String id) {
    for (final i in items) {
      if (i.mapID == id) return i;
    }
    return null;
  }

  void retire({MapLayersApi? api}) {
    _mediaBaseDue = false;
    ++_epoch;
    ++_request;
    _expiry?.cancel();
    _refresh?.cancel();
    _baseDue = false;
    _baseRetries = 0;
    publicView = null;
    privateView = null;
    _clearAuthor();
    loading = false;
    error = null;
    _unavailableKinds.clear();
    visible.remove('OPPORTUNITY');
    if (api != null) {
      _api.dispose();
      _api = api;
    }
    _notify();
  }

  void _clearAuthor() {
    _mediaAuthorDue = false;
    ++_authorRequest;
    _authorExpiry?.cancel();
    _authorRefresh?.cancel();
    authorView = null;
    authorID = null;
    authorCityID = null;
    authorBounds = null;
    authorError = null;
    authorLoading = false;
    _authorDue = false;
    _authorRetries = 0;
  }

  void leaveAuthor() {
    if (_disposed || authorID == null) return;
    _clearAuthor();
    _notify();
  }

  Future<bool> loadAuthor(String city, MapBounds area, String author) =>
      _startAuthor(city, area, author);

  Future<bool> _startAuthor(
    String city,
    MapBounds area,
    String author, {
    bool automatic = false,
  }) {
    final key = (
      _api,
      _epoch,
      authorizationHeader(),
      organizationWorkspaceID(),
      city,
      area,
      author,
    );
    if (_authorFlight != null &&
        _authorFlightKey == key &&
        _authorFlightSerial == _authorRequest) {
      return _authorFlight!;
    }
    final future = _readAuthor(city, area, author, automatic: automatic);
    _authorFlight = future;
    _authorFlightKey = key;
    _authorFlightSerial = _authorRequest;
    unawaited(
      future.then((_) {
        if (identical(_authorFlight, future)) {
          _authorFlight = null;
          _authorFlightKey = null;
          if (_mediaAuthorDue) _renewAuthor();
        }
      }),
    );
    return future;
  }

  Future<bool> _readAuthor(
    String city,
    MapBounds area,
    String author, {
    required bool automatic,
  }) async {
    if (_disposed || !area.isValid || city != cityID) return false;
    final changed =
        author != authorID || city != authorCityID || area != authorBounds;
    if (changed) _clearAuthor();
    authorID = author;
    authorCityID = city;
    authorBounds = area;
    _authorRefresh?.cancel();
    _authorDue = false;
    _authorAutomatic = automatic;
    if (!automatic) _authorRetries = 0;
    final api = _api, epoch = _epoch, serial = ++_authorRequest;
    final visibility = _visibilityGeneration;
    final token = authorizationHeader(), org = organizationWorkspaceID();
    final clock = Stopwatch()..start();
    bool current() =>
        !_disposed &&
        api == _api &&
        epoch == _epoch &&
        serial == _authorRequest &&
        authorID == author &&
        authorCityID == city &&
        authorBounds == area &&
        token == authorizationHeader() &&
        org == organizationWorkspaceID() &&
        (!automatic || (visibility == _visibilityGeneration && _canRefresh));
    authorLoading = true;
    authorError = null;
    _notify();
    try {
      final view = await api.read(
        city,
        area,
        authorization: org == null ? token : null,
        authorID: author,
      );
      if (!current()) return false;
      final remaining = _remaining(view, clock);
      if (remaining <= Duration.zero) {
        authorView = null;
        authorError = '作者范围的来源已到期，请重新读取';
        return false;
      }
      _authorExpiry?.cancel();
      authorView = view;
      if (view.rejectedItemCount > 0) authorError = '部分作者动态暂不可读，请重试';
      _authorRetries = 0;
      _evidence('success', view: view, duration: remaining, author: true);
      _authorExpiry = Timer(remaining, () {
        if (_disposed ||
            api != _api ||
            epoch != _epoch ||
            authorID != author ||
            authorCityID != city ||
            authorBounds != area ||
            token != authorizationHeader() ||
            org != organizationWorkspaceID()) {
          return;
        }
        if (identical(authorView, view)) {
          _evidence('expiry', view: view, author: true);
          authorView = null;
          _authorDue = true;
          authorError = '作者范围的来源已到期，请重新读取';
          _notify();
        }
      });
      _scheduleRefresh(
        _mediaRefreshRemaining(view, remaining),
        () =>
            !_disposed &&
            api == _api &&
            epoch == _epoch &&
            serial == _authorRequest &&
            authorID == author &&
            authorCityID == city &&
            authorBounds == area &&
            token == authorizationHeader() &&
            org == organizationWorkspaceID(),
        author: true,
      );
      return true;
    } catch (failure) {
      if (current()) {
        if (!_retryable(failure) ||
            authorView?.validUntil.isAfter(DateTime.now().toUtc()) != true) {
          authorView = null;
        }
        authorError = '当前范围的作者公开动态暂不可用，请重新读取';
        if (automatic && _retryable(failure)) {
          _scheduleRetry(
            () =>
                !_disposed &&
                api == _api &&
                epoch == _epoch &&
                serial == _authorRequest &&
                authorID == author &&
                authorCityID == city &&
                authorBounds == area &&
                token == authorizationHeader() &&
                org == organizationWorkspaceID(),
            author: true,
          );
        }
      }
      return false;
    } finally {
      if (current()) {
        authorLoading = false;
        _notify();
      }
    }
  }

  void toggle(String kind, bool enabled) {
    if (_disposed || !mapLayerKinds.contains(kind)) return;
    if (kind == 'OPPORTUNITY' &&
        (authorizationHeader() == null || organizationWorkspaceID() != null)) {
      return;
    }
    enabled ? visible.add(kind) : visible.remove(kind);
    _notify();
    if (enabled &&
        kind == 'OPPORTUNITY' &&
        privateView == null &&
        cityID != null &&
        bounds != null) {
      unawaited(load(cityID!, bounds!));
    }
  }

  Future<void> load(String city, MapBounds area) => _startBase(city, area);

  Future<void> _startBase(
    String city,
    MapBounds area, {
    bool automatic = false,
  }) {
    final key = (
      _api,
      _epoch,
      authorizationHeader(),
      organizationWorkspaceID(),
      city,
      area,
      visible.contains('OPPORTUNITY'),
    );
    if (_baseFlight != null &&
        _baseFlightKey == key &&
        _baseFlightSerial == _request) {
      return _baseFlight!;
    }
    final future = _readBase(city, area, automatic: automatic);
    _baseFlight = future;
    // A first read may retire a previous city synchronously before its await.
    _baseFlightKey = (
      _api,
      _epoch,
      authorizationHeader(),
      organizationWorkspaceID(),
      city,
      area,
      visible.contains('OPPORTUNITY'),
    );
    _baseFlightSerial = _request;
    unawaited(
      future.then((_) {
        if (identical(_baseFlight, future)) {
          _baseFlight = null;
          _baseFlightKey = null;
          if (_mediaBaseDue) _renewBase();
        }
      }),
    );
    return future;
  }

  Future<void> _readBase(
    String city,
    MapBounds area, {
    required bool automatic,
  }) async {
    if (_disposed || !area.isValid) return;
    _refresh?.cancel();
    if (cityID != city) retire();
    if (bounds != null && bounds != area) {
      _expiry?.cancel();
      publicView = null;
      privateView = null;
    }
    cityID = city;
    bounds = area;
    _baseDue = false;
    _mediaBaseDue = false;
    _baseAutomatic = automatic;
    if (!automatic) _baseRetries = 0;
    final api = _api, epoch = _epoch, serial = ++_request;
    final visibility = _visibilityGeneration;
    final clock = Stopwatch()..start();
    final token = authorizationHeader(), org = organizationWorkspaceID();
    final wantsPrivate =
        visible.contains('OPPORTUNITY') && token != null && org == null;
    final requestedKinds = {
      ...mapLayerKinds.where((kind) => kind != 'OPPORTUNITY'),
      if (wantsPrivate) 'OPPORTUNITY',
    };
    bool current() =>
        !_disposed &&
        api == _api &&
        epoch == _epoch &&
        serial == _request &&
        cityID == city &&
        bounds == area &&
        token == authorizationHeader() &&
        org == organizationWorkspaceID() &&
        (!automatic || (visibility == _visibilityGeneration && _canRefresh));
    loading = true;
    error = null;
    _unavailableKinds.clear();
    // A same-identity refresh preserves the still-valid snapshot and its timer.
    // A new request may not extend the prior source lease while it is pending.
    if (!wantsPrivate) privateView = null;
    _notify();
    try {
      final public = await api.read(
        city,
        area,
        authorization: org == null ? token : null,
      );
      if (!current()) return;
      TypedMapView? private;
      var privateUnavailable = false;
      if (wantsPrivate) {
        try {
          private = await api.read(
            city,
            area,
            authorization: token,
            private: true,
          );
        } catch (_) {
          if (!current()) return;
          // The public read remains authoritative even when the separately
          // authorized opportunity source fails. Do not revive older private
          // data or describe a failed private request as an empty result.
          privateUnavailable = true;
        }
      }
      if (!current()) return;
      _expiry?.cancel();
      publicView = public;
      privateView = private;
      if (public.rejectedItemCount > 0 ||
          (private?.rejectedItemCount ?? 0) > 0) {
        error = '部分地图内容暂不可读，请重试';
      }
      if (privateUnavailable) {
        _unavailableKinds.add('OPPORTUNITY');
        error = '公开地图已读取；我的活动机会暂不可用，请重试';
      }
      final remaining = [
        _remaining(public, clock),
        if (private != null) _remaining(private, clock),
      ].reduce((a, b) => a < b ? a : b);
      final duration = remaining;
      if (duration <= Duration.zero) {
        publicView = null;
        privateView = null;
        _unavailableKinds
          ..clear()
          ..addAll(requestedKinds);
        error = '地图来源已到期，请重新读取';
      } else {
        _baseRetries = 0;
        _evidence('success', view: public, duration: duration);
        _expiry = Timer(duration, () {
          if (_disposed ||
              api != _api ||
              epoch != _epoch ||
              cityID != city ||
              bounds != area ||
              token != authorizationHeader() ||
              org != organizationWorkspaceID()) {
            return;
          }
          if (identical(publicView, public)) {
            _evidence('expiry', view: public);
            publicView = null;
            _baseDue = true;
          }
          if (identical(privateView, private)) privateView = null;
          _unavailableKinds
            ..clear()
            ..addAll(requestedKinds);
          error = '地图来源已到期，请重新读取';
          _notify();
        });
        // A refresh keeps the old lease while pending. It cannot keep expired
        // coordinates on screen or revive an earlier identity/range.
        _scheduleRefresh(
          _mediaRefreshRemaining(public, duration),
          () =>
              !_disposed &&
              api == _api &&
              epoch == _epoch &&
              serial == _request &&
              cityID == city &&
              bounds == area &&
              token == authorizationHeader() &&
              org == organizationWorkspaceID(),
        );
      }
    } catch (failure) {
      if (current()) {
        final now = DateTime.now().toUtc();
        if (!_retryable(failure) ||
            publicView?.validUntil.isAfter(now) != true) {
          publicView = null;
        }
        if (!_retryable(failure) ||
            privateView?.validUntil.isAfter(now) != true) {
          privateView = null;
        }
        _unavailableKinds
          ..clear()
          ..addAll(requestedKinds);
        error = '当前图层暂不可用，请重新读取';
        if (automatic && _retryable(failure)) {
          _scheduleRetry(
            () =>
                !_disposed &&
                api == _api &&
                epoch == _epoch &&
                serial == _request &&
                cityID == city &&
                bounds == area &&
                token == authorizationHeader() &&
                org == organizationWorkspaceID(),
          );
        }
      }
    } finally {
      if (current()) {
        loading = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _disposed = true;
    ++_epoch;
    ++_request;
    _expiry?.cancel();
    _refresh?.cancel();
    _clearAuthor();
    _api.dispose();
    super.dispose();
  }
}
