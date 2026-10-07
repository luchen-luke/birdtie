import 'package:birdtie_client/src/workspace/place_history_controller.dart';
import 'package:birdtie_client/src/workspace/place_history_section.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'place_history_controller_test.dart'
    show historyTestPlace, historyTestData, historyTestReply;

void main() {
  testWidgets('中文摘要明确安排边界和UNKNOWN，大字可滚动', (t) async {
    await t.binding.setSurfaceSize(const Size(360, 640));
    addTearDown(() => t.binding.setSurfaceSize(null));
    final c = PlaceHistoryController(
      placeID: historyTestPlace,
      authorizationHeader: () => null,
      client: MockClient((_) async => historyTestReply(historyTestData())),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await c.refresh();
    await t.pumpWidget(
      MaterialApp(
        builder: (ctx, child) => MediaQuery(
          data: MediaQuery.of(
            ctx,
          ).copyWith(textScaler: const TextScaler.linear(1.8)),
          child: child!,
        ),
        home: Scaffold(
          body: SingleChildScrollView(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: PlaceHistorySection(controller: c),
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('公开分享'), findsOneWidget);
    expect(find.text('周末 · 羽毛球：2 项安排'), findsOneWidget);
    await t.ensureVisible(find.textContaining('各项适合度尚未核验'));
    expect(find.textContaining('不代表实际举办、到场或人数'), findsOneWidget);
    expect(t.takeException(), isNull);
  });
}
