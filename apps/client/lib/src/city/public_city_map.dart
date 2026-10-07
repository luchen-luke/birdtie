import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:latlong2/latlong.dart';

import '../config/birdtie_environment.dart';
import 'map_link.dart';
import 'amap_city_map_stub.dart'
    if (dart.library.html) 'amap_city_map_web.dart';
import 'mapbox_style.dart';
import 'native_city_map_stub.dart'
    if (dart.library.io) 'native_city_map_io.dart';
import 'public_city_controller.dart';
import '../workspace/map_entities.dart';

const _mapboxPublicToken = BirdtieEnvironment.mapboxWebPublicToken;
const _mapboxMobileToken = BirdtieEnvironment.mapboxMobilePublicToken;
// The public token used with Static Tiles must include styles:tiles.

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
    this.onCameraMotion,
    this.onMapUnavailable,
    this.fullBleed = false,
    this.contextKey = '',
    this.ornamentTop,
    this.onOrnamentHeightChanged,
  });

  final PublicCity? city;
  final List<PublicPlace> places;
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
  final ValueChanged<double>? onOrnamentHeightChanged;

  @override
  State<PublicCityMapView> createState() => _PublicCityMapViewState();
}

class _PublicCityMapViewState extends State<PublicCityMapView> {
  bool _tilesFailed = false;
  final MapController _webMapController = MapController();

