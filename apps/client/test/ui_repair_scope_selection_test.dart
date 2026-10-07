import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_entity_result_card.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_context_selection_page.dart';
import 'package:birdtie_client/src/workspace/person_contexts_page.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'chat_entity_router_test.dart' show ChatTestAuth;
import 'model_egress_api_test.dart' show egressOwner;
import 'now_context_selection_api_test.dart';

class _ScopeCity extends PublicCityController {
  static const value = PublicCity(
    id: 'synthetic-now006-city',
    name: '合成城市',
    region: '仅单元夹具',
    contentStatus: 'building',
    source: PublicSource(
      label: 'fixture',
      maintainer: 'test',
      freshness: 'unverified',
      updatedAt: null,
    ),
    map: null,
  );
  @override
  List<PublicCity> get cities => const [value];
}

Future<void> _frame(
  WidgetTester t,
  BirdtieAuthController auth,
  http.Client client, {
  double scale = 1,
  PublicCityController? cityController,
  AgentTaskSource? source,
}) async {
  final city = cityController ?? _ScopeCity();
  final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: client,
    apiBaseUrl: 'https://scope.test',
  );
  await t.pumpWidget(
    MaterialApp(
      builder: (c, child) => MediaQuery(
        data: MediaQuery.of(c).copyWith(textScaler: TextScaler.linear(scale)),
        child: child!,
      ),
      home: MapWorkspace(
        city: city,
        auth: auth,
        moments: moments,
        agentTaskSource: source,
        seedClient: client,
        seedApiBaseUrl: 'https://scope.test',
      ),
    ),
  );
  await t.pumpAndSettle();
  addTearDown(() async {
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    city.dispose();
    auth.dispose();
    client.close();
  });
}

Future<void> _open(WidgetTester t) async {
  await t.tap(find.byKey(const Key('now-city-picker')));
  await t.pumpAndSettle();
}

Future<void> _tap(WidgetTester t, Finder finder) async {
  final scroll = find.descendant(
    of: find.byKey(const Key('now-scope-options')),
    matching: find.byType(Scrollable),
  );
  await t.scrollUntilVisible(finder, 180, scrollable: scroll);
  await t.pumpAndSettle();
  await t.tap(finder);
  await t.pumpAndSettle();
}

http.Response _empty() => http.Response('{"data":[]}', 200);

const _oldCityID = 'synthetic-old-city';
const _destinationCityID = 'synthetic-now006-city';

// Synthetic HTTP fixtures exercise the original catalog controller and typed
// projection. They are not live source or device evidence.
Future<PublicCityController> _catalog(http.Client client) async {
  final city = PublicCityController(
    client: client,
    apiBaseUrl: 'https://scope.test',
  );
  await city.loadCities();
  city.selectCity(_oldCityID);
  return city;
}

http.Response _cities({bool includeDestination = true}) => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': [
        for (final id in [
          _oldCityID,
          if (includeDestination) _destinationCityID,
        ])
          {
            'id': id,
            'name': id == _oldCityID ? '原合成城市' : '目的合成城市',
            'region': '仅单元夹具',
            'contentStatus': 'building',
            'source': {
              'label': 'fixture',
              'maintainer': 'test',
              'freshness': 'unverified',
            },
          },
      ],
    }),
  ),
  200,
  headers: {'content-type': 'application/json'},
);

