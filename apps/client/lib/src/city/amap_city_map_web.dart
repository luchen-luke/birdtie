import '../workspace/map_content_routes.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:js_interop';
import 'package:flutter/material.dart';
import 'package:web/web.dart' as web;
import '../config/birdtie_environment.dart';
import '../workspace/map_entities.dart';
import '../workspace/map_display_policy.dart';
import 'public_city_controller.dart';
import 'map_navigation_target.dart';
import 'map_camera_focus.dart';
import 'amap_session_consent.dart';
import 'map_privacy_sheet.dart';
import 'map_coordinate_boundary.dart';
import 'map_point_selection_sheet.dart';
import 'map_cluster_members_sheet.dart';
import 'amap_city_map_stub.dart'
    show AMapSourceGate, AMapDisplayFrame, canonicalAMapViewport;

@JS('birdtieAMapMount')
external JSNumber _mountAMap(
  JSString elementID,
  JSString config,
  JSFunction onFrame,
  JSFunction onSelect,
  JSFunction onStatus,
  JSFunction onMotion,
  JSFunction onViewport,
  JSFunction onCoordinate,
);
@JS('birdtieAMapUpdate')
external void _updateAMap(JSNumber handle, JSString config);
@JS('birdtieAMapLayout')
external void _layoutAMap(JSNumber handle, JSString layout);
@JS('birdtieAMapProject')
external void _projectAMap(JSNumber handle);
@JS('birdtieAMapReaderVisible')
external void _readerAMap(JSNumber handle, JSBoolean visible);
@JS('birdtieAMapDestroy')
external void _destroyAMap(JSNumber handle);

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
    this.ornamentTop,
    this.onViewportSettled,
    this.onViewportInitialized,
    this.onCameraSnapshot,
    this.onContentViewportSettled,
    this.onContentOverlap,
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
  final double? ornamentTop;
  final ValueChanged<MapCameraSnapshot>? onCameraSnapshot;
  final ValueChanged<MapBounds>? onViewportSettled, onViewportInitialized;
  final void Function(MapBounds, double)? onContentViewportSettled;
  final ValueChanged<List<MapEntity>>? onContentOverlap;
  final void Function(double, double)? onCoordinateSelected;
  final bool Function()? coordinateSourceCurrent;
  @override
  State<AMapCityMapView> createState() => _AMapCityMapViewState();
}

class _AMapCityMapViewState extends State<AMapCityMapView> {
  static const _key = BirdtieEnvironment.amapWebPublicKey;
  static const _serviceHost = BirdtieEnvironment.amapServiceHost;
  static int _nextID = 0;
  final _gate = AMapSourceGate(), _changes = MapReaderChanges();
  JSNumber? _handle;
  JSFunction? _onFrame,
      _onSelect,
      _onStatus,
      _onMotion,
      _onViewport,
      _onCoordinate;
  AMapDisplayFrame? _frame;
  MapCameraFocusInput? _focus;
  int _mountEpoch = 0, _viewEpoch = 0, _focusSerial = 0, _renderEpoch = 0;
  bool _failed = false, _choiceOpen = false;
  bool _viewportInitialized = false;
  (MapBounds, double)? _lastViewport;
  Object? _sourceScope;
  int _sourceScopeSerial = 0;
  bool _privacyPromptAttempted = false;

  bool get _serviceConfigured {
    final host = Uri.tryParse(_serviceHost);
    return _key.isNotEmpty &&
        host != null &&
        (host.scheme == 'https' ||
            host.scheme == 'http' &&
                (host.host == 'localhost' || host.host == '127.0.0.1')) &&
        host.path.endsWith('/_AMapService');
  }

