import 'package:birdtie_client/src/workspace/map_layer_controls.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  for (final ready in [false, true]) {
    testWidgets('真实图层${ready ? '待加载' : '缺范围'}不冒充已读取零内容', (t) async {
      final controller = MapLayersController(
        api: MapLayersApi(apiBaseUrl: 'http://fixture.test'),
        authorizationHeader: () => null,
        organizationWorkspaceID: () => null,
      );
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MapLayerControls(
              controller: controller,
              current: () => true,
              onRefresh: ready ? () {} : null,
              onOpen: (_) {},
            ),
          ),
        ),
      );
      expect(find.text('当前图层没有符合公开点位条件的内容。'), findsNothing);
      if (!ready) expect(find.text('地图范围暂未就绪，请先选择有地图配置的城市。'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      controller.dispose();
    });
  }
}
