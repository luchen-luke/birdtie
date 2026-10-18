import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:amap_map/amap_map.dart' as amap;
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:x_amap_base/x_amap_base.dart' as abase;

import '../workspace/map_display_policy.dart';
import '../workspace/map_entities.dart';
import '../workspace/map_marker_bitmap.dart';
import '../workspace/map_marker_visual.dart';
import 'amap_native_configuration.dart';
import 'amap_offline_style.dart';
import 'amap_session_consent.dart';
import 'map_privacy_sheet.dart';
import 'map_camera_focus.dart';
import 'map_cluster_members_sheet.dart';
import 'map_coordinate_boundary.dart';
import 'map_point_selection_sheet.dart';
import 'native_map_diagnostics.dart';
import 'native_map_failure.dart';
import 'now_map_compass.dart';
import 'public_city_controller.dart';
import 'map_navigation_target.dart';

/// Query generations fence reads, but only authority/content retirement needs
/// an opaque cover while copied native pixels are being removed. Keeping these
/// separate prevents a viewport refresh from flashing the entire basemap.
@visibleForTesting
bool amapRequiresNativeRetirement({
  required Object? previousAuthority,
  required Object authority,
  required Iterable<MapEntity> previous,
  required Map<String, MapEntity> next,
  Set<String>? copiedMediaIDs,
  Set<String>? copiedAuxiliaryIDs,
  DateTime? now,
}) {
  if (previousAuthority != authority) return true;
  final at = now ?? DateTime.now().toUtc();
  for (final old in previous) {
    final fresh = next[old.id];
    final grant = old.readGrant;
    if (grant != null && !grant.authorizes(old, at)) return true;
    final mediaWasCopied =
        copiedMediaIDs == null || copiedMediaIDs.contains(old.id);
    final auxiliaryWasCopied =
        copiedAuxiliaryIDs == null || copiedAuxiliaryIDs.contains(old.id);
    final retainedDecorationAllowed =
        (!mediaWasCopied ||
            old.mediaPixels == null ||
            grant?.authorizesCopiedPixels(old.mediaPixels!, at) == true) &&
        (!auxiliaryWasCopied ||
            old.recentMoments == null ||
            old.recentMoments!.currentAt(at));
    if (fresh == null) {
      // A viewport no longer selects this row. Its independently issued finite
      // permission may still allow the already copied native pixels until the
      // incremental removal ACK. It grants no new media reads or rendering.
      if (grant != null && retainedDecorationAllowed) continue;
      return true;
    }
    if (fresh.sourceVersion != old.sourceVersion ||
        fresh.kind != old.kind ||
        fresh.latitude != old.latitude ||
        fresh.longitude != old.longitude ||
        fresh.precision != old.precision ||
        fresh.categoryCode != old.categoryCode ||
        fresh.title != old.title ||
        fresh.subtitle != old.subtitle ||
        fresh.contentCount != old.contentCount ||
        fresh.anchorCount != old.anchorCount)
      return true;
    if (grant != null && retainedDecorationAllowed) continue;
    final pixels = old.mediaPixels;
    final freshPixels = fresh.mediaPixels;
    if ((copiedMediaIDs == null || copiedMediaIDs.contains(old.id)) &&
        pixels != null &&
        ((pixels.validUntil != null && !at.isBefore(pixels.validUntil!)) ||
            freshPixels == null ||
            !freshPixels.current() ||
            (freshPixels.validUntil != null &&
                !at.isBefore(freshPixels.validUntil!)) ||
            freshPixels.key != pixels.key ||
            (!pixels.current() &&
                (pixels.validUntil == null ||
                    freshPixels.validUntil == null)) ||
            // A successful read can replace the active grant before the old
            // deadline. Only the exact immutable cached PNGs may be reauthorized;
            // equal IDs/keys alone do not prove equal pixels.
            !identical(pixels.normalPNG, freshPixels.normalPNG) ||
            !identical(pixels.selectedPNG, freshPixels.selectedPNG) ||
            pixels.devicePixelRatio != freshPixels.devicePixelRatio)) {
      return true;
    }
    if ((copiedAuxiliaryIDs == null || copiedAuxiliaryIDs.contains(old.id)) &&
        old.recentMoments != null &&
        (!old.recentMoments!.currentAt(at) ||
            fresh.recentMoments?.labelAt(at) !=
                old.recentMoments!.labelAt(at))) {
      return true;
    }
  }
  return false;
}

@visibleForTesting
List<DateTime> amapCopiedMarkerDeadlines(
  MapEntity entity, {
  required bool mediaCopied,
  required bool auxiliaryCopied,
}) => [
  if (mediaCopied && entity.mediaPixels?.validUntil != null)
    entity.mediaPixels!.validUntil!,
  if (auxiliaryCopied && entity.recentMoments != null)
    entity.recentMoments!.validUntil,
  if (entity.readGrant != null) entity.readGrant!.validUntil,
];

class _AMapNativeCopy {
  const _AMapNativeCopy(this.entity, this.mediaCopied, this.auxiliaryCopied);
  final MapEntity entity;
  final bool mediaCopied, auxiliaryCopied;
  List<DateTime> get deadlines => amapCopiedMarkerDeadlines(
    entity,
    mediaCopied: mediaCopied,
    auxiliaryCopied: auxiliaryCopied,
  );
  bool invalidAt(DateTime at) {
    final grant = entity.readGrant, pixels = entity.mediaPixels;
    if (grant != null && !grant.authorizes(entity, at)) return true;
    if (mediaCopied &&
        (pixels == null ||
            (grant != null
                ? !grant.authorizesCopiedPixels(pixels, at)
                : !pixels.current() ||
                      (pixels.validUntil != null &&
                          !at.isBefore(pixels.validUntil!)))))
      return true;
    return auxiliaryCopied && entity.recentMoments?.currentAt(at) != true;
  }

  bool canHandoffTo(MapEntity? fresh, DateTime at) {
    if (fresh == null || deadlines.any((d) => !at.isBefore(d))) return false;
    final replacement =
        fresh.readGrant?.authorizesNativeReplacement(entity, at) == true;
    if (!replacement &&
        (invalidAt(at) ||
            amapRequiresNativeRetirement(
              previousAuthority: 'same',
              authority: 'same',
              previous: [entity],
              next: {fresh.id: fresh},
              copiedMediaIDs: {if (mediaCopied) entity.id},
              copiedAuxiliaryIDs: {if (auxiliaryCopied) entity.id},
              now: at,
            )))
      return false;
    if (entity.readGrant != null &&
        fresh.readGrant?.authorizes(fresh, at) != true)
      return false;
    if (mediaCopied) {
      final old = entity.mediaPixels, next = fresh.mediaPixels;
      if (old == null ||
          next == null ||
          !next.current() ||
          (!replacement && next.key != old.key) ||
          !identical(next.normalPNG, old.normalPNG) ||
          !identical(next.selectedPNG, old.selectedPNG) ||
          next.devicePixelRatio != old.devicePixelRatio ||
          next.validUntil == null ||
          !at.isBefore(next.validUntil!))
        return false;
    }
    return !auxiliaryCopied ||
        entity.recentMoments?.currentAt(at) == true &&
            fresh.recentMoments?.labelAt(at) ==
                entity.recentMoments?.labelAt(at);
  }

  @override
  bool operator ==(Object other) =>
      other is _AMapNativeCopy &&
      identical(entity, other.entity) &&
      mediaCopied == other.mediaCopied &&
      auxiliaryCopied == other.auxiliaryCopied;
  @override
  int get hashCode =>
      Object.hash(identityHashCode(entity), mediaCopied, auxiliaryCopied);
}

/// Serial native render control. Losing focus (for example while showing the
/// IME) is not a hidden surface and must not tear down/restart the GL renderer.
/// Content authorization still has its separate, strict resumed-only guard.
class AMapRenderControl {
  AMapRenderControl({
    required this.setPaused,
    required this.setFps,
    required this.current,
    required this.applied,
    required this.failed,
  });
  final Future<void> Function(bool) setPaused;
  final Future<void> Function(int) setFps;
  final bool Function() current;
  final void Function(bool paused, int fps) applied;
  final VoidCallback failed;
  late final _queue = LatestMapRenderQueue(_apply);
  bool _closed = false, _paused = false;
  int _fps = 30;
  bool? _lastPaused;
  int? _lastFps;

