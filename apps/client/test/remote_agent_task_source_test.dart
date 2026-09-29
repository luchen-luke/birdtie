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
}
