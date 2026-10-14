import 'dart:math' as math;
import 'package:flutter/material.dart';

import '../workspace/now_ui_tokens.dart';

/// A single direction control. Flutter owns the 48dp target; Mapbox owns bearing.
class NowMapCompass extends StatelessWidget {
  const NowMapCompass({
    super.key,
    required this.bearing,
    required this.onNorth,
  });
  final double bearing;
  final VoidCallback onNorth;

  String get _directionValue {
    if (!bearing.isFinite) return '方向未知';
    final direction = (bearing % 360).round() % 360;
    return direction == 0 ? '朝北' : '方向$direction度';
  }

  @override
  Widget build(BuildContext context) => SizedBox(
    key: const Key('now-map-compass'),
    width: 48,
    height: 48,
    child: Material(
      color: Colors.transparent,
      child: Stack(
        children: [
          Positioned.fill(
            child: Semantics(
              // One accessible control owns both the direction and north
              // action. The inner IconButton retains its visual tooltip.
              button: true,
              enabled: true,
              label: '地图朝北',
              value: _directionValue,
              onTap: onNorth,
              child: ExcludeSemantics(
                child: IconButton(
                  tooltip: '地图朝北',
                  onPressed: onNorth,
                  padding: EdgeInsets.zero,
                  constraints: const BoxConstraints(
                    minWidth: 48,
                    minHeight: 48,
                  ),
                  icon: Transform.rotate(
                    angle: bearing.isFinite ? -bearing * math.pi / 180 : 0,
                    child: Icon(
                      Icons.navigation_outlined,
                      size: 20,
                      color: NowUiTokens.ownerMapOverlay.secondary,
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    ),
  );
}
