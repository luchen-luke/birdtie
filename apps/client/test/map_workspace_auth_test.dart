import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'entity_action_contract_test.dart' show actionWire;
import 'dart:async';
import 'dart:convert';
import 'now_context_query_api_test.dart'
    show onlineWire, onlineContextID, onlineIntentID, onlineOwnerID;

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:birdtie_client/src/workspace/external_place_history_store.dart';
import 'package:birdtie_client/src/auth/birdtie_login_sheet.dart';
import 'map_layers_api_test.dart' as typed;
import 'active_social_intent_api_test.dart'
    show nowOwner, nowList, nowOptions, nowResponse;
import 'now_context_selection_api_test.dart'
    show selectionOptions, selectionReceipt;
import 'now_scope_recovery_test.dart' show nowOpenAccountTools;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _FixtureAuth extends BirdtieAuthController {
  String? person = 'A';
  String? rejectedOwner;
  String sessionSuffix = '';
  @override
  bool get signedIn => person != null;
  @override
  String? get authorizationHeader =>
      person == null ? null : 'Bearer fixture-$person$sessionSuffix';
  @override
  String? get accountID => person;
  @override
  String? get displayName => person;
  @override
  String? get rejectedSessionOwnerID => rejectedOwner;

  void use(String? next) {
    person = next;
    rejectedOwner = null;
    notifyListeners();
  }

  void reject() {
    rejectedOwner = person;
    person = null;
    notifyListeners();
  }
}

class _SameTokenAuth extends _FixtureAuth {
  @override
  String? get authorizationHeader => 'Bearer fixed-native-session';
}

class _FixtureCity extends PublicCityController {
  static const city = PublicCity(
    id: 'fixture-city',
    name: '测试城市',
    region: '合成测试',
    contentStatus: 'building',
    source: PublicSource(
      label: '合成测试',
      maintainer: '测试',
      freshness: 'test',
      updatedAt: null,
    ),
    map: null,
  );
  @override
  PublicCity? get selectedCity => city;
  @override
  List<PublicCity> get cities => const [city];
  @override
  List<PublicPlace> get places => const [
    PublicPlace(
      id: 'public-fixture-place',
      name: '公开测试地点',
      categoryCode: 'other',
      summary: '公开目录',
      source: PublicSource(
        label: '公共目录',
        maintainer: '测试',
        freshness: 'test',
        updatedAt: null,
      ),
      location: PublicPlaceLocation(
        coordinateSystem: 'wgs84',
        precision: 'point',
        latitude: 57.15,
        longitude: -2.1,
      ),
    ),
  ];
}

class _PendingSource extends AgentTaskSource {
  final pending = Completer<AgentResult>();
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => pending.future;
}

const _personResult = AgentResult(
  entities: [],
  activities: [],
  places: [],
  note: '公开候选，发送前确认',
  people: [
    AgentPerson(
      accountID: '11111111-1111-4111-8111-111111111111',
      displayName: '合成候选',
      topic: '羽毛球',
      areaLabel: '大致区域',
    ),
  ],
);

Future<(MapCanvas, PrivateMomentController)> _show(
  WidgetTester tester,
  _FixtureAuth auth,
  _FixtureCity city, {
  AgentTaskSource? agent,
  ConnectionSource? connections,
  http.Client? seedClient,
  String? apiBase,
}) async {
  tester.view.physicalSize = const Size(430, 932);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
  );
  await tester.pumpWidget(
    MaterialApp(
      home: MapWorkspace(
        city: city,
        auth: auth,
        moments: moments,
        agentTaskSource: agent,
        connectionSource: connections,
        seedClient: seedClient,
        seedApiBaseUrl: apiBase,
      ),
    ),
  );
  await tester.pumpAndSettle();
  return (tester.widget<MapCanvas>(find.byType(MapCanvas)), moments);
}

void _showPerson(MapCanvas map) {
  map.workspace.task = const AgentTask(
    id: 'fixture-task',
    query: '找朋友',
    status: 'COMPLETED',
    principalID: 'A',
  );
  map.workspace.result = _personResult;
  map.workspace.setSheetExtent(AgentSheetExtent.medium);
}

Future<void> _close(
  WidgetTester tester,
  _FixtureAuth auth,
  _FixtureCity city,
  PrivateMomentController moments,
) async {
  await tester.pump(const Duration(milliseconds: 400));
  await tester.pumpWidget(const SizedBox());
  auth.dispose();
  city.dispose();
  moments.dispose();
}

Future<void> _openViewChoice(WidgetTester tester) async {
  await nowOpenAccountTools(tester);
  final entry = find.byTooltip('选择查看范围');
  await tester.scrollUntilVisible(
    entry,
    120,
    scrollable: find
        .descendant(
          of: find.byKey(const Key('now-tools-menu')),
          matching: find.byType(Scrollable),
        )
        .first,
  );
  await tester.ensureVisible(entry);
  await tester.pumpAndSettle();
  expect(entry.hitTestable(), findsOneWidget);
  await tester.tap(entry);
  await tester.pumpAndSettle();
}

Map<String, dynamic> _nativeOnlineSelection(String label) {
  final raw = selectionOptions();
  raw['owner'] = {'type': 'PERSON', 'id': onlineOwnerID};
  final online = Map<String, dynamic>.from((raw['items'] as List)[1] as Map);
  online['contextId'] = onlineContextID;
  online['label'] = label;
  raw['items'] = [online];
  return raw;
}

