import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/entity_peek_card.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'map_layers_api_test.dart' as wire;
import 'now_scope_recovery_test.dart';

void main() {
  testWidgets('到期后保留选中ID但显示更新入口，合法重读才能恢复地点卡', (t) async {
    final fixture = NowFixture();
    addTearDown(fixture.dispose);
    await fixture.mount(t);
    final canvasFinder = find.byType(MapCanvas);
    final canvas = t.widget<MapCanvas>(canvasFinder);
    final element = canvasFinder.evaluate().single;
    final layers = canvas.layers!;
    final workspace = canvas.workspace;
    final reads = <http.Request>[];
    var includePlace = true;
    final api = MapLayersApi(
      client: MockClient((r) async {
        reads.add(r);
        final view = wire.mapTestView(milliseconds: 3000)
          ..['cityId'] = 'alpha'
          ..['items'] = [if (includePlace) wire.mapTestItem('PLACE')];
        return http.Response(jsonEncode({'data': view}), 200,
            headers: {'content-type': 'application/json; charset=utf-8'});
      }),
      apiBaseUrl: fixture.base,
    );
    layers.retire(api: api);
    await layers.load('alpha', wire.mapTestBounds);
    final selectedID = layers.items.single.mapID;
    workspace.selectEntity(selectedID);
    await t.pumpAndSettle();
    expect(find.byType(EntityPeekCard), findsOneWidget);
    expect(find.byKey(const Key('now-map-source-recovery')), findsNothing);
    workspace.setSheetExtent(AgentSheetExtent.expanded);
    await t.pumpAndSettle();
    expect(find.byType(EntityPeekCard), findsNothing);
    expect(find.byKey(const Key('now-native-context-scroll')), findsNothing);
    workspace.setSheetExtent(AgentSheetExtent.peek);
    await t.pumpAndSettle();
    final navigator = Navigator.of(t.element(find.byType(MapWorkspace)));
    navigator.push(MaterialPageRoute<void>(
        builder: (_) => const Scaffold(body: Text('真实覆盖路由测试'))));
    await t.pumpAndSettle();
    await t.pump(const Duration(seconds: 4));
    expect(layers.items, isEmpty);
    expect(layers.error, contains('到期'));
    expect(workspace.selectedEntityId, selectedID);
    navigator.pop();
    await t.pumpAndSettle();
    expect(find.byType(EntityPeekCard), findsNothing);
    expect(find.byKey(const Key('now-map-source-recovery')), findsOneWidget);
    expect(find.text('更新地图点位').hitTestable(), findsOneWidget);
    expect(reads, hasLength(1));
    await t.tap(find.text('更新地图点位'));
    await t.pumpAndSettle();
    expect(reads, hasLength(2));
    expect(reads.every((r) => r.method == 'GET'), true);
    expect(reads.last.url.queryParameters, reads.first.url.queryParameters);
    expect(layers.items.single.mapID, selectedID);
    expect(workspace.selectedEntityId, selectedID);
    expect(find.byType(EntityPeekCard), findsOneWidget);
    expect(find.byKey(const Key('now-map-source-recovery')), findsNothing);
    expect(identical(element, canvasFinder.evaluate().single), true);
    includePlace = false;
    await layers.load('alpha', wire.mapTestBounds);
    await t.pumpAndSettle();
    expect(reads, hasLength(3));
    expect(layers.items, isEmpty);
    expect(layers.error, isNull);
    expect(workspace.selectedEntityId, selectedID);
    expect(find.byType(EntityPeekCard), findsNothing);
    expect(find.byKey(const Key('now-map-source-recovery')), findsNothing);
    expect(find.byKey(const Key('now-native-context-scroll')), findsOneWidget);
    expect(t.takeException(), isNull);
    await fixture.unmount(t);
  });
}
