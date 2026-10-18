import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'now_ui_tokens.dart';
import 'map_pin_category.dart';

enum MapLabelSide { right, left, top, bottom }

/// The same measured logical rectangle is consumed by every provider.
class MapLabelPlacement {
  const MapLabelPlacement({
    required this.side,
    required this.rect,
    required this.name,
    required this.fontSize,
    required this.color,
    this.auxiliary,
    this.offset = Offset.zero,
  });
  final MapLabelSide side;
  final Rect rect;
  final String name;
  final String? auxiliary;
  final double fontSize;
  final Color color;
  final Offset offset;
  @override
  bool operator ==(Object other) =>
      other is MapLabelPlacement &&
      other.side == side &&
      other.rect == rect &&
      other.name == name &&
      other.auxiliary == auxiliary &&
      other.fontSize == fontSize &&
      other.color == color &&
      other.offset == offset;
  @override
  int get hashCode =>
      Object.hash(side, rect, name, auxiliary, fontSize, color, offset);
  double get nameHeight => fontSize * 1.25;
  Offset get auxiliaryOrigin => rect.topLeft + Offset(0, nameHeight + 1);
}

/// Shared geometry; decoration never changes a geographic anchor or hit target.
class MapMarkerVisual {
  static const hitSize = 48.0, labelWidth = 112.0, labelFontSize = 12.0;
  static const labelGap = 4.0;
  static const labelColor = NowUiTokens.lightSurfaceTitle;
  static const countColor = Color(0xFF69717A);
  static const labelBudget = 6;
  static double diameter({
    required bool selected,
    required bool dense,
    bool moment = false,
  }) => selected
      ? (moment ? 34 : 28)
      : dense
      ? (moment ? 8 : 7)
      : (moment ? 30 : 24);
  static double radius({required bool dense}) => dense ? 1 : 6;
  static const placeColor = Color(0xFF193B32);
  static const densePlaceColor = Color(0xFF678779);
  static const momentColor = Color(0xFF87683C);

  static IconData placeIcon(String categoryCode) =>
      MapPinCategory.forCode(categoryCode).icon;
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
          style: TextStyle(
            fontSize: selected ? 13 : labelFontSize,
            fontWeight: FontWeight.w500,
          ),
        ),
        textDirection: TextDirection.ltr,
        maxLines: 1,
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

  static int clusterCount({
    required String categoryCode,
    required int contentCount,
    required int anchorCount,
    int fallback = 0,
  }) => categoryCode == 'moment_cluster'
      ? contentCount
      : anchorCount > 0
      ? anchorCount
      : fallback;
  static String clusterUnit(String categoryCode) => switch (categoryCode) {
    'place_cluster' => '个地点',
    'moment_cluster' => '条动态',
    _ => '项',
  };
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
