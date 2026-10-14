import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';

/// Shared geometry; decoration never changes a geographic anchor or hit target.
class MapMarkerVisual {
  static const hitSize = 48.0, labelWidth = 144.0, labelFontSize = 12.0;
  static const labelBudget = 6;
  static double diameter({required bool selected, required bool dense}) =>
      selected
      ? 36
      : dense
      ? 16
      : 28;
  static double radius({required bool dense}) => dense ? 2 : 4;
  static const placeColor = Color(0xFF193B32);
  static const momentColor = Color(0xFF87683C);

  static String nativeLabel(
    String title, {
    required bool selected,
    String? recent,
  }) {
    final runes = title.runes.toList();
    bool fits(String value) {
      final painter = TextPainter(
        text: TextSpan(text: value, style: const TextStyle(fontSize: 14)),
        textDirection: TextDirection.ltr,
        maxLines: selected ? 2 : 1,
      )..layout(maxWidth: labelWidth);
      final okay = !painter.didExceedMaxLines;
      painter.dispose();
      return okay;
    }

    var label = title;
    if (!fits(title)) {
      var low = 0, high = runes.length;
      while (low < high) {
        final mid = (low + high + 1) ~/ 2;
        if (fits('${String.fromCharCodes(runes.take(mid))}…')) {
          low = mid;
        } else {
          high = mid - 1;
        }
      }
      label = '${String.fromCharCodes(runes.take(low))}…';
    }
    return recent == null ? label : '$label\n$recent';
  }
}

/// A complete, authorized count has its own deadline. Unknown is never zero.
class MapRecentMoments {
  const MapRecentMoments({
    required this.count,
    required this.checkedAt,
    required this.windowStart,
    required this.validUntil,
  });
  final int count;
  final DateTime checkedAt, windowStart, validUntil;
  bool currentAt(DateTime now) => now.isBefore(validUntil);
  String? labelAt(DateTime now) =>
      currentAt(now) && count > 0 ? '近30天 · $count条动态' : null;
  String get sourceKey =>
      '$count:${checkedAt.microsecondsSinceEpoch}:${validUntil.microsecondsSinceEpoch}';
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
  });
  final String key;
  final ui.Image image;
  final Uint8List normalPNG, selectedPNG;
  final double devicePixelRatio;
  final bool Function() current;
  Uint8List? png({
    required bool selected,
    required bool dense,
    double? ratio,
  }) => current() && !dense && (ratio == null || ratio == devicePixelRatio)
      ? (selected ? selectedPNG : normalPNG)
      : null;
}
