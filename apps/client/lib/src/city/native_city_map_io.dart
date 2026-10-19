import '../workspace/map_content_routes.dart';
import 'dart:async';
import 'map_cluster_members_sheet.dart';
import 'map_point_selection_sheet.dart';
import 'dart:convert';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart';

import 'mapbox_style.dart';
import 'map_camera_focus.dart';
import 'native_map_diagnostics.dart';
import 'native_map_failure.dart';
import 'now_map_compass.dart';
import 'public_city_controller.dart';
import 'map_navigation_target.dart';
import '../workspace/map_entities.dart';
import '../workspace/map_marker_bitmap.dart';
import '../workspace/map_marker_visual.dart';
import '../workspace/map_display_policy.dart';

const _northCameraEvidenceEnabled =
    kDebugMode &&
    bool.fromEnvironment('BIRDTIE_NOW_CAMERA_EVIDENCE', defaultValue: false);

/// Snapshot serialization only; values must come from getCameraState on device.
@visibleForTesting
Map<String, Object> nowNorthCameraEvidenceSnapshot(CameraState camera) => {
  'center': {
    'longitude': camera.center.coordinates.lng.toDouble(),
    'latitude': camera.center.coordinates.lat.toDouble(),
  },
  'zoom': camera.zoom,
  'pitch': camera.pitch,
  'bearing': camera.bearing,
};

class _NorthCameraEvidence {
  _NorthCameraEvidence({
    required this.id,
    required this.map,
    required this.focusSerial,
    required this.scopeGeneration,
    required this.before,
  });
  final int id;
  final MapboxMap map;
  final int focusSerial;
  final int scopeGeneration;
  final Map<String, Object> before;
  final Stopwatch elapsed = Stopwatch()..start();
}

/// Numeric-only profile/debug instrumentation; no entity, account, coordinate,
/// token, text or media data is retained. Release builds do not increment it.
class NativeContentMapMetrics {
  int mapCreations = 0, markerCreates = 0, markerUpdates = 0, markerDeletes = 0;
  int renderPasses = 0,
      renderedCount = 0,
      renderMicros = 0,
      lastRenderMicros = 0;
  int pendingPeak = 0, coalesced = 0;
  int styleReadyMicros = 0,
      firstContentMicros = 0,
      firstCoverMicros = 0,
      maxRenderMicros = 0;
}

class NativeCityMapView extends StatefulWidget {
  const NativeCityMapView({
    super.key,
    required this.city,
    required this.places,
    required this.accessToken,
    required this.onPlaceSelected,
    this.placeStateMessage,
    this.entities = const [],
    this.selectedEntityId,
    this.onEntitySelected,
    this.onViewportSettled,
    this.onViewportInitialized,
    this.onCameraSnapshot,
    this.onContentViewportSettled,
    this.onContentOverlap,
    this.onCameraMotion,
    this.onMapUnavailable,
    this.fullBleed = false,
    this.contextKey = '',
    this.preserveViewport = false,
    this.ornamentTop,
    this.contentRect,
    this.focusRevision = 0,
    this.onSelectedAnchor,
    this.readerChanges,
    this.currentReaderEntities,
    this.currentReaderScope,
    this.onMapReaderVisibilityChanged,
    this.onCoordinateSelected,
    this.coordinateSourceCurrent,
  });

  final PublicCity? city;
  final List<PublicPlace> places;
  final String accessToken;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final String? placeStateMessage;
  final List<MapEntity> entities;
  final String? selectedEntityId;
  final ValueChanged<MapEntity>? onEntitySelected;
  final ValueChanged<MapCameraSnapshot>? onCameraSnapshot;
  final ValueChanged<MapBounds>? onViewportSettled;
  final ValueChanged<MapBounds>? onViewportInitialized;
  final void Function(MapBounds bounds, double zoom)? onContentViewportSettled;
  final ValueChanged<List<MapEntity>>? onContentOverlap;
  final VoidCallback? onCameraMotion;
  final ValueChanged<String>? onMapUnavailable;
  final bool fullBleed;
  final String contextKey;
  final bool preserveViewport;
  final double? ornamentTop;
  final Rect? contentRect;
  final int focusRevision;
  final ValueChanged<({String? id, Offset? point})>? onSelectedAnchor;
  final Listenable? readerChanges;
  final List<MapEntity> Function()? currentReaderEntities;
  final Object? Function()? currentReaderScope;
  final ValueChanged<({Object token, bool visible})>?
  onMapReaderVisibilityChanged;
  final void Function(double latitude, double longitude)? onCoordinateSelected;
  final bool Function()? coordinateSourceCurrent;

  @override
  State<NativeCityMapView> createState() => _NativeCityMapViewState();
}