  Future<void> update(AppLifecycleState state, {required bool covered}) {
    _paused =
        state == AppLifecycleState.hidden ||
        state == AppLifecycleState.paused ||
        state == AppLifecycleState.detached;
    _fps = _paused || covered ? 5 : 30;
    return _queue.schedule();
  }

  Future<void> _apply(int serial) async {
    bool live() => !_closed && current();
    if (!live()) return;
    final paused = _paused, fps = _fps;
    try {
      if (_lastPaused != paused) {
        await setPaused(paused);
        if (!live()) return;
        _lastPaused = paused;
      }
      if (serial != _queue.serial) return;
      if (_lastFps != fps) {
        await setFps(fps);
        if (!live()) return;
        _lastFps = fps;
      }
      if (serial == _queue.serial) applied(paused, fps);
    } catch (_) {
      if (live()) failed();
    }
  }

  void close() {
    _closed = true;
    _queue.close();
  }
}

/// Birdtie owns permissions, content and WGS84 coordinates. The SDK owns only
/// the native map and its GCJ-02 display/projection. No location SDK is started.
class AMapCityMapView extends StatefulWidget {
  const AMapCityMapView({
    super.key,
    required this.city,
    required this.places,
    required this.entities,
    required this.selectedEntityId,
    required this.onPlaceSelected,
    required this.onEntitySelected,
    required this.fullBleed,
    this.contextKey = '',
    this.readerChanges,
    this.currentReaderEntities,
    this.currentReaderScope,
    this.currentRenderAuthorityScope,
    this.onMapReaderVisibilityChanged,
    this.onSelectedAnchor,
    this.onCameraMotion,
    this.onMapUnavailable,
    this.preserveViewport = false,
    this.focusRevision = 0,
    this.contentRect,
    this.onViewportSettled,
    this.onViewportInitialized,
    this.onCameraSnapshot,
    this.onContentViewportSettled,
    this.onContentOverlap,
    this.ornamentTop,
    this.onCoordinateSelected,
    this.coordinateSourceCurrent,
  });
  final PublicCity city;
  final List<PublicPlace> places;
  final List<MapEntity> entities;
  final String? selectedEntityId;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final ValueChanged<MapEntity>? onEntitySelected;
  final bool fullBleed, preserveViewport;
  final String contextKey;
  final Listenable? readerChanges;
  final List<MapEntity> Function()? currentReaderEntities;
  final Object? Function()? currentReaderScope;
  final Object? Function()? currentRenderAuthorityScope;
  final ValueChanged<({Object token, bool visible})>?
  onMapReaderVisibilityChanged;
  final ValueChanged<({String? id, Offset? point})>? onSelectedAnchor;
  final VoidCallback? onCameraMotion;
  final ValueChanged<String>? onMapUnavailable;
  final int focusRevision;
  final Rect? contentRect;
  final ValueChanged<MapCameraSnapshot>? onCameraSnapshot;
  final ValueChanged<MapBounds>? onViewportSettled, onViewportInitialized;
  final void Function(MapBounds bounds, double zoom)? onContentViewportSettled;
  final ValueChanged<List<MapEntity>>? onContentOverlap;
  final double? ornamentTop;
  final void Function(double latitude, double longitude)? onCoordinateSelected;
  final bool Function()? coordinateSourceCurrent;

  @override
  State<AMapCityMapView> createState() => _AMapCityMapViewState();
}

