import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/now_glass_surface.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'one local clipped filter preserves sharp content; reduced effects remove filtering',
    (t) async {
      for (final reduced in [false, true]) {
        final theme = ThemeData.light();
        await t.pumpWidget(
          MaterialApp(
            theme: theme.copyWith(
              extensions: [
                BirdtieSurfaceTokens.fromTheme(
                  theme,
                ).copyWith(reduceEffects: reduced),
              ],
            ),
            home: const Scaffold(
              body: Align(
                alignment: Alignment.topRight,
                child: SizedBox(
                  width: 132,
                  height: 220,
                  child: NowGlassSurface(child: Text('Neo')),
                ),
              ),
            ),
          ),
        );
        // MaterialApp animates ThemeData changes, including extension fallbacks.
        await t.pumpAndSettle();
        expect(
          find.byType(BackdropFilter),
          reduced ? findsNothing : findsOneWidget,
        );
        expect(find.byType(ImageFiltered), findsNothing);
        expect(find.text('Neo'), findsOneWidget);
        final surface = t.getRect(find.byType(NowGlassSurface));
        expect(surface.size, const Size(132, 220));
        if (!reduced) expect(t.getRect(find.byType(ClipRRect)), surface);
      }
    },
  );
}