  Future<void> _preparePrivacy({bool review = false}) async {
    if (!_serviceConfigured) return;
    final consent = AMapSessionConsent.controller;
    await consent.load();
    if (!mounted) return;
    if (!review &&
        (consent.accepted || consent.hasDecision || _privacyPromptAttempted)) {
      setState(() {});
      return;
    }
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
    if (!AMapSessionConsent.accepted && _handle != null) {
      ++_mountEpoch;
      ++_renderEpoch;
      _destroyAMap(_handle!);
      _handle = null;
      _frame = null;
      _viewportInitialized = false;
      _lastViewport = null;
      _gate.retireProjection();
      ++_viewEpoch;
      _changes.changed();
    }
    setState(() {});
  }

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
  Object? _scope() => mounted
      ? (
          _gate.revision,
          widget.city.id,
          widget.city.map?.navigationGeneration,
          widget.contextKey,
          widget.currentReaderScope?.call(),
        )
      : null;
  MapCameraFocusInput _focusInput() => MapCameraFocusInput(
    city: widget.city,
    contextKey: widget.contextKey,
    places: const [],
    entities: _gate.rows,
    selectedEntityId: widget.selectedEntityId,
    preserveViewport: widget.preserveViewport,
  );
  @override
  void initState() {
    super.initState();
    widget.readerChanges?.addListener(_sourceChanged);
    AMapSessionConsent.controller.addListener(_privacyChanged);
    _syncSource();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(_preparePrivacy());
    });
  }

  @override
  void didUpdateWidget(AMapCityMapView old) {
    super.didUpdateWidget(old);
    if (old.readerChanges != widget.readerChanges) {
      old.readerChanges?.removeListener(_sourceChanged);
      widget.readerChanges?.addListener(_sourceChanged);
    }
    if (old.city.id != widget.city.id) {
      _viewportInitialized = false;
      _lastViewport = null;
    }
    _syncSource(forceFocus: old.focusRevision != widget.focusRevision);
    if (old.onCoordinateSelected != widget.onCoordinateSelected &&
        _handle != null) {
      _updateAMap(
        _handle!,
        jsonEncode({
          'schema': 'birdtie-amap-source-v1',
          'revision': _gate.revision,
          'coordinateSelection': widget.onCoordinateSelected != null,
        }).toJS,
      );
    }
    if (old.selectedEntityId != widget.selectedEntityId ||
        old.contentRect != widget.contentRect)
      if (_handle case final h?) _projectAMap(h);
  }

  void _sourceChanged() {
    if (mounted) _syncSource();
  }

  void _reportAnchor(Offset? point) {
    final serial = ++_renderEpoch,
        scope = _scope(),
        selected = widget.selectedEntityId;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted &&
          serial == _renderEpoch &&
          scope == _scope() &&
          selected == widget.selectedEntityId)
        widget.onSelectedAnchor?.call((id: selected, point: point));
    });
  }

  List<Map<String, Object?>> _markers() => [
    for (final e in _gate.rows)
      {
        'id': e.id,
        'kind': e.kind.name,
        'title': e.title,
        'sourceVersion': e.sourceVersion,
        'subtitle': e.subtitle,
        'latitude': e.latitude,
        'longitude': e.longitude,
        'precision': e.precision,
        'contentCount': e.contentCount,
        'anchorCount': e.anchorCount,
        'categoryCode': e.categoryCode,
        'ownedContent': MapDisplayPolicy.contentEntity(e),
        if (e.kind == MapEntityKind.moment &&
            e.mediaPixels?.current() == true &&
            (e.mediaPixels?.validUntil ??
                    e.momentPhoto?.expiresAt ??
                    e.placePhoto?.expiresAt) !=
                null) ...{
          'mediaKey': e.mediaPixels!.key,
          'mediaPNG': base64Encode(e.mediaPixels!.normalPNG),
          'mediaSelectedPNG': base64Encode(e.mediaPixels!.selectedPNG),
          'mediaExpiresAt':
              (e.mediaPixels?.validUntil ??
                      e.momentPhoto?.expiresAt ??
                      e.placePhoto!.expiresAt)
                  .millisecondsSinceEpoch,
          'mediaDPR': e.mediaPixels!.devicePixelRatio,
        },
        if (e.kind == MapEntityKind.place &&
            e.recentMoments?.labelAt(DateTime.now().toUtc()) != null) ...{
          'recentLabel': e.recentMoments!.labelAt(DateTime.now().toUtc()),
          'recentExpiresAt': e.recentMoments!.validUntil.millisecondsSinceEpoch,
        },
      },
  ];
  List<Map<String, Object?>> _routeJSON() => [
    for (final segment in currentMapContentSegments(_gate.rows))
      {
        'id': segment.id,
        'fingerprint': segment.fingerprint,
        'fromId': segment.edge.from.id,
        'toId': segment.edge.to.id,
        'fromSourceVersion': segment.edge.from.sourceVersion,
        'toSourceVersion': segment.edge.to.sourceVersion,
        'expiresAt': segment.edge.geometry!.validUntil.millisecondsSinceEpoch,
        'coordinateSystem': 'gcj02',
        'selected': segment.selected(widget.selectedEntityId),
        'points': [
          for (final point in segment.points)
            (() {
              final p = MapCoordinateBoundary.toAmap(
                point.latitude,
                point.longitude,
              );
              return [p.longitude, p.latitude];
            })(),
        ],
      },
  ];
  Map<String, Object?> _focusJSON(MapCameraFocusInput input) {
    final padding = MapDisplayPolicy.focusPadding(
      MediaQuery.sizeOf(context),
      widget.contentRect,
    );
    return {
      'serial': ++_focusSerial,
      'latitude': input.center!.latitude,
      'longitude': input.center!.longitude,
      'zoom': input.zoom,
      'bearing': input.bearing,
      'pitch': input.pitch,
      'fit': input.fitsBounds,
      if (input.cityBounds?.isValid == true)
        'bounds': {
          'west': input.cityBounds!.west,
          'south': input.cityBounds!.south,
          'east': input.cityBounds!.east,
          'north': input.cityBounds!.north,
        },
      'padding': [padding.top, padding.right, padding.bottom, padding.left],
    };
  }

  void _syncSource({bool forceFocus = false}) {
    if (!mounted) return;
    final sourceScope = (
      widget.city.id,
      widget.city.map?.navigationGeneration,
      widget.contextKey,
      widget.currentReaderScope?.call(),
    );
    // Every source revision replaces the complete authorized set and retires
    // its clicks. Viewport/source-version changes can retain unchanged nodes;
    // city/task changes reset the provider transport as a separate boundary.
    final transportScope = (
      widget.city.id,
      widget.contextKey,
      widget.city.map?.navigationGeneration,
    );
    if (_sourceScope != transportScope) {
      _sourceScope = transportScope;
      _sourceScopeSerial++;
    }
    final changed = _gate.replace(_source(), sourceScope);
    final next = _focusInput(), previous = _focus;
    final focus =
        forceFocus || previous == null || next.shouldFocusAfter(previous);
    _focus = next;
    if (changed) {
      _frame = null;
      _reportAnchor(null);
      // Synchronous retirement precedes all SDK/conversion promises.
      if (_handle case final h?)
        _updateAMap(
          h,
          jsonEncode({
            'schema': 'birdtie-amap-source-v1',
            'revision': _gate.revision,
            'scope': _sourceScopeSerial,
            'coordinateSelection': widget.onCoordinateSelected != null,
            'markers': _markers(),
            'routes': _routeJSON(),
            if (focus && next.center != null) 'focus': _focusJSON(next),
          }).toJS,
        );
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _changes.changed();
      });
    } else if (focus && next.center != null && _handle != null) {
      _updateAMap(
        _handle!,
        jsonEncode({
          'schema': 'birdtie-amap-source-v1',
          'revision': _gate.revision,
          'scope': _sourceScopeSerial,
          'coordinateSelection': widget.onCoordinateSelected != null,
          'focus': _focusJSON(next),
        }).toJS,
      );
    }
  }

  void _created(Object element) {
    if (!AMapSessionConsent.accepted) return;
    final div = element as web.HTMLDivElement;
    final id = 'birdtie-amap-${_nextID++}', epoch = ++_mountEpoch;
    div.id = id;
    div.style.width = '100%';
    div.style.height = '100%';
    if (_handle case final h?) _destroyAMap(h);
    _handle = null;
    _syncSource();
    bool current() =>
        mounted && epoch == _mountEpoch && AMapSessionConsent.accepted;
    _onFrame = ((JSString raw) {
      if (!current()) return;
      _syncSource();
      final frame = _gate.acceptFrame(
        raw.toDart,
        selectedID: widget.selectedEntityId,
        contentRect: widget.contentRect,
      );
      if (frame == null || _handle == null) return;
      _frame = frame;
      _layoutAMap(
        _handle!,
        jsonEncode(frame.layout(widget.selectedEntityId)).toJS,
      );
      _reportAnchor(frame.displayed[widget.selectedEntityId]);
    }).toJS;
    _onSelect = ((JSString raw) {
      if (!current()) return;
      _syncSource();
      try {
        final v = jsonDecode(raw.toDart);
        if (v is! Map ||
            v.length != 5 ||
            v['schema'] != 'birdtie-amap-select-v1' ||
            v['revision'] is! int ||
            v['id'] is! String ||
            v['kind'] is! String ||
            v['frame'] != _frame?.frame)
          return;
        final row =
            _gate.resolve(
              v['revision'] as int,
              v['id'] as String,
              v['kind'] as String,
            ) ??
            _frame?.resolveDisplay(v['id'] as String, v['kind'] as String);
        if (row != null) unawaited(_selectPoint(row));
      } catch (_) {}
    }).toJS;
    _onStatus = ((JSString raw) {
      if (!current()) return;
      try {
        final v = jsonDecode(raw.toDart);
        if (v is! Map ||
            v.length != 3 ||
            v['schema'] != 'birdtie-amap-status-v1' ||
            v['revision'] != _gate.revision ||
            v['ready'] is! bool)
          return;
        final ready = v['ready'] as bool;
        if (_failed == ready) setState(() => _failed = !ready);
        if (!ready) widget.onMapUnavailable?.call('高德底图暂不可用，请使用地点列表。');
      } catch (_) {}
    }).toJS;
    _onMotion = (() {
      if (current()) {
        ++_focusSerial;
        widget.onCameraMotion?.call();
      }
    }).toJS;
    _onViewport = ((JSString raw) {
      if (!current()) return;
      try {
        final v = jsonDecode(raw.toDart);
        if (v is! Map ||
            v['schema'] != 'birdtie-amap-viewport-v1' ||
            v['revision'] != _gate.revision ||
            v['zoom'] is! num ||
            !(v['zoom'] as num).isFinite ||
            v['points'] is! List ||
            (v['points'] as List).length != 5)
          return;
        final points = <Offset>[];
        for (final p in v['points'] as List) {
          if (p is! List ||
              p.length != 2 ||
              p[0] is! num ||
              p[1] is! num ||
              !(p[0] as num).isFinite ||
              !(p[1] as num).isFinite ||
              (p[0] as num).abs() > 180 ||
              (p[1] as num).abs() > 90)
            return;
          points.add(
            Offset((p[0] as num).toDouble(), (p[1] as num).toDouble()),
          );
        }
        final bounds = canonicalAMapViewport(points);
        final zoom = (v['zoom'] as num).toDouble();
        if (bounds == null || zoom < 1 || zoom > 20) return;
        if (!_viewportInitialized) {
          _viewportInitialized = true;
          widget.onViewportInitialized?.call(bounds);
        }
        final center = MapCoordinateBoundary.fromAmap(
          points.last.dy,
          points.last.dx,
        );
        final snapshot = MapCameraSnapshot(
          latitude: center.latitude,
          longitude: center.longitude,
          zoom: zoom,
          bearing: v['bearing'] is num ? (v['bearing'] as num).toDouble() : 0,
          pitch: v['pitch'] is num ? (v['pitch'] as num).toDouble() : 0,
        );
        if (_lastViewport == (bounds, zoom)) return;
        _lastViewport = (bounds, zoom);
        widget.onViewportSettled?.call(bounds);
        widget.onContentViewportSettled?.call(bounds, zoom);
        if (snapshot.isValid) widget.onCameraSnapshot?.call(snapshot);
      } catch (_) {}
    }).toJS;
    _onCoordinate = ((JSString raw) {
      if (!current() ||
          widget.onCoordinateSelected == null ||
          !(widget.coordinateSourceCurrent?.call() ?? true))
        return;
      try {
        final v = jsonDecode(raw.toDart);
        if (v is! Map ||
            v['schema'] != 'birdtie-amap-coordinate-v1' ||
            v['revision'] != _gate.revision ||
            v['longitude'] is! num ||
            v['latitude'] is! num)
          return;
        final lat = (v['latitude'] as num).toDouble();
        final lon = (v['longitude'] as num).toDouble();
        if (!lat.isFinite || !lon.isFinite || lat.abs() > 90 || lon.abs() > 180)
          return;
        final wgs = MapCoordinateBoundary.fromAmap(lat, lon);
        widget.onCoordinateSelected?.call(wgs.latitude, wgs.longitude);
      } catch (_) {}
    }).toJS;
    final viewport = widget.city.map!;
    try {
      _handle = _mountAMap(
        id.toJS,
        jsonEncode({
          'key': _key,
          'serviceHost': _serviceHost,
          'latitude': viewport.latitude,
          'longitude': viewport.longitude,
          'zoom': viewport.defaultZoom,
        }).toJS,
        _onFrame!,
        _onSelect!,
        _onStatus!,
        _onMotion!,
        _onViewport!,
        _onCoordinate!,
      );
      final focus = _focusInput();
      _updateAMap(
        _handle!,
        jsonEncode({
          'schema': 'birdtie-amap-source-v1',
          'revision': _gate.revision,
          'scope': _sourceScopeSerial,
          'coordinateSelection': widget.onCoordinateSelected != null,
          'markers': _markers(),
          if (focus.center != null) 'focus': _focusJSON(focus),
        }).toJS,
      );
    } catch (_) {
      if (current()) setState(() => _failed = true);
    }
  }

  void _visibility(({Object token, bool visible}) value) {
    if (_handle case final h?) _readerAMap(h, value.visible.toJS);
    widget.onMapReaderVisibilityChanged?.call(value);
  }

  Future<void> _selectPoint(MapEntity tapped) async {
    if (!mounted || _choiceOpen || _frame == null) return;
    _syncSource();
    final exact = _gate.resolve(_gate.revision, tapped.id, tapped.kind.name);
    if (exact == null || !MapDisplayPolicy.samePointSource(tapped, exact))
      return;
    if (tapped.isCluster && MapDisplayPolicy.contentEntity(tapped)) {
      widget.onEntitySelected?.call(tapped);
      return;
    }
    _choiceOpen = true;
    final scope = _scope(), revision = _gate.revision;
    try {
      final candidates = MapDisplayPolicy.displayCluster(tapped)
          ? MapDisplayPolicy.currentDisplayClusterMembers(tapped, _gate.rows)
          : tapped.isCluster
          ? _gate.rows.where((e) => tapped.memberIDs.contains(e.id)).toList()
          : [exact];
      if (candidates.length > 1 &&
          candidates.every(MapDisplayPolicy.contentEntity) &&
          widget.onContentOverlap != null) {
        widget.onContentOverlap!(candidates);
        return;
      }
      final ids = {for (final e in candidates) (e.id, e.kind.name)};
      List<MapEntity> fresh() {
        _syncSource();
        return scope == _scope()
            ? _gate.rows
                  .where((e) => ids.contains((e.id, e.kind.name)))
                  .toList()
            : const [];
      }

      if (fresh().isEmpty) return;
      final selected = tapped.isCluster
          ? await showCurrentMapClusterMembers(
              context: context,
              cluster: tapped,
              changes: _changes,
              currentMembers: fresh,
              currentScope: _scope,
              onVisibilityChanged: _visibility,
            )
          : candidates.length == 1
          ? fresh().firstOrNull
          : await showCurrentMapPointChoices(
              context: context,
              changes: _changes,
              currentCandidates: fresh,
              currentScope: _scope,
              onVisibilityChanged: _visibility,
            );
      if (!mounted || selected == null || scope != _scope()) return;
      final current = _gate.resolve(revision, selected.id, selected.kind.name);
      if (current == null) return;
      if (current.kind == MapEntityKind.place) {
        final place = widget.places
            .where(
              (p) =>
                  'place:${p.id}' == current.id &&
                  p.name == current.title &&
                  p.summary == current.subtitle &&
                  p.location.latitude == current.latitude &&
                  p.location.longitude == current.longitude,
            )
            .firstOrNull;
        if (place != null) {
          widget.onPlaceSelected(place);
          return;
        }
      }
      widget.onEntitySelected?.call(current);
    } finally {
      _choiceOpen = false;
    }
  }

  void _retry() {
    ++_mountEpoch;
    ++_renderEpoch;
    _gate.retireProjection();
    _frame = null;
    _viewportInitialized = false;
    _lastViewport = null;
    _changes.changed();
    _reportAnchor(null);
    if (_handle case final h?) _destroyAMap(h);
    _handle = null;
    ++_viewEpoch;
    setState(() => _failed = false);
  }

  @override
  void dispose() {
    ++_mountEpoch;
    ++_renderEpoch;
    widget.readerChanges?.removeListener(_sourceChanged);
    AMapSessionConsent.controller.removeListener(_privacyChanged);
    _gate.dispose();
    _changes.changed();
    _changes.dispose();
    if (_handle case final h?) _destroyAMap(h);
    _handle = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_serviceConfigured)
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text('地图暂不可用，请稍后重试或查看内容列表。'),
            TextButton(onPressed: _retry, child: const Text('重试')),
          ],
        ),
      );
    if (!AMapSessionConsent.accepted)
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              AMapSessionConsent.controller.error ?? '地图服务隐私选择尚未同意，仍可查看可用内容。',
            ),
            TextButton(
              onPressed: () => unawaited(_preparePrivacy(review: true)),
              child: const Text('地图服务隐私说明'),
            ),
          ],
        ),
      );
    return ClipRRect(
      borderRadius: BorderRadius.circular(widget.fullBleed ? 0 : 18),
      child: SizedBox(
        height: widget.fullBleed ? double.infinity : 430,
        child: Stack(
          children: [
            Positioned.fill(
              child: HtmlElementView.fromTagName(
                key: ValueKey('amap-view:$_viewEpoch'),
                tagName: 'div',
                onElementCreated: _created,
              ),
            ),
            if (_failed)
              Align(
                alignment: Alignment.topCenter,
                child: Card(
                  child: Padding(
                    padding: const EdgeInsets.all(10),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Flexible(child: Text('高德底图暂不可用，请使用地点列表。')),
                        TextButton(onPressed: _retry, child: const Text('重试')),
                      ],
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
