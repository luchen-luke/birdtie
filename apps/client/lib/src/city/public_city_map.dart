import 'dart:async';
import 'dart:math' as math;
import 'map_cluster_members_sheet.dart';

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:latlong2/latlong.dart';

import '../config/birdtie_environment.dart';
import 'map_link.dart';
import 'amap_city_map_stub.dart'
    if (dart.library.html) 'amap_city_map_web.dart'
    if (dart.library.io) 'amap_city_map_io.dart';
import 'map_provider_policy.dart';
import 'mapbox_web_basemap.dart';
import 'map_camera_focus.dart';
import 'native_city_map_stub.dart'
    if (dart.library.io) 'native_city_map_io.dart';
import 'public_city_controller.dart';
import 'map_navigation_target.dart';
import '../workspace/map_entities.dart';
import '../workspace/map_display_policy.dart';
import '../workspace/map_content_routes.dart';

String get _mapboxPublicToken => BirdtieEnvironment.mapboxGlWebPublicToken;
const _mapboxMobileToken = BirdtieEnvironment.mapboxMobilePublicToken;

/// FlutterMap wheel/keyboard events have no MoveEnd. Collect their quiet period
/// here; explicit gesture ends share the same replaceable publication slot.
class WebMapViewportScheduler {
  WebMapViewportScheduler({required this.onMotion, required this.onSettled});

  final VoidCallback onMotion;
  final ValueChanged<MapCamera> onSettled;
  Timer? _pending;
  bool _disposed = false;

  void cancel() {
    _pending?.cancel();
    _pending = null;
  }

  void handle(MapEvent event) {
    if (_disposed) return;
    if (event.source == MapEventSource.mapController ||
        event.source == MapEventSource.fitCamera) {
      cancel();
      return;
    }
    final ended =
        event is MapEventMoveEnd ||
        event is MapEventFlingAnimationEnd ||
        event is MapEventFlingAnimationNotStarted ||
        event is MapEventDoubleTapZoomEnd ||
        event is MapEventRotateEnd;
    final moving =
        event is MapEventWithMove ||
        event is MapEventMoveStart ||
        event is MapEventRotateStart ||
        event is MapEventDoubleTapZoomStart ||
        event is MapEventFlingAnimationStart;
    if (!ended && !moving) return;
    cancel();
    if (moving) onMotion();
    final needsQuietPeriod =
        event is MapEventScrollWheelZoom ||
        event is MapEventNonRotatedSizeChange ||
        (event is MapEventRotate &&
            event.source == MapEventSource.cursorKeyboardRotation);
    if (!ended && !needsQuietPeriod) return;
    _pending = Timer(
      needsQuietPeriod ? const Duration(milliseconds: 250) : Duration.zero,
      () {
        _pending = null;
        if (!_disposed) onSettled(event.camera);
      },
    );
  }

  void dispose() {
    _disposed = true;
    cancel();
  }
}

class PublicCityMapView extends StatefulWidget {
  const PublicCityMapView({
    super.key,
    required this.city,
    required this.places,
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
    this.onOrnamentHeightChanged,
    this.contentRect,
    this.focusRevision = 0,
    this.onSelectedAnchor,
    this.readerChanges,
    this.currentReaderEntities,
    this.currentReaderScope,
    this.currentRenderAuthorityScope,
    this.onMapReaderVisibilityChanged,
    this.onCoordinateSelected,
    this.coordinateSourceCurrent,
  });

  final PublicCity? city;
  final List<PublicPlace> places;
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
  final ValueChanged<double>? onOrnamentHeightChanged;
  final Rect? contentRect;
  final int focusRevision;
  final ValueChanged<({String? id, Offset? point})>? onSelectedAnchor;
  final Listenable? readerChanges;
  final List<MapEntity> Function()? currentReaderEntities;
  final Object? Function()? currentReaderScope;
  final Object? Function()? currentRenderAuthorityScope;
  final ValueChanged<({Object token, bool visible})>?
  onMapReaderVisibilityChanged;

  /// Optional editor input; a map tap is never a Place, visit or GPS fact.
  final void Function(double latitude, double longitude)? onCoordinateSelected;
  final bool Function()? coordinateSourceCurrent;

  @override
  State<PublicCityMapView> createState() => _PublicCityMapViewState();
}

