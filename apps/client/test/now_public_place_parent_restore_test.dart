import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart' show NowFixtureCity;
import 'place_detail_sheet_test.dart'
    show workspacePlaceID, workspaceMomentID, workspacePlaceData, jsonReply;
import 'place_history_controller_test.dart' show historyTestData;
import 'map_layers_api_test.dart' show mapTestBounds, mapTestItem;

class _ReadOnlyTaskSource extends AgentTaskSource {
  int resolves = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    resolves++;
    throw StateError('Opening/closing public content must not submit a task.');
  }
}

/// Native MapWorkspace and its original map/controller/readers. The city has
/// no SDK tile configuration: this proves widget/state behavior, not real tiles.
class _ParentFixture {
  final auth = SeedTestAuth()..token = null;
  final city = NowFixtureCity();
  final source = _ReadOnlyTaskSource();
  final requests = <http.Request>[];
  int placeReads = 0,
      historyReads = 0,
      mapReads = 0,
      previewPageReads = 0,
      publicMomentReads = 0;
  late final client = MockClient((r) async {
    requests.add(r);
    if (r.method != 'GET') return http.Response('{}', 503);
    if (r.url.path == '/v1/cities/alpha/map-layers') {
      mapReads++;
      return http.Response.bytes(
        utf8.encode(jsonEncode({'data': mapData()})),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    }
    if (r.url.path == '/v1/places/$workspacePlaceID') {
      placeReads++;
      return jsonReply({'data': workspacePlaceData()});
    }
    if (r.url.path == '/v1/places/$workspacePlaceID/social-history/moments') {
      historyReads++;
      previewPageReads++;
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
      publicMomentReads++;
      return jsonReply({'data': _momentData(matches.single, author: author)});
    }
    if (r.url.path == '/v1/places/$workspacePlaceID/social-history') {
      historyReads++;
      // The stable reader retains its original summary v1/v2 route.
      // Preview pagination is separate above; no author is invented.
      final data = historyTestData();
      if (r.url.queryParameters['include'] == 'public-author') {
        data['schemaVersion'] = 'place-social-history-v2';
        for (final row in data['recentMoments'] as List) {
          row['authorId'] = null;
          row['authorPublicProfile'] = null;
        }
      }
      return jsonReply({'data': data});
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
    apiBaseUrl: 'https://parent-unit.test',
  );

  // Grouped-place list input remains public MOMENT records. This path opens
  // their place preview; independent map MOMENT navigation has separate tests.
  Map<String, dynamic> mapData() {
    final now = DateTime.now().toUtc();
    return {
      'schemaVersion': 'typed-map-layers-v1',
      'cityId': 'alpha',
      'scope': 'PUBLIC',
      'observedAt': now.toIso8601String(),
      'validUntil': now.add(const Duration(minutes: 2)).toIso8601String(),
      'truncated': false,
      'items': [
        for (var i = 0; i < 18; i++)
          (() {
            final suffix = (i + 1).toString().padLeft(12, '0');
            final moment = i == 8
                ? workspaceMomentID
                : '11110000-0000-4000-8000-$suffix';
            final place = i == 8
                ? workspacePlaceID
                : '22220000-0000-4000-8000-$suffix';
            final row = mapTestItem('MOMENT');
            row['id'] = moment;
            row['entityRef'] = {'type': 'MOMENT', 'id': moment};
            row['detailRef'] = {'type': 'MOMENT', 'id': moment};
            row['anchor']['placeId'] = place;
            row['anchor']['label'] = '入口地点${i.toString().padLeft(2, '0')}';
            return row;
          })(),
      ],
    };
  }

  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          agentTaskSource: source,
          seedClient: client,
          seedApiBaseUrl: 'https://parent-unit.test',
          placeDetailClient: client,
          placeDetailApiBaseUrl: 'https://parent-unit.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    // The real parent gets the same finite, public native projection through its
    // original API. No separate panel harness or manually assigned publicView.
    await canvas(t).layers!.load(city.selectedCity!.id, mapTestBounds);
    canvas(t).mapState.initializeViewport(mapTestBounds);
    await t.pumpAndSettle();
    expect(canvas(t).layers!.error, isNull);
    await t.tap(find.byKey(const Key('now-explore-handle')));
    await t.pumpAndSettle();
    expect(
      find.byType(NowPublicPlaceList, skipOffstage: false),
      findsOneWidget,
    );
    expect(canvas(t).layers!.items, hasLength(18));
    expect(find.text('入口地点08'), findsOneWidget);
  }

  MapCanvas canvas(WidgetTester t) =>
      t.widget<MapCanvas>(find.byType(MapCanvas));
  AgentWorkspaceController workspace(WidgetTester t) => canvas(t).workspace;
  Future<void> unmount(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    moments.dispose();
    city.dispose();
    auth.dispose();
    client.close();
  }

  void readsOnly() {
    expect(source.resolves, 0);
    expect(requests, isNotEmpty);
    expect(requests.every((r) => r.method == 'GET'), isTrue);
    expect(requests.any((r) => r.url.path.contains('/agent/')), isFalse);
  }
}

Finder _contextScroll() => find.descendant(
  of: find.byKey(
    const PageStorageKey('now-native-context-scroll'),
    skipOffstage: false,
  ),
  matching: find.byType(Scrollable, skipOffstage: false),
  skipOffstage: false,
);

Future<void> _openReaderFromPreview(WidgetTester t) async {
  expect(find.byType(NowPlacePreview), findsOneWidget);
  expect(find.byType(NowPublicPlacePanel), findsNothing);
  await t.ensureVisible(find.text('查看地点'));
  await t.tap(find.text('查看地点'));
  await t.pumpAndSettle();
  expect(find.byType(NowPlacePreview), findsNothing);
  expect(find.byType(NowPublicPlacePanel), findsOneWidget);
}

Future<void> _openFromList(WidgetTester t) async {
  await t.scrollUntilVisible(
    find.text('入口地点08'),
    150,
    scrollable: _contextScroll(),
  );
  await t.tap(find.text('入口地点08'));
  await t.pumpAndSettle();
  await _openReaderFromPreview(t);
  expect(find.byType(NowPublicPlacePanel), findsOneWidget);
}

void main() {
  testWidgets('真实Now列表打开关闭保留原地图范围与滚动且清除临时Moment选择', (t) async {
    final f = _ParentFixture();
    await f.mount(t);
    final ws = f.workspace(t), taskEpoch = ws.taskEpoch;
    final canvasElement = find.byType(MapCanvas).evaluate().single;
    final canvasState = t.state(find.byType(MapCanvas));
    final mapElement = find.byType(PublicCityMapView).evaluate().single;
    final mapState = t.state(find.byType(PublicCityMapView));
    final viewport = f.canvas(t).mapState;
    final contextElement = find
        .byKey(
          const PageStorageKey('now-native-context-scroll'),
          skipOffstage: false,
        )
        .evaluate()
        .single;
    final scroll = t.state<ScrollableState>(_contextScroll());
    final listState = t.widget<NowPublicPlaceList>(
      find.byType(NowPublicPlaceList, skipOffstage: false),
    );
    final itemIDs = listState.items.map((i) => i.mapID).toList();
    await t.scrollUntilVisible(
      find.text('入口地点08'),
      150,
      scrollable: _contextScroll(),
    );
    final offset = scroll.position.pixels;
    expect(offset, greaterThan(0));
    expect(ws.selectedEntityId, isNull);
    debugPrint(
      jsonEncode({
        'evidence': 'parent-place-context-scroll',
        'phase': 'before',
        'pixels': offset,
        'maxScrollExtent': scroll.position.maxScrollExtent,
        'taskEpoch': taskEpoch,
      }),
    );
    await t.tap(find.text('入口地点08'));
    await t.pumpAndSettle();
    final previewHistoryReads = f.historyReads;
    expect(previewHistoryReads, 1);
    expect(f.previewPageReads, 1);
    await _openReaderFromPreview(t);
    expect(find.byType(NowPublicPlacePanel), findsOneWidget);
    expect(ws.selectedEntityId, 'moment:$workspaceMomentID');
    expect(ws.preserveReplyCamera, isTrue);
    expect(
      t
          .widget<PublicCityMapView>(find.byType(PublicCityMapView))
          .preserveViewport,
      isTrue,
    );
    expect(find.byType(MapCanvas).evaluate().single, same(canvasElement));
    expect(t.state(find.byType(MapCanvas)), same(canvasState));
    expect(find.byType(PublicCityMapView).evaluate().single, same(mapElement));
    expect(t.state(find.byType(PublicCityMapView)), same(mapState));
    expect(find.byType(Scaffold), findsOneWidget);
    expect(
      find.byType(NowPublicPlaceList, skipOffstage: false),
      findsOneWidget,
    );
    expect(
      find
          .byKey(
            const PageStorageKey('now-native-context-scroll'),
            skipOffstage: false,
          )
          .evaluate()
          .single,
      same(contextElement),
    );
    expect(t.state<ScrollableState>(_contextScroll()), same(scroll));
    expect(scroll.position.pixels, closeTo(offset, .01));
    debugPrint(
      jsonEncode({
        'evidence': 'parent-place-context-scroll',
        'phase': 'during',
        'pixels': scroll.position.pixels,
        'maxScrollExtent': scroll.position.maxScrollExtent,
        'selectedEntityId': ws.selectedEntityId,
        'preserveViewport': ws.preserveReplyCamera,
      }),
    );
    expect(find.text('入口地点08').hitTestable(), findsNothing);
    expect(f.placeReads, 1);
    expect(f.historyReads - previewHistoryReads, 1);
    await t.tap(find.byTooltip('关闭内容返回地图'));
    await t.pumpAndSettle();
    expect(find.byType(NowPublicPlacePanel), findsNothing);
    expect(ws.selectedEntityId, isNull);
    expect(ws.preserveReplyCamera, isTrue);
    final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(view.selectedEntityId, isNull);
    expect(view.preserveViewport, isTrue);
    expect(find.byType(MapCanvas).evaluate().single, same(canvasElement));
    expect(t.state(find.byType(MapCanvas)), same(canvasState));
    expect(find.byType(PublicCityMapView).evaluate().single, same(mapElement));
    expect(t.state(find.byType(PublicCityMapView)), same(mapState));
    expect(f.canvas(t).mapState, same(viewport));
    expect(viewport.viewportBounds, mapTestBounds);
    expect(scroll.position.pixels, closeTo(offset, .01));
    expect(t.state<ScrollableState>(_contextScroll()), same(scroll));
    debugPrint(
      jsonEncode({
        'evidence': 'parent-place-context-scroll',
        'phase': 'after',
        'pixels': scroll.position.pixels,
        'maxScrollExtent': scroll.position.maxScrollExtent,
        'selectedEntityId': ws.selectedEntityId,
        'preserveViewport': view.preserveViewport,
      }),
    );
    expect(
      t
          .widget<NowPublicPlaceList>(
            find.byType(NowPublicPlaceList, skipOffstage: false),
          )
          .items
          .map((i) => i.mapID),
      itemIDs,
    );
    expect(find.text('入口地点08').hitTestable(), findsOneWidget);
    expect(ws.task, isNull);
    expect(ws.taskEpoch, taskEpoch);
    expect(ws.conversation, isEmpty);
    f.readsOnly();
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });

  testWidgets('原列表回调在同源当前已有选择时关闭恢复真实可见canonical ID', (t) async {
    final f = _ParentFixture();
    await f.mount(t);
    final list = t.widget<NowPublicPlaceList>(
      find.byType(NowPublicPlaceList, skipOffstage: false),
    );
    final selectedBefore = list.items.first.mapID;
    final target = list.items.singleWhere((i) => i.id == workspaceMomentID);
    final ws = f.workspace(t), taskEpoch = ws.taskEpoch;
    // Capture the real parent list callback, then exercise its current canonical
    // prior-selection branch. The UI tap path is separately covered above.
    ws.selectEntity(selectedBefore, preserveViewport: true);
    list.onOpen(target);
    await t.pumpAndSettle();
    await _openReaderFromPreview(t);
    expect(ws.selectedEntityId, target.mapID);
    expect(ws.preserveReplyCamera, isTrue);
    await t.tap(find.byTooltip('关闭内容返回地图'));
    await t.pumpAndSettle();
    expect(ws.selectedEntityId, selectedBefore);
    final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(view.selectedEntityId, selectedBefore);
    expect(view.entities.any((e) => e.id == selectedBefore), isTrue);
    expect(view.preserveViewport, isTrue);
    expect(ws.taskEpoch, taskEpoch);
    f.readsOnly();
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });

  testWidgets('关闭退役来源面板不恢复消失的旧选择或旧公开结果', (t) async {
    final f = _ParentFixture();
    await f.mount(t);
    final ws = f.workspace(t), epoch = ws.taskEpoch;
    await _openFromList(t);
    f.canvas(t).layers!.retire();
    await t.pumpAndSettle();
    expect(find.text('身份、地点或公开来源已变化，请返回地图重新核实。'), findsOneWidget);
    await t.tap(find.byTooltip('关闭内容返回地图'));
    await t.pumpAndSettle();
    expect(find.byType(NowPublicPlacePanel), findsNothing);
    expect(ws.selectedEntityId, isNull);
    expect(
      t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
      isEmpty,
    );
    expect(find.byType(NowPublicPlaceList, skipOffstage: false), findsNothing);
    expect(ws.taskEpoch, epoch);
    f.readsOnly();
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });

  testWidgets('关闭退役面板不覆盖新来源里用户已明确选择的另一个实体', (t) async {
    final f = _ParentFixture();
    await f.mount(t);
    final ws = f.workspace(t);
    await _openFromList(t);
    final layers = f.canvas(t).layers!;
    layers.retire();
    await layers.load('alpha', mapTestBounds);
    final nextSelection = layers.items.first.mapID;
    expect(nextSelection, isNot('moment:$workspaceMomentID'));
    ws.selectEntity(nextSelection, preserveViewport: true);
    final epoch = ws.taskEpoch;
    await t.pumpAndSettle();
    expect(find.text('身份、地点或公开来源已变化，请返回地图重新核实。'), findsOneWidget);
    await t.tap(find.byTooltip('关闭内容返回地图'));
    await t.pumpAndSettle();
    expect(ws.selectedEntityId, nextSelection);
    final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    expect(view.selectedEntityId, nextSelection);
    expect(view.entities.any((e) => e.id == nextSelection), isTrue);
    expect(ws.taskEpoch, epoch);
    f.readsOnly();
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });

  testWidgets('来源同epoch但原选择已不在当前图层时关闭仅清临时选择', (t) async {
    final f = _ParentFixture();
    await f.mount(t);
    final list = t.widget<NowPublicPlaceList>(
      find.byType(NowPublicPlaceList, skipOffstage: false),
    );
    final ws = f.workspace(t), taskEpoch = ws.taskEpoch;
    ws.selectEntity(list.items.first.mapID, preserveViewport: true);
    list.onOpen(list.items.singleWhere((i) => i.id == workspaceMomentID));
    await t.pumpAndSettle();
    await _openReaderFromPreview(t);
    final layers = f.canvas(t).layers!, sourceEpoch = layers.epoch;
    layers.toggle('MOMENT', false);
    expect(layers.epoch, sourceEpoch);
    await t.pumpAndSettle();
    await t.tap(find.byTooltip('关闭内容返回地图'));
    await t.pumpAndSettle();
    expect(ws.selectedEntityId, isNull);
    expect(ws.preserveReplyCamera, isTrue);
    expect(ws.taskEpoch, taskEpoch);
    expect(
      t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
      isEmpty,
    );
    f.readsOnly();
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });
}
