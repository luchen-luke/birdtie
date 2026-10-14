import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

double contrast(Color a, Color b) {
  final x = a.computeLuminance(), y = b.computeLuminance();
  return ((x > y ? x : y) + .05) / ((x < y ? x : y) + .05);
}

void main() {
  test('confirmed roles are shared by the global theme', () {
    final theme = birdtieTheme(Brightness.light);
    final old = theme.extension<NowUiTokens>()!;
    final reviewed = NowUiTokens.reviewedTypography(theme);
    expect(theme.textTheme.bodyMedium!.fontSize, 15);
    expect(old.placeTitle.fontSize, 16);
    expect(old.body, reviewed.body);
    expect(theme.textTheme.bodyMedium, reviewed.body);
    expect(reviewed.pageTitle.fontSize, 20);
    expect(reviewed.pageTitle.fontWeight, FontWeight.w600);
    expect(reviewed.sheetTitle.fontSize, 18);
    expect(reviewed.sheetTitle.fontWeight, FontWeight.w600);
    expect(reviewed.placeTitle.fontSize, 16);
    expect(reviewed.placeTitle.fontWeight, FontWeight.w500);
    expect(reviewed.body.fontSize, 15);
    expect(reviewed.body.fontWeight, FontWeight.w400);
    expect(reviewed.body.height, 1.5);
    expect(reviewed.user, reviewed.body);
    expect(reviewed.composer.fontSize, 16);
    expect(reviewed.action.fontSize, 14);
    expect(reviewed.chrome.fontSize, 15);
    expect(reviewed.metadata.fontSize, 13);
  });

  test('review keeps a supplied existing family and CJK fallback', () {
    final theme = ThemeData(
      textTheme: ThemeData().textTheme.apply(
        fontFamily: 'ExistingLocalFont',
        fontFamilyFallback: ['ExistingCJK'],
      ),
    );
    final roles = NowUiTokens.reviewedTypography(theme);
    for (final style in [
      roles.sheetTitle,
      roles.placeTitle,
      roles.body,
      roles.composer,
      roles.action,
      roles.metadata,
    ]) {
      expect(style.fontFamily, 'ExistingLocalFont');
      expect(style.fontFamilyFallback, ['ExistingCJK']);
    }
  });

  for (final brightness in Brightness.values) {
    test('normal text meets 4.5 on $brightness composite surfaces', () {
      final theme = birdtieTheme(brightness);
      final roles = NowUiTokens.reviewedTypography(theme);
      final surface = theme.extension<BirdtieSurfaceTokens>()!;
      final backgrounds = [
        theme.scaffoldBackgroundColor,
        theme.colorScheme.surface,
        theme.colorScheme.secondaryContainer,
        for (final alpha in [surface.floatingOpacity, surface.contentOpacity])
          for (final under in [Colors.black, Colors.white])
            Color.alphaBlend(
              surface.background.withValues(alpha: alpha),
              under,
            ),
      ];
      var minimum = 100.0;
      for (final background in backgrounds) {
        for (final style in [
          roles.pageTitle,
          roles.sheetTitle,
          roles.placeTitle,
          roles.body,
          roles.user,
          roles.composer,
          roles.address,
          roles.chrome,
          roles.action,
          roles.listSubtitle,
          roles.metadata,
          roles.body.copyWith(color: theme.colorScheme.error),
        ]) {
          final ratio = contrast(style.color!, background);
          minimum = ratio < minimum ? ratio : minimum;
          expect(
            ratio,
            greaterThanOrEqualTo(4.5),
            reason: '$brightness ${style.color} over $background',
          );
        }
      }
      // Real Theme/alpha math, including opposite-color map extremes.
      debugPrint(
        'Typography $brightness min contrast: $minimum; '
        'font=${roles.body.fontFamily} fallback=${roles.body.fontFamilyFallback}',
      );
    });
  }
}
