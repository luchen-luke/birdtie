import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';

import '../city/public_city_controller.dart';
import '../city/public_city_map.dart';
import '../city/map_navigation_target.dart';
import 'agent_workspace_controller.dart';
import 'agent_result_projection.dart';
import 'map_entities.dart';
import 'map_layers_controller.dart';
import 'map_layers_api.dart';
import 'map_media_controller.dart';
import 'owned_map_controller.dart';
import 'now_discovery_controller.dart';

/// Owns a stable map subtree. Input and overlay changes do not rebuild it.
class PersonalCollectionProjection {
  PersonalCollectionProjection({
    required this.id,
    required this.cityID,
    required this.title,
    required List<MapEntity> entities,
    required this.sourceCurrent,
  }) : entities = List.unmodifiable(entities);
  final String id, cityID, title;
  final List<MapEntity> entities;
  final bool Function() sourceCurrent;
  bool get current => sourceCurrent();
}

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
    this.media,
    this.mapContent,
    this.mediaOwnerID,
    this.mediaAuthorizationHeader,
    this.mediaWorkspaceID,
    this.onChooseCity,
    this.ornamentTop,
    this.onOrnamentHeightChanged,
    this.contentRect,
    this.focusRevision,
    this.onSelectedAnchor,
    this.onEntityPicked,
    this.onContentOverlap,
    this.onMapReaderVisibilityChanged,
    this.personalCollection,
  });

  final PublicCityController city;
  final AgentWorkspaceController workspace;
  final MapViewportState mapState;
  final NowDiscoveryController discovery;
  final MapLayersController? layers;
  final MapMediaController? media;
  final OwnedMapController? mapContent;
  final String? Function()? mediaOwnerID,
      mediaAuthorizationHeader,
      mediaWorkspaceID;
  final ValueListenable<PersonalCollectionProjection?>? personalCollection;
  final ValueChanged<MapBounds> onInitialViewport;
  final ValueChanged<String> onMapUnavailable;
  final VoidCallback? onChooseCity;
  final ValueListenable<double?>? ornamentTop;
  final ValueChanged<double>? onOrnamentHeightChanged;
  final ValueListenable<Rect?>? contentRect;
  final ValueListenable<int>? focusRevision;
  final ValueChanged<({String? id, Offset? point})>? onSelectedAnchor;
  final ValueChanged<String>? onEntityPicked;
  final ValueChanged<List<MapEntity>>? onContentOverlap;
  final ValueChanged<({Object token, bool visible})>?
  onMapReaderVisibilityChanged;

  @override
  State<MapCanvas> createState() => _MapCanvasState();
}

class _MapCanvasState extends State<MapCanvas> {
  late String _projection;
  late AgentWorkspaceController _selectionWorkspace;
  late int _selectionEpoch;
  AgentResult? _selectionResult;
  TypedMapView? _selectionAuthorView;
  late int _navigationGeneration;

  @override
  void initState() {
    super.initState();
    _navigationGeneration = widget.city.navigationGeneration;
    widget.city.addListener(_refreshProjection);
    widget.workspace.addListener(_refreshProjection);
    widget.discovery.addListener(_refreshProjection);
    widget.layers?.addListener(_refreshProjection);
    widget.media?.addListener(_refreshProjection);
    widget.mapContent?.addListener(_refreshProjection);
    widget.personalCollection?.addListener(_refreshProjection);
    widget.ornamentTop?.addListener(_refreshOrnaments);
    widget.contentRect?.addListener(_refreshOrnaments);
    widget.focusRevision?.addListener(_refreshOrnaments);
    _cityResult = widget.workspace.presentedResult;
    _cityTaskCityID = widget.workspace.task?.cityID;
    _cityTaskKey = widget.workspace.presentedResult == null
        ? 'idle'
        : widget.workspace.task?.id ?? 'idle';
    _projection = _projectionKey();
    _selectionWorkspace = widget.workspace;
    _selectionEpoch = widget.workspace.taskEpoch;
    _selectionResult = widget.workspace.presentedResult;
    _selectionAuthorView = widget.layers?.authorView;
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
    if (oldWidget.media != widget.media) {
      oldWidget.media?.removeListener(_refreshProjection);
      widget.media?.addListener(_refreshProjection);
    }
    if (oldWidget.mapContent != widget.mapContent) {
      oldWidget.mapContent?.removeListener(_refreshProjection);
      widget.mapContent?.addListener(_refreshProjection);
    }
    if (oldWidget.personalCollection != widget.personalCollection) {
      oldWidget.personalCollection?.removeListener(_refreshProjection);
      widget.personalCollection?.addListener(_refreshProjection);
    }
    if (oldWidget.ornamentTop != widget.ornamentTop) {
      oldWidget.ornamentTop?.removeListener(_refreshOrnaments);
      widget.ornamentTop?.addListener(_refreshOrnaments);
    }
    if (oldWidget.contentRect != widget.contentRect) {
      oldWidget.contentRect?.removeListener(_refreshOrnaments);
      widget.contentRect?.addListener(_refreshOrnaments);
    }
    if (oldWidget.focusRevision != widget.focusRevision) {
      oldWidget.focusRevision?.removeListener(_refreshOrnaments);
      widget.focusRevision?.addListener(_refreshOrnaments);
    }
    _refreshProjection();
  }

