import 'package:flutter/material.dart';

/// Semantic application text. The map style owns its own road/place labels.
/// Font families and fallback remain the platform/theme's existing choices;
/// these roles never alter MediaQuery's TextScaler.
@immutable
class NowUiTokens extends ThemeExtension<NowUiTokens> {
  const NowUiTokens({
    required this.sheetTitle,
    required this.placeTitle,
    required this.chrome,
    required this.body,
    required this.user,
    required this.composer,
    required this.address,
    required this.action,
    required this.listSubtitle,
    required this.metadata,
  });

  static const lightSurfaceTitle = Color(0xFF20242A);
  static const lightSurfaceBody = Color(0xFF374151);
  static const lightSurfaceSecondary = Color(0xFF566273);
  // V4's local right-edge paper belongs to the fixed light map, even when
  // the application's panels use dark mode. It is not a global surface.
  static const ownerMapEdgePaper = Color(0xFFFAFBF8);
  static const socialColumnWidth = 64.0;
  static const socialPanelWidth = 256.0;
  static const socialPanelMaxHeight = 360.0;
  // Recent names and previews share the same readable local surface.
  static const socialRailWidth = socialPanelWidth;
  static const socialRecentBaseline = 4;
  static const socialCornerRadius = 20.0;
  static const socialAvatarSize = 36.0;
  static const glassOpacity = .86;
  static const glassBlurSigma = 1.5;
  static const clearControlOpacity = .94;
  static const composerGlassOpacity = .90;
  static const navigationGlassOpacity = .97;
  static const glassElevation = 3.0;
  static const glassEdgeWidth = 1.0;
  static const sidebarWidth = 310.0;
  static const sidebarMaxWidth = 382.0;

  /// One subtle edge for local controls, navigation and the solid reader.
  /// High contrast uses an explicit outline instead of a reflective highlight.
  static Color materialEdge(ThemeData theme, {bool highContrast = false}) =>
      highContrast
      ? theme.colorScheme.onSurface.withValues(alpha: .65)
      : Color.alphaBlend(
          Colors.white.withValues(
            alpha: theme.brightness == Brightness.light ? .62 : .10,
          ),
          theme.colorScheme.outlineVariant,
        );

