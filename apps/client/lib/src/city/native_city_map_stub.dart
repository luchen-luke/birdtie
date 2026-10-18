import 'package:flutter/material.dart';

import 'public_city_controller.dart';
import 'map_navigation_target.dart';
import '../workspace/map_entities.dart';

class NativeCityMapView extends StatelessWidget {
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
    this.onViewportSettled,
    this.onContentViewportSettled,
    this.onContentOverlap,
    this.onViewportInitialized,
    this.onCameraSnapshot,
    this.onCameraMotion,
    this.onMapUnavailable,
    this.fullBleed = false,
    this.contextKey = '',
    this.preserveViewport = false,
    this.ornamentTop,
    this.contentRect,
    this.focusRevision = 0,
    this.onSelectedAnchor,
    this.readerChanges,
    this.currentReaderEntities,
    this.currentReaderScope,
    this.onMapReaderVisibilityChanged,
    this.onCoordinateSelected,
    this.coordinateSourceCurrent,
  });

  final PublicCity? city;
  final List<PublicPlace> places;
  final String accessToken;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final String? placeStateMessage;
  final List<MapEntity> entities;
  final String? selectedEntityId;
  final ValueChanged<MapEntity>? onEntitySelected;
  final ValueChanged<MapCameraSnapshot>? onCameraSnapshot;
  final ValueChanged<MapBounds>? onViewportSettled;
  final void Function(MapBounds bounds, double zoom)? onContentViewportSettled;
  final ValueChanged<List<MapEntity>>? onContentOverlap;
  final ValueChanged<MapBounds>? onViewportInitialized;
  final VoidCallback? onCameraMotion;
  final ValueChanged<String>? onMapUnavailable;
  final bool fullBleed;
  final String contextKey;
  final bool preserveViewport;
  final double? ornamentTop;
  final Rect? contentRect;
  final int focusRevision;
  final ValueChanged<({String? id, Offset? point})>? onSelectedAnchor;
  final Listenable? readerChanges;
  final List<MapEntity> Function()? currentReaderEntities;
  final Object? Function()? currentReaderScope;
  final ValueChanged<({Object token, bool visible})>?
  onMapReaderVisibilityChanged;
  final void Function(double latitude, double longitude)? onCoordinateSelected;
  final bool Function()? coordinateSourceCurrent;

  @override
  Widget build(BuildContext context) => const SizedBox.shrink();
}
