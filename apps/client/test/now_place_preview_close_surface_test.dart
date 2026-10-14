import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/entity_peek_card.dart';
import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart' show NowFixtureCity;
import 'map_layers_api_test.dart' show mapTestBounds, mapTestItem, mapTestID;
import 'place_detail_sheet_test.dart'
    show workspacePlaceID, workspaceMomentID, workspacePlaceData, jsonReply;

class _NoQuerySource extends AgentTaskSource {
  int resolves = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    resolves++;
    throw StateError(
      'Native public place navigation must not query the Agent.',
    );
  }
}

/// Actual MapWorkspace, API adapters, map selection dispatch and composer.
/// No Mapbox SDK/tile setup: logical-pixel callback samples are test inputs,
/// and this does not prove device pixel units or real native annotation taps.
class _Host {
  final auth = SeedTestAuth()..token = null;
  final city = NowFixtureCity();
  final source = _NoQuerySource();
  final requests = <http.Request>[];
  Duration lease = const Duration(minutes: 2);
  String kind = 'PLACE';
  String get entityID => kind == 'PLACE'
      ? workspacePlaceID
      : kind == 'MOMENT'
      ? workspaceMomentID
      : mapTestID;
  late final client = MockClient((r) async {
    requests.add(r);
    if (r.method != 'GET') return http.Response('{}', 503);
    if (r.url.path == '/v1/cities/alpha/map-layers') {
      final now = DateTime.now().toUtc();
      final row = mapTestItem(kind);
      row['id'] = entityID;
      row['entityRef'] = {'type': kind, 'id': entityID};
      row['detailRef'] = {'type': kind, 'id': entityID};
      row['anchor']['placeId'] = workspacePlaceID;
      row['anchor']['label'] = '带长中文与 Aberdeen café 混排的真实合成地点标题';
      return jsonReply({
        'data': {
          'schemaVersion': 'typed-map-layers-v1',
          'cityId': 'alpha',
          'scope': 'PUBLIC',
          'observedAt': now.toIso8601String(),
          'validUntil': now.add(lease).toIso8601String(),
          'truncated': false,
          'items': [row],
        },
      });
    }
    if (r.url.path == '/v1/places/$workspacePlaceID/social-history/moments') {
      final author = r.url.queryParameters['include'] == 'public-author';
      final next = r.url.queryParameters.containsKey('cursor');
      expect(r.url.queryParameters, {
        if (author) 'include': 'public-author',
        if (next) 'cursor': 'synthetic_preview_cursor',
      });
      return jsonReply({'data': _pageData(next: next, author: author)});
    }
    if (r.url.path.startsWith('/v1/moments/')) {
      final matches = [
        for (var n = 1; n <= 7; n++) n,
      ].where((n) => r.url.path == '/v1/moments/${_storyID(n)}');
      if (matches.isEmpty) return http.Response('{}', 404);
      final author = r.url.queryParameters['include'] == 'public-author';
      expect(r.url.queryParameters, {if (author) 'include': 'public-author'});
      return jsonReply({'data': _momentData(matches.single, author: author)});
    }
    if (r.url.path == '/v1/places/$workspacePlaceID/social-history') {
      final now = DateTime.now().toUtc();
      final author = r.url.queryParameters['include'] == 'public-author';
      return jsonReply({
        'data': {
          'schemaVersion': author
              ? 'place-social-history-v2'
              : 'place-social-history-v1',
          'placeId': workspacePlaceID,
          'cityId': 'alpha',
          'windowDays': 30,
          'windowStart': now
              .subtract(const Duration(days: 30))
              .toIso8601String(),
          'checkedAt': now.toIso8601String(),
          'recentMomentCount': 7,
          'recentMoments': [
            for (var n = 1; n <= 5; n++)
              {
                'id': n == 1
                    ? workspaceMomentID
                    : '81000000-0000-4000-8000-${n.toString().padLeft(12, '0')}',
                'title': '真实公开故事 $n',
                'excerpt': '明确公开的合成节选',
                'revision': 2,
                'publishedAt': now
                    .subtract(Duration(days: n))
                    .toIso8601String(),
                if (author) ...{'authorId': null, 'authorPublicProfile': null},
              },
          ],
          'activityPatterns': [],
          'suitability': null,
        },
      });
    }
    if (r.url.path == '/v1/places/$workspacePlaceID') {
      return jsonReply({'data': workspacePlaceData()});
    }
    if (r.url.path.endsWith('/activities') || r.url.path == '/v1/me/moments') {
      return jsonReply({'data': []});
    }
    return http.Response('{}', 404);
  });
  // Synthetic native page fixtures only; no prototype assets/permissions.
  late final _historyEnd = DateTime.now().toUtc();
  String _storyID(int n) => n == 1
      ? workspaceMomentID
      : '81000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
  Map<String, dynamic> _pageData({
    required bool next,
    required bool author,
  }) => {
    'schemaVersion': 'place-social-history-page-v1',
    'placeId': workspacePlaceID,
    'cityId': 'alpha',
    'authorMode': author ? 'PUBLIC_PROFILE' : 'NONE',
    'windowDays': 30,
    'windowStart': _historyEnd
        .subtract(const Duration(days: 30))
        .toIso8601String(),
    'windowEnd': _historyEnd.toIso8601String(),
    'observedAt': _historyEnd.toIso8601String(),
    'validUntil': _historyEnd.add(const Duration(minutes: 2)).toIso8601String(),
    'sourceVersion': List.filled(64, 'a').join(),
    'count': 7,
    'items': [
      for (var n = next ? 6 : 1; n <= (next ? 7 : 5); n++)
        {
          'id': _storyID(n),
          'title': '真实公开故事 $n',
          'excerpt': '明确公开的合成节选',
          'revision': 2,
          'publishedAt': _historyEnd
              .subtract(Duration(days: n))
              .toIso8601String(),
          'authorId': null,
          'authorPublicProfile': null,
        },
    ],
    'nextCursor': next ? null : 'synthetic_preview_cursor',
  };
  Map<String, dynamic> _momentData(int n, {required bool author}) => {
    'schemaVersion': author ? 'public-moment-v2' : 'public-moment-v1',
    'id': _storyID(n),
    'title': '真实公开故事 $n',
    'body': '本人明确公开的合成完整正文 $n',
    'placeId': workspacePlaceID,
    'placeName': '明确公开的合成地点',
    'cityId': 'alpha',
    'revision': 2,
    'publishedAt': _historyEnd.subtract(Duration(days: n)).toIso8601String(),
    if (author) ...{'authorId': null, 'authorPublicProfile': null},
  };

  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: client,
    apiBaseUrl: 'https://host-unit.test',
  );
  final size = ValueNotifier(const Size(390, 844));
  final scale = ValueNotifier(1.0);
  late final presentation = Listenable.merge([size, scale]);

  MapCanvas canvas(WidgetTester t) =>
      t.widget<MapCanvas>(find.byType(MapCanvas));
  AgentWorkspaceController workspace(WidgetTester t) => canvas(t).workspace;

  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = size.value;
    t.view.devicePixelRatio = 1;
    await t.pumpWidget(
      MaterialApp(
        theme: birdtieTheme(Brightness.light),
        builder: (context, child) => AnimatedBuilder(
          animation: presentation,
          child: child,
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context).copyWith(
              size: size.value,
              textScaler: TextScaler.linear(scale.value),
            ),
            child: child!,
          ),
        ),
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          agentTaskSource: source,
          seedClient: client,
          seedApiBaseUrl: 'https://host-unit.test',
          placeDetailClient: client,
          placeDetailApiBaseUrl: 'https://host-unit.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    await canvas(t).layers!.load('alpha', mapTestBounds);
    canvas(t).mapState.initializeViewport(mapTestBounds);
    await t.pumpAndSettle();
    expect(canvas(t).layers!.error, isNull);
  }

  Future<void> select(WidgetTester t, Offset logicalAnchor) async {
    final map = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    final entity = map.entities.singleWhere(
      (e) => e.id == '${kind.toLowerCase()}:$entityID',
    );
    map.onEntitySelected!(
      entity,
    ); // The production callback validates the original visible source.
    await t.pump();
    canvas(t).onSelectedAnchor?.call((id: entity.id, point: logicalAnchor));
    await t.pumpAndSettle();
    expect(
      find.byType(NowPlacePreview),
      kind == 'PLACE' ? findsOneWidget : findsNothing,
    );
    expect(
      find.byType(NowPublicPlacePanel),
      kind == 'MOMENT' ? findsOneWidget : findsNothing,
    );
    if (kind == 'MOMENT') {
      expect(
        t.widget<PublicMomentPage>(find.byType(PublicMomentPage)).momentID,
        entityID,
      );
      expect(
        requests.where((r) => r.url.path.startsWith('/v1/places/')),
        isEmpty,
      );
    }
  }

  Future<void> resize(WidgetTester t, Size next, double factor) async {
    t.view.physicalSize = next;
    size.value = next;
    scale.value = factor;
    await t.pumpAndSettle();
  }

  void assertCloseReachable(WidgetTester t) {
    final host = t.getRect(find.byKey(const Key('now-place-preview-position')));
    final close = find.byTooltip('收起地点预览');
    final hit = t.getRect(close);
    expect(hit.width, greaterThanOrEqualTo(48));
    expect(hit.height, greaterThanOrEqualTo(48));
    expect(host.height, greaterThanOrEqualTo(56));
    expect(host.contains(hit.topLeft), isTrue);
    expect(host.contains(hit.bottomRight - const Offset(.1, .1)), isTrue);
    expect(
      host.bottom,
      lessThanOrEqualTo(t.getRect(find.byType(AgentComposer)).top),
    );
    expect(close.hitTestable(), findsOneWidget);
  }

  Future<void> finish(WidgetTester t) async {
    expect(source.resolves, 0);
    expect(requests.every((r) => r.method == 'GET'), isTrue);
    expect(requests.any((r) => r.url.path.contains('/agent/')), isFalse);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
    moments.dispose();
    auth.dispose();
    city.dispose();
    client.close();
    size.dispose();
    scale.dispose();
    t.view.resetPhysicalSize();
    t.view.resetDevicePixelRatio();
    t.view.resetViewInsets();
  }
}

