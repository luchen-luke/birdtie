import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'entity_action_contract_test.dart' show actionWire;
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/entity_peek_card.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:flutter/material.dart';
import 'now_context_query_api_test.dart' show onlineWire, onlineIntentID;
import 'package:birdtie_client/src/workspace/now_context_query_api.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('真实typed活动保留独立个人提醒：原ID一次POST删除，零RSVP', (t) async {
    const id = '22e9cd18-babb-4359-9d1e-53593bedff47';
    final a = PublicActivity(
      id: id,
      title: '合成原活动',
      hostLabel: '主办人',
      placeName: '',
      summary: '公开内容',
      startsAt: DateTime.utc(2030),
      endsAt: DateTime.utc(2030, 1, 1, 2),
      timeZone: 'UTC',
      schedule: '2030年1月1日',
      status: 'upcoming',
      source: const PublicSource(
        label: '本地fixture',
        maintainer: 'test',
        freshness: 'current',
        updatedAt: null,
      ),
      location: null,
    );
    final v = EntityActionView.decode(
      actionWire(ref: const EntityActionRef('activity', id)),
      const EntityActionRef('activity', id),
    );
    final item = AgentResultItem(
      entity: const AgentResultRef(type: 'activity', id: id),
      title: a.title,
      summary: a.summary,
      scope: 'AUTHORIZED_VIEW',
      detail: const AgentResultRef(type: 'activity', id: id),
      actions: v.actions,
      actionsSourceVersion: v.sourceVersion,
      actionsValidUntil: v.validUntil,
    );
    var present = false, posts = 0, deletes = 0, rsvp = 0;
    String? token = 'Bearer A';
    final client = MockClient((r) async {
      if (r.url.path.contains('participations')) rsvp++;
      if (r.method == 'POST') {
        posts++;
        present = true;
        expect(r.url.path, '/v1/me/activity-plans');
        expect(jsonDecode(r.body), {'activityId': id});
        return http.Response('{"data":{"id":"original-plan"}}', 201);
      }
      if (r.method == 'DELETE') {
        deletes++;
        present = false;
        expect(r.url.path, '/v1/me/activity-plans/original-plan');
        return http.Response('', 204);
      }
      return http.Response.bytes(
        utf8.encode(
          jsonEncode({
            'data': present
                ? [
                    {
                      'id': 'original-plan',
                      'activityId': id,
                      'title': a.title,
                      'status': 'upcoming',
                      'available': true,
                    },
                  ]
                : [],
          }),
        ),
        200,
      );
    });
    final plans = ActivityPlansController(
      authorizationHeader: () => token,
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    final ws = AgentWorkspaceController()
      ..task = const AgentTask(id: 'local', query: '找活动', status: 'COMPLETED')
      ..result = AgentResult(
        entities: [],
        activities: [a],
        places: [],
        note: '原活动',
        resultSet: AgentResultSet(
          id: 'local-result',
          status: 'COMPLETE',
          entities: [item.entity],
          items: [item],
          generatedAt: DateTime.now(),
        ),
      )
      ..setSheetExtent(AgentSheetExtent.medium);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: ws,
            plans: plans,
            availableHeight: 650,
            onOpenEntity: (_) {},
            onSuggestion: (_) {},
            onRetry: () {},
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.byTooltip('添加个人提醒'),
      100,
      scrollable: find.byType(Scrollable).last,
    );
    await t.tap(find.byTooltip('添加个人提醒'));
    await t.pumpAndSettle();
    expect(posts, 1);
    expect(rsvp, 0);
    expect(plans.contains(id), isTrue);
    await t.tap(find.byTooltip('移除个人提醒'));
    await t.pumpAndSettle();
    expect(deletes, 1);
    expect(plans.contains(id), isFalse);
    token = null;
    plans.clear();
    await t.pumpAndSettle();
    await t.tap(find.byTooltip('添加个人提醒'));
    await t.pumpAndSettle();
    expect(posts, 1);
    expect(rsvp, 0);
    await t.pumpWidget(const SizedBox());
    plans.dispose();
    ws.dispose();
    client.close();
  });
  for (final brightness in Brightness.values) {
    testWidgets('结果面板复用主题前景与表面 $brightness', (tester) async {
      final wire = NowOnlineResponse.decode(onlineWire());
      final ws = AgentWorkspaceController()
        ..task = wire.task
        ..result = wire.result()
        ..setSheetExtent(AgentSheetExtent.medium);
      final theme = ThemeData(brightness: brightness);
      await tester.pumpWidget(
        MaterialApp(
          theme: theme,
          home: Scaffold(
            body: AgentResultsSheet(
              workspace: ws,
              availableHeight: 700,
              onSuggestion: (_) {},
              onRetry: () {},
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final panel = tester.widget<AnimatedContainer>(
        find.byType(AnimatedContainer).first,
      );
      expect(
        (panel.decoration as BoxDecoration).color,
        theme.colorScheme.surface,
      );
      final answer = tester.widget<Text>(find.text(wire.answer));
      expect(answer.style?.color, theme.colorScheme.onSurface);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      ws.dispose();
    });
  }
  testWidgets('线上长中文卡片320宽大字仍可滚动打开，无未知提交重试', (tester) async {
    tester.view.physicalSize = const Size(320, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final raw = onlineWire();
    (raw['items'] as List).first['title'] = '一起线上阅读中文书籍并讨论不同文化背景的学习心得';
    final data = NowOnlineResponse.decode(raw);
    final ws = AgentWorkspaceController()
      ..task = data.task
      ..result = data.result()
      ..setSheetExtent(AgentSheetExtent.medium);
    String? opened;
    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 800),
            textScaler: TextScaler.linear(3),
          ),
          child: Scaffold(
            body: AgentResultsSheet(
              workspace: ws,
              availableHeight: 540,
              onSuggestion: (_) {},
              onRetry: () {},
              onOpenOnlineIntent: (id) => opened = id,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    final card = find.byKey(const Key('online-intent-$onlineIntentID'));
    final scroll = find
        .descendant(
          of: find.byType(AgentConversation),
          matching: find.byType(Scrollable),
        )
        .first;
    final position = tester.state<ScrollableState>(scroll).position;
    final previousOffset = position.pixels;
    // A 688dp card cannot fit in this 202dp message viewport. Exercise a real
    // user drag instead of ensureVisible, which resets both nested offsets and
    // aligns the oversized card behind the source header.
    await tester.drag(scroll, const Offset(0, 120));
    await tester.pumpAndSettle();
    expect(position.pixels, lessThan(previousOffset));
    expect(card, findsOneWidget);
    expect(tester.getSize(card).height, greaterThanOrEqualTo(48));
    final visibleCard = tester
        .getRect(card)
        .intersect(
          tester.getRect(find.byKey(const Key('agent-sheet-content'))),
        );
    expect(visibleCard.height, greaterThanOrEqualTo(48));
    await tester.tapAt(visibleCard.center);
    expect(opened, onlineIntentID);
    ws.requestError = '提交结果尚未确认，请从最近对话核实。';
    ws.setSheetExtent(AgentSheetExtent.medium);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('agent-sheet-retry')), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    ws.dispose();
  });
  testWidgets('线上回答先于稳定意图卡片且不显示地图空态', (tester) async {
    final wire = NowOnlineResponse.decode(onlineWire());
    final ws = AgentWorkspaceController()
      ..task = wire.task
      ..result = wire.result()
      ..setSheetExtent(AgentSheetExtent.medium);
    String? id;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: ws,
            onSuggestion: (_) {},
            onRetry: () {},
            onOpenOnlineIntent: (x) => id = x,
            availableHeight: 800,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text(wire.answer), findsOneWidget);
    expect(find.text('一起线上阅读'), findsOneWidget);
    expect(find.text('当前没有可展示的结果。'), findsNothing);
    expect(
      tester.getTopLeft(find.text(wire.answer)).dy,
      lessThan(tester.getTopLeft(find.text('一起线上阅读')).dy),
    );
    await tester.tap(find.byKey(const Key('online-intent-$onlineIntentID')));
    expect(id, onlineIntentID);
    await tester.pumpWidget(const SizedBox());
    ws.dispose();
  });
  testWidgets('关系工具回答与审阅动作独立于公开地图结果空态', (tester) async {
    final workspace = AgentWorkspaceController()
      ..task = const AgentTask(
        id: 'relationship-task',
        query: '我的关系信号',
        status: 'COMPLETED',
        intent: 'PERSONAL_RELATIONSHIP_CONTEXT',
        principalType: 'person',
      )
      ..result = const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '仅使用本人授权的互动记录',
        message: '关系信号可供本人审阅',
        actions: [
          AgentAction(type: 'OPEN_RELATIONSHIP_CONTEXT', label: '查看关系信号'),
        ],
      )
      ..setSheetExtent(AgentSheetExtent.medium);
    String? opened;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: workspace,
            onSuggestion: (_) {},
            onRetry: () {},
            onAction: (action) => opened = action.type,
            availableHeight: 700,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('我的关系信号'), findsOneWidget);
    expect(find.text('本人授权 · 最近 30 天的互动记录'), findsOneWidget);
    expect(find.text('当前没有可展示的结果。'), findsNothing);
    expect(find.textContaining('0 个活动'), findsNothing);
    await tester.tap(find.text('查看关系信号'));
    expect(opened, 'OPEN_RELATIONSHIP_CONTEXT');
    await tester.pumpWidget(const SizedBox());
    workspace.dispose();
  });
  testWidgets('Agent 地点结果没有坐标仍可打开同一地点详情', (tester) async {
    const placeID = '11111111-1111-4111-8111-111111111111';
    final workspace = AgentWorkspaceController()
      ..task = const AgentTask(
        id: 'task-place',
        query: '找地点',
        status: 'COMPLETED',
      )
      ..result = const AgentResult(
        entities: [],
        activities: [],
        places: [
          PublicPlace(
            id: placeID,
            name: '无公开坐标的地点',
            categoryCode: 'other',
            summary: '',
            source: PublicSource(
              label: '城市资料',
              maintainer: '',
              freshness: 'current',
              updatedAt: null,
            ),
            location: PublicPlaceLocation(
              coordinateSystem: '',
              precision: 'none',
              latitude: null,
              longitude: null,
            ),
          ),
        ],
        note: '地点结果',
      )
      ..setSheetExtent(AgentSheetExtent.medium);
    String? opened;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: workspace,
            onOpenPlace: (id) => opened = id,
            onSuggestion: (_) {},
            onRetry: () {},
            availableHeight: 700,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('无公开坐标的地点'));
    expect(opened, placeID);
    workspace.dispose();
  });

  testWidgets('只有已完成个人找活动查询显示社交草稿入口', (tester) async {
    final workspace = AgentWorkspaceController()
      ..task = const AgentTask(
        id: 'task-social',
        query: '找周末羽毛球',
        status: 'COMPLETED',
        intent: 'FIND_ACTIVITY',
        principalType: 'person',
      )
      ..result = const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '活动',
      )
      ..setSheetExtent(AgentSheetExtent.medium);
    AgentTask? saved;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: workspace,
            onSaveSocialIntent: (task) => saved = task,
            onSuggestion: (_) {},
            onRetry: () {},
            availableHeight: 700,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存为社交意图草稿'));
    expect(saved?.id, 'task-social');
    workspace.task = const AgentTask(
      id: 'task-org',
      query: '找活动',
      status: 'COMPLETED',
      intent: 'FIND_ACTIVITY',
      principalType: 'organization',
    );
    workspace.notifyListeners();
    await tester.pumpAndSettle();
    expect(find.text('保存为社交意图草稿'), findsNothing);
    workspace.dispose();
  });

  testWidgets(
    'result sheet moves through explicit peek, results and chat detents',
    (tester) async {
      final workspace = AgentWorkspaceController()
        ..task = const AgentTask(
          id: 'task-detents',
          query: '找周末羽毛球',
          status: 'COMPLETED',
        )
        ..result = const AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: '已完成的合成查询',
        );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Align(
              alignment: Alignment.bottomCenter,
              child: AgentResultsSheet(
                workspace: workspace,
                onSuggestion: (_) {},
                onRetry: () {},
                availableHeight: 700,
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final handle = find.byKey(const Key('agent-sheet-handle'));
      expect(workspace.sheetExtent, AgentSheetExtent.peek);
      final peek = find.byType(AgentResultsSheet);
      final expandSummary = find.byKey(const Key('agent-sheet-expand-summary'));
      expect(expandSummary.hitTestable(), findsOneWidget);
      expect(tester.getSize(expandSummary).height, greaterThanOrEqualTo(48));
      final mapToggle = find.byKey(const Key('agent-sheet-map-toggle'));
      expect(mapToggle.hitTestable(), findsOneWidget);
      expect(find.byTooltip('展开对话').hitTestable(), findsOneWidget);
      expect(tester.getSize(mapToggle).height, greaterThanOrEqualTo(48));
      expect(tester.getSize(peek).height, greaterThanOrEqualTo(104));
      expect(tester.getSize(peek).height, lessThan(301));

      await tester.tap(handle);
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.medium);
      expect(tester.getSize(find.byType(AgentResultsSheet)).height, 301);

      await tester.drag(handle, const Offset(0, -120));
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      expect(
        tester.getSize(find.byType(AgentResultsSheet)).height,
        700.0.clamp(0.0, tester.getSize(find.byType(Scaffold)).height),
      );
      expect(workspace.contentMode, AgentContentMode.results);
      expect(find.byType(AgentConversation), findsOneWidget);
      final task = workspace.task, result = workspace.result;
      await tester.tap(mapToggle);
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.peek);
      await tester.tap(mapToggle);
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      expect(workspace.task, same(task));
      expect(workspace.result, same(result));
      expect(find.byType(AgentConversation), findsOneWidget);

      await tester.tap(handle);
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.medium);
      expect(workspace.contentMode, AgentContentMode.results);
      expect(find.byType(AgentConversation), findsOneWidget);
      await tester.drag(handle, const Offset(0, 120));
      await tester.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.peek);
      expect(tester.takeException(), isNull);
      workspace.dispose();
    },
  );

  testWidgets('authorized create action is exposed as a real callback', (
    tester,
  ) async {
    AgentAction? selected;
    final workspace = AgentWorkspaceController()
      ..task = const AgentTask(
        id: 'task-create',
        query: '创建活动',
        status: 'COMPLETED',
      )
      ..result = const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '组织工作台',
        message: '可以创建活动草稿。',
        actions: [
          AgentAction(
            type: 'OPEN_ORGANIZATION_CONSOLE',
            label: '去创建活动',
            targetType: 'organization',
            targetID: 'org-1',
          ),
        ],
      )
      ..setSheetExtent(AgentSheetExtent.medium);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: workspace,
            onAction: (action) => selected = action,
            onSuggestion: (_) {},
            onRetry: () {},
            availableHeight: 700,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('去创建活动'));
    expect(selected?.targetID, 'org-1');
    workspace.dispose();
  });
  testWidgets(
    'organization search shows real organization without activity empty state',
    (tester) async {
      final workspace = AgentWorkspaceController()
        ..task = const AgentTask(
          id: 'task-organization',
          query: '找组织',
          status: 'COMPLETED',
        )
        ..result = const AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: '公开组织',
          message: '找到 1 个公开组织。',
          organizations: [
            AgentOrganization(
              id: 'org-1',
              name: '华人学生会',
              description: '学生社团',
              verificationStatus: 'unverified',
            ),
          ],
        )
        ..setSheetExtent(AgentSheetExtent.medium);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentResultsSheet(
              workspace: workspace,
              onSuggestion: (_) {},
              onRetry: () {},
              availableHeight: 700,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('华人学生会'), findsOneWidget);
      expect(find.textContaining('当前没有可展示'), findsNothing);
      workspace.dispose();
    },
  );
  testWidgets('long entity title remains readable in the result sheet', (
    tester,
  ) async {
    const title =
        'A long community badminton session with beginner coaching, equipment and refreshments';
    final workspace = AgentWorkspaceController()
      ..task = const AgentTask(id: 'task-1', query: '找活动', status: 'COMPLETED')
      ..result = AgentResult(
        entities: const [],
        activities: [
          PublicActivity(
            id: 'activity-1',
            hostLabel: 'Birdtie',
            placeName: 'Aberdeen Sports Centre',
            title: title,
            summary: 'Open to all',
            startsAt: DateTime.utc(2026, 10, 3, 10),
            endsAt: DateTime.utc(2026, 10, 3, 12),
            timeZone: 'Europe/London',
            schedule: 'Sat · 10:00',
            status: 'upcoming',
            source: PublicSource(
              label: 'Birdtie',
              maintainer: 'Birdtie',
              freshness: 'verified',
              updatedAt: null,
            ),
            location: null,
          ),
        ],
        places: const [],
        note: '找到 1 个公开活动。',
        message: '这是活动结果之前显示的智能体回复。',
      )
      ..setSheetExtent(AgentSheetExtent.medium);

    // The unified stream renders real conversation messages; a task query is
    // metadata and is not a synthetic user message.
    workspace.conversation.add(const AgentMessage(role: 'user', text: '找活动'));

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Align(
            alignment: Alignment.bottomCenter,
            child: AgentResultsSheet(
              workspace: workspace,
              onSuggestion: (_) {},
              onRetry: () {},
              availableHeight: 700,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text(title), findsOneWidget);
    expect(find.text('这是活动结果之前显示的智能体回复。'), findsOneWidget);
    expect(
      tester.getTopLeft(find.text('这是活动结果之前显示的智能体回复。')).dy,
      lessThan(tester.getTopLeft(find.text(title)).dy),
    );
    expect(tester.takeException(), isNull);

    workspace.setSheetExtent(AgentSheetExtent.expanded);
    await tester.pumpAndSettle();
    expect(find.text('找活动'), findsOneWidget);
    expect(tester.takeException(), isNull);
    workspace.dispose();
  });

  testWidgets('entity peek wraps long titles to two lines and stays separate', (
    tester,
  ) async {
    const title = '一段很长的活动标题，用于确认地图预览仍然易读并且不会挤压查看操作';
    var opened = false;
    const entity = MapEntity(
      id: 'activity:test',
      kind: MapEntityKind.activity,
      title: title,
      subtitle: '即将开始',
      latitude: 57.14,
      longitude: -2.1,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 360,
              child: EntityPeekCard(
                entity: entity,
                onOpen: () => opened = true,
              ),
            ),
          ),
        ),
      ),
    );
    final titleWidget = tester.widget<Text>(find.text(title));
    expect(titleWidget.maxLines, 2);
    await tester.tap(find.text('查看'));
    expect(opened, isTrue);
  });

  testWidgets('area pulse reflects in-bounds activities and requires a tap', (
    tester,
  ) async {
    final visible = PublicActivity(
      id: 'visible',
      hostLabel: '',
      placeName: '',
      title: '周末社交活动',
      summary: '',
      startsAt: DateTime.utc(2026, 10, 3),
      endsAt: DateTime.utc(2026, 10, 3, 2),
      timeZone: 'Europe/London',
      schedule: '',
      status: 'upcoming',
      source: PublicSource(
        label: '',
        maintainer: '',
        freshness: '',
        updatedAt: null,
      ),
      location: const PublicPlaceLocation(
        coordinateSystem: 'wgs84',
        precision: 'point',
        latitude: 57.14,
        longitude: -2.1,
      ),
    );
    const bounds = MapBounds(west: -2.2, south: 57, east: -2, north: 57.2);
    String? query;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AreaPulseStack(
            pulse: NowPulse(
              cityID: 'aberdeen-gb',
              bounds: bounds,
              status: 'populated',
              total: 1,
              categories: const [PulseCategory(code: 'badminton', count: 1)],
              activities: [visible],
              truncated: false,
            ),
            bounds: bounds,
            loading: false,
            onSearch: (value) => query = value,
          ),
        ),
      ),
    );
    expect(find.text('当前区域 · 1'), findsOneWidget);
    expect(find.text('羽毛球 · 1'), findsOneWidget);
    expect(query, isNull);
    await tester.tap(find.text('羽毛球 · 1'));
    expect(query, '找羽毛球活动');
  });

  testWidgets('Local Pulse shows no more than three compact facts', (
    tester,
  ) async {
    const bounds = MapBounds(west: -2.2, south: 57, east: -2, north: 57.2);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AreaPulseStack(
            pulse: const NowPulse(
              cityID: 'aberdeen-gb',
              bounds: bounds,
              status: 'populated',
              total: 7,
              categories: [
                PulseCategory(code: 'badminton', count: 3),
                PulseCategory(code: 'sports', count: 2),
                PulseCategory(code: 'culture', count: 1),
              ],
              activities: [],
              truncated: false,
            ),
            bounds: bounds,
            loading: false,
            onSearch: (_) {},
          ),
        ),
      ),
    );
    expect(find.text('当前区域 · 7'), findsOneWidget);
    expect(find.text('羽毛球 · 3'), findsOneWidget);
    expect(find.text('运动 · 2'), findsOneWidget);
    expect(find.text('文化 · 1'), findsNothing);
  });
}
