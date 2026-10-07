import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/entity_peek_card.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_bitmap.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'map_layers_api_test.dart' as layers;
import 'agent_result_projection_test.dart' as projection;

// Deliberately synthetic widget transport. The actual native Mapbox instance,
// annotation calls, camera pixels and frame stalls require ROOT device evidence.
class _Auth extends BirdtieAuthController {
  String person = layers.mapTestID;
  @override
  bool get signedIn => true;
  @override
  String? get accountID => person;
  @override
  String? get authorizationHeader => 'Bearer fixed-test-session';
  void use(String id) {
    person = id;
    notifyListeners();
  }
}

class _City extends PublicCityController {
  String current = 'stress-city';
  @override
  PublicCity? get selectedCity => PublicCity(
    id: current,
    name: '合成地图压力城市',
    region: 'LOCAL_SYNTHETIC',
    contentStatus: 'building',
    source: const PublicSource(
      label: '合成本地数据',
      maintainer: '测试',
      freshness: 'test',
      updatedAt: null,
    ),
    map: null,
  );
  @override
  List<PublicCity> get cities => [selectedCity!];
  @override
  void selectCity(String id) {
    current = id;
    notifyListeners();
  }
}

class _Client extends MockClient {
  _Client(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

Map<String, dynamic> _wire({
  String city = 'stress-city',
  String label = '当前公开来源',
  bool private = false,
}) {
  final wire = layers.mapTestView(private: private)..['cityId'] = city;
  for (final item in wire['items'] as List) {
    item['title'] = label;
  }
  return wire;
}

http.Response _reply(Map<String, dynamic> body) => http.Response(
  jsonEncode({'data': body}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

class _Harness {
  final auth = _Auth(), city = _City();
  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
  );
  final requests = <http.Request>[];
  late final _Client client;
  _Harness({Future<http.Response> Function(http.Request)? reader}) {
    client = _Client((r) async {
      requests.add(r);
      if (reader != null) return reader(r);
      if (r.url.path.endsWith('/map-layers') ||
          r.url.path.endsWith('/map-opportunities')) {
        return _reply(
          _wire(
            city: r.url.pathSegments[r.url.pathSegments.length - 2],
            private: r.url.path.endsWith('/map-opportunities'),
          ),
        );
      }
      return http.Response('{}', 403);
    });
  }
  Future<MapCanvas> show(
    WidgetTester t, {
    double scale = 1,
    String base = 'https://owned.test',
  }) async {
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: TextScaler.linear(scale)),
          child: child!,
        ),
        home: MapWorkspace(
          key: const ValueKey('map-runtime-current'),
          city: city,
          auth: auth,
          moments: moments,
          seedClient: client,
          seedApiBaseUrl: base,
        ),
      ),
    );
    await t.pumpAndSettle();
    return t.widget<MapCanvas>(find.byType(MapCanvas));
  }

  int get mapReads => requests
      .where(
        (r) =>
            r.url.path.contains('/map-layers') ||
            r.url.path.contains('/map-opportunities'),
      )
      .length;
  Future<void> close(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    auth.dispose();
    city.dispose();
    expect(client.closes, 0);
    client.close();
  }
}

AgentResult _typedResult(String title, {bool anchored = true}) {
  final item = AgentResultItem.decode(
    projection.typedItem(
      'activity',
      'runtime-activity',
      anchor: anchored
          ? {
              'coordinateSystem': 'wgs84',
              'precision': 'point',
              'latitude': 57.2,
              'longitude': -2.1,
            }
          : null,
    )..['title'] = title,
  );
  return AgentResult(
    entities: const [],
    activities: const [],
    places: const [],
    note: '先说明当前实际规则，再给结果',
    resultSet: AgentResultSet(
      id: 'runtime-result-set',
      status: 'COMPLETED',
      entities: [item.entity],
      generatedAt: DateTime.utc(2026, 10, 4),
      schema: typedAgentResultSchema,
      items: [item],
    ),
  );
}

