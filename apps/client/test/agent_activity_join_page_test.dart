import 'package:birdtie_client/src/workspace/agent_activity_join_page.dart';
import 'package:birdtie_client/src/workspace/agent_activity_join_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'agent_activity_join_api_test.dart';
import 'agent_activity_join_controller_test.dart' show JoinHarness;
import 'agent_activity_join_pending_store_test.dart' show joinPending;

Future<void> joinPump(WidgetTester tester) async {
  await tester.runAsync(() async {
    await Future<void>.delayed(Duration.zero);
  });
  await tester.pump();
  await tester.runAsync(() async {
    await Future<void>.delayed(Duration.zero);
  });
  await tester.pump();
}

AgentActivityJoinPage joinPage(
  JoinHarness h, {
  Listenable? changes,
  bool recoveryOnly = false,
}) => AgentActivityJoinPage(
  authorizationHeader: () => h.token,
  ownerID: () => h.owner,
  organizationWorkspaceID: () => h.organization,
  sourceCurrent: () => h.source,
  identityChanges: changes,
  client: h.client,
  apiBaseUrl: 'http://local',
  selection: recoveryOnly ? null : joinSelection(),
  pendingStore: h.store,
  now: () => h.now,
  elapsed: () => h.elapsed,
);
void main() {
  testWidgets(
    'unknown personal organizer remains a distinct personal host and never becomes the applicant',
    (tester) async {
      final h = JoinHarness();
      h.intercept = (request) async {
        if (!request.url.path.endsWith('/previews')) return null;
        final review = joinReviewWire()
          ..['organizer'] = {'type': 'PERSON', 'id': joinOther, 'name': null}
          ..['hostLabel'] = null;
        final preview = joinPreviewForRequest(request)..['review'] = review;
        h.original = preview;
        return joinResponse(preview, status: 201);
      };
      await tester.pumpWidget(MaterialApp(home: joinPage(h)));
      await joinPump(tester);
      expect(joinOther, isNot(joinOwner));
      expect(find.text('周末原生徒步活动'), findsOneWidget);
      expect(find.text('主办方：个人主办方（主办方名称未提供）'), findsOneWidget);
      expect(find.textContaining('本人主办方'), findsNothing);
      expect(find.text('主办方：本人名称未提供'), findsNothing);
      expect(find.widgetWithText(FilledButton, '确认并让智能体报名'), findsOneWidget);
      expect(h.calls('confirm'), 0);
      expect(h.calls('execute'), 0);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox.shrink());
      h.close();
    },
  );
  testWidgets(
    'normal narrow screen renders complete native conditions and reachable explicit intelligent-agent action without auto execute',
    (tester) async {
      tester.view.devicePixelRatio = 1;
      tester.view.physicalSize = const Size(320, 700);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.view.resetPhysicalSize);
      final h = JoinHarness();
      await tester.pumpWidget(MaterialApp(home: joinPage(h)));
      await joinPump(tester);
      expect(find.text('周末原生徒步活动'), findsOneWidget);
      expect(find.text(joinReviewWire()['summary']), findsOneWidget);
      expect(find.text(joinReviewWire()['description']), findsOneWidget);
      expect(find.textContaining('需自行确认体力适合'), findsOneWidget);
      expect(find.text('费用：原活动未明确费用'), findsOneWidget);
      expect(find.text('地点：原生公园东门'), findsOneWidget);
      expect(find.textContaining('Asia/Shanghai'), findsOneWidget);
      expect(find.textContaining('UTC'), findsNWidgets(2));
      expect(find.textContaining('这一步不会付款'), findsOneWidget);
      expect(h.calls('confirm'), 0);
      expect(h.calls('execute'), 0);
      final action = find.widgetWithText(FilledButton, '确认并让智能体报名');
      await tester.ensureVisible(action);
      await tester.pump();
      expect(tester.getSize(action).height, greaterThanOrEqualTo(48));
      await tester.tap(action);
      await joinPump(tester);
      expect(h.calls('confirm'), 1);
      expect(h.calls('execute'), 1);
      expect(find.textContaining('原报名操作已生效'), findsOneWidget);
      expect(find.text('周末原生徒步活动'), findsNothing);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox.shrink());
      h.close();
    },
  );
  testWidgets(
    'default OFF keeps truthful unavailable state and real reread action without capability toggle or fallback RSVP',
    (tester) async {
      final h = JoinHarness();
      h.intercept = (_) async =>
          http.Response('{"error":"agent_activity_join_unavailable"}', 503);
      await tester.pumpWidget(MaterialApp(home: joinPage(h)));
      await joinPump(tester);
      expect(find.textContaining('当前无法准备智能体报名'), findsOneWidget);
      expect(find.widgetWithText(FilledButton, '确认并让智能体报名'), findsNothing);
      expect(find.byType(Switch), findsNothing);
      final retry = find.text('重新读取报名预览');
      await tester.ensureVisible(retry);
      await tester.tap(retry);
      await joinPump(tester);
      expect(h.calls('previews'), 2);
      expect(h.calls('confirm'), 0);
      expect(h.calls('execute'), 0);
      expect(
        h.requests.any(
          (r) => !r.url.path.startsWith('/v1/me/agent-activity-join/'),
        ),
        false,
      );
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox.shrink());
      h.close();
    },
  );
  testWidgets(
    'profile recovery renders no journal target facts and committed stop reads causal receipt instead of claiming cancelled',
    (tester) async {
      final store = MemoryAgentActivityJoinPendingStore();
      await tester.runAsync(
        () => store.reserve('http://local', joinOwner, joinPending()),
      );
      final h = JoinHarness(pendingStore: store)..revokeCommitted = true;
      await tester.pumpWidget(
        MaterialApp(home: joinPage(h, recoveryOnly: true)),
      );
      await joinPump(tester);
      expect(find.text('核实原报名'), findsOneWidget);
      expect(find.text(joinActivity), findsNothing);
      expect(find.text('周末原生徒步活动'), findsNothing);
      expect(find.text('确认并让智能体报名'), findsNothing);
      expect(h.requests, isEmpty);
      final stop = find.text('结束并核实原批准');
      await tester.ensureVisible(stop);
      await tester.tap(stop);
      await joinPump(tester);
      expect(h.calls('revoke'), 1);
      expect(h.calls('reconcile'), 1);
      expect(h.calls('execute'), 0);
      expect(find.textContaining('原报名操作已生效'), findsOneWidget);
      expect(find.textContaining('已取消报名'), findsNothing);
      expect(find.text('周末原生徒步活动'), findsNothing);
      expect(await store.read('http://local', joinOwner), null);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox.shrink());
      h.close();
    },
  );
  testWidgets(
    'background permanently retires native review while theme rebuild preserves unmodified source',
    (tester) async {
      final h = JoinHarness(), page = joinPage(h);
      await tester.pumpWidget(MaterialApp(home: page));
      await joinPump(tester);
      expect(find.text('周末原生徒步活动'), findsOneWidget);
      await tester.pumpWidget(MaterialApp(theme: ThemeData.dark(), home: page));
      await tester.pump();
      expect(find.text('周末原生徒步活动'), findsOneWidget);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      await tester.pump();
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await tester.pump();
      expect(find.text('周末原生徒步活动'), findsNothing);
      expect(find.text('确认并让智能体报名'), findsNothing);
      expect(find.textContaining('本次批准已停用'), findsOneWidget);
      expect(h.calls('confirm'), 0);
      expect(h.calls('execute'), 0);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox.shrink());
      h.close();
    },
  );
  testWidgets(
    'source then owner ABA notifications clear original review and do not resurrect executable card',
    (tester) async {
      final h = JoinHarness(), changes = ChangeNotifier();
      await tester.pumpWidget(MaterialApp(home: joinPage(h, changes: changes)));
      await joinPump(tester);
      h.source = false;
      changes.notifyListeners();
      await tester.pump();
      h.source = true;
      changes.notifyListeners();
      await tester.pump();
      expect(find.text('周末原生徒步活动'), findsNothing);
      h.owner = joinOther;
      changes.notifyListeners();
      await tester.pump();
      h.owner = joinOwner;
      changes.notifyListeners();
      await tester.pump();
      expect(find.text('确认并让智能体报名'), findsNothing);
      expect(find.textContaining('身份或服务来源已变化'), findsOneWidget);
      expect(h.calls('execute'), 0);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox.shrink());
      changes.dispose();
      h.close();
    },
  );
  testWidgets(
    'unknown confirm presents original reconcile and fenced stop, without execute or repeated confirmation',
    (tester) async {
      final h = JoinHarness();
      h.intercept = (r) async =>
          r.url.path.endsWith('/confirm') ? http.Response('{}', 503) : null;
      await tester.pumpWidget(MaterialApp(home: joinPage(h)));
      await joinPump(tester);
      final action = find.text('确认并让智能体报名');
      await tester.ensureVisible(action);
      await tester.tap(action);
      await joinPump(tester);
      expect(find.text('核实原报名操作'), findsOneWidget);
      expect(find.text('结束并核实原批准'), findsOneWidget);
      expect(find.text('确认并让智能体报名'), findsNothing);
      expect(h.calls('confirm'), 1);
      expect(h.calls('execute'), 0);
      final stop = find.text('结束并核实原批准');
      await tester.ensureVisible(stop);
      await tester.tap(stop);
      await joinPump(tester);
      expect(find.textContaining('未由这次批准提交'), findsOneWidget);
      expect(h.calls('execute'), 0);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox.shrink());
      h.close();
    },
  );
}
