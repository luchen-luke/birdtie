import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart';

import 'mapbox_style.dart';
import 'map_camera_focus.dart';
import 'native_map_failure.dart';
import 'public_city_controller.dart';
import '../workspace/map_entities.dart';
import '../workspace/map_marker_bitmap.dart';

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
    this.onCameraMotion,
    this.onMapUnavailable,
    this.fullBleed = false,
    this.contextKey = '',
    this.ornamentTop,
  });

  final PublicCity? city;
  final List<PublicPlace> places;
  final String accessToken;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final String? placeStateMessage;
  final List<MapEntity> entities;
  final String? selectedEntityId;
  final ValueChanged<MapEntity>? onEntitySelected;
  final ValueChanged<MapBounds>? onViewportSettled;
  final ValueChanged<MapBounds>? onViewportInitialized;
  final VoidCallback? onCameraMotion;
  final ValueChanged<String>? onMapUnavailable;
  final bool fullBleed;
  final String contextKey;
  final double? ornamentTop;

  @override
  State<NativeCityMapView> createState() => _NativeCityMapViewState();
}

class _NativeCityMapViewState extends State<NativeCityMapView> {
  MapboxMap? _map;
  PointAnnotationManager? _markers;
  Cancelable? _markerTapSubscription;
  final Map<String, PointAnnotation> _annotations = {};
  final Map<String, String> _annotationFingerprints = {};
  Future<void> _markerUpdates = Future<void>.value();
  Timer? _cameraSettleTimer;
  DateTime _lastCameraChangeAt = DateTime.fromMillisecondsSinceEpoch(0);
  double? _renderedClusterZoom;
  DateTime _ignoreIdleUntil = DateTime.fromMillisecondsSinceEpoch(0);
  bool _styleReady = false;
  final _mapFailure = NativeMapFailureState();
  Timer? _mapRetryTimer;
  CameraOptions? _retryCamera;
  int? _retryScopeGeneration;
  String? _retryContextKey;
  String? _retrySelectionID;
  int _ornamentSerial = 0;
  int _scopeGeneration = 0;
  int _focusSerial = 0;
  MapCameraFocusInput? _retryFocusInput;
  int? _retryMotionGeneration;
  late final CameraViewportState _initialViewport;

  @override
  void initState() {
    super.initState();
    MapboxOptions.setAccessToken(widget.accessToken);
    final viewport = widget.city?.map;
    _initialViewport = CameraViewportState(
      center: Point(
        coordinates: Position(
          viewport?.longitude ?? 0,
          viewport?.latitude ?? 0,
        ),
      ),
      zoom: viewport?.defaultZoom ?? 1,
    );
  }

  MapCameraFocusInput _focusInput(NativeCityMapView view) =>
      MapCameraFocusInput(
        city: view.city,
        contextKey: view.contextKey,
        places: view.places,
        entities: view.entities,
        selectedEntityId: view.selectedEntityId,
      );