class _PublicCityMapViewState extends State<PublicCityMapView> {
  bool _tilesFailed = false;
  int _webBasemapEpoch = 0;
  final _readerChanges = MapReaderChanges();
  int _readerEpoch = 0;
  final MapController _webMapController = MapController();
  bool _webMapReady = false;
  bool _webBasemapReady = false;
  int _webFocusSerial = 0;
  int _webMotionGeneration = 0;
  final ValueNotifier<int> _webDisplayRevision = ValueNotifier(0);
  late final _webViewportEvents = WebMapViewportScheduler(
    onMotion: () {
      _webFocusSerial++;
      _webMotionGeneration++;
      widget.onCameraMotion?.call();
    },
    onSettled: _publishWebSettledViewport,
  );

  void _publishWebSettledViewport(MapCamera camera) {
    if (!mounted || !_usesWebMapbox || !_webMapReady) return;
    _webDisplayRevision.value++;
    final visible = camera.visibleBounds;
    final settled = MapBounds(
      west: visible.west,
      south: visible.south,
      east: visible.east,
      north: visible.north,
    );
    if (settled.isValid) {
      widget.onViewportSettled?.call(settled);
      _publishWebCamera(camera);
    }
    if (_webBasemapReady) _publishWebContentViewport(camera);
  }

  MapCameraFocusInput _focusInput(PublicCityMapView view) =>
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

  bool get _usesWebMapbox =>
      kIsWeb &&
      mapProviderForCountry(
            countryCode: widget.city?.countryCode ?? '',
            configuredProvider: widget.city?.map?.provider ?? '',
          ) ==
          'mapbox' &&
      _mapboxPublicToken.startsWith('pk.');

