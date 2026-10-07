import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('Now allows the bounded supplier round beyond 12 seconds', (
    tester,
  ) async {
    final response = Completer<http.Response>();
    var requests = 0;
    var finished = false;
    Object? failure;
    final source = RemoteAgentTaskSource(
      cityID: () => 'alpha',
      authorizationHeader: () => null,
      apiBaseUrl: 'https://deadline-fixture.test',
      client: MockClient((request) {
        requests++;
        return response.future;
      }),
    );
    final pending = source
        .resolve('找地点', [], [])
        .then<void>(
          (_) {
            finished = true;
          },
          onError: (Object error) {
            finished = true;
            failure = error;
          },
        );
    await tester.pump();
    await tester.pump(const Duration(seconds: 20));
    expect(requests, 1);
    expect(finished, isFalse);
    response.complete(
      http.Response(
        jsonEncode({
          'error': {'code': 'supplier_unavailable'},
        }),
        503,
      ),
    );
    await tester.pump();
    await pending;
    expect(finished, isTrue);
    expect(failure, isA<AgentRequestFailure>());
    expect((failure as AgentRequestFailure).statusCode, 503);
    expect(requests, 1);
    source.dispose();
  });

  testWidgets('Now original timeout has one completion and no retry', (
    tester,
  ) async {
    final response = Completer<http.Response>();
    var requests = 0;
    var completions = 0;
    Object? failure;
    final source = RemoteAgentTaskSource(
      cityID: () => 'alpha',
      authorizationHeader: () => null,
      apiBaseUrl: 'https://deadline-fixture.test',
      client: MockClient((request) {
        requests++;
        return response.future;
      }),
    );
    final pending = source
        .resolve('找地点', [], [])
        .then<void>(
          (_) {
            completions++;
          },
          onError: (Object error) {
            completions++;
            failure = error;
          },
        );
    await tester.pump();
    await tester.pump(const Duration(seconds: 34));
    expect(completions, 0);
    await tester.pump(const Duration(seconds: 2));
    await pending;
    expect(completions, 1);
    expect(failure, isA<AgentRequestFailure>());
    expect((failure as AgentRequestFailure).userMessage, contains('超时'));
    // A late supplier response does not complete the failed turn or send again.
    response.complete(http.Response('{}', 200));
    await tester.pump();
    expect(completions, 1);
    expect(requests, 1);
    source.dispose();
  });
}
