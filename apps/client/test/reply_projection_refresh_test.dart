import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/map_camera_focus.dart';
import 'package:birdtie_client/src/city/native_city_map_io.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_reply_membership.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart';

import 'reply_history_restore_test.dart' as history;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;

// Synthetic original GET wire + fake clock/SDK only; no device or provider.
class _Reads {
  DateTime now = DateTime.utc(2026, 10, 8, 2);
  String? token = 'Bearer synthetic-original', org, online;
  String city = history.cityID, owner = history.ownerID;
  int epoch = 0, status = 200;
  bool revokeA = false;
  void Function(Map<String, dynamic>)? mutate;
  Completer<http.Response>? pending;
  Completer<http.Response>? pendingPost;
  final methods = <String>[];
  final allMethods = <String>[];
  int recentReads = 0;
  late final RemoteAgentTaskSource source = RemoteAgentTaskSource(
    cityID: () => city,
    authorizationHeader: () => token,
    organizationWorkspaceID: () => org,
    onlineContextID: () => online,
    publicEvidenceOwnerID: () => owner,
    publicEvidenceEpoch: () => epoch,
    publicEvidenceNow: () => now,
    apiBaseUrl: 'http://localhost:8080',
    client: MockClient((request) async {
      allMethods.add(request.method);
      if (pendingPost != null && request.method == 'POST') {
        methods.add(request.method);
        expectSync(
          request.url.path,
          '/v1/cities/${history.cityID}/agent/tasks',
        );
        expectSync(request.headers['Authorization'], token);
        expectSync(jsonDecode(request.body), {
          'query': '找地点 C',
          'taskId': history.taskID,
        });
        return pendingPost!.future;
      }
      expectSync(request.method, 'GET');
      if (request.url.path == '/v1/me/agent-tasks') {
        recentReads++;
        return http.Response(
          jsonEncode({'data': []}),
          200,
          headers: {'content-type': 'application/json'},
        );
      }
      methods.add(request.method);
      expectSync(request.url.path, '/v1/me/agent-tasks/${history.taskID}');
      expectSync(request.headers['Authorization'], token);
      return pending?.future ?? Future.value(response());
    }),
  );
  http.Response response() {
    final wire = history.historyWire(now, revokeA: revokeA);
    for (final entry in wire['messageResults'] as List) {
      entry['validUntil'] = now
          .add(const Duration(seconds: 30))
          .toIso8601String();
    }
    mutate?.call(wire);
    return http.Response.bytes(utf8.encode(jsonEncode({'data': wire})), status);
  }

  late final AgentWorkspaceController workspace = AgentWorkspaceController(
    source: source,
    replyRefreshAllowed: () => visible,
  );
  bool visible = true;
  Future<void> open() => workspace.reopen(
    AgentTask.fromJson(
      history.historyWire(now)['task'] as Map<String, dynamic>,
    ),
    [],
    [],
  );
  Future<void> elapse(WidgetTester t, int seconds) async {
    now = now.add(Duration(seconds: seconds));
    await t.pump(Duration(seconds: seconds));
    await t.pump();
  }

  void retire() {
    ++epoch;
    workspace.retireReplyRefresh();
  }

  bool _closed = false;
  void dispose() {
    if (!_closed) {
      _closed = true;
      workspace.dispose();
    }
  }
}

class _Unsupported extends AgentTaskSource {
  int writes = 0;
  @override
  Future<AgentResult> resolve(
    String q,
    List<PublicActivity> a,
    List<PublicPlace> p,
  ) async {
    writes++;
    throw StateError('GET must not fall through to POST');
  }
}

class _ObservedCity extends PublicCityController {
  _ObservedCity(this.reads);
  final _Reads reads;
  @override
  PublicCity get selectedCity => PublicCity(
    id: reads.city,
    name: 'synthetic',
    region: 'test',
    contentStatus: 'test',
    source: _source,
    map: null,
  );
  @override
  void selectCity(String id) {
    reads.city = id;
    notifyListeners();
  }
}

class _City extends PublicCityController {
  @override
  PublicCity get selectedCity => _city();
  @override
  List<PublicPlace> get places => [
    PublicPlace(
      id: 'synthetic-city-catalog',
      name: 'catalog must not replace a reply',
      categoryCode: 'culture',
      summary: 'synthetic',
      source: _source,
      location: const PublicPlaceLocation(
        coordinateSystem: 'wgs84',
        precision: 'point',
        latitude: 58,
        longitude: -1,
      ),
    ),
  ];
}

