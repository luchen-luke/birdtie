import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/config/birdtie_environment.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/saved_collection_append.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/chat_entity_router.dart';
import 'package:birdtie_client/src/workspace/community_page.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/public_business_page.dart';
import 'package:birdtie_client/src/workspace/public_organization_page.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:birdtie_client/src/workspace/route_draft_page.dart';
import 'package:birdtie_client/src/workspace/organization_map_location_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'chat_entity_router_test.dart' show ChatTestAuth;
import 'now_scope_recovery_test.dart'
    show NowFixtureCity, NowFixtureSource, nowTap;
import 'personal_collection_controller_test.dart'
    show collectionReceipt, collectionID;
import 'supplier_profile_api_test.dart' show publicSupplier;

const _org = 'ba000000-0000-4000-8000-000000000001';
const _business = 'ba000000-0000-4000-8000-000000000002';
const _community = 'ba000000-0000-4000-8000-000000000003';
const _placeA = 'ba000000-0000-4000-8000-000000000004';
const _placeB = 'ba000000-0000-4000-8000-000000000005';
const _savedA = 'ba000000-0000-4000-8000-000000000006';
const _savedB = 'ba000000-0000-4000-8000-000000000007';
const _oldPlace = 'ba000000-0000-4000-8000-000000000008';
const _privateNote = '仅本人私密备注，不能流入集合或地图';
const _draft = '还未发送的中文输入 English draft';
const _collectionTitle = '同城已存在的合成旅程';
const _base = 'https://batch-a-navigation.test';

http.Response _json(Object? value, [int status = 200]) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': value})),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

class _NavigationFixture {
  final auth = ChatTestAuth();
  final city = NowFixtureCity();
  late final source = NowFixtureSource(city);
  late final client = MockClient(reply);
  late final moments = PrivateMomentController(
    client: client,
    apiBaseUrl: _base,
    authorizationHeader: () => auth.authorizationHeader,
  );
  final calls = <http.Request>[];
  Map<String, dynamic>? appended;

  List<Map<String, dynamic>> get savedRows => [
    for (final p in [
      (_placeA, _savedA, '合成地点甲', 57.1, -2.1),
      (_placeB, _savedB, '合成地点乙', 57.102, -2.104),
    ])
      {
        'id': p.$2,
        'kind': 'place',
        'targetId': p.$1,
        'title': p.$3,
        'summary': '当前获准公开简介',
        'cityId': 'alpha',
        'cityName': '甲验收城市',
        'available': true,
        'note': _privateNote,
        'noteRevision': 0,
        'latitude': p.$4,
        'longitude': p.$5,
        'sourceExpiresAt': DateTime.now()
            .toUtc()
            .add(const Duration(minutes: 10))
            .toIso8601String(),
      },
  ];
  Map<String, dynamic> collection() =>
      collectionReceipt(
          owner: auth.accountID!,
          title: _collectionTitle,
          revision: appended == null ? 1 : 2,
        )
        ..['cityId'] = 'alpha'
        ..['entries'] = [
          {
            'placeId': _oldPlace,
            'placeName': '原顺序地点',
            'note': '保留原集合备注',
            'locationVisibility': 'hidden',
            'available': true,
            'sourceExpiresAt': DateTime.now()
                .toUtc()
                .add(const Duration(minutes: 10))
                .toIso8601String(),
          },
        ];

