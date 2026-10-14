import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:birdtie_client/src/city/now_map_compass.dart';

List<SemanticsNode> _buttons(WidgetTester tester) {
  final buttons = <SemanticsNode>[];
  void visit(SemanticsNode node) {
    if (node.hasFlag(SemanticsFlag.isButton)) buttons.add(node);
    node.visitChildren((child) {
      visit(child);
      return true;
    });
  }

  visit(tester.binding.pipelineOwner.semanticsOwner!.rootSemanticsNode!);
  return buttons;
}

void main() {
  final cases = <double, String>{
    0: '朝北',
    360: '朝北',
    -360: '朝北',
    720: '朝北',
    45: '方向45度',
    -45: '方向315度',
    765: '方向45度',
    -405: '方向315度',
    359: '方向359度',
  };

  for (final entry in cases.entries) {
    testWidgets('compass has one button with direction at ${entry.key}°', (
      tester,
    ) async {
      final semantics = tester.ensureSemantics();
      try {
        var northTaps = 0;
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: NowMapCompass(
                bearing: entry.key,
                onNorth: () => northTaps++,
              ),
            ),
          ),
        );

        final buttons = _buttons(tester);
        expect(buttons, hasLength(1));
        final data = buttons.single.getSemanticsData();
        expect(data.value, entry.value);
        expect(data.label, '地图朝北');
        expect(data.hasAction(SemanticsAction.tap), isTrue);
        expect(
          tester.widget<IconButton>(find.byType(IconButton)).tooltip,
          '地图朝北',
        );
        expect(
          tester.getSize(find.byKey(const Key('now-map-compass'))),
          const Size(48, 48),
        );

        await tester.tap(find.byType(IconButton));
        expect(northTaps, 1);
        expect(tester.takeException(), isNull);
      } finally {
        semantics.dispose();
      }
    });
  }
}
