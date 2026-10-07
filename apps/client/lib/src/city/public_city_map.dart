import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:latlong2/latlong.dart';

import 'map_link.dart';
import 'amap_city_map_stub.dart'
    if (dart.library.html) 'amap_city_map_web.dart';
import 'mapbox_style.dart';
import 'native_city_map_stub.dart'
    if (dart.library.io) 'native_city_map_io.dart';
import 'public_city_controller.dart';
import '../workspace/map_entities.dart';

const _mapboxPublicToken = String.fromEnvironment(
  'BIRDTIE_MAPBOX_PUBLIC_TOKEN',
);
const _mapboxMobileToken = String.fromEnvironment(
  'BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN',
);
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
    this.fullBleed = false,
    this.contextKey = '',
  });

  final PublicCity city;
  final List<PublicPlace> places;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final String? placeStateMessage;
  final List<MapEntity> entities;
  final String? selectedEntityId;
  final ValueChanged<MapEntity>? onEntitySelected;
  final bool fullBleed;
  final String contextKey;

  @override
  State<PublicCityMapView> createState() => _PublicCityMapViewState();
}

class _PublicCityMapViewState extends State<PublicCityMapView> {
  bool _tilesFailed = false;
  final MapController _mapController = MapController();
  String _focusedContext = '';
  String _focusedResultSignature = '';

  @override
  void didUpdateWidget(covariant PublicCityMapView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.city.id != widget.city.id) _tilesFailed = false;
    final resultChanged =
        oldWidget.contextKey != widget.contextKey ||
        oldWidget.entities.map((entity) => entity.id).join(',') !=
            widget.entities.map((entity) => entity.id).join(',') ||
        oldWidget.places.map((place) => place.id).join(',') !=
            widget.places.map((place) => place.id).join(',');
    if (oldWidget.selectedEntityId != widget.selectedEntityId) {
      WidgetsBinding.instance.addPostFrameCallback(
        (_) => _focusContext(selectedOnly: true),
      );
    } else if (resultChanged) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _focusContext());
    }
  }

  void _focusContext({bool selectedOnly = false}) {
    if (!mounted) return;
    final viewport = widget.city.map;
    if (viewport == null) return;
    final context = widget.contextKey;
    final selected = widget.entities.where(
      (entity) => entity.id == widget.selectedEntityId,
    );
    final selectedPlace = widget.places.where(
      (place) =>
          'place:${place.id}' == widget.selectedEntityId &&
          place.location.hasPublicPoint,
    );
    if (selected.isNotEmpty) {
      _mapController.move(
        LatLng(selected.first.latitude, selected.first.longitude),
        14,
      );
      return;
    }
    if (selectedPlace.isNotEmpty) {
      _mapController.move(
        LatLng(
          selectedPlace.first.location.latitude!,
          selectedPlace.first.location.longitude!,
        ),
        14,
      );
      return;
    }
    if (selectedOnly) return;
    final signature = [
      ...widget.entities.map((entity) => entity.id),
      ...widget.places.map((place) => 'place:${place.id}'),
    ]..sort();
    final resultSignature = signature.join(',');
    if (context == _focusedContext &&
        resultSignature == _focusedResultSignature) {
      return;
    }
    _focusedContext = context;
    _focusedResultSignature = resultSignature;
    if (context == 'idle') {
      _mapController.move(
        LatLng(viewport.latitude, viewport.longitude),
        viewport.defaultZoom,
      );
      return;
    }
    final points = <LatLng>{
      for (final entity in widget.entities)
        LatLng(entity.latitude, entity.longitude),
      for (final place in widget.places)
        if (place.location.hasPublicPoint)
          LatLng(place.location.latitude!, place.location.longitude!),
    }.toList();
    if (points.length == 1) {
      _mapController.move(points.single, 12.8);
    } else if (points.length > 1) {
      _mapController.fitCamera(
        CameraFit.bounds(
          bounds: LatLngBounds.fromPoints(points),
          padding: const EdgeInsets.fromLTRB(48, 150, 48, 180),
          maxZoom: 14,
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final viewport = widget.city.map;
    if (viewport == null) {
      return const _MapUnavailable('该城市尚未配置地图视图。');
    }
    if (viewport.provider == 'amap') {
      return AMapCityMapView(
        key: ValueKey(widget.city.id),
        city: widget.city,
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
        return NativeCityMapView(
          key: ValueKey(widget.city.id),
          city: widget.city,
          places: widget.places,
          accessToken: _mapboxMobileToken,
          onPlaceSelected: widget.onPlaceSelected,
          placeStateMessage: widget.placeStateMessage,
          entities: widget.entities,
          selectedEntityId: widget.selectedEntityId,
          onEntitySelected: widget.onEntitySelected,
          fullBleed: widget.fullBleed,
          contextKey: widget.contextKey,
        );
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
              key: ValueKey(widget.city.id),
              mapController: _mapController,
              options: MapOptions(
                initialCenter: LatLng(viewport.latitude, viewport.longitude),
                initialZoom: viewport.defaultZoom,
                initialCameraFit:
                    widget.contextKey != 'idle' && distinctTaskPoints.length > 1
                    ? CameraFit.bounds(
                        bounds: LatLngBounds.fromPoints(taskPoints),
                        padding: const EdgeInsets.fromLTRB(48, 150, 48, 180),
                        maxZoom: 14,
                      )
                    : null,
                minZoom: 3,
                maxZoom: 18,
                backgroundColor: const Color(0xFFE9ECE4),
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
              top: widget.fullBleed ? 142 : null,
              bottom: widget.fullBleed ? null : 8,
              child: const _MapboxAttribution(),
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
