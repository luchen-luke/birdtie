import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';

import '../city/public_city_controller.dart';
import '../city/public_city_map.dart';
import 'agent_workspace_controller.dart';
import 'map_entities.dart';
import 'map_layers_controller.dart';
import 'now_discovery_controller.dart';

/// Owns a stable map subtree. Input and overlay changes do not rebuild it.
class MapCanvas extends StatefulWidget {
  const MapCanvas({
    super.key,
    required this.city,
    required this.workspace,
    required this.mapState,
    required this.discovery,
    required this.onInitialViewport,
    required this.onMapUnavailable,
    this.layers,
    this.onChooseCity,
    this.ornamentTop,
    this.onOrnamentHeightChanged,
  });

  final PublicCityController city;
  final AgentWorkspaceController workspace;
  final MapViewportState mapState;
  final NowDiscoveryController discovery;
  final MapLayersController? layers;
  final ValueChanged<MapBounds> onInitialViewport;
  final ValueChanged<String> onMapUnavailable;
  final VoidCallback? onChooseCity;
  final ValueListenable<double?>? ornamentTop;
  final ValueChanged<double>? onOrnamentHeightChanged;

  @override
  State<MapCanvas> createState() => _MapCanvasState();
}

class _MapCanvasState extends State<MapCanvas> {
  late String _projection;
  late AgentWorkspaceController _selectionWorkspace;
  late int _selectionEpoch;
  AgentResult? _selectionResult;

  @override
  void initState() {
    super.initState();
    widget.city.addListener(_refreshProjection);
    widget.workspace.addListener(_refreshProjection);
    widget.discovery.addListener(_refreshProjection);
    widget.layers?.addListener(_refreshProjection);
    widget.ornamentTop?.addListener(_refreshOrnaments);
    _cityResult = widget.workspace.presentedResult;
    _cityTaskCityID = widget.workspace.task?.cityID;
    _cityTaskKey = widget.workspace.presentedResult == null
        ? 'idle'
        : widget.workspace.task?.id ?? 'idle';
    _projection = _projectionKey();
    _selectionWorkspace = widget.workspace;
    _selectionEpoch = widget.workspace.taskEpoch;
    _selectionResult = widget.workspace.presentedResult;
  }

