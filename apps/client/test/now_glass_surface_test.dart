import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/now_glass_surface.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'IME removes live texture sampling without replacing input or bounds',
    (t) async {
      final insets = ValueNotifier<double>(0);
      final input = TextEditingController(text: '保留输入稿');
      addTearDown(insets.dispose);
      addTearDown(input.dispose);
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<double>(
            valueListenable: insets,
            builder: (context, value, _) => MediaQuery(
              data: MediaQuery.of(
                context,
              ).copyWith(viewInsets: EdgeInsets.only(bottom: value)),
              child: Scaffold(
                resizeToAvoidBottomInset: false,
                body: Align(
                  alignment: Alignment.topLeft,
                  child: SizedBox(
                    width: 320,
                    height: 80,
                    child: NowGlassSurface(
                      role: NowGlassRole.composer,
                      child: TextField(controller: input),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      final element = find.byType(TextField).evaluate().single;
      final bounds = t.getRect(find.byType(NowGlassSurface));
      expect(find.byType(BackdropFilter), findsOneWidget);
      for (final inset in [1.0, 150.0, 300.0, 1.0, 0.0]) {
        insets.value = inset;
        await t.pumpAndSettle();
        expect(find.byType(BackdropFilter), findsOneWidget);
        expect(
          t.widget<BackdropFilter>(find.byType(BackdropFilter)).enabled,
          inset == 0,
        );
        expect(find.byType(TextField).evaluate().single, same(element));
        expect(t.getRect(find.byType(NowGlassSurface)), bounds);
        expect(input.text, '保留输入稿');
      }
      await t.pumpWidget(const SizedBox());
    },
  );
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
