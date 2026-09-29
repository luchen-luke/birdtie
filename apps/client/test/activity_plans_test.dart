import 'dart:convert';

import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
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