  Future<http.Response> reply(http.Request r) async {
    calls.add(r);
    // Async Mock callbacks may be invoked inside a tester pump guard.
    if (r.url.path.startsWith('/v1/me/saved') ||
        r.url.path.startsWith('/v1/me/follows') ||
        r.url.path.startsWith('/v1/me/personal-collections') ||
        r.url.path == '/v1/organizations/$_org' ||
        r.url.path == '/v1/businesses/$_business' ||
        r.url.path == '/v1/communities/$_community') {
      expectSync(r.headers['Authorization'], auth.authorizationHeader);
    }
    if (r.method == 'POST' && r.url.path.endsWith('/append-places')) {
      appended = jsonDecode(r.body) as Map<String, dynamic>;
      return _json({
        'operationId': appended!['operationId'],
        'kind': 'journey',
        'collectionId': collectionID,
        'appliedRevision': 2,
        'status': 'SUCCEEDED',
      });
    }
    if (r.method != 'GET') return _json(null, 500);
    switch (r.url.path) {
      case '/v1/me/follows/page':
        return _json({
          'items': [
            for (final target in [
              ('ORGANIZATION', _org, '合成关注组织'),
              ('BUSINESS', _business, '合成关注商家'),
              ('COMMUNITY', _community, '合成关注社群'),
            ])
              {
                'id': target.$2,
                'target': {'targetType': target.$1, 'targetId': target.$2},
                'label': target.$3,
                'createdAt': '2026-10-14T00:00:00Z',
              },
          ],
          'nextCursor': null,
        });
      case '/v1/me/conversations/page':
        return _json({'items': [], 'nextCursor': null, 'unreadCount': 0});
      case '/v1/me/ties/page':
      case '/v1/me/connection-requests/page':
        return _json({'items': [], 'nextCursor': null});
      case '/v1/me/saved/page':
        return _json({'items': savedRows, 'nextCursor': null});
      case '/v1/me/saved':
        return _json(savedRows);
      case '/v1/me/personal-collections':
        return _json({
          'items': r.url.queryParameters['kind'] == 'journey'
              ? [collection()]
              : [],
          'nextCursor': null,
        });
      case '/v1/me/personal-collections/journey/$collectionID':
        return _json(collection());
      case '/v1/organizations/$_org':
        return _json({
          'id': _org,
          'name': '原ID当前组织详情',
          'verificationStatus': 'verified',
          'upcomingActivities': [],
        });
      case '/v1/me/organizations/$_org/map-location/control':
        final readAt = DateTime.now().toUtc();
        return _json({
          'organizationId': _org,
          'authorityVersion': 1,
          'role': '',
          'canonical': null,
          'observedAt': readAt.toIso8601String(),
          'expiresAt': readAt
              .add(const Duration(seconds: 30))
              .toIso8601String(),
          'proposals': [
            {
              'id': 'ba000000-0000-4000-8000-000000000009',
              'organizationId': _org,
              'cityId': 'alpha',
              'latitude': 57.150019,
              'longitude': -2.090019,
              'status': 'PENDING',
              'version': 1,
              'canonicalRevision': 0,
              'submittedBy': 'ba000000-0000-4000-8000-000000000010',
              'canApprove': true,
              'canReject': true,
              'canWithdraw': false,
            },
          ],
        });
      case '/v1/businesses/$_business':
        return _json(
          publicSupplier(published: false)
            ..['id'] = _business
            ..['name'] = '原ID当前商家详情',
        );
      case '/v1/communities/$_community':
        return _json({
          'id': _community,
          'name': '原ID当前社群详情',
          'description': '隔离公开介绍',
          'visibility': 'public',
          'joinPolicy': 'open',
          'status': 'active',
          'memberCount': 0,
        });
      case '/v1/communities/$_community/activities':
        return _json([]);
      default:
        return _json([]);
    }
  }

