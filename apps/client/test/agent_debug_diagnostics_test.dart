import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/workspace/agent_debug_diagnostics.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('Agent parses the API badminton response contract', () async {
    final body = File(
      'test/fixtures/agent_badminton_response.json',
    ).readAsStringSync();
    final diagnostics = AgentDebugDiagnostics();
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => null,
      apiBaseUrl: 'http://127.0.0.1:3692',
      diagnostics: diagnostics,
      client: MockClient(
        (request) async => http.Response.bytes(
          utf8.encode(body),
          200,
          headers: {'X-Request-ID': request.headers['X-Request-ID']!},
        ),
      ),
    );
    final result = await source.resolve('badminton', const [], const []);
    expect(result.activities, hasLength(2));
    expect(diagnostics.resultCount, 2);
    source.dispose();
  });

  test(
    'Agent request ID is sent and debug result count reflects response',
    () async {
      final diagnostics = AgentDebugDiagnostics();
      String? sentID;
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => null,
        apiBaseUrl: 'http://127.0.0.1:3692',
        diagnostics: diagnostics,
        client: MockClient((request) async {
          sentID = request.headers['X-Request-ID'];
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': {
                  'activities': [],
                  'places': [],
                  'people': [],
                  'groups': [],
                  'message': '暂时没有活动。',
                },
              }),
            ),
            200,
            headers: {
              'X-Request-ID': sentID!,
              'content-type': 'application/json; charset=utf-8',
            },
          );
        }),
      );
      final result = await source.resolve('找活动', const [], const []);
      expect(RegExp(r'^[0-9a-f]{32}$').hasMatch(sentID!), isTrue);
      expect(diagnostics.requestId, sentID);
      expect(diagnostics.status, 'HTTP 200');
      expect(diagnostics.resultCount, 0);
      expect(diagnostics.authenticated, isFalse);
      expect(result.responseMessage, '暂时没有活动。');
      source.dispose();
    },
  );

  test(
    'Agent request failure keeps request ID and status in debug metadata',
    () async {
      final diagnostics = AgentDebugDiagnostics();
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => 'Bearer test-only',
        apiBaseUrl: 'http://127.0.0.1:3692',
        diagnostics: diagnostics,
        client: MockClient(
          (request) async => http.Response(
            jsonEncode({
              'error': {'code': 'service_unavailable'},
            }),
            503,
            headers: {'X-Request-ID': request.headers['X-Request-ID']!},
          ),
        ),
      );
      await expectLater(
        source.resolve('找活动', const [], const []),
        throwsA(isA<AgentRequestFailure>()),
      );
      expect(diagnostics.status, 'HTTP 503');
      expect(diagnostics.error, 'service_unavailable');
      expect(diagnostics.authenticated, isTrue);
      source.dispose();
    },
  );
}
