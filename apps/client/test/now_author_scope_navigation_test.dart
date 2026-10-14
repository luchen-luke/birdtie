import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/public_author_moments.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'map_layers_api_test.dart' show mapTestBounds, mapTestItem;
import 'place_detail_sheet_test.dart'
    show workspacePlaceID, workspaceMomentID, workspacePlaceData;
import 'public_intent_identity_test.dart';
import 'public_moment_author_v2_test.dart' show publicMomentV2Data;
import 'place_history_controller_test.dart' show historyTestData;

class _NoQueries extends AgentTaskSource {
  int queries = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List activities,
    List places,
  ) async {
    queries++;
    throw StateError('Public author browsing cannot create an Agent query.');
  }
}

class _Parent extends ProfileScenario {
  _Parent() {
    profiles[profileOwnerA]!['visibility'] = 'public';
  }
  final taskSource = _NoQueries();
  int authorReads = 0;
  bool authorEmpty = false, authorFailure = false, baseEmpty = false;
  Completer<http.Response>? pendingBase;
  Map<String, dynamic>? lastBaseWire;
  @override
  Future<http.Response> reply(http.Request r) async {
    Map<String, dynamic>? data;
    if (r.url.path == '/v1/cities/profile-synthetic-city/map-layers') {
      calls.add(r);
      if (r.method != 'GET') throw StateError('map read must be GET');
      final author = r.url.queryParameters['authorId'];
      if (author == null && pendingBase != null) {
        final held = pendingBase!;
        pendingBase = null;
        return held.future;
      }
      if (author != null) {
        authorReads++;
        if (author != profileOwnerA ||
            r.url.queryParameters['scope'] != 'public-author' ||
            r.headers['Authorization'] != profileTokenA) {
          throw StateError('author request target, scope or actor changed');
        }
        if (authorFailure) return http.Response('{}', 404);
      }
      final now = DateTime.now().toUtc();
      final item = mapTestItem('MOMENT')
        ..['id'] = workspaceMomentID
        ..['entityRef'] = {'type': 'MOMENT', 'id': workspaceMomentID}
        ..['detailRef'] = {'type': 'MOMENT', 'id': workspaceMomentID};
      (item['anchor'] as Map)
        ..['placeId'] = workspacePlaceID
        ..['label'] = '本人合成记录地点';
      data = {
        'schemaVersion': author == null
            ? 'typed-map-layers-v1'
            : 'typed-map-layers-v2',
        'cityId': city.selectedCity.id,
        'scope': author == null ? 'PUBLIC' : 'PUBLIC_AUTHOR',
        if (author != null) 'authorId': author,
        'observedAt': now.toIso8601String(),
        'validUntil': now.add(const Duration(minutes: 2)).toIso8601String(),
        'truncated': false,
        'items':
            (author != null && authorEmpty) || (author == null && baseEmpty)
            ? []
            : [item],
      };
      if (author == null) lastBaseWire = data;
      return profileJson(data);
    }
    if (r.url.path == '/v1/places/$workspacePlaceID')
      data = workspacePlaceData();
    if (r.url.path == '/v1/places/$workspacePlaceID/social-history')
      data = historyTestData();
    if (r.url.path == '/v1/moments/$workspaceMomentID') {
      data = publicMomentV2Data()
        ..['id'] = workspaceMomentID
        ..['placeId'] = workspacePlaceID
        ..['authorId'] = profileOwnerA
        ..['authorPublicProfile'] = {
          'accountId': profileOwnerA,
          'displayName': '甲已保存资料',
        };
    }
    if (r.url.path == '/v1/accounts/$profileOwnerA/profile')
      data = profiles[profileOwnerA]!;
    if (data != null) {
      calls.add(r);
      if (r.method != 'GET') throw StateError('public source read must be GET');
      return profileJson(data);
    }
    return super.reply(r);
  }

