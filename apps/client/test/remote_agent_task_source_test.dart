import 'dart:convert';

import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test(
    'remote result maps only public point entities and carries task history',
    () async {
      final client = MockClient((request) async {
        expect(request.headers['Authorization'], 'Bearer test');
        if (request.method == 'POST') {
          expect(request.url.path, '/v1/cities/aberdeen-gb/agent/tasks');
          expect(jsonDecode(request.body)['query'], 'badminton');
          return http.Response(
            jsonEncode({
              'data': {
                'cityId': 'aberdeen-gb',
                'query': 'badminton',
                'mode': 'rules',
                'taskId': '11111111-1111-1111-1111-111111111111',
                'activities': [
                  {
                    'id': 'activity-1',
                    'hostLabel': 'Club',
                    'title': 'Badminton',
                    'summary': '',
                    'startsAt': '2026-10-03T10:00:00Z',
                    'endsAt': '2026-10-03T11:00:00Z',
                    'timeZone': 'Europe/London',
                    'status': 'upcoming',
                    'source': {'label': 'Club'},
                    'location': {
                      'coordinateSystem': 'wgs84',
                      'precision': 'point',
                      'latitude': 57.14,
                      'longitude': -2.10,
                    },
                  },
                ],
                'people': [
                  {
                    'accountId': 'person-1',
                    'displayName': 'Kevin',
                    'topic': 'Badminton',
                    'areaLabel': 'Aberdeen',
                  },
                ],
                'groups': [
                  {
                    'id': 'group-1',
                    'name': 'Players',
                    'summary': '',
                    'location': {
                      'coordinateSystem': 'wgs84',
                      'precision': 'city',
                    },
                  },
                ],
                'places': [],
              },
            }),
            200,
          );
        }
        if (request.url.path == '/v1/me/agent-tasks') {
          return http.Response(
            jsonEncode({
              'data': [
                {
                  'id': '11111111-1111-1111-1111-111111111111',
                  'cityId': 'aberdeen-gb',
                  'query': 'badminton',
                  'status': 'active',
                },
              ],
            }),
            200,
          );
        }
        return http.Response('{}', 404);
      });
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => 'Bearer test',
        client: client,
        apiBaseUrl: 'https://birdtie.example',
      );
      final result = await source.resolve('badminton', [], []);
      expect(result.taskID, '11111111-1111-1111-1111-111111111111');
      expect(result.activities, hasLength(1));
      expect(result.people, hasLength(1));
      expect(result.groups, hasLength(1));
      expect(result.entities, hasLength(1));
      expect(result.entities.single.title, 'Badminton');
      expect(result.entities.single.isDemo, isFalse);
      final recent = await source.loadRecent();
      expect(recent.single.cityID, 'aberdeen-gb');
      source.dispose();
    },
  );
}
