import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/content/public_intent_section.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'map_layers_api_test.dart' show mapTestBounds, mapTestItem;
import 'place_detail_sheet_test.dart'
    show workspacePlaceID, workspaceMomentID, workspacePlaceData;
import 'public_intent_identity_test.dart';
import 'public_moment_author_v2_test.dart' show publicMomentV2Data;
import 'place_history_controller_test.dart' show historyTestData;

class _NoTaskSource extends AgentTaskSource {
  int queries = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List activities,
    List places,
  ) async {
    queries++;
    throw StateError('Own profile navigation cannot submit a Now query.');
  }
}

// Original MapWorkspace, original public-content panel and original owner
// editor over a local HTTP double. This does not prove server ACL or real tiles.
class _OwnParent extends ProfileScenario {
  _OwnParent() {
    profiles[profileOwnerA]!['visibility'] = 'public';
  }
  final taskSource = _NoTaskSource();
  int mapReads = 0;
  Completer<http.Response>? pendingMap;

  @override
  Future<http.Response> reply(http.Request r) async {
    Map<String, dynamic>? data;
    if (r.url.path == '/v1/cities/profile-synthetic-city/map-layers') {
      calls.add(r);
      mapReads++;
      final hold = pendingMap;
      pendingMap = null;
      if (hold != null) return hold.future;
      final now = DateTime.now().toUtc();
      final item = mapTestItem('PLACE')
        ..['id'] = workspacePlaceID
        ..['entityRef'] = {'type': 'PLACE', 'id': workspacePlaceID}
        ..['detailRef'] = {'type': 'PLACE', 'id': workspacePlaceID};
      (item['anchor'] as Map)
        ..['placeId'] = workspacePlaceID
        ..['label'] = '本人合成记录地点';
      return profileJson({
        'schemaVersion': 'typed-map-layers-v1',
        'cityId': city.selectedCity.id,
        'scope': 'PUBLIC',
        'observedAt': now.toIso8601String(),
        'validUntil': now.add(const Duration(minutes: 2)).toIso8601String(),
        'truncated': false,
        'items': [item],
      });
    } else if (r.url.path == '/v1/places/$workspacePlaceID') {
      data = workspacePlaceData();
    } else if (r.url.path ==
        '/v1/places/$workspacePlaceID/social-history/moments') {
      expect(r.url.queryParameters, {'include': 'public-author'});
      final now = DateTime.now().toUtc();
      data = {
        'schemaVersion': 'place-social-history-page-v1',
        'placeId': workspacePlaceID,
        'cityId': city.selectedCity.id,
        'authorMode': 'PUBLIC_PROFILE',
        'windowDays': 30,
        'windowStart': now.subtract(const Duration(days: 30)).toIso8601String(),
        'windowEnd': now.toIso8601String(),
        'observedAt': now.toIso8601String(),
        'validUntil': now.add(const Duration(minutes: 2)).toIso8601String(),
        'sourceVersion': 'a' * 64,
        'count': 1,
        'items': [
          {
            'id': workspaceMomentID,
            'title': '公开分享',
            'excerpt': '公开的节选',
            'revision': 2,
            'publishedAt': now
                .subtract(const Duration(days: 1))
                .toIso8601String(),
            'authorId': profileOwnerA,
            'authorPublicProfile': {
              'accountId': profileOwnerA,
              'displayName': '甲已保存资料',
            },
          },
        ],
        'nextCursor': null,
      };
    } else if (r.url.path == '/v1/places/$workspacePlaceID/social-history') {
      // The existing PlaceDetailSheet requests original v1 history. Author
      // opt-in belongs to the subsequent public Moment read, not this request.
      expect(r.url.query, isEmpty);
      data = historyTestData();
    } else if (r.url.path == '/v1/moments/$workspaceMomentID') {
      data = publicMomentV2Data()
        ..['id'] = workspaceMomentID
        ..['placeId'] = workspacePlaceID
        ..['authorId'] = profileOwnerA
        ..['authorPublicProfile'] = {
          'accountId': profileOwnerA,
          'displayName': '甲已保存资料',
        };
    } else if (r.url.path == '/v1/accounts/$profileOwnerA/profile') {
      data = profiles[profileOwnerA]!;
    }
    if (data != null) {
      calls.add(r);
      expect(r.method, 'GET');
      return profileJson(data);
    }
    return super.reply(r);
  }

