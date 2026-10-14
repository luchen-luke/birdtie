import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('recent messages and deeper views share a readable bounded width', () {
    expect(NowUiTokens.socialRecentBaseline, 4);
    expect(
      NowUiTokens.socialRailWidth,
      greaterThan(NowUiTokens.socialAvatarSize + 48),
    );
    expect(NowUiTokens.socialRailWidth, NowUiTokens.socialPanelWidth);
    expect(NowUiTokens.socialPanelMaxHeight, lessThanOrEqualTo(360));
    expect(NowUiTokens.socialCornerRadius, 20);
    expect(NowUiTokens.glassOpacity, inExclusiveRange(0, 1));
  });
  for (final brightness in Brightness.values) {
    test('$brightness semantic roles match V2 without changing families', () {
      final original = ThemeData(useMaterial3: true, brightness: brightness);
      final theme = birdtieTheme(brightness);
      final tokens = theme.extension<NowUiTokens>()!;
      final roles = [
        (tokens.sheetTitle, 18.0, 27.0, FontWeight.w600),
        (tokens.placeTitle, 16.0, 24.0, FontWeight.w500),
        (tokens.chrome, 15.0, 22.5, FontWeight.w500),
        (tokens.body, 15.0, 22.5, FontWeight.w400),
        (tokens.user, 15.0, 22.5, FontWeight.w400),
        (tokens.composer, 16.0, 24.0, FontWeight.w400),
        (tokens.address, 15.0, 22.5, FontWeight.w400),
        (tokens.action, 14.0, 21.0, FontWeight.w500),
        (tokens.listSubtitle, 13.0, 19.5, FontWeight.w400),
        (tokens.metadata, 13.0, 19.5, FontWeight.w400),
      ];
      for (final (style, size, line, weight) in roles) {
        expect(style.fontSize, size);
        expect(style.fontSize! * style.height!, closeTo(line, .0001));
        expect(style.fontWeight, weight);
        expect(style.letterSpacing, 0);
        expect(style.fontFamily, original.textTheme.bodyLarge!.fontFamily);
        expect(
          style.fontFamilyFallback,
          original.textTheme.bodyLarge!.fontFamilyFallback,
        );
      }
      expect(tokens.chromeLabel, tokens.chrome);
      expect(tokens.userMessage, tokens.user);
      expect(theme.textTheme.bodyLarge, tokens.body);
      expect(theme.textTheme.bodyMedium, tokens.body);
      expect(theme.textTheme.titleLarge, tokens.sheetTitle);
      expect(theme.textTheme.labelLarge, tokens.action);
      expect(theme.extension<BirdtieSurfaceTokens>(), isNotNull);
    });
  }

  test(
    'custom existing family and fallback survive the approved semantic palette',
    () {
      const original = TextStyle(
        fontFamily: 'ExistingLicensedLocalFamily',
        fontFamilyFallback: ['ExistingCJKFallback'],
        color: Colors.brown,
        letterSpacing: 1.5,
      );
      final text = ThemeData().textTheme.apply(
        fontFamily: original.fontFamily,
        fontFamilyFallback: original.fontFamilyFallback,
        bodyColor: original.color,
        displayColor: original.color,
      );
      final tokens = NowUiTokens.fromTheme(ThemeData(textTheme: text));
      for (final style in [tokens.body, tokens.address, tokens.action]) {
        expect(style.fontFamily, original.fontFamily);
        expect(style.fontFamilyFallback, original.fontFamilyFallback);
        expect(style.color, isNotNull);
        expect(style.letterSpacing, 0);
      }
    },
  );

  test('copy and theme interpolation preserve semantic roles', () {
    final base = NowUiTokens.fromTheme(ThemeData());
    const color = Color(0xFFEF5350);
    final changed = base.copyWith(address: base.address.copyWith(color: color));
    expect(changed.body, base.body);
    expect(changed.address.color, color);
    expect(base.lerp(changed, 0).address, base.address);
    expect(base.lerp(changed, 1).address, changed.address);
    expect(base.lerp(null, .5), same(base));
    expect(base.lerp(changed, .5).composer, base.composer);
  });

  testWidgets(
    'fallback token lookup retains the supplied platform TextScaler',
    (tester) async {
      final scaler = TextScaler.linear(2);
      late NowUiTokens tokens;
      late TextScaler current;
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData(),
          home: MediaQuery(
            data: MediaQueryData(textScaler: scaler),
            child: Builder(
              builder: (context) {
                tokens = NowUiTokens.of(context);
                current = MediaQuery.textScalerOf(context);
                return Text('中文 address 14', style: tokens.body);
              },
            ),
          ),
        ),
      );
      expect(tokens.composer.fontSize, 16);
      expect(current, same(scaler));
      expect(current.scale(tokens.composer.fontSize!), 32);
    },
  );
}
