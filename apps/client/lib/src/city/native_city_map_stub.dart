import 'package:flutter/material.dart';

import 'public_city_controller.dart';
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
  Widget build(BuildContext context) => const SizedBox.shrink();
}
