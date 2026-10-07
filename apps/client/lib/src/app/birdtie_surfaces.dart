import 'dart:ui' show ImageFilter, lerpDouble;

import 'package:flutter/material.dart';

/// First composer/place samples only. Candidate values require device evidence
/// before adoption by other surfaces. The platform map is never blurred here.
@immutable
class BirdtieSurfaceTokens extends ThemeExtension<BirdtieSurfaceTokens> {
  const BirdtieSurfaceTokens({
    required this.background,
    required this.border,
    required this.shadow,
    this.floatingOpacity = .94,
    this.contentOpacity = .98,
    this.blurSigma = 4,
    this.floatingElevation = 3,
    this.borderWidth = 1,
    this.composerRadius = 30,
    this.contentRadius = 24,
    this.reduceEffects = const bool.fromEnvironment(
      'BIRDTIE_REDUCE_SURFACE_EFFECTS',
    ),
  });

  factory BirdtieSurfaceTokens.fromTheme(ThemeData theme) =>
      BirdtieSurfaceTokens(
        background: theme.scaffoldBackgroundColor,
        border: theme.colorScheme.outlineVariant,
        shadow: theme.colorScheme.shadow.withValues(alpha: .12),
      );

  final Color background, border, shadow;
  final double floatingOpacity, contentOpacity, blurSigma, floatingElevation;
  final double borderWidth, composerRadius, contentRadius;
  // Explicit build fallback for constrained devices. No hardware guessing.
  final bool reduceEffects;

  @override
  BirdtieSurfaceTokens copyWith({
    Color? background,
    Color? border,
    Color? shadow,
    double? floatingOpacity,
    double? contentOpacity,
    double? blurSigma,
    double? floatingElevation,
    double? borderWidth,
    double? composerRadius,
    double? contentRadius,
    bool? reduceEffects,
  }) => BirdtieSurfaceTokens(
    background: background ?? this.background,
    border: border ?? this.border,
    shadow: shadow ?? this.shadow,
    floatingOpacity: floatingOpacity ?? this.floatingOpacity,
    contentOpacity: contentOpacity ?? this.contentOpacity,
    blurSigma: blurSigma ?? this.blurSigma,
    floatingElevation: floatingElevation ?? this.floatingElevation,
    borderWidth: borderWidth ?? this.borderWidth,
    composerRadius: composerRadius ?? this.composerRadius,
    contentRadius: contentRadius ?? this.contentRadius,
    reduceEffects: reduceEffects ?? this.reduceEffects,
  );

  @override
  BirdtieSurfaceTokens lerp(BirdtieSurfaceTokens? other, double t) {
    if (other == null) return this;
    return BirdtieSurfaceTokens(
      background: Color.lerp(background, other.background, t)!,
      border: Color.lerp(border, other.border, t)!,
      shadow: Color.lerp(shadow, other.shadow, t)!,
      floatingOpacity: lerpDouble(floatingOpacity, other.floatingOpacity, t)!,
      contentOpacity: lerpDouble(contentOpacity, other.contentOpacity, t)!,
      blurSigma: lerpDouble(blurSigma, other.blurSigma, t)!,
      floatingElevation: lerpDouble(
        floatingElevation,
        other.floatingElevation,
        t,
      )!,
      borderWidth: lerpDouble(borderWidth, other.borderWidth, t)!,
      composerRadius: lerpDouble(composerRadius, other.composerRadius, t)!,
      contentRadius: lerpDouble(contentRadius, other.contentRadius, t)!,
      reduceEffects: t < .5 ? reduceEffects : other.reduceEffects,
    );
  }
}

ThemeData birdtieTheme(Brightness brightness) {
  final theme = ThemeData(
    useMaterial3: true,
    colorScheme: ColorScheme.fromSeed(
      seedColor: const Color(0xFF193B32),
      brightness: brightness,
    ),
    scaffoldBackgroundColor: brightness == Brightness.light
        ? const Color(0xFFFCFBF8)
        : const Color(0xFF171C19),
  );
  return theme.copyWith(extensions: [BirdtieSurfaceTokens.fromTheme(theme)]);
}

enum BirdtieSurfaceKind { floating, content, critical }

final birdtieFloatingButtonStyle = IconButton.styleFrom(
  minimumSize: const Size(48, 48),
  visualDensity: VisualDensity.standard,
  tapTargetSize: MaterialTapTargetSize.padded,
);

class BirdtieSurface extends StatelessWidget {
  const BirdtieSurface({super.key, required this.kind, required this.child});

  final BirdtieSurfaceKind kind;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final tokens =
        theme.extension<BirdtieSurfaceTokens>() ??
        BirdtieSurfaceTokens.fromTheme(theme);
    final media = MediaQuery.maybeOf(context);
    final opaque =
        tokens.reduceEffects ||
        (media?.highContrast ?? false) ||
        (media?.disableAnimations ?? false) ||
        kind == BirdtieSurfaceKind.critical;
    final floating = kind == BirdtieSurfaceKind.floating;
    final radius = BorderRadius.circular(
      floating ? tokens.composerRadius : tokens.contentRadius,
    );
    final material = Material(
      color: tokens.background.withValues(
        alpha: opaque
            ? 1
            : floating
            ? tokens.floatingOpacity
            : tokens.contentOpacity,
      ),
      surfaceTintColor: Colors.transparent,
      shadowColor: tokens.shadow,
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: radius,
        side: BorderSide(color: tokens.border, width: tokens.borderWidth),
      ),
      child: child,
    );
    // One tightly clipped filter on the composer, never on card content.
    final clipped = ClipRRect(
      borderRadius: radius,
      child: floating && !opaque && tokens.blurSigma > 0
          ? BackdropFilter(
              filter: ImageFilter.blur(
                sigmaX: tokens.blurSigma,
                sigmaY: tokens.blurSigma,
              ),
              child: material,
            )
          : material,
    );
    return DecoratedBox(
      decoration: BoxDecoration(
        borderRadius: radius,
        boxShadow: floating
            ? [
                BoxShadow(
                  color: tokens.shadow,
                  blurRadius: tokens.floatingElevation * 3,
                  offset: Offset(0, tokens.floatingElevation),
                ),
              ]
            : const [],
      ),
      child: clipped,
    );
  }
}