  @override
  void dispose() {
    widget.city.removeListener(_refreshProjection);
    widget.workspace.removeListener(_refreshProjection);
    widget.discovery.removeListener(_refreshProjection);
    widget.layers?.removeListener(_refreshProjection);
    widget.media?.removeListener(_refreshProjection);
    widget.mapContent?.removeListener(_refreshProjection);
    widget.personalCollection?.removeListener(_refreshProjection);
    widget.ornamentTop?.removeListener(_refreshOrnaments);
    widget.contentRect?.removeListener(_refreshOrnaments);
    widget.focusRevision?.removeListener(_refreshOrnaments);
    super.dispose();
  }

  AgentResult? _cityResult;
  void _refreshOrnaments() {
    if (mounted) setState(() {});
  }

  String? _cityTaskCityID;
  String _cityTaskKey = 'idle';
  bool get _needsCityRecovery =>
      widget.city.renderCity == null &&
      widget.workspace.queryContextType != 'ONLINE' &&
      widget.workspace.task == null;
  bool get _showCityRecovery =>
      _needsCityRecovery && !widget.workspace.inputFocused;
  AgentResult? get _mapResult => !widget.workspace.taskMapProjectionCurrent
      ? null
      : widget.workspace.queryContextType == 'ONLINE'
      ? _cityResult
      : widget.workspace.presentedResult;
  String get _mapContextKey => !widget.workspace.taskMapProjectionCurrent
      ? 'idle'
      : widget.workspace.queryContextType == 'ONLINE'
      ? _cityTaskKey
      : widget.workspace.presentedResult == null
      ? 'idle'
      : widget.workspace.task?.id ?? 'idle';

  bool get _showsMapContent {
    if (widget.personalCollection?.value != null ||
        widget.layers?.authorID != null ||
        widget.city.contentCityID != widget.mapContent?.cityID)
      return false;
    if (widget.workspace.areaOperation?.read?.current == true &&
        widget.workspace.areaOperation?.cityID == widget.city.selectedCity?.id)
      return true;
    final resultCity = widget.workspace.queryContextType == 'ONLINE'
        ? _cityTaskCityID
        : widget.workspace.task?.cityID;
    return !(_mapResult?.projectionItems != null &&
        resultCity == widget.city.selectedCity?.id);
  }

  List<MapEntity> _entities() {
    final base = _baseEntities();
    if (!_showsMapContent) return base;
    final content =
        widget.mapContent?.entities
            .where(
              (entity) =>
                  widget.layers == null ||
                  widget.layers!.visible.contains('PLACE') ||
                  (widget.layers!.visible.contains('MOMENT') &&
                      (widget.mapContent?.find(entity.id)?.momentCount ?? 0) >
                          0),
            )
            .toList() ??
        const <MapEntity>[];
    final places = content.map((e) => e.placeID).whereType<String>().toSet();
    // These pixels have their own session-bound reader. Passing them through
    // the public-media decorator would erase or misattribute that authority.
    return [
      for (final entity in base)
        if (entity.kind != MapEntityKind.place ||
            !places.contains(entity.placeID))
          entity,
      ...content,
    ];
  }

