import 'dart:async';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:birdtie_client/src/workspace/now_context_query_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'map_layers_api_test.dart' as typed;
import 'now_scope_recovery_test.dart' show NowFixtureCity, NowFixtureSource;
import 'now_scope_recovery_test.dart' show NowFixture, nowField;
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/entity_peek_card.dart';

class _MapCity extends PublicCityController {
  @override
  PublicCity? get selectedCity => const PublicCity(
    id: 'test-city',
    name: '测试城市',
    region: '合成',
    contentStatus: 'building',
    source: PublicSource(
      label: '测试',
      maintainer: '测试',
      freshness: 'test',
      updatedAt: null,
    ),
    map: null,
  );
}

void main() {
  for (final mode in [AgentContentMode.results, AgentContentMode.conversation]) {
    testWidgets('地图来源选择从expanded$mode显露真实轻卡并保留任务与安全草稿', (t) async {
      t.view.physicalSize = const Size(1220, 2656);
      t.view.devicePixelRatio = 3.25;
      t.view.padding = const FakeViewPadding(top: 78);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      final f = _NativePinFixture();
      addTearDown(f.dispose);
      await f.mount(t);
      final ws = f.workspace(t);
      await ws.submit('badminton', [], [], cityID: f.city.selectedCity!.id);
      ws.showContent(AgentContentMode.conversation);
      if (mode == AgentContentMode.results) ws.showContent(mode);
      expect(ws.contentMode, mode);
      await t.enterText(nowField(), '尚未发送的安全地图草稿');
      FocusManager.instance.primaryFocus?.unfocus();
      await t.pumpAndSettle();
      expect(ws.sheetExtent, AgentSheetExtent.expanded);
      expect(find.byType(EntityPeekCard), findsNothing);
      final task = ws.task, result = ws.result, epoch = ws.taskEpoch;
      final history = List<AgentMessage>.of(ws.conversation);
      final map = find.byType(PublicCityMapView).evaluate().single;
      final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      final pin = view.entities.first;
      view.onEntitySelected!(pin);
      await t.pumpAndSettle();
      expect(ws.sheetExtent, AgentSheetExtent.peek);
      // Opening the editor expands the shared message flow in either view.
      expect(ws.contentMode, AgentContentMode.conversation);
      expect(ws.selectedEntityId, pin.id);
      expect(find.byType(EntityPeekCard).hitTestable(), findsOneWidget);
      expect(t.widget<EntityPeekCard>(find.byType(EntityPeekCard)).entity.id, pin.id);
      expect(ws.task, same(task));
      expect(ws.result, same(result));
      expect(ws.taskEpoch, epoch);
      expect(ws.conversation, history);
      expect(t.widget<TextField>(nowField()).controller!.text, '尚未发送的安全地图草稿');
      expect(identical(map, find.byType(PublicCityMapView).evaluate().single), isTrue);
      expect(f.requests, hasLength(1));
      ws.setSheetExtent(AgentSheetExtent.expanded);
      await t.pumpAndSettle();
      view.onEntitySelected!(pin);
      await t.pumpAndSettle();
      expect(ws.sheetExtent, AgentSheetExtent.peek);
      expect(ws.selectedEntityId, pin.id);
      expect(find.byType(EntityPeekCard).hitTestable(), findsOneWidget);
      expect(f.requests, hasLength(1));
      expect(ws.taskEpoch, epoch);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    });
  }

  testWidgets('地图来源旧callback未知ID或城市退役不修改当前选择或面板', (t) async {
    final f = _NativePinFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final ws = f.workspace(t);
    await ws.submit('badminton', [], [], cityID: f.city.selectedCity!.id);
    ws.showContent(AgentContentMode.conversation);
    await t.pumpAndSettle();
    final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    final pin = view.entities.first;
    const unknown = MapEntity(id: 'activity:unknown-stale', kind: MapEntityKind.activity,
        title: '旧未知引用', subtitle: '', latitude: 57, longitude: -2);
    final task = ws.task, result = ws.result, epoch = ws.taskEpoch;
    view.onEntitySelected!(unknown);
    expect(ws.selectedEntityId, isNull);
    expect(ws.sheetExtent, AgentSheetExtent.expanded);
    f.city.selection = null;
    view.onEntitySelected!(pin);
    expect(ws.selectedEntityId, isNull);
    expect(ws.sheetExtent, AgentSheetExtent.expanded);
    expect(ws.task, same(task));
    expect(ws.result, same(result));
    expect(ws.taskEpoch, epoch);
    expect(f.requests, hasLength(1));
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  });

  testWidgets('地图来源旧任务callback不能借相同ID覆盖新结果选择', (t) async {
    final f = _NativePinFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final ws = f.workspace(t);
    await ws.submit('badminton', [], [], cityID: f.city.selectedCity!.id);
    await t.pumpAndSettle();
    final oldView = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    final pin = oldView.entities.first;
    await ws.submit('badminton weekend', [], [], cityID: f.city.selectedCity!.id);
    ws.showContent(AgentContentMode.conversation);
    await t.pumpAndSettle();
    final task = ws.task, result = ws.result, epoch = ws.taskEpoch;
    oldView.onEntitySelected!(pin);
    expect(ws.selectedEntityId, isNull);
    expect(ws.sheetExtent, AgentSheetExtent.expanded);
    expect(ws.task, same(task));
    expect(ws.result, same(result));
    expect(ws.taskEpoch, epoch);
    final currentView = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(currentView.entities.first.id, pin.id);
    currentView.onEntitySelected!(currentView.entities.first);
    await t.pumpAndSettle();
    expect(ws.selectedEntityId, pin.id);
    expect(ws.sheetExtent, AgentSheetExtent.peek);
    expect(find.byType(EntityPeekCard).hitTestable(), findsOneWidget);
    expect(f.requests, hasLength(2));
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  });

  testWidgets('背景城市恢复只服务空闲CITY，任务和ONLINE不重复，投影不随输入重建', (t) async {
    final city = NowFixtureCity(selected: false);
    final source = NowFixtureSource(city);
    final ws = AgentWorkspaceController(source: source);
    final viewport = MapViewportState();
    final discovery = NowDiscoveryController(authorizationHeader: () => null);
    var chooses = 0, initialBounds = 0;
    await t.pumpWidget(
      MaterialApp(
        home: MapCanvas(
          city: city,
          workspace: ws,
          mapState: viewport,
          discovery: discovery,
          onInitialViewport: (_) => initialBounds++,
          onMapUnavailable: (_) {},
          onChooseCity: () => chooses++,
        ),
      ),
    );
    final map = find.byType(PublicCityMapView);
    final element = map.evaluate().single, state = t.state(map);
    void unscopedStable() {
      final view = t.widget<PublicCityMapView>(map);
      expect(view.city, isNull);
      expect(view.entities, isEmpty);
      expect(view.places, isEmpty);
      expect(view.contextKey, 'idle');
      expect(view.onViewportInitialized, isNull);
      expect(view.onViewportSettled, isNull);
      expect(identical(element, map.evaluate().single), true);
      expect(identical(state, t.state(map)), true);
      expect(initialBounds, 0);
      expect(source.queries, isEmpty);
      expect(viewport.viewportBounds, isNull);
    }

    expect(find.text('选择城市'), findsOneWidget);
    unscopedStable();
    final idleView = t.widget<PublicCityMapView>(map);
    ws.beginTyping();
    ws.stopTyping();
    await t.pump();
    expect(identical(idleView, t.widget<PublicCityMapView>(map)), true);
    expect(find.text('选择城市'), findsOneWidget);
    ws.requireCity('weekend');
    await t.pump();
    expect(find.text('选择城市'), findsNothing);
    expect(ws.queryState, AgentQueryState.needsScope);
    unscopedStable();
    final taskView = t.widget<PublicCityMapView>(map);
    ws.beginTyping();
    ws.stopTyping();
    ws.setSheetExtent(AgentSheetExtent.expanded);
    await t.pump();
    expect(identical(taskView, t.widget<PublicCityMapView>(map)), true);
    expect(find.text('选择城市'), findsNothing);
    ws.newTask();
    await t.pump();
    expect(find.text('选择城市'), findsOneWidget);
    unscopedStable();
    ws.queryContextType = 'ONLINE';
    ws.beginTyping();
    await t.pump();
    expect(find.text('选择城市'), findsNothing);
    unscopedStable();
    ws.queryContextType = 'CITY';
    ws.stopTyping();
    await t.pump();
    expect(find.text('选择城市'), findsOneWidget);
    await t.tap(find.text('选择城市'));
    await t.pump();
    expect(chooses, 1);
    unscopedStable();
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    discovery.dispose();
    viewport.dispose();
    ws.dispose();
    city.dispose();
  });

  for (final path in [
    'needsScope',
    'needsScopeRetry',
    'needsScopeArea',
    'selectedCity',
    'followUp',
  ]) {
    testWidgets(
      'actual captured anonymous public wire $path retains real city and only typed public pins',
      (t) async {
        final city = _CapturedMapCity(selected: !path.startsWith('needsScope'));
        final requests = <http.Request>[];
        final client = MockClient((request) async {
          requests.add(request);
          if (path == 'needsScopeRetry' && requests.length == 1) {
            return http.Response('{"error":{"code":"UNAVAILABLE"}}', 503);
          }
          expect(request.method, 'POST');
          expect(request.url.path, '/v1/cities/aberdeen-gb/agent/tasks');
          expect(request.headers.containsKey('Authorization'), false);
          expect(
            request.headers.containsKey('X-Birdtie-Organization-Workspace'),
            false,
          );
          return http.Response(
            _capturedAnonymousPublicWire,
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        });
        final source = RemoteAgentTaskSource(
          cityID: () => city.selectedCity?.id,
          authorizationHeader: () => null,
          client: client,
          apiBaseUrl: 'http://replayed-local-wire.test',
        );
        final ws = AgentWorkspaceController(source: source),
            map = MapViewportState(),
            discovery = NowDiscoveryController(authorizationHeader: () => null);
        // MapWorkspace always supplies its typed layer controller. An empty
        // current layer snapshot must not mask a typed Agent projection.
        final layerClient = MockClient((request) async {
          fail('No ambient layer request is needed to replay the Agent wire');
        });
        final layers = MapLayersController(
          api: MapLayersApi(
            client: layerClient,
            apiBaseUrl: 'http://replayed-local-wire.test',
          ),
          authorizationHeader: () => null,
          organizationWorkspaceID: () => null,
        )..cityID = 'aberdeen-gb';
        await t.pumpWidget(
          MaterialApp(
            home: MapCanvas(
              city: city,
              workspace: ws,
              mapState: map,
              discovery: discovery,
              layers: layers,
              onInitialViewport: (_) {},
              onMapUnavailable: (_) {},
            ),
          ),
        );
        final element = find.byType(PublicCityMapView).evaluate().single;
        String? pendingID;
        if (path.startsWith('needsScope')) {
          ws.requireCity('badminton');
          pendingID = ws.task!.id;
          expect(ws.task!.cityID, isNull);
          expect(requests, isEmpty);
          city.choose();
          await ws.resumeCity('aberdeen-gb', [], []);
          if (path == 'needsScopeRetry') {
            expect(ws.requestError, isNotNull);
            expect(ws.result, isNull);
            expect(ws.task!.cityID, isNull);
            expect(requests.single.body, '{"query":"badminton"}');
            await ws.retry();
            expect(requests.last.body, '{"query":"badminton"}');
          }
          expect(ws.task!.id, pendingID);
          expect(ws.task!.query, 'badminton');
          expect(requests.first.body, '{"query":"badminton"}');
          if (path == 'needsScopeArea') {
            const bounds = MapBounds(
              west: -2.2,
              south: 57.0,
              east: -2.0,
              north: 57.3,
            );
            await ws.searchThisArea(
              [],
              [],
              bounds: bounds,
              cityID: 'aberdeen-gb',
            );
            expect(ws.task!.id, pendingID);
            expect(
              jsonDecode(requests.last.body)['mapBounds'],
              bounds.toJson(),
            );
            // Successful public filters now supply finite canonical words.
            expect(jsonDecode(requests.last.body)['query'], '找活动 羽毛球. 搜索此区域');
            expect(ws.task!.filters['mapWest'], '${bounds.west}');
          }
        } else {
          await ws.submit('badminton', [], [], cityID: 'aberdeen-gb');
          if (path == 'followUp') {
            pendingID = ws.task!.id;
            await ws.submit('近一点的呢？', [], [], cityID: 'aberdeen-gb');
            expect(ws.task!.id, pendingID);
            expect(
              jsonDecode(requests.last.body)['query'],
              '找活动 羽毛球 全城. 近一点的呢？',
            );
          }
        }
        await t.pump();
        final result = ws.result!;
        expect(result.task, isNull);
        expect(result.taskID, isNull);
        expect(result.projectionItems, hasLength(3));
        final expectedIDs = {
          'activity:b1700000-0000-4000-8000-000000000005',
          'activity:b1700000-0000-4000-8000-000000000006',
          'activity:b1700000-0000-4000-8000-000000000017',
        };
        expect(result.entities.map((e) => e.id).toSet(), expectedIDs);
        final projection = t.widget<PublicCityMapView>(
          find.byType(PublicCityMapView),
        );
        expect(projection.entities.map((e) => e.id).toSet(), expectedIDs);
        expect(ws.task!.cityID, 'aberdeen-gb');
        expect(ws.recent.first.cityID, 'aberdeen-gb');
        expect(
          requests.length,
          ['followUp', 'needsScopeRetry', 'needsScopeArea'].contains(path)
              ? 2
              : 1,
        );
        expect(ws.conversation.first.text, 'badminton');
        expect(
          ws.conversation.where((m) => m.role == 'assistant').length,
          ['followUp', 'needsScopeArea'].contains(path) ? 2 : 1,
        );
        expect(
          identical(element, find.byType(PublicCityMapView).evaluate().single),
          true,
        );
        // Keyboard/layout notifications must not change the typed Pin source.
        ws.beginTyping();
        ws.stopTyping();
        ws.setSheetExtent(AgentSheetExtent.expanded);
        await t.pump();
        expect(
          t
              .widget<PublicCityMapView>(find.byType(PublicCityMapView))
              .entities
              .map((e) => e.id)
              .toSet(),
          expectedIDs,
        );
        await t.pumpWidget(const SizedBox());
        layers.dispose();
        layerClient.close();
        discovery.dispose();
        map.dispose();
        ws.dispose();
        city.dispose();
        client.close();
      },
    );
  }
  testWidgets(
    'anonymous captured wire retired by scope ABA never resurrects old local task or pins',
    (t) async {
      final city = _CapturedMapCity(selected: false);
      final pending = Completer<http.Response>();
      var requests = 0;
      final client = MockClient((r) {
        requests++;
        return pending.future;
      });
      final source = RemoteAgentTaskSource(
        cityID: () => city.selectedCity?.id,
        authorizationHeader: () => null,
        client: client,
        apiBaseUrl: 'http://replayed-local-wire.test',
      );
      final ws = AgentWorkspaceController(source: source),
          map = MapViewportState(),
          discovery = NowDiscoveryController(authorizationHeader: () => null);
      await t.pumpWidget(
        MaterialApp(
          home: MapCanvas(
            city: city,
            workspace: ws,
            mapState: map,
            discovery: discovery,
            onInitialViewport: (_) {},
            onMapUnavailable: (_) {},
          ),
        ),
      );
      ws.requireCity('badminton');
      city.choose();
      final turn = ws.resumeCity('aberdeen-gb', [], []);
      await t.pump();
      expect(requests, 1);
      ws.retirePendingQueryForViewChange();
      ws.newTask();
      pending.complete(
        http.Response(
          _capturedAnonymousPublicWire,
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      );
      await turn;
      await t.pump();
      expect(ws.task, isNull);
      expect(ws.result, isNull);
      expect(ws.recent, isEmpty);
      expect(
        t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      discovery.dispose();
      map.dispose();
      ws.dispose();
      city.dispose();
      client.close();
    },
  );
  for (final authority in ['nativeTask', 'nativeTaskID']) {
    test(
      'captured public wire $authority city authority is not overwritten by local fallback',
      () async {
        final body =
            jsonDecode(_capturedAnonymousPublicWire) as Map<String, dynamic>;
        final data = body['data'] as Map<String, dynamic>;
        const id = '33333333-3333-4333-8333-333333333333';
        if (authority == 'nativeTask') {
          data['task'] = {
            'id': id,
            'query': '权威原任务',
            'status': 'COMPLETED',
            'cityId': 'remote-authoritative-city',
          };
        } else {
          data['taskId'] = id;
        }
        final client = MockClient(
          (r) async => http.Response(
            jsonEncode(body),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          ),
        );
        final source = RemoteAgentTaskSource(
          cityID: () => 'aberdeen-gb',
          authorizationHeader: () => null,
          client: client,
          apiBaseUrl: 'http://replayed-local-wire.test',
        );
        final ws = AgentWorkspaceController(source: source)
          ..requireCity('badminton');
        await ws.resumeCity('aberdeen-gb', [], []);
        expect(ws.task!.id, id);
        expect(
          ws.task!.cityID,
          authority == 'nativeTask'
              ? 'remote-authoritative-city'
              : 'aberdeen-gb',
        );
        expect(
          ws.task!.query,
          authority == 'nativeTask' ? '权威原任务' : 'badminton',
        );
        ws.dispose();
        client.close();
      },
    );
  }

  testWidgets(
    'typed layers and cards share exact IDs toggles preserve map Element selection and task',
    (t) async {
      final city = _MapCity(),
          ws = AgentWorkspaceController(),
          map = MapViewportState(),
          discovery = NowDiscoveryController(authorizationHeader: () => null);
      final layers = MapLayersController(
        api: MapLayersApi(
          client: MockClient((r) async {
            final v = typed.mapTestView()..['cityId'] = 'test-city';
            return http.Response(
              jsonEncode({'data': v}),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          }),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => null,
        organizationWorkspaceID: () => null,
      );
      await layers.load('test-city', typed.mapTestBounds);
      ws.task = const AgentTask(
        id: 'preserved-task',
        query: '当前查询',
        status: 'COMPLETED',
      );
      final originalID = 'moment:${typed.mapTestID}';
      ws.selectEntity(originalID);
      await t.pumpWidget(
        MaterialApp(
          home: MapCanvas(
            city: city,
            workspace: ws,
            mapState: map,
            discovery: discovery,
            layers: layers,
            onInitialViewport: (_) {},
            onMapUnavailable: (_) {},
          ),
        ),
      );
      final element = find.byType(PublicCityMapView).evaluate().single;
      var projection = t.widget<PublicCityMapView>(
        find.byType(PublicCityMapView),
      );
      expect(projection.places, isEmpty);
      expect(
        projection.entities.map((i) => i.id).toSet(),
        layers.items.map((i) => i.mapID).toSet(),
      );
      layers.toggle('MOMENT', false);
      await t.pump();
      projection = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      expect(projection.entities.map((i) => i.id), isNot(contains(originalID)));
      expect(ws.selectedEntityId, originalID);
      expect(ws.task?.id, 'preserved-task');
      layers.toggle('MOMENT', true);
      await t.pump();
      expect(
        identical(element, find.byType(PublicCityMapView).evaluate().single),
        true,
      );
      expect(
        t
            .widget<PublicCityMapView>(find.byType(PublicCityMapView))
            .entities
            .map((i) => i.id),
        contains(originalID),
      );
      layers.retire();
      await t.pump();
      expect(
        t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      layers.dispose();
      discovery.dispose();
      map.dispose();
      ws.dispose();
      city.dispose();
    },
  );
  testWidgets(
    'ONLINE retains current city projection key pins selection and map Element',
    (tester) async {
      final city = _MapCity(),
          ws = AgentWorkspaceController(),
          map = MapViewportState(),
          discovery = NowDiscoveryController(authorizationHeader: () => null);
      const pin = MapEntity(
        id: 'activity:original',
        kind: MapEntityKind.activity,
        title: '原城市活动',
        subtitle: '公开',
        latitude: 57,
        longitude: -2,
      );
      ws.task = const AgentTask(
        id: 'old-city-task',
        query: '城市活动',
        status: 'COMPLETED',
      );
      ws.result = const AgentResult(
        entities: [pin],
        activities: [],
        places: [],
        note: '城市结果',
      );
      ws.selectEntity(pin.id);
      await tester.pumpWidget(
        MaterialApp(
          home: MapCanvas(
            city: city,
            workspace: ws,
            mapState: map,
            discovery: discovery,
            onInitialViewport: (_) {},
            onMapUnavailable: (_) {},
          ),
        ),
      );
      final element = find.byType(PublicCityMapView).evaluate().single;
      final before = tester.widget<PublicCityMapView>(
        find.byType(PublicCityMapView),
      );
      expect(before.entities.single.id, pin.id);
      expect(before.contextKey, 'old-city-task');
      ws.queryContextType = 'ONLINE';
      ws.newTask(preserveMapSelection: true);
      ws.task = const AgentTask(
        id: 'online-task',
        query: '阅读',
        status: 'COMPLETED',
        contextType: 'ONLINE',
        contextID: 'real-online-id',
      );
      ws.result = const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '线上回答',
        onlineContext: NowOnlineContext(id: 'real-online-id', label: '阅读'),
      );
      ws.setSheetExtent(AgentSheetExtent.medium);
      await tester.pump();
      final after = tester.widget<PublicCityMapView>(
        find.byType(PublicCityMapView),
      );
      expect(
        identical(find.byType(PublicCityMapView).evaluate().single, element),
        true,
      );
      expect(after.entities.single.id, pin.id);
      expect(after.contextKey, before.contextKey);
      expect(after.selectedEntityId, pin.id);
      ws.clearAccountContext();
      await tester.pump();
      expect(
        tester
            .widget<PublicCityMapView>(find.byType(PublicCityMapView))
            .entities,
        isEmpty,
      );
      await tester.pumpWidget(const SizedBox());
      ws.dispose();
      city.dispose();
      map.dispose();
      discovery.dispose();
    },
  );
}

// Captured actual LOCAL 9674 anonymous POST200, query badminton, city aberdeen-gb.
// This is the root's new diagnostic request, not the original phone request.
// Source: work/v5-age038-resume/public-query-pin-contract38sq/response.json
// Exact wire SHA256: eac7818ed205739dbab4f36e0bff6b8ea73f3c773641c083f22701daac189436
// Replay is HTTP-client controlled; no network mutation or production proof.
final _capturedAnonymousPublicWire = utf8.decode(
  base64Decode(
    'eyJkYXRhIjp7ImNvbW1lcmNpYWxUcnVzdFZlcnNpb24iOiJzcG9uc29yZWQtb3Bwb3J0dW5pdHktdjEiLCJzcG9uc29yZWRTdGF0'
    'dXMiOiJhdmFpbGFibGUiLCJzcG9uc29yZWRPcHBvcnR1bml0aWVzIjpbXSwiY2l0eUlkIjoiYWJlcmRlZW4tZ2IiLCJxdWVyeSI6'
    'ImJhZG1pbnRvbiIsIm1vZGUiOiJydWxlcyIsIm5vdGUiOiJCaXJkdGllIOW3suWPkeW4g+a0u+WKqCDCtyDmjInop4TliJnljLnp'
    'hY3pnIDmsYIiLCJtZXNzYWdlIjoi5om+5YiwIDMg5Liq56ym5ZCI5p2h5Lu255qE5YWs5byA5rS75Yqo44CCIiwicHJpbmNpcGFs'
    'VHlwZSI6IlBFUlNPTiIsInByaW5jaXBhbElkIjoiIiwid29ya3NwYWNlIjoiUEVSU09OQUwiLCJwZXJtaXNzaW9ucyI6WyJjaXR5'
    'X2NvbnRleHQucmVhZCIsInJlbGF0aW9uc2hpcF9jb250ZXh0LnJlYWQiXSwiYWN0aXZpdGllcyI6W3siaWQiOiJiMTcwMDAwMC0w'
    'MDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMDUiLCJvcmdhbml6ZXIiOnsidHlwZSI6Ik9SR0FOSVpBVElPTiIsImlkIjoiYjE3MDAw'
    'MDAtMDAwMC00MDAwLTgwMDAtMDAwMDAwMDAwMDAyIiwibmFtZSI6IkJpcmR0aWUg5pys5Zyw5rWL6K+V57695q+b55CD56S+In0s'
    'InZpc2liaWxpdHkiOiJwdWJsaWMiLCJvcmdhbml6YXRpb25JZCI6ImIxNzAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAw'
    'MiIsImNpdHlJZCI6ImFiZXJkZWVuLWdiIiwicGxhY2VJZCI6ImIxNzAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAwNCIs'
    'InBsYWNlTmFtZSI6IkJpcmR0aWUg5rWL6K+V5L2T6IKy6aaGIiwibW9kYWxpdHkiOiJpbl9wZXJzb24iLCJwaHlzaWNhbFBsYWNl'
    'U3RhdHVzIjoiY29uZmlybWVkIiwiaG9zdExhYmVsIjoiQmlyZHRpZSDmnKzlnLDmtYvor5Xnvr3mr5vnkIPnpL4iLCJ0aXRsZSI6'
    'IuWRqOWFree+veavm+eQg+S6pOa1ge+8iOacrOWcsOa1i+ivle+8iSIsInN1bW1hcnkiOiLku4XkvpvmnKzlnLDlvIDlj5HmtYvo'
    'r5XnmoTomZrmnoTnvr3mr5vnkIPmtLvliqjjgIIiLCJkZXNjcmlwdGlvbiI6IiIsImNhdGVnb3J5Q29kZSI6ImJhZG1pbnRvbiIs'
    'InByaWNlTWlub3IiOjAsInN0YXJ0c0F0IjoiMjAyNi0xMC0xMFQxODowMDowMCswODowMCIsImVuZHNBdCI6IjIwMjYtMTAtMTBU'
    'MjA6MDA6MDArMDg6MDAiLCJ0aW1lWm9uZSI6IkV1cm9wZS9Mb25kb24iLCJzY2hlZHVsZSI6IjEw5pyIMTDml6XvvIjlkajlha3v'
    'vIkxMTowMCIsImVuZFNjaGVkdWxlIjoiMTDmnIgxMOaXpe+8iOWRqOWFre+8iTEzOjAwIiwic3RhdHVzIjoidXBjb21pbmciLCJs'
    'b2NhdGlvbiI6eyJjb29yZGluYXRlU3lzdGVtIjoid2dzODQiLCJwcmVjaXNpb24iOiJwb2ludCIsImxhdGl0dWRlIjo1Ny4xNSwi'
    'bG9uZ2l0dWRlIjotMi4xfSwic291cmNlIjp7ImxhYmVsIjoi5pys5Zyw5byA5Y+R56S65L6LIiwicmVmZXJlbmNlIjoiZGV2LXNl'
    'ZWQ6Ly9iYWRtaW50b24td2Vla2VuZCIsIm1haW50YWluZXIiOiJCaXJkdGllIOacrOWcsOW8gOWPkeeOr+WigyIsInVwZGF0ZWRB'
    'dCI6IjIwMjYtMTAtMDZUMDE6MTM6MzAuODAyNTUyKzA4OjAwIiwidmVyaWZpZWRBdCI6IjIwMjYtMTAtMDZUMDE6MTM6MzAuODAy'
    'NTUyKzA4OjAwIiwiZXhwaXJlc0F0IjoiMjAyNi0xMC0xOVQwNzowMDowMCswODowMCIsImZyZXNobmVzcyI6ImN1cnJlbnQifX0s'
    'eyJpZCI6ImIxNzAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAwNiIsIm9yZ2FuaXplciI6eyJ0eXBlIjoiT1JHQU5JWkFU'
    'SU9OIiwiaWQiOiJiMTcwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMDIiLCJuYW1lIjoiQmlyZHRpZSDmnKzlnLDmtYvo'
    'r5Xnvr3mr5vnkIPnpL4ifSwidmlzaWJpbGl0eSI6InB1YmxpYyIsIm9yZ2FuaXphdGlvbklkIjoiYjE3MDAwMDAtMDAwMC00MDAw'
    'LTgwMDAtMDAwMDAwMDAwMDAyIiwiY2l0eUlkIjoiYWJlcmRlZW4tZ2IiLCJwbGFjZUlkIjoiYjE3MDAwMDAtMDAwMC00MDAwLTgw'
    'MDAtMDAwMDAwMDAwMDA3IiwicGxhY2VOYW1lIjoiQmlyZHRpZSDmtYvor5XljJfljLrkvZPogrLppoYiLCJtb2RhbGl0eSI6Imlu'
    'X3BlcnNvbiIsInBoeXNpY2FsUGxhY2VTdGF0dXMiOiJjb25maXJtZWQiLCJob3N0TGFiZWwiOiJCaXJkdGllIOacrOWcsOa1i+iv'
    'lee+veavm+eQg+ekviIsInRpdGxlIjoi5ZGo5pel57695q+b55CD5Y+M5omT77yI5pys5Zyw5rWL6K+V77yJIiwic3VtbWFyeSI6'
    'IuS7heS+m+acrOWcsOW8gOWPkea1i+ivleeahOiZmuaehOWPjOaJk+a0u+WKqOOAgiIsImRlc2NyaXB0aW9uIjoiIiwiY2F0ZWdv'
    'cnlDb2RlIjoiYmFkbWludG9uIiwicHJpY2VNaW5vciI6MCwic3RhcnRzQXQiOiIyMDI2LTEwLTExVDIxOjAwOjAwKzA4OjAwIiwi'
    'ZW5kc0F0IjoiMjAyNi0xMC0xMVQyMzowMDowMCswODowMCIsInRpbWVab25lIjoiRXVyb3BlL0xvbmRvbiIsInNjaGVkdWxlIjoi'
    'MTDmnIgxMeaXpe+8iOWRqOaXpe+8iTE0OjAwIiwiZW5kU2NoZWR1bGUiOiIxMOaciDEx5pel77yI5ZGo5pel77yJMTY6MDAiLCJz'
    'dGF0dXMiOiJ1cGNvbWluZyIsImxvY2F0aW9uIjp7ImNvb3JkaW5hdGVTeXN0ZW0iOiJ3Z3M4NCIsInByZWNpc2lvbiI6InBvaW50'
    'IiwibGF0aXR1ZGUiOjU3LjE2LCJsb25naXR1ZGUiOi0yLjA4fSwic291cmNlIjp7ImxhYmVsIjoi5pys5Zyw5byA5Y+R56S65L6L'
    'IiwicmVmZXJlbmNlIjoiZGV2LXNlZWQ6Ly9iYWRtaW50b24td2Vla2VuZCIsIm1haW50YWluZXIiOiJCaXJkdGllIOacrOWcsOW8'
    'gOWPkeeOr+WigyIsInVwZGF0ZWRBdCI6IjIwMjYtMTAtMDZUMDE6MTM6MzAuODAyNTUyKzA4OjAwIiwidmVyaWZpZWRBdCI6IjIw'
    'MjYtMTAtMDZUMDE6MTM6MzAuODAyNTUyKzA4OjAwIiwiZXhwaXJlc0F0IjoiMjAyNi0xMC0xOVQwNzowMDowMCswODowMCIsImZy'
    'ZXNobmVzcyI6ImN1cnJlbnQifX0seyJpZCI6ImIxNzAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAxNyIsIm9yZ2FuaXpl'
    'ciI6eyJ0eXBlIjoiT1JHQU5JWkFUSU9OIiwiaWQiOiJiMTcwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMTMiLCJuYW1l'
    'IjoiQWJlcmRlZW4gQ1NTQe+8iOiZmuaehOacrOWcsOa1i+ivle+8jOmdnuWumOaWue+8iSJ9LCJ2aXNpYmlsaXR5IjoicHVibGlj'
    'Iiwib3JnYW5pemF0aW9uSWQiOiJiMTcwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMTMiLCJjaXR5SWQiOiJhYmVyZGVl'
    'bi1nYiIsInBsYWNlSWQiOiJiMTcwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMDQiLCJwbGFjZU5hbWUiOiJCaXJkdGll'
    'IOa1i+ivleS9k+iCsummhiIsIm1vZGFsaXR5IjoiaW5fcGVyc29uIiwicGh5c2ljYWxQbGFjZVN0YXR1cyI6ImNvbmZpcm1lZCIs'
    'Imhvc3RMYWJlbCI6IkFiZXJkZWVuIENTU0HvvIjomZrmnoTmnKzlnLDmtYvor5XvvIzpnZ7lrpjmlrnvvIkiLCJ0aXRsZSI6IuWR'
    'qOacq+e+veavm+eQg+e7g+S5oO+8iOacrOWcsOa1i+ivle+8iSIsInN1bW1hcnkiOiLomZrmnoTkvZPogrLmtLvliqjvvIzku4Xk'
    'vpvlvIDlj5HmtYvor5XjgIIiLCJkZXNjcmlwdGlvbiI6IiIsImNhdGVnb3J5Q29kZSI6ImJhZG1pbnRvbiIsInByaWNlTWlub3Ii'
    'OjAsInN0YXJ0c0F0IjoiMjAyNi0xMC0xOFQyMTowMDowMCswODowMCIsImVuZHNBdCI6IjIwMjYtMTAtMThUMjM6MDA6MDArMDg6'
    'MDAiLCJ0aW1lWm9uZSI6IkV1cm9wZS9Mb25kb24iLCJzY2hlZHVsZSI6IjEw5pyIMTjml6XvvIjlkajml6XvvIkxNDowMCIsImVu'
    'ZFNjaGVkdWxlIjoiMTDmnIgxOOaXpe+8iOWRqOaXpe+8iTE2OjAwIiwic3RhdHVzIjoidXBjb21pbmciLCJsb2NhdGlvbiI6eyJj'
    'b29yZGluYXRlU3lzdGVtIjoid2dzODQiLCJwcmVjaXNpb24iOiJwb2ludCIsImxhdGl0dWRlIjo1Ny4xNSwibG9uZ2l0dWRlIjot'
    'Mi4xfSwic291cmNlIjp7ImxhYmVsIjoi5pys5Zyw5byA5Y+R56S65L6LIiwicmVmZXJlbmNlIjoiZGV2LXNlZWQ6Ly9mdW5jdGlv'
    'bmFsLW12cCIsIm1haW50YWluZXIiOiJCaXJkdGllIOacrOWcsOW8gOWPkeeOr+WigyIsInVwZGF0ZWRBdCI6IjIwMjYtMTAtMDZU'
    'MDE6MTM6MzAuODA4NDU4KzA4OjAwIiwiZXhwaXJlc0F0IjoiMjAyNi0xMS0wMlQwNzowMDowMCswODowMCIsImZyZXNobmVzcyI6'
    'InVudmVyaWZpZWQifX1dLCJwZW9wbGUiOltdLCJncm91cHMiOltdLCJvcmdhbml6YXRpb25zIjpudWxsLCJwbGFjZXMiOltdLCJm'
    'b2xsb3dVcHMiOlsi5oyJ5biC5Lit5b+D6Led56a75o6S5bqPIl0sInJlcXVlc3RJZCI6ImJpcmR0aWUtcmVjLW1hcHF1ZXJ5LTdl'
    'NTJmYzIyZjU3NjQ5NTlhNzg4OWRhN2VjYjQ5OGFkIiwicmVzdWx0U2V0Ijp7InNjaGVtYSI6InR5cGVkLWFnZW50LXJlc3VsdHMt'
    'djEiLCJpZCI6ImJpcmR0aWUtcmVjLW1hcHF1ZXJ5LTdlNTJmYzIyZjU3NjQ5NTlhNzg4OWRhN2VjYjQ5OGFkIiwicXVlcnkiOiJi'
    'YWRtaW50b24iLCJjaXR5SWQiOiJhYmVyZGVlbi1nYiIsImVudGl0aWVzIjpbeyJ0eXBlIjoiYWN0aXZpdHkiLCJpZCI6ImIxNzAw'
    'MDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAwNSJ9LHsidHlwZSI6ImFjdGl2aXR5IiwiaWQiOiJiMTcwMDAwMC0wMDAwLTQw'
    'MDAtODAwMC0wMDAwMDAwMDAwMDYifSx7InR5cGUiOiJhY3Rpdml0eSIsImlkIjoiYjE3MDAwMDAtMDAwMC00MDAwLTgwMDAtMDAw'
    'MDAwMDAwMDE3In1dLCJpdGVtcyI6W3siZW50aXR5UmVmIjp7InR5cGUiOiJhY3Rpdml0eSIsImlkIjoiYjE3MDAwMDAtMDAwMC00'
    'MDAwLTgwMDAtMDAwMDAwMDAwMDA1In0sInRpdGxlIjoi5ZGo5YWt57695q+b55CD5Lqk5rWB77yI5pys5Zyw5rWL6K+V77yJIiwi'
    'c3VtbWFyeSI6IuS7heS+m+acrOWcsOW8gOWPkea1i+ivleeahOiZmuaehOe+veavm+eQg+a0u+WKqOOAgiIsInNjb3BlIjoiQVVU'
    'SE9SSVpFRF9WSUVXIiwiZGV0YWlsUmVmIjp7InR5cGUiOiJhY3Rpdml0eSIsImlkIjoiYjE3MDAwMDAtMDAwMC00MDAwLTgwMDAt'
    'MDAwMDAwMDAwMDA1In0sInNoYXJlUmVmIjp7InR5cGUiOiJhY3Rpdml0eSIsImlkIjoiYjE3MDAwMDAtMDAwMC00MDAwLTgwMDAt'
    'MDAwMDAwMDAwMDA1In0sImFuY2hvciI6eyJjb29yZGluYXRlU3lzdGVtIjoid2dzODQiLCJwcmVjaXNpb24iOiJwb2ludCIsImxh'
    'dGl0dWRlIjo1Ny4xNSwibG9uZ2l0dWRlIjotMi4xLCJwbGFjZUlkIjoiYjE3MDAwMDAtMDAwMC00MDAwLTgwMDAtMDAwMDAwMDAw'
    'MDA0In19LHsiZW50aXR5UmVmIjp7InR5cGUiOiJhY3Rpdml0eSIsImlkIjoiYjE3MDAwMDAtMDAwMC00MDAwLTgwMDAtMDAwMDAw'
    'MDAwMDA2In0sInRpdGxlIjoi5ZGo5pel57695q+b55CD5Y+M5omT77yI5pys5Zyw5rWL6K+V77yJIiwic3VtbWFyeSI6IuS7heS+'
    'm+acrOWcsOW8gOWPkea1i+ivleeahOiZmuaehOWPjOaJk+a0u+WKqOOAgiIsInNjb3BlIjoiQVVUSE9SSVpFRF9WSUVXIiwiZGV0'
    'YWlsUmVmIjp7InR5cGUiOiJhY3Rpdml0eSIsImlkIjoiYjE3MDAwMDAtMDAwMC00MDAwLTgwMDAtMDAwMDAwMDAwMDA2In0sInNo'
    'YXJlUmVmIjp7InR5cGUiOiJhY3Rpdml0eSIsImlkIjoiYjE3MDAwMDAtMDAwMC00MDAwLTgwMDAtMDAwMDAwMDAwMDA2In0sImFu'
    'Y2hvciI6eyJjb29yZGluYXRlU3lzdGVtIjoid2dzODQiLCJwcmVjaXNpb24iOiJwb2ludCIsImxhdGl0dWRlIjo1Ny4xNiwibG9u'
    'Z2l0dWRlIjotMi4wOCwicGxhY2VJZCI6ImIxNzAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAwNyJ9fSx7ImVudGl0eVJl'
    'ZiI6eyJ0eXBlIjoiYWN0aXZpdHkiLCJpZCI6ImIxNzAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAxNyJ9LCJ0aXRsZSI6'
    'IuWRqOacq+e+veavm+eQg+e7g+S5oO+8iOacrOWcsOa1i+ivle+8iSIsInN1bW1hcnkiOiLomZrmnoTkvZPogrLmtLvliqjvvIzk'
    'u4XkvpvlvIDlj5HmtYvor5XjgIIiLCJzY29wZSI6IkFVVEhPUklaRURfVklFVyIsImRldGFpbFJlZiI6eyJ0eXBlIjoiYWN0aXZp'
    'dHkiLCJpZCI6ImIxNzAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAxNyJ9LCJzaGFyZVJlZiI6eyJ0eXBlIjoiYWN0aXZp'
    'dHkiLCJpZCI6ImIxNzAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAxNyJ9LCJhbmNob3IiOnsiY29vcmRpbmF0ZVN5c3Rl'
    'bSI6Indnczg0IiwicHJlY2lzaW9uIjoicG9pbnQiLCJsYXRpdHVkZSI6NTcuMTUsImxvbmdpdHVkZSI6LTIuMSwicGxhY2VJZCI6'
    'ImIxNzAwMDAwLTAwMDAtNDAwMC04MDAwLTAwMDAwMDAwMDAwNCJ9fV0sImZpbHRlcnMiOnsiY2F0ZWdvcnkiOiJiYWRtaW50b24i'
    'LCJjdXJyZW50UXVlcnkiOiJiYWRtaW50b24iLCJsb2NhdGlvblByZWZlcmVuY2UiOiJjaXR5IiwidGFyZ2V0SW50ZW50IjoiRklO'
    'RF9BQ1RJVklUWSIsInRpbWVQcmVmZXJlbmNlIjoiYW55dGltZSJ9LCJnZW5lcmF0ZWRBdCI6IjIwMjYtMTAtMDVUMjM6MDM6MzQu'
    'ODU2MjA2M1oiLCJzdGF0dXMiOiJyZWFkeSJ9LCJhY3Rpb25zIjpbXSwibWFwRWZmZWN0cyI6eyJjYW1lcmEiOiJwcmVzZXJ2ZSIs'
    'InBpbkVudGl0eUlkcyI6WyJhY3Rpdml0eTpiMTcwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMDUiLCJhY3Rpdml0eTpi'
    'MTcwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwMDYiLCJhY3Rpdml0eTpiMTcwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAw'
    'MDAwMDAwMTciXX19fQ==',
  ),
);

class _NativePinFixture {
  final shared = NowFixture();
  final city = _CapturedMapCity(selected: true);
  final requests = <http.Request>[];
  late final client = MockClient((request) async {
    requests.add(request);
    return http.Response(_capturedAnonymousPublicWire, 200,
        headers: {'content-type': 'application/json; charset=utf-8'});
  });
  late final source = RemoteAgentTaskSource(cityID: () => city.selectedCity?.id,
      authorizationHeader: () => null, client: client,
      apiBaseUrl: 'http://replayed-native-pin-unit.test');
  Future<void> mount(WidgetTester t) async {
    await t.pumpWidget(MaterialApp(home: MapWorkspace(
      city: city, auth: shared.auth, moments: shared.moments,
      agentTaskSource: source, seedClient: shared.client,
      seedApiBaseUrl: shared.base,
    )));
    await t.pumpAndSettle();
  }
  AgentWorkspaceController workspace(WidgetTester t) => t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
  void dispose() {
    client.close();
    city.dispose();
    shared.dispose();
  }
}

class _CapturedMapCity extends PublicCityController {
  _CapturedMapCity({required bool selected})
    : selection = selected ? scope : null;
  static const scope = PublicCity(
    id: 'aberdeen-gb',
    name: '阿伯丁（本地原始响应复验）',
    region: '本地验收',
    contentStatus: 'test',
    map: null,
    source: PublicSource(
      label: '真实本地wire重放',
      maintainer: 'fixture',
      freshness: 'replay',
      updatedAt: null,
    ),
  );
  PublicCity? selection;
  @override
  PublicCity? get selectedCity => selection;
  void choose() {
    selection = scope;
    notifyListeners();
  }
}