  @override
  void didUpdateWidget(covariant MapCanvas oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.city != widget.city) {
      oldWidget.city.removeListener(_refreshProjection);
      widget.city.addListener(_refreshProjection);
    }
    if (oldWidget.workspace != widget.workspace) {
      oldWidget.workspace.removeListener(_refreshProjection);
      widget.workspace.addListener(_refreshProjection);
    }
    if (oldWidget.discovery != widget.discovery) {
      oldWidget.discovery.removeListener(_refreshProjection);
      widget.discovery.addListener(_refreshProjection);
    }
    if (oldWidget.layers != widget.layers) {
      oldWidget.layers?.removeListener(_refreshProjection);
      widget.layers?.addListener(_refreshProjection);
    }
    if (oldWidget.ornamentTop != widget.ornamentTop) {
      oldWidget.ornamentTop?.removeListener(_refreshOrnaments);
      widget.ornamentTop?.addListener(_refreshOrnaments);
    }
    _refreshProjection();
  }

  @override
  void dispose() {
    widget.city.removeListener(_refreshProjection);
    widget.workspace.removeListener(_refreshProjection);
    widget.discovery.removeListener(_refreshProjection);
    widget.layers?.removeListener(_refreshProjection);
    widget.ornamentTop?.removeListener(_refreshOrnaments);
    super.dispose();
  }

  AgentResult? _cityResult;
  void _refreshOrnaments() {
    if (mounted) setState(() {});
  }

  String? _cityTaskCityID;
  String _cityTaskKey = 'idle';
  bool get _needsCityRecovery =>
      widget.city.selectedCity == null &&
      widget.workspace.queryContextType != 'ONLINE' &&
      widget.workspace.task == null;
  bool get _showCityRecovery =>
      _needsCityRecovery && !widget.workspace.inputFocused;
  AgentResult? get _mapResult => widget.workspace.queryContextType == 'ONLINE'
      ? _cityResult
      : widget.workspace.presentedResult;
  String get _mapContextKey => widget.workspace.queryContextType == 'ONLINE'
      ? _cityTaskKey
      : widget.workspace.presentedResult == null
      ? 'idle'
      : widget.workspace.task?.id ?? 'idle';

  List<MapEntity> _entities() => widget.city.selectedCity == null
      ? const []
      : _mapResult?.projectionItems != null &&
            (widget.workspace.queryContextType == 'ONLINE'
                    ? _cityTaskCityID
                    : widget.workspace.task?.cityID) ==
                widget.city.selectedCity?.id
      ? _mapResult!.entities
      : widget.layers != null
      ? widget.layers!.cityID == widget.city.selectedCity?.id
            ? widget.layers!.items.map((i) => i.entity).toList(growable: false)
            : const []
      : [
          ...?_mapResult?.entities,
          if (_mapResult == null)
            for (final activity
                in widget.discovery.pulse?.activities ??
                    const <PublicActivity>[])
              if (activity.location case final location?)
                if (location.hasPublicPoint)
                  MapEntity(
                    id: 'activity:${activity.id}',
                    kind: MapEntityKind.activity,
                    title: activity.title,
                    subtitle: mapActivityStatusLabel(activity.status),
                    latitude: location.latitude!,
                    longitude: location.longitude!,
                  ),
          for (final org in widget.city.organizationPins)
            MapEntity(
              id: 'organization:${org.id}',
              kind: MapEntityKind.organization,
              title: org.name,
              subtitle: '已审核的公开组织地点',
              latitude: org.latitude,
              longitude: org.longitude,
            ),
        ];

  List<PublicPlace> _places() =>
      widget.city.selectedCity == null || widget.layers != null
      ? const []
      : _mapResult?.places
                .where((place) => place.location.hasPublicPoint)
                .toList() ??
            widget.city.places
                .where((place) => place.location.hasPublicPoint)
                .toList();

  String _projectionKey() {
    final city = widget.city.selectedCity;
    final map = city?.map;
    return [
      city?.id ?? '',
      if (city == null) '${widget.city.cityError}:${widget.city.citiesLoading}',
      'cityRecovery:$_needsCityRecovery',
      map?.provider ?? '',
      '${map?.latitude}:${map?.longitude}:${map?.defaultZoom}',
      widget.workspace.selectedEntityId ?? '',
      _mapContextKey,
      for (final entity in _entities())
        '${entity.id}:${entity.kind}:${entity.title}:${entity.subtitle}:${entity.latitude}:${entity.longitude}',
      for (final place in _places())
        '${place.id}:${place.name}:${place.location.latitude}:${place.location.longitude}',
    ].join('|');
  }

  void _refreshProjection() {
    if (widget.workspace.queryContextType != 'ONLINE') {
      _cityResult = widget.workspace.presentedResult;
      _cityTaskCityID = widget.workspace.task?.cityID;
      _cityTaskKey = widget.workspace.presentedResult == null
          ? 'idle'
          : widget.workspace.task?.id ?? 'idle';
    }
    final next = _projectionKey();
    // A successful turn can retain exactly the same task ID and Pins. Refresh
    // its callback fence without replacing the stable map or its context key.
    final selectionChanged =
        !identical(_selectionWorkspace, widget.workspace) ||
        _selectionEpoch != widget.workspace.taskEpoch ||
        !identical(_selectionResult, widget.workspace.presentedResult);
    if (!mounted || (next == _projection && !selectionChanged)) return;
    _projection = next;
    _selectionWorkspace = widget.workspace;
    _selectionEpoch = widget.workspace.taskEpoch;
    _selectionResult = widget.workspace.presentedResult;
    setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final selectedCity = widget.city.selectedCity;
    final selectionWorkspace = widget.workspace;
    final selectionEpoch = selectionWorkspace.taskEpoch;
    final selectionResult = selectionWorkspace.presentedResult;
    void selectMapEntity(String id, {bool place = false}) {
      final workspace = widget.workspace;
      if (!identical(workspace, selectionWorkspace) ||
          selectedCity == null ||
          widget.city.selectedCity?.id != selectedCity.id ||
          workspace.queryContextType != 'CITY' ||
          workspace.taskEpoch != selectionEpoch ||
          !identical(workspace.presentedResult, selectionResult)) {
        return;
      }
      final visible = place
          ? _places().any((item) => 'place:${item.id}' == id)
          : _entities().any((item) => item.id == id);
      if (!visible) return;
      if (workspace.task == null) {
        // Keep the existing idle public-map selection path.
        workspace.selectEntity(id);
      } else if (workspace.task!.cityID == selectedCity.id) {
        workspace.selectMapEntity(id);
      }
    }

    return Stack(
      children: [
        Positioned.fill(
          child: PublicCityMapView(
            city: selectedCity,
            places: _places(),
            entities: _entities(),
            selectedEntityId: selectedCity == null
                ? null
                : widget.workspace.selectedEntityId,
            contextKey: selectedCity == null ? 'idle' : _mapContextKey,
            onEntitySelected: (entity) => selectMapEntity(entity.id),
            onPlaceSelected: (place) =>
                selectMapEntity('place:${place.id}', place: true),
            onViewportSettled: selectedCity == null
                ? null
                : widget.mapState.cameraSettled,
            onViewportInitialized: selectedCity == null
                ? null
                : widget.onInitialViewport,
            onCameraMotion: selectedCity == null
                ? null
                : widget.mapState.cameraStarted,
            onMapUnavailable: widget.onMapUnavailable,
            fullBleed: true,
            ornamentTop: widget.ornamentTop?.value,
            onOrnamentHeightChanged: widget.onOrnamentHeightChanged,
          ),
        ),
        AnimatedBuilder(
          animation: widget.workspace,
          builder: (context, _) {
            if (!_showCityRecovery) return const SizedBox.shrink();
            return Center(
              child: Padding(
                padding: const EdgeInsets.all(32),
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 340),
                  child: Material(
                    color: Theme.of(context).colorScheme.surface,
                    borderRadius: BorderRadius.circular(12),
                    child: Padding(
                      padding: const EdgeInsets.all(16),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Text(
                            widget.city.cityError ??
                                (widget.city.citiesLoading
                                    ? '正在读取公开城市…'
                                    : '从顶部城市与范围入口选择城市，查看公开内容。'),
                            textAlign: TextAlign.center,
                          ),
                          if (widget.city.cityError != null) ...[
                            const SizedBox(height: 12),
                            FilledButton.icon(
                              onPressed: widget.city.citiesLoading
                                  ? null
                                  : () => widget.city.loadCities(),
                              icon: const Icon(Icons.refresh),
                              label: const Text('重新读取城市'),
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                ),
              ),
            );
          },
        ),
      ],
    );
  }
}
