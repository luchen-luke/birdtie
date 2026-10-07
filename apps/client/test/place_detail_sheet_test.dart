import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'profile_completion_sheet_test.dart' show completeSeed;
import 'agent_seed_controller_test.dart' show seedResponse;
import 'moment_publication_controller_test.dart' show publicationTestPreview;
import 'place_history_controller_test.dart' show historyTestData;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'entity_action_contract_test.dart' show actionWire;

http.Response jsonReply(Object value, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode(value)), status);

const workspacePlaceID = '22222222-2222-4222-8222-222222222222';
const workspaceMomentID = '11111111-1111-4111-8111-111111111111';
Map<String, dynamic> workspacePlaceData() => {
  'id': workspacePlaceID,
  'name': '入口地点',
  'source': {'label': '本地合成来源'},
  'location': {'precision': 'none'},
};
Map<String, dynamic> workspaceOwnData(String title) => {
  'id': workspaceMomentID,
  'placeId': workspacePlaceID,
  'title': title,
  'body': '私人正文',
  'revision': 3,
  'status': 'draft',
  'visibility': 'private',
  'locationPrecision': 'place',
};

class _WorkspacePlaceTransport {
  final calls = <String>[];
  Completer<http.Response>? ownPending;
  int ownReads = 0;
  late final client = MockClient((r) async {
    calls.add('${r.method} ${r.url.path}');
    if (r.method != 'GET') return http.Response('{}', 503);
    if (r.url.path.endsWith('/social-history')) {
      return jsonReply({'data': historyTestData()});
    }
    if (r.url.path.endsWith('/publication')) {
      return jsonReply({'data': publicationTestPreview()});
    }
    if (r.url.path == '/v1/me/moments') {
      ownReads++;
      if (ownPending case final pending?) {
        ownPending = null;
        return pending.future;
      }
      return jsonReply({
        'data': [workspaceOwnData('本轮个人记录')],
      });
    }
    if (r.url.path.endsWith('/activities')) return jsonReply({'data': []});
    if (r.url.path.endsWith('/venue')) return http.Response('{}', 404);
    return jsonReply({'data': workspacePlaceData()});
  });
}

Widget _workspacePlaceHarness(
  ValueNotifier<String?> workspace,
  _WorkspacePlaceTransport transport,
) => MaterialApp(
  home: Scaffold(
    body: PlaceDetailSheet(
      placeID: workspacePlaceID,
      authorizationHeader: () => 'Bearer same-person',
      organizationWorkspaceID: () => workspace.value,
      workspaceChanges: workspace,
      onOpenActivity: (_) {},
      apiBaseUrl: 'https://test',
      client: transport.client,
    ),
  ),
);

// A selected public test scope is explicit input, not guessed by the component.
// Keep the real catalog loader/selector and all identity-sensitive detail data.
Future<PublicCityController> _givenWorkspacePlaceCity() async {
  final client = MockClient(
    (r) async => jsonReply({
      'data': r.url.path == '/v1/cities'
          ? [
              {
                'id': 'synthetic-place-city',
                'name': '合成地点范围',
                'region': 'fixture',
                'contentStatus': 'test',
                'source': {
                  'label': '组件测试',
                  'maintainer': 'fixture',
                  'freshness': 'test',
                },
                'map': null,
              },
            ]
          : [],
    }),
  );
  addTearDown(client.close);
  final city = PublicCityController(
    client: client,
    apiBaseUrl: 'http://catalog-fixture.test',
  );
  await city.loadCities();
  expect(city.selectedCity, isNull);
  city.selectCity('synthetic-place-city');
  expect(city.selectedCity?.id, 'synthetic-place-city');
  return city;
}

class _WorkspacePlaceSource extends AgentTaskSource {
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => AgentResult(
    entities: const [],
    activities: const [],
    places: [PublicPlace.fromJson(workspacePlaceData())],
    note: '本地测试接口返回地点',
  );
}

