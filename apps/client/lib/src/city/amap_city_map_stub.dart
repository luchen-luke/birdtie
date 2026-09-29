import 'package:flutter/material.dart';

import 'public_city_controller.dart';
import '../workspace/map_entities.dart';

class AMapCityMapView extends StatelessWidget {
  const AMapCityMapView({
    super.key,
    required this.city,
    required this.places,
    required this.entities,
    required this.selectedEntityId,
    required this.onPlaceSelected,
    required this.onEntitySelected,
    required this.fullBleed,
  });

  final PublicCity city;
  final List<PublicPlace> places;
  final List<MapEntity> entities;
  final String? selectedEntityId;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final ValueChanged<MapEntity>? onEntitySelected;
  final bool fullBleed;

  @override
  Widget build(BuildContext context) => const Center(
    child: Padding(
      padding: EdgeInsets.all(24),
      child: Text('该设备上的高德地图尚未配置，请使用地点列表。'),
    ),
  );
}
