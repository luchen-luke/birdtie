import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/map_layer_controls.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'map_layers_api_test.dart' as f;

void main() {
  testWidgets(
    'Chinese six controls narrow font3 scroll reach original card no overflow',
    (tester) async {
      tester.view.physicalSize = const Size(320, 640);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final c = MapLayersController(
        api: MapLayersApi(
          client: MockClient(
            (r) async => http.Response(
              jsonEncode({'data': f.mapTestView()}),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            ),
          ),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => null,
        organizationWorkspaceID: () => null,
      );
      await c.load('owned-city', f.mapTestBounds);
      final opened = <String>[];
      await tester.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(textScaler: TextScaler.linear(3)),
            child: Scaffold(
              body: MapLayerControls(
                controller: c,
                current: () => true,
                onRefresh: () {},
                onOpen: (i) => opened.add(i.mapID),
              ),
            ),
          ),
        ),
      );
      expect(tester.takeException(), null);
      await tester.scrollUntilVisible(
        find.byKey(ValueKey('map-layer-card-business:${f.mapTestID}')),
        250,
        scrollable: find.byType(Scrollable),
      );
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(ValueKey('map-layer-card-business:${f.mapTestID}')),
      );
      expect(opened, ['business:${f.mapTestID}']);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
      c.dispose();
    },
  );
}