  static LinearGradient materialSheen(ThemeData theme) => LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [
      Colors.white.withValues(
        alpha: theme.brightness == Brightness.light ? .16 : .05,
      ),
      Colors.transparent,
    ],
  );

  static Color materialShadow(ThemeData theme) =>
      theme.colorScheme.shadow.withValues(alpha: .12);
  static const socialFadeWidth = 96.0;
  static const socialFadeTail = 32.0;

  // mapbox_style.dart is the owner's fixed light map, independent of app
  // brightness. These glyph colors do not recolor dark panels or map tiles.
  // Small map labels need the existing darker body color: the panel's
  // secondary color does not reach 4.5:1 over the map's light sea blue.
  static const ownerMapOverlay = NowMapOverlayPalette(
    title: lightSurfaceTitle,
    body: lightSurfaceBody,
    secondary: lightSurfaceBody,
    glyphHalo: Colors.white,
  );

  /// The user-approved roles, shared by the whole application.
  factory NowUiTokens.fromTheme(ThemeData theme) {
    final text = theme.textTheme;
    final light = theme.brightness == Brightness.light;
    final titleColor = light ? lightSurfaceTitle : theme.colorScheme.onSurface;
    final bodyColor = light ? lightSurfaceBody : theme.colorScheme.onSurface;
    // Passes the floating .94 surface and the existing tinted user bubble.
    final secondaryColor = light
        ? lightSurfaceSecondary
        : theme.colorScheme.onSurfaceVariant;
    TextStyle role(
      TextStyle? base,
      double size,
      FontWeight weight,
      Color color, {
      double height = 1.5,
    }) => (base ?? const TextStyle()).copyWith(
      fontSize: size,
      height: height,
      fontWeight: weight,
      letterSpacing: 0,
      color: color,
    );
    final body = role(text.bodyLarge, 15, FontWeight.w400, bodyColor);
    return NowUiTokens(
      sheetTitle: role(text.titleLarge, 18, FontWeight.w600, titleColor),
      placeTitle: role(text.titleMedium, 16, FontWeight.w500, titleColor),
      chrome: role(text.titleSmall, 15, FontWeight.w500, titleColor),
      body: body,
      user: body,
      composer: role(text.bodyLarge, 16, FontWeight.w400, bodyColor),
      address: body,
      action: role(
        text.labelLarge,
        14,
        FontWeight.w500,
        theme.colorScheme.primary,
      ),
      listSubtitle: role(text.bodyMedium, 13, FontWeight.w400, secondaryColor),
      metadata: role(text.bodySmall, 13, FontWeight.w400, secondaryColor),
    );
  }

  // Retain the first-review API for existing callers without a second palette.
  factory NowUiTokens.reviewedTypography(ThemeData theme) =>
      NowUiTokens.fromTheme(theme);

  static NowUiTokens reviewedOf(BuildContext context) => of(context);

  static NowUiTokens of(BuildContext context) {
    final theme = Theme.of(context);
    return theme.extension<NowUiTokens>() ?? NowUiTokens.fromTheme(theme);
  }

  final TextStyle sheetTitle, placeTitle, chrome, body, user, composer;
  final TextStyle address, action, listSubtitle, metadata;
  TextStyle get pageTitle => sheetTitle.copyWith(
    fontSize: 20,
    height: 1.4,
    fontWeight: FontWeight.w600,
  );
  TextStyle get chromeLabel => chrome;
  TextStyle get userMessage => user;
  TextStyle get composerHint => body.copyWith(color: metadata.color);

  static const space4 = 4.0;
  static const space8 = 8.0;
  static const space12 = 12.0;
  static const space16 = 16.0;
  static const space24 = 24.0;
  static const space32 = 32.0;
  static const contentInsetHorizontal = space16;
  static const composerInsetHorizontal = space16;
  static const composerMinHeight = 56.0;
  static const sheetCornerTop = space24;
  static const sheetCornerBottom = 0.0;
  static const actionHitSize = 48.0;

  @override
  NowUiTokens copyWith({
    TextStyle? sheetTitle,
    TextStyle? placeTitle,
    TextStyle? chrome,
    TextStyle? body,
    TextStyle? user,
    TextStyle? composer,
    TextStyle? address,
    TextStyle? action,
    TextStyle? listSubtitle,
    TextStyle? metadata,
  }) => NowUiTokens(
    sheetTitle: sheetTitle ?? this.sheetTitle,
    placeTitle: placeTitle ?? this.placeTitle,
    chrome: chrome ?? this.chrome,
    body: body ?? this.body,
    user: user ?? this.user,
    composer: composer ?? this.composer,
    address: address ?? this.address,
    action: action ?? this.action,
    listSubtitle: listSubtitle ?? this.listSubtitle,
    metadata: metadata ?? this.metadata,
  );

  @override
  NowUiTokens lerp(NowUiTokens? other, double t) {
    if (other == null) return this;
    return NowUiTokens(
      sheetTitle: TextStyle.lerp(sheetTitle, other.sheetTitle, t)!,
      placeTitle: TextStyle.lerp(placeTitle, other.placeTitle, t)!,
      chrome: TextStyle.lerp(chrome, other.chrome, t)!,
      body: TextStyle.lerp(body, other.body, t)!,
      user: TextStyle.lerp(user, other.user, t)!,
      composer: TextStyle.lerp(composer, other.composer, t)!,
      address: TextStyle.lerp(address, other.address, t)!,
      action: TextStyle.lerp(action, other.action, t)!,
      listSubtitle: TextStyle.lerp(listSubtitle, other.listSubtitle, t)!,
      metadata: TextStyle.lerp(metadata, other.metadata, t)!,
    );
  }
}

/// Application glyphs drawn directly over the owner-supplied light map.
/// This palette is not a claim of support for independently themed dark tiles.
@immutable
class NowMapOverlayPalette {
  const NowMapOverlayPalette({
    required this.title,
    required this.body,
    required this.secondary,
    required this.glyphHalo,
  });

  final Color title, body, secondary, glyphHalo;
}