const _source = PublicSource(
  label: 'synthetic',
  maintainer: 'test',
  freshness: 'test',
  updatedAt: null,
);
PublicCity _city() => const PublicCity(
  id: history.cityID,
  name: 'synthetic',
  region: 'test',
  contentStatus: 'test',
  source: _source,
  map: PublicCityMap(
    provider: 'mapbox',
    latitude: 57,
    longitude: -2,
    defaultZoom: 11,
    sourceRef: 'synthetic://camera-test',
  ),
);
MapEntity _entity(String id, double latitude) => MapEntity(
  id: id,
  kind: MapEntityKind.place,
  title: 'synthetic',
  subtitle: 'test',
  latitude: latitude,
  longitude: -2,
);

void main() {
  test('GET-only重读到期后仍按原身份解析同一任务与membership', () async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final a = f.workspace.task!;
    f.now = f.now.add(const Duration(seconds: 31));
    final fresh = await f.source.rereadReplyProjections(a);
    expect(fresh, isNotNull);
    final b = fresh!.task!;
    expect(b.id, a.id);
    expect(b.query, a.query);
    expect(b.status, a.status);
    expect(b.intent, a.intent);
    expect(b.cityID, a.cityID);
    expect(b.contextType, a.contextType);
    expect(b.contextID, a.contextID);
    expect(b.principalType, a.principalType);
    expect(b.principalID, a.principalID);
    expect(b.actingUserID, a.actingUserID);
    expect(b.filters, a.filters);
    expect(b.createdAt, a.createdAt);
    expect(b.updatedAt, a.updatedAt);
    expect(b.messages.length, a.messages.length);
    for (var i = 0; i < a.messages.length; i++) {
      expect(b.messages[i].role, a.messages[i].role);
      expect(b.messages[i].text, a.messages[i].text);
      expect(
        b.messages[i].resultMembership?.refs,
        a.messages[i].resultMembership?.refs,
      );
    }
    expect(await f.workspace.refreshReplyProjections(), true);
  });
  testWidgets('30秒原TTL跨100秒安静GET，任务消息筛选草稿选择与map实例不变', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace;
    w.showReplyOnMap(w.replies.first.result, 'place:${history.placeA}');
    w.beginTyping();
    final task = w.task,
        epoch = w.taskEpoch,
        messages = List.of(w.conversation);
    final old = w.replies.first.result;
    final draft = TextEditingController(text: '尚未发送的草稿');
    addTearDown(draft.dispose);
    final city = _City();
    addTearDown(city.dispose);
    final discovery = NowDiscoveryController(authorizationHeader: () => null);
    addTearDown(discovery.dispose);
    final viewport = MapViewportState();
    addTearDown(viewport.dispose);
    const bounds = MapBounds(west: -3, south: 56, east: -1, north: 58);
    viewport.cameraSettled(bounds);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Column(
            children: [
              Expanded(
                child: MapCanvas(
                  city: city,
                  workspace: w,
                  mapState: viewport,
                  discovery: discovery,
                  onInitialViewport: (_) {},
                  onMapUnavailable: (_) {},
                ),
              ),
              TextField(controller: draft),
            ],
          ),
        ),
      ),
    );
    final map = find.byType(PublicCityMapView).evaluate().single;
    final mapState = t.state(find.byType(PublicCityMapView));
    for (var i = 0; i < 4; i++) {
      await f.elapse(t, 25);
      expect(w.presentedResult!.entities.single.id, 'place:${history.placeA}');
      expect(w.result!.entities.single.id, 'place:${history.placeB}');
      expect(w.replies.last.result, same(w.result));
      expect(w.task, same(task));
      expect(w.taskEpoch, epoch);
      expect(w.conversation, orderedEquals(messages));
      expect(w.task!.filters, task!.filters);
      expect(w.selectedEntityId, 'place:${history.placeA}');
      expect(w.sheetExtent, AgentSheetExtent.expanded);
      expect(w.inputFocused, true);
      expect(w.contentMode, AgentContentMode.conversation);
      expect(w.requestError, isNull);
      expect(draft.text, '尚未发送的草稿');
      expect(viewport.viewportBounds, same(bounds));
      expect(find.byType(PublicCityMapView).evaluate().single, same(map));
      expect(t.state(find.byType(PublicCityMapView)), same(mapState));
      expect(
        t
            .widget<PublicCityMapView>(find.byType(PublicCityMapView))
            .preserveViewport,
        true,
      );
    }
    expect(old.replyProjectionCurrent, false);
    expect(f.methods, List.filled(5, 'GET'));
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });

  testWidgets('跨TTL迟到GET先隐藏旧轮，成功只换新lease不复活旧对象', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace, old = w.replies.first.result;
    w.showReplyOnMap(old, 'place:${history.placeA}');
    f.pending = Completer<http.Response>();
    await f.elapse(t, 25);
    expect(f.methods, ['GET', 'GET']);
    await f.elapse(t, 6);
    expect(old.replyProjectionCurrent, false);
    expect(w.presentedResult!.entities, isEmpty);
    expect(w.presentedResult, same(old));
    expect(w.preserveReplyCamera, true);
    final current = w.refreshReplyProjections(),
        duplicate = w.refreshReplyProjections();
    expect(identical(current, duplicate), true);
    expect(f.methods, hasLength(2));
    f.pending!.complete(f.response());
    await t.pump();
    expect(await current, true);
    expect(w.presentedResult!.entities.single.id, 'place:${history.placeA}');
    expect(old.replyProjectionCurrent, false);
    expect(w.selectedEntityId, 'place:${history.placeA}');
    f.dispose();
  });

  testWidgets('历史A先到期但当前B仍valid时保持A的空地图，fresh对应A才恢复', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    f.mutate = (wire) {
      wire['messageResults'][0]['validUntil'] = f.now
          .add(const Duration(seconds: 20))
          .toIso8601String();
    };
    await f.open();
    final w = f.workspace, oldA = w.replies.first.result;
    w.showReplyOnMap(oldA, 'place:${history.placeA}');
    f.pending = Completer<http.Response>();
    await f.elapse(t, 15);
    await f.elapse(t, 6);
    expect(oldA.replyProjectionCurrent, false);
    expect(w.result!.replyProjectionCurrent, true);
    expect(w.result!.entities.single.id, 'place:${history.placeB}');
    expect(w.presentedResult, same(oldA));
    expect(w.presentedResult!.entities, isEmpty);
    expect(w.canUseReplyProjection(oldA), false);
    expect(w.selectedEntityId, 'place:${history.placeA}');
    expect(f.methods, ['GET', 'GET']);
    f.pending!.complete(f.response());
    await t.pump();
    expect(w.presentedResult!.entities.single.id, 'place:${history.placeA}');
    expect(oldA.replyProjectionCurrent, false);
    expect(w.selectedEntityId, 'place:${history.placeA}');
    f.dispose();
  });

  testWidgets('原GET撤权旧A清空选点，绝不拿当前B填旧A地图', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace;
    w.showReplyOnMap(w.replies.first.result, 'place:${history.placeA}');
    f.revokeA = true;
    await f.elapse(t, 25);
    expect(w.presentedResult!.entities, isEmpty);
    expect(w.result!.entities.single.id, 'place:${history.placeB}');
    expect(w.selectedEntityId, isNull);
    expect(w.preserveReplyCamera, true);
    expect(w.replies, hasLength(2));
    expect(w.conversation, hasLength(4));
    expect(f.methods, ['GET', 'GET']);
    f.dispose();
  });

  testWidgets('authoritative部分history漏A保持空地图，不fallback当前B或城市目录', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace;
    w.showReplyOnMap(w.replies.first.result, 'place:${history.placeA}');
    f.mutate = (wire) {
      (wire['messageResults'] as List).removeAt(0);
    };
    expect(await w.refreshReplyProjections(), true);
    expect(w.presentedResult!.entities, isEmpty);
    expect(w.presentedResult!.projectionItems, isEmpty);
    expect(w.presentedResult!.places, isEmpty);
    expect(w.presentedResult!.replyProjectionCurrent, false);
    expect(w.result!.entities.single.id, 'place:${history.placeB}');
    expect(w.selectedEntityId, isNull);
    expect(w.replies, hasLength(1));
    expect(w.conversation, hasLength(4));
    final city = _City();
    final viewport = MapViewportState();
    final discovery = NowDiscoveryController(authorizationHeader: () => null);
    await t.pumpWidget(
      MaterialApp(
        home: MapCanvas(
          city: city,
          workspace: w,
          mapState: viewport,
          discovery: discovery,
          onInitialViewport: (_) {},
          onMapUnavailable: (_) {},
        ),
      ),
    );
    final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(city.places, isNotEmpty);
    expect(view.entities, isEmpty);
    expect(view.places, isEmpty);
    await t.pumpWidget(const SizedBox());
    city.dispose();
    viewport.dispose();
    discovery.dispose();
    expect(f.methods, ['GET', 'GET']);
    f.dispose();
  });

  testWidgets('部分history漏A后fresh同membership恢复A，安静地图从不改成B', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace;
    final original = w.task;
    w.showReplyOnMap(w.replies.first.result, 'place:${history.placeA}');
    f.mutate = (wire) {
      (wire['messageResults'] as List).removeAt(0);
    };
    expect(await w.refreshReplyProjections(), true);
    expect(w.presentedResult!.projectionItems, isEmpty);
    expect(w.result!.entities.single.id, 'place:${history.placeB}');
    f.mutate = null;
    expect(await w.refreshReplyProjections(), true);
    expect(w.presentedResult!.entities.single.id, 'place:${history.placeA}');
    expect(w.result!.entities.single.id, 'place:${history.placeB}');
    expect(identical(w.task, original), true);
    expect(w.conversation, hasLength(4));
    expect(w.preserveReplyCamera, true);
    expect(f.methods, ['GET', 'GET', 'GET']);
    f.dispose();
  });

  testWidgets('旧TTL在新的同Task POST等待时到期，不抑制新问答坐标focus', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace;
    final before = w.result!;
    final city = _City();
    addTearDown(city.dispose);
    final discovery = NowDiscoveryController(authorizationHeader: () => null);
    addTearDown(discovery.dispose);
    final viewport = MapViewportState();
    addTearDown(viewport.dispose);
    const bounds = MapBounds(west: -3, south: 56, east: -1, north: 58);
    viewport.cameraSettled(bounds);
    await t.pumpWidget(
      MaterialApp(
        home: MapCanvas(
          city: city,
          workspace: w,
          mapState: viewport,
          discovery: discovery,
          onInitialViewport: (_) {},
          onMapUnavailable: (_) {},
        ),
      ),
    );
    final map = find.byType(PublicCityMapView).evaluate().single;
    final mapState = t.state(find.byType(PublicCityMapView));
    final oldView = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    final previous = MapCameraFocusInput(
      city: _city(),
      contextKey: history.taskID,
      places: oldView.places,
      selectedEntityId: null,
      entities: oldView.entities,
    );
    await f.elapse(t, 20);
    f.pendingPost = Completer<http.Response>();
    final query = w.submit('找地点 C', [], [], cityID: history.cityID);
    await t.pump();
    expect(w.state, AgentViewState.searching);
    expect(w.preserveReplyCamera, false);
    await f.elapse(t, 11);
    expect(before.replyProjectionCurrent, false);
    final emptyView = t.widget<PublicCityMapView>(
      find.byType(PublicCityMapView),
    );
    expect(emptyView.entities, isEmpty);
    expect(emptyView.places, isEmpty);
    expect(emptyView.preserveViewport, true);
    final pendingCamera = MapCameraFocusInput(
      city: _city(),
      contextKey: history.taskID,
      places: emptyView.places,
      selectedEntityId: null,
      entities: emptyView.entities,
      preserveViewport: emptyView.preserveViewport,
    );
    expect(pendingCamera.shouldFocusAfter(previous), false);
    expect(viewport.viewportBounds, same(bounds));
    expect(find.byType(PublicCityMapView).evaluate().single, same(map));
    expect(t.state(find.byType(PublicCityMapView)), same(mapState));
    final wire = history.historyWire(f.now);
    final task = wire['task'] as Map<String, dynamic>;
    final messages = task['conversation'] as List;
    messages.addAll(<Map<String, dynamic>>[
      {'role': 'user', 'text': '找地点 C'},
      {'role': 'assistant', 'text': '地点 C'},
    ]);
    final digest = agentReplyTurnDigest(
      messages.map(
        (m) => (role: m['role'] as String, text: m['text'] as String),
      ),
    );
    final setID = 'reply:${history.taskID}:${digest.substring(0, 24)}';
    messages.last['resultMembership'] = {
      'schema': agentReplyMembershipSchema,
      'taskId': history.taskID,
      'cityId': history.cityID,
      'kind': 'place',
      'turnDigest': digest,
      'resultSetId': setID,
      'refs': [
        {'type': 'place', 'id': history.placeB},
      ],
    };
    final histories = wire['messageResults'] as List;
    final latest =
        jsonDecode(jsonEncode(histories.last)) as Map<String, dynamic>;
    latest['messageIndex'] = 5;
    latest['turnDigest'] = digest;
    final set = latest['resultSet'] as Map;
    set['id'] = setID;
    (set['items'] as List).single['anchor']['latitude'] = 58.2;
    histories.add(latest);
    task['filters'] = {'currentQuery': '找地点 C', 'searchTerm': 'C'};
    wire['message'] = '地点 C';
    wire['resultSet'] = set;
    wire['mapEffects'] = latest['mapEffects'];
    f.pendingPost!.complete(
      http.Response.bytes(utf8.encode(jsonEncode({'data': wire})), 200),
    );
    await query;
    expect(w.state, AgentViewState.results);
    expect(w.task!.id, history.taskID);
    expect(w.conversation, hasLength(6));
    expect(w.result!.entities.single.latitude, 58.2);
    expect(w.preserveReplyCamera, false);
    final current = MapCameraFocusInput(
      city: _city(),
      contextKey: history.taskID,
      places: [],
      selectedEntityId: null,
      entities: w.result!.entities,
      preserveViewport: w.preserveReplyCamera,
    );
    expect(current.shouldFocusAfter(pendingCamera), true);
    await t.pump();
    expect(
      t
          .widget<PublicCityMapView>(find.byType(PublicCityMapView))
          .preserveViewport,
      false,
    );
    expect(find.byType(PublicCityMapView).evaluate().single, same(map));
    expect(t.state(find.byType(PublicCityMapView)), same(mapState));
    expect(f.methods, ['GET', 'POST']);
    f.dispose();
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('actual MapCanvas过期legacyplaces不渲染旧坐标或退回城市目录', (t) async {
    final city = _City();
    addTearDown(city.dispose);
    final w = AgentWorkspaceController();
    addTearDown(w.dispose);
    final discovery = NowDiscoveryController(authorizationHeader: () => null);
    addTearDown(discovery.dispose);
    final viewport = MapViewportState();
    addTearDown(viewport.dispose);
    w.task = AgentTask(
      id: history.taskID,
      query: 'synthetic',
      status: 'COMPLETED',
      cityID: history.cityID,
    );
    w.result = AgentResult(
      entities: [],
      activities: [],
      note: '',
      places: [
        PublicPlace(
          id: history.placeA,
          name: 'synthetic legacy point',
          categoryCode: 'culture',
          summary: 'synthetic',
          source: _source,
          location: const PublicPlaceLocation(
            coordinateSystem: 'wgs84',
            precision: 'point',
            latitude: 57,
            longitude: -2,
          ),
        ),
      ],
      replyProjection: AgentReplyProjectionLifetime(
        validUntil: DateTime.utc(1970),
        current: () => false,
      ),
    );
    await t.pumpWidget(
      MaterialApp(
        home: MapCanvas(
          city: city,
          workspace: w,
          mapState: viewport,
          discovery: discovery,
          onInitialViewport: (_) {},
          onMapUnavailable: (_) {},
        ),
      ),
    );
    final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(view.entities, isEmpty);
    expect(view.places, isEmpty);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('GET503不改变原查询状态，不循环重读或付费POST', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final w = f.workspace, task = w.task, messages = List.of(w.conversation);
    f.status = 503;
    await f.elapse(t, 25);
    await f.elapse(t, 90);
    f.visible = false;
    w.updateReplyRefreshVisibility();
    f.visible = true;
    w.updateReplyRefreshVisibility();
    await t.pump();
    expect(f.methods, ['GET', 'GET']);
    expect(w.task, same(task));
    expect(w.conversation, messages);
    expect(w.requestError, isNull);
    expect(w.state, AgentViewState.results);
    expect(w.result!.entities, isEmpty);
    f.dispose();
  });

  testWidgets('后台零GET，前台恢复过期卡片且同一flight只读一次', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    f.visible = false;
    f.workspace.updateReplyRefreshVisibility();
    await f.elapse(t, 60);
    expect(f.methods, ['GET']);
    expect(f.workspace.result!.entities, isEmpty);
    f.pending = Completer<http.Response>();
    f.visible = true;
    f.workspace.updateReplyRefreshVisibility();
    final a = f.workspace.refreshReplyProjections(),
        b = f.workspace.refreshReplyProjections();
    expect(identical(a, b), true);
    await t.pump();
    expect(f.methods, ['GET', 'GET']);
    f.pending!.complete(f.response());
    await t.pump();
    expect(await a, true);
    expect(f.workspace.result!.entities.single.id, 'place:${history.placeB}');
    f.dispose();
  });

  for (final change in [
    'token',
    'org',
    'city',
    'online',
    'owner',
    'epoch',
    'task',
    'dispose',
  ]) {
    testWidgets('等待GET期间$change ABA同步永久退休，无中间current读取也不回写', (t) async {
      final f = _Reads();
      await f.open();
      final w = f.workspace, old = w.result;
      f.pending = Completer<http.Response>();
      final read = w.refreshReplyProjections();
      await t.pump();
      switch (change) {
        case 'token':
          f.token = 'Bearer synthetic-B';
          f.retire();
          f.token = 'Bearer synthetic-original';
        case 'org':
          f.org = 'synthetic-B';
          f.retire();
          f.org = null;
        case 'city':
          f.city = 'synthetic-B';
          f.retire();
          f.city = history.cityID;
        case 'online':
          f.online = 'synthetic-B';
          f.retire();
          f.online = null;
        case 'owner':
          f.owner = history.placeA;
          f.retire();
          f.owner = history.ownerID;
        case 'epoch':
          f.retire();
        case 'task':
          w.newTask();
        case 'dispose':
          w.dispose();
      }
      f.pending!.complete(f.response());
      await t.pump();
      expect(await read, false);
      if (change == 'task') {
        expect(w.task, isNull);
        expect(w.result, isNull);
      } else {
        expect(w.result, same(old));
      }
      expect(f.methods, ['GET', 'GET']);
      if (change != 'dispose') w.dispose();
    });
  }

  for (final change in [
    'messages',
    'filters',
    'membership',
    'empty_history',
    'expired',
    'missing_history',
  ]) {
    testWidgets('新GET$change不得把其他原文或membership嫁接当前轮', (t) async {
      final f = _Reads();
      addTearDown(f.dispose);
      await f.open();
      final w = f.workspace, old = w.result, messages = List.of(w.conversation);
      f.mutate = (wire) {
        switch (change) {
          case 'messages':
            wire['task']['conversation'][0]['text'] = 'changed';
          case 'filters':
            wire['task']['filters']['searchTerm'] = 'changed';
          case 'membership':
            wire['task']['conversation'][1]['resultMembership']['refs'][0]['id'] =
                history.placeB;
          case 'empty_history':
            wire['messageResults'] = <dynamic>[];
          case 'expired':
            for (final entry in wire['messageResults']) {
              entry['validUntil'] = f.now.toIso8601String();
            }
          case 'missing_history':
            wire.remove('messageResults');
        }
      };
      final accepted = await w.refreshReplyProjections();
      if (change == 'empty_history') {
        expect(accepted, true);
        expect(w.replies, isEmpty);
        expect(w.result!.entities, isEmpty);
      } else {
        expect(accepted, false);
        expect(w.result, same(old));
      }
      expect(w.conversation, messages);
      expect(f.methods, ['GET', 'GET']);
      f.dispose();
    });
  }

  testWidgets('GET中途后台再前台丢弃旧响应，仅顺序执行一个新的授权GET', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final old = f.workspace.result;
    f.pending = Completer<http.Response>();
    final first = f.workspace.refreshReplyProjections();
    await t.pump();
    f.visible = false;
    f.workspace.updateReplyRefreshVisibility();
    f.visible = true;
    f.workspace.updateReplyRefreshVisibility();
    f.workspace.updateReplyRefreshVisibility();
    expect(f.methods, ['GET', 'GET']);
    f.pending!.complete(f.response());
    await t.pump();
    expect(await first, false);
    await t.pump();
    expect(f.methods, ['GET', 'GET', 'GET']);
    expect(f.workspace.result, isNot(same(old)));
    f.dispose();
  });

  testWidgets('等待GET时原filters被改变不回写新投影，原GET不重发', (t) async {
    final f = _Reads();
    addTearDown(f.dispose);
    await f.open();
    final old = f.workspace.result;
    f.pending = Completer<http.Response>();
    final read = f.workspace.refreshReplyProjections();
    await t.pump();
    f.workspace.task!.filters['searchTerm'] = 'changed';
    f.mutate = (wire) {
      wire['task']['filters']['searchTerm'] = 'changed';
    };
    f.pending!.complete(f.response());
    await t.pump();
    expect(await read, false);
    expect(f.workspace.result, same(old));
    expect(f.methods, ['GET', 'GET']);
    f.dispose();
  });

  for (final transition in ['app', 'route', 'cityABA', 'authABA']) {
    testWidgets('原MapWorkspace $transition listener实际围栏与安静GET', (t) async {
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final f = _Reads();
      final city = _ObservedCity(f);
      final auth = SeedTestAuth()
        ..token = f.token
        ..owner = history.ownerID;
      final client = MockClient(
        (r) async => http.Response(
          jsonEncode({'data': []}),
          200,
          headers: {'content-type': 'application/json'},
        ),
      );
      final moments = PrivateMomentController(
        client: client,
        apiBaseUrl: 'http://synthetic.test',
        authorizationHeader: () => auth.authorizationHeader,
      );
      final navigator = GlobalKey<NavigatorState>();
      await t.pumpWidget(
        MaterialApp(
          navigatorKey: navigator,
          home: MapWorkspace(
            city: city,
            auth: auth,
            moments: moments,
            agentTaskSource: f.source,
            seedClient: client,
            seedApiBaseUrl: 'http://synthetic.test',
          ),
        ),
      );
      await t.pumpAndSettle();
      final w = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
      await w.reopen(
        AgentTask.fromJson(history.historyWire(f.now)['task']),
        [],
        [],
      );
      await t.pump();
      final old = w.result;
      if (transition == 'app' || transition == 'route') {
        if (transition == 'app') {
          t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
          t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
          t.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
        } else {
          unawaited(
            navigator.currentState!.push<void>(
              MaterialPageRoute(
                builder: (_) => const Scaffold(body: Text('synthetic child')),
              ),
            ),
          );
          await t.pumpAndSettle();
        }
        f.now = f.now.add(const Duration(seconds: 60));
        await t.pump(const Duration(seconds: 60));
        await t.pump();
        expect(f.methods, ['GET']);
        expect(old!.entities, isEmpty);
        if (transition == 'app') {
          t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
          t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
          t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
        } else {
          navigator.currentState!.pop();
          await t.pumpAndSettle();
        }
        await t.pump();
        expect(f.methods, ['GET', 'GET']);
        expect(w.result!.entities.single.id, 'place:${history.placeB}');
      } else {
        f.pending = Completer<http.Response>();
        final read = w.refreshReplyProjections();
        await t.pump();
        if (transition == 'cityABA') {
          city.selectCity('synthetic-other');
          city.selectCity(history.cityID);
        } else {
          auth.changeIdentity('Bearer synthetic-B');
          auth.changeIdentity(f.token, nextOwner: history.ownerID);
        }
        f.pending!.complete(f.response());
        await t.pump();
        expect(await read, false);
        if (transition == 'cityABA') {
          expect(w.result, same(old));
        } else {
          expect(w.task, isNull);
          expect(w.result, isNull);
        }
        expect(f.methods, ['GET', 'GET']);
      }
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      moments.dispose();
      city.dispose();
      auth.dispose();
      client.close();
      expect(f.allMethods, everyElement('GET'));
      expect(t.takeException(), isNull);
    });
  }

  test('unsupported source GET端口不能回落到resolve/restore写入', () async {
    final source = _Unsupported();
    final task = AgentTask.fromJson(
      history.historyWire(DateTime.utc(2026, 10, 8))['task'],
    );
    expect(source.canRereadReplyProjections(task), false);
    expect(await source.rereadReplyProjections(task), isNull);
    expect(source.writes, 0);
  });
  for (final kind in [
    'guest',
    'organization',
    'local',
    'online',
    'foreign_owner',
    'foreign_city',
  ]) {
    test('原remote $kind 不支持安静重读，零HTTP', () async {
      final f = _Reads();
      var task = AgentTask.fromJson(history.historyWire(f.now)['task']);
      switch (kind) {
        case 'guest':
          f.token = null;
        case 'organization':
          f.org = 'synthetic';
        case 'local':
          task = AgentTask(
            id: 'local-1',
            query: 'x',
            status: 'COMPLETED',
            cityID: history.cityID,
          );
        case 'online':
          task = AgentTask(
            id: history.taskID,
            query: 'x',
            status: 'COMPLETED',
            contextType: 'ONLINE',
          );
        case 'foreign_owner':
          f.owner = history.placeA;
        case 'foreign_city':
          f.city = 'synthetic-other';
      }
      expect(await f.source.rereadReplyProjections(task), isNull);
      expect(f.methods, isEmpty);
      f.source.dispose();
    });
  }

  test('共享camera seam静默更新空→新点与撤权保视角，新任务和用户点选仍focus', () {
    MapCameraFocusInput input(
      List<MapEntity> entities, {
      bool quiet = false,
      String? selected,
      String task = 'task-a',
    }) => MapCameraFocusInput(
      city: _city(),
      contextKey: task,
      places: [],
      entities: entities,
      selectedEntityId: selected,
      preserveViewport: quiet,
    );
    final old = input([_entity('a', 57)], selected: 'a');
    final expired = input([], quiet: true, selected: 'a');
    expect(expired.shouldFocusAfter(old), false);
    expect(expired.hasSameViewInputs(old), false);
    final fresh = input([_entity('a', 58)], quiet: true, selected: 'a');
    expect(fresh.shouldFocusAfter(expired), false);
    final cleared = input([], quiet: true);
    expect(cleared.shouldFocusAfter(fresh), false);
    expect(
      input([_entity('b', 59)], selected: 'b').shouldFocusAfter(cleared),
      true,
    );
    expect(
      input(
        [_entity('b', 59)],
        quiet: true,
        task: 'task-b',
      ).shouldFocusAfter(cleared),
      true,
    );
  });
  testWidgets(
    'actual native SDK seam静默更新pins不飞镜，旧bounds迟到仍被原围栏退休',
    (t) async {
      final f = await _QuietNativeFixture.mount(t);
      final pending = Completer<CameraOptions>();
      f.map.pendingFit = pending;
      await f.update(entities: [_entity('a', 57), _entity('b', 58)]);
      expect(f.map.boundsRequests, 1);
      final mapElement = find.byType(MapWidget).evaluate().single;
      await f.update(entities: [], quiet: true);
      await f.update(entities: [_entity('a', 60)], selected: 'a', quiet: true);
      expect(f.map.flights, isEmpty);
      expect(find.byType(MapWidget).evaluate().single, same(mapElement));
      pending.complete(
        CameraOptions(center: Point(coordinates: Position(10, 10)), zoom: 5),
      );
      await t.pump();
      expect(f.map.flights, isEmpty);
      await f.update(entities: [_entity('b', 59)], selected: 'b');
      expect(f.map.flights, hasLength(1));
      await f.close();
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
}

CameraState _camera() => CameraState(
  center: Point(coordinates: Position(-3, 54)),
  padding: MbxEdgeInsets(top: 0, left: 0, bottom: 0, right: 0),
  zoom: 9,
  bearing: 0,
  pitch: 0,
);

class _Cancel extends Fake implements Cancelable {
  @override
  void cancel() {}
}

class _Points extends Fake implements PointAnnotationManager {
  @override
  Future<void> setIconAllowOverlap(bool value) async {}
  @override
  Cancelable tapEvents({required Function(PointAnnotation) onTap}) => _Cancel();
  @override
  Future<PointAnnotation> create(PointAnnotationOptions option) async =>
      PointAnnotation(
        id: option.customData!['entityId'] as String,
        geometry: option.geometry,
        image: option.image,
        customData: option.customData,
      );
  @override
  Future<void> update(PointAnnotation annotation) async {}
  @override
  Future<void> delete(PointAnnotation annotation) async {}
}

class _Annotations extends Fake implements AnnotationManager {
  final _Points points = _Points();
  @override
  Future<PointAnnotationManager> createPointAnnotationManager({
    String? id,
    String? below,
  }) async => points;
}

class _Map extends Fake implements MapboxMap {
  @override
  final AnnotationManager annotations = _Annotations();
  final flights = <CameraOptions>[], restored = <CameraOptions>[];
  Completer<CameraOptions>? pendingFit;
  Completer<CameraState>? pendingCamera;
  int boundsRequests = 0, styleReloads = 0;
  @override
  Future<CameraState> getCameraState() {
    final pending = pendingCamera;
    pendingCamera = null;
    return pending?.future ?? Future.value(_camera());
  }

  @override
  Future<CameraOptions> cameraForCoordinateBounds(
    CoordinateBounds bounds,
    MbxEdgeInsets padding,
    double? bearing,
    double? pitch,
    double? maxZoom,
    ScreenCoordinate? offset,
  ) {
    boundsRequests++;
    final pending = pendingFit;
    pendingFit = null;
    return pending?.future ??
        Future.value(CameraOptions(center: bounds.southwest, zoom: 12));
  }

  @override
  Future<void> flyTo(
    CameraOptions camera,
    MapAnimationOptions? animation,
  ) async {
    flights.add(camera);
  }

  @override
  Future<void> setCamera(CameraOptions camera) async {
    restored.add(camera);
  }

  @override
  Future<void> loadStyleURI(String style) async {
    styleReloads++;
  }
}

class _QuietNativeFixture {
  _QuietNativeFixture(this.t);
  final WidgetTester t;
  final map = _Map();
  static Future<_QuietNativeFixture> mount(WidgetTester t) async {
    const channel =
        'dev.flutter.pigeon.mapbox_maps_flutter._MapboxOptions.setAccessToken';
    t.binding.defaultBinaryMessenger.setMockMessageHandler(
      channel,
      (_) async => const StandardMessageCodec().encodeMessage([null]),
    );
    addTearDown(
      () =>
          t.binding.defaultBinaryMessenger.setMockMessageHandler(channel, null),
    );
    final pending = Completer<Object?>();
    t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform_views,
      (call) async => call.method == 'create' ? pending.future : null,
    );
    addTearDown(
      () => t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform_views,
        null,
      ),
    );
    final f = _QuietNativeFixture(t);
    await f.update();
    t.widget<MapWidget>(find.byType(MapWidget)).onMapCreated!(f.map);
    await f.styleLoaded();
    f.map.flights.clear();
    return f;
  }

  Future<void> update({
    List<MapEntity> entities = const [],
    String? selected,
    String context = 'task-a',
    bool quiet = false,
  }) async {
    await t.pumpWidget(
      MaterialApp(
        home: NativeCityMapView(
          key: const ValueKey('native-camera-fixture'),
          city: _city(),
          places: const [],
          entities: entities,
          selectedEntityId: selected,
          contextKey: context,
          preserveViewport: quiet,
          accessToken: 'pk.synthetic_widget_only',
          onPlaceSelected: (_) {},
        ),
      ),
    );
    await t.pump();
  }

  Future<void> styleLoaded() async {
    t.widget<MapWidget>(find.byType(MapWidget)).onStyleLoadedListener!(
      StyleLoadedEventData.fromJson({
        'timeInterval': {'begin': 0, 'end': 1},
      }),
    );
    await t.pump();
    await t.pump();
  }

  Future<void> failure() async {
    t.widget<MapWidget>(find.byType(MapWidget)).onMapLoadErrorListener!(
      MapLoadingErrorEventData.fromJson({
        'type': MapLoadErrorType.TILE.index,
        'timestamp': 0,
        'message': 'HTTP 403',
        'sourceId': null,
        'tileId': null,
      }),
    );
    await t.pump();
  }

  Future<void> close() async {
    await t.pumpWidget(const SizedBox());
    await t.pump();
  }
}
