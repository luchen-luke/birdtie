import 'destination_presentation.dart';
import 'public_city_controller.dart';

/// Common ordering is editorial, never a popularity claim.
const destinationCommonCityIDs = <String>[
  'civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b',
  'aberdeen-gb',
];

Iterable<String> get destinationFeaturedCityIDs => destinationReviewedCityIDs
    .where((id) => !destinationCommonCityIDs.contains(id));

/// All reviewed illustrated destinations remain available in their category.
/// Current/recent entries are actual choices, not an invented catalog. The
/// caller's full catalog and global search are never restricted by this view.
List<PublicCity> destinationShortlist({
  required Iterable<PublicCity> catalog,
  Iterable<String> recentCityIDs = const [],
  String? currentCityID,
  String filter = 'all',
}) {
  final byID = <String, PublicCity>{};
  for (final city in catalog) {
    byID.putIfAbsent(city.id, () => city);
  }
  final result = <PublicCity>[], seen = <String>{};
  void add(String? id) {
    final city = byID[id];
    if (city == null ||
        seen.contains(id) ||
        city.name.trim().isEmpty ||
        !RegExp(r'^[A-Z]{2}$').hasMatch(city.countryCode) ||
        city.source.reference.isEmpty ||
        (destinationReviewedCityIDs.contains(id) &&
            !destinationHasReviewedIdentity(city))) {
      return;
    }
    seen.add(city.id);
    if (filter == 'CN' && city.countryCode != 'CN' ||
        filter == 'abroad' && city.countryCode == 'CN')
      return;
    result.add(city);
  }

  destinationCommonCityIDs.forEach(add);
  add(currentCityID);
  recentCityIDs.take(6).forEach(add);
  destinationFeaturedCityIDs.forEach(add);
  return List.unmodifiable(result);
}
