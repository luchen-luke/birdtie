import 'dart:async';
import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'map_layers_api_test.dart' as f;

http.Response frame() => http.Response(
  jsonEncode({'data': f.mapTestView(milliseconds: 12000)}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  testWidgets('visible foreground map renews reads without removing current pins', (tester) async {
    var reads = 0;
    final methods = <String>[];
    final c = MapLayersController(
      api: MapLayersApi(client: MockClient((r) async {
        methods.add(r.method);
        reads++;
        return frame();
      }), apiBaseUrl: 'http://owned.test'),
      authorizationHeader: () => null,
      organizationWorkspaceID: () => null,
      refreshAllowed: () => true,
    );
    await c.load('owned-city', f.mapTestBounds);
    final initialLease = c.publicView!.validUntil.difference(c.publicView!.observedAt);
    final states = <String>[];
    c.addListener(() => states.add('$reads:${c.loading}:${c.error}'));
    final ids = c.items.map((i) => i.mapID).toList();
    final old = c.publicView;
    await tester.pump(const Duration(seconds: 10));
    expect(reads, 2, reason: 'lease=$initialLease states=$states loading=${c.loading} error=${c.error}');
    expect(methods, ['GET', 'GET']);
    expect(c.items.map((i) => i.mapID).toList(), ids);
    expect(identical(old, c.publicView), false);
    expect(c.error, null);
    c.dispose();
  });

  testWidgets('pending renewal cannot extend the old source lease', (tester) async {
    var reads = 0;
    final pending = Completer<http.Response>();
    final c = MapLayersController(
      api: MapLayersApi(client: MockClient((_) async => ++reads == 1 ? frame() : pending.future), apiBaseUrl: 'http://owned.test'),
      authorizationHeader: () => null,
      organizationWorkspaceID: () => null,
      refreshAllowed: () => true,
    );
    await c.load('owned-city', f.mapTestBounds);
    final ids = c.items.map((i) => i.mapID).toList();
    await tester.pump(const Duration(seconds: 10));
    expect(c.loading, true);
    expect(c.items.map((i) => i.mapID).toList(), ids);
    await tester.pump(const Duration(seconds: 3));
    expect(c.items, isEmpty);
    expect(c.error, contains('到期'));
    c.dispose();
    pending.complete(frame());
    await tester.pump();
    expect(c.items, isEmpty);
  });

  testWidgets('background or covered route does not renew the map', (tester) async {
    var reads = 0;
    var visible = true;
    final c = MapLayersController(
      api: MapLayersApi(client: MockClient((_) async { reads++; return frame(); }), apiBaseUrl: 'http://owned.test'),
      authorizationHeader: () => null,
      organizationWorkspaceID: () => null,
      refreshAllowed: () => visible,
    );
    await c.load('owned-city', f.mapTestBounds);
    visible = false;
    await tester.pump(const Duration(seconds: 13));
    expect(reads, 1);
    expect(c.items, isEmpty);
    expect(c.error, contains('到期'));
    c.dispose();
  });

  testWidgets('retired identity ABA rejects late automatic renewal', (tester) async {
    var reads = 0;
    String? token = 'Bearer A';
    final pending = Completer<http.Response>();
    final c = MapLayersController(
      api: MapLayersApi(client: MockClient((_) async => ++reads == 1 ? frame() : pending.future), apiBaseUrl: 'http://owned.test'),
      authorizationHeader: () => token,
      organizationWorkspaceID: () => null,
      refreshAllowed: () => true,
    );
    await c.load('owned-city', f.mapTestBounds);
    await tester.pump(const Duration(seconds: 10));
    expect(reads, 2);
    token = 'Bearer B'; c.retire();
    token = 'Bearer A'; c.retire();
    pending.complete(frame());
    await tester.pump();
    expect(c.items, isEmpty);
    expect(c.loading, false);
    expect(c.error, null);
    c.dispose();
  });
}
