import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'map_layers_api_test.dart' as f;

http.Response view({bool private = false}) => http.Response(
  jsonEncode({'data': f.mapTestView(private: private)}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  for (final status in [403, 503]) {
    test('public success and private HTTP $status remain partial', () async {
      var privateFails = false;
      final c = MapLayersController(
        api: MapLayersApi(
          client: MockClient((request) async {
            expect(request.method, 'GET');
            expect(request.headers['Authorization'], 'Bearer owner');
            final private = request.url.path.contains('map-opportunities');
            return private && privateFails
                ? http.Response('{}', status)
                : view(private: private);
          }),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => 'Bearer owner',
        organizationWorkspaceID: () => null,
      );
      addTearDown(c.dispose);
      await c.load('owned-city', f.mapTestBounds);
      c.visible.add('OPPORTUNITY');
      await c.load('owned-city', f.mapTestBounds);
      expect(c.privateView?.items, hasLength(1));
      final publicIDs = c.publicView!.items.map((item) => item.mapID).toList();

      privateFails = true;
      await c.load('owned-city', f.mapTestBounds);

      expect(c.loading, false);
      expect(c.partial, true);
      expect(c.unavailableKinds, {'OPPORTUNITY'});
      expect(c.error, contains('活动机会暂不可用'));
      expect(c.publicView!.items.map((item) => item.mapID), publicIDs);
      expect(c.privateView, null);
      expect(c.items.where((item) => item.kind == 'OPPORTUNITY'), isEmpty);
      expect(c.items, hasLength(5));

      privateFails = false;
      await c.load('owned-city', f.mapTestBounds);
      expect(c.partial, false);
      expect(c.unavailableKinds, isEmpty);
      expect(c.error, null);
      expect(c.privateView?.items, hasLength(1));
    });
  }

  test(
    'public failure is unavailable rather than an authoritative empty',
    () async {
      var fails = false;
      var privateReads = 0;
      final c = MapLayersController(
        api: MapLayersApi(
          client: MockClient((request) async {
            final private = request.url.path.contains('map-opportunities');
            if (private) privateReads++;
            return fails ? http.Response('{}', 503) : view(private: private);
          }),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => 'Bearer owner',
        organizationWorkspaceID: () => null,
      );
      addTearDown(c.dispose);
      await c.load('owned-city', f.mapTestBounds);
      c.visible.add('OPPORTUNITY');
      fails = true;
      await c.load('owned-city', f.mapTestBounds);

      expect(c.partial, false);
      expect(c.loading, false);
      expect(c.publicView, null);
      expect(c.privateView, null);
      expect(c.error, isNotNull);
      expect(c.unavailableKinds, mapLayerKinds.toSet());
      expect(privateReads, 0);
      c.retire();
      expect(c.unavailableKinds, isEmpty);
      expect(c.error, null);
    },
  );

  test(
    'late private failure cannot overwrite a newer successful range',
    () async {
      final oldPrivate = Completer<http.Response>();
      final privateStarted = Completer<void>();
      var deferOldPrivate = true;
      final c = MapLayersController(
        api: MapLayersApi(
          client: MockClient((request) async {
            final private = request.url.path.contains('map-opportunities');
            if (private && deferOldPrivate) {
              privateStarted.complete();
              return oldPrivate.future;
            }
            return view(private: private);
          }),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => 'Bearer owner',
        organizationWorkspaceID: () => null,
      );
      addTearDown(c.dispose);
      await c.load('owned-city', f.mapTestBounds);
      c.visible.add('OPPORTUNITY');
      final oldLoad = c.load('owned-city', f.mapTestBounds);
      await privateStarted.future;
      deferOldPrivate = false;
      const nextBounds = MapBounds(
        west: -2.15,
        south: 57.15,
        east: -2.05,
        north: 57.25,
      );
      await c.load('owned-city', nextBounds);
      final currentPublic = c.publicView;
      final currentPrivate = c.privateView;

      oldPrivate.complete(http.Response('{}', 503));
      await oldLoad;

      expect(c.bounds, nextBounds);
      expect(identical(c.publicView, currentPublic), true);
      expect(identical(c.privateView, currentPrivate), true);
      expect(c.partial, false);
      expect(c.unavailableKinds, isEmpty);
      expect(c.error, null);
      expect(c.loading, false);
    },
  );

  test(
    'late private failure after identity retirement cannot restore partial data',
    () async {
      final pending = Completer<http.Response>();
      final started = Completer<void>();
      var token = 'Bearer A';
      final c = MapLayersController(
        api: MapLayersApi(
          client: MockClient((request) async {
            if (request.url.path.contains('map-opportunities')) {
              started.complete();
              return pending.future;
            }
            return view();
          }),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => token,
        organizationWorkspaceID: () => null,
      );
      addTearDown(c.dispose);
      await c.load('owned-city', f.mapTestBounds);
      c.visible.add('OPPORTUNITY');
      final load = c.load('owned-city', f.mapTestBounds);
      await started.future;
      token = 'Bearer B';
      c.retire();
      token = 'Bearer A';
      c.retire();
      pending.complete(http.Response('{}', 403));
      await load;

      expect(c.publicView, null);
      expect(c.privateView, null);
      expect(c.partial, false);
      expect(c.unavailableKinds, isEmpty);
      expect(c.error, null);
      expect(c.loading, false);
    },
  );
}
