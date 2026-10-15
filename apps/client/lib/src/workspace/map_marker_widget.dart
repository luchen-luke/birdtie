import 'package:flutter/material.dart';

import 'map_marker_visual.dart';

/// A compact map point, independent of entity decoding, routing and media fetches.
/// Only the existing live pixel guard can make a photo visible.
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
  });

  final String id, title, subtitle;
  final String categoryCode;
  final bool moment, selected, dense, showLabel;
  final VoidCallback onTap;
  final MapMediaPixels? mediaPixels;
  final MapRecentMoments? recentMoments;

  @override
  Widget build(BuildContext context) {
    final pale = dense && !selected;
    final size = MapMarkerVisual.diameter(selected: selected, dense: pale);
    final pixels = !pale && mediaPixels?.current() == true ? mediaPixels : null;
    final recent = !moment
        ? recentMoments?.labelAt(DateTime.now().toUtc())
        : null;
    final color = moment
        ? MapMarkerVisual.momentColor
        : pale
        ? MapMarkerVisual.densePlaceColor
        : MapMarkerVisual.placeColor;
    final radius = moment ? MapMarkerVisual.radius(dense: pale) : size / 2;
    return Semantics(
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
        width: MapMarkerVisual.hitSize,
        height: MapMarkerVisual.hitSize,
        child: Stack(
          clipBehavior: Clip.none,
          children: [
            IconButton(
              key: ValueKey('individual-map-target:$id'),
              tooltip: title.isEmpty && moment ? '查看动态' : title,
              constraints: const BoxConstraints.tightFor(
                width: MapMarkerVisual.hitSize,
                height: MapMarkerVisual.hitSize,
              ),
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
                    color: selected ? MapMarkerVisual.placeColor : Colors.white,
                    width: selected
                        ? 2
                        : pale
                        ? .75
                        : 1.5,
                  ),
                ),
                child: pixels != null
                    ? Padding(
                        padding: EdgeInsets.all(selected ? 1 : 0),
                        child: ClipRRect(
                          borderRadius: BorderRadius.circular(radius),
                          child: ColoredBox(
                            color: Colors.white,
                            child: Padding(
                              padding: EdgeInsets.all(selected ? 1 : 0),
                              child: ClipRRect(
                                borderRadius: BorderRadius.circular(radius),
                                child: RawImage(
                                  key: ValueKey(
                                    '${moment ? 'map-moment-photo' : 'map-place-photo'}:$id',
                                  ),
                                  image: pixels.image,
                                  fit: BoxFit.cover,
                                ),
                              ),
                            ),
                          ),
                        ),
                      )
                    : pale
                    ? null
                    : Icon(
                        moment
                            ? Icons.notes_outlined
                            : MapMarkerVisual.placeIcon(categoryCode),
                        color: Colors.white,
                        size: selected
                            ? 18
                            : pale
                            ? 10
                            : 14,
                      ),
              ),
            ),
            if (showLabel && !pale && (title.isNotEmpty || recent != null))
              Positioned(
                key: ValueKey('individual-map-caption:$id'),
                left:
                    MapMarkerVisual.hitSize / 2 +
                    size / 2 +
                    MapMarkerVisual.labelGap,
                top: 0,
                bottom: 0,
                width: MapMarkerVisual.labelWidth,
                child: IgnorePointer(
                  child: Align(
                    alignment: Alignment.centerLeft,
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (title.isNotEmpty)
                          _MapCaption(
                            text: title,
                            maxLines: selected ? 2 : 1,
                            weight: FontWeight.w500,
                            color: MapMarkerVisual.labelColor,
                          ),
                        if (recent != null)
                          _MapCaption(
                            key: ValueKey('map-place-recent:$id'),
                            text: recent,
                            color: MapMarkerVisual.countColor,
                          ),
                      ],
                    ),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _MapCaption extends StatelessWidget {
  const _MapCaption({
    super.key,
    required this.text,
    required this.color,
    this.maxLines = 1,
    this.weight = FontWeight.w400,
  });
  final String text;
  final Color color;
  final int maxLines;
  final FontWeight weight;

  @override
  Widget build(BuildContext context) {
    final style = TextStyle(
      fontSize: MapMarkerVisual.labelFontSize,
      height: 1.2,
      fontWeight: weight,
    );
    return Stack(
      children: [
        ExcludeSemantics(
          child: Text(
            text,
            maxLines: maxLines,
            overflow: TextOverflow.ellipsis,
            style: style.copyWith(
              foreground: Paint()
                ..color = Colors.white
                ..style = PaintingStyle.stroke
                ..strokeWidth = 3,
            ),
          ),
        ),
        Text(
          text,
          maxLines: maxLines,
          overflow: TextOverflow.ellipsis,
          style: style.copyWith(color: color),
        ),
      ],
    );
  }
}
