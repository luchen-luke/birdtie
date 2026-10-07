import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'agent_task_detail_page_test.dart'
    show taskDestinationID, taskDestinationData, taskReply;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:flutter_test/flutter_test.dart';
import 'now_scope_recovery_test.dart';
import 'agent_result_projection_test.dart' show typedItem;

const _unsupportedMessage = '暂时无法处理这类请求。你可以搜索活动、组织或地点。';

// Exact existing WithContract shape, synthetic values only; not a native API
// capture, a real identity, or a verified organization/place.
Map<String, dynamic> _publicQueryWire({
  String status = 'unsupported',
  String message = _unsupportedMessage,
  String? kind,
}) {
  final item = kind == null ? null : typedItem(kind, 'fixture-$kind');
  return {
    'cityId': 'alpha',
    'mode': 'rules',
    'query': '合成查询',
    'principalType': 'ANONYMOUS',
    'principalId': '',
    'workspace': 'PERSONAL',
    'permissions': [],
    'activities': [],
    'people': [],
    'groups': [],
    'organizations': [
      if (kind == 'organization')
        {
          'id': 'fixture-organization',
          'name': '原实体中文标题',
          'description': '合成公开组织，不是真实合作方',
          'verificationStatus': 'unverified',
        },
    ],
    'places': [
      if (kind == 'place')
        {
          'id': 'fixture-place',
          'name': '原实体中文标题',
          'categoryCode': 'sports_venue',
          'summary': '合成公开地点',
          'source': {'label': '组件测试'},
          'location': {'coordinateSystem': 'wgs84', 'precision': 'none'},
        },
    ],
    'message': message,
    'note': '',
    'requestId': 'synthetic-public-query',
    'resultSet': {
      'schema': 'typed-agent-results-v1',
      'id': 'synthetic-result-set',
      'status': status,
      'generatedAt': '2026-10-06T00:00:00Z',
      'items': [?item],
      'entities': [if (item != null) item['entityRef']],
    },
    'actions': [],
    'followUps': [],
    'mapEffects': {'camera': 'preserve', 'pinEntityIds': []},
  };
}

http.Response _publicReply(Map<String, dynamic> wire) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': wire})),
  200,
  headers: {
    'content-type': 'application/json; charset=utf-8',
    'x-request-id': 'synthetic-public-query',
  },
);

Future<void> _mountPublicRemote(
  WidgetTester t,
  NowFixture f,
  RemoteAgentTaskSource source,
) async {
  await t.pumpWidget(
    MaterialApp(
      home: MapWorkspace(
        city: f.city,
        auth: f.auth,
        moments: f.moments,
        agentTaskSource: source,
        seedClient: f.client,
        seedApiBaseUrl: f.base,
      ),
    ),
  );
  await t.pumpAndSettle();
}

