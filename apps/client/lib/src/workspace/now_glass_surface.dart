import 'package:flutter/material.dart';

import '../app/birdtie_surfaces.dart';
import 'now_ui_tokens.dart';

enum NowGlassRole { control, composer, navigation }

/// Local material with a single optional backdrop. Surface sheen is painted
/// behind sharp content; navigation never adds another backdrop sampler.
class NowGlassSurface extends StatelessWidget {
  const NowGlassSurface({
    super.key,
    required this.child,
    this.radius,
    this.backdropBlur = true,
    this.role = NowGlassRole.control,
  });
  final Widget child;
  final double? radius;
  final bool backdropBlur;
  final NowGlassRole role;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final tokens =
        theme.extension<BirdtieSurfaceTokens>() ??
        BirdtieSurfaceTokens.fromTheme(theme);
    final media = MediaQuery.maybeOf(context);
    final highContrast = media?.highContrast ?? false;
    final reduced =
        tokens.reduceEffects ||
        highContrast ||
        (media?.disableAnimations ?? false);
    final blur = backdropBlur && role != NowGlassRole.navigation;
    final opacity = switch (role) {
      NowGlassRole.control =>
        blur ? NowUiTokens.glassOpacity : NowUiTokens.clearControlOpacity,
      NowGlassRole.composer => NowUiTokens.composerGlassOpacity,
      NowGlassRole.navigation => NowUiTokens.navigationGlassOpacity,
    };
    final borderRadius = radius ?? NowUiTokens.socialCornerRadius;
    return Theme(
      data: theme.copyWith(
        extensions: [
          ...theme.extensions.values.where(
            (value) => value is! BirdtieSurfaceTokens,
          ),
          tokens.copyWith(
            composerRadius: borderRadius,
            floatingOpacity: opacity,
            blurSigma: blur ? NowUiTokens.glassBlurSigma : 0,
            border: NowUiTokens.materialEdge(theme, highContrast: highContrast),
            borderWidth: NowUiTokens.glassEdgeWidth,
            floatingElevation: reduced || role == NowGlassRole.navigation
                ? 0
                : NowUiTokens.glassElevation,
          ),
        ],
      ),
      child: BirdtieSurface(
        kind: BirdtieSurfaceKind.floating,
        child: DecoratedBox(
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(borderRadius),
            gradient: reduced ? null : NowUiTokens.materialSheen(theme),
          ),
          child: child,
        ),
      ),
    );
  }
}
