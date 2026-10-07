import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'Personal sidebar keeps direct merchant console without Org admin',
    (tester) async {
      final city = PublicCityController();
      final workspace = AgentWorkspaceController();
      final organizations = OrganizationWorkspaceController(
        authorizationHeader: () => null,
      );
      SidebarDestination? selected;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Sidebar(
              workspace: workspace,
              city: city,
              onCitySelected: (_) {},
              onNew: () {},
              onDestination: (v) => selected = v,
              onRecent: (_) {},
              organizations: organizations,
              onCreateOrganization: () {},
              onViewInvitations: () {},
            ),
          ),
        ),
      );
      expect(find.text('组织活动管理'), findsNothing);
      await tester.ensureVisible(find.text('商家工作台'));
      await tester.tap(find.text('商家工作台'));
      expect(selected, SidebarDestination.business);
      await tester.pumpWidget(const SizedBox.shrink());
      city.dispose();
      workspace.dispose();
      organizations.dispose();
    },
  );
}
