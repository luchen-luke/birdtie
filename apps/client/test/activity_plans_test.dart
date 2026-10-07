import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class TrackingClient extends MockClient {
  TrackingClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

void main() {
  test('Plans支持合法offset及严格日期，不虚构线上地址', () {
    final row = {
      'id': 'p',
      'activityId': 'a',
      'startsAt': '2026-10-05T19:30:00+08:00',
      'endsAt': '2026-10-05T20:30:00+08:00',
      'modality': 'online',
      'physicalPlaceStatus': 'not_applicable',
      'timeZone': 'Asia/Shanghai',
    };
    final p = ActivityPlan.fromJson(row);
    expect(p.startsAt, DateTime.utc(2026, 10, 5, 11, 30));
    expect(p.endsAt!.difference(p.startsAt!), const Duration(hours: 1));
    for (final invalid in [
      '2026-02-30T19:30:00Z',
      '2026-10-05T19:30:00+25:00',
      'yesterday',
    ]) {
      expect(
        () => ActivityPlan.fromJson({...row, 'startsAt': invalid}),
        throwsA(anything),
      );
    }
  });
  test('Plans真实身份通知A-B-A使迟到GET退休，borrowed关闭0', () async {
    var token = 'Bearer A';
    final change = ChangeNotifier(), pending = Completer<http.Response>();
    var calls = 0;
    final client = TrackingClient((r) async {
      calls++;
      return pending.future;
    });
    final c = ActivityPlansController(
      authorizationHeader: () => token,
      client: client,
      apiBaseUrl: 'https://api.test',
      identityChanges: change,
    );
    final load = c.load();
    await Future<void>.delayed(Duration.zero);
    token = 'Bearer B';
    change.notifyListeners();
    token = 'Bearer A';
    change.notifyListeners();
    pending.complete(
      http.Response(
        '{"data":[{"id":"p","activityId":"a","title":"OLD_PRIVATE"}]}',
        200,
      ),
    );
    await load;
    expect(c.plans, isEmpty);
    expect(c.failed, isFalse);
    expect(calls, 1);
    c.dispose();
    c.dispose();
    expect(client.closes, 0);
    change.dispose();
    client.close();
  });
  test('Plans通知期间切换身份在提交前退休，零POST', () async {
    var token = 'Bearer A', calls = 0;
    final change = ChangeNotifier();
    final client = TrackingClient((r) async {
      calls++;
      return http.Response('', 201);
    });
    final c = ActivityPlansController(
      authorizationHeader: () => token,
      client: client,
      apiBaseUrl: 'https://api.test',
      identityChanges: change,
    );
    c.addListener(() {
      if (c.busy.isNotEmpty) {
        token = 'Bearer B';
        change.notifyListeners();
        token = 'Bearer A';
        change.notifyListeners();
      }
    });
    await c.toggle('a');
    expect(calls, 0);
    expect(c.busy, isEmpty);
    c.dispose();
    change.dispose();
    client.close();
  });
  test('Plans失效来源响应清旧列表，dispose迟到不通知', () async {
    final pending = Completer<http.Response>();
    var calls = 0;
    final client = TrackingClient((r) async {
      calls++;
      return calls == 1
          ? http.Response('{"data":[{"id":"p","activityId":"a"}]}', 200)
          : pending.future;
    });
    final c = ActivityPlansController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    await c.load();
    expect(c.plans.length, 1);
    final load = c.load();
    c.dispose();
    pending.complete(http.Response('', 409));
    await load;
    expect(client.closes, 0);
    client.close();
  });
  test('私人提醒写结果迟到A-B-A清空后零新读取，借用client不关闭', () async {
    var token = 'Bearer A', calls = 0;
    final pending = Completer<http.Response>();
    final client = MockClient((r) async {
      calls++;
      return r.method == 'POST'
          ? pending.future
          : http.Response('{"data":[]}', 200);
    });
    final controller = ActivityPlansController(
      authorizationHeader: () => token,
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    final result = controller.toggle('target');
    await Future<void>.delayed(Duration.zero);
    token = 'Bearer B';
    controller.clear();
    token = 'Bearer A';
    controller.clear();
    pending.complete(http.Response('{"data":{"id":"original-plan"}}', 201));
    await result;
    expect(calls, 1);
    expect(controller.plans, isEmpty);
    expect(controller.busy, isEmpty);
    controller.dispose();
    client.close();
  });
  test('My Activities adds and removes a private plan', () async {
    var planned = false;
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer test-session');
      switch ('${request.method} ${request.url.path}') {
        case 'GET /v1/me/activity-plans':
          return http.Response(
            jsonEncode({
              'data': planned
                  ? [
                      {
                        'id': 'plan-1',
                        'activityId': 'activity-1',
                        'title': 'Badminton',
                        'cityId': 'aberdeen-gb',
                        'status': 'upcoming',
                        'available': true,
                        'startsAt': '2026-10-01T12:00:00Z',
                      },
                    ]
                  : [],
            }),
            200,
          );
        case 'POST /v1/me/activity-plans':
          expect(jsonDecode(request.body), {'activityId': 'activity-1'});
          planned = true;
          return http.Response('{"data":{"id":"plan-1"}}', 201);
        case 'DELETE /v1/me/activity-plans/plan-1':
          planned = false;
          return http.Response('', 204);
        default:
          return http.Response('', 404);
      }
    });
    final plans = ActivityPlansController(
      authorizationHeader: () => 'Bearer test-session',
      client: client,
      apiBaseUrl: 'http://localhost:8080',
    );
    await plans.load();
    expect(plans.plans, isEmpty);
    await plans.toggle('activity-1');
    expect(plans.contains('activity-1'), isTrue);
    await plans.toggle('activity-1');
    expect(plans.plans, isEmpty);
    plans.dispose();
  });
}