void main() {
  testWidgets(
    'public navigation rejects a changed point before opening external map',
    (tester) async {
      var changed = false;
      final opened = <Uri>[];
      Map<String, dynamic> detail() => workspacePlaceData()
        ..['location'] = {
          'coordinateSystem': 'wgs84',
          'precision': 'point',
          'latitude': changed ? 58.1 : 57.1,
          'longitude': -2.1,
        };
      final client = MockClient((r) async {
        if (r.url.path.contains('/entity-actions/')) {
          final data = actionWire(
            ref: const EntityActionRef('place', workspacePlaceID),
          );
          for (final dynamic a in data['actions'] as List) {
            a['state'] = const {'NAVIGATE', 'SHARE'}.contains(a['kind'])
                ? 'AVAILABLE'
                : 'UNAVAILABLE';
            if (a['kind'] == 'SHARE') {
              a['operation'] = 'EXPORT_PUBLIC';
              a['allowedOperations'] = ['EXPORT_PUBLIC'];
              a['label'] = '打开系统分享';
            }
          }
          return jsonReply({'data': data});
        }
        if (r.url.path.endsWith('/venue')) return http.Response('{}', 404);
        if (r.url.path.endsWith('/social-history')) {
          return jsonReply({'data': historyTestData()});
        }
        if (r.url.path.endsWith('/activities') ||
            r.url.path == '/v1/me/moments') {
          return jsonReply({'data': []});
        }
        return jsonReply({'data': detail()});
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PlaceDetailSheet(
              placeID: workspacePlaceID,
              authorizationHeader: () => null,
              onOpenActivity: (_) {},
              apiBaseUrl: 'http://api.test',
              client: client,
              openExternal: (url) async {
                opened.add(url);
                return true;
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      changed = true;
      await tester.ensureVisible(find.text('导航'));
      await tester.tap(find.text('导航'));
      await tester.pumpAndSettle();
      if (find.text('继续').evaluate().isNotEmpty) {
        await tester.tap(find.text('继续'));
        await tester.pumpAndSettle();
      }
      expect(
        opened,
        isEmpty,
        reason:
            'new opaque action source must not navigate cached old coordinates',
      );
      expect(find.textContaining('导航地点已变化'), findsOneWidget);
    },
  );
  testWidgets('真实Map地点入口跨键盘和主题重建保持；真实连接更换仍失效', (t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final auth = SeedTestAuth(), city = await _givenWorkspacePlaceCity();
    final transport = _WorkspacePlaceTransport();
    final replacement = _WorkspacePlaceTransport();
    final source = _WorkspacePlaceSource();
    final seedClient = MockClient((_) async => seedResponse(completeSeed()));
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: transport.client,
      apiBaseUrl: 'https://test',
    );
    Widget app({
      Brightness brightness = Brightness.light,
      double inset = 0,
      double scale = 1,
      http.Client? detailClient,
    }) => MaterialApp(
      theme: birdtieTheme(brightness),
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(context).copyWith(
          viewInsets: EdgeInsets.only(bottom: inset),
          textScaler: TextScaler.linear(scale),
        ),
        child: child!,
      ),
      home: MapWorkspace(
        city: city,
        auth: auth,
        moments: moments,
        agentTaskSource: source,
        seedClient: seedClient,
        seedApiBaseUrl: 'https://test',
        placeDetailClient: detailClient ?? transport.client,
        placeDetailApiBaseUrl: 'https://test',
      ),
    );
    await t.pumpWidget(app());
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField).first, '找地点');
    await t.testTextInput.receiveAction(TextInputAction.send);
    await t.pumpAndSettle();
    await t.tap(find.text('入口地点'));
    await t.pumpAndSettle();
    for (final inset in [260.0, 0.0]) {
      await t.pumpWidget(
        app(brightness: Brightness.dark, inset: inset, scale: 2),
      );
      await t.pumpAndSettle();
      expect(
        find.descendant(
          of: find.byType(PlaceDetailSheet),
          matching: find.text('入口地点'),
        ),
        findsOneWidget,
      );
      expect(find.textContaining('地点页面连接已变更'), findsNothing);
      expect(t.takeException(), isNull);
    }
    // Identity-sensitive provider replacement is still a permanent boundary.
    await t.pumpWidget(app(detailClient: replacement.client));
    await t.pumpAndSettle();
    expect(find.byType(PlaceDetailSheet), findsNothing);
    expect(find.text('请重新打开内容'), findsOneWidget);
    expect(replacement.calls, isEmpty);
    await t.pumpWidget(app());
    await t.pumpAndSettle();
    expect(find.byType(PlaceDetailSheet), findsNothing);
    expect(find.text('请重新打开内容'), findsOneWidget);
    await t.binding.handlePopRoute();
    await t.pumpAndSettle();
    expect(find.text('请重新打开内容'), findsNothing);
    expect(transport.calls.every((c) => c.startsWith('GET ')), isTrue);
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    city.dispose();
    auth.dispose();
    transport.client.close();
    replacement.client.close();
    seedClient.close();
  });
  for (final brightness in Brightness.values) {
    testWidgets(
      'place $brightness long title and primary actions at 320 and text scale 3',
      (t) async {
        t.view.physicalSize = const Size(320, 740);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        const title = '这是包含非常长中文和 Aberdeen Sports Centre 的已发布地点名称';
        final writes = <String>[];
        final client = MockClient((r) async {
          if (r.method != 'GET') writes.add(r.method);
          if (r.url.path.endsWith('/social-history')) {
            return jsonReply({'data': historyTestData()});
          }
          if (r.url.path.endsWith('/activities')) {
            return jsonReply({'data': []});
          }
          if (r.url.path.endsWith('/venue')) return http.Response('{}', 404);
          return jsonReply({
            'data': {
              ...workspacePlaceData(),
              'name': title,
              'summary': '这里只显示已发布资料，没有补造营业时间。',
              'addressLabel': '合成测试地址 Aberdeen',
            },
          });
        });
        await t.pumpWidget(
          MaterialApp(
            theme: birdtieTheme(brightness),
            home: MediaQuery(
              data: const MediaQueryData(textScaler: TextScaler.linear(3)),
              child: Scaffold(
                body: PlaceDetailSheet(
                  placeID: workspacePlaceID,
                  authorizationHeader: () => null,
                  onOpenActivity: (_) {},
                  apiBaseUrl: 'https://test',
                  client: client,
                ),
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(find.text(title), findsOneWidget);
        expect(t.takeException(), isNull);
        expect(find.byType(BackdropFilter), findsNothing);
        final surface = t.widget<BirdtieSurface>(find.byType(BirdtieSurface));
        expect(surface.kind, BirdtieSurfaceKind.content);
        await t.scrollUntilVisible(
          find.text('分享地点'),
          180,
          scrollable: find
              .descendant(
                of: find.byType(PlaceDetailSheet),
                matching: find.byType(Scrollable),
              )
              .first,
        );
        expect(t.takeException(), isNull);
        expect(writes, isEmpty);
        await t.pumpWidget(const SizedBox.shrink());
        client.close();
      },
    );
  }
  testWidgets('place transport error uses opaque critical surface', (t) async {
    final client = MockClient((_) async => http.Response('{}', 503));
    await t.pumpWidget(
      MaterialApp(
        theme: birdtieTheme(Brightness.dark),
        home: Scaffold(
          body: PlaceDetailSheet(
            placeID: workspacePlaceID,
            authorizationHeader: () => null,
            onOpenActivity: (_) {},
            apiBaseUrl: 'https://test',
            client: client,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(
      t.widget<BirdtieSurface>(find.byType(BirdtieSurface)).kind,
      BirdtieSurfaceKind.critical,
    );
    expect(find.byType(BackdropFilter), findsNothing);
    expect(find.text('重试'), findsOneWidget);
    await t.pumpWidget(const SizedBox.shrink());
    client.close();
  });
  testWidgets('实际Map地点结果入口在组织工作区不读取或显示个人记录', (t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final auth = SeedTestAuth(),
        city = await _givenWorkspacePlaceCity(),
        transport = _WorkspacePlaceTransport();
    final seedClient = MockClient((_) async => seedResponse(completeSeed()));
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: transport.client,
      apiBaseUrl: 'https://test',
    );
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          agentTaskSource: _WorkspacePlaceSource(),
          seedClient: seedClient,
          seedApiBaseUrl: 'https://test',
          placeDetailClient: transport.client,
          placeDetailApiBaseUrl: 'https://test',
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField).first, '找地点');
    await t.testTextInput.receiveAction(TextInputAction.send);
    await t.pumpAndSettle();
    await t.tap(find.text('入口地点'));
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.text('本轮个人记录'),
      140,
      scrollable: find
          .descendant(
            of: find.byType(PlaceDetailSheet),
            matching: find.byType(Scrollable),
          )
          .first,
    );
    expect(transport.ownReads, 1);
    await t.binding.handlePopRoute();
    await t.pumpAndSettle();
    await t.tap(find.byTooltip('打开侧边栏'));
    await t.pumpAndSettle();
    final orgs = t.widget<Sidebar>(find.byType(Sidebar)).organizations;
    const organization = OrganizationWorkspace(
      id: 'synthetic-org',
      name: '合成组织',
      role: 'admin',
      organizationType: 'society',
    );
    orgs.organizations = [organization];
    orgs.select(null);
    await t.pumpAndSettle();
    await t.tap(find.text('个人工作区'));
    await t.pumpAndSettle();
    await t.tap(find.text('合成组织 · 管理员'));
    await t.pumpAndSettle();
    expect(orgs.active?.id, organization.id);
    await t.binding.handlePopRoute();
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField).first, '组织找地点');
    await t.testTextInput.receiveAction(TextInputAction.send);
    await t.pumpAndSettle();
    await t.tap(find.text('入口地点'));
    await t.pumpAndSettle();
    expect(transport.ownReads, 1);
    expect(find.text('本轮个人记录'), findsNothing);
    expect(find.text('查看公开预览'), findsNothing);
    expect(find.text('发给好友'), findsNothing);
    expect(find.textContaining('当前为组织工作区'), findsOneWidget);
    // Actual controller selected by the sidebar also invalidates an already-open sheet.
    orgs.select(null);
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.text('本轮个人记录'),
      140,
      scrollable: find
          .descendant(
            of: find.byType(PlaceDetailSheet),
            matching: find.byType(Scrollable),
          )
          .first,
    );
    expect(transport.ownReads, 2);
    orgs.select(organization);
    await t.pumpAndSettle();
    expect(find.text('本轮个人记录'), findsNothing);
    expect(transport.calls.where((c) => !c.startsWith('GET ')), isEmpty);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    city.dispose();
    auth.dispose();
  });
  testWidgets('组织初始地点详情仅用公开来源，零个人请求', (t) async {
    final workspace = ValueNotifier<String?>('org-one'),
        transport = _WorkspacePlaceTransport();
    addTearDown(workspace.dispose);
    await t.pumpWidget(_workspacePlaceHarness(workspace, transport));
    await t.pumpAndSettle();
    expect(find.text('入口地点'), findsOneWidget);
    expect(find.textContaining('当前为组织工作区'), findsOneWidget);
    expect(find.text('我关联这里的记录'), findsNothing);
    expect(find.text('发给好友'), findsNothing);
    expect(transport.ownReads, 0);
    expect(
      transport.calls,
      isNot(contains('GET /v1/places/$workspacePlaceID/activities')),
    );
    expect(
      transport.calls,
      contains('GET /v1/places/$workspacePlaceID/social-history'),
    );
    expect(transport.calls.every((c) => c.startsWith('GET ')), true);
  });
  testWidgets('同个人令牌组织往返的旧私人列表迟到不能覆盖新读取', (t) async {
    final workspace = ValueNotifier<String?>(null),
        transport = _WorkspacePlaceTransport();
    addTearDown(workspace.dispose);
    final late = Completer<http.Response>();
    transport.ownPending = late;
    await t.pumpWidget(_workspacePlaceHarness(workspace, transport));
    await t.pump();
    await t.pump();
    expect(transport.ownReads, 1);
    workspace.value = 'org-one';
    await t.pump();
    workspace.value = null;
    await t.pumpAndSettle();
    expect(transport.ownReads, 2);
    late.complete(
      jsonReply({
        'data': [workspaceOwnData('旧列表迟到正文')],
      }),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(find.text('本轮个人记录'), 140);
    expect(find.text('旧列表迟到正文'), findsNothing);
    expect(find.text('本轮个人记录'), findsOneWidget);
    expect(t.takeException(), isNull);
  });
  testWidgets('已勾选公开预览遇组织切换立即隐藏个人正文和批准，取消零写', (t) async {
    final workspace = ValueNotifier<String?>(null),
        transport = _WorkspacePlaceTransport();
    addTearDown(workspace.dispose);
    await t.pumpWidget(_workspacePlaceHarness(workspace, transport));
    await t.pumpAndSettle();
    await t.scrollUntilVisible(find.text('查看公开预览'), 140);
    await t.pumpAndSettle();
    await t.tap(find.text('查看公开预览'));
    await t.pumpAndSettle();
    await t.ensureVisible(find.byType(CheckboxListTile));
    await t.tap(find.byType(CheckboxListTile));
    await t.pump();
    expect(
      t.widget<FilledButton>(find.byType(FilledButton)).onPressed,
      isNotNull,
    );
    workspace.value = 'org-one';
    await t.pumpAndSettle();
    expect(find.byType(CheckboxListTile), findsNothing);
    expect(find.text('由我确认的内容'), findsNothing);
    expect(find.textContaining('个人记录和公开操作已暂停'), findsOneWidget);
    workspace.value = null;
    await t.pumpAndSettle();
    expect(find.byType(CheckboxListTile), findsNothing);
    await t.ensureVisible(find.text('取消'));
    await t.tap(find.text('取消'));
    await t.pumpAndSettle();
    expect(transport.calls.where((c) => !c.startsWith('GET ')), isEmpty);
    expect(t.takeException(), isNull);
  });

  testWidgets('公开预览打开期间账号变化清除具体批准，不发旧提交', (t) async {
    const placeID = '22222222-2222-4222-8222-222222222222';
    const momentID = '11111111-1111-4111-8111-111111111111';
    final token = ValueNotifier('Bearer self');
    addTearDown(token.dispose);
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') {
        writes++;
        return http.Response('{}', 503);
      }
      if (r.url.path.endsWith('/social-history')) {
        return jsonReply({'data': historyTestData()});
      }
      if (r.url.path.endsWith('/publication')) {
        return jsonReply({
          'data': {
            'momentId': momentID,
            'placeId': placeID,
            'cityId': 'test',
            'placeName': '测试图书馆',
            'title': '当前版本',
            'body': '明确公开的内容',
            'revision': 1,
            'snapshot': 'mp1.1.2.${'a' * 64}',
            'expiresAt': DateTime.now()
                .toUtc()
                .add(const Duration(seconds: 90))
                .toIso8601String(),
          },
        });
      }
      if (r.url.path == '/v1/me/moments') {
        return jsonReply({
          'data': [
            {
              'id': momentID,
              'placeId': placeID,
              'title': '当前版本',
              'body': '明确公开的内容',
              'revision': 1,
              'status': 'draft',
              'visibility': 'private',
              'locationPrecision': 'place',
            },
          ],
        });
      }
      if (r.url.path.endsWith('/activities')) return jsonReply({'data': []});
      if (r.url.path.endsWith('/venue')) return http.Response('{}', 404);
      return jsonReply({
        'data': {
          'id': placeID,
          'name': '测试图书馆',
          'source': {'label': '本地合成来源'},
          'location': {'coordinateSystem': '', 'precision': 'none'},
        },
      });
    });
    await t.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<String>(
          valueListenable: token,
          builder: (_, value, _) => Scaffold(
            body: PlaceDetailSheet(
              placeID: placeID,
              authorizationHeader: () => value,
              onOpenActivity: (_) {},
              apiBaseUrl: 'https://test',
              client: client,
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(find.text('查看公开预览'), 150);
    await t.pumpAndSettle();
    await t.tap(find.text('查看公开预览'));
    await t.pumpAndSettle();
    await t.ensureVisible(find.byType(CheckboxListTile));
    await t.pumpAndSettle();
    await t.tap(find.byType(CheckboxListTile));
    await t.pump();
    expect(
      t.widget<FilledButton>(find.byType(FilledButton)).onPressed,
      isNotNull,
    );
    token.value = 'Bearer another-person';
    await t.pumpAndSettle();
    expect(find.text('当前版本'), findsNothing);
    expect(find.byType(CheckboxListTile), findsNothing);
    expect(find.text('登录状态已变化，请重新查看记录。'), findsOneWidget);
    expect(writes, 0);
    expect(t.takeException(), isNull);
  });
  testWidgets('本人具体版本公开后显示权威状态并可撤回，地图Place ID稳定', (tester) async {
    const placeID = '22222222-2222-4222-8222-222222222222',
        momentID = '11111111-1111-4111-8111-111111111111';
    var status = 'draft', revision = 1, publicWrites = 0, withdraws = 0;
    Map<String, dynamic> own() => {
      'id': momentID,
      'placeId': placeID,
      'title': '我检查过的记录',
      'body': '我选择公开的内容',
      'revision': revision,
      'status': status,
      'visibility': status == 'published' ? 'public' : 'private',
      'locationPrecision': 'place',
    };
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        publicWrites++;
        expect(jsonDecode(r.body), {
          'revision': 1,
          'snapshot': 'mp1.1.2.${'a' * 64}',
          'confirmPublic': true,
        });
        status = 'published';
        revision++;
        return jsonReply({
          'data': {
            'momentId': momentID,
            'placeId': placeID,
            'revision': revision,
            'status': 'published',
            'publishedAt': DateTime.now().toUtc().toIso8601String(),
          },
        });
      }
      if (r.method == 'DELETE') {
        withdraws++;
        expect(r.url.queryParameters, {'revision': '2'});
        status = 'withdrawn';
        revision++;
        return http.Response('', 204);
      }
      if (r.url.path.endsWith('/social-history')) {
        final d = historyTestData();
        d['recentMomentCount'] = status == 'published' ? 1 : 0;
        if (status != 'published') d['recentMoments'] = <dynamic>[];
        return jsonReply({'data': d});
      }
      if (r.url.path.endsWith('/publication')) {
        return jsonReply({
          'data': {
            'momentId': momentID,
            'placeId': placeID,
            'cityId': 'test',
            'placeName': '测试图书馆',
            'title': '我检查过的记录',
            'body': '我选择公开的内容',
            'revision': revision,
            'snapshot': 'mp1.1.2.${'a' * 64}',
            'expiresAt': DateTime.now()
                .toUtc()
                .add(const Duration(seconds: 90))
                .toIso8601String(),
          },
        });
      }
      if (r.url.path == '/v1/me/moments/$momentID') {
        return jsonReply({'data': own()});
      }
      if (r.url.path == '/v1/me/moments') {
        return jsonReply({
          'data': status == 'withdrawn' ? [] : [own()],
        });
      }
      if (r.url.path.endsWith('/activities')) return jsonReply({'data': []});
      if (r.url.path.endsWith('/venue')) return http.Response('{}', 404);
      return jsonReply({
        'data': {
          'id': placeID,
          'name': '测试图书馆',
          'location': {'coordinateSystem': '', 'precision': 'none'},
          'source': {'label': '合成公开来源'},
          'summary': '公开地点',
        },
      });
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PlaceDetailSheet(
            placeID: placeID,
            authorizationHeader: () => 'Bearer self',
            onOpenActivity: (_) {},
            apiBaseUrl: 'https://test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('查看公开预览'), 150);
    await tester.pumpAndSettle();
    await tester.tap(find.text('查看公开预览'));
    await tester.pumpAndSettle();
    expect(publicWrites, 0);
    await tester.ensureVisible(find.byType(CheckboxListTile));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(CheckboxListTile));
    await tester.pump();
    await tester.ensureVisible(find.text('确认公开'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认公开'));
    await tester.pumpAndSettle();
    expect(publicWrites, 1);
    expect(find.text('已明确公开'), findsOneWidget);
    await tester.ensureVisible(find.text('撤回公开'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('撤回公开'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byType(CheckboxListTile));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(CheckboxListTile));
    await tester.pump();
    await tester.ensureVisible(find.text('确认撤回'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认撤回'));
    await tester.pumpAndSettle();
    expect(withdraws, 1);
    expect(find.text('已明确公开'), findsNothing);
    expect(tester.takeException(), isNull);
  });
  testWidgets('地点详情呈现经审核举办能力、经营关系与本人私密记录', (tester) async {
    const placeID = '11111111-1111-4111-8111-111111111111';
    final openedURLs = <Uri>[];
    final shares = <String>[];
    var currentToken = 'Bearer person';
    String? currentAuthorization() => currentToken;
    String? openedOrganization;
    String? openedBusiness;
    final client = MockClient((request) async {
      if (request.url.path.contains('/entity-actions/')) {
        final data = actionWire(ref: const EntityActionRef('place', placeID))
          ..['title'] = '测试体育馆';
        for (final dynamic a in data['actions'] as List) {
          a['state'] = const {'NAVIGATE', 'SHARE'}.contains(a['kind'])
              ? 'AVAILABLE'
              : 'UNAVAILABLE';
          if (a['kind'] == 'SHARE') {
            a['operation'] = 'EXPORT_PUBLIC';
            a['allowedOperations'] = ['EXPORT_PUBLIC'];
            a['label'] = '系统公开分享';
          }
        }
        return jsonReply({'data': data});
      }
      if (request.url.path.endsWith('/activities')) {
        return jsonReply({'data': []});
      }
      if (request.url.path.endsWith('/venue')) {
        return jsonReply({
          'data': {
            'placeId': placeID,
            'capacity': 24,
            'reservationSupport': 'external_url',
            'reservationUrl': 'https://example.org/reserve',
            'sourceUrl': 'https://example.org/venue-source',
            'reviewedAt': '2026-01-01T00:00:00Z',
            'bookingSourceVersion':
                'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
            'bookingSourceRevision':
                'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
            'bookingObservedAt': DateTime.now().toUtc().toIso8601String(),
            'bookingValidUntil': DateTime.now()
                .toUtc()
                .add(const Duration(seconds: 30))
                .toIso8601String(),
            'expiresAt': '2099-01-01T00:00:00Z',
            'suitability': ['badminton'],
            'amenities': ['meeting'],
            'operatorOrganizationId': '33333333-3333-4333-8333-333333333333',
            'operatorOrganizationName': '测试社团',
            'businesses': [
              {'id': '44444444-4444-4444-8444-444444444444', 'name': '已核验商家'},
            ],
          },
        });
      }
      if (request.url.path.endsWith('/me/moments')) {
        if (request.headers['Authorization'] != 'Bearer person') {
          return jsonReply({'data': []});
        }
        return jsonReply({
          'data': [
            {'placeId': placeID, 'title': '我的私人回忆', 'body': '只给我看'},
            {'placeId': 'elsewhere', 'title': '其他地点的记录', 'body': '不应出现'},
          ],
        });
      }
      return jsonReply({
        'data': {
          'id': placeID,
          'name': '测试体育馆',
          'categoryCode': 'sports_venue',
          'summary': '已审核的客观资料',
          'addressLabel': '测试街 1 号',
          'source': {
            'label': '城市审核资料',
            'reference': 'https://example.org/source',
          },
          'location': {
            'coordinateSystem': 'wgs84',
            'precision': 'point',
            'latitude': 57.15,
            'longitude': -2.1,
          },
        },
      });
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PlaceDetailSheet(
            placeID: placeID,
            authorizationHeader: currentAuthorization,
            onOpenActivity: (_) {},
            onOpenOrganization: (id) => openedOrganization = id,
            onOpenBusiness: (id) => openedBusiness = id,
            shareText: (text) async => shares.add(text),
            openExternal: (url) async {
              openedURLs.add(url);
              return true;
            },
            apiBaseUrl: 'https://api.test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('地点类别：体育场馆'), findsOneWidget);
    expect(find.text('资料所列容量：24 人'), findsOneWidget);
    expect(find.text('适合：羽毛球'), findsOneWidget);
    expect(find.text('已核验经营关系：已核验商家'), findsOneWidget);
    expect(find.text('举报商家'), findsOneWidget);
    await tester.ensureVisible(find.text('商家资料'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('商家资料'));
    expect(openedBusiness, '44444444-4444-4444-8444-444444444444');
    await tester.scrollUntilVisible(
      find.text('我的私人回忆'),
      150,
      scrollable: find
          .descendant(
            of: find.byType(ListView).first,
            matching: find.byType(Scrollable),
          )
          .first,
    );
    expect(find.text('我的私人回忆'), findsOneWidget);
    expect(find.text('其他地点的记录'), findsNothing);
    expect(find.text('仅自己可见，不会随地点分享。'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('分享地点'),
      -150,
      scrollable: find
          .descendant(
            of: find.byType(ListView).first,
            matching: find.byType(Scrollable),
          )
          .first,
    );
    await tester.ensureVisible(find.text('分享地点'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('分享地点'));
    await tester.pumpAndSettle();
    expect(shares, isEmpty);
    await tester.tap(find.text('打开系统分享'));
    await tester.pumpAndSettle();
    expect(shares.single, contains('测试体育馆'));
    expect(shares.single, isNot(contains('我的私人回忆')));
    await tester.ensureVisible(find.text('导航'));
    await tester.tap(find.text('导航'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('打开地图'));
    await tester.pumpAndSettle();
    expect(openedURLs.single.queryParameters['query'], '57.15,-2.1');
    await tester.scrollUntilVisible(
      find.text('场地关联组织：测试社团'),
      150,
      scrollable: find
          .descendant(
            of: find.byType(ListView).first,
            matching: find.byType(Scrollable),
          )
          .first,
    );
    await tester.tap(find.text('场地关联组织：测试社团'));
    expect(openedOrganization, '33333333-3333-4333-8333-333333333333');
    currentToken = 'Bearer another-person';
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PlaceDetailSheet(
            placeID: placeID,
            authorizationHeader: currentAuthorization,
            onOpenActivity: (_) {},
            apiBaseUrl: 'https://api.test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pump();
    expect(find.text('我的私人回忆'), findsNothing);
    await tester.pumpAndSettle();
    expect(find.text('我的私人回忆'), findsNothing);
    expect(find.text('最近没有找到关联这里的私人记录。'), findsOneWidget);
  });

  testWidgets('地图地点详情使用同一 Place ID 与真实可见活动', (tester) async {
    const placeID = '11111111-1111-4111-8111-111111111111';
    const activityID = '22222222-2222-4222-8222-222222222222';
    final paths = <String>[];
    final client = MockClient((request) async {
      paths.add(request.url.path);
      expect(request.headers['Authorization'], 'Bearer person');
      if (request.url.path.endsWith('/venue')) {
        return http.Response('{}', 404);
      }
      if (request.url.path.endsWith('/me/moments')) {
        return http.Response(jsonEncode({'data': []}), 200);
      }
      if (request.url.path.endsWith('/activities')) {
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': [
                {
                  'id': activityID,
                  'title': '周末羽毛球',
                  'startsAt': '2026-10-04T10:00:00Z',
                  'endsAt': '2026-10-04T12:00:00Z',
                  'timeZone': 'Europe/London',
                  'schedule': '周日 11:00',
                  'status': 'upcoming',
                  'source': {'label': '活动主办方'},
                },
              ],
            }),
          ),
          200,
        );
      }
      return http.Response.bytes(
        utf8.encode(
          jsonEncode({
            'data': {
              'id': placeID,
              'name': '测试体育馆',
              'categoryCode': 'sports',
              'summary': '已审核的地点',
              'addressLabel': '市中心测试街 1 号',
              'source': {'label': '城市审核资料'},
              'location': {
                'coordinateSystem': 'wgs84',
                'precision': 'point',
                'latitude': 57.15,
                'longitude': -2.1,
              },
            },
          }),
        ),
        200,
      );
    });
    String? opened;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PlaceDetailSheet(
            placeID: placeID,
            authorizationHeader: () => 'Bearer person',
            onOpenActivity: (activity) => opened = activity.id,
            apiBaseUrl: 'https://api.test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(paths, [
      '/v1/places/$placeID/social-history',
      '/v1/places/$placeID',
      '/v1/places/$placeID/activities',
      '/v1/places/$placeID/venue',
      '/v1/me/moments',
    ]);
    expect(find.text('地址：市中心测试街 1 号'), findsOneWidget);
    expect(find.text('资料来源：城市审核资料'), findsOneWidget);
    await tester.tap(find.text('周末羽毛球'));
    expect(opened, activityID);
  });

  testWidgets('地点活动读取失败时不展示缓存或虚构活动', (tester) async {
    const placeID = '11111111-1111-4111-8111-111111111111';
    final client = MockClient((request) async {
      if (request.url.path.endsWith('/activities')) {
        return http.Response('{}', 503);
      }
      return jsonReply({
        'data': {
          'id': placeID,
          'name': '测试地点',
          'source': {'label': '来源'},
          'location': {'precision': 'none'},
        },
      });
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PlaceDetailSheet(
            placeID: placeID,
            authorizationHeader: () => null,
            onOpenActivity: (_) {},
            apiBaseUrl: 'https://api.test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('测试地点'), findsOneWidget);
    expect(find.text('活动暂不可用，请稍后重试。'), findsOneWidget);
    expect(find.text('这里的活动'), findsOneWidget);
    expect(find.text('周末羽毛球'), findsNothing);
    expect(find.text('导航'), findsNothing);
    expect(find.text('我最近的私人记录'), findsNothing);
  });
  for (final id in [
    'invalid',
    '00000000-0000-0000-0000-000000000000',
    '44444444-4444-4444-8444-444444444444',
  ]) {
    testWidgets('商家入口严格 ID 且需要公共详情回调 $id', (tester) async {
      final client = MockClient((request) async {
        if (request.url.path.endsWith('/venue')) {
          return jsonReply({
            'data': {
              'placeId': workspacePlaceID,
              'reservationSupport': 'unknown',
              'sourceUrl': 'https://example.org/source',
              'reviewedAt': '2026-01-01T00:00:00Z',
              'bookingSourceVersion':
                  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
              'bookingSourceRevision':
                  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
              'bookingObservedAt': DateTime.now().toUtc().toIso8601String(),
              'bookingValidUntil': DateTime.now()
                  .toUtc()
                  .add(const Duration(seconds: 30))
                  .toIso8601String(),
              'expiresAt': '2099-01-01T00:00:00Z',
              'businesses': [
                {'id': id, 'name': '测试经营关系'},
              ],
            },
          });
        }
        if (request.url.path.endsWith('/social-history')) {
          return jsonReply({'data': historyTestData()});
        }
        if (request.url.path.endsWith('/activities')) {
          return jsonReply({'data': []});
        }
        return jsonReply({'data': workspacePlaceData()});
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PlaceDetailSheet(
              placeID: workspacePlaceID,
              authorizationHeader: () => null,
              onOpenActivity: (_) {},
              onOpenBusiness: (_) {},
              apiBaseUrl: 'https://api.test',
              client: client,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(
        find.text('商家资料'),
        id.startsWith('4444') ? findsOneWidget : findsNothing,
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PlaceDetailSheet(
              placeID: workspacePlaceID,
              authorizationHeader: () => null,
              onOpenActivity: (_) {},
              apiBaseUrl: 'https://api.test',
              client: client,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('商家资料'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      client.close();
    });
  }
}
