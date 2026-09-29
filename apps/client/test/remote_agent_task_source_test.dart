import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('anonymous local Recent reruns the public City query', () async {
    final requests = <http.Request>[];
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen',
      authorizationHeader: () => null,
      apiBaseUrl: 'http://localhost:8080',
      client: MockClient((request) async {
        requests.add(request);
        return http.Response(
          jsonEncode({
            'data': {
              'cityId': 'aberdeen',
              'mode': 'rules',
              'activities': [],
              'people': [],
              'groups': [],
              'places': [],
            },
          }),
          200,
        );
      }),
    );
    await source.restore(
      const AgentTask(id: 'local-1', query: 'badminton', status: 'active'),
      [],
      [],
    );
    expect(requests, hasLength(1));
    expect(requests.single.method, 'POST');
    expect(requests.single.url.path, '/v1/cities/aberdeen/agent/tasks');
    expect(requests.single.headers.containsKey('Authorization'), isFalse);
    source.dispose();
  });

  test(
    'signed-in follow-up sends the same task and organization workspace',
    () async {
      final requests = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => 'Bearer local-session',
        organizationWorkspaceID: () => 'org-1',
        apiBaseUrl: 'http://localhost:8080',
        client: MockClient((request) async {
          requests.add(request);
          return http.Response(
            jsonEncode({
              'data': {
                'cityId': 'aberdeen-gb',
                'mode': 'rules',
                'taskId': 'task-1',
                'activities': [],
                'people': [],
                'groups': [],
                'places': [],
                'note': 'Showing closer activities.',
              },
            }),
            200,
          );
        }),
      );
      await source.followUp(
        const AgentTask(
          id: 'task-1',
          query: 'Find badminton this weekend',
          status: 'COMPLETED',
        ),
        'Anything closer?',
        [],
        [],
      );
      expect(requests, hasLength(1));
      expect(requests.single.method, 'POST');
      expect(jsonDecode(requests.single.body), {
        'query': 'Anything closer?',
        'taskId': 'task-1',
      });
      expect(requests.single.headers['Authorization'], 'Bearer local-session');
      expect(
        requests.single.headers['X-Birdtie-Organization-Workspace'],
        'org-1',
      );
      source.dispose();
    },
  );

  test(
    'anonymous follow-up carries prior intent without a local fake task ID',
    () async {
      final requests = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => null,
        apiBaseUrl: 'http://localhost:8080',
        client: MockClient((request) async {
          requests.add(request);
          return http.Response(
            jsonEncode({
              'data': {
                'cityId': 'aberdeen-gb',
                'mode': 'rules',
                'activities': [],
                'people': [],
                'groups': [],
                'places': [],
              },
            }),
            200,
          );
        }),
      );
      await source.followUp(
        const AgentTask(
          id: 'local-1',
          query: 'Find badminton this weekend',
          status: 'COMPLETED',
        ),
        'Anything closer?',
        [],
        [],
      );
      expect(jsonDecode(requests.single.body), {
        'query': 'Find badminton this weekend. Anything closer?',
      });
      source.dispose();
    },
  );
}
