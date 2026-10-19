import 'package:flutter/material.dart';

import 'map_marker_visual.dart';
import 'map_pin_category.dart';

/// A 48 dp action target, independent of its compact geographic glyph.
class CompactMapPointMarker extends StatelessWidget {
  const CompactMapPointMarker({
    super.key,
    required this.id,
    required this.title,
    required this.subtitle,
    required this.moment,
    required this.selected,
    required this.onTap,
    this.mediaPixels,
    this.recentMoments,
    this.dense = false,
    this.showLabel = false,
    this.categoryCode = '',
    this.placement,
  });
  final String id, title, subtitle, categoryCode;
  final bool moment, selected, dense, showLabel;
  final VoidCallback onTap;
  final MapMediaPixels? mediaPixels;
  final MapRecentMoments? recentMoments;
  final MapLabelPlacement? placement;

  @override
  Widget build(BuildContext context) {
    final size = MapMarkerVisual.diameter(
      selected: selected,
      dense: dense,
      moment: moment,
    );
    final micro = !moment && dense && !selected;
    final pixels =
        moment &&
            mediaPixels?.current() == true &&
            (mediaPixels!.validUntil == null ||
                DateTime.now().toUtc().isBefore(mediaPixels!.validUntil!))
        ? mediaPixels
        : null;
    final recent = !moment
        ? recentMoments?.labelAt(DateTime.now().toUtc())
        : null;
    final category = MapPinCategory.forCode(categoryCode);
    final color = moment ? MapMarkerVisual.momentColor : category.fill;
    final radius = moment ? MapMarkerVisual.momentRadius : size / 2;
    final fallbackAux = !moment && (selected || !dense) ? recent : null;
    final font = selected ? 13.0 : 12.0;
    final fallbackHeight = font * 1.25 + (fallbackAux == null ? 0 : 15);
    final caption =
        placement ??
        MapLabelPlacement(
          side: MapLabelSide.right,
          rect: Rect.fromLTWH(0, 0, 112, fallbackHeight),
          offset: Offset(size / 2 + 4, -fallbackHeight / 2),
          name: MapMarkerVisual.nativeLabel(title, selected: selected),
          fontSize: font,
          color: category.label,
          auxiliary: fallbackAux,
        );
    return Semantics(
      identifier: 'birdtie-map-point:$id',
      button: true,
      selected: selected,
      label: [
        '${moment ? '动态' : '地点'} · $title',
        if (subtitle.isNotEmpty) subtitle,
        if (recent != null) recent,
      ].join(' · '),
      excludeSemantics: true,
      onTap: onTap,
      child: SizedBox(
        width: 48,
        height: 48,
        child: Stack(
          clipBehavior: Clip.none,
          children: [
            IconButton(
              key: ValueKey('individual-map-target:$id'),
              tooltip: title.isEmpty && moment ? '查看动态' : title,
              constraints: const BoxConstraints.tightFor(width: 48, height: 48),
              padding: EdgeInsets.zero,
              onPressed: onTap,
              icon: Container(
                key: ValueKey('individual-map-shape:$id'),
                width: size,
                height: size,
                decoration: BoxDecoration(
                  shape: moment ? BoxShape.rectangle : BoxShape.circle,
                  borderRadius: moment ? BorderRadius.circular(radius) : null,
                  color: color,
                  border: Border.all(
                    color: Colors.white,
                    width: micro ? 1 : 1.5,
                  ),
                ),
                clipBehavior: Clip.antiAlias,
                child: pixels != null
                    ? RawImage(
                        key: ValueKey('map-moment-photo:$id'),
                        image: pixels.image,
                        fit: BoxFit.cover,
                      )
                    : micro
                    ? null
                    : Icon(
                        moment ? Icons.notes_outlined : category.icon,
                        color: Colors.white,
                        size: !moment && dense && !selected ? 12 : 14,
                      ),
              ),
            ),
            if (!micro &&
                showLabel &&
                caption.name.isNotEmpty &&
                (!moment || selected || placement != null))
              Positioned(
                key: ValueKey('individual-map-caption:$id'),
                left: 24 + caption.offset.dx,
                top: 24 + caption.offset.dy,
                width: caption.rect.width,
                height: caption.rect.height,
                child: IgnorePointer(
                  child: MapPinLabel(placement: caption, id: id),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// Measured name/auxiliary hierarchy shared by Flutter and native overlays.
class MapPinLabel extends StatelessWidget {
  const MapPinLabel({super.key, required this.placement, this.id = ''});
  final MapLabelPlacement placement;
  final String id;
  @override
  Widget build(BuildContext context) => Column(
    mainAxisSize: MainAxisSize.min,
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      _MapCaption(
        text: placement.name,
        color: placement.color,
        fontSize: placement.fontSize,
        weight: FontWeight.w500,
      ),
      if (placement.auxiliary case final auxiliary?) ...[
        const SizedBox(height: 1),
        _MapCaption(
          key: ValueKey('map-place-recent:$id'),
          text: auxiliary,
          color: MapMarkerVisual.countColor,
          fontSize: 11,
        ),
      ],
    ],
  );
}

class _MapCaption extends StatelessWidget {
  const _MapCaption({
    super.key,
    required this.text,
    required this.color,
    required this.fontSize,
    this.weight = FontWeight.w400,
  });
  final String text;
  final Color color;
  final double fontSize;
  final FontWeight weight;
  @override
  Widget build(BuildContext context) {
    final style = TextStyle(
      fontSize: fontSize,
      height: 1.25,
      fontWeight: weight,
    );
    return Stack(
      children: [
        ExcludeSemantics(
          child: Text(
            text,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: style.copyWith(
              foreground: Paint()
                ..color = Colors.white
                ..style = PaintingStyle.stroke
                ..strokeWidth = 2.5,
            ),
          ),
        ),
        Text(
          text,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: style.copyWith(color: color),
        ),
      ],
    );
  }
}
