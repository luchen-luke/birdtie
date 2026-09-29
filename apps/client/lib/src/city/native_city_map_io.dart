import 'dart:async';

import 'package:flutter/material.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart';

import 'mapbox_style.dart';
import 'public_city_controller.dart';
import '../workspace/map_entities.dart';

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
    this.fullBleed = false,
    this.contextKey = '',
  });

  final PublicCity city;
  final List<PublicPlace> places;
  final String accessToken;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final String? placeStateMessage;
  final List<MapEntity> entities;
  final String? selectedEntityId;
  final ValueChanged<MapEntity>? onEntitySelected;
  final bool fullBleed;
  final String contextKey;

  @override
  State<NativeCityMapView> createState() => _NativeCityMapViewState();
}

class _NativeCityMapViewState extends State<NativeCityMapView> {
  MapboxMap? _map;
  CircleAnnotationManager? _markers;
  bool _styleReady = false;
  late final CameraViewportState _initialViewport;

  @override
  void initState() {
    super.initState();
    MapboxOptions.setAccessToken(widget.accessToken);
    final viewport = widget.city.map!;
    _initialViewport = CameraViewportState(
      center: Point(
        coordinates: Position(viewport.longitude, viewport.latitude),
      ),
      zoom: viewport.defaultZoom,
    );
  }

  @override
  void didUpdateWidget(covariant NativeCityMapView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.accessToken != widget.accessToken) {
      MapboxOptions.setAccessToken(widget.accessToken);
    }
    if (oldWidget.places != widget.places ||
        oldWidget.entities != widget.entities ||
        oldWidget.selectedEntityId != widget.selectedEntityId) {
      unawaited(_refreshMarkers());
    }
    if (oldWidget.contextKey != widget.contextKey ||
        oldWidget.places != widget.places ||
        oldWidget.entities != widget.entities ||
        oldWidget.selectedEntityId != widget.selectedEntityId) {
      unawaited(_focusContext());
    }
  }

  Future<void> _focusContext() async {
    final map = _map;
    if (map == null) return;
    if (widget.contextKey == 'idle') {
      final viewport = widget.city.map!;
      await map.flyTo(
        CameraOptions(
          center: Point(
            coordinates: Position(viewport.longitude, viewport.latitude),
          ),
          zoom: viewport.defaultZoom,
        ),
        MapAnimationOptions(duration: 450),
      );
      return;
    }
    final points = <(double, double)>[
      for (final entity in widget.entities) (entity.latitude, entity.longitude),
      for (final place in widget.places)
        if (place.location.hasPublicPoint)
          (place.location.latitude!, place.location.longitude!),
    ];
    if (points.isEmpty) return;
    final selected = widget.entities.where(
      (entity) => entity.id == widget.selectedEntityId,
    );
    final selectedPlace = widget.places.where(
      (place) =>
          'place:${place.id}' == widget.selectedEntityId &&
          place.location.hasPublicPoint,
    );
    final latitude = selected.isNotEmpty
        ? selected.first.latitude
        : selectedPlace.isNotEmpty
        ? selectedPlace.first.location.latitude!
        : points.map((point) => point.$1).reduce((a, b) => a + b) /
              points.length;
    final longitude = selected.isNotEmpty
        ? selected.first.longitude
        : selectedPlace.isNotEmpty
        ? selectedPlace.first.location.longitude!
        : points.map((point) => point.$2).reduce((a, b) => a + b) /
              points.length;
    await map.flyTo(
      CameraOptions(
        center: Point(coordinates: Position(longitude, latitude)),
        zoom: selected.isNotEmpty || selectedPlace.isNotEmpty ? 14 : 12.8,
      ),
      MapAnimationOptions(duration: 450),
    );
  }

  Future<void> _onStyleLoaded() async {
    final map = _map;
    if (map == null || !mounted) return;
    if (widget.fullBleed) {
      await map.logo.updateSettings(
        LogoSettings(
          position: OrnamentPosition.TOP_LEFT,
          marginTop: 163,
          marginLeft: 8,
        ),
      );
      await map.attribution.updateSettings(
        AttributionSettings(
          position: OrnamentPosition.TOP_LEFT,
          marginTop: 168,
          marginLeft: 105,
        ),
      );
    }
    _styleReady = true;
    _markers ??= await map.annotations.createCircleAnnotationManager();
    _markers!.tapEvents(
      onTap: (annotation) {
        final placeId = annotation.customData?['placeId'];
        final entityId = annotation.customData?['entityId'];
        if (entityId is String) {
          for (final entity in widget.entities) {
            if (entity.id == entityId) {
              widget.onEntitySelected?.call(entity);
              return;
            }
          }
        }
        for (final place in widget.places) {
          if (place.id == placeId) {
            widget.onPlaceSelected(place);
            return;
          }
        }
      },
    );
    await _refreshMarkers();
    await _focusContext();
  }

  Future<void> _refreshMarkers() async {
    final markers = _markers;
    if (!_styleReady || markers == null) return;
    await markers.deleteAll();
    if (!mounted) return;
    final options = [
      for (final place in widget.places)
        if (place.location.hasPublicPoint)
          CircleAnnotationOptions(
            geometry: Point(
              coordinates: Position(
                place.location.longitude!,
                place.location.latitude!,
              ),
            ),
            circleColor: const Color(0xFF17493C).toARGB32(),
            circleRadius: 10,
            circleStrokeColor: Colors.white.toARGB32(),
            circleStrokeWidth: 2,
            customData: {'placeId': place.id},
          ),
      for (final entity in widget.entities)
        CircleAnnotationOptions(
          geometry: Point(
            coordinates: Position(entity.longitude, entity.latitude),
          ),
          circleColor:
              (entity.id == widget.selectedEntityId
                      ? const Color(0xFF193B32)
                      : switch (entity.kind) {
                          MapEntityKind.person => const Color(0xFF4A7869),
                          MapEntityKind.peopleCluster => const Color(
                            0xFF4A7869,
                          ),
                          MapEntityKind.activity => const Color(0xFFB96743),
                          MapEntityKind.group => const Color(0xFF455C8B),
                          MapEntityKind.place => const Color(0xFF193B32),
                        })
                  .toARGB32(),
          circleRadius: entity.id == widget.selectedEntityId ? 15 : 12,
          circleStrokeColor: Colors.white.toARGB32(),
          circleStrokeWidth: 3,
          customData: {'entityId': entity.id},
        ),
    ];
    if (options.isNotEmpty) await markers.createMulti(options);
  }

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
            MapWidget(
              key: ValueKey(widget.city.id),
              styleUri: mapboxStyleUri,
              viewport: _initialViewport,
              onMapCreated: (map) => _map = map,
              onStyleLoadedListener: (_) => unawaited(_onStyleLoaded()),
            ),
            if ((!widget.fullBleed && pointCount == 0) ||
                widget.placeStateMessage != null)
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
