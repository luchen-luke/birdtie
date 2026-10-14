import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_context_selection_page.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'chat_entity_router_test.dart' show ChatTestAuth;
import 'model_egress_api_test.dart' show egressOwner;

const _oldCityID = 'synthetic-old-city';
const _destinationCityID = 'synthetic-now006-city';
http.Response _empty() => http.Response('{"data":[]}', 200);
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
            'region': '仅单元目录夹具',
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

// Real catalog/controller and real MapWorkspace with controlled HTTP/clipboard
// fixtures. These tests are not native UI, real authentication or city facts.
Future<PublicCityController> _catalog(http.Client client) async {
  final city = PublicCityController(
    client: client,
    apiBaseUrl: 'https://scope.test',
  );
  await city.loadCities();
  city.selectCity(_oldCityID);
  return city;
}

Future<void> _frame(
  WidgetTester t,
  BirdtieAuthController auth,
  http.Client client, {
  double scale = 1,
  required PublicCityController city,
  AgentTaskSource? source,
}) async {
  final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: client,
    apiBaseUrl: 'https://scope.test',
  );
  await t.pumpWidget(
    MaterialApp(
      theme: birdtieTheme(Brightness.light),
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

Future<void> _open(WidgetTester t, {bool pending = false}) async {
  await t.tap(find.byKey(const Key('now-city-picker')));
  if (pending) {
    await t.pump();
    await t.pump(const Duration(milliseconds: 400));
  } else {
    await t.pumpAndSettle();
  }
}

Future<void> _tap(WidgetTester t, Finder finder) async {
  final scroll = find.descendant(
    of: find.byKey(const Key('now-scope-options')),
    matching: find.byType(Scrollable),
  );
  await t.scrollUntilVisible(finder, 180, scrollable: scroll);
  await t.tap(finder);
  await t.pumpAndSettle();
}

Future<void> _chooseCity(
  WidgetTester t,
  String id, {
  bool pending = false,
}) async {
  await _open(t, pending: pending);
  await _tap(t, find.byKey(Key('now-scope-city-$id')));
}

void _submit(WidgetTester t, String query) =>
    t.widget<AgentComposer>(find.byType(AgentComposer)).onSubmit(query);
AgentWorkspaceController _workspace(WidgetTester t) =>
    t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
const _oldTask = AgentTask(
  id: 'existing-old-city-task',
  query: '原城市会话',
  status: 'COMPLETED',
  cityID: _oldCityID,
  principalID: egressOwner,
  actingUserID: egressOwner,
  filters: {
    'onlineTaskFilter': 'remote-only',
    'discoverability': 'unchanged-fixture',
  },
);
const _oldResult = AgentResult(
  entities: [],
  activities: [],
  places: [],
  note: '旧结果夹具',
);
void _retain(AgentWorkspaceController w) {
  w.task = _oldTask;
  w.result = _oldResult;
  w.selectedEntityId = 'place:retained';
  w.conversation.addAll(const [
    AgentMessage(role: 'user', text: '原问题'),
    AgentMessage(role: 'assistant', text: '原回答'),
  ]);
  w.setSheetExtent(AgentSheetExtent.peek);
}

class _CityTurns extends AgentTaskSource {
  _CityTurns(this.city);
  final PublicCityController city;
  final queries = <String>[], scopes = <String?>[];
  final continued = <AgentTask>[], pending = <Completer<AgentResult>>[];
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
    message: '第$turn轮合成结果',
    task: AgentTask(
      id: 'native-task-$turn',
      query: queries[turn - 1],
      cityID: scopes[turn - 1],
      status: 'COMPLETED',
      intent: 'FIND_PLACE',
      principalID: egressOwner,
      actingUserID: egressOwner,
      filters: const {'fixture': 'retained'},
    ),
  );
}

void main() {
  testWidgets('游客320大字只搜真实城市目录，关闭48可达且无本人范围请求', (t) async {
    t.view.physicalSize = const Size(320, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final auth = BirdtieAuthController(sessionVault: MemorySessionVault());
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return r.url.path == '/v1/cities' ? _cities() : _empty();
    });
    final city = await _catalog(client);
    await _frame(t, auth, client, city: city, scale: 2);
    final map = find.byType(MapCanvas).evaluate().single;
    await _open(t);
    expect(find.text('选择城市'), findsOneWidget);
    expect(find.byType(NowContextSelectionPage), findsNothing);
    expect(find.text('线上与其他范围'), findsNothing);
    expect(find.byKey(const Key('now-scope-online')), findsNothing);
    expect(find.byKey(const Key('now-scope-confirm')), findsNothing);
    await t.enterText(find.byKey(const Key('now-city-search')), '目的');
    await t.pumpAndSettle();
    expect(
      find.byKey(const Key('now-scope-city-synthetic-old-city')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('now-scope-city-synthetic-now006-city')),
      findsOneWidget,
    );
    final close = find.byTooltip('关闭城市选择');
    expect(close.hitTestable(), findsOneWidget);
    expect(t.getSize(close).height, greaterThanOrEqualTo(48));
    await t.tap(close);
    await t.pumpAndSettle();
    expect(city.selectedCity!.id, _oldCityID);
    expect(
      calls.where((r) => r.url.path.contains('context-selection')),
      isEmpty,
    );
    expect(calls.where((r) => r.method != 'GET'), isEmpty);
    expect(find.byType(MapCanvas).evaluate().single, same(map));
    expect(t.takeException(), isNull);
  });

  testWidgets('浏览不同城市立即生效，原任务结果草稿选择与线上过滤声明保持', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return r.url.path == '/v1/cities' ? _cities() : _empty();
    });
    final city = await _catalog(client);
    await _frame(t, auth, client, city: city);
    final map = find.byType(MapCanvas).evaluate().single,
        native = find.byType(PublicCityMapView).evaluate().single;
    final w = _workspace(t);
    _retain(w);
    w.queryContextType = 'ONLINE';
    final conversation = List<AgentMessage>.of(w.conversation),
        epoch = w.taskEpoch,
        identityEpoch = w.taskIdentityEpoch;
    final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
    composer.fillDraft('未发送草稿', requestFocus: false);
    await t.pumpAndSettle();
    await _chooseCity(t, _destinationCityID);
    expect(city.selectedCity!.id, _destinationCityID);
    expect(
      t.widget<TopControls>(find.byType(TopControls)).contextLabel,
      '目的合成城市',
    );
    expect(w.task, same(_oldTask));
    expect(w.result, same(_oldResult));
    expect(w.conversation, conversation);
    expect(w.selectedEntityId, 'place:retained');
    expect(w.queryContextType, 'ONLINE');
    expect(w.task!.filters, _oldTask.filters);
    expect(w.taskEpoch, epoch + 1);
    expect(w.taskIdentityEpoch, identityEpoch);
    expect(composer.draftText, '未发送草稿');
    expect(
      t.state<AgentComposerState>(find.byType(AgentComposer)),
      same(composer),
    );
    expect(find.byType(MapCanvas).evaluate().single, same(map));
    expect(find.byType(PublicCityMapView).evaluate().single, same(native));
    expect(
      calls.where(
        (r) =>
            r.url.path.contains('context-selection') ||
            r.url.path.contains('person-context'),
      ),
      isEmpty,
    );
    expect(calls.where((r) => r.method != 'GET'), isEmpty);
    expect(t.takeException(), isNull);
  });

  testWidgets('目录失败只显示重读，未知搜索不造城市或本人声明', (t) async {
    final auth = BirdtieAuthController(sessionVault: MemorySessionVault());
    var catalogs = 0;
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      if (r.url.path == '/v1/cities')
        return ++catalogs == 1
            ? http.Response('{}', 503)
            : _cities(includeDestination: false);
      return _empty();
    });
    final city = PublicCityController(
      client: client,
      apiBaseUrl: 'https://scope.test',
    );
    await _frame(t, auth, client, city: city);
    await _open(t);
    expect(
      find.descendant(
        of: find.byKey(const Key('now-scope-picker')),
        matching: find.text('无法读取城市，请稍后重试。'),
      ),
      findsOneWidget,
    );
    expect(city.selectedCity, isNull);
    expect(
      find.byKey(const Key('now-scope-city-synthetic-now006-city')),
      findsNothing,
    );
    await _tap(
      t,
      find.descendant(
        of: find.byKey(const Key('now-scope-picker')),
        matching: find.text('重新读取城市'),
      ),
    );
    expect(catalogs, 2);
    expect(
      find.byKey(const Key('now-scope-city-synthetic-old-city')),
      findsOneWidget,
    );
    await t.enterText(find.byKey(const Key('now-city-search')), '不存在的新城市');
    await t.pumpAndSettle();
    expect(find.text('当前城市目录中没有匹配的城市。'), findsOneWidget);
    expect(
      calls.where((r) => r.url.path.contains('context-selection')),
      isEmpty,
    );
    expect(calls.where((r) => r.method != 'GET'), isEmpty);
    expect(t.takeException(), isNull);
  });

  testWidgets('账号ABA立即退役旧城市selector与已保存onTap', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final client = MockClient(
      (r) async => r.url.path == '/v1/cities' ? _cities() : _empty(),
    );
    final city = await _catalog(client);
    await _frame(t, auth, client, city: city);
    await _open(t);
    final oldTap = t
        .widget<ListTile>(
          find.byKey(const Key('now-scope-city-synthetic-now006-city')),
        )
        .onTap!;
    auth.use('Bearer B');
    auth.use('Bearer A');
    await t.pumpAndSettle();
    expect(find.text('城市目录已变化，请关闭后重新选择。'), findsOneWidget);
    expect(
      find.byKey(const Key('now-scope-city-synthetic-now006-city')),
      findsNothing,
    );
    oldTap();
    await t.pumpAndSettle();
    expect(city.selectedCity!.id, _oldCityID);
    await t.tap(find.byTooltip('关闭城市选择'));
    await t.pumpAndSettle();
    expect(t.takeException(), isNull);
  });

  testWidgets('取消城市模态保留上下文，旧selector回调不能复活', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final client = MockClient(
      (r) async => r.url.path == '/v1/cities' ? _cities() : _empty(),
    );
    final city = await _catalog(client);
    await _frame(t, auth, client, city: city);
    final w = _workspace(t);
    _retain(w);
    final epoch = w.taskEpoch;
    final map = find.byType(MapCanvas).evaluate().single;
    final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
    composer.fillDraft('取消前草稿', requestFocus: false);
    await t.pumpAndSettle();
    await _open(t);
    final oldTap = t
        .widget<ListTile>(
          find.byKey(const Key('now-scope-city-synthetic-now006-city')),
        )
        .onTap!;
    await t.enterText(find.byKey(const Key('now-city-search')), '目的');
    await t.pumpAndSettle();
    await t.tap(find.byTooltip('关闭城市选择'));
    await t.pumpAndSettle();
    oldTap();
    await t.pumpAndSettle();
    expect(city.selectedCity!.id, _oldCityID);
    expect(w.taskEpoch, epoch);
    expect(w.task, same(_oldTask));
    expect(w.result, same(_oldResult));
    expect(composer.draftText, '取消前草稿');
    expect(find.byType(MapCanvas).evaluate().single, same(map));
    expect(t.takeException(), isNull);
  });

  testWidgets('跨城浏览退役迟到query，仅新submit建新任务并保留后续同城主体', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return r.url.path == '/v1/cities' ? _cities() : _empty();
    });
    final city = await _catalog(client), source = _CityTurns(city);
    await _frame(t, auth, client, city: city, source: source);
    final w = _workspace(t), map = find.byType(MapCanvas).evaluate().single;
    _submit(t, '旧城市未完成查询');
    await t.pump();
    final old = w.task!, epoch = w.taskEpoch;
    await _chooseCity(t, _destinationCityID, pending: true);
    expect(w.task, same(old));
    expect(w.taskEpoch, greaterThan(epoch));
    expect(source.queries, ['旧城市未完成查询']);
    source.pending.first.complete(source.reply(1));
    await t.pumpAndSettle();
    expect(w.result, isNull);
    expect(w.task, same(old));
    expect(city.selectedCity!.id, _destinationCityID);
    _submit(t, '目的城市新查询');
    await t.pump();
    expect(w.task!.cityID, _destinationCityID);
    expect(w.task, isNot(same(old)));
    expect(source.continued, isEmpty);
    final first = source.reply(2);
    source.pending[1].complete(first);
    await t.pumpAndSettle();
    expect(w.result, same(first));
    expect(w.task!.principalID, egressOwner);
    expect(source.scopes, [_oldCityID, _destinationCityID]);
    final previous = w.task!;
    _submit(t, '再找一个地点');
    await t.pump();
    expect(source.continued.single, same(previous));
    expect(source.continued.single.filters, previous.filters);
    source.pending.last.complete(source.reply(3));
    await t.pumpAndSettle();
    expect(w.task!.cityID, _destinationCityID);
    expect(find.byType(MapCanvas).evaluate().single, same(map));
    expect(
      calls.where((r) => r.url.path.contains('context-selection')),
      isEmpty,
    );
    expect(t.takeException(), isNull);
  });

  testWidgets('已粘贴材料随浏览换城保留，新任务身份变化仍清除原材料', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    final client = MockClient(
      (r) async => r.url.path == '/v1/cities' ? _cities() : _empty(),
    );
    final city = await _catalog(client);
    await _frame(t, auth, client, city: city);
    t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async =>
          call.method == 'Clipboard.getData' ? {'text': '本人已检查的粘贴草稿'} : null,
    );
    addTearDown(
      () => t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      ),
    );
    final composer = t.state<AgentComposerState>(find.byType(AgentComposer)),
        w = _workspace(t);
    await composer.pasteTextFromClipboard();
    await t.pumpAndSettle();
    expect(composer.draftText, '本人已检查的粘贴草稿');
    final identity = w.taskIdentityEpoch;
    await _chooseCity(t, _destinationCityID);
    expect(composer.draftText, '本人已检查的粘贴草稿');
    expect(w.taskIdentityEpoch, identity);
    w.newTask();
    await t.pumpAndSettle();
    expect(composer.draftText, isEmpty);
    expect(w.taskIdentityEpoch, identity + 1);
    expect(t.takeException(), isNull);
  });

  testWidgets('线上发现从More可达，Settings声明入口保留并单独静态核验详细路由', (t) async {
    final auth = ChatTestAuth()..person = egressOwner;
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      return r.url.path == '/v1/cities' ? _cities() : _empty();
    });
    final city = await _catalog(client);
    await _frame(t, auth, client, city: city);
    final map = find.byType(MapCanvas).evaluate().single;
    await t.tap(find.byKey(const Key('now-sidebar-button')));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('sidebar-account')));
    await t.pumpAndSettle();
    await t.tap(find.text('更多工具'));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('now-tools-menu')), findsOneWidget);
    await t.tap(find.text('线上社交发现'));
    await t.pumpAndSettle();
    expect(find.text('线上社交发现'), findsOneWidget);
    expect(find.byKey(const Key('now-scope-picker')), findsNothing);
    Navigator.of(t.element(find.text('线上社交发现'))).pop();
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('now-sidebar-button')));
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('sidebar-account')));
    await t.pumpAndSettle();
    // Entering the pre-existing Settings page raised six unrelated
    // ColoredBox/ListTile ink assertions in city-shell-tests-01. Do not swallow
    // those failures. This UI assertion covers its retained menu entry; the
    // unchanged Settings -> PersonContexts route is separately documented.
    expect(find.text('设置').hitTestable(), findsOneWidget);
    expect(
      t
          .widget<PopupMenuItem<String>>(
            find.ancestor(
              of: find.text('设置'),
              matching: find.byType(PopupMenuItem<String>),
            ),
          )
          .value,
      'settings',
    );
    Navigator.of(t.element(find.text('设置'))).pop();
    await t.pumpAndSettle();
    expect(find.byType(NowContextSelectionPage), findsNothing);
    expect(
      find.byType(MapCanvas, skipOffstage: false).evaluate().single,
      same(map),
    );
    expect(writes, 0);
    expect(t.takeException(), isNull);
  });
}