// Byte-preserved registered HTTP + native096 replies, synthetic fixture
// identities/data only; the HTTP transport below replays these responses.
final _nativeQueryCaptures =
    (jsonDecode(r'''[
  {
    "name": "WEEKEND-000",
    "originalSHA256": "57ba75c69cb373848b66da61d35df64c19c2b1980357355535776b44606eeb1c",
    "method": "POST",
    "path": "/v1/cities/builder033-6a138f82-9d44-4017-9958-2c63692cb11b/agent/tasks",
    "requestBody": "{\"query\":\"weekend\"}",
    "requestID": "2b26416742a67939c62a666af3a5d630",
    "responseBody": "{\"data\":{\"commercialTrustVersion\":\"sponsored-opportunity-v1\",\"sponsoredStatus\":\"available\",\"sponsoredOpportunities\":[],\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"query\":\"weekend\",\"mode\":\"rules\",\"note\":\"\",\"message\":\"暂时无法处理这类请求。你可以搜索活动、组织或地点。\",\"principalType\":\"PERSON\",\"principalId\":\"\",\"workspace\":\"PERSONAL\",\"permissions\":[\"city_context.read\",\"relationship_context.read\"],\"activities\":[],\"people\":[],\"groups\":[],\"organizations\":[],\"places\":[],\"followUps\":[],\"requestId\":\"2b26416742a67939c62a666af3a5d630\",\"resultSet\":{\"schema\":\"typed-agent-results-v1\",\"id\":\"2b26416742a67939c62a666af3a5d630\",\"query\":\"weekend\",\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"entities\":[],\"items\":[],\"filters\":{\"currentQuery\":\"weekend\",\"locationPreference\":\"\",\"searchTerm\":\"\",\"targetIntent\":\"\"},\"generatedAt\":\"2026-10-06T01:31:33.4296527Z\",\"status\":\"unsupported\"},\"actions\":[],\"mapEffects\":{\"camera\":\"preserve\",\"pinEntityIds\":[]}}}",
    "scope": "ACTUAL_REGISTERED_HTTP_NATIVE096_SYNTHETIC_NOT_IDP_OR_PILOT",
    "status": 200
  },
  {
    "name": "UNSUPPORTED-000",
    "originalSHA256": "d1c0aa9e264988274776d81d738b0296944fc85b268d89e6c9bcd48d46f6265b",
    "method": "POST",
    "path": "/v1/cities/builder033-6a138f82-9d44-4017-9958-2c63692cb11b/agent/tasks",
    "requestBody": "{\"query\":\"帮我买飞机票\"}",
    "requestID": "fb5e4b9fb0ad4259761f0cc8018d844c",
    "responseBody": "{\"data\":{\"commercialTrustVersion\":\"sponsored-opportunity-v1\",\"sponsoredStatus\":\"available\",\"sponsoredOpportunities\":[],\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"query\":\"帮我买飞机票\",\"mode\":\"rules\",\"note\":\"\",\"message\":\"暂时无法处理这类请求。你可以搜索活动、组织或地点。\",\"principalType\":\"PERSON\",\"principalId\":\"\",\"workspace\":\"PERSONAL\",\"permissions\":[\"city_context.read\",\"relationship_context.read\"],\"activities\":[],\"people\":[],\"groups\":[],\"organizations\":[],\"places\":[],\"followUps\":[],\"requestId\":\"fb5e4b9fb0ad4259761f0cc8018d844c\",\"resultSet\":{\"schema\":\"typed-agent-results-v1\",\"id\":\"fb5e4b9fb0ad4259761f0cc8018d844c\",\"query\":\"帮我买飞机票\",\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"entities\":[],\"items\":[],\"filters\":{\"currentQuery\":\"帮我买飞机票\",\"locationPreference\":\"\",\"searchTerm\":\"\",\"targetIntent\":\"\"},\"generatedAt\":\"2026-10-06T01:31:33.444152Z\",\"status\":\"unsupported\"},\"actions\":[],\"mapEffects\":{\"camera\":\"preserve\",\"pinEntityIds\":[]}}}",
    "scope": "ACTUAL_REGISTERED_HTTP_NATIVE096_SYNTHETIC_NOT_IDP_OR_PILOT",
    "status": 200
  },
  {
    "name": "CLARIFY-000",
    "originalSHA256": "0ac66e91553f95d1e2d349bccab12d4c21f7b7c64185738586ce6d636494b7d2",
    "method": "POST",
    "path": "/v1/cities/builder033-6a138f82-9d44-4017-9958-2c63692cb11b/agent/tasks",
    "requestBody": "{\"query\":\"近一点的呢？\"}",
    "requestID": "b6cac6ecfa76039a9c5a0f39f5aa79c1",
    "responseBody": "{\"data\":{\"commercialTrustVersion\":\"sponsored-opportunity-v1\",\"sponsoredStatus\":\"available\",\"sponsoredOpportunities\":[],\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"query\":\"近一点的呢？\",\"mode\":\"rules\",\"note\":\"\",\"message\":\"请先搜索活动，再继续筛选或比较结果。\",\"principalType\":\"PERSON\",\"principalId\":\"\",\"workspace\":\"PERSONAL\",\"permissions\":[\"city_context.read\",\"relationship_context.read\"],\"activities\":[],\"people\":[],\"groups\":[],\"organizations\":[],\"places\":[],\"followUps\":[],\"requestId\":\"b6cac6ecfa76039a9c5a0f39f5aa79c1\",\"resultSet\":{\"schema\":\"typed-agent-results-v1\",\"id\":\"b6cac6ecfa76039a9c5a0f39f5aa79c1\",\"query\":\"近一点的呢？\",\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"entities\":[],\"items\":[],\"filters\":{\"currentQuery\":\"近一点的呢？\",\"locationPreference\":\"\",\"searchTerm\":\"\",\"targetIntent\":\"\"},\"generatedAt\":\"2026-10-06T01:31:33.4492938Z\",\"status\":\"unsupported\"},\"actions\":[],\"mapEffects\":{\"camera\":\"preserve\",\"pinEntityIds\":[]}}}",
    "scope": "ACTUAL_REGISTERED_HTTP_NATIVE096_SYNTHETIC_NOT_IDP_OR_PILOT",
    "status": 200
  },
  {
    "name": "BOUNDS_REQUIRED-000",
    "originalSHA256": "5ed27c865524bea1371a9f0a33d2707ed20964c152404e2973030dea35dd8baa",
    "method": "POST",
    "path": "/v1/cities/builder033-6a138f82-9d44-4017-9958-2c63692cb11b/agent/tasks",
    "requestBody": "{\"query\":\"搜索此区域\"}",
    "requestID": "41c9b0e942125a97c6b7ada88d87eded",
    "responseBody": "{\"data\":{\"commercialTrustVersion\":\"sponsored-opportunity-v1\",\"sponsoredStatus\":\"available\",\"sponsoredOpportunities\":[],\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"query\":\"搜索此区域\",\"mode\":\"rules\",\"note\":\"\",\"message\":\"请先移动地图并选定搜索范围。\",\"principalType\":\"PERSON\",\"principalId\":\"\",\"workspace\":\"PERSONAL\",\"permissions\":[\"city_context.read\",\"relationship_context.read\"],\"activities\":[],\"people\":[],\"groups\":[],\"organizations\":[],\"places\":[],\"followUps\":[],\"requestId\":\"41c9b0e942125a97c6b7ada88d87eded\",\"resultSet\":{\"schema\":\"typed-agent-results-v1\",\"id\":\"41c9b0e942125a97c6b7ada88d87eded\",\"query\":\"搜索此区域\",\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"entities\":[],\"items\":[],\"filters\":{\"currentQuery\":\"搜索此区域\",\"locationPreference\":\"viewport\",\"searchTerm\":\"\",\"targetIntent\":\"FIND_ACTIVITY\"},\"generatedAt\":\"2026-10-06T01:31:33.4553987Z\",\"status\":\"unsupported\"},\"actions\":[],\"mapEffects\":{\"camera\":\"preserve\",\"pinEntityIds\":[]}}}",
    "scope": "ACTUAL_REGISTERED_HTTP_NATIVE096_SYNTHETIC_NOT_IDP_OR_PILOT",
    "status": 200
  },
  {
    "name": "PUBLIC_PLACE-000",
    "originalSHA256": "8b3653efdfc8ab3f33ce0a743c989a93e1be7356186a43f4fb9f28156a06c757",
    "method": "POST",
    "path": "/v1/cities/builder033-6a138f82-9d44-4017-9958-2c63692cb11b/agent/tasks",
    "requestBody": "{\"query\":\"找地点\"}",
    "requestID": "23279aaadc38ea76c739c70c942de20b",
    "responseBody": "{\"data\":{\"commercialTrustVersion\":\"sponsored-opportunity-v1\",\"sponsoredStatus\":\"available\",\"sponsoredOpportunities\":[],\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"query\":\"找地点\",\"mode\":\"rules\",\"note\":\"结果来自当前城市已发布的真实地点。\",\"message\":\"找到 1 个已发布地点。\",\"principalType\":\"PERSON\",\"principalId\":\"\",\"workspace\":\"PERSONAL\",\"permissions\":[\"city_context.read\",\"relationship_context.read\"],\"activities\":[],\"people\":[],\"groups\":[],\"organizations\":[],\"places\":[{\"id\":\"5c3bea2a-41ab-4d6d-8e09-8e31558d016b\",\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"name\":\"合成 Context 地点\",\"categoryCode\":\"sports\",\"summary\":\"\",\"location\":{\"coordinateSystem\":\"wgs84\",\"precision\":\"point\",\"latitude\":57.15,\"longitude\":-2.1},\"source\":{\"label\":\"LOCAL_SYNTHETIC_FIXTURE\",\"reference\":\"local:AGE033\",\"maintainer\":\"合成维护者\",\"updatedAt\":\"2026-10-06T09:31:33.331392+08:00\",\"freshness\":\"unverified\"}}],\"followUps\":[],\"requestId\":\"23279aaadc38ea76c739c70c942de20b\",\"resultSet\":{\"schema\":\"typed-agent-results-v1\",\"id\":\"23279aaadc38ea76c739c70c942de20b\",\"query\":\"找地点\",\"cityId\":\"builder033-6a138f82-9d44-4017-9958-2c63692cb11b\",\"entities\":[{\"type\":\"place\",\"id\":\"5c3bea2a-41ab-4d6d-8e09-8e31558d016b\"}],\"items\":[{\"entityRef\":{\"type\":\"place\",\"id\":\"5c3bea2a-41ab-4d6d-8e09-8e31558d016b\"},\"title\":\"合成 Context 地点\",\"summary\":\"\",\"scope\":\"AUTHORIZED_VIEW\",\"detailRef\":{\"type\":\"place\",\"id\":\"5c3bea2a-41ab-4d6d-8e09-8e31558d016b\"},\"shareRef\":{\"type\":\"place\",\"id\":\"5c3bea2a-41ab-4d6d-8e09-8e31558d016b\"},\"anchor\":{\"coordinateSystem\":\"wgs84\",\"precision\":\"point\",\"latitude\":57.15,\"longitude\":-2.1,\"placeId\":\"5c3bea2a-41ab-4d6d-8e09-8e31558d016b\"}}],\"filters\":{\"currentQuery\":\"找地点\",\"locationPreference\":\"city\",\"searchTerm\":\"\",\"targetIntent\":\"FIND_PLACE\"},\"generatedAt\":\"2026-10-06T01:31:33.4605326Z\",\"status\":\"ready\"},\"actions\":[],\"mapEffects\":{\"camera\":\"preserve\",\"pinEntityIds\":[\"place:5c3bea2a-41ab-4d6d-8e09-8e31558d016b\"]}}}",
    "scope": "ACTUAL_REGISTERED_HTTP_NATIVE096_SYNTHETIC_NOT_IDP_OR_PILOT",
    "status": 200
  },
  {
    "name": "PUBLIC_ORGANIZATION-000",
    "originalSHA256": "806b41e26d6cbf5d085611e11c2bfc8c102b48987c149ff924608f6e6fc763da",
    "method": "POST",
    "path": "/v1/cities/aberdeen-gb/agent/tasks",
    "requestBody": "{\"query\":\"找组织\"}",
    "requestID": "8e1dd522ecc2ada1032431120ececc56",
    "responseBody": "{\"data\":{\"commercialTrustVersion\":\"sponsored-opportunity-v1\",\"sponsoredStatus\":\"available\",\"sponsoredOpportunities\":[],\"cityId\":\"aberdeen-gb\",\"query\":\"找组织\",\"mode\":\"rules\",\"note\":\"仅显示在当前城市有可发现活动的公开组织。\",\"message\":\"找到 2 个公开组织。\",\"principalType\":\"PERSON\",\"principalId\":\"\",\"workspace\":\"PERSONAL\",\"permissions\":[\"city_context.read\",\"relationship_context.read\"],\"activities\":[],\"people\":[],\"groups\":[],\"organizations\":[{\"id\":\"b1700000-0000-4000-8000-000000000013\",\"name\":\"Aberdeen CSSA（虚构本地测试，非官方）\",\"description\":\"仅供本地开发验收的虚构组织，不代表 Aberdeen CSSA 注册、授权或合作。\",\"verificationStatus\":\"unverified\"},{\"id\":\"b1700000-0000-4000-8000-000000000002\",\"name\":\"Birdtie 本地测试羽毛球社\",\"description\":\"仅供本地开发测试的虚构社团。\",\"verificationStatus\":\"unverified\"}],\"places\":[],\"followUps\":[],\"requestId\":\"8e1dd522ecc2ada1032431120ececc56\",\"resultSet\":{\"schema\":\"typed-agent-results-v1\",\"id\":\"8e1dd522ecc2ada1032431120ececc56\",\"query\":\"找组织\",\"cityId\":\"aberdeen-gb\",\"entities\":[{\"type\":\"organization\",\"id\":\"b1700000-0000-4000-8000-000000000013\"},{\"type\":\"organization\",\"id\":\"b1700000-0000-4000-8000-000000000002\"}],\"items\":[{\"entityRef\":{\"type\":\"organization\",\"id\":\"b1700000-0000-4000-8000-000000000013\"},\"title\":\"Aberdeen CSSA（虚构本地测试，非官方）\",\"summary\":\"仅供本地开发验收的虚构组织，不代表 Aberdeen CSSA 注册、授权或合作。\",\"scope\":\"AUTHORIZED_VIEW\",\"detailRef\":{\"type\":\"organization\",\"id\":\"b1700000-0000-4000-8000-000000000013\"},\"shareRef\":{\"type\":\"organization\",\"id\":\"b1700000-0000-4000-8000-000000000013\"}},{\"entityRef\":{\"type\":\"organization\",\"id\":\"b1700000-0000-4000-8000-000000000002\"},\"title\":\"Birdtie 本地测试羽毛球社\",\"summary\":\"仅供本地开发测试的虚构社团。\",\"scope\":\"AUTHORIZED_VIEW\",\"detailRef\":{\"type\":\"organization\",\"id\":\"b1700000-0000-4000-8000-000000000002\"},\"shareRef\":{\"type\":\"organization\",\"id\":\"b1700000-0000-4000-8000-000000000002\"}}],\"filters\":{\"currentQuery\":\"找组织\",\"locationPreference\":\"city\",\"searchTerm\":\"\",\"targetIntent\":\"FIND_ORGANIZATION\"},\"generatedAt\":\"2026-10-06T01:31:33.464659Z\",\"status\":\"ready\"},\"actions\":[],\"mapEffects\":{\"camera\":\"preserve\",\"pinEntityIds\":[]}}}",
    "scope": "ACTUAL_REGISTERED_HTTP_NATIVE096_SYNTHETIC_NOT_IDP_OR_PILOT",
    "status": 200
  }
]''')
            as List<dynamic>)
        .cast<Map<String, dynamic>>();