  @override
  void didUpdateWidget(covariant PublicCityMapView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.city?.id != widget.city?.id ||
        oldWidget.contextKey != widget.contextKey) {
      ++_readerEpoch;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _readerChanges.changed();
    });
    final previous = _focusInput(oldWidget), current = _focusInput(widget);
    final explicitFocus = oldWidget.focusRevision != widget.focusRevision;
    if (!current.hasSameViewInputs(previous) || explicitFocus) {
      _webFocusSerial++;
      _webViewportEvents.cancel();
    }
    if (oldWidget.city?.id != widget.city?.id) _tilesFailed = false;
    if (!_usesWebMapbox) {
      _webMapReady = false;
      _webBasemapReady = false;
    } else if (current.shouldFocusAfter(previous) || explicitFocus) {
      _scheduleWebFocus();
    }
  }

  void _invalidatePendingWebFocus() {
    _webViewportEvents.cancel();
    _webFocusSerial++;
    _webMotionGeneration++;
  }

  void _webBasemapStatus(bool ready) {
    if (!mounted || !_usesWebMapbox) return;
    final becameReady = ready && !_webBasemapReady;
    _webBasemapReady = ready;
    if (_tilesFailed != !ready) setState(() => _tilesFailed = !ready);
    if (!ready) {
      widget.onMapUnavailable?.call('地图底图暂不可用，请使用地点列表。');
    } else if (becameReady && _webMapReady) {
      final bounds = _webMapController.camera.visibleBounds;
      final initial = MapBounds(
        west: bounds.west,
        south: bounds.south,
        east: bounds.east,
        north: bounds.north,
      );
      if (initial.isValid) {
        widget.onViewportInitialized?.call(initial);
        _publishWebCamera(_webMapController.camera);
      }
      _publishWebContentViewport(_webMapController.camera);
    }
  }

  void _publishWebCamera(MapCamera camera) {
    final snapshot = MapCameraSnapshot(
      latitude: camera.center.latitude,
      longitude: camera.center.longitude,
      zoom: camera.zoom,
      bearing: -camera.rotation,
    );
    if (snapshot.isValid) widget.onCameraSnapshot?.call(snapshot);
  }

  void _publishWebContentViewport(MapCamera camera) {
    if (widget.onContentViewportSettled == null) return;
    final full =
        Offset.zero & Size(camera.nonRotatedSize.x, camera.nonRotatedSize.y);
    final visible = (widget.contentRect ?? full).intersect(full);
    if (visible.isEmpty) return;
    Offset project(Offset pixel) {
      final coordinate = camera.pointToLatLng(math.Point(pixel.dx, pixel.dy));
      return Offset(coordinate.longitude, coordinate.latitude);
    }

    final bounds = MapDisplayPolicy.projectedViewport(
      corners: [
        for (final p in [
          visible.topLeft,
          visible.topRight,
          visible.bottomRight,
          visible.bottomLeft,
        ])
          project(p),
      ],
      center: project(visible.center),
    );
    if (bounds != null)
      widget.onContentViewportSettled?.call(bounds, camera.zoom);
  }

  void _retryWebBasemap() {
    setState(() {
      ++_webBasemapEpoch;
      _tilesFailed = false;
      _webBasemapReady = false;
    });
  }

  void _scheduleWebFocus() {
    final serial = ++_webFocusSerial;
    final motion = _webMotionGeneration;
    final input = _focusInput(widget);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted ||
          !_usesWebMapbox ||
          !_webMapReady ||
          serial != _webFocusSerial ||
          motion != _webMotionGeneration ||
          !input.hasSameViewInputs(_focusInput(widget))) {
        return;
      }
      final center = input.center;
      if (center == null) return;
      _webMapController.rotate(-input.bearing);
      if (input.fitsBounds) {
        _webMapController.fitCamera(
          CameraFit.bounds(
            bounds: LatLngBounds.fromPoints([
              for (final point in input.points)
                LatLng(point.latitude, point.longitude),
            ]),
            padding: _webFocusPadding(),
            maxZoom: 14,
          ),
        );
      } else {
        _webMapController.move(
          LatLng(center.latitude, center.longitude),
          input.zoom,
          offset: widget.contentRect == null
              ? Offset.zero
              : widget.contentRect!.center -
                    Offset(
                      _webMapController.camera.nonRotatedSize.x / 2,
                      _webMapController.camera.nonRotatedSize.y / 2,
                    ),
        );
      }
      if (_webBasemapReady)
        _publishWebContentViewport(_webMapController.camera);
    });
  }

  EdgeInsets _webFocusPadding() {
    final box = context.findRenderObject();
    final size = box is RenderBox && box.hasSize
        ? box.size
        : MediaQuery.sizeOf(context);
    return MapDisplayPolicy.focusPadding(size, widget.contentRect);
  }

  @override
  void dispose() {
    ++_readerEpoch;
    _readerChanges.changed();
    _readerChanges.dispose();
    _webFocusSerial++;
    _webViewportEvents.dispose();
    _webMapController.dispose();
    _webDisplayRevision.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final city = widget.city;
    final viewport = city?.map;
    final provider = mapProviderForCountry(
      countryCode: city?.countryCode ?? '',
      configuredProvider: viewport?.provider ?? 'mapbox',
    );
    final coordinateScope = _pointReaderScope();
    final coordinateCallback = widget.onCoordinateSelected;
    bool coordinateCurrent() =>
        mounted &&
        coordinateScope == _pointReaderScope() &&
        identical(coordinateCallback, widget.onCoordinateSelected) &&
        (widget.coordinateSourceCurrent?.call() ?? true);
    // The native basemap uses public build configuration, not business catalog
    // availability. A world camera is a view, never a guessed city or GPS fix.
    if (!kIsWeb &&
        (defaultTargetPlatform == TargetPlatform.iOS ||
            defaultTargetPlatform == TargetPlatform.android) &&
        _mapboxMobileToken.startsWith('pk.') &&
        provider == 'mapbox') {
      return NativeCityMapView(
        key: const ValueKey('now-mapbox-native'),
        city: city,
        places: city == null ? const [] : widget.places,
        accessToken: _mapboxMobileToken,
        onPlaceSelected: widget.onPlaceSelected,
        placeStateMessage: widget.placeStateMessage,
        entities: city == null ? const [] : widget.entities,
        selectedEntityId: city == null ? null : widget.selectedEntityId,
        onEntitySelected: widget.onEntitySelected,
        onCameraSnapshot: widget.onCameraSnapshot,
        onViewportSettled: city == null ? null : widget.onViewportSettled,
        onViewportInitialized: city == null
            ? null
            : widget.onViewportInitialized,
        onContentViewportSettled: city == null
            ? null
            : widget.onContentViewportSettled,
        onContentOverlap: widget.onContentOverlap,
        onCameraMotion: city == null ? null : widget.onCameraMotion,
        onMapUnavailable: widget.onMapUnavailable,
        fullBleed: widget.fullBleed,
        contextKey: city == null ? 'idle' : widget.contextKey,
        preserveViewport: widget.preserveViewport,
        ornamentTop: widget.ornamentTop,
        contentRect: widget.contentRect,
        focusRevision: widget.focusRevision,
        onSelectedAnchor: widget.onSelectedAnchor,
        readerChanges: widget.readerChanges,
        currentReaderEntities: widget.currentReaderEntities,
        currentReaderScope: widget.currentReaderScope,
        onMapReaderVisibilityChanged: widget.onMapReaderVisibilityChanged,
        onCoordinateSelected: city == null ? null : widget.onCoordinateSelected,
        coordinateSourceCurrent: widget.coordinateSourceCurrent,
      );
    }
    if (city == null) {
      return const _MapUnavailable('当前平台的底图暂不可用，可选择城市查看公开内容。');
    }
    if (viewport == null) {
      return const _MapUnavailable('该目的地的地图暂不可用，请使用地点列表。');
    }
    if (provider == 'amap') {
      return AMapCityMapView(
        key: const ValueKey('now-amap'),
        city: city,
        places: widget.places,
        entities: widget.entities,
        selectedEntityId: widget.selectedEntityId,
        onPlaceSelected: widget.onPlaceSelected,
        onEntitySelected: widget.onEntitySelected,
        fullBleed: widget.fullBleed,
        contextKey: widget.contextKey,
        readerChanges: widget.readerChanges,
        currentReaderEntities: widget.currentReaderEntities,
        currentReaderScope: widget.currentReaderScope,
        currentRenderAuthorityScope: widget.currentRenderAuthorityScope,
        onMapReaderVisibilityChanged: widget.onMapReaderVisibilityChanged,
        onSelectedAnchor: widget.onSelectedAnchor,
        onCameraMotion: widget.onCameraMotion,
        onMapUnavailable: widget.onMapUnavailable,
        preserveViewport: widget.preserveViewport,
        focusRevision: widget.focusRevision,
        contentRect: widget.contentRect,
        ornamentTop: widget.ornamentTop,
        onViewportInitialized: widget.onViewportInitialized,
        onCameraSnapshot: widget.onCameraSnapshot,
        onViewportSettled: widget.onViewportSettled,
        onContentViewportSettled: widget.onContentViewportSettled,
        onContentOverlap: widget.onContentOverlap,
        onCoordinateSelected: widget.onCoordinateSelected,
        coordinateSourceCurrent: coordinateCurrent,
      );
    }
    if (provider != 'mapbox') {
      return const _MapUnavailable('该城市地图暂不可用，请使用地点列表。');
    }
    if (!kIsWeb) {
      if (defaultTargetPlatform == TargetPlatform.iOS ||
          defaultTargetPlatform == TargetPlatform.android) {
        if (!_mapboxMobileToken.startsWith('pk.')) {
          return const _MapUnavailable('地图暂不可用，请使用地点列表。');
        }
        // Valid native configuration returned above independently of catalog.
        return const _MapUnavailable('地图暂不可用，可选择目的地查看公开内容。');
      }
      return const _MapUnavailable('该城市地图暂不可用，请使用地点列表。');
    }
    if (!_mapboxPublicToken.startsWith('pk.')) {
      return const _MapUnavailable('地图暂不可用，请使用地点列表。');
    }
    final focusInput = _focusInput(widget);
    final center =
        focusInput.center ??
        MapCameraPoint('', viewport.latitude, viewport.longitude);
    final taskPoints = [
      for (final point in focusInput.points)
        LatLng(point.latitude, point.longitude),
    ];
    final points = widget.places.where(
      (place) => place.location.hasPublicPoint,
    );
    return ClipRRect(
      borderRadius: BorderRadius.circular(widget.fullBleed ? 0 : 18),
      child: SizedBox(
        height: widget.fullBleed ? double.infinity : 430,
        child: Stack(
          children: [
            Listener(
              onPointerDown: (_) => _invalidatePendingWebFocus(),
              child: FlutterMap(
                key: const ValueKey('now-mapbox-web'),
                mapController: _webMapController,
                options: MapOptions(
                  onTap: coordinateCallback == null
                      ? null
                      : (_, point) {
                          if (coordinateCurrent() &&
                              _webMapReady &&
                              _webBasemapReady &&
                              !_tilesFailed &&
                              point.latitude.isFinite &&
                              point.longitude.isFinite &&
                              point.latitude >= -90 &&
                              point.latitude <= 90 &&
                              point.longitude >= -180 &&
                              point.longitude <= 180) {
                            coordinateCallback(point.latitude, point.longitude);
                          }
                        },
                  initialCenter: LatLng(center.latitude, center.longitude),
                  initialZoom: focusInput.zoom,
                  initialRotation: -focusInput.bearing,
                  onMapReady: () {
                    if (!mounted ||
                        !_usesWebMapbox ||
                        !focusInput.hasSameViewInputs(_focusInput(widget))) {
                      return;
                    }
                    _webMapReady = true;
                    _webDisplayRevision.value++;
                    _scheduleWebFocus();
                    if (_webBasemapReady)
                      _publishWebContentViewport(_webMapController.camera);
                  },
                  initialCameraFit: focusInput.fitsBounds
                      ? CameraFit.bounds(
                          bounds: LatLngBounds.fromPoints(taskPoints),
                          padding: _webFocusPadding(),
                          maxZoom: 14,
                        )
                      : null,
                  minZoom: 3,
                  maxZoom: 18,
                  backgroundColor: Colors.transparent,
                  onMapEvent: _webViewportEvents.handle,
                ),
                children: [
                  Builder(
                    builder: (mapContext) {
                      final camera = MapCamera.of(mapContext);
                      return MapboxWebBasemap(
                        key: ValueKey('mapbox-web-basemap:$_webBasemapEpoch'),
                        accessToken: _mapboxPublicToken,
                        camera: MapboxWebCamera(
                          latitude: camera.center.latitude,
                          longitude: camera.center.longitude,
                          zoom: camera.zoom,
                          rotation: camera.rotation,
                        ),
                        onReady: () => _webBasemapStatus(true),
                        onUnavailable: () => _webBasemapStatus(false),
                      );
                    },
                  ),
                  ValueListenableBuilder<int>(
                    valueListenable: _webDisplayRevision,
                    builder: (context, revision, _) => WebMapDisplayLayer(
                      places: widget.places,
                      entities: widget.entities,
                      selectedID: widget.selectedEntityId,
                      contentRect: widget.contentRect,
                      revision: revision,
                      onSelected: _selectWebEntity,
                      onContentOverlap: widget.onContentOverlap,
                      readerChanges: Listenable.merge([
                        _readerChanges,
                        widget.readerChanges,
                      ]),
                      currentReaderEntities: _currentPointSource,
                      currentReaderScope: _pointReaderScope,
                      onMapReaderVisibilityChanged:
                          widget.onMapReaderVisibilityChanged,
                      onSelectedAnchor: widget.onSelectedAnchor,
                    ),
                  ),
                ],
              ),
            ),
            if (_tilesFailed ||
                (!widget.fullBleed && points.isEmpty) ||
                widget.placeStateMessage != null)
              Positioned(
                top: 12,
                left: 12,
                right: 12,
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    _MapNote(
                      _tilesFailed
                          ? '地图底图暂不可用，请使用地点列表。'
                          : widget.placeStateMessage ?? '当前列表没有可公开的精确地点标记。',
                    ),
                    if (_tilesFailed)
                      TextButton(
                        onPressed: _retryWebBasemap,
                        child: const Text('重试底图'),
                      ),
                  ],
                ),
              ),
            Positioned(
              left: 8,
              right: 8,
              top: widget.fullBleed
                  ? widget.ornamentTop ?? MediaQuery.paddingOf(context).top + 8
                  : null,
              bottom: widget.fullBleed ? null : 8,
              child: Builder(
                builder: (attributionContext) {
                  WidgetsBinding.instance.addPostFrameCallback((_) {
                    if (!attributionContext.mounted) return;
                    final box = attributionContext.findRenderObject();
                    if (box is RenderBox && box.hasSize) {
                      widget.onOrnamentHeightChanged?.call(box.size.height);
                    }
                  });
                  return const _MapboxAttribution();
                },
              ),
            ),
          ],
        ),
      ),
    );
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

  Future<void> _selectWebEntity(MapEntity entity) async {
    if (entity.isCluster && !MapDisplayPolicy.contentEntity(entity)) {
      final selected = await showCurrentMapClusterMembers(
        context: context,
        cluster: entity,
        changes: Listenable.merge([_readerChanges, widget.readerChanges]),
        currentMembers: () => _currentClusterMembers(entity),
        currentScope: () => mounted
            ? (
                _readerEpoch,
                widget.currentReaderScope?.call() ?? widget.contextKey,
              )
            : null,
        onVisibilityChanged: widget.onMapReaderVisibilityChanged,
      );
      if (!mounted || selected == null) return;
      await _selectWebEntity(selected);
      return;
    }
    for (final place in widget.places) {
      if ('place:${place.id}' == entity.id) {
        widget.onPlaceSelected(place);
        return;
      }
    }
    widget.onEntitySelected?.call(entity);
  }
}

