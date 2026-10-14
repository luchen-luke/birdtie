import 'dart:convert';

import 'package:birdtie_client/src/workspace/activity_participations.dart';
import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:birdtie_client/src/workspace/agent_run_page.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_run_api_test.dart';
import 'agent_run_pending_store_test.dart' show TestRunPendingStore;
import 'public_intent_identity_test.dart' show ProfileCity;

void main() {
  testWidgets('正常字号真实Plans空数据只显示一次暂无活动，空分组退场且城市创建边界可见', (t) async {
    final auth = SeedTestAuth()..owner = runOwner;
    final city = ProfileCity();
    final calls = <http.Request>[];
    final client = MockClient((request) async {
      calls.add(request);
      expect(request.method, 'GET');
      expect(request.bodyBytes, isEmpty);
      return http.Response(
        jsonEncode({'data': <Object>[]}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    String? token() => auth.authorizationHeader;
    const base = 'https://reachable-plans.test';
    final plans = ActivityPlansController(
      client: client,
      apiBaseUrl: base,
      authorizationHeader: token,
    );
    final participation = ActivityParticipationController(
      client: client,
      apiBaseUrl: base,
      authorizationHeader: token,
    );
    final saved = SavedController(
      client: client,
      apiBaseUrl: base,
      authorizationHeader: token,
    );
    final organizations = OrganizationWorkspaceController(
      client: client,
      apiBaseUrl: base,
      authorizationHeader: token,
    );
    try {
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MyActivitiesPage(
              plans: plans,
              participations: participation,
              saved: saved,
              auth: auth,
              city: city,
              organizations: organizations,
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('暂无活动'), findsOneWidget);
      for (final label in ['即将参加', '收藏的活动', '已结束', '个人提醒']) {
        expect(find.text(label), findsNothing);
      }
      expect(find.text('目前仅支持在阿伯丁发起活动。'), findsOneWidget);
      final create = find.widgetWithText(FilledButton, '发起活动');
      expect(t.widget<FilledButton>(create).onPressed, isNull);
      expect(calls, isNotEmpty);
      expect(t.takeException(), isNull);
    } finally {
      await t.pumpWidget(const SizedBox());
      plans.dispose();
      participation.dispose();
      saved.dispose();
      organizations.dispose();
      city.dispose();
      auth.dispose();
      client.close();
    }
  });

  testWidgets('正常字号处理成功仍是待确认候选，终态不显示停止操作且读取零写', (t) async {
    final auth = SeedTestAuth()..owner = runOwner;
    final calls = <http.Request>[];
    final record = runRecord(state: 'SUCCEEDED', version: 2);
    final client = MockClient((request) async {
      calls.add(request);
      expect(request.method, 'GET');
      expect(request.bodyBytes, isEmpty);
      return runResponse(
        request.url.path.endsWith('/page') ? runPage(items: [record]) : record,
      );
    });
    try {
      await t.pumpWidget(
        MaterialApp(
          home: AgentRunPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'https://reachable-run.test',
            pendingStore: TestRunPendingStore(),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.tap(find.byType(ListTile));
      await t.pumpAndSettle();
      expect(find.text('已形成待检查的候选，是否保存为本人记忆仍由你另行确认。'), findsOneWidget);
      expect(find.text('检查并停止'), findsNothing);
      expect(calls.length, 2);
      expect(t.takeException(), isNull);
    } finally {
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    }
  });
}
