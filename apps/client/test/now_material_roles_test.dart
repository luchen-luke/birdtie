// Direct widget evidence only; native map rendering and frame cost need the
// coordinator's matched build/device checkpoint.
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/now_glass_surface.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'now_scope_recovery_test.dart';

void main() {
  for (final brightness in Brightness.values) {
    testWidgets('$brightness local material roles preserve sharp content and '
        'become opaque without sampling in accessibility modes', (t) async {
      final theme = birdtieTheme(brightness);
      for (final role in NowGlassRole.values) {
        for (final mode in ['normal', 'reduced', 'contrast', 'motion']) {
          final fallback = mode != 'normal';
          await t.pumpWidget(
            MaterialApp(
              theme: theme.copyWith(
                extensions: [
                  ...theme.extensions.values.where(
                    (value) => value is! BirdtieSurfaceTokens,
                  ),
                  theme.extension<BirdtieSurfaceTokens>()!.copyWith(
                    reduceEffects: mode == 'reduced',
                  ),
                ],
              ),
              home: MediaQuery(
                data: MediaQueryData(
                  highContrast: mode == 'contrast',
                  disableAnimations: mode == 'motion',
                ),
                child: Scaffold(
                  body: Align(
                    alignment: Alignment.topRight,
                    child: SizedBox(
                      width: 256,
                      height: 120,
                      child: NowGlassSurface(
                        role: role,
                        child: const Text('可读的姓名与草稿'),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          );
          await t.pumpAndSettle();
          final local = find.byType(NowGlassSurface);
          final material = t.widget<Material>(
            find.descendant(of: local, matching: find.byType(Material)).first,
          );
          expect(
            find.byType(BackdropFilter),
            fallback || role == NowGlassRole.navigation
                ? findsNothing
                : findsOneWidget,
          );
          expect(find.byType(ImageFiltered), findsNothing);
          expect(find.text('可读的姓名与草稿'), findsOneWidget);
          expect(t.getSize(local), const Size(256, 120));
          expect(material.color!.a, fallback ? 1 : lessThan(1));
          if (role == NowGlassRole.navigation && !fallback) {
            expect(material.color!.a, greaterThan(.95));
          }
          final decorations = t.widgetList<DecoratedBox>(
            find.descendant(of: local, matching: find.byType(DecoratedBox)),
          );
          final gradients = decorations
              .map((w) => w.decoration)
              .whereType<BoxDecoration>()
              .where((d) => d.gradient != null);
          expect(gradients.length, fallback ? 0 : 1);
          expect(t.takeException(), isNull);
        }
      }
    });
  }

  for (final sizeAndScale in [
    (const Size(390, 844), 1.0),
    (const Size(320, 640), 2.0),
  ]) {
    testWidgets(
      'side drawer ${sizeAndScale.$1} scale ${sizeAndScale.$2} '
      'has reachable primary actions and closes without losing draft or map',
      (t) async {
        t.view.physicalSize = sizeAndScale.$1;
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final fixture = NowFixture();
        addTearDown(fixture.dispose);
        await fixture.mount(t, scale: sizeAndScale.$2);
        await t.enterText(nowField(), '保留这段未发送的草稿');
        FocusManager.instance.primaryFocus?.unfocus();
        t.testTextInput.hide();
        await t.pumpAndSettle();
        final canvas = find.byType(MapCanvas).evaluate().single;
        final composer = find.byType(AgentComposer).evaluate().single;
        final workspace = fixture.workspace(t);
        final task = workspace.task;
        await nowTap(t, find.byTooltip('打开侧边栏'));
        final drawer = find.byType(Drawer);
        final close = find.byKey(const Key('sidebar-close'));
        final create = find.byKey(const Key('sidebar-new-conversation'));
        expect(drawer, findsOneWidget);
        expect(close.hitTestable(), findsOneWidget);
        expect(create.hitTestable(), findsOneWidget);
        expect(t.getSize(close).height, greaterThanOrEqualTo(48));
        expect(t.getSize(close).width, greaterThanOrEqualTo(48));
        expect(t.getSize(create).height, greaterThanOrEqualTo(48));
        expect(
          t.getSize(drawer).width,
          lessThanOrEqualTo(sizeAndScale.$1.width - 48),
        );
        expect(
          t.getRect(create).top,
          lessThan(
            t
                .getRect(
                  find.descendant(
                    of: find.byType(Sidebar),
                    matching: find.text('Now'),
                  ),
                )
                .top,
          ),
        );
        expect(
          find.descendant(of: drawer, matching: find.byType(BackdropFilter)),
          findsNothing,
        );
        expect(find.byType(BottomSheet), findsNothing);
        expect(t.takeException(), isNull);
        await nowTap(t, close);
        expect(close, findsNothing);
        expect(find.byType(MapCanvas).evaluate().single, same(canvas));
        expect(find.byType(AgentComposer).evaluate().single, same(composer));
        expect(workspace.task, same(task));
        expect(t.widget<TextField>(nowField()).controller!.text, '保留这段未发送的草稿');
        expect(fixture.source.queries, isEmpty);
        expect(t.takeException(), isNull);
        await fixture.unmount(t);
      },
    );
  }

  testWidgets('answer reader remains a solid surface with one shared edge', (
    t,
  ) async {
    final fixture = NowFixture();
    addTearDown(fixture.dispose);
    await fixture.mount(t);
    await nowSend(t, '附近有什么');
    final reader = t.widget<Material>(
      find.byKey(const Key('now-primary-surface')),
    );
    expect(reader.color!.a, 1);
    expect(reader.surfaceTintColor, Colors.transparent);
    expect((reader.shape! as RoundedRectangleBorder).side.width, 1);
    expect(find.byType(AgentComposer), findsOneWidget);
    expect(find.byType(MapCanvas), findsOneWidget);
    expect(t.takeException(), isNull);
    await fixture.unmount(t);
  });
}
