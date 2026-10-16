import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'now_ui_tokens.dart';

/// Shared geometry; decoration never changes a geographic anchor or hit target.
class MapMarkerVisual {
  static const hitSize = 48.0, labelWidth = 112.0, labelFontSize = 12.0;
  static const labelGap = 3.0;
  static const labelColor = NowUiTokens.lightSurfaceTitle;
  static const countColor = NowUiTokens.lightSurfaceBody;
  static const labelBudget = 6;
  static double diameter({required bool selected, required bool dense}) =>
      selected
      ? 36
      : dense
      ? 6
      : 28;
  static double radius({required bool dense}) => dense ? 2 : 4;
  static const placeColor = Color(0xFF193B32);
  static const densePlaceColor = Color(0xFF678779);
  static const momentColor = Color(0xFF87683C);

  static IconData placeIcon(String categoryCode) => switch (categoryCode
      .trim()
      .toLowerCase()) {
    'cafe' || 'coffee_shop' || 'tea_house' => Icons.local_cafe_outlined,
    'restaurant' ||
    'food' ||
    'food_drink' ||
    'fast_food' ||
    'snack_shop' => Icons.restaurant_outlined,
    'bar' ||
    'pub' ||
    'nightclub' ||
    'wine_tasting_room' => Icons.local_bar_outlined,
    'distillery' ||
    'brewery' ||
    'winery' ||
    'specialty_production' => Icons.factory_outlined,
    'museum' || 'culture_scenery' || 'culture' => Icons.museum_outlined,
    'art_gallery' || 'creative_park' => Icons.palette_outlined,
    'park' ||
    'forest' ||
    'nature' ||
    'nature_outdoor' ||
    'nature_reserve' => Icons.park_outlined,
    'hotel' || 'hostel' || 'lodging' || 'guest_house' => Icons.hotel_outlined,
    'shop' ||
    'shopping' ||
    'shopping_mall' ||
    'gift_shop' ||
    'liquor_store' => Icons.storefront_outlined,
    'bookstore' || 'library' => Icons.menu_book_outlined,
    'transport' || 'transport_hub' => Icons.directions_transit_outlined,
    'airport' => Icons.local_airport_outlined,
    'beach' || 'island' => Icons.beach_access_outlined,
    'mountain' || 'hiking_trail' || 'viewpoint' => Icons.landscape_outlined,
    _ => Icons.place_outlined,
  };

  static String nativeLabel(
    String title, {
    required bool selected,
    String? recent,
  }) {
    final letters = title.characters.toList();
    bool fits(String value) {
      final painter = TextPainter(
        text: TextSpan(
          text: value,
          style: const TextStyle(
            fontSize: labelFontSize,
            fontWeight: FontWeight.w500,
          ),
        ),
        textDirection: TextDirection.ltr,
        maxLines: selected ? 2 : 1,
      )..layout(maxWidth: labelWidth);
      final okay = !painter.didExceedMaxLines;
      painter.dispose();
      return okay;
    }

    var label = title;
    if (!fits(title)) {
      var low = 0, high = letters.length;
      while (low < high) {
        final mid = (low + high + 1) ~/ 2;
        if (fits('${letters.take(mid).join()}…')) {
          low = mid;
        } else {
          high = mid - 1;
        }
      }
      label = '${letters.take(low).join()}…';
    }
    return recent == null ? label : '$label\n$recent';
  }

  /// Mapbox offsets are expressed in text ems; the geographic anchor stays put.
  static List<double> nativeLabelOffset({
    required bool selected,
    required bool dense,
  }) => [
    (diameter(selected: selected, dense: dense) / 2 + labelGap) / labelFontSize,
    0,
  ];
}

/// A complete, authorized count has its own deadline. Unknown is never zero.
class MapRecentMoments {
  const MapRecentMoments({
    required this.count,
    required this.checkedAt,
    required this.windowStart,
    required this.validUntil,
  }) : visibleTotal = false,
       sourceVersion = '';
  const MapRecentMoments.visible({
    required this.count,
    required this.checkedAt,
    required this.validUntil,
    required this.sourceVersion,
  }) : windowStart = checkedAt,
       visibleTotal = true;
  final int count;
  final bool visibleTotal;
  final String sourceVersion;
  final DateTime checkedAt, windowStart, validUntil;
  bool currentAt(DateTime now) => now.isBefore(validUntil);
  String? labelAt(DateTime now) => currentAt(now) && count > 0
      ? (visibleTotal ? '$count条动态' : '近30天 · $count条动态')
      : null;
  String get sourceKey =>
      '${visibleTotal ? 'visible' : '30d'}:$sourceVersion:$count:${checkedAt.microsecondsSinceEpoch}:${validUntil.microsecondsSinceEpoch}';
}

/// RAM-only verified pixels. Providers receive no token, URL or private proof.
/// The live guard also fences retained widget/native work after cache eviction.
class MapMediaPixels {
  const MapMediaPixels({
    required this.key,
    required this.image,
    required this.normalPNG,
    required this.selectedPNG,
    required this.devicePixelRatio,
    required this.current,
    this.validUntil,
  });
  final String key;
  final ui.Image image;
  final Uint8List normalPNG, selectedPNG;
  final double devicePixelRatio;
  final bool Function() current;

  /// Original authority deadline for providers that retain a pixel copy outside
  /// Dart. A copy cannot invent or extend this lease; the live guard still applies.
  final DateTime? validUntil;
  Uint8List? png({
    required bool selected,
    required bool dense,
    double? ratio,
  }) => current() && !dense && (ratio == null || ratio == devicePixelRatio)
      ? (selected ? selectedPNG : normalPNG)
      : null;
}
