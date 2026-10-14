import 'dart:async';

import 'package:birdtie_client/src/config/birdtie_environment.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/activity_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/community_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'chat_entity_router_test.dart'
    show ChatTestAuth, chatActivity, chatReply, chatDomainReply;
import 'entity_share_pending_store_test.dart' show shareOwner, shareTarget;
import 'now_scope_recovery_test.dart'
    show NowFixtureCity, NowFixtureSource, nowTap;

const _savedTitle = '已收藏的长中文活动名称 Aberdeen 国际学生周末见面活动';
const _detailTitle = '同一原始活动 ID 的当前权威详情';
const _retiredTitle = '账号切换前迟到的私密活动内容不得重新出现';
const _base = BirdtieEnvironment.apiBaseUrl;

http.Response _jsonReply(Object? value, [int status = 200]) {
  final response = chatReply(value, status);
  return http.Response.bytes(
    response.bodyBytes,
    response.statusCode,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );
}

/// Original MapWorkspace, original side bar and original detail route over
/// synthetic HTTP. Not server ACL, phone or production acceptance evidence.
class _SavedNavigationFixture {
  _SavedNavigationFixture() {
    client = MockClient(reply);
    moments = PrivateMomentController(
      client: client,
      apiBaseUrl: _base,
      authorizationHeader: () => auth.authorizationHeader,
    );
  }

  final auth = ChatTestAuth();
  final city = NowFixtureCity();
  late final source = NowFixtureSource(city);
  late final MockClient client;
  late final PrivateMomentController moments;
  final calls = <http.Request>[];
  bool available = true;
  String savedKind = 'activity';
  int detailStatus = 200;
  Completer<http.Response>? firstDetail;
  String seedBase = 'https://seed-navigation-a.test';

  Iterable<http.Request> get activityReads => calls.where(
    (r) => r.method == 'GET' && r.url.path == '/v1/activities/$shareTarget',
  );
  Iterable<http.Request> get writes => calls.where((r) => r.method != 'GET');

  Future<http.Response> reply(http.Request r) async {
    calls.add(r);
    if (r.method != 'GET') return _jsonReply(null, 500);
    if (r.url.path == '/v1/me/saved' || r.url.path == '/v1/me/saved/page') {
      final items = [
        {
          'id': '41000000-0000-0000-0000-000000000001',
          'kind': savedKind,
          'targetId': shareTarget,
          'title': _savedTitle,
          'summary': '收藏不代表报名；详情必须读取当前活动。',
          'cityId': 'alpha',
          'available': available,
        },
      ];
      return _jsonReply(
        r.url.path.endsWith('/page')
            ? {'items': items, 'nextCursor': null}
            : items,
      );
    }
    if (r.url.path == '/v1/activities/$shareTarget') {
      final held = firstDetail;
      firstDetail = null;
      if (held != null) return held.future;
      return _jsonReply({
        ...chatActivity(),
        'title': _detailTitle,
      }, detailStatus);
    }
    if (r.url.path == '/v1/activities/$shareTarget/participations/me') {
      return _jsonReply(null);
    }
    if (r.url.path == '/v1/places/$shareTarget' ||
        r.url.path == '/v1/communities/$shareTarget' ||
        r.url.path == '/v1/communities/$shareTarget/activities') {
      return chatDomainReply(r);
    }
    // Existing list/load parsers either receive a valid empty list or report
    // their original non-blocking source error. No new domain writes are faked.
    return _jsonReply([]);
  }