  @override
  void didUpdateWidget(covariant PublicCityMapView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.city?.id != widget.city?.id) _tilesFailed = false;
  }

  @override
  Widget build(BuildContext context) {
    final city = widget.city;
    final viewport = city?.map;
    // The native basemap uses public build configuration, not business catalog
    // availability. A world camera is a view, never a guessed city or GPS fix.
    if (!kIsWeb &&
        (defaultTargetPlatform == TargetPlatform.iOS ||
            defaultTargetPlatform == TargetPlatform.android) &&
        _mapboxMobileToken.startsWith('pk.') &&
        (viewport == null || viewport.provider == 'mapbox')) {
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
        onViewportSettled: city == null ? null : widget.onViewportSettled,
        onViewportInitialized: city == null
            ? null
            : widget.onViewportInitialized,
        onCameraMotion: city == null ? null : widget.onCameraMotion,
        onMapUnavailable: widget.onMapUnavailable,
        fullBleed: widget.fullBleed,
        contextKey: city == null ? 'idle' : widget.contextKey,
        ornamentTop: widget.ornamentTop,
      );
    }
    if (city == null) {
      return const _MapUnavailable('当前平台的底图暂不可用，可选择城市查看公开内容。');
    }
    if (viewport == null) {
      return const _MapUnavailable('该城市尚未配置地图视图。');
    }
    if (viewport.provider == 'amap') {
      return AMapCityMapView(
        key: ValueKey('${city.id}-amap'),
        city: city,
        places: widget.places,
        entities: widget.entities,
        selectedEntityId: widget.selectedEntityId,
        onPlaceSelected: widget.onPlaceSelected,
        onEntitySelected: widget.onEntitySelected,
        fullBleed: widget.fullBleed,
      );
    }
    if (viewport.provider != 'mapbox') {
      return const _MapUnavailable('该城市地图暂不可用，请使用地点列表。');
    }
    if (!kIsWeb) {
      if (defaultTargetPlatform == TargetPlatform.iOS ||
          defaultTargetPlatform == TargetPlatform.android) {
        if (!_mapboxMobileToken.startsWith('pk.')) {
          return const _MapUnavailable('地图暂不可用，请使用地点列表。');
        }
        // Valid native configuration returned above independently of catalog.
        return const _MapUnavailable('地图配置暂不可用，可选择城市查看公开内容。');
      }
      return const _MapUnavailable('该城市地图暂不可用，请使用地点列表。');
    }
    if (!_mapboxPublicToken.startsWith('pk.')) {
      return const _MapUnavailable('地图暂不可用，请使用地点列表。');
    }
    final taskPoints = <LatLng>[
      for (final entity in widget.entities)
        LatLng(entity.latitude, entity.longitude),
      for (final place in widget.places)
        if (place.location.hasPublicPoint)
          LatLng(place.location.latitude!, place.location.longitude!),
    ];
    final mapCenter = taskPoints.isEmpty
        ? LatLng(viewport.latitude, viewport.longitude)
        : LatLng(
            taskPoints.map((point) => point.latitude).reduce((a, b) => a + b) /
                taskPoints.length,
            taskPoints.map((point) => point.longitude).reduce((a, b) => a + b) /
                taskPoints.length,
          );
    final distinctTaskPoints = taskPoints.toSet();
    final points = widget.places.where(
      (place) => place.location.hasPublicPoint,
    );
    return ClipRRect(
      borderRadius: BorderRadius.circular(widget.fullBleed ? 0 : 18),
      child: SizedBox(
        height: widget.fullBleed ? double.infinity : 430,
        child: Stack(
          children: [
            FlutterMap(
              key: ValueKey('${city.id}-mapbox-web'),
              mapController: _webMapController,
              options: MapOptions(
                initialCenter: mapCenter,
                initialZoom: taskPoints.isEmpty ? viewport.defaultZoom : 12.8,
                initialCameraFit: distinctTaskPoints.length > 1
                    ? CameraFit.bounds(
                        bounds: LatLngBounds.fromPoints(taskPoints),
                        padding: const EdgeInsets.fromLTRB(48, 150, 48, 180),
                        maxZoom: 14,
                      )
                    : null,
                minZoom: 3,
                maxZoom: 18,
                backgroundColor: const Color(0xFFE9ECE4),
                onMapEvent: (event) {
                  if (event is! MapEventMoveEnd &&
                      event is! MapEventFlingAnimationEnd) {
                    return;
                  }
                  final visibleBounds = event.camera.visibleBounds;
                  final settled = MapBounds(
                    west: visibleBounds.west,
                    south: visibleBounds.south,
                    east: visibleBounds.east,
                    north: visibleBounds.north,
                  );
                  if (settled.isValid) {
                    scheduleMicrotask(() {
                      if (mounted) widget.onViewportSettled?.call(settled);
                    });
                  }
                },
              ),
              children: [
                TileLayer(
                  urlTemplate:
                      'https://api.mapbox.com/styles/v1/$mapboxStylePath/tiles/512/{z}/{x}/{y}?access_token=$_mapboxPublicToken',
                  tileSize: 512,
                  zoomOffset: -1,
                  userAgentPackageName: 'com.birdtie.client',
                  errorTileCallback: (tile, error, stackTrace) {
                    if (mounted && !_tilesFailed) {
                      setState(() => _tilesFailed = true);
                    }
                  },
                ),
                MarkerLayer(
                  markers: [
                    for (final place in points)
                      Marker(
                        point: LatLng(
                          place.location.latitude!,
                          place.location.longitude!,
                        ),
                        width: 44,
                        height: 44,
                        child: IconButton.filled(
                          tooltip: place.name,
                          onPressed: () => widget.onPlaceSelected(place),
                          icon: const Icon(Icons.place, size: 20),
                        ),
                      ),
                  ],
                ),
                if (widget.entities.isNotEmpty &&
                    widget.onEntitySelected != null)
                  MapEntityLayer(
                    entities: widget.entities,
                    selectedId: widget.selectedEntityId,
                    onSelected: widget.onEntitySelected!,
                  ),
              ],
            ),
            if (_tilesFailed ||
                (!widget.fullBleed && points.isEmpty) ||
                widget.placeStateMessage != null)
              Positioned(
                top: 12,
                left: 12,
                right: 12,
                child: _MapNote(
                  _tilesFailed
                      ? '地图底图暂不可用，请使用地点列表。'
                      : widget.placeStateMessage ?? '当前列表没有可公开的精确地点标记。',
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
}

class _MapUnavailable extends StatelessWidget {
  const _MapUnavailable(this.message);
  final String message;

  @override
  Widget build(BuildContext context) => Container(
    height: double.infinity,
    alignment: Alignment.center,
    decoration: BoxDecoration(
      color: const Color(0xFFE9ECE4),
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
      color: Colors.white.withValues(alpha: 0.94),
      borderRadius: BorderRadius.circular(8),
    ),
    child: Text(message, style: const TextStyle(fontSize: 12)),
  );
}

class _MapboxAttribution extends StatelessWidget {
  const _MapboxAttribution();

  static void _open(String address) => openMapAttribution(address);

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
    decoration: BoxDecoration(
      color: Colors.white.withValues(alpha: 0.96),
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
      style: const TextStyle(
        fontSize: 11,
        color: Color(0xFF193B32),
        decoration: TextDecoration.underline,
      ),
    ),
  );
}
