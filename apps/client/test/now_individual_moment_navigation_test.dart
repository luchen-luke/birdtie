import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'now_scope_recovery_test.dart' show NowFixture;
import 'map_layers_api_test.dart' show mapTestItem, mapTestView, mapTestBounds;
import 'public_moment_api_test.dart' show momentReply;
import 'public_moment_author_v2_test.dart' show publicMomentV2Data;
import 'entity_share_pending_store_test.dart'
    show shareTarget, shareConversation;

void main() {
  testWidgets(
    'actual Now guest marker opens exact MOMENT, never associated PLACE summary',
    (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      final requests = <http.Request>[];
      final client = MockClient((r) async {
        requests.add(r);
        expect(r.method, 'GET');
        expect(r.headers.containsKey('Authorization'), isFalse);
        if (r.url.path == '/v1/cities/alpha/map-layers') {
          final moment = mapTestItem('MOMENT')
            ..['id'] = shareTarget
            ..['entityRef'] = {'type': 'MOMENT', 'id': shareTarget}
            ..['detailRef'] = {'type': 'MOMENT', 'id': shareTarget};
          moment['anchor']['placeId'] = shareConversation;
          final place = mapTestItem('PLACE')
            ..['id'] = shareConversation
            ..['entityRef'] = {'type': 'PLACE', 'id': shareConversation}
            ..['detailRef'] = {'type': 'PLACE', 'id': shareConversation};
          place['anchor']['placeId'] = shareConversation;
          final view = mapTestView()
            ..['cityId'] = 'alpha'
            ..['items'] = [place, moment];
          return http.Response.bytes(
            utf8.encode(jsonEncode({'data': view})),
            200,
          );
        }
        if (r.url.path == '/v1/moments/$shareTarget') {
          return momentReply(publicMomentV2Data()..['title'] = '独立动态的真实详情');
        }
        throw StateError('unexpected read: ${r.url.path}');
      });
      addTearDown(client.close);
      await t.pumpWidget(
        MaterialApp(
          home: MapWorkspace(
            city: f.city,
            auth: f.auth,
            moments: f.moments,
            agentTaskSource: f.source,
            seedClient: client,
            seedApiBaseUrl: f.base,
          ),
        ),
      );
      await t.pumpAndSettle();
      final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
      final original = find.byType(MapCanvas).evaluate().single;
      final composer = find.byType(AgentComposer).evaluate().single;
      final composerState = t.state<AgentComposerState>(
        find.byType(AgentComposer),
      );
      composerState.fillDraft('保留原来的草稿', requestFocus: false);
      await canvas.layers!.load('alpha', mapTestBounds);
      await t.pumpAndSettle();
      final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      final moment = view.entities.singleWhere(
        (e) => e.id == 'moment:$shareTarget',
      );
      view.onEntitySelected!(moment);
      await t.pumpAndSettle();
      expect(find.byType(PublicMomentPage).hitTestable(), findsOneWidget);
      expect(
        t.widget<PublicMomentPage>(find.byType(PublicMomentPage)).momentID,
        shareTarget,
      );
      expect(find.text('独立动态的真实详情'), findsOneWidget);
      expect(find.byType(NowPublicPlacePanel), findsOneWidget);
      expect(
        t.widget<PublicMomentPage>(find.byType(PublicMomentPage)).embedded,
        isTrue,
      );
      expect(find.byType(AgentComposer).evaluate().single, same(composer));
      expect(composerState.draftText, '保留原来的草稿');
      expect(find.byType(TextField), findsOneWidget);
      expect(
        find.byTooltip('返回上一层'),
        findsNothing,
        reason: 'an unrequested hidden place frame must not exist',
      );
      expect(
        requests.where((r) => r.url.path.startsWith('/v1/places/')),
        isEmpty,
      );
      expect(f.source.queries, isEmpty);
      expect(canvas.workspace.task, isNull);
      expect(
        find.byType(MapCanvas, skipOffstage: false).evaluate().single,
        same(original),
      );
      await t.tap(find.byTooltip('关闭内容返回地图'));
      await t.pumpAndSettle();
      expect(find.byType(MapCanvas).evaluate().single, same(original));
      expect(find.byType(AgentComposer).evaluate().single, same(composer));
      expect(composerState.draftText, '保留原来的草稿');
      expect(find.byType(TextField), findsOneWidget);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );

  testWidgets(
    'signed-in embedded moment has one Now input and preserves explicit public navigation',
    (t) async {
      final f = NowFixture();
      f.auth.token = 'Bearer synthetic-moment-reader';
      final identity = f.auth.authorizationHeader;
      final controller = AgentWorkspaceController(source: f.source);
      final editorKey = GlobalKey<AgentComposerState>();
      final show = ValueNotifier(true);
      final reads = <String>[];
      var sends = 0;
      final client = MockClient((r) async {
        expect(r.method, 'GET');
        expect(r.headers['Authorization'], identity);
        reads.add(r.url.path);
        expect(r.url.path, '/v1/moments/$shareTarget');
        return momentReply(publicMomentV2Data()..['title'] = '已登录的公开详情');
      });
      addTearDown(() {
        show.dispose();
        controller.dispose();
        client.close();
        f.dispose();
      });
      final panel = NowPublicPlacePanel(
        initialObjectDetail: true,
        initialMomentID: shareTarget,
        item: const TypedMapItem(
          kind: 'MOMENT',
          id: shareTarget,
          title: '对应动态',
          detailType: 'MOMENT',
          detailID: shareTarget,
          placeID: shareConversation,
          anchorLabel: '公开场地',
          latitude: 57.2,
          longitude: -2.1,
          sourceVersion: 'synthetic-current',
        ),
        auth: f.auth,
        identityChanges: f.auth,
        workspaceID: () => null,
        current: () => f.auth.authorizationHeader == identity,
        revalidate: () async => f.auth.authorizationHeader == identity,
        onClose: () => show.value = false,
        onOpenActivity: (_) {},
        client: client,
        apiBaseUrl: f.base,
      );
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ValueListenableBuilder<bool>(
              valueListenable: show,
              builder: (_, visible, _) => Column(
                children: [
                  Expanded(child: visible ? panel : const SizedBox()),
                  AgentComposer(
                    key: editorKey,
                    workspace: controller,
                    onSubmit: (_) => sends++,
                    onSearchArea: () {},
                    hasSearchArea: false,
                  ),
                ],
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      editorKey.currentState!.fillDraft(
        '保留 Now 草稿 quiet cafe',
        requestFocus: false,
      );
      await t.pump();
      final editor = editorKey.currentState;
      expect(find.text('已登录的公开详情'), findsOneWidget);
      expect(
        find.byType(TextField),
        findsOneWidget,
        reason: 'signed-in public content must not introduce another editor',
      );
      expect(find.text('查看地点'), findsOneWidget);
      expect(find.text('查看作者主页'), findsOneWidget);
      expect(find.text('发给好友'), findsOneWidget);
      expect(find.byTooltip('返回上一层'), findsNothing);
      expect(reads, ['/v1/moments/$shareTarget']);
      expect(f.source.queries, isEmpty);
      await t.tap(find.byTooltip('关闭内容返回地图'));
      await t.pumpAndSettle();
      expect(editorKey.currentState, same(editor));
      expect(editorKey.currentState!.draftText, '保留 Now 草稿 quiet cafe');
      expect(find.byType(TextField), findsOneWidget);
      expect(sends, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
}
