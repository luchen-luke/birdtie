import 'package:birdtie_client/src/city/now_map_compass.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/now_social_edge.dart';
import 'package:birdtie_client/src/workspace/now_social_edge_source.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';

import 'now_social_edge_test.dart' as original;

// Independent background samples, not colors obtained from the candidate
// palette. Four samples are exact native APK06 PNG pixels; sea is the supplied
// representative light-map reference (not present in that city-center PNG).
const _lightTiles = [
  (name: 'PNG white road at (94,400)', color: Color(0xFFFFFFFF)),
  (name: 'PNG park at (488,633)', color: Color(0xFFC2E9B3)),
  (name: 'PNG block at (1,400)', color: Color(0xFFEBE7E4)),
  (name: 'PNG pale area at (242,404)', color: Color(0xFFF6F2E4)),
  (name: 'supplied light sea RGB 168,219,251', color: Color(0xFFA8DBFB)),
];

double _contrast(Color foreground, Color background) {
  final actual = Color.alphaBlend(foreground, background);
  final a = actual.computeLuminance(), b = background.computeLuminance();
  return ((a > b ? a : b) + .05) / ((a < b ? a : b) + .05);
}

Color _renderedTextColor(WidgetTester tester, Finder text) {
  final paragraph = tester.renderObject<RenderParagraph>(text);
  return paragraph.text.style!.color!;
}

