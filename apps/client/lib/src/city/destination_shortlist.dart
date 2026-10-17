import 'public_city_controller.dart';

/// Civu's explicit common destinations, reconciled with the current Birdtie
/// public catalog. These are editorial entry points, never popularity counts.
/// Source: Civu-client civu_destination_picker.dart:357-398 and
/// featured_destinations.dart at 1bbf0613b513001447f326225c944c1a1fbd8e8f.
const destinationCommonCityIDs = <String>[
  'civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b',
  'aberdeen-gb',
];

/// Reviewed additions fill a small browse surface without enumerating the
/// catalog or turning the artwork coverage into a destination whitelist.
const destinationFeaturedCityIDs = <String>[
  'civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00',
  'civu-geo-bbffd2af-68b4-49ee-bb01-5e5a7673aa9b',
  'civu-geo-66920ecb-9717-42bb-8320-a43a1d2851d2',
  'civu-geo-7800f687-1404-4bb4-aed3-ddd54a6376af',
];

// The source reference and country were read from the live public catalog on
// 2026-10-17. Names, camera hints and provider objects are not identity proofs.
const _reviewed = <String, (String, String)>{
  'civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b': (
    'CN',
    'civu:geo_area:4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b',
  ),
  'aberdeen-gb': ('GB', 'docs/product/01-Birdtie-产品文档-V3.0.md'),
  'civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00': (
    'CN',
    'civu:geo_area:7a536573-67c6-42f0-9ede-c423fb5dba00',
  ),
  'civu-geo-bbffd2af-68b4-49ee-bb01-5e5a7673aa9b': (
    'CN',
    'civu:geo_area:bbffd2af-68b4-49ee-bb01-5e5a7673aa9b',
  ),
  'civu-geo-66920ecb-9717-42bb-8320-a43a1d2851d2': (
    'GB',
    'civu:geo_area:66920ecb-9717-42bb-8320-a43a1d2851d2',
  ),
  'civu-geo-7800f687-1404-4bb4-aed3-ddd54a6376af': (
    'CN',
    'civu:geo_area:7800f687-1404-4bb4-aed3-ddd54a6376af',
  ),
};

/// Returns only existing catalog objects: common entries, a real current/recent
/// choice, then reviewed additions. It never manufactures cities or changes
/// the caller's full catalog/search source. Filters apply to this small list.
List<PublicCity> destinationShortlist({
  required Iterable<PublicCity> catalog,
  Iterable<String> recentCityIDs = const [],
  String? currentCityID,
  String filter = 'all',
  int limit = 8,
}) {
  final byID = <String, PublicCity>{};
  for (final city in catalog) {
    byID.putIfAbsent(city.id, () => city);
  }
  final result = <PublicCity>[], seen = <String>{};
  final maximum = limit.clamp(1, 8);
  void add(String? id) {
    final city = byID[id];
    if (city == null ||
        seen.contains(id) ||
        result.length >= maximum ||
        city.name.trim().isEmpty ||
        !RegExp(r'^[A-Z]{2}$').hasMatch(city.countryCode) ||
        city.source.reference.isEmpty) {
      return;
    }
    final proof = _reviewed[id];
    if (proof != null &&
        (city.countryCode != proof.$1 || city.source.reference != proof.$2)) {
      return;
    }
    seen.add(city.id);
    result.add(city);
  }

  destinationCommonCityIDs.forEach(add);
  add(currentCityID);
  recentCityIDs.forEach(add);
  destinationFeaturedCityIDs.forEach(add);
  return List.unmodifiable(
    result.where(
      (city) => switch (filter) {
        'CN' => city.countryCode == 'CN',
        'abroad' => city.countryCode != 'CN',
        _ => true,
      },
    ),
  );
}
