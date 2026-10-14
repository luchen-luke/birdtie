import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';

import 'map_entities.dart';
import 'map_marker_visual.dart';

typedef _MarkerKey = (
  MapEntityKind,
  bool,
  int,
  bool,
  bool,
  double,
  double,
  double,
);

class MapMarkerBitmap {
  static final Map<_MarkerKey, Uint8List> _cache = {};

  // Bounded across source revisions and DPRs. Every glyph stays at its anchor.
  static const cacheLimit = 128;
  static const pointCanvasSize = 96.0;
  static double pixelRatio(double value) =>
      value.isFinite && value > 0 ? value.clamp(.5, 8.0) : 1.0;

  static void _remember(_MarkerKey key, Uint8List bytes) {
    _cache.remove(key);
    _cache[key] = bytes;
    while (_cache.length > cacheLimit ||
        _cache.values.fold<int>(0, (n, b) => n + b.length) > 2 * 1024 * 1024) {
      _cache.remove(_cache.keys.first);
    }
  }

  /// The glyph is centered on its true coordinate, without layout connectors.
  /// Native SDKs divide PNG pixels by the map/screen pixel ratio, so rasterize
  /// at that ratio rather than shrinking the painted glyph on high-DPI phones.
  /// Native SDK touch bounds/assistive navigation require device verification.
  static Future<Uint8List> _individualPoint(
    MapEntityKind kind,
    bool selected,
    bool dense,
    double ratio,
    ui.Image? photo,
  ) async {
    const anchor = Offset(pointCanvasSize / 2, pointCanvasSize / 2);
    const center = anchor;
    final pale = dense && !selected;
    final diameter = MapMarkerVisual.diameter(selected: selected, dense: pale);
    final moment = kind == MapEntityKind.moment;
    final color = moment ? const Color(0xFF87683C) : const Color(0xFF193B32);
    final recorder = ui.PictureRecorder(), canvas = Canvas(recorder);
    canvas.scale(ratio);
    final rect = Rect.fromCenter(
      center: center,
      width: diameter,
      height: diameter,
    );
    final shape = Path();
    if (moment) {
      shape.addRRect(
        RRect.fromRectAndRadius(
          rect,
          Radius.circular(MapMarkerVisual.radius(dense: pale)),
        ),
      );
    } else {
      shape.addOval(rect);
    }
    canvas.drawPath(shape, Paint()..color = color);
    if (!pale && photo != null) {
      canvas.save();
      canvas.clipPath(shape);
      paintImage(canvas: canvas, rect: rect, image: photo, fit: BoxFit.cover);
      canvas.restore();
    }
    {
      // The shared 28/36/16 diameter is the outside edge, as in the Flutter
      // marker. Clip the centered stroke inward so native pixels do not grow.
      canvas.save();
      canvas.clipPath(shape);
      canvas.drawPath(
        shape,
        Paint()
          ..color = selected ? MapMarkerVisual.placeColor : Colors.white
          ..style = PaintingStyle.stroke
          ..strokeWidth = selected ? 4 : 3,
      );
      final glyph = Paint()
        ..color = Colors.white
        ..style = PaintingStyle.stroke
        ..strokeWidth = selected ? 1.8 : 1.4;
      if (moment && (photo == null || pale)) {
        final halfWidth = selected
            ? 7.0
            : pale
            ? 3.0
            : 5.0;
        final gap = pale ? 3.0 : 4.0;
        for (final y in [-gap, 0.0, gap]) {
          // Solid 2 dp lines retain an opaque white core at DPR 1. A thin
          // stroke through an integer pixel center otherwise becomes a blur.
          canvas.drawRect(
            Rect.fromCenter(
              center: center + Offset(0, y),
              width: halfWidth * 2,
              height: 2,
            ),
            Paint()..color = Colors.white,
          );
        }
      } else if (photo == null || pale) {
        canvas.drawCircle(
          center,
          selected
              ? 5
              : pale
              ? 3
              : 4,
          glyph,
        );
      }
      canvas.restore();
    }
    final picture = recorder.endRecording();
    final extent = (pointCanvasSize * ratio).ceil();
    final image = await picture.toImage(extent, extent);
    picture.dispose();
    try {
      return (await image.toByteData(
        format: ui.ImageByteFormat.png,
      ))!.buffer.asUint8List();
    } finally {
      image.dispose();
    }
  }

