import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  for (final brightness in Brightness.values) {
    test('$brightness text contrast survives worst map backgrounds', () {
      final theme = birdtieTheme(brightness);
      final tokens = theme.extension<BirdtieSurfaceTokens>()!;
      for (final alpha in [
        tokens.floatingOpacity,
        tokens.contentOpacity,
        1.0,
      ]) {
        for (final backdrop in [Colors.black, Colors.white]) {
          final background = Color.alphaBlend(
            tokens.background.withValues(alpha: alpha),
            backdrop,
          );
          for (final foreground in [
            theme.colorScheme.onSurface,
            theme.colorScheme.onSurfaceVariant,
          ]) {
            final a = foreground.computeLuminance();
            final b = background.computeLuminance();
            final ratio =
                (a > b ? a + .05 : b + .05) / (a > b ? b + .05 : a + .05);
            expect(ratio, greaterThanOrEqualTo(4.5));
          }
        }
      }
    });
    for (final fallback in ['none', 'contrast', 'motion', 'device']) {
      testWidgets('$brightness floating fallback $fallback', (tester) async {
        var theme = birdtieTheme(brightness);
        if (fallback == 'device') {
          theme = theme.copyWith(
            extensions: [
              theme.extension<BirdtieSurfaceTokens>()!.copyWith(
                reduceEffects: true,
              ),
            ],
          );
        }
        await tester.pumpWidget(
          MaterialApp(
            theme: theme,
            home: MediaQuery(
              data: MediaQueryData(
                highContrast: fallback == 'contrast',
                disableAnimations: fallback == 'motion',
              ),
              child: const BirdtieSurface(
                kind: BirdtieSurfaceKind.floating,
                child: Text('你想做什么？'),
              ),
            ),
          ),
        );
        final material = tester.widget<Material>(
          find.descendant(
            of: find.byType(BirdtieSurface),
            matching: find.byType(Material),
          ),
        );
        expect(material.color!.a, fallback == 'none' ? closeTo(.94, .01) : 1);
        expect(
          find.byType(BackdropFilter),
          fallback == 'none' ? findsOneWidget : findsNothing,
        );
      });
    }
  }
  for (final kind in [
    BirdtieSurfaceKind.content,
    BirdtieSurfaceKind.critical,
  ]) {
    testWidgets('$kind content is readable without any blur', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: birdtieTheme(Brightness.light),
          home: BirdtieSurface(kind: kind, child: const Text('地址与最终确认')),
        ),
      );
      expect(find.byType(BackdropFilter), findsNothing);
      final material = tester.widget<Material>(
        find.descendant(
          of: find.byType(BirdtieSurface),
          matching: find.byType(Material),
        ),
      );
      expect(
        material.color!.a,
        kind == BirdtieSurfaceKind.critical ? 1 : closeTo(.98, .01),
      );
    });
  }
}
