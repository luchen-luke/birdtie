import 'package:flutter/material.dart';

/// Presentation of an existing authoritative category; unknown stays unknown.
enum MapPinCategoryKind {
  dining,
  cafe,
  nightlife,
  culture,
  nature,
  sport,
  shopping,
  other,
}

class MapPinCategory {
  const MapPinCategory(this.kind, this.icon, this.fill, this.label);
  final MapPinCategoryKind kind;
  final IconData icon;
  final Color fill, label;

  static const dining = MapPinCategory(
    MapPinCategoryKind.dining,
    Icons.restaurant_outlined,
    Color(0xFFD18016),
    Color(0xFF99570D),
  );
  static const cafe = MapPinCategory(
    MapPinCategoryKind.cafe,
    Icons.local_cafe_outlined,
    Color(0xFFC27A45),
    Color(0xFF8D5126),
  );
  static const nightlife = MapPinCategory(
    MapPinCategoryKind.nightlife,
    Icons.local_bar_outlined,
    Color(0xFF9361C5),
    Color(0xFF674098),
  );
  static const culture = MapPinCategory(
    MapPinCategoryKind.culture,
    Icons.museum_outlined,
    Color(0xFF49A567),
    Color(0xFF287541),
  );
  static const nature = MapPinCategory(
    MapPinCategoryKind.nature,
    Icons.park_outlined,
    Color(0xFF229F94),
    Color(0xFF166F68),
  );
  static const sport = MapPinCategory(
    MapPinCategoryKind.sport,
    Icons.sports_soccer_outlined,
    Color(0xFF448DD0),
    Color(0xFF2A6195),
  );
  static const shopping = MapPinCategory(
    MapPinCategoryKind.shopping,
    Icons.shopping_bag_outlined,
    Color(0xFFD7679E),
    Color(0xFF9B396B),
  );
  static const other = MapPinCategory(
    MapPinCategoryKind.other,
    Icons.place_outlined,
    Color(0xFF73808A),
    Color(0xFF4F5A63),
  );

  static MapPinCategory forCode(String value) =>
      switch (value.trim().toLowerCase()) {
        'restaurant' ||
        'food' ||
        'food_drink' ||
        'fast_food' ||
        'snack_shop' ||
        'bakery' => dining,
        'cafe' || 'coffee_shop' || 'tea_house' || 'tea' => cafe,
        'bar' ||
        'pub' ||
        'nightclub' ||
        'brewery' ||
        'wine_tasting_room' => nightlife,
        'museum' ||
        'culture_scenery' ||
        'culture' ||
        'art_gallery' ||
        'gallery' ||
        'creative_park' ||
        'attraction' ||
        'historic_site' ||
        'landmark' ||
        'library' => culture,
        'park' ||
        'forest' ||
        'nature' ||
        'nature_outdoor' ||
        'nature_reserve' ||
        'beach' ||
        'island' ||
        'mountain' ||
        'viewpoint' => nature,
        'sport' ||
        'sports' ||
        'sports_center' ||
        'fitness' ||
        'gym' ||
        'stadium' ||
        'hiking_trail' ||
        'outdoor_sports' ||
        'badminton' => sport,
        'shop' ||
        'shopping' ||
        'shopping_mall' ||
        'market' ||
        'gift_shop' ||
        'liquor_store' ||
        'bookstore' => shopping,
        _ => other,
      };
}
