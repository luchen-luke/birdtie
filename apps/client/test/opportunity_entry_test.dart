import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _ownerTask = AgentTask(
  id: 'owned-completed-activity-query',
  query: '找周末羽毛球',
  status: 'COMPLETED',
  intent: 'FIND_ACTIVITY',
  principalType: 'person',
  principalID: 'person-owner',
  filters: {'category': 'badminton', 'timePreference': 'weekend'},
);

const _emptyResult = AgentResult(
  entities: [],
  activities: [],
  places: [],
  note: '当前公开查询没有活动；你可独立查看本人有效意图的机会。',
);

class _CountingAgentSource extends AgentTaskSource {
  int queries = 0;

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    queries++;
    return _emptyResult;
  }
}

Future<void> _mountSheet(
  WidgetTester tester,
  AgentWorkspaceController workspace, {
  ValueChanged<AgentAction>? onAction,
  ValueChanged<AgentTask>? onSaveSocialIntent,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Align(
          alignment: Alignment.bottomCenter,
          child: AgentResultsSheet(
            workspace: workspace,
            onAction: onAction,
            onSaveSocialIntent: onSaveSocialIntent,
            onSuggestion: (_) {},
            onRetry: () {},
            availableHeight: 1000,
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('本人完成活动查询后只分发中文活动机会入口动作', (tester) async {
    final source = _CountingAgentSource();
    final workspace = AgentWorkspaceController(source: source)
      ..task = _ownerTask
      ..result = _emptyResult
      ..setSheetExtent(AgentSheetExtent.medium);
    final actions = <AgentAction>[];
    var drafts = 0;
    await _mountSheet(
      tester,
      workspace,
      onAction: actions.add,
      onSaveSocialIntent: (_) => drafts++,
    );
    expect(find.text('查看我的活动机会'), findsOneWidget);
    expect(actions, isEmpty);
    expect(source.queries, 0);
    await tester.ensureVisible(find.text('查看我的活动机会'));
    await tester.tap(find.text('查看我的活动机会'));
    await tester.pumpAndSettle();
    expect(actions, hasLength(1));
    expect(actions.single.type, 'OPEN_OPPORTUNITIES');
    expect(actions.single.label, '查看我的活动机会');
    expect(actions.single.targetID, isNull);
    expect(actions.single.targetType, isNull);
    expect(source.queries, 0);
    expect(drafts, 0);
    expect(workspace.task, same(_ownerTask));
    expect(workspace.result, same(_emptyResult));
    expect(workspace.task!.status, 'COMPLETED');
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    workspace.dispose();
  });

  for (final task in const [
    AgentTask(
      id: 'anonymous',
      query: '找活动',
      status: 'COMPLETED',
      intent: 'FIND_ACTIVITY',
      principalType: 'person',
    ),
    AgentTask(
      id: 'organization',
      query: '找活动',
      status: 'COMPLETED',
      intent: 'FIND_ACTIVITY',
      principalType: 'organization',
      principalID: 'organization-owner',
    ),
    AgentTask(
      id: 'new-people',
      query: '找新朋友',
      status: 'COMPLETED',
      intent: 'FIND_NEW_PEOPLE',
      principalType: 'person',
      principalID: 'person-owner',
    ),
    AgentTask(
      id: 'failed',
      query: '找活动',
      status: 'FAILED',
      intent: 'FIND_ACTIVITY',
      principalType: 'person',
      principalID: 'person-owner',
    ),
    AgentTask(
      id: 'active',
      query: '找活动',
      status: 'ACTIVE',
      intent: 'FIND_ACTIVITY',
      principalType: 'person',
      principalID: 'person-owner',
    ),
  ]) {
    testWidgets('${task.id} 不显示本人活动机会入口', (tester) async {
      final workspace = AgentWorkspaceController()
        ..task = task
        ..result = _emptyResult
        ..setSheetExtent(AgentSheetExtent.medium);
      var opened = 0;
      await _mountSheet(tester, workspace, onAction: (_) => opened++);
      expect(find.text('查看我的活动机会'), findsNothing);
      expect(opened, 0);
      await tester.pumpWidget(const SizedBox());
      workspace.dispose();
    });
  }

  testWidgets('没有消费回调或没有结果时不提供无效入口', (tester) async {
    final workspace = AgentWorkspaceController()
      ..task = _ownerTask
      ..result = _emptyResult
      ..setSheetExtent(AgentSheetExtent.medium);
    await _mountSheet(tester, workspace);
    expect(find.text('查看我的活动机会'), findsNothing);
    workspace.result = null;
    await _mountSheet(tester, workspace, onAction: (_) {});
    expect(find.text('查看我的活动机会'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    workspace.dispose();
  });

  testWidgets('活动机会入口保留旧草稿回调且不自动激活意图', (tester) async {
    final source = _CountingAgentSource();
    final workspace = AgentWorkspaceController(source: source)
      ..task = _ownerTask
      ..result = _emptyResult
      ..setSheetExtent(AgentSheetExtent.medium);
    final savedDrafts = <AgentTask>[];
    var actions = 0;
    await _mountSheet(
      tester,
      workspace,
      onAction: (_) => actions++,
      onSaveSocialIntent: savedDrafts.add,
    );
    expect(find.text('保存为社交意图草稿'), findsOneWidget);
    expect(savedDrafts, isEmpty);
    await tester.tap(find.text('保存为社交意图草稿'));
    await tester.pumpAndSettle();
    expect(savedDrafts, hasLength(1));
    expect(savedDrafts.single, same(_ownerTask));
    expect(actions, 0);
    expect(source.queries, 0);
    expect(workspace.task!.status, 'COMPLETED');
    expect(workspace.task!.filters, same(_ownerTask.filters));
    expect(find.text('确认发布'), findsNothing);
    expect(find.text('确认启用'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    workspace.dispose();
  });

  testWidgets('单纯查看机会不改地图实体相机选择或执行其他操作', (tester) async {
    const entity = MapEntity(
      id: 'activity:synthetic-opportunity-entry',
      kind: MapEntityKind.activity,
      title: '合成公开活动',
      subtitle: '即将开始',
      latitude: 57.14,
      longitude: -2.1,
    );
    const result = AgentResult(
      entities: [entity],
      activities: [],
      places: [],
      note: '用于核验入口对当前地图结果没有副作用。',
      mapEffects: AgentMapEffects(
        camera: 'preserve',
        pinEntityIDs: ['activity:synthetic-opportunity-entry'],
      ),
    );
    const bounds = MapBounds(west: -2.2, south: 57, east: -2, north: 57.2);
    final source = _CountingAgentSource();
    final workspace = AgentWorkspaceController(source: source)
      ..task = _ownerTask
      ..result = result
      ..selectedEntityId = entity.id
      ..setSheetExtent(AgentSheetExtent.medium);
    final mapState = MapViewportState()..cameraSettled(bounds);
    final city = PublicCityController();
    var httpCalls = 0;
    final plans = ActivityPlansController(
      authorizationHeader: () => 'Bearer synthetic-widget-session',
      apiBaseUrl: 'http://widget-test.invalid',
      client: MockClient((_) async {
        httpCalls++;
        return http.Response('{}', 500);
      }),
    );
    final discovery = NowDiscoveryController(
      authorizationHeader: () => 'Bearer synthetic-widget-session',
      apiBaseUrl: 'http://widget-test.invalid',
      client: MockClient((_) async {
        httpCalls++;
        return http.Response('{}', 500);
      }),
    );
    var navigation = 0;
    var invitations = 0;
    var activityOpens = 0;
    var suggestions = 0;
    var retries = 0;
    var drafts = 0;
    var cameraNotifications = 0;
    var workspaceNotifications = 0;
    mapState.addListener(() => cameraNotifications++);
    workspace.addListener(() => workspaceNotifications++);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Stack(
            children: [
              Positioned.fill(
                child: MapCanvas(
                  city: city,
                  workspace: workspace,
                  mapState: mapState,
                  discovery: discovery,
                  onInitialViewport: mapState.initializeViewport,
                  onMapUnavailable: mapState.mapUnavailable,
                ),
              ),
              Align(
                alignment: Alignment.bottomCenter,
                child: AgentResultsSheet(
                  workspace: workspace,
                  plans: plans,
                  onAction: (_) => navigation++,
                  onContact: (_) => invitations++,
                  onOpenActivity: (_) => activityOpens++,
                  onSaveSocialIntent: (_) => drafts++,
                  onSuggestion: (_) => suggestions++,
                  onRetry: () => retries++,
                  availableHeight: 1000,
                ),
              ),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    final mapElement = find.byType(MapCanvas).evaluate().single;
    final mapWidget = tester.widget<MapCanvas>(find.byType(MapCanvas));
    await tester.ensureVisible(find.text('查看我的活动机会'));
    await tester.tap(find.text('查看我的活动机会'));
    await tester.pumpAndSettle();
    expect(navigation, 1);
    expect(source.queries, 0);
    expect(httpCalls, 0);
    expect(invitations, 0);
    expect(activityOpens, 0);
    expect(suggestions, 0);
    expect(retries, 0);
    expect(drafts, 0);
    expect(workspaceNotifications, 0);
    expect(cameraNotifications, 0);
    expect(workspace.selectedEntityId, entity.id);
    expect(workspace.task, same(_ownerTask));
    expect(workspace.result, same(result));
    expect(workspace.result!.entities.single, same(entity));
    expect(workspace.result!.mapEffects!.camera, 'preserve');
    expect(mapState.viewportBounds, bounds);
    expect(mapState.searchAreaBounds, bounds);
    expect(mapState.cameraMoving, isFalse);
    expect(find.byType(MapCanvas).evaluate().single, same(mapElement));
    expect(tester.widget<MapCanvas>(find.byType(MapCanvas)), same(mapWidget));
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    workspace.dispose();
    mapState.dispose();
    city.dispose();
    discovery.dispose();
    plans.dispose();
  });
}
