import 'package:flutter/material.dart';

/// Map-attached chrome uses cheap color gradients, never a blurred map copy.
/// High contrast and reduced animation use an opaque readable edge instead.
class NowMapEdge extends StatelessWidget {
  const NowMapEdge({
    super.key,
    required this.child,
    this.bottom = false,
    this.opaqueTailInset = 0,
  });
  final Widget child;
  final bool bottom;

  /// A paint-only fade tail may disappear in opaque accessibility mode.
  /// The child and its measured toolbar geometry remain untouched.
  final double opaqueTailInset;

  @override
  Widget build(BuildContext context) {
    final media = MediaQuery.of(context);
    final surface = Theme.of(context).colorScheme.surface;
    final opaque =
        media.highContrast ||
        media.disableAnimations ||
        media.accessibleNavigation;
    final decoration = BoxDecoration(
      color: opaque ? surface : null,
      gradient: opaque
          ? null
          : LinearGradient(
              begin: bottom ? Alignment.bottomCenter : Alignment.topCenter,
              end: bottom ? Alignment.topCenter : Alignment.bottomCenter,
              colors: [
                surface,
                surface.withValues(alpha: .97),
                surface.withValues(alpha: 0),
              ],
              stops: const [0, .72, 1],
            ),
    );
    if (opaqueTailInset > 0) {
      // Keep the toolbar mounted under the same parent when accessibility
      // flags change; only its background paint and tail inset may change.
      return Stack(
        children: [
          Positioned.fill(
            bottom: opaque ? opaqueTailInset : 0,
            child: DecoratedBox(decoration: decoration),
          ),
          child,
        ],
      );
    }
    return DecoratedBox(decoration: decoration, child: child);
  }
}
