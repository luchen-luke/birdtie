import 'dart:async';
import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'map_layers_api_test.dart' as f;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'refresh retains current pins but never renews their prior lease',
    () async {
      final pending = Completer<http.Response>();
      var count = 0;
      final c = MapLayersController(
        api: MapLayersApi(
          client: MockClient((_) async {
            if (++count > 1) return pending.future;
            return http.Response(
              jsonEncode({'data': f.mapTestView(milliseconds: 200)}),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          }),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => null,
        organizationWorkspaceID: () => null,
      );
      await c.load('owned-city', f.mapTestBounds);
      final ids = c.items.map((e) => e.mapID).toList();
      final refresh = c.load('owned-city', f.mapTestBounds);
      expect(c.loading, true);
      expect(c.items.map((e) => e.mapID).toList(), ids);
      await Future<void>.delayed(const Duration(milliseconds: 240));
      expect(c.items, isEmpty);
      pending.complete(
        http.Response(
          jsonEncode({'data': f.mapTestView()}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      );
      await refresh;
      expect(c.items.map((e) => e.mapID).toList(), ids);
      c.dispose();
    },
  );
  test('late A-B-A read cannot restore private or public frame', () async {
    final pending = Completer<http.Response>();
    var token = 'Bearer A';
    final c = MapLayersController(
      api: MapLayersApi(
        client: MockClient((r) => pending.future),
        apiBaseUrl: 'http://old.test',
      ),
      authorizationHeader: () => token,
      organizationWorkspaceID: () => null,
    );
    final load = c.load('owned-city', f.mapTestBounds);
    token = 'Bearer B';
    c.retire();
    token = 'Bearer A';
    c.retire();
    pending.complete(
      http.Response(
        jsonEncode({'data': f.mapTestView()}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    await load;
    expect(c.items, isEmpty);
    expect(c.loading, false);
    expect(c.visible, isNot(contains('OPPORTUNITY')));
    c.dispose();
  });
  test(
    'visibility local stable IDs source expiry clears both projected cards and pins',
    () async {
      var gets = 0;
      final c = MapLayersController(
        api: MapLayersApi(
          client: MockClient((r) async {
            gets++;
            return http.Response(
              jsonEncode({'data': f.mapTestView(milliseconds: 80)}),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          }),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => null,
        organizationWorkspaceID: () => null,
      );
      await c.load('owned-city', f.mapTestBounds);
      expect(c.items.length, 5);
      final id = c.items.first.mapID;
      c.toggle('PLACE', false);
      expect(c.find(id), null);
      c.toggle('PLACE', true);
      expect(c.find(id)?.id, f.mapTestID);
      expect(gets, 1);
      await Future<void>.delayed(const Duration(milliseconds: 100));
      expect(c.items, isEmpty);
      expect(c.error, contains('到期'));
      c.dispose();
    },
  );
  test(
    'explicit own overlay and retire remove source no background mutation',
    () async {
      final paths = <String>[];
      String? org;
      final c = MapLayersController(
        api: MapLayersApi(
          client: MockClient((r) async {
            paths.add(r.url.path);
            return http.Response(
              jsonEncode({
                'data': f.mapTestView(
                  private: r.url.path.contains('map-opportunities'),
                ),
              }),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          }),
          apiBaseUrl: 'http://owned.test',
        ),
        authorizationHeader: () => 'Bearer owner',
        organizationWorkspaceID: () => org,
      );
      await c.load('owned-city', f.mapTestBounds);
      expect(paths.length, 1);
      c.toggle('OPPORTUNITY', true);
      await Future<void>.delayed(Duration.zero);
      await Future<void>.delayed(Duration.zero);
      expect(c.items.where((i) => i.kind == 'OPPORTUNITY').length, 1);
      expect(paths.last, contains('/me/'));
      org = 'organization';
      c.retire();
      c.toggle('OPPORTUNITY', true);
      expect(c.items, isEmpty);
      expect(c.visible, isNot(contains('OPPORTUNITY')));
      c.dispose();
    },
  );
}
