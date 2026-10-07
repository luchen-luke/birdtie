import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/entity_peek_card.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'now_scope_recovery_test.dart';

Map<String, dynamic> catalogCity(String id) => {
  'id': id,
  'name': '合成目录$id',
  'region': '组件测试',
  'contentStatus': 'test',
  'source': {'label': '合成API', 'maintainer': 'fixture', 'freshness': 'test'},
  'map': null,
};
http.Response cityReply(Object data, {int status = 200}) => http.Response(
  jsonEncode({'data': data}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  testWidgets('缺城市输入时收起中央恢复，顶部选城与暂停后恢复保留地图草稿', (t) async {
    t.view.physicalSize = const Size(1220, 2656);
    t.view.devicePixelRatio = 3.25;
    t.view.padding = const FakeViewPadding(top: 150, bottom: 52);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    addTearDown(t.view.resetPadding);
    addTearDown(t.view.resetViewInsets);
    var mapBuildCount = 0;
    final oldRebuild = debugOnRebuildDirtyWidget;
    debugOnRebuildDirtyWidget = (element, builtOnce) {
      oldRebuild?.call(element, builtOnce);
      if (element.widget is PublicCityMapView) mapBuildCount++;
    };
    addTearDown(() => debugOnRebuildDirtyWidget = oldRebuild);
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    final map = find.byType(PublicCityMapView);
    final element = map.evaluate().single, state = t.state(map);
    final mapWidget = t.widget<PublicCityMapView>(map);
    final idleBuildCount = mapBuildCount;
    expect(idleBuildCount, greaterThan(0));
    expect(find.text('选择城市').hitTestable(), findsOneWidget);
    expect(find.text('当前：未选城市').hitTestable(), findsOneWidget);
    expect(f.workspace(t).task, isNull);
    await nowTap(t, nowField());
    expect(t.testTextInput.isVisible, true);
    expect(f.workspace(t).inputFocused, true);
    t.view.viewInsets = const FakeViewPadding(bottom: 1056);
    await t.pumpAndSettle();
    final oldCTA = find.text('选择城市');
    final composerRect = t.getRect(find.byType(AgentComposer));
    final ctaRect = oldCTA.evaluate().isEmpty ? null : t.getRect(oldCTA);
    debugPrintSynchronously((jsonEncode({
      'cityRecoveryStep': 'actual-field-and-ime',
      'centralCTA': oldCTA.evaluate().length,
      'composerRect': [composerRect.left, composerRect.top, composerRect.right, composerRect.bottom],
      'ctaRect': ctaRect == null ? null : [ctaRect.left, ctaRect.top, ctaRect.right, ctaRect.bottom],
      'ctaOverlapsComposer': ctaRect?.overlaps(composerRect),
      'mapBuildCount': mapBuildCount,
      'idleBuildCount': idleBuildCount,
      'ime': t.testTextInput.isVisible,
    })).toString());
    expect(find.text('选择城市'), findsNothing);
    expect(find.text('选择城市，查看公开活动和地点。'), findsNothing);
    for (final inset in [980.0, 1012.0, 1056.0]) {
      t.view.viewInsets = FakeViewPadding(bottom: inset);
      await t.enterText(nowField(), '本人未发送的公开地点草稿$inset');
      await t.pumpAndSettle();
      expect(f.workspace(t).inputFocused, true);
      expect(find.text('选择城市'), findsNothing);
      expect(mapBuildCount, idleBuildCount);
      expect(identical(element, map.evaluate().single), true);
      expect(identical(state, t.state(map)), true);
      expect(identical(mapWidget, t.widget<PublicCityMapView>(map)), true);
      expect(f.source.queries, isEmpty);
    }
    final draft = t.widget<TextField>(nowField()).controller!.value;
    expect(find.text('当前：未选城市').hitTestable(), findsOneWidget);
    await nowTap(t, find.text('当前：未选城市'));
    expect(t.testTextInput.isVisible, false);
    expect(f.workspace(t).inputFocused, false);
    t.view.viewInsets = const FakeViewPadding();
    await t.pumpAndSettle();
    expect(find.byType(BottomSheet), findsOneWidget);
    expect(find.byTooltip('取消选择城市').hitTestable(), findsOneWidget);
    expect(f.source.queries, isEmpty);
    await nowTap(t, find.byTooltip('取消选择城市'));
    expect(find.byType(BottomSheet), findsNothing);
    expect(find.text('选择城市').hitTestable(), findsOneWidget);
    expect(find.text('选择城市，查看公开活动和地点。'), findsOneWidget);
    expect(f.city.selectedCity, isNull);
    expect(f.workspace(t).task, isNull);
    expect(t.widget<TextField>(nowField()).controller!.value, draft);
    expect(mapBuildCount, idleBuildCount);
    expect(identical(element, map.evaluate().single), true);
    expect(identical(state, t.state(map)), true);
    expect(identical(mapWidget, t.widget<PublicCityMapView>(map)), true);
    expect(f.source.queries, isEmpty);
    expect(t.takeException(), isNull);
    debugPrintSynchronously((jsonEncode({'cityRecoveryStep': 'actual-top-city-and-cancel-restores', 'mapBuildCount': mapBuildCount, 'idleBuildCount': idleBuildCount, 'ime': t.testTextInput.isVisible, 'queries': f.source.queries.length})).toString());
    await f.unmount(t);
  });
  testWidgets('真实未选城市原Now不常驻附近活动loading且选城恢复仍可达', (t) async {
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t);
    final mapElement = find.byType(MapCanvas).evaluate().single;
    expect(f.city.selectedCity, isNull);
    expect(find.byType(AreaPulseStack), findsNothing);
    expect(find.text('正在查看附近活动…'), findsNothing);
    expect(find.text('选择城市').hitTestable(), findsOneWidget);
    await nowTap(t, find.text('选择城市'));
    await nowTap(t, find.text('甲验收城市'));
    expect(f.city.selectedCity?.id, 'alpha');
    expect(
      identical(mapElement, find.byType(MapCanvas).evaluate().single),
      true,
    );
    await f.unmount(t);
  });
  testWidgets('原Now已选范围无viewport不假loading，有真实范围结果和失败各自表达', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
    canvas.mapState.reset();
    canvas.discovery.clear();
    await t.pumpAndSettle();
    expect(find.byType(AreaPulseStack), findsNothing);
    const bounds = MapBounds(west: -2.2, south: 57.1, east: -2.1, north: 57.2);
    canvas.mapState.initializeViewport(bounds);
    canvas.discovery.loading = true;
    canvas.discovery.notifyListeners();
    await t.pumpAndSettle();
    expect(find.text('正在读取这片区域的活动…'), findsOneWidget);
    expect(find.text('当前地图范围内暂无公开活动'), findsNothing);
    // Explicit synthetic model state control, not native/network success.
    canvas.discovery.pulse = NowPulse.fromJson({
      'cityId': 'alpha',
      'bounds': [-2.2, 57.1, -2.1, 57.2],
      'status': 'empty',
      'total': 0,
      'categories': [],
      'activities': [],
      'truncated': false,
    });
    canvas.discovery.loading = false;
    canvas.discovery.notifyListeners();
    await t.pumpAndSettle();
    expect(find.text('当前地图范围内暂无公开活动'), findsOneWidget);
    expect(find.text('正在查看附近活动…'), findsNothing);
    canvas.discovery.error = '合成活动请求失败';
    canvas.discovery.notifyListeners();
    await t.pumpAndSettle();
    expect(find.text('合成活动请求失败'), findsOneWidget);
    expect(find.text('当前地图范围内暂无公开活动'), findsNothing);
    await f.unmount(t);
  });
  testWidgets('原Now地图明确失败保留城市公开活动fallback而不称附近定位', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
    canvas.mapState.mapUnavailable('合成底图失败');
    canvas.discovery.cityError = '合成城市活动读取失败';
    canvas.discovery.notifyListeners();
    await t.pumpAndSettle();
    expect(find.text('当前城市近期活动'), findsOneWidget);
    expect(find.text('合成城市活动读取失败'), findsOneWidget);
    expect(find.text('正在查看附近活动…'), findsNothing);
    await f.unmount(t);
  });
  testWidgets('真实目录503的地图空态有恢复入口，重读成功后明确选城', (t) async {
    var reads = 0;
    final client = MockClient((r) async {
      if (r.url.path != '/v1/cities') return cityReply([]);
      reads++;
      return reads == 1
          ? cityReply({}, status: 503)
          : cityReply([catalogCity('alpha')]);
    });
    final city = PublicCityController(
      client: client,
      apiBaseUrl: 'http://catalog-fixture.test',
    );
    addTearDown(city.dispose);
    addTearDown(client.close);
    await city.loadCities();
    expect(city.cityError, isNotNull);
    final f = NowFixture();
    addTearDown(f.dispose);
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: f.auth,
          moments: f.moments,
          seedClient: f.client,
          seedApiBaseUrl: 'http://fixture.test',
          agentTaskSource: f.source,
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('选择城市'), findsOneWidget);
    await nowTap(t, find.text('选择城市'));
    await nowTap(t, find.text('合成目录alpha'));
    expect(city.selectedCity?.id, 'alpha');
    expect(reads, 2);
    await f.unmount(t);
  });
  test('真实原生目录首次读取不擅自选择第一城市', () async {
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      return cityReply(
        r.url.path == '/v1/cities'
            ? [catalogCity('alpha'), catalogCity('beta')]
            : [],
      );
    });
    final city = PublicCityController(
      client: client,
      apiBaseUrl: 'http://catalog-fixture.test',
    );
    await city.loadCities();
    expect(requests.where((r) => r.url.path == '/v1/cities').length, 1);
    expect(
      requests.every((r) => !r.headers.containsKey('Authorization')),
      true,
    );
    expect(city.cities.map((c) => c.id), ['alpha', 'beta']);
    expect(city.selectedCity, isNull, reason: '公开浏览范围需本人选择，不代表GPS');
    city.dispose();
    client.close();
  });
  test('真实目录失败再读取成功保留显式城市，不因为排序改变而换范围', () async {
    var reads = 0;
    final client = MockClient((r) async {
      if (r.url.path != '/v1/cities') return cityReply([]);
      reads++;
      return reads == 1
          ? cityReply({}, status: 503)
          : cityReply([catalogCity('beta'), catalogCity('alpha')]);
    });
    final city = PublicCityController(
      client: client,
      apiBaseUrl: 'http://catalog-fixture.test',
    );
    await city.loadCities();
    expect(city.cityError, isNotNull);
    await city.loadCities();
    expect(city.cityError, isNull);
    city.selectCity('alpha');
    await city.loadCities();
    expect(city.selectedCity?.id, 'alpha');
    expect(reads, 3);
    city.dispose();
    client.close();
  });
}
