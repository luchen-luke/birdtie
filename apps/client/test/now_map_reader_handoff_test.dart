import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/map_cluster_members_sheet.dart';
import 'package:birdtie_client/src/city/map_point_selection_sheet.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'map_layers_api_test.dart' show mapTestBounds;
import 'now_scope_recovery_test.dart' show NowFixtureCity;
import 'place_detail_sheet_test.dart' show jsonReply;

String _id(int n) => '17000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';

http.Response _mapJson(Object value) => http.Response.bytes(
  jsonReply(value).bodyBytes,
  200,
  headers: {'content-type': 'application/json', 'cache-control': 'no-store'},
);

class _NoAgent extends AgentTaskSource {
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => throw StateError('Opening a map reader must not create an Agent task.');
}

void main() {
  testWidgets(
    'Now owned map hands visibility to readers and retires covered or changed identity',
    (t) async {
      final auth = SeedTestAuth()..owner = _id(9001);
      final city = NowFixtureCity();
      final requests = <http.Request>[];
      var reads = 0;
      final source = List.filled(64, 'a').join();
      final client = MockClient((r) async {
        requests.add(r);
        if (r.method != 'GET') return http.Response('{}', 503);
        final now = DateTime.now().toUtc();
        if (r.url.path == '/v1/me/map-content') {
          reads++;
          return _mapJson({
            'data': {
              'schemaVersion': 'map-content-v1',
              'scope': 'MAP_CONTENT',
              'viewerAccountId': auth.accountID,
              'cityId': 'alpha',
              'bounds': mapTestBounds.toJson(),
              'observedAt': now.toIso8601String(),
              'validUntil': now
                  .add(const Duration(seconds: 30))
                  .toIso8601String(),
              'sourceVersion': source,
              'truncated': false,
              'pins': [
                for (var n = 1; n <= 2; n++)
                  {
                    'placeId': _id(n),
                    'name': '合成地点 $n',
                    'categoryCode': 'culture.museum',
                    'cityId': null,
                    'anchor': {
                      'coordinateSystem': 'wgs84',
                      'precision': 'point',
                      'latitude': 57.2,
                      'longitude': -2.1,
                    },
                    'visibilitySources': ['SELF_PRIVATE'],
                    'sourceVersion': source,
                    'momentCount': 0,
                    'moments': [],
                    'momentsTruncated': false,
                    'cover': null,
                  },
              ],
            },
          });
        }
        if (r.url.path == '/v1/cities/alpha/map-layers') {
          return jsonReply({
            'data': {
              'schemaVersion': 'typed-map-layers-v1',
              'scope': 'PUBLIC',
              'cityId': 'alpha',
              'observedAt': now.toIso8601String(),
              'validUntil': now
                  .add(const Duration(minutes: 2))
                  .toIso8601String(),
              'truncated': false,
              'items': [],
            },
          });
        }
        if (r.url.path.endsWith('/activities') ||
            r.url.path == '/v1/me/moments') {
          return jsonReply({'data': []});
        }
        return http.Response('{}', 404);
      });
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://map-reader-handoff.test',
      );
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      MapCanvas canvas() =>
          t.widget<MapCanvas>(find.byType(MapCanvas, skipOffstage: false));
      Future<void> waitForRead() async {
        for (var i = 0; i < 50 && canvas().mapContent!.loading; i++) {
          await t.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 20)),
          );
          await t.pump();
        }
      }

      try {
        await t.pumpWidget(
          MaterialApp(
            theme: birdtieTheme(Brightness.light),
            home: MapWorkspace(
              city: city,
              auth: auth,
              moments: moments,
              agentTaskSource: _NoAgent(),
              seedClient: client,
              seedApiBaseUrl: 'https://map-reader-handoff.test',
              placeDetailClient: client,
              placeDetailApiBaseUrl: 'https://map-reader-handoff.test',
            ),
          ),
        );
        await t.pumpAndSettle();
        await canvas().layers!.load('alpha', mapTestBounds);
        canvas().mapState.initializeViewport(mapTestBounds);
        canvas().mapContent!.sync(cityID: 'alpha', bounds: mapTestBounds);
        await waitForRead();
        await t.pumpAndSettle();
        final owned = canvas().mapContent!;
        expect(
          owned.items,
          hasLength(2),
          reason:
              'reads=$reads error=${owned.error} current=${owned.current} loading=${owned.loading} route=${ModalRoute.of(t.element(find.byType(MapWorkspace)))?.isCurrent}',
        );
        final originalEpoch = owned.epoch, originalReads = reads;
        final mapState = canvas().mapState;
        final mapElement = t.element(find.byType(MapCanvas));
        final composer = t.state<AgentComposerState>(
          find.byType(AgentComposer),
        );
        final nowContext = t.element(find.byType(MapWorkspace));
        final navigator = Navigator.of(nowContext);

        Future<MapEntity?> open(bool cluster) {
          final map = t.widget<PublicCityMapView>(
            find.byType(PublicCityMapView, skipOffstage: false),
          );
          return cluster
              ? showCurrentMapClusterMembers(
                  context: nowContext,
                  cluster: MapEntity(
                    id: 'cluster:synthetic-reader',
                    kind: MapEntityKind.cluster,
                    title: '附近地点',
                    subtitle: '',
                    latitude: 57.2,
                    longitude: -2.1,
                    memberIDs: map.currentReaderEntities!()
                        .map((e) => e.id)
                        .toList(),
                  ),
                  changes: map.readerChanges!,
                  currentMembers: map.currentReaderEntities!,
                  currentScope: map.currentReaderScope!,
                  onVisibilityChanged: map.onMapReaderVisibilityChanged,
                )
              : showCurrentMapPointChoices(
                  context: nowContext,
                  changes: map.readerChanges!,
                  currentCandidates: map.currentReaderEntities!,
                  currentScope: map.currentReaderScope!,
                  onVisibilityChanged: map.onMapReaderVisibilityChanged,
                );
        }

        // Exercise the real Now callback and strict owned-map decoder, not a
        // permissive controller stub or a callback-only modal harness.
        for (final cluster in [false, true]) {
          final selected = open(cluster);
          await t.pumpAndSettle();
          expect(
            owned.current,
            isTrue,
            reason: 'Opening keeps the authorized map read.',
          );
          expect(owned.epoch, originalEpoch);
          expect(reads, originalReads);
          expect(ModalRoute.of(nowContext)!.isCurrent, isFalse);
          final key = cluster ? 'current-map-member:' : 'map-point-choice:';
          await t.tap(find.byKey(ValueKey('$key${owned.items.first.mapID}')));
          await t.pumpAndSettle();
          expect((await selected)?.id, owned.items.first.mapID);
          expect(owned.epoch, originalEpoch);
          expect(reads, originalReads);
          expect(ModalRoute.of(nowContext)!.isCurrent, isTrue);
          expect(t.element(find.byType(MapCanvas)), same(mapElement));
          expect(canvas().mapState, same(mapState));
          expect(t.state(find.byType(AgentComposer)), same(composer));
        }

        // A foreign route still revokes the reader. Its old callback cannot
        // select a row or pop the new route after the source was retired.
        final covered = open(false);
        await t.pumpAndSettle();
        final staleTap = t
            .widget<ListTile>(
              find.byKey(
                ValueKey('map-point-choice:${owned.items.first.mapID}'),
              ),
            )
            .onTap!;
        navigator.push(
          MaterialPageRoute<void>(
            builder: (_) => const Scaffold(
              body: Text('其他页面', key: Key('foreign-reader-route')),
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(owned.current, isFalse);
        expect(owned.items, isEmpty);
        expect(owned.epoch, greaterThan(originalEpoch));
        staleTap();
        await t.pumpAndSettle();
        expect(find.byKey(const Key('foreign-reader-route')), findsOneWidget);
        expect(await covered, isNull);
        expect(
          await open(false),
          isNull,
          reason: 'Covered Now cannot claim a new reader.',
        );
        navigator.pop();
        await t.pumpAndSettle();
        await waitForRead();
        expect(owned.current, isTrue);

        final changed = open(true);
        await t.pumpAndSettle();
        final beforeIdentity = owned.epoch;
        auth.changeIdentity(null);
        await t.pumpAndSettle();
        expect(await changed, isNull);
        expect(owned.current, isFalse);
        expect(owned.items, isEmpty);
        expect(owned.epoch, greaterThan(beforeIdentity));
        expect(
          find.byKey(const Key('current-map-cluster-members')),
          findsNothing,
        );
        expect(canvas().workspace.task, isNull);
        expect(requests.every((r) => r.method == 'GET'), isTrue);
        expect(requests.any((r) => r.url.path.contains('/agent/')), isFalse);
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox.shrink());
        await t.pump();
        moments.dispose();
        auth.dispose();
        city.dispose();
        client.close();
        t.view.resetPhysicalSize();
        t.view.resetDevicePixelRatio();
      }
    },
  );
}