void expectOnlyComposerAfterPreviewClose(WidgetTester t) {
  expect(find.byType(NowPlacePreview), findsNothing);
  expect(find.byType(NowPublicPlacePanel), findsNothing);
  expect(find.byType(EntityPeekCard), findsNothing);
  expect(find.byType(AgentComposer), findsOneWidget);
  expect(find.byType(TextField), findsOneWidget);
  final surface = t.widget<Material>(
    find.byKey(const Key('now-primary-surface')),
  );
  expect(surface.color, Colors.transparent);
  // Actual mounted bounds: no content-sized blank between the root surface
  // edge and the one composer. This is not a guessed result row height.
  final overlay = t.getRect(find.byKey(const Key('now-task-overlay')));
  final composer = t.getRect(find.byType(AgentComposer));
  expect(overlay.height - composer.height, lessThanOrEqualTo(24));
}

void main() {
  for (final kind in ['PLACE', 'MOMENT']) {
    testWidgets('$kind 公开内容关闭保留原选点和地图，不回落大块旧Peek内容面板', (t) async {
      final f = _Host()..kind = kind;
      try {
        await f.mount(t);
        final ws = f.workspace(t);
        final map = t.state(find.byType(PublicCityMapView));
        final mapElement = find.byType(MapCanvas).evaluate().single;
        final viewport = f.canvas(t).mapState.viewportBounds;
        final composer = t.state<AgentComposerState>(
          find.byType(AgentComposer),
        );
        final task = ws.task, result = ws.result, epoch = ws.taskEpoch;
        final messages = List<AgentMessage>.of(ws.conversation);
        final contextScroll = t.state<ScrollableState>(
          find
              .descendant(
                of: find.byKey(
                  const PageStorageKey('now-native-context-scroll'),
                  skipOffstage: false,
                ),
                matching: find.byType(Scrollable, skipOffstage: false),
              )
              .first,
        );
        final pixels = contextScroll.position.pixels;
        composer.fillDraft('预览关闭仍保留的草稿 café', requestFocus: false);
        await f.select(t, const Offset(340, 420));
        final selected = ws.selectedEntityId;
        final reads = f.requests.length;
        await t.tap(find.byTooltip(kind == 'PLACE' ? '收起地点预览' : '关闭内容返回地图'));
        await t.pumpAndSettle();
        expectOnlyComposerAfterPreviewClose(t);
        expect(ws.selectedEntityId, selected);
        expect(f.canvas(t).mapState.viewportBounds, viewport);
        expect(t.state(find.byType(PublicCityMapView)), same(map));
        expect(find.byType(MapCanvas).evaluate().single, same(mapElement));
        expect(
          t.state<AgentComposerState>(find.byType(AgentComposer)),
          same(composer),
        );
        expect(composer.draftText, '预览关闭仍保留的草稿 café');
        expect(ws.task, same(task));
        expect(ws.result, same(result));
        expect(ws.taskEpoch, epoch);
        expect(ws.conversation, messages);
        expect(contextScroll.mounted, isTrue);
        expect(contextScroll.position.pixels, pixels);
        expect(f.requests.length, reads);
        ws.setSheetExtent(AgentSheetExtent.medium);
        ws.beginTyping();
        await t.pumpAndSettle();
        ws.stopTyping();
        await t.pumpAndSettle();
        expectOnlyComposerAfterPreviewClose(t);
        expect(ws.selectedEntityId, selected);
        expect(composer.draftText, '预览关闭仍保留的草稿 café');
        expect(f.requests.length, reads);
      } finally {
        await f.finish(t);
      }
    });

    testWidgets('$kind原公开内容到期，刷新真实原地图后收起不借旧内容恢复大面板', (t) async {
      final f = _Host()
        ..kind = kind
        ..lease = const Duration(seconds: 2);
      try {
        await f.mount(t);
        final ws = f.workspace(t);
        final map = t.state(find.byType(PublicCityMapView));
        final composer = t.state<AgentComposerState>(
          find.byType(AgentComposer),
        );
        composer.fillDraft('到期并刷新后草稿仍在', requestFocus: false);
        await f.select(t, const Offset(340, 420));
        final selected = ws.selectedEntityId;
        final deadline = f.canvas(t).layers!.publicView!.validUntil;
        final originalReader = kind == 'MOMENT'
            ? t.state(find.byType(NowPublicPlacePanel))
            : null;
        await t.pump(const Duration(seconds: 3));
        await t.pumpAndSettle();
        expect(f.canvas(t).layers!.publicView, isNull);
        expect(f.canvas(t).layers!.items, isEmpty);
        expect(f.canvas(t).layers!.error, isNotNull);
        expect(
          find.text(kind == 'PLACE' ? '请重新打开地点' : '请重新打开内容'),
          findsOneWidget,
        );
        expect(find.textContaining('7 条公开分享'), findsNothing);
        // Use the original API/controller to load a fresh map. The existing
        // preview remains retired: a fresh map response never renews its lease.
        f.lease = const Duration(minutes: 2);
        await f.canvas(t).layers!.load('alpha', mapTestBounds);
        await t.pumpAndSettle();
        expect(f.canvas(t).layers!.publicView, isNotNull);
        if (kind == 'PLACE') {
          expect(
            t.widget<NowPlacePreview>(find.byType(NowPlacePreview)).validUntil,
            deadline,
          );
        } else {
          expect(
            t.state(find.byType(NowPublicPlacePanel)),
            same(originalReader),
          );
          expect(find.byType(PublicMomentPage), findsNothing);
        }
        expect(
          find.text(kind == 'PLACE' ? '请重新打开地点' : '请重新打开内容'),
          findsOneWidget,
        );
        await t.tap(find.byTooltip(kind == 'PLACE' ? '收起地点预览' : '关闭内容返回地图'));
        await t.pumpAndSettle();
        expectOnlyComposerAfterPreviewClose(t);
        expect(ws.selectedEntityId, selected);
        expect(t.state(find.byType(PublicCityMapView)), same(map));
        expect(
          t.state<AgentComposerState>(find.byType(AgentComposer)),
          same(composer),
        );
        expect(composer.draftText, '到期并刷新后草稿仍在');
        expect(
          f.requests.where((r) => r.url.path.contains('/map-layers')).length,
          2,
        );
      } finally {
        await f.finish(t);
      }
    });
  }

  testWidgets('关闭地点preview后原Agent任务回答与Resultset继续展示，不清会话或滚动', (t) async {
    final f = _Host();
    try {
      await f.mount(t);
      final ws = f.workspace(t);
      final previewEntity = f.canvas(t).layers!.items.single.entity;
      final originalPlace = PublicPlace.fromJson(workspacePlaceData());
      // Synthetic presentation fixture, matching the original host regression
      // style. No Agent request/native authority/model permission is supplied.
      final resultSet = AgentResultSet(
        id: '仅组件状态合成结果集',
        status: 'ready',
        entities: [AgentResultRef(type: 'place', id: originalPlace.id)],
        generatedAt: DateTime.now().toUtc(),
      );
      ws.task = const AgentTask(
        id: '03000000-0000-4000-8000-000000000001',
        query: '仅组件既有地点任务',
        status: 'COMPLETED',
        cityID: 'alpha',
        intent: 'FIND_PLACE',
      );
      ws.result = AgentResult(
        // This legacy PLACE has no public point, just as its original API
        // fixture says. No fabricated map coordinates or action grant.
        entities: const [],
        activities: const [],
        places: [originalPlace],
        resultSet: resultSet,
        note: '原Agent合成回答仍可读',
      );
      ws.state = AgentViewState.results;
      ws.conversation.addAll([
        for (var i = 0; i < 8; i++)
          AgentMessage(
            role: 'assistant',
            text: '合成历史回答$i：${'只用于测试原会话阅读位置保留。' * 12}',
          ),
        const AgentMessage(role: 'assistant', text: '原Agent合成回答仍可读'),
      ]);
      ws.showContent(AgentContentMode.conversation);
      await t.pumpAndSettle();
      final task = ws.task, result = ws.result, epoch = ws.taskEpoch;
      final messages = List<AgentMessage>.of(ws.conversation);
      final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
      composer.fillDraft('原任务旁未发送草稿', requestFocus: false);
      final sheet = find.byType(AgentResultsSheet).evaluate().single;
      final answer = find.byType(AgentConversation).evaluate().single;
      final scroll = find
          .descendant(
            of: find.byType(AgentConversation),
            matching: find.byType(Scrollable),
          )
          .first;
      await t.fling(scroll, const Offset(0, 180), 500);
      await t.pumpAndSettle();
      final position = t.state<ScrollableState>(scroll).position;
      expect(position.pixels, greaterThan(0));
      final pixels = position.pixels;
      // Presentation-only existing Agent fixture has no fake AUTHORIZED_VIEW
      // grant. Feed the original controller's current native entity selection
      // to the real host preview observer; do not bypass/fake map tap authority.
      ws.selectEntity(previewEntity.id, preserveViewport: true);
      await t.pumpAndSettle();
      expect(find.byType(NowPlacePreview), findsOneWidget);
      expect(find.byType(AgentResultsSheet), findsNothing);
      await t.tap(find.byTooltip('收起地点预览'));
      await t.pumpAndSettle();
      expect(find.byType(NowPlacePreview), findsNothing);
      expect(find.byType(AgentResultsSheet), findsOneWidget);
      expect(find.byType(AgentConversation), findsOneWidget);
      expect(find.byType(AgentResultsSheet).evaluate().single, same(sheet));
      expect(find.byType(AgentConversation).evaluate().single, same(answer));
      expect(position.pixels, closeTo(pixels, .5));
      ws.setSheetExtent(AgentSheetExtent.expanded);
      await t.pumpAndSettle();
      expect(position.pixels, closeTo(pixels, .5));
      expect(ws.task, same(task));
      expect(ws.result, same(result));
      expect(ws.result!.resultSet, same(resultSet));
      expect(ws.taskEpoch, epoch);
      expect(ws.conversation, messages);
      expect(composer.draftText, '原任务旁未发送草稿');
      // Original non-typed PLACE result row is still reachable in its original
      // Agent results mode, independent of expired/absent native layers.
      f.canvas(t).layers!.retire();
      ws.showContent(AgentContentMode.results);
      await t.pumpAndSettle();
      expect(find.byType(AgentResultsSheet), findsOneWidget);
      // All original offset/element assertions above precede these actual
      // user scrolls. Results mode retains the same lazy conversation list;
      // it does not force a reader of older messages back to the final row.
      await t.scrollUntilVisible(
        find.text('原Agent合成回答仍可读'),
        150,
        scrollable: scroll,
      );
      await t.pumpAndSettle();
      expect(find.text('原Agent合成回答仍可读'), findsOneWidget);
      expect(find.text('原Agent合成回答仍可读').hitTestable(), findsOneWidget);
      await t.scrollUntilVisible(
        find.text(originalPlace.name),
        150,
        scrollable: scroll,
      );
      await t.pumpAndSettle();
      expect(find.text(originalPlace.name), findsOneWidget);
      expect(find.text(originalPlace.name).hitTestable(), findsOneWidget);
      expect(ws.result, same(result));
      expect(ws.result!.places.single, same(originalPlace));
    } finally {
      await f.finish(t);
    }
  });

  testWidgets('原Activity选点仍有真实Peek卡与原面板，不被地点preview显示条件误隐藏', (t) async {
    final f = _Host()..kind = 'ACTIVITY';
    try {
      await f.mount(t);
      final map = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      final entity = map.entities.single;
      map.onEntitySelected!(entity);
      await t.pumpAndSettle();
      expect(f.workspace(t).selectedEntityId, entity.id);
      expect(find.byType(NowPlacePreview), findsNothing);
      expect(find.byType(EntityPeekCard), findsOneWidget);
      expect(
        t.widget<Material>(find.byKey(const Key('now-primary-surface'))).color,
        Theme.of(t.element(find.byType(MapWorkspace))).colorScheme.surface,
      );
    } finally {
      await f.finish(t);
    }
  });
}
