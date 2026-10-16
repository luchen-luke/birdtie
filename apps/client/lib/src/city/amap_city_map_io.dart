import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:amap_map/amap_map.dart' as amap;
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:x_amap_base/x_amap_base.dart' as abase;

import '../workspace/map_display_policy.dart';
import '../workspace/map_entities.dart';
import '../workspace/map_marker_bitmap.dart';
import '../workspace/map_marker_visual.dart';
import 'map_camera_focus.dart';
import 'map_cluster_members_sheet.dart';
import 'map_coordinate_boundary.dart';
import 'map_point_selection_sheet.dart';
import 'now_map_compass.dart';
import 'public_city_controller.dart';

/// Consent is retained only for this running app session. A city/provider switch
/// does not invent consent or repeatedly ask after the same explicit choice.
class _AMapSessionConsent {
  static bool accepted = false;
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
    this.onMapReaderVisibilityChanged,
    this.onSelectedAnchor,
    this.onCameraMotion,
    this.onMapUnavailable,
    this.preserveViewport = false,
    this.focusRevision = 0,
    this.contentRect,
    this.onViewportSettled,
    this.onViewportInitialized,
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
  final ValueChanged<({Object token, bool visible})>?
  onMapReaderVisibilityChanged;
  final ValueChanged<({String? id, Offset? point})>? onSelectedAnchor;
  final VoidCallback? onCameraMotion;
  final ValueChanged<String>? onMapUnavailable;
  final int focusRevision;
  final Rect? contentRect;
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
  static const _androidKey = String.fromEnvironment('BIRDTIE_AMAP_ANDROID_KEY');
  static const _iosKey = String.fromEnvironment('BIRDTIE_AMAP_IOS_KEY');
  static final _privacyURL = Uri.parse('https://lbs.amap.com/pages/privacy/');
  final _changes = MapReaderChanges();
  late final _renderQueue = LatestMapRenderQueue(_render);
  amap.AMapController? _map;
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
  Set<String> _labels = const {}, _previousVisible = const {};
  final Set<Object> _readerTokens = {};
  Object? _sourceScope;
  String _sourceKey = '';
  int _sourceEpoch = 0, _cameraEpoch = 0, _sdkEpoch = 0, _focusSerial = 0;
  bool _foreground = true, _configured = false, _moving = false;
  bool _retiring = false, _choiceOpen = false, _initialized = false;
  String? _failure;
  Size _size = Size.zero;
  double _ratio = 1, _screenRatio = 1;
  Timer? _settle, _expiry, _startup;