  Future<void> mount(WidgetTester t) async {
    await t.pumpWidget(
      MaterialApp(
        key: const ValueKey('saved-navigation-app'),
        home: MapWorkspace(
          key: const ValueKey('saved-navigation-workspace'),
          auth: auth,
          city: city,
          moments: moments,
          agentTaskSource: source,
          seedClient: client,
          seedApiBaseUrl: seedBase,
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  Future<void> open(WidgetTester t, bool fromSaved) async {
    await nowTap(t, find.byTooltip('打开侧边栏'));
    await nowTap(t, find.text('我的内容'));
    await nowTap(t, find.text(fromSaved ? '收藏' : '我的活动'));
    expect(
      find.byType(fromSaved ? SavedPage : MyActivitiesPage),
      findsOneWidget,
    );
  }

  Future<void> unmount(WidgetTester t) async {
    await t.pumpWidget(const SizedBox.shrink());
    await t.pumpAndSettle();
  }

  void dispose() {
    moments.dispose();
    city.dispose();
    auth.dispose();
    client.close();
  }
}

void _prepare(WidgetTester t) {
  // MapWorkspace intentionally uses the original compiled API namespace for
  // its personal controllers. The test command must configure that namespace.
  expect(_base, 'https://saved-navigation.test');
  FlutterSecureStorage.setMockInitialValues({});
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Future<void> _withFixture(
  WidgetTester t,
  Future<void> Function(_SavedNavigationFixture) body,
) async {
  _prepare(t);
  final f = _SavedNavigationFixture();
  addTearDown(f.dispose);
  // Capture every original http.Client() created by MapWorkspace, including
  // SavedController and Plans. No network or real model is involved.
  await http.runWithClient(() async {
    try {
      await body(f);
    } finally {
      await f.unmount(t);
    }
  }, () => MockClient(f.reply));
}

void main() {
  for (final kind in ['place', 'group']) {
    testWidgets('侧栏收藏 $kind 打开同一原生实体详情且不产生写入', (t) async {
      await _withFixture(t, (f) async {
        f.savedKind = kind;
        await f.mount(t);
        await f.open(t, true);
        await nowTap(t, find.text(_savedTitle));
        expect(
          find.byType(kind == 'place' ? PlaceDetailSheet : CommunityDetailPage),
          findsOneWidget,
        );
        final expected = kind == 'place'
            ? '/v1/places/$shareTarget'
            : '/v1/communities/$shareTarget';
        final reads = f.calls.where(
          (r) => r.method == 'GET' && r.url.path == expected,
        );
        expect(reads, hasLength(1));
        expect(reads.single.url.origin, _base);
        expect(f.writes, isEmpty);
        expect(t.takeException(), isNull);
      });
    });
  }
  for (final fromSaved in [true, false]) {
    final entry = fromSaved ? '侧栏收藏' : '我的活动收藏';

    testWidgets('$entry 经原生列表打开同 ID 稳定详情并返回，零报名或收藏写入', (t) async {
      await _withFixture(t, (f) async {
        await f.mount(t);
        await f.open(t, fromSaved);
        expect(f.activityReads, isEmpty);
        await nowTap(t, find.text(_savedTitle));
        expect(find.byType(ActivityDetailSheet), findsOneWidget);
        expect(find.text(_detailTitle), findsWidgets);
        // The outer stable router and the current detail each read the exact
        // original ID, retaining the personal API/pending-journal namespace.
        expect(f.activityReads.length, 2);
        for (final r in f.activityReads) {
          expect(r.url.origin, _base);
          expect(r.headers['Authorization'], 'Bearer A');
        }
        final detail = t.widget<ActivityDetailSheet>(
          find.byType(ActivityDetailSheet),
        );
        expect(detail.accountID?.call(), shareOwner);
        expect(detail.authorizationHeader(), 'Bearer A');
        expect(detail.apiBaseUrl, _base);
        expect(detail.workspaceID?.call(), isNull);
        expect(f.writes, isEmpty);
        expect(f.source.queries, isEmpty);
        expect(await const FlutterSecureStorage().readAll(), isEmpty);
        await t.binding.handlePopRoute();
        await t.pumpAndSettle();
        expect(find.text(_savedTitle), findsOneWidget);
        expect(find.byType(ActivityDetailSheet), findsNothing);
        expect(f.writes, isEmpty);
        expect(t.takeException(), isNull);
      });
    });

    testWidgets('$entry 不可用收藏禁用导航且不读取旧活动', (t) async {
      await _withFixture(t, (f) async {
        f.available = false;
        await f.mount(t);
        await f.open(t, fromSaved);
        final masked = find.text(fromSaved ? '内容暂不可用' : '活动暂不可用');
        await t.ensureVisible(masked);
        await t.pumpAndSettle();
        final row = find.ancestor(of: masked, matching: find.byType(ListTile));
        expect(t.widget<ListTile>(row).onTap, isNull);
        await t.tap(masked);
        await t.pumpAndSettle();
        expect(f.activityReads, isEmpty);
        expect(find.byType(ActivityDetailSheet), findsNothing);
        expect(find.text(_savedTitle), findsNothing);
        expect(f.writes, isEmpty);
        expect(t.takeException(), isNull);
      });
    });

    testWidgets('$entry 当前详情 404 拒绝旧收藏正文与报名控件', (t) async {
      await _withFixture(t, (f) async {
        f.detailStatus = 404;
        await f.mount(t);
        await f.open(t, fromSaved);
        await nowTap(t, find.text(_savedTitle));
        expect(find.text('活动已失效、不可访问，或暂时无法读取。'), findsOneWidget);
        expect(find.byType(ActivityDetailSheet), findsNothing);
        expect(find.text(_detailTitle), findsNothing);
        expect(f.activityReads.length, 1);
        expect(f.writes, isEmpty);
        expect(t.takeException(), isNull);
      });
    });

    testWidgets('$entry 账号 A→B→A 后原迟到私密正文不能恢复', (t) async {
      await _withFixture(t, (f) async {
        final pending = Completer<http.Response>();
        f.firstDetail = pending;
        await f.mount(t);
        await f.open(t, fromSaved);
        await t.ensureVisible(find.text(_savedTitle));
        await t.tap(find.text(_savedTitle));
        await t.pump();
        await t.pump(const Duration(milliseconds: 300));
        expect(f.activityReads.length, 1);
        f.auth.use('Bearer B');
        f.auth.use('Bearer A');
        pending.complete(
          _jsonReply({...chatActivity(), 'title': _retiredTitle}),
        );
        await t.pumpAndSettle();
        expect(find.text(_retiredTitle), findsNothing);
        expect(find.byType(ActivityDetailSheet), findsNothing);
        expect(f.activityReads.length, 1);
        expect(f.writes, isEmpty);
        expect(t.takeException(), isNull);
      });
    });
  }

  testWidgets('侧栏收藏同 key 来源 A→B→A 退役原详情且保留原 API 命名空间', (t) async {
    await _withFixture(t, (f) async {
      final pending = Completer<http.Response>();
      f.firstDetail = pending;
      await f.mount(t);
      await f.open(t, true);
      await t.ensureVisible(find.text(_savedTitle));
      await t.tap(find.text(_savedTitle));
      await t.pump();
      await t.pump(const Duration(milliseconds: 300));
      expect(f.activityReads.length, 1);
      expect(f.activityReads.single.url.origin, _base);
      f.seedBase = 'https://seed-navigation-b.test';
      // Do not settle a still-pending spinner. Rebind the actual parent first.
      await t.pumpWidget(
        MaterialApp(
          key: const ValueKey('saved-navigation-app'),
          home: MapWorkspace(
            key: const ValueKey('saved-navigation-workspace'),
            auth: f.auth,
            city: f.city,
            moments: f.moments,
            agentTaskSource: f.source,
            seedClient: f.client,
            seedApiBaseUrl: f.seedBase,
          ),
        ),
      );
      await t.pump();
      f.seedBase = 'https://seed-navigation-a.test';
      await f.mount(t);
      pending.complete(_jsonReply({...chatActivity(), 'title': _retiredTitle}));
      await t.pumpAndSettle();
      expect(find.text(_retiredTitle), findsNothing);
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
      expect(find.byType(ActivityDetailSheet), findsNothing);
      expect(f.activityReads.length, 1);
      expect(f.writes, isEmpty);
      expect(t.takeException(), isNull);
    });
  });
}
