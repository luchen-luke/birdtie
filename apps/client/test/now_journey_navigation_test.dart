import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';

void main() {
  testWidgets(
    'personal journeys has direct visible path and hides when signed out',
    (tester) async {
      final workspace = AgentWorkspaceController();
      final city = PublicCityController();
      final organizations = OrganizationWorkspaceController(
        authorizationHeader: () => null,
      );
      var opened = 0;
      Widget view(bool signedIn) => MaterialApp(
        home: Scaffold(
          body: Sidebar(
            workspace: workspace,
            city: city,
            organizations: organizations,
            onCitySelected: (_) {},
            onNew: () {},
            onDestination: (_) {},
            onRecent: (_) {},
            onCreateOrganization: () {},
            onViewInvitations: () {},
            onJourneys: () => opened++,
            signedIn: signedIn,
          ),
        ),
      );
      await tester.pumpWidget(view(true));
      await tester.tap(find.text('我的内容'));
      await tester.pumpAndSettle();
      expect(find.text('我的旅程'), findsOneWidget);
      await tester.ensureVisible(find.text('我的旅程'));
      await tester.tap(find.text('我的旅程'));
      expect(opened, 1);
      await tester.pumpWidget(view(false));
      await tester.pumpAndSettle();
      expect(find.text('我的旅程'), findsNothing);
      await tester.pumpWidget(const SizedBox.shrink());
      workspace.dispose();
      city.dispose();
      organizations.dispose();
    },
  );
}