  MapCanvas canvas(WidgetTester t) => t.widget(find.byType(MapCanvas));
  Future<void> mountParent(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          auth: auth,
          city: city,
          moments: moments,
          agentTaskSource: taskSource,
          seedClient: client,
          seedApiBaseUrl: source.value.$2,
          placeDetailClient: client,
          placeDetailApiBaseUrl: source.value.$2,
        ),
      ),
    );
    await t.pumpAndSettle();
    await canvas(t).layers!.load(city.selectedCity.id, mapTestBounds);
    canvas(t).mapState.initializeViewport(mapTestBounds);
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('now-explore-handle')));
    await t.pumpAndSettle();
  }

  Future<void> openAuthor(WidgetTester t) async {
    // This is an individual map MOMENT, not the place-grouped public list.
    final map = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    map.onEntitySelected!(
      map.entities.singleWhere(
        (entity) => entity.id == 'moment:$workspaceMomentID',
      ),
    );
    await t.pumpAndSettle();
    expect(find.byType(NowPublicPlacePanel), findsOneWidget);
    expect(
      t.widget<PublicMomentPage>(find.byType(PublicMomentPage)).momentID,
      workspaceMomentID,
    );
    expect(calls.where((r) => r.url.path.startsWith('/v1/places/')), isEmpty);
    final author = find.text('甲已保存资料').last;
    await t.ensureVisible(author);
    await t.tap(author);
    await t.pumpAndSettle();
    expect(find.byType(PublicPersonPage), findsOneWidget);
  }

  Future<void> openScope(WidgetTester t) async {
    final button = find.text('当前范围的公开动态');
    await t.ensureVisible(button);
    await t.tap(button);
    await t.pumpAndSettle();
  }
}

