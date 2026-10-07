import 'dart:convert';

import 'package:birdtie_client/src/workspace/organization_activity_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('draft, publish and list use authenticated organization API', () async {
    final calls = <String>[];
    final activity = <String, dynamic>{
      'id': 'activity-1',
      'organizationId': 'org-1',
      'cityId': 'aberdeen-gb',
      'title': '羽毛球活动',
      'summary': '周末',
      'description': '周末羽毛球',
      'startsAt': '2026-10-03T10:00:00Z',
      'endsAt': '2026-10-03T12:00:00Z',
      'publicationStatus': 'draft',
      'visibility': 'public',
      'priceMinor': 0,
    };
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer local-test');
      calls.add('${request.method} ${request.url.path}');
      if (request.method == 'GET') {
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': [activity],
            }),
          ),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      if (request.url.path.endsWith('/publish')) {
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': {...activity, 'publicationStatus': 'published'},
            }),
          ),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      return http.Response.bytes(
        utf8.encode(jsonEncode({'data': activity})),
        201,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    final api = OrganizationActivityApi(
      authorizationHeader: () => 'Bearer local-test',
      client: client,
      apiBaseUrl: 'http://127.0.0.1:3693',
    );
    final draft = await api.save('org-1', {'title': '羽毛球活动'});
    final published = await api.publish('org-1', draft.id);
    final items = await api.list('org-1');
    expect(draft.publicationStatus, 'draft');
    expect(published.publicationStatus, 'published');
    expect(items.single.title, '羽毛球活动');
    expect(calls, [
      'POST /v1/me/organizations/org-1/activities',
      'POST /v1/me/organizations/org-1/activities/activity-1/publish',
      'GET /v1/me/organizations/org-1/activities',
    ]);
  });

  test('backend role denial has Chinese UI message', () async {
    final api = OrganizationActivityApi(
      authorizationHeader: () => 'Bearer member',
      client: MockClient(
        (_) async => http.Response(
          '{"error":{"code":"organization_admin_required"}}',
          403,
        ),
      ),
      apiBaseUrl: 'http://127.0.0.1:3693',
    );
    await expectLater(
      api.list('org-1'),
      throwsA(
        isA<OrganizationActivityException>().having(
          (error) => error.chineseMessage,
          'message',
          contains('管理员'),
        ),
      ),
    );
  });
}