/// The actual FlutterMap layer used by the Mapbox Web provider. Keeping this
/// provider boundary mountable also lets widget tests exercise real projection,
/// true-anchor shapes and current-source selection without a second map backend.
class WebMapDisplayLayer extends StatefulWidget {
  const WebMapDisplayLayer({
    super.key,
    required this.places,
    required this.entities,
    required this.selectedID,
    required this.contentRect,
    required this.revision,
    required this.onSelected,
    required this.readerChanges,
    required this.currentReaderEntities,
    required this.currentReaderScope,
    this.onMapReaderVisibilityChanged,
    this.onSelectedAnchor,
    this.onContentOverlap,
  });
  final List<PublicPlace> places;
  final List<MapEntity> entities;
  final String? selectedID;
  final Rect? contentRect;
  final int revision;
  final ValueChanged<MapEntity> onSelected;
  final ValueChanged<List<MapEntity>>? onContentOverlap;
  final Listenable readerChanges;
  final List<MapEntity> Function() currentReaderEntities;
  final Object? Function() currentReaderScope;
  final ValueChanged<({Object token, bool visible})>?
  onMapReaderVisibilityChanged;
  final ValueChanged<({String? id, Offset? point})>? onSelectedAnchor;
  @override
  State<WebMapDisplayLayer> createState() => _WebMapDisplayLayerState();
}