  @override
  void didUpdateWidget(covariant NativeCityMapView oldWidget) {
    super.didUpdateWidget(oldWidget);
    final previous = _focusInput(oldWidget);
    final current = _focusInput(widget);
    if (!current.hasSameViewInputs(previous)) {
      _scopeGeneration++;
      _focusSerial++;
    }
    if (_styleReady && current.shouldFocusAfter(previous)) {
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
    if (_placeFingerprint(oldWidget.places) !=
            _placeFingerprint(widget.places) ||
        _entityFingerprint(oldWidget.entities) !=
            _entityFingerprint(widget.entities) ||
        oldWidget.selectedEntityId != widget.selectedEntityId) {
      unawaited(_refreshMarkers());
    }
  }

  void _invalidatePendingFocus() {
    // Observe pointer input without competing with SDK gestures or clearing
    // the shared entity selection, task result or user camera.
    _focusSerial++;
    _cameraMotionGeneration++;
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
                longitudes.reduce((a, b) => a < b ? a : b),
                latitudes.reduce((a, b) => a < b ? a : b),
              ),
            ),
            northeast: Point(
              coordinates: Position(
                longitudes.reduce((a, b) => a > b ? a : b),
                latitudes.reduce((a, b) => a > b ? a : b),
              ),
            ),
            infiniteBounds: false,
          ),
          MbxEdgeInsets(top: 150, left: 48, bottom: 180, right: 48),
          0,
          0,
          14,
          null,
        );
      } else {
        camera = CameraOptions(
          center: Point(
            coordinates: Position(center.longitude, center.latitude),
          ),
          zoom: input.zoom,
        );
      }
      if (!current()) return;
      await _animateCamera(map, camera);
    } catch (_) {
      // Keep the existing map/error/retry state on a transient camera API error.
      debugPrint('Birdtie map camera focus unavailable.');
    }
  }

  Future<void> _animateCamera(MapboxMap map, CameraOptions camera) async {
    _cameraChangedSinceIdle = false;
    _cameraMotionGeneration++;
    _ignoreIdleUntil = DateTime.now().add(const Duration(milliseconds: 750));
    await map.flyTo(camera, MapAnimationOptions(duration: 450));
  }

  bool _cameraChangedSinceIdle = false;
  int _cameraMotionGeneration = 0;

  Future<void> _publishInitialViewport() async {
    final map = _map;
    if (map == null ||
        widget.onViewportInitialized == null ||
        _mapFailure.failure != null) {
      return;
    }
    final generation = _scopeGeneration;
    for (var attempt = 0; attempt < 5 && mounted; attempt++) {
      await Future<void>.delayed(const Duration(milliseconds: 250));
      try {
        final camera = await map.getCameraState();
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
        if (!mounted ||
            generation != _scopeGeneration ||
            _mapFailure.failure != null) {
          return;
        }
        if (visible.isValid) {
          widget.onViewportInitialized?.call(visible);
          return;
        }
      } catch (_) {
        // Mapbox can reject a snapshot while the first style is still loading.
      }
    }
  }

  void _onCameraChanged(CameraChangedEventData event) {
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
    unawaited(_publishViewport());
  }

  Future<void> _onMapIdle(MapIdleEventData event) async {
    if (!_styleReady) return;
    if (DateTime.now().isBefore(_ignoreIdleUntil) || !_cameraChangedSinceIdle) {
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
    if (map == null || widget.onViewportSettled == null) return;
    try {
      final camera = await map.getCameraState();
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
          widget.onViewportSettled != null &&
          visible.isValid) {
        widget.onViewportSettled!(visible);
        if (_renderedClusterZoom != camera.zoom) {
          unawaited(_refreshMarkers());
        }
      }
    } catch (_) {
      // A camera snapshot can be unavailable while the native map is starting.
    }
  }

  @override
  void dispose() {
    _cameraSettleTimer?.cancel();
    _mapRetryTimer?.cancel();
    _markerTapSubscription?.cancel();
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
            '${entity.id}:${entity.latitude}:${entity.longitude}:${entity.kind}:${entity.title}:${entity.subtitle}',
      )
      .join('|');

  Future<void> _updateOrnaments() async {
    final map = _map;
    if (map == null || !mounted || !widget.fullBleed) return;
    final serial = ++_ornamentSerial;
    final top = widget.ornamentTop ?? MediaQuery.paddingOf(context).top + 8;
    try {
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
    final firstStyle = !_styleReady;
    debugPrint('Birdtie native map style loaded.');
    if (widget.fullBleed) {
      await map.scaleBar.updateSettings(ScaleBarSettings(enabled: false));
      await _updateOrnaments();
    }
    if (!mounted || map != _map) return;
    _styleReady = true;
    _markers ??= await map.annotations.createPointAnnotationManager();
    await _markers!.setIconAllowOverlap(true);
    _markerTapSubscription ??= _markers!.tapEvents(
      onTap: (annotation) async {
        final entityId = annotation.customData?['entityId'];
        if (entityId is String) {
          if (entityId.startsWith('cluster:')) {
            final cluster = _renderedEntities.where(
              (entity) => entity.id == entityId,
            );
            if (cluster.isNotEmpty) {
              await _openClusterMembers(cluster.first);
            }
            return;
          }
          if (entityId.startsWith('place:')) {
            final placeId = entityId.substring('place:'.length);
            for (final place in widget.places) {
              if (place.id == placeId) {
                widget.onPlaceSelected(place);
                return;
              }
            }
          }
          for (final entity in widget.entities) {
            if (entity.id == entityId) {
              widget.onEntitySelected?.call(entity);
              return;
            }
          }
        }
      },
    );
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
    final changed = _mapFailure.report(event.type.name, event.message);
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
    if (!mounted || !_mapFailure.mapLoaded()) return;
    _mapRetryTimer?.cancel();
    setState(() {});
    // Publish recovered bounds without changing the user's camera or task.
    unawaited(_publishInitialViewport());
  }

  Future<void> _retryMap() async {
    final map = _map;
    if (map == null || !_mapFailure.beginRetry()) return;
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

  Future<void> _openClusterMembers(MapEntity cluster) async {
    final members = cluster.memberIDs
        .map((id) {
          for (final entity in widget.entities) {
            if (entity.id == id) return entity;
          }
          for (final place in widget.places) {
            if ('place:${place.id}' == id && place.location.hasPublicPoint) {
              return MapEntity(
                id: id,
                kind: MapEntityKind.place,
                title: place.name,
                subtitle: place.summary,
                latitude: place.location.latitude!,
                longitude: place.location.longitude!,
              );
            }
          }
          return null;
        })
        .whereType<MapEntity>()
        .toList();
    if (!mounted || members.isEmpty) return;
    final selected = await showModalBottomSheet<MapEntity>(
      context: context,
      isScrollControlled: true,
      backgroundColor: const Color(0xFFFCFBF8),
      builder: (sheetContext) => SafeArea(
        child: SizedBox(
          height: MediaQuery.sizeOf(sheetContext).height * 0.72,
          child: ListView(
            children: [
              const ListTile(title: Text('这里有多个活动、组织与地点')),
              for (final member in members)
                ListTile(
                  title: Text(member.title),
                  subtitle: Text(member.subtitle),
                  onTap: () => Navigator.pop(sheetContext, member),
                ),
            ],
          ),
        ),
      ),
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

  Future<void> _refreshMarkers() async {
    _markerUpdates = _markerUpdates.then((_) => _syncMarkers());
    await _markerUpdates;
  }

  Future<void> _syncMarkers() async {
    final markers = _markers;
    if (!_styleReady || markers == null) return;
    final source = <MapEntity>[
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
          ),
    ];
    final camera = await _map!.getCameraState();
    _renderedClusterZoom = camera.zoom;
    _renderedEntities = clusterMapEntities(
      source,
      zoom: camera.zoom,
      selectedId: widget.selectedEntityId,
    );
    final specs = <String, PointAnnotationOptions>{};
    for (final entity in _renderedEntities) {
      final bytes = await MapMarkerBitmap.render(
        kind: entity.kind,
        selected: entity.id == widget.selectedEntityId,
        count: entity.isCluster ? entity.memberIDs.length : 0,
      );
      specs[entity.id] = PointAnnotationOptions(
        geometry: Point(
          coordinates: Position(entity.longitude, entity.latitude),
        ),
        image: bytes,
        iconAnchor: IconAnchor.BOTTOM,
        iconSize: entity.id == widget.selectedEntityId ? 1.08 : 1,
        symbolSortKey: entity.id == widget.selectedEntityId ? 100 : 0,
        customData: {'entityId': entity.id},
      );
    }
    final stale = _annotations.keys
        .where((id) => !specs.containsKey(id))
        .toList();
    for (final id in stale) {
      final annotation = _annotations.remove(id);
      _annotationFingerprints.remove(id);
      if (annotation != null) await markers.delete(annotation);
    }
    for (final entry in specs.entries) {
      if (!mounted) return;
      final id = entry.key;
      final option = entry.value;
      final fingerprint =
          '${option.geometry.coordinates.lng}:${option.geometry.coordinates.lat}:${option.image.hashCode}:${option.symbolSortKey}';
      final annotation = _annotations[id];
      if (annotation == null) {
        final created = await markers.create(option);
        _annotations[id] = created;
        _annotationFingerprints[id] = fingerprint;
      } else if (_annotationFingerprints[id] != fingerprint) {
        annotation.geometry = option.geometry;
        annotation.image = option.image;
        annotation.iconAnchor = option.iconAnchor;
        annotation.iconSize = option.iconSize;
        annotation.symbolSortKey = option.symbolSortKey;
        annotation.customData = option.customData;
        await markers.update(annotation);
        _annotationFingerprints[id] = fingerprint;
      }
    }
  }

  List<MapEntity> _renderedEntities = const [];

  @override
  Widget build(BuildContext context) {
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
                onMapCreated: (map) => _map = map,
                onStyleLoadedListener: (_) => unawaited(_onStyleLoaded()),
                onMapLoadErrorListener: _onMapLoadError,
                onMapLoadedListener: (_) => _onMapLoaded(),
                onCameraChangeListener: _onCameraChanged,
                onMapIdleListener: _onMapIdle,
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
                                const Text(
                                  '已保留地点与结果。',
                                  style: TextStyle(fontSize: 12),
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
                    color: Colors.white.withValues(alpha: 0.94),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 10,
                      vertical: 6,
                    ),
                    child: Text(
                      widget.placeStateMessage ?? '当前列表没有可公开的精确地点标记。',
                      style: const TextStyle(fontSize: 12),
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