  bool get _supported =>
      defaultTargetPlatform == TargetPlatform.android ||
      defaultTargetPlatform == TargetPlatform.iOS;
  bool get _hasKey =>
      (defaultTargetPlatform == TargetPlatform.iOS ? _iosKey : _androidKey)
          .trim()
          .isNotEmpty;
  // Native Android projection uses physical pixels; iOS uses logical points.
  double get _projectionScale =>
      defaultTargetPlatform == TargetPlatform.android ? _screenRatio : 1;
  Object _scope() => (
    widget.city.id,
    widget.contextKey,
    widget.currentReaderScope?.call(),
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
    WidgetsBinding.instance.addObserver(this);
    widget.readerChanges?.addListener(_sourceChanged);
    _syncSource();
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
    if (_configured ||
        !_supported ||
        !_hasKey ||
        !_AMapSessionConsent.accepted) {
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
      apiKey: const abase.AMapApiKey(androidKey: _androidKey, iosKey: _iosKey),
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
    final unique = <String, MapEntity>{};
    if (_foreground) {
      for (final e in _source()) {
        if (e.hasMapAnchor && e.id.isNotEmpty)
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
          unique[id]!.memberIDs,
        ],
    ]);
    if (scope != _sourceScope || key != _sourceKey) {
      final scopeChanged = scope != _sourceScope;
      final removed = _rows.values.any(
        (old) => unique[old.id]?.sourceVersion != old.sourceVersion,
      );
      if (scope != _sourceScope || removed) _retiring = true;
      ++_sourceEpoch;
      _sourceScope = scope;
      _sourceKey = key;
      _rows = Map.unmodifiable(unique);
      _geometry = scopeChanged
          ? const {}
          : {
              for (final id in unique.keys)
                if (_geometry[id] case final point?) id: point,
            };
      _positions = const {};
      _displayed = const [];
      _labels = const {};
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
    final deadlines = <DateTime>[
      for (final e in _rows.values) ...[
        if (e.mediaPixels?.validUntil != null) e.mediaPixels!.validUntil!,
        if (e.placePhoto != null) e.placePhoto!.expiresAt,
        if (e.momentPhoto != null) e.momentPhoto!.expiresAt,
        if (e.recentMoments != null) e.recentMoments!.validUntil,
      ],
    ].where((d) => d.isAfter(now)).toList();
    if (deadlines.isEmpty) return;
    deadlines.sort();
    _expiry = Timer(deadlines.first.difference(now), () {
      if (!mounted) return;
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
    _startup?.cancel();
    _map = map;
    _change(() => _failure = null);
    unawaited(_focusCamera(_focusInput()));
    _scheduleSettled();
  }

  Future<void> _focusCamera(MapCameraFocusInput input) async {
    final map = _map, source = _sourceEpoch, serial = ++_focusSerial;
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
      } else {
        await map.moveCamera(
          amap.CameraUpdate.newLatLngZoom(
            _displayCoordinate(input.center!.latitude, input.center!.longitude),
            input.zoom,
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
    _camera = camera;
    ++_cameraEpoch;
    ++_focusSerial;
    _settle?.cancel();
    if (!_moving) widget.onCameraMotion?.call();
    _moving = true;
    _positions = const {};
    _reportAnchor(null);
    _change(() {});
  }

  void _onMoveEnd(amap.CameraPosition camera) {
    _camera = camera;
    _moving = false;
    ++_cameraEpoch;
    _scheduleSettled();
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
      );
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
      if (!_initialized) {
        _initialized = true;
        widget.onViewportInitialized?.call(bounds);
      }
      if (!current()) return;
      widget.onViewportSettled?.call(bounds);
      widget.onContentViewportSettled?.call(
        bounds,
        _camera?.zoom ?? widget.city.map!.defaultZoom,
      );
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
    final map = _map, source = _sourceEpoch, camera = _cameraEpoch;
    final scope = _scope();
    if (map == null || _size.isEmpty || _moving) return;
    bool current() =>
        mounted &&
        map == _map &&
        serial == _renderQueue.serial &&
        source == _sourceEpoch &&
        camera == _cameraEpoch &&
        scope == _scope() &&
        !_moving;
    final rows = _rows.values.toList();
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
        );
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
        protectedIDs: {
          for (final e in rows)
            if (e.isTaskResult) e.id,
        },
      );
      final next = <String, amap.Marker>{}, keys = <String, String>{};
      for (final e in plan.entities) {
        final selected = e.id == widget.selectedEntityId;
        final dense = plan.denseIDs.contains(e.id);
        final media = e.mediaPixels;
        // Pixels originate in Birdtie's existing permission-aware 12-item /
        // four-request media controllers. This adapter never fetches media.
        final png = media?.png(selected: selected, dense: dense, ratio: _ratio);
        final key = jsonEncode([
          e.kind.name,
          e.sourceVersion,
          e.latitude,
          e.longitude,
          e.categoryCode,
          e.precision,
          e.contentCount,
          e.memberIDs.length,
          selected,
          dense,
          _ratio,
          _screenRatio,
          if (png != null) media!.key,
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
                  ? (e.contentCount > 0 ? e.contentCount : e.memberIDs.length)
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
        final marker = amap.Marker(
          position: _displayCoordinate(e.latitude, e.longitude),
          anchor: compact ? const Offset(.5, .5) : const Offset(.5, 1),
          icon: amap.BitmapDescriptor.fromBytes(nativeBytes),
          clickable: false,
          infoWindowEnable: false,
          zIndex: selected ? 100 : 0,
        )..setIdForCopy(e.id);
        next[e.id] = marker;
      }
      if (!current()) return;
      _change(() {
        if (!current()) return;
        _markers = next;
        _geometry = geometry;
        _geometryCamera = camera;
        _markerKeys = keys;
        _displayed = plan.entities;
        _positions = {...positions, ...plan.clusterPositions};
        _labels = plan.labelIDs;
        _previousVisible = plan.entities.map((e) => e.id).toSet();
      });
      // MarkerUpdates is the plugin's incremental add/change/remove path.
      // Let its widget update reach the platform channel before uncovering a
      // retired permission scope; the subsequent SDK projection is a barrier.
      await WidgetsBinding.instance.endOfFrame;
      if (!current()) return;
      await map.toScreenCoordinate(
        _camera?.target ??
            _displayCoordinate(
              widget.city.map!.latitude,
              widget.city.map!.longitude,
            ),
      );
      if (!current()) return;
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
    final map = _map;
    if (map == null) return;
    try {
      if (defaultTargetPlatform == TargetPlatform.android) {
        await map.setRenderingPaused(!_foreground);
      }
      if (!mounted || map != _map) return;
      await map.setRenderFps(_foreground && _readerTokens.isEmpty ? 30 : 5);
    } catch (_) {
      // Older iOS SDKs do not expose render throttling. Access guards remain.
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
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
    ++_sourceEpoch;
    ++_cameraEpoch;
    ++_sdkEpoch;
    ++_focusSerial;
    _renderQueue.close();
    _settle?.cancel();
    _expiry?.cancel();
    _startup?.cancel();
    widget.readerChanges?.removeListener(_sourceChanged);
    WidgetsBinding.instance.removeObserver(this);
    _changes.dispose();
    // AMapWidget owns and disposes its platform controller exactly once.
    _map = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_supported || !_hasKey || widget.city.map == null) {
      return const Center(
        child: Padding(
          padding: EdgeInsets.all(24),
          child: Text('该设备的高德地图尚未配置，请使用地点列表。'),
        ),
      );
    }
    if (!_configured) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Text(
                  '启用国内地图',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 12),
                const Text(
                  '国内地图由高德提供。启用后，高德会按其隐私权政策处理提供地图服务所需的设备和网络信息。你可以继续使用地点列表。',
                ),
                TextButton(
                  onPressed: () async {
                    final opened = await launchUrl(
                      _privacyURL,
                      mode: LaunchMode.externalApplication,
                    );
                    if (!opened && context.mounted) {
                      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
                        const SnackBar(content: Text('暂时无法打开隐私权政策，请稍后再试。')),
                      );
                    }
                  },
                  child: const Text('查看高德地图开放平台隐私权政策'),
                ),
                FilledButton(
                  onPressed: () {
                    _AMapSessionConsent.accepted = true;
                    _configureAfterConsent();
                    setState(() {});
                  },
                  child: const Text('同意并启用高德地图'),
                ),
              ],
            ),
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
            amap.AMapWidget(
              key: ValueKey('birdtie-native-amap:$sdk'),
              initialCameraPosition: initial,
              markers: _markers.values.toSet(),
              onMapCreated: (map) => _onCreated(map, sdk),
              onCameraMove: _onMove,
              onCameraMoveEnd: _onMoveEnd,
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
                                  _size.height - widget.contentRect!.bottom + 8,
                                )) *
                          _projectionScale)
                      .round(),
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
                  if (_labels.contains(e.id) && e.title.isNotEmpty)
                    Positioned(
                      left: point.dx + 21,
                      top: point.dy - 9,
                      width: MapMarkerVisual.labelWidth,
                      child: IgnorePointer(
                        child: Text(
                          MapMarkerVisual.nativeLabel(
                            e.title,
                            selected: e.id == widget.selectedEntityId,
                            recent: e.recentMoments?.labelAt(
                              DateTime.now().toUtc(),
                            ),
                          ),
                          maxLines: e.id == widget.selectedEntityId ? 3 : 2,
                          style: const TextStyle(
                            fontSize: 12,
                            color: MapMarkerVisual.labelColor,
                            shadows: [
                              Shadow(color: Colors.white, blurRadius: 4),
                            ],
                          ),
                        ),
                      ),
                    ),
                ],
            if (_retiring || !_foreground)
              const ColoredBox(color: Color(0xFFF3F4EF)),
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