  static Future<Uint8List> render({
    required MapEntityKind kind,
    required bool selected,
    int count = 0,
    bool approximate = false,
    bool dense = false,
    double devicePixelRatio = 1,
    Offset visualOffset = Offset.zero,
    ui.Image? photo,
  }) async {
    final individual =
        !approximate &&
        (kind == MapEntityKind.place || kind == MapEntityKind.moment);
    // Other legacy glyphs keep their existing presentation in this repair.
    final ratio = individual ? pixelRatio(devicePixelRatio) : 1.0;
    // Retain the optional argument for compatibility with older callers, but
    // never move a point or restore the retired ring/leader presentation.
    const offset = Offset.zero;
    final key = (
      kind,
      selected,
      count,
      approximate,
      dense,
      ratio,
      offset.dx,
      offset.dy,
    );
    final cached = photo == null ? _cache.remove(key) : null;
    if (cached != null) {
      _cache[key] = cached;
      return cached;
    }

    if (individual) {
      final bytes = await _individualPoint(kind, selected, dense, ratio, photo);
      if (photo == null) _remember(key, bytes);
      return bytes;
    }
    const width = 72.0;
    const height = 84.0;
    final recorder = ui.PictureRecorder();
    final canvas = Canvas(recorder);
    final center = const Offset(width / 2, 35);
    final color = selected
        ? const Color(0xFF193B32)
        : switch (kind) {
            MapEntityKind.person ||
            MapEntityKind.peopleCluster => const Color(0xFF4A7869),
            MapEntityKind.activity => const Color(0xFFB96743),
            MapEntityKind.group ||
            MapEntityKind.community => const Color(0xFF455C8B),
            MapEntityKind.organization => const Color(0xFF455C8B),
            MapEntityKind.place => const Color(0xFF193B32),
            MapEntityKind.cluster => const Color(0xFF315E50),
            MapEntityKind.moment => const Color(0xFF87683C),
            MapEntityKind.business => const Color(0xFF705B8B),
            MapEntityKind.opportunity => const Color(0xFF356E7C),
          };

    final shadow = Paint()..color = const Color(0x33000000);
    canvas.drawOval(
      Rect.fromCenter(
        center: const Offset(width / 2, 76),
        width: 30,
        height: 7,
      ),
      shadow,
    );
    final pin = Path()
      ..moveTo(width / 2, 73)
      ..cubicTo(63, 57, 62, 49, 62, 36)
      ..arcToPoint(
        const Offset(10, 36),
        radius: const Radius.circular(26),
        clockwise: false,
      )
      ..cubicTo(10, 49, 9, 57, width / 2, 73)
      ..close();
    final graphic =
        kind == MapEntityKind.place ||
        kind == MapEntityKind.moment ||
        kind == MapEntityKind.business;
    final outline = graphic
        ? (Path()
            ..addRRect(
              RRect.fromRectAndRadius(
                const Rect.fromLTWH(7, 7, 58, 58),
                const Radius.circular(12),
              ),
            )
            ..moveTo(29, 65)
            ..lineTo(36, 73)
            ..lineTo(43, 65))
        : pin;
    if (!approximate) canvas.drawPath(outline, Paint()..color = Colors.white);
    canvas.drawPath(
      approximate
          ? (Path()..addOval(Rect.fromCircle(center: center, radius: 27)))
          : outline,
      Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = selected ? 6 : 4,
    );
    final fillPaint = Paint()
      ..color = approximate ? color.withValues(alpha: 0.68) : color;
    if (graphic && !approximate) {
      // A category graphic is the truthful fallback when no public image exists.
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          const Rect.fromLTWH(12, 12, 48, 48),
          const Radius.circular(8),
        ),
        fillPaint,
      );
    } else {
      canvas.drawCircle(center, 21, fillPaint);
    }

    if ((kind == MapEntityKind.cluster ||
            kind == MapEntityKind.peopleCluster) &&
        count > 0) {
      final text = count.toString();
      final builder =
          ui.ParagraphBuilder(ui.ParagraphStyle(textAlign: TextAlign.center))
            ..pushStyle(
              ui.TextStyle(
                color: Colors.white,
                fontSize: 16,
                fontWeight: FontWeight.w500,
                fontFamily: 'Roboto',
              ),
            );
      builder.addText(text);
      final paragraph = builder.build()
        ..layout(const ui.ParagraphConstraints(width: 42));
      canvas.drawParagraph(paragraph, const Offset(15, 26));
    } else {
      final white = Paint()
        ..color = Colors.white
        ..strokeWidth = 2.6
        ..strokeCap = StrokeCap.round
        ..strokeJoin = StrokeJoin.round
        ..style = PaintingStyle.stroke;
      final fill = Paint()..color = Colors.white;
      switch (kind) {
        case MapEntityKind.activity:
          canvas.drawCircle(const Offset(36, 29), 4, fill);
          final shuttle = Path()
            ..moveTo(31, 35)
            ..lineTo(41, 35)
            ..lineTo(38, 43)
            ..lineTo(34, 43)
            ..close();
          canvas.drawPath(shuttle, white);
          canvas.drawLine(const Offset(34, 37), const Offset(38, 42), white);
          canvas.drawLine(const Offset(38, 37), const Offset(34, 42), white);
        case MapEntityKind.person:
          canvas.drawCircle(const Offset(36, 28), 5, fill);
          canvas.drawArc(
            const Rect.fromLTWH(27, 34, 18, 13),
            3.14,
            3.14,
            false,
            white,
          );
        case MapEntityKind.peopleCluster:
          canvas.drawCircle(const Offset(29, 35), 2, fill);
          canvas.drawCircle(const Offset(36, 35), 2, fill);
          canvas.drawCircle(const Offset(43, 35), 2, fill);
        case MapEntityKind.group ||
            MapEntityKind.community ||
            MapEntityKind.organization:
          canvas.drawCircle(const Offset(32, 29), 4, fill);
          canvas.drawCircle(const Offset(40, 29), 4, fill);
          canvas.drawArc(
            const Rect.fromLTWH(25, 34, 15, 12),
            3.14,
            3.14,
            false,
            white,
          );
          canvas.drawArc(
            const Rect.fromLTWH(32, 34, 15, 12),
            3.14,
            3.14,
            false,
            white,
          );
        case MapEntityKind.place:
          canvas.drawCircle(const Offset(36, 35), 7, white);
          canvas.drawCircle(const Offset(36, 35), 3, Paint()..color = color);
        case MapEntityKind.cluster:
          canvas.drawCircle(const Offset(29, 35), 2, fill);
          canvas.drawCircle(const Offset(36, 35), 2, fill);
          canvas.drawCircle(const Offset(43, 35), 2, fill);
        case MapEntityKind.moment:
          canvas.drawRect(const Rect.fromLTWH(28, 26, 16, 19), white);
          canvas.drawLine(const Offset(31, 32), const Offset(41, 32), white);
          canvas.drawLine(const Offset(31, 37), const Offset(40, 37), white);
        case MapEntityKind.business:
          canvas.drawRect(const Rect.fromLTWH(27, 32, 18, 13), white);
          canvas.drawLine(const Offset(25, 30), const Offset(47, 30), white);
          canvas.drawLine(const Offset(29, 26), const Offset(43, 26), white);
        case MapEntityKind.opportunity:
          canvas.drawCircle(const Offset(36, 32), 7, white);
          canvas.drawLine(const Offset(32, 41), const Offset(40, 41), white);
          canvas.drawLine(const Offset(34, 45), const Offset(38, 45), white);
      }
    }

    final image = await recorder.endRecording().toImage(
      width.toInt(),
      height.toInt(),
    );
    final data = await image.toByteData(format: ui.ImageByteFormat.png);
    image.dispose();
    final bytes = data!.buffer.asUint8List();
    _remember(key, bytes);
    return bytes;
  }
}