class _WebMapDisplayLayerState extends State<WebMapDisplayLayer> {
  ({String content, Rect? rect})? _inputs;
  MapDisplayPlan? _plan;

  final _readerChanges = MapReaderChanges();
  int _readerEpoch = 0, _anchorFrame = 0;
  Timer? _routeExpiry;
  @override
  void initState() {
    super.initState();
    widget.readerChanges.addListener(_routeSourceChanged);
  }

  void _routeSourceChanged() {
    if (mounted) setState(() {});
  }

  void _armRouteExpiry(Iterable<MapEntity> source) {
    _routeExpiry?.cancel();
    DateTime? until;
    final now = DateTime.now().toUtc();
    for (final segment in currentMapContentSegments(source)) {
      for (final at in [
        segment.edge.geometry!.validUntil,
        segment.edge.from.readGrant!.validUntil,
        segment.edge.to.readGrant!.validUntil,
      ]) {
        if (at.isAfter(now) && (until == null || at.isBefore(until)))
          until = at;
      }
    }
    if (until != null)
      _routeExpiry = Timer(until.difference(now), _routeSourceChanged);
  }

  Object? _pointReaderScope() =>
      mounted ? (_readerEpoch, widget.currentReaderScope()) : null;
  List<MapEntity> _currentPointSource() => {
    for (final entity in widget.currentReaderEntities())
      if (entity.hasMapAnchor) entity.id: entity,
  }.values.toList();