class _NativeCityMapViewState extends State<NativeCityMapView>
    with WidgetsBindingObserver {
  MapboxMap? _map;
  final _diagnostics = NativeMapDiagnostics(
    provider: NativeMapDiagnosticProvider.mapbox,
  );
  final _readerChanges = MapReaderChanges();
  final _pointSemantics =
      ValueNotifier<List<({MapEntity entity, Offset point})>>([]);
  int _readerEpoch = 0;
  PointAnnotationManager? _markers;
  PolylineAnnotationManager? _contentLines;
  final _lineAnnotations = <String, PolylineAnnotation>{};
  final _lineFingerprints = <String, String>{};
  final _directionAnnotations = <String, PointAnnotation>{};
  final _directionFingerprints = <String, String>{};
  final Map<String, PointAnnotation> _annotations = {};
  final Map<String, String> _annotationFingerprints = {};
  late final _markerQueue = LatestMapRenderQueue((serial) async {
    try {
      await _syncMarkers(serial);
    } catch (error) {
      if (kProfileMode)
        debugPrint(
          'BIRDTIE_MAP_SYNC ${jsonEncode({'event': 'error', 'serial': serial, 'type': error.runtimeType.toString(), 'code': error is PlatformException ? error.code : ''})}',
        );
      rethrow;
    }
  });
  final contentMapMetrics = NativeContentMapMetrics();
  final _mapLifetime = Stopwatch()..start();
  Timer? _cameraSettleTimer, _initialViewportTimer;
  Completer<bool>? _initialViewportWait;
  int _initialViewportSerial = 0;
  DateTime _lastCameraChangeAt = DateTime.fromMillisecondsSinceEpoch(0);
  DateTime _ignoreIdleUntil = DateTime.fromMillisecondsSinceEpoch(0);
  bool _styleReady = false;
  bool _coordinateMapLoaded = false;
  final _mapFailure = NativeMapFailureState();
  Timer? _mapRetryTimer;
  CameraOptions? _retryCamera;
  int? _retryScopeGeneration;
  String? _retryContextKey;
  String? _retrySelectionID;
  int _ornamentSerial = 0;
  final _bearing = ValueNotifier<double>(0);
  int _scopeGeneration = 0;
  final _styleOpacityOriginals = <String, Object?>{};
  String _lastLayoutDiagnostic = '';
  int _focusSerial = 0;
  int _northEvidenceSerial = 0;
  _NorthCameraEvidence? _northEvidence;
  Timer? _northEvidenceTimer;
  MapCameraFocusInput? _retryFocusInput;
  int? _retryMotionGeneration;
  late final CameraViewportState _initialViewport;

  @override
  void initState() {
    super.initState();
    _diagnostics.mounted();
    if (WidgetsBinding.instance.lifecycleState case final state?) {
      _diagnostics.lifecycle(state.index);
    }
    if (_diagnostics.enabled) WidgetsBinding.instance.addObserver(this);
    MapboxOptions.setAccessToken(widget.accessToken);
    final viewport = widget.city?.map;
    _initialViewport = CameraViewportState(
      center: Point(
        coordinates: Position(
          viewport?.longitude ?? -2.0943,
          viewport?.latitude ?? 57.1497,
        ),
      ),
      // The requested initial Aberdeen camera is only an exploration view.
      // No GPS fix, selected city, public location or entity is manufactured.
      zoom: viewport?.defaultZoom ?? 13,
      bearing: viewport?.bearing ?? 0,
      pitch: viewport?.pitch ?? 0,
    );
  }

  MapCameraFocusInput _focusInput(NativeCityMapView view) =>
      MapCameraFocusInput(
        city: view.city,
        contextKey: view.contextKey,
        places: view.places,
        entities: view.entities
            .where((e) => e.hasMapAnchor)
            .toList(growable: false),
        selectedEntityId: view.selectedEntityId,
        preserveViewport: view.preserveViewport,
      );

  double _bitmapPixelRatio = 1;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final next = MapMarkerBitmap.pixelRatio(
      MediaQuery.devicePixelRatioOf(context),
    );
    if (next == _bitmapPixelRatio) return;
    _bitmapPixelRatio = next;
    if (_styleReady) unawaited(_refreshMarkers());
  }

  @override
  void didUpdateWidget(covariant NativeCityMapView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.city?.id != widget.city?.id ||
        oldWidget.contextKey != widget.contextKey) {
      ++_readerEpoch;
    }
    if (_placeFingerprint(oldWidget.places) !=
            _placeFingerprint(widget.places) ||
        _entityFingerprint(oldWidget.entities) !=
            _entityFingerprint(widget.entities) ||
        oldWidget.selectedEntityId != widget.selectedEntityId ||
        oldWidget.contentRect != widget.contentRect) {
      _pointSemantics.value = const [];
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _readerChanges.changed();
    });
    final previous = _focusInput(oldWidget);
    final current = _focusInput(widget);
    final explicitFocus = oldWidget.focusRevision != widget.focusRevision;
    final viewInputsChanged = !current.hasSameViewInputs(previous);
    if (viewInputsChanged) {
      _scopeGeneration++;
      _focusSerial++;
    }
    if (explicitFocus) _focusSerial++;
    if (_styleReady && (current.shouldFocusAfter(previous) || explicitFocus)) {
      unawaited(_focusContext());
    }
    if (_styleReady && current.cityKey != previous.cityKey) {
      unawaited(_publishInitialViewport());
    }
    if (_styleReady && oldWidget.ornamentTop != widget.ornamentTop) {
      unawaited(_updateOrnaments());
    }
    if (oldWidget.accessToken != widget.accessToken) {
      MapboxOptions.setAccessToken(widget.accessToken);
    }
    // Every input that retires an in-flight pass also queues its replacement.
    // Reader restoration must not wait for a later source lease refresh.
    if (viewInputsChanged ||
        _placeFingerprint(oldWidget.places) !=
            _placeFingerprint(widget.places) ||
        _entityFingerprint(oldWidget.entities) !=
            _entityFingerprint(widget.entities) ||
        oldWidget.selectedEntityId != widget.selectedEntityId ||
        oldWidget.contentRect != widget.contentRect) {
      unawaited(_refreshMarkers());
    }
  }

  void _invalidatePendingFocus() {
    // Observe pointer input without competing with SDK gestures or clearing
    // the shared entity selection, task result or user camera.
    _focusSerial++;
    _cameraMotionGeneration++;
    // A real touch supersedes programmatic movement immediately, including
    // its ignored SDK idle window. Its own settled event owns the next read.
    _ignoreIdleUntil = DateTime.fromMillisecondsSinceEpoch(0);
    ++_initialViewportSerial;
    _initialViewportTimer?.cancel();
    final waiting = _initialViewportWait;
    _initialViewportWait = null;
    if (waiting != null && !waiting.isCompleted) waiting.complete(false);
    if (_northCameraEvidenceEnabled) {
      _cancelNorthCameraEvidence('pointer_or_north_action');
    }
  }

  Future<void> _focusContext() async {
    final map = _map;
    if (!mounted || !_styleReady || map == null) return;
    final input = _focusInput(widget);
    final center = input.center;
    if (center == null) return;
    final serial = ++_focusSerial;
    final scope = _scopeGeneration;
    final motion = _cameraMotionGeneration;
    bool current() =>
        mounted &&
        map == _map &&
        _styleReady &&
        serial == _focusSerial &&
        scope == _scopeGeneration &&
        motion == _cameraMotionGeneration &&
        input.hasSameViewInputs(_focusInput(widget));
    try {
      CameraOptions camera;
      if (input.fitsBounds) {
        final latitudes = input.points.map((point) => point.latitude).toList();
        final longitudes = input.points
            .map((point) => point.longitude)
            .toList();
        camera = await map.cameraForCoordinateBounds(
          CoordinateBounds(
            southwest: Point(
              coordinates: Position(
                input.cityBounds?.west ??
                    longitudes.reduce((a, b) => a < b ? a : b),
                latitudes.reduce((a, b) => a < b ? a : b),
              ),
            ),
            northeast: Point(
              coordinates: Position(
                input.cityBounds?.east ??
                    longitudes.reduce((a, b) => a > b ? a : b),
                latitudes.reduce((a, b) => a > b ? a : b),
              ),
            ),
            infiniteBounds: false,
          ),
          _focusPadding(),
          input.bearing,
          input.pitch,
          14,
          null,
        );
      } else {
        camera = CameraOptions(
          center: Point(
            coordinates: Position(center.longitude, center.latitude),
          ),
          zoom: input.zoom,
          bearing: input.bearing,
          pitch: input.pitch,
          padding: widget.contentRect == null ? null : _focusPadding(),
        );
      }
      if (!current()) return;
      await _animateCamera(map, camera);
    } catch (_) {
      // Keep the existing map/error/retry state on a transient camera API error.
      debugPrint('Birdtie map camera focus unavailable.');
    }
  }

  MbxEdgeInsets _focusPadding() {
    final box = context.findRenderObject();
    final mapSize = box is RenderBox && box.hasSize
        ? box.size
        : MediaQuery.sizeOf(context);
    final padding = MapDisplayPolicy.focusPadding(mapSize, widget.contentRect);
    return MbxEdgeInsets(
      top: padding.top,
      left: padding.left,
      bottom: padding.bottom,
      right: padding.right,
    );
  }

  Future<void> _animateCamera(MapboxMap map, CameraOptions camera) async {
    if (_northCameraEvidenceEnabled) {
      _cancelNorthCameraEvidence('programmatic_focus');
    }
    _cameraChangedSinceIdle = false;
    _cameraMotionGeneration++;
    _ignoreIdleUntil = DateTime.now().add(const Duration(milliseconds: 750));
    final motion = _cameraMotionGeneration, scope = _scopeGeneration;
    await map.flyTo(camera, MapAnimationOptions(duration: 450));
    // SDK completion only acknowledges the command. Publish after its existing
    // animation exclusion window, fenced against a new city or user gesture.
    if (mounted &&
        map == _map &&
        motion == _cameraMotionGeneration &&
        scope == _scopeGeneration) {
      unawaited(_publishInitialViewport());
    }
  }

  Future<void> _returnNorth() async {
    final map = _map;
    if (map == null || !mounted) return;
    _invalidatePendingFocus();
    final focusSerial = _focusSerial;
    final scopeGeneration = _scopeGeneration;
    _NorthCameraEvidence? evidence;
    if (_northCameraEvidenceEnabled) {
      final id = ++_northEvidenceSerial;
      try {
        final before = await map.getCameraState();
        // The extra debug snapshot await must never deliver a north reset after
        // a new user gesture, scope change or map replacement.
        if (!mounted ||
            map != _map ||
            focusSerial != _focusSerial ||
            scopeGeneration != _scopeGeneration ||
            id != _northEvidenceSerial) {
          return;
        }
        evidence = _NorthCameraEvidence(
          id: id,
          map: map,
          focusSerial: focusSerial,
          scopeGeneration: scopeGeneration,
          before: nowNorthCameraEvidenceSnapshot(before),
        );
        _northEvidence = evidence;
        _logNorthCameraEvidence({
          'operationId': id,
          'phase': 'before',
          'camera': evidence.before,
        });
      } catch (_) {
        _logNorthCameraEvidence({
          'operationId': id,
          'phase': 'before_unavailable',
        });
      }
      if (!mounted ||
          map != _map ||
          focusSerial != _focusSerial ||
          scopeGeneration != _scopeGeneration ||
          id != _northEvidenceSerial) {
        return;
      }
    }
    try {
      await map.easeTo(
        CameraOptions(bearing: 0),
        MapAnimationOptions(duration: 300),
      );
      if (_northCameraEvidenceEnabled &&
          evidence != null &&
          identical(_northEvidence, evidence) &&
          _isCurrentNorthCameraEvidence(evidence)) {
        // Android's easeTo Future only acknowledges animation start. Prefer a
        // real SDK idle callback; fallback is explicitly not an idle claim.
        _northEvidenceTimer?.cancel();
        _northEvidenceTimer = Timer(const Duration(milliseconds: 600), () {
          unawaited(_completeNorthCameraEvidence('sample_after_timeout'));
        });
      }
    } catch (_) {
      if (_northCameraEvidenceEnabled && identical(_northEvidence, evidence)) {
        _cancelNorthCameraEvidence('ease_to_failed');
      }
      if (mounted) debugPrint('Birdtie map north reset unavailable.');
    }
  }

  bool _isCurrentNorthCameraEvidence(_NorthCameraEvidence evidence) =>
      mounted &&
      evidence.map == _map &&
      evidence.id == _northEvidenceSerial &&
      evidence.focusSerial == _focusSerial &&
      evidence.scopeGeneration == _scopeGeneration;

  void _logNorthCameraEvidence(Map<String, Object?> payload) {
    if (!_northCameraEvidenceEnabled) return;
    debugPrint(
      'BirdtieNorthCameraEvidence ${jsonEncode(payload)}',
      wrapWidth: 4096,
    );
  }

  void _cancelNorthCameraEvidence(String reason) {
    final evidence = _northEvidence;
    _northEvidence = null;
    _northEvidenceTimer?.cancel();
    _northEvidenceTimer = null;
    if (evidence == null) return;
    _logNorthCameraEvidence({
      'operationId': evidence.id,
      'phase': 'cancelled',
      'reason': reason,
      'elapsedMs': evidence.elapsed.elapsedMilliseconds,
    });
  }

  Future<void> _completeNorthCameraEvidence(String sampleSource) async {
    final evidence = _northEvidence;
    if (evidence == null) return;
    // A previous gesture can deliver an already queued idle event while the
    // north animation starts. Do not label its pre-duration state as "after".
    if (sampleSource == 'sdk_idle' &&
        evidence.elapsed.elapsedMilliseconds < 300) {
      return;
    }
    // Claim once before awaiting native state so idle and timeout cannot emit
    // two after snapshots for the same operation.
    _northEvidence = null;
    _northEvidenceTimer?.cancel();
    _northEvidenceTimer = null;
    if (!_isCurrentNorthCameraEvidence(evidence)) {
      _logNorthCameraEvidence({
        'operationId': evidence.id,
        'phase': 'superseded',
      });
      return;
    }
    try {
      final after = await evidence.map.getCameraState();
      if (!_isCurrentNorthCameraEvidence(evidence)) {
        _logNorthCameraEvidence({
          'operationId': evidence.id,
          'phase': 'superseded',
        });
        return;
      }
      _logNorthCameraEvidence({
        'operationId': evidence.id,
        'phase': 'after',
        'sampleSource': sampleSource,
        'elapsedMs': evidence.elapsed.elapsedMilliseconds,
        'before': evidence.before,
        'after': nowNorthCameraEvidenceSnapshot(after),
      });
    } catch (_) {
      _logNorthCameraEvidence({
        'operationId': evidence.id,
        'phase': 'after_unavailable',
      });
    }
  }

  bool _cameraChangedSinceIdle = false;
  int _cameraMotionGeneration = 0;

  Future<MapBounds?> _contentViewport(MapboxMap map, CameraState camera) async {
    if (widget.onContentViewportSettled == null) return null;
    final box = context.findRenderObject();
    if (box is! RenderBox || !box.hasSize) return null;
    final full = Offset.zero & box.size;
    final visible = (widget.contentRect ?? full).intersect(full);
    if (visible.isEmpty) return null;
    try {
      final points = await map.coordinatesForPixels([
        for (final point in [
          visible.topLeft,
          visible.topRight,
          visible.bottomRight,
          visible.bottomLeft,
          visible.center,
        ])
          ScreenCoordinate(x: point.dx, y: point.dy),
      ]);
      if (points.length != 5 || points.any((p) => p == null)) return null;
      final coordinates = [
        for (final point in points)
          Offset(
            point!.coordinates.lng.toDouble(),
            point.coordinates.lat.toDouble(),
          ),
      ];
      return MapDisplayPolicy.projectedViewport(
        corners: coordinates.take(4).toList(),
        center: coordinates.last,
      );
    } catch (_) {
      // Failed screen projection never invents a world/city fallback query.
      return null;
    }
  }

  Future<void> _tuneProviderLabels(MapboxMap map) async {
    var candidates = 0, applied = 0, skipped = 0;
    try {
      final layers = await map.style.getStyleLayers();
      for (final layer in layers) {
        if (!mounted || map != _map) return;
        if (layer == null || layer.type != 'symbol') continue;
        var sourceLayer = '';
        var poi = mapboxProviderPoiLayer(layer.id, layer.type);
        if (!poi) {
          try {
            final source = await map.style.getStyleLayerProperty(
              layer.id,
              'source-layer',
            );
            sourceLayer = source.value is String ? source.value as String : '';
            poi = mapboxProviderPoiLayer(
              layer.id,
              layer.type,
              sourceLayer: source.value is String ? source.value as String : '',
            );
          } catch (_) {
            /* Unknown source metadata cannot establish a match. */
          }
        }
        if (!mounted || map != _map) return;
        // Ordinary provider POIs remain part of the basemap. Birdtie content
        // uses its own square identities rather than replacing the directory.
        if (poi) continue;
        if (mapboxSecondaryLabelFactor(
              layer.id,
              layer.type,
              sourceLayer: sourceLayer,
            ) ==
            null)
          continue;
        candidates++;
        try {
          final icon = await map.style.getStyleLayerProperty(
            layer.id,
            'icon-image',
          );
          final factor = mapboxSecondaryLabelFactor(
            layer.id,
            layer.type,
            sourceLayer: sourceLayer,
            hasIconImage: icon.value != null && icon.value != '',
          );
          if (factor == null) {
            skipped++;
            continue;
          }
          if (!_styleOpacityOriginals.containsKey(layer.id)) {
            final original = await map.style.getStyleLayerProperty(
              layer.id,
              'text-opacity',
            );
            _styleOpacityOriginals[layer.id] = original.value;
          }
          final weighted = mapboxWeightedTextOpacity(
            _styleOpacityOriginals[layer.id],
            factor,
          );
          if (weighted == null || !mounted || map != _map) {
            skipped++;
            continue;
          }
          await map.style.setStyleLayerProperty(
            layer.id,
            'text-opacity',
            weighted,
          );
          applied++;
        } catch (_) {
          skipped++;
        }
      }
    } catch (_) {
      // Style availability cannot retire authenticated Birdtie content.
    }
    if (!kReleaseMode)
      debugPrint(
        'BIRDTIE_MAP_LABEL_WEIGHT ${jsonEncode({'candidates': candidates, 'applied': applied, 'skipped': skipped})}',
      );
  }

  Future<bool> _waitForInitialViewport(Duration duration) {
    _initialViewportTimer?.cancel();
    final old = _initialViewportWait;
    if (old != null && !old.isCompleted) old.complete(false);
    final next = _initialViewportWait = Completer<bool>();
    _initialViewportTimer = Timer(duration, () {
      if (identical(_initialViewportWait, next)) {
        _initialViewportWait = null;
        _initialViewportTimer = null;
      }
      next.complete(true);
    });
    return next.future;
  }

  Future<void> _publishInitialViewport() async {
    final map = _map;
    if (map == null ||
        (widget.onViewportInitialized == null &&
            widget.onCameraSnapshot == null &&
            widget.onContentViewportSettled == null) ||
        _mapFailure.failure != null) {
      return;
    }
    final generation = _scopeGeneration,
        motion = _cameraMotionGeneration,
        serial = ++_initialViewportSerial;
    bool current() =>
        mounted &&
        map == _map &&
        generation == _scopeGeneration &&
        motion == _cameraMotionGeneration &&
        serial == _initialViewportSerial &&
        _mapFailure.failure == null;
    for (var attempt = 0; attempt < 5 && current(); attempt++) {
      final animationRemaining = _ignoreIdleUntil.difference(DateTime.now());
      final delay =
          attempt == 0 && animationRemaining > const Duration(milliseconds: 250)
          ? animationRemaining + const Duration(milliseconds: 16)
          : const Duration(milliseconds: 250);
      if (!await _waitForInitialViewport(delay) || !current()) return;
      try {
        final camera = await map.getCameraState();
        final contentBounds = await _contentViewport(map, camera);
        final bounds = await map.coordinateBoundsForCamera(
          CameraOptions(
            center: camera.center,
            padding: camera.padding,
            zoom: camera.zoom,
            bearing: camera.bearing,
            pitch: camera.pitch,
          ),
        );
        final southwest = bounds.southwest.coordinates;
        final northeast = bounds.northeast.coordinates;
        final visible = MapBounds(
          west: southwest.lng.toDouble(),
          south: southwest.lat.toDouble(),
          east: northeast.lng.toDouble(),
          north: northeast.lat.toDouble(),
        );
        if (!current()) {
          return;
        }
        if (visible.isValid || contentBounds != null) {
          if (visible.isValid) widget.onViewportInitialized?.call(visible);
          if (contentBounds != null) {
            widget.onContentViewportSettled?.call(contentBounds, camera.zoom);
          }
          _publishCameraSnapshot(camera);
          return;
        }
      } catch (_) {
        // Mapbox can reject a snapshot while the first style is still loading.
      }
    }
  }

  void _publishCameraSnapshot(CameraState camera) {
    final snapshot = MapCameraSnapshot(
      latitude: camera.center.coordinates.lat.toDouble(),
      longitude: camera.center.coordinates.lng.toDouble(),
      zoom: camera.zoom,
      bearing: camera.bearing,
      pitch: camera.pitch,
    );
    if (snapshot.isValid) widget.onCameraSnapshot?.call(snapshot);
  }

  void _onCameraChanged(CameraChangedEventData event) {
    if (mounted) {
      _diagnostics.cameraMoving();
      _bearing.value = event.cameraState.bearing;
      if (_pointSemantics.value.isNotEmpty) _pointSemantics.value = const [];
    }
    if (!_styleReady) return;
    final now = DateTime.now();
    if (now.isAfter(_ignoreIdleUntil)) {
      if (!_cameraChangedSinceIdle) widget.onCameraMotion?.call();
      _cameraChangedSinceIdle = true;
      _cameraMotionGeneration++;
      _lastCameraChangeAt = now;
      _cameraSettleTimer ??= Timer(
        const Duration(milliseconds: 400),
        _settleAfterQuiet,
      );
    }
  }

  void _settleAfterQuiet() {
    _cameraSettleTimer = null;
    if (!_cameraChangedSinceIdle || !mounted) return;
    const quietPeriod = Duration(milliseconds: 400);
    final elapsed = DateTime.now().difference(_lastCameraChangeAt);
    if (elapsed < quietPeriod) {
      _cameraSettleTimer = Timer(quietPeriod - elapsed, _settleAfterQuiet);
      return;
    }
    _diagnostics.cameraSettled();
    unawaited(_publishViewport());
  }

  Future<void> _onMapIdle(MapIdleEventData event) async {
    if (mounted) _diagnostics.cameraSettled();
    if (_northCameraEvidenceEnabled) {
      unawaited(_completeNorthCameraEvidence('sdk_idle'));
    }
    if (!_styleReady) return;
    if (DateTime.now().isBefore(_ignoreIdleUntil) || !_cameraChangedSinceIdle) {
      unawaited(_refreshMarkers());
      return;
    }
    await _publishViewport();
  }

  Future<void> _publishViewport() async {
    if (DateTime.now().isBefore(_ignoreIdleUntil) || !_cameraChangedSinceIdle) {
      return;
    }
    _cameraChangedSinceIdle = false;
    final generation = _cameraMotionGeneration;
    final scopeGeneration = _scopeGeneration;
    _cameraSettleTimer?.cancel();
    _cameraSettleTimer = null;
    final map = _map;
    if (map == null ||
        (widget.onViewportSettled == null &&
            widget.onCameraSnapshot == null &&
            widget.onContentViewportSettled == null))
      return;
    try {
      final camera = await map.getCameraState();
      final contentBounds = await _contentViewport(map, camera);
      final bounds = await map.coordinateBoundsForCamera(
        CameraOptions(
          center: camera.center,
          padding: camera.padding,
          zoom: camera.zoom,
          bearing: camera.bearing,
          pitch: camera.pitch,
        ),
      );
      final southwest = bounds.southwest.coordinates;
      final northeast = bounds.northeast.coordinates;
      final visible = MapBounds(
        west: southwest.lng.toDouble(),
        south: southwest.lat.toDouble(),
        east: northeast.lng.toDouble(),
        north: northeast.lat.toDouble(),
      );
      if (mounted &&
          generation == _cameraMotionGeneration &&
          scopeGeneration == _scopeGeneration &&
          (visible.isValid || contentBounds != null)) {
        if (visible.isValid) widget.onViewportSettled?.call(visible);
        if (contentBounds != null) {
          widget.onContentViewportSettled?.call(contentBounds, camera.zoom);
        }
        _publishCameraSnapshot(camera);
        unawaited(_refreshMarkers());
      }
    } catch (_) {
      // A camera snapshot can be unavailable while the native map is starting.
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _diagnostics.lifecycle(state.index);
  }

  @override
  void dispose() {
    if (_diagnostics.enabled) WidgetsBinding.instance.removeObserver(this);
    _diagnostics.dispose();
    _markerQueue.close();
    ++_readerEpoch;
    _readerChanges.changed();
    _readerChanges.dispose();
    _pointSemantics.dispose();
    _northEvidenceTimer?.cancel();
    _northEvidence = null;
    _bearing.dispose();
    _cameraSettleTimer?.cancel();
    ++_initialViewportSerial;
    _initialViewportTimer?.cancel();
    final waiting = _initialViewportWait;
    _initialViewportWait = null;
    if (waiting != null && !waiting.isCompleted) waiting.complete(false);
    _mapRetryTimer?.cancel();
    super.dispose();
  }

  String _placeFingerprint(List<PublicPlace> places) => places
      .map(
        (place) =>
            '${place.id}:${place.location.latitude}:${place.location.longitude}',
      )
      .join('|');

  String _entityFingerprint(List<MapEntity> entities) => entities
      .map(
        (entity) =>
            '${entity.id}:${entity.latitude}:${entity.longitude}:${entity.kind}:${entity.title}:${entity.subtitle}:${entity.placeID}:${entity.precision}:${entity.publicLocation}:${entity.isTaskResult}:${entity.decorationKey}:${entity.sourceVersion}:${entity.threadID}:${entity.threadPosition}:${entity.nextContentIDs.join(",")}',
      )
      .join('|');

  Future<void> _updateOrnaments() async {
    final map = _map;
    if (map == null || !mounted || !widget.fullBleed) return;
    final serial = ++_ornamentSerial;
    final top = widget.ornamentTop ?? MediaQuery.paddingOf(context).top + 8;
    try {
      await map.compass.updateSettings(CompassSettings(enabled: false));
      if (!mounted || map != _map || serial != _ornamentSerial) return;
      await map.logo.updateSettings(
        LogoSettings(
          enabled: true,
          position: OrnamentPosition.TOP_LEFT,
          marginTop: top,
          marginLeft: 8,
        ),
      );
      if (!mounted || map != _map || serial != _ornamentSerial) return;
      if (top !=
          (widget.ornamentTop ?? MediaQuery.paddingOf(context).top + 8)) {
        await _updateOrnaments();
        return;
      }
      await map.attribution.updateSettings(
        AttributionSettings(
          enabled: true,
          position: OrnamentPosition.TOP_LEFT,
          marginTop: top + 5,
          marginLeft: 105,
        ),
      );
    } catch (_) {
      if (mounted && map == _map && serial == _ornamentSerial) {
        // Keep the provider's enabled default ornaments on transient SDK errors.
        debugPrint('Birdtie map ornament layout update unavailable.');
      }
    }
  }

  Future<void> _onStyleLoaded() async {
    final map = _map;
    if (map == null || !mounted) return;
    _diagnostics.styleLoaded();
    final firstStyle = !_styleReady;
    debugPrint('Birdtie native map style loaded.');
    if (widget.fullBleed) {
      await map.scaleBar.updateSettings(ScaleBarSettings(enabled: false));
      await _updateOrnaments();
    }
    if (!mounted || map != _map) return;
    await _tuneProviderLabels(map);
    if (!mounted || map != _map) return;
    _styleReady = true;
    if (!kReleaseMode && contentMapMetrics.styleReadyMicros == 0)
      contentMapMetrics.styleReadyMicros = _mapLifetime.elapsedMicroseconds;
    _markers ??= await map.annotations.createPointAnnotationManager();
    await _markers!.setIconAllowOverlap(true);
    // The shared plan measures visible glyphs and actual labels. Transparent
    // bitmap padding must not introduce a second native collision layout.
    await _markers!.setIconIgnorePlacement(true);
    await _markers!.setTextAllowOverlap(true);
    await _markers!.setTextIgnorePlacement(true);
    await _markers!.setTextOptional(true);
    // With no annotation tap sink, the native SDK forwards the recognized tap
    // to MapWidget.onTapListener. Its real touch position avoids transparent
    // bitmap bounds choosing an unrelated annotation before our hit test.
    await _refreshMarkers();
    final retryCamera = _retryCamera;
    _retryCamera = null;
    final retryInput = _retryFocusInput;
    final sameRetryInput =
        retryInput != null && retryInput.hasSameViewInputs(_focusInput(widget));
    if (!mounted || map != _map) return;
    if (retryCamera != null &&
        sameRetryInput &&
        _retryScopeGeneration == _scopeGeneration &&
        _retryContextKey == widget.contextKey &&
        _retrySelectionID == widget.selectedEntityId &&
        _retryMotionGeneration == _cameraMotionGeneration) {
      await map.setCamera(retryCamera);
    } else if (firstStyle ||
        (retryCamera != null &&
            retryInput != null &&
            _focusInput(widget).shouldFocusAfter(retryInput))) {
      await _focusContext();
    }
    unawaited(_publishInitialViewport());
  }

  void _onMapLoadError(MapLoadingErrorEventData event) {
    if (!mounted) return;
    _coordinateMapLoaded = false;
    final changed = _mapFailure.report(event.type.name, event.message);
    final failure = _mapFailure.failure!;
    _diagnostics.resourceError(
      resource: failure.resource,
      reason: failure.reason,
      httpStatus: failure.httpStatus,
    );
    _mapRetryTimer?.cancel();
    if (changed) {
      debugPrint(
        'Birdtie map load error ${event.type}: '
        '${_mapFailure.failure!.safeDiagnostic}',
      );
      setState(() {});
    }
    if (_mapFailure.takeUnavailableNotification()) {
      widget.onMapUnavailable?.call(_mapFailure.failure!.message);
    }
  }

  void _onMapLoaded() {
    if (!mounted) return;
    _diagnostics.mapLoaded();
    _coordinateMapLoaded = true;
    if (!_mapFailure.mapLoaded()) return;
    _mapRetryTimer?.cancel();
    setState(() {});
    // Publish recovered bounds without changing the user's camera or task.
    unawaited(_publishInitialViewport());
  }

  Future<void> _retryMap() async {
    final map = _map;
    if (map == null || !_mapFailure.beginRetry()) return;
    _diagnostics.styleRetry();
    setState(() {});
    _mapRetryTimer?.cancel();
    _mapRetryTimer = Timer(const Duration(seconds: 15), () {
      if (mounted && _mapFailure.retryFinishedWithoutRecovery()) {
        setState(() {});
      }
    });
    final retryInput = _focusInput(widget);
    final retryScope = _scopeGeneration;
    final retryMotion = _cameraMotionGeneration;
    try {
      final camera = await map.getCameraState();
      if (!mounted || map != _map) return;
      if (retryScope != _scopeGeneration ||
          retryMotion != _cameraMotionGeneration ||
          !retryInput.hasSameViewInputs(_focusInput(widget))) {
        _mapRetryTimer?.cancel();
        if (_mapFailure.retryFinishedWithoutRecovery()) setState(() {});
        return;
      }
      _retryCamera = CameraOptions(
        center: camera.center,
        padding: camera.padding,
        zoom: camera.zoom,
        bearing: camera.bearing,
        pitch: camera.pitch,
      );
      _retryScopeGeneration = retryScope;
      _retryFocusInput = retryInput;
      _retryMotionGeneration = retryMotion;
      _retryContextKey = widget.contextKey;
      _retrySelectionID = widget.selectedEntityId;
      // Reload only provider resources on the existing native map instance.
      _styleOpacityOriginals.clear();
      await map.loadStyleURI(mapboxStyleUri);
    } catch (_) {
      _mapRetryTimer?.cancel();
      _retryCamera = null;
      if (mounted && _mapFailure.retryFinishedWithoutRecovery()) {
        setState(() {});
      }
      debugPrint('Birdtie native map retry unavailable.');
    }
  }

  Object? _pointReaderScope() => mounted
      ? (_readerEpoch, widget.currentReaderScope?.call() ?? widget.contextKey)
      : null;

  List<MapEntity> _currentPointSource() {
    final source =
        widget.currentReaderEntities?.call() ??
        <MapEntity>[
          ...widget.entities,
          for (final place in widget.places)
            if (place.location.hasPublicPoint)
              MapEntity(
                id: 'place:${place.id}',
                kind: MapEntityKind.place,
                title: place.name,
                subtitle: place.summary,
                latitude: place.location.latitude!,
                longitude: place.location.longitude!,
                placeID: place.id,
                categoryCode: place.categoryCode,
                publicLocation: true,
              ),
        ];
    final byID = <String, MapEntity>{};
    for (final entity in source.where((e) => e.hasMapAnchor)) {
      byID.putIfAbsent(entity.id, () => entity);
    }
    return byID.values.toList();
  }

  List<MapEntity> _currentRenderedPointSource() {
    final source = _currentPointSource();
    return [
      ...source,
      for (final entity in _renderedEntities)
        if (MapDisplayPolicy.displayCluster(entity) &&
            MapDisplayPolicy.currentDisplayClusterMembers(
              entity,
              source,
            ).isNotEmpty)
          entity,
    ];
  }

  void _dispatchCurrentPoint(MapEntity chosen) {
    final current = _currentPointSource()
        .where((e) => MapDisplayPolicy.samePointSource(chosen, e))
        .firstOrNull;
    if (current == null) return;
    for (final place in widget.places) {
      if (current.kind == MapEntityKind.place &&
          place.location.hasPublicPoint &&
          'place:${place.id}' == current.id) {
        widget.onPlaceSelected(place);
        return;
      }
    }
    widget.onEntitySelected?.call(current);
  }

  VoidCallback _pointSemanticAction(MapEntity original) {
    final scope = _pointReaderScope();
    return () {
      if (!mounted || scope != _pointReaderScope()) return;
      final fresh = _renderedEntities
          .where((e) => MapDisplayPolicy.samePointSource(original, e))
          .firstOrNull;
      if (fresh != null) unawaited(_selectMapPoint(fresh.id));
    };
  }

  bool _pointChoiceOpen = false;
  Future<void> _selectMapPoint(
    String? tappedID, {
    Offset? touchPosition,
  }) async {
    final map = _map;
    if (!mounted || map == null || _pointChoiceOpen) return;
    final rendered = _renderedEntities
        .where((e) => e.id == tappedID)
        .firstOrNull;
    if (touchPosition == null && rendered == null) return;
    // Preserve genuinely source-provided privacy-group behavior. The display
    // policy never creates new cluster identities from PLACE/MOMENT objects.
    if (touchPosition == null && rendered!.isCluster) {
      if (MapDisplayPolicy.contentEntity(rendered)) {
        _dispatchCurrentPoint(rendered);
        return;
      }
      await _openClusterMembers(rendered);
      return;
    }
    if (touchPosition == null) {
      _dispatchCurrentPoint(rendered!);
      return;
    }
    _pointChoiceOpen = true;
    final scope = _pointReaderScope(), generation = _scopeGeneration;
    final motion = _cameraMotionGeneration;
    bool current() =>
        mounted &&
        map == _map &&
        scope == _pointReaderScope() &&
        generation == _scopeGeneration;
    try {
      final shown = _renderedEntities.map((e) => (e.id, e.kind)).toSet();
      final originals = _currentRenderedPointSource()
          .where((e) => shown.contains((e.id, e.kind)))
          .toList();
      final pixels = await map.pixelsForCoordinates([
        for (final entity in originals)
          Point(coordinates: Position(entity.longitude, entity.latitude)),
      ]);
      if (!current() || motion != _cameraMotionGeneration) return;
      final rawPositions = <String, Offset>{
        for (var i = 0; i < originals.length && i < pixels.length; i++)
          if (pixels[i] case final pixel?)
            originals[i].id: Offset(pixel.x, pixel.y),
      };
      final positions = rawPositions;
      final hits = MapDisplayPolicy.visibleHitTargetsAt(
        entities: originals,
        positions: positions,
        touchPosition: touchPosition,
        placements: _previousPlacements,
        glyphSizes: _renderedGlyphSizes,
        nonPointHitRects: _nonPointHitRects(originals, positions),
      );
      final identities = hits.map((entity) => (entity.id, entity.kind)).toSet();
      final rank = {for (var i = 0; i < hits.length; i++) hits[i].id: i};
      List<MapEntity> freshHits() {
        final fresh = _currentRenderedPointSource()
            .where((e) => identities.contains((e.id, e.kind)))
            .toList();
        fresh.sort((a, b) => rank[a.id]!.compareTo(rank[b.id]!));
        return fresh;
      }

      final fresh = freshHits();
      if (fresh.isEmpty || !current()) return;
      if (fresh.length > 1 &&
          widget.onContentOverlap != null &&
          fresh.every(MapDisplayPolicy.contentEntity)) {
        widget.onContentOverlap!(List.unmodifiable(fresh));
        return;
      }
      final selected = fresh.length == 1
          ? fresh.single
          : await showCurrentMapPointChoices(
              context: context,
              changes: Listenable.merge([_readerChanges, widget.readerChanges]),
              currentCandidates: freshHits,
              currentScope: _pointReaderScope,
              onVisibilityChanged: widget.onMapReaderVisibilityChanged,
            );
      if (selected != null && current()) {
        if (selected.isCluster && !MapDisplayPolicy.contentEntity(selected)) {
          await _openClusterMembers(selected);
        } else {
          _dispatchCurrentPoint(selected);
        }
      }
    } catch (_) {
      if (mounted)
        debugPrint('Birdtie current map-point selection unavailable.');
    } finally {
      _pointChoiceOpen = false;
    }
  }

  Map<String, Rect> _nonPointHitRects(
    List<MapEntity> entities,
    Map<String, Offset> positions,
  ) {
    final rects = <String, Rect>{};
    for (final entity in entities) {
      if (entity.kind == MapEntityKind.cluster) {
        final point = positions[entity.id];
        if (point != null)
          rects[entity.id] = Rect.fromCenter(
            center: point,
            width: 48,
            height: 48,
          );
        continue;
      }
      if (entity.precision == 'point' &&
          {MapEntityKind.place, MapEntityKind.moment}.contains(entity.kind))
        continue;
      final point = positions[entity.id], annotation = _annotations[entity.id];
      final bytes = annotation?.image;
      if (point == null ||
          bytes == null ||
          bytes.length < 24 ||
          bytes[0] != 137 ||
          bytes[1] != 80 ||
          bytes[2] != 78 ||
          bytes[3] != 71)
        continue;
      // These are our generated PNGs already sent to this native annotation.
      // Their BOTTOM anchor is a paint bound, never a new geographic coordinate.
      final data = bytes.buffer.asByteData(bytes.offsetInBytes, bytes.length);
      final width = data.getUint32(16), height = data.getUint32(20);
      if (width == 0 || height == 0 || width > 4096 || height > 4096) continue;
      final scale = (annotation!.iconSize ?? 1) / _bitmapPixelRatio;
      if (!scale.isFinite || scale <= 0) continue;
      rects[entity.id] = Rect.fromLTWH(
        point.dx - width * scale / 2,
        point.dy - height * scale,
        width * scale,
        height * scale,
      ).expandToInclude(Rect.fromCenter(center: point, width: 48, height: 48));
    }
    return rects;
  }

  List<MapEntity> _currentClusterMembers(MapEntity cluster) {
    if (MapDisplayPolicy.displayCluster(cluster)) {
      return MapDisplayPolicy.currentDisplayClusterMembers(
        cluster,
        _currentPointSource(),
      );
    }
    final originals =
        widget.currentReaderEntities?.call() ??
        <MapEntity>[
          ...widget.entities,
          for (final place in widget.places)
            if (place.location.hasPublicPoint)
              MapEntity(
                id: 'place:${place.id}',
                kind: MapEntityKind.place,
                title: place.name,
                subtitle: place.summary,
                latitude: place.location.latitude!,
                longitude: place.location.longitude!,
                placeID: place.id,
                categoryCode: place.categoryCode,
                publicLocation: true,
              ),
        ];
    final byID = {for (final entity in originals) entity.id: entity};
    return [
      for (final id in cluster.memberIDs)
        if (byID[id] case final entity?) entity,
    ];
  }

  Future<void> _openClusterMembers(MapEntity cluster) async {
    final selected = await showCurrentMapClusterMembers(
      context: context,
      cluster: cluster,
      changes: Listenable.merge([_readerChanges, widget.readerChanges]),
      currentMembers: () => _currentClusterMembers(cluster),
      currentScope: () => mounted
          ? (
              _readerEpoch,
              widget.currentReaderScope?.call() ?? widget.contextKey,
            )
          : null,
      onVisibilityChanged: widget.onMapReaderVisibilityChanged,
    );
    if (selected == null || !mounted) return;
    if (selected.id.startsWith('place:')) {
      for (final place in widget.places) {
        if ('place:${place.id}' == selected.id) {
          widget.onPlaceSelected(place);
          return;
        }
      }
    }
    widget.onEntitySelected?.call(selected);
  }

  Future<void> _refreshMarkers() => _markerQueue.schedule();

  int get _markerSerial => _markerQueue.serial;
  ({String source, String camera, double ratio, Rect? rect})?
  _lastDisplayInputs;
  Set<String> _previousVisibleIDs = const {};
  Map<String, MapLabelPlacement> _previousPlacements = const {};
  Map<String, double> _renderedGlyphSizes = const {};

  Future<void> _syncContentLines(
    List<MapEntity> source,
    bool Function() current,
  ) async {
    final edges = {
      for (final segment in currentMapContentSegments(source))
        segment.id: segment,
    };
    var lines = _contentLines;
    if (lines == null) {
      if (edges.isEmpty || _map == null) return;
      lines = await _map!.annotations.createPolylineAnnotationManager();
      await lines.setLineDasharray([2.25, 1.75]);
      _contentLines = lines;
    }
    bool authorized(MapContentRouteSegment segment) =>
        mounted &&
        currentMapContentSegments(widget.entities).any(
          (fresh) =>
              fresh.id == segment.id &&
              fresh.fingerprint == segment.fingerprint &&
              fresh.edge.from.sourceVersion ==
                  segment.edge.from.sourceVersion &&
              fresh.edge.to.sourceVersion == segment.edge.to.sourceVersion,
        );
    for (final id in _lineAnnotations.keys.toList()) {
      if (!edges.containsKey(id)) {
        final old = _lineAnnotations.remove(id)!;
        _lineFingerprints.remove(id);
        await lines.delete(old);
      }
    }
    final directions = edges.values.take(64).toList();
    final directionIDs = directions.map((e) => e.id).toSet();
    final points = _markers;
    if (points != null) {
      for (final id in _directionAnnotations.keys.toList()) {
        if (!directionIDs.contains(id)) {
          await points.delete(_directionAnnotations.remove(id)!);
          _directionFingerprints.remove(id);
        }
      }
      for (final segment in directions) {
        if (!current()) return;
        final arrow = segment.direction;
        if (arrow == null) continue;
        final key = '${segment.fingerprint}:${_bearing.value}';
        if (_directionFingerprints[segment.id] == key) continue;
        final option = PointAnnotationOptions(
          geometry: Point(
            coordinates: Position(arrow.longitude, arrow.latitude),
          ),
          textField: '➤',
          textSize: 14,
          textColor: const Color(0xff2c6b58).toARGB32(),
          textHaloColor: Colors.white.toARGB32(),
          textHaloWidth: 1,
          textRotate: arrow.bearing - 90 - _bearing.value,
          customData: {
            'routeDirection': true,
            'entityId': 'route-direction:${segment.id}',
          },
        );
        final old = _directionAnnotations[segment.id];
        if (old != null) {
          old.geometry = option.geometry;
          old.textRotate = option.textRotate;
          await points.update(old);
          if (!authorized(segment)) {
            await points.delete(old);
            _directionAnnotations.remove(segment.id);
            _directionFingerprints.remove(segment.id);
            return;
          }
        } else {
          final created = await points.create(option);
          if (!authorized(segment)) {
            await points.delete(created);
            return;
          }
          _directionAnnotations[segment.id] = created;
        }
        _directionFingerprints[segment.id] = key;
      }
    }
    for (final edge in edges.values) {
      if (!current()) return;
      final selected = edge.selected(widget.selectedEntityId);
      final fingerprint =
          '${edge.edge.from.sourceVersion}:${edge.edge.to.sourceVersion}:${edge.fingerprint}:$selected';
      if (_lineFingerprints[edge.id] == fingerprint) continue;
      final geometry = LineString(
        coordinates: [
          for (final point in edge.points)
            Position(point.longitude, point.latitude),
        ],
      );
      final color =
          (selected ? const Color(0xff2c6b58) : const Color(0xb32c6b58))
              .toARGB32();
      final old = _lineAnnotations[edge.id];
      if (old != null) {
        old.geometry = geometry;
        old.lineColor = color;
        old.lineWidth = 3;
        await lines.update(old);
        if (!authorized(edge)) {
          await lines.delete(old);
          _lineAnnotations.remove(edge.id);
          _lineFingerprints.remove(edge.id);
          return;
        }
      } else {
        final created = await lines.create(
          PolylineAnnotationOptions(
            geometry: geometry,
            lineColor: color,
            lineWidth: 3,
          ),
        );
        if (!authorized(edge)) {
          await lines.delete(created);
          return;
        }
        _lineAnnotations[edge.id] = created;
      }
      _lineFingerprints[edge.id] = fingerprint;
    }
  }

  Future<void> _syncMarkers(int serial) async {
    final elapsed = Stopwatch()..start();
    if (!kReleaseMode) contentMapMetrics.renderPasses++;
    final markers = _markers;
    final map = _map;
    if (!_styleReady ||
        markers == null ||
        map == null ||
        !mounted ||
        serial != _markerSerial) {
      return;
    }
    final scope = _scopeGeneration, motion = _cameraMotionGeneration;
    final ratio = _bitmapPixelRatio;
    final sourceKey =
        '${_entityFingerprint(widget.entities)}:${_placeFingerprint(widget.places)}:${widget.selectedEntityId}';
    bool current() =>
        mounted &&
        serial == _markerSerial &&
        map == _map &&
        scope == _scopeGeneration &&
        motion == _cameraMotionGeneration &&
        ratio == _bitmapPixelRatio &&
        sourceKey ==
            '${_entityFingerprint(widget.entities)}:${_placeFingerprint(widget.places)}:${widget.selectedEntityId}';

    final unique = <String, MapEntity>{};
    for (final entity in <MapEntity>[
      ...widget.entities,
      for (final place in widget.places)
        if (place.location.hasPublicPoint)
          MapEntity(
            id: 'place:${place.id}',
            kind: MapEntityKind.place,
            title: place.name,
            subtitle: place.summary,
            latitude: place.location.latitude!,
            longitude: place.location.longitude!,
            placeID: place.id,
            categoryCode: place.categoryCode,
            publicLocation: true,
          ),
    ]) {
      if (entity.hasMapAnchor) unique.putIfAbsent(entity.id, () => entity);
    }
    final source = unique.values.toList(growable: false);
    await _syncContentLines(source, current);
    if (!current()) return;
    // Remove retired source identities before awaiting a camera projection.
    // A stale cluster count must not keep a withdrawn/expired member visible.
    for (final previous in _renderedEntities) {
      // Retire cached private pixels before any camera/projection await. Keep
      // the authorized object's annotation at exactly the existing geometry.
      final existing = _annotations[previous.id];
      final fresh = unique[previous.id];
      if (fresh == null ||
          fresh.sourceVersion != previous.sourceVersion ||
          fresh.recentMoments?.currentAt(DateTime.now().toUtc()) != true) {
        final auxiliary = _annotations.remove('map-aux:${previous.id}');
        _annotationFingerprints.remove('map-aux:${previous.id}');
        if (auxiliary != null) {
          _lastDisplayInputs = null;
          await markers.delete(auxiliary);
          if (!current()) return;
        }
      }
      if (existing != null &&
          previous.recentMoments != null &&
          !previous.recentMoments!.currentAt(DateTime.now().toUtc()) &&
          unique[previous.id]?.recentMoments?.currentAt(
                DateTime.now().toUtc(),
              ) !=
              true) {
        _lastDisplayInputs = null;
        existing.textField = previous.kind == MapEntityKind.place
            ? previous.title
            : '';
        existing.iconImage = null;
        await markers.update(existing);
        if (!kReleaseMode) contentMapMetrics.markerUpdates++;
        _annotationFingerprints.remove(previous.id);
        if (!current()) return;
      }
      if (existing != null &&
          previous.mediaPixels != null &&
          (unique[previous.id]?.mediaPixels?.current() != true ||
              unique[previous.id]?.mediaPixels?.key !=
                  previous.mediaPixels!.key)) {
        final fallback = await MapMarkerBitmap.render(
          kind: previous.kind,
          selected: previous.id == widget.selectedEntityId,
          devicePixelRatio: ratio,
          categoryCode: previous.categoryCode,
        );
        if (!current()) return;
        _lastDisplayInputs = null;
        existing.image = fallback;
        existing.iconImage = null;
        existing.textField = previous.kind == MapEntityKind.place
            ? previous.title
            : '';
        await markers.update(existing);
        if (!kReleaseMode) contentMapMetrics.markerUpdates++;
        _annotationFingerprints.remove(previous.id);
        if (!current()) return;
      }
      final retired = MapDisplayPolicy.displayCluster(previous)
          ? MapDisplayPolicy.currentDisplayClusterMembers(
              previous,
              source,
            ).isEmpty
          : !unique.containsKey(previous.id);
      if (retired) {
        final annotation = _annotations.remove(previous.id);
        _annotationFingerprints.remove(previous.id);
        if (annotation != null) {
          _lastDisplayInputs = null;
          await markers.delete(annotation);
          if (!kReleaseMode) contentMapMetrics.markerDeletes++;
        }
        if (!mounted || serial != _markerSerial || map != _map) return;
      }
    }
    final camera = await map.getCameraState();
    if (!current()) return;
    final cameraKey =
        '${camera.center.coordinates.lng}:${camera.center.coordinates.lat}:${camera.zoom}:${camera.bearing}:${camera.pitch}:${camera.padding.left}:${camera.padding.top}:${camera.padding.right}:${camera.padding.bottom}';
    // dart:ui toString overrides are stripped in profile/release builds.
    // Compare Rect by value; its diagnostic string cannot key layout state.
    final displayInputs = (
      source: sourceKey,
      camera: cameraKey,
      ratio: ratio,
      rect: widget.contentRect,
    );
    // The workspace IME gate offers only settled contentRect changes. Clip
    // covered glyphs then, while retaining annotations that remain visible.
    if (_lastDisplayInputs == displayInputs) {
      _pointSemantics.value = [
        for (final entity in _renderedEntities)
          if (_lastPointPositions[entity.id] case final point?)
            (entity: entity, point: point),
      ];
      return;
    }
    // A different display pass can mutate native annotations before its final
    // ACK. An interrupted pass must never reuse the previous successful key
    // with an empty or partially changed native set when the reader closes.
    _lastDisplayInputs = null;
    final pixels = await map.pixelsForCoordinates([
      for (final entity in source)
        Point(coordinates: Position(entity.longitude, entity.latitude)),
    ]);
    if (!current()) return;
    final positions = <String, Offset>{
      for (var i = 0; i < source.length && i < pixels.length; i++)
        if (pixels[i] case final pixel?) source[i].id: Offset(pixel.x, pixel.y),
    };
    widget.onSelectedAnchor?.call((
      id: widget.selectedEntityId,
      point: positions[widget.selectedEntityId],
    ));
    // Project a fixed logical screen segment under this exact SDK camera.
    // Neither provider zoom numbers nor pixel density stand in for map scale.
    double? metersPerPixel;
    final box = context.findRenderObject();
    if (box is RenderBox && box.hasSize) {
      final center = box.size.center(Offset.zero);
      final segment = await map.coordinatesForPixels([
        ScreenCoordinate(x: center.dx - 24, y: center.dy),
        ScreenCoordinate(x: center.dx + 24, y: center.dy),
      ]);
      if (!current()) return;
      if (segment.length == 2 && segment[0] != null && segment[1] != null) {
        metersPerPixel = MapDisplayPolicy.scaleFromSegment(
          Offset(
            segment[0]!.coordinates.lng.toDouble(),
            segment[0]!.coordinates.lat.toDouble(),
          ),
          Offset(
            segment[1]!.coordinates.lng.toDouble(),
            segment[1]!.coordinates.lat.toDouble(),
          ),
          48,
        );
      }
    }
    final plan = MapDisplayPolicy.plan(
      entities: source,
      positions: positions,
      zoom: camera.zoom,
      selectedID: widget.selectedEntityId,
      contentRect: widget.contentRect,
      previouslyVisibleIDs: _previousVisibleIDs,
      previousPlacements: _previousPlacements,
      metersPerPixel: metersPerPixel,
      protectedIDs: {
        for (final entity in source)
          if (entity.isTaskResult) entity.id,
      },
    );
    final displayedPositions = {...positions, ...plan.clusterPositions};
    final specs = <String, PointAnnotationOptions>{};
    final imageKeys = <String, String>{};
    for (final entity in plan.entities) {
      if (plan.occludedGlyphIDs.contains(entity.id)) continue;
      final contentCluster = entity.kind == MapEntityKind.cluster;
      final compactPoint =
          contentCluster ||
          ({MapEntityKind.place, MapEntityKind.moment}.contains(entity.kind) &&
              entity.precision == 'point');
      final media = entity.kind == MapEntityKind.moment
          ? entity.mediaPixels
          : null;
      final photo = media?.png(
        selected: entity.id == widget.selectedEntityId,
        dense: plan.denseIDs.contains(entity.id),
        ratio: ratio,
      );
      imageKeys[entity.id] = photo != null
          ? 'media:${media!.key}:$ratio:${entity.id == widget.selectedEntityId}'
          : 'glyph:${entity.kind}:${entity.categoryCode}:${entity.precision}:${entity.contentCount}:${entity.anchorCount}:${entity.memberIDs.length}:$ratio:${entity.id == widget.selectedEntityId}:${plan.denseIDs.contains(entity.id)}';
      final bytes =
          photo ??
          await MapMarkerBitmap.render(
            kind: entity.kind,
            selected: entity.id == widget.selectedEntityId,
            count: entity.isCluster
                ? MapMarkerVisual.clusterCount(
                    categoryCode: entity.categoryCode,
                    contentCount: entity.contentCount,
                    anchorCount: entity.anchorCount,
                    fallback: entity.memberIDs.length,
                  )
                : 0,
            approximate: entity.precision == 'area',
            dense: plan.denseIDs.contains(entity.id),
            devicePixelRatio: ratio,
            categoryCode: entity.categoryCode,
          );
      if (!current()) return;
      specs[entity.id] = PointAnnotationOptions(
        geometry: Point(
          coordinates: Position(entity.longitude, entity.latitude),
        ),
        image: bytes,
        iconAnchor: compactPoint ? IconAnchor.CENTER : IconAnchor.BOTTOM,
        iconSize: compactPoint
            ? 1.0
            : entity.id == widget.selectedEntityId
            ? 1.08
            : entity.isCluster
            ? 1
            : 0.78,
        symbolSortKey: entity.id == widget.selectedEntityId ? 100 : 0,
        customData: {'entityId': entity.id, 'kind': entity.kind.name},
        textField: plan.placements[entity.id]?.name ?? '',
        textSize: plan.placements[entity.id]?.fontSize ?? 12,
        textMaxWidth:
            MapMarkerVisual.labelWidth /
            (plan.placements[entity.id]?.fontSize ?? 12),
        textOffset: [
          (plan.placements[entity.id]?.offset.dx ?? 0) /
              (plan.placements[entity.id]?.fontSize ?? 12),
          (plan.placements[entity.id]?.offset.dy ?? 0) /
              (plan.placements[entity.id]?.fontSize ?? 12),
        ],
        textAnchor: TextAnchor.TOP_LEFT,
        textColor:
            (plan.placements[entity.id]?.color ?? MapMarkerVisual.labelColor)
                .toARGB32(),
        textHaloColor: Colors.white.toARGB32(),
        textHaloWidth: 1.25,
      );
      final placement = plan.placements[entity.id];
      if (placement?.auxiliary != null) {
        final id = 'map-aux:${entity.id}';
        specs[id] = PointAnnotationOptions(
          geometry: Point(
            coordinates: Position(entity.longitude, entity.latitude),
          ),
          textField: placement!.auxiliary,
          textSize: 11,
          textMaxWidth: 112 / 11,
          textAnchor: TextAnchor.TOP_LEFT,
          textOffset: [
            placement.offset.dx / 11,
            (placement.offset.dy + placement.nameHeight + 1) / 11,
          ],
          textColor: MapMarkerVisual.countColor.toARGB32(),
          textHaloColor: Colors.white.toARGB32(),
          textHaloWidth: 1.25,
          symbolSortKey: entity.id == widget.selectedEntityId ? 100 : 0,
          customData: {'entityId': entity.id, 'kind': entity.kind.name},
        );
      }
    }
    if (!current()) return;
    final nextRenderedEntities = plan.entities
        .where((e) => !plan.occludedGlyphIDs.contains(e.id))
        .toList();
    final stale = _annotations.keys
        .where((id) => !specs.containsKey(id))
        .toList();
    for (final id in stale) {
      if (!current()) return;
      final annotation = _annotations.remove(id);
      _annotationFingerprints.remove(id);
      if (annotation != null) {
        await markers.delete(annotation);
        if (!kReleaseMode) contentMapMetrics.markerDeletes++;
      }
    }
    for (final entry in specs.entries) {
      if (!current()) return;
      final id = entry.key;
      final option = entry.value;
      final fingerprint =
          '${option.geometry.coordinates.lng}:${option.geometry.coordinates.lat}:${imageKeys[id]}:${option.symbolSortKey}:${option.iconSize}:${option.textField}:${option.textSize}:${option.textOffset}:${option.textColor}:${option.textAnchor}';
      final annotation = _annotations[id];
      if (annotation == null) {
        final created = await markers.create(option);
        if (!kReleaseMode) contentMapMetrics.markerCreates++;
        if (!current()) {
          await markers.delete(created);
          if (!kReleaseMode) contentMapMetrics.markerDeletes++;
          return;
        }
        // Android returns its generated sprite name. Sending that name back
        // after a replacement bitmap would overwrite the new image again.
        created.iconImage = null;
        _annotations[id] = created;
        _annotationFingerprints[id] = fingerprint;
      } else if (_annotationFingerprints[id] != fingerprint) {
        annotation.geometry = option.geometry;
        annotation.image = option.image;
        annotation.iconImage = null;
        annotation.iconAnchor = option.iconAnchor;
        annotation.iconSize = option.iconSize;
        annotation.symbolSortKey = option.symbolSortKey;
        annotation.customData = option.customData;
        annotation.textField = option.textField;
        annotation.textSize = option.textSize;
        annotation.textMaxWidth = option.textMaxWidth;
        annotation.textOffset = option.textOffset;
        annotation.textAnchor = option.textAnchor;
        annotation.textColor = option.textColor;
        annotation.textHaloColor = option.textHaloColor;
        annotation.textHaloWidth = option.textHaloWidth;
        await markers.update(annotation);
        if (!kReleaseMode) contentMapMetrics.markerUpdates++;
        _annotationFingerprints[id] = fingerprint;
      }
    }
    if (!current()) return;
    _renderedEntities = nextRenderedEntities;
    _lastPointPositions = displayedPositions;
    _pointSemantics.value = [
      for (final entity in _renderedEntities)
        if (displayedPositions[entity.id] case final point?)
          (entity: entity, point: point),
    ];
    _lastDisplayInputs = displayInputs;
    if (kProfileMode) {
      final metrics = jsonEncode({
        'provider': 'mapbox',
        'cameraEpoch': _cameraMotionGeneration,
        'applied': 1,
        'sourceCount': source.length,
        'nativeAnnotationCount': _annotations.length,
        'contentRect': widget.contentRect == null
            ? null
            : [
                widget.contentRect!.left,
                widget.contentRect!.top,
                widget.contentRect!.right,
                widget.contentRect!.bottom,
              ],
        'metersPerLogicalPixel': plan.metersPerPixel,
        'screenRatio': ratio,
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
    _previousVisibleIDs = plan.entities.map((e) => e.id).toSet();
    _previousPlacements = plan.placements;
    _renderedGlyphSizes = plan.glyphSizes;
    if (!kReleaseMode) {
      contentMapMetrics.lastRenderMicros = elapsed.elapsedMicroseconds;
      if (elapsed.elapsedMicroseconds > contentMapMetrics.maxRenderMicros)
        contentMapMetrics.maxRenderMicros = elapsed.elapsedMicroseconds;
      if (contentMapMetrics.firstContentMicros == 0 &&
          plan.entities.any((e) => e.id.startsWith('owned-map:')))
        contentMapMetrics.firstContentMicros = _mapLifetime.elapsedMicroseconds;
      if (contentMapMetrics.firstCoverMicros == 0 &&
          plan.entities.any(
            (e) =>
                e.id.startsWith('owned-map:') &&
                e.mediaPixels?.current() == true,
          ))
        contentMapMetrics.firstCoverMicros = _mapLifetime.elapsedMicroseconds;
      contentMapMetrics.renderMicros += elapsed.elapsedMicroseconds;
      contentMapMetrics.renderedCount = plan.entities.length;
      contentMapMetrics.pendingPeak = _markerQueue.peakPending;
      contentMapMetrics.coalesced = _markerQueue.coalesced;
    }
    if (_northCameraEvidenceEnabled && mounted) {
      final public = source.where((e) => e.publicLocation).toList();
      final sample =
          public.where((e) => e.id == widget.selectedEntityId).firstOrNull ??
          public.firstOrNull;
      final box = context.findRenderObject();
      debugPrint(
        'Birdtie map display evidence ${jsonEncode({
          'annotationManagerID': markers.id,
          'rendering': 'individual original objects -> point annotations; density only affects glyph/label',
          'camera': nowNorthCameraEvidenceSnapshot(camera),
          'logicalMapSize': box is RenderBox && box.hasSize ? [box.size.width, box.size.height] : null,
          'dpr': MediaQuery.devicePixelRatioOf(context),
          'contentRect': widget.contentRect == null ? null : [widget.contentRect!.left, widget.contentRect!.top, widget.contentRect!.right, widget.contentRect!.bottom],
          'publicSourceCount': public.length,
          'renderedCount': plan.entities.length,
          'publicSample': sample == null ? null : {
                  'entityID': sample.id,
                  'placeID': sample.placeID,
                  'kind': sample.kind.name,
                  'precision': sample.precision,
                  'latitude': sample.latitude,
                  'longitude': sample.longitude,
                  'logicalPixel': positions[sample.id] == null ? null : [positions[sample.id]!.dx, positions[sample.id]!.dy],
                },
          'groups': [
            for (final entity in plan.entities)
              if (entity.isCluster && entity.publicLocation) {'entityID': entity.id, 'memberIDs': entity.memberIDs, 'label': entity.groupSummary},
          ],
        })}',
      );
    }
  }

  List<MapEntity> _renderedEntities = const [];
  Map<String, Offset> _lastPointPositions = const {};

  @override
  Widget build(BuildContext context) {
    final coordinateScope = _pointReaderScope();
    final coordinateCallback = widget.onCoordinateSelected;
    final pointCount = widget.places
        .where((place) => place.location.hasPublicPoint)
        .length;
    return ClipRRect(
      borderRadius: BorderRadius.circular(widget.fullBleed ? 0 : 18),
      child: SizedBox(
        height: widget.fullBleed ? double.infinity : 430,
        child: Stack(
          children: [
            Listener(
              onPointerDown: (_) => _invalidatePendingFocus(),
              child: MapWidget(
                key: const ValueKey('now-mapbox-sdk'),
                styleUri: mapboxStyleUri,
                viewport: _initialViewport,
                // In locked SDK 2.26 viewport transitions start after the
                // platform view exists. Seed native creation as well, so a new
                // overseas map cannot first show the SDK's default globe.
                cameraOptions: CameraOptions(
                  center: _initialViewport.center,
                  zoom: _initialViewport.zoom,
                  bearing: _initialViewport.bearing,
                  pitch: _initialViewport.pitch,
                ),
                onMapCreated: (map) {
                  _map = map;
                  _diagnostics.created();
                  if (!kReleaseMode) contentMapMetrics.mapCreations++;
                  debugPrint('Birdtie native map created.');
                  unawaited(_updateOrnaments());
                },
                onStyleLoadedListener: (_) => unawaited(_onStyleLoaded()),
                onMapLoadErrorListener: _onMapLoadError,
                onMapLoadedListener: (_) => _onMapLoaded(),
                onRenderFrameFinishedListener: _diagnostics.enabled
                    ? (event) => _diagnostics.renderFrame(
                        fullyRendered: event.renderMode == RenderMode.FULL,
                        durationMicros: event.timeInterval.end
                            .difference(event.timeInterval.begin)
                            .inMicroseconds,
                        needsRepaint: event.needsRepaint,
                      )
                    : null,
                onCameraChangeListener: _onCameraChanged,
                onMapIdleListener: _onMapIdle,
                onTapListener: (gesture) {
                  if (coordinateCallback == null) {
                    if (_styleReady &&
                        _mapFailure.failure == null &&
                        gesture.gestureState == GestureState.ended) {
                      unawaited(
                        _selectMapPoint(
                          null,
                          touchPosition: Offset(
                            gesture.touchPosition.x,
                            gesture.touchPosition.y,
                          ),
                        ),
                      );
                    }
                    return;
                  }
                  final latitude = gesture.point.coordinates.lat.toDouble();
                  final longitude = gesture.point.coordinates.lng.toDouble();
                  if (mounted &&
                      _coordinateMapLoaded &&
                      _styleReady &&
                      _mapFailure.failure == null &&
                      coordinateScope == _pointReaderScope() &&
                      identical(
                        coordinateCallback,
                        widget.onCoordinateSelected,
                      ) &&
                      (widget.coordinateSourceCurrent?.call() ?? true) &&
                      latitude.isFinite &&
                      longitude.isFinite &&
                      latitude >= -90 &&
                      latitude <= 90 &&
                      longitude >= -180 &&
                      longitude <= 180) {
                    coordinateCallback(latitude, longitude);
                  }
                },
              ),
            ),
            ValueListenableBuilder<List<({MapEntity entity, Offset point})>>(
              valueListenable: _pointSemantics,
              builder: (context, points, _) => Stack(
                children: [
                  for (final point in points)
                    Positioned(
                      left: point.point.dx - 24,
                      top: point.point.dy - 24,
                      width: 48,
                      height: 48,
                      child: Semantics(
                        key: ValueKey(
                          'native-map-point-semantics:${point.entity.id}',
                        ),
                        container: true,
                        button: true,
                        selected: point.entity.id == widget.selectedEntityId,
                        label:
                            '${mapPointKindLabel(point.entity.kind)} · ${point.entity.title} · ${point.entity.subtitle}',
                        onTap: _pointSemanticAction(point.entity),
                        // Semantics owns the action; transparent children must not
                        // capture native drag/tap gestures or create a second map.
                        child: const IgnorePointer(child: SizedBox.expand()),
                      ),
                    ),
                ],
              ),
            ),
            if (widget.fullBleed && _mapFailure.failure == null)
              Positioned(
                top:
                    widget.ornamentTop ??
                    MediaQuery.viewPaddingOf(context).top + 64,
                left: 16,
                child: ValueListenableBuilder<double>(
                  valueListenable: _bearing,
                  builder: (context, bearing, _) => NowMapCompass(
                    bearing: bearing,
                    onNorth: () => unawaited(_returnNorth()),
                  ),
                ),
              ),
            if (_mapFailure.failure case final failure?)
              Positioned(
                top: widget.fullBleed
                    ? (widget.ornamentTop ??
                              MediaQuery.paddingOf(context).top + 8) +
                          30
                    : 12,
                left: 12,
                right: 12,
                child: Semantics(
                  liveRegion: true,
                  child: Material(
                    key: const Key('native-map-load-error'),
                    color: Theme.of(context).colorScheme.surface,
                    elevation: 2,
                    borderRadius: BorderRadius.circular(12),
                    child: Padding(
                      padding: const EdgeInsets.only(
                        left: 12,
                        top: 8,
                        bottom: 8,
                      ),
                      child: Row(
                        children: [
                          const Icon(Icons.warning_amber_rounded, size: 22),
                          const SizedBox(width: 8),
                          Expanded(
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(failure.message),
                                const SizedBox(height: 2),
                                Text(
                                  '已保留地点与结果。',
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                              ],
                            ),
                          ),
                          TextButton(
                            key: const Key('native-map-retry'),
                            onPressed: _mapFailure.retrying
                                ? null
                                : () => unawaited(_retryMap()),
                            style: TextButton.styleFrom(
                              minimumSize: const ui.Size(64, 48),
                            ),
                            child: Text(_mapFailure.retrying ? '重试中' : '重试'),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
              ),
            if (_mapFailure.failure == null &&
                ((!widget.fullBleed && pointCount == 0) ||
                    widget.placeStateMessage != null))
              Positioned(
                top: 12,
                left: 12,
                right: 12,
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: Theme.of(
                      context,
                    ).colorScheme.surface.withValues(alpha: 0.94),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 10,
                      vertical: 6,
                    ),
                    child: Text(
                      widget.placeStateMessage ?? '当前列表没有可公开的精确地点标记。',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