class _CityTurns extends AgentTaskSource {
  _CityTurns(this.city);
  final PublicCityController city;
  final queries = <String>[];
  final scopes = <String?>[];
  final continued = <AgentTask>[];
  final pending = <Completer<AgentResult>>[];

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) {
    queries.add(query);
    scopes.add(city.selectedCity?.id);
    final next = Completer<AgentResult>();
    pending.add(next);
    return next.future;
  }

  @override
  Future<AgentResult> followUp(
    AgentTask task,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) {
    continued.add(task);
    return resolve(query, activities, places);
  }

  AgentResult reply(int turn) => AgentResult(
    entities: const [],
    activities: const [],
    places: const [],
    note: '',
    message: '第$turn轮合成地点',
    task: const AgentTask(
      id: 'canonical-city-task',
      query: '合成地点查询',
      status: 'COMPLETED',
      cityID: _destinationCityID,
      intent: 'FIND_PLACE',
      principalID: egressOwner,
      actingUserID: egressOwner,
      filters: {'targetIntent': 'FIND_PLACE', 'fixture': 'retained'},
    ),
    resultSet: AgentResultSet(
      id: 'city-result-$turn',
      status: 'ready',
      generatedAt: DateTime.utc(2026, 10, 8),
      entities: [AgentResultRef(type: 'place', id: 'target-$turn')],
      items: [
        AgentResultItem(
          entity: AgentResultRef(type: 'place', id: 'target-$turn'),
          title: '目的城市地点$turn',
          summary: '仅用于跨城投影验证',
          scope: 'AUTHORIZED_VIEW',
          detail: AgentResultRef(type: 'place', id: 'target-$turn'),
          anchor: const AgentResultAnchor(
            precision: 'point',
            latitude: 57.14,
            longitude: -2.1,
          ),
        ),
      ],
    ),
  );
}

Future<void> _chooseDeclaredCity(
  WidgetTester t,
  Map<String, dynamic> options,
) async {
  await _open(t);
  final optionID = (options['items'] as List).first['optionId'];
  await _tap(t, find.byKey(Key('now-scope-option-$optionID')));
  await _tap(t, find.byKey(const Key('now-scope-confirm')));
}

void _submit(WidgetTester t, String query) =>
    t.widget<AgentComposer>(find.byType(AgentComposer)).onSubmit(query);