  List<MapEntity> _currentRenderedPointSource() {
    final source = _currentPointSource();
    final visible =
        _plan?.entities
            .where((e) => !_plan!.occludedGlyphIDs.contains(e.id))
            .toList() ??
        const <MapEntity>[];
    final ids = visible.map((e) => e.id).toSet();
    return [
      ...source.where((e) => ids.contains(e.id)),
      for (final entity in visible)
        if (MapDisplayPolicy.displayCluster(entity) &&
            MapDisplayPolicy.currentDisplayClusterMembers(
              entity,
              source,
            ).isNotEmpty)
          entity,
    ];
  }

  Map<String, Offset> _project(Iterable<MapEntity> source, MapCamera camera) =>
      {
        for (final entity in source)
          if (entity.hasMapAnchor)
            entity.id: () {
              final point = camera.latLngToScreenPoint(
                LatLng(entity.latitude, entity.longitude),
              );
              return Offset(point.x, point.y);
            }(),
      };

  @override
  void didUpdateWidget(WebMapDisplayLayer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.readerChanges, widget.readerChanges)) {
      oldWidget.readerChanges.removeListener(_routeSourceChanged);
      widget.readerChanges.addListener(_routeSourceChanged);
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _readerChanges.changed();
    });
  }

  @override
  void dispose() {
    widget.readerChanges.removeListener(_routeSourceChanged);
    _routeExpiry?.cancel();
    ++_readerEpoch;
    ++_anchorFrame;
    _readerChanges.changed();
    _readerChanges.dispose();
    super.dispose();
  }

  bool _pointChoiceOpen = false;
  Future<void> _selectPoint(MapEntity tapped) async {
    if (!mounted || _pointChoiceOpen) return;
    final current = _currentRenderedPointSource()
        .where((e) => MapDisplayPolicy.samePointSource(tapped, e))
        .firstOrNull;
    if (current == null) return;
    if (MapDisplayPolicy.displayCluster(current) &&
        MapDisplayPolicy.currentDisplayClusterMembers(
          current,
          _currentPointSource(),
        ).isEmpty)
      return;
    widget.onSelected(current);
  }

  @override
  Widget build(BuildContext context) {
    final camera = MapCamera.of(context);
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
      unique.putIfAbsent(entity.id, () => entity);
    }
    final positions = _project(unique.values, camera);
    final contentInputs =
        '${widget.revision}:${widget.selectedID}:${unique.values.map((e) => '${e.id}:${e.kind}:${e.title}:${e.subtitle}:${e.latitude}:${e.longitude}:${e.placeID}:${e.precision}:${e.isTaskResult}:${e.memberIDs.join(',')}:${e.decorationKey}').join('|')}';
    // Production builds strip Rect.toString; retain its numeric value in
    // the cache key so reader/keyboard coverage always invalidates the plan.
    final inputs = (content: contentInputs, rect: widget.contentRect);
    // Moving the camera repositions the existing MarkerLayer. Recompute its
    // density only after the camera settles, or authorized source changes.
    if (_inputs != inputs) {
      _inputs = inputs;
      _plan = MapDisplayPolicy.plan(
        entities: unique.values.toList(),
        zoom: camera.zoom,
        selectedID: widget.selectedID,
        contentRect: widget.contentRect,
        previouslyVisibleIDs:
            _plan?.entities.map((e) => e.id).toSet() ?? const {},
        protectedIDs: {
          for (final entity in unique.values)
            if (entity.isTaskResult) entity.id,
        },
        positions: positions,
        previousPlacements: _plan?.placements ?? const {},
        metersPerPixel: MapDisplayPolicy.scaleFromSegment(
          Offset(
            camera
                .pointToLatLng(
                  math.Point(
                    camera.nonRotatedSize.x / 2 - 24,
                    camera.nonRotatedSize.y / 2,
                  ),
                )
                .longitude,
            camera
                .pointToLatLng(
                  math.Point(
                    camera.nonRotatedSize.x / 2 - 24,
                    camera.nonRotatedSize.y / 2,
                  ),
                )
                .latitude,
          ),
          Offset(
            camera
                .pointToLatLng(
                  math.Point(
                    camera.nonRotatedSize.x / 2 + 24,
                    camera.nonRotatedSize.y / 2,
                  ),
                )
                .longitude,
            camera
                .pointToLatLng(
                  math.Point(
                    camera.nonRotatedSize.x / 2 + 24,
                    camera.nonRotatedSize.y / 2,
                  ),
                )
                .latitude,
          ),
          48,
        ),
      );
    }
    final selected = unique[widget.selectedID];
    final anchor = (
      id: widget.selectedID,
      point: selected?.hasMapAnchor == true ? positions[selected!.id]! : null,
    );
    final frame = ++_anchorFrame, scope = _pointReaderScope();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted &&
          frame == _anchorFrame &&
          scope == _pointReaderScope() &&
          widget.selectedID == anchor.id) {
        widget.onSelectedAnchor?.call(anchor);
      }
    });
    _armRouteExpiry(unique.values);
    return Stack(
      children: [
        PolylineLayer(
          polylines: [
            for (final edge in currentMapContentSegments(unique.values))
              Polyline(
                points: [
                  for (final point in edge.points)
                    LatLng(point.latitude, point.longitude),
                ],
                color: edge.selected(widget.selectedID)
                    ? const Color(0xff2c6b58)
                    : const Color(0xb32c6b58),
                strokeWidth: 3,
                pattern: StrokePattern.dashed(segments: [7, 5]),
              ),
          ],
        ),
        IgnorePointer(
          child: MarkerLayer(
            markers: [
              for (final segment in currentMapContentSegments(
                unique.values,
              ).take(64))
                if (segment.direction case final direction?)
                  Marker(
                    point: LatLng(direction.latitude, direction.longitude),
                    width: 14,
                    height: 14,
                    rotate: false,
                    child: Transform.rotate(
                      angle: direction.bearing * math.pi / 180,
                      child: const Icon(
                        Icons.navigation,
                        size: 12,
                        color: Color(0xff2c6b58),
                      ),
                    ),
                  ),
            ],
          ),
        ),
        MapEntityLayer(
          entities: _plan!.entities
              .where((e) => !_plan!.occludedGlyphIDs.contains(e.id))
              .toList(),
          labelIDs: _plan!.labelIDs,
          denseIDs: _plan!.denseIDs,
          placements: _plan!.placements,
          selectedId: widget.selectedID,
          onSelected: (entity) {
            if (scope == _pointReaderScope()) _selectPoint(entity);
          },
        ),
      ],
    );
  }
}

