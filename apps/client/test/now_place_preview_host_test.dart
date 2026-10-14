import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/now_social_edge.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart' show NowFixtureCity;
import 'map_layers_api_test.dart' show mapTestBounds, mapTestItem;
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
  late final client = MockClient((r) async {
    requests.add(r);
    if (r.method != 'GET') return http.Response('{}', 503);
    if (r.url.path == '/v1/cities/alpha/map-layers') {
      final now = DateTime.now().toUtc();
      final row = mapTestItem('PLACE');
      row['id'] = workspacePlaceID;
      row['entityRef'] = {'type': 'PLACE', 'id': workspacePlaceID};
      row['detailRef'] = {'type': 'PLACE', 'id': workspacePlaceID};
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
      (e) => e.id == 'place:$workspacePlaceID',
    );
    map.onEntitySelected!(
      entity,
    ); // The production callback validates the original visible source.
    await t.pump();
    canvas(t).onSelectedAnchor?.call((id: entity.id, point: logicalAnchor));
    await t.pumpAndSettle();
    expect(find.byType(NowPlacePreview), findsOneWidget);
    expect(find.byType(NowPublicPlacePanel), findsNothing);
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

void main() {
  testWidgets('真实Now选点预览关闭，extent与IME通知不自动回开；单图草稿视口任务保留', (t) async {
    final f = _Host();
    await f.mount(t);
    final ws = f.workspace(t), task = ws.task, epoch = ws.taskEpoch;
    final conversation = List<AgentMessage>.of(ws.conversation);
    final mapElement = find.byType(MapCanvas).evaluate().single;
    final mapState = t.state(find.byType(PublicCityMapView));
    final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
    composer.fillDraft('未发送中文草稿 café', requestFocus: false);
    final bounds = f.canvas(t).mapState.viewportBounds;
    await f.select(t, const Offset(340, 420));
    final selected = ws.selectedEntityId;
    f.assertCloseReachable(t);
    await t.tap(find.byTooltip('收起地点预览'));
    await t.pumpAndSettle();
    expect(find.byType(NowPlacePreview), findsNothing);
    expect(
      ws.selectedEntityId,
      selected,
    ); // Map-tap selection is the pre-preview selection.
    ws.setSheetExtent(AgentSheetExtent.medium);
    await t.pumpAndSettle();
    expect(find.byType(NowPlacePreview), findsNothing);
    ws.beginTyping();
    await t.pumpAndSettle();
    ws.stopTyping();
    await t.pumpAndSettle();
    expect(find.byType(NowPlacePreview), findsNothing);
    expect(find.byType(MapCanvas).evaluate().single, same(mapElement));
    expect(t.state(find.byType(PublicCityMapView)), same(mapState));
    expect(
      t.state<AgentComposerState>(find.byType(AgentComposer)),
      same(composer),
    );
    expect(composer.draftText, '未发送中文草稿 café');
    expect(ws.task, same(task));
    expect(ws.taskEpoch, epoch);
    expect(ws.conversation, conversation);
    expect(f.canvas(t).mapState.viewportBounds, bounds);
    await f.finish(t);
  });

  testWidgets('右侧锚点展开三故事使用全安全宽度，200%窄屏自然减少列', (t) async {
    final f = _Host();
    await f.mount(t);
    await f.select(t, const Offset(375, 530));
    await t.ensureVisible(find.text('看看公开动态'));
    await t.tap(find.text('看看公开动态'));
    await t.pumpAndSettle();
    final host = t.getRect(find.byKey(const Key('now-place-preview-position')));
    expect(host.left, closeTo(16, .1));
    expect(host.right, closeTo(374, .1));
    expect(find.text('真实公开故事 1'), findsOneWidget);
    expect(find.text('真实公开故事 2'), findsOneWidget);
    expect(find.text('真实公开故事 3'), findsOneWidget);
    expect(find.text('真实公开故事 4'), findsNothing);
    f.assertCloseReachable(t);
    await f.resize(t, const Size(320, 568), 2);
    expect(find.text('真实公开故事 1'), findsOneWidget);
    expect(find.text('真实公开故事 2'), findsNothing);
    expect(t.takeException(), isNull);
    f.assertCloseReachable(t);
    await f.finish(t);
  });

  testWidgets('200%底部锚点宿主关闭可触达，viewport不靠48px切掉按钮padding', (t) async {
    final f = _Host();
    f.size.value = const Size(320, 568);
    f.scale.value = 2;
    await f.mount(t);
    await f.select(t, const Offset(310, 550));
    f.assertCloseReachable(t);
    await t.tap(find.byTooltip('收起地点预览'));
    await t.pumpAndSettle();
    expect(find.byType(NowPlacePreview), findsNothing);
    expect(find.byType(AgentComposer), findsOneWidget);
    await f.finish(t);
  });

  testWidgets('preview显示期间键盘优先单输入框，输入草稿和地图实例不会回退', (t) async {
    final f = _Host();
    await f.mount(t);
    await f.select(t, const Offset(200, 400));
    final map = t.state(find.byType(PublicCityMapView));
    final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
    composer.fillDraft('键盘期间保存的草稿', requestFocus: true);
    t.view.viewInsets = const FakeViewPadding(bottom: 320);
    await t.pumpAndSettle();
    expect(find.byType(TextField), findsOneWidget);
    expect(find.byType(NowPlacePreview), findsNothing);
    expect(
      t.getRect(find.byType(AgentComposer)).bottom,
      lessThanOrEqualTo(524),
    );
    expect(t.state(find.byType(PublicCityMapView)), same(map));
    expect(
      t.state<AgentComposerState>(find.byType(AgentComposer)),
      same(composer),
    );
    expect(composer.draftText, '键盘期间保存的草稿');
    composer.pauseEditing();
    t.view.resetViewInsets();
    await t.pumpAndSettle();
    expect(find.byType(TextField), findsOneWidget);
    expect(composer.draftText, '键盘期间保存的草稿');
    await f.finish(t);
  });

  testWidgets('原source到期旧标题隐藏，恢复提示不会盖住唯一输入或让关闭不可达', (t) async {
    final f = _Host()..lease = const Duration(seconds: 2);
    await f.mount(t);
    await f.select(t, const Offset(380, 720));
    await t.pump(const Duration(seconds: 3));
    await t.pumpAndSettle();
    expect(find.text('带长中文与 Aberdeen café 混排的真实合成地点标题'), findsNothing);
    expect(find.textContaining('7 条公开分享'), findsNothing);
    expect(find.byType(NowPublicPlacePanel), findsNothing);
    expect(find.byType(AgentComposer), findsOneWidget);
    if (find.byType(NowPlacePreview).evaluate().isNotEmpty) {
      f.assertCloseReachable(t);
      await t.tap(find.byTooltip('收起地点预览'));
      await t.pumpAndSettle();
    }
    expect(find.byType(NowPlacePreview), findsNothing);
    await f.finish(t);
  });

  testWidgets('source epoch替换时关闭不恢复旧临时选择，不因同ID新回包借回旧权限', (t) async {
    final f = _Host();
    await f.mount(t);
    await f.select(t, const Offset(260, 410));
    final ws = f.workspace(t), layers = f.canvas(t).layers!;
    layers.retire();
    await layers.load('alpha', mapTestBounds);
    await t.pumpAndSettle();
    expect(find.text('请重新打开地点'), findsOneWidget);
    f.assertCloseReachable(t);
    await t.tap(find.byTooltip('收起地点预览'));
    await t.pumpAndSettle();
    expect(ws.selectedEntityId, isNull);
    expect(find.byType(NowPlacePreview), findsNothing);
    ws.setSheetExtent(AgentSheetExtent.peek);
    await t.pumpAndSettle();
    expect(find.byType(NowPlacePreview), findsNothing);
    await f.finish(t);
  });

  testWidgets('地点展开到第二组后输入并退出，原strip State与页码保留且零新增GET', (t) async {
    final f = _Host();
    try {
      await f.mount(t);
      await f.select(t, const Offset(375, 530));
      await t.ensureVisible(find.text('看看公开动态'));
      await t.tap(find.text('看看公开动态'));
      await t.pumpAndSettle();
      await t.ensureVisible(find.byTooltip('下一组公开动态'));
      await t.tap(find.byTooltip('下一组公开动态'));
      await t.pumpAndSettle();
      expect(find.text('真实公开故事 4'), findsOneWidget);
      expect(find.text('4–5 / 5 条已读取动态'), findsOneWidget);
      final preview = t.state(find.byType(NowPlacePreview));
      final strip = t.state(find.byType(NowPlaceStoryStrip));
      final stripElement = find.byType(NowPlaceStoryStrip).evaluate().single;
      final map = t.state(find.byType(PublicCityMapView));
      final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
      final reads = f.requests.length;
      composer.fillDraft('第二组地点故事旁的原草稿 café', requestFocus: true);
      t.view.viewInsets = const FakeViewPadding(bottom: 320);
      await t.pumpAndSettle();
      expect(find.byType(TextField), findsOneWidget);
      expect(find.byType(NowPlacePreview), findsNothing);
      expect(find.byType(NowPlaceStoryStrip), findsNothing);
      expect(preview.mounted, isTrue);
      expect(strip.mounted, isTrue);
      expect(
        t.state(find.byType(NowPlacePreview, skipOffstage: false)),
        same(preview),
      );
      expect(
        t.state(find.byType(NowPlaceStoryStrip, skipOffstage: false)),
        same(strip),
      );
      expect(
        find.byType(NowPlaceStoryStrip, skipOffstage: false).evaluate().single,
        same(stripElement),
      );
      expect(find.text('4–5 / 5 条已读取动态', skipOffstage: false), findsOneWidget);
      expect(
        find.byTooltip('收起地点预览', skipOffstage: false).hitTestable(),
        findsNothing,
      );
      expect(
        t.getRect(find.byType(AgentComposer)).bottom,
        lessThanOrEqualTo(524),
      );
      composer.pauseEditing();
      t.view.resetViewInsets();
      await t.pumpAndSettle();
      expect(t.state(find.byType(NowPlacePreview)), same(preview));
      expect(t.state(find.byType(NowPlaceStoryStrip)), same(strip));
      expect(find.text('4–5 / 5 条已读取动态'), findsOneWidget);
      expect(find.text('真实公开故事 4'), findsOneWidget);
      expect(find.text('真实公开故事 1'), findsNothing);
      expect(
        t.getRect(find.byKey(const Key('now-place-preview-position'))).left,
        closeTo(16, .1),
      );
      expect(f.requests.length, reads);
      expect(composer.draftText, '第二组地点故事旁的原草稿 café');
      expect(
        t.state<AgentComposerState>(find.byType(AgentComposer)),
        same(composer),
      );
      expect(t.state(find.byType(PublicCityMapView)), same(map));
      f.assertCloseReachable(t);
    } finally {
      await f.finish(t);
    }
  });

  testWidgets('打开并关闭真实社交栏保留原Agent回答Element与非零内外阅读滚动', (t) async {
    final f = _Host();
    try {
      await f.mount(t);
      final ws = f.workspace(t);
      // Synthetic existing conversation, following the original material
      // workspace fixture. This is state-retention evidence, not a model or
      // native authority/permission fixture, and performs no Agent request.
      ws.task = const AgentTask(
        id: '03000000-0000-4000-8000-000000000001',
        query: '仅组件合成的既有长会话',
        status: 'COMPLETED',
        cityID: 'alpha',
      );
      ws.result = const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '仅组件状态测试，未使用模型或联网问答。',
      );
      ws.state = AgentViewState.results;
      ws.conversation.addAll([
        for (var i = 0; i < 7; i++) ...[
          AgentMessage(role: 'user', text: '合成历史问题 $i'),
          AgentMessage(
            role: 'assistant',
            text: '合成历史回答 $i：${'这些文字只检查阅读位置，不能作为真实检索或推荐结果。' * 10}',
          ),
        ],
        const AgentMessage(role: 'assistant', text: '合成当前最后一条回答。'),
      ]);
      ws.showContent(AgentContentMode.conversation);
      await t.pumpAndSettle();
      final conversation = find.byType(AgentConversation);
      final scroll = find
          .descendant(of: conversation, matching: find.byType(Scrollable))
          .first;
      await t.fling(scroll, const Offset(0, 180), 500);
      await t.pumpAndSettle();
      final scrollState = t.state<ScrollableState>(scroll);
      final position = scrollState.position;
      expect(position.pixels, greaterThan(0));
      expect(position.pixels, lessThan(position.maxScrollExtent - 20));
      final nested = t.state<NestedScrollViewState>(
        find.byType(NestedScrollView),
      );
      final outer = nested.outerController.position;
      final innerPixels = position.pixels, outerPixels = outer.pixels;
      final answerElement = conversation.evaluate().single;
      final sheetElement = find.byType(AgentResultsSheet).evaluate().single;
      final mapElement = find.byType(MapCanvas).evaluate().single;
      final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
      composer.fillDraft('返回回答时仍保留的草稿', requestFocus: false);
      final task = ws.task, result = ws.result, epoch = ws.taskEpoch;
      final messages = List<AgentMessage>.of(ws.conversation);
      final reads = f.requests.length;
      await t.tap(find.byKey(const Key('now-social-button')));
      await t.pumpAndSettle();
      expect(find.byType(NowSocialEdge), findsOneWidget);
      expect(find.byType(AgentResultsSheet), findsNothing);
      expect(find.byType(AgentConversation), findsNothing);
      expect(nested.mounted, isTrue);
      expect(scrollState.mounted, isTrue);
      expect(
        find.byType(AgentResultsSheet, skipOffstage: false).evaluate().single,
        same(sheetElement),
      );
      expect(
        find.byType(AgentConversation, skipOffstage: false).evaluate().single,
        same(answerElement),
      );
      expect(
        t.state<NestedScrollViewState>(
          find.byType(NestedScrollView, skipOffstage: false),
        ),
        same(nested),
      );
      expect(position.pixels, closeTo(innerPixels, .5));
      expect(outer.pixels, closeTo(outerPixels, .5));
      expect(
        find
            .byKey(const Key('agent-sheet-map-toggle'), skipOffstage: false)
            .hitTestable(),
        findsNothing,
      );
      await t.tap(find.byKey(const Key('now-social-button')));
      await t.pumpAndSettle();
      expect(find.byType(NowSocialEdge), findsNothing);
      expect(
        find.byType(AgentResultsSheet).evaluate().single,
        same(sheetElement),
      );
      expect(conversation.evaluate().single, same(answerElement));
      expect(
        t.state<NestedScrollViewState>(find.byType(NestedScrollView)),
        same(nested),
      );
      expect(t.state<ScrollableState>(scroll), same(scrollState));
      expect(scrollState.position, same(position));
      expect(position.pixels, closeTo(innerPixels, .5));
      expect(outer.pixels, closeTo(outerPixels, .5));
      expect(ws.task, same(task));
      expect(ws.result, same(result));
      expect(ws.taskEpoch, epoch);
      expect(ws.conversation, orderedEquals(messages));
      expect(composer.draftText, '返回回答时仍保留的草稿');
      expect(
        t.state<AgentComposerState>(find.byType(AgentComposer)),
        same(composer),
      );
      expect(find.byType(MapCanvas).evaluate().single, same(mapElement));
      expect(
        f.requests.length,
        reads,
      ); // Guest social display has no native read.
      expect(find.byType(TextField), findsOneWidget);
    } finally {
      await f.finish(t);
    }
  });

  testWidgets('展开preview在键盘隐藏时原lease到期移除故事，并清除宿主展开展示', (t) async {
    final f = _Host()..lease = const Duration(seconds: 8);
    try {
      await f.mount(t);
      await f.select(t, const Offset(375, 530));
      await t.ensureVisible(find.text('看看公开动态'));
      await t.tap(find.text('看看公开动态'));
      await t.pumpAndSettle();
      expect(find.byType(NowPlaceStoryStrip), findsOneWidget);
      final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
      composer.fillDraft('期限届满期间的原草稿', requestFocus: true);
      t.view.viewInsets = const FakeViewPadding(bottom: 320);
      await t.pumpAndSettle();
      await t.pump(const Duration(seconds: 9));
      await t.pumpAndSettle();
      expect(
        find.byType(NowPlaceStoryStrip, skipOffstage: false),
        findsNothing,
      );
      expect(find.text('真实公开故事 1', skipOffstage: false), findsNothing);
      expect(
        find.text('带长中文与 Aberdeen café 混排的真实合成地点标题', skipOffstage: false),
        findsNothing,
      );
      composer.pauseEditing();
      t.view.resetViewInsets();
      await t.pumpAndSettle();
      expect(find.text('请重新打开地点'), findsOneWidget);
      f.canvas(t).onSelectedAnchor?.call((
        id: 'place:$workspacePlaceID',
        point: const Offset(375, 530),
      ));
      await t.pumpAndSettle();
      // Same right anchor: collapsed root geometry has left >16. This proves
      // its presentation flag did not retain the retired expanded setting.
      expect(
        t.getRect(find.byKey(const Key('now-place-preview-position'))).left,
        greaterThan(16),
      );
      expect(find.byType(NowPlaceStoryStrip), findsNothing);
      expect(composer.draftText, '期限届满期间的原草稿');
      expect(find.byType(TextField), findsOneWidget);
      f.assertCloseReachable(t);
    } finally {
      await f.finish(t);
    }
  });

  testWidgets('preview隐藏期间账号A-B-A永久退役旧故事与已捕获reader回调', (t) async {
    final f = _Host();
    try {
      await f.mount(t);
      await f.select(t, const Offset(375, 530));
      await t.ensureVisible(find.text('看看公开动态'));
      await t.tap(find.text('看看公开动态'));
      await t.pumpAndSettle();
      final oldPreview = t.widget<NowPlacePreview>(
        find.byType(NowPlacePreview),
      );
      final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
      composer.fillDraft('原账号草稿', requestFocus: true);
      t.view.viewInsets = const FakeViewPadding(bottom: 320);
      await t.pumpAndSettle();
      f.auth.changeIdentity(
        'Bearer synthetic-peer',
        nextOwner: '03000000-0000-4000-8000-000000000002',
      );
      f.auth.changeIdentity(null);
      await t.pumpAndSettle();
      expect(
        find.byType(NowPlaceStoryStrip, skipOffstage: false),
        findsNothing,
      );
      expect(find.text('真实公开故事 1', skipOffstage: false), findsNothing);
      oldPreview.onOpenPlace();
      await t.pumpAndSettle();
      expect(
        find.byType(NowPublicPlacePanel, skipOffstage: false),
        findsNothing,
      );
      composer.pauseEditing();
      t.view.resetViewInsets();
      await t.pumpAndSettle();
      expect(
        find.byType(NowPlaceStoryStrip, skipOffstage: false),
        findsNothing,
      );
      expect(
        find.text('带长中文与 Aberdeen café 混排的真实合成地点标题', skipOffstage: false),
        findsNothing,
      );
      expect(find.byType(TextField), findsOneWidget);
      expect(find.byType(AgentComposer), findsOneWidget);
    } finally {
      await f.finish(t);
    }
  });
}