class _AMapCityMapViewState extends State<AMapCityMapView>
    with WidgetsBindingObserver {
  final _changes = MapReaderChanges();
  final _diagnostics = NativeMapDiagnostics(
    provider: NativeMapDiagnosticProvider.amap,
  );
  late final _renderQueue = LatestMapRenderQueue(_render);
  amap.AMapController? _map;
  AMapRenderControl? _renderControl;
  AppLifecycleState _renderLifecycle = AppLifecycleState.resumed;
  amap.CameraPosition? _camera;
  MapCameraFocusInput? _focus;
  Map<String, MapEntity> _rows = const {};
  Map<String, amap.Marker> _markers = const {};
  Map<String, String> _markerKeys = const {};
  Map<String, Offset> _positions = const {};
  // Geometry is reusable across cover changes, but only for this camera and
  // the current bounded source. It grants neither a click target nor access.
  Map<String, ({double latitude, double longitude, Offset point})> _geometry =
      const {};
  int _geometryCamera = -1;
  List<MapEntity> _displayed = const [];
  Map<String, MapLabelPlacement> _placements = const {};
  Set<String> _previousVisible = const {};
  double? _metersPerPixel;
  int _metersPerPixelCamera = -1;
  String _lastLayoutDiagnostic = '';
  int _pointerEpoch = 0;
  Completer<amap.CameraPosition?>? _fitCamera;
  final Set<Object> _readerTokens = {};
  Object? _sourceScope;
  Object? _sourceAuthority;
  Set<String> _copiedMediaIDs = const {}, _copiedAuxiliaryIDs = const {};
  Set<String> _nativeSourceIDs = const {};
  List<_AMapNativeCopy> _nativeCopies = const [];
  List<DateTime> _copiedDeadlines = const [];
  bool _nativeWritePending = false;
  String _sourceKey = '';
  int _sourceEpoch = 0, _cameraEpoch = 0, _sdkEpoch = 0, _focusSerial = 0;
  bool _foreground = true, _configured = false, _moving = false;
  bool _retiring = false, _choiceOpen = false, _initialized = false;
  String? _failure;
  AMapNativeConfiguration? _keyConfiguration;
  int _configurationRequest = 0;
  amap.CustomStyleOptions? _offlineStyle;
  bool _styleFailed = false;
  int _styleRequest = 0;
  Size _size = Size.zero;
  double _ratio = 1, _screenRatio = 1;
  Timer? _settle, _expiry, _startup;
  bool _privacyPromptAttempted = false;
  bool _privacyPreparationQueued = false;

  bool get _supported =>
      defaultTargetPlatform == TargetPlatform.android ||
      defaultTargetPlatform == TargetPlatform.iOS;
  bool get _styleReady =>
      defaultTargetPlatform != TargetPlatform.android || _offlineStyle != null;
  // Native Android projection uses physical pixels; iOS uses logical points.
  double get _projectionScale =>
      defaultTargetPlatform == TargetPlatform.android ? _screenRatio : 1;
  Object _scope() => (
    widget.city.id,
    widget.city.map?.navigationGeneration,
    AMapSessionConsent.controller.generation,
    AMapSessionConsent.accepted,
    widget.contextKey,
    widget.currentReaderScope?.call(),
    _foreground,
  );
  Object _renderAuthorityScope() => (
    widget.city.id,
    widget.city.map?.navigationGeneration,
    AMapSessionConsent.controller.generation,
    AMapSessionConsent.accepted,
    widget.contextKey,
    // Legacy callers retain the conservative, full reader-scope boundary.
    (widget.currentRenderAuthorityScope ?? widget.currentReaderScope)?.call(),
    _foreground,
  );
  List<MapEntity> _source() =>
      widget.currentReaderEntities?.call() ??
      [
        ...widget.entities,
        for (final p in widget.places)
          if (p.location.hasPublicPoint)
            MapEntity(
              id: 'place:${p.id}',
              kind: MapEntityKind.place,
              title: p.name,
              subtitle: p.summary,
              latitude: p.location.latitude!,
              longitude: p.location.longitude!,
              placeID: p.id,
              categoryCode: p.categoryCode,
              publicLocation: true,
            ),
      ];
  MapCameraFocusInput _focusInput() => MapCameraFocusInput(
    city: widget.city,
    contextKey: widget.contextKey,
    places: const [],
    entities: _rows.values.toList(),
    selectedEntityId: widget.selectedEntityId,
    preserveViewport: widget.preserveViewport,
  );
  abase.LatLng _displayCoordinate(double latitude, double longitude) {
    final point = MapCoordinateBoundary.toAmap(latitude, longitude);
    return abase.LatLng(point.latitude, point.longitude);
  }

  @override
  void initState() {
    super.initState();
    _diagnostics.mounted();
    if (WidgetsBinding.instance.lifecycleState case final state?) {
      _diagnostics.lifecycle(state.index);
      _renderLifecycle = state;
      _foreground = state == AppLifecycleState.resumed;
    }
    WidgetsBinding.instance.addObserver(this);
    widget.readerChanges?.addListener(_sourceChanged);
    AMapSessionConsent.controller.addListener(_privacyChanged);
    _syncSource();
    unawaited(_readKeyConfiguration());
    if (defaultTargetPlatform == TargetPlatform.android) {
      unawaited(_loadOfflineStyle());
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(_preparePrivacy());
    });
  }

  Future<void> _preparePrivacy({bool review = false}) async {
    if (!_supported || _keyConfiguration?.configured != true || !_styleReady)
      return;
    final consent = AMapSessionConsent.controller;
    await consent.load();
    if (!mounted) return;
    if (consent.accepted && !review) {
      _configureAfterConsent();
      if (mounted) setState(() {});
      return;
    }
    if (!review && (consent.hasDecision || _privacyPromptAttempted)) return;
    _privacyPromptAttempted = true;
    final navigation = widget.city.map?.navigationGeneration;
    await showMapServicePrivacySheet(
      context,
      current: () =>
          mounted && navigation == widget.city.map?.navigationGeneration,
      reviewExisting: review,
    );
    if (mounted) _privacyChanged();
  }

  void _privacyChanged() {
    if (!mounted) return;
    if (!AMapSessionConsent.accepted && _configured) {
      _cancelFitCamera();
      ++_sdkEpoch;
      ++_sourceEpoch;
      ++_cameraEpoch;
      ++_focusSerial;
      _configured = false;
      _initialized = false;
      _map = null;
      _markers = const {};
      _markerKeys = const {};
      _displayed = const [];
      _positions = const {};
      _geometry = const {};
      _placements = const {};
      _previousVisible = const {};
      _startup?.cancel();
      _settle?.cancel();
      _expiry?.cancel();
      amap.AMapInitializer.updatePrivacyAgree(
        const abase.AMapPrivacyStatement(
          hasContains: true,
          hasShow: true,
          hasAgree: false,
        ),
      );
    }
    _configureAfterConsent();
    setState(() {});
  }

  Future<void> _loadOfflineStyle() async {
    final request = ++_styleRequest;
    try {
      final style = await androidAMapOfflineStyle.load();
      if (!mounted || request != _styleRequest) return;
      setState(() {
        _offlineStyle = style;
        _styleFailed = false;
      });
      // Local style bytes are ready; this is not an SDK render/loaded event.
      _diagnostics.stylePrepared();
      _configureAfterConsent();
    } catch (_) {
      if (!mounted || request != _styleRequest) return;
      _diagnostics.resourceError(
        resource: NativeMapResource.style,
        reason: NativeMapFailureReason.unavailable,
        stage: NativeMapDiagnosticFailureStage.localStyle,
      );
      setState(() => _styleFailed = true);
    }
  }

  void _retryOfflineStyle() {
    setState(() => _styleFailed = false);
    unawaited(_loadOfflineStyle());
  }

  Future<void> _readKeyConfiguration() async {
    final request = ++_configurationRequest;
    final configuration = await AMapNativeConfiguration.inspect();
    if (!mounted || request != _configurationRequest) return;
    setState(() => _keyConfiguration = configuration);
    _configureAfterConsent();
  }

  void _retryKeyConfiguration() {
    setState(() => _keyConfiguration = null);
    unawaited(_readKeyConfiguration());
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final actual = MediaQuery.devicePixelRatioOf(context);
    final screenRatio = actual.isFinite && actual > 0 ? actual : 1.0;
    final ratio = MapMarkerBitmap.pixelRatio(screenRatio);
    if (_ratio != ratio || _screenRatio != screenRatio) {
      _ratio = ratio;
      _screenRatio = screenRatio;
      ++_cameraEpoch;
      _markerKeys = const {};
      unawaited(_renderQueue.schedule());
    }
    _configureAfterConsent();
  }

  void _configureAfterConsent() {
    if (!_configured &&
        _supported &&
        _keyConfiguration?.configured == true &&
        _styleReady &&
        !AMapSessionConsent.accepted &&
        !_privacyPromptAttempted &&
        !_privacyPreparationQueued &&
        !AMapSessionConsent.controller.hasDecision) {
      _privacyPreparationQueued = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _privacyPreparationQueued = false;
        if (mounted) unawaited(_preparePrivacy());
      });
    }
    if (_configured ||
        !_supported ||
        _keyConfiguration?.configured != true ||
        !_styleReady ||
        !AMapSessionConsent.accepted) {
      return;
    }
    // This statement is reached only after the service disclosure was shown
    // and the user pressed the explicit consent button in this app session.
    amap.AMapInitializer.updatePrivacyAgree(
      const abase.AMapPrivacyStatement(
        hasContains: true,
        hasShow: true,
        hasAgree: true,
      ),
    );
    amap.AMapInitializer.init(
      context,
      // Null preserves Android's existing manifest configuration. An empty
      // Dart key must not override the native key.
      apiKey: _keyConfiguration!.apiKey,
    );
    _configured = true;
    _armStartup();
  }

  void _armStartup() {
    _startup?.cancel();
    final sdk = _sdkEpoch;
    _startup = Timer(const Duration(seconds: 20), () {
      if (mounted && sdk == _sdkEpoch && _map == null) {
        _fail('高德地图暂未加载，请重试或使用地点列表。');
      }
    });
  }

  void _change(VoidCallback change) {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(change);
      });
    } else {
      setState(change);
    }
  }

  @override
  void didUpdateWidget(AMapCityMapView old) {
    super.didUpdateWidget(old);
    if (old.readerChanges != widget.readerChanges) {
      old.readerChanges?.removeListener(_sourceChanged);
      widget.readerChanges?.addListener(_sourceChanged);
    }
    if (old.city.id != widget.city.id) _initialized = false;
    _syncSource(forceFocus: old.focusRevision != widget.focusRevision);
    if (old.selectedEntityId != widget.selectedEntityId ||
        old.contentRect != widget.contentRect) {
      unawaited(_renderQueue.schedule());
    }
  }

  void _sourceChanged() {
    if (mounted) _syncSource();
  }

  void _syncSource({bool forceFocus = false}) {
    if (!mounted) return;
    final scope = _scope();
    final authority = _renderAuthorityScope();
    final unique = <String, MapEntity>{};
    if (_foreground) {
      for (final e in _source()) {
        if (e.hasMapAnchor &&
            e.id.isNotEmpty &&
            (e.readGrant == null ||
                e.readGrant!.authorizes(e, DateTime.now().toUtc())))
          unique.putIfAbsent(e.id, () => e);
      }
    }
    final ids = unique.keys.toList()..sort();
    final key = jsonEncode([
      for (final id in ids)
        [
          id,
          unique[id]!.kind.name,
          unique[id]!.sourceVersion,
          unique[id]!.latitude,
          unique[id]!.longitude,
          unique[id]!.title,
          unique[id]!.subtitle,
          unique[id]!.decorationKey,
          unique[id]!.mediaPixels?.validUntil?.microsecondsSinceEpoch,
          unique[id]!.readGrant?.validUntil.microsecondsSinceEpoch,
          unique[id]!.memberIDs,
        ],
    ]);
    if (scope != _sourceScope ||
        authority != _sourceAuthority ||
        key != _sourceKey ||
        _nativeCopies.any((copy) => copy.invalidAt(DateTime.now().toUtc()))) {
      final authorityChanged = authority != _sourceAuthority;
      final now = DateTime.now().toUtc();
      if (!authorityChanged && !_retiring && !_nativeWritePending) {
        // A new verified response may independently authorize unchanged native
        // facts/bytes even when its permission source version changed. Only
        // already copied display data transfers; old readers remain retired.
        _nativeCopies = [
          for (final copy in _nativeCopies)
            if (copy.canHandoffTo(unique[copy.entity.id], now))
              _AMapNativeCopy(
                unique[copy.entity.id]!,
                copy.mediaCopied,
                copy.auxiliaryCopied,
              )
            else
              copy,
        ];
        _copiedDeadlines = [
          for (final copy in _nativeCopies) ...copy.deadlines,
        ];
      }
      final copiedIDs = {..._copiedMediaIDs, ..._copiedAuxiliaryIDs};
      final retire =
          authorityChanged ||
          _copiedDeadlines.any((d) => !now.isBefore(d)) ||
          _nativeCopies.any(
            (copy) =>
                copy.invalidAt(now) ||
                amapRequiresNativeRetirement(
                  previousAuthority: _sourceAuthority,
                  authority: authority,
                  previous: [copy.entity],
                  next: unique,
                  copiedMediaIDs: {if (copy.mediaCopied) copy.entity.id},
                  copiedAuxiliaryIDs: {
                    if (copy.auxiliaryCopied) copy.entity.id,
                  },
                  now: now,
                ),
          );
      if (kProfileMode && retire) {
        var invalidReadGrant = 0, invalidCopiedPixels = 0, expiredAux = 0;
        var sameIDSourceChanged = 0, sameIDFactsChanged = 0;
        var freshMatchButOldGrantFalse = 0, strictNoGrant = 0;
        for (final copy in _nativeCopies) {
          final old = copy.entity, fresh = unique[copy.entity.id];
          final grant = old.readGrant;
          final readInvalid = grant != null && !grant.authorizes(old, now);
          if (readInvalid) invalidReadGrant++;
          if (copy.mediaCopied &&
              old.mediaPixels != null &&
              grant?.authorizesCopiedPixels(old.mediaPixels!, now) == false)
            invalidCopiedPixels++;
          if (copy.auxiliaryCopied && old.recentMoments?.currentAt(now) != true)
            expiredAux++;
          if (fresh == null) continue;
          final sourceChanged = fresh.sourceVersion != old.sourceVersion;
          final factsChanged =
              fresh.kind != old.kind ||
              fresh.latitude != old.latitude ||
              fresh.longitude != old.longitude ||
              fresh.precision != old.precision ||
              fresh.categoryCode != old.categoryCode ||
              fresh.title != old.title ||
              fresh.subtitle != old.subtitle ||
              fresh.contentCount != old.contentCount ||
              fresh.anchorCount != old.anchorCount;
          if (sourceChanged) sameIDSourceChanged++;
          if (factsChanged) sameIDFactsChanged++;
          if (readInvalid &&
              !sourceChanged &&
              !factsChanged &&
              fresh.readGrant?.authorizes(fresh, now) == true)
            freshMatchButOldGrantFalse++;
          if (grant == null &&
              amapRequiresNativeRetirement(
                previousAuthority: authority,
                authority: authority,
                previous: [old],
                next: unique,
                copiedMediaIDs: {if (copy.mediaCopied) old.id},
                copiedAuxiliaryIDs: {if (copy.auxiliaryCopied) old.id},
                now: now,
              ))
            strictNoGrant++;
        }
        debugPrint(
          'BIRDTIE_AMAP_RETIRE ${jsonEncode({'stage': 'source', 'authorityChanged': authorityChanged, 'moving': _moving, 'nativeWritePending': _nativeWritePending, 'expiredCopy': _copiedDeadlines.any((d) => !now.isBefore(d)), 'missingCopy': copiedIDs.any((id) => !unique.containsKey(id)), 'invalidReadGrant': invalidReadGrant, 'invalidCopiedPixels': invalidCopiedPixels, 'expiredAux': expiredAux, 'sameIDSourceChanged': sameIDSourceChanged, 'sameIDFactsChanged': sameIDFactsChanged, 'freshMatchButOldGrantFalse': freshMatchButOldGrantFalse, 'strictNoGrant': strictNoGrant})}',
        );
      }
      if (retire) _retiring = true;
      ++_sourceEpoch;
      _sourceScope = scope;
      _sourceAuthority = authority;
      _sourceKey = key;
      _rows = Map.unmodifiable(unique);
      _geometry = authorityChanged
          ? const {}
          : {
              for (final id in unique.keys)
                if (_geometry[id] case final point?) id: point,
            };
      _positions = const {};
      _displayed = const [];
      if (retire) _placements = const {};
      _reportAnchor(null);
      _change(() {});
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _changes.changed();
      });
      _armExpiry();
      unawaited(_renderQueue.schedule());
    }
    final next = _focusInput(), previous = _focus;
    _focus = next;
    if (_map != null &&
        (forceFocus || previous == null || next.shouldFocusAfter(previous))) {
      unawaited(_focusCamera(next));
    }
  }

  void _armExpiry() {
    _expiry?.cancel();
    final now = DateTime.now().toUtc();
    // Every copied source has its own finite grant, including bare names and
    // coordinates. Unpainted Place covers/counts still add no pixel deadline.
    final deadlines = _copiedDeadlines.where((d) => d.isAfter(now)).toList();
    if (deadlines.isEmpty) return;
    deadlines.sort();
    _expiry = Timer(deadlines.first.difference(now), () {
      if (!mounted) return;
      if (kProfileMode)
        debugPrint('BIRDTIE_AMAP_RETIRE {"stage":"copied_expiry"}');
      // Retire copied native pixels at the grant deadline, including when the
      // source's identity stays stable and only its media grant has expired.
      _retiring = true;
      _change(() {});
      _syncSource();
      _markerKeys = const {};
      unawaited(_renderQueue.schedule());
      _armExpiry();
    });
  }

  void _onCreated(amap.AMapController map, int sdk) {
    if (!mounted || sdk != _sdkEpoch) return;
    // AMapController.init waits for map#waitForMap before delivering this
    // callback. It is readiness evidence, not a presented native frame.
    _diagnostics.created();
    _diagnostics.mapLoaded();
    _startup?.cancel();
    _map = map;
    _renderControl?.close();
    _renderControl = AMapRenderControl(
      setPaused: (paused) async {
        if (defaultTargetPlatform == TargetPlatform.android) {
          await map.setRenderingPaused(paused);
        }
      },
      setFps: map.setRenderFps,
      current: () =>
          mounted &&
          map == _map &&
          sdk == _sdkEpoch &&
          _configured &&
          AMapSessionConsent.accepted,
      applied: (paused, fps) => _diagnostics.renderingState(
        paused: defaultTargetPlatform == TargetPlatform.android ? paused : null,
        fps: fps,
      ),
      failed: () => _diagnostics.resourceError(
        resource: NativeMapResource.other,
        reason: NativeMapFailureReason.unavailable,
        stage: NativeMapDiagnosticFailureStage.renderControl,
      ),
    );
    unawaited(_setRenderingState());
    _change(() => _failure = null);
    unawaited(_focusCamera(_focusInput()));
    _scheduleSettled();
  }

  Future<void> _focusCamera(MapCameraFocusInput input) async {
    final map = _map, source = _sourceEpoch, serial = ++_focusSerial;
    final pointer = _pointerEpoch;
    if (map == null || input.center == null || !_foreground) return;
    bool current() =>
        mounted &&
        map == _map &&
        source == _sourceEpoch &&
        serial == _focusSerial;
    try {
      final rect = widget.contentRect;
      if (rect != null && !_size.isEmpty) {
        await map.setScreenAnchor(
          Offset(
            (rect.center.dx / _size.width).clamp(.05, .95),
            (rect.center.dy / _size.height).clamp(.05, .95),
          ),
        );
        if (!current()) return;
      }
      if (input.fitsBounds) {
        final orientation = input.bearing != 0 || input.pitch != 0;
        final fitted = orientation ? Completer<amap.CameraPosition?>() : null;
        if (fitted != null) {
          _cancelFitCamera();
          _fitCamera = fitted;
        }
        final points = input.points
            .map((p) => _displayCoordinate(p.latitude, p.longitude))
            .toList();
        await map.moveCamera(
          amap.CameraUpdate.newLatLngBounds(
            abase.LatLngBounds(
              southwest: abase.LatLng(
                points.map((p) => p.latitude).reduce(math.min),
                points.map((p) => p.longitude).reduce(math.min),
              ),
              northeast: abase.LatLng(
                points.map((p) => p.latitude).reduce(math.max),
                points.map((p) => p.longitude).reduce(math.max),
              ),
            ),
            48,
          ),
        );
        if (fitted != null) {
          final camera = await fitted.future.timeout(
            const Duration(seconds: 3),
            onTimeout: () => null,
          );
          if (identical(_fitCamera, fitted)) _fitCamera = null;
          if (camera == null ||
              !mounted ||
              map != _map ||
              source != _sourceEpoch ||
              pointer != _pointerEpoch ||
              !input.hasSameViewInputs(_focusInput()))
            return;
          await map.moveCamera(
            amap.CameraUpdate.newCameraPosition(
              amap.CameraPosition(
                target: camera.target,
                zoom: camera.zoom,
                bearing: input.bearing,
                tilt: input.pitch,
              ),
            ),
          );
        }
      } else {
        await map.moveCamera(
          amap.CameraUpdate.newCameraPosition(
            amap.CameraPosition(
              target: _displayCoordinate(
                input.center!.latitude,
                input.center!.longitude,
              ),
              zoom: input.zoom,
              bearing: input.bearing,
              tilt: input.pitch,
            ),
          ),
        );
      }
      if (current()) _scheduleSettled();
    } catch (_) {
      if (current()) _fail('地图视角暂时无法更新，请重试。');
    }
  }

  void _onMove(amap.CameraPosition camera) {
    if (!mounted) return;
    final wasMoving = _moving;
    _diagnostics.cameraMoving();
    _camera = camera;
    ++_cameraEpoch;
    ++_focusSerial;
    _settle?.cancel();
    if (!wasMoving) widget.onCameraMotion?.call();
    _moving = true;
    _positions = const {};
    _reportAnchor(null);
    // Only the first tick changes visible overlays. Camera coordinates and
    // epochs keep advancing without rebuilding the platform view each tick.
    if (!wasMoving) _change(() {});
  }

  void _onMoveEnd(amap.CameraPosition camera) {
    if (mounted) _diagnostics.cameraSettled();
    _camera = camera;
    _moving = false;
    ++_cameraEpoch;
    final fitted = _fitCamera;
    if (fitted != null && !fitted.isCompleted) fitted.complete(camera);
    _scheduleSettled();
  }

  void _cancelFitCamera() {
    final fitted = _fitCamera;
    _fitCamera = null;
    if (fitted != null && !fitted.isCompleted) fitted.complete(null);
  }

  void _scheduleSettled() {
    _settle?.cancel();
    final sdk = _sdkEpoch, camera = _cameraEpoch;
    _settle = Timer(const Duration(milliseconds: 180), () {
      if (mounted && sdk == _sdkEpoch && camera == _cameraEpoch && !_moving) {
        unawaited(_publishViewport());
        unawaited(_renderQueue.schedule());
      }
    });
  }

  Future<void> _publishViewport() async {
    final map = _map, camera = _cameraEpoch, scope = _scope();
    if (map == null || _size.isEmpty || !_foreground || _moving) return;
    bool current() =>
        mounted &&
        map == _map &&
        camera == _cameraEpoch &&
        scope == _scope() &&
        !_moving;
    try {
      final size = _size, scale = _projectionScale;
      final logical = [
        Offset.zero,
        Offset(size.width, 0),
        Offset(size.width, size.height),
        Offset(0, size.height),
        Offset(size.width / 2 - 24, size.height / 2),
        Offset(size.width / 2 + 24, size.height / 2),
        Offset(size.width / 2, size.height / 2),
      ];
      final projected = await Future.wait(
        logical.map(
          (p) => map.fromScreenCoordinate(
            amap.ScreenCoordinate(
              x: (p.dx * scale).round(),
              y: (p.dy * scale).round(),
            ),
          ),
        ),
      ).timeout(const Duration(seconds: 3));
      if (!current()) return;
      final canonical = projected.map((p) {
        final point = MapCoordinateBoundary.fromAmap(p.latitude, p.longitude);
        return Offset(point.longitude, point.latitude);
      }).toList();
      final bounds = MapDisplayPolicy.projectedViewport(
        corners: canonical.take(4).toList(),
        center: canonical.last,
      );
      if (bounds == null) return;
      _metersPerPixel = MapDisplayPolicy.scaleFromSegment(
        canonical[4],
        canonical[5],
        48,
      );
      _metersPerPixelCamera = camera;
      final snapshot = MapCameraSnapshot(
        latitude: canonical.last.dy,
        longitude: canonical.last.dx,
        zoom: _camera?.zoom ?? widget.city.map!.defaultZoom,
        bearing: _camera?.bearing ?? widget.city.map!.bearing,
        pitch: _camera?.tilt ?? widget.city.map!.pitch,
      );
      if (!_initialized) {
        _initialized = true;
        widget.onViewportInitialized?.call(bounds);
      }
      if (!current()) {
        // Initial viewport consumers may synchronously start their own source
        // reads and change reader scope. Reproject under the new scope rather
        // than dropping the first content query until the user's first pan.
        if (mounted && map == _map && !_moving && _foreground) {
          _scheduleSettled();
        }
        return;
      }
      widget.onViewportSettled?.call(bounds);
      widget.onContentViewportSettled?.call(
        bounds,
        _camera?.zoom ?? widget.city.map!.defaultZoom,
      );
      if (snapshot.isValid) widget.onCameraSnapshot?.call(snapshot);
      if (mounted && map == _map && camera == _cameraEpoch && !_moving) {
        unawaited(_renderQueue.schedule());
      }
    } catch (_) {
      if (current()) _fail('地图范围暂时无法读取，请移动地图重试。');
    }
  }

  void _reportAnchor(Offset? point) {
    final source = _sourceEpoch,
        camera = _cameraEpoch,
        selected = widget.selectedEntityId;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted &&
          source == _sourceEpoch &&
          camera == _cameraEpoch &&
          selected == widget.selectedEntityId) {
        widget.onSelectedAnchor?.call((id: selected, point: point));
      }
    });
  }

  Future<void> _render(int serial) async {
    final map = _map,
        source = _sourceEpoch,
        camera = _cameraEpoch,
        sdk = _sdkEpoch;
    final scope = _scope();
    final authority = _renderAuthorityScope();
    if (map == null || _size.isEmpty) return;
    // This priority phase is the sole writer, in the same latest-work queue as
    // layout. Removing copied private pixels must not wait for a drag to stop.
    // Camera ticks invalidate geometry, not this permission-retirement ACK.
    bool retirementCurrent() =>
        mounted &&
        map == _map &&
        sdk == _sdkEpoch &&
        serial == _renderQueue.serial &&
        source == _sourceEpoch &&
        scope == _scope() &&
        authority == _renderAuthorityScope();
    if (_retiring) {
      try {
        if (!retirementCurrent()) return;
        _nativeWritePending = true;
        _change(() {
          if (!retirementCurrent()) return;
          _markers = const {};
          _markerKeys = const {};
          _positions = const {};
          _displayed = const [];
          _placements = const {};
        });
        await WidgetsBinding.instance.endOfFrame;
        if (!retirementCurrent()) return;
        await map
            .toScreenCoordinate(
              _camera?.target ??
                  _displayCoordinate(
                    widget.city.map!.latitude,
                    widget.city.map!.longitude,
                  ),
            )
            .timeout(const Duration(seconds: 3));
        if (!retirementCurrent()) return;
        _copiedMediaIDs = const {};
        _copiedAuxiliaryIDs = const {};
        _copiedDeadlines = const [];
        _nativeSourceIDs = const {};
        _nativeCopies = const [];
        _nativeWritePending = false;
        _armExpiry();
        _change(() {
          if (retirementCurrent()) _retiring = false;
        });
      } catch (_) {
        if (retirementCurrent()) _fail('地图内容暂时无法显示，请重试。');
        return;
      }
    }
    if (_moving || !retirementCurrent()) return;
    bool current() =>
        mounted &&
        map == _map &&
        serial == _renderQueue.serial &&
        source == _sourceEpoch &&
        camera == _cameraEpoch &&
        scope == _scope() &&
        authority == _renderAuthorityScope() &&
        !_moving;
    final rows = _rows.values
        .where(
          (e) =>
              e.readGrant == null ||
              e.readGrant!.authorizes(e, DateTime.now().toUtc()),
        )
        .toList();
    final positions = <String, Offset>{};
    final geometry =
        <String, ({double latitude, double longitude, Offset point})>{};
    final missing = <MapEntity>[];
    for (final e in rows) {
      final cached = _geometryCamera == camera ? _geometry[e.id] : null;
      if (cached != null &&
          cached.latitude == e.latitude &&
          cached.longitude == e.longitude) {
        positions[e.id] = cached.point;
        geometry[e.id] = cached;
      } else {
        missing.add(e);
      }
    }
    try {
      // At most four projection calls are active; a superseding source or
      // camera invalidates the remainder rather than accumulating a queue.
      for (var start = 0; start < missing.length; start += 4) {
        final batch = missing.skip(start).take(4).toList();
        final values = await Future.wait(
          batch.map(
            (e) => map.toScreenCoordinate(
              _displayCoordinate(e.latitude, e.longitude),
            ),
          ),
        ).timeout(const Duration(seconds: 3));
        if (!current()) return;
        for (var i = 0; i < batch.length; i++) {
          final point = Offset(
            values[i].x / _projectionScale,
            values[i].y / _projectionScale,
          );
          positions[batch[i].id] = point;
          geometry[batch[i].id] = (
            latitude: batch[i].latitude,
            longitude: batch[i].longitude,
            point: point,
          );
        }
      }
      final plan = MapDisplayPolicy.plan(
        entities: rows,
        positions: positions,
        zoom: _camera?.zoom ?? widget.city.map!.defaultZoom,
        selectedID: widget.selectedEntityId,
        contentRect: widget.contentRect,
        previouslyVisibleIDs: _previousVisible,
        previousPlacements: _placements,
        metersPerPixel: _metersPerPixelCamera == camera
            ? _metersPerPixel
            : null,
        protectedIDs: {
          for (final e in rows)
            if (e.isTaskResult) e.id,
        },
      );
      if (kProfileMode) {
        final metrics = jsonEncode({
          'provider': 'amap',
          'cameraEpoch': camera,
          'metersPerLogicalPixel': plan.metersPerPixel,
          'screenRatio': _screenRatio,
          'width': _size.width,
          'height': _size.height,
          'visible': plan.entities.length - plan.occludedGlyphIDs.length,
          'occludedGlyphs': plan.occludedGlyphIDs.length,
          'dense': plan.denseIDs.length,
          'labels': plan.placements.length,
          'auxiliary': plan.placements.values
              .where((p) => p.auxiliary != null)
              .length,
        });
        if (metrics != _lastLayoutDiagnostic) {
          _lastLayoutDiagnostic = metrics;
          debugPrint('BIRDTIE_MAP_LAYOUT $metrics');
        }
      }
      final next = <String, amap.Marker>{}, keys = <String, String>{};
      final copiedMediaIDs = <String>{}, copiedAuxiliaryIDs = <String>{};
      final copiedGuards = <bool Function()>[];
      final visibleEntities = plan.entities
          .where((e) => !plan.occludedGlyphIDs.contains(e.id))
          .toList();
      final nativeSourceIDs = {
        for (final e in visibleEntities)
          if (_rows.containsKey(e.id)) e.id else ...e.memberIDs,
      };
      for (final e in visibleEntities) {
        final placement = plan.placements[e.id];
        final selected = e.id == widget.selectedEntityId;
        final dense = plan.denseIDs.contains(e.id);
        final media = e.kind == MapEntityKind.moment ? e.mediaPixels : null;
        // Pixels originate in Birdtie's existing permission-aware 12-item /
        // four-request media controllers. This adapter never fetches media.
        final png = media?.png(selected: selected, dense: dense, ratio: _ratio);
        if (png != null) copiedMediaIDs.add(e.id);
        if (placement?.auxiliary != null) copiedAuxiliaryIDs.add(e.id);
        if (png != null) {
          copiedGuards.add(
            () =>
                media!.current() &&
                (media.validUntil == null ||
                    DateTime.now().toUtc().isBefore(media.validUntil!)),
          );
        }
        if (placement?.auxiliary != null) {
          copiedGuards.add(
            () =>
                e.recentMoments?.labelAt(DateTime.now().toUtc()) ==
                placement!.auxiliary,
          );
        }
        final key = jsonEncode([
          e.kind.name,
          e.sourceVersion,
          e.latitude,
          e.longitude,
          e.categoryCode,
          e.precision,
          e.contentCount,
          e.anchorCount,
          e.memberIDs.length,
          selected,
          dense,
          _ratio,
          _screenRatio,
          if (png != null) media!.key,
          if (placement != null)
            [
              placement.name,
              placement.auxiliary,
              placement.offset.dx,
              placement.offset.dy,
              placement.rect.width,
              placement.rect.height,
              placement.fontSize,
              placement.color.toARGB32(),
            ],
        ]);
        keys[e.id] = key;
        if (_markerKeys[e.id] == key && _markers[e.id] != null) {
          next[e.id] = _markers[e.id]!;
          continue;
        }
        final bytes =
            png ??
            await MapMarkerBitmap.render(
              kind: e.kind,
              selected: selected,
              dense: dense,
              count: e.isCluster
                  ? MapMarkerVisual.clusterCount(
                      categoryCode: e.categoryCode,
                      contentCount: e.contentCount,
                      anchorCount: e.anchorCount,
                      fallback: e.memberIDs.length,
                    )
                  : 0,
              approximate: e.precision == 'area',
              devicePixelRatio: _ratio,
              categoryCode: e.categoryCode,
            );
        if (!current()) return;
        if (png != null && media?.current() != true) {
          unawaited(_renderQueue.schedule());
          return;
        }
        final compact =
            e.kind == MapEntityKind.cluster ||
            (e.precision == 'point' &&
                {MapEntityKind.place, MapEntityKind.moment}.contains(e.kind));
        // The plugin passes Android PNG pixels through unchanged; iOS divides
        // their dimensions by UIScreen.scale. Match the real screen scale,
        // independently of bitmap raster/cache budget and projection units.
        final bitmapRatio = png != null
            ? media!.devicePixelRatio
            : e.kind == MapEntityKind.cluster ||
                  (e.precision != 'area' &&
                      {
                        MapEntityKind.place,
                        MapEntityKind.moment,
                      }.contains(e.kind))
            ? _ratio
            : 1.0;
        final nativeBytes = await _nativePNG(bytes, bitmapRatio);
        if (!current()) return;
        if (png != null && media?.current() != true) {
          unawaited(_renderQueue.schedule());
          return;
        }
        var markerBytes = nativeBytes;
        var markerAnchor = compact ? const Offset(.5, .5) : const Offset(.5, 1);
        if (placement != null) {
          bool labelCurrent() =>
              current() &&
              (png == null || media?.current() == true) &&
              (placement.auxiliary == null ||
                  e.recentMoments?.labelAt(DateTime.now().toUtc()) ==
                      placement.auxiliary);
          final composed = await MapMarkerBitmap.withNativeLabel(
            glyph: nativeBytes,
            devicePixelRatio: _screenRatio,
            glyphAnchor: markerAnchor,
            placement: placement,
            current: labelCurrent,
          );
          if (!current()) return;
          if (composed == null || !labelCurrent()) {
            unawaited(_renderQueue.schedule());
            return;
          }
          markerBytes = composed.png;
          markerAnchor = composed.anchor;
        }
        final marker = amap.Marker(
          position: _displayCoordinate(e.latitude, e.longitude),
          anchor: markerAnchor,
          icon: amap.BitmapDescriptor.fromBytes(markerBytes),
          clickable: false,
          infoWindowEnable: false,
          zIndex: selected ? 100 : 0,
        )..setIdForCopy(e.id);
        next[e.id] = marker;
      }
      if (!current()) return;
      if (copiedGuards.any((live) => !live())) {
        unawaited(_renderQueue.schedule());
        return;
      }
      final nextPositions = {...positions, ...plan.clusterPositions};
      final nativeChanged = !mapEquals(_markerKeys, keys);
      final overlayChanged =
          !mapEquals(_positions, nextPositions) ||
          !mapEquals(_placements, plan.placements) ||
          !listEquals(
            _displayed.map((e) => e.id).toList(),
            visibleEntities.map((e) => e.id).toList(),
          );
      // These are geometry caches, never permission or read grants.
      _geometry = geometry;
      _geometryCamera = camera;
      final barrierNeeded = nativeChanged || _retiring;
      // Until the SDK acknowledges removal, it may still hold an old bitmap
      // clipped out by the new content rect. Keep that copy's live guards and
      // deadline, even though the next display plan no longer paints it.
      if (_copiedDeadlines.any((d) => !DateTime.now().toUtc().isBefore(d))) {
        _retiring = true;
        _change(() {});
        unawaited(_renderQueue.schedule());
        return;
      }
      final pendingBarrier = barrierNeeded || _nativeWritePending || _retiring;
      final nativeCopies = [
        for (final id in nativeSourceIDs)
          _AMapNativeCopy(
            _rows[id]!,
            copiedMediaIDs.contains(id),
            copiedAuxiliaryIDs.contains(id),
          ),
      ];
      final pendingCopies = pendingBarrier
          ? {..._nativeCopies, ...nativeCopies}.toList()
          : nativeCopies;
      if (pendingCopies.length > 2048 ||
          pendingCopies.any((copy) => copy.invalidAt(DateTime.now().toUtc()))) {
        _retiring = true;
        _change(() {});
        unawaited(_renderQueue.schedule());
        return;
      }
      _nativeCopies = pendingCopies;
      _nativeWritePending = pendingBarrier;
      _nativeSourceIDs = pendingBarrier
          ? {..._nativeSourceIDs, ...nativeSourceIDs}
          : nativeSourceIDs;
      _copiedMediaIDs = pendingBarrier
          ? {..._copiedMediaIDs, ...copiedMediaIDs}
          : copiedMediaIDs;
      _copiedAuxiliaryIDs = pendingBarrier
          ? {..._copiedAuxiliaryIDs, ...copiedAuxiliaryIDs}
          : copiedAuxiliaryIDs;
      _copiedDeadlines = [for (final copy in _nativeCopies) ...copy.deadlines];
      _armExpiry();
      _previousVisible = plan.entities.map((e) => e.id).toSet();
      if (nativeChanged || overlayChanged) {
        _change(() {
          if (!current()) return;
          if (nativeChanged) {
            _markers = next;
            _markerKeys = keys;
          }
          _displayed = visibleEntities;
          _positions = nextPositions;
          _placements = plan.placements;
        });
      }
      // Rect-only and cached geometry updates need no native write barrier.
      // A retiring ACL/source scope must always keep the full barrier below.
      if (!pendingBarrier) {
        _reportAnchor(nextPositions[widget.selectedEntityId]);
        return;
      }
      // MarkerUpdates is the plugin's incremental add/change/remove path.
      // Let its widget update reach the platform channel before uncovering a
      // retired permission scope; the subsequent SDK projection is a barrier.
      await WidgetsBinding.instance.endOfFrame;
      if (!current()) return;
      await map
          .toScreenCoordinate(
            _camera?.target ??
                _displayCoordinate(
                  widget.city.map!.latitude,
                  widget.city.map!.longitude,
                ),
          )
          .timeout(const Duration(seconds: 3));
      if (!current()) return;
      if (_nativeCopies.any((copy) => copy.invalidAt(DateTime.now().toUtc()))) {
        _retiring = true;
        _change(() {});
        unawaited(_renderQueue.schedule());
        return;
      }
      _copiedMediaIDs = copiedMediaIDs;
      _copiedAuxiliaryIDs = copiedAuxiliaryIDs;
      _nativeCopies = nativeCopies;
      _copiedDeadlines = [for (final copy in _nativeCopies) ...copy.deadlines];
      _nativeSourceIDs = nativeSourceIDs;
      _nativeWritePending = false;
      _armExpiry();
      _change(() {
        if (current()) _retiring = false;
      });
      _reportAnchor(_positions[widget.selectedEntityId]);
    } catch (_) {
      if (current()) _fail('地图内容暂时无法显示，请重试。');
    }
  }

  Future<Uint8List> _nativePNG(Uint8List bytes, double rasterRatio) async {
    if (rasterRatio == _screenRatio) return bytes;
    final scale = _screenRatio / rasterRatio;
    final codec = await ui.instantiateImageCodec(bytes);
    ui.Image? image;
    try {
      image = (await codec.getNextFrame()).image;
      final width = math.max(1, (image.width * scale).round());
      final height = math.max(1, (image.height * scale).round());
      final recorder = ui.PictureRecorder();
      final canvas = Canvas(recorder);
      canvas.drawImageRect(
        image,
        Rect.fromLTWH(0, 0, image.width.toDouble(), image.height.toDouble()),
        Rect.fromLTWH(0, 0, width.toDouble(), height.toDouble()),
        Paint()..filterQuality = FilterQuality.medium,
      );
      final picture = recorder.endRecording();
      try {
        final native = await picture.toImage(width, height);
        try {
          return (await native.toByteData(
            format: ui.ImageByteFormat.png,
          ))!.buffer.asUint8List();
        } finally {
          native.dispose();
        }
      } finally {
        picture.dispose();
      }
    } finally {
      image?.dispose();
      codec.dispose();
    }
  }

  MapEntity? _currentEntity(MapEntity old) => _source()
      .where(
        (e) =>
            e.id == old.id &&
            e.kind == old.kind &&
            e.hasMapAnchor &&
            e.sourceVersion == old.sourceVersion &&
            e.latitude == old.latitude &&
            e.longitude == old.longitude,
      )
      .firstOrNull;

  Future<void> _tap(abase.LatLng point) async {
    final map = _map, source = _sourceEpoch, camera = _cameraEpoch;
    if (map == null || _moving || _retiring || !_foreground) return;
    try {
      final pixel = await map.toScreenCoordinate(point);
      if (!mounted ||
          map != _map ||
          source != _sourceEpoch ||
          camera != _cameraEpoch)
        return;
      final candidates = MapDisplayPolicy.hitTargetsAt(
        entities: _displayed,
        positions: _positions,
        touchPosition: Offset(
          pixel.x / _projectionScale,
          pixel.y / _projectionScale,
        ),
      );
      if (candidates.isNotEmpty) {
        await _select(candidates);
      } else {
        _selectCoordinate(point);
      }
    } catch (_) {
      // A stale or failed projection never guesses a target from coordinates.
    }
  }

  Future<void> _select(List<MapEntity> initial) async {
    if (!mounted || _choiceOpen || _moving || _retiring || !_foreground) return;
    final epoch = _sourceEpoch, scope = _scope();
    bool current() => mounted && epoch == _sourceEpoch && scope == _scope();
    _choiceOpen = true;
    try {
      final tapped = initial.first;
      if (initial.length == 1 &&
          MapDisplayPolicy.contentEntity(tapped) &&
          tapped.isCluster) {
        final fresh = _currentEntity(tapped);
        if (fresh != null) widget.onEntitySelected?.call(fresh);
        return;
      }
      final candidates =
          initial.length == 1 && MapDisplayPolicy.displayCluster(tapped)
          ? MapDisplayPolicy.currentDisplayClusterMembers(tapped, _rows.values)
          : initial;
      List<MapEntity> fresh() => current()
          ? [
              for (final old in candidates)
                if (_currentEntity(old) case final e?) e,
            ]
          : const [];
      if (fresh().isEmpty) return;
      if (candidates.length > 1 &&
          candidates.every(MapDisplayPolicy.contentEntity) &&
          widget.onContentOverlap != null) {
        widget.onContentOverlap!(fresh());
        return;
      }
      final chosen = candidates.length == 1
          ? fresh().single
          : await showCurrentMapPointChoices(
              context: context,
              changes: _changes,
              currentCandidates: fresh,
              currentScope: () => current() ? (scope, epoch) : null,
              onVisibilityChanged: _readerVisibility,
            );
      if (!current() || chosen == null) return;
      final entity = _currentEntity(chosen);
      if (entity == null) return;
      final place = widget.places
          .where(
            (p) =>
                entity.id == 'place:${p.id}' &&
                p.location.hasPublicPoint &&
                p.location.latitude == entity.latitude &&
                p.location.longitude == entity.longitude,
          )
          .firstOrNull;
      if (place != null)
        widget.onPlaceSelected(place);
      else
        widget.onEntitySelected?.call(entity);
    } finally {
      _choiceOpen = false;
    }
  }

  void _selectCoordinate(abase.LatLng point) {
    if (!mounted ||
        !_foreground ||
        _retiring ||
        _moving ||
        widget.onCoordinateSelected == null ||
        widget.coordinateSourceCurrent?.call() != true)
      return;
    final canonical = MapCoordinateBoundary.fromAmap(
      point.latitude,
      point.longitude,
    );
    widget.onCoordinateSelected!(canonical.latitude, canonical.longitude);
  }

  void _readerVisibility(({Object token, bool visible}) value) {
    if (value.visible)
      _readerTokens.add(value.token);
    else
      _readerTokens.remove(value.token);
    widget.onMapReaderVisibilityChanged?.call(value);
    unawaited(_setRenderingState());
  }

  Future<void> _setRenderingState() async {
    await _renderControl?.update(
      _renderLifecycle,
      covered: _readerTokens.isNotEmpty,
    );
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _diagnostics.lifecycle(state.index);
    _renderLifecycle = state;
    _foreground = state == AppLifecycleState.resumed;
    ++_cameraEpoch;
    ++_focusSerial;
    _settle?.cancel();
    _syncSource();
    unawaited(_setRenderingState());
    if (_foreground) {
      _moving = false;
      _scheduleSettled();
    }
  }

  void _fail(String message) {
    _diagnostics.resourceError(
      resource: NativeMapResource.other,
      reason: NativeMapFailureReason.unavailable,
      stage: NativeMapDiagnosticFailureStage.initialization,
    );
    _change(() => _failure = message);
    widget.onMapUnavailable?.call(message);
  }

  void _retry() {
    if (_map != null) {
      _change(() => _failure = null);
      _syncSource();
      _scheduleSettled();
      return;
    }
    ++_sdkEpoch;
    _change(() => _failure = null);
    _armStartup();
  }

  Future<void> _north() async {
    final map = _map, camera = _camera;
    if (map == null || camera == null) return;
    try {
      await map.moveCamera(
        amap.CameraUpdate.newCameraPosition(
          amap.CameraPosition(
            target: camera.target,
            zoom: camera.zoom,
            tilt: 0,
            bearing: 0,
          ),
        ),
      );
    } catch (_) {
      if (mounted) _fail('地图方向暂时无法更新，请重试。');
    }
  }

  @override
  void dispose() {
    _cancelFitCamera();
    _diagnostics.dispose();
    ++_styleRequest;
    ++_configurationRequest;
    ++_sourceEpoch;
    ++_cameraEpoch;
    ++_sdkEpoch;
    ++_focusSerial;
    _renderQueue.close();
    _renderControl?.close();
    _settle?.cancel();
    _expiry?.cancel();
    _startup?.cancel();
    widget.readerChanges?.removeListener(_sourceChanged);
    AMapSessionConsent.controller.removeListener(_privacyChanged);
    WidgetsBinding.instance.removeObserver(this);
    _changes.dispose();
    // AMapWidget owns and disposes its platform controller exactly once.
    _map = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_supported || widget.city.map == null) {
      return const Center(
        child: Padding(
          padding: EdgeInsets.all(24),
          child: Text('地图暂不可用，请使用地点列表。'),
        ),
      );
    }
    final keyConfiguration = _keyConfiguration;
    if (keyConfiguration == null) {
      return const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(),
            SizedBox(height: 12),
            Text('正在加载地图…'),
          ],
        ),
      );
    }
    if (!keyConfiguration.configured) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text('地图暂不可用，请重试或使用地点列表。'),
              TextButton(
                onPressed: _retryKeyConfiguration,
                child: const Text('重试'),
              ),
            ],
          ),
        ),
      );
    }
    if (!_styleReady) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (!_styleFailed) ...[
                const CircularProgressIndicator(),
                const SizedBox(height: 12),
              ],
              Text(_styleFailed ? '地图样式暂时无法加载，请重试或使用地点列表。' : '正在准备地图样式…'),
              if (_styleFailed)
                TextButton(
                  onPressed: _retryOfflineStyle,
                  child: const Text('重试地图样式'),
                ),
            ],
          ),
        ),
      );
    }
    if (!_configured || !AMapSessionConsent.accepted) {
      final consent = AMapSessionConsent.controller;
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (!consent.loaded || consent.busy)
                const CircularProgressIndicator(),
              const SizedBox(height: 12),
              Text(
                consent.error ??
                    (consent.loaded
                        ? '地图服务隐私选择尚未同意，仍可查看可用内容。'
                        : '正在读取地图服务隐私选择…'),
              ),
              if (consent.loaded && !consent.busy)
                TextButton(
                  onPressed: () => unawaited(_preparePrivacy(review: true)),
                  child: const Text('地图服务隐私说明'),
                ),
            ],
          ),
        ),
      );
    }
    final sdk = _sdkEpoch;
    final initial =
        _camera ??
        amap.CameraPosition(
          target: _displayCoordinate(
            widget.city.map!.latitude,
            widget.city.map!.longitude,
          ),
          zoom: widget.city.map!.defaultZoom,
          bearing: widget.city.map!.bearing,
          tilt: widget.city.map!.pitch,
        );
    return LayoutBuilder(
      builder: (context, box) {
        final size = Size(box.maxWidth, box.maxHeight);
        if (size.isFinite && size != _size) {
          _size = size;
          ++_cameraEpoch;
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (mounted) _scheduleSettled();
          });
        }
        return Stack(
          fit: StackFit.expand,
          children: [
            Listener(
              onPointerDown: (_) {
                ++_pointerEpoch;
                _cancelFitCamera();
              },
              child: amap.AMapWidget(
                key: ValueKey('birdtie-native-amap:$sdk'),
                initialCameraPosition: initial,
                customStyleOptions: _offlineStyle,
                markers: _markers.values.toSet(),
                onMapCreated: (map) => _onCreated(map, sdk),
                onCameraMove: (camera) {
                  if (sdk == _sdkEpoch &&
                      _configured &&
                      AMapSessionConsent.accepted)
                    _onMove(camera);
                },
                onCameraMoveEnd: (camera) {
                  if (sdk == _sdkEpoch &&
                      _configured &&
                      AMapSessionConsent.accepted)
                    _onMoveEnd(camera);
                },
                onTap: (point) => unawaited(_tap(point)),
                onLongPress: _selectCoordinate,
                touchPoiEnabled: false,
                labelsEnabled: true,
                compassEnabled: false,
                buildingsEnabled: false,
                tiltGesturesEnabled: false,
                scaleEnabled: true,
                logoLeftMargin: 12,
                logoBottomMargin:
                    ((widget.contentRect == null
                                ? 12.0
                                : math.max(
                                    12.0,
                                    _size.height -
                                        widget.contentRect!.bottom +
                                        8,
                                  )) *
                            _projectionScale)
                        .round(),
              ),
            ),
            if (!_moving && !_retiring && _foreground)
              for (final e in _displayed)
                if (_positions[e.id] case final point?) ...[
                  Positioned(
                    left: point.dx - 24,
                    top: point.dy - 24,
                    width: 48,
                    height: 48,
                    child: Semantics(
                      button: true,
                      label: '${mapPointKindLabel(e.kind)} ${e.title}',
                      onTap: () => unawaited(
                        _select(
                          MapDisplayPolicy.overlappingHitTargets(
                            entities: _displayed,
                            positions: _positions,
                            tappedID: e.id,
                          ),
                        ),
                      ),
                      child: const SizedBox.expand(),
                    ),
                  ),
                ],
            if (_retiring || !_foreground)
              const ColoredBox(
                key: Key('amap-retiring-cover'),
                color: Color(0xFFF3F4EF),
              ),
            if (_map == null && _failure == null)
              Center(
                child: Semantics(
                  liveRegion: true,
                  child: const Text('正在加载地图…'),
                ),
              ),
            Positioned(
              top: widget.ornamentTop ?? MediaQuery.paddingOf(context).top + 8,
              right: 12,
              child: NowMapCompass(
                bearing: _camera?.bearing ?? 0,
                onNorth: () => unawaited(_north()),
              ),
            ),
            if (_failure != null)
              Center(
                child: Material(
                  color: Theme.of(context).colorScheme.surface,
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(_failure!),
                        TextButton(
                          onPressed: _retry,
                          child: const Text('重试地图'),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
          ],
        );
      },
    );
  }
}