class _PendingArea extends AgentTaskSource {
  final pending = <double, Completer<AgentResult>>{};
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => _typedResult('原活动');
  @override
  Future<AgentResult> searchArea(
    AgentTask? task,
    String query,
    MapBounds bounds,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => (pending[bounds.west] = Completer<AgentResult>()).future;
}

void main() {
  for (final scale in [1.0, 3.0]) {
    testWidgets('真实Now $scale倍字：30轮输入IME开合保留六层投影/选中卡/任务与map Element', (
      t,
    ) async {
      t.view.physicalSize = Size(scale == 1 ? 430 : 320, 900);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetViewInsets);
      final f = _Harness();
      final canvas = await f.show(t, scale: scale);
      canvas.onInitialViewport(layers.mapTestBounds);
      await t.pumpAndSettle();
      canvas.layers!.toggle('OPPORTUNITY', true);
      await t.pumpAndSettle();
      expect(canvas.layers!.items, hasLength(6));
      canvas.workspace.task = const AgentTask(
        id: 'preserved-current-task',
        query: '帮我找周末的羽毛球',
        status: 'COMPLETED',
        cityID: 'stress-city',
      );
      final selected = 'moment:${layers.mapTestID}';
      canvas.workspace.selectEntity(selected);
      await t.pumpAndSettle();
      final mapElement = find.byType(MapCanvas).evaluate().single;
      final nativeElement = find.byType(PublicCityMapView).evaluate().single;
      final nativeWidget = t.widget<PublicCityMapView>(
        find.byType(PublicCityMapView),
      );
      final originalPins = nativeWidget.entities.map((e) => e.id).toSet();
      final reads = f.mapReads;
      for (var cycle = 0; cycle < 30; cycle++) {
        t.view.viewInsets = const FakeViewPadding(bottom: 260);
        await t.showKeyboard(find.byType(TextField).first);
        for (var letter = 1; letter <= 3; letter++) {
          await t.enterText(
            find.byType(TextField).first,
            '连续输入第$cycle轮${'羽毛球' * letter}',
          );
          await t.pump(const Duration(milliseconds: 30));
        }
        FocusManager.instance.primaryFocus?.unfocus();
        t.view.viewInsets = FakeViewPadding.zero;
        await t.pump(const Duration(milliseconds: 300));
        expect(
          identical(mapElement, find.byType(MapCanvas).evaluate().single),
          true,
        );
        expect(
          identical(
            nativeElement,
            find.byType(PublicCityMapView).evaluate().single,
          ),
          true,
        );
        expect(
          identical(
            nativeWidget,
            t.widget<PublicCityMapView>(find.byType(PublicCityMapView)),
          ),
          true,
        );
        expect(
          t
              .widget<PublicCityMapView>(find.byType(PublicCityMapView))
              .entities
              .map((e) => e.id)
              .toSet(),
          originalPins,
        );
        expect(canvas.workspace.selectedEntityId, selected);
        expect(canvas.workspace.task!.id, 'preserved-current-task');
        expect(find.byType(EntityPeekCard), findsOneWidget);
        expect(f.mapReads, reads);
        expect(t.takeException(), isNull);
      }
      await f.close(t);
    });
  }
  testWidgets('60次地图相机回调保留当前Pin/Card/selection；只有显式范围读取才新增GET', (t) async {
    final f = _Harness();
    final c = await f.show(t);
    c.onInitialViewport(layers.mapTestBounds);
    await t.pumpAndSettle();
    c.workspace.selectEntity('moment:${layers.mapTestID}');
    await t.pumpAndSettle();
    final element = find.byType(PublicCityMapView).evaluate().single,
        reads = f.mapReads;
    final ids = c.layers!.items.map((e) => e.mapID).toSet();
    for (var i = 0; i < 60; i++) {
      final map = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      map.onCameraMotion!();
      final bounds = MapBounds(
        west: -2.2 + i * .0001,
        south: 57.1,
        east: -2,
        north: 57.3,
      );
      map.onViewportSettled!(bounds);
      await t.pump();
      expect(c.workspace.selectedEntityId, 'moment:${layers.mapTestID}');
      expect(c.layers!.items.map((e) => e.mapID).toSet(), ids);
      expect(find.byType(EntityPeekCard), findsOneWidget);
      expect(f.mapReads, reads);
      expect(
        identical(element, find.byType(PublicCityMapView).evaluate().single),
        true,
      );
    }
    await t.tap(find.byTooltip('打开更多工具'));
    await t.pumpAndSettle();
    expect(find.byTooltip('地图图层').hitTestable(), findsOneWidget);
    await t.tap(find.byTooltip('地图图层'));
    await t.pumpAndSettle();
    final action = find.text('读取当前地图范围');
    expect(action, findsOneWidget);
    await t.ensureVisible(action);
    await t.tap(action);
    await t.pumpAndSettle();
    expect(f.mapReads, reads + 1);
    expect(c.workspace.selectedEntityId, 'moment:${layers.mapTestID}');
    expect(t.takeException(), isNull);
    await f.close(t);
  });
  testWidgets('十个viewport请求倒序回包原子替换最新Pin和轻卡，旧结果不覆盖', (t) async {
    final pending = <Completer<http.Response>>[];
    final f = _Harness(
      reader: (r) async {
        if (r.url.path.endsWith('/map-layers')) {
          final p = Completer<http.Response>();
          pending.add(p);
          return p.future;
        }
        return http.Response('{}', 403);
      },
    );
    final c = await f.show(t);
    final futures = <Future<void>>[];
    for (var i = 0; i < 10; i++) {
      futures.add(
        c.layers!.load(
          'stress-city',
          MapBounds(west: -2.2 + i * .0001, south: 57.1, east: -2, north: 57.3),
        ),
      );
    }
    await t.pump();
    expect(pending, hasLength(10));
    pending.last.complete(_reply(_wire(label: '最新第9轮来源')));
    await futures.last;
    await t.pumpAndSettle();
    c.workspace.selectEntity('moment:${layers.mapTestID}');
    await t.pumpAndSettle();
    for (var i = 8; i >= 0; i--) {
      pending[i].complete(_reply(_wire(label: '旧来源$i不得恢复')));
      await futures[i];
      await t.pump();
      expect(c.layers!.find('moment:${layers.mapTestID}')!.title, '最新第9轮来源');
      expect(find.textContaining('旧来源'), findsNothing);
    }
    expect(
      t
          .widget<PublicCityMapView>(find.byType(PublicCityMapView))
          .entities
          .every((i) => i.title == '最新第9轮来源'),
      true,
    );
    expect(find.byType(EntityPeekCard), findsOneWidget);
    expect(t.takeException(), isNull);
    await f.close(t);
  });
  testWidgets('同token账号A-B-A退休缓存和挂起PRIVATE来源，不自动重读机会层', (t) async {
    Completer<http.Response>? late;
    var delay = false;
    final f = _Harness(
      reader: (r) async {
        if (r.url.path.endsWith('/map-layers')) return _reply(_wire());
        if (r.url.path.endsWith('/map-opportunities')) {
          if (delay) {
            late = Completer<http.Response>();
            return late!.future;
          }
          return _reply(_wire(private: true));
        }
        return http.Response('{}', 403);
      },
    );
    final c = await f.show(t);
    c.onInitialViewport(layers.mapTestBounds);
    await t.pumpAndSettle();
    c.layers!.toggle('OPPORTUNITY', true);
    await t.pumpAndSettle();
    expect(c.layers!.privateView, isNotNull);
    delay = true;
    final pending = c.layers!.load('stress-city', layers.mapTestBounds);
    await t.pump();
    expect(late, isNotNull);
    final reads = f.mapReads;
    f.auth.use(layers.mapTestPlace);
    f.auth.use(layers.mapTestID);
    await t.pump();
    late!.complete(_reply(_wire(private: true, label: '旧本人私人结果')));
    await pending;
    await t.pumpAndSettle();
    expect(c.layers!.items, isEmpty);
    expect(c.layers!.privateView, isNull);
    expect(c.layers!.visible, isNot(contains('OPPORTUNITY')));
    expect(f.mapReads, reads);
    expect(find.text('旧本人私人结果'), findsNothing);
    await f.close(t);
  });
  testWidgets('同key endpoint A-B-A的迟到层来源不复活，借用client不被关闭', (t) async {
    final pending = Completer<http.Response>();
    final f = _Harness(
      reader: (r) async => r.url.path.endsWith('/map-layers')
          ? pending.future
          : http.Response('{}', 403),
    );
    final c = await f.show(t, base: 'https://old.test');
    final element = find.byType(MapCanvas).evaluate().single;
    final read = c.layers!.load('stress-city', layers.mapTestBounds);
    await t.pump();
    await f.show(t, base: 'https://new.test');
    await f.show(t, base: 'https://old.test');
    pending.complete(_reply(_wire(label: '旧端点私密内容')));
    await read;
    await t.pumpAndSettle();
    expect(c.layers!.items, isEmpty);
    expect(identical(element, find.byType(MapCanvas).evaluate().single), true);
    expect(f.client.closes, 0);
    expect(find.text('旧端点私密内容'), findsNothing);
    await f.close(t);
  });
  testWidgets('新typed ResultSet同原实体选中；ONLINE与拖动不造点或替换CITY投影', (t) async {
    final f = _Harness();
    final c = await f.show(t);
    c.workspace.task = const AgentTask(
      id: 'typed-city-task',
      query: '原查询',
      status: 'COMPLETED',
      cityID: 'stress-city',
    );
    c.workspace.result = _typedResult('具有明确公开锚点');
    c.workspace.selectEntity('activity:runtime-activity');
    await t.pumpAndSettle();
    final initial = t.widget<PublicCityMapView>(find.byType(PublicCityMapView)),
        element = find.byType(PublicCityMapView).evaluate().single;
    expect(initial.entities.single.id, 'activity:runtime-activity');
    expect(find.byType(EntityPeekCard), findsOneWidget);
    c.workspace.queryContextType = 'ONLINE';
    c.workspace.newTask(preserveMapSelection: true);
    c.workspace.task = const AgentTask(
      id: 'online-task',
      query: '无地图线上',
      status: 'COMPLETED',
      contextType: 'ONLINE',
    );
    c.workspace.result = _typedResult('没有点位的线上内容', anchored: false);
    c.workspace.setSheetExtent(AgentSheetExtent.medium);
    await t.pumpAndSettle();
    for (var i = 0; i < 20; i++) {
      final p = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      p.onCameraMotion!();
      p.onViewportSettled!(layers.mapTestBounds);
      await t.pump();
      expect(p.entities.single.id, 'activity:runtime-activity');
      expect(p.contextKey, initial.contextKey);
      expect(c.workspace.selectedEntityId, 'activity:runtime-activity');
      expect(
        t.widget<EntityPeekCard>(find.byType(EntityPeekCard)).entity.id,
        'activity:runtime-activity',
      );
      expect(
        identical(element, find.byType(PublicCityMapView).evaluate().single),
        true,
      );
    }
    expect(
      t
          .widget<PublicCityMapView>(find.byType(PublicCityMapView))
          .entities
          .single
          .id,
      initial.entities.single.id,
    );
    f.city.selectCity('different-city');
    await t.pumpAndSettle();
    expect(
      t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
      isEmpty,
    );
    expect(find.byType(EntityPeekCard), findsNothing);
    expect(c.workspace.task!.id, 'online-task');
    expect(t.takeException(), isNull);
    await f.close(t);
  });
  testWidgets('城市A-B-A退休挂起旧viewport，旧City同ID回包不能恢复Pin或轻卡', (t) async {
    final late = Completer<http.Response>();
    final f = _Harness(
      reader: (r) async => r.url.path.endsWith('/map-layers')
          ? late.future
          : http.Response('{}', 403),
    );
    final c = await f.show(t);
    final read = c.layers!.load('stress-city', layers.mapTestBounds);
    await t.pump();
    final before = f.mapReads;
    f.city.selectCity('different-city');
    await t.pump();
    f.city.selectCity('stress-city');
    await t.pump();
    late.complete(_reply(_wire(label: '旧城市视口来源')));
    await read;
    await t.pumpAndSettle();
    expect(c.layers!.items, isEmpty);
    expect(f.mapReads, before);
    expect(
      t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
      isEmpty,
    );
    expect(find.byType(EntityPeekCard), findsNothing);
    expect(find.text('旧城市视口来源'), findsNothing);
    expect(t.takeException(), isNull);
    await f.close(t);
  });
  test('原Agent范围请求20轮反序回包保持最新typed ResultSet；旧回包不复活已退休selection', () async {
    final source = _PendingArea(), w = AgentWorkspaceController(source: source);
    await w.submit('原查询', [], [], cityID: 'stress-city');
    w.selectEntity('activity:runtime-activity');
    final futures = <Future<void>>[];
    for (var i = 0; i < 20; i++) {
      futures.add(
        w.searchThisArea(
          [],
          [],
          bounds: MapBounds(
            west: -2.2 + i * .0001,
            south: 57.1,
            east: -2,
            north: 57.3,
          ),
          cityID: 'stress-city',
        ),
      );
      expect(w.selectedEntityId, 'activity:runtime-activity');
    }
    for (var i = 19; i >= 0; i--) {
      source.pending[-2.2 + i * .0001]!.complete(_typedResult('第$i轮范围'));
      await futures[i];
      expect(w.result!.projectionItems!.single.title, '第19轮范围');
      expect(w.selectedEntityId, isNull);
    }
    w.dispose();
  });
  test('六类100轮marker bitmap复用缓存，选中ID不入cluster且未选聚合ID稳定', () async {
    final bytes = <MapEntityKind, Object>{};
    final entities = <MapEntity>[];
    for (final kind in [
      MapEntityKind.place,
      MapEntityKind.activity,
      MapEntityKind.moment,
      MapEntityKind.organization,
      MapEntityKind.business,
      MapEntityKind.opportunity,
    ]) {
      bytes[kind] = await MapMarkerBitmap.render(kind: kind, selected: false);
      entities.add(
        MapEntity(
          id: '${kind.name}:original',
          kind: kind,
          title: '明确合成公开锚点',
          subtitle: '非实际用户位置',
          latitude: 57.2,
          longitude: -2.1,
        ),
      );
    }
    const selected = 'moment:original';
    final first = clusterMapEntities(
      entities,
      zoom: 10,
      selectedId: selected,
    ).map((i) => i.id).toSet();
    for (var i = 0; i < 100; i++) {
      for (final kind in bytes.keys) {
        expect(
          identical(
            bytes[kind],
            await MapMarkerBitmap.render(kind: kind, selected: false),
          ),
          true,
        );
      }
      final grouped = clusterMapEntities(
        entities.reversed.toList(),
        zoom: 10,
        selectedId: selected,
      );
      expect(grouped.map((i) => i.id).toSet(), first);
      expect(grouped.where((i) => i.id == selected), hasLength(1));
      expect(
        grouped
            .where((i) => i.isCluster)
            .every((i) => !i.memberIDs.contains(selected)),
        true,
      );
    }
  });
}
