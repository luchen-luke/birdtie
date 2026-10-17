import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/city/destination_search_shortcuts.dart';

void main() {
  testWidgets(
    'unmapped approved artwork remains explicit search, categories preserved',
    (tester) async {
      String? query;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: CustomScrollView(
              slivers: [
                DestinationSearchShortcutGrid(
                  filter: 'CN',
                  onSearch: (v) => query = v,
                ),
              ],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(destinationShortcutsFor('all').length, 5);
      expect(destinationShortcutsFor('CN').length, 1);
      expect(destinationShortcutsFor('abroad').length, 4);
      expect(find.text('大理'), findsOneWidget);
      expect(find.text('中国 · 云南 · 搜索'), findsOneWidget);
      await tester.tap(find.byKey(const Key('destination-search-dali')));
      expect(query, '大理 中国');
      expect(destinationSearchShortcuts.map((v) => v.asset).toSet().length, 5);
    },
  );
}
