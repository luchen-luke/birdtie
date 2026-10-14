import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/config/birdtie_environment.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/activity_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/activity_participation_operation.dart';
import 'package:birdtie_client/src/workspace/activity_participation_pending_store.dart';
import 'package:birdtie_client/src/workspace/activity_participations.dart';
import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'chat_entity_router_test.dart' show ChatTestAuth, chatActivity;
import 'entity_action_contract_test.dart' show actionWire;
import 'entity_share_pending_store_test.dart' show shareOwner, shareTarget;
import 'now_scope_recovery_test.dart'
    show NowFixtureCity, NowFixtureSource, nowTap;

const _base = BirdtieEnvironment.apiBaseUrl;
const _title = '已收藏的长中文活动名称 Aberdeen 国际学生周末见面活动';
const _detailTitle = '实际收藏入口同一活动 ID 的长中文 English 权威详情';
const _entryFilter = String.fromEnvironment('BIRDTIE_TEST_ROUTE_ENTRY');

http.Response _json(Object? value, [int status = 200]) => http.Response(
  jsonEncode({'data': value}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

class _RouteFixture {
  _RouteFixture({this.directPlans = false}) {
    client = MockClient(reply);
    replacement = MockClient(reply);
    moments = PrivateMomentController(
      client: client,
      apiBaseUrl: _base,
      authorizationHeader: token,
    );
    organizations = OrganizationWorkspaceController(
      client: client,
      apiBaseUrl: _base,
      authorizationHeader: token,
    );
    plansA = ActivityPlansController(
      client: client,
      apiBaseUrl: _base,
      authorizationHeader: token,
    );
    plansB = ActivityPlansController(
      client: replacement,
      apiBaseUrl: 'https://plan-source-b.test',
      authorizationHeader: token,
    );
    plans = plansA;
    participations = ActivityParticipationController(
      client: client,
      apiBaseUrl: _base,
      authorizationHeader: token,
    );
    saved = SavedController(
      client: client,
      apiBaseUrl: _base,
      authorizationHeader: token,
    );
  }
  final bool directPlans;
  final auth = ChatTestAuth();
  final city = NowFixtureCity();
  late final source = NowFixtureSource(city);
  final presentation = ValueNotifier<int>(0);
  late final MockClient client, replacement;
  late final PrivateMomentController moments;
  late final OrganizationWorkspaceController organizations;
  late final ActivityPlansController plansA, plansB;
  late ActivityPlansController plans;
  late final ActivityParticipationController participations;
  late final SavedController saved;
  late http.Client seedClient = client;
  String seedBase = 'https://seed-route-a.test';
  ThemeMode theme = ThemeMode.light;
  double scale = 1;
  final calls = <http.Request>[];
  final store = SecureParticipationPendingStore();
  bool responseLost = false, joined = false;
  PendingParticipationOperation? original;

  String? token() => auth.authorizationHeader;
  Iterable<http.Request> get posts => calls.where((r) => r.method == 'POST');
  Iterable<http.Request> get executes => posts.where(
    (r) => r.url.path == '/v1/activities/$shareTarget/participation-operations',
  );
  Iterable<http.Request> get closes =>
      posts.where((r) => r.url.path.endsWith('/close'));
  Iterable<http.Request> get operationGets => calls.where(
    (r) =>
        r.method == 'GET' && r.url.path.contains('/participation-operations/'),
  );

  Map<String, dynamic> binding(PendingParticipationOperation p) => {
    'ownerId': shareOwner,
    'activityId': shareTarget,
    'operationId': p.operationID,
    'operation': p.operation,
    'requestDigest': p.requestDigest,
    'effectKey': participationEffectKey(shareOwner, p.operationID),
    'toolVersion': participationToolVersion,
    'recordedAt': '2026-10-10T08:00:00.123456Z',
  };
  Future<http.Response> reply(http.Request r) async {
    calls.add(r);
    if (r.url.path.contains('/entity-actions/')) {
      final wire = actionWire(
        ref: const EntityActionRef('activity', shareTarget),
      );
      for (final dynamic action in wire['actions'] as List) {
        action['state'] = action['kind'] == 'JOIN'
            ? 'AVAILABLE'
            : 'UNAVAILABLE';
        if (action['kind'] == 'JOIN') action['label'] = '报名参加';
      }
      return _json(wire);
    }
    if (r.method == 'POST' &&
        r.url.path.endsWith('/participation-operations')) {
      final pending = (await store.read(_base, shareOwner)).single;
      original = pending;
      expect(r.url.origin, _base);
      expect(r.headers['Authorization'], 'Bearer A');
      expect(r.headers['X-Birdtie-Action-Version'], 'a' * 64);
      expect(jsonDecode(r.body), {
        'operationId': pending.operationID,
        'operation': 'JOIN',
      });
      expect(pending.activityID, shareTarget);
      if (responseLost) {
        throw TimeoutException('synthetic before-forward response loss');
      }
      joined = true;
      return _json({
        ...binding(pending),
        'schemaVersion': participationOperationSchema,
        'status': 'COMMITTED',
        'effectId': 'a1000000-0000-4000-8000-000000000004',
        'state': 'going',
        'effectAt': '2026-10-10T08:00:00.123Z',
      }, 201);
    }
    if (r.method == 'POST' && r.url.path.endsWith('/close')) {
      final pending = (await store.read(_base, shareOwner)).single;
      expect(pending.stopRequested, true);
      expect(pending.sameBinding(original!), true);
      expect(
        r.url.path,
        '/v1/activities/$shareTarget/participation-operations/${original!.operationID}/close',
      );
      expect(jsonDecode(r.body), {
        'operation': pending.operation,
        'requestDigest': pending.requestDigest,
      });
      return _json({
        ...binding(pending),
        'schemaVersion': participationClosureSchema,
        'closed': true,
      });
    }
    if (r.url.path.contains('/participation-operations/')) {
      expect(r.method, 'GET');
      expect(
        r.url.path,
        '/v1/activities/$shareTarget/participation-operations/${original!.operationID}',
      );
      return _json(null, 404);
    }
    if (r.method != 'GET') return _json(null, 500);
    if (r.url.path == '/v1/me/saved' || r.url.path == '/v1/me/saved/page') {
      final items = [
        {
          'id': '41000000-0000-0000-0000-000000000001',
          'kind': 'activity',
          'targetId': shareTarget,
          'title': _title,
          'summary': '收藏不代表报名；原生 UI 必须进入真实当前详情。',
          'cityId': 'alpha',
          'available': true,
        },
      ];
      return _json(
        r.url.path.endsWith('/page')
            ? {'items': items, 'nextCursor': null}
            : items,
      );
    }
    if (r.url.path == '/v1/activities/$shareTarget') {
      return _json({...chatActivity(), 'title': _detailTitle});
    }
    if (r.url.path.endsWith('/participations/me')) {
      return _json(joined ? {'status': 'going'} : null);
    }
    return _json([]);
  }

  Widget app() => ValueListenableBuilder<int>(
    valueListenable: presentation,
    builder: (_, _, _) => MaterialApp(
      key: const ValueKey('route-binding-app'),
      theme: ThemeData.light(),
      darkTheme: ThemeData.dark(),
      themeMode: theme,
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(
          context,
        ).copyWith(textScaler: TextScaler.linear(scale)),
        child: child!,
      ),
      home: directPlans
          ? Scaffold(
              body: MyActivitiesPage(
                key: const ValueKey('route-binding-plans'),
                plans: plans,
                participations: participations,
                saved: saved,
                auth: auth,
                city: city,
                organizations: organizations,
              ),
            )
          : MapWorkspace(
              key: const ValueKey('route-binding-map'),
              auth: auth,
              city: city,
              moments: moments,
              agentTaskSource: source,
              seedClient: seedClient,
              seedApiBaseUrl: seedBase,
            ),
    ),
  );
  Future<void> mountAndOpen(WidgetTester t, bool fromSaved) async {
    await t.pumpWidget(app());
    await t.pumpAndSettle();
    if (!directPlans) {
      await nowTap(t, find.byTooltip('打开侧边栏'));
      await nowTap(t, find.text('我的内容'));
      await nowTap(t, find.text(fromSaved ? '收藏' : '我的活动'));
    }
    expect(
      find.byType(fromSaved ? SavedPage : MyActivitiesPage),
      findsOneWidget,
    );
    await nowTap(t, find.text(_title));
    expect(find.byType(ActivityDetailSheet), findsOneWidget);
    final detail = t.widget<ActivityDetailSheet>(
      find.byType(ActivityDetailSheet),
    );
    expect(detail.accountID?.call(), shareOwner);
    expect(detail.workspaceID?.call(), isNull);
    expect(detail.apiBaseUrl, _base);
    expect(posts, isEmpty);
  }

  Future<void> approve(WidgetTester t) async {
    await nowTap(t, find.text('报名参加'));
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(posts, isEmpty);
    await nowTap(t, find.text('确认报名'));
  }

  Future<void> redraw(
    WidgetTester t, {
    required double textScale,
    bool dark = false,
    bool narrow = false,
  }) async {
    scale = textScale;
    theme = dark ? ThemeMode.dark : ThemeMode.light;
    if (narrow) t.view.physicalSize = const Size(320, 844);
    presentation.value++;
    await t.pumpAndSettle();
    expect(
      find.byType(ActivityDetailSheet),
      findsOneWidget,
      reason:
          'same actual entry must retain live detail under presentation rebuild',
    );
    expect(find.text('请重新打开内容'), findsNothing);
  }

  Future<void> unmount(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  }

  void dispose() {
    presentation.dispose();
    moments.dispose();
    organizations.dispose();
    plansA.dispose();
    plansB.dispose();
    saved.dispose();
    participations.dispose();
    city.dispose();
    auth.dispose();
    client.close();
    replacement.close();
  }
}

Future<void> _fixture(
  WidgetTester t,
  Future<void> Function(_RouteFixture) body, {
  bool directPlans = false,
}) async {
  expect(_base, 'https://saved-navigation.test');
  FlutterSecureStorage.setMockInitialValues({});
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  final f = _RouteFixture(directPlans: directPlans);
  await http.runWithClient(() async {
    try {
      await body(f);
    } finally {
      await f.unmount(t);
      f.dispose();
    }
  }, () => MockClient(f.reply));
}

void main() {
  testWidgets('真实两收藏入口重建与报名恢复八场景', (t) async {
    // Keep the original secure journal serial tail inside one FakeAsync zone.
    Future<void> scenario(String name, Future<void> Function() body) async {
      debugPrint('SCENARIO START: $name');
      await body();
      debugPrint('SCENARIO PASS: $name');
    }

    for (final fromSaved in [true, false]) {
      final entry = fromSaved ? 'saved' : 'plans';
      if (_entryFilter.isNotEmpty && entry != _entryFilter) continue;
      await scenario('$entry 原生入口 UNKNOWN 正常字号窄屏仍可核对同ID和明确停止', () async {
        await _fixture(t, (f) async {
          f.responseLost = true;
          await f.mountAndOpen(t, fromSaved);
          await f.approve(t);
          expect(f.executes, hasLength(1));
          final pending = (await f.store.read(_base, shareOwner)).single;
          expect(pending.sameBinding(f.original!), true);
          expect(find.text('停止这次提交'), findsOneWidget);
          if (!const bool.fromEnvironment(
            'BIRDTIE_TEST_NORMAL_TEXT_ONLY',
            defaultValue: true,
          )) {
            // Preserve the historical cases; current user scope runs normal text only.
            await f.redraw(t, textScale: 1.3);
            await f.redraw(t, textScale: 2, dark: true, narrow: true);
          } else {
            await f.redraw(t, textScale: 1);
            await f.redraw(t, textScale: 1, dark: true, narrow: true);
          }
          expect(
            (await f.store.read(_base, shareOwner)).single.sameBinding(pending),
            true,
          );
          expect(f.executes, hasLength(1));
          expect(f.operationGets, isEmpty);
          await nowTap(t, find.text('核对原操作'));
          expect(f.operationGets, hasLength(1));
          expect(
            f.operationGets.single.url.path.endsWith('/${pending.operationID}'),
            true,
          );
          await nowTap(t, find.text('停止这次提交'));
          expect(find.text('确认停止提交'), findsOneWidget);
          await nowTap(t, find.text('确认停止提交'));
          expect(f.closes, hasLength(1));
          expect(f.executes, hasLength(1));
          expect(f.posts, hasLength(2));
          expect(await f.store.read(_base, shareOwner), isEmpty);
          expect(find.text('已停止提交，原报名结果未核实。请查看当前报名状态。'), findsOneWidget);
          expect(t.takeException(), isNull);
        });
      });
      await scenario('$entry 原生入口批准dialog重建只一次JOIN', () async {
        await _fixture(t, (f) async {
          await f.mountAndOpen(t, fromSaved);
          await nowTap(t, find.text('报名参加'));
          expect(find.byType(AlertDialog), findsOneWidget);
          await f.redraw(t, textScale: 1.3, dark: true);
          await nowTap(t, find.text('确认报名'));
          expect(f.executes, hasLength(1));
          expect(f.posts, hasLength(1));
          expect(find.text('取消报名'), findsOneWidget);
          expect(await f.store.read(_base, shareOwner), isEmpty);
          expect(t.takeException(), isNull);
        });
      });
      await scenario('$entry 原生入口真实账号A-B-A旧确认零POST', () async {
        await _fixture(t, (f) async {
          await f.mountAndOpen(t, fromSaved);
          await nowTap(t, find.text('报名参加'));
          expect(find.byType(AlertDialog), findsOneWidget);
          f.auth.use('Bearer B');
          f.auth.use('Bearer A');
          await t.pumpAndSettle();
          final confirm = find.text('确认报名');
          if (confirm.evaluate().isNotEmpty) await nowTap(t, confirm);
          expect(f.posts, isEmpty);
          expect(await f.store.read(_base, shareOwner), isEmpty);
          expect(find.byType(ActivityDetailSheet), findsNothing);
          expect(t.takeException(), isNull);
        });
      });
      await scenario('$entry 原生入口真实来源A-B-A旧确认零POST', () async {
        await _fixture(t, (f) async {
          await f.mountAndOpen(t, fromSaved);
          await nowTap(t, find.text('报名参加'));
          expect(find.byType(AlertDialog), findsOneWidget);
          if (fromSaved) {
            f.seedClient = f.replacement;
            f.seedBase = 'https://seed-route-b.test';
          } else {
            f.plans = f.plansB;
          }
          f.presentation.value++;
          await t.pump();
          if (fromSaved) {
            f.seedClient = f.client;
            f.seedBase = 'https://seed-route-a.test';
          } else {
            f.plans = f.plansA;
          }
          f.presentation.value++;
          await t.pumpAndSettle();
          final confirm = find.text('确认报名');
          if (confirm.evaluate().isNotEmpty) await nowTap(t, confirm);
          expect(f.posts, isEmpty);
          expect(await f.store.read(_base, shareOwner), isEmpty);
          expect(find.byType(ActivityDetailSheet), findsNothing);
          expect(t.takeException(), isNull);
        }, directPlans: !fromSaved);
      });
    }
  });
}