void main() {
  testWidgets('游客320大字同sheet显示城市与线上，关闭可达且无本人范围请求', (t) async {
    t.view.physicalSize = const Size(320, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final auth = BirdtieAuthController(sessionVault: MemorySessionVault());
    var personal = 0, writes = 0;
    final client = MockClient((r) async {
      if (r.url.path.contains('context-selection')) personal++;
      if (r.method != 'GET') writes++;
      return _empty();
    });
    await _frame(t, auth, client, scale: 2);
    final map = find.byType(MapCanvas).evaluate().single;
    await _open(t);
    expect(find.byKey(const Key('now-scope-picker')), findsOneWidget);
    expect(find.byType(NowContextSelectionPage), findsNothing);
    expect(find.text('选择查询情境'), findsNothing);
    expect(find.text('当前城市'), findsNothing);
    final city = find.byKey(const Key('now-scope-city-synthetic-now006-city'));
    expect(city, findsOneWidget);
    final close = find.byTooltip('取消选择城市与范围');
    expect(close.hitTestable(), findsOneWidget);
    expect(t.getSize(close).height, greaterThanOrEqualTo(48));
    await _tap(t, find.byKey(const Key('now-scope-online')));
    expect(find.text('线上社交发现'), findsOneWidget);
    expect(personal, 0);
    expect(writes, 0);
    expect(find.byKey(const Key('now-scope-picker')), findsNothing);
    expect(
      identical(
        find.byType(MapCanvas, skipOffstage: false).evaluate().single,
        map,
      ),
      isTrue,
    );
    expect(t.takeException(), isNull);
  });

  testWidgets('本人线上范围读原receipt后仅改查看范围，任务结果地图与草稿保留', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final raw = selectionOptions();
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      if (r.url.path.endsWith('/context-selection/options')) {
        return selectionResponse(raw);
      }
      if (r.url.path.endsWith('/context-selection/resolve')) {
        return selectionResponse(
          selectionReceipt(raw, (raw['items'] as List)[1]),
        );
      }
      return _empty();
    });
    await _frame(t, auth, client);
    final map = find.byType(MapCanvas).evaluate().single;
    final workspace = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
    const task = AgentTask(
      id: 'local-existing',
      query: '旧任务',
      status: 'COMPLETED',
    );
    const result = AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '夹具旧结果',
    );
    workspace.task = task;
    workspace.result = result;
    workspace.selectedEntityId = 'place:retained';
    workspace.setSheetExtent(AgentSheetExtent.expanded);
    t
        .state<AgentComposerState>(find.byType(AgentComposer))
        .fillDraft('未发送草稿', requestFocus: false);
    await t.pumpAndSettle();
    await _open(t);
    expect(
      find.text('本人合成城市'),
      findsNWidgets(2),
      reason: '当前与过去的本人声明分别保留，不冒充实时所在地',
    );
    expect(find.byType(NowContextSelectionPage), findsNothing);
    await _tap(t, find.text('本人合成线上情境'));
    expect(calls.where((r) => r.method == 'POST'), isEmpty);
    await _tap(t, find.byKey(const Key('now-scope-confirm')));
    final resolve = calls.where((r) => r.method == 'POST').single;
    expect(resolve.url.path, '/v1/me/now/context-selection/resolve');
    expect(jsonDecode(resolve.body), {
      'optionsToken': raw['optionsToken'],
      'optionId': (raw['items'] as List)[1]['optionId'],
    });
    expect(workspace.task, same(task));
    expect(workspace.result, same(result));
    expect(workspace.selectedEntityId, 'place:retained');
    expect(workspace.queryContextType, 'CITY', reason: '查看切换不是查询提交');
    expect(
      t.widget<TopControls>(find.byType(TopControls)).contextLabel,
      '本人合成线上情境',
    );
    expect(
      t.widget<TextField>(find.byType(TextField).first).controller!.text,
      '未发送草稿',
    );
    expect(identical(find.byType(MapCanvas).evaluate().single, map), isTrue);
    expect(t.takeException(), isNull);
  });

  testWidgets('范围读取失败保留公开城市，明确重读后恢复本人选项', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    var reads = 0, writes = 0;
    final raw = selectionOptions();
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      if (r.url.path.endsWith('/context-selection/options')) {
        reads++;
        return reads == 1 ? http.Response('{}', 401) : selectionResponse(raw);
      }
      return _empty();
    });
    await _frame(t, auth, client);
    await _open(t);
    expect(
      find.byKey(const Key('now-scope-city-synthetic-now006-city')),
      findsOneWidget,
    );
    await _tap(t, find.text('重新读取本人范围'));
    expect(reads, 2);
    expect(writes, 0);
    await t.scrollUntilVisible(
      find.text('本人合成线上情境'),
      -180,
      scrollable: find.descendant(
        of: find.byKey(const Key('now-scope-options')),
        matching: find.byType(Scrollable),
      ),
    );
    expect(find.text('本人合成线上情境'), findsOneWidget);
    expect(t.takeException(), isNull);
  });

  testWidgets('账号ABA后迟到范围不重现，不允许旧receipt或旧选项执行', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final pending = Completer<http.Response>();
    var posts = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') posts++;
      if (r.url.path.endsWith('/context-selection/options')) {
        return pending.future;
      }
      return _empty();
    });
    await _frame(t, auth, client);
    await t.tap(find.byKey(const Key('now-city-picker')));
    await t.pump();
    await t.pump(const Duration(milliseconds: 400));
    auth.use('Bearer B');
    auth.use('Bearer A');
    pending.complete(selectionResponse(selectionOptions()));
    await t.pumpAndSettle();
    expect(find.text('账号或工作身份已变化，请关闭后重新选择。'), findsOneWidget);
    expect(find.text('本人合成线上情境'), findsNothing);
    expect(find.byKey(const Key('now-scope-confirm')), findsNothing);
    expect(find.byKey(const Key('now-scope-online')), findsNothing);
    expect(posts, 0);
    await t.tap(find.byTooltip('取消选择城市与范围'));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('now-scope-picker')), findsNothing);
    expect(t.takeException(), isNull);
  });

  testWidgets('声明管理沿用PersonContextsPage，返回同picker重新读取且不新增声明', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    var reads = 0, writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      if (r.url.path.endsWith('/context-selection/options')) {
        reads++;
        return selectionResponse(selectionOptions());
      }
      return _empty();
    });
    await _frame(t, auth, client);
    final map = find.byType(MapCanvas).evaluate().single;
    await _open(t);
    await _tap(t, find.byKey(const Key('now-scope-manage')));
    expect(find.byType(PersonContextsPage), findsOneWidget);
    expect(find.byType(NowContextSelectionPage), findsNothing);
    expect(find.byKey(const Key('now-scope-picker')), findsNothing);
    final page = t.element(find.byType(PersonContextsPage));
    final navigator = Navigator.of(page);
    navigator.pop();
    await t.pumpAndSettle();
    expect(find.byKey(const Key('now-scope-picker')), findsOneWidget);
    expect(reads, 2);
    expect(writes, 0);
    expect(identical(find.byType(MapCanvas).evaluate().single, map), isTrue);
    expect(t.takeException(), isNull);
  });

  testWidgets('跨城receipt只在提交时同步地图，typed卡片地点与多轮任务主体一致', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final raw = selectionOptions();
    var optionReads = 0, receiptReads = 0;
    final client = MockClient((r) async {
      if (r.url.path == '/v1/cities') return _cities();
      if (r.url.path.endsWith('/context-selection/options')) {
        optionReads++;
        return selectionResponse(raw);
      }
      if (r.url.path.endsWith('/context-selection/resolve')) {
        receiptReads++;
        return selectionResponse(
          selectionReceipt(raw, (raw['items'] as List).first),
        );
      }
      return _empty();
    });
    final city = await _catalog(client);
    final source = _CityTurns(city);
    await _frame(t, auth, client, cityController: city, source: source);
    final workspace = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
    final map = find.byType(PublicCityMapView).evaluate().single;
    const oldTask = AgentTask(
      id: 'existing-old-city-task',
      query: '原城市会话',
      status: 'COMPLETED',
      cityID: _oldCityID,
      principalID: egressOwner,
      actingUserID: egressOwner,
    );
    const oldResult = AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '旧夹具',
    );
    workspace.task = oldTask;
    workspace.result = oldResult;
    workspace.setSheetExtent(AgentSheetExtent.expanded);
    final beforeEpoch = workspace.taskEpoch;
    await _chooseDeclaredCity(t, raw);
    expect(city.selectedCity!.id, _oldCityID, reason: '单纯查看选择不改变地图城市');
    expect(workspace.task, same(oldTask));
    expect(workspace.result, same(oldResult));
    expect(
      workspace.taskEpoch,
      beforeEpoch + 1,
      reason: '查看选择退役旧的待返回查询，但不清除原任务或结果',
    );
    expect(source.queries, isEmpty);
    expect(optionReads, 1);
    expect(receiptReads, 1);

    _submit(t, '在本人目的城市找地点');
    await t.pump();
    await t.pump(const Duration(milliseconds: 400));
    expect(optionReads, 2, reason: '提交重新读取本人当前范围');
    expect(receiptReads, 2, reason: '提交重新核验原选项，不借用旧receipt');
    expect(source.scopes, [_destinationCityID]);
    expect(city.selectedCity!.id, _destinationCityID);
    expect(workspace.task!.cityID, _destinationCityID);
    final first = source.reply(1);
    source.pending.single.complete(first);
    await t.pumpAndSettle();
    expect(workspace.result, same(first), reason: '城市listener没有退役刚提交的结果');
    expect(workspace.task!.id, 'canonical-city-task');
    expect(workspace.task!.principalID, egressOwner);
    expect(workspace.task!.actingUserID, egressOwner);
    final card = t.widget<AgentEntityResultCard>(
      find.byType(AgentEntityResultCard),
    );
    expect(card.item, same(first.projectionItems!.single));
    expect(card.onOpen, isNotNull);
    var view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(view.city!.id, _destinationCityID);
    expect(view.entities.map((entity) => entity.id), ['place:target-1']);
    expect(
      view.entities.map((entity) => entity.id),
      first.entities.map((entity) => entity.id),
    );
    expect(
      view.entities.single.id,
      first.projectionItems!.single.detail!.mapID,
    );
    expect(view.entities.single.id, card.item.entity.mapID);
    view.onEntitySelected!(view.entities.single);
    await t.pumpAndSettle();
    expect(workspace.selectedEntityId, 'place:target-1');
    expect(
      identical(find.byType(PublicCityMapView).evaluate().single, map),
      isTrue,
    );

    final previous = workspace.task!;
    _submit(t, '再找一个地点');
    await t.pump();
    await t.pump(const Duration(milliseconds: 400));
    expect(source.continued.single, same(previous));
    expect(source.continued.single.principalID, egressOwner);
    expect(source.continued.single.actingUserID, egressOwner);
    expect(source.continued.single.filters, previous.filters);
    expect(source.scopes, [_destinationCityID, _destinationCityID]);
    final second = source.reply(2);
    source.pending.last.complete(second);
    await t.pumpAndSettle();
    expect(workspace.result, same(second));
    expect(workspace.replies.map((reply) => reply.result), [first, second]);
    view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(view.entities.map((entity) => entity.id), ['place:target-2']);
    expect(
      view.entities.map((entity) => entity.id),
      second.entities.map((entity) => entity.id),
    );
    expect(optionReads, 3);
    expect(receiptReads, 3);
    expect(t.takeException(), isNull);
  });

  testWidgets('核验CITY不在真实目录时保留旧城市任务结果与输入且不发查询', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final raw = selectionOptions();
    final client = MockClient((r) async {
      if (r.url.path == '/v1/cities') return _cities(includeDestination: false);
      if (r.url.path.endsWith('/context-selection/options')) {
        return selectionResponse(raw);
      }
      if (r.url.path.endsWith('/context-selection/resolve')) {
        return selectionResponse(
          selectionReceipt(raw, (raw['items'] as List).first),
        );
      }
      return _empty();
    });
    final city = await _catalog(client);
    final source = _CityTurns(city);
    await _frame(t, auth, client, cityController: city, source: source);
    final workspace = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
    const oldTask = AgentTask(
      id: 'old-task',
      query: '旧查询',
      status: 'COMPLETED',
      cityID: _oldCityID,
    );
    const oldResult = AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '旧结果夹具',
    );
    workspace.task = oldTask;
    workspace.result = oldResult;
    workspace.setSheetExtent(AgentSheetExtent.expanded);
    await _chooseDeclaredCity(t, raw);
    await t.pump(const Duration(seconds: 5));
    await t.pumpAndSettle();
    final epoch = workspace.taskEpoch;
    _submit(t, '保留目的城市查询草稿');
    await t.pumpAndSettle();
    expect(source.queries, isEmpty);
    expect(city.selectedCity!.id, _oldCityID);
    expect(workspace.task, same(oldTask));
    expect(workspace.result, same(oldResult));
    expect(workspace.taskEpoch, epoch);
    expect(
      t.widget<TextField>(find.byType(TextField).first).controller!.text,
      '保留目的城市查询草稿',
    );
    expect(find.text('查看范围或会话已变化，请重新选择。原任务、结果和输入已保留。'), findsOneWidget);
    expect(t.takeException(), isNull);
  });

  testWidgets('跨城提交核验期间账号ABA使迟到receipt失效，不切地图或发旧查询', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final raw = selectionOptions();
    final lateReceipt = Completer<http.Response>();
    var receipts = 0;
    final client = MockClient((r) async {
      if (r.url.path == '/v1/cities') return _cities();
      if (r.url.path.endsWith('/context-selection/options')) {
        return selectionResponse(raw);
      }
      if (r.url.path.endsWith('/context-selection/resolve')) {
        receipts++;
        if (receipts == 2) return lateReceipt.future;
        return selectionResponse(
          selectionReceipt(raw, (raw['items'] as List).first),
        );
      }
      return _empty();
    });
    final city = await _catalog(client);
    final source = _CityTurns(city);
    await _frame(t, auth, client, cityController: city, source: source);
    await _chooseDeclaredCity(t, raw);
    _submit(t, '不可复用旧账号的目的城市查询');
    await t.pump();
    expect(receipts, 2);
    auth.use('Bearer B');
    auth.use('Bearer A');
    lateReceipt.complete(
      selectionResponse(selectionReceipt(raw, (raw['items'] as List).first)),
    );
    await t.pumpAndSettle();
    expect(source.queries, isEmpty);
    expect(city.selectedCity!.id, _oldCityID);
    expect(
      t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).city!.id,
      _oldCityID,
    );
    expect(
      t.widget<MapCanvas>(find.byType(MapCanvas)).workspace.result,
      isNull,
    );
    expect(t.takeException(), isNull);
  });
}
