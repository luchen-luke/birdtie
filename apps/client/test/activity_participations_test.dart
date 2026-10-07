import 'dart:convert';
import 'dart:async';

import 'package:birdtie_client/src/workspace/activity_participations.dart';
import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'chat_entity_router_test.dart' show ChatTestAuth, chatDomainReply;
import 'entity_share_pending_store_test.dart' show shareTarget;

http.Response plansReply(Object? v) => http.Response(
  jsonEncode({'data': v}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
http.Response plansDomainReply(http.Request r) {
  final v = chatDomainReply(r);
  return http.Response.bytes(
    v.bodyBytes,
    v.statusCode,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );
}

Widget plansTestHost(MyActivitiesPage page, {double scale = 1}) => MaterialApp(
  builder: (c, w) => MediaQuery(
    data: MediaQuery.of(c).copyWith(textScaler: TextScaler.linear(scale)),
    child: w!,
  ),
  home: Scaffold(body: page),
);
void main() {
  test('Plans时刻跨午夜/BST-GMT不伪造英国当地时间，未知时区明确', () {
    for (final pair in [
      ('2030-07-01T01:00:00+01:00', '2030-07-01T00:00:00Z'),
      ('2030-01-01T00:30:00Z', '2030-01-01T08:30:00+08:00'),
    ]) {
      final a = plansNullableTime(pair.$1), b = plansNullableTime(pair.$2);
      expect(a, b);
      expect(
        plansSchedule(
          a,
          a!.add(const Duration(days: 1, hours: 2)),
          'Europe/London',
        ),
        plansSchedule(
          b,
          b!.add(const Duration(days: 1, hours: 2)),
          'Europe/London',
        ),
      );
      expect(
        plansSchedule(a, b, 'Europe/London'),
        contains('你的设备时间 · 活动时区 Europe/London（未换算）'),
      );
    }
    expect(
      plansSchedule(DateTime.utc(2030, 1, 1), null, ''),
      contains('活动时区待确认'),
    );
  });
  for (final retired in [false, true]) {
    testWidgets('Plans当前原ID详情/同key运输退休，零身份写 $retired', (t) async {
      final auth = ChatTestAuth();
      final city = PublicCityController();
      final org = OrganizationWorkspaceController(
        authorizationHeader: () => auth.authorizationHeader,
      );
      var detailReads = 0, writes = 0;
      final pending = Completer<http.Response>();
      final client = MockClient((r) async {
        expect(r.headers['Authorization'], 'Bearer A');
        if (r.method != 'GET') {
          writes++;
        }
        if (r.url.path == '/v1/me/participations') {
          return plansReply([
            {
              'id': 'p',
              'activityId': shareTarget,
              'status': 'going',
              'available': true,
              'title': '真实原活动稳定详情',
              'startsAt': '2030-10-03T08:00:00Z',
              'endsAt': '2030-10-03T10:00:00Z',
              'timeZone': 'Europe/London',
              'modality': 'online',
              'physicalPlaceStatus': 'not_applicable',
              'activityStatus': 'upcoming',
            },
          ]);
        }
        if (r.url.path == '/v1/me/activity-plans' ||
            r.url.path == '/v1/me/saved' ||
            r.url.path == '/v1/me/activities') {
          return plansReply([]);
        }
        if (r.url.path == '/v1/activities/$shareTarget') {
          detailReads++;
          return retired ? pending.future : plansDomainReply(r);
        }
        return plansDomainReply(r);
      });
      ActivityPlansController make(http.Client c, String base) =>
          ActivityPlansController(
            authorizationHeader: () => auth.authorizationHeader,
            client: c,
            apiBaseUrl: base,
          );
      final plans = make(client, 'https://api.a');
      final participation = ActivityParticipationController(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://api.a',
      );
      final saved = SavedController(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://api.a',
      );
      MyActivitiesPage page(ActivityPlansController p) => MyActivitiesPage(
        key: const ValueKey('same'),
        plans: p,
        participations: participation,
        saved: saved,
        auth: auth,
        city: city,
        organizations: org,
      );
      await t.pumpWidget(plansTestHost(page(plans)));
      await t.pumpAndSettle();
      await t.tap(find.text('真实原活动稳定详情'));
      await t.pump();
      await t.pump(const Duration(milliseconds: 300));
      expect(
        detailReads,
        retired ? 1 : 2,
      ); // Outer stable router and current ActivityDetail each re-read the same original ID.
      ActivityPlansController? replacement;
      if (retired) {
        replacement = make(client, 'https://api.b');
        await t.pumpWidget(plansTestHost(page(replacement)));
        await t.pump();
        await t.pumpWidget(plansTestHost(page(plans)));
        await t.pump();
        pending.complete(
          plansDomainReply(
            http.Request(
              'GET',
              Uri.parse('https://api.a/v1/activities/$shareTarget'),
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(find.text('当前活动'), findsNothing);
      } else {
        await t.pumpAndSettle();
        expect(find.text('当前活动'), findsWidgets);
      }
      expect(writes, 0);
      await t.pumpWidget(const SizedBox.shrink());
      await t.pump();
      replacement?.dispose();
      plans.dispose();
      participation.dispose();
      saved.dispose();
      org.dispose();
      city.dispose();
      auth.dispose();
      client.close();
    });
  }
  testWidgets('Plans 320大字长标题具体日期地点可滚动读，无overflow', (t) async {
    t.view.physicalSize = const Size(320, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final client = MockClient(
      (r) async => plansReply(
        r.url.path == '/v1/me/participations'
            ? [
                {
                  'id': 'p',
                  'activityId': shareTarget,
                  'status': 'going',
                  'available': true,
                  'title': '这是一项很长的中文活动名称需要完整阅读具体时间地点而不是被省略',
                  'startsAt': '2030-10-03T08:00:00+08:00',
                  'endsAt': '2030-10-03T10:00:00+08:00',
                  'timeZone': 'Asia/Shanghai',
                  'modality': 'in_person',
                  'physicalPlaceStatus': 'tbd',
                  'activityStatus': 'upcoming',
                },
              ]
            : [],
      ),
    );
    final a = ChatTestAuth(),
        city = PublicCityController(),
        org = OrganizationWorkspaceController(
          authorizationHeader: () => 'Bearer A',
        );
    final plans = ActivityPlansController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    final p = ActivityParticipationController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    final saved = SavedController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    await t.pumpWidget(
      plansTestHost(
        MyActivitiesPage(
          plans: plans,
          participations: p,
          saved: saved,
          auth: a,
          city: city,
          organizations: org,
        ),
        scale: 3,
      ),
    );
    await t.pumpAndSettle();
    await t.drag(find.byType(ListView), const Offset(0, -450));
    await t.pumpAndSettle();
    expect(find.textContaining('活动时区 Asia/Shanghai'), findsOneWidget);
    expect(find.textContaining('线下活动 · 地点待定'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
    plans.dispose();
    p.dispose();
    saved.dispose();
    a.dispose();
    city.dispose();
    org.dispose();
    client.close();
  });
  test('报名offset及线上地点具体时间显示，缺项不猜', () {
    final p = ActivityParticipation.fromJson({
      'id': 'p',
      'activityId': 'a',
      'status': 'going',
      'startsAt': '2026-10-05T19:30:00+08:00',
      'endsAt': '2026-10-05T20:30:00+08:00',
      'modality': 'online',
      'physicalPlaceStatus': 'not_applicable',
      'timeZone': 'Asia/Shanghai',
    });
    expect(p.startsAt, DateTime.utc(2026, 10, 5, 11, 30));
    expect(p.joined, isTrue);
    expect(
      plansLocation(p.modality, p.physicalPlaceStatus, p.placeName),
      '线上活动 · 进入方式请查看活动详情',
    );
    expect(
      plansSchedule(p.startsAt, p.endsAt, p.timeZone),
      contains('活动时区 Asia/Shanghai'),
    );
    expect(plansLocation('in_person', 'tbd', ''), '线下活动 · 地点待定');
    expect(plansSchedule(null, null, ''), '时间待确认');
  });
  test('报名GET身份ABA/迟到退休且不把提醒升级为报名', () async {
    var token = 'Bearer A';
    final change = ChangeNotifier(), pending = Completer<http.Response>();
    final client = MockClient((r) async {
      expect(r.method, 'GET');
      return pending.future;
    });
    final c = ActivityParticipationController(
      authorizationHeader: () => token,
      identityChanges: change,
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    final load = c.load();
    token = 'Bearer B';
    change.notifyListeners();
    token = 'Bearer A';
    change.notifyListeners();
    pending.complete(
      http.Response(
        '{"data":[{"id":"p","activityId":"a","status":"going","title":"OLD_PRIVATE"}]}',
        200,
      ),
    );
    await load;
    expect(c.items, isEmpty);
    c.dispose();
    change.dispose();
    client.close();
  });
  test('报名失效source 409移除旧payload', () async {
    var good = true;
    final client = MockClient(
      (r) async => good
          ? http.Response(
              '{"data":[{"id":"p","activityId":"a","status":"going"}]}',
              200,
            )
          : http.Response('', 409),
    );
    final c = ActivityParticipationController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    await c.load();
    expect(c.items.length, 1);
    good = false;
    await c.load();
    expect(c.items, isEmpty);
    expect(c.failed, isTrue);
    c.dispose();
    client.close();
  });
  testWidgets(
    'My Activities separates joined, saved, past and private reminders',
    (tester) async {
      final client = MockClient((request) async {
        expect(request.headers['Authorization'], 'Bearer test');
        final data = switch (request.url.path) {
          '/v1/me/participations' => [
            {
              'id': 'p1',
              'activityId': 'a1',
              'status': 'going',
              'title': '周六羽毛球',
              'cityId': 'aberdeen-gb',
              'startsAt': '2026-10-03T10:00:00Z',
              'activityStatus': 'upcoming',
              'available': true,
            },
            {
              'id': 'p2',
              'activityId': 'a2',
              'status': 'pending',
              'title': '已结束的比赛',
              'cityId': 'aberdeen-gb',
              'startsAt': '2026-09-01T10:00:00Z',
              'activityStatus': 'completed',
              'available': true,
            },
          ],
          '/v1/me/saved' => [
            {
              'id': 's1',
              'kind': 'activity',
              'targetId': 'a3',
              'title': '收藏的活动',
              'summary': '',
              'cityId': 'aberdeen-gb',
              'available': true,
            },
          ],
          '/v1/me/activity-plans' => [
            {
              'id': 'r1',
              'activityId': 'a4',
              'title': '只是个人提醒',
              'cityId': 'aberdeen-gb',
              'status': 'upcoming',
              'available': true,
            },
          ],
          _ => <Object>[],
        };
        return http.Response(
          jsonEncode({'data': data}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      final attendance = ActivityParticipationController(
        authorizationHeader: () => 'Bearer test',
        client: client,
        apiBaseUrl: 'http://api.test',
      );
      final saved = SavedController(
        authorizationHeader: () => 'Bearer test',
        client: client,
        apiBaseUrl: 'http://api.test',
      );
      final plans = ActivityPlansController(
        authorizationHeader: () => 'Bearer test',
        client: client,
        apiBaseUrl: 'http://api.test',
      );
      final auth = BirdtieAuthController(apiBaseUrl: 'http://api.test');
      final city = PublicCityController();
      final organizations = OrganizationWorkspaceController(
        authorizationHeader: () => auth.authorizationHeader,
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MyActivitiesPage(
              plans: plans,
              participations: attendance,
              saved: saved,
              auth: auth,
              city: city,
              organizations: organizations,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('即将参加'), findsOneWidget);
      expect(find.text('周六羽毛球'), findsOneWidget);
      expect(find.textContaining('已报名 · 即将开始'), findsOneWidget);
      expect(find.text('收藏的活动'), findsNWidgets(2));
      expect(find.text('已结束'), findsOneWidget);
      await tester.drag(find.byType(ListView), const Offset(0, -300));
      await tester.pumpAndSettle();
      expect(find.textContaining('已结束\n'), findsOneWidget);
      expect(find.text('个人提醒不代表已报名或确认参加。'), findsOneWidget);
      expect(attendance.items.length, 2);
      await tester.pumpWidget(const SizedBox.shrink());
      attendance.dispose();
      saved.dispose();
      plans.dispose();
      auth.dispose();
      city.dispose();
      organizations.dispose();
    },
  );
}
