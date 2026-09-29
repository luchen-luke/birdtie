import 'dart:async';

import 'package:flutter/material.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart';

import 'mapbox_style.dart';
import 'public_city_controller.dart';

class NativeCityMapView extends StatefulWidget {
  const NativeCityMapView({
    super.key,
    required this.city,
    required this.places,
    required this.accessToken,
    required this.onPlaceSelected,
    this.placeStateMessage,
  });

  final PublicCity city;
  final List<PublicPlace> places;
  final String accessToken;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final String? placeStateMessage;

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
    if (oldWidget.places != widget.places) {
      unawaited(_refreshMarkers());
    }
  }

  Future<void> _onStyleLoaded() async {
    final map = _map;
    if (map == null || !mounted) return;
    _styleReady = true;
    _markers ??= await map.annotations.createCircleAnnotationManager();
    _markers!.tapEvents(
      onTap: (annotation) {
        final placeId = annotation.customData?['placeId'];
        for (final place in widget.places) {
          if (place.id == placeId) {
            widget.onPlaceSelected(place);
            return;
          }
        }
      },
    );
    await _refreshMarkers();
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
    ];
    if (options.isNotEmpty) await markers.createMulti(options);
  }

  @override
  Widget build(BuildContext context) {
    final pointCount = widget.places
        .where((place) => place.location.hasPublicPoint)
        .length;
    return ClipRRect(
      borderRadius: BorderRadius.circular(18),
      child: SizedBox(
        height: 430,
        child: Stack(
          children: [
            MapWidget(
              key: ValueKey(widget.city.id),
              styleUri: mapboxStyleUri,
              viewport: _initialViewport,
              onMapCreated: (map) => _map = map,
              onStyleLoadedListener: (_) => unawaited(_onStyleLoaded()),
            ),
            if (pointCount == 0 || widget.placeStateMessage != null)
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