  Future<void> mount(WidgetTester t) async {
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          auth: auth,
          city: city,
          moments: moments,
          agentTaskSource: source,
          seedClient: client,
          seedApiBaseUrl: _base,
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  MapCanvas canvas(WidgetTester t) =>
      t.widget<MapCanvas>(find.byType(MapCanvas));
  Future<void> saved(WidgetTester t) async {
    await nowTap(t, find.byTooltip('打开侧边栏'));
    await nowTap(t, find.text('我的内容'));
    await nowTap(t, find.text('收藏'));
    expect(find.byType(SavedPage), findsOneWidget);
    expect(calls.where((r) => r.url.path == '/v1/me/saved/page'), hasLength(1));
    expect(find.text('合成地点甲'), findsOneWidget);
    expect(find.text('合成地点乙'), findsOneWidget);
  }

  Future<void> follows(WidgetTester t) async {
    await nowTap(t, find.byTooltip('联系'));
    await nowTap(t, find.byKey(const Key('edge-all-conversations')));
    await nowTap(t, find.byKey(const ValueKey('edge-tab-contacts')));
    await nowTap(t, find.byKey(const ValueKey('edge-filter-follows')));
    expect(
      calls.where((r) => r.url.path == '/v1/me/follows/page'),
      hasLength(1),
    );
  }

  Finder get composerInput => find.descendant(
    of: find.byType(AgentComposer),
    matching: find.byType(TextField),
  );
  Future<void> unmount(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  }

  void dispose() {
    moments.dispose();
    source.dispose();
    city.dispose();
    auth.dispose();
    client.close();
  }
}

Future<void> _fixture(
  WidgetTester t,
  Future<void> Function(_NavigationFixture) body,
) async {
  expect(
    BirdtieEnvironment.apiBaseUrl,
    _base,
    reason:
        'MapWorkspace 的原 Saved/Follow transports 使用编译期地址。'
        '请以 --dart-define=BIRDTIE_API_BASE_URL=$_base 运行；全部请求仍由 MockClient 隔离。',
  );
  FlutterSecureStorage.setMockInitialValues({});
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  final f = _NavigationFixture();
  await http.runWithClient(() async {
    try {
      await f.mount(t);
      await body(f);
    } finally {
      await f.unmount(t);
      f.dispose();
    }
  }, () => MockClient(f.reply));
}

void main() {
  testWidgets(
    'batchB nonmember reviewer Follow canonical organization opens original map-control page without Agent and keeps Now map input',
    (t) async {
      await _fixture(t, (f) async {
        final canvas = f.canvas(t), mapState = t.state(find.byType(MapCanvas));
        await t.enterText(f.composerInput, _draft);
        await t.pumpAndSettle();
        final text = t.widget<TextField>(f.composerInput).controller!;
        await f.follows(t);
        await nowTap(t, find.text('合成关注组织'));
        expect(find.byType(PublicOrganizationPage), findsOneWidget);
        expect(find.text('组织 Agent 暂不可用。'), findsOneWidget);
        await t.ensureVisible(find.text('地图位置管理与审核'));
        await t.tap(find.text('地图位置管理与审核'));
        // The original secure recovery journal and streamed HTTP need actual
        // async turns before settling animations; no writes are substituted.
        for (var stage = 0; stage < 3; stage++) {
          await t.pump();
          await t.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 30)),
          );
        }
        await t.pumpAndSettle();
        final page = t.widget<OrganizationMapLocationPage>(
          find.byType(OrganizationMapLocationPage),
        );
        expect(page.organizationID, _org);
        expect(page.ownerID!(), f.auth.accountID);
        expect(
          page.cityID,
          isEmpty,
          reason:
              'Reviewer does not invent a city to read an existing proposal',
        );
        expect(page.apiBaseUrl, _base);
        expect(page.sourceCurrent!(), isTrue);
        expect(
          f.calls
              .where((r) => r.url.path.endsWith('/map-location/control'))
              .length,
          greaterThanOrEqualTo(2),
        );
        expect(f.calls.where((r) => r.method != 'GET'), isEmpty);
        expect(f.source.queries, isEmpty);
        final context = t.element(find.byType(OrganizationMapLocationPage));
        Navigator.of(context).pop();
        await t.pumpAndSettle();
        await t.binding.handlePopRoute();
        await t.pumpAndSettle();
        expect(identical(f.canvas(t), canvas), isTrue);
        expect(identical(t.state(find.byType(MapCanvas)), mapState), isTrue);
        expect(
          identical(t.widget<TextField>(f.composerInput).controller, text),
          isTrue,
        );
        expect(text.text, _draft);
        expect(t.takeException(), isNull);
      });
    },
  );
  testWidgets(
    'batchC route returns ordered points to same Now MapCanvas and leaves original input; close is zero domain write',
    (t) async {
      await _fixture(t, (f) async {
        final mapState = t.state(find.byType(MapCanvas)), canvas = f.canvas(t);
        await t.enterText(f.composerInput, _draft);
        await t.pumpAndSettle();
        final text = t.widget<TextField>(f.composerInput).controller!;
        final composer = t.state<AgentComposerState>(
          find.byType(AgentComposer),
        );
        composer.showNowMaterialMenu();
        await t.pumpAndSettle();
        await nowTap(t, find.text('多站路线草稿'));
        expect(find.byType(RouteDraftPage), findsOneWidget);
        final c = t
            .widget<RouteDraftPage>(find.byType(RouteDraftPage))
            .controller;
        expect(c.value.days.single.text, _draft);
        c.add(_placeA);
        c.add(_placeB);
        c.move(1, -1);
        await t.pumpAndSettle();
        await nowTap(t, find.text('当天地点上原地图'));
        expect(find.byType(RouteDraftPage), findsNothing);
        expect(identical(t.state(find.byType(MapCanvas)), mapState), true);
        expect(identical(f.canvas(t), canvas), true);
        expect(
          identical(t.widget<TextField>(f.composerInput).controller, text),
          true,
        );
        expect(text.text, _draft);
        expect(canvas.personalCollection!.value!.entities.map((e) => e.id), [
          'place:$_placeB',
          'place:$_placeA',
        ]);
        expect(canvas.personalCollection!.value!.title, contains('路线未计算'));
        expect(f.calls.where((r) => r.method != 'GET'), isEmpty);
        expect(f.source.queries, isEmpty);
        await nowTap(t, find.byTooltip('返回原地图'));
        composer.showNowMaterialMenu();
        await t.pumpAndSettle();
        await nowTap(t, find.text('多站路线草稿'));
        final restored = t
            .widget<RouteDraftPage>(find.byType(RouteDraftPage))
            .controller;
        expect(restored.value.days.single.places, [_placeB, _placeA]);
        await t.pageBack();
        await t.pumpAndSettle();
        expect(f.calls.where((r) => r.method != 'GET'), isEmpty);
        expect(text.text, _draft);
        expect(t.takeException(), isNull);
      });
    },
  );
  for (final target in [
    ('ORGANIZATION', _org, 'organization'),
    ('BUSINESS', _business, 'business'),
    ('COMMUNITY', _community, 'community'),
  ]) {
    testWidgets(
      'batchA navigation follow ${target.$1} opens canonical original ID',
      (t) async {
        await _fixture(t, (f) async {
          final map = t.state(find.byType(MapCanvas));
          await f.follows(t);
          await nowTap(
            t,
            find.byKey(ValueKey('edge-follow-${target.$1}-${target.$2}')),
          );
          final detail = t.widget<ChatEntityDetail>(
            find.byType(ChatEntityDetail),
          );
          expect(detail.type, target.$3);
          expect(detail.id, target.$2);
          expect(
            f.calls.where(
              (r) =>
                  r.method == 'GET' &&
                  r.url.path ==
                      '/v1/${const {'organization': 'organizations', 'business': 'businesses', 'community': 'communities'}[target.$3]}/${target.$2}',
            ),
            hasLength(1),
          );
          expect(switch (target.$3) {
            'organization' => find.byType(PublicOrganizationPage),
            'business' => find.byType(PublicBusinessPage),
            _ => find.byType(CommunityDetailPage),
          }, findsOneWidget);
          expect(f.calls.where((r) => r.method != 'GET'), isEmpty);
          await t.pageBack();
          await t.pumpAndSettle();
          expect(identical(t.state(find.byType(MapCanvas)), map), isTrue);
          expect(t.takeException(), isNull);
        });
      },
    );
  }
  testWidgets(
    'batchA navigation Saved selection appends to original existing private collection',
    (t) async {
      await _fixture(t, (f) async {
        await f.saved(t);
        final row = find.widgetWithText(ListTile, '合成地点甲');
        final checkbox = find.descendant(
          of: row,
          matching: find.byType(Checkbox),
        );
        await nowTap(t, checkbox);
        await nowTap(t, find.text('加入已有旅程或主题地图'));
        expect(find.byType(SavedCollectionAppendPage), findsOneWidget);
        await nowTap(t, find.text(_collectionTitle));
        expect(f.appended, isNull);
        expect(find.text('合成地点甲'), findsOneWidget);
        expect(find.textContaining('不复制私人收藏备注'), findsOneWidget);
        expect(
          t
              .widget<FilledButton>(find.widgetWithText(FilledButton, '确认加入'))
              .onPressed,
          isNull,
        );
        await nowTap(t, find.text('确认将这些地点加入此记录'));
        await nowTap(t, find.text('确认加入'));
        expect(f.appended!['expectedRevision'], 1);
        expect(f.appended!['cityId'], 'alpha');
        expect(f.appended!['entries'], [
          {'placeId': _placeA, 'note': '', 'locationVisibility': 'hidden'},
        ]);
        expect(jsonEncode(f.appended), isNot(contains(_privateNote)));
        final posts = f.calls.where((r) => r.method == 'POST').toList();
        expect(posts, hasLength(1));
        expect(
          posts.single.url.path,
          '/v1/me/personal-collections/journey/$collectionID/append-places',
        );
        expect(
          t
              .widget<SavedCollectionAppendPage>(
                find.byType(SavedCollectionAppendPage),
              )
              .controller
              .succeeded,
          isTrue,
        );
        expect(t.takeException(), isNull);
      });
    },
  );
  testWidgets(
    'batchA navigation Saved map keeps MapCanvas input viewport and compares original points',
    (t) async {
      await _fixture(t, (f) async {
        final mapState = t.state(find.byType(MapCanvas));
        final canvas = f.canvas(t);
        const bounds = MapBounds(
          west: -2.3,
          east: -2.0,
          south: 57.0,
          north: 57.2,
        );
        canvas.mapState.initializeViewport(bounds);
        await t.pumpAndSettle();
        await t.enterText(f.composerInput, _draft);
        await t.pumpAndSettle();
        final textController = t.widget<TextField>(f.composerInput).controller;
        await f.saved(t);
        await nowTap(t, find.text('已加载地点上图与比较'));
        expect(find.byType(SavedPage), findsNothing);
        expect(identical(t.state(find.byType(MapCanvas)), mapState), isTrue);
        expect(identical(f.canvas(t), canvas), isTrue);
        expect(
          identical(
            t.widget<TextField>(f.composerInput).controller,
            textController,
          ),
          isTrue,
        );
        expect(textController!.text, _draft);
        final projection = canvas.personalCollection!.value!;
        expect(projection.entities.map((e) => e.id).toList(), [
          'place:$_placeA',
          'place:$_placeB',
        ]);
        expect(
          projection.entities.every((e) => !e.subtitle.contains(_privateNote)),
          isTrue,
        );
        final publicMap = t.widget<PublicCityMapView>(
          find.byType(PublicCityMapView),
        );
        expect(publicMap.preserveViewport, isTrue);
        // This dispatches the actual provider callback on the real map subtree.
        // It is widget routing evidence, not a native OS map-touch claim.
        publicMap.onEntitySelected!(projection.entities.first);
        await t.pumpAndSettle();
        t
            .widget<PublicCityMapView>(find.byType(PublicCityMapView))
            .onEntitySelected!(projection.entities.last);
        await t.pumpAndSettle();
        expect(canvas.workspace.preserveReplyCamera, isTrue);
        expect(canvas.mapState.viewportBounds, bounds);
        await nowTap(t, find.byTooltip('比较已选收藏地点'));
        expect(find.text('比较收藏地点'), findsOneWidget);
        expect(find.text('合成地点甲'), findsOneWidget);
        expect(find.text('合成地点乙'), findsOneWidget);
        expect(f.calls.where((r) => r.method != 'GET'), isEmpty);
        expect(f.source.queries, isEmpty);
        await t.binding.handlePopRoute();
        await t.pumpAndSettle();
        await nowTap(t, find.byTooltip('返回原地图'));
        expect(canvas.personalCollection!.value, isNull);
        expect(identical(t.state(find.byType(MapCanvas)), mapState), isTrue);
        expect(textController.text, _draft);
        expect(canvas.mapState.viewportBounds, bounds);
        expect(f.source.queries, isEmpty);
        expect(t.takeException(), isNull);
      });
    },
  );
}
