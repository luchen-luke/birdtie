import 'package:birdtie_client/src/workspace/public_query_field_evidence_panel.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'public_query_field_evidence_test.dart' show evidenceData, readEvidence;

void main() {
  testWidgets('source label link and unverified warning expire together without granting authority', (t) async {
    final data = evidenceData();
    final source = (data['activities'] as List).single['source'] as Map<String, dynamic>;
    source['label'] = '样例公开活动来源';
    source['reference'] = 'https://public-source.example/activity';
    source['freshness'] = 'unverified';
    final evidence = readEvidence(data);
    final changes = ChangeNotifier(); addTearDown(changes.dispose);
    final item = decodeAgentResultItems(data['resultSet']).single;
    await t.pumpWidget(MaterialApp(home: Scaffold(body: PublicQueryFieldEvidencePanel(
      evidence: evidence, item: item, current: (_) => true, changes: changes,
    ))));
    await t.tap(find.text('查看来源与时间')); await t.pump();
    expect(find.text('样例公开活动来源'), findsOneWidget);
    expect(find.text('打开来源'), findsOneWidget);
    expect(find.text('此来源尚未核实。'), findsOneWidget);
    expect(find.textContaining('读取期限'), findsNothing);
    expect(find.textContaining('大小限制'), findsNothing);
    await t.pump(const Duration(seconds: 21));
    expect(find.text('打开来源'), findsNothing);
    expect(find.text('样例公开活动来源'), findsNothing);
    expect(evidence.current, isFalse);
    await t.pumpWidget(const SizedBox.shrink());
  });
  testWidgets('empty history has no empty heading and failures never display arbitrary exception text', (t) async {
    final workspace = AgentWorkspaceController();
    final city = PublicCityController(apiBaseUrl: '');
    final organizations = OrganizationWorkspaceController(authorizationHeader: () => null);
    addTearDown(workspace.dispose); addTearDown(city.dispose); addTearDown(organizations.dispose);
    Widget view() => MaterialApp(home: Scaffold(body: Sidebar(
      workspace: workspace, city: city, organizations: organizations, signedIn: false,
      onCitySelected: (_) {}, onNew: () {}, onDestination: (_) {}, onRecent: (_) {},
      onCreateOrganization: () {}, onViewInvitations: () {},
    )));
    await t.pumpWidget(view()); await t.pump();
    expect(find.text('最近对话'), findsNothing);
    expect(find.text('你开始的对话会显示在这里。'), findsNothing);
    workspace.recentError = 'INTERNAL_ONLY stack trace requestId secret=sample';
    await t.pumpWidget(view()); await t.pump();
    expect(find.text('最近对话加载失败。'), findsOneWidget);
    expect(find.textContaining('INTERNAL_ONLY'), findsNothing);
    expect(find.text('重试'), findsOneWidget);
    expect(t.getSize(find.widgetWithText(TextButton, '重试')).height, greaterThanOrEqualTo(48));
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
  });
}