  List<MapEntity> _baseEntities() => widget.personalCollection?.value != null
      ? widget.personalCollection!.value!.current &&
                widget.personalCollection!.value!.cityID ==
                    widget.city.selectedCity?.id
            ? widget.personalCollection!.value!.entities
            : const []
      : widget.city.selectedCity == null
      ? const []
      : widget.layers?.authorID != null
      ? widget.layers!.authorCityID == widget.city.selectedCity?.id
            ? _typedMapEntities(widget.layers!.items)
            : const []
      : widget.workspace.areaOperation?.read?.current == true &&
            widget.workspace.areaOperation!.cityID ==
                widget.city.selectedCity?.id
      ? _typedMapEntities(widget.workspace.areaOperation!.items)
      : _mapResult?.projectionItems != null &&
            (widget.workspace.queryContextType == 'ONLINE'
                    ? _cityTaskCityID
                    : widget.workspace.task?.cityID) ==
                widget.city.selectedCity?.id
      ? _resultMapEntities(_mapResult!)
      : widget.layers != null
      ? widget.layers!.cityID == widget.city.selectedCity?.id
            ? _typedMapEntities(
                widget.layers!.items.where(
                  (item) =>
                      widget.mapContent == null ||
                      (item.kind != 'PLACE' && item.kind != 'MOMENT'),
                ),
              )
            : const []
      : [
          if (_mapResult case final result?) ..._resultMapEntities(result),
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
      widget.personalCollection?.value != null ||
          widget.city.selectedCity == null ||
          widget.layers != null ||
          _mapResult?.replyProjectionCurrent == false
      ? const []
      : _mapResult?.places
                .where((place) => place.location.hasPublicPoint)
                .toList() ??
            widget.city.places
                .where((place) => place.location.hasPublicPoint)
                .toList();

  List<MapEntity> _typedMapEntities(Iterable<TypedMapItem> items) => [
    for (final item in items)
      _decorate(
        item.entity.withAnchorMetadata(
          placeID: item.placeID,
          precision: 'point',
          publicLocation: item.kind != 'OPPORTUNITY',
        ),
      ),
  ];

  MapEntity _decorate(MapEntity entity) =>
      widget.media?.decorate(entity) ?? entity;

  void _syncMedia() {
    final controller = widget.media;
    if (controller == null) return;
    final layers = widget.layers;
    final area = widget.workspace.areaOperation;
    final typed =
        widget.personalCollection?.value == null &&
        widget.city.selectedCity != null &&
        layers?.cityID == widget.city.selectedCity!.id &&
        (_mapResult == null || area?.read?.current == true);
    final items = typed && layers?.authorID == null
        ? (area?.read?.current == true
              ? area!.items
              : layers!.items
                    .where(
                      (item) =>
                          widget.mapContent == null ||
                          (item.kind != 'PLACE' && item.kind != 'MOMENT'),
                    )
                    .toList())
        : const <TypedMapItem>[];
    controller.sync(
      ownerID: widget.mediaOwnerID?.call(),
      workspaceID: widget.mediaWorkspaceID?.call(),
      authorization: widget.mediaAuthorizationHeader?.call(),
      cityID: widget.city.selectedCity?.id ?? '',
      sourceGeneration: (
        layers,
        layers?.epoch,
        layers?.publicView,
        layers?.authorView,
        layers?.authorID,
        area,
        widget.workspace.taskEpoch,
      ),
      entities: [
        ...items
            .where((e) => e.mapID == widget.workspace.selectedEntityId)
            .map((e) => e.entity),
        ...items
            .where((e) => e.mapID != widget.workspace.selectedEntityId)
            .map((e) => e.entity),
      ],
      devicePixelRatio: MediaQuery.devicePixelRatioOf(context),
    );
  }

  List<MapEntity> _resultMapEntities(AgentResult result) => [
    for (final entity in result.entities)
      if (result.projectionItems
              ?.where((i) => i.entity.mapID == entity.id)
              .firstOrNull
              ?.anchor
          case final anchor?)
        entity.withAnchorMetadata(
          placeID: anchor.placeID,
          precision: anchor.precision,
          isTaskResult: true,
        )
      else
        entity.withAnchorMetadata(
          placeID: entity.placeID,
          precision: entity.precision,
          publicLocation: entity.publicLocation,
          isTaskResult: true,
        ),
  ];

  String _projectionKey() {
    final city = widget.city.renderCity;
    final map = city?.map;
    return [
      city?.id ?? '',
      widget.city.contentCityID,
      if (city == null) '${widget.city.cityError}:${widget.city.citiesLoading}',
      'cityRecovery:$_needsCityRecovery',
      map?.provider ?? '',
      'navigation:${widget.city.navigationGeneration}',
      '${map?.latitude}:${map?.longitude}:${map?.defaultZoom}',
      widget.workspace.selectedEntityId ?? '',
      _mapContextKey,
      'quiet:${widget.workspace.preserveReplyCamera}',
      'collection:${widget.personalCollection?.value?.id}:${widget.personalCollection?.value?.current}',
      'author:${widget.layers?.epoch}:${widget.layers?.authorID}:${widget.layers?.authorView?.validUntil}',
      'media:${widget.media?.revision}',
      'content:${widget.mapContent?.epoch}:${widget.mapContent?.current}:${widget.mapContent?.loading}:${widget.mapContent?.error}:${widget.mapContent?.view?.truncated}',
      for (final entity in _entities())
        '${entity.id}:${entity.kind}:${entity.title}:${entity.subtitle}:${entity.latitude}:${entity.longitude}:${entity.placeID}:${entity.precision}:${entity.sourceVersion}:${entity.decorationKey}',
      for (final place in _places())
        '${place.id}:${place.name}:${place.location.latitude}:${place.location.longitude}',
    ].join('|');
  }

  void _refreshProjection() {
    final generation = widget.city.navigationGeneration;
    if (_navigationGeneration != generation) {
      _navigationGeneration = generation;
      if (widget.city.mapTarget?.source != MapNavigationSource.task) {
        final wasCurrent = widget.workspace.taskMapProjectionCurrent;
        widget.workspace.detachTaskMapProjection(notify: false);
        if (wasCurrent && widget.workspace.task != null) {
          widget.workspace.retirePendingQueryForViewChange();
        }
      }
    }
    if (!widget.workspace.taskMapProjectionCurrent) {
      _cityResult = null;
      _cityTaskCityID = null;
      _cityTaskKey = 'idle';
    } else if (widget.workspace.queryContextType != 'ONLINE') {
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
        !identical(_selectionResult, widget.workspace.presentedResult) ||
        !identical(_selectionAuthorView, widget.layers?.authorView);
    if (!mounted || (next == _projection && !selectionChanged)) return;
    _projection = next;
    _selectionWorkspace = widget.workspace;
    _selectionEpoch = widget.workspace.taskEpoch;
    _selectionResult = widget.workspace.presentedResult;
    _selectionAuthorView = widget.layers?.authorView;
    setState(() {});
  }

  // A source GET can replace the projection while a cluster sheet closes.
  // Recheck the selected public row against that fresh read of the exact
  // same task and reply. This grants only selection; domain actions still
  // obtain their own current authorization and confirmation.
  bool _sameReadSelection(AgentResult? shown, AgentResult? fresh, String id) {
    final before = shown?.resultSet, after = fresh?.resultSet;
    if (before == null ||
        after == null ||
        before.schema != typedAgentResultSchema ||
        after.schema != typedAgentResultSchema ||
        before.id.isEmpty ||
        before.id != after.id ||
        before.status != 'ready' ||
        after.status != 'ready' ||
        fresh?.replyProjectionCurrent != true) {
      return false;
    }
    final oldRows = before.items?.where((i) => i.entity.mapID == id).toList();
    final rows = fresh?.projectionItems
        ?.where((i) => i.entity.mapID == id)
        .toList();
    if (oldRows?.length != 1 || rows?.length != 1) return false;
    final old = oldRows!.single, current = rows!.single;
    final oldPoint = old.anchor, point = current.anchor;
    return old.valid &&
        current.valid &&
        old.scope == 'AUTHORIZED_VIEW' &&
        current.scope == old.scope &&
        current.entity == old.entity &&
        current.detail == old.detail &&
        current.share == old.share &&
        current.title == old.title &&
        current.summary == old.summary &&
        current.sourceVersion == old.sourceVersion &&
        oldPoint != null &&
        point != null &&
        point.precision == oldPoint.precision &&
        point.publicZone == oldPoint.publicZone &&
        point.placeID == oldPoint.placeID &&
        point.latitude == oldPoint.latitude &&
        point.longitude == oldPoint.longitude;
  }

  @override
  Widget build(BuildContext context) {
    _syncMedia();
    final selectedCity = widget.city.selectedCity;
    final renderCity = widget.city.renderCity;
    final browseKey = widget.city.browseKey;
    final navigationGeneration = widget.city.navigationGeneration;
    final contentCityID = widget.city.contentCityID;
    final selectionWorkspace = widget.workspace;
    final selectionEpoch = selectionWorkspace.taskEpoch;
    final selectionResult = selectionWorkspace.presentedResult;
    final selectionTask = selectionWorkspace.task;
    final selectionArea = selectionWorkspace.areaOperation;
    final authorLayers = widget.layers;
    final authorEpoch = authorLayers?.epoch;
    final authorID = authorLayers?.authorID;
    final authorView = authorLayers?.authorView;
    final personalCollection = widget.personalCollection?.value;
    final mapContent = widget.mapContent;
    final contentEpoch = mapContent?.epoch;
    void selectMapEntity(
      String id, {
      MapEntity? shownEntity,
      PublicPlace? shownPlace,
    }) {
      if (navigationGeneration != widget.city.navigationGeneration) return;
      final workspace = widget.workspace;
      if (personalCollection != null) {
        if (!identical(personalCollection, widget.personalCollection?.value) ||
            !personalCollection.current ||
            selectedCity?.id != personalCollection.cityID ||
            widget.city.selectedCity?.id != personalCollection.cityID ||
            shownEntity == null ||
            !personalCollection.entities.any(
              (e) =>
                  e.id == id &&
                  e.title == shownEntity.title &&
                  e.latitude == shownEntity.latitude &&
                  e.longitude == shownEntity.longitude,
            ))
          return;
        workspace.selectEntity(id, preserveViewport: true);
        widget.onEntityPicked?.call(id);
        return;
      }
      final contentPin = mapContent?.find(id);
      if (contentPin != null) {
        if (!_showsMapContent ||
            !identical(mapContent, widget.mapContent) ||
            contentEpoch != mapContent!.epoch ||
            !identical(workspace, selectionWorkspace) ||
            selectionEpoch != workspace.taskEpoch ||
            selectedCity?.id != widget.city.selectedCity?.id ||
            browseKey != widget.city.browseKey ||
            shownEntity == null ||
            shownEntity.sourceVersion != contentPin.sourceVersion ||
            shownEntity.latitude != contentPin.latitude ||
            shownEntity.longitude != contentPin.longitude ||
            !mapContent.sourceCurrent(contentPin))
          return;
        workspace.selectEntity(id, preserveViewport: true);
        widget.onEntityPicked?.call(id);
        return;
      }
      if (!identical(workspace, selectionWorkspace) ||
          selectedCity == null ||
          widget.city.selectedCity?.id != selectedCity.id ||
          (authorID == null && workspace.queryContextType != 'CITY') ||
          workspace.taskEpoch != selectionEpoch ||
          !identical(workspace.task, selectionTask) ||
          (!identical(workspace.presentedResult, selectionResult) &&
              !_sameReadSelection(
                selectionResult,
                workspace.presentedResult,
                id,
              ))) {
        return;
      }
      if (!identical(authorLayers, widget.layers) ||
          authorEpoch != widget.layers?.epoch ||
          authorID != widget.layers?.authorID) {
        return;
      }
      if (authorID != null &&
          (!identical(authorLayers, widget.layers) ||
              authorLayers?.epoch != authorEpoch ||
              authorLayers?.authorID != authorID ||
              !identical(authorLayers?.authorView, authorView) ||
              authorView == null ||
              authorLayers?.find(id) == null)) {
        return;
      }
      // Native cluster rows can outlive a refresh. Their old display values
      // must not borrow a fresh callback merely because the ID still exists.
      final visible = shownPlace != null
          ? _places().any(
              (item) =>
                  'place:${item.id}' == id &&
                  item.name == shownPlace.name &&
                  item.summary == shownPlace.summary &&
                  item.location.latitude == shownPlace.location.latitude &&
                  item.location.longitude == shownPlace.location.longitude,
            )
          : shownEntity != null &&
                _entities().any(
                  (item) =>
                      item.id == id &&
                      item.kind == shownEntity.kind &&
                      item.title == shownEntity.title &&
                      item.subtitle == shownEntity.subtitle &&
                      item.latitude == shownEntity.latitude &&
                      item.longitude == shownEntity.longitude &&
                      item.sourceVersion == shownEntity.sourceVersion,
                );
      if (!visible) return;
      if (authorID != null) {
        workspace.selectEntity(id, preserveViewport: true);
        widget.onEntityPicked?.call(id);
        return;
      }
      if (selectionArea != null &&
          (!identical(selectionArea, workspace.areaOperation) ||
              (selectionArea.read != null && !selectionArea.current))) {
        return;
      }
      if (selectionArea?.read?.current == true &&
          selectionArea!.cityID == selectedCity.id) {
        if (!identical(selectionArea, workspace.areaOperation)) return;
        for (final item in selectionArea.items) {
          if (item.mapID == id && workspace.retainsMapAreaItem(item)) {
            workspace.selectEntity(id, preserveViewport: true);
            widget.onEntityPicked?.call(id);
            return;
          }
        }
        return;
      }
      if (workspace.task == null) {
        // Keep the existing idle public-map selection path.
        workspace.selectEntity(id, preserveViewport: authorID != null);
        widget.onEntityPicked?.call(id);
      } else if (workspace.task!.cityID == selectedCity.id) {
        if (workspace.selectMapEntity(id)) widget.onEntityPicked?.call(id);
      }
    }

    final contentMessage = !_showsMapContent
        ? null
        : widget.mapContent?.error ??
              (widget.mapContent?.loading == true && _entities().isEmpty
                  ? '正在读取地图内容…'
                  : widget.mapContent?.mapContentIncomplete == true
                  ? '已加载 ${widget.mapContent!.loadedPointCount} 个位置'
                  : widget.mapContent?.current == true && _entities().isEmpty
                  ? '这个范围内暂无可访问动态。'
                  : null);
    return Stack(
      children: [
        Positioned.fill(
          child: PublicCityMapView(
            city: renderCity,
            places: _places(),
            entities: _entities(),
            selectedEntityId: renderCity == null
                ? null
                : widget.workspace.selectedEntityId,
            contextKey: selectedCity == null
                ? 'idle'
                : personalCollection?.id ?? _mapContextKey,
            preserveViewport:
                personalCollection != null ||
                widget.workspace.preserveReplyCamera ||
                widget.workspace.areaOperation != null,
            onEntitySelected: (entity) =>
                selectMapEntity(entity.id, shownEntity: entity),
            onPlaceSelected: (place) =>
                selectMapEntity('place:${place.id}', shownPlace: place),
            onContentOverlap: widget.onContentOverlap,
            onCameraSnapshot: (camera) {
              if (navigationGeneration != widget.city.navigationGeneration)
                return;
              final bounds = widget.mapState.viewportBounds;
              widget.city.mapCameraSettled(
                camera,
                navigationGeneration,
                longitudeSpan: bounds?.longitudeSpan,
                latitudeSpan: bounds == null
                    ? null
                    : bounds.north - bounds.south,
              );
            },
            onContentViewportSettled: renderCity == null
                ? null
                : (bounds, zoom) {
                    if (browseKey != widget.city.browseKey ||
                        navigationGeneration !=
                            widget.city.navigationGeneration)
                      return;
                    widget.mapState.zoom = zoom;
                    widget.mapContent?.observeViewport(
                      navigationGeneration: navigationGeneration,
                      cityID: contentCityID,
                      bounds: bounds,
                      zoom: zoom,
                      devicePixelRatio: MediaQuery.devicePixelRatioOf(context),
                      selectedMarkerID: widget.workspace.selectedEntityId,
                    );
                  },
            onViewportSettled: renderCity == null
                ? null
                : (bounds) {
                    if (navigationGeneration !=
                        widget.city.navigationGeneration)
                      return;
                    widget.mapState.cameraSettled(bounds);
                  },
            onViewportInitialized: renderCity == null
                ? null
                : (bounds) {
                    if (navigationGeneration !=
                        widget.city.navigationGeneration)
                      return;
                    widget.city.markMapPresented(navigationGeneration);
                    widget.onInitialViewport(bounds);
                  },
            onCameraMotion: renderCity == null
                ? null
                : () {
                    if (navigationGeneration !=
                        widget.city.navigationGeneration)
                      return;
                    widget.city.mapCameraStarted(navigationGeneration);
                    widget.mapState.cameraStarted();
                  },
            onMapUnavailable: widget.onMapUnavailable,
            fullBleed: true,
            ornamentTop: widget.ornamentTop?.value,
            contentRect: widget.contentRect?.value,
            focusRevision: widget.focusRevision?.value ?? 0,
            onOrnamentHeightChanged: widget.onOrnamentHeightChanged,
            onSelectedAnchor: widget.onSelectedAnchor,
            onMapReaderVisibilityChanged: widget.onMapReaderVisibilityChanged,
            readerChanges: Listenable.merge([
              widget.city,
              widget.workspace,
              widget.discovery,
              widget.layers,
              widget.media,
              widget.mapContent,
              widget.personalCollection,
            ]),
            // Runtime comparison only: never serialize identity credentials.
            // Viewport/query epochs remain in the stricter reader scope below.
            currentRenderAuthorityScope: () => (
              widget.mediaOwnerID?.call(),
              widget.mediaAuthorizationHeader?.call(),
              widget.mediaWorkspaceID?.call(),
              widget.city.navigationGeneration,
              widget.city.selectedCity?.id,
              widget.city.browseKey,
              widget.layers,
              widget.layers?.epoch,
              widget.layers?.cityID,
              widget.layers?.authorID,
              widget.layers?.authorCityID,
              (widget.layers?.visible.toList()?..sort())?.join(','),
              widget.mapContent,
              widget.mapContent?.permissionEpoch,
              widget.mapContent?.cityID,
              widget.workspace.taskEpoch,
              widget.workspace.task?.id,
              widget.personalCollection?.value,
            ),
            currentReaderScope: () => (
              widget.city.navigationGeneration,
              widget.layers,
              widget.layers?.epoch,
              widget.layers?.cityID,
              widget.layers?.bounds,
              widget.layers?.authorID,
              widget.layers?.authorBounds,
              widget.mapContent,
              widget.mapContent?.epoch,
              widget.mapContent?.cityID,
              widget.mapContent?.view?.sourceVersion,
              widget.city.selectedCity?.id,
              widget.city.browseKey,
              widget.workspace.taskEpoch,
              widget.workspace.task?.id,
              widget.personalCollection?.value,
            ),
            currentReaderEntities: () => <MapEntity>[
              ..._entities(),
              for (final place in _places())
                if (place.location.hasPublicPoint)
                  MapEntity(
                    id: 'place:${place.id}',
                    kind: MapEntityKind.place,
                    title: place.name,
                    subtitle: place.summary,
                    latitude: place.location.latitude!,
                    longitude: place.location.longitude!,
                    placeID: place.id,
                    publicLocation: true,
                  ),
            ],
          ),
        ),
        if (contentMessage != null)
          Positioned(
            top: (widget.contentRect?.value?.top ?? 150) + 8,
            left: 16,
            right: 16,
            child: Align(
              alignment: Alignment.topCenter,
              child: Material(
                color: Theme.of(
                  context,
                ).colorScheme.surface.withValues(alpha: .94),
                borderRadius: BorderRadius.circular(12),
                child: Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 12,
                    vertical: 4,
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Flexible(
                        child: Text(
                          contentMessage,
                          key: const Key('now-map-content-notice'),
                        ),
                      ),
                      if (widget.mapContent?.hasPreviousMapPage == true)
                        TextButton(
                          onPressed: widget.mapContent!.loading
                              ? null
                              : widget.mapContent!.previousMapPage,
                          child: const Text('上一组'),
                        ),
                      if (widget.mapContent?.hasNextMapPage == true)
                        TextButton(
                          onPressed: widget.mapContent!.loading
                              ? null
                              : widget.mapContent!.nextMapPage,
                          child: const Text('继续加载'),
                        ),
                      if (widget.mapContent?.error != null)
                        TextButton(
                          onPressed: () => widget.mapContent?.refresh(),
                          child: const Text('重试'),
                        ),
                    ],
                  ),
                ),
              ),
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
