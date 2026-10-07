import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
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
  String? cityID, error;
  MapBounds? bounds;
  bool loading = false, _disposed = false;
  int _epoch = 0, _request = 0;
  Timer? _expiry;
  Timer? _refresh;
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
  List<TypedMapItem> get items => List.unmodifiable([
    ...?publicView?.items.where((i) => visible.contains(i.kind)),
    ...?privateView?.items.where((i) => visible.contains(i.kind)),
  ]);
  TypedMapItem? find(String id) {
    for (final i in items) {
      if (i.mapID == id) return i;
    }
    return null;
  }

  void retire({MapLayersApi? api}) {
    ++_epoch;
    ++_request;
    _expiry?.cancel();
    _refresh?.cancel();
    publicView = null;
    privateView = null;
    loading = false;
    error = null;
    visible.remove('OPPORTUNITY');
    if (api != null) {
      _api.dispose();
      _api = api;
    }
    _notify();
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

  Future<void> load(String city, MapBounds area) async {
    if (_disposed || !area.isValid) return;
    _refresh?.cancel();
    if (cityID != city) retire();
    cityID = city;
    bounds = area;
    final api = _api, epoch = _epoch, serial = ++_request;
    final clock = Stopwatch()..start();
    final token = authorizationHeader(), org = organizationWorkspaceID();
    final wantsPrivate =
        visible.contains('OPPORTUNITY') && token != null && org == null;
    bool current() =>
        !_disposed &&
        api == _api &&
        epoch == _epoch &&
        serial == _request &&
        token == authorizationHeader() &&
        org == organizationWorkspaceID();
    loading = true;
    error = null;
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
      final private = wantsPrivate
          ? await api.read(city, area, authorization: token, private: true)
          : null;
      if (!current()) return;
      _expiry?.cancel();
      publicView = public;
      privateView = private;
      final remaining = [
        public.validUntil.difference(public.observedAt),
        if (private != null) private.validUntil.difference(private.observedAt),
      ].reduce((a, b) => a < b ? a : b);
      // A relative lease starts before the requests, so network time cannot renew it.
      final elapsed = clock.elapsed;
      final duration = remaining - elapsed;
      if (duration <= Duration.zero) {
        publicView = null;
        privateView = null;
        error = '地图来源已到期，请重新读取';
      } else {
        _expiry = Timer(duration, () {
          if (_disposed ||
              api != _api ||
              epoch != _epoch ||
              token != authorizationHeader() ||
              org != organizationWorkspaceID()) {
            return;
          }
          if (identical(publicView, public)) publicView = null;
          if (identical(privateView, private)) privateView = null;
          error = '地图来源已到期，请重新读取';
          _notify();
        });
        // A refresh keeps the old lease while pending. It cannot keep expired
        // coordinates on screen or revive an earlier identity/range.
        if (refreshAllowed != null && duration >= const Duration(seconds: 4)) {
          final lead = Duration(
            microseconds: (duration.inMicroseconds ~/ 4).clamp(
              0,
              const Duration(seconds: 10).inMicroseconds,
            ),
          );
          _refresh = Timer(duration - lead, () {
            if (current() && !loading && refreshAllowed!()) {
              unawaited(load(city, area));
            }
          });
        }
      }
    } catch (_) {
      if (current()) {
        publicView = null;
        privateView = null;
        error = '当前图层暂不可用，请重新读取';
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
    _api.dispose();
    super.dispose();
  }
}
