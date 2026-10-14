import 'dart:async';
import 'dart:convert';
import 'dart:js_interop';
import 'package:flutter/material.dart';
import 'package:web/web.dart' as web;
import '../config/birdtie_environment.dart';
import '../workspace/map_entities.dart';
import '../workspace/map_display_policy.dart';
import 'public_city_controller.dart';
import 'map_camera_focus.dart';
import 'map_point_selection_sheet.dart';
import 'map_cluster_members_sheet.dart';
import 'amap_city_map_stub.dart' show AMapSourceGate, AMapDisplayFrame;

@JS('birdtieAMapMount')
external JSNumber _mountAMap(
  JSString elementID,
  JSString config,
  JSFunction onFrame,
  JSFunction onSelect,
  JSFunction onStatus,
  JSFunction onMotion,
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
    this.onMapReaderVisibilityChanged,
    this.onSelectedAnchor,
    this.onCameraMotion,
    this.onMapUnavailable,
    this.preserveViewport = false,
    this.focusRevision = 0,
    this.contentRect,
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
  @override
  State<AMapCityMapView> createState() => _AMapCityMapViewState();
}

class _AMapCityMapViewState extends State<AMapCityMapView> {
  static const _key = BirdtieEnvironment.amapWebPublicKey;
  static const _serviceHost = BirdtieEnvironment.amapServiceHost;
  static int _nextID = 0;
  final _gate = AMapSourceGate(), _changes = MapReaderChanges();
  JSNumber? _handle;
  JSFunction? _onFrame, _onSelect, _onStatus, _onMotion;
  AMapDisplayFrame? _frame;
  MapCameraFocusInput? _focus;
  int _mountEpoch = 0, _viewEpoch = 0, _focusSerial = 0, _renderEpoch = 0;
  bool _failed = false, _choiceOpen = false;

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
  Object? _scope() => mounted
      ? (
          _gate.revision,
          widget.city.id,
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
    _syncSource();
  }

  @override
  void didUpdateWidget(AMapCityMapView old) {
    super.didUpdateWidget(old);
    if (old.readerChanges != widget.readerChanges) {
      old.readerChanges?.removeListener(_sourceChanged);
      widget.readerChanges?.addListener(_sourceChanged);
    }
    _syncSource(forceFocus: old.focusRevision != widget.focusRevision);
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
        'subtitle': e.subtitle,
        'latitude': e.latitude,
        'longitude': e.longitude,
        'precision': e.precision,
        if ({MapEntityKind.place, MapEntityKind.moment}.contains(e.kind) &&
            e.mediaPixels?.current() == true) ...{
          'mediaKey': e.mediaPixels!.key,
          'mediaPNG': base64Encode(e.mediaPixels!.normalPNG),
          'mediaSelectedPNG': base64Encode(e.mediaPixels!.selectedPNG),
          'mediaExpiresAt':
              (e.kind == MapEntityKind.moment
                      ? e.momentPhoto!.expiresAt
                      : e.placePhoto!.expiresAt)
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
      'fit': input.fitsBounds,
      'padding': [padding.top, padding.right, padding.bottom, padding.left],
    };
  }

  void _syncSource({bool forceFocus = false}) {
    if (!mounted) return;
    final changed = _gate.replace(_source(), (
      widget.city.id,
      widget.contextKey,
      widget.currentReaderScope?.call(),
    ));
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
            'markers': _markers(),
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
          'focus': _focusJSON(next),
        }).toJS,
      );
    }
  }

  void _created(Object element) {
    final div = element as web.HTMLDivElement;
    final id = 'birdtie-amap-${_nextID++}', epoch = ++_mountEpoch;
    div.id = id;
    div.style.width = '100%';
    div.style.height = '100%';
    if (_handle case final h?) _destroyAMap(h);
    _handle = null;
    _syncSource();
    bool current() => mounted && epoch == _mountEpoch;
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
        final row = _gate.resolve(
          v['revision'] as int,
          v['id'] as String,
          v['kind'] as String,
        );
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
      );
      final focus = _focusInput();
      _updateAMap(
        _handle!,
        jsonEncode({
          'schema': 'birdtie-amap-source-v1',
          'revision': _gate.revision,
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
    _choiceOpen = true;
    final scope = _scope(), revision = _gate.revision;
    try {
      final candidates = tapped.isCluster
          ? _gate.rows.where((e) => tapped.memberIDs.contains(e.id)).toList()
          : _frame!.candidates(tapped.id);
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
    _gate.dispose();
    _changes.changed();
    _changes.dispose();
    if (_handle case final h?) _destroyAMap(h);
    _handle = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final host = Uri.tryParse(_serviceHost);
    if (_key.isEmpty ||
        host == null ||
        !(host.scheme == 'https' ||
            (host.scheme == 'http' &&
                (host.host == 'localhost' || host.host == '127.0.0.1'))) ||
        !host.path.endsWith('/_AMapService'))
      return const Center(child: Text('高德地图尚未配置，请使用地点列表。'));
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
