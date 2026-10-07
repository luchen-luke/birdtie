import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';

import 'map_entities.dart';

class MapMarkerBitmap {
  static final Map<String, Uint8List> _cache = {};

  static Future<Uint8List> render({
    required MapEntityKind kind,
    required bool selected,
    int count = 0,
  }) async {
    final key = '$kind:$selected:$count';
    final cached = _cache[key];
    if (cached != null) return cached;

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
    canvas.drawPath(pin, Paint()..color = Colors.white);
    canvas.drawPath(
      pin,
      Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = selected ? 6 : 4,
    );
    canvas.drawCircle(center, 21, Paint()..color = color);

    if (kind == MapEntityKind.cluster || kind == MapEntityKind.peopleCluster) {
      final text = (count > 0 ? count : 2).toString();
      final builder =
          ui.ParagraphBuilder(ui.ParagraphStyle(textAlign: TextAlign.center))
            ..pushStyle(
              ui.TextStyle(
                color: Colors.white,
                fontSize: 22,
                fontWeight: FontWeight.w700,
                fontFamily: 'Roboto',
              ),
            );
      builder.addText(text);
      final paragraph = builder.build()
        ..layout(const ui.ParagraphConstraints(width: 42));
      canvas.drawParagraph(paragraph, const Offset(15, 23));
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
          break;
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
          break;
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
    _cache[key] = bytes;
    return bytes;
  }
}
