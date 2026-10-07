import 'dart:convert';

import 'package:birdtie_client/src/workspace/community_api.dart';
import 'package:birdtie_client/src/workspace/organization_activity_api.dart';
import 'package:birdtie_client/src/workspace/organization_console.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('活动形式在编辑前后保留线上、线下和混合地点状态', () {
    const place = 'b1700000-0000-4000-8000-000000000004';
    expect(activityLocationChoice('online', 'not_applicable'), 'online');
    expect(activityLocationChoice('hybrid', 'confirmed'), 'hybrid_confirmed');
    expect(activityLocationChoice('hybrid', 'tbd'), 'hybrid_tbd');
    expect(activityLocationFields('online', place), {
      'placeId': null,
      'modality': 'online',
      'physicalPlaceStatus': 'not_applicable',
    });
    expect(activityLocationFields('hybrid_confirmed', place), {
      'placeId': place,
      'modality': 'hybrid',
      'physicalPlaceStatus': 'confirmed',
    });
    expect(activityLocationFields('hybrid_tbd', place), {
      'placeId': null,
      'modality': 'hybrid',
      'physicalPlaceStatus': 'tbd',
    });
  });
  test('reviewed Business organizer has a Chinese label', () {
    final organizer = ActivityOrganizer.fromJson({
      'type': 'BUSINESS',
      'id': 'business-1',
      'name': '示例商家',
    });
    expect(organizer.label, '商家：示例商家');
  });

  test('only managed active communities and organizations can organize', () {
    CommunityItem community(String id, String role, String status) =>
        CommunityItem(
          id: id,
          name: id,
          description: '',
          visibility: 'public',
          joinPolicy: 'open',
          status: status,
          memberCount: 2,
          myRole: role,
          myStatus: 'active',
        );
    final options = authorizedActivityOrganizers(
      'self',
      [
        community('owned', 'owner', 'active'),
        community('admin', 'admin', 'active'),
        community('member', 'member', 'active'),
        community('archived', 'owner', 'archived'),
      ],
      businesses: const [
        ActivityOrganizer(type: 'BUSINESS', id: 'verified-biz', name: '已核验商家'),
      ],
      [
        const OrganizationWorkspace(
          id: 'org-admin',
          name: '管理组织',
          role: 'admin',
          organizationType: 'society',
        ),
        const OrganizationWorkspace(
          id: 'org-member',
          name: '普通成员组织',
          role: 'member',
          organizationType: 'society',
        ),
      ],
    );
    expect(options.map((item) => '${item.type}:${item.id}'), [
      'PERSON:self',
      'COMMUNITY:owned',
      'COMMUNITY:admin',
      'ORGANIZATION:org-admin',
      'BUSINESS:verified-biz',
    ]);
  });

  test(
    'generic activity creation and publishing sends explicit organizer',
    () async {
      final calls = <String>[];
      final client = MockClient((request) async {
        calls.add('${request.method} ${request.url.path}');
        expect(request.headers['Authorization'], 'Bearer local');
        if (request.method == 'POST' &&
            request.url.path == '/v1/me/activities') {
          final body = jsonDecode(request.body) as Map<String, dynamic>;
          expect(body['organizer'], {'type': 'COMMUNITY', 'id': 'community-1'});
        }
        final row = {
          'id': 'activity-1',
          'cityId': 'aberdeen-gb',
          'title': '周末羽毛球',
          'summary': '',
          'description': '',
          'startsAt': '2026-10-03T10:00:00Z',
          'endsAt': '2026-10-03T12:00:00Z',
          'publicationStatus': request.url.path.endsWith('/publish')
              ? 'published'
              : 'draft',
          'visibility': 'public',
          'priceMinor': 0,
          'organizer': {
            'type': 'COMMUNITY',
            'id': 'community-1',
            'name': '羽毛球社群',
          },
        };
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': row})),
          request.url.path.endsWith('/publish') ? 200 : 201,
        );
      });
      final api = OrganizationActivityApi(
        authorizationHeader: () => 'Bearer local',
        client: client,
        apiBaseUrl: 'http://api.test',
      );
      final draft = await api.saveAs(
        const ActivityOrganizer(
          type: 'COMMUNITY',
          id: 'community-1',
          name: '羽毛球社群',
        ),
        {'title': '周末羽毛球'},
      );
      expect(draft.organizer?.name, '羽毛球社群');
      final published = await api.publishMine(draft.id);
      expect(published.publicationStatus, 'published');
      expect(calls, [
        'POST /v1/me/activities',
        'POST /v1/me/activities/activity-1/publish',
      ]);
    },
  );

  test('商家主办方由本人鉴权接口读取，不从旧组织类型推断', () async {
    final api = OrganizationActivityApi(
      authorizationHeader: () => 'Bearer local',
      apiBaseUrl: 'https://api.test',
      client: MockClient((request) async {
        expect(request.method, 'GET');
        expect(request.url.path, '/v1/me/activity-organizers/businesses');
        expect(request.headers['Authorization'], 'Bearer local');
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': [
                {'type': 'BUSINESS', 'id': 'business-1', 'name': '已核验商家'},
              ],
            }),
          ),
          200,
        );
      }),
    );
    final options = await api.managedBusinessOrganizers();
    expect(options.single.label, '商家：已核验商家');
    api.dispose();
  });

  test('旧 API 尚无商家主办方接口时保留其他主办方编辑能力', () async {
    final api = OrganizationActivityApi(
      authorizationHeader: () => 'Bearer local',
      apiBaseUrl: 'https://api.test',
      client: MockClient((request) async => http.Response('{}', 404)),
    );
    expect(await api.managedBusinessOrganizers(), isEmpty);
    api.dispose();
  });
}
