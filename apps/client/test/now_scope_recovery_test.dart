import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;

/// Synthetic catalog/HTTP only. Mounts the unchanged Birdtie MapWorkspace.
class NowFixtureCity extends PublicCityController {
  static const catalog = [
    PublicCity(
      id: 'alpha',
      name: '甲验收城市',
      region: '合成测试',
      contentStatus: 'test',
      map: null,
      source: PublicSource(
        label: '组件夹具',
        maintainer: 'test',
        freshness: 'synthetic',
        updatedAt: null,
      ),
    ),
    PublicCity(
      id: 'beta',
      name: '乙验收城市',
      region: '合成测试',
      contentStatus: 'test',
      map: null,
      source: PublicSource(
        label: '组件夹具',
        maintainer: 'test',
        freshness: 'synthetic',
        updatedAt: null,
      ),
    ),
  ];
  NowFixtureCity({bool selected = true})
    : selection = selected ? catalog[0] : null;
  PublicCity? selection;
  int selections = 0;
  @override
  PublicCity? get selectedCity => selection;
  @override
  List<PublicCity> get cities => catalog;
  @override
  Future<void> loadCities() async {
    notifyListeners();
  }

  @override
  void selectCity(String id) {
    selection = catalog.singleWhere((c) => c.id == id);
    selections++;
    notifyListeners();
  }
}

class NowFixtureSource extends AgentTaskSource {
  NowFixtureSource(this.city);
  final NowFixtureCity city;
  final queries = <String>[];
  final scopes = <String?>[];
  String? failure;
  Completer<AgentResult>? pending;
  final pendingQueries = <String, Completer<AgentResult>>{};
  AgentResult response(String query) => AgentResult(
    entities: const [],
    activities: const [],
    places: const [],
    note: '合成权威响应：$query',
    task: AgentTask(
      id: 'test-${queries.length}',
      query: query,
      status: 'completed',
      cityID: city.selectedCity?.id,
    ),
  );
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    queries.add(query);
    scopes.add(city.selectedCity?.id);
    if (city.selectedCity == null) {
      throw const AgentRequestFailure('请先选择城市，再搜索公开活动。', code: 'NEEDS_CITY');
    }
    if (failure != null) throw AgentRequestFailure(failure!);
    return pendingQueries[query]?.future ?? pending?.future ?? response(query);
  }
}

