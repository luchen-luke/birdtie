import 'dart:async';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'place_detail_sheet_test.dart'
    show workspacePlaceID, workspaceMomentID, workspacePlaceData, jsonReply;
import 'place_history_controller_test.dart' show historyTestData;
import 'public_moment_api_test.dart' show momentReply;
import 'public_moment_author_v2_test.dart'
    show publicMomentV2Data, publicAuthorTestID;

const _otherPlaceID = '33333333-3333-4333-8333-333333333333';
const _otherMomentID = '44444444-4444-4444-8444-444444444444';
const _retired = '身份、地点或公开来源已变化，请返回地图重新核实。';

TypedMapItem _mapItem({
  String kind = 'MOMENT',
  String id = workspaceMomentID,
  String place = workspacePlaceID,
  String label = '入口地点',
}) => TypedMapItem(
  kind: kind,
  id: id,
  title: '合成公开地图记录',
  detailType: kind,
  detailID: id,
  placeID: place,
  anchorLabel: label,
  sourceVersion: 'synthetic-current-source-v1',
  latitude: 57.1,
  longitude: -2.1,
);

class _ExistingTaskSource extends AgentTaskSource {
  int resolves = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    resolves++;
    return AgentResult(
      entities: const [],
      activities: const [],
      places: const [],
      note: '仅测试既有任务的原控制器；没有供应商调用。',
    );
  }
}

class _PanelTransport {
  final requests = <http.Request>[];
  int placeReads = 0, historyReads = 0, momentReads = 0, profileReads = 0;
  bool authorHidden = false, momentUnavailable = false;
  Completer<http.Response>? momentPending;

  Map<String, dynamic> momentData() => publicMomentV2Data(hidden: authorHidden)
    ..['id'] = workspaceMomentID
    ..['placeId'] = workspacePlaceID
    ..['placeName'] = '入口地点'
    ..['title'] = '当前公开动态第$momentReads次'
    ..['body'] = List.filled(32, '本人公开的合成测试内容。').join('\n');

  late final client = MockClient((r) async {
    requests.add(r);
    if (r.method != 'GET') return http.Response('{}', 503);
    switch (r.url.path) {
      case '/v1/places/$workspacePlaceID':
        placeReads++;
        return jsonReply({
          'data': workspacePlaceData()
            // Both original and public-content-first layouts must really
            // scroll before opening the visible original history action.
            ..['summary'] = List.filled(40, '合成地点描述，用于滚动回归。').join('\n'),
        });
      case '/v1/places/$workspacePlaceID/social-history':
        historyReads++;
        final history = historyTestData();
        ((history['recentMoments'] as List).single as Map)['title'] =
            '当前地点公开记录第$historyReads次';
        return jsonReply({'data': history});
      case '/v1/places/$workspacePlaceID/activities':
      case '/v1/me/moments':
        return jsonReply({'data': []});
      case '/v1/places/$workspacePlaceID/venue':
        return http.Response('{}', 404);
      case '/v1/moments/$workspaceMomentID':
        momentReads++;
        if (momentPending case final pending?) {
          momentPending = null;
          return pending.future;
        }
        if (momentUnavailable) return http.Response('{}', 404);
        expect(r.url.queryParameters, {'include': 'public-author'});
        return momentReply(momentData());
      case '/v1/accounts/$publicAuthorTestID/profile':
        profileReads++;
        return jsonReply({
          'data': {
            'accountId': publicAuthorTestID,
            'displayName': '当前公开作者主页',
            'visibility': 'public',
            'bio': '本人公开简介，合成测试。',
          },
        });
      default:
        return http.Response('{}', 404);
    }
  });
}

