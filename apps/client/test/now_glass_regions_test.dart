// Direct component harness; native map/audio/performance are outside it.
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/now_glass_surface.dart';
import 'package:birdtie_client/src/workspace/now_social_edge.dart';
import 'package:birdtie_client/src/workspace/now_social_edge_source.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:birdtie_client/src/workspace/voice_input.dart';
import 'package:birdtie_client/src/workspace/voice_input_consent.dart';
import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

typedef _Effects = ({bool reduced, bool highContrast, bool disabledAnimations});

void main() {
  testWidgets('clear header keeps local bounds and actions; real input/social '
      'retain live backdrop and drafts through accessible fallback', (t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final semantics = t.ensureSemantics();
    try {
      final effects = ValueNotifier<_Effects>((
        reduced: false,
        highContrast: false,
        disabledAnimations: false,
      ));
      final identity = ValueNotifier<int>(0);
      final workspace = AgentWorkspaceController();
      final voice = NativeVoiceInputPort(supported: () => false);
      var reads = 0, submits = 0, sidebarTaps = 0, cityTaps = 0;
      var contactTaps = 0, notificationTaps = 0;
      final client = MockClient((_) async {
        reads++;
        return http.Response('{}', 500);
      });
      final api = ConnectionSource(
        authorizationHeader: () => null,
        apiBaseUrl: 'https://material.fixture',
        client: client,
      );
      final socialSource = NowSocialEdgeSource(api);
      final consent = MemoryVoiceInputConsentStore();
      addTearDown(effects.dispose);
      addTearDown(identity.dispose);
      addTearDown(workspace.dispose);
      addTearDown(client.close);
      addTearDown(api.dispose);
      await t.pumpWidget(
        ValueListenableBuilder<_Effects>(
          valueListenable: effects,
          builder: (_, flags, _) {
            final theme = ThemeData.light();
            return MaterialApp(
              theme: theme.copyWith(
                extensions: [
                  BirdtieSurfaceTokens.fromTheme(
                    theme,
                  ).copyWith(reduceEffects: flags.reduced),
                ],
              ),
              home: Builder(
                builder: (context) => MediaQuery(
                  data: MediaQuery.of(context).copyWith(
                    highContrast: flags.highContrast,
                    disableAnimations: flags.disabledAnimations,
                  ),
                  child: Scaffold(
                    body: Stack(
                      children: [
                        Align(
                          alignment: Alignment.topLeft,
                          child: TopControls(
                            contextLabel: 'Aberdeen',
                            onSidebar: () => sidebarTaps++,
                            onTools: () {},
                            onContext: () => cityTaps++,
                            onInbox: () => contactTaps++,
                            onNotifications: () => notificationTaps++,
                            hasUnreadMessages: true,
                            notificationsExpanded: true,
                          ),
                        ),
                        Positioned(
                          top: 96,
                          right: 8,
                          child: NowSocialEdge(
                            open: true,
                            source: socialSource,
                            identityChanges: identity,
                            current: () => true,
                            accountID: () => null,
                            workspaceID: () => null,
                            onOpenAuthor: (_) {},
                            onOpenConversation: (_) {},
                            onOpenManagement: () {},
                            onSignIn: () {},
                            onUsePersonal: () {},
                          ),
                        ),
                        Align(
                          alignment: Alignment.bottomCenter,
                          child: AgentComposer(
                            embedded: true,
                            workspace: workspace,
                            onSubmit: (_) => submits++,
                            onSearchArea: () {},
                            hasSearchArea: false,
                            voiceInputPort: voice,
                            voiceConsentStore: consent,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            );
          },
        ),
      );
      await t.pumpAndSettle();
      final header = find.byType(TopControls);
      final input = find.byType(AgentComposer);
      final social = find.byType(NowSocialEdge);
      Finder inRegion(Finder region, Type type) =>
          find.descendant(of: region, matching: find.byType(type));
      Material outerMaterial(Finder surface) {
        final material = t.widget<Material>(
          find.descendant(of: surface, matching: find.byType(Material)).first,
        );
        expect(
          material.color,
          isNotNull,
          reason: 'the local outer material owns the reading base',
        );
        return material;
      }

      void verifyRegions({required bool fallback}) {
        expect(inRegion(header, NowGlassSurface), findsNWidgets(4));
        expect(inRegion(header, ClipRRect), findsNWidgets(4));
        expect(inRegion(header, BackdropFilter), findsNothing);
        final headerSurfaces = inRegion(header, BirdtieSurface);
        expect(headerSurfaces, findsNWidgets(4));
        for (final element in headerSurfaces.evaluate()) {
          final surface = find.byElementPredicate(
            (candidate) => identical(candidate, element),
          );
          final material = outerMaterial(surface);
          if (fallback) {
            expect(
              material.color!.a,
              1,
              reason: 'all four header reading bases become opaque',
            );
          } else {
            expect(material.color!.a, greaterThanOrEqualTo(.9));
            expect(material.color!.a, lessThan(1));
          }
        }
        for (final region in [input, social]) {
          expect(inRegion(region, NowGlassSurface), findsOneWidget);
          expect(
            inRegion(region, BackdropFilter),
            fallback ? findsNothing : findsOneWidget,
          );
          final surface = inRegion(region, BirdtieSurface);
          expect(
            surface,
            findsOneWidget,
            reason: 'each real input/social region has one reading base',
          );
          final material = outerMaterial(surface);
          if (fallback) {
            expect(
              material.color!.a,
              1,
              reason: 'accessibility paints an opaque local reading base',
            );
          } else {
            expect(material.color!.a, greaterThan(0));
            expect(material.color!.a, lessThan(1));
            expect(
              t.getRect(inRegion(region, BackdropFilter)),
              t.getRect(inRegion(region, NowGlassSurface)),
            );
          }
        }
        expect(find.byType(ImageFiltered), findsNothing);
        final nodes = t.semantics.simulatedAccessibilityTraversal();
        for (final label in ['打开侧边栏', '联系，有未读', '收起通知']) {
          final node = nodes.where((n) => n.label == label).single;
          expect(node.hasFlag(SemanticsFlag.isButton), true);
          expect(node.getSemanticsData().hasAction(SemanticsAction.tap), true);
        }
        for (final key in [
          'now-sidebar-button',
          'now-social-button',
          'now-notifications-button',
        ]) {
          expect(t.getSize(find.byKey(Key(key))), const Size(48, 48));
        }
        expect(
          t.getSize(find.byKey(const Key('now-city-picker'))).height,
          greaterThanOrEqualTo(48),
        );
        expect(
          reads,
          0,
          reason: 'guest material does not introduce private reads',
        );
        expect(
          submits,
          0,
          reason: 'render/fallback changes never submit a draft',
        );
        expect(t.takeException(), isNull);
      }

      verifyRegions(fallback: false);
      await t.tap(find.byKey(const Key('now-sidebar-button')));
      await t.tap(find.byKey(const Key('now-city-picker')));
      await t.tap(find.byKey(const Key('now-social-button')));
      await t.tap(find.byKey(const Key('now-notifications-button')));
      expect(
        (sidebarTaps, cityTaps, contactTaps, notificationTaps),
        (1, 1, 1, 1),
      );
      final editor = find.descendant(
        of: input,
        matching: find.byType(TextField),
      );
      await t.enterText(editor, '保留这份可编辑草稿');
      final originalController = t.widget<TextField>(editor).controller!;
      for (final flags in <_Effects>[
        (reduced: false, highContrast: true, disabledAnimations: false),
        (reduced: false, highContrast: false, disabledAnimations: true),
        (reduced: true, highContrast: false, disabledAnimations: false),
      ]) {
        effects.value = flags;
        await t.pumpAndSettle();
        verifyRegions(fallback: true);
        expect(
          t.widget<TextField>(editor).controller,
          same(originalController),
        );
        expect(originalController.text, '保留这份可编辑草稿');
      }
      effects.value = (
        reduced: false,
        highContrast: false,
        disabledAnimations: false,
      );
      await t.pumpAndSettle();
      verifyRegions(fallback: false);
      expect(originalController.text, '保留这份可编辑草稿');
      await t.pumpWidget(const SizedBox());
    } finally {
      semantics.dispose();
    }
  });
}