void main() {
  testWidgets('关闭面板后旧作者callback不读取或复活隐藏范围', (t) async {
    final s = _Parent();
    addTearDown(s.dispose);
    await s.mountParent(t);
    await s.openAuthor(t);
    final panel = t.widget<NowPublicPlacePanel>(
      find.byType(NowPublicPlacePanel),
    );
    final old = panel.onOpenAuthorScope!;
    panel.onClose();
    await t.pumpAndSettle();
    final before = s.calls.length;
    expect(await old(profileOwnerA), isFalse);
    expect(s.calls.length, before);
    expect(s.canvas(t).layers!.authorID, isNull);
    expect(s.taskSource.queries, 0);
    expect(s.mutations, isEmpty);
    await s.unmount(t);
  });
  testWidgets('作者入口原来源GET挂起时关闭，迟到响应不得接续作者请求', (t) async {
    final s = _Parent();
    addTearDown(s.dispose);
    await s.mountParent(t);
    await s.openAuthor(t);
    final panel = t.widget<NowPublicPlacePanel>(
      find.byType(NowPublicPlacePanel),
    );
    final held = Completer<http.Response>();
    s.pendingBase = held;
    final opening = panel.onOpenAuthorScope!(profileOwnerA);
    await t.pump();
    panel.onClose();
    await t.pump();
    held.complete(profileJson(s.lastBaseWire!));
    await t.pumpAndSettle();
    expect(await opening, isFalse);
    expect(s.authorReads, 0);
    expect(s.canvas(t).layers!.authorID, isNull);
    expect(find.byType(NowPublicPlacePanel), findsNothing);
    expect(s.taskSource.queries, 0);
    expect(s.mutations, isEmpty);
    await s.unmount(t);
  });
  testWidgets('原Now作者入口→同集合列表与地图→点位详情→返回列表保留原地图任务状态', (t) async {
    final s = _Parent();
    addTearDown(s.dispose);
    await s.mountParent(t);
    final mapElement = find.byType(PublicCityMapView).evaluate().single;
    final mapState = t.state(find.byType(MapCanvas));
    final ws = s.canvas(t).workspace;
    final epoch = ws.taskEpoch;
    final history = List.of(ws.conversation);
    await s.openAuthor(t);
    await s.openScope(t);
    expect(find.byType(PublicAuthorMoments).hitTestable(), findsOneWidget);
    final layers = s.canvas(t).layers!;
    final row = layers.items.single;
    expect(layers.authorID, profileOwnerA);
    expect(s.authorReads, greaterThan(0));
    expect(
      t
          .widget<PublicCityMapView>(find.byType(PublicCityMapView))
          .entities
          .single
          .id,
      row.mapID,
    );
    await t.tap(find.text('查看作者地图'));
    await t.pumpAndSettle();
    final view = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    view.onEntitySelected!(view.entities.single);
    await t.pumpAndSettle();
    expect(ws.selectedEntityId, row.mapID);
    expect(find.byType(PublicMomentPage).hitTestable(), findsOneWidget);
    expect(
      t
          .widget<PublicMomentPage>(find.byType(PublicMomentPage).hitTestable())
          .momentID,
      row.detailID,
    );
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(find.byType(PublicAuthorMoments).hitTestable(), findsOneWidget);
    expect(find.byType(PublicCityMapView).evaluate().single, same(mapElement));
    expect(t.state(find.byType(MapCanvas)), same(mapState));
    expect(ws.task, isNull);
    expect(ws.taskEpoch, epoch);
    expect(ws.conversation, history);
    expect(s.taskSource.queries, 0);
    expect(s.mutations, isEmpty);
    await s.unmount(t);
  });
  testWidgets('作者有效空集合不补城市内容，返回TA后退出作者范围', (t) async {
    final s = _Parent()..authorEmpty = true;
    addTearDown(s.dispose);
    await s.mountParent(t);
    await s.openAuthor(t);
    await s.openScope(t);
    expect(find.text('当前范围暂无该作者可见的公开动态。'), findsOneWidget);
    expect(s.canvas(t).layers!.items, isEmpty);
    expect(
      t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).entities,
      isEmpty,
    );
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(s.canvas(t).layers!.authorID, isNull);
    expect(s.canvas(t).layers!.items, isNotEmpty);
    expect(s.taskSource.queries, 0);
    expect(s.mutations, isEmpty);
    await s.unmount(t);
  });
  testWidgets('作者404诚实显示不可用并退出失败范围，不创建Now查询', (t) async {
    final s = _Parent()..authorFailure = true;
    addTearDown(s.dispose);
    await s.mountParent(t);
    await s.openAuthor(t);
    await s.openScope(t);
    expect(find.text('作者当前范围的公开动态暂不可用，请返回重新打开。'), findsOneWidget);
    expect(find.byType(PublicAuthorMoments), findsNothing);
    expect(s.canvas(t).layers!.authorID, isNull);
    expect(s.taskSource.queries, 0);
    expect(s.mutations, isEmpty);
    await s.unmount(t);
  });
  for (final invalidation in [
    'author-aba',
    'account-aba',
    'base-withdrawal-aba',
  ]) {
    testWidgets('受控作者浏览后 $invalidation 不复活旧来源或旧作者callback', (t) async {
      final s = _Parent();
      addTearDown(s.dispose);
      await s.mountParent(t);
      await s.openAuthor(t);
      await s.openScope(t);
      expect(find.byType(PublicAuthorMoments).hitTestable(), findsOneWidget);
      final panel = t.widget<NowPublicPlacePanel>(
        find.byType(NowPublicPlacePanel),
      );
      final old = panel.onOpenAuthorScope!;
      final layers = s.canvas(t).layers!;
      expect(layers.authorID, profileOwnerA);
      if (invalidation == 'author-aba') {
        // External changes are not this panel's controlled navigation, even
        // when the same author is restored before another frame is painted.
        layers.leaveAuthor();
        await layers.loadAuthor(
          s.city.selectedCity.id,
          mapTestBounds,
          profileOwnerA,
        );
      } else if (invalidation == 'account-aba') {
        s.toB();
        s.toA();
      } else {
        s.baseEmpty = true;
        await layers.load(s.city.selectedCity.id, mapTestBounds);
        expect(layers.findBase('moment:$workspaceMomentID'), isNull);
        s.baseEmpty = false;
        await layers.load(s.city.selectedCity.id, mapTestBounds);
        expect(layers.findBase('moment:$workspaceMomentID'), isNotNull);
      }
      await t.pumpAndSettle();
      final before = s.calls.length;
      expect(panel.current(), isFalse);
      expect(await old(profileOwnerA), isFalse);
      expect(s.calls.length, before);
      expect(find.byType(PublicAuthorMoments).hitTestable(), findsNothing);
      expect(s.taskSource.queries, 0);
      expect(s.mutations, isEmpty);
      expect(t.takeException(), isNull);
      await s.unmount(t);
    });
  }
}