  MapCanvas canvas(WidgetTester t) =>
      t.widget<MapCanvas>(find.byType(MapCanvas));

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
    expect(canvas(t).layers!.error, isNull);
  }

  Future<void> openAuthor(WidgetTester t) async {
    // Desktop widget tests have no native map tiles. Invoke the current original
    // map-selection callback with its real source entity, not an obsolete label.
    final map = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    final moment = map.entities.singleWhere(
      (e) => e.id == 'place:$workspacePlaceID',
    );
    map.onEntitySelected!(moment);
    await t.pumpAndSettle();
    expect(find.byType(NowPlacePreview), findsOneWidget);
    final placeEntry = find.text('查看地点');
    await t.ensureVisible(placeEntry);
    await t.tap(placeEntry);
    await t.pumpAndSettle();
    expect(find.byType(NowPublicPlacePanel), findsOneWidget);
    expect(find.byType(PlaceDetailSheet), findsOneWidget);
    final place = find.text('查看公开动态');
    await t.scrollUntilVisible(
      place,
      180,
      scrollable: find
          .descendant(
            of: find.byType(PlaceDetailSheet),
            matching: find.byType(Scrollable),
          )
          .last,
    );
    await t.ensureVisible(place);
    await t.tap(place);
    await t.pumpAndSettle();
    final author = find.text('甲已保存资料').last;
    await t.ensureVisible(author);
    await t.tap(author);
    await t.pumpAndSettle();
    expect(find.byType(PublicPersonPage), findsOneWidget);
  }
}

void main() {
  testWidgets('原Now作者入口复用本人原编辑页，保存返回保留原地图与选择', (t) async {
    final s = _OwnParent();
    addTearDown(s.dispose);
    await s.mountParent(t);
    final mapState = t.state(find.byType(MapCanvas));
    final viewport = s.canvas(t).mapState;
    final workspace = s.canvas(t).workspace;
    await s.openAuthor(t);
    final selected = workspace.selectedEntityId;
    final page = t.widget<PublicPersonPage>(find.byType(PublicPersonPage));
    expect(page.onOpenOwnProfile, isNotNull);
    final edit = find.text('编辑我的资料与隐私');
    await t.ensureVisible(edit);
    await t.tap(edit);
    await t.pumpAndSettle();
    expect(find.byType(LegacyProfilePage), findsOneWidget);
    expect(find.byType(PublicIntentSection), findsOneWidget);
    await t.scrollUntilVisible(
      profileField('显示名称'),
      180,
      scrollable: find.byType(Scrollable).last,
    );
    await t.enterText(profileField('显示名称'), '本人原编辑器保存');
    await profileTap(t, '保存个人资料');
    await t.pumpAndSettle();
    final put = s.mutations.single;
    expect(put.method, 'PUT');
    expect(put.url.path, '/v1/me/profile');
    expect(put.url.host, 'profile.test');
    expect(put.headers['Authorization'], profileTokenA);
    expect((jsonDecode(put.body) as Map)['displayName'], '本人原编辑器保存');
    await t.pageBack();
    await t.pumpAndSettle();
    expect(find.byType(LegacyProfilePage), findsNothing);
    expect(find.byType(PublicPersonPage), findsOneWidget);
    expect(find.text('本人原编辑器保存'), findsOneWidget);
    expect(identical(t.state(find.byType(MapCanvas)), mapState), isTrue);
    expect(identical(s.canvas(t).mapState, viewport), isTrue);
    expect(workspace.selectedEntityId, selected);
    expect(s.taskSource.queries, 0);
    await s.unmount(t);
    expect(s.client.closed, isFalse);
  });

  testWidgets('当前公开来源重验失败不得从本人作者进入旧编辑目的地', (t) async {
    final s = _OwnParent();
    addTearDown(s.dispose);
    await s.mountParent(t);
    await s.openAuthor(t);
    final page = t.widget<PublicPersonPage>(find.byType(PublicPersonPage));
    expect(page.onOpenOwnProfile, isNotNull);
    s.pendingMap = Completer<http.Response>();
    final pending = s.pendingMap!;
    final open = page.onOpenOwnProfile!();
    await t.pump();
    pending.complete(http.Response('{}', 503));
    await t.pumpAndSettle();
    await open;
    expect(find.byType(LegacyProfilePage), findsNothing);
    expect(find.text('身份、地点或公开来源已变化，请返回地图重新核实。'), findsOneWidget);
    expect(s.mutations, isEmpty);
    expect(s.taskSource.queries, 0);
    await s.unmount(t);
  });

  testWidgets('本人入口重验等待期间owner ABA不能复活旧编辑导航', (t) async {
    final s = _OwnParent();
    addTearDown(s.dispose);
    await s.mountParent(t);
    await s.openAuthor(t);
    final page = t.widget<PublicPersonPage>(find.byType(PublicPersonPage));
    expect(page.onOpenOwnProfile, isNotNull);
    s.pendingMap = Completer<http.Response>();
    final pending = s.pendingMap!;
    final open = page.onOpenOwnProfile!();
    await t.pump();
    s.toB();
    await t.pump();
    s.toA();
    pending.complete(http.Response('{}', 503));
    await t.pumpAndSettle();
    await open;
    expect(find.byType(LegacyProfilePage), findsNothing);
    expect(s.mutations, isEmpty);
    expect(s.taskSource.queries, 0);
    await s.unmount(t);
  });
}
