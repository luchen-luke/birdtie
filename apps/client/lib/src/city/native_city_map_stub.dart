import 'package:flutter/material.dart';

import 'public_city_controller.dart';

class NativeCityMapView extends StatelessWidget {
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
  Widget build(BuildContext context) => const SizedBox.shrink();
}
