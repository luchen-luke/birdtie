import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_run_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_run_api_test.dart';
import 'agent_run_pending_store_test.dart' show TestRunPendingStore;

void main() {
  testWidgets('本人状态页小屏默认文字检查原版本后仅一次取消，成功只是候选处理', (t) async {
    t.view.physicalSize = const Size(360, 740);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    var posts = 0;
    var cancelled = false;
    final auth = SeedTestAuth()..owner = runOwner;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        expect(r.url.path, '/v1/me/agent-runs/$runID/cancel');
        expect(jsonDecode(r.body), {'expectedVersion': 1});
        posts++;
        cancelled = true;
      }
      return runResponse(
        r.url.path.endsWith('/page')
            ? runPage()
            : runRecord(
                state: cancelled ? 'CANCELLED' : 'WAITING_CONFIRMATION',
                version: cancelled ? 2 : 1,
              ),
      );
    });
    try {
      await t.pumpWidget(
        MaterialApp(
          home: AgentRunPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://local',
            pendingStore: TestRunPendingStore(),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('智能体处理状态'), findsOneWidget);
      expect(posts, 0);
      await t.tap(find.text('等待明确许可'));
      await t.pumpAndSettle();
      await t.scrollUntilVisible(find.text('检查并停止'), 150);
      await t.tap(find.text('检查并停止'));
      await t.pumpAndSettle();
      expect(find.textContaining('原文字与已经形成的候选保留'), findsOneWidget);
      await t.tap(find.text('确认停止'));
      await t.pumpAndSettle();
      expect(posts, 1);
      expect(find.textContaining('当前状态：已取消'), findsWidgets);
      expect(t.takeException(), null);
    } finally {
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    }
  });
  testWidgets('组织边界不读取本人Run', (t) async {
    final auth = SeedTestAuth()..owner = runOwner;
    var calls = 0;
    final client = MockClient((r) async {
      calls++;
      return runResponse(runPage());
    });
    try {
      await t.pumpWidget(
        MaterialApp(
          home: AgentRunPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://local',
            organizationWorkspaceID: () => runID,
            pendingStore: TestRunPendingStore(),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(calls, 0);
      expect(find.textContaining('账号或来源已变化'), findsOneWidget);
    } finally {
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    }
  });
}