void main() {
  for (final brightness in Brightness.values) {
    testWidgets(
      '$brightness app on fixed light map: real social glyphs contrast and original reads',
      (tester) async {
        tester.view.physicalSize = const Size(320, 640);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final changes = ValueNotifier<int>(0);
        var reads = 0;
        final api = ConnectionSource(
          authorizationHeader: () => 'Bearer synthetic-overlay',
          apiBaseUrl: 'https://map-overlay.fixture',
          client: MockClient((request) async {
            expect(request.method, 'GET');
            reads++;
            return original.originalWire(
              request.url.path,
              name: '地图验收成员 River',
            );
          }),
        );
        final source = NowSocialEdgeSource(api);
        try {
          for (final tileEntry in _lightTiles.asMap().entries) {
            final tile = tileEntry.value;
            await tester.pumpWidget(
              MaterialApp(
                theme: ThemeData(brightness: brightness),
                home: Builder(
                  builder: (context) {
                    final roles = NowUiTokens.of(context);
                    return Scaffold(
                      body: Stack(
                        children: [
                          Positioned.fill(child: ColoredBox(color: tile.color)),
                          Positioned(
                            left: 0,
                            bottom: 0,
                            width: 200,
                            child: Material(
                              color: Theme.of(context).colorScheme.surface,
                              child: Column(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  Text('独立面板标题', style: roles.sheetTitle),
                                  Text('独立面板正文', style: roles.body),
                                  Text('独立面板说明', style: roles.metadata),
                                ],
                              ),
                            ),
                          ),
                          Positioned(
                            right: 0,
                            top: 0,
                            bottom: 0,
                            child: NowSocialEdge(
                              open: true,
                              source: source,
                              identityChanges: changes,
                              current: () => true,
                              accountID: () => original.ownerID,
                              workspaceID: () => null,
                              onOpenAuthor: (_) {},
                              onOpenConversation: (_) {},
                              onOpenManagement: () {},
                              onSignIn: () {},
                              onUsePersonal: () {},
                            ),
                          ),
                        ],
                      ),
                    );
                  },
                ),
              ),
            );
            await tester.pumpAndSettle();
            expect(
              reads,
              1 + tileEntry.key * 2,
              reason: 'Tile presentation is not a new identity.',
            );
            original.expectSocialViewport(tester);
            for (final label in ['地图验收成员 River', '7', '消息']) {
              final matches = find.text(label);
              expect(matches, findsWidgets);
              for (final element in matches.evaluate()) {
                final finder = find.byElementPredicate((e) => e == element);
                final actual = _renderedTextColor(tester, finder);
                expect(
                  _contrast(actual, tile.color),
                  greaterThanOrEqualTo(4.5),
                  reason: '$brightness $label $actual on ${tile.name}',
                );
                final fade = tester.widget<DecoratedBox>(
                  find.byKey(const Key('now-social-edge-fade')),
                );
                final local =
                    (fade.decoration as BoxDecoration).gradient!
                        as LinearGradient;
                for (final background in [
                  Colors.black,
                  Colors.white,
                  tile.color,
                ]) {
                  expect(
                    _contrast(
                      actual,
                      Color.alphaBlend(local.colors[1], background),
                    ),
                    greaterThanOrEqualTo(4.5),
                    reason: '实际局部渐变正文区域的最小不透明度合成对比',
                  );
                }
                final text = element.widget as Text;
                if (text.style?.shadows case final shadows?) {
                  expect(shadows, isEmpty);
                }
              }
            }
            final action = find.byKey(const ValueKey('edge-tab-contacts'));
            final button = tester.widget<IconButton>(action);
            final foreground = button.color!;
            expect(find.byKey(const Key('edge-management')), findsNothing);
            expect(
              _contrast(foreground, tile.color),
              greaterThanOrEqualTo(4.5),
            );
            expect(tester.getSize(action).width, greaterThanOrEqualTo(48));
            expect(tester.getSize(action).height, greaterThanOrEqualTo(48));
            await tester.ensureVisible(action);
            await tester.tap(action);
            await tester.pumpAndSettle();
            expect(reads, 2 + tileEntry.key * 2);
            expect(find.text('联系人'), findsOneWidget);
            expect(
              find.byKey(
                const ValueKey('edge-conversation-${original.chatID}'),
              ),
              findsNothing,
            );
            await tester.tap(find.byKey(const ValueKey('edge-tab-messages')));
            await tester.pumpAndSettle();
            expect(reads, 3 + tileEntry.key * 2);
            final panel = tester.element(find.text('独立面板正文'));
            final panelTheme = Theme.of(panel);
            final panelBody = tester.widget<Text>(find.text('独立面板正文'));
            final panelMetadata = tester.widget<Text>(find.text('独立面板说明'));
            expect(panelBody.style!.fontSize, 15);
            expect(panelBody.style!.fontWeight, FontWeight.w400);
            expect(panelMetadata.style!.fontSize, 13);
            expect(
              panelBody.style!.color,
              brightness == Brightness.dark
                  ? panelTheme.colorScheme.onSurface
                  : const Color(0xFF374151),
            );
            expect(
              panelMetadata.style!.color,
              brightness == Brightness.dark
                  ? panelTheme.colorScheme.onSurfaceVariant
                  : const Color(0xFF566273),
            );
            expect(tester.takeException(), isNull);
          }
          expect(reads, 1 + _lightTiles.length * 2);
        } finally {
          await tester.pumpWidget(const SizedBox.shrink());
          api.dispose();
          changes.dispose();
        }
      },
    );

    testWidgets(
      '$brightness app on fixed light map: compass contrast, 20 glyph, 48 target and bearing action',
      (tester) async {
        var north = 0;
        for (final tile in _lightTiles) {
          await tester.pumpWidget(
            MaterialApp(
              theme: ThemeData(brightness: brightness),
              home: Scaffold(
                body: Stack(
                  children: [
                    Positioned.fill(child: ColoredBox(color: tile.color)),
                    Align(
                      alignment: Alignment.topRight,
                      child: NowMapCompass(bearing: 90, onNorth: () => north++),
                    ),
                  ],
                ),
              ),
            ),
          );
          await tester.pumpAndSettle();
          final compass = find.byKey(const Key('now-map-compass'));
          final icon = tester.widget<Icon>(
            find.byIcon(Icons.navigation_outlined),
          );
          final actual = icon.color!;
          expect(
            _contrast(actual, tile.color),
            greaterThanOrEqualTo(4.5),
            reason: '$brightness compass $actual on ${tile.name}',
          );
          expect(icon.size, 20);
          expect(tester.getSize(compass), const Size(48, 48));
          expect(tester.getSize(find.byType(IconButton)), const Size(48, 48));
          final rotation = tester.widget<Transform>(
            find.descendant(of: compass, matching: find.byType(Transform)),
          );
          expect(rotation.transform.storage[0], closeTo(0, .0001));
          expect(rotation.transform.storage[1], closeTo(-1, .0001));
          final surface = tester.widget<Material>(
            find.descendant(of: compass, matching: find.byType(Material)).first,
          );
          expect(surface.color, Colors.transparent);
          await tester.tap(find.byTooltip('地图朝北'));
          expect(tester.takeException(), isNull);
        }
        expect(north, _lightTiles.length);
      },
    );
  }
}
