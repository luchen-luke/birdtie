import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'chat_entity_router_test.dart' show ChatTestAuth;
import 'map_layers_api_test.dart' as fixtures;

class _City extends PublicCityController {
  @override
  PublicCity get selectedCity => const PublicCity(
    id: 'aberdeen-gb',
    name: 'Aberdeen',
    region: '测试',
    contentStatus: 'building',
    source: PublicSource(
      label: 'fixture',
      maintainer: 'test',
      freshness: 'test',
      updatedAt: null,
    ),
    map: null,
  );
}

void main() {
  testWidgets('实际Now区域A挂起时新B可点击，B先返回后A不得覆盖或插入伪聊天', (t) async {
    const b = MapBounds(west: -2.3, south: 57.0, east: -2.1, north: 57.2);
    final aResponse = Completer<http.Response>();
    final bResponse = Completer<http.Response>();
    final requests = <http.Request>[];
    final client = MockClient((request) async {
      if (!request.url.path.endsWith('/map-layers')) {
        return http.Response('{}', 404);
      }
      requests.add(request);
      if (request.url.queryParameters['west'] == '${b.west}') {
        return bResponse.future;
      }
      return aResponse.future;
    });
    final auth = ChatTestAuth(), city = _City();
    final moments = PrivateMomentController(
      client: client,
      apiBaseUrl: '',
      authorizationHeader: () => null,
    );
    final connections = ConnectionSource(
      client: client,
      apiBaseUrl: '',
      authorizationHeader: () => null,
    );
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          connectionSource: connections,
          seedClient: client,
          seedApiBaseUrl: 'https://owned.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
    final mapElement = find.byType(MapCanvas).evaluate().single;
    const task = AgentTask(
      id: 'preserved-race-task',
      cityID: 'aberdeen-gb',
      query: '本人既有问题',
      status: 'COMPLETED',
    );
    canvas.workspace.task = task;
    canvas.workspace.result = const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '原回答',
    );
    canvas.workspace.conversation.addAll(const [
      AgentMessage(role: 'user', text: '本人既有问题'),
      AgentMessage(role: 'assistant', text: '原回答'),
    ]);
    final messages = List<AgentMessage>.of(canvas.workspace.conversation);
    canvas.mapState.cameraSettled(fixtures.mapTestBounds);
    await t.pumpAndSettle();
    final command = find.byKey(const Key('now-map-area-command'));
    await t.tap(command);
    await t.pump();
    expect(canvas.workspace.areaOperation?.status, AgentAreaStatus.loading);
    expect(requests, hasLength(1));
    // A remains pending. A new user camera range must expose an enabled B
    // command; a global loading flag must not block explicit replacement.
    canvas.mapState.cameraStarted();
    canvas.mapState.cameraSettled(b);
    await t.pump();
    expect(command, findsOneWidget);
    expect(t.widget<TextButton>(command).onPressed, isNotNull);
    await t.tap(command);
    await t.pump();
    expect(requests, hasLength(2));
    expect(requests.every((request) => request.method == 'GET'), isTrue);
    expect(requests.every((request) => request.body.isEmpty), isTrue);
    expect(requests.last.url.queryParameters['west'], '${b.west}');
    expect(canvas.workspace.areaOperation?.bounds, b);

    http.Response reply(String title) {
      final view = fixtures.mapTestView();
      final now = DateTime.now().toUtc();
      view['cityId'] = 'aberdeen-gb';
      view['observedAt'] = now.toIso8601String();
      view['validUntil'] = now
          .add(const Duration(minutes: 2))
          .toIso8601String();
      final item = fixtures.mapTestItem('PLACE')..['title'] = title;
      (item['anchor'] as Map)
        ..['latitude'] = 57.15
        ..['longitude'] = -2.15;
      view['items'] = [item];
      return http.Response(
        jsonEncode({'data': view}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    }

    bResponse.complete(reply('区域B结果'));
    await t.pumpAndSettle();
    final accepted = canvas.workspace.areaOperation;
    expect(accepted?.status, AgentAreaStatus.success);
    expect(accepted?.items.single.title, '区域B结果');
    expect(find.text('区域B结果'), findsOneWidget);
    aResponse.complete(reply('区域A迟到结果'));
    await t.pumpAndSettle();
    expect(canvas.workspace.areaOperation, same(accepted));
    expect(canvas.workspace.areaOperation?.bounds, b);
    expect(find.text('区域B结果'), findsOneWidget);
    expect(find.text('区域A迟到结果'), findsNothing);
    expect(canvas.workspace.task, same(task));
    expect(canvas.workspace.conversation, messages);
    expect(
      canvas.workspace.conversation.where((m) => m.text == '搜索此区域'),
      isEmpty,
    );
    expect(
      t
          .widget<PublicCityMapView>(find.byType(PublicCityMapView))
          .entities
          .map((entity) => entity.title),
      ['区域B结果'],
    );
    expect(find.byType(MapCanvas).evaluate().single, same(mapElement));
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    connections.dispose();
    city.dispose();
    auth.dispose();
    client.close();
  });
  testWidgets('已有Agent回合的新区域pin可选，保持镜头与历史', (t) async {
    final client = MockClient((r) async {
      if (!r.url.path.endsWith('/map-layers')) return http.Response('{}', 404);
      final view = fixtures.mapTestView();
      final now = DateTime.now().toUtc();
      view['cityId'] = 'aberdeen-gb';
      view['observedAt'] = now.toIso8601String();
      view['validUntil'] = now
          .add(const Duration(minutes: 2))
          .toIso8601String();
      return http.Response(
        jsonEncode({'data': view}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    final auth = ChatTestAuth(), city = _City();
    final moments = PrivateMomentController(
      client: client,
      apiBaseUrl: '',
      authorizationHeader: () => null,
    );
    final connections = ConnectionSource(
      client: client,
      apiBaseUrl: '',
      authorizationHeader: () => null,
    );
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          connectionSource: connections,
          seedClient: client,
          seedApiBaseUrl: 'https://owned.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
    const task = AgentTask(
      id: 'existing',
      cityID: 'aberdeen-gb',
      query: '历史回合',
      status: 'COMPLETED',
    );
    canvas.workspace.task = task;
    canvas.workspace.result = const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '历史解释',
    );
    canvas.mapState.cameraSettled(fixtures.mapTestBounds);
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('now-map-area-command')));
    await t.pumpAndSettle();
    expect(canvas.workspace.areaOperation?.status, AgentAreaStatus.success);
    final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(view.preserveViewport, isTrue);
    expect(view.entities, hasLength(5));
    view.onEntitySelected!(view.entities.first);
    await t.pumpAndSettle();
    expect(canvas.workspace.selectedEntityId, view.entities.first.id);
    expect(canvas.workspace.task, same(task));
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    connections.dispose();
    city.dispose();
    auth.dispose();
    client.close();
  });
  testWidgets('实际Now区域按钮只GET，一次loading/empty，草稿和地图保留', (t) async {
    final response = Completer<http.Response>();
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      if (!r.url.path.endsWith('/map-layers')) {
        return http.Response('{}', 404);
      }
      requests.add(r);
      return response.future;
    });
    final auth = ChatTestAuth(), city = _City();
    final moments = PrivateMomentController(
      client: client,
      apiBaseUrl: '',
      authorizationHeader: () => null,
    );
    final connections = ConnectionSource(
      client: client,
      apiBaseUrl: '',
      authorizationHeader: () => null,
    );
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          connectionSource: connections,
          seedClient: client,
          seedApiBaseUrl: 'https://owned.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    final mapElement = find.byType(MapCanvas).evaluate().single;
    final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
    final editor = t.state<AgentComposerState>(find.byType(AgentComposer));
    editor.fillDraft('保留的地图草稿');
    editor.pauseEditing();
    canvas.mapState.cameraSettled(fixtures.mapTestBounds);
    await t.pumpAndSettle();
    final chip = find.byKey(const Key('now-map-area-command'));
    final logicalWidth = t.view.physicalSize.width / t.view.devicePixelRatio;
    expect(t.getCenter(chip).dx, closeTo(logicalWidth / 2, .5));
    expect(t.getSize(chip).width, lessThan(logicalWidth * .6));
    await t.tap(chip);
    await t.pump();
    expect(canvas.workspace.areaOperation?.status, AgentAreaStatus.loading);
    expect(requests, hasLength(1));
    expect(requests.single.method, 'GET');
    expect(requests.single.url.path, '/v1/cities/aberdeen-gb/map-layers');
    expect(requests.single.body, isEmpty);
    final view = fixtures.mapTestView();
    final now = DateTime.now().toUtc();
    view['cityId'] = 'aberdeen-gb';
    view['observedAt'] = now.toIso8601String();
    view['validUntil'] = now.add(const Duration(minutes: 2)).toIso8601String();
    view['items'] = [];
    response.complete(
      http.Response(
        jsonEncode({'data': view}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    await t.pumpAndSettle();
    expect(
      canvas.workspace.areaOperation?.status,
      AgentAreaStatus.empty,
      reason:
          '${canvas.workspace.areaOperation?.failure?.userMessage}; '
          'layer error=${canvas.layers?.error} epoch=${canvas.layers?.epoch} '
          'bounds=${canvas.layers?.bounds?.toJson()} view=${canvas.layers?.publicView?.cityID}',
    );
    expect(find.text('本次未找到符合条件的结果。'), findsOneWidget);
    expect(find.text('搜索此区域'), findsNothing);
    expect(canvas.workspace.task, isNull);
    expect(canvas.workspace.conversation, isEmpty);
    expect(editor.draftText, '保留的地图草稿');
    expect(find.byType(MapCanvas).evaluate().single, same(mapElement));
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    connections.dispose();
    city.dispose();
    auth.dispose();
    client.close();
  });
  for (final size in [
    const Size(360, 800),
    const Size(390, 844),
    const Size(430, 932),
    const Size(640, 360),
    const Size(320, 640),
  ]) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets('Now单面板/输入保留 ${size.width}x${size.height} 字号$scale', (
        t,
      ) async {
        t.view.physicalSize = size;
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        addTearDown(t.view.resetViewInsets);
        final auth = ChatTestAuth(), city = _City();
        final client = MockClient(
          (r) async => http.Response('{"data":[]}', 200),
        );
        final moments = PrivateMomentController(
          client: client,
          apiBaseUrl: '',
          authorizationHeader: () => null,
        );
        final connections = ConnectionSource(
          client: client,
          apiBaseUrl: '',
          authorizationHeader: () => null,
        );
        await t.pumpWidget(
          MaterialApp(
            builder: (c, w) => MediaQuery(
              data: MediaQuery.of(
                c,
              ).copyWith(textScaler: TextScaler.linear(scale)),
              child: w!,
            ),
            home: MapWorkspace(
              city: city,
              auth: auth,
              moments: moments,
              connectionSource: connections,
              seedClient: client,
              seedApiBaseUrl: '',
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(t.takeException(), isNull);
        final map = find.byType(MapCanvas).evaluate().single;
        final editor = t.state<AgentComposerState>(find.byType(AgentComposer));
        final ws = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
        final task = const AgentTask(
          id: 'retained',
          query: '正常历史',
          status: 'COMPLETED',
        );
        ws.task = task;
        ws.result = const AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: '本次明确的回答',
        );
        ws.selectEntity('place:retained', preserveViewport: true);
        ws.setSheetExtent(AgentSheetExtent.peek);
        await t.pumpAndSettle();
        editor.fillDraft('保留中文草稿\n第二行');
        await t.pumpAndSettle();
        final draft = editor.draftText;
        final keyboard = size.width == 320
            ? 500.0
            : size.height > 600
            ? 310.0
            : 150.0;
        for (var cycle = 0; cycle < 3; cycle++) {
          t.view.viewInsets = FakeViewPadding(bottom: keyboard);
          await t.pumpAndSettle();
          expect(t.takeException(), isNull);
          expect(find.byType(TextField), findsOneWidget);
          expect(find.byKey(const Key('now-primary-surface')), findsOneWidget);
          expect(
            find.byKey(const Key('now-context-keyboard-restore')),
            findsNothing,
          );
          expect(
            find.byKey(const Key('now-sheet-keyboard-restore')),
            findsNothing,
          );
          expect(
            t.getBottomRight(find.byType(AgentComposer)).dy,
            lessThanOrEqualTo(size.height - keyboard),
          );
          expect(
            t.getBottomRight(find.byKey(const Key('now-primary-surface'))).dy,
            closeTo(size.height - keyboard, .01),
            reason: '主表面外边界贴到真实contentBottom，呼吸空间只在面板内部',
          );
          expect(
            t.state<AgentComposerState>(find.byType(AgentComposer)),
            same(editor),
          );
          expect(editor.draftText, draft);
          expect(find.byType(MapCanvas).evaluate().single, same(map));
          expect(ws.task, same(task));
          expect(ws.selectedEntityId, 'place:retained');
          t.view.viewInsets = const FakeViewPadding();
          await t.pumpAndSettle();
        }
        await t.pumpWidget(const SizedBox());
        moments.dispose();
        connections.dispose();
        city.dispose();
        auth.dispose();
        client.close();
      });
    }
  }
}