Map<String, dynamic> _publicMomentWire(DateTime now) => {
  'schemaVersion': 'public-moment-v2',
  'id': typed.mapTestID,
  'title': '原始公开动态',
  'body': '仍公开的完整正文',
  'placeId': typed.mapTestPlace,
  'placeName': '公开场地',
  'cityId': 'fixture-city',
  'revision': 2,
  'publishedAt': now.subtract(const Duration(days: 1)).toIso8601String(),
  'authorId': null,
  'authorPublicProfile': null,
};

Map<String, dynamic> _publicHistoryWire(DateTime now) => {
  'schemaVersion': 'place-social-history-page-v1',
  'placeId': typed.mapTestPlace,
  'cityId': 'fixture-city',
  'authorMode': 'PUBLIC_PROFILE',
  'windowDays': 30,
  'windowStart': now.subtract(const Duration(days: 30)).toIso8601String(),
  'windowEnd': now.toIso8601String(),
  'observedAt': now.toIso8601String(),
  'validUntil': now.add(const Duration(minutes: 2)).toIso8601String(),
  'sourceVersion': List.filled(64, 'a').join(),
  'count': 1,
  'items': [
    {
      'id': typed.mapTestID,
      'title': '原始公开动态',
      'excerpt': '仍公开的合成节选',
      'revision': 2,
      'publishedAt': now.subtract(const Duration(days: 1)).toIso8601String(),
      'authorId': null,
      'authorPublicProfile': null,
    },
  ],
  'nextCursor': null,
};

const _recoveryTaskID = '33333333-3333-4333-8333-333333333333';
const _recoveryOwner = '22222222-2222-4222-8222-222222222222';
const _privateQueryBeforeRejection = '原账号未核验的私密需求';
const _privateAnswerBeforeRejection = '原账号未核验的私密历史';
const _canonicalRecoveryQuery = '服务端重新读取的合成需求';
const _canonicalRecoveryAnswer = '服务端重新核验的合成历史';

void _showPrivateNativeTask(MapCanvas map, {String id = _recoveryTaskID}) {
  final task = AgentTask(
    id: id,
    query: _privateQueryBeforeRejection,
    status: 'COMPLETED',
    cityID: 'fixture-city',
    contextType: 'CITY',
    contextID: '44444444-4444-4444-8444-444444444444',
    principalID: _recoveryOwner,
    actingUserID: _recoveryOwner,
    intent: 'PRIVATE_TEST_INTENT',
    filters: const {'privateSyntheticFilter': 'must-not-survive'},
    messages: const [
      AgentMessage(role: 'user', text: _privateQueryBeforeRejection),
      AgentMessage(role: 'assistant', text: _privateAnswerBeforeRejection),
    ],
  );
  map.workspace.task = task;
  map.workspace.recent.add(task);
  map.workspace.conversation.addAll(task.messages);
  map.workspace.result = const AgentResult(
    entities: [],
    activities: [],
    places: [],
    note: _privateAnswerBeforeRejection,
  );
  map.workspace.state = AgentViewState.results;
  map.workspace.showContent(AgentContentMode.conversation);
  map.workspace.setSheetExtent(AgentSheetExtent.expanded);
}

http.Response _canonicalRecoveryResponse() => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': {
        'activities': [],
        'people': [],
        'groups': [],
        'places': [],
        'taskId': _recoveryTaskID,
        'message': _canonicalRecoveryAnswer,
        'task': {
          'id': _recoveryTaskID,
          'query': _canonicalRecoveryQuery,
          'status': 'COMPLETED',
          'principalType': 'PERSON',
          'principalId': _recoveryOwner,
          'actingUserId': _recoveryOwner,
          'cityId': 'fixture-city',
          'contextType': 'CITY',
          'contextId': '44444444-4444-4444-8444-444444444444',
          'conversation': [
            {'role': 'user', 'text': _canonicalRecoveryQuery},
            {'role': 'assistant', 'text': _canonicalRecoveryAnswer},
          ],
        },
      },
    }),
  ),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

MockClient _nativeRecoveryClient(
  List<http.Request> requests, {
  Future<http.Response> Function()? readTask,
}) => MockClient((request) async {
  requests.add(request);
  if (request.method == 'GET' &&
      request.url.path == '/v1/me/agent-tasks/$_recoveryTaskID') {
    return readTask == null ? _canonicalRecoveryResponse() : readTask();
  }
  if (request.method == 'GET' && request.url.path == '/v1/me/agent-tasks') {
    return http.Response(
      '{"data":[]}',
      200,
      headers: {'content-type': 'application/json'},
    );
  }
  return http.Response(
    '{}',
    404,
    headers: {'content-type': 'application/json'},
  );
});

class _ResolveCountingSource extends AgentTaskSource {
  int resolveCalls = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    resolveCalls++;
    return const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '不应自动执行',
    );
  }
}

Future<void> _closeRecoveryLoginSheet(WidgetTester tester) async {
  final sheet = find.byType(BirdtieLoginSheet);
  if (sheet.evaluate().isNotEmpty) {
    await tester.tap(
      find.descendant(
        of: sheet,
        matching: find.widgetWithText(TextButton, '返回'),
      ),
    );
    await tester.pumpAndSettle();
  }
}

