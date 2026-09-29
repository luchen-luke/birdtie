import 'package:birdtie_client/src/app/birdtie_app.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'map workspace replaces bottom navigation and accepts an intent',
    (tester) async {
      await tester.pumpWidget(const BirdtieApp());
      expect(find.byType(NavigationBar), findsNothing);
      expect(find.byTooltip('Open sidebar'), findsOneWidget);
      expect(find.byTooltip('Open inbox'), findsOneWidget);
      expect(find.text('What do you want to do?'), findsOneWidget);

      await tester.enterText(
        find.byType(TextField).first,
        'Find someone to play badminton this weekend',
      );
      await tester.testTextInput.receiveAction(TextInputAction.send);
      await tester.pump();
      expect(find.byType(AgentResultSheet), findsOneWidget);
      await tester.pump(const Duration(milliseconds: 700));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('No matching published activities'),
        findsOneWidget,
      );

      await tester.tap(find.byTooltip('Open sidebar'));
      await tester.pumpAndSettle();
      expect(find.text('RECENT AGENT TASKS'), findsOneWidget);
      await tester.tap(find.text('Profile'));
      await tester.pumpAndSettle();
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.byType(AgentResultSheet), findsOneWidget);
      expect(
        find.text('Find someone to play badminton this weekend'),
        findsWidgets,
      );

      await tester.tap(find.byTooltip('Open sidebar'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('New'));
      await tester.pumpAndSettle();
      expect(find.byType(AgentResultSheet), findsNothing);

      await tester.tap(find.byTooltip('Open inbox'));
      await tester.pumpAndSettle();
      expect(find.text('Needs attention'), findsOneWidget);
      await tester.scrollUntilVisible(
        find.text('Agent Updates'),
        180,
        scrollable: find.byType(Scrollable).last,
      );
      expect(find.text('Agent Updates'), findsOneWidget);
    },
  );
}