/// Real original map/reader widgets, with only local HTTP doubles and no tiles.
/// The parent binds its source and identity fence exactly once for this panel.
class _PanelFixture {
  _PanelFixture() {
    auth.token = null;
    workspace = AgentWorkspaceController(source: source);
    city = PublicCityController(
      client: transport.client,
      apiBaseUrl: 'https://unit.test',
    );
    discovery = NowDiscoveryController(
      authorizationHeader: () => auth.authorizationHeader,
      client: transport.client,
      apiBaseUrl: 'https://unit.test',
    );
    viewport.initializeViewport(
      const MapBounds(west: -2.2, south: 57, east: -2, north: 57.2),
    );
    changes = Listenable.merge([auth, workspaceID, sourceCurrent]);
    map = MapCanvas(
      key: const Key('original-map'),
      city: city,
      workspace: workspace,
      mapState: viewport,
      discovery: discovery,
      onInitialViewport: (_) {},
      onMapUnavailable: (_) {},
    );
    panel = NowPublicPlacePanel(
      key: const Key('bound-public-panel'),
      item: _mapItem(),
      auth: auth,
      identityChanges: changes,
      workspaceID: () => workspaceID.value,
      current: () =>
          sourceCurrent.value &&
          auth.authorizationHeader == null &&
          workspaceID.value == null,
      revalidate: () async {
        revalidations++;
        if (verifyPending case final pending?) {
          verifyPending = null;
          return pending.future;
        }
        if (verifyError) throw StateError('synthetic source reread failed');
        if (withdrawOnVerify) sourceCurrent.value = false;
        return verifyValid;
      },
      onClose: () {
        closes++;
        visible.value = false;
      },
      onOpenActivity: (_) => activityOpens++,
      apiBaseUrl: 'https://unit.test',
      client: transport.client,
    );
  }