void main() {
  testWidgets(
    'shared401 clears private history then same-owner login restores canonical native task by GET only',
    (tester) async {
      final auth = _FixtureAuth()..person = _recoveryOwner;
      final city = _FixtureCity(), requests = <http.Request>[];
      final pendingRead = Completer<http.Response>();
      final client = _nativeRecoveryClient(
        requests,
        readTask: () => pendingRead.future,
      );
      final source = RemoteAgentTaskSource(
        cityID: () => city.selectedCity!.id,
        authorizationHeader: () => auth.authorizationHeader,
        apiBaseUrl: 'https://native-recovery.test',
        client: client,
        externalPlaceHistoryStore: MemoryExternalPlaceHistoryStore(),
      );
      final (map, moments) = await _show(
        tester,
        auth,
        city,
        agent: source,
        seedClient: client,
        apiBase: 'https://native-recovery.test',
      );
      final element = find.byType(MapCanvas).evaluate().single;
      _showPrivateNativeTask(map);
      await tester.pumpAndSettle();
      expect(find.textContaining(_privateAnswerBeforeRejection), findsWidgets);
      auth.reject();
      expect(map.workspace.task, isNull);
      expect(map.workspace.result, isNull);
      expect(map.workspace.conversation, isEmpty);
      expect(map.workspace.recent, isEmpty);
      await tester.pumpAndSettle();
      expect(find.textContaining(_privateAnswerBeforeRejection), findsNothing);
      expect(
        requests.where(
          (r) => r.url.path == '/v1/me/agent-tasks/$_recoveryTaskID',
        ),
        isEmpty,
      );
      auth.sessionSuffix = '-fresh';
      auth.use(_recoveryOwner);
      for (var n = 0; n < 4; n++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      final reads = requests
          .where((r) => r.url.path == '/v1/me/agent-tasks/$_recoveryTaskID')
          .toList();
      expect(reads, hasLength(1));
      expect(reads.single.method, 'GET');
      expect(
        reads.single.headers['Authorization'],
        'Bearer fixture-$_recoveryOwner-fresh',
      );
      expect(reads.single.body, isEmpty);
      expect(map.workspace.task!.query, isEmpty);
      expect(map.workspace.task!.messages, isEmpty);
      expect(map.workspace.task!.filters, isEmpty);
      expect(map.workspace.task!.intent, isNull);
      expect(map.workspace.result, isNull);
      expect(map.workspace.conversation.every((m) => m.text.isEmpty), isTrue);
      expect(find.textContaining(_privateAnswerBeforeRejection), findsNothing);
      pendingRead.complete(_canonicalRecoveryResponse());
      await tester.pumpAndSettle();
      await _closeRecoveryLoginSheet(tester);
      expect(map.workspace.task!.id, _recoveryTaskID);
      expect(map.workspace.task!.query, _canonicalRecoveryQuery);
      expect(map.workspace.conversation.map((m) => m.text), [
        _canonicalRecoveryQuery,
        _canonicalRecoveryAnswer,
      ]);
      expect(map.workspace.result!.responseMessage, _canonicalRecoveryAnswer);
      expect(find.textContaining(_privateAnswerBeforeRejection), findsNothing);
      expect(find.textContaining(_canonicalRecoveryAnswer), findsWidgets);
      expect(
        identical(element, find.byType(MapCanvas).evaluate().single),
        isTrue,
      );
      expect(requests.where((r) => r.method != 'GET'), isEmpty);
      expect(tester.takeException(), isNull);
      await _close(tester, auth, city, moments);
      client.close();
    },
  );
  for (final transition in [
    'ordinaryLogout',
    'logoutAfterRejection',
    'differentOwnerAfterRejection',
    'nonNativeTask',
  ]) {
    testWidgets('native recovery selector does not restore after $transition', (
      tester,
    ) async {
      final auth = _FixtureAuth()..person = _recoveryOwner;
      final city = _FixtureCity(), requests = <http.Request>[];
      final client = _nativeRecoveryClient(requests);
      final source = RemoteAgentTaskSource(
        cityID: () => city.selectedCity!.id,
        authorizationHeader: () => auth.authorizationHeader,
        apiBaseUrl: 'https://native-recovery.test',
        client: client,
        externalPlaceHistoryStore: MemoryExternalPlaceHistoryStore(),
      );
      final (map, moments) = await _show(
        tester,
        auth,
        city,
        agent: source,
        seedClient: client,
        apiBase: 'https://native-recovery.test',
      );
      _showPrivateNativeTask(
        map,
        id: transition == 'nonNativeTask'
            ? 'local-public-task'
            : _recoveryTaskID,
      );
      await tester.pumpAndSettle();
      if (transition == 'ordinaryLogout') {
        auth.use(null);
      } else {
        auth.reject();
        await tester.pumpAndSettle();
        if (transition == 'logoutAfterRejection') {
          auth.use(null);
        }
        if (transition == 'differentOwnerAfterRejection') {
          auth.use('55555555-5555-4555-8555-555555555555');
        }
      }
      await tester.pumpAndSettle();
      expect(map.workspace.task, isNull);
      expect(map.workspace.conversation, isEmpty);
      expect(find.textContaining(_privateAnswerBeforeRejection), findsNothing);
      auth.sessionSuffix = '-fresh';
      auth.use(_recoveryOwner);
      await tester.pumpAndSettle();
      await _closeRecoveryLoginSheet(tester);
      expect(map.workspace.task, isNull);
      expect(map.workspace.result, isNull);
      expect(map.workspace.conversation, isEmpty);
      expect(
        requests.where(
          (r) => r.url.path == '/v1/me/agent-tasks/$_recoveryTaskID',
        ),
        isEmpty,
      );
      expect(requests.where((r) => r.method != 'GET'), isEmpty);
      expect(tester.takeException(), isNull);
      await _close(tester, auth, city, moments);
      client.close();
    });
  }
  testWidgets(
    'same-owner login never calls generic AgentTaskSource restore which falls back to resolve',
    (tester) async {
      final auth = _FixtureAuth()..person = _recoveryOwner;
      final city = _FixtureCity(), requests = <http.Request>[];
      final client = _nativeRecoveryClient(requests);
      final source = _ResolveCountingSource();
      final (map, moments) = await _show(
        tester,
        auth,
        city,
        agent: source,
        seedClient: client,
        apiBase: 'https://native-recovery.test',
      );
      _showPrivateNativeTask(map);
      await tester.pumpAndSettle();
      auth.reject();
      expect(map.workspace.task, isNull);
      expect(map.workspace.conversation, isEmpty);
      await tester.pumpAndSettle();
      auth.use(_recoveryOwner);
      await tester.pumpAndSettle();
      await _closeRecoveryLoginSheet(tester);
      expect(source.resolveCalls, 0);
      expect(map.workspace.task, isNull);
      expect(map.workspace.result, isNull);
      expect(map.workspace.conversation, isEmpty);
      expect(find.textContaining(_privateAnswerBeforeRejection), findsNothing);
      expect(requests.where((r) => r.method != 'GET'), isEmpty);
      expect(tester.takeException(), isNull);
      await _close(tester, auth, city, moments);
      client.close();
    },
  );
  testWidgets('查看目的地保持原Task结果选择地图身份，选择不提交查询或声明', (tester) async {
    final auth = _FixtureAuth()..person = onlineOwnerID;
    final city = _FixtureCity();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    final requests = <http.Request>[];
    final options = selectionOptions();
    options['owner'] = {'type': 'PERSON', 'id': onlineOwnerID};
    final destination =
        Map<String, dynamic>.from((options['items'] as List).first as Map)
          ..['label'] = '合成目的地'
          ..['viewMode'] = 'DESTINATION'
          ..['relation'] = ''
          ..['declared'] = false;
    options['items'] = [destination];
    final client = MockClient((r) async {
      requests.add(r);
      final data = r.url.path.endsWith('/context-selection/options')
          ? options
          : r.url.path.endsWith('/context-selection/resolve')
          ? selectionReceipt(options, destination)
          : null;
      return http.Response(
        jsonEncode({'data': data}),
        data == null ? 404 : 200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          seedClient: client,
          seedApiBaseUrl: 'https://native.test',
        ),
      ),
    );
    await tester.pumpAndSettle();
    final element = find.byType(MapCanvas).evaluate().single;
    final map = tester.widget<MapCanvas>(find.byType(MapCanvas));
    _showPerson(map);
    final task = map.workspace.task, result = map.workspace.result;
    map.workspace.selectEntity('place:public-fixture-place');
    await tester.pumpAndSettle();
    await _openViewChoice(tester);
    await tester.tap(find.text('合成目的地'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('核验并切换查看范围'));
    await tester.pumpAndSettle();
    expect(identical(task, map.workspace.task), true);
    expect(identical(result, map.workspace.result), true);
    expect(map.workspace.selectedEntityId, 'place:public-fixture-place');
    expect(identical(element, find.byType(MapCanvas).evaluate().single), true);
    expect(city.selectedCity!.id, 'fixture-city');
    expect(auth.accountID, onlineOwnerID);
    expect(requests.where((r) => r.method != 'GET').map((r) => r.url.path), [
      '/v1/me/now/context-selection/resolve',
    ]);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    moments.dispose();
    auth.dispose();
    city.dispose();
    client.close();
  });
  testWidgets('同会话账号 A B A 永久退休私人地图和迟到来源', (tester) async {
    final auth = _SameTokenAuth()..person = nowOwner;
    final city = _FixtureCity();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    final late = Completer<http.Response>();
    var ownReads = 0;
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      final own = r.url.path.endsWith('/map-opportunities');
      if (own && ++ownReads > 1) return late.future;
      if (own || r.url.path.endsWith('/map-layers')) {
        final wire = typed.mapTestView(private: own)
          ..['cityId'] = 'fixture-city';
        return http.Response(
          jsonEncode({'data': wire}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      return http.Response('{}', 404);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          seedClient: client,
          seedApiBaseUrl: 'https://native.test',
        ),
      ),
    );
    await tester.pumpAndSettle();
    final canvas = find.byType(MapCanvas).evaluate().single;
    final map = tester.widget<MapCanvas>(find.byType(MapCanvas));
    map.onInitialViewport(typed.mapTestBounds);
    await tester.pumpAndSettle();
    final layers = map.layers!;
    layers.toggle('OPPORTUNITY', true);
    await tester.pumpAndSettle();
    expect(layers.privateView?.items.length, 1);
    final selected = layers.privateView!.items.single.mapID;
    map.workspace.selectEntity(selected);
    final refresh = layers.load('fixture-city', typed.mapTestBounds);
    await tester.pump();
    expect(ownReads, 2);
    auth.use('44444444-4444-4444-8444-444444444444');
    expect(layers.items, isEmpty);
    auth.use(nowOwner);
    late.complete(
      http.Response(
        jsonEncode({
          'data': typed.mapTestView(private: true)..['cityId'] = 'fixture-city',
        }),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    await refresh;
    await tester.pumpAndSettle();
    expect(layers.privateView, isNull);
    expect(layers.publicView?.scope, 'PUBLIC');
    expect(
      layers.publicView?.items.any((item) => item.kind == 'OPPORTUNITY'),
      false,
    );
    expect(layers.visible, isNot(contains('OPPORTUNITY')));
    expect(
      ownReads,
      2,
    ); // No automatic replay or restoration of the own overlay.
    expect(map.workspace.selectedEntityId, isNull);
    expect(identical(canvas, find.byType(MapCanvas).evaluate().single), true);
    expect(requests.where((r) => r.method != 'GET'), isEmpty);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    moments.dispose();
    auth.dispose();
    city.dispose();
    client.close();
  });
  testWidgets('工具中的原生意图卡窄屏大字可达，旧入口重绑不复活或写入', (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final auth = _FixtureAuth()..person = nowOwner;
    final city = _FixtureCity();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    final methods = <String>[];
    final client = MockClient((r) async {
      methods.add(r.method);
      if (r.url.path.startsWith('/v1/me/active-social-intents')) {
        return nowResponse(
          r.url.path.endsWith('/options') ? nowOptions() : nowList(),
        );
      }
      return http.Response('{}', 404);
    });
    Future<void> show(String base) async {
      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(3)),
            child: child!,
          ),
          home: MapWorkspace(
            key: const ValueKey('same'),
            city: city,
            auth: auth,
            moments: moments,
            seedClient: client,
            seedApiBaseUrl: base,
          ),
        ),
      );
      await tester.pumpAndSettle();
    }

    await show('https://old.test');
    final canvas = find.byType(MapCanvas).evaluate().single;
    expect(find.text('当前社交意图'), findsNothing);
    expect(tester.takeException(), isNull);
    await nowOpenAccountTools(tester);
    final entry = find.byTooltip('我的社交意图');
    await tester.scrollUntilVisible(
      entry,
      120,
      scrollable: find
          .descendant(
            of: find.byKey(const Key('now-tools-menu')),
            matching: find.byType(Scrollable),
          )
          .first,
    );
    await tester.pumpAndSettle();
    expect(entry.hitTestable(), findsOneWidget);
    expect(tester.getSize(entry).height, greaterThanOrEqualTo(48));
    await tester.tap(entry);
    await tester.pumpAndSettle();
    expect(find.text('当前社交意图'), findsOneWidget);
    await show('https://new.test');
    await show('https://old.test');
    expect(find.text('请重新打开内容'), findsOneWidget);
    expect(find.text('当前社交意图'), findsNothing);
    expect(methods.where((m) => m != 'GET'), isEmpty);
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(identical(canvas, find.byType(MapCanvas).evaluate().single), true);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    moments.dispose();
    auth.dispose();
    city.dispose();
    client.close();
  });
  testWidgets(
    'typed map entry original Moment ID and same-key endpoint rebind retires old detail',
    (tester) async {
      final auth = _FixtureAuth(), city = _FixtureCity();
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
      );
      final requests = <http.Request>[];
      final late = Completer<http.Response>();
      final observed = DateTime.now().toUtc();
      var momentReads = 0;
      final client = MockClient((r) async {
        requests.add(r);
        if (r.url.path.endsWith('/map-layers')) {
          final v = typed.mapTestView()..['cityId'] = 'fixture-city';
          return http.Response(
            jsonEncode({'data': v}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        if (r.url.path ==
            '/v1/places/${typed.mapTestPlace}/social-history/moments') {
          return nowResponse(_publicHistoryWire(observed));
        }
        if (r.url.path == '/v1/moments/${typed.mapTestID}') {
          if (++momentReads > 1) return late.future;
          return nowResponse(_publicMomentWire(observed));
        }
        return http.Response('{}', 404);
      });
      Future<void> show(String base) async {
        await tester.pumpWidget(
          MaterialApp(
            home: MapWorkspace(
              key: const ValueKey('same-map'),
              city: city,
              auth: auth,
              moments: moments,
              seedClient: client,
              seedApiBaseUrl: base,
            ),
          ),
        );
        await tester.pumpAndSettle();
      }

      await show('https://old.test');
      final map = tester.widget<MapCanvas>(find.byType(MapCanvas));
      final element = find.byType(MapCanvas).evaluate().single;
      map.onInitialViewport(typed.mapTestBounds);
      await tester.pumpAndSettle();
      expect(map.layers?.items.length, 5);
      map.workspace.selectEntity('moment:${typed.mapTestID}');
      await tester.pumpAndSettle();
      expect(find.byType(NowPlacePreview), findsNothing);
      expect(find.byType(NowPublicPlacePanel), findsOneWidget);
      expect(
        tester.widget<PublicMomentPage>(find.byType(PublicMomentPage)).momentID,
        typed.mapTestID,
      );
      expect(find.text('原始公开动态'), findsOneWidget);
      expect(
        requests.where((r) => r.url.path.startsWith('/v1/places/')),
        isEmpty,
      );
      final refresh = tester
          .widget<RefreshIndicator>(
            find.descendant(
              of: find.byType(PublicMomentPage),
              matching: find.byType(RefreshIndicator),
            ),
          )
          .onRefresh();
      await tester.pump();
      expect(
        requests
            .where((r) => r.url.path == '/v1/moments/${typed.mapTestID}')
            .length,
        2,
      );
      await show('https://new.test');
      late.complete(
        http.Response(
          jsonEncode({
            'data': {'title': '旧endpoint迟到私人正文'},
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      );
      await refresh;
      await tester.pumpAndSettle();
      expect(find.textContaining('旧endpoint迟到私人正文'), findsNothing);
      expect(find.byType(NowPublicPlacePanel), findsOneWidget);
      expect(find.text('请重新打开内容'), findsOneWidget);
      expect(find.byType(NowPlacePreview), findsNothing);
      await show('https://old.test');
      expect(find.text('请重新打开内容'), findsOneWidget);
      await tester.tap(find.byTooltip('关闭内容返回地图'));
      await tester.pumpAndSettle();
      expect(find.byType(NowPublicPlacePanel), findsNothing);
      expect(map.layers?.items, isEmpty);
      expect(
        identical(element, find.byType(MapCanvas).evaluate().single),
        true,
      );
      expect(requests.where((r) => r.method != 'GET'), isEmpty);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
      moments.dispose();
      auth.dispose();
      city.dispose();
      client.close();
    },
  );
  testWidgets('本人 ONLINE 情境查询回答卡片原ID详情与 A B A 撤回路线', (tester) async {
    final auth = _FixtureAuth()..person = onlineOwnerID, city = _FixtureCity();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    final requests = <http.Request>[];
    final choiceOptions = _nativeOnlineSelection('阅读伙伴');
    final client = MockClient((r) async {
      requests.add(r);
      Object? data;
      if (r.url.path == '/v1/me/now/context-selection/options') {
        data = choiceOptions;
      }
      if (r.url.path == '/v1/me/now/context-selection/resolve') {
        data = selectionReceipt(
          choiceOptions,
          (choiceOptions['items'] as List).single as Map<String, dynamic>,
        );
      }
      if (r.url.path == '/v1/me/now/online/tasks') data = onlineWire();
      if (r.url.path == '/v1/me/now/online/intents/$onlineIntentID') {
        data = onlineWire(task: false);
      }
      return http.Response(
        jsonEncode(
          data == null
              ? {
                  'error': {'code': 'unavailable'},
                }
              : {'data': data},
        ),
        data == null ? 503 : 200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          auth: auth,
          city: city,
          moments: moments,
          seedClient: client,
          seedApiBaseUrl: 'http://localhost',
        ),
      ),
    );
    await tester.pumpAndSettle();
    final element = find.byType(MapCanvas).evaluate().single;
    await _openViewChoice(tester);
    expect(find.text('阅读伙伴'), findsOneWidget);
    await tester.tap(find.text('阅读伙伴'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('核验并切换查看范围'));
    await tester.pumpAndSettle();
    expect(
      requests.where((r) => r.url.path == '/v1/me/now/online/tasks'),
      isEmpty,
    );
    expect(identical(element, find.byType(MapCanvas).evaluate().single), true);
    await tester.enterText(find.byType(TextField).first, '阅读');
    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.pumpAndSettle();
    expect(find.text('一起线上阅读'), findsOneWidget);
    final onlineMap = tester.widget<MapCanvas>(find.byType(MapCanvas));
    expect(onlineMap.workspace.task?.contextType, 'ONLINE');
    expect(onlineMap.workspace.task?.contextID, onlineContextID);
    expect(onlineMap.workspace.result?.onlineIntents.single.id, onlineIntentID);
    expect(onlineMap.workspace.result?.entities, isEmpty);
    await tester.tap(find.byKey(const Key('online-intent-$onlineIntentID')));
    await tester.pumpAndSettle();
    expect(find.text('公开线上意图'), findsOneWidget);
    expect(find.textContaining('意图编号：'), findsNothing);
    expect(find.textContaining(onlineIntentID), findsNothing);
    expect(find.text('一起线上阅读'), findsWidgets);
    final detailRequests = requests
        .where(
          (r) =>
              r.method == 'GET' &&
              r.url.path == '/v1/me/now/online/intents/$onlineIntentID',
        )
        .toList();
    expect(detailRequests, hasLength(1));
    expect(
      detailRequests.single.headers['Authorization'],
      'Bearer fixture-$onlineOwnerID',
    );
    auth.use('B');
    auth.use(onlineOwnerID);
    await tester.pumpAndSettle();
    expect(find.text('一起线上阅读'), findsNothing);
    expect(find.text('请重新打开内容'), findsOneWidget);
    expect(
      requests
          .where(
            (r) =>
                r.method == 'POST' && r.url.path == '/v1/me/now/online/tasks',
          )
          .length,
      1,
    );
    await tester.pumpWidget(const SizedBox());
    moments.dispose();
    auth.dispose();
    city.dispose();
    client.close();
  });
  testWidgets('线上情境旧路线同 key 更换 API后退役且不发送新POST', (tester) async {
    tester.view.physicalSize = const Size(320, 720);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final auth = _FixtureAuth()..person = onlineOwnerID, city = _FixtureCity();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    final newRequests = <http.Request>[];
    final oldClient = MockClient(
      (r) async => http.Response(
        jsonEncode({'data': _nativeOnlineSelection('旧情境')}),
        r.url.path == '/v1/me/now/context-selection/options' ? 200 : 503,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    final newClient = MockClient((r) async {
      newRequests.add(r);
      return http.Response('{}', 503);
    });
    Widget app(http.Client client, String base) => MaterialApp(
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(3)),
        child: child!,
      ),
      home: MapWorkspace(
        key: const ValueKey('same'),
        auth: auth,
        city: city,
        moments: moments,
        seedClient: client,
        seedApiBaseUrl: base,
      ),
    );
    await tester.pumpWidget(app(oldClient, 'http://old.invalid'));
    await tester.pumpAndSettle();
    await _openViewChoice(tester);
    await tester.scrollUntilVisible(
      find.text('旧情境'),
      160,
      scrollable: find.byType(Scrollable).last,
    );
    expect(find.text('旧情境'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(app(newClient, 'http://new.invalid'));
    await tester.pumpAndSettle();
    expect(find.text('旧情境'), findsNothing);
    expect(find.text('请重新打开内容'), findsOneWidget);
    await tester.pumpWidget(app(oldClient, 'http://old.invalid'));
    await tester.pumpAndSettle();
    expect(find.text('旧情境'), findsNothing);
    expect(newRequests.where((r) => r.method == 'POST'), isEmpty);
    await tester.pumpWidget(const SizedBox());
    moments.dispose();
    auth.dispose();
    city.dispose();
    oldClient.close();
    newClient.close();
  });
  testWidgets('身份变化保留公共目录地点选择，地图和区域不重新创建', (tester) async {
    final auth = _FixtureAuth(), city = _FixtureCity();
    final (map, moments) = await _show(tester, auth, city);
    final element = find.byType(MapCanvas).evaluate().single;
    const bounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.2);
    map.mapState.initializeViewport(bounds);
    map.workspace.selectEntity('place:public-fixture-place');
    await tester.pumpAndSettle();
    auth.use('B');
    await tester.pumpAndSettle();
    expect(map.workspace.selectedEntityId, 'place:public-fixture-place');
    expect(map.workspace.task, isNull);
    expect(map.workspace.result, isNull);
    expect(
      identical(find.byType(MapCanvas).evaluate().single, element),
      isTrue,
    );
    expect(map.mapState.viewportBounds, bounds);
    await _close(tester, auth, city, moments);
  });
  testWidgets('登录A切到登录B即时清旧Agent且拒迟到结果，地图实例和区域保持', (tester) async {
    final auth = _FixtureAuth(),
        city = _FixtureCity(),
        source = _PendingSource();
    final (map, moments) = await _show(tester, auth, city, agent: source);
    final element = find.byType(MapCanvas).evaluate().single;
    const bounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.2);
    map.mapState.initializeViewport(bounds);
    final turn = map.workspace.submit('A的私人活动查询', [], []);
    await tester.pump();
    expect(map.workspace.task, isNotNull);
    expect(auth.signedIn, isTrue);
    auth.use('B');
    expect(auth.signedIn, isTrue);
    expect(map.workspace.task, isNull);
    expect(map.workspace.result, isNull);
    expect(map.workspace.conversation, isEmpty);
    source.pending.complete(
      const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: 'A私密迟到回复',
        task: AgentTask(
          id: 'A-task',
          query: 'A-private',
          status: 'COMPLETED',
          principalID: 'A',
          messages: [AgentMessage(role: 'assistant', text: 'A私密迟到回复')],
        ),
      ),
    );
    await turn;
    await tester.pumpAndSettle();
    expect(map.workspace.task, isNull);
    expect(map.workspace.result, isNull);
    expect(map.workspace.recent, isEmpty);
    expect(find.textContaining('A私密迟到回复'), findsNothing);
    expect(
      identical(find.byType(MapCanvas).evaluate().single, element),
      isTrue,
    );
    expect(
      identical(tester.widget<MapCanvas>(find.byType(MapCanvas)), map),
      isTrue,
    );
    expect(map.mapState.viewportBounds, bounds);
    await _close(tester, auth, city, moments);
  });

  testWidgets('联系预览取消不发送；切换身份关闭旧预览且旧确认回调不发送', (tester) async {
    final auth = _FixtureAuth(), city = _FixtureCity();
    final writes = <http.Request>[];
    final client = MockClient((request) async {
      if (request.method != 'GET') writes.add(request);
      return http.Response('{}', 201);
    });
    final connections = ConnectionSource(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    final (map, moments) = await _show(
      tester,
      auth,
      city,
      connections: connections,
    );
    _showPerson(map);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byTooltip('申请联系'));
    await tester.tap(find.byTooltip('申请联系'));
    await tester.pumpAndSettle();
    expect(writes, isEmpty);
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(writes, isEmpty);
    await tester.tap(find.byTooltip('申请联系'));
    await tester.pumpAndSettle();
    await tester.enterText(find.widgetWithText(TextField, '留言'), '明确邀请留言');
    final confirm = tester
        .widget<FilledButton>(find.widgetWithText(FilledButton, '检查申请'))
        .onPressed!;
    auth.use('B');
    confirm();
    await tester.pumpAndSettle();
    expect(find.text('联系 合成候选'), findsNothing);
    expect(writes, isEmpty);
    expect(map.workspace.result, isNull);
    expect(tester.takeException(), isNull);
    await _close(tester, auth, city, moments);
    client.close();
  });

  testWidgets('当前身份明确发送只写一次；注销后迟到成功不显示旧反馈', (tester) async {
    final auth = _FixtureAuth(), city = _FixtureCity();
    final writes = <http.Request>[];
    final pending = Completer<http.Response>();
    final client = MockClient((request) async {
      if (request.method == 'GET') {
        final d = actionWire(
          ref: const EntityActionRef(
            'person',
            '11111111-1111-4111-8111-111111111111',
          ),
        );
        for (final dynamic a in d['actions'] as List) {
          a['state'] = a['kind'] == 'CONNECT' ? 'AVAILABLE' : 'UNAVAILABLE';
        }
        return http.Response.bytes(utf8.encode(jsonEncode({'data': d})), 200);
      }
      writes.add(request);
      return pending.future;
    });
    final connections = ConnectionSource(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    final (map, moments) = await _show(
      tester,
      auth,
      city,
      connections: connections,
    );
    _showPerson(map);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byTooltip('申请联系'));
    await tester.tap(find.byTooltip('申请联系'));
    await tester.pumpAndSettle();
    await tester.enterText(find.widgetWithText(TextField, '留言'), '确认邀请留言');
    await tester.pump();
    await tester.tap(find.text('检查申请'));
    await tester.pumpAndSettle();
    expect(writes, isEmpty);
    await tester.tap(find.text('发送好友申请'));
    await tester.pump();
    expect(writes, hasLength(1));
    expect(writes.single.headers['Authorization'], 'Bearer fixture-A');
    expect(writes.single.headers['X-Birdtie-Action-Version'], 'a' * 64);
    expect(
      writes.single.headers['X-Birdtie-Action-Operation'],
      'REQUEST_FRIEND',
    );
    expect(writes.single.headers['X-Birdtie-Action-Until'], isNotEmpty);
    expect(jsonDecode(writes.single.body), {
      'recipientAccountId': '11111111-1111-4111-8111-111111111111',
      'scope': 'friend',
      'note': '确认邀请留言',
    });
    auth.use(null);
    pending.complete(http.Response('{}', 201));
    await tester.pumpAndSettle();
    expect(writes, hasLength(1));
    expect(find.text('联系 合成候选'), findsNothing);
    expect(find.textContaining('申请已发送'), findsNothing);
    expect(tester.takeException(), isNull);
    await _close(tester, auth, city, moments);
    client.close();
  });
  testWidgets('本人明确私信申请保持city和conversation scope，注销迟到不反馈', (tester) async {
    final auth = _FixtureAuth(), city = _FixtureCity();
    final writes = <http.Request>[];
    final pending = Completer<http.Response>();
    final client = MockClient((request) async {
      if (request.method == 'GET') {
        final d = actionWire(
          ref: const EntityActionRef(
            'person',
            '11111111-1111-4111-8111-111111111111',
          ),
        );
        for (final dynamic a in d['actions'] as List) {
          a['state'] = a['kind'] == 'CONNECT' ? 'AVAILABLE' : 'UNAVAILABLE';
        }
        d['actions'][0]['allowedOperations'] = [
          'REQUEST_FRIEND',
          'REQUEST_CONVERSATION',
        ];
        return http.Response.bytes(utf8.encode(jsonEncode({'data': d})), 200);
      }
      writes.add(request);
      return pending.future;
    });
    final connections = ConnectionSource(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    final (map, moments) = await _show(
      tester,
      auth,
      city,
      connections: connections,
    );
    _showPerson(map);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byTooltip('申请联系'));
    await tester.tap(find.byTooltip('申请联系'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('申请私信'));
    await tester.pump();
    await tester.enterText(find.widgetWithText(TextField, '留言'), '确认邀请留言');
    await tester.pump();
    await tester.tap(find.text('检查申请'));
    await tester.pumpAndSettle();
    expect(writes, isEmpty);
    await tester.tap(find.text('发送私信申请'));
    await tester.pump();
    expect(writes, hasLength(1));
    expect(writes.single.headers['Authorization'], 'Bearer fixture-A');
    expect(writes.single.headers['X-Birdtie-Action-Version'], 'a' * 64);
    expect(
      writes.single.headers['X-Birdtie-Action-Operation'],
      'REQUEST_CONVERSATION',
    );
    expect(writes.single.headers['X-Birdtie-Action-Until'], isNotEmpty);
    expect(jsonDecode(writes.single.body), {
      'recipientAccountId': '11111111-1111-4111-8111-111111111111',
      'cityId': 'fixture-city',
      'note': '确认邀请留言',
    });
    auth.use(null);
    pending.complete(http.Response('{}', 201));
    await tester.pumpAndSettle();
    expect(writes, hasLength(1));
    expect(find.text('联系 合成候选'), findsNothing);
    expect(find.textContaining('申请已发送'), findsNothing);
    expect(tester.takeException(), isNull);
    await _close(tester, auth, city, moments);
    client.close();
  });
}
