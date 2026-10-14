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
    'private source failure keeps current public cards and retires old private rows',
    (tester) async {
      tester.view.physicalSize = const Size(800, 1600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      var failPrivate = false;
      var failPublic = false;
      final requests = <String>[];
      final controller = MapLayersController(
        api: MapLayersApi(
          client: MockClient((request) async {
            expect(request.method, 'GET');
            expect(request.body, isEmpty);
            final private = request.url.path.endsWith('/map-opportunities');
            requests.add(private ? 'private' : 'public');
            if (private ? failPrivate : failPublic) {
              return http.Response('{}', private ? 503 : 403);
            }
            return http.Response(
              jsonEncode({'data': f.mapTestView(private: private)}),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          }),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => 'Bearer mock-owner',
        organizationWorkspaceID: () => null,
      );
      try {
        await controller.load('owned-city', f.mapTestBounds);
        controller.toggle('OPPORTUNITY', true);
        await controller.load('owned-city', f.mapTestBounds);
        final oldPrivate = controller.privateView;
        expect(oldPrivate?.items, hasLength(1));
        final opened = <String>[];
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: MapLayerControls(
                controller: controller,
                current: () => true,
                onRefresh: () {},
                onOpen: (item) => opened.add(item.mapID),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();

        failPrivate = true;
        await controller.load('owned-city', f.mapTestBounds);
        await tester.pumpAndSettle();
        expect(controller.partial, isTrue);
        expect(controller.unavailableKinds, {'OPPORTUNITY'});
        expect(controller.publicView?.items, hasLength(5));
        expect(controller.privateView, isNull);
        expect(controller.items.any((item) => item.kind == 'OPPORTUNITY'), isFalse);
        expect(find.text('公开地图已读取；我的活动机会暂不可用，请重试'), findsOneWidget);
        expect(
          find.byKey(ValueKey('map-layer-card-${oldPrivate!.items.single.mapID}')),
          findsNothing,
        );
        final moment = find.byKey(
          ValueKey('map-layer-card-moment:${f.mapTestID}'),
        );
        await tester.scrollUntilVisible(moment, 250);
        await tester.pumpAndSettle();
        expect(moment.hitTestable(), findsOneWidget);
        await tester.tap(moment);
        expect(opened, ['moment:${f.mapTestID}']);
        expect(tester.takeException(), isNull);

        failPublic = true;
        await controller.load('owned-city', f.mapTestBounds);
        await tester.pumpAndSettle();
        expect(controller.partial, isFalse);
        expect(controller.publicView, isNull);
        expect(controller.privateView, isNull);
        expect(controller.items, isEmpty);
        expect(moment, findsNothing);
        expect(requests, [
          'public',
          'public',
          'private',
          'public',
          'private',
          'public',
        ]);
        expect(opened, ['moment:${f.mapTestID}']);
        expect(tester.takeException(), isNull);
      } finally {
        try {
          await tester.pumpWidget(const SizedBox());
        } finally {
          controller.dispose();
        }
      }
    },
  );
}
