import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'map_layers_api_test.dart' as f;

const a = '33333333-3333-4333-8333-333333333333';
const b = '44444444-4444-4444-8444-444444444444';

class _City extends PublicCityController {
  @override
  PublicCity? get selectedCity => const PublicCity(
    id: 'owned-city',
    name: '合成城市',
    region: '合成',
    contentStatus: 'building',
    source: PublicSource(
      label: '合成',
      maintainer: '合成',
      freshness: 'test',
      updatedAt: null,
    ),
    map: null,
  );
}

class _Scope {
  final city = _City();
  final ws = AgentWorkspaceController();
  final viewport = MapViewportState();
  final discovery = NowDiscoveryController(authorizationHeader: () => null);
  late final layers = MapLayersController(
    api: MapLayersApi(
      apiBaseUrl: 'http://owned.test',
      client: MockClient((r) async {
        final author = r.url.queryParameters['authorId'];
        if (author != null && failure) return http.Response('{}', 503);
        final wire = f.mapTestView(
          milliseconds: author == null ? 120000 : lease,
          observedAt: fixedObservedAt,
        );
        if (author != null) {
          wire['schemaVersion'] = 'typed-map-layers-v2';
          wire['scope'] = 'PUBLIC_AUTHOR';
          wire['authorId'] = author;
          wire['items'] = [f.mapTestItem('MOMENT')];
        }
        return http.Response(
          jsonEncode({'data': wire}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }),
    ),
    authorizationHeader: () => null,
    organizationWorkspaceID: () => null,
  );
  bool failure = false;
  int lease = 120000;
  String? fixedObservedAt;
  PublicCityMapView view(WidgetTester t) =>
      t.widget(find.byType(PublicCityMapView));
  Future<void> mount(WidgetTester t) async {
    await layers.load('owned-city', f.mapTestBounds);
    viewport.initializeViewport(f.mapTestBounds);
    await t.pumpWidget(
      MaterialApp(
        home: MapCanvas(
          city: city,
          workspace: ws,
          mapState: viewport,
          discovery: discovery,
          layers: layers,
          onInitialViewport: (_) {},
          onMapUnavailable: (_) {},
        ),
      ),
    );
    await t.pump();
  }

  Future<void> author(WidgetTester t, String id) async {
    ws.selectEntity(null, preserveViewport: true);
    await layers.loadAuthor('owned-city', f.mapTestBounds, id);
    await t.pump();
  }

  Future<void> close(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    layers.dispose();
    discovery.dispose();
    viewport.dispose();
    ws.dispose();
    city.dispose();
  }
}

void main() {
  testWidgets('作者列表和原地图共享同一实体，返回保留地图与视野对象', (t) async {
    final s = _Scope();
    await s.mount(t);
    final element = find.byType(PublicCityMapView).evaluate().single;
    final state = t.state(find.byType(MapCanvas));
    final base = s.layers.publicView;
    final epoch = s.ws.taskEpoch;
    final history = List.of(s.ws.conversation);
    await s.author(t, a);
    final view = s.view(t);
    expect(view.entities.map((i) => i.id), s.layers.items.map((i) => i.mapID));
    expect(view.entities.length, 1);
    expect(view.preserveViewport, isTrue);
    view.onEntitySelected!(view.entities.single);
    await t.pump();
    expect(s.ws.selectedEntityId, s.layers.items.single.mapID);
    expect(s.ws.preserveReplyCamera, isTrue);
    s.layers.leaveAuthor();
    await t.pump();
    expect(s.view(t).entities.length, 5);
    expect(s.layers.publicView, same(base));
    expect(find.byType(PublicCityMapView).evaluate().single, same(element));
    expect(t.state(find.byType(MapCanvas)), same(state));
    expect(s.viewport.viewportBounds, f.mapTestBounds);
    expect(s.ws.taskEpoch, epoch);
    expect(s.ws.task, isNull);
    expect(s.ws.conversation, history);
    await s.close(t);
  });
  testWidgets('旧城市callback不能借作者范围相同Moment ID选择', (t) async {
    final s = _Scope();
    await s.mount(t);
    final old = s.view(t),
        pin = s.view(t).entities.firstWhere((e) => e.id.startsWith('moment:'));
    await s.author(t, a);
    old.onEntitySelected!(pin);
    await t.pump();
    expect(s.ws.selectedEntityId, isNull);
    await s.close(t);
  });
  testWidgets('切换作者后相同ID的旧callback不能选择', (t) async {
    final s = _Scope();
    await s.mount(t);
    await s.author(t, a);
    final old = s.view(t), pin = s.view(t).entities.single;
    await s.author(t, b);
    old.onEntitySelected!(pin);
    await t.pump();
    expect(s.ws.selectedEntityId, isNull);
    final fresh = s.view(t);
    fresh.onEntitySelected!(fresh.entities.single);
    await t.pump();
    expect(s.ws.selectedEntityId, fresh.entities.single.id);
    await s.close(t);
  });
  testWidgets('同作者重读后旧cluster callback不能借新lease', (t) async {
    final s = _Scope();
    await s.mount(t);
    await s.author(t, a);
    final old = s.view(t), pin = s.view(t).entities.single;
    await s.author(t, a);
    old.onEntitySelected!(pin);
    await t.pump();
    expect(s.ws.selectedEntityId, isNull);
    await s.close(t);
  });
  testWidgets('deadline和点位完全相同的作者重读，旧callback拒绝但当前点位可点', (t) async {
    final s = _Scope()
      ..fixedObservedAt = DateTime.now().toUtc().toIso8601String();
    await s.mount(t);
    await s.author(t, a);
    final old = s.view(t), pin = s.view(t).entities.single;
    final oldSource = s.layers.authorView!;
    final element = find.byType(PublicCityMapView).evaluate().single;
    await s.author(t, a);
    expect(identical(s.layers.authorView, oldSource), isFalse);
    expect(s.layers.authorView!.validUntil, oldSource.validUntil);
    old.onEntitySelected!(pin);
    await t.pump();
    expect(s.ws.selectedEntityId, isNull);
    final current = s.view(t);
    current.onEntitySelected!(current.entities.single);
    await t.pump();
    expect(s.ws.selectedEntityId, current.entities.single.id);
    expect(find.byType(PublicCityMapView).evaluate().single, same(element));
    expect(s.viewport.viewportBounds, f.mapTestBounds);
    expect(s.ws.task, isNull);
    await s.close(t);
  });
  testWidgets('作者来源过期后原地图为空，旧callback不恢复卡片', (t) async {
    final s = _Scope()..lease = 60;
    await s.mount(t);
    await s.author(t, a);
    final old = s.view(t), pin = s.view(t).entities.single;
    await t.pump(const Duration(milliseconds: 100));
    expect(s.view(t).entities, isEmpty);
    expect(s.layers.baseItems, isNotEmpty);
    old.onEntitySelected!(pin);
    await t.pump();
    expect(s.ws.selectedEntityId, isNull);
    await s.close(t);
  });
  testWidgets('作者请求失败地图为空，明确返回才恢复仍有效原城市', (t) async {
    final s = _Scope()..failure = true;
    await s.mount(t);
    await s.author(t, a);
    expect(s.view(t).entities, isEmpty);
    expect(s.layers.authorError, isNotNull);
    s.layers.leaveAuthor();
    await t.pump();
    expect(s.view(t).entities.length, 5);
    await s.close(t);
  });
}