  final auth = SeedTestAuth();
  final transport = _PanelTransport();
  final source = _ExistingTaskSource();
  final sourceCurrent = ValueNotifier(true);
  final workspaceID = ValueNotifier<String?>(null);
  final visible = ValueNotifier(true);
  final composer = TextEditingController(text: '原会话未发送草稿');
  final viewport = MapViewportState();
  late final Listenable changes;
  late final PublicCityController city;
  late final AgentWorkspaceController workspace;
  late final NowDiscoveryController discovery;
  late final MapCanvas map;
  late final NowPublicPlacePanel panel;
  int revalidations = 0, closes = 0, activityOpens = 0;
  bool verifyValid = true, verifyError = false, withdrawOnVerify = false;
  Completer<bool>? verifyPending;

  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await workspace.submit('已有合成任务', [], [], cityID: 'synthetic-city');
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ValueListenableBuilder<bool>(
            valueListenable: visible,
            builder: (_, value, _) => Stack(
              children: [
                Positioned.fill(child: map),
                Positioned(
                  top: 0,
                  left: 0,
                  right: 0,
                  child: TextField(
                    key: const Key('existing-composer'),
                    controller: composer,
                  ),
                ),
                if (value)
                  Positioned(
                    top: 60,
                    left: 0,
                    right: 0,
                    bottom: 0,
                    child: panel,
                  ),
              ],
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  Future<void> revealMomentAction(WidgetTester t) async {
    await t.scrollUntilVisible(
      find.text('查看公开动态'),
      180,
      scrollable: _placeScrollable(),
    );
    await t.ensureVisible(find.text('查看公开动态'));
    await t.pumpAndSettle();
  }

  Future<void> openMoment(WidgetTester t) async {
    await revealMomentAction(t);
    await t.tap(find.text('查看公开动态'));
    await t.pumpAndSettle();
  }

  Future<void> openAuthor(WidgetTester t) async {
    await openMoment(t);
    await t.tap(find.text('公开作者名字'));
    await t.pumpAndSettle();
  }

  void expectReadOnly() {
    expect(transport.requests, isNotEmpty);
    expect(transport.requests.every((r) => r.method == 'GET'), isTrue);
    expect(transport.requests.every((r) => r.url.host == 'unit.test'), isTrue);
    expect(
      transport.requests.where(
        (r) => r.url.path.contains('/agent') || r.url.path.contains('/tasks'),
      ),
      isEmpty,
    );
    expect(
      source.resolves,
      1,
      reason: 'navigation/close must not submit another task',
    );
    expect(activityOpens, 0);
  }

  void dispose() {
    auth.dispose();
    city.dispose();
    workspace.dispose();
    discovery.dispose();
    viewport.dispose();
    sourceCurrent.dispose();
    workspaceID.dispose();
    visible.dispose();
    composer.dispose();
    transport.client.close();
  }
}

Finder _placeScrollable() => find
    .descendant(
      of: find.byType(PlaceDetailSheet),
      matching: find.byType(Scrollable),
    )
    .first;
Finder _momentScrollable() => find
    .descendant(
      of: find.byType(PublicMomentPage),
      matching: find.byType(Scrollable),
    )
    .first;

void main() {
  testWidgets('地点分组仅当前MOMENT集合，计数与truncated不冒充全站到访', (t) async {
    final a = _mapItem(), b = _mapItem(id: _otherMomentID);
    final c = _mapItem(
      id: publicAuthorTestID,
      place: _otherPlaceID,
      label: '第二地点',
    );
    TypedMapItem? opened;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: NowPublicPlaceList(
            items: [
              a,
              _mapItem(kind: 'PLACE', id: workspacePlaceID),
              b,
              _mapItem(kind: 'ACTIVITY', id: 'activity'),
              c,
            ],
            truncated: true,
            onOpen: (value) => opened = value,
          ),
        ),
      ),
    );
    expect(find.byType(ListTile), findsNWidgets(2));
    expect(find.text('此地图结果中有 2 条公开记录'), findsOneWidget);
    expect(find.text('此地图结果中有 1 条公开记录'), findsOneWidget);
    expect(find.text('当前地图范围 · 不代表作者现在位于这里'), findsOneWidget);
    expect(find.text('当前范围内容较多，仅展示已加载的地图结果；可缩小地图范围。'), findsOneWidget);
    expect(find.textContaining('位作者'), findsNothing);
    await t.tap(find.text('入口地点'));
    expect(opened, same(a));
    await t.tap(find.text('第二地点'));
    expect(opened, same(c));
  });

  testWidgets('无MOMENT的范围诚实空集，不从PLACE或ACTIVITY造公开记录', (t) async {
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: NowPublicPlaceList(
            items: [
              _mapItem(kind: 'PLACE'),
              _mapItem(kind: 'ACTIVITY'),
            ],
            truncated: false,
            onOpen: (_) => fail('empty list must not open a place'),
          ),
        ),
      ),
    );
    expect(find.byType(ListTile), findsNothing);
    expect(find.text('当前范围暂无可见的公开地点动态。'), findsOneWidget);
    expect(find.textContaining('条公开记录'), findsNothing);
    expect(find.textContaining('内容较多'), findsNothing);
  });

  testWidgets('同一原Map与Scaffold中地点动态作者逐层返回，原任务草稿视口不变', (t) async {
    final f = _PanelFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final mapElement = find.byType(MapCanvas).evaluate().single;
    final mapState = t.state(find.byType(MapCanvas));
    final cityMapElement = find.byType(PublicCityMapView).evaluate().single;
    final task = f.workspace.task, epoch = f.workspace.taskEpoch;
    final conversation = List<AgentMessage>.of(f.workspace.conversation);
    final viewport = f.viewport.viewportBounds;
    expect(
      t
          .widget<PlaceDetailSheet>(find.byType(PlaceDetailSheet))
          .publicContentFirst,
      isTrue,
      reason:
          'only the Now panel opts the original public history into its first content lane',
    );
    void stableParent() {
      expect(find.byType(MapCanvas), findsOneWidget);
      expect(find.byType(PublicCityMapView), findsOneWidget);
      expect(find.byType(Scaffold), findsOneWidget);
      expect(find.byType(AppBar), findsNothing);
      expect(find.byType(MapCanvas).evaluate().single, same(mapElement));
      expect(t.state(find.byType(MapCanvas)), same(mapState));
      expect(
        find.byType(PublicCityMapView).evaluate().single,
        same(cityMapElement),
      );
      expect(f.workspace.task, same(task));
      expect(f.workspace.taskEpoch, epoch);
      expect(f.workspace.conversation, conversation);
      expect(f.composer.text, '原会话未发送草稿');
      expect(f.viewport.viewportBounds, viewport);
      expect(
        find.text('地点页面连接已变更，请返回后重新打开。', skipOffstage: false),
        findsNothing,
        reason:
            'ordinary panel rebuilds must keep the original reader connection',
      );
    }

    stableParent();
    await f.openMoment(t);
    expect(find.text('公开动态'), findsOneWidget);
    expect(find.byType(PublicMomentPage), findsOneWidget);
    stableParent();
    await t.tap(find.text('公开作者名字'));
    await t.pumpAndSettle();
    expect(find.text('作者主页'), findsOneWidget);
    expect(find.byType(PublicPersonPage), findsOneWidget);
    expect(find.text('当前公开作者主页'), findsOneWidget);
    stableParent();
    final momentReads = f.transport.momentReads;
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(f.transport.momentReads, greaterThan(momentReads));
    expect(find.text('当前公开动态第${f.transport.momentReads}次'), findsOneWidget);
    expect(find.text('当前公开作者主页'), findsNothing);
    final placeReads = f.transport.placeReads,
        historyReads = f.transport.historyReads;
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(f.transport.placeReads, greaterThan(placeReads));
    expect(f.transport.historyReads, greaterThan(historyReads));
    expect(find.text('地点与公开动态'), findsOneWidget);
    stableParent();
    expect(f.revalidations, 4);
    await t.tap(find.byTooltip('关闭内容返回地图'));
    await t.pumpAndSettle();
    expect(f.closes, 1);
    expect(find.byType(NowPublicPlacePanel), findsNothing);
    stableParent();
    f.expectReadOnly();
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('back重读新版本同时PageStorage恢复地点和动态滚动', (t) async {
    final f = _PanelFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.revealMomentAction(t);
    await t.pumpAndSettle();
    final placeOffset = t
        .state<ScrollableState>(_placeScrollable())
        .position
        .pixels;
    expect(placeOffset, greaterThan(0));
    final initialPlace = t.state<ScrollableState>(_placeScrollable());
    debugPrint(
      'PANEL_SCROLL place-before: pixels=$placeOffset '
      'max=${initialPlace.position.maxScrollExtent} '
      'stored=${PageStorage.maybeOf(initialPlace.context)?.readState(initialPlace.context)} '
      'reads=${f.transport.placeReads}/${f.transport.historyReads}',
    );
    await f.openMoment(t);
    final openAuthor = t
        .widget<ListTile>(
          find.descendant(
            of: find.byType(PublicMomentPage),
            matching: find.byType(ListTile),
          ),
        )
        .onTap!;
    await t.drag(_momentScrollable(), const Offset(0, -190));
    await t.pumpAndSettle();
    final momentOffset = t
        .state<ScrollableState>(_momentScrollable())
        .position
        .pixels;
    expect(momentOffset, greaterThan(0));
    final momentScroll = t.state<ScrollableState>(_momentScrollable());
    debugPrint(
      'PANEL_SCROLL before-author: pixels=$momentOffset '
      'max=${momentScroll.position.maxScrollExtent} '
      'stored=${PageStorage.maybeOf(momentScroll.context)?.readState(momentScroll.context)}',
    );
    // Retain the real tile callback before the lazy list scrolls it off-screen.
    // Its original reader still checks current identity before navigating.
    openAuthor();
    await t.pumpAndSettle();
    final hiddenMoment = t.state<ScrollableState>(
      find
          .descendant(
            of: find.byType(PublicMomentPage, skipOffstage: false),
            matching: find.byType(Scrollable, skipOffstage: false),
          )
          .first,
    );
    debugPrint(
      'PANEL_SCROLL during-author: pixels=${hiddenMoment.position.pixels} '
      'max=${hiddenMoment.position.maxScrollExtent} '
      'stored=${PageStorage.maybeOf(hiddenMoment.context)?.readState(hiddenMoment.context)}',
    );
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    final returnedMoment = t.state<ScrollableState>(_momentScrollable());
    debugPrint(
      'PANEL_SCROLL after-back: pixels=${returnedMoment.position.pixels} '
      'max=${returnedMoment.position.maxScrollExtent} '
      'stored=${PageStorage.maybeOf(returnedMoment.context)?.readState(returnedMoment.context)} '
      'momentReads=${f.transport.momentReads}',
    );
    expect(
      t.state<ScrollableState>(_momentScrollable()).position.pixels,
      closeTo(momentOffset, .1),
    );
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    final returnedPlace = t.state<ScrollableState>(_placeScrollable());
    debugPrint(
      'PANEL_SCROLL place-after: pixels=${returnedPlace.position.pixels} '
      'max=${returnedPlace.position.maxScrollExtent} '
      'stored=${PageStorage.maybeOf(returnedPlace.context)?.readState(returnedPlace.context)} '
      'reads=${f.transport.placeReads}/${f.transport.historyReads}',
    );
    expect(
      t.state<ScrollableState>(_placeScrollable()).position.pixels,
      closeTo(placeOffset, .1),
    );
    f.expectReadOnly();
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });

  for (final mode in ['withdrawn', 'read-error']) {
    testWidgets('作者层back先重核$mode，退役旧地点动态缓存不能回生', (t) async {
      final f = _PanelFixture();
      addTearDown(f.dispose);
      await f.mount(t);
      await f.openAuthor(t);
      final verifies = f.revalidations;
      if (mode == 'withdrawn') {
        f.withdrawOnVerify = true;
        f.verifyValid = false;
      } else {
        f.verifyError = true;
      }
      await t.tap(find.byTooltip('返回上一层'));
      await t.pumpAndSettle();
      expect(f.revalidations, verifies + 1);
      expect(find.text(_retired), findsOneWidget);
      expect(find.byType(PublicPersonPage), findsNothing);
      expect(find.byType(PublicMomentPage), findsNothing);
      expect(find.byType(PlaceDetailSheet), findsNothing);
      f.sourceCurrent.value = true;
      f.verifyError = false;
      f.verifyValid = true;
      await t.pumpAndSettle();
      expect(find.text(_retired), findsOneWidget);
      await t.tap(find.byTooltip('关闭内容返回地图'));
      await t.pumpAndSettle();
      expect(f.closes, 1);
      f.expectReadOnly();
      await t.pumpWidget(const SizedBox());
    });
  }

  for (final mode in ['source', 'account', 'workspace']) {
    testWidgets('$mode ABA永久退役绑定面板，不恢复原已读作者/动态', (t) async {
      final f = _PanelFixture();
      addTearDown(f.dispose);
      await f.mount(t);
      await f.openAuthor(t);
      if (mode == 'source') {
        f.sourceCurrent.value = false;
        f.sourceCurrent.value = true;
      } else if (mode == 'account') {
        f.auth.changeIdentity('Bearer different-account');
        f.auth.changeIdentity(null);
      } else {
        f.workspaceID.value = 'synthetic-org';
        f.workspaceID.value = null;
      }
      await t.pumpAndSettle();
      expect(find.text(_retired), findsOneWidget);
      expect(find.text('当前公开作者主页'), findsNothing);
      expect(find.byType(PublicMomentPage), findsNothing);
      expect(find.byType(PlaceDetailSheet), findsNothing);
      expect(find.byTooltip('返回上一层'), findsNothing);
      f.expectReadOnly();
      await t.pumpWidget(const SizedBox());
    });
  }

  testWidgets('revalidate晚到true不能在source ABA后打开旧动态', (t) async {
    final f = _PanelFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final pending = Completer<bool>();
    f.verifyPending = pending;
    await f.revealMomentAction(t);
    await t.tap(find.text('查看公开动态'));
    await t.pump();
    f.sourceCurrent.value = false;
    f.sourceCurrent.value = true;
    await t.pump();
    pending.complete(true);
    await t.pumpAndSettle();
    expect(find.text(_retired), findsOneWidget);
    expect(find.byType(PublicMomentPage), findsNothing);
    expect(f.transport.momentReads, 0);
    f.expectReadOnly();
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('公开动态迟到响应在来源退役后不能恢复正文或作者', (t) async {
    final f = _PanelFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final pending = Completer<http.Response>();
    f.transport.momentPending = pending;
    await f.revealMomentAction(t);
    await t.tap(find.text('查看公开动态'));
    await t.pump();
    await t.pump();
    expect(f.transport.momentReads, 1);
    f.sourceCurrent.value = false;
    await t.pump();
    pending.complete(
      momentReply(f.transport.momentData()..['title'] = '不应回生的旧动态'),
    );
    await t.pumpAndSettle();
    expect(find.text(_retired), findsOneWidget);
    expect(find.text('不应回生的旧动态'), findsNothing);
    expect(find.text('公开作者名字'), findsNothing);
    f.expectReadOnly();
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('当前来源仍在但动态已撤回，back重读后不显示缓存正文', (t) async {
    final f = _PanelFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.openAuthor(t);
    f.transport.momentUnavailable = true;
    final reads = f.transport.momentReads;
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(f.transport.momentReads, greaterThan(reads));
    expect(find.text('这条动态已撤回、不可访问，或暂时无法读取。'), findsOneWidget);
    expect(find.textContaining('当前公开动态第'), findsNothing);
    expect(find.text('查看作者主页'), findsNothing);
    f.expectReadOnly();
    await t.pumpWidget(const SizedBox());
  });

  testWidgets('关闭待重核面板后迟到结果无效，关闭不提交新任务或provider', (t) async {
    final f = _PanelFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final task = f.workspace.task, epoch = f.workspace.taskEpoch;
    final pending = Completer<bool>();
    f.verifyPending = pending;
    await f.revealMomentAction(t);
    await t.tap(find.text('查看公开动态'));
    await t.pump();
    await t.tap(find.byTooltip('关闭内容返回地图'));
    await t.pump();
    expect(find.byType(NowPublicPlacePanel), findsNothing);
    pending.complete(true);
    await t.pumpAndSettle();
    expect(f.closes, 1);
    expect(f.workspace.task, same(task));
    expect(f.workspace.taskEpoch, epoch);
    expect(f.transport.momentReads, 0);
    expect(find.byType(MapCanvas), findsOneWidget);
    f.expectReadOnly();
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });
}
