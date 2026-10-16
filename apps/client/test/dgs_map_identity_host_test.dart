import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart' show NowFixtureCity;
import 'content_map_controller_test.dart' as owned;
import 'map_layers_api_test.dart' as typed;

// Real host/listeners and decoders; isolated HTTP fixtures, no live writes.
void main() {
  for (final transition in ['workspace', 'token', 'account']) {
    testWidgets(
      'identity $transition restores current map without a camera move',
      (t) async {
        final auth = SeedTestAuth()..owner = owned.id(9000);
        final city = NowFixtureCity();
        final fixture = owned.Fixture();
        var typedReads = 0;
        var ownedReads = 0;
        final client = MockClient((request) async {
          final now = DateTime.now().toUtc();
          if (request.url.path.endsWith('/map-layers')) {
            typedReads++;
            return http.Response(
              jsonEncode({
                'data': {
                  'schemaVersion': 'typed-map-layers-v1',
                  'cityId': 'alpha',
                  'scope': 'PUBLIC',
                  'observedAt': now.toIso8601String(),
                  'validUntil': now
                      .add(const Duration(minutes: 2))
                      .toIso8601String(),
                  'truncated': false,
                  'items': [typed.mapTestItem('PLACE')],
                },
              }),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          }
          if (request.url.path == '/v1/me/map-content') {
            ownedReads++;
            fixture.now = now;
            fixture.owner = auth.accountID!;
            return http.Response(
              jsonEncode({'data': fixture.page(request.url)}),
              200,
              headers: {
                'content-type': 'application/json; charset=utf-8',
                'cache-control': 'no-store',
                'x-content-type-options': 'nosniff',
              },
            );
          }
          return http.Response('{"data":[]}', 200);
        });
        final moments = PrivateMomentController(
          authorizationHeader: () => auth.authorizationHeader,
          client: client,
          apiBaseUrl: owned.base,
        );
        try {
          t.view.physicalSize = const Size(390, 844);
          t.view.devicePixelRatio = 1;
          t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
          await t.pumpWidget(
            MaterialApp(
              home: MapWorkspace(
                city: city,
                auth: auth,
                moments: moments,
                seedClient: client,
                seedApiBaseUrl: owned.base,
              ),
            ),
          );
          await t.pumpAndSettle();
          final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
          final map = t.widget<PublicCityMapView>(
            find.byType(PublicCityMapView),
          );
          map.onViewportInitialized!(owned.bounds);
          map.onContentViewportSettled!(owned.bounds, 13);
          await t.pumpAndSettle();
          expect(
            canvas.layers!.publicView,
            isNotNull,
            reason: 'reads=$typedReads error=${canvas.layers!.error}',
          );
          await owned.drain(t, () => !canvas.mapContent!.loading);
          expect(
            canvas.mapContent!.current,
            isTrue,
            reason: 'reads=$ownedReads error=${canvas.mapContent!.error}',
          );
          final oldPin = canvas.mapContent!.items.single;
          final initialReads = typedReads;
          final initialOwnedReads = ownedReads;
          if (transition == 'workspace') {
            // Open the actual drawer to access its existing shared controller.
            final scaffold = t.state<ScaffoldState>(
              find.byType(Scaffold).first,
            );
            scaffold.openDrawer();
            await t.pumpAndSettle();
            final orgs = t.widget<Sidebar>(find.byType(Sidebar)).organizations;
            scaffold.closeDrawer();
            await t.pumpAndSettle();
            orgs.select(
              const OrganizationWorkspace(
                id: 'test-org',
                name: '隔离组织',
                role: 'owner',
                organizationType: 'club',
              ),
            );
            await t.pumpAndSettle();
            expect(canvas.mapContent!.current, isFalse);
            orgs.select(null);
          } else {
            auth.changeIdentity(
              'Bearer next-session',
              nextOwner: transition == 'account'
                  ? owned.id(9001)
                  : owned.id(9000),
            );
          }
          await t.pumpAndSettle();
          await owned.drain(t, () => !canvas.mapContent!.loading);
          expect(
            canvas.layers!.publicView,
            isNotNull,
            reason:
                'The current public layer must reload after all listeners settle.',
          );
          expect(typedReads, greaterThan(initialReads));
          expect(canvas.mapContent!.current, isTrue);
          expect(ownedReads, greaterThan(initialOwnedReads));
          expect(canvas.mapContent!.sourceCurrent(oldPin), isFalse);
          expect(canvas.mapState.viewportBounds, owned.bounds);
          expect(t.takeException(), isNull);
        } finally {
          await t.pumpWidget(const SizedBox.shrink());
          moments.dispose();
          auth.dispose();
          city.dispose();
          client.close();
          t.view.resetPhysicalSize();
          t.view.resetDevicePixelRatio();
          await t.pump();
        }
      },
    );
  }
}