class _MapUnavailable extends StatelessWidget {
  const _MapUnavailable(this.message);
  final String message;

  @override
  Widget build(BuildContext context) => Container(
    height: double.infinity,
    alignment: Alignment.center,
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.surface,
      borderRadius: BorderRadius.circular(18),
    ),
    child: Padding(
      padding: const EdgeInsets.all(24),
      child: Text(message, textAlign: TextAlign.center),
    ),
  );
}

class _MapNote extends StatelessWidget {
  const _MapNote(this.message);
  final String message;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.surface.withValues(alpha: 0.94),
      borderRadius: BorderRadius.circular(8),
    ),
    child: Text(message, style: Theme.of(context).textTheme.bodySmall),
  );
}

class _MapboxAttribution extends StatelessWidget {
  const _MapboxAttribution();

  static void _open(String address) => openMapAttribution(address);

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.surface.withValues(alpha: 0.94),
      borderRadius: BorderRadius.circular(8),
    ),
    child: Wrap(
      spacing: 8,
      runSpacing: 4,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        Semantics(
          label: 'Mapbox',
          child: SvgPicture.asset(
            'assets/images/mapbox-logo.svg',
            width: 121.5,
            height: 30,
          ),
        ),
        _AttributionLink(
          label: '© Mapbox',
          address: 'https://www.mapbox.com/about/maps/',
          onOpen: _open,
        ),
        _AttributionLink(
          label: '© OpenStreetMap',
          address: 'https://www.openstreetmap.org/copyright',
          onOpen: _open,
        ),
        _AttributionLink(
          label: '改进地图',
          address: 'https://apps.mapbox.com/feedback/',
          onOpen: _open,
        ),
      ],
    ),
  );
}

class _AttributionLink extends StatelessWidget {
  const _AttributionLink({
    required this.label,
    required this.address,
    required this.onOpen,
  });

  final String label;
  final String address;
  final void Function(String) onOpen;

  @override
  Widget build(BuildContext context) => InkWell(
    onTap: () => onOpen(address),
    child: Text(
      label,
      style: Theme.of(
        context,
      ).textTheme.bodySmall?.copyWith(decoration: TextDecoration.underline),
    ),
  );
}