class NowFixture {
  NowFixture({bool selected = true}) {
    city = NowFixtureCity(selected: selected);
    source = NowFixtureSource(city);
    client = MockClient(
      (r) async => http.Response(
        jsonEncode({'data': []}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    moments = PrivateMomentController(
      client: client,
      apiBaseUrl: 'http://now-fixture.test',
      authorizationHeader: () => auth.authorizationHeader,
    );
  }
  final auth = SeedTestAuth()..token = null;
  late final NowFixtureCity city;
  late final NowFixtureSource source;
  late final MockClient client;
  late final PrivateMomentController moments;
  String base = 'http://now-fixture.test';
  Future<void> mount(
    WidgetTester t, {
    double scale = 1,
    double? keyboardInset,
    double? safeBottom,
  }) async {
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context).copyWith(
            textScaler: TextScaler.linear(scale),
            viewInsets: keyboardInset == null
                ? null
                : EdgeInsets.only(bottom: keyboardInset),
            padding: safeBottom == null
                ? null
                : EdgeInsets.only(bottom: safeBottom, top: 24),
          ),
          child: child!,
        ),
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          agentTaskSource: source,
          seedClient: client,
          seedApiBaseUrl: base,
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  AgentWorkspaceController workspace(WidgetTester t) =>
      t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
  Future<void> unmount(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  }

  void dispose() {
    moments.dispose();
    city.dispose();
    auth.dispose();
    client.close();
  }
}

Finder nowField() => find.descendant(
  of: find.byType(AgentComposer),
  matching: find.byType(TextField),
);
Future<void> nowTap(WidgetTester t, Finder target) async {
  await t.ensureVisible(target);
  await t.pumpAndSettle();
  expect(target.hitTestable(), findsOneWidget);
  await t.tap(target);
  await t.pumpAndSettle();
}

Future<void> nowSend(WidgetTester t, String query) async {
  await t.enterText(nowField(), query);
  await t.testTextInput.receiveAction(TextInputAction.send);
  await t.pumpAndSettle();
}

Future<void> nowBack(WidgetTester t) async {
  await t.binding.handlePopRoute();
  await t.pumpAndSettle();
}

// BT-UXR-007/010: tools are reached from the fixed account menu, not a
// fourth top action. This helper follows the same visible navigation as a user.
Future<void> nowOpenAccountTools(WidgetTester t) async {
  await nowTap(t, find.byTooltip('打开侧边栏'));
  await nowTap(t, find.byKey(const Key('sidebar-account')));
  await nowTap(t, find.text('更多工具'));
}

void main() {
  testWidgets('实际选城模态高度在外层受限且句柄避开系统状态栏', (t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t, safeBottom: 20);
    await nowTap(t, find.text('当前：未选城市'));
    final rect = t.getRect(find.byType(BottomSheet));
    debugPrintSynchronously(
      (jsonEncode({
        'actualSheet': [rect.left, rect.top, rect.right, rect.bottom],
        'viewport': [390, 844],
        'safeTop': 24,
      })).toString(),
    );
    expect(rect.top, greaterThanOrEqualTo(24));
    expect(rect.height, lessThanOrEqualTo(844 * .7 + 48));
    expect(find.byTooltip('取消选择城市与范围').hitTestable(), findsOneWidget);
    expect(f.source.queries, isEmpty);
    await nowBack(t);
    expect(f.city.selectedCity, isNull);
    expect(f.source.queries, isEmpty);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });
  testWidgets('选城明确取消不提交且保留原查询，再选择一次仅恢复一次', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, '找周末的地点');
    final task = f.workspace(t).task;
    await nowTap(t, find.text('选择城市继续'));
    await nowTap(t, find.byTooltip('取消选择城市与范围'));
    expect(find.byType(BottomSheet), findsNothing);
    expect(identical(f.workspace(t).task, task), true);
    expect(f.workspace(t).pendingScopeQuery, '找周末的地点');
    expect(f.source.queries, isEmpty);
    expect(find.text('选择城市继续'), findsOneWidget);
    expect(find.textContaining('0 个'), findsNothing);
    await nowTap(t, find.text('选择城市继续'));
    await nowTap(t, find.text('甲验收城市'));
    expect(f.source.queries, ['找周末的地点']);
    expect(f.source.scopes, ['alpha']);
    expect(f.workspace(t).pendingScopeQuery, isNull);
    await nowTap(t, find.text('当前：甲验收城市'));
    await nowTap(t, find.text('甲验收城市'));
    expect(f.source.queries.length, 1);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });
  testWidgets('缺城市任务只显示当前任务的一个选城恢复入口，地图实例稳定', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    final map = find.byType(PublicCityMapView);
    final element = map.evaluate().single;
    final state = t.state(map);
    // BT-UXR-010/011: the selected-city slot is the only idle city entry.
    expect(find.text('选择城市'), findsNothing);
    expect(
      find.byKey(const Key('now-city-picker')).hitTestable(),
      findsOneWidget,
    );
    expect(find.text('当前：未选城市'), findsOneWidget);
    await nowSend(t, 'weekend');
    final task = f.workspace(t).task;
    expect(f.workspace(t).queryState, AgentQueryState.needsScope);
    expect(f.workspace(t).pendingScopeQuery, 'weekend');
    expect(find.text('选择城市继续'), findsOneWidget);
    expect(find.text('选择城市'), findsNothing);
    expect(find.text('当前：未选城市'), findsOneWidget);
    final unscoped = t.widget<PublicCityMapView>(map);
    expect(unscoped.city, isNull);
    expect(unscoped.entities, isEmpty);
    expect(unscoped.places, isEmpty);
    expect(unscoped.contextKey, 'idle');
    expect(unscoped.onViewportInitialized, isNull);
    expect(unscoped.onViewportSettled, isNull);
    expect(f.source.queries, isEmpty);
    expect(identical(element, map.evaluate().single), true);
    expect(identical(state, t.state(map)), true);
    await nowTap(t, find.text('选择城市继续'));
    expect(find.text('选择城市与范围'), findsOneWidget); // Actual picker only.
    await nowBack(t);
    expect(find.text('选择城市与范围'), findsNothing);
    expect(find.text('选择城市继续'), findsOneWidget);
    expect(identical(f.workspace(t).task, task), true);
    expect(f.source.queries, isEmpty);
    await nowTap(t, find.text('选择城市继续'));
    await nowTap(t, find.text('甲验收城市'));
    expect(f.source.queries, ['weekend']);
    expect(f.source.scopes, ['alpha']);
    expect(f.workspace(t).pendingScopeQuery, isNull);
    expect(t.widget<PublicCityMapView>(map).city?.id, 'alpha');
    expect(identical(element, map.evaluate().single), true);
    expect(identical(state, t.state(map)), true);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });

  testWidgets('真实Now已选城市正常查询只提交一次', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, '找地点');
    expect(f.source.queries, ['找地点']);
    expect(f.workspace(t).result?.responseMessage, '合成权威响应：找地点');
    await f.unmount(t);
  });
  testWidgets('真实Now顶部城市胶囊可直接选择已有目录城市', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    await nowTap(t, find.text('当前：未选城市'));
    expect(
      find.descendant(
        of: find.byType(BottomSheet),
        matching: find.text('选择城市与范围'),
      ),
      findsOneWidget,
    );
    await nowTap(t, find.text('甲验收城市'));
    expect(f.city.selectedCity?.id, 'alpha');
    await f.unmount(t);
  });
  testWidgets('缺城市查询保留原文且提供一个选城恢复入口', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, '找组织');
    expect(f.workspace(t).task?.query, '找组织');
    expect(find.text('选择城市继续'), findsOneWidget);
    await nowTap(t, find.text('选择城市继续'));
    await nowTap(t, find.text('乙验收城市'));
    expect(f.source.queries.where((q) => q == '找组织').length, 1);
    expect(f.source.scopes, ['beta']);
    await f.unmount(t);
  });
  testWidgets('取消选城零请求，真正再次选择仅恢复原查询一次', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, '找周末的组织');
    final original = f.workspace(t).task!.id;
    await nowTap(t, find.text('选择城市继续'));
    await nowBack(t);
    expect(f.source.queries, isEmpty);
    expect(f.workspace(t).task?.id, original);
    expect(f.workspace(t).pendingScopeQuery, '找周末的组织');
    await nowTap(t, find.text('当前：未选城市'));
    await nowTap(t, find.text('甲验收城市'));
    expect(f.source.queries, ['找周末的组织']);
    expect(f.source.scopes, ['alpha']);
    expect(
      f
          .workspace(t)
          .conversation
          .where((m) => m.role == 'user')
          .map((m) => m.text),
      ['找周末的组织'],
    );
    await nowTap(t, find.text('当前：甲验收城市'));
    await nowTap(t, find.text('甲验收城市'));
    expect(f.source.queries.length, 1);
    await f.unmount(t);
  });
  testWidgets('新草稿替换未提交查询，选城不复活上一轮', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, '找组织');
    await nowSend(t, '找地点');
    await nowTap(t, find.text('选择城市继续'));
    await nowTap(t, find.text('乙验收城市'));
    expect(f.source.queries, ['找地点']);
    expect(f.workspace(t).task?.query, '找地点');
    await f.unmount(t);
  });
  for (final change in ['account-aba', 'token-aba', 'source-aba']) {
    testWidgets('选城入口$change永久失效，零旧查询自动恢复', (t) async {
      final f = NowFixture(selected: false);
      addTearDown(f.dispose);
      f.auth.token = 'Bearer test-A';
      f.auth.owner = 'test-owner';
      await f.mount(t);
      await nowSend(t, '找我的旧组织草稿');
      await nowTap(t, find.text('选择城市继续'));
      if (change == 'source-aba') {
        f.base = 'http://now-b.test';
        await f.mount(t);
        f.base = 'http://now-fixture.test';
        await f.mount(t);
      } else {
        f.auth.changeIdentity(
          'Bearer test-B',
          nextOwner: change == 'token-aba' ? 'test-owner' : 'peer',
        );
        await t.pumpAndSettle();
        f.auth.changeIdentity('Bearer test-A', nextOwner: 'test-owner');
        await t.pumpAndSettle();
      }
      expect(f.source.queries, isEmpty);
      expect(f.workspace(t).pendingScopeQuery, isNull);
      expect(find.text('账号或工作身份已变化，请关闭后重新选择。'), findsOneWidget);
      await nowBack(t);
      await nowTap(t, find.text('当前：未选城市'));
      await nowTap(t, find.text('甲验收城市'));
      expect(f.source.queries, isEmpty);
      await nowSend(t, '找当前地点');
      expect(f.source.queries, ['找当前地点']);
      await f.unmount(t);
    });
  }
  testWidgets('外部城市A-B-A事件不能重用旧待选范围批准', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, '旧查询');
    await nowTap(t, find.text('选择城市继续'));
    f.city.selectCity('alpha');
    f.city.selectCity('beta');
    f.city.selectCity('alpha');
    await t.pumpAndSettle();
    expect(f.workspace(t).pendingScopeQuery, isNull);
    expect(f.source.queries, isEmpty);
    await nowBack(t);
    await nowSend(t, '新查询');
    expect(f.source.queries, ['新查询']);
    expect(f.source.scopes, ['alpha']);
    await f.unmount(t);
  });
  testWidgets('三轮真实提交迟到响应不覆盖最新查询、城市或错误状态', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final a = Completer<AgentResult>(),
        b = Completer<AgentResult>(),
        c = Completer<AgentResult>();
    f.source.pendingQueries.addAll({'A': a, 'B': b, 'C': c});
    Future<void> send(String text) async {
      await t.enterText(nowField(), text);
      await t.testTextInput.receiveAction(TextInputAction.send);
      await t.pump();
    }

    await send('A');
    await send('B');
    await send('C');
    expect(f.workspace(t).queryState, AgentQueryState.loading);
    c.complete(f.source.response('C'));
    await t.pumpAndSettle();
    a.complete(f.source.response('A'));
    b.completeError(const AgentRequestFailure('旧B错误'));
    await t.pumpAndSettle();
    expect(f.source.queries, ['A', 'B', 'C']);
    expect(f.workspace(t).task?.query, 'C');
    expect(f.workspace(t).result?.responseMessage, '合成权威响应：C');
    expect(f.workspace(t).requestError, isNull);
    expect(find.text('旧B错误'), findsNothing);
    expect(find.byType(AgentResultsSheet), findsOneWidget);
    await f.unmount(t);
  });
}