Future<void> _currentTurnScrollToEnd(
  WidgetTester t, {
  bool settle = true,
}) async {
  final body = find.byType(AgentConversation);
  final list = find.descendant(of: body, matching: find.byType(ListView));
  expect(list, findsOneWidget);
  // Actual inner-list gestures, bounded; never infer an absent lazy child before
  // reaching the current conversation tail.
  for (var i = 0; i < 6; i++) {
    await t.drag(list, const Offset(0, -240));
    if (settle) {
      await t.pumpAndSettle();
    } else {
      await t.pump(const Duration(milliseconds: 100));
    }
  }
  final scrollable = find.descendant(
    of: body,
    matching: find.byType(Scrollable),
  );
  final position = t.state<ScrollableState>(scrollable).position;
  if (!settle) {
    // Wait only for this gesture's finite ballistic motion, not for the active
    // query spinner. Its mocked HTTP response remains pending, under 12s.
    for (var i = 0; i < 20 && position.isScrollingNotifier.value; i++) {
      await t.pump(const Duration(milliseconds: 100));
    }
  }
  expect(position.pixels, closeTo(position.maxScrollExtent, 1));
}

void main() {
  for (final failure in ['401', '403', '503', 'network']) {
    testWidgets('当前轮摘要守卫 真实Now首ready后$failure追问失败不复用旧摘要动作建议', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      final requests = <http.Request>[];
      final oldWire =
          _publicQueryWire(
              status: 'ready',
              message: '上一轮已完成的地点说明',
              kind: 'place',
            )
            ..['actions'] = [
              {'type': 'CREATE_ACTIVITY', 'label': '上一轮的活动动作'},
            ]
            ..['followUps'] = ['上一轮的筛选建议'];
      final source = RemoteAgentTaskSource(
        cityID: () => f.city.selectedCity?.id,
        authorizationHeader: () => f.auth.authorizationHeader,
        apiBaseUrl: 'https://current-turn-fixture.test',
        client: MockClient((r) async {
          requests.add(r);
          expect(r.method, 'POST');
          if (requests.length == 1) return _publicReply(oldWire);
          if (requests.length == 2) {
            if (failure == 'network') {
              throw http.ClientException('synthetic network');
            }
            return http.Response(
              jsonEncode({
                'error': {
                  'code': failure == '401'
                      ? 'unauthorized'
                      : failure == '403'
                      ? 'forbidden'
                      : 'service_unavailable',
                },
              }),
              int.parse(failure),
            );
          }
          return _publicReply(
            _publicQueryWire(
              status: 'ready',
              message: '本次明确重试的地点说明',
              kind: 'place',
            ),
          );
        }),
      );
      await _mountPublicRemote(t, f, source);
      await nowSend(t, '找地点');
      final w = f.workspace(t),
          originalTask = w.task,
          originalResult = w.result;
      expect(w.queryState, AgentQueryState.success);
      await nowTap(t, find.text('继续对话'));
      await _currentTurnScrollToEnd(t);
      final initialBody = find.byType(AgentConversation);
      for (final text in ['1 个地点', '上一轮的活动动作', '上一轮的筛选建议']) {
        final target = find.descendant(
          of: initialBody,
          matching: find.text(text),
        );
        expect(
          target,
          findsOneWidget,
          reason: 'positive current ready control',
        );
        expect(target.hitTestable(), findsOneWidget);
      }
      await nowTap(t, find.text('查看结果'));
      w.selectEntity('place:fixture-place');
      final map = find.byType(MapCanvas).evaluate().single;
      await nowSend(t, '近一点的呢？');
      expect(w.queryState, AgentQueryState.error);
      expect(w.result, same(originalResult));
      expect(w.task, same(originalTask));
      expect(w.selectedEntityId, 'place:fixture-place');
      expect(requests, hasLength(2));
      expect(
        w.requestFailure?.statusCode,
        failure == 'network' ? isNull : int.parse(failure),
      );
      await nowTap(t, find.text('继续对话'));
      expect(find.text('查询未完成'), findsOneWidget);
      if (failure == '401' || failure == '403') {
        expect(find.text('重试'), findsNothing);
        expect(w.permitsUnchangedRetry, isFalse);
        expect(find.text(w.requestFailure!.recoveryMessage!), findsOneWidget);
      } else {
        expect(find.text('重试'), findsOneWidget);
      }
      await _currentTurnScrollToEnd(t);
      final body = find.byType(AgentConversation);
      expect(body, findsOneWidget);
      expect(
        find.descendant(of: body, matching: find.text('上一轮已完成的地点说明')),
        findsOneWidget,
      );
      expect(
        find.descendant(of: body, matching: find.text('近一点的呢？')),
        findsOneWidget,
      );
      expect(
        find.descendant(of: body, matching: find.text('1 个地点')),
        findsNothing,
        reason: 'retained prior result is not the failed current answer',
      );
      expect(
        find.descendant(of: body, matching: find.text('上一轮的活动动作')),
        findsNothing,
      );
      expect(
        find.descendant(of: body, matching: find.text('上一轮的筛选建议')),
        findsNothing,
      );
      expect(find.textContaining('0 个'), findsNothing);
      expect(find.text('当前没有可见结果'), findsNothing);
      if (failure == '401' || failure == '403') {
        expect(requests, hasLength(2));
      } else {
        final list = find.descendant(of: body, matching: find.byType(ListView));
        for (var i = 0; i < 6; i++) {
          await t.drag(list, const Offset(0, 240));
          await t.pumpAndSettle();
        }
        expect(find.text('重试'), findsOneWidget);
        await nowTap(t, find.byKey(const Key('agent-sheet-retry')));
        expect(requests, hasLength(3));
        expect(w.queryState, AgentQueryState.success);
        expect(w.requestError, isNull);
        await _currentTurnScrollToEnd(t);
        expect(
          find.descendant(of: body, matching: find.text('1 个地点')),
          findsOneWidget,
        );
        expect(
          find.descendant(of: body, matching: find.text('本次明确重试的地点说明')),
          findsOneWidget,
        );
      }
      expect(find.byType(MapCanvas).evaluate().single, same(map));
      expect(w.task?.id, originalTask?.id);
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
    });
  }
  for (final answer in ['ready', 'empty', 'unsupported']) {
    testWidgets('当前轮摘要守卫 pending后$answer只呈现当前完成轮且保留历史', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      final held = Completer<http.Response>();
      var posts = 0;
      final oldWire =
          _publicQueryWire(status: 'ready', message: '上轮的历史说明', kind: 'place')
            ..['actions'] = [
              {'type': 'CREATE_ACTIVITY', 'label': '上轮动作'},
            ]
            ..['followUps'] = ['上轮建议'];
      final source = RemoteAgentTaskSource(
        cityID: () => f.city.selectedCity?.id,
        authorizationHeader: () => f.auth.authorizationHeader,
        apiBaseUrl: 'https://current-turn-fixture.test',
        client: MockClient((r) async {
          expect(r.method, 'POST');
          posts++;
          return posts == 1 ? _publicReply(oldWire) : held.future;
        }),
      );
      await _mountPublicRemote(t, f, source);
      await nowSend(t, '找地点');
      final w = f.workspace(t),
          originalResult = w.result,
          originalTask = w.task;
      await nowTap(t, find.text('继续对话'));
      await _currentTurnScrollToEnd(t);
      final body = find.byType(AgentConversation);
      for (final text in ['1 个地点', '上轮动作', '上轮建议']) {
        expect(
          find.descendant(of: body, matching: find.text(text)).hitTestable(),
          findsOneWidget,
        );
      }
      w.selectEntity('place:fixture-place');
      final map = find.byType(MapCanvas).evaluate().single;
      await t.enterText(nowField(), '补充当前轮条件');
      await t.testTextInput.receiveAction(TextInputAction.send);
      await t.pump(const Duration(milliseconds: 100));
      expect(posts, 2);
      expect(w.queryState, AgentQueryState.loading);
      expect(w.result, same(originalResult));
      expect(w.task, same(originalTask));
      expect(w.selectedEntityId, 'place:fixture-place');
      expect(w.contentMode, AgentContentMode.conversation);
      expect(w.sheetExtent, AgentSheetExtent.peek);
      final handle = find.byKey(const Key('agent-sheet-handle'));
      expect(handle.hitTestable(), findsOneWidget);
      await t.tap(handle);
      await t.pump(const Duration(milliseconds: 100));
      expect(w.sheetExtent, AgentSheetExtent.medium);
      await _currentTurnScrollToEnd(t, settle: false);

      expect(
        find
            .descendant(of: body, matching: find.text('正在查找当前公开信息…'))
            .hitTestable(),
        findsOneWidget,
      );
      for (final text in ['1 个地点', '上轮动作', '上轮建议']) {
        expect(
          find.descendant(of: body, matching: find.text(text)),
          findsNothing,
          reason: 'retained ready data is not the still-pending answer',
        );
      }
      expect(find.textContaining('0 个'), findsNothing);
      final currentWire = _publicQueryWire(
        status: answer == 'unsupported' ? 'unsupported' : 'ready',
        kind: answer == 'ready' ? 'place' : null,
        message: answer == 'unsupported' ? _unsupportedMessage : '本轮$answer说明',
      );
      if (answer == 'ready') {
        currentWire['actions'] = [
          {'type': 'CREATE_ACTIVITY', 'label': '本轮动作'},
        ];
        currentWire['followUps'] = ['本轮建议'];
      }
      held.complete(_publicReply(currentWire));
      await t.pumpAndSettle();
      expect(
        w.queryState,
        answer == 'ready'
            ? AgentQueryState.success
            : answer == 'empty'
            ? AgentQueryState.empty
            : AgentQueryState.unsupported,
      );
      expect(w.requestError, isNull);
      expect(w.task?.id, originalTask?.id);
      expect(w.conversation.map((m) => m.text), contains('上轮的历史说明'));
      await _currentTurnScrollToEnd(t);
      expect(
        find.descendant(
          of: body,
          matching: find.text(
            answer == 'unsupported' ? _unsupportedMessage : '本轮$answer说明',
          ),
        ),
        findsOneWidget,
      );
      for (final text in ['上轮动作', '上轮建议']) {
        expect(
          find.descendant(of: body, matching: find.text(text)),
          findsNothing,
        );
      }
      if (answer == 'ready') {
        for (final text in ['1 个地点', '本轮动作', '本轮建议']) {
          expect(
            find.descendant(of: body, matching: find.text(text)).hitTestable(),
            findsOneWidget,
          );
        }
      } else {
        expect(
          find.descendant(of: body, matching: find.text('1 个地点')),
          findsNothing,
        );
        expect(
          find.descendant(of: body, matching: find.text('当前没有可见结果')),
          answer == 'empty' ? findsOneWidget : findsNothing,
        );
      }
      expect(find.textContaining('0 个'), findsNothing);
      expect(find.byType(MapCanvas).evaluate().single, same(map));
      expect(posts, 2);
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
    });
  }
  for (final late in ['unsupported', '503']) {
    testWidgets('当前轮摘要守卫 旧任务迟到$late不能污染新任务对话摘要', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      final held = Completer<http.Response>();
      var posts = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => f.city.selectedCity?.id,
        authorizationHeader: () => f.auth.authorizationHeader,
        apiBaseUrl: 'https://current-turn-fixture.test',
        client: MockClient((r) async {
          posts++;
          if (posts == 2) return held.future;
          final wire = _publicQueryWire(
            status: 'ready',
            kind: 'place',
            message: posts == 1 ? '已退休任务历史' : '新任务当前历史',
          );
          wire['actions'] = [
            {
              'type': 'CREATE_ACTIVITY',
              'label': posts == 1 ? '已退休任务动作' : '新任务动作',
            },
          ];
          wire['followUps'] = [posts == 1 ? '已退休任务建议' : '新任务建议'];
          return _publicReply(wire);
        }),
      );
      await _mountPublicRemote(t, f, source);
      await nowSend(t, '找地点');
      final w = f.workspace(t), oldTask = w.task;
      final map = find.byType(MapCanvas).evaluate().single;
      await t.enterText(nowField(), '旧任务待回条件');
      await t.testTextInput.receiveAction(TextInputAction.send);
      await t.pump(const Duration(milliseconds: 100));
      expect(w.queryState, AgentQueryState.loading);
      expect(posts, 2);
      // Explicit existing controller new-task action, never a failure recovery
      // strategy. The four failed-followup cases above retain all old domain data.
      w.newTask();
      await nowSend(t, '新的地点查询');
      final currentTask = w.task, currentResult = w.result;
      expect(currentTask?.id, isNot(oldTask?.id));
      expect(w.queryState, AgentQueryState.success);
      expect(posts, 3);
      held.complete(
        late == 'unsupported'
            ? _publicReply(_publicQueryWire(message: '已退休任务迟到能力说明'))
            : http.Response('{"error":{"code":"service_unavailable"}}', 503),
      );
      await t.pumpAndSettle();
      expect(w.task, same(currentTask));
      expect(w.result, same(currentResult));
      expect(w.queryState, AgentQueryState.success);
      expect(w.requestError, isNull);
      await nowTap(t, find.text('继续对话'));
      await _currentTurnScrollToEnd(t);
      final body = find.byType(AgentConversation);
      for (final text in ['1 个地点', '新任务动作', '新任务建议']) {
        expect(
          find.descendant(of: body, matching: find.text(text)).hitTestable(),
          findsOneWidget,
        );
      }
      for (final text in ['已退休任务历史', '已退休任务动作', '已退休任务建议', '已退休任务迟到能力说明']) {
        expect(
          find.descendant(of: body, matching: find.text(text)),
          findsNothing,
        );
      }
      expect(find.text('查询未完成'), findsNothing);
      expect(find.text('重试'), findsNothing);
      expect(find.byType(MapCanvas).evaluate().single, same(map));
      expect(posts, 3);
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
    });
  }
  for (final status in [401, 403]) {
    for (final retired in ['turn', 'scope-aba', 'account-aba']) {
      testWidgets(
        'GET recovery late HTTP$status after $retired cannot replace fresh Now task',
        (t) async {
          final f = NowFixture();
          f.auth.token = 'Bearer synthetic-owner';
          addTearDown(f.dispose);
          final held = Completer<http.Response>();
          final requests = <http.Request>[];
          final source = RemoteAgentTaskSource(
            cityID: () => f.city.selectedCity?.id,
            authorizationHeader: () => f.auth.authorizationHeader,
            apiBaseUrl: 'https://get-recovery-fixture.test',
            client: MockClient((r) async {
              if (r.method == 'GET' && r.url.path == '/v1/me/agent-tasks') {
                return http.Response('{"data":[]}', 200);
              }
              requests.add(r);
              if (r.method == 'GET') return held.future;
              expect(jsonDecode(r.body)['query'], '找地点');
              return _publicReply(
                _publicQueryWire(
                  status: 'ready',
                  kind: 'place',
                  message: '新的当前查询公开地点。',
                ),
              );
            }),
          );
          await _mountPublicRemote(t, f, source);
          final w = f.workspace(t);
          final oldRead = w.reopen(
            const AgentTask(
              id: taskDestinationID,
              query: '旧合成历史查询',
              status: 'COMPLETED',
              cityID: 'alpha',
            ),
            [],
            [],
          );
          await t.pump(const Duration(milliseconds: 20));
          expect(w.queryState, AgentQueryState.loading);
          if (retired == 'scope-aba') {
            f.city.selectCity('beta');
            f.city.selectCity('alpha');
            await t.pumpAndSettle();
          }
          if (retired == 'account-aba') {
            f.auth.changeIdentity('Bearer temporary', nextOwner: 'temporary');
            f.auth.changeIdentity('Bearer synthetic-owner', nextOwner: 'owner');
            await t.pumpAndSettle();
          }
          await nowSend(t, '找地点');
          final currentTask = w.task, currentResult = w.result;
          expect(w.queryState, AgentQueryState.success);
          held.complete(
            http.Response(
              jsonEncode({
                'error': {'code': status == 401 ? 'unauthorized' : 'forbidden'},
              }),
              status,
            ),
          );
          await oldRead;
          await t.pumpAndSettle();
          expect(requests.where((r) => r.method == 'GET'), hasLength(1));
          expect(requests.where((r) => r.method == 'POST'), hasLength(1));
          expect(w.task, same(currentTask));
          expect(w.result, same(currentResult));
          expect(w.requestFailure, isNull);
          expect(w.requestError, isNull);
          expect(w.queryState, AgentQueryState.success);
          expect(find.text('重试'), findsNothing);
          expect(t.takeException(), isNull);
          await f.unmount(t);
          source.dispose();
        },
      );
    }
  }

  for (final status in [401, 403]) {
    for (final mode in AgentContentMode.values) {
      for (final extent in [
        AgentSheetExtent.peek,
        AgentSheetExtent.medium,
        AgentSheetExtent.expanded,
      ]) {
        testWidgets(
          'GET recovery actual Now HTTP$status ${mode.name}/${extent.name} has no fake zero or invalid retry',
          (t) async {
            final f = NowFixture();
            f.auth.token = 'Bearer synthetic-owner';
            addTearDown(f.dispose);
            final requests = <http.Request>[];
            final source = RemoteAgentTaskSource(
              cityID: () => f.city.selectedCity?.id,
              authorizationHeader: () => f.auth.authorizationHeader,
              apiBaseUrl: 'https://get-recovery-fixture.test',
              client: MockClient((r) async {
                if (r.method == 'GET' && r.url.path == '/v1/me/agent-tasks') {
                  return http.Response('{"data":[]}', 200);
                }
                requests.add(r);
                return http.Response(
                  jsonEncode({
                    'error': {
                      'code': status == 401 ? 'unauthorized' : 'forbidden',
                    },
                  }),
                  status,
                );
              }),
            );
            await _mountPublicRemote(t, f, source);
            final w = f.workspace(t);
            const previous = AgentTask(
              id: taskDestinationID,
              query: '原合成历史查询',
              status: 'COMPLETED',
              cityID: 'alpha',
              messages: [AgentMessage(role: 'assistant', text: '原合成历史回答')],
            );
            await w.reopen(previous, [], []);
            w.showContent(mode);
            w.setSheetExtent(extent);
            await t.pumpAndSettle();
            debugPrintSynchronously((
              jsonEncode({
                'getRecovery': 'actual-mounted-Remote-Now',
                'status': status,
                'mode': mode.name,
                'extent': extent.name,
                'retryMounted': find.text('重试').evaluate().isNotEmpty,
                'zeroMounted': find.textContaining('0 个').evaluate().isNotEmpty,
                'requestFailureStatus': w.requestFailure?.statusCode,
                'method': requests.single.method,
              })).toString());
            expect(requests.single.method, 'GET');
            expect(
              requests.single.url.path,
              '/v1/me/agent-tasks/$taskDestinationID',
            );
            expect(find.textContaining('0 个'), findsNothing);
            expect(find.text('未找到符合条件的公开内容'), findsNothing);
            expect(w.requestFailure?.statusCode, status);
            expect(w.result, isNull);
            expect(w.task, same(previous));
            expect(w.conversation, previous.messages);
            expect(find.text('重试'), findsNothing);
            final recovery = find.text(
              status == 401
                  ? '请先通过个人资料重新登录，再发起查询。'
                  : '请先确认当前身份和访问权限；恢复权限后再发起查询。',
            );
            await t.ensureVisible(recovery);
            await t.pumpAndSettle();
            expect(recovery.hitTestable(), findsOneWidget);
            await w.retry();
            await t.pumpAndSettle();
            expect(requests, hasLength(1));
            expect(w.sheetExtent, extent);
            expect(w.contentMode, mode);
            expect(t.takeException(), isNull);
            await f.unmount(t);
            source.dispose();
          },
        );
      }
    }
  }
  for (final failure in ['503', 'network']) {
    testWidgets(
      'GET recovery actual Now $failure single readable retry restores same original ID',
      (t) async {
        final f = NowFixture();
        f.auth.token = 'Bearer synthetic-owner';
        addTearDown(f.dispose);
        final requests = <http.Request>[];
        final source = RemoteAgentTaskSource(
          cityID: () => f.city.selectedCity?.id,
          authorizationHeader: () => f.auth.authorizationHeader,
          apiBaseUrl: 'https://get-recovery-fixture.test',
          client: MockClient((r) async {
            if (r.method == 'GET' && r.url.path == '/v1/me/agent-tasks') {
              return http.Response('{"data":[]}', 200);
            }
            requests.add(r);
            if (requests.length == 1) {
              if (failure == 'network') {
                throw http.ClientException('synthetic network');
              }
              return http.Response(
                '{"error":{"code":"service_unavailable"}}',
                503,
              );
            }
            return taskReply(taskDestinationData());
          }),
        );
        await _mountPublicRemote(t, f, source);
        final w = f.workspace(t);
        const previous = AgentTask(
          id: taskDestinationID,
          query: '原合成历史查询',
          status: 'COMPLETED',
          cityID: 'alpha',
        );
        await w.reopen(previous, [], []);
        await t.pumpAndSettle();
        expect(w.queryState, AgentQueryState.error);
        expect(w.result, isNull);
        expect(find.text('重试'), findsOneWidget);
        await nowTap(t, find.byKey(const Key('agent-sheet-retry')));
        expect(requests, hasLength(2));
        for (final r in requests) {
          expect(r.method, 'GET');
          expect(r.url.path, '/v1/me/agent-tasks/$taskDestinationID');
        }
        expect(w.task?.id, taskDestinationID);
        expect(w.requestFailure, isNull);
        expect(w.requestError, isNull);
        expect(w.queryState, AgentQueryState.empty);
        expect(find.text('重试'), findsNothing);
        expect(t.takeException(), isNull);
        await f.unmount(t);
        source.dispose();
      },
    );
  }
  testWidgets(
    'GET recovery actual Now normal owned history renders original result and never POSTs',
    (t) async {
      final f = NowFixture();
      f.auth.token = 'Bearer synthetic-owner';
      addTearDown(f.dispose);
      final requests = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => f.city.selectedCity?.id,
        authorizationHeader: () => f.auth.authorizationHeader,
        apiBaseUrl: 'https://get-recovery-fixture.test',
        client: MockClient((r) async {
          if (r.method == 'GET' && r.url.path == '/v1/me/agent-tasks') {
            return http.Response('{"data":[]}', 200);
          }
          requests.add(r);
          return taskReply(taskDestinationData());
        }),
      );
      await _mountPublicRemote(t, f, source);
      final w = f.workspace(t);
      await w.reopen(
        const AgentTask(
          id: taskDestinationID,
          query: '原合成历史查询',
          status: 'COMPLETED',
          cityID: 'alpha',
        ),
        [],
        [],
      );
      await t.pumpAndSettle();
      expect(requests.single.method, 'GET');
      expect(w.task?.id, taskDestinationID);
      expect(w.queryState, AgentQueryState.empty);
      expect(w.requestFailure, isNull);
      expect(w.requestError, isNull);
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
    },
  );

  for (final status in [401, 403]) {
    for (final retired in ['turn', 'scope-aba', 'account-aba']) {
      testWidgets('实际Remote迟到HTTP$status经$retired退休不污染新查询或恢复旧权限错误', (t) async {
        final f = NowFixture();
        addTearDown(f.dispose);
        final held = Completer<http.Response>();
        final queries = <String>[];
        final client = MockClient((request) async {
          final query = jsonDecode(request.body)['query'] as String;
          queries.add(query);
          if (query == '迟到的合成查询') return held.future;
          return _publicReply(
            _publicQueryWire(
              status: 'ready',
              kind: 'place',
              message: '新查询的合成公开地点。',
            ),
          );
        });
        final source = RemoteAgentTaskSource(
          cityID: () => f.city.selectedCity?.id,
          authorizationHeader: () => f.auth.authorizationHeader,
          client: client,
          apiBaseUrl: 'http://permission-retry-fixture.test',
        );
        await _mountPublicRemote(t, f, source);
        await t.enterText(nowField(), '迟到的合成查询');
        await t.testTextInput.receiveAction(TextInputAction.send);
        await t.pump(const Duration(milliseconds: 50));
        expect(f.workspace(t).queryState, AgentQueryState.loading);
        if (retired == 'scope-aba') {
          f.city.selectCity('beta');
          f.city.selectCity('alpha');
          await t.pumpAndSettle();
        } else if (retired == 'account-aba') {
          f.auth.changeIdentity(
            'Bearer synthetic-temporary',
            nextOwner: 'temporary',
          );
          f.auth.changeIdentity(null);
          await t.pumpAndSettle();
        }
        await nowSend(t, '找地点');
        final w = f.workspace(t), task = w.task, result = w.result;
        expect(w.queryState, AgentQueryState.success);
        held.complete(
          http.Response(
            jsonEncode({
              'error': {'code': status == 401 ? 'unauthorized' : 'forbidden'},
            }),
            status,
          ),
        );
        await t.pumpAndSettle();
        expect(queries, ['迟到的合成查询', '找地点']);
        expect(w.task, same(task));
        expect(w.result, same(result));
        expect(w.queryState, AgentQueryState.success);
        expect(w.requestFailure, isNull);
        expect(w.requestError, isNull);
        expect(find.text('重试'), findsNothing);
        expect(find.text('请先通过个人资料重新登录，再发起查询。'), findsNothing);
        expect(find.text('请先确认当前身份和访问权限；恢复权限后再发起查询。'), findsNothing);
        expect(t.takeException(), isNull);
        await f.unmount(t);
        source.dispose();
        client.close();
      });
    }
  }
  testWidgets('实际Remote未知结果machinecode独立于服务错误文字要求核对且不重发', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    var posts = 0;
    final client = MockClient((request) async {
      posts++;
      return http.Response('{"error":{"code":"online_result_unknown"}}', 503);
    });
    final source = RemoteAgentTaskSource(
      cityID: () => f.city.selectedCity?.id,
      authorizationHeader: () => f.auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'http://permission-retry-fixture.test',
    );
    await _mountPublicRemote(t, f, source);
    await nowSend(t, '找地点');
    final w = f.workspace(t);
    expect(w.requestFailure?.code, 'online_result_unknown');
    expect(w.requestError, isNot(contains('提交结果尚未确认')));
    expect(find.text('重试'), findsNothing);
    final recovery = find.text('请先从最近对话核对提交结果；不会重复发送原请求。');
    await t.ensureVisible(recovery);
    await t.pumpAndSettle();
    expect(recovery.hitTestable(), findsOneWidget);
    await w.retry();
    expect(posts, 1);
    expect(w.queryState, AgentQueryState.error);
    expect(t.takeException(), isNull);
    await f.unmount(t);
    source.dispose();
    client.close();
  });
  for (final status in [401, 403]) {
    for (final mode in AgentContentMode.values) {
      for (final extent in [
        AgentSheetExtent.peek,
        AgentSheetExtent.medium,
        AgentSheetExtent.expanded,
      ]) {
        testWidgets(
          '实际Remote HTTP$status在${mode.name}/${extent.name}明确前置恢复且无同请求重试',
          (t) async {
            final f = NowFixture();
            addTearDown(f.dispose);
            var posts = 0;
            final client = MockClient((request) async {
              expect(request.method, 'POST');
              expect(jsonDecode(request.body)['query'], '找地点');
              posts++;
              return http.Response(
                jsonEncode({
                  'error': {
                    'code': status == 401 ? 'unauthorized' : 'forbidden',
                    'message': 'Synthetic HTTP fixture',
                  },
                }),
                status,
              );
            });
            final source = RemoteAgentTaskSource(
              cityID: () => f.city.selectedCity?.id,
              authorizationHeader: () => f.auth.authorizationHeader,
              client: client,
              apiBaseUrl: 'http://permission-retry-fixture.test',
            );
            await _mountPublicRemote(t, f, source);
            await nowSend(t, '找地点');
            final w = f.workspace(t), task = w.task;
            expect(w.queryState, AgentQueryState.error);
            expect(w.requestFailure?.statusCode, status);
            expect(posts, 1);
            w.showContent(mode);
            w.setSheetExtent(extent);
            await t.pumpAndSettle();
            debugPrintSynchronously((
              jsonEncode({
                'permissionRetry': 'real-mounted-Remote-Now',
                'status': status,
                'mode': mode.name,
                'extent': extent.name,
                'retryMounted': find.text('重试').evaluate().isNotEmpty,
                'posts': posts,
              })).toString());
            expect(find.text('重试'), findsNothing);
            final recovery = find.text(
              status == 401
                  ? '请先通过个人资料重新登录，再发起查询。'
                  : '请先确认当前身份和访问权限；恢复权限后再发起查询。',
            );
            await t.ensureVisible(recovery);
            await t.pumpAndSettle();
            expect(recovery.hitTestable(), findsOneWidget);
            expect(find.textContaining('0 个'), findsNothing);
            expect(find.text('未找到符合条件的公开内容'), findsNothing);
            await w.retry();
            await t.pumpAndSettle();
            expect(posts, 1);
            expect(w.task, same(task));
            expect(w.result, isNull);
            expect(w.contentMode, mode);
            expect(w.sheetExtent, extent);
            expect(t.takeException(), isNull);
            await f.unmount(t);
            source.dispose();
            client.close();
          },
        );
      }
    }
  }
  for (final failure in ['503', 'network']) {
    testWidgets('实际Remote $failure错误保留唯一实点重试并成功恢复原读请求', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      final requests = <Map<String, dynamic>>[];
      final client = MockClient((request) async {
        requests.add(jsonDecode(request.body) as Map<String, dynamic>);
        if (requests.length == 1) {
          if (failure == 'network') {
            throw http.ClientException('Synthetic connection interruption');
          }
          return http.Response('{"error":{"code":"service_unavailable"}}', 503);
        }
        return _publicReply(
          _publicQueryWire(status: 'empty', message: '真实完成的合成空地点响应。'),
        );
      });
      final source = RemoteAgentTaskSource(
        cityID: () => f.city.selectedCity?.id,
        authorizationHeader: () => f.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'http://permission-retry-fixture.test',
      );
      await _mountPublicRemote(t, f, source);
      await nowSend(t, '找地点');
      final w = f.workspace(t);
      expect(w.queryState, AgentQueryState.error);
      expect(find.text('重试'), findsOneWidget);
      await nowTap(t, find.byKey(const Key('agent-sheet-retry')));
      expect(requests.length, 2);
      expect(requests[1], requests[0]);
      expect(w.requestError, isNull);
      expect(w.queryState, AgentQueryState.empty);
      expect(find.text('重试'), findsNothing);
      expect(find.text('真实完成的合成空地点响应。'), findsOneWidget);
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
      client.close();
    });
  }
  for (final capture in _nativeQueryCaptures) {
    testWidgets('原注册HTTP/native096回执${capture['name']}经实际Remote与Now不改实体或能力边界', (
      t,
    ) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final data =
          (jsonDecode(capture['responseBody'] as String)
                  as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final rawSet = data['resultSet'] as Map<String, dynamic>;
      final unsupported = rawSet['status'] == 'unsupported';
      final query =
          jsonDecode(capture['requestBody'] as String)['query'] as String;
      final f = NowFixture();
      f.city.selection = PublicCity(
        id: data['cityId'] as String,
        name: '原生协议夹具范围',
        region: '合成测试',
        contentStatus: 'test',
        map: null,
        source: const PublicSource(
          label: '实际回执夹具',
          maintainer: 'test',
          freshness: 'synthetic',
          updatedAt: null,
        ),
      );
      addTearDown(f.dispose);
      var calls = 0;
      final client = MockClient((request) async {
        calls++;
        expect(request.method, capture['method']);
        expect(request.url.path, capture['path']);
        expect(
          jsonDecode(request.body),
          jsonDecode(capture['requestBody'] as String),
        );
        expect(request.headers.containsKey('Authorization'), false);
        expect(
          request.headers.containsKey('X-Birdtie-Organization-Workspace'),
          false,
        );
        return http.Response.bytes(
          utf8.encode(capture['responseBody'] as String),
          capture['status'] as int,
          headers: {
            'content-type': 'application/json; charset=utf-8',
            'x-request-id': capture['requestID'] as String,
          },
        );
      });
      final source = RemoteAgentTaskSource(
        cityID: () => f.city.selectedCity?.id,
        authorizationHeader: () => f.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'http://captured-native-wire-fixture.test',
      );
      await _mountPublicRemote(t, f, source);
      await nowSend(t, query);
      final w = f.workspace(t), r = w.result!;
      expect(r.resultSet!.status, rawSet['status']);
      expect(r.requestID, data['requestId']);
      expect(r.responseMessage, data['message']);
      expect(r.task, isNull);
      expect(r.taskID, isNull);
      expect(r.actions, isEmpty);
      expect(calls, 1);
      if (unsupported) {
        expect(w.queryState.name, 'unsupported');
        expect(w.task!.status, 'FAILED');
        for (final mode in AgentContentMode.values) {
          w.showContent(mode);
          w.setSheetExtent(AgentSheetExtent.peek);
          await t.pumpAndSettle();
          expect(
            find.text(data['message'] as String).hitTestable(),
            findsOneWidget,
          );
          expect(find.text('未找到符合条件的公开内容'), findsNothing);
          expect(find.text('当前没有可展示的结果。'), findsNothing);
          expect(find.text('当前没有可见结果'), findsNothing);
          expect(find.textContaining('0 个'), findsNothing);
          expect(find.text('重试'), findsNothing);
          expect(calls, 1);
        }
        expect(r.projectionItems, isEmpty);
        expect(r.entities, isEmpty);
        expect(r.mapEffects!.pinEntityIDs, isEmpty);
      } else {
        expect(w.queryState, AgentQueryState.success);
        expect(w.task!.status, 'COMPLETED');
        final originalItems = rawSet['items'] as List<dynamic>;
        expect(r.projectionItems!.length, originalItems.length);
        for (var index = 0; index < originalItems.length; index++) {
          final raw = originalItems[index] as Map<String, dynamic>;
          final ref = raw['entityRef'] as Map<String, dynamic>;
          final parsed = r.projectionItems![index];
          expect(parsed.entity.type, ref['type']);
          expect(parsed.entity.id, ref['id']);
          expect(parsed.title, raw['title']);
          expect(
            parsed.detail!.type,
            (raw['detailRef'] as Map<String, dynamic>)['type'],
          );
          expect(
            parsed.detail!.id,
            (raw['detailRef'] as Map<String, dynamic>)['id'],
          );
        }
        expect(
          r.mapEffects!.pinEntityIDs,
          (data['mapEffects'] as Map<String, dynamic>)['pinEntityIds'],
        );
        final kind = capture['name'] == 'PUBLIC_PLACE-000'
            ? 'place'
            : 'organization';
        expect(
          r.projectionItems!.every((item) => item.entity.type == kind),
          true,
        );
        expect(
          find.text(
            '${r.projectionItems!.length} 个${kind == 'place' ? '地点' : '组织'}',
          ),
          findsOneWidget,
        );
        expect(find.text('未找到符合条件的公开内容'), findsNothing);
        expect(find.text('当前没有可展示的结果。'), findsNothing);
        expect(find.textContaining('0 个'), findsNothing);
      }
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
      client.close();
    });
  }
  testWidgets('真实完成的公开空查询保留empty状态与真实解释，不伪造unsupported', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    final client = MockClient(
      (r) async => _publicReply(
        _publicQueryWire(status: 'empty', message: '没有符合条件的已发布地点。'),
      ),
    );
    final source = RemoteAgentTaskSource(
      cityID: () => f.city.selectedCity?.id,
      authorizationHeader: () => f.auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'http://public-wire-fixture.test',
    );
    await _mountPublicRemote(t, f, source);
    await nowSend(t, '找地点');
    final w = f.workspace(t);
    expect(w.queryState, AgentQueryState.empty);
    expect(w.task?.status, 'COMPLETED');
    expect(find.text('未找到符合条件的公开内容'), findsOneWidget);
    expect(find.text('没有符合条件的已发布地点。'), findsOneWidget);
    expect(find.text('重试'), findsNothing);
    expect(t.takeException(), isNull);
    await f.unmount(t);
    source.dispose();
    client.close();
  });
  for (final retired in ['turn', 'scope-aba', 'account-aba']) {
    testWidgets('真实Remote迟到unsupported经$retired退休不能覆盖当前公开结果或复活旧任务', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      final held = Completer<http.Response>();
      final queries = <String>[];
      final client = MockClient((r) async {
        if (r.method == 'GET') return http.Response('{"data":[]}', 200);
        final query = jsonDecode(r.body)['query'] as String;
        queries.add(query);
        return query == 'weekend'
            ? held.future
            : _publicReply(
                _publicQueryWire(
                  status: 'ready',
                  kind: 'place',
                  message: '当前地点结果。',
                ),
              );
      });
      final source = RemoteAgentTaskSource(
        cityID: () => f.city.selectedCity?.id,
        authorizationHeader: () => f.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'http://public-wire-fixture.test',
      );
      await _mountPublicRemote(t, f, source);
      // Do not settle an intentionally pending animated search through the
      // real Remote source's 12-second timeout.
      await t.enterText(nowField(), 'weekend');
      await t.testTextInput.receiveAction(TextInputAction.send);
      await t.pump(const Duration(milliseconds: 50));
      final w = f.workspace(t), epoch = w.taskEpoch;
      expect(w.queryState, AgentQueryState.loading);
      if (retired == 'scope-aba') {
        f.city.selectCity('beta');
        f.city.selectCity('alpha');
        await t.pumpAndSettle();
      } else if (retired == 'account-aba') {
        f.auth.changeIdentity(
          'Bearer synthetic-temporary',
          nextOwner: 'temporary',
        );
        f.auth.changeIdentity(null);
        await t.pumpAndSettle();
      }
      await nowSend(t, '找地点');
      final task = w.task, result = w.result;
      expect(w.queryState, AgentQueryState.success);
      expect(w.taskEpoch, greaterThan(epoch));
      held.complete(_publicReply(_publicQueryWire()));
      await t.pumpAndSettle();
      expect(identical(task, w.task), true);
      expect(identical(result, w.result), true);
      expect(w.queryState, AgentQueryState.success);
      expect(w.requestError, isNull);
      expect(
        w.conversation.where((m) => m.text == _unsupportedMessage),
        isEmpty,
      );
      expect(find.text(_unsupportedMessage), findsNothing);
      expect(queries, ['weekend', '找地点']);
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
      client.close();
    });
  }
  test('真实Remote保留unsupported契约且本地任务不假完成，真实空查询保持empty', () async {
    var wire = _publicQueryWire(), calls = 0;
    final client = MockClient((r) async {
      calls++;
      expect(r.headers.containsKey('Authorization'), false);
      expect(jsonDecode(r.body)['taskId'], isNull);
      return _publicReply(wire);
    });
    final source = RemoteAgentTaskSource(
      cityID: () => 'alpha',
      authorizationHeader: () => null,
      client: client,
      apiBaseUrl: 'http://public-wire-fixture.test',
    );
    final w = AgentWorkspaceController(source: source);
    await w.submit('weekend', [], [], cityID: 'alpha');
    expect(w.result?.resultSet?.status, 'unsupported');
    expect(w.result?.task, isNull);
    expect(w.result?.taskID, isNull);
    expect(w.queryState.name, 'unsupported');
    expect(w.task?.status, 'FAILED');
    expect(w.task?.cityID, 'alpha');
    expect(w.result?.responseMessage, _unsupportedMessage);
    expect(w.requestError, isNull);
    expect(w.conversation.last.text, _unsupportedMessage);
    expect(calls, 1);
    wire = _publicQueryWire(status: 'empty', message: '没有符合条件的已发布地点。');
    await w.submit('找地点', [], [], cityID: 'alpha');
    expect(w.queryState, AgentQueryState.empty);
    expect(w.task?.status, 'COMPLETED');
    expect(calls, 2);
    w.dispose();
    source.dispose();
    client.close();
  });
  for (final mode in AgentContentMode.values) {
    for (final extent in [
      AgentSheetExtent.peek,
      AgentSheetExtent.medium,
      AgentSheetExtent.expanded,
    ]) {
      testWidgets(
        'unsupported真实Now ${mode.name}/${extent.name}只说明能力边界无假空和HTTP重试',
        (t) async {
          t.view.physicalSize = const Size(390, 844);
          t.view.devicePixelRatio = 1;
          addTearDown(t.view.resetPhysicalSize);
          addTearDown(t.view.resetDevicePixelRatio);
          final f = NowFixture();
          addTearDown(f.dispose);
          var calls = 0;
          final client = MockClient((r) async {
            calls++;
            expect(jsonDecode(r.body)['query'], 'weekend');
            return _publicReply(_publicQueryWire());
          });
          final source = RemoteAgentTaskSource(
            cityID: () => f.city.selectedCity?.id,
            authorizationHeader: () => f.auth.authorizationHeader,
            client: client,
            apiBaseUrl: 'http://public-wire-fixture.test',
          );
          await _mountPublicRemote(t, f, source);
          await nowSend(t, 'weekend');
          final w = f.workspace(t);
          w.showContent(mode);
          w.setSheetExtent(extent);
          await t.pumpAndSettle();
          expect(find.text('未找到符合条件的公开内容'), findsNothing);
          expect(find.text('当前没有可展示的结果。'), findsNothing);
          expect(find.text('当前没有可见结果'), findsNothing);
          expect(find.textContaining('0 个'), findsNothing);
          expect(find.text('重试'), findsNothing);
          expect(find.text('选择城市继续'), findsNothing);
          expect(find.text(_unsupportedMessage).hitTestable(), findsOneWidget);
          expect(w.result?.responseMessage, _unsupportedMessage);
          expect(calls, 1);
          if (extent == AgentSheetExtent.peek) {
            await nowTap(
              t,
              find.text(mode == AgentContentMode.results ? '展开说明' : '展开对话'),
            );
            expect(w.sheetExtent, AgentSheetExtent.medium);
            expect(
              find.text(_unsupportedMessage).hitTestable(),
              findsOneWidget,
            );
            expect(calls, 1);
          }
          expect(t.takeException(), isNull);
          await f.unmount(t);
          source.dispose();
          client.close();
        },
      );
    }
  }
  for (final kind in ['place', 'organization']) {
    testWidgets('真实Remote公开$kind保持同Ref类型与中文数量，不变成unsupported或活动空结果', (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final f = NowFixture();
      addTearDown(f.dispose);
      var calls = 0;
      final client = MockClient((r) async {
        calls++;
        return _publicReply(
          _publicQueryWire(
            status: 'ready',
            kind: kind,
            message: kind == 'place' ? '找到 1 个已发布地点。' : '找到 1 个公开组织。',
          ),
        );
      });
      final source = RemoteAgentTaskSource(
        cityID: () => f.city.selectedCity?.id,
        authorizationHeader: () => f.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'http://public-wire-fixture.test',
      );
      await _mountPublicRemote(t, f, source);
      await nowSend(t, kind == 'place' ? '找地点' : '找组织');
      final w = f.workspace(t), r = w.result!;
      expect(w.queryState, AgentQueryState.success);
      expect(r.projectionItems!.single.entity.type, kind);
      expect(r.projectionItems!.single.entity.id, 'fixture-$kind');
      expect(r.resultSet!.entities.single, r.projectionItems!.single.entity);
      expect(r.entities, isEmpty); // No public coordinates were supplied.
      expect(r.mapEffects!.pinEntityIDs, isEmpty);
      expect(find.text(kind == 'place' ? '1 个地点' : '1 个组织'), findsOneWidget);
      expect(find.text('原实体中文标题'), findsOneWidget);
      expect(find.textContaining('0 个'), findsNothing);
      expect(find.text('未找到符合条件的公开内容'), findsNothing);
      expect(find.text('当前没有可展示的结果。'), findsNothing);
      expect(calls, 1);
      expect(t.takeException(), isNull);
      await f.unmount(t);
      source.dispose();
      client.close();
    });
  }
  test('缺API配置的实际Native四入口均拒绝相对URL读取或写入', () async {
    var calls = 0;
    final client = MockClient((r) async {
      calls++;
      return http.Response('{"data":[]}', 200);
    });
    final source = RemoteAgentTaskSource(
      cityID: () => 'explicit-city',
      authorizationHeader: () => 'Bearer synthetic-native-owner',
      client: client,
      apiBaseUrl: '',
    );
    const id = '11111111-1111-4111-8111-111111111111',
        owner = '22222222-2222-4222-8222-222222222222';
    await expectLater(
      source.resolve('公开地点', [], []),
      throwsA(isA<AgentRequestFailure>()),
    );
    await expectLater(source.loadRecent(), throwsA(isA<AgentRequestFailure>()));
    await expectLater(
      source.restore(
        const AgentTask(id: id, query: '历史', status: 'COMPLETED'),
        [],
        [],
      ),
      throwsA(isA<AgentRequestFailure>()),
    );
    await expectLater(
      source.readByID(id, ownerID: owner),
      throwsA(isA<AgentRequestFailure>()),
    );
    expect(calls, 0);
    source.dispose();
    client.close();
  });
  for (final failed in [true, false]) {
    test('旧任务恢复${failed ? '失败' : '成功'}迟到不能污染新任务结果或错误', () async {
      final city = NowFixtureCity(),
          source = NowFixtureSource(NowFixtureCity());
      final held = Completer<AgentResult>();
      source.pending = held;
      final w = AgentWorkspaceController(source: source);
      final old = w.reopen(
        const AgentTask(id: 'old-server-task', query: 'A', status: 'COMPLETED'),
        [],
        [],
      );
      source.pending = null;
      await w.submit('C', [], []);
      if (failed) {
        held.completeError(StateError('old restore failure'));
      } else {
        held.complete(source.response('A'));
      }
      await old;
      final observedError = w.requestError, observedTask = w.task?.query;
      final observedResult = w.result?.responseMessage;
      w.dispose();
      city.dispose();
      source.city.dispose();
      expect(observedError, isNull);
      expect(observedTask, 'C');
      expect(observedResult, '合成权威响应：C');
    });
  }
  testWidgets('缺城市不是零结果，也不同时显示暂无与重试', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, '找地点');
    expect(find.textContaining('0 个活动'), findsNothing);
    expect(find.text('暂无可显示的结果。'), findsNothing);
    expect(find.text('重试'), findsNothing);
    expect(find.text('选择城市继续'), findsOneWidget);
    await f.unmount(t);
  });
  testWidgets('真实网络错误只有一个恢复动作，无伪空结果', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    f.source.failure = '连接中断，请重试。';
    await f.mount(t);
    await nowSend(t, '找地点');
    expect(find.text('重试'), findsOneWidget);
    expect(find.text('暂无可显示的结果。'), findsNothing);
    expect(find.textContaining('0 个活动'), findsNothing);
    await f.unmount(t);
  });
  testWidgets('缺范围或查询错误时主动打开对话仍可读原输入且不新增假空或第二恢复', (t) async {
    for (final selected in [false, true]) {
      final f = NowFixture(selected: selected);
      f.source.failure = '连接中断，请重试。';
      await f.mount(t);
      await nowSend(t, '保留我的原句');
      await nowTap(t, find.byTooltip('打开对话'));
      expect(
        find.descendant(
          of: find.byType(AgentConversation),
          matching: find.text('保留我的原句'),
        ),
        findsOneWidget,
      );
      expect(find.textContaining('0 个'), findsNothing);
      expect(find.text('暂无可显示的结果。'), findsNothing);
      expect(find.text(selected ? '重试' : '选择城市继续'), findsOneWidget);
      await f.unmount(t);
      f.dispose();
    }
  });
}
